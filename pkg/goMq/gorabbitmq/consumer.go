package gorabbitmq

import (
	"context"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ConsumerOption 消费者选项配置函数类型
type ConsumerOption func(*consumerOptions)

// consumerOptions 消费者配置选项
type consumerOptions struct {
	exchangeDeclare *exchangeDeclareOptions // 交换机声明选项
	queueDeclare    *queueDeclareOptions    // 队列声明选项
	queueBind       *queueBindOptions       // 队列绑定选项
	qos             *qosOptions             // QoS选项
	consume         *consumeOptions         // 消费选项

	msgDurable bool // 消息是否持久化
	isAutoAck  bool // 是否自动确认消息
}

// apply 应用消费者选项
func (o *consumerOptions) apply(opts ...ConsumerOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultConsumerOptions 默认消费者设置
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

// WithConsumerExchangeDeclareOptions 设置交换机声明选项
func WithConsumerExchangeDeclareOptions(opts ...ExchangeDeclareOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.exchangeDeclare.apply(opts...)
	}
}

// WithConsumerQueueDeclareOptions 设置队列声明选项
func WithConsumerQueueDeclareOptions(opts ...QueueDeclareOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.queueDeclare.apply(opts...)
	}
}

// WithConsumerQueueBindOptions 设置队列绑定选项
func WithConsumerQueueBindOptions(opts ...QueueBindOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.queueBind.apply(opts...)
	}
}

// WithConsumerQosOptions 设置消费QoS选项
func WithConsumerQosOptions(opts ...QosOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.qos.apply(opts...)
	}
}

// WithConsumerConsumeOptions 设置消费者消费选项
func WithConsumerConsumeOptions(opts ...ConsumeOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.consume.apply(opts...)
	}
}

// WithConsumerAutoAck 设置消费者自动确认选项
func WithConsumerAutoAck(enable bool) ConsumerOption {
	return func(o *consumerOptions) {
		o.isAutoAck = enable
	}
}

// WithConsumerMsgDurable 设置消费者消息持久化选项
func WithConsumerMsgDurable(enable bool) ConsumerOption {
	return func(o *consumerOptions) {
		o.msgDurable = enable
	}
}

// -------------------------------------------------------------------------------------------

// Consumer 消费者会话
type Consumer struct {
	zapLog    *zap.Logger   // 日志记录器
	Exchange  *Exchange     // 交换机
	QueueName string        // 队列名称
	conn      *Connection   // 连接
	ch        *amqp.Channel // 通道

	exchangeDeclareOption *exchangeDeclareOptions // 交换机声明选项
	queueDeclareOption    *queueDeclareOptions    // 队列声明选项
	queueBindOption       *queueBindOptions       // 队列绑定选项
	qosOption             *qosOptions             // QoS选项
	consumeOption         *consumeOptions         // 消费选项

	msgDurable bool       // 消息是否持久化
	isAutoAck  bool       // 是否自动确认
	count      int64      // 消费成功的消息数量
	mu         sync.Mutex // 互斥锁
}

// Handler 消息处理函数类型
type Handler func(ctx context.Context, data []byte, tagID string) error

// NewConsumer 创建一个消费者
func NewConsumer(exchange *Exchange, queueName string, conn *Connection, opts ...ConsumerOption) (*Consumer, error) {
	o := defaultConsumerOptions()
	o.apply(opts...)
	c := &Consumer{
		zapLog:    conn.zapLog,
		Exchange:  exchange,
		QueueName: queueName,
		conn:      conn,

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

// initialize 初始化消费者会话
func (c *Consumer) initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.mutex.Lock()
	// 创建一个新的通道
	ch, err := c.conn.conn.Channel()
	if err != nil {
		c.conn.mutex.Unlock()
		return err
	}
	c.ch = ch
	c.conn.mutex.Unlock()

	// 声明交换机类型
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

	// 声明队列，如果不存在则自动创建，如果存在则跳过创建
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
	// 绑定队列和交换机
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

	// 设置预取值，在消费者端设置channel.Qos来限制一次消费的消息数量，
	// 平衡消息吞吐量和公平性，防止消费者被突发的大量信息流量冲击
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

// consumeWithContext 带上下文的消费消息
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

// Consume 在goroutine中循环消费消息
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

			// 循环检查连接
			select {
			case <-ticker.C:
				if !c.conn.CheckConnected(ctx) {
					continue
				}
			case <-c.conn.exit:
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
				case <-c.conn.exit:
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

// Close 关闭消费者
func (c *Consumer) Close() {
	if c.ch != nil {
		_ = c.ch.Close()
	}
}

// Count 获取消费成功的消息数量
func (c *Consumer) Count() int64 {
	return atomic.LoadInt64(&c.count)
}
