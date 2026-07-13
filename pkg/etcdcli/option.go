package etcdcli

import (
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Option 用于设置 etcd 客户端的选项。
type Option func(*options)

// options 包含了 etcd 客户端的各种配置选项。
type options struct {
	dialTimeout time.Duration // 连接超时时间，单位为秒

	username string // 认证用户名
	password string // 认证密码

	isSecure           bool   // 是否启用安全模式
	serverNameOverride string // etcd 域名
	certFile           string // 证书文件路径

	autoSyncInterval    time.Duration // 成员列表自动同步的时间间隔
	dialKeepAliveTime    time.Duration // 保持连接的时间间隔
	dialKeepAliveTimeout time.Duration // 保持连接的超时时间

	// 如果设置了此参数，上述所有字段均无效
	config *clientv3.Config
}

// defaultOptions 返回默认的 options 配置。
func defaultOptions() *options {
	return &options{
		dialTimeout:          5 * time.Second, // 默认连接超时时间为 5 秒
		dialKeepAliveTime:    20 * time.Second, // 默认保持连接的时间间隔为 20 秒
		dialKeepAliveTimeout: 10 * time.Second, // 默认保持连接的超时时间为 10 秒
	}
}

// apply 应用传递的选项到 options 结构体。
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithDialTimeout 设置连接超时时间。
func WithDialTimeout(duration time.Duration) Option {
	return func(o *options) {
		o.dialTimeout = duration
	}
}

// WithAuth 设置认证信息。
func WithAuth(username string, password string) Option {
	return func(o *options) {
		o.username = username
		o.password = password
	}
}

// WithSecure 设置 TLS 安全连接。
func WithSecure(serverNameOverride string, certFile string) Option {
	return func(o *options) {
		o.isSecure = true
		o.serverNameOverride = serverNameOverride
		o.certFile = certFile
	}
}

// WithAutoSyncInterval 设置成员列表自动同步的时间间隔。
func WithAutoSyncInterval(duration time.Duration) Option {
	return func(o *options) {
		o.autoSyncInterval = duration
	}
}

// WithDialKeepAliveTime 设置保持连接的时间间隔。
func WithDialKeepAliveTime(duration time.Duration) Option {
	return func(o *options) {
		o.dialKeepAliveTime = duration
	}
}

// WithDialKeepAliveTimeout 设置保持连接的超时时间。
func WithDialKeepAliveTimeout(duration time.Duration) Option {
	return func(o *options) {
		o.dialKeepAliveTimeout = duration
	}
}

// WithConfig 设置 etcd 客户端的配置。
func WithConfig(c *clientv3.Config) Option {
	return func(o *options) {
		o.config = c
	}
}
