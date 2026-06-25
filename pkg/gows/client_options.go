package gows

import "time"

// ClientOption 客户端配置选项函数类型。
// 采用 Functional Options 模式，支持可扩展的配置传递，
// 新增配置项无需修改 NewClient 的函数签名。
type ClientOption func(*clientOptions)

// clientOptions 客户端内部配置参数集合
type clientOptions struct {
	readQueueSize  int           // 读取队列缓冲区容量（默认 64）
	writeQueueSize int           // 写入队列缓冲区容量（默认 64）
	readTimeout    time.Duration // readLoop 读取超时时间
	writeTimeout   time.Duration // writeWithRetry 写入超时时间
	readLimit      int64         // 单条消息读取大小限制
	writeLimit     int64         // 单条消息写入大小限制
}

func defaultClientOptions() *clientOptions {
	return &clientOptions{
		writeQueueSize: 64,
		readQueueSize:  64,
	}
}

func (o *clientOptions) apply(opts ...ClientOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// 队列容量通过 Upgrade 的 WithQueueSize 选项传播到 Client

// withWriteQueueSize 内部 ClientOption，供 Upgrade 将队列大小传入 NewClient
func withWriteQueueSize(n int) ClientOption {
	return func(o *clientOptions) {
		if n > 0 {
			o.writeQueueSize = n
		}
	}
}

// withReadQueueSize 内部 ClientOption，供 Upgrade 将队列大小传入 NewClient
func withReadQueueSize(n int) ClientOption {
	return func(o *clientOptions) {
		if n > 0 {
			o.readQueueSize = n
		}
	}
}

// withReadTimeout 内部 ClientOption，供 Upgrade 将读取超时传入 NewClient
func withClientReadTimeout(d time.Duration) ClientOption {
	return func(o *clientOptions) {
		if d > 0 {
			o.readTimeout = d
		}
	}
}

// withWriteTimeout 内部 ClientOption，供 Upgrade 将写入超时传入 NewClient
func withClientWriteTimeout(d time.Duration) ClientOption {
	return func(o *clientOptions) {
		if d > 0 {
			o.writeTimeout = d
		}
	}
}

// withReadLimit 内部 ClientOption，供 Upgrade 将读取大小限制传入 NewClient
func withClientReadLimit(limit int64) ClientOption {
	return func(o *clientOptions) {
		if limit > 0 {
			o.readLimit = limit
		}
	}
}

// withWriteLimit 内部 ClientOption，供 Upgrade 将写入大小限制传入 NewClient
func withClientWriteLimit(limit int64) ClientOption {
	return func(o *clientOptions) {
		if limit > 0 {
			o.writeLimit = limit
		}
	}
}
