package gows

import (
	"context"
	"encoding/json"

	"github.com/gorilla/websocket"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// startSpan 创建带标准属性的 tracing span。
// 自动设置 uid/remoteAddr/requestID，消除 WriteJSONCtx/WriteRawCtx/ReadMessageCtx 中的重复样板代码。
func (c *Client) startSpan(ctx context.Context, spanName string, extraAttrs ...attribute.KeyValue) trace.Span {
	if ctx == nil {
		ctx = c.clientCtx
	}
	_, span := otel.Tracer("gows").Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	span.SetAttributes(
		attribute.String("ws.uid", c.uid),
		attribute.String("ws.remote_addr", c.remoteAddr),
		requestIDAttr(ctx),
	)
	span.SetAttributes(extraAttrs...)
	return span
}

// WriteJSONCtx 异步非阻塞地向客户端发送 JSON 消息。
func (c *Client) WriteJSONCtx(ctx context.Context, v any) error {
	span := c.startSpan(ctx, "ws.write")
	defer span.End()

	if c.clientIsClosed.Load() {
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

	if ctx == nil {
		ctx = c.clientCtx
	}
	select {
	case c.writeCh <- data:
		span.SetAttributes(attribute.Int("ws.write_queue_len", len(c.writeCh)))
		span.SetStatus(codes.Ok, "message queued")
		return nil
	case <-c.clientCtx.Done():
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return websocket.ErrCloseSent
	case <-ctx.Done():
		span.SetAttributes(attribute.Bool("ws.ctx_cancelled", true))
		span.SetStatus(codes.Error, ctx.Err().Error())
		return ctx.Err()
	default:
		span.SetAttributes(attribute.Bool("ws.write_queue_full", true))
		span.SetStatus(codes.Error, "write queue full, message dropped")
		return ErrWriteQueueFull
	}
}

// WriteRawCtx 直接写入预序列化的原始字节数据，跳过 JSON 序列化步骤。
func (c *Client) WriteRawCtx(ctx context.Context, data []byte) error {
	span := c.startSpan(ctx, "ws.write_raw", attribute.Int("ws.data_size", len(data)))
	defer span.End()

	if c.clientIsClosed.Load() {
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return websocket.ErrCloseSent
	}

	if err := c.checkWriteLimit(data); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	if ctx == nil {
		ctx = c.clientCtx
	}
	select {
	case c.writeCh <- data:
		span.SetAttributes(attribute.Int("ws.write_queue_len", len(c.writeCh)))
		span.SetStatus(codes.Ok, "raw message queued")
		return nil
	case <-c.clientCtx.Done():
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return websocket.ErrCloseSent
	case <-ctx.Done():
		span.SetAttributes(attribute.Bool("ws.ctx_cancelled", true))
		span.SetStatus(codes.Error, ctx.Err().Error())
		return ctx.Err()
	default:
		span.SetAttributes(attribute.Bool("ws.write_queue_full", true))
		span.SetStatus(codes.Error, "write queue full, message dropped")
		return ErrWriteQueueFull
	}
}

// ReadMessageCtx 同步阻塞读取客户端发送的一条消息。
func (c *Client) ReadMessageCtx(ctx context.Context) ([]byte, error) {
	span := c.startSpan(ctx, "ws.read")
	defer span.End()

	if c.clientIsClosed.Load() {
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return nil, websocket.ErrCloseSent
	}

	if ctx == nil {
		ctx = c.clientCtx
	}
	select {
	case data, ok := <-c.readCh:
		if !ok {
			span.SetStatus(codes.Error, "connection closed")
			return nil, c.getLastReadErr()
		}
		span.SetAttributes(attribute.Int("ws.msg_size", len(data)))
		span.SetStatus(codes.Ok, "message received")
		return data, nil
	case <-c.clientCtx.Done():
		span.SetStatus(codes.Error, "connection closed")
		return nil, websocket.ErrCloseSent
	case <-ctx.Done():
		span.SetAttributes(attribute.Bool("ws.ctx_cancelled", true))
		span.SetStatus(codes.Error, ctx.Err().Error())
		return nil, ctx.Err()
	}
}
