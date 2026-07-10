package nacoscli

import (
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
)

// options 包含 Nacos 客户端的全部配置选项。
type options struct {
	ipAddr      string // 服务器地址
	port        uint64 // 端口
	scheme      string // 协议，http 或 grpc
	contextPath string // 路径
	namespaceID string // 命名空间 ID
	timeoutMs   uint64 // 请求超时时间（毫秒）
	username    string // 认证用户名
	password    string // 认证密码

	// 如果设置了 clientConfig，上述 namespaceID/timeoutMs/username/password 等字段将无效
	clientConfig *constant.ClientConfig
	// 如果设置了 serverConfigs，上述 ipAddr/port/scheme/contextPath 等字段将无效
	serverConfigs []constant.ServerConfig
}

// defaultOptions 返回默认的 options 结构体实例。
func defaultOptions() *options {
	return &options{
		timeoutMs: 5000,
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
func WithPort(port uint64) Option {
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
func WithTimeoutMs(timeoutMs uint64) Option {
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
