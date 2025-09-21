package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

// ProducerExample 生产者使用示例
func main() {
	ctx := context.Background()
	//// 创建 RabbitMQ 连接
	//conn, err := gorabbitmq.NewConnection(
	//	ctx,
	//	"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
	//	gorabbitmq.WithLogger(logger.Get()),
	//	gorabbitmq.WithMaxRetries(0), // 无限重试
	//	gorabbitmq.WithReconnectTime(2*time.Second),
	//	gorabbitmq.WithDialTimeout(5*time.Second),
	//)
	//if err != nil {
	//	log.Fatalf("创建连接失败: %v", err)
	//}
	//defer conn.Close()

	pool, err := gorabbitmq.NewPool(
		ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		gorabbitmq.WithInitialCap(1),            // 初始连接数
		gorabbitmq.WithMaxCap(1000),             // 最大连接数
		gorabbitmq.WithMaxIdle(time.Second*10),  // 最大空闲时间
		gorabbitmq.WithPoolLogger(logger.Get()), // 日志记录器
		gorabbitmq.WithAntsPoolSize(1),          // 配置 ants 协程池大小为 10
		gorabbitmq.WithConnOptions( // 连接选项
			gorabbitmq.WithLogger(logger.Get()),
			gorabbitmq.WithReconnectTime(time.Second*3),
			gorabbitmq.WithDialTimeout(time.Second*5),
			gorabbitmq.WithHeartbeat(time.Second*3),
		),
	)
	if err != nil {
		logger.Fatal("Failed to create connection pool", zap.Error(err))
	}
	defer pool.Close(ctx)
	conn, err := pool.Get(ctx)
	if err != nil {
		logger.Fatal("Failed to get connection from pool", zap.Error(err))
	}
	exchangeName := "exchange"
	deadQueueName := "deadQueueName"
	normalQueueName := "deadNormalQueueName"
	deadRoutingKey := exchangeName + "." + deadQueueName
	normalRoutineKey := exchangeName + "." + normalQueueName
	exchange := gorabbitmq.NewDirectExchange(exchangeName, normalRoutineKey)
	// 创建生产者
	deadOpts := []gorabbitmq.ConsumerOption{
		gorabbitmq.WithConsumerDeadLetterOptions(
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
		gorabbitmq.WithConsumerQosOptions(
			gorabbitmq.WithQosEnable(),
			gorabbitmq.WithQosPrefetchCount(1),
			gorabbitmq.WithQosPrefetchSize(0),
			gorabbitmq.WithQosPrefetchGlobal(false),
		),
		gorabbitmq.WithConsumerConsumeOptions(
			gorabbitmq.WithConsumeConsumer("hello"),
			gorabbitmq.WithConsumeExclusive(false),
			gorabbitmq.WithConsumeNoLocal(false),
			gorabbitmq.WithConsumeNoWait(false),
			gorabbitmq.WithConsumeArgs(nil),
		),
		gorabbitmq.WithConsumerAutoAck(false),
		gorabbitmq.WithConsumerMsgDurable(true),
	}
	consumer, err := gorabbitmq.NewConsumer(exchange, normalQueueName, conn, deadOpts...)
	consumer.Consume(ctx, func(ctx context.Context, data []byte, messageId string, tagID string) error {
		fmt.Println(string(data))
		fmt.Println(tagID)
		fmt.Println(messageId)
		return errors.New("fuck")
	})
	forever := make(chan struct{})
	<-forever
}
