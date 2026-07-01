package gows

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/18721889353/sunshine/pkg/logger"
)

// subscribeUID 订阅指定 UID 的队列消息，启动后台消费。
// 用户断线后队列保留，重连后继续消费积压消息。
func (dd *DistributedDispatcher) subscribeUID(ctx context.Context, uid string) {
	if dd.backend == nil || uid == "" {
		return
	}

	stopCh := make(chan struct{})
	if _, loaded := dd.uidSubs.LoadOrStore(uid, stopCh); loaded {
		return // 已订阅
	}

	msgCh, err := dd.backend.Subscribe(ctx, uid)
	if err != nil {
		logger.WarnWithCtx(ctx, "subscribe uid failed",
			logger.String("uid", uid),
			logger.Err(err),
		)
		dd.uidSubs.Delete(uid)
		return
	}

	go func() {
		for {
			select {
			case <-stopCh:
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				// 跳过自发布消息回环
				if msg.InstanceID == dd.instanceID {
					continue
				}
				// 投递到本地对应用户
				dd.deliverToUIDs(nil, []string{uid}, msg.Payload)
			}
		}
	}()
}

// unsubscribeUID 取消订阅指定 UID 的队列。
func (dd *DistributedDispatcher) unsubscribeUID(ctx context.Context, uid string) {
	if dd.backend == nil || uid == "" {
		return
	}

	if err := dd.backend.Unsubscribe(ctx, uid); err != nil {
		logger.WarnWithCtx(ctx, "unsubscribe uid failed",
			logger.String("uid", uid),
			logger.Err(err),
		)
	}

	if stopCh, loaded := dd.uidSubs.LoadAndDelete(uid); loaded {
		if ch, ok := stopCh.(chan struct{}); ok {
			close(ch)
		}
	}
}

// RegisterCtx 将客户端连接注册到 Dispatcher 全局列表（带自定义上下文）。
// 如果达到最大连接数上限，返回 ErrMaxConnections。
// 分布式模式下自动向所有实例广播上线通知，ctx 中的 tracing 信息会随消息传播。
// 调用方应检查此错误并执行 client.Close() 释放连接资源。
//
// 注意:
//   - 若 ctx 已过期，返回 ctx.Err() 并回滚 map 修改
func (dd *DistributedDispatcher) RegisterCtx(ctx context.Context, client *Client) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 先检查 ctx 是否已取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxConns := dd.maxConns.Load()
	if maxConns > 0 {
		currentCount := dd.clientCount.Load()
		if currentCount >= maxConns {
			dd.totalRejected.Add(1)
			logger.WarnWithCtx(client.clientCtx, "ws dispatcher max connections reached, rejecting",
				logger.String("uid", client.uid),
				logger.String("remote_addr", client.remoteAddr),
				logger.Int32("max", maxConns),
			)
			return ErrMaxConnections
		}
	}

	dd.clients.Store(client, struct{}{})
	dd.clientCount.Add(1)
	// 更新 map 后检查 ctx，过期则回滚
	select {
	case <-ctx.Done():
		dd.clients.Delete(client)
		dd.clientCount.Add(-1)
		return ctx.Err()
	default:
	}

	// 递增本地 UID 索引（确认注册后，不回滚）
	if client.uid != "" {
		actual, _ := dd.uidIndex.LoadOrStore(client.uid, &atomic.Int32{})
		counter, ok := actual.(*atomic.Int32)
		if ok {
			counter.Add(1)
		}
	}

	dd.publishClientEvent(ctx, MsgTypeClientOnline, client.uid)

	// 订阅该 UID 的持久化队列（Direct 模式，支持离线消息）
	dd.subscribeUID(ctx, client.uid)
	return nil
}

// UnregisterCtx 从 Dispatcher 全局列表中移除客户端连接（带自定义上下文）。
// 分布式模式下自动向所有实例广播下线通知，ctx 中的 tracing 信息会随消息传播。
// 若 ctx 已过期，回滚 map 删除并返回 ctx.Err()。
func (dd *DistributedDispatcher) UnregisterCtx(ctx context.Context, client *Client) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 先检查 ctx 是否已取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	_, loaded := dd.clients.LoadAndDelete(client)
	if loaded {
		dd.clientCount.Add(-1)
	}
	// 删除后检查 ctx，过期则回滚
	select {
	case <-ctx.Done():
		if loaded {
			dd.clients.Store(client, struct{}{})
			dd.clientCount.Add(1)
		}
		return ctx.Err()
	default:
	}
	dd.publishClientEvent(ctx, MsgTypeClientOffline, client.uid)

	// 递减本地 UID 索引（确认注销后，不回滚）
	if client.uid != "" && loaded {
		dd.decrementUIDIndex(client.uid)
	}

	// 该 UID 无剩余连接时取消订阅，保留队列中的离线消息
	if !dd.hasLocalUID(client.uid) {
		dd.unsubscribeUID(ctx, client.uid)
	}
	return nil
}

// publishClientEvent 向 Backend 发布客户端上线/下线事件。
// 自动填充 InstanceID 用于自发布消息跳过。
func (dd *DistributedDispatcher) publishClientEvent(ctx context.Context, eventType, uid string) {
	if dd.backend == nil {
		return
	}
	data, err := json.Marshal(map[string]string{"uid": uid})
	if err != nil {
		logger.WarnWithCtx(ctx, "dispatcher publish event marshal failed",
			logger.String("event", eventType),
			logger.String("uid", uid),
			logger.Err(err),
		)
		return
	}
	if err := dd.backend.Publish(ctx, &PubSubMessage{
		InstanceID: dd.instanceID,
		Type:       eventType,
		UIDs:       []string{uid},
		Payload:    data,
	}); err != nil {
		logger.WarnWithCtx(ctx, "dispatcher publish event failed",
			logger.String("event", eventType),
			logger.Err(err),
		)
	}
}
