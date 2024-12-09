package consulcli

import (
	"time"

	"github.com/hashicorp/consul/api"
)

// Option 设置 Consul 客户端选项的函数类型。
type Option func(*options)

// options 包含 Consul 客户端的各种配置选项。
type options struct {
	scheme     string        // 协议方案（如 "http" 或 "https"）
	waitTime   time.Duration // 阻塞查询的最大等待时间
	datacenter string        // 数据中心名称
	token      string        // 访问令牌

	// 如果设置了此参数，则上述所有字段将无效
	config *api.Config
}

// defaultOptions 返回默认的选项配置。
func defaultOptions() *options {
	return &options{
		scheme:   "http",          // 默认协议方案为 "http"
		waitTime: time.Second * 5, // 默认阻塞查询的最大等待时间为 5 秒
	}
}

// apply 应用传入的选项到当前选项配置。
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithWaitTime 设置阻塞查询的最大等待时间。
func WithWaitTime(waitTime time.Duration) Option {
	return func(o *options) {
		o.waitTime = waitTime
	}
}

// WithScheme 设置协议方案（如 "http" 或 "https"）。
func WithScheme(scheme string) Option {
	return func(o *options) {
		o.scheme = scheme
	}
}

// WithDatacenter 设置数据中心名称。
func WithDatacenter(datacenter string) Option {
	return func(o *options) {
		o.datacenter = datacenter
	}
}

// WithToken 设置访问令牌。
func WithToken(token string) Option {
	return func(o *options) {
		o.token = token
	}
}

// WithConfig 设置完整的 Consul 配置。
func WithConfig(c *api.Config) Option {
	return func(o *options) {
		o.config = c
	}
}
