package gorabbitmq

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ConsumerOption 消费者选项配置函数类型
type ConsumerOption func(*consumerOptions)

// consumerOptions 消费者配置选项
type consumerOptions struct {
	logger             *zap.Logger                // 日志记录器
	customerDeadLetter *CustomerDeadLetterOptions // 自定义死信队列选项
	normalLetter       *NormalLetterOptions       // 正常队列选项
	deadLetter         *DeadLetterOptions         // 死信队列选项
	qos                *qosOptions                // QoS选项
	consume            *consumeOptions            // 消费选项

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
		logger:             defaultLogger,
		customerDeadLetter: defaultCustomerDeadLetterOptions(),
		normalLetter:       defaultNormalLetterOptions(),
		deadLetter:         defaultDeadLetterOptions(),
		qos:                defaultQosOptions(),
		consume:            defaultConsumeOptions(),
		msgDurable:         true,
		isAutoAck:          true,
	}
}

// WithConsumerCustomerDeadLetterOptions set dead letter options.
func WithConsumerCustomerDeadLetterOptions(opts ...CustomerDeadLetterOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.customerDeadLetter.apply(opts...)
	}
} // WithConsumerNormalLetterOptions set dead letter options.
func WithConsumerNormalLetterOptions(opts ...NormalLetterOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.normalLetter.apply(opts...)
	}
}

// WithConsumerDeadLetterOptions set dead letter options.
func WithConsumerDeadLetterOptions(opts ...DeadLetterOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.deadLetter.apply(opts...)
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
	exchange  *Exchange     // 交换机
	QueueName string        // 队列名称
	conn      *Connection   // 连接
	ch        *amqp.Channel // 通道

	customerDeadLetter *CustomerDeadLetterOptions // 自定义死信队列选项
	normalLetter       *NormalLetterOptions
	deadLetter         *DeadLetterOptions
	qosOption          *qosOptions     // QoS选项
	consumeOption      *consumeOptions // 消费选项

	msgDurable bool         // 消息是否持久化
	isAutoAck  bool         // 是否自动确认
	mu         sync.RWMutex // 读写锁，保护所有字段访问

	tracer trace.Tracer // OpenTelemetry tracer for reuse
}

// Handler 消息处理函数类型
type Handler func(ctx context.Context, data []byte, tagID string) error

// NewConsumer 创建一个消费者
func NewConsumer(exchange *Exchange, queueName string, conn *Connection, opts ...ConsumerOption) (*Consumer, error) {
	o := defaultConsumerOptions()
	o.apply(opts...)
	c := &Consumer{
		zapLog: conn.zapLog,

		exchange:  exchange,
		QueueName: queueName,
		conn:      conn,

		customerDeadLetter: o.customerDeadLetter,
		normalLetter:       o.normalLetter,
		deadLetter:         o.deadLetter,
		qosOption:          o.qos,
		consumeOption:      o.consume,

		msgDurable: o.msgDurable,
		isAutoAck:  o.isAutoAck,

		tracer: otel.Tracer("gorabbitmq"), // 初始化 tracer
	}

	return c, nil
}

// initialize 初始化消费者会话
func (c *Consumer) initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.conn.mutex.Lock()
	// 创建一个新的通道
	channel, err := c.conn.conn.Channel()
	if err != nil {
		c.conn.mutex.Unlock()
		return err
	}
	c.ch = channel
	c.conn.mutex.Unlock()

	if c.customerDeadLetter.exchangeName != "sunshine" && c.normalLetter.exchangeName != "sunshine" {
		return fmt.Errorf("cannot set both customerDeadLetter and normalLetter")
	}
	if c.customerDeadLetter.exchangeName != "sunshine" && c.deadLetter.exchangeName != "sunshine" {
		return fmt.Errorf("cannot set both customerDeadLetter and deadLetter")
	}
	if c.normalLetter.exchangeName != "sunshine" && c.deadLetter.exchangeName != "sunshine" {
		return fmt.Errorf("cannot set both normalLetter and deadLetter")
	}
	var fields []zap.Field
	//--------------------------------自定义死信队列队列----------------------------------------------------
	if c.customerDeadLetter.exchangeName != "sunshine" {
		fields = logFields(c.exchange, map[string]any{
			"msgDurable":                            c.msgDurable,
			"isAutoAck":                             c.isAutoAck,
			"queueName":                             c.QueueName,
			"customerDeadLetter.exchangeDeclare":    fmt.Sprintf("%+v", c.customerDeadLetter.exchangeDeclare),
			"customerDeadLetter.deadQueueDeclare":   fmt.Sprintf("%+v", c.customerDeadLetter.deadQueueDeclare),
			"customerDeadLetter.errQueueDeclare":    fmt.Sprintf("%+v", c.customerDeadLetter.errQueueDeclare),
			"customerDeadLetter.normalQueueDeclare": fmt.Sprintf("%+v", c.customerDeadLetter.normalQueueDeclare),
		})
		// 声明交换机
		err = channel.ExchangeDeclare(
			c.exchange.name,  //交换机名称
			c.exchange.eType, // 交换机类型  (direct, topic, fanout, headers)
			c.customerDeadLetter.exchangeDeclare.durable,    //是否持久化
			c.customerDeadLetter.exchangeDeclare.autoDelete, //是否自动删除
			c.customerDeadLetter.exchangeDeclare.internal,   //是否是内部交换机
			c.customerDeadLetter.exchangeDeclare.noWait,     //是否非阻塞
			c.customerDeadLetter.exchangeDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
		//  声明死信队列并设置异常策略
		if c.customerDeadLetter.deadQueueDeclare.args == nil {
			c.customerDeadLetter.deadQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    c.exchange.name,
				"x-dead-letter-routing-key": c.customerDeadLetter.errRoutingKey,
				"x-message-ttl":             int32(600000), // 600秒后过期
			}
		}
		// QueueDeclare 声明队列
		//exclusive 当设置为 true 时，队列变为排他队列（Exclusive Queue）
		//排他队列只能被当前连接（Connection）中的信道（Channel）访问
		//当连接关闭时，排他队列会自动删除
		//当 noWait = false（默认值）时：
		//客户端发送队列声明或交换机声明请求
		//客户端等待服务器返回确认响应
		//只有收到服务器确认后，方法才返回
		//如果操作失败，会返回错误
		//当 noWait = true 时：
		//客户端发送队列声明或交换机声明请求
		//客户端不等待服务器的确认响应，立即返回
		//无法知道操作是否成功执行
		//即使操作失败，也不会返回错误
		dlq, err := channel.QueueDeclare(
			c.customerDeadLetter.deadQueueName,               //队列名称
			c.customerDeadLetter.deadQueueDeclare.durable,    //是否持久化
			c.customerDeadLetter.deadQueueDeclare.autoDelete, //是否自动删除
			c.customerDeadLetter.deadQueueDeclare.exclusive,  //是否排他
			c.customerDeadLetter.deadQueueDeclare.noWait,     //是否非阻塞
			c.customerDeadLetter.deadQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			dlq.Name,                            //队列名称
			c.customerDeadLetter.deadRoutingKey, //路由键
			c.exchange.name,                     //交换机名称
			c.customerDeadLetter.deadQueueBind.noWait, //是否非阻塞
			c.customerDeadLetter.deadQueueBind.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}

		// 声明异常队列并设置死信策略
		if c.customerDeadLetter.errQueueDeclare.args == nil {
			c.customerDeadLetter.errQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    c.exchange.name,
				"x-dead-letter-routing-key": c.customerDeadLetter.deadRoutingKey,
			}
		}
		// QueueDeclare 声明队列
		elq, err := channel.QueueDeclare(
			c.customerDeadLetter.errQueueName,               //队列名称
			c.customerDeadLetter.errQueueDeclare.durable,    //是否持久化
			c.customerDeadLetter.errQueueDeclare.autoDelete, //是否自动删除
			c.customerDeadLetter.errQueueDeclare.exclusive,  //是否排他
			c.customerDeadLetter.errQueueDeclare.noWait,     //是否非阻塞
			c.customerDeadLetter.errQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			elq.Name,                                 //队列名称
			c.customerDeadLetter.errRoutingKey,       //路由键
			c.exchange.name,                          //交换机名称
			c.customerDeadLetter.errQueueBind.noWait, //是否非阻塞
			c.customerDeadLetter.errQueueBind.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
		// 声明普通队列并设置死信策略
		if c.customerDeadLetter.normalQueueDeclare.args == nil {
			c.customerDeadLetter.normalQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    c.exchange.name,
				"x-dead-letter-routing-key": c.customerDeadLetter.deadRoutingKey,
			}
		}
		// QueueDeclare 声明队列
		lq, err := channel.QueueDeclare(
			c.customerDeadLetter.normalQueueName,               //队列名称
			c.customerDeadLetter.normalQueueDeclare.durable,    //是否持久化
			c.customerDeadLetter.normalQueueDeclare.autoDelete, //是否自动删除
			c.customerDeadLetter.normalQueueDeclare.exclusive,  //是否排他
			c.customerDeadLetter.normalQueueDeclare.noWait,     //是否非阻塞
			c.customerDeadLetter.normalQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			lq.Name,                               //队列名称
			c.customerDeadLetter.normalRoutingKey, //路由键
			c.exchange.name,                       //交换机名称
			c.customerDeadLetter.normalQueueDeclare.noWait, //是否非阻塞
			c.customerDeadLetter.normalQueueDeclare.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
	}
	//--------------------------------死信队列队列----------------------------------------------------
	if c.deadLetter.exchangeName != "sunshine" {
		fields = logFields(c.exchange, map[string]any{
			"msgDurable":                            c.msgDurable,
			"isAutoAck":                             c.isAutoAck,
			"queueName":                             c.QueueName,
			"customerDeadLetter.exchangeDeclare":    fmt.Sprintf("%+v", c.deadLetter.exchangeDeclare),
			"customerDeadLetter.deadQueueDeclare":   fmt.Sprintf("%+v", c.deadLetter.deadQueueDeclare),
			"customerDeadLetter.normalQueueDeclare": fmt.Sprintf("%+v", c.deadLetter.normalQueueDeclare),
		})
		// 声明交换机
		err = channel.ExchangeDeclare(
			c.exchange.name,                         //交换机名称
			c.exchange.eType,                        // 交换机类型  (direct, topic, fanout, headers)
			c.deadLetter.exchangeDeclare.durable,    //是否持久化
			c.deadLetter.exchangeDeclare.autoDelete, //是否自动删除
			c.deadLetter.exchangeDeclare.internal,   //是否是内部交换机
			c.deadLetter.exchangeDeclare.noWait,     //是否非阻塞
			c.deadLetter.exchangeDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
		//  声明死信队列并设置异常策略
		if c.deadLetter.deadQueueDeclare.args == nil {
			c.deadLetter.deadQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    c.exchange.name,
				"x-dead-letter-routing-key": c.deadLetter.normalRoutingKey,
				"x-message-ttl":             int32(600000), // 600秒后过期
			}
		}
		// QueueDeclare 声明队列
		//exclusive 当设置为 true 时，队列变为排他队列（Exclusive Queue）
		//排他队列只能被当前连接（Connection）中的信道（Channel）访问
		//当连接关闭时，排他队列会自动删除
		//当 noWait = false（默认值）时：
		//客户端发送队列声明或交换机声明请求
		//客户端等待服务器返回确认响应
		//只有收到服务器确认后，方法才返回
		//如果操作失败，会返回错误
		//当 noWait = true 时：
		//客户端发送队列声明或交换机声明请求
		//客户端不等待服务器的确认响应，立即返回
		//无法知道操作是否成功执行
		//即使操作失败，也不会返回错误
		dlq, err := channel.QueueDeclare(
			c.deadLetter.deadQueueName,               //队列名称
			c.deadLetter.deadQueueDeclare.durable,    //是否持久化
			c.deadLetter.deadQueueDeclare.autoDelete, //是否自动删除
			c.deadLetter.deadQueueDeclare.exclusive,  //是否排他
			c.deadLetter.deadQueueDeclare.noWait,     //是否非阻塞
			c.deadLetter.deadQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			dlq.Name,                          //队列名称
			c.deadLetter.deadRoutingKey,       //路由键
			c.exchange.name,                   //交换机名称
			c.deadLetter.deadQueueBind.noWait, //是否非阻塞
			c.deadLetter.deadQueueBind.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}

		// 声明普通队列并设置死信策略
		if c.deadLetter.normalQueueDeclare.args == nil {
			c.deadLetter.normalQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    c.exchange.name,
				"x-dead-letter-routing-key": c.deadLetter.deadRoutingKey,
			}
		}
		// QueueDeclare 声明队列
		lq, err := channel.QueueDeclare(
			c.deadLetter.normalQueueName,               //队列名称
			c.deadLetter.normalQueueDeclare.durable,    //是否持久化
			c.deadLetter.normalQueueDeclare.autoDelete, //是否自动删除
			c.deadLetter.normalQueueDeclare.exclusive,  //是否排他
			c.deadLetter.normalQueueDeclare.noWait,     //是否非阻塞
			c.deadLetter.normalQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			lq.Name,                                //队列名称
			c.deadLetter.normalRoutingKey,          //路由键
			c.exchange.name,                        //交换机名称
			c.deadLetter.normalQueueDeclare.noWait, //是否非阻塞
			c.deadLetter.normalQueueDeclare.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
	}
	//--------------------------------正常队列----------------------------------------------------
	if c.normalLetter.exchangeName != "sunshine" {
		fields = logFields(c.exchange, map[string]any{
			"msgDurable":                      c.msgDurable,
			"isAutoAck":                       c.isAutoAck,
			"queueName":                       c.QueueName,
			"normalLetter.exchangeDeclare":    fmt.Sprintf("%+v", c.normalLetter.exchangeDeclare),
			"normalLetter.normalQueueDeclare": fmt.Sprintf("%+v", c.normalLetter.normalQueueDeclare),
		})
		// 声明交换机
		err = channel.ExchangeDeclare(
			c.exchange.name,                           //交换机名称
			c.exchange.eType,                          // 交换机类型  (direct, topic, fanout, headers)
			c.normalLetter.exchangeDeclare.durable,    //是否持久化
			c.normalLetter.exchangeDeclare.autoDelete, //是否自动删除
			c.normalLetter.exchangeDeclare.internal,   //是否是内部交换机
			c.normalLetter.exchangeDeclare.noWait,     //是否非阻塞
			c.normalLetter.exchangeDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}

		// QueueDeclare 声明队列
		nlq, err := channel.QueueDeclare(
			c.normalLetter.normalQueueName,               //队列名称
			c.normalLetter.normalQueueDeclare.durable,    //是否持久化
			c.normalLetter.normalQueueDeclare.autoDelete, //是否自动删除
			c.normalLetter.normalQueueDeclare.exclusive,  //是否排他
			c.normalLetter.normalQueueDeclare.noWait,     //是否非阻塞
			c.normalLetter.normalQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			nlq.Name,                                 //队列名称
			c.normalLetter.normalRoutingKey,          //路由键
			c.exchange.name,                          //交换机名称
			c.normalLetter.normalQueueDeclare.noWait, //是否非阻塞
			c.normalLetter.normalQueueDeclare.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
	}
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
					ctx, span := c.tracer.Start(ctx, "consume message")
					span.SetAttributes(attribute.String("message.body", string(d.Body)))

					tagID := strings.Join([]string{d.Exchange, c.QueueName, strconv.FormatUint(d.DeliveryTag, 10)}, "/")
					err = handler(ctx, d.Body, tagID)
					if err != nil {
						span.RecordError(err)
						c.zapLog.Warn("[rabbitmq consumer] handle message error", zap.String("err", err.Error()), zap.String("tagID", tagID))
						if !c.isAutoAck {
							//如果设置为 true，则将消息重新排队，以便稍后再次尝试处理。
							//如果设置为 false，则将消息从队列中移除，不再重新排队
							if err = d.Reject(false); err != nil {
								span.RecordError(err)
								c.zapLog.Warn("[rabbitmq consumer] manual Reject error", zap.String("err", err.Error()), zap.String("tagID", tagID))
							}
						}
						//c.zapLog.Info("[rabbitmq consumer] manual Reject done", zap.String("tagID", tagID))
						span.End()
						continue
					}
					if !c.isAutoAck {
						if err = d.Ack(false); err != nil {
							span.RecordError(err)
							c.zapLog.Warn("[rabbitmq consumer] manual ack error", zap.String("err", err.Error()), zap.String("tagID", tagID))
						}
						//c.zapLog.Info("[rabbitmq consumer] manual ack done", zap.String("tagID", tagID))
					}
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
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ch != nil {
		_ = c.ch.Close()
	}
}
