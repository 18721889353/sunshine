package gows

// ErrWriteQueueFull 写入队列已满，消息被丢弃的错误标识。
// 当客户端写入缓冲区满载时 WriteJSON 返回此错误，
// 发送方可根据此错误判断是否为短暂拥塞，决定是否降级处理。
var ErrWriteQueueFull = &writeQueueFullError{}

// ErrWriteLimitExceeded 单条消息大小超过写入限制的错误标识。
// 当消息超过 WithWriteLimit 设置的字节数时 WriteJSON/WriteRaw 返回此错误。
var ErrWriteLimitExceeded = &writeLimitExceededError{}

// writeQueueFullError 写入队列满载的错误类型
type writeQueueFullError struct{}

// Error 实现 error 接口，返回写入队列已满的错误描述信息。
func (e *writeQueueFullError) Error() string {
	return "write queue is full, message dropped"
}

// writeLimitExceededError 写入大小超限的错误类型
type writeLimitExceededError struct{}

// Error 实现 error 接口，返回写入大小超限的错误描述信息。
func (e *writeLimitExceededError) Error() string {
	return "write message exceeds size limit"
}
