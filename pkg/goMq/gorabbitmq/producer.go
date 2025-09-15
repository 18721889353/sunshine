package gorabbitmq

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// producerOptions 生产者配置选项
type producerOptions struct {
	logger             *zap.Logger                // 日志记录器
	customerDeadLetter *CustomerDeadLetterOptions // 自定义死信队列选项
	normalLetter       *NormalLetterOptions       // 正常队列选项
	deadLetter         *DeadLetterOptions         // 死信队列选项
	msgDurable         bool                       // 消息是否持久化
	mandatory          bool                       // 消息不可路由时是否返回给发送者
	isDelay            bool                       // 是否延迟消息
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
		normalLetter:       defaultNormalLetterOptions(),
		deadLetter:         defaultDeadLetterOptions(),
		//用于控制消息是否持久化存储。当设置为 true 时，表示消息需要被持久化，以确保在 RabbitMQ 服务器重启后消息不会丢失。
		msgDurable: true,
		//mandatory 设置为 true 时，如果消息无法根据 exchange 类型和 routing key 规则路由到任何队列，消息会被返回给发送者
		//mandatory 设置为 false 时，无法路由的消息会被直接丢弃
		mandatory: true,
		isDelay:   false,
	}
}

// WithProducerCustomerDeadLetterOptions set dead letter options.
func WithProducerCustomerDeadLetterOptions(opts ...CustomerDeadLetterOption) ProducerOption {
	return func(o *producerOptions) {
		o.customerDeadLetter.apply(opts...)
	}
}

// WithProducerDeadLetterOptions set dead letter options.
func WithProducerDeadLetterOptions(opts ...DeadLetterOption) ProducerOption {
	return func(o *producerOptions) {
		o.deadLetter.apply(opts...)
	}
}

// WithProducerNormalLetterOptions set dead letter options.
func WithProducerNormalLetterOptions(opts ...NormalLetterOption) ProducerOption {
	return func(o *producerOptions) {
		o.normalLetter.apply(opts...)
	}
}

// WithProducerMsgDurable 消息是否持久化 set producer persistent option.
func WithProducerMsgDurable(enable bool) ProducerOption {
	return func(o *producerOptions) {
		o.msgDurable = enable
	}
}

// WithProducerMandatory  消息不可路由时是否返回给发送者 set producer mandatory option.
func WithProducerMandatory(enable bool) ProducerOption {
	return func(o *producerOptions) {
		o.mandatory = enable
	}
}
func WithProducerIsDelay(enable bool) ProducerOption {
	return func(o *producerOptions) {
		o.isDelay = enable
	}
}

// -------------------------------------------------------------------------------------------

// Producer RabbitMQ生产者结构体
type Producer struct {
	zapLog     *zap.Logger // 日志记录器
	Exchange   *Exchange   // 交换机
	QueueName  string      // 队列名称
	connection *Connection
	conn       *amqp.Connection // RabbitMQ连接
	channel    *amqp.Channel    // RabbitMQ通道

	deliveryMode uint8 // 消息投递模式 amqp.Persistent 或 amqp.Transient

	// If true, the message will be returned to the sender if the queue cannot be
	// found according to its own exchange type and routeKey rules.
	mandatory          bool                       // 消息不可路由时是否返回给发送者
	customerDeadLetter *CustomerDeadLetterOptions // 自定义死信队列选项
	deadLetter         *DeadLetterOptions         // 自定义死信队列选项
	normalLetter       *NormalLetterOptions
	tracer             trace.Tracer // OpenTelemetry tracer for reuse
	isDelay            bool
}

// NewProducer 创建一个新的生产者实例
// ctx: 上下文
// exchange: 交换机配置
// connection: RabbitMQ连接
// opts: 生产者配置选项
// 返回生产者实例和可能的错误
func NewProducer(ctx context.Context, exchange *Exchange, connection *Connection, opts ...ProducerOption) (*Producer, error) {
	o := defaultProducerOptions()
	o.apply(opts...)
	//var fields []zap.Field

	// crate a new channel
	amqpConn := connection.GetConn(ctx)
	channel, err := amqpConn.Channel()
	if err != nil {
		return nil, err
	}
	if o.customerDeadLetter.exchangeName != "sunshine" && o.normalLetter.exchangeName != "sunshine" {
		return nil, fmt.Errorf("cannot set both customerDeadLetter and normalLetter")
	}
	if o.customerDeadLetter.exchangeName != "sunshine" && o.deadLetter.exchangeName != "sunshine" {
		return nil, fmt.Errorf("cannot set both customerDeadLetter and deadLetter")
	}
	if o.normalLetter.exchangeName != "sunshine" && o.deadLetter.exchangeName != "sunshine" {
		return nil, fmt.Errorf("cannot set both normalLetter and deadLetter")
	}
	//--------------------------------自定义死信队列队列----------------------------------------------------
	if o.customerDeadLetter.exchangeName != "sunshine" {
		// 声明交换机
		err = channel.ExchangeDeclare(
			exchange.name,  //交换机名称
			exchange.eType, // 交换机类型  (direct, topic, fanout, headers)
			o.customerDeadLetter.exchangeDeclare.durable,    //是否持久化
			o.customerDeadLetter.exchangeDeclare.autoDelete, //是否自动删除
			o.customerDeadLetter.exchangeDeclare.internal,   //是否是内部交换机
			o.customerDeadLetter.exchangeDeclare.noWait,     //是否非阻塞
			o.customerDeadLetter.exchangeDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		//  声明死信队列并设置异常策略
		if o.customerDeadLetter.deadQueueDeclare.args == nil {
			o.customerDeadLetter.deadQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    exchange.name,
				"x-dead-letter-routing-key": o.customerDeadLetter.errRoutingKey,
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
			o.customerDeadLetter.deadQueueName,               //队列名称
			o.customerDeadLetter.deadQueueDeclare.durable,    //是否持久化
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

		// 声明异常队列并设置死信策略
		if o.customerDeadLetter.errQueueDeclare.args == nil {
			o.customerDeadLetter.errQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    exchange.name,
				"x-dead-letter-routing-key": o.customerDeadLetter.deadRoutingKey,
			}
		}
		// QueueDeclare 声明队列
		elq, err := channel.QueueDeclare(
			o.customerDeadLetter.errQueueName,               //队列名称
			o.customerDeadLetter.errQueueDeclare.durable,    //是否持久化
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
		// 声明普通队列并设置死信策略
		if o.customerDeadLetter.normalQueueDeclare.args == nil {
			o.customerDeadLetter.normalQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    exchange.name,
				"x-dead-letter-routing-key": o.customerDeadLetter.deadRoutingKey,
			}
		}
		// QueueDeclare 声明队列
		lq, err := channel.QueueDeclare(
			o.customerDeadLetter.normalQueueName,               //队列名称
			o.customerDeadLetter.normalQueueDeclare.durable,    //是否持久化
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
	//--------------------------------死信队列队列----------------------------------------------------
	if o.deadLetter.exchangeName != "sunshine" {
		// 声明交换机
		err = channel.ExchangeDeclare(
			exchange.name,                           //交换机名称
			exchange.eType,                          // 交换机类型  (direct, topic, fanout, headers)
			o.deadLetter.exchangeDeclare.durable,    //是否持久化
			o.deadLetter.exchangeDeclare.autoDelete, //是否自动删除
			o.deadLetter.exchangeDeclare.internal,   //是否是内部交换机
			o.deadLetter.exchangeDeclare.noWait,     //是否非阻塞
			o.deadLetter.exchangeDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		//  声明死信队列并设置异常策略
		if o.deadLetter.deadQueueDeclare.args == nil {
			o.deadLetter.deadQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    exchange.name,
				"x-dead-letter-routing-key": o.deadLetter.normalRoutingKey,
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
			o.deadLetter.deadQueueName,               //队列名称
			o.deadLetter.deadQueueDeclare.durable,    //是否持久化
			o.deadLetter.deadQueueDeclare.autoDelete, //是否自动删除
			o.deadLetter.deadQueueDeclare.exclusive,  //是否排他
			o.deadLetter.deadQueueDeclare.noWait,     //是否非阻塞
			o.deadLetter.deadQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			dlq.Name,                          //队列名称
			o.deadLetter.deadRoutingKey,       //路由键
			exchange.name,                     //交换机名称
			o.deadLetter.deadQueueBind.noWait, //是否非阻塞
			o.deadLetter.deadQueueBind.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}

		// 声明普通队列并设置死信策略
		if o.deadLetter.normalQueueDeclare.args == nil {
			o.deadLetter.normalQueueDeclare.args = amqp.Table{
				"x-dead-letter-exchange":    exchange.name,
				"x-dead-letter-routing-key": o.deadLetter.deadRoutingKey,
			}
		}
		// QueueDeclare 声明队列
		lq, err := channel.QueueDeclare(
			o.deadLetter.normalQueueName,               //队列名称
			o.deadLetter.normalQueueDeclare.durable,    //是否持久化
			o.deadLetter.normalQueueDeclare.autoDelete, //是否自动删除
			o.deadLetter.normalQueueDeclare.exclusive,  //是否排他
			o.deadLetter.normalQueueDeclare.noWait,     //是否非阻塞
			o.deadLetter.normalQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			lq.Name,                                //队列名称
			o.deadLetter.normalRoutingKey,          //路由键
			exchange.name,                          //交换机名称
			o.deadLetter.normalQueueDeclare.noWait, //是否非阻塞
			o.deadLetter.normalQueueDeclare.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
	}
	//--------------------------------正常队列----------------------------------------------------
	if o.normalLetter.exchangeName != "sunshine" {
		// 声明交换机
		err = channel.ExchangeDeclare(
			exchange.name,                             //交换机名称
			exchange.eType,                            // 交换机类型  (direct, topic, fanout, headers)
			o.normalLetter.exchangeDeclare.durable,    //是否持久化
			o.normalLetter.exchangeDeclare.autoDelete, //是否自动删除
			o.normalLetter.exchangeDeclare.internal,   //是否是内部交换机
			o.normalLetter.exchangeDeclare.noWait,     //是否非阻塞
			o.normalLetter.exchangeDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}

		// QueueDeclare 声明队列
		nlq, err := channel.QueueDeclare(
			o.normalLetter.normalQueueName,               //队列名称
			o.normalLetter.normalQueueDeclare.durable,    //是否持久化
			o.normalLetter.normalQueueDeclare.autoDelete, //是否自动删除
			o.normalLetter.normalQueueDeclare.exclusive,  //是否排他
			o.normalLetter.normalQueueDeclare.noWait,     //是否非阻塞
			o.normalLetter.normalQueueDeclare.args,       //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
		// BindQueue 绑定队列到交换机
		err = channel.QueueBind(
			nlq.Name,                                 //队列名称
			o.normalLetter.normalRoutingKey,          //路由键
			exchange.name,                            //交换机名称
			o.normalLetter.normalQueueDeclare.noWait, //是否非阻塞
			o.normalLetter.normalQueueDeclare.args,   //其他参数
		)
		if err != nil {
			_ = channel.Close()
			return nil, err
		}
	}

	//------------------------------------------------------------------------------------
	//amqp.Persistent: 这是一个常量，值为2，表示消息是持久化的。当设置为持久化模式时，消息会被写入磁盘，即使RabbitMQ服务器重启，消息也不会丢失。
	//amqp.Transient: 这是一个常量，值为1，表示消息是瞬态的。瞬态消息只保存在内存中，不进行磁盘持久化。如果RabbitMQ服务器重启，这些消息会丢失
	deliveryMode := amqp.Persistent
	if !o.msgDurable {
		deliveryMode = amqp.Transient
	}

	return &Producer{
		zapLog:             connection.zapLog,
		connection:         connection,
		conn:               amqpConn,
		channel:            channel,
		Exchange:           exchange,
		deliveryMode:       deliveryMode,
		mandatory:          o.mandatory,
		customerDeadLetter: o.customerDeadLetter,
		deadLetter:         o.deadLetter,
		normalLetter:       o.normalLetter,
		isDelay:            o.isDelay,
		tracer:             otel.Tracer("gorabbitmq"), // 初始化 tracer
	}, nil
}

// PublishDirect 发送direct类型消息
// ctx: 上下文
// body: 消息体
// 返回可能的错误
func (p *Producer) PublishDirect(ctx context.Context, body []byte) (err error) {
	ctx, span := p.tracer.Start(ctx, "PublishDirect")
	defer span.End()
	if p.Exchange.eType != exchangeTypeDirect {
		err = fmt.Errorf("invalid exchange type (%s), only supports direct type", p.Exchange.eType)
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.Int("body.size", len(body))) // 记录消息大小而不是内容
	routingKey := p.Exchange.routingKey
	if p.isDelay {
		routingKey = p.deadLetter.deadRoutingKey
	}
	// ctx: 上下文
	// exchange: 交换机名称
	// key: 路由键
	// mandatory: 不可路由时是否返回消息
	// immediate: 是否立即发送
	// msg: 消息内容
	err = p.channel.PublishWithContext(
		ctx,
		p.Exchange.name,
		routingKey,
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

// PublishFanout 发送fanout类型消息
// ctx: 上下文
// body: 消息体
// 返回可能的错误
//func (p *Producer) PublishFanout(ctx context.Context, body []byte) (err error) {
//	ctx, span := p.tracer.Start(ctx, "PublishFanout")
//	defer span.End()
//	if p.Exchange.eType != exchangeTypeFanout {
//		err = fmt.Errorf("invalid exchange type (%s), only supports fanout type", p.Exchange.eType)
//		span.RecordError(err)
//		return err
//	}
//	span.SetAttributes(attribute.Int("body.size", len(body))) // 记录消息大小而不是内容
//	routingKey := p.Exchange.routingKey
//	if p.isDelay {
//		routingKey = p.deadLetter.deadRoutingKey
//	}
//
//	err = p.channel.PublishWithContext(
//		ctx,
//		p.Exchange.name,
//		routingKey,
//		p.mandatory,
//		false,
//		amqp.Publishing{
//			DeliveryMode: p.deliveryMode,
//			ContentType:  "text/plain",
//			Body:         body,
//		},
//	)
//	if err != nil {
//		span.RecordError(err)
//	}
//	return err
//}

// PublishTopic 发送topic类型消息
// ctx: 上下文
// topicKey: topic路由键
// body: 消息体
// 返回可能的错误
//func (p *Producer) PublishTopic(ctx context.Context, topicKey string, body []byte) (err error) {
//	ctx, span := p.tracer.Start(ctx, "PublishTopic")
//	defer span.End()
//
//	if p.Exchange.eType != exchangeTypeTopic {
//		err = fmt.Errorf("invalid exchange type (%s), only supports topic type", p.Exchange.eType)
//		span.RecordError(err)
//		return err
//	}
//	span.SetAttributes(attribute.Int("body.size", len(body))) // 记录消息大小而不是内容
//	err = p.channel.PublishWithContext(
//		ctx,
//		p.Exchange.name,
//		topicKey,
//		p.mandatory,
//		false,
//		amqp.Publishing{
//			DeliveryMode: p.deliveryMode,
//			ContentType:  "text/plain",
//			Body:         body,
//		},
//	)
//	if err != nil {
//		span.RecordError(err)
//	}
//	return err
//}

// PublishHeaders 发送headers类型消息
// ctx: 上下文
// headersKeys: 消息头键值对
// body: 消息体
// 返回可能的错误
//func (p *Producer) PublishHeaders(ctx context.Context, headersKeys map[string]interface{}, body []byte) (err error) {
//	ctx, span := p.tracer.Start(ctx, "PublishHeaders")
//	defer span.End()
//	if p.Exchange.eType != exchangeTypeHeaders {
//		err = fmt.Errorf("invalid exchange type (%s), only supports headers type", p.Exchange.eType)
//		span.RecordError(err)
//		return err
//	}
//	span.SetAttributes(
//		attribute.Int("body.size", len(body)),            // 记录消息大小而不是内容
//		attribute.Int("headers.count", len(headersKeys)), // 记录headers数量
//	)
//	err = p.channel.PublishWithContext(
//		ctx,
//		p.Exchange.name,
//		p.Exchange.routingKey,
//		p.mandatory,
//		false,
//		amqp.Publishing{
//			DeliveryMode: p.deliveryMode,
//			Headers:      headersKeys,
//			ContentType:  "text/plain",
//			Body:         body,
//		},
//	)
//	if err != nil {
//		span.RecordError(err)
//	}
//	return err
//}

// Close 关闭生产者通道
// 返回可能的错误
func (p *Producer) Close() error {
	return p.channel.Close()
}

func logFields(exchange *Exchange, data map[string]any) []zap.Field {
	body := map[string]any{
		"exchange": exchange.name,
		"type":     exchange.eType,
	}
	for s, a := range data {
		body[s] = a
	}
	switch exchange.eType {
	case exchangeTypeDirect, exchangeTypeTopic:
		body["routingKey"] = exchange.routingKey
	case exchangeTypeHeaders:
		body["headersKeys"] = exchange.headersKeys
	}
	return []zap.Field{
		zap.Any("body", body),
	}
}
