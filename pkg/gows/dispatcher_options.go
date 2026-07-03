package gows

import (
	"fmt"
)

// dispatcherOptions 分发中心内部配置参数集合
type dispatcherOptions struct {
	maxClientNum int32 // 最大客户端数（0=不限制）
	workerNum    int32 // worker 协程数（默认 4）
}

func defaultDispatcherOptions() *dispatcherOptions {
	return &dispatcherOptions{
		workerNum: 4,
	}
}

func (o *dispatcherOptions) apply(opts ...DispatcherOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// DispatcherOption 分发中心配置选项函数类型
type DispatcherOption func(*dispatcherOptions)

// WithMaxConnections 设置最大连接数限制。
// 参数:
//   - n: 最大连接数。达到上限时 Register 返回 ErrMaxConnections。
//     0 表示不限制（默认）。
func WithMaxConnections(n int) DispatcherOption {
	return func(o *dispatcherOptions) {
		if n > 0 {
			o.maxClientNum = int32(n)
		}
	}
}

// WithWorkerPool 设置后台 Worker 协程数，用于消息投递的背压保护。
// 参数:
//   - n: Worker 协程数（默认 4，建议 2~16）。Worker 池满时消息降级为串行处理。
//     0 或 1 表示关闭 WorkerPool，receiveLoop 串行投递。
func WithWorkerPool(n int) DispatcherOption {
	return func(o *dispatcherOptions) {
		if n > 1 {
			o.workerNum = int32(n)
		}
	}
}

// ErrMaxConnections 达到最大连接数限制的错误
var ErrMaxConnections = fmt.Errorf("dispatcher: max connections reached")

// ErrEmptyUID UID 为空时禁止注册
var ErrEmptyUID = fmt.Errorf("dispatcher: empty uid")

// ErrClientNotRegistered 客户端未注册到 Dispatcher
var ErrClientNotRegistered = fmt.Errorf("dispatcher: client not registered")
