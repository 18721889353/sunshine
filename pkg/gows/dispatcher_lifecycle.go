package gows

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/18721889353/sunshine/pkg/logger"
)

// RegisterCtx 将客户端连接注册到 Dispatcher 全局列表（带自定义上下文）。
//
// 注册流程依次执行：最大连接数检查 → 空 UID 拒绝 → SSO 踢旧连接 → 入表计数 →
// 事件广播 → 消费队列。分布式模式下自动向所有实例广播上线通知。
//
// 参数：
//   - ctx：用于跨实例链路追踪的上下文，tracing 信息随事件消息传播
//   - client：已建立的 WebSocket 客户端连接，需包含 uid、remoteAddr 等有效信息
//
// 返回值：
//   - nil：注册成功，client 已加入全局列表并开始接收广播/点对点消息
//   - ErrMaxConnections：达到最大连接数上限，调用方应执行 client.Close() 释放资源
//   - ErrEmptyUID：客户端 UID 为空，禁止注册
func (dd *DistributedDispatcher) RegisterCtx(ctx context.Context, client *Client) error {
	maxClientNum := dd.maxClientNum.Load()
	if maxClientNum > 0 {
		currentCount := dd.localClientTotal.Load()
		if currentCount >= maxClientNum {
			logger.WarnWithCtx(client.clientCtx, "ws dispatcher max connections reached, rejecting",
				logger.String("uid", client.uid),
				logger.String("remote_addr", client.remoteAddr),
				logger.Int32("max", maxClientNum),
			)
			return ErrMaxConnections
		}
	}

	// 空 UID 禁止注册
	if client.uid == "" {
		return ErrEmptyUID
	}

	// 单点登录：先踢旧连接，再注册新连接
	if dd.enableSSO.Load() {
		if oldClient, ok := dd.ssoClients.Load(client.uid); ok {
			if prevClient, ok := oldClient.(*Client); ok && prevClient != client {
				// 从 Dispatcher 中移除旧连接，确保注册新连接前状态一致
				dd.clients.Delete(prevClient)
				dd.localClientTotal.Add(-1)
				dd.decrementLocalUIDCount(prevClient.uid)
				if err := prevClient.Close(); err != nil {
					logger.WarnWithCtx(client.clientCtx, "ws dispatcher close old SSO connection failed",
						logger.String("uid", prevClient.uid),
						logger.String("remote_addr", prevClient.remoteAddr),
						logger.Err(err),
					)
				}
			}
		}
	}

	dd.clients.Store(client, struct{}{})
	dd.localClientTotal.Add(1)
	// 递增本地 UID 连接计数
	actual, _ := dd.localUIDCounts.LoadOrStore(client.uid, &atomic.Int32{})
	if counter, ok := actual.(*atomic.Int32); ok {
		counter.Add(1)
	}

	// 更新 SSO 映射
	if dd.enableSSO.Load() {
		dd.ssoClients.Store(client.uid, client)
	}

	dd.publishMq(ctx, MsgTypeClientOnline, client.uid)
	dd.consumeAndDeliverUID(ctx, client.uid)
	return nil
}

// consumeAndDeliverUID 启动后端消费者订阅 UID 队列，后台 goroutine 持续接收
// 远端消息并投递给本地 WebSocket 客户端。
// 同一 UID 仅启动一个消费者 goroutine，重复调用直接返回。
// goroutine 中通过 InstanceID 过滤掉本实例自发布的消息，防止回环。
// 用户断线后 MQ 队列保留（消息不丢失），重连后继续消费积压消息。
//
// 参数：
//   - ctx：携带 tracing 信息的上下文
//   - uid：要消费消息的用户标识
func (dd *DistributedDispatcher) consumeAndDeliverUID(ctx context.Context, uid string) {
	if dd.backend == nil {
		return
	}

	stopCh := make(chan struct{})
	// 该 UID 消费者已启动，直接返回
	if _, loaded := dd.uidCancelChs.LoadOrStore(uid, stopCh); loaded {
		return
	}

	msgCh, err := dd.backend.Subscribe(ctx, uid)
	if err != nil {
		logger.WarnWithCtx(ctx, "subscribe uid failed",
			logger.String("uid", uid),
			logger.Err(err),
		)
		dd.uidCancelChs.Delete(uid)
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.WarnWithCtx(ctx, "ws dispatcher uidSub goroutine panic recovered",
					logger.String("uid", uid),
					logger.Any("panic", r),
				)
			}
		}()
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
				if err := dd.WriteRawToLocalUIDs(nil, []string{uid}, msg.Payload); err != nil {
					logger.WarnWithCtx(ctx, "ws uid consumer write to local failed",
						logger.String("uid", uid),
						logger.Err(err),
					)
				}
			}
		}
	}()
}

// UnregisterCtx 从 Dispatcher 全局列表中移除客户端连接（带自定义上下文）。
//
// 注销流程依次执行：从 clients 删除 → 事件广播 → 递减 UID 计数 →
// 清理 SSO → 停止消费。分布式模式下自动向所有实例广播下线通知。
//
// 参数：
//   - ctx：携带 tracing 信息的上下文
//   - client：要移除的客户端连接
//
// 返回值：
//   - nil：注销成功
func (dd *DistributedDispatcher) UnregisterCtx(ctx context.Context, client *Client) error {
	_, loaded := dd.clients.LoadAndDelete(client)
	if !loaded {
		return ErrClientNotRegistered
	}

	dd.localClientTotal.Add(-1)
	dd.decrementLocalUIDCount(client.uid)

	// 清理 SSO 映射（仅当该客户端是当前映射值时）
	if dd.enableSSO.Load() {
		dd.ssoClients.CompareAndDelete(client.uid, client)
	}

	dd.publishMq(ctx, MsgTypeClientOffline, client.uid)

	// 该 UID 无剩余连接时取消订阅，保留队列中的离线消息
	if !dd.hasLocalUID(client.uid) {
		dd.stopConsumeUID(ctx, client.uid)
	}
	return nil
}

// stopConsumeUID 停止消费指定 UID 的消息。
// 关闭消费者但保留 MQ 队列及离线消息，待用户重连后继续消费。
//
// 参数：
//   - ctx：携带 tracing 信息的上下文
//   - uid：要停止消费的用户标识
func (dd *DistributedDispatcher) stopConsumeUID(ctx context.Context, uid string) {
	if dd.backend == nil {
		return
	}

	if err := dd.backend.Unsubscribe(ctx, uid); err != nil {
		logger.WarnWithCtx(ctx, "unsubscribe uid failed",
			logger.String("uid", uid),
			logger.Err(err),
		)
	}

	if stopCh, loaded := dd.uidCancelChs.LoadAndDelete(uid); loaded {
		if ch, ok := stopCh.(chan struct{}); ok {
			close(ch)
		}
	}
}

// publishMq 向 Backend 发布客户端上线/下线事件。
// 自动填充 InstanceID 用于自发布消息跳过。
// backend 为 nil 时（单机模式）直接返回。
//
// 参数：
//   - ctx：携带 tracing 信息的上下文
//   - eventType：事件类型（MsgTypeClientOnline / MsgTypeClientOffline）
//   - uid：事件关联的用户标识
func (dd *DistributedDispatcher) publishMq(ctx context.Context, eventType, uid string) {
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
