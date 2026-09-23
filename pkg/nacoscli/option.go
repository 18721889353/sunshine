package nacoscli

import (
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
)

// options 包含 Nacos 客户端的全部配置选项。
type options struct {
	ipAddr      string // 服务器地址
	port        int    // 端口
	scheme      string // 协议，http 或 grpc
	contextPath string // 路径
	namespaceID string // 命名空间 ID
	timeoutMs   int    // 请求超时时间（毫秒）
	username    string // 认证用户名
	password    string // 认证密码

	// 如果设置了 clientConfig，上述 namespaceID/timeoutMs/username/password 等字段将无效
	clientConfig *constant.ClientConfig
	// 如果设置了 serverConfigs，上述 ipAddr/port/scheme/contextPath 等字段将无效
	serverConfigs []constant.ServerConfig

	// WatchConfig 专用配置
	maxRetries     int           // 最大重试次数，0 表示无限重试
	createDelay    time.Duration // 创建失败后等待时间，默认 5 秒
	reconnectDelay time.Duration // 连接断开后等待时间，默认 3 秒
}

// defaultOptions 返回默认的 options 结构体实例。
func defaultOptions() *options {
	return &options{
		timeoutMs:      5000,
		createDelay:    5 * time.Second,
		reconnectDelay: 3 * time.Second,
	}
}

// Option 是一个函数类型，用于设置 Nacos 客户端的选项。
type Option func(*options)

// apply 应用传入的选项列表到 options 结构体。
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithIPAddr 设置 Nacos 服务器地址。
func WithIPAddr(ipAddr string) Option {
	return func(o *options) {
		o.ipAddr = ipAddr
	}
}

// WithPort 设置 Nacos 服务器端口。
func WithPort(port int) Option {
	return func(o *options) {
		o.port = port
	}
}

// WithScheme 设置 Nacos 协议，支持 http 或 grpc。
func WithScheme(scheme string) Option {
	return func(o *options) {
		o.scheme = scheme
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
func WithTimeoutMs(timeoutMs int) Option {
	return func(o *options) {
		o.timeoutMs = timeoutMs
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

// WithMaxRetries 设置 WatchConfig 最大重试次数，0 表示无限重试（默认）。
func WithMaxRetries(n int) Option {
	return func(o *options) {
		o.maxRetries = n
	}
}

// WithCreateDelay 设置 WatchConfig 创建监听器失败后的重试等待时间，默认 5 秒。
func WithCreateDelay(d time.Duration) Option {
	return func(o *options) {
		o.createDelay = d
	}
}

// WithReconnectDelay 设置 WatchConfig 连接断开后的重连等待时间，默认 3 秒。
func WithReconnectDelay(d time.Duration) Option {
	return func(o *options) {
		o.reconnectDelay = d
	}
}
