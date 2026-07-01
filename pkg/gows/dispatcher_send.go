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
		span.SetAttributes(requestIDAttr(ctx))
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

	// 1. 先投递本地客户端（立即送达，不走 MQ 回环）
	dd.deliverToUIDs(span, uids, payload)

	// 2. 再发布到 MQ 供远端实例消费（自发布消息由 subscribeUID 的 InstanceID 过滤跳过）
	msg := &PubSubMessage{
		InstanceID: dd.instanceID,
		Type:       MsgTypeSendToUID,
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
// 广播使用临时队列（exclusive+auto-delete），不做持久化，重启后离线消息不保留。
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
		span.SetAttributes(requestIDAttr(ctx))
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
		Type:       MsgTypeBroadcast,
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

// BroadcastReliableCtx 可靠广播：转为按所有在线 UID 逐个下发。
// 每条消息走 UID 持久化队列，自动支持离线积压和重连补推。
// 适用于需要确保所有用户（含离线用户）最终能收到的广播场景。
func (dd *DistributedDispatcher) BroadcastReliableCtx(ctx context.Context, v any) {
	uids := dd.ConnectedUIDs()
	if len(uids) == 0 {
		return
	}
	dd.SendToMultiUIDCtx(ctx, uids, v)
}

// BroadcastFilterCtx 向满足 filter 条件的本地客户端广播消息（仅本地）。
func (dd *DistributedDispatcher) BroadcastFilterCtx(ctx context.Context, v any, filter func(*Client) bool) {
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.broadcast_filter", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(requestIDAttr(ctx))

	payload, err := json.Marshal(v)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws broadcast_filter marshal failed", logger.Err(err))
		return
	}

	var deadClients []*Client
	var totalCount, sentCount int
	dd.clients.Range(func(key, _ any) bool {
		c, ok := key.(*Client)
		if !ok {
			return true
		}
		totalCount++
		if !c.IsAlive() {
			deadClients = append(deadClients, c)
			return true
		}
		if filter(c) {
			if err := c.WriteRawCtx(c.clientCtx, payload); err != nil {
				logger.WarnWithCtx(ctx, "ws broadcast_filter write failed",
					logger.String("uid", c.uid),
					logger.Err(err),
				)
			}
			sentCount++
		}
		return true
	})

	span.SetAttributes(
		attribute.Int("ws.broadcast_targets", totalCount),
		attribute.Int("ws.broadcast_sent", sentCount),
	)

	if len(deadClients) > 0 {
		span.SetAttributes(attribute.Int("ws.dead_clients_cleaned", len(deadClients)))
		dd.deleteDeadClients(deadClients)
	}

	span.SetStatus(codes.Ok, "broadcast_filter completed")
}
