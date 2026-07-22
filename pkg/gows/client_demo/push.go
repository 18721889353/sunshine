// Package service 业务逻辑层 - 提供 WebSocket 消息推送能力
//
// 基于 Sunshine gows + RabbitMQ 架构，下游微服务通过此包
// 向 WebSocket 网关的 DistributedDispatcher 发送消息，
// 实现跨实例的精准推送和广播。
//
// 推送流程:
//
//	web (业务服务)
//	  │
//	  ├─ SendToUser(ctx, uid, data)
//	  │     └─ backend.Publish(PubSubMessage{send_to_uid, [uid], payload})
//	  │           └─ RabbitMQ Direct Exchange "ws:messages", routing_key = uid
//	  │                              ↓
//	  │            gw Subscribe(uid) 消费
//	  │                              ↓
//	  │            WriteRawToLocalUIDs → client.WriteRawToClientWriteCh()
//	  │                              ↓
//	  │            客户端收到推送消息
//	  │
//	  ├─ Broadcast(ctx, data)
//	  └─ BroadcastReliable(ctx, data)
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/gows"
	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	pushBackend     *gows.RabbitMQBackend
	pushBackendOnce sync.Once
)

// getPushBackend 惰性初始化 gows.RabbitMQBackend（单例，线程安全）。
// 使用 gows.NewRabbitMQBackend 而非 gorabbitmqclient，原因：
//   - 直接使用 gows 的 Publish API，与 gw 网关使用同一套消息协议
//   - 无需手动序列化 PubSubMessage，无需关心 exchange/routing_key 细节
//   - 内部自带 ProducerPool（连接池），复用 TCP 连接，性能更优
func getPushBackend(_ context.Context) *gows.RabbitMQBackend {
	pushBackendOnce.Do(func() {
		pushBackend = gows.NewRabbitMQBackend(
			config.Get().Websocket.Distributed.RabbitmqURL,
			config.Get().Websocket.Distributed.Exchange,
		)
		pushBackend.SetPoolCap(
			config.Get().Websocket.Distributed.PoolInitialCap,
			config.Get().Websocket.Distributed.PoolMaxCap,
		)
		pushBackend.SetReceiveChanSize(config.Get().Websocket.Distributed.ReceiveChanSize)
	})
	return pushBackend
}

// SendToUser 向指定用户推送消息。
//
// 参数:
//   - ctx: 上下文（建议传入含 request_id 的业务 context，用于链路追踪）
//   - uid: 目标用户 ID（与 gw 网关中 WebSocket Client 的 UID 一致）
//   - data: 业务数据，最终以 JSON 形式发送给客户端
//
// 返回值:
//   - error: 推送失败时返回错误（MQ 连接异常、序列化失败等）
//
// 示例:
//
//	import "hello/internal/service"
//
//	service.SendToUser(ctx, "user_123", map[string]interface{}{
//	    "event": "order_update",
//	    "order_id": "ORD20260717001",
//	    "status": "paid",
//	})
func SendToUser(ctx context.Context, uid string, data interface{}) error {
	if uid == "" {
		logger.WarnWithCtx(ctx, "[push] SendToUser skipped: empty uid")
		return nil
	}
	return SendToMultiUser(ctx, []string{uid}, data)
}

// SendToMultiUser 向多个用户推送消息。
//
// 参数:
//   - ctx: 上下文
//   - uids: 目标用户 ID 列表（自动跳过空字符串）
//   - data: 业务数据，最终以 JSON 形式发送给客户端
//
// 示例:
//
//	service.SendToMultiUser(ctx, []string{"user_1", "user_2"}, notification)
func SendToMultiUser(ctx context.Context, uids []string, data interface{}) error {
	if len(uids) == 0 {
		logger.WarnWithCtx(ctx, "[push] SendToMultiUser skipped: empty uids")
		return nil
	}

	payload, err := json.Marshal(data)
	if err != nil {
		logger.WarnWithCtx(ctx, "[push] marshal payload failed", logger.Err(err))
		return fmt.Errorf("push marshal payload: %w", err)
	}

	backend := getPushBackend(ctx)

	msg := &gows.PubSubMessage{
		InstanceID: config.Get().App.Name,
		Type:       gows.MsgTypeSendToUID,
		UIDs:       uids,
		Payload:    payload,
	}

	if err := backend.Publish(ctx, msg); err != nil {
		logger.WarnWithCtx(ctx, "[push] publish failed",
			logger.Any("uids", uids),
			logger.Err(err),
		)
		return fmt.Errorf("push publish: %w", err)
	}

	logger.InfoWithCtx(ctx, "[push] sent success",
		logger.Int("uid_count", len(uids)),
	)
	return nil
}

// Broadcast 向所有在线客户端广播消息。
// 注意: 广播使用 Fanout 交换机，不持久化。重启后离线消息不保留。
//
// 参数:
//   - ctx: 上下文
//   - data: 业务数据
func Broadcast(ctx context.Context, data interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("broadcast marshal payload: %w", err)
	}

	backend := getPushBackend(ctx)

	msg := &gows.PubSubMessage{
		InstanceID: config.Get().App.Name,
		Type:       gows.MsgTypeBroadcast,
		Payload:    payload,
	}

	if err := backend.Publish(ctx, msg); err != nil {
		logger.WarnWithCtx(ctx, "[push] broadcast failed", logger.Err(err))
		return fmt.Errorf("broadcast publish: %w", err)
	}

	return nil
}
