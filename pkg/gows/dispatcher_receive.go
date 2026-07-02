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
	defer dd.receiveWg.Done()

	tracer := otel.Tracer("gows")
	for {
		if dd.receiveOnce(ctx, tracer, msgCh) {
			return
		}
	}
}

// receiveOnce 单次接收并处理一条消息，panic 恢复后返回 false 继续循环。
func (dd *DistributedDispatcher) receiveOnce(ctx context.Context, tracer trace.Tracer, msgCh <-chan *PubSubMessage) (done bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.WarnWithCtx(ctx, "ws dispatcher receiveOnce panic recovered",
				logger.Any("panic", r),
			)
			done = false // 继续循环
		}
	}()
	select {
	case <-dd.receiveStopCh:
		return true
	case msg, ok := <-msgCh:
		if !ok {
			return true
		}
		dd.dispatchMessage(ctx, tracer, msg)
		return false
	}
}

// dispatchMessage 将消息分发到 WorkerPool 或串行处理。
// WorkerPool 可用时异步投递，否则串行降级（反压保护）。
func (dd *DistributedDispatcher) dispatchMessage(ctx context.Context, tracer trace.Tracer, msg *PubSubMessage) {
	if dd.workerStarted.Load() {
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
	case MsgTypeBroadcast:
		dd.deliverBroadcast(span, msg.Payload)

	case MsgTypeSendToUID:
		dd.deliverToUIDs(span, msg.UIDs, msg.Payload)

	case MsgTypeClientOnline:
		dd.handleRemoteOnline(msg.UIDs)

	case MsgTypeClientOffline:
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
		// 使用 *atomic.Int32 指针原子递增，无 CAS 循环
		actual, _ := dd.remoteUIDs.LoadOrStore(uid, &atomic.Int32{})
		counter, ok := actual.(*atomic.Int32)
		if !ok {
			continue
		}
		if counter.Add(1) == 1 {
			dd.remoteUIDCount.Add(1) // 新 UID 加入
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
		counter, ok := val.(*atomic.Int32)
		if !ok {
			continue
		}
		if counter.Add(-1) <= 0 {
			if _, loaded := dd.remoteUIDs.LoadAndDelete(uid); loaded {
				dd.remoteUIDCount.Add(-1) // UID 移除
			}
		}
	}
}

// hasLocalUID 检查指定 UID 是否在本实例上有在线连接（O(1) 查找）。
func (dd *DistributedDispatcher) hasLocalUID(uid string) bool {
	_, ok := dd.uidIndex.Load(uid)
	return ok
}

// deliverBroadcast 向本地所有在线客户端广播消息。
func (dd *DistributedDispatcher) deliverBroadcast(span trace.Span, payload json.RawMessage) {
	var deadClients []*Client
	var sentCount, totalCount int
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
		if err := c.WriteRawCtx(c.clientCtx, payload); err != nil {
			logger.WarnWithCtx(c.clientCtx, "ws distributed broadcast write failed",
				logger.String("uid", c.uid),
				logger.Err(err),
			)
		}
		sentCount++
		return true
	})

	span.SetAttributes(
		attribute.Int("ws.local_sent", sentCount),
		attribute.Int("ws.local_clients", totalCount),
	)

	if len(deadClients) > 0 {
		dd.deleteDeadClients(deadClients)
	}

	span.SetStatus(codes.Ok, "broadcast delivered")
}

// deleteDeadClients 批量从 dispatcher 中删除已关闭的僵尸连接，递减 clientCount 和 uidIndex。
func (dd *DistributedDispatcher) deleteDeadClients(clients []*Client) {
	for _, client := range clients {
		if _, loaded := dd.clients.LoadAndDelete(client); loaded {
			dd.clientCount.Add(-1)
			if client.uid != "" {
				dd.decrementUIDIndex(client.uid)
			}
		}
	}
}

// deliverToUIDs 向本地指定 UID 的客户端投递消息。
func (dd *DistributedDispatcher) deliverToUIDs(span trace.Span, uids []string, payload json.RawMessage) {
	if len(uids) == 0 {
		if span != nil {
			span.SetStatus(codes.Ok, "no target uids")
		}
		return
	}

	uidSet := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uidSet[uid] = struct{}{}
	}

	var deadClients []*Client
	var sentCount, totalCount int
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
		if _, ok := uidSet[c.uid]; ok {
			if err := c.WriteRawCtx(c.clientCtx, payload); err != nil {
				logger.WarnWithCtx(c.clientCtx, "ws distributed send_to_uid write failed",
					logger.String("uid", c.uid),
					logger.Err(err),
				)
			}
			sentCount++
		}
		return true
	})

	if span != nil {
		span.SetAttributes(
			attribute.Int("ws.local_sent", sentCount),
			attribute.Int("ws.local_clients", totalCount),
		)
	}

	if len(deadClients) > 0 {
		dd.deleteDeadClients(deadClients)
	}

	if span != nil {
		span.SetStatus(codes.Ok, "send_to_uid delivered")
	}
}

// decrementUIDIndex 原子递减本地 UID 索引计数器，归零时删除条目。
func (dd *DistributedDispatcher) decrementUIDIndex(uid string) {
	val, ok := dd.uidIndex.Load(uid)
	if !ok {
		return
	}
	counter, ok := val.(*atomic.Int32)
	if ok && counter.Add(-1) <= 0 {
		dd.uidIndex.Delete(uid)
	}
}
