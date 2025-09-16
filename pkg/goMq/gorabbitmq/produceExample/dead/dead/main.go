package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

// ProducerExample 生产者使用示例
func main() {
	ctx := context.Background()
	// 创建 RabbitMQ 连接
	conn, err := gorabbitmq.NewConnection(
		ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		gorabbitmq.WithLogger(logger.Get()),
		gorabbitmq.WithMaxRetries(0), // 无限重试
		gorabbitmq.WithReconnectTime(2*time.Second),
		gorabbitmq.WithDialTimeout(5*time.Second),
	)
	if err != nil {
		log.Fatalf("创建连接失败: %v", err)
	}
	exchangeName := "exchange"
	deadQueueName := "deadQueueName"
	normalQueueName := "deadNormalQueueName"
	deadRoutingKey := exchangeName + "." + deadQueueName
	normalRoutineKey := exchangeName + "." + normalQueueName
	defer conn.Close()
	exchange := gorabbitmq.NewDirectExchange(exchangeName, normalRoutineKey)
	// 创建生产者
	deadOpts := []gorabbitmq.ProducerOption{
		gorabbitmq.WithProducerIsDelay(true),
		gorabbitmq.WithProducerDeadLetterOptions(
			gorabbitmq.WithDeadLetter(exchange.Name(), deadQueueName, deadRoutingKey, normalQueueName, exchange.RoutingKey()),
			gorabbitmq.WithDeadLetterExchangeDeclareOptions(
				gorabbitmq.WithExchangeDeclareDurable(true),
				gorabbitmq.WithExchangeDeclareAutoDelete(false),
				gorabbitmq.WithExchangeDeclareInternal(false),
				gorabbitmq.WithExchangeDeclareNoWait(false),
				gorabbitmq.WithExchangeDeclareArgs(nil),
			),
			gorabbitmq.WithDeadLetterDeadQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(true),
				gorabbitmq.WithQueueDeclareExclusive(false),
				gorabbitmq.WithQueueDeclareAutoDelete(false),
				gorabbitmq.WithQueueDeclareNoWait(false),
				gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
					"x-dead-letter-exchange":    exchange.Name(),
					"x-dead-letter-routing-key": normalRoutineKey,
					"x-message-ttl":             20000,
				})),
			gorabbitmq.WithDeadLetterDeadQueueBindOptions(
				gorabbitmq.WithQueueBindNoWait(false),
				gorabbitmq.WithQueueBindArgs(nil),
			),
			gorabbitmq.WithDeadLetterNormalQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(true),
				gorabbitmq.WithQueueDeclareExclusive(false),
				gorabbitmq.WithQueueDeclareAutoDelete(false),
				gorabbitmq.WithQueueDeclareNoWait(false),
				gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
					"x-dead-letter-exchange":    exchange.Name(),
					"x-dead-letter-routing-key": deadRoutingKey,
				})),
			gorabbitmq.WithDeadLetterNormalQueueBindOptions(
				gorabbitmq.WithQueueBindNoWait(false),
				gorabbitmq.WithQueueBindArgs(nil),
			),
		),
	}
	producer, err := gorabbitmq.NewProducer(ctx, exchange, conn, deadOpts...)
	if err != nil {
		log.Fatalf("创建生产者失败: %v", err)
	}
	defer producer.Close()

	err = producer.PublishDirect(
		ctx,
		[]byte("Hello"),
	)
	if err != nil {
		panic(err)
	}

	fmt.Println("生产者示例执行完成，请运行消费者示例查看消息接收情况")
}
