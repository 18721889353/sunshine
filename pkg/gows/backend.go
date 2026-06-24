package gows

import (
	"context"
	"encoding/json"
)

// PubSubMessage 跨实例分发的消息结构。
// InstanceID 用于防止消息回环（自己发的消息自己不再重复处理）。
type PubSubMessage struct {
	InstanceID string          `json:"instance_id"`    // 发送方实例 ID
	Type       string          `json:"type"`           // "broadcast" | "send_to_uid"
	UIDs       []string        `json:"uids,omitempty"` // send_to_uid 的目标 UID 列表
	Payload    json.RawMessage `json:"payload"`        // JSON 序列化的业务消息
}

// Backend 分布式后端接口，抽象跨实例消息传递。
// 所有实现必须 goroutine 安全。
//
// 内置实现:
//   - NewRedisBackend(client, channel) — 基于 Redis Pub/Sub
//
// 可自行实现接入 Kafka / RabbitMQ / NATS 等中间件。
type Backend interface {
	// Publish 向所有实例发布消息。
	Publish(ctx context.Context, msg *PubSubMessage) error

	// Receive 返回一个只读消息通道，所有实例的消息通过此通道接收。
	// ctx 取消时关闭通道并释放资源。
	Receive(ctx context.Context) (<-chan *PubSubMessage, error)

	// Close 释放后端资源（如关闭 Redis Pub/Sub 连接）。
	Close() error
}
