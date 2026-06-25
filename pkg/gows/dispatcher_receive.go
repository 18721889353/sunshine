package gows

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// receiveLoop 后台协程：持续从后端接收跨实例消息并在本地投递。
// 使用 WorkerPool 实现背压保护：当 Worker 全忙时，新消息降级为串行处理。
func (dd *DistributedDispatcher) receiveLoop(ctx context.Context, msgCh <-chan *PubSubMessage) {
	defer dd.wg.Done()

	tracer := otel.Tracer("gows")
	for {
		select {
		case <-dd.done:
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			dd.dispatchMessage(ctx, tracer, msg)
		}
	}
}

// dispatchMessage 将消息分发到 WorkerPool 或串行处理。
// WorkerPool 可用时异步投递，否则串行降级（反压保护）。
func (dd *DistributedDispatcher) dispatchMessage(ctx context.Context, tracer trace.Tracer, msg *PubSubMessage) {
	if atomic.LoadInt32(&dd.workerStarted) == 1 {
		// WorkerPool 模式：尝试投递到 Worker 队列
		task := func() {
			dd.deliverMessage(ctx, tracer, msg)
		}
		select {
		case dd.workerCh <- task:
			return // 成功投递
		default:
			// Worker 队列满，降级为串行处理（反压保护）
			logger.WarnWithCtx(ctx, "ws dispatcher worker pool full, fallback to serial processing")
		}
	}
	// 串行处理（默认或降级）
	dd.deliverMessage(ctx, tracer, msg)
}

// deliverMessage 将一条跨实例消息投递到本地匹配的客户端。
func (dd *DistributedDispatcher) deliverMessage(ctx context.Context, tracer trace.Tracer, msg *PubSubMessage) {
	_, span := tracer.Start(ctx, "ws.distributed.deliver",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.msg_type", msg.Type),
		attribute.Int("ws.target_uids", len(msg.UIDs)),
		requestIDAttr(ctx),
	)

	switch msg.Type {
	case "broadcast":
		dd.deliverBroadcast(span, msg.Payload)

	case "send_to_uid":
		dd.deliverToUIDs(span, msg.UIDs, msg.Payload)

	case "client_online":
		dd.handleRemoteOnline(msg.UIDs)

	case "client_offline":
		dd.handleRemoteOffline(msg.UIDs)

	default:
		span.SetStatus(codes.Error, "unknown message type: "+msg.Type)
	}
}

// handleRemoteOnline 处理远端客户端上线通知。
// 如果 UID 已在本实例上连接，则不重复计入远端（避免自发布干扰）。
func (dd *DistributedDispatcher) handleRemoteOnline(uids []string) {
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		// 跳过本实例已有的 UID
		if dd.hasLocalUID(uid) {
			continue
		}
		// 递增计数
		for {
			val, _ := dd.remoteUIDs.LoadOrStore(uid, int32(0))
			count, ok := val.(int32)
			if !ok {
				continue
			}
			if dd.remoteUIDs.CompareAndSwap(uid, count, count+1) {
				break
			}
		}
	}
}

// handleRemoteOffline 处理远端客户端下线通知。
func (dd *DistributedDispatcher) handleRemoteOffline(uids []string) {
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		// 如果本实例上仍有此 UID，说明是本实例的上线通知被其他实例回传，忽略下线
		if dd.hasLocalUID(uid) {
			continue
		}
		val, ok := dd.remoteUIDs.Load(uid)
		if !ok {
			continue
		}
		count, ok := val.(int32)
		if !ok {
			continue
		}
		if count <= 1 {
			dd.remoteUIDs.Delete(uid)
		} else {
			dd.remoteUIDs.CompareAndSwap(uid, count, count-1)
		}
	}
}

// hasLocalUID 检查指定 UID 是否在本实例上有在线连接。
func (dd *DistributedDispatcher) hasLocalUID(uid string) bool {
	dd.mu.RLock()
	defer dd.mu.RUnlock()
	for client := range dd.clients {
		if client.uid == uid {
			return true
		}
	}
	return false
}

// deliverBroadcast 向本地所有在线客户端广播消息。
func (dd *DistributedDispatcher) deliverBroadcast(span trace.Span, payload json.RawMessage) {
	dd.mu.RLock()
	clients := make([]*Client, 0, len(dd.clients))
	for client := range dd.clients {
		clients = append(clients, client)
	}
	dd.mu.RUnlock()

	var deadClients []*Client
	var sentCount int
	for _, client := range clients {
		if !client.IsAlive() {
			deadClients = append(deadClients, client)
			continue
		}
		if err := client.WriteRaw(payload); err != nil {
			logger.WarnWithCtx(client.ctx, "ws distributed broadcast write failed",
				logger.String("uid", client.uid),
				logger.Err(err),
			)
		}
		sentCount++
	}

	span.SetAttributes(
		attribute.Int("ws.local_sent", sentCount),
		attribute.Int("ws.local_clients", len(clients)),
	)

	if len(deadClients) > 0 {
		dd.mu.Lock()
		for _, client := range deadClients {
			delete(dd.clients, client)
		}
		dd.mu.Unlock()
	}

	span.SetStatus(codes.Ok, "broadcast delivered")
}

// deliverToUIDs 向本地指定 UID 的客户端投递消息。
func (dd *DistributedDispatcher) deliverToUIDs(span trace.Span, uids []string, payload json.RawMessage) {
	if len(uids) == 0 {
		span.SetStatus(codes.Ok, "no target uids")
		return
	}

	uidSet := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uidSet[uid] = struct{}{}
	}

	dd.mu.RLock()
	clients := make([]*Client, 0, len(dd.clients))
	for client := range dd.clients {
		clients = append(clients, client)
	}
	dd.mu.RUnlock()

	var deadClients []*Client
	var sentCount int
	for _, client := range clients {
		if !client.IsAlive() {
			deadClients = append(deadClients, client)
			continue
		}
		if _, ok := uidSet[client.uid]; ok {
			if err := client.WriteRaw(payload); err != nil {
				logger.WarnWithCtx(client.ctx, "ws distributed send_to_uid write failed",
					logger.String("uid", client.uid),
					logger.Err(err),
				)
			}
			sentCount++
		}
	}

	span.SetAttributes(
		attribute.Int("ws.local_sent", sentCount),
		attribute.Int("ws.local_clients", len(clients)),
	)

	if len(deadClients) > 0 {
		dd.mu.Lock()
		for _, client := range deadClients {
			delete(dd.clients, client)
		}
		dd.mu.Unlock()
	}

	span.SetStatus(codes.Ok, "send_to_uid delivered")
}
