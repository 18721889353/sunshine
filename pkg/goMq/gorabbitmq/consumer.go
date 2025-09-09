package gorabbitmq

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// ConsumerOption consumer option.
type ConsumerOption func(*consumerOptions)

type consumerOptions struct {
	exchangeDeclare *exchangeDeclareOptions
	queueDeclare    *queueDeclareOptions
	queueBind       *queueBindOptions
	qos             *qosOptions
	consume         *consumeOptions

	msgDurable bool // persistent or not
	isAutoAck  bool // auto-answer or not, if false, manual ACK required
}

func (o *consumerOptions) apply(opts ...ConsumerOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// default consumer settings
func defaultConsumerOptions() *consumerOptions {
	return &consumerOptions{
		exchangeDeclare: defaultExchangeDeclareOptions(),
		queueDeclare:    defaultQueueDeclareOptions(),
		queueBind:       defaultQueueBindOptions(),
		qos:             defaultQosOptions(),
		consume:         defaultConsumeOptions(),
		msgDurable:      true,
		isAutoAck:       true,
	}
}

// WithConsumerExchangeDeclareOptions set exchange declare option.
func WithConsumerExchangeDeclareOptions(opts ...ExchangeDeclareOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.exchangeDeclare.apply(opts...)
	}
}

// WithConsumerQueueDeclareOptions set queue declare option.
func WithConsumerQueueDeclareOptions(opts ...QueueDeclareOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.queueDeclare.apply(opts...)
	}
}

// WithConsumerQueueBindOptions set queue bind option.
func WithConsumerQueueBindOptions(opts ...QueueBindOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.queueBind.apply(opts...)
	}
}

// WithConsumerQosOptions set consume qos option.
func WithConsumerQosOptions(opts ...QosOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.qos.apply(opts...)
	}
}

// WithConsumerConsumeOptions set consumer consume option.
func WithConsumerConsumeOptions(opts ...ConsumeOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.consume.apply(opts...)
	}
}

// WithConsumerAutoAck set consumer auto ack option.
func WithConsumerAutoAck(enable bool) ConsumerOption {
	return func(o *consumerOptions) {
		o.isAutoAck = enable
	}
}

// WithConsumerMsgDurable set consumer persistent option.
func WithConsumerMsgDurable(enable bool) ConsumerOption {
	return func(o *consumerOptions) {
		o.msgDurable = enable
	}
}

// -------------------------------------------------------------------------------------------

// ConsumeOption consume option.
type ConsumeOption func(*consumeOptions)

type consumeOptions struct {
	consumer  string     // used to distinguish between multiple consumers
	exclusive bool       // only available to the program that created it
	noLocal   bool       // if set to true, a message sent by a producer in the same Connection cannot be passed to a consumer in this Connection.
	noWait    bool       // block processing
	args      amqp.Table // additional properties
}

func (o *consumeOptions) apply(opts ...ConsumeOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// default consume settings
func defaultConsumeOptions() *consumeOptions {
	return &consumeOptions{
		consumer:  "",
		exclusive: false,
		noLocal:   false,
		noWait:    false,
		args:      nil,
	}
}

// WithConsumeConsumer set consume consumer option.
func WithConsumeConsumer(consumer string) ConsumeOption {
	return func(o *consumeOptions) {
		o.consumer = consumer
	}
}

// WithConsumeExclusive set consume exclusive option.
func WithConsumeExclusive(enable bool) ConsumeOption {
	return func(o *consumeOptions) {
		o.exclusive = enable
	}
}

// WithConsumeNoLocal set consume noLocal option.
func WithConsumeNoLocal(enable bool) ConsumeOption {
	return func(o *consumeOptions) {
		o.noLocal = enable
	}
}

// WithConsumeNoWait set consume no wait option.
func WithConsumeNoWait(enable bool) ConsumeOption {
	return func(o *consumeOptions) {
		o.noWait = enable
	}
}

// WithConsumeArgs set consume args option.
func WithConsumeArgs(args map[string]interface{}) ConsumeOption {
	return func(o *consumeOptions) {
		o.args = args
	}
}

// -------------------------------------------------------------------------------------------

// QosOption QoS选项类型
// 用于配置RabbitMQ消费者的QoS（服务质量）参数
type QosOption func(*qosOptions)

// qosOptions QoS配置选项结构体
// 包含所有与QoS相关的配置参数
type qosOptions struct {
	enable        bool // 是否启用QoS功能
	prefetchCount int  // 预取消息数量，0表示无限制
	prefetchSize  int  // 预取消息大小，0表示无限制
	global        bool // 是否全局生效（对整个通道生效，而不仅仅是当前消费者）
}

func (o *qosOptions) apply(opts ...QosOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultQosOptions 默认QoS设置
// 返回包含默认QoS配置的选项结构体
func defaultQosOptions() *qosOptions {
	return &qosOptions{
		enable:        false,
		prefetchCount: 0,
		prefetchSize:  0,
		global:        false,
	}
}

// WithQosEnable 设置启用QoS功能选项
// 用于开启消费者的QoS（服务质量）控制，配合其他QoS选项使用可以限制消费者预取消息的数量，
// 实现流量控制、负载均衡和资源管理，防止消费者被大量消息淹没
func WithQosEnable() QosOption {
	return func(o *qosOptions) {
		o.enable = true
	}
}

// WithQosPrefetchCount 设置QoS预取消息数量选项
// 控制消费者在任意时刻可以预取并处理的最大消息数量
// prefetchCount > 0 时启用，值为0表示无限制
// 通过限制预取消息数量可以实现消费者间的负载均衡
func WithQosPrefetchCount(count int) QosOption {
	return func(o *qosOptions) {
		o.prefetchCount = count
	}
}

// WithQosPrefetchSize 设置QoS预取消息大小选项
// 控制消费者在任意时刻可以预取消息的总大小（以字节为单位）
// prefetchSize > 0 时启用，值为0表示无限制
// 注意：该参数在RabbitMQ中很少使用，通常设置为0
func WithQosPrefetchSize(size int) QosOption {
	return func(o *qosOptions) {
		o.prefetchSize = size
	}
}

// WithQosPrefetchGlobal 设置QoS全局生效选项
// 控制QoS设置是否应用于整个通道（true）还是仅应用于当前消费者（false）
// 当设置为true时，QoS设置将应用于该通道上的所有消费者
func WithQosPrefetchGlobal(enable bool) QosOption {
	return func(o *qosOptions) {
		o.global = enable
	}
}

// -------------------------------------------------------------------------------------------

// Consumer session
type Consumer struct {
	zapLog     *zap.Logger // 日志记录器
	Exchange   *Exchange
	QueueName  string
	connection *Connection
	ch         *amqp.Channel

	exchangeDeclareOption *exchangeDeclareOptions
	queueDeclareOption    *queueDeclareOptions
	queueBindOption       *queueBindOptions
	qosOption             *qosOptions
	consumeOption         *consumeOptions

	msgDurable bool  // persistent or not
	isAutoAck  bool  // auto ack or not
	count      int64 // consumer success message number
	mu         sync.Mutex
}

// Handler message
type Handler func(ctx context.Context, data []byte, tagID string) error

//type Handler func(ctx context.Context, d *amqp.Delivery, isAutoAck bool) error

// NewConsumer create a consumer
func NewConsumer(exchange *Exchange, queueName string, connection *Connection, opts ...ConsumerOption) (*Consumer, error) {
	o := defaultConsumerOptions()
	o.apply(opts...)
	c := &Consumer{
		zapLog:     connection.zapLog,
		Exchange:   exchange,
		QueueName:  queueName,
		connection: connection,

		exchangeDeclareOption: o.exchangeDeclare,
		queueDeclareOption:    o.queueDeclare,
		queueBindOption:       o.queueBind,
		qosOption:             o.qos,
		consumeOption:         o.consume,

		msgDurable: o.msgDurable,
		isAutoAck:  o.isAutoAck,
	}

	return c, nil
}

// initialize a consumer session
func (c *Consumer) initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connection.mutex.Lock()
	// crate a new channel
	ch, err := c.connection.conn.Channel()
	if err != nil {
		c.connection.mutex.Unlock()
		return err
	}
	c.ch = ch
	c.connection.mutex.Unlock()

	// declare the exchange type
	// 声明交换机
	err = ch.ExchangeDeclare(
		c.Exchange.name,                    // 交换机名称
		c.Exchange.eType,                   // 交换机类型
		c.msgDurable,                       // 是否持久化
		c.exchangeDeclareOption.autoDelete, // 是否自动删除
		c.exchangeDeclareOption.internal,   // 是否内部交换机
		c.exchangeDeclareOption.noWait,     // 是否等待服务器响应
		c.exchangeDeclareOption.args,       // 额外的参数
	)
	if err != nil {
		_ = ch.Close()
		return err
	}

	// declare a queue and create it automatically if it doesn't exist, or skip creation if it does.
	// 声明队列
	queue, err := ch.QueueDeclare(
		c.QueueName,                     // 队列名称
		c.msgDurable,                    // 是否持久化
		c.queueDeclareOption.autoDelete, // 是否自动删除
		c.queueDeclareOption.exclusive,  // 是否是独占的
		c.queueDeclareOption.noWait,     // 是否等待服务器响应
		c.queueDeclareOption.args,       // 额外的参数
	)
	if err != nil {
		_ = ch.Close()
		return err
	}

	args := c.queueBindOption.args
	if c.Exchange.eType == exchangeTypeHeaders {
		args = c.Exchange.headersKeys
	}
	// binding queue and exchange
	err = ch.QueueBind(
		queue.Name,
		c.Exchange.routingKey,
		c.Exchange.name,
		c.queueBindOption.noWait,
		args,
	)
	if err != nil {
		_ = ch.Close()
		return err
	}

	// setting the prefetch value, set channel.Qos on the consumer side to limit the number of messages consumed at a time,
	// balancing message throughput and fairness, and prevent consumers from being hit by sudden bursts of information traffic.
	if c.qosOption.enable {
		err = ch.Qos(c.qosOption.prefetchCount, c.qosOption.prefetchSize, c.qosOption.global)
		if err != nil {
			_ = ch.Close()
			return err
		}
	}

	fields := logFields(c.Exchange, nil)
	fields = append(fields, zap.String("autoAck", strconv.FormatBool(c.isAutoAck)))
	c.zapLog.Info("[rabbitmq consumer] initialized", fields...)
	return nil
}

func (c *Consumer) consumeWithContext(ctx context.Context) (<-chan amqp.Delivery, error) {
	return c.ch.ConsumeWithContext(
		ctx,
		c.QueueName,
		c.consumeOption.consumer,
		c.isAutoAck,
		c.consumeOption.exclusive,
		c.consumeOption.noLocal,
		c.consumeOption.noWait,
		c.consumeOption.args,
	)
}

// Consume messages for loop in goroutine
func (c *Consumer) Consume(ctx context.Context, handler Handler) {
	go func() {
		ticker := time.NewTicker(time.Second * 2)
		isFirst := true
		for {
			if isFirst {
				isFirst = false
				ticker.Reset(time.Millisecond * 10)
			} else {
				ticker.Reset(time.Second * 2)
			}

			// check connection for loop
			select {
			case <-ticker.C:
				if !c.connection.CheckConnected(ctx) {
					continue
				}
			case <-c.connection.exit:
				c.Close()
				return
			}
			ticker.Stop()

			err := c.initialize()
			if err != nil {
				c.zapLog.Warn("[rabbitmq consumer] initialize consumer error", zap.String("err", err.Error()), zap.String("queue", c.QueueName))
				continue
			}

			delivery, err := c.consumeWithContext(ctx)
			if err != nil {
				c.zapLog.Warn("[rabbitmq consumer] execution of consumption error", zap.String("err", err.Error()), zap.String("queue", c.QueueName))
				continue
			}
			//c.zapLog.Info("[rabbitmq consumer] queue is ready and waiting for messages, queue=" + c.QueueName)
			tracer := otel.Tracer("rabbitmq-Consume")

			isContinueConsume := false
			for {
				select {
				case <-c.connection.exit:
					c.Close()
					return
				case d, ok := <-delivery:
					if !ok {
						c.zapLog.Warn("[rabbitmq consumer] exit consume message, queue=" + c.QueueName)
						isContinueConsume = true
						break
					}
					// 开始一个新的 span
					ctx, span := tracer.Start(ctx, "consume message")
					span.SetAttributes(attribute.String("message.body", string(d.Body)))

					tagID := strings.Join([]string{d.Exchange, c.QueueName, strconv.FormatUint(d.DeliveryTag, 10)}, "/")
					err = handler(ctx, d.Body, tagID)
					if err != nil {
						span.RecordError(err)
						c.zapLog.Warn("[rabbitmq consumer] handle message error", zap.String("err", err.Error()), zap.String("tagID", tagID))
						//如果设置为 true，则将消息重新排队，以便稍后再次尝试处理。
						//如果设置为 false，则将消息从队列中移除，不再重新排队
						if err = d.Reject(false); err != nil {
							span.RecordError(err)
							c.zapLog.Warn("[rabbitmq consumer] manual Reject error", zap.String("err", err.Error()), zap.String("tagID", tagID))
							continue
						}
						//c.zapLog.Info("[rabbitmq consumer] manual Reject done", zap.String("tagID", tagID))

						continue
					}
					if !c.isAutoAck {
						if err = d.Ack(false); err != nil {
							span.RecordError(err)
							c.zapLog.Warn("[rabbitmq consumer] manual ack error", zap.String("err", err.Error()), zap.String("tagID", tagID))
							continue
						}
						//c.zapLog.Info("[rabbitmq consumer] manual ack done", zap.String("tagID", tagID))
					}
					atomic.AddInt64(&c.count, 1)
					// 结束 span
					span.End()
				}

				if isContinueConsume {
					break
				}
			}
			c.Close()
		}
	}()
}

// Close consumer
func (c *Consumer) Close() {
	if c.ch != nil {
		_ = c.ch.Close()
	}
}

// Count consumer success message number
func (c *Consumer) Count() int64 {
	return atomic.LoadInt64(&c.count)
}
