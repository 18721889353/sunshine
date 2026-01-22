package mq

//
//import (
//	"context"
//	"github.com/18721889353/sunshine/internal/config"
//	"github.com/18721889353/sunshine/internal/database"
//	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
//	"github.com/18721889353/sunshine/pkg/logger"
//	"strconv"
//	"sync"
//)
//
//// BaseConsumer 提供RabbitMQ消费者的基础实现
//type BaseConsumer struct {
//	name           string
//	wg             sync.WaitGroup
//	handler        MessageHandler
//	consumers      []*gorabbitmq.Consumer
//	consumersMutex sync.RWMutex
//	startOnce      sync.Once
//}
//
//// MessageHandler 定义消息处理函数类型
//type MessageHandler func(ctx context.Context, data []byte, messageId string, tagID string) error
//
//// NewBaseConsumer 创建一个新的基础消费者
//func NewBaseConsumer(name string, handler MessageHandler) *BaseConsumer {
//	return &BaseConsumer{
//		name:    name,
//		handler: handler,
//	}
//}
//
//// Name 返回消费者名称
//func (bc *BaseConsumer) Name() string {
//	return bc.name
//}
//
//// handleMessage 内部消息处理函数，适配gorabbitmq的Handler类型
//func (bc *BaseConsumer) handleMessage(ctx context.Context, data []byte, messageId string, tagID string) error {
//	bc.wg.Add(1)
//	defer bc.wg.Done()
//	return bc.handler(ctx, data, messageId, tagID)
//}
//
//// 辅助方法：构建基础选项
//func (bc *BaseConsumer) prepareOptions(queueConfig config.DoingOrder, index int) []gorabbitmq.ConsumerOption {
//	consumerName := queueConfig.ConsumerOption.Consumer
//	if consumerName == "" {
//		consumerName = "consumer"
//	}
//	consumerName += "_" + strconv.Itoa(index+1)
//
//	return []gorabbitmq.ConsumerOption{
//		gorabbitmq.WithConsumerQosOptions(
//			gorabbitmq.WithQosEnable(),
//			gorabbitmq.WithQosPrefetchCount(queueConfig.ConsumerOption.PrefetchCount),
//			gorabbitmq.WithQosPrefetchSize(queueConfig.ConsumerOption.PrefetchSize),
//			gorabbitmq.WithQosPrefetchGlobal(queueConfig.ConsumerOption.Global),
//		),
//		gorabbitmq.WithConsumerMsgDurable(queueConfig.ConsumerOption.MsgDurable),
//		gorabbitmq.WithConsumerAutoAck(queueConfig.ConsumerOption.IsAutoAck),
//		gorabbitmq.WithConsumerConsumeOptions(
//			gorabbitmq.WithConsumeConsumer(consumerName),
//			gorabbitmq.WithConsumeExclusive(queueConfig.ConsumerOption.Exclusive),
//			gorabbitmq.WithConsumeNoLocal(queueConfig.ConsumerOption.NoLocal),
//			gorabbitmq.WithConsumeNoWait(queueConfig.ConsumerOption.NoWait),
//		),
//	}
//}
//
//// buildBaseOptions 构建通用的消费者配置
//func (bc *BaseConsumer) buildBaseOptions(cfg config.DoingOrder, index int) []gorabbitmq.ConsumerOption {
//	name := cfg.ConsumerOption.Consumer
//	if name == "" {
//		name = "consumer"
//	}
//	name += strconv.Itoa(index + 1)
//
//	return []gorabbitmq.ConsumerOption{
//		// 为每个消费者构建独立的选项
//		gorabbitmq.WithConsumerQosOptions(
//			gorabbitmq.WithQosEnable(),
//			gorabbitmq.WithQosPrefetchCount(cfg.ConsumerOption.PrefetchCount),
//			gorabbitmq.WithQosPrefetchSize(cfg.ConsumerOption.PrefetchSize),
//			gorabbitmq.WithQosPrefetchGlobal(cfg.ConsumerOption.Global),
//		),
//		gorabbitmq.WithConsumerMsgDurable(cfg.ConsumerOption.MsgDurable),
//		gorabbitmq.WithConsumerAutoAck(cfg.ConsumerOption.IsAutoAck),
//		// 添加消费选项
//		gorabbitmq.WithConsumerConsumeOptions(
//			gorabbitmq.WithConsumeConsumer(name),
//			gorabbitmq.WithConsumeExclusive(cfg.ConsumerOption.Exclusive),
//			gorabbitmq.WithConsumeNoLocal(cfg.ConsumerOption.NoLocal),
//			gorabbitmq.WithConsumeNoWait(cfg.ConsumerOption.NoWait),
//			gorabbitmq.WithConsumeArgs(nil),
//		),
//	}
//}
//
//// buildDeadLetterOptions 封装死信队列选项
//func (bc *BaseConsumer) buildDeadLetterOptions(exchange *gorabbitmq.Exchange, cfg config.DoingOrder, deadQueueName, deadRoutingKey, normalQueueName, normalRoutineKey string) []gorabbitmq.ConsumerOption {
//	return []gorabbitmq.ConsumerOption{
//		gorabbitmq.WithConsumerDeadLetterOptions(
//			gorabbitmq.WithDeadLetter(exchange.Name(), deadQueueName, deadRoutingKey, normalQueueName, exchange.RoutingKey()),
//			gorabbitmq.WithDeadLetterExchangeDeclareOptions(
//				gorabbitmq.WithExchangeDeclareDurable(cfg.ExchangeDeclareOptions.Durable),
//				gorabbitmq.WithExchangeDeclareAutoDelete(cfg.ExchangeDeclareOptions.AutoDelete),
//				gorabbitmq.WithExchangeDeclareInternal(cfg.ExchangeDeclareOptions.Internal),
//				gorabbitmq.WithExchangeDeclareNoWait(cfg.ExchangeDeclareOptions.NoWait),
//				gorabbitmq.WithExchangeDeclareArgs(nil),
//			),
//			gorabbitmq.WithDeadLetterDeadQueueDeclareOptions(
//				gorabbitmq.WithQueueDeclareDurable(cfg.DeadQueueDeclareOption.Durable),
//				gorabbitmq.WithQueueDeclareExclusive(cfg.DeadQueueDeclareOption.Exclusive),
//				gorabbitmq.WithQueueDeclareAutoDelete(cfg.DeadQueueDeclareOption.AutoDelete),
//				gorabbitmq.WithQueueDeclareNoWait(cfg.DeadQueueDeclareOption.NoWait),
//				gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
//					"x-dead-letter-exchange":    exchange.Name(),
//					"x-dead-letter-routing-key": normalRoutineKey,
//					"x-message-ttl":             cfg.DeadQueueDeclareOption.Args.XMessageTTL,
//				})),
//			gorabbitmq.WithDeadLetterDeadQueueBindOptions(
//				gorabbitmq.WithQueueBindNoWait(cfg.DeadQueueBindOption.NoWait),
//				gorabbitmq.WithQueueBindArgs(nil),
//			),
//			gorabbitmq.WithDeadLetterNormalQueueDeclareOptions(
//				gorabbitmq.WithQueueDeclareDurable(cfg.NormalQueueDeclareOption.Durable),
//				gorabbitmq.WithQueueDeclareExclusive(cfg.NormalQueueDeclareOption.Exclusive),
//				gorabbitmq.WithQueueDeclareAutoDelete(cfg.NormalQueueDeclareOption.AutoDelete),
//				gorabbitmq.WithQueueDeclareNoWait(cfg.NormalQueueDeclareOption.NoWait),
//				gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
//					"x-dead-letter-exchange":    exchange.Name(),
//					"x-dead-letter-routing-key": deadRoutingKey,
//				})),
//			gorabbitmq.WithDeadLetterNormalQueueBindOptions(
//				gorabbitmq.WithQueueBindNoWait(cfg.NormalQueueBindOption.NoWait),
//				gorabbitmq.WithQueueBindArgs(nil),
//			),
//		),
//	}
//}
//
//// buildNormalLetterOptions 封装普通队列选项
//func (bc *BaseConsumer) buildNormalLetterOptions(exchange *gorabbitmq.Exchange, cfg config.DoingOrder, normalQueueName string) []gorabbitmq.ConsumerOption {
//	return []gorabbitmq.ConsumerOption{
//		gorabbitmq.WithConsumerNormalLetterOptions(
//			gorabbitmq.WithNormalLetter(exchange.Name(), normalQueueName, exchange.RoutingKey()),
//			gorabbitmq.WithNormalLetterExchangeDeclareOptions(
//				gorabbitmq.WithExchangeDeclareDurable(cfg.ExchangeDeclareOptions.Durable),
//				gorabbitmq.WithExchangeDeclareAutoDelete(cfg.ExchangeDeclareOptions.AutoDelete),
//				gorabbitmq.WithExchangeDeclareInternal(cfg.ExchangeDeclareOptions.Internal),
//				gorabbitmq.WithExchangeDeclareNoWait(cfg.ExchangeDeclareOptions.NoWait),
//				gorabbitmq.WithExchangeDeclareArgs(nil),
//			),
//			gorabbitmq.WithNormalLetterNormalQueueDeclareOptions(
//				gorabbitmq.WithQueueDeclareDurable(cfg.NormalQueueDeclareOption.Durable),
//				gorabbitmq.WithQueueDeclareExclusive(cfg.NormalQueueDeclareOption.Exclusive),
//				gorabbitmq.WithQueueDeclareAutoDelete(cfg.NormalQueueDeclareOption.AutoDelete),
//				gorabbitmq.WithQueueDeclareNoWait(cfg.NormalQueueDeclareOption.NoWait),
//				gorabbitmq.WithQueueDeclareArgs(nil),
//			),
//			gorabbitmq.WithNormalLetterNormalQueueBindOptions(
//				gorabbitmq.WithQueueBindNoWait(cfg.NormalQueueBindOption.NoWait),
//				gorabbitmq.WithQueueBindArgs(nil),
//			)),
//	}
//}
//
//// Start 启动消费者
//func (bc *BaseConsumer) Start(ctx context.Context, queueConfig config.DoingOrder, queueType string) error {
//	var err error
//	bc.startOnce.Do(func() {
//		// 这里的逻辑只会被执行一次
//		// 如果内部有需要返回的 error，需要定义在外部变量
//		err = bc.doStart(ctx, queueConfig, queueType)
//	})
//	return err
//
//}
//
//// 提取实际启动逻辑
//func (bc *BaseConsumer) doStart(ctx context.Context, queueConfig config.DoingOrder, queueType string) error {
//	go func() {
//		cfg := config.Get().Rabbitmq
//		mqObject := database.GetMainRabbitMQ()
//		if cfg.Enable && queueConfig.Enable {
//			logger.Info("Starting " + bc.name)
//			exchangeName := queueConfig.ExchangeName
//			deadQueueName := queueConfig.DeadQueueName
//			deadRoutingKey := queueConfig.DeadKey
//			normalQueueName := queueConfig.NormalQueueName
//			normalRoutineKey := queueConfig.NormalKey
//			exchange := gorabbitmq.NewDirectExchange(exchangeName, normalRoutineKey)
//			// 获取需要启动的消费者数量
//			consumerNum := queueConfig.ConsumerNum
//			if consumerNum <= 0 {
//				consumerNum = 1
//			}
//			// 创建指定数量的消费者
//			for i := 0; i < consumerNum; i++ {
//				// 为每个消费者创建独立的函数作用域，确保每个消费者使用独立的连接
//				connection, err := mqObject.GetConnection(ctx)
//				if err != nil {
//					logger.Panic("database.GetRabbitMQ().GetConnection error", logger.Err(err))
//				}
//				// 构建基础选项
//				consumerOpts := bc.buildBaseOptions(queueConfig, i)
//
//				if queueType == "dead" {
//					consumerOpts = append(consumerOpts, bc.buildDeadLetterOptions(exchange, queueConfig, deadQueueName, deadRoutingKey, normalQueueName, normalRoutineKey)...)
//				}
//				if queueType == "normal" {
//					consumerOpts = append(consumerOpts, bc.buildNormalLetterOptions(exchange, queueConfig, normalQueueName)...)
//				}
//
//				consumer, err := gorabbitmq.NewConsumer(exchange, normalQueueName, connection, consumerOpts...)
//				if err != nil {
//					logger.Panic("异步消息队列 failed to create rabbitmq consumer error", logger.Err(err))
//				}
//				bc.consumersMutex.Lock()
//				bc.consumers = append(bc.consumers, consumer)
//				bc.consumersMutex.Unlock()
//
//				// 启动异步消费 (底层 consumer.go)
//				// 将上下文向下传递给具体的底层消费逻辑
//				consumer.Consume(ctx, bc.handleMessage)
//				logger.Info("队列 " + normalQueueName + " 消费者 " + strconv.Itoa(i+1) + " 已启动")
//			}
//			logger.Info("消息队列 " + queueConfig.NormalQueueName + " 总共启动了 " + strconv.Itoa(consumerNum) + " 个消费者")
//		}
//
//		//监听信号，无论是内部 Stop 还是外部 Context 取消
//		select {
//		case <-ctx.Done():
//			logger.Warn(bc.name + " 收到全局 Context 取消信号")
//		}
//
//		logger.Warn(bc.name + " 主监听协程已通过 Context 退出")
//	}()
//	return nil
//}
//
//// Stop 停止消费者
//func (bc *BaseConsumer) Stop() error {
//	logger.Warn(">>> 接收到停止指令: " + bc.name)
//	bc.consumersMutex.Lock()
//	for _, consumer := range bc.consumers {
//		if consumer != nil {
//			consumer.Close()
//			logger.Info("成功停止" + consumer.QueueName)
//		}
//	}
//	bc.consumersMutex.Unlock()
//	bc.wg.Wait()
//	logger.Warn("<<< 消费者服务已安全停止: " + bc.name)
//	return nil
//}
