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
// 自动设置 uid/remoteAddr/requestID，消除 WriteJSONToClientWriteCh/WriteRawToClientWriteCh/ReadMsgFromClientReadCh 中的重复样板代码。
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

// WriteJSONToClientWriteCh 向客户端写入 JSON 消息，队列满时阻塞等待。
// 阻塞期间只响应 clientCtx 关闭，不受上游 ctx 取消影响。
func (c *Client) WriteJSONToClientWriteCh(ctx context.Context, v any) error {
	if ctx == nil {
		return ErrNilContext
	}
	// 隔离上游 ctx 的取消信号（如 Gin 请求结束），保留 tracing 等 value 数据
	lifecycleCtx := context.WithoutCancel(ctx)

	span := c.startSpan(lifecycleCtx, "ws.WriteJSONToClientWriteCh")
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

	select {
	case c.writeCh <- data:
		span.SetAttributes(attribute.Int("ws.write_queue_len", len(c.writeCh)))
		span.SetStatus(codes.Ok, "message queued")
		return nil
	case <-c.clientCtx.Done():
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return websocket.ErrCloseSent
	}
}

// WriteRawToClientWriteCh 直接写入预序列化的原始字节数据，跳过 JSON 序列化步骤。
// 队列满时阻塞等待，阻塞期间只响应 clientCtx 关闭，不受上游 ctx 取消影响。
func (c *Client) WriteRawToClientWriteCh(ctx context.Context, data []byte) error {
	if ctx == nil {
		return ErrNilContext
	}
	// 隔离上游 ctx 的取消信号（如 Gin 请求结束），保留 tracing 等 value 数据
	lifecycleCtx := context.WithoutCancel(ctx)

	span := c.startSpan(lifecycleCtx, "ws.WriteRawToClientWriteCh", attribute.Int("ws.data_size", len(data)))
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

	select {
	case c.writeCh <- data:
		span.SetAttributes(attribute.Int("ws.write_queue_len", len(c.writeCh)))
		span.SetStatus(codes.Ok, "raw message queued")
		return nil
	case <-c.clientCtx.Done():
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return websocket.ErrCloseSent
	}
}

// ReadMsgFromClientReadCh 同步阻塞读取客户端发送的一条消息。
func (c *Client) ReadMsgFromClientReadCh(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	// 隔离上游 ctx 的取消信号（如 Gin 请求结束），保留 tracing 等 value 数据
	lifecycleCtx := context.WithoutCancel(ctx)

	span := c.startSpan(lifecycleCtx, "ws.ReadMsgFromClientReadCh")
	defer span.End()

	if c.clientIsClosed.Load() {
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return nil, websocket.ErrCloseSent
	}

	select {
	case data, ok := <-c.readCh:
		if !ok {
			span.SetStatus(codes.Error, "connection closed")
			return nil, websocket.ErrCloseSent
		}
		span.SetAttributes(attribute.Int("ws.msg_size", len(data)))
		span.SetStatus(codes.Ok, "message received")
		return data, nil
	case <-c.clientCtx.Done():
		span.SetStatus(codes.Error, "connection closed")
		return nil, websocket.ErrCloseSent
	}
}
