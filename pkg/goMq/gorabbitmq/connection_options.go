package gorabbitmq

import (
	"crypto/tls"
	"time"
)

// ConnectionOption 连接配置选项函数类型
type ConnectionOption func(*connectionOptions)

// connectionOptions 连接配置选项
type connectionOptions struct {
	tlsConfig       *tls.Config   // TLS 配置，如果使用 amqps 协议则必须设置
	reconnectTime   time.Duration // 重连时间间隔，默认为 3 秒
	dialTimeout     time.Duration // 连接超时时间，默认为 5 秒
	heartbeat       time.Duration // 心跳间隔
	deadlineTimeout time.Duration // TCP 连接成功后 AMQP 握手协商超时，默认 30 秒
	maxRetries      int           // 最大重连次数，0 表示无限重试
}

// apply 应用连接配置选项
func (o *connectionOptions) apply(opts ...ConnectionOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultConnectionOptions 返回默认连接配置。
//   - reconnectTime:   3s，断开后重连前的等待间隔
//   - dialTimeout:     5s，TCP 拨号超时
//   - heartbeat:       5s，AMQP 心跳间隔，用于保活和检测对端存活
//   - deadlineTimeout: 5s，TCP 连接建立后 AMQP 握手协商的最大等待时长
//   - maxRetries:      0，0 表示无限重试
func defaultConnectionOptions() *connectionOptions {
	return &connectionOptions{
		tlsConfig:       nil,
		reconnectTime:   time.Second * 3,
		dialTimeout:     time.Second * 5,
		heartbeat:       time.Second * 5,
		deadlineTimeout: time.Second * 5,
		maxRetries:      0,
	}
}

// WithTLSConfig 设置 TLS 配置选项
func WithTLSConfig(tlsConfig *tls.Config) ConnectionOption {
	return func(o *connectionOptions) {
		if tlsConfig == nil {
			tlsConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		}
		o.tlsConfig = tlsConfig
	}
}

// WithReconnectTime 设置重连时间间隔选项
func WithReconnectTime(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 3
		}
		o.reconnectTime = d
	}
}

// WithDialTimeout 设置连接超时时间选项
func WithDialTimeout(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 5
		}
		o.dialTimeout = d
	}
}

// WithHeartbeat 设置心跳间隔选项
func WithHeartbeat(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 5
		}
		o.heartbeat = d
	}
}

// WithDeadlineTimeout 设置 AMQP 握手超时选项
func WithDeadlineTimeout(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 5
		}
		o.deadlineTimeout = d
	}
}

// WithMaxRetries 设置最大重连次数，0表示无限重试，默认为0
func WithMaxRetries(maxRetries int) ConnectionOption {
	return func(o *connectionOptions) {
		o.maxRetries = maxRetries
	}
}
