// Package gorabbitmqconsumer 提供 RabbitMQ 消费者的基础实现。
package gorabbitmqconsumer

import (
	"context"
	"runtime/debug"
	"strconv"
	"sync"

	"github.com/jinzhu/copier"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
)

type ctxKeyQueueType string

// QueueTypeKey 消息来源队列类型上下文键值。
// 在 handler 中通过 ctx.Value(gorabbitmqconsumer.QueueTypeKey) 获取：
//   - "normal"   → 正常队列（首次消费）
//   - "retry"    → 重试队列（死信TTL超时后重试）
//   - 空字符串   → 非 customerDead 模式（queueType=dead/normal 时无此值）
const QueueTypeKey ctxKeyQueueType = "queue_type"

// MessageHandler 定义消息处理函数类型
type MessageHandler func(ctx context.Context, data []byte, messageID string, tagID string) error

// BaseConsumer 提供RabbitMQ消费者的基础实现（通用包版）
type BaseConsumer struct {
	name           string
	wg             sync.WaitGroup
	handler        MessageHandler
	consumers      []*gorabbitmq.Consumer
	consumersMutex sync.RWMutex
	connRelease    func() // 释放连接的回调，池模式归还连接，单连接模式无操作
}

// ConsumerOption 定义初始化选项
type ConsumerOption func(*BaseConsumer)

// WithConnRelease 设置连接释放回调，用于在 Start 异常或消费者停止时将连接归还给连接池。
// 池模式使用示例：WithConnRelease(func() { client.PutConnection(context.Background(), conn) })
func WithConnRelease(release func()) ConsumerOption {
	return func(bc *BaseConsumer) {
		bc.connRelease = release
	}
}

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
//
// 注意:
//   - Start 成功后 goroutine 持有 connection 生命周期，goroutine 退出时会自动释放
//   - Start 异常时（配置错误/未启用）会立即释放 connection
//   - 如需将连接归还给连接池，使用 WithConnRelease 设置回调
func (bc *BaseConsumer) Start(ctx context.Context, connection *gorabbitmq.Connection, rawConfig any) error {
	var queueConfig config.DoingOrder
	if err := copier.Copy(&queueConfig, rawConfig); err != nil {
		logger.ErrorWithCtx(ctx, bc.name+" config copy error", logger.Err(err))
		if bc.connRelease != nil {
			bc.connRelease()
		}
		return err
	}
	if !queueConfig.Enable {
		if bc.connRelease != nil {
			bc.connRelease()
		}
		return nil
	}

	go func() {
		// panic 恢复 - 最外层安全网
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorWithCtx(ctx, bc.name+" consumer goroutine panicked",
					logger.Any("panic", r),
					logger.String("stack", string(debug.Stack())))
			}
		}()

		// 关闭连接
		defer connection.Close()

		// 归还连接到连接池
		defer func() {
			if bc.connRelease != nil {
				bc.connRelease()
			}
		}()

		logger.InfoWithCtx(ctx, "Starting "+bc.name)
		exchangeName := queueConfig.ExchangeName
		deadQueueName := queueConfig.DeadQueueName
		deadRoutingKey := queueConfig.DeadKey
		errQueueName := queueConfig.ErrQueueName
		errRoutingKey := queueConfig.ErrKey
		normalQueueName := queueConfig.NormalQueueName
		normalRoutineKey := queueConfig.NormalKey
		exchange := gorabbitmq.NewDirectExchange(exchangeName, normalRoutineKey)
		// 获取需要启动的消费者数量
		consumerNum := queueConfig.ConsumerNum
		if consumerNum <= 0 {
			consumerNum = 1
		}
		if consumerNum > 100 {
			consumerNum = 100
			logger.WarnWithCtx(ctx, bc.name+" consumerNum capped at 100")
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

			if queueConfig.QueueType == "customerDead" {
				consumerOpts = append(consumerOpts, bc.buildCustomerDeadLetterOptions(exchange, queueConfig, deadQueueName, deadRoutingKey, errQueueName, errRoutingKey, normalQueueName)...)
			}

			// 添加消费者名称
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
			consumer.Consume(context.WithValue(ctx, QueueTypeKey, "normal"), bc.handleMessage)
			logger.InfoWithCtx(ctx, "队列 "+normalQueueName+" 消费者 "+strconv.Itoa(i+1)+" 已启动")

			// 自定义死信模式：额外创建重试队列消费者
			if queueConfig.QueueType == "customerDead" {
				errConsumerOpts := bc.buildCustomerDeadLetterOptions(exchange, queueConfig, deadQueueName, deadRoutingKey, errQueueName, errRoutingKey, normalQueueName)
				errConsumerOpts = append(errConsumerOpts, gorabbitmq.WithConsumerName(bc.name))

				errConsumer, err := gorabbitmq.NewConsumer(exchange, errQueueName, connection, errConsumerOpts...)
				if err != nil {
					logger.PanicWithCtx(ctx, "异步消息队列 failed to create rabbitmq err consumer error", logger.Err(err))
				}
				bc.consumersMutex.Lock()
				bc.consumers = append(bc.consumers, errConsumer)
				bc.consumersMutex.Unlock()

				errConsumer.Consume(context.WithValue(ctx, QueueTypeKey, "retry"), bc.handleMessage)
				logger.InfoWithCtx(ctx, "重试队列 "+errQueueName+" 消费者 "+strconv.Itoa(i+1)+" 已启动")
			}
		}

		// 监听 Context 取消信号
		<-ctx.Done()
		logger.WarnWithCtx(ctx, bc.name+" 收到 Context 取消信号，主循环退出")
	}()

	return nil
}

// SetConnRelease 设置连接释放回调，用于在 Start 后才获取连接的场景（如 init() 创建、Start() 取连接的模式）。
// 池模式使用示例：baseConsumer.SetConnRelease(func() { client.PutConnection(context.Background(), conn) })
func (bc *BaseConsumer) SetConnRelease(release func()) {
	bc.connRelease = release
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

// buildCustomerDeadLetterOptions 封装自定义死信队列选项
func (bc *BaseConsumer) buildCustomerDeadLetterOptions(
	exchange *gorabbitmq.Exchange,
	cfg config.DoingOrder,
	deadQueueName, deadRoutingKey, errQueueName, errRoutingKey, normalQueueName string,
) []gorabbitmq.ConsumerOption {
	return []gorabbitmq.ConsumerOption{
		gorabbitmq.WithConsumerCustomerDeadLetterOptions(
			gorabbitmq.WithCustomerDeadLetter(exchange.Name(), deadQueueName, deadRoutingKey, errQueueName, errRoutingKey, normalQueueName, exchange.RoutingKey()),
			gorabbitmq.WithCustomerDeadLetterExchangeDeclareOptions(
				gorabbitmq.WithExchangeDeclareDurable(cfg.ExchangeDeclareOptions.Durable),
				gorabbitmq.WithExchangeDeclareAutoDelete(cfg.ExchangeDeclareOptions.AutoDelete),
				gorabbitmq.WithExchangeDeclareInternal(cfg.ExchangeDeclareOptions.Internal),
				gorabbitmq.WithExchangeDeclareNoWait(cfg.ExchangeDeclareOptions.NoWait),
				gorabbitmq.WithExchangeDeclareArgs(nil),
			),
			gorabbitmq.WithCustomerDeadLetterDeadQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(cfg.DeadQueueDeclareOption.Durable),
				gorabbitmq.WithQueueDeclareExclusive(cfg.DeadQueueDeclareOption.Exclusive),
				gorabbitmq.WithQueueDeclareAutoDelete(cfg.DeadQueueDeclareOption.AutoDelete),
				gorabbitmq.WithQueueDeclareNoWait(cfg.DeadQueueDeclareOption.NoWait),
				gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
					"x-dead-letter-exchange":    exchange.Name(),
					"x-dead-letter-routing-key": errRoutingKey,
					"x-message-ttl":             cfg.DeadQueueDeclareOption.Args.XMessageTTL,
				})),
			gorabbitmq.WithCustomerDeadLetterDeadQueueBindOptions(
				gorabbitmq.WithQueueBindNoWait(cfg.DeadQueueBindOption.NoWait),
				gorabbitmq.WithQueueBindArgs(nil),
			),
			gorabbitmq.WithCustomerDeadLetterErrQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(cfg.ErrQueueDeclareOption.Durable),
				gorabbitmq.WithQueueDeclareExclusive(cfg.ErrQueueDeclareOption.Exclusive),
				gorabbitmq.WithQueueDeclareAutoDelete(cfg.ErrQueueDeclareOption.AutoDelete),
				gorabbitmq.WithQueueDeclareNoWait(cfg.ErrQueueDeclareOption.NoWait),
				gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
					"x-dead-letter-exchange":    exchange.Name(),
					"x-dead-letter-routing-key": deadRoutingKey,
				})),
			gorabbitmq.WithCustomerDeadLetterErrQueueBindOptions(
				gorabbitmq.WithQueueBindNoWait(cfg.ErrQueueBindOption.NoWait),
				gorabbitmq.WithQueueBindArgs(nil),
			),
			gorabbitmq.WithCustomerDeadLetterNormalQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(cfg.NormalQueueDeclareOption.Durable),
				gorabbitmq.WithQueueDeclareExclusive(cfg.NormalQueueDeclareOption.Exclusive),
				gorabbitmq.WithQueueDeclareAutoDelete(cfg.NormalQueueDeclareOption.AutoDelete),
				gorabbitmq.WithQueueDeclareNoWait(cfg.NormalQueueDeclareOption.NoWait),
				gorabbitmq.WithQueueDeclareArgs(map[string]interface{}{
					"x-dead-letter-exchange":    exchange.Name(),
					"x-dead-letter-routing-key": deadRoutingKey,
				})),
			gorabbitmq.WithCustomerDeadLetterNormalQueueBindOptions(
				gorabbitmq.WithQueueBindNoWait(cfg.NormalQueueBindOption.NoWait),
				gorabbitmq.WithQueueBindArgs(nil),
			),
		),
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
	logger.InfoWithCtx(ctx, "<<< 消费者服务已安全停止: "+bc.name)
	return nil
}
