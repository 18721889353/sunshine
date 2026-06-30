package gows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/gorilla/websocket"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// writeLoopRetries 写入失败最大重试次数
const (
	writeLoopRetries = 3                // 写入失败最大重试次数
	writeDeadline    = 10 * time.Second // 默认单次写入超时时间
)

// ErrWriteQueueFull 写入队列已满，消息被丢弃的错误标识。
// 当客户端写入缓冲区满载时 WriteJSON 返回此错误，
// 发送方可根据此错误判断是否为短暂拥塞，决定是否降级处理。
var ErrWriteQueueFull = errors.New("write queue is full, message dropped")

// ErrWriteLimitExceeded 单条消息大小超过写入限制的错误标识。
// 当消息超过 WithWriteLimit 设置的字节数时 WriteJSON/WriteRaw 返回此错误。
var ErrWriteLimitExceeded = errors.New("write message exceeds size limit")

// readLoop 内部读取循环 goroutine。
// 持续从底层 WebSocket 连接读取消息并推入 readCh 供 ReadMessageCtx 消费。
// 通过 readCh 缓冲通道解耦网络读取和业务处理，满队列时丢弃消息实现背压保护。
// 支持主动读超时保护（独立于心跳），超时或读取失败时记录错误并强制关闭底层连接。
//
// 核心机制:
//   - readTimeout > 0 时每次读前设置 SetReadDeadline，超时后 ReadMessage 返回 timeout 错误
//   - 读取成功后阻塞推入 readCh，队列满时反压到 TCP 读取层，不丢弃消息
//   - 读取失败后调用 forceCloseConn 关闭底层 TCP 连接（不自锁），不调用 Close
//   - 退出时 defer 关闭 readCh，触发 ReadMessageCtx 的 <-c.readCh 返回 !ok
//
// 退出路径:
//   - ReadMessage 返回错误（网络断开/超时/连接关闭）→ forceCloseConn → return
//   - SetReadDeadline 失败（连接已不可用）→ forceCloseConn → return
//   - closeSignalCh 被关闭（Close 调用）→ return（阻塞推入时响应）
//
// 注意:
//   - 读取失败不重试，直接关闭连接由客户端发起重连
//   - 使用 forceCloseConn 而非 Close 避免 writeLoop 自锁
func (c *Client) readLoop() {
	defer func() {
		close(c.readCh)
		c.loopWg.Done()
	}()

	for {
		// 主动读超时保护（独立于心跳，不依赖 PongHandler）
		if c.readTimeout > 0 {
			if err := c.wsConn.SetReadDeadline(time.Now().Add(c.readTimeout)); err != nil {
				c.recordReadErr(err)
				c.forceCloseConn()
				return
			}
		}

		_, data, err := c.wsConn.ReadMessage()
		if err != nil {
			c.recordReadErr(err)
			c.forceCloseConn()
			return
		}

		// 阻塞推入 readCh，队列满时反压到 TCP 读取层，不丢弃消息
		// 关闭时通过 closeSignalCh 退出，不永久阻塞
		select {
		case c.readCh <- data:
			c.numReceived.Add(1)
			c.markLastRead()
		case <-c.closeSignalCh:
			return
		}
	}
}

// writeLoop 内部写入循环 goroutine。
// 从 writeCh 中逐条消费数据并通过底层连接写入网络。
// 网络写入失败时进行最多 writeLoopRetries 次指数退避重试：
//   - 第 1 次重试等待 100ms
//   - 第 2 次重试等待 200ms
//   - 第 3 次重试等待 400ms
//
// 全部重试失败后调用 forceCloseConn 关闭底层 TCP 连接，
// 触发消息读取方 ReadMessage 返回错误，进而由调用方执行 Close 完整清理。
// 使用 forceCloseConn 而非直接调用 Close 避免 writeLoop 自锁。
func (c *Client) writeLoop() {
	defer c.loopWg.Done()

	for {
		data, ok := <-c.writeCh
		if !ok {
			return
		}
		if err := c.writeWithRetry(data); err != nil {
			logger.WarnWithCtx(c.clientCtx, "ws client write failed after retries, force closing",
				logger.String("uid", c.uid),
				logger.String("remote_addr", c.remoteAddr),
				logger.Int("retries", writeLoopRetries),
				logger.Err(err),
			)
			c.forceCloseConn()
			return
		}
		c.numSent.Add(1)
		c.markLastWrite()
	}
}

// writeWithRetry 带指数退避 + 随机 jitter 的写入操作。
// 每次写入前设置 10 秒 WriteDeadline，防止网络卡死。
// 退避公式: baseDelay × 2^(attempt-1) + jitter(0~baseDelay)，
// jitter 随机化防止惊群效应。
// 参数:
//   - data: 待写入的序列化字节数据
//
// 返回:
//   - error: 所有重试均失败时返回最后一次错误
func (c *Client) writeWithRetry(data []byte) error {
	var lastErr error
	for attempt := 0; attempt <= writeLoopRetries; attempt++ {
		if attempt > 0 {
			// 指数退避：100ms, 200ms, 400ms
			baseDelay := time.Duration(100*math.Pow(2, float64(attempt-1))) * time.Millisecond
			// 随机 jitter: [0, baseDelay) 范围，防止惊群效应
			jitter := time.Duration(rand.Int64N(int64(baseDelay)))
			time.Sleep(baseDelay + jitter)
		}

		// 设置写入超时，防止 TCP 半连接导致永久阻塞
		// 若 c.writeTimeout 为 0，使用默认的 writeDeadline (10s)
		// 若设置 deadline 失败，说明连接已不可用，直接进入重试
		timeout := c.writeTimeout
		if timeout <= 0 {
			timeout = writeDeadline
		}
		if err := c.wsConn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			lastErr = err
			c.recordWriteErr(err)
			continue
		}

		if err := c.wsConn.WriteMessage(websocket.TextMessage, data); err != nil {
			lastErr = err
			c.recordWriteErr(err)
			continue
		}
		return nil
	}
	return fmt.Errorf("write failed after %d retries: %w", writeLoopRetries, lastErr)
}

// checkWriteLimit 检查写入数据大小是否超过 writeLimit 限制。
func (c *Client) checkWriteLimit(data []byte) error {
	if c.writeLimit > 0 && len(data) > int(c.writeLimit) {
		return ErrWriteLimitExceeded
	}
	return nil
}

// WriteJSONCtx 异步非阻塞地向客户端发送 JSON 消息。
// 参数:
//   - ctx: 上下文，用于链路追踪。传入 nil 或非 tracing context 不影响功能。
//   - v:   待发送的数据，会被序列化为 JSON 格式
//
// 返回:
//   - error: 参见 WriteJSON 的错误语义
func (c *Client) WriteJSONCtx(ctx context.Context, v any) error {
	if ctx == nil {
		ctx = c.clientCtx
	}
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.write", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.uid", c.uid),
		attribute.String("ws.remote_addr", c.remoteAddr),
		requestIDAttr(ctx),
	)

	if c.closed.Load() {
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return websocket.ErrCloseSent
	}
	data, err := json.Marshal(v)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	if err := c.checkWriteLimit(data); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	select {
	case c.writeCh <- data:
		span.SetAttributes(attribute.Int("ws.write_queue_len", len(c.writeCh)))
		span.SetStatus(codes.Ok, "message queued")
		return nil
	default:
		span.SetAttributes(attribute.Bool("ws.write_queue_full", true))
		span.SetStatus(codes.Error, "write queue full, message dropped")
		return ErrWriteQueueFull
	}
}

// WriteRawCtx 直接写入预序列化的原始字节数据，跳过 JSON 序列化步骤。
// 参数:
//   - ctx:  上下文，用于链路追踪
//   - data: 已序列化的 JSON 字节数据
//
// 返回:
//   - error: 与 WriteJSONCtx 相同的错误语义
func (c *Client) WriteRawCtx(ctx context.Context, data []byte) error {
	if ctx == nil {
		ctx = c.clientCtx
	}
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.write_raw", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.uid", c.uid),
		attribute.String("ws.remote_addr", c.remoteAddr),
		requestIDAttr(ctx),
		attribute.Int("ws.data_size", len(data)),
	)

	if c.closed.Load() {
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return websocket.ErrCloseSent
	}

	if err := c.checkWriteLimit(data); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	select {
	case c.writeCh <- data:
		span.SetAttributes(attribute.Int("ws.write_queue_len", len(c.writeCh)))
		span.SetStatus(codes.Ok, "raw message queued")
		return nil
	default:
		span.SetAttributes(attribute.Bool("ws.write_queue_full", true))
		span.SetStatus(codes.Error, "write queue full, message dropped")
		return ErrWriteQueueFull
	}
}

// ReadMessageCtx 同步阻塞读取客户端发送的一条消息。
// 参数:
//   - ctx: 上下文，用于链路追踪
//
// 返回:
//   - []byte: 消息原始字节内容
//   - error:  读取失败、连接关闭或读取超时时返回非 nil 错误
func (c *Client) ReadMessageCtx(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = c.clientCtx
	}
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.read", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.uid", c.uid),
		attribute.String("ws.remote_addr", c.remoteAddr),
		requestIDAttr(ctx),
	)

	select {
	case data, ok := <-c.readCh:
		if !ok {
			span.SetStatus(codes.Error, "connection closed")
			return nil, c.getLastReadErr()
		}
		span.SetAttributes(attribute.Int("ws.msg_size", len(data)))
		span.SetStatus(codes.Ok, "message received")
		return data, nil
	case <-c.closeSignalCh:
		span.SetStatus(codes.Error, "connection closed")
		return nil, websocket.ErrCloseSent
	}
}
