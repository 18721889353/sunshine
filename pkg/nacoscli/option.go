package nacoscli

import (
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
)

// options 包含 Nacos 客户端的配置选项。
type options struct {
	username string
	password string

	// 如果设置了 clientConfig，上述字段（username, password）将无效
	clientConfig  *constant.ClientConfig
	serverConfigs []constant.ServerConfig
}

// defaultOptions 返回默认的 options 结构体实例。
func defaultOptions() *options {
	return &options{
		clientConfig:  nil,
		serverConfigs: nil,
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

// WithAuth 设置 Nacos 客户端的身份验证信息。
func WithAuth(username string, password string) Option {
	return func(o *options) {
		o.username = username
		o.password = password
	}
}

// WithClientConfig 设置 Nacos 客户端的配置。
func WithClientConfig(clientConfig *constant.ClientConfig) Option {
	return func(o *options) {
		o.clientConfig = clientConfig
	}
}

// WithServerConfigs 设置 Nacos 服务器的配置。
func WithServerConfigs(serverConfigs []constant.ServerConfig) Option {
	return func(o *options) {
		o.serverConfigs = serverConfigs
	}
}
