package gows

import (
	"context"
	"encoding/json"
)

// PubSubMessage 跨实例分发的消息结构。
// InstanceID 用于防止消息回环（自己发的消息自己不再重复处理）。
type PubSubMessage struct {
	InstanceID string          `json:"instance_id"`    // 发送方实例 ID
	Type       string          `json:"type"`           // "broadcast" | "send_to_uid" | "client_online" | "client_offline"
	UIDs       []string        `json:"uids,omitempty"` // send_to_uid 的目标 UID 列表
	Payload    json.RawMessage `json:"payload"`        // JSON 序列化的业务消息
}

// Backend 分布式后端接口，抽象跨实例消息传递。
// 所有实现必须 goroutine 安全。
//
// 内置实现:
//   - NewRabbitMQBackend(url, exchange) — 基于 RabbitMQ Direct 交换机
//
// 可自行实现接入 Kafka / Redis Pub/Sub / NATS 等中间件。
type Backend interface {
	// Publish 发布消息到分布式后端。
	// 根据消息类型决定路由:
	//   - broadcast: 广播到所有实例
	//   - send_to_uid: 按 UIDs 路由到对应持久化队列
	//   - client_online/client_offline: 广播到所有实例
	Publish(ctx context.Context, msg *PubSubMessage) error

	// ReceiveBroadcast 返回本实例的广播消息通道。
	// 所有 broadcast/client_online/client_offline 类型消息通过此通道接收。
	// ctx 取消时关闭通道并释放资源。
	ReceiveBroadcast(ctx context.Context) (<-chan *PubSubMessage, error)

	// Subscribe 订阅指定 UID 的消息队列。
	// 为每个在线用户创建独立消费者，绑定到该 UID 的持久化队列。
	// 用户断线后队列保留（消息不丢失），重连后继续消费。
	Subscribe(ctx context.Context, uid string) (<-chan *PubSubMessage, error)

	// Unsubscribe 取消订阅指定 UID 的消息队列。
	// 关闭消费者但保留队列及其中的消息，支持离线消息积压。
	Unsubscribe(ctx context.Context, uid string) error

	// Close 释放后端资源（如关闭 RabbitMQ 连接和所有消费者）。
	Close() error
}
