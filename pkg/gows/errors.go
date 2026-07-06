package gows

import (
	"errors"
)

// ErrWriteQueueFull 写入队列已满，消息被丢弃的错误标识。
// 当客户端写入缓冲区满载时 WriteJSON 返回此错误，
// 发送方可根据此错误判断是否为短暂拥塞，决定是否降级处理。
var ErrWriteQueueFull = errors.New("write queue is full, message dropped")

// ErrWriteLimitExceeded 单条消息大小超过写入限制的错误标识。
// 当消息超过 WithWriteLimit 设置的字节数时 WriteJSON/WriteRaw 返回此错误。
var ErrWriteLimitExceeded = errors.New("write message exceeds size limit")

// ErrNilContext 传入 nil context 的错误标识。
// 当 ReadMsgFromClientReadCh 接收到 nil context 时返回此错误，由调用方自行修复。
var ErrNilContext = errors.New("ctx must not be nil")

// ErrNoDispatcher Client 未关联 Dispatcher，无法执行跨实例发送/广播。
var ErrNoDispatcher = errors.New("client has no dispatcher configured")

// ErrTokenInvalid token 无效（格式正确但缺少 uid 字段）
var ErrTokenInvalid = errors.New("token is invalid: missing uid")

// ErrMaxConnections 达到最大连接数限制的错误
var ErrMaxConnections = errors.New("dispatcher: max connections reached")

// ErrEmptyUID UID 为空时禁止注册
var ErrEmptyUID = errors.New("dispatcher: empty uid")

// ErrClientNotRegistered 客户端未注册到 Dispatcher
var ErrClientNotRegistered = errors.New("dispatcher: client not registered")

// ErrClientNotFound 未找到指定 UID 的客户端连接
var ErrClientNotFound = errors.New("dispatcher: client not found")
