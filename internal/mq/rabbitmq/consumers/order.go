package consumers

import (
	"context"
	"errors"
	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/database"
	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"
	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"sync"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/rabbitmq"
)

func init() {
	mq.RegisterConsumer(newOrderConsumer())
}

type orderConsumer struct {
	name            string
	consumer        *rabbitmq.Consumer
	stopConsumeChan chan struct{}  // 用于控制循环的退出
	wg              sync.WaitGroup // 用于等待处理完成
}

// NewOrderConsumer 初始化订单消费者
func newOrderConsumer() *orderConsumer {
	return &orderConsumer{
		name:            "orderMqConsumer",
		stopConsumeChan: make(chan struct{}),
	}
}

// Name 返回消费者名称
func (s *orderConsumer) Name() string {
	return s.name
}

// Start 启动消费者
func (s *orderConsumer) Start() error {
	ctx := context.Background()
	go func(s *orderConsumer) {
		cfg := config.Get().Rabbitmq
		mqObject := database.GetRabbitMQ()
		if cfg.Enable && cfg.TestOrder.Enable {
			logger.Info("Starting " + s.name)
			connection, err := mqObject.GetConnection(ctx)
			if err != nil {
				logger.Panic("database.GetRabbitMQ().GetConnection error", logger.Err(err))
			}

			exchangeName := cfg.TestOrder.ExchangeName

			deadQueueName := cfg.TestOrder.DeadQueueName
			deadRoutingKey := exchangeName + "." + deadQueueName
			normalQueueName := cfg.TestOrder.NormalQueueName
			normalRoutineKey := exchangeName + "." + normalQueueName
			exchange := gorabbitmq.NewDirectExchange(exchangeName, normalRoutineKey)
			if cfg.TestOrder.QueueType == "dead" {
				deadLetterOptions := cfg.TestOrder.DeadLetterOptions
				deadOpts := []gorabbitmq.ConsumerOption{
					gorabbitmq.WithConsumerDeadLetterOptions(
						gorabbitmq.WithDeadLetter(exchange.Name(), deadQueueName, deadRoutingKey, normalQueueName, exchange.RoutingKey()),
						gorabbitmq.WithDeadLetterExchangeDeclareOptions(
							gorabbitmq.WithExchangeDeclareDurable(deadLetterOptions.ExchangeDeclareOptions.Durable),
							gorabbitmq.WithExchangeDeclareAutoDelete(deadLetterOptions.ExchangeDeclareOptions.AutoDelete),
							gorabbitmq.WithExchangeDeclareInternal(deadLetterOptions.ExchangeDeclareOptions.Internal),
							gorabbitmq.WithExchangeDeclareNoWait(deadLetterOptions.ExchangeDeclareOptions.NoWait),
							gorabbitmq.WithExchangeDeclareArgs(nil),
						),
						gorabbitmq.WithDeadLetterDeadQueueDeclareOptions(
							gorabbitmq.WithQueueDeclareDurable(deadLetterOptions.DeadQueueDeclareOption.Durable),
							gorabbitmq.WithQueueDeclareExclusive(deadLetterOptions.DeadQueueDeclareOption.Exclusive),
							gorabbitmq.WithQueueDeclareAutoDelete(deadLetterOptions.DeadQueueDeclareOption.AutoDelete),
							gorabbitmq.WithQueueDeclareNoWait(deadLetterOptions.DeadQueueDeclareOption.NoWait),
							gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
								"x-dead-letter-exchange":    exchange.Name(),
								"x-dead-letter-routing-key": normalRoutineKey,
								"x-message-ttl":             20000,
							})),
						gorabbitmq.WithDeadLetterDeadQueueBindOptions(
							gorabbitmq.WithQueueBindNoWait(deadLetterOptions.DeadQueueBindOption.NoWait),
							gorabbitmq.WithQueueBindArgs(nil),
						),
						gorabbitmq.WithDeadLetterNormalQueueDeclareOptions(
							gorabbitmq.WithQueueDeclareDurable(deadLetterOptions.NormalQueueDeclareOption.Durable),
							gorabbitmq.WithQueueDeclareExclusive(deadLetterOptions.NormalQueueDeclareOption.Exclusive),
							gorabbitmq.WithQueueDeclareAutoDelete(deadLetterOptions.NormalQueueDeclareOption.AutoDelete),
							gorabbitmq.WithQueueDeclareNoWait(deadLetterOptions.NormalQueueDeclareOption.NoWait),
							gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
								"x-dead-letter-exchange":    exchange.Name(),
								"x-dead-letter-routing-key": deadRoutingKey,
							})),
						gorabbitmq.WithDeadLetterNormalQueueBindOptions(
							gorabbitmq.WithQueueBindNoWait(deadLetterOptions.NormalQueueBindOption.NoWait),
							gorabbitmq.WithQueueBindArgs(nil),
						),
					),
					gorabbitmq.WithConsumerQosOptions(
						gorabbitmq.WithQosEnable(),
						gorabbitmq.WithQosPrefetchCount(deadLetterOptions.ConsumerOption.PrefetchCount),
						gorabbitmq.WithQosPrefetchSize(deadLetterOptions.ConsumerOption.PrefetchSize),
						gorabbitmq.WithQosPrefetchGlobal(deadLetterOptions.ConsumerOption.Global),
					),
					gorabbitmq.WithConsumerConsumeOptions(
						gorabbitmq.WithConsumeConsumer(deadLetterOptions.ConsumerOption.Consumer),
						gorabbitmq.WithConsumeExclusive(deadLetterOptions.ConsumerOption.Exclusive),
						gorabbitmq.WithConsumeNoLocal(deadLetterOptions.ConsumerOption.NoLocal),
						gorabbitmq.WithConsumeNoWait(deadLetterOptions.ConsumerOption.NoWait),
						gorabbitmq.WithConsumeArgs(nil),
					),
					gorabbitmq.WithConsumerMsgDurable(deadLetterOptions.ConsumerOption.MsgDurable),
					gorabbitmq.WithConsumerAutoAck(deadLetterOptions.ConsumerOption.IsAutoAck),
				}
				consumer, err := gorabbitmq.NewConsumer(exchange, normalQueueName, connection, deadOpts...)
				if err != nil {
					logger.Panic("异步消息队列 failed to create rabbitmq consumer error", logger.Err(err))
				}
				consumer.Consume(ctx, s.handleOrderMessage)
			}
			logger.Info("消息队列 " + cfg.TestOrder.NormalQueueName + "启动成功")
		}
		<-s.stopConsumeChan

		logger.Warn(s.name + " goroutine 退出")
	}(s)
	return nil
}

// Stop 停止消费者
func (s *orderConsumer) Stop() error {
	logger.Warn("开始停止" + s.name)
	if s.stopConsumeChan != nil {
		close(s.stopConsumeChan)
	}
	// 等待所有正在处理的消息完成
	s.wg.Wait()
	logger.Warn("成功停止" + s.name)
	return nil
}

// handleOrderMessage 处理订单消息
func (s *orderConsumer) handleOrderMessage(ctx context.Context, data []byte, tagID string) error {
	s.wg.Add(1)
	defer s.wg.Done()
	// 解析订单数据
	// 在这里添加实际的订单处理逻辑
	return errors.New("fuck")
}
