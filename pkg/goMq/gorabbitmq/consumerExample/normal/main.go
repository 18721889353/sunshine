package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

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
	normalOpts := []gorabbitmq.ConsumerOption{
		gorabbitmq.WithConsumerNormalLetterOptions(
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
		gorabbitmq.WithConsumerQosOptions(
			gorabbitmq.WithQosEnable(),
			gorabbitmq.WithQosPrefetchCount(1),
			gorabbitmq.WithQosPrefetchSize(0),
			gorabbitmq.WithQosPrefetchGlobal(false),
		),
		gorabbitmq.WithConsumerConsumeOptions(
			gorabbitmq.WithConsumeConsumer("hello-normal"),
			gorabbitmq.WithConsumeExclusive(false),
			gorabbitmq.WithConsumeNoLocal(false),
			gorabbitmq.WithConsumeNoWait(false),
			gorabbitmq.WithConsumeArgs(nil),
		),
		gorabbitmq.WithConsumerAutoAck(true),
	}

	consumer, err := gorabbitmq.NewConsumer(exchange, "normalQueueName", conn, normalOpts...)
	consumer.Consume(ctx, func(ctx context.Context, data []byte, msgId, tagID string) error {
		fmt.Println(string(data))
		fmt.Println(tagID)
		return errors.New("fuck")
	})
	forever := make(chan struct{})
	<-forever
}
