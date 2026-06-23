package gows

import (
	"time"
)

const (
	// defaultHeartbeatInterval 默认心跳间隔 30 秒
	defaultHeartbeatInterval = 30 * time.Second
	// defaultWriteTimeout 默认写入超时（由 WriteJSON 的非阻塞特性决定，此处仅做预留）
	defaultWriteTimeout = 0 // 0 表示不额外超时控制，依赖 WriteJSON 非阻塞写入
)

// HeartbeatOption 心跳配置选项函数类型。
// 采用 Functional Options 模式，支持可扩展的配置传递。
type HeartbeatOption func(*heartbeatOptions)

// heartbeatOptions 心跳内部配置参数集合
type heartbeatOptions struct {
	interval    time.Duration  // 心跳发送间隔
	pingMessage func() any     // 自定义 ping 消息生成器
	writeTimeout time.Duration // 写入超时时间（0 表示不限制）
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

// WithPingMessage 设置自定义 ping 消息生成器。
// 参数:
//   - fn: 生成待发送消息的函数，每次心跳触发时调用。
//     返回 any 类型，需能被 WriteJSON 序列化。
//
// 默认生成 {"type":"ping"}。可用于传递服务端时间戳等额外信息，
// 如: func() any { return Message{Type: "ping", Data: time.Now().Unix()} }
func WithPingMessage(fn func() any) HeartbeatOption {
	return func(o *heartbeatOptions) {
		o.pingMessage = fn
	}
}

// WithHeartbeatWriteTimeout 设置心跳消息的写入超时时间。
// 参数:
//   - timeout: 超时时间。超过此时间仍未写入队列则放弃本次心跳。
//     0 表示不限制（使用非阻塞写入，队列满直接丢弃）。
func WithHeartbeatWriteTimeout(timeout time.Duration) HeartbeatOption {
	return func(o *heartbeatOptions) {
		if timeout > 0 {
			o.writeTimeout = timeout
		}
	}
}

// StartHeartbeat 启动心跳保活循环，定期向客户端发送 ping 消息检测连接存活。
// 客户端无响应或写入队列满时自动退出，由连接关闭信号同步退出。
// 通常以 go StartHeartbeat(client) 或 go StartHeartbeat(client, opts...) 形式启动。
//
// 参数:
//   - client: 需要保活的客户端连接
//   - opts: 可选心跳配置（间隔、自定义 ping、写入超时）
func StartHeartbeat(client *Client, opts ...HeartbeatOption) {
	o := heartbeatOptions{
		interval:    defaultHeartbeatInterval,
		pingMessage: func() any { return Message{Type: "ping"} },
		writeTimeout: defaultWriteTimeout,
	}
	for _, opt := range opts {
		opt(&o)
	}

	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			msg := o.pingMessage()
			var err error
			if o.writeTimeout > 0 {
				// 带超时控制的写入：防止心跳协程永久阻塞
				done := make(chan error, 1)
				go func() {
					done <- client.WriteJSON(msg)
				}()
				select {
				case err = <-done:
				case <-time.After(o.writeTimeout):
					err = ErrWriteQueueFull
				}
			} else {
				// 非阻塞写入：队列满立即返回 ErrWriteQueueFull
				err = client.WriteJSON(msg)
			}

			if err != nil {
				return
			}
		case <-client.Done():
			return
		}
	}
}
