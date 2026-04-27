// Package gorabbitmqconsumer 提供 RabbitMQ 消费者的基础实现。
package gorabbitmqconsumer

import (
	"context"
	"strconv"
	"sync"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/jinzhu/copier"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

// MessageHandler 定义消息处理函数类型
type MessageHandler func(ctx context.Context, data []byte, messageID string, tagID string) error

// BaseConsumer 提供RabbitMQ消费者的基础实现（通用包版）
type BaseConsumer struct {
	name           string
	wg             sync.WaitGroup
	handler        MessageHandler
	consumers      []*gorabbitmq.Consumer
	consumersMutex sync.RWMutex
}

// ConsumerOption 定义初始化选项
type ConsumerOption func(*BaseConsumer)

// NewBaseConsumer 创建一个新的基础消费者
func NewBaseConsumer(name string, handler MessageHandler, opts ...ConsumerOption) *BaseConsumer {
	bc := &BaseConsumer{
		name:    name,
		handler: handler,
	}
	for _, opt := range opts {
		opt(bc)
	}
	return bc
}

// Name 返回消费者名称
func (bc *BaseConsumer) Name() string {
	return bc.name
}

// handleMessage 内部消息处理函数，适配gorabbitmq的Handler类型
func (bc *BaseConsumer) handleMessage(ctx context.Context, data []byte, messageID string, tagID string) error {
	bc.wg.Add(1)
	defer bc.wg.Done()
	return bc.handler(ctx, data, messageID, tagID)
}

// Start 启动消费者
// connection: 外部注入的连接
// rawConfig: 客户端传入的 config.DoingOrder 实例
func (bc *BaseConsumer) Start(ctx context.Context, connection *gorabbitmq.Connection, rawConfig any) error {
	var queueConfig config.DoingOrder
	if err := copier.Copy(&queueConfig, rawConfig); err != nil {
		logger.ErrorWithCtx(ctx, bc.name+" config copy error", logger.Err(err))
		return err
	}
	if !queueConfig.Enable {
		return nil
	}

	go func() {
		// 防止 goroutine panic 导致整个服务崩溃
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorWithCtx(ctx, bc.name+" consumer goroutine panicked",
					logger.Any("panic", r),
					logger.String("stack", "")) // Stack trace 会在 panic 时自动记录
			}
		}()

		logger.InfoWithCtx(ctx, "Starting "+bc.name)
		exchangeName := queueConfig.ExchangeName
		deadQueueName := queueConfig.DeadQueueName
		deadRoutingKey := queueConfig.DeadKey
		normalQueueName := queueConfig.NormalQueueName
		normalRoutineKey := queueConfig.NormalKey
		exchange := gorabbitmq.NewDirectExchange(exchangeName, normalRoutineKey)
		// 获取需要启动的消费者数量
		consumerNum := queueConfig.ConsumerNum
		if consumerNum <= 0 {
			consumerNum = 1
		}
		// 创建指定数量的消费者
		for i := 0; i < consumerNum; i++ {
			// 构建基础选项
			consumerOpts := bc.buildBaseOptions(queueConfig, i)

			if queueConfig.QueueType == "dead" {
				consumerOpts = append(consumerOpts, bc.buildDeadLetterOptions(exchange, queueConfig, deadQueueName, deadRoutingKey, normalQueueName, normalRoutineKey)...)
			}
			if queueConfig.QueueType == "normal" {
				consumerOpts = append(consumerOpts, bc.buildNormalLetterOptions(exchange, queueConfig, normalQueueName)...)
			}

			// 添加消费者名称用于 trace span
			consumerOpts = append(consumerOpts, gorabbitmq.WithConsumerName(bc.name))

			consumer, err := gorabbitmq.NewConsumer(exchange, normalQueueName, connection, consumerOpts...)
			if err != nil {
				logger.PanicWithCtx(ctx, "异步消息队列 failed to create rabbitmq consumer error", logger.Err(err))
			}
			bc.consumersMutex.Lock()
			bc.consumers = append(bc.consumers, consumer)
			bc.consumersMutex.Unlock()

			// 启动异步消费 (底层 consumer.go)
			// 将上下文向下传递给具体的底层消费逻辑
			consumer.Consume(ctx, bc.handleMessage)
			logger.InfoWithCtx(ctx, "队列 "+normalQueueName+" 消费者 "+strconv.Itoa(i+1)+" 已启动")
		}

		// 监听信号，无论是内部 Stop 还是外部 Context 取消
		<-ctx.Done()
		logger.WarnWithCtx(ctx, bc.name+" 收到全局 Context 取消信号")

		logger.WarnWithCtx(ctx, bc.name+" 收到 Context 取消信号，主循环退出")
	}()

	return nil
}

// buildBaseOptions 构建通用的消费者配置
func (bc *BaseConsumer) buildBaseOptions(cfg config.DoingOrder, index int) []gorabbitmq.ConsumerOption {
	name := cfg.ConsumerOption.Consumer
	if name == "" {
		name = "consumer"
	}
	name += strconv.Itoa(index + 1)

	return []gorabbitmq.ConsumerOption{
		// 为每个消费者构建独立的选项
		gorabbitmq.WithConsumerQosOptions(
			gorabbitmq.WithQosEnable(),
			gorabbitmq.WithQosPrefetchCount(cfg.ConsumerOption.PrefetchCount),
			gorabbitmq.WithQosPrefetchSize(cfg.ConsumerOption.PrefetchSize),
			gorabbitmq.WithQosPrefetchGlobal(cfg.ConsumerOption.Global),
		),
		gorabbitmq.WithConsumerMsgDurable(cfg.ConsumerOption.MsgDurable),
		gorabbitmq.WithConsumerAutoAck(cfg.ConsumerOption.IsAutoAck),
		// 添加消费选项
		gorabbitmq.WithConsumerConsumeOptions(
			gorabbitmq.WithConsumeConsumer(name),
			gorabbitmq.WithConsumeExclusive(cfg.ConsumerOption.Exclusive),
			gorabbitmq.WithConsumeNoLocal(cfg.ConsumerOption.NoLocal),
			gorabbitmq.WithConsumeNoWait(cfg.ConsumerOption.NoWait),
			gorabbitmq.WithConsumeArgs(nil),
		),
	}
}

// buildDeadLetterOptions 封装死信队列选项
func (bc *BaseConsumer) buildDeadLetterOptions(
	exchange *gorabbitmq.Exchange,
	cfg config.DoingOrder,
	deadQueueName, deadRoutingKey, normalQueueName, normalRoutineKey string,
) []gorabbitmq.ConsumerOption {
	return []gorabbitmq.ConsumerOption{
		gorabbitmq.WithConsumerDeadLetterOptions(
			gorabbitmq.WithDeadLetter(exchange.Name(), deadQueueName, deadRoutingKey, normalQueueName, exchange.RoutingKey()),
			gorabbitmq.WithDeadLetterExchangeDeclareOptions(
				gorabbitmq.WithExchangeDeclareDurable(cfg.ExchangeDeclareOptions.Durable),
				gorabbitmq.WithExchangeDeclareAutoDelete(cfg.ExchangeDeclareOptions.AutoDelete),
				gorabbitmq.WithExchangeDeclareInternal(cfg.ExchangeDeclareOptions.Internal),
				gorabbitmq.WithExchangeDeclareNoWait(cfg.ExchangeDeclareOptions.NoWait),
				gorabbitmq.WithExchangeDeclareArgs(nil),
			),
			gorabbitmq.WithDeadLetterDeadQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(cfg.DeadQueueDeclareOption.Durable),
				gorabbitmq.WithQueueDeclareExclusive(cfg.DeadQueueDeclareOption.Exclusive),
				gorabbitmq.WithQueueDeclareAutoDelete(cfg.DeadQueueDeclareOption.AutoDelete),
				gorabbitmq.WithQueueDeclareNoWait(cfg.DeadQueueDeclareOption.NoWait),
				gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
					"x-dead-letter-exchange":    exchange.Name(),
					"x-dead-letter-routing-key": normalRoutineKey,
					"x-message-ttl":             cfg.DeadQueueDeclareOption.Args.XMessageTTL,
				})),
			gorabbitmq.WithDeadLetterDeadQueueBindOptions(
				gorabbitmq.WithQueueBindNoWait(cfg.DeadQueueBindOption.NoWait),
				gorabbitmq.WithQueueBindArgs(nil),
			),
			gorabbitmq.WithDeadLetterNormalQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(cfg.NormalQueueDeclareOption.Durable),
				gorabbitmq.WithQueueDeclareExclusive(cfg.NormalQueueDeclareOption.Exclusive),
				gorabbitmq.WithQueueDeclareAutoDelete(cfg.NormalQueueDeclareOption.AutoDelete),
				gorabbitmq.WithQueueDeclareNoWait(cfg.NormalQueueDeclareOption.NoWait),
				gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
					"x-dead-letter-exchange":    exchange.Name(),
					"x-dead-letter-routing-key": deadRoutingKey,
				})),
			gorabbitmq.WithDeadLetterNormalQueueBindOptions(
				gorabbitmq.WithQueueBindNoWait(cfg.NormalQueueBindOption.NoWait),
				gorabbitmq.WithQueueBindArgs(nil),
			),
		),
	}
}

// buildNormalLetterOptions 封装普通队列选项
func (bc *BaseConsumer) buildNormalLetterOptions(exchange *gorabbitmq.Exchange, cfg config.DoingOrder, normalQueueName string) []gorabbitmq.ConsumerOption {
	return []gorabbitmq.ConsumerOption{
		gorabbitmq.WithConsumerNormalLetterOptions(
			gorabbitmq.WithNormalLetter(exchange.Name(), normalQueueName, exchange.RoutingKey()),
			gorabbitmq.WithNormalLetterExchangeDeclareOptions(
				gorabbitmq.WithExchangeDeclareDurable(cfg.ExchangeDeclareOptions.Durable),
				gorabbitmq.WithExchangeDeclareAutoDelete(cfg.ExchangeDeclareOptions.AutoDelete),
				gorabbitmq.WithExchangeDeclareInternal(cfg.ExchangeDeclareOptions.Internal),
				gorabbitmq.WithExchangeDeclareNoWait(cfg.ExchangeDeclareOptions.NoWait),
				gorabbitmq.WithExchangeDeclareArgs(nil),
			),
			gorabbitmq.WithNormalLetterNormalQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(cfg.NormalQueueDeclareOption.Durable),
				gorabbitmq.WithQueueDeclareExclusive(cfg.NormalQueueDeclareOption.Exclusive),
				gorabbitmq.WithQueueDeclareAutoDelete(cfg.NormalQueueDeclareOption.AutoDelete),
				gorabbitmq.WithQueueDeclareNoWait(cfg.NormalQueueDeclareOption.NoWait),
				gorabbitmq.WithQueueDeclareArgs(nil),
			),
			gorabbitmq.WithNormalLetterNormalQueueBindOptions(
				gorabbitmq.WithQueueBindNoWait(cfg.NormalQueueBindOption.NoWait),
				gorabbitmq.WithQueueBindArgs(nil),
			)),
	}
}

// Stop 优雅停止
func (bc *BaseConsumer) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.WarnWithCtx(ctx, ">>> 接收到停止指令: "+bc.name)
	bc.consumersMutex.Lock()
	for _, consumer := range bc.consumers {
		if consumer != nil {
			consumer.Close()
			logger.InfoWithCtx(ctx, "成功停止"+consumer.QueueName)
		}
	}
	bc.consumersMutex.Unlock()
	bc.wg.Wait()
	logger.WarnWithCtx(ctx, "<<< 消费者服务已安全停止: "+bc.name)
	return nil
}
