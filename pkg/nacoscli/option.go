package nacoscli

import (
	"strings"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
)

// options 包含 Nacos 客户端的全部配置选项。
type options struct {
	ipAddr      string        // 服务器地址
	port        int           // 端口
	scheme      string        // 协议，http、https 或 grpc（空表示未设置，由 SDK 默认 http）
	contextPath string        // 路径
	namespaceID string        // 命名空间 ID
	timeoutMs   int           // 请求超时时间（毫秒）
	getTimeout  time.Duration // GetConfig 拉取超时，默认 30 秒
	username    string        // 认证用户名
	password    string        // 认证密码

	// 如果设置了 clientConfig，上述 namespaceID/timeoutMs/username/password 等字段将无效
	clientConfig *constant.ClientConfig
	// 如果设置了 serverConfigs，上述 ipAddr/port/scheme/contextPath 等字段将无效
	serverConfigs []constant.ServerConfig

	// WatchConfig 专用配置
	maxRetries  int           // 最大重试次数，0 表示无限重试
	createDelay time.Duration // 创建或注册失败后的重试等待时间，默认 5 秒
}

// defaultOptions 返回默认的 options 结构体实例。
func defaultOptions() *options {
	return &options{
		timeoutMs:   5000,
		getTimeout:  30 * time.Second,
		createDelay: 5 * time.Second,
	}
}

// Option 是一个函数类型，用于设置 Nacos 客户端的选项。
type Option func(*options)

// apply 应用传入的选项列表到 options 结构体。
// nil Option 防御：跳过以避免动态拼接选项（如 GetConfig(params, nil)）时 panic。
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
}

// WithIPAddr 设置 Nacos 服务器地址。
func WithIPAddr(ipAddr string) Option {
	return func(o *options) {
		o.ipAddr = ipAddr
	}
}

// WithPort 设置 Nacos 服务器端口，port 必须非负。
func WithPort(port int) Option {
	return func(o *options) {
		if port >= 0 {
			o.port = port
		}
	}
}

// WithScheme 设置 Nacos 协议，合法取值为 http、https、grpc（大小写与首尾空格不敏感）。
// 防御式取值（与 WithTimeoutMs 同一风格）：
//   - 空串表示「未设置」，不覆盖先前值，由 SDK 侧按默认 http 处理；
//   - 白名单外的取值（如 "h2"）被忽略，保留先前设置的值。
//
// 理由：SDK 用 Scheme 直接拼接请求地址（getAddress: scheme://ip:port），非法协议名
// 不会在创建时报错，而是到运行期请求才失败，且错误信息不指向配置项本身，定位成本高。
// 注：v2 默认传输模式为 gRPC，该拼接主要用于鉴权与 HTTP 兜底路径，grpc 取值仍按原样透传。
func WithScheme(scheme string) Option {
	return func(o *options) {
		switch normalized := strings.ToLower(strings.TrimSpace(scheme)); normalized {
		case "http", "https", "grpc":
			o.scheme = normalized
		default:
			// 空串（未设置）与白名单外取值均不写入，保留先前设置的值
		}
	}
}

// WithContextPath 设置 Nacos 上下文路径。
func WithContextPath(contextPath string) Option {
	return func(o *options) {
		o.contextPath = contextPath
	}
}

// WithNamespaceID 设置 Nacos 命名空间 ID。
func WithNamespaceID(namespaceID string) Option {
	return func(o *options) {
		o.namespaceID = namespaceID
	}
}

// WithTimeoutMs 设置 Nacos 客户端请求超时时间（毫秒），默认 5000。
// timeoutMs 必须为正数，0 或负值会被忽略，保留先前设置的值
// （0 会让 SDK 行为未定义，等价于立即超时）。
func WithTimeoutMs(timeoutMs int) Option {
	return func(o *options) {
		if timeoutMs > 0 {
			o.timeoutMs = timeoutMs
		}
	}
}

// WithAuth 设置 Nacos 客户端的身份验证信息。
func WithAuth(username, password string) Option {
	return func(o *options) {
		o.username = username
		o.password = password
	}
}

// WithClientConfig 设置 Nacos 客户端的完整配置。
// 设置后将忽略 WithNamespaceID、WithTimeoutMs、WithAuth 等单字段选项。
func WithClientConfig(clientConfig *constant.ClientConfig) Option {
	return func(o *options) {
		o.clientConfig = clientConfig
	}
}

// WithServerConfigs 设置 Nacos 服务器的完整配置列表。
// 设置后将忽略 WithIPAddr、WithPort、WithScheme、WithContextPath 等单字段选项。
func WithServerConfigs(serverConfigs []constant.ServerConfig) Option {
	return func(o *options) {
		o.serverConfigs = serverConfigs
	}
}

// WithMaxRetries 设置 WatchConfig 最大重试次数。
// 语义：创建/注册连续失败累计达到 n 次时放弃（第 n 次失败后不再重试，直接退出后台 goroutine），
// 即 n 是「总失败次数上限」而非「额外重试次数」。
// 0 表示无限重试（默认），负值会被忽略，保留先前设置的值。
// 例：WithMaxRetries(3) → 第 3 次失败时记录 Error 日志并退出（共 3 次尝试）。
func WithMaxRetries(n int) Option {
	return func(o *options) {
		if n >= 0 {
			o.maxRetries = n
		}
	}
}

// WithCreateDelay 设置 WatchConfig 创建或注册失败后的重试等待时间，默认 5 秒。
// d 必须为正数，0 或负值会被忽略，保留默认值
// （0 会让 time.After(0) 立即触发，在无限重试下形成忙循环并打满 CPU）。
func WithCreateDelay(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.createDelay = d
		}
	}
}

// WithGetTimeout 设置 GetConfig 拉取配置的超时时间，默认 30 秒。
// d 必须为正数，0 或负值会被忽略，保留默认值
// （0 会让 context.WithTimeout 立即到期，GetConfig 必然超时）。
func WithGetTimeout(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.getTimeout = d
		}
	}
}
