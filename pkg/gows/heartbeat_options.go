package gows

import "time"

func defaultHeartbeatOptions() *heartbeatOptions {
	return &heartbeatOptions{
		interval:      defaultHeartbeatInterval,
		pongTimeout:   defaultPongTimeout,
		pingWriteWait: defaultPingWriteWait,
	}
}

func (o *heartbeatOptions) apply(opts ...HeartbeatOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// HeartbeatOption 心跳配置选项函数类型。
// 采用 Functional Options 模式，支持可扩展的配置传递。
type HeartbeatOption func(*heartbeatOptions)

// heartbeatOptions 心跳内部配置参数集合
type heartbeatOptions struct {
	interval      time.Duration // 心跳发送间隔
	pongTimeout   time.Duration // Pong 等待超时（默认 10s）
	pingWriteWait time.Duration // Ping 控制帧写入超时（默认 5s）
}

// WithHeartbeatInterval 设置心跳发送间隔。
// 参数:
//   - interval: 间隔时间，建议 10~60 秒。<=0 时使用默认值 30 秒。
func WithHeartbeatInterval(interval time.Duration) HeartbeatOption {
	return func(o *heartbeatOptions) {
		if interval > 0 {
			o.interval = interval
		}
	}
}

// WithPongTimeout 设置 Pong 超时时间。
// 参数:
//   - timeout: 超过此时间未收到 Pong 响应帧，触发连接关闭。
//     建议为心跳间隔的 1/3 ~ 1/2，默认 10 秒。
func WithPongTimeout(timeout time.Duration) HeartbeatOption {
	return func(o *heartbeatOptions) {
		if timeout > 0 {
			o.pongTimeout = timeout
		}
	}
}

// WithPingWriteWait 设置 Ping 控制帧的写入超时时间。
// 参数:
//   - timeout: 超过此时间 Ping 帧写入失败，认为连接异常。默认 5 秒。
func WithPingWriteWait(timeout time.Duration) HeartbeatOption {
	return func(o *heartbeatOptions) {
		if timeout > 0 {
			o.pingWriteWait = timeout
		}
	}
}
