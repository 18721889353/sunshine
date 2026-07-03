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

// dispatchLoop 后台协程：持续从后端接收跨实例消息并通过 WorkerPool 异步分发。
// WorkerPool 满时 Submit 阻塞发送，天然反压到消息源。
func (dd *DistributedDispatcher) dispatchLoop(ctx context.Context, msgCh <-chan *PubSubMessage) {
	defer dd.receiveWg.Done()
	tracer := otel.Tracer("gows")
	for {
		select {
		case <-dd.receiveStopCh:
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.WarnWithCtx(ctx, "ws dispatcher receiveLoop panic recovered",
							logger.Any("panic", r),
						)
					}
				}()
				if dd.workerPool != nil {
					if err := dd.workerPool.Submit(func() {
						dd.deliverMessage(ctx, tracer, msg)
					}); err != nil {
						dd.deliverMessage(ctx, tracer, msg)
					}
				} else {
					dd.deliverMessage(ctx, tracer, msg)
				}
			}()
		}
	}
}

// deliverMessage 根据消息类型将跨实例消息投递到本地匹配的客户端。
//
// 参数:
//   - ctx: 用于链路追踪的上下文
//   - tracer: OpenTelemetry Tracer，用于创建追踪 span
//   - msg: 从后端接收到的跨实例消息，包含类型、目标 UID 列表和负载
//
// 消息类型路由:
//   - MsgTypeBroadcast: 投递到所有本地在线客户端
//   - MsgTypeSendToUID: 投递到指定 UID 的本地客户端
//   - MsgTypeClientOnline: 更新远端在线 UID 集合（递增计数）
//   - MsgTypeClientOffline: 更新远端在线 UID 集合（递减计数）
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
		_ = dd.deliverBroadcast(span, msg.Payload)

	case MsgTypeSendToUID:
		_ = dd.WriteRawToLocalUIDs(span, msg.UIDs, msg.Payload)

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
		actual, _ := dd.remoteUIDCounts.LoadOrStore(uid, &atomic.Int32{})
		counter, ok := actual.(*atomic.Int32)
		if !ok {
			continue
		}
		if counter.Add(1) == 1 {
			dd.remoteUIDTotal.Add(1) // 新 UID 加入
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
		val, ok := dd.remoteUIDCounts.Load(uid)
		if !ok {
			continue
		}
		counter, ok := val.(*atomic.Int32)
		if !ok {
			continue
		}
		if counter.Add(-1) <= 0 {
			if _, loaded := dd.remoteUIDCounts.LoadAndDelete(uid); loaded {
				dd.remoteUIDTotal.Add(-1) // UID 移除
			}
		}
	}
}

// hasLocalUID 检查指定 UID 是否在本实例上有在线连接（O(1) 查找）。
func (dd *DistributedDispatcher) hasLocalUID(uid string) bool {
	_, ok := dd.localUIDCounts.Load(uid)
	return ok
}

// deliverBroadcast 向本地所有在线客户端广播消息。
func (dd *DistributedDispatcher) deliverBroadcast(span trace.Span, payload json.RawMessage) error {
	if dd.deliverPool == nil {
		return ErrNoDispatcher
	}
	var sentCount atomic.Int64
	totalCount, _ := dd.forEachAliveClient(func(c *Client) {
		if err := dd.submitDeliverTask(c, payload, "broadcast"); err != nil {
			logger.WarnWithCtx(context.Background(), "ws deliver pool submit failed",
				logger.String("op", "broadcast"),
				logger.Err(err),
			)
			return
		}
		sentCount.Add(1)
	})
	if span != nil {
		span.SetAttributes(
			attribute.Int("ws.local_sent", int(sentCount.Load())),
			attribute.Int("ws.local_clients", totalCount),
		)
		span.SetStatus(codes.Ok, "broadcast delivered")
	}
	return nil
}

// submitDeliverTask 提交投递任务到 ants 协程池，包含 recover 保护和写失败日志。
// op 用于日志标识，如 "broadcast"、"send_to_uid"。
func (dd *DistributedDispatcher) submitDeliverTask(client *Client, payload json.RawMessage, op string) error {
	return dd.deliverPool.Submit(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.WarnWithCtx(client.clientCtx, "ws deliver "+op+" goroutine panic",
					logger.String("uid", client.uid),
					logger.Any("panic", r),
				)
			}
		}()
		if err := client.WriteRawToClientWriteCh(client.clientCtx, payload); err != nil {
			logger.WarnWithCtx(client.clientCtx, "ws distributed "+op+" write failed",
				logger.String("uid", client.uid),
				logger.Err(err),
			)
		}
	})
}

// deleteDeadClients 批量从 dispatcher 中删除已关闭的僵尸连接，递减 localClientTotal 和 localUIDCounts。
func (dd *DistributedDispatcher) deleteDeadClients(clients []*Client) {
	for _, client := range clients {
		if _, loaded := dd.clients.LoadAndDelete(client); loaded {
			dd.localClientTotal.Add(-1)
			if client.uid != "" {
				dd.decrementLocalUIDCount(client.uid)
			}
		}
	}
}

// WriteRawToLocalUIDs 向本地指定 UID 的客户端投递预序列化的原始消息。
func (dd *DistributedDispatcher) WriteRawToLocalUIDs(span trace.Span, uids []string, payload json.RawMessage) error {
	if dd.deliverPool == nil {
		return ErrNoDispatcher
	}
	if len(uids) == 0 {
		if span != nil {
			span.SetStatus(codes.Ok, "no target uids")
		}
		return nil
	}

	uidSet := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uidSet[uid] = struct{}{}
	}

	var sentCount atomic.Int64
	totalCount, _ := dd.forEachAliveClient(func(c *Client) {
		if _, ok := uidSet[c.uid]; ok {
			if err := dd.submitDeliverTask(c, payload, "send_to_uid"); err != nil {
				logger.WarnWithCtx(context.Background(), "ws deliver pool submit failed",
					logger.String("uid", c.uid),
					logger.String("op", "send_to_uid"),
					logger.Err(err),
				)
				return
			}
			sentCount.Add(1)
		}
	})

	if span != nil {
		span.SetAttributes(
			attribute.Int("ws.local_sent", int(sentCount.Load())),
			attribute.Int("ws.local_clients", totalCount),
		)
		span.SetStatus(codes.Ok, "send_to_uid delivered")
	}
	return nil
}

// decrementLocalUIDCount 原子递减本地 UID 索引计数器，归零时删除条目。
func (dd *DistributedDispatcher) decrementLocalUIDCount(uid string) {
	val, ok := dd.localUIDCounts.Load(uid)
	if !ok {
		return
	}
	counter, ok := val.(*atomic.Int32)
	if ok && counter.Add(-1) <= 0 {
		dd.localUIDCounts.Delete(uid)
	}
}
