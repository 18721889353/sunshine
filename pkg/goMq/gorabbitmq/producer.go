package gorabbitmq

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// producerOptions 生产者配置选项
type producerOptions struct {
	logger             *zap.Logger
	customerDeadLetter *CustomerDeadLetterOptions
	durable            bool // is it persistent
	mandatory          bool
}

// ProducerOption 生产者配置选项函数类型
type ProducerOption func(*producerOptions)

// apply 应用生产者配置选项
func (o *producerOptions) apply(opts ...ProducerOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultProducerOptions 默认生产者配置选项
func defaultProducerOptions() *producerOptions {
	return &producerOptions{
		logger:             defaultLogger,
		customerDeadLetter: defaultCustomerDeadLetterOptions(),
		durable:            true,
		mandatory:          true,
	}
}

// WithProducerCustomerDeadLetterOptions set dead letter options.
func WithProducerCustomerDeadLetterOptions(opts ...CustomerDeadLetterOption) ProducerOption {
	return func(o *producerOptions) {
		o.customerDeadLetter.apply(opts...)
	}
}

// WithProducerDurable set producer persistent option.
func WithProducerDurable(enable bool) ProducerOption {
	return func(o *producerOptions) {
		o.durable = enable
	}
}

// WithProducerMandatory set producer mandatory option.
func WithProducerMandatory(enable bool) ProducerOption {
	return func(o *producerOptions) {
		o.mandatory = enable
	}
}

// -------------------------------------------------------------------------------------------

// Producer RabbitMQ生产者结构体
type Producer struct {
	zapLog    *zap.Logger
	Exchange  *Exchange        // exchange
	QueueName string           // queue name
	conn      *amqp.Connection // rabbitmq connection
	channel   *amqp.Channel    // rabbitmq channel

	// persistent or not
	isPersistent bool
	deliveryMode uint8 // amqp.Persistent or amqp.Transient

	// If true, the message will be returned to the sender if the queue cannot be
	// found according to its own exchange type and routeKey rules.
	mandatory          bool
	customerDeadLetter *CustomerDeadLetterOptions
}

// NewProducer 创建一个新的生产者实例
// conn: RabbitMQ连接
// opts: 生产者配置选项
// 返回生产者实例和可能的错误
func NewProducer(exchange *Exchange, connection *Connection, opts ...ProducerOption) (*Producer, error) {
	o := defaultProducerOptions()
	o.apply(opts...)
	// crate a new channel
	amqpConn := connection.GetConn()
	channel, err := amqpConn.Channel()
	if err != nil {
		return nil, err
	}

	// customerDeadLetter a queue and create it automatically if it doesn't exist, or skip creation if it does.
	if o.customerDeadLetter.isEnabled() {
		// 声明交换机
		err = channel.ExchangeDeclare(
			exchange.name,  //交换机名称
			exchange.eType, // 交换机类型  (direct, topic, fanout, headers)
			o.durable,      //是否持久化
			o.customerDeadLetter.exchangeDeclare.autoDelete, //是否自动删除
			o.customerDeadLetter.exchangeDeclare.internal,   //是否是内部交换机
			o.customerDeadLetter.exchangeDeclare.noWait,     //是否非阻塞
			o.customerDeadLetter.exchangeDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		//------------------------------------------------------------------------------------
		//  声明死信队列并设置异常策略
		if o.customerDeadLetter.deadQueueDeclare.args == nil {
			o.customerDeadLetter.deadQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    exchange.name,
				"x-dead-letter-routing-key": o.customerDeadLetter.errRoutingKey,
				"x-message-ttl":             int32(600000), // 600秒后过期
			}
		}
		// QueueDeclare 声明队列
		dlq, err := channel.QueueDeclare(
			o.customerDeadLetter.deadQueueName, //队列名称
			o.durable,                          //是否持久化
			o.customerDeadLetter.deadQueueDeclare.autoDelete, //是否自动删除
			o.customerDeadLetter.deadQueueDeclare.exclusive,  //是否排他
			o.customerDeadLetter.deadQueueDeclare.noWait,     //是否非阻塞
			o.customerDeadLetter.deadQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			dlq.Name,                            //队列名称
			o.customerDeadLetter.deadRoutingKey, //路由键
			exchange.name,                       //交换机名称
			o.customerDeadLetter.deadQueueBind.noWait, //是否非阻塞
			o.customerDeadLetter.deadQueueBind.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}

		//------------------------------------------------------------------------------------
		// 声明异常队列并设置死信策略
		if o.customerDeadLetter.errQueueDeclare.args == nil {
			o.customerDeadLetter.errQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    exchange.name,
				"x-dead-letter-routing-key": o.customerDeadLetter.deadRoutingKey,
			}
		}
		// QueueDeclare 声明队列
		elq, err := channel.QueueDeclare(
			o.customerDeadLetter.errQueueName, //队列名称
			o.durable,                         //是否持久化
			o.customerDeadLetter.errQueueDeclare.autoDelete, //是否自动删除
			o.customerDeadLetter.errQueueDeclare.exclusive,  //是否排他
			o.customerDeadLetter.errQueueDeclare.noWait,     //是否非阻塞
			o.customerDeadLetter.errQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			elq.Name,                                 //队列名称
			o.customerDeadLetter.errRoutingKey,       //路由键
			exchange.name,                            //交换机名称
			o.customerDeadLetter.errQueueBind.noWait, //是否非阻塞
			o.customerDeadLetter.errQueueBind.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		//------------------------------------------------------------------------------------
		// 声明普通队列并设置死信策略
		if o.customerDeadLetter.normalQueueDeclare.args == nil {
			o.customerDeadLetter.normalQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    exchange.name,
				"x-dead-letter-routing-key": o.customerDeadLetter.deadRoutingKey,
			}
		}
		// QueueDeclare 声明队列
		lq, err := channel.QueueDeclare(
			o.customerDeadLetter.normalQueueName, //队列名称
			o.durable,                            //是否持久化
			o.customerDeadLetter.normalQueueDeclare.autoDelete, //是否自动删除
			o.customerDeadLetter.normalQueueDeclare.exclusive,  //是否排他
			o.customerDeadLetter.normalQueueDeclare.noWait,     //是否非阻塞
			o.customerDeadLetter.normalQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			lq.Name,                               //队列名称
			o.customerDeadLetter.normalRoutingKey, //路由键
			exchange.name,                         //交换机名称
			o.customerDeadLetter.normalQueueDeclare.noWait, //是否非阻塞
			o.customerDeadLetter.normalQueueDeclare.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
	}

	//------------------------------------------------------------------------------------
	deliveryMode := amqp.Persistent
	if !o.durable {
		deliveryMode = amqp.Transient
	}
	return &Producer{
		zapLog:             connection.zapLog,
		conn:               amqpConn,
		channel:            channel,
		Exchange:           exchange,
		isPersistent:       o.durable,
		deliveryMode:       deliveryMode,
		mandatory:          o.mandatory,
		customerDeadLetter: o.customerDeadLetter,
	}, nil
}

// PublishDirect send direct type message
func (p *Producer) PublishDirect(ctx context.Context, body []byte) error {
	if p.Exchange.eType != exchangeTypeDirect {
		return fmt.Errorf("invalid exchange type (%s), only supports direct type", p.Exchange.eType)
	}
	// ctx: 上下文
	// exchange: 交换机名称
	// key: 路由键
	// mandatory: 是否强制发送
	// immediate: 是否立即发送
	// msg: 消息内容
	return p.channel.PublishWithContext(
		ctx,
		p.Exchange.name,
		p.Exchange.routingKey,
		p.mandatory,
		false,
		amqp.Publishing{
			DeliveryMode: p.deliveryMode,
			ContentType:  "text/plain",
			Body:         body,
		},
	)
}

// PublishFanout send fanout type message
func (p *Producer) PublishFanout(ctx context.Context, body []byte) error {
	if p.Exchange.eType != exchangeTypeFanout {
		return fmt.Errorf("invalid exchange type (%s), only supports fanout type", p.Exchange.eType)
	}
	return p.channel.PublishWithContext(
		ctx,
		p.Exchange.name,
		p.Exchange.routingKey,
		p.mandatory,
		false,
		amqp.Publishing{
			DeliveryMode: p.deliveryMode,
			ContentType:  "text/plain",
			Body:         body,
		},
	)
}

// PublishTopic send topic type message
func (p *Producer) PublishTopic(ctx context.Context, topicKey string, body []byte) (err error) {
	tracer := otel.Tracer("PublishTopic")
	ctx, span := tracer.Start(ctx, "PublishTopic")
	defer span.End()

	if p.Exchange.eType != exchangeTypeTopic {
		err = fmt.Errorf("invalid exchange type (%s), only supports topic type", p.Exchange.eType)
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.String("body", string(body)))
	err = p.channel.PublishWithContext(
		ctx,
		p.Exchange.name,
		topicKey,
		p.mandatory,
		false,
		amqp.Publishing{
			DeliveryMode: p.deliveryMode,
			ContentType:  "text/plain",
			Body:         body,
		},
	)
	if err != nil {
		span.RecordError(err)
	}
	return err
}

// PublishHeaders send headers type message
func (p *Producer) PublishHeaders(ctx context.Context, headersKeys map[string]interface{}, body []byte) error {
	if p.Exchange.eType != exchangeTypeHeaders {
		return fmt.Errorf("invalid exchange type (%s), only supports headers type", p.Exchange.eType)
	}
	return p.channel.PublishWithContext(
		ctx,
		p.Exchange.name,
		p.Exchange.routingKey,
		p.mandatory,
		false,
		amqp.Publishing{
			DeliveryMode: p.deliveryMode,
			Headers:      headersKeys,
			ContentType:  "text/plain",
			Body:         body,
		},
	)
}

// Close 关闭生产者通道
// 返回可能的错误
func (p *Producer) Close() error {
	return p.channel.Close()
}
