// Package mq 提供 RabbitMQ 消息队列的通用消费者接口和注册管理功能。
// 该包定义了消费者接口，并提供了消费者的注册和获取机制。
package mq

import "context"

// Consumer 定义通用的消费者接口
type Consumer interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Name() string
}

var (
	// 消费者注册表
	consumerRegistry = make(map[string]Consumer)
)

// RegisterConsumer 注册消费者
func RegisterConsumer(consumer Consumer) {
	if _, exists := consumerRegistry[consumer.Name()]; exists {
		panic("duplicate consumer name: " + consumer.Name())
	}
	consumerRegistry[consumer.Name()] = consumer
}

// GetConsumers 获取所有已注册的消费者
func GetConsumers() []Consumer {
	consumers := make([]Consumer, 0, len(consumerRegistry))
	for _, consumer := range consumerRegistry {
		consumers = append(consumers, consumer)
	}
	return consumers
}
