package gows

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ---------------------------------------------------------------------------
// 公开 API：发送 / 广播
// ---------------------------------------------------------------------------

// SendToUIDCtx 向指定 UID 的客户端发送消息（跨实例）。
// 所有实例通过 Backend 接收后在本地查找并投递。
// 自动清理已关闭的僵尸连接。
func (dd *DistributedDispatcher) SendToUIDCtx(ctx context.Context, uid string, v any) {
	dd.SendToMultiUIDCtx(ctx, []string{uid}, v)
}

// SendToMultiUIDCtx 向多个指定 UID 的客户端发送消息（跨实例）。
func (dd *DistributedDispatcher) SendToMultiUIDCtx(ctx context.Context, uids []string, v any) {
	if dd.backend == nil {
		// 单机模式：直接本地投递
		payload, err := json.Marshal(v)
		if err != nil {
			logger.WarnWithCtx(ctx, "ws send_to_uids marshal failed", logger.Err(err))
			return
		}
		tracer := otel.Tracer("gows")
		_, span := tracer.Start(ctx, "ws.local.send_to_uids",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		defer span.End()
		dd.deliverToUIDs(span, uids, payload)
		return
	}

	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.distributed.send_to_uids",
		trace.WithSpanKind(trace.SpanKindProducer),
	)
	defer span.End()
	span.SetAttributes(
		attribute.StringSlice("ws.target_uids", uids),
		requestIDAttr(ctx),
	)

	if len(uids) == 0 {
		span.SetStatus(codes.Ok, "no target uids")
		return
	}

	payload, err := json.Marshal(v)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws distributed send_to_uids marshal failed",
			logger.Err(err),
		)
		return
	}

	msg := &PubSubMessage{
		InstanceID: dd.instanceID,
		Type:       "send_to_uid",
		UIDs:       uids,
		Payload:    payload,
	}
	if err := dd.backend.Publish(ctx, msg); err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws distributed send_to_uids publish failed",
			logger.Err(err),
			logger.String("uids", fmt.Sprintf("%v", uids)),
		)
		return
	}

	span.SetStatus(codes.Ok, "published")
}

// BroadcastCtx 向所有在线客户端广播消息（跨实例）。
func (dd *DistributedDispatcher) BroadcastCtx(ctx context.Context, v any) {
	if dd.backend == nil {
		// 单机模式：直接本地广播
		payload, err := json.Marshal(v)
		if err != nil {
			logger.WarnWithCtx(ctx, "ws broadcast marshal failed", logger.Err(err))
			return
		}
		tracer := otel.Tracer("gows")
		_, span := tracer.Start(ctx, "ws.local.broadcast",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		defer span.End()
		dd.deliverBroadcast(span, payload)
		return
	}

	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.distributed.broadcast",
		trace.WithSpanKind(trace.SpanKindProducer),
	)
	defer span.End()
	span.SetAttributes(requestIDAttr(ctx))

	payload, err := json.Marshal(v)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws distributed broadcast marshal failed",
			logger.Err(err),
		)
		return
	}

	msg := &PubSubMessage{
		InstanceID: dd.instanceID,
		Type:       "broadcast",
		Payload:    payload,
	}
	if err := dd.backend.Publish(ctx, msg); err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws distributed broadcast publish failed",
			logger.Err(err),
		)
		return
	}

	span.SetStatus(codes.Ok, "published")
}

// BroadcastFilterCtx 向满足 filter 条件的本地客户端广播消息（仅本地）。
func (dd *DistributedDispatcher) BroadcastFilterCtx(ctx context.Context, v any, filter func(*Client) bool) {
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.broadcast_filter", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(requestIDAttr(ctx))

	dd.mu.RLock()
	clients := make([]*Client, 0, len(dd.clients))
	for client := range dd.clients {
		clients = append(clients, client)
	}
	dd.mu.RUnlock()

	span.SetAttributes(attribute.Int("ws.broadcast_targets", len(clients)))

	var deadClients []*Client
	payload, err := json.Marshal(v)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws broadcast_filter marshal failed", logger.Err(err))
		return
	}
	for _, client := range clients {
		if !client.IsAlive() {
			deadClients = append(deadClients, client)
			continue
		}
		if filter(client) {
			if err := client.WriteRawCtx(client.ctx, payload); err != nil {
				logger.WarnWithCtx(ctx, "ws broadcast_filter write failed",
					logger.String("uid", client.uid),
					logger.Err(err),
				)
			}
		}
	}

	if len(deadClients) > 0 {
		span.SetAttributes(attribute.Int("ws.dead_clients_cleaned", len(deadClients)))
		dd.mu.Lock()
		for _, client := range deadClients {
			delete(dd.clients, client)
		}
		dd.mu.Unlock()
	}

	span.SetStatus(codes.Ok, "broadcast_filter completed")
}
