package mq

//import (
//	"context"
//	"strconv"
//	"sync"
//
//	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
//	"github.com/18721889353/sunshine/pkg/logger"
//)
//
//// BaseConsumer 提供RabbitMQ消费者的基础实现
//type BaseConsumer struct {
//	name            string
//	stopConsumeChan chan struct{}
//	wg              sync.WaitGroup
//	handler         MessageHandler
//}
//
//// MessageHandler 定义消息处理函数类型
//type MessageHandler func(ctx context.Context, data []byte, tagID string) error
//
//// NewBaseConsumer 创建一个新的基础消费者
//func NewBaseConsumer(name string, handler MessageHandler) *BaseConsumer {
//	return &BaseConsumer{
//		name:            name,
//		stopConsumeChan: make(chan struct{}),
//		handler:         handler,
//	}
//}
//
//// Name 返回消费者名称
//func (bc *BaseConsumer) Name() string {
//	return bc.name
//}
//
//// handleMessage 内部消息处理函数，适配gorabbitmq的Handler类型
//func (bc *BaseConsumer) handleMessage(ctx context.Context, data []byte, tagID string) error {
//	bc.wg.Add(1)
//	defer bc.wg.Done()
//	return bc.handler(ctx, data, tagID)
//}
//
//// Start 启动消费者
//func (bc *BaseConsumer) Start(queueConfig config.DoingOrder, queueType string) error {
//	ctx := context.Background()
//	go func() {
//		cfg := config.Get().Rabbitmq
//		mqObject := database.GetRabbitMQ()
//		if cfg.Enable && queueConfig.Enable {
//			logger.Info("Starting " + bc.name)
//			exchangeName := queueConfig.ExchangeName
//			deadQueueName := queueConfig.DeadQueueName
//			deadRoutingKey := exchangeName + "." + deadQueueName
//			normalQueueName := queueConfig.NormalQueueName
//			normalRoutineKey := exchangeName + "." + normalQueueName
//			exchange := gorabbitmq.NewDirectExchange(exchangeName, normalRoutineKey)
//			queueOptions := queueConfig
//			// 获取需要启动的消费者数量
//			consumerNum := queueOptions.ConsumerNum
//			if consumerNum <= 0 {
//				consumerNum = 1
//			}
//			// 创建指定数量的消费者
//			for i := 0; i < consumerNum; i++ {
//				// 为每个消费者创建独立的函数作用域，确保每个消费者使用独立的连接
//				func(consumerIndex int) {
//					connection, err := mqObject.GetConnection(ctx)
//					if err != nil {
//						logger.Panic("database.GetRabbitMQ().GetConnection error", logger.Err(err))
//					}
//					defer mqObject.PutConnection(ctx, connection) // 将连接放回连接池
//
//					// 为每个消费者构建独立的选项
//					consumerOpts := []gorabbitmq.ConsumerOption{
//						gorabbitmq.WithConsumerQosOptions(
//							gorabbitmq.WithQosEnable(),
//							gorabbitmq.WithQosPrefetchCount(queueOptions.ConsumerOption.PrefetchCount),
//							gorabbitmq.WithQosPrefetchSize(queueOptions.ConsumerOption.PrefetchSize),
//							gorabbitmq.WithQosPrefetchGlobal(queueOptions.ConsumerOption.Global),
//						),
//						gorabbitmq.WithConsumerMsgDurable(queueOptions.ConsumerOption.MsgDurable),
//						gorabbitmq.WithConsumerAutoAck(queueOptions.ConsumerOption.IsAutoAck),
//					}
//
//					// 设置消费者的唯一名称
//					consumerName := queueOptions.ConsumerOption.Consumer
//					if consumerName == "" {
//						consumerName = "consumer"
//					}
//					consumerName += strconv.Itoa(consumerIndex + 1)
//
//					// 添加消费选项
//					consumerOpts = append(consumerOpts, gorabbitmq.WithConsumerConsumeOptions(
//						gorabbitmq.WithConsumeConsumer(consumerName),
//						gorabbitmq.WithConsumeExclusive(queueOptions.ConsumerOption.Exclusive),
//						gorabbitmq.WithConsumeNoLocal(queueOptions.ConsumerOption.NoLocal),
//						gorabbitmq.WithConsumeNoWait(queueOptions.ConsumerOption.NoWait),
//						gorabbitmq.WithConsumeArgs(nil),
//					))
//
//					if queueType == "dead" {
//						consumerOpts = append(consumerOpts,
//							gorabbitmq.WithConsumerDeadLetterOptions(
//								gorabbitmq.WithDeadLetter(exchange.Name(), deadQueueName, deadRoutingKey, normalQueueName, exchange.RoutingKey()),
//								gorabbitmq.WithDeadLetterExchangeDeclareOptions(
//									gorabbitmq.WithExchangeDeclareDurable(queueOptions.ExchangeDeclareOptions.Durable),
//									gorabbitmq.WithExchangeDeclareAutoDelete(queueOptions.ExchangeDeclareOptions.AutoDelete),
//									gorabbitmq.WithExchangeDeclareInternal(queueOptions.ExchangeDeclareOptions.Internal),
//									gorabbitmq.WithExchangeDeclareNoWait(queueOptions.ExchangeDeclareOptions.NoWait),
//									gorabbitmq.WithExchangeDeclareArgs(nil),
//								),
//								gorabbitmq.WithDeadLetterDeadQueueDeclareOptions(
//									gorabbitmq.WithQueueDeclareDurable(queueOptions.DeadQueueDeclareOption.Durable),
//									gorabbitmq.WithQueueDeclareExclusive(queueOptions.DeadQueueDeclareOption.Exclusive),
//									gorabbitmq.WithQueueDeclareAutoDelete(queueOptions.DeadQueueDeclareOption.AutoDelete),
//									gorabbitmq.WithQueueDeclareNoWait(queueOptions.DeadQueueDeclareOption.NoWait),
//									gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
//										"x-dead-letter-exchange":    exchange.Name(),
//										"x-dead-letter-routing-key": normalRoutineKey,
//										"x-message-ttl":             queueOptions.DeadQueueDeclareOption.Args.XMessageTTL,
//									})),
//								gorabbitmq.WithDeadLetterDeadQueueBindOptions(
//									gorabbitmq.WithQueueBindNoWait(queueOptions.DeadQueueBindOption.NoWait),
//									gorabbitmq.WithQueueBindArgs(nil),
//								),
//								gorabbitmq.WithDeadLetterNormalQueueDeclareOptions(
//									gorabbitmq.WithQueueDeclareDurable(queueOptions.NormalQueueDeclareOption.Durable),
//									gorabbitmq.WithQueueDeclareExclusive(queueOptions.NormalQueueDeclareOption.Exclusive),
//									gorabbitmq.WithQueueDeclareAutoDelete(queueOptions.NormalQueueDeclareOption.AutoDelete),
//									gorabbitmq.WithQueueDeclareNoWait(queueOptions.NormalQueueDeclareOption.NoWait),
//									gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
//										"x-dead-letter-exchange":    exchange.Name(),
//										"x-dead-letter-routing-key": deadRoutingKey,
//									})),
//								gorabbitmq.WithDeadLetterNormalQueueBindOptions(
//									gorabbitmq.WithQueueBindNoWait(queueOptions.NormalQueueBindOption.NoWait),
//									gorabbitmq.WithQueueBindArgs(nil),
//								),
//							))
//					}
//					if queueType == "normal" {
//						consumerOpts = append(consumerOpts,
//							gorabbitmq.WithConsumerNormalLetterOptions(
//								gorabbitmq.WithNormalLetter(exchange.Name(), normalQueueName, exchange.RoutingKey()),
//								gorabbitmq.WithNormalLetterExchangeDeclareOptions(
//									gorabbitmq.WithExchangeDeclareDurable(queueOptions.ExchangeDeclareOptions.Durable),
//									gorabbitmq.WithExchangeDeclareAutoDelete(queueOptions.ExchangeDeclareOptions.AutoDelete),
//									gorabbitmq.WithExchangeDeclareInternal(queueOptions.ExchangeDeclareOptions.Internal),
//									gorabbitmq.WithExchangeDeclareNoWait(queueOptions.ExchangeDeclareOptions.NoWait),
//									gorabbitmq.WithExchangeDeclareArgs(nil),
//								),
//								gorabbitmq.WithNormalLetterNormalQueueDeclareOptions(
//									gorabbitmq.WithQueueDeclareDurable(queueOptions.NormalQueueDeclareOption.Durable),
//									gorabbitmq.WithQueueDeclareExclusive(queueOptions.NormalQueueDeclareOption.Exclusive),
//									gorabbitmq.WithQueueDeclareAutoDelete(queueOptions.NormalQueueDeclareOption.AutoDelete),
//									gorabbitmq.WithQueueDeclareNoWait(queueOptions.NormalQueueDeclareOption.NoWait),
//									gorabbitmq.WithQueueDeclareArgs(nil),
//								),
//								gorabbitmq.WithNormalLetterNormalQueueBindOptions(
//									gorabbitmq.WithQueueBindNoWait(queueOptions.NormalQueueBindOption.NoWait),
//									gorabbitmq.WithQueueBindArgs(nil),
//								),
//							))
//					}
//
//					consumer, err := gorabbitmq.NewConsumer(exchange, normalQueueName, connection, consumerOpts...)
//					if err != nil {
//						logger.Panic("异步消息队列 failed to create rabbitmq consumer error", logger.Err(err))
//					}
//					consumer.Consume(ctx, bc.handleMessage)
//					logger.Info("消息队列 " + queueConfig.NormalQueueName + " 第" + strconv.Itoa(consumerIndex+1) + "个消费者(" + consumerName + ")启动成功")
//				}(i)
//			}
//			logger.Info("消息队列 " + queueConfig.NormalQueueName + " 总共启动了 " + strconv.Itoa(consumerNum) + " 个消费者")
//		}
//		<-bc.stopConsumeChan
//
//		logger.Warn(bc.name + " goroutine 退出")
//	}()
//	return nil
//}
//
//// Stop 停止消费者
//func (bc *BaseConsumer) Stop() error {
//	logger.Warn("开始停止" + bc.name)
//	if bc.stopConsumeChan != nil {
//		close(bc.stopConsumeChan)
//	}
//	// 等待所有正在处理的消息完成
//	bc.wg.Wait()
//	logger.Warn("成功停止" + bc.name)
//	return nil
//}
