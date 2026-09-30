// Package jwt 提供基于 HMAC 签名的 JWT token 签发、解析与刷新能力，服务于登录态校验场景。
//
// 核心功能：
//   - 标准 token：GenerateToken / ParseToken 以 uid + name 为主体，可附带自定义字段，
//     适合常规登录态（Authorization: Bearer <token>）。
//   - 自定义字段 token：GenerateCustomToken / ParseCustomToken 以纯 KV 字段为主体，
//     解析后用 CustomClaims.Get / GetString / GetInt / GetUint64 取值。
//   - token 刷新：RefreshToken / RefreshCustomToken 在原 token 未过期时重新签发，延长有效期。
//   - 选项模式：Init 配合 Option（WithSigningKey、WithExpire、WithSigningMethod 等）集中配置，
//     未显式设置的选项使用默认值，优先级：单字段选项 > 默认值。
//   - 安全默认值：本包不提供默认签名密钥，未配置密钥时签发/解析返回
//     ErrSigningKeyNotConfigured（fail loud）；生产环境建议再传 RequireSigningKey()
//     把问题提前到 Init 阶段 panic 暴露。
//
// 使用方式：
//   - 初始化：jwt.Init(jwt.WithSigningKey("..."), jwt.WithExpire(time.Hour))
//   - 签发：token, err := jwt.GenerateToken("10001", "sunshine")
//   - 校验：claims, err := jwt.ParseToken(token)
//
// 设计说明 — 全局配置的并发安全：
//
// Init 与所有签发/解析函数共享同一份全局配置，而 Init 可能在运行期被配置热更新重复调用
// （见 internal/config 的 reloadJwtConfig），此时请求线程正在并发解析 token。
// 因此配置存放在 atomic.Pointer[options] 中：Init 只做原子写，签发/解析只做原子读，
// 读写之间无需加锁，也不会读到「写了一半的配置」。
//
// 注意：多次 Init 之间应由调用方保证串行（配置热更新回调天然串行），以最后一次调用整体生效。
//
// 设计说明 — sub 字段的双层声明：
//
// CustomRegisteredClaims 在嵌入 jwt.RegisteredClaims 的同时声明了 `Subject any`，
// 两者 json 标签同为 "sub"，encoding/json 按「浅层字段优先」规则只序列化外层的 any 字段。
// 这是有意保留的兼容设计：历史 token 的 sub 可能是数字等非字符串类型，
// 只有 any 才能保证旧 token 仍可解析。读取 sub 时请使用 CustomRegisteredClaims.Subject。
package jwt

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrTokenExpired token 已过期，可用 errors.Is 判断，等价于 jwt.ErrTokenExpired。
	ErrTokenExpired = jwt.ErrTokenExpired
	// ErrNotInitialized 包尚未初始化，签发或解析前需先调用 Init。
	ErrNotInitialized = errors.New("jwt 尚未初始化，请先调用 jwt.Init()")
	// ErrSignatureInvalid token 校验未通过（claims 类型断言失败或校验位为假），
	// 属兜底错误；签名不匹配等常见失败由底层库错误直接返回，可用 errors.Is(jwt.ErrTokenSignatureInvalid) 判断。
	ErrSignatureInvalid = errors.New("token 签名校验失败")
	// ErrSigningKeyNotConfigured 签名密钥未配置：Init 未传 WithSigningKey（或只传了空串）。
	// 本包刻意不提供默认密钥，避免静默用公开密钥签发 token；用 errors.Is 判断。
	ErrSigningKeyNotConfigured = errors.New("jwt 签名密钥未配置，请调用 jwt.Init(jwt.WithSigningKey(\"...\")) 设置")
)

// optStore 全局配置，原子读写保证 Init 与签发/解析并发安全；未 Init 时为 nil。
var optStore atomic.Pointer[options]

// Init 初始化全局 jwt 配置，可多次调用（如配置热更新），以最后一次调用整体生效。
// 未显式设置的选项使用默认值，不传任何选项等价于恢复默认配置。
//
// 安全校验：本包不提供默认签名密钥，未配置密钥时签发/解析会返回
// ErrSigningKeyNotConfigured；若传入 RequireSigningKey() 而密钥缺失或为空，
// Init 直接 panic（fail fast），防止服务带病启动。
func Init(opts ...Option) {
	o := defaultOptions()
	o.apply(opts...)
	if o.requireSigningKey && len(o.signingKey) == 0 {
		panic(ErrSigningKeyNotConfigured)
	}
	optStore.Store(o)
}

// currentOptions 返回当前全局配置，并做两级前置校验：
//  1. 未初始化（optStore 为 nil）返回 ErrNotInitialized；
//  2. 已初始化但签名密钥为空返回 ErrSigningKeyNotConfigured。
//
// 所有签发/解析入口都经由本函数，保证「未配置密钥」在任何路径上都 fail loud，
// 调用方必须显式处理该错误，不得直接解引用返回值。
func currentOptions() (*options, error) {
	o := optStore.Load()
	if o == nil {
		return nil, ErrNotInitialized
	}
	if len(o.signingKey) == 0 {
		return nil, ErrSigningKeyNotConfigured
	}
	return o, nil
}

// buildRegisteredClaims 按指定时间点和配置构造 RegisteredClaims。
// 所有签发入口（GenerateToken / GenerateCustomToken / RefreshToken / RefreshCustomToken）
// 统一走本函数，避免四处重复构造导致字段更新遗漏。
//
// 注意：notBefore 为零值时不写入 nbf 字段，避免向 token 写入零值时间戳（-62135596800）；
// 调用方通过 WithNotBefore 显式设置后才写入。
func buildRegisteredClaims(o *options, now time.Time) jwt.RegisteredClaims {
	registeredClaims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(now.Add(o.expire)),
		IssuedAt:  jwt.NewNumericDate(now),
		Subject:   o.subject,
		Issuer:    o.issuer,
		Audience:  o.audience,
		ID:        o.id,
	}
	if !o.notBefore.IsZero() {
		registeredClaims.NotBefore = jwt.NewNumericDate(o.notBefore)
	}
	return registeredClaims
}

// CustomRegisteredClaims 自定义注册声明。
//
// Subject 有意遮蔽内嵌 jwt.RegisteredClaims 的同名字段（json 标签同为 "sub"），
// 用于兼容 sub 为数字等非字符串类型的历史 token，详见包注释「sub 字段的双层声明」。
type CustomRegisteredClaims struct {
	jwt.RegisteredClaims
	Subject any `json:"sub,omitempty"`
}

// Claims 标准 token 的声明，包含 uid、name、自定义字段和注册声明，
// 与 GenerateToken / ParseToken / RefreshToken 配套使用。
type Claims struct {
	UID    string         `json:"uid"`
	Name   string         `json:"name"`
	Fields map[string]any `json:"data"`
	CustomRegisteredClaims
}

// GenerateToken 签发标准 token，claims 包含 uid、name 与可选的自定义字段。
// kvs 只取第一个 map 作为附加字段；不传或传空时 Fields 为空 map。
// 返回的字符串可直接放入 Authorization: Bearer <token> 请求头。
//
// 并发约定：内部会遍历传入的 map 做浅拷贝，Go map 非并发安全，
// 调用方需保证该 map 在函数返回前不被并发读写；浅拷贝不递归，嵌套 map/切片等
// 引用类型的 value 由调用方自行保证并发安全。
func GenerateToken(uid string, name string, kvs ...map[string]any) (string, error) {
	o, err := currentOptions()
	if err != nil {
		return "", err
	}

	// 1. 附加字段只取第一个可变参数，多余的忽略；
	//    浅拷贝一份再使用，避免调用方在签发期间并发修改原 map 引发 data race
	fields := make(map[string]any)
	if len(kvs) > 0 && kvs[0] != nil {
		fields = make(map[string]any, len(kvs[0]))
		for k, v := range kvs[0] {
			fields[k] = v
		}
	}

	// 2. 组装 claims，注册声明统一由 buildRegisteredClaims 构造
	claims := Claims{
		UID:    uid,
		Name:   name,
		Fields: fields,
		CustomRegisteredClaims: CustomRegisteredClaims{
			RegisteredClaims: buildRegisteredClaims(o, time.Now()),
			Subject:          o.subject,
		},
	}

	// 3. 用配置的 HMAC 算法签名
	token := jwt.NewWithClaims(o.signingMethod, claims)
	return token.SignedString(o.signingKey)
}

// ParseToken 解析并校验标准 token，返回 Claims。
// 校验内容包括签名、过期时间（exp）等；token 过期返回 ErrTokenExpired。
func ParseToken(tokenString string) (*Claims, error) {
	o, err := currentOptions()
	if err != nil {
		return nil, err
	}
	return parseTokenWith(tokenString, o)
}

// parseTokenWith 用指定的配置快照解析并校验标准 token，是 ParseToken 与 RefreshToken
// 的共享内核：RefreshToken 借此让「校验」与「重新签发」冻结在同一份配置上，
// 避免两次 Load 之间配置热更新造成的中间态（见 RefreshToken 注释）。
func parseTokenWith(tokenString string, o *options) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(_ *jwt.Token) (interface{}, error) {
		return o.signingKey, nil
	}, buildParseOptions(o)...)
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrSignatureInvalid
}

// buildParseOptions 按配置快照构造解析侧的校验选项，标准与自定义 token 两条链路共用：
//   - 限定允许的签名算法（只信任当前配置的算法，收紧 alg 攻击面）；
//   - leeway > 0 时启用时钟偏差容忍；
//   - expectedIssuer / expectedAudience 非零值时启用 iss / aud 校验
//     （多租户隔离），未设置时保持 golang-jwt/v5 默认的不校验行为。
//
// 校验项均为显式条件追加，不依赖底层库「空值自动跳过」的内部行为，避免版本升级时语义漂移。
func buildParseOptions(o *options) []jwt.ParserOption {
	parseOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{o.signingMethod.Alg()}),
	}
	if o.leeway > 0 {
		parseOpts = append(parseOpts, jwt.WithLeeway(o.leeway))
	}
	if o.expectedIssuer != "" {
		parseOpts = append(parseOpts, jwt.WithIssuer(o.expectedIssuer))
	}
	if len(o.expectedAudience) > 0 {
		parseOpts = append(parseOpts, jwt.WithAudience(o.expectedAudience...))
	}
	return parseOpts
}

// RefreshToken 刷新标准 token，在保留 uid、name 与自定义字段的前提下按当前配置重新签发。
//
// 注意：刷新前会先校验原 token，因此已过期的 token 无法刷新
// （返回 ErrTokenExpired），只支持「未过期时提前续期」的用法。
//
// 配置一致性：整个刷新流程（校验 + 重新签发）只 Load 一次配置，
// 冻结在同一份配置快照上，避免两次 Load 之间配置热更新导致
// 「用旧配置校验、用新配置签发」的中间态；跨调用的语义仍是「按刷新时刻的配置重新签发」。
func RefreshToken(tokenString string) (string, error) {
	o, err := currentOptions()
	if err != nil {
		return "", err
	}

	claims, err := parseTokenWith(tokenString, o)
	if err != nil {
		return "", err
	}

	// 重新签发：业务字段沿用原 claims，注册声明按同一份配置快照刷新（有效期重新计算）
	claims.CustomRegisteredClaims.RegisteredClaims = buildRegisteredClaims(o, time.Now())
	claims.CustomRegisteredClaims.Subject = o.subject

	token := jwt.NewWithClaims(o.signingMethod, claims)
	return token.SignedString(o.signingKey)
}

// -------------------------------------------------------------------------------------------

// KV 自定义字段 map 类型。
type KV = map[string]any

// CustomClaims 自定义字段 token 的声明，与 GenerateCustomToken / ParseCustomToken /
// RefreshCustomToken 配套使用，业务数据全部放在 Fields 中。
type CustomClaims struct {
	Fields KV `json:"data"`
	CustomRegisteredClaims
}

// Get 按 key 读取自定义字段，字段不存在时返回 false。
func (c *CustomClaims) Get(key string) (val interface{}, isExist bool) {
	if c.Fields == nil {
		return nil, false
	}
	val, isExist = c.Fields[key]
	return val, isExist
}

// GetString 按 key 读取字符串类型字段，字段不存在或类型不匹配时返回 false。
func (c *CustomClaims) GetString(key string) (string, bool) {
	val, isExist := c.Get(key)
	if isExist {
		if str, ok := val.(string); ok {
			return str, ok
		}
	}
	return "", false
}

// GetInt 按 key 读取整数类型字段，字段不存在或类型不匹配时返回 false。
// 经 token JSON 往返后数字为 float64，内存对象中可能为 int，两者都支持。
// 类型集合有意与 GetUint64 不对称：负 int 能被无损读出，不受无符号转换影响。
func (c *CustomClaims) GetInt(key string) (int, bool) {
	val, isExist := c.Get(key)
	if isExist {
		if v, ok := val.(float64); ok {
			return int(v), true
		}
		if v, ok := val.(int); ok {
			return v, true
		}
	}
	return 0, false
}

// GetUint64 按 key 读取无符号整数类型字段，字段不存在或类型不匹配时返回 false。
// 经 token JSON 往返后数字为 float64，内存对象中可能为 uint64，两者都支持。
//
// 类型集合有意不包含内存对象中的 int（与 GetInt 不对称）：负 int 若被
// uint64(v) 静默转换会得到巨大的无符号值，属于数据损坏而非读取成功，
// 故返回 false 让调用方显式感知类型不匹配；需要读 int 请用 GetInt。
func (c *CustomClaims) GetUint64(key string) (uint64, bool) {
	val, isExist := c.Get(key)
	if isExist {
		if v, ok := val.(float64); ok {
			return uint64(v), true
		}
		if v, ok := val.(uint64); ok {
			return v, true
		}
	}
	return 0, false
}

// GenerateCustomToken 按自定义字段签发 token，业务数据全部放在 kv 中。
// kv 为 nil 时签发的 token 解析后 Fields 为 nil。
// 非 nil 的 kv 会先浅拷贝再使用，避免调用方在签发期间并发修改原 map 引发 data race。
//
// 并发约定：Go map 非并发安全，调用方需保证 kv 在函数返回前不被并发读写；
// 浅拷贝不递归，嵌套 map/切片等引用类型的 value 由调用方自行保证并发安全。
func GenerateCustomToken(kv KV) (string, error) {
	o, err := currentOptions()
	if err != nil {
		return "", err
	}

	var fields KV
	if kv != nil {
		fields = make(KV, len(kv))
		for k, v := range kv {
			fields[k] = v
		}
	}

	claims := CustomClaims{
		Fields: fields,
		CustomRegisteredClaims: CustomRegisteredClaims{
			RegisteredClaims: buildRegisteredClaims(o, time.Now()),
			Subject:          o.subject,
		},
	}

	token := jwt.NewWithClaims(o.signingMethod, claims)
	return token.SignedString(o.signingKey)
}

// ParseCustomToken 解析并校验自定义字段 token，返回 CustomClaims。
// 校验内容包括签名、过期时间（exp）等；token 过期返回 ErrTokenExpired。
func ParseCustomToken(tokenString string) (*CustomClaims, error) {
	o, err := currentOptions()
	if err != nil {
		return nil, err
	}
	return parseCustomTokenWith(tokenString, o)
}

// parseCustomTokenWith 用指定的配置快照解析并校验自定义字段 token，
// 是 ParseCustomToken 与 RefreshCustomToken 的共享内核（同 parseTokenWith）。
func parseCustomTokenWith(tokenString string, o *options) (*CustomClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(_ *jwt.Token) (interface{}, error) {
		return o.signingKey, nil
	}, buildParseOptions(o)...)
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*CustomClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrSignatureInvalid
}

// RefreshCustomToken 刷新自定义字段 token，在保留 Fields 的前提下按当前配置重新签发。
//
// 注意：刷新前会先校验原 token，因此已过期的 token 无法刷新
// （返回 ErrTokenExpired），只支持「未过期时提前续期」的用法。
//
// 配置一致性：同 RefreshToken，整个刷新流程只 Load 一次配置，
// 校验与重新签发冻结在同一份配置快照上。
func RefreshCustomToken(tokenString string) (string, error) {
	o, err := currentOptions()
	if err != nil {
		return "", err
	}

	claims, err := parseCustomTokenWith(tokenString, o)
	if err != nil {
		return "", err
	}

	// 重新签发：自定义字段沿用原 claims，注册声明按同一份配置快照刷新（有效期重新计算）
	claims.CustomRegisteredClaims.RegisteredClaims = buildRegisteredClaims(o, time.Now())
	claims.CustomRegisteredClaims.Subject = o.subject

	token := jwt.NewWithClaims(o.signingMethod, claims)
	return token.SignedString(o.signingKey)
}
