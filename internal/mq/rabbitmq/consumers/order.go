package consumers

import (
	"context"
	"time"

	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"

	"github.com/18721889353/sunshine/internal/config"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/rabbitmq"
)

func init() {
	mq.RegisterConsumer(newOrderConsumer())
}

type orderConsumer struct {
	name            string
	consumer        *rabbitmq.Consumer
	stopConsumeChan chan struct{} // 用于控制循环的退出
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
	go func(s *orderConsumer) {
		logger.Info("Starting " + s.name)
		cfg := config.Get().Rabbitmq.Order
		if cfg.IsOpen {
			connection, err := rabbitmq.NewConnection(
				cfg.URL,
				rabbitmq.WithLogger(logger.Get()),
				rabbitmq.WithDialTimeout(time.Duration(cfg.DialTimeout)*time.Second),
				rabbitmq.WithDeadlineTimeout(time.Second*30),
				rabbitmq.WithHeartbeat(time.Second*10),
			)
			if err != nil {
				logger.Panic("异步消息队列 NewConnection error", logger.Err(err))
			}
			defer func() {
				if connection != nil {
					connection.Close()
				}
			}()
			for _, consumerInfo := range cfg.ConsumerInfos {
				exchange := rabbitmq.NewTopicExchange(cfg.ExchangeName, cfg.ExchangeName+"_"+cfg.NormalQueueName)
				// 定义公共选项
				commonOpts := []rabbitmq.ConsumerOption{
					rabbitmq.WithConsumerAutoAck(cfg.AutoAck),
					rabbitmq.WithConsumerQosOptions(
						rabbitmq.WithQosEnable(),
						rabbitmq.WithQosPrefetchCount(1),      //指定消费者可以同时处理的消息数量上限。例如，如果设置为 5，则消费者在处理完前 5 条消息之前不会接收新的消息。 prefetchCount 为 0，表示不限制消息数量
						rabbitmq.WithQosPrefetchSize(0),       //指定消费者可以同时处理的消息大小上限（以字节为单位）。例如，如果设置为 1024，则消费者在处理完当前消息总大小达到 1024 字节之前不会接收新的消息  prefetchSize 为 0，表示不限制消息大小
						rabbitmq.WithQosPrefetchGlobal(false), //指定是否全局应用 QoS 设置。如果设置为 true，则 QoS 设置将应用于所有消费者；如果设置为 false，则仅应用于当前通道上的消费者
					),
				}
				// 根据cfg.IsDeadQueue添加额外的选项
				var queueOpts []rabbitmq.ConsumerOption
				if cfg.IsDeadQueue {
					// 声明死信队列
					deadLetterExchange := rabbitmq.NewTopicExchange(cfg.ExchangeName, cfg.ExchangeName+"_"+cfg.DeadQueueName)
					deadOpts := []rabbitmq.ProducerOption{
						rabbitmq.WithProducerQueueDeclareOptions(
							rabbitmq.WithQueueDeclareArgs(
								map[string]interface{}{
									"x-message-ttl":             cfg.XMessageTTL,
									"x-dead-letter-exchange":    cfg.ExchangeName,
									"x-dead-letter-routing-key": cfg.ExchangeName + "_" + cfg.NormalQueueName,
								}),
						),
					}
					_, err = rabbitmq.NewProducer(deadLetterExchange, cfg.DeadQueueName, connection, deadOpts...)
					if err != nil {
						logger.Panic("Failed to create dead letter producer", logger.Err(err))
					}

					queueOpts = append(commonOpts, rabbitmq.WithConsumerQueueDeclareOptions(
						rabbitmq.WithQueueDeclareArgs(map[string]interface{}{
							"x-dead-letter-exchange":    cfg.ExchangeName,
							"x-dead-letter-routing-key": cfg.ExchangeName + "_" + cfg.DeadQueueName,
						}),
					))
				} else {
					queueOpts = commonOpts
				}
				consumer, err := rabbitmq.NewConsumer(exchange, cfg.NormalQueueName, connection, queueOpts...)
				if err != nil {
					logger.Panic("异步消息队列 failed to create rabbitmq consumer error", logger.Err(err))

				}
				// 启动消费者goroutine
				for i := 0; i < consumerInfo.CouponNum; i++ {
					consumer.Consume(context.Background(), s.handleOrderMessage)
				}
				logger.Info("消息队列 " + cfg.NormalQueueName + "启动成功")
			}
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
	logger.Warn("成功停止" + s.name)
	return nil
}

// handleOrderMessage 处理订单消息
func (s *orderConsumer) handleOrderMessage(ctx context.Context, data []byte, tagID string) error {
	logger.Info("Received order message", logger.Any("data", string(data)), logger.Any("tagID", tagID))
	// 解析订单数据
	logger.Info(s.name + "开始处理数据")
	time.Sleep(time.Second * 1)
	logger.Info("doing.... " + s.name)
	// 在这里添加实际的订单处理逻辑
	logger.Info(s.name + "数据处理完成")
	return nil
}
