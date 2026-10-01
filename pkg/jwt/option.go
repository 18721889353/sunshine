package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 本包支持的 HMAC 签名算法，通过 WithSigningMethod 选用。
//
// 注意：以下三个为包级导出变量（沿袭 golang-jwt 上游设计），仅供读取与传参使用，
// 库外代码禁止赋值——对其重新赋值会波及同进程内所有引用方（全局可变状态）。
var (
	// HS256 HMAC-SHA256 签名算法，本包默认算法。
	HS256 = jwt.SigningMethodHS256
	// HS384 HMAC-SHA384 签名算法。
	HS384 = jwt.SigningMethodHS384
	// HS512 HMAC-SHA512 签名算法。
	HS512 = jwt.SigningMethodHS512
)

// 默认配置项，未通过 Option 显式设置时使用。
//
// 安全设计：本包不提供默认签名密钥。未通过 WithSigningKey 设置密钥时，
// 签发/解析返回 ErrSigningKeyNotConfigured（fail loud），
// 避免「配置漏读密钥后静默用公开密钥签发 token」的漏洞路径；
// 生产环境可额外传 RequireSigningKey()，把问题提前到 New/Reload 阶段返回 error（fail fast）。
var (
	defaultSigningMethod = HS256          // 默认签名算法
	defaultExpire        = 24 * time.Hour // 默认有效期
	defaultIssuer        = ""             // 默认签发者（空表示不写入 iss）
)

// options 实例配置，由 New/Reload 统一构建后存入 Manager.optStore。
type options struct {
	signingKey    []byte
	expire        time.Duration
	issuer        string
	signingMethod *jwt.SigningMethodHMAC

	// 解析侧校验配置：期望签发者 / 期望受众 / 时钟偏差容忍，零值表示不做对应校验
	expectedIssuer   string
	expectedAudience []string
	leeway           time.Duration

	// requireSigningKey 为 true 时，New/Reload 要求 signingKey 非空，否则返回错误（见 RequireSigningKey）
	requireSigningKey bool

	// validMethods 解析侧算法白名单，由 finalize 在 apply 之后预计算（见 finalize 注释），
	// buildParseOptions 直接引用，省去每次解析时重新分配 []string
	validMethods []string

	// 以下为可选的 RegisteredClaims 字段，零值表示不写入对应 token 声明
	subject   string
	audience  []string
	id        string
	notBefore time.Time
}

// defaultOptions 构造默认配置，默认值集中在本函数管理。
// 注意：signingKey 默认为 nil，本包刻意不提供默认密钥（安全设计，见文件头注释）。
func defaultOptions() *options {
	return &options{
		signingMethod: defaultSigningMethod,
		expire:        defaultExpire,
		issuer:        defaultIssuer,
	}
}

// Option 实例配置项，通过 New / Reload 传入，未设置的项保留默认值。
type Option func(*options)

// apply 依次应用配置项，后应用的同名配置覆盖先前值（最后赋值胜出）。
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// finalize 在 apply 完成后预计算派生配置（当前为解析侧算法白名单），
// 由 New / Reload 在 Store 前调用，保证白名单与生效配置同批原子提交。
//
// buildParseOptions 对未 finalize 的配置保留兜底（现场构造白名单），
// 即使未来新增构造路径遗漏本方法，也不会静默失去算法白名单（保持 fail loud）。
func (o *options) finalize() {
	// 防御：defaultOptions 总是设置默认算法，WithSigningMethod(nil) 也会被忽略，
	// 正常路径下 signingMethod 恒非 nil；此处兜底覆盖「未来新增 options 构造路径
	// 遗漏默认值」的情况——回退到默认算法（与未设置该选项的语义一致）而非 panic
	if o.signingMethod == nil {
		o.signingMethod = defaultSigningMethod
	}
	o.validMethods = []string{o.signingMethod.Alg()}
}

// WithSigningKey 设置签名密钥，影响所属 Manager 的全部签发与解析方法。
//
// 传空串视为未设置：忽略并保留当前值（不覆盖），避免配置中心返回空值时
// 把已有密钥静默清空。未设置密钥时，签发/解析返回 ErrSigningKeyNotConfigured。
// 生产环境必须显式设置，建议配合 RequireSigningKey() 在 New 阶段校验。
func WithSigningKey(key string) Option {
	return func(o *options) {
		if key == "" {
			return
		}
		o.signingKey = []byte(key)
	}
}

// RequireSigningKey 要求 New/Reload 时必须已配置非空签名密钥，否则直接返回
// ErrSigningKeyNotConfigured（fail fast，不创建/不更新实例）。
//
// 不传本选项时，未配置密钥的问题会推迟到首次签发/解析 token 才返回
// ErrSigningKeyNotConfigured（fail loud）；生产环境传入本选项可把失败
// 提前到启动阶段，让「配置漏读密钥」在服务对外服务前就暴露。
func RequireSigningKey() Option {
	return func(o *options) {
		o.requireSigningKey = true
	}
}

// WithSigningMethod 设置签名算法，可选 HS256、HS384、HS512，默认 HS256。
// 传入 nil 时忽略，保留当前值，避免签发时因算法为空而 panic。
func WithSigningMethod(sm *jwt.SigningMethodHMAC) Option {
	return func(o *options) {
		if sm != nil {
			o.signingMethod = sm
		}
	}
}

// WithExpire 设置 token 有效期，从签发时刻起计算，默认 24 小时。
//
// 边界语义（有意不拦截非正值）：
//   - 0 表示签发即过期（exp = 签发时刻），负值表示签发即已过期；
//   - 负值是测试构造「已过期 token」的标准手段（如 TestParseTokenExpired
//     用 WithExpire(-time.Minute) 即时构造，无需 sleep 等待）；
//   - 生产环境必须传正值，传 0 或负值属于配置错误，会在首次解析时
//     以 ErrTokenExpired 形式暴露（fail loud，不会静默接受过期 token）。
func WithExpire(d time.Duration) Option {
	return func(o *options) {
		o.expire = d
	}
}

// WithIssuer 设置签发者（iss 字段），空串表示不写入该字段。
// 注意：本选项只控制写入；解析侧的 iss 校验由 WithExpectedIssuer 配置（读写分离）。
func WithIssuer(issuer string) Option {
	return func(o *options) {
		o.issuer = issuer
	}
}

// WithExpectedIssuer 设置解析时期望的签发者（iss 校验），空串表示不校验（默认）。
//
// 未设置时，任何携带有效签名的 token 都能通过解析——哪怕 iss 不属于本服务；
// 多租户 / 多服务共享同一密钥的场景下，应显式设置本选项，
// 由 golang-jwt/v5 按 WithIssuer 语义拒绝 iss 不匹配的 token（ErrTokenInvalidIssuer）。
// 与写入端的 WithIssuer 相互独立：前者管校验，后者管签发。
func WithExpectedIssuer(issuer string) Option {
	return func(o *options) {
		o.expectedIssuer = issuer
	}
}

// WithExpectedAudience 设置解析时期望的受众（aud 校验），空表示不校验（默认）。
//
// 与 WithExpectedIssuer 同理：多服务共享密钥时用它隔离受众；
// 校验语义（至少匹配 / 全部匹配）由 golang-jwt/v5 的 WithAudience 定义。
// 入参会内部拷贝，调用方后续修改原切片不影响已生效的配置。
func WithExpectedAudience(audience ...string) Option {
	return func(o *options) {
		if len(audience) == 0 {
			o.expectedAudience = nil
			return
		}
		o.expectedAudience = append([]string(nil), audience...)
	}
}

// WithLeeway 设置解析时的时钟偏差容忍（leeway），默认 0（不容忍）。
//
// 分布式部署下各节点系统时钟存在偏差，token 在过期边界会被相邻节点
// 随机接受或拒绝；生产环境建议按节点间最大时钟偏差设置（如 30s）。
// 传非正值时忽略，保留当前值（避免负 leeway 反向收紧校验）。
func WithLeeway(d time.Duration) Option {
	return func(o *options) {
		if d <= 0 {
			return
		}
		o.leeway = d
	}
}

// WithSubject 设置主题（sub 字段），空串表示不写入该字段。
func WithSubject(subject string) Option {
	return func(o *options) {
		o.subject = subject
	}
}

// WithAudience 设置受众（aud 字段），nil 或空切片表示不写入该字段。
// 入参会内部拷贝，调用方后续修改原切片不影响已生效的配置（与签发入参 map 的浅拷贝口径一致）。
func WithAudience(audience []string) Option {
	return func(o *options) {
		if len(audience) == 0 {
			o.audience = nil
			return
		}
		o.audience = append([]string(nil), audience...)
	}
}

// WithID 设置 JWT ID（jti 字段），空串表示不写入该字段。
func WithID(id string) Option {
	return func(o *options) {
		o.id = id
	}
}

// WithNotBefore 设置生效时间（nbf 字段），零值表示不写入该字段。
func WithNotBefore(notBefore time.Time) Option {
	return func(o *options) {
		o.notBefore = notBefore
	}
}
