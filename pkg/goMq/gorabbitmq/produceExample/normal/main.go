package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

// Message 示例消息结构体
type Message struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
	Time    string `json:"time"`
}

// ProducerExample 生产者使用示例
func main() {
	ctx := context.Background()
	// 创建 RabbitMQ 连接
	conn, err := gorabbitmq.NewConnection(
		ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		gorabbitmq.WithMaxRetries(0), // 无限重试
		gorabbitmq.WithReconnectTime(2*time.Second),
		gorabbitmq.WithDialTimeout(5*time.Second),
	)
	if err != nil {
		log.Fatalf("创建连接失败: %v", err)
	}
	defer conn.Close()
	exchangeName := "exchange"
	normalQueueName := "normalQueueName"
	normalRoutineKey := "normalRoutingKey"
	exchange := gorabbitmq.NewDirectExchange(exchangeName, normalRoutineKey)
	// 创建生产者
	normalOpts := []gorabbitmq.ProducerOption{
		gorabbitmq.WithProducerNormalLetterOptions(
			gorabbitmq.WithNormalLetter(exchange.Name(), normalQueueName, exchange.RoutingKey()),
			gorabbitmq.WithNormalLetterExchangeDeclareOptions(
				gorabbitmq.WithExchangeDeclareDurable(true),
				gorabbitmq.WithExchangeDeclareAutoDelete(false),
				gorabbitmq.WithExchangeDeclareInternal(false),
				gorabbitmq.WithExchangeDeclareNoWait(false),
				gorabbitmq.WithExchangeDeclareArgs(nil),
			),
			gorabbitmq.WithNormalLetterNormalQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(true),
				gorabbitmq.WithQueueDeclareExclusive(false),
				gorabbitmq.WithQueueDeclareAutoDelete(false),
				gorabbitmq.WithQueueDeclareNoWait(false),
				gorabbitmq.WithQueueDeclareArgs(nil),
			),
			gorabbitmq.WithNormalLetterNormalQueueBindOptions(
				gorabbitmq.WithQueueBindNoWait(false),
				gorabbitmq.WithQueueBindArgs(nil),
			),
		),
	}
	producer, err := gorabbitmq.NewProducer(ctx, exchange, conn, normalOpts...)
	if err != nil {
		log.Fatalf("创建生产者失败: %v", err)
	}
	defer func() { _ = producer.Close() }()

	err = producer.PublishDirect(
		ctx,
		normalRoutineKey,
		[]byte("Hello"),
		uuid.New().String(),
	)
	if err != nil {
		panic(err)
	}

	fmt.Println("生产者示例执行完成，请运行消费者示例查看消息接收情况")
}
