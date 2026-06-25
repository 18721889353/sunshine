package gows

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"sync/atomic"
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

// readLoop 内部读取循环 goroutine。
// 持续从底层连接读取 WebSocket 消息并推入 readCh 供 ReadMessage 消费。
// 通过 select 监听 closeCh 实现优雅退出。
// 当 readCh 满载时丢弃消息，防止背压阻塞影响关闭流程。
// 读取失败时记录错误并退出循环，由 Close 负责完整清理。
func (c *Client) readLoop() {
	defer func() {
		close(c.readCh)
		c.wg.Done()
	}()

	for {
		select {
		case <-c.closeCh:
			return
		default:
		}

		// 主动读超时保护（独立于心跳，不依赖 PongHandler）
		if c.readTimeout > 0 {
			if err := c.conn.SetReadDeadline(time.Now().Add(c.readTimeout)); err != nil {
				c.recordReadErr(err)
				return
			}
		}

		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.recordReadErr(err)
			return
		}

		// 非阻塞推入 readCh，队列满时丢弃防止阻塞
		select {
		case c.readCh <- data:
		default:
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
	defer c.wg.Done()
	for {
		select {
		case data, ok := <-c.writeCh:
			if !ok {
				return
			}
			if err := c.writeWithRetry(data); err != nil {
				logger.WarnWithCtx(c.ctx, "ws client write failed after retries, force closing",
					logger.String("uid", c.uid),
					logger.String("remote_addr", c.remoteAddr),
					logger.Int("retries", writeLoopRetries),
					logger.Err(err),
				)
				c.forceCloseConn()
				return
			}
			atomic.AddInt64(&c.numSent, 1)
			c.markLastWrite()
		case <-c.closeCh:
			return
		}
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
		if err := c.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			lastErr = err
			c.recordWriteErr(err)
			continue
		}

		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
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

// WriteJSONCtx 异步非阻塞地向客户端发送 JSON 消息（带自定义上下文）。
// 参数:
//   - ctx: 上下文，用于链路追踪。传入 nil 或非 tracing context 不影响功能。
//   - v:   待发送的数据，会被序列化为 JSON 格式
//
// 返回:
//   - error: 参见 WriteJSON 的错误语义
func (c *Client) WriteJSONCtx(ctx context.Context, v any) error {
	if ctx == nil {
		ctx = c.ctx
	}
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.write", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.uid", c.uid),
		attribute.String("ws.remote_addr", c.remoteAddr),
		requestIDAttr(ctx),
	)

	if atomic.LoadInt32(&c.closed) == 1 {
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

// WriteJSON 异步非阻塞地向客户端发送 JSON 消息，使用客户端默认上下文。
// 如需自定义 tracing 上下文，请使用 WriteJSONCtx。
func (c *Client) WriteJSON(v any) error {
	return c.WriteJSONCtx(c.ctx, v)
}

// WriteRawCtx 直接写入预序列化的原始字节数据，跳过 JSON 序列化步骤（带自定义上下文）。
// 参数:
//   - ctx:  上下文，用于链路追踪
//   - data: 已序列化的 JSON 字节数据
//
// 返回:
//   - error: 与 WriteJSONCtx 相同的错误语义
func (c *Client) WriteRawCtx(ctx context.Context, data []byte) error {
	if ctx == nil {
		ctx = c.ctx
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

	if atomic.LoadInt32(&c.closed) == 1 {
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

// WriteRaw 直接写入预序列化的原始字节数据，使用客户端默认上下文。
// 如需自定义 tracing 上下文，请使用 WriteRawCtx。
func (c *Client) WriteRaw(data []byte) error {
	return c.WriteRawCtx(c.ctx, data)
}

// ReadMessageCtx 同步阻塞读取客户端发送的一条消息（带自定义上下文）。
// 参数:
//   - ctx: 上下文，用于链路追踪
//
// 返回:
//   - []byte: 消息原始字节内容
//   - error:  读取失败、连接关闭或读取超时时返回非 nil 错误
func (c *Client) ReadMessageCtx(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = c.ctx
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
		atomic.AddInt64(&c.numReceived, 1)
		c.markLastRead()
		span.SetAttributes(attribute.Int("ws.msg_size", len(data)))
		span.SetStatus(codes.Ok, "message received")
		return data, nil
	case <-c.closeCh:
		span.SetStatus(codes.Error, "connection closed")
		return nil, websocket.ErrCloseSent
	}
}

// ReadMessage 同步阻塞读取客户端发送的一条消息，使用客户端默认上下文。
// 如需自定义 tracing 上下文，请使用 ReadMessageCtx。
func (c *Client) ReadMessage() ([]byte, error) {
	return c.ReadMessageCtx(c.ctx)
}
