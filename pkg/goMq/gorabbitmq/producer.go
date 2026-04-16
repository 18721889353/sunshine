package gorabbitmq

import (
	"context"
	"fmt"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// producerOptions 生产者配置选项
type producerOptions struct {
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
	Exchange   *Exchange // 交换机
	QueueName  string    // 队列名称
	Connection *Connection
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
		Connection:         connection,
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
func (p *Producer) PublishDirect(ctx context.Context, routingKey string, body []byte, messageID string) (err error) {
	spanName := fmt.Sprintf("rabbitmq.publish.direct.%s", p.Exchange.name)
	ctx, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	startTime := time.Now()

	// 提取 Context 中的 RequestID（大厂标准：关联业务日志和 Trace）
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			span.SetAttributes(attribute.String(string(logger.ContextKeyForRequestID()), reqIDStr))
		}
	}

	// 设置 OpenTelemetry Messaging Semantic Conventions 标准属性
	span.SetAttributes(
		attribute.String("messaging.system", "rabbitmq"),
		attribute.String("messaging.operation", "publish"),
		attribute.String("messaging.destination.name", p.Exchange.name),
		attribute.String("messaging.destination.kind", "exchange"),
		attribute.String("messaging.rabbitmq.routing_key", routingKey),
		attribute.String("messaging.rabbitmq.exchange.type", "direct"),
		attribute.Int("messaging.message.body.size", len(body)),
		attribute.String("messaging.message.id", messageID),
		attribute.Int("messaging.rabbitmq.delivery_mode", int(p.deliveryMode)),
		attribute.Bool("messaging.rabbitmq.mandatory", p.mandatory),
	)

	if p.Exchange.eType != exchangeTypeDirect {
		err = fmt.Errorf("invalid exchange type (%s), only supports direct type", p.Exchange.eType)
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", "configuration-error"),
				attribute.String("error.context", "exchange-type-validation"),
			),
		)
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "[rabbitmq producer] invalid exchange type",
			logger.Err(err),
			logger.String("exchange", p.Exchange.name))
		return err
	}

	span.AddEvent("preparing message for publish",
		trace.WithAttributes(
			attribute.String("exchange", p.Exchange.name),
			attribute.String("routing_key", routingKey),
			attribute.String("message_id", messageID),
			attribute.Int("body_size", len(body)),
			attribute.Int("delivery_mode", int(p.deliveryMode)),
			attribute.Bool("mandatory", p.mandatory),
		))

	// 注入 Trace Context 到消息头（大厂标准做法）
	// 使用 OpenTelemetry Propagator 自动注入标准 W3C Trace Context
	headersMap := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headersMap))

	// 将 request_id 也注入到消息 Header，以便 Consumer 可以获取
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			headersMap[string(logger.ContextKeyRequestID)] = reqIDStr
		}
	}

	// 转换为 amqp.Table
	headers := amqp.Table{}
	for k, v := range headersMap {
		headers[k] = v
	}

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
			Timestamp:    time.Now(),
			MessageId:    messageID,
			Headers:      headers, // 携带 Trace 信息
		},
	)

	duration := time.Since(startTime)
	span.SetAttributes(attribute.Float64("messaging.operation.duration_ms", float64(duration.Milliseconds())))

	if err != nil {
		errorMsg := fmt.Sprintf("publish failed: %v | exchange=%s | routing_key=%s | exchange_type=direct | message_id=%s | body_size=%d",
			err, p.Exchange.name, routingKey, messageID, len(body))
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", fmt.Sprintf("%T", err)),
				attribute.String("error.context", "publish-direct-failed"),
				attribute.String("error.exchange", p.Exchange.name),
				attribute.String("error.routing_key", routingKey),
				attribute.String("error.exchange_type", "direct"),
			),
		)
		span.SetStatus(codes.Error, errorMsg)
		span.AddEvent("publish failed",
			trace.WithAttributes(
				attribute.String("error.message", err.Error()),
				attribute.String("exchange", p.Exchange.name),
				attribute.String("routing_key", routingKey),
				attribute.String("message_id", messageID),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			),
		)
		logger.WarnWithCtx(ctx, "[rabbitmq producer] publish direct failed",
			logger.Err(err),
			logger.String("exchange", p.Exchange.name),
			logger.String("routing_key", routingKey),
			logger.String("message_id", messageID),
			logger.Float64("duration_ms", float64(duration.Milliseconds())))
	} else {
		span.AddEvent("message published successfully",
			trace.WithAttributes(
				attribute.String("exchange", p.Exchange.name),
				attribute.String("routing_key", routingKey),
				attribute.String("message_id", messageID),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			))
	}
	return err
}

// PublishFanout 发送fanout类型消息
// ctx: 上下文
// body: 消息体
// 返回可能的错误
func (p *Producer) PublishFanout(ctx context.Context, body []byte, messageID string) (err error) {
	spanName := fmt.Sprintf("rabbitmq.publish.fanout.%s", p.Exchange.name)
	ctx, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	startTime := time.Now()

	// 提取 Context 中的 RequestID（大厂标准：关联业务日志和 Trace）
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			span.SetAttributes(attribute.String(string(logger.ContextKeyForRequestID()), reqIDStr))
		}
	}

	routingKey := p.Exchange.routingKey
	if p.isDelay {
		routingKey = p.deadLetter.deadRoutingKey
	}

	// 设置 OpenTelemetry Messaging Semantic Conventions 标准属性
	span.SetAttributes(
		attribute.String("messaging.system", "rabbitmq"),
		attribute.String("messaging.operation", "publish"),
		attribute.String("messaging.destination.name", p.Exchange.name),
		attribute.String("messaging.destination.kind", "exchange"),
		attribute.String("messaging.rabbitmq.routing_key", routingKey),
		attribute.String("messaging.rabbitmq.exchange.type", "fanout"),
		attribute.Int("messaging.message.body.size", len(body)),
		attribute.String("messaging.message.id", messageID),
		attribute.Int("messaging.rabbitmq.delivery_mode", int(p.deliveryMode)),
		attribute.Bool("messaging.rabbitmq.mandatory", p.mandatory),
		attribute.Bool("messaging.rabbitmq.is_delay", p.isDelay),
	)

	if p.Exchange.eType != exchangeTypeFanout {
		err = fmt.Errorf("invalid exchange type (%s), only supports fanout type", p.Exchange.eType)
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", "configuration-error"),
				attribute.String("error.context", "exchange-type-validation"),
			),
		)
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "[rabbitmq producer] invalid exchange type",
			logger.Err(err),
			logger.String("exchange", p.Exchange.name))
		return err
	}

	span.AddEvent("preparing message for publish",
		trace.WithAttributes(
			attribute.String("exchange", p.Exchange.name),
			attribute.String("routing_key", routingKey),
			attribute.String("message_id", messageID),
			attribute.Int("body_size", len(body)),
			attribute.Int("delivery_mode", int(p.deliveryMode)),
			attribute.Bool("mandatory", p.mandatory),
			attribute.Bool("is_delay", p.isDelay),
		))

	// 注入 Trace Context 到消息头
	headersMap := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headersMap))

	// 将 request_id 也注入到消息 Header，以便 Consumer 可以获取
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			headersMap[string(logger.ContextKeyRequestID)] = reqIDStr
		}
	}

	// 转换为 amqp.Table
	headers := amqp.Table{}
	for k, v := range headersMap {
		headers[k] = v
	}

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
			Timestamp:    time.Now(),
			MessageId:    messageID,
			Headers:      headers,
		},
	)

	duration := time.Since(startTime)
	span.SetAttributes(attribute.Float64("messaging.operation.duration_ms", float64(duration.Milliseconds())))

	if err != nil {
		errorMsg := fmt.Sprintf("publish failed: %v | exchange=%s | routing_key=%s | exchange_type=fanout | message_id=%s | body_size=%d",
			err, p.Exchange.name, routingKey, messageID, len(body))
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", fmt.Sprintf("%T", err)),
				attribute.String("error.context", "publish-fanout-failed"),
				attribute.String("error.exchange", p.Exchange.name),
				attribute.String("error.routing_key", routingKey),
				attribute.String("error.exchange_type", "fanout"),
			),
		)
		span.SetStatus(codes.Error, errorMsg)
		span.AddEvent("publish failed",
			trace.WithAttributes(
				attribute.String("error.message", err.Error()),
				attribute.String("exchange", p.Exchange.name),
				attribute.String("routing_key", routingKey),
				attribute.String("message_id", messageID),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			),
		)
		logger.WarnWithCtx(ctx, "[rabbitmq producer] publish fanout failed",
			logger.Err(err),
			logger.String("exchange", p.Exchange.name),
			logger.String("routing_key", routingKey),
			logger.String("message_id", messageID),
			logger.Float64("duration_ms", float64(duration.Milliseconds())))
	} else {
		span.AddEvent("message published successfully",
			trace.WithAttributes(
				attribute.String("exchange", p.Exchange.name),
				attribute.String("routing_key", routingKey),
				attribute.String("message_id", messageID),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			))
	}
	return err
}

// PublishTopic 发送topic类型消息
// ctx: 上下文
// topicKey: topic路由键
// body: 消息体
// 返回可能的错误
func (p *Producer) PublishTopic(ctx context.Context, routingKey string, body []byte, messageID string) (err error) {
	spanName := fmt.Sprintf("rabbitmq.publish.topic.%s", p.Exchange.name)
	ctx, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	startTime := time.Now()

	// 提取 Context 中的 RequestID（大厂标准：关联业务日志和 Trace）
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			span.SetAttributes(attribute.String(string(logger.ContextKeyForRequestID()), reqIDStr))
		}
	}

	// 设置 OpenTelemetry Messaging Semantic Conventions 标准属性
	span.SetAttributes(
		attribute.String("messaging.system", "rabbitmq"),
		attribute.String("messaging.operation", "publish"),
		attribute.String("messaging.destination.name", p.Exchange.name),
		attribute.String("messaging.destination.kind", "exchange"),
		attribute.String("messaging.rabbitmq.routing_key", routingKey),
		attribute.String("messaging.rabbitmq.exchange.type", "topic"),
		attribute.Int("messaging.message.body.size", len(body)),
		attribute.String("messaging.message.id", messageID),
		attribute.Int("messaging.rabbitmq.delivery_mode", int(p.deliveryMode)),
		attribute.Bool("messaging.rabbitmq.mandatory", p.mandatory),
	)

	if p.Exchange.eType != exchangeTypeTopic {
		err = fmt.Errorf("invalid exchange type (%s), only supports topic type", p.Exchange.eType)
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", "configuration-error"),
				attribute.String("error.context", "exchange-type-validation"),
			),
		)
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "[rabbitmq producer] invalid exchange type",
			logger.Err(err),
			logger.String("exchange", p.Exchange.name))
		return err
	}

	span.AddEvent("preparing message for publish",
		trace.WithAttributes(
			attribute.String("exchange", p.Exchange.name),
			attribute.String("routing_key", routingKey),
			attribute.String("message_id", messageID),
			attribute.Int("body_size", len(body)),
			attribute.Int("delivery_mode", int(p.deliveryMode)),
			attribute.Bool("mandatory", p.mandatory),
		))

	// 注入 Trace Context 到消息头
	headersMap := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headersMap))

	// 将 request_id 也注入到消息 Header，以便 Consumer 可以获取
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			headersMap[string(logger.ContextKeyRequestID)] = reqIDStr
		}
	}

	// 转换为 amqp.Table
	headers := amqp.Table{}
	for k, v := range headersMap {
		headers[k] = v
	}

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
			Timestamp:    time.Now(),
			MessageId:    messageID,
			Headers:      headers,
		},
	)

	duration := time.Since(startTime)
	span.SetAttributes(attribute.Float64("messaging.operation.duration_ms", float64(duration.Milliseconds())))

	if err != nil {
		errorMsg := fmt.Sprintf("publish failed: %v | exchange=%s | routing_key=%s | exchange_type=topic | message_id=%s | body_size=%d",
			err, p.Exchange.name, routingKey, messageID, len(body))
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", fmt.Sprintf("%T", err)),
				attribute.String("error.context", "publish-topic-failed"),
				attribute.String("error.exchange", p.Exchange.name),
				attribute.String("error.routing_key", routingKey),
				attribute.String("error.exchange_type", "topic"),
			),
		)
		span.SetStatus(codes.Error, errorMsg)
		span.AddEvent("publish failed",
			trace.WithAttributes(
				attribute.String("error.message", err.Error()),
				attribute.String("exchange", p.Exchange.name),
				attribute.String("routing_key", routingKey),
				attribute.String("message_id", messageID),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			),
		)
		logger.WarnWithCtx(ctx, "[rabbitmq producer] publish topic failed",
			logger.Err(err),
			logger.String("exchange", p.Exchange.name),
			logger.String("routing_key", routingKey),
			logger.String("message_id", messageID),
			logger.Float64("duration_ms", float64(duration.Milliseconds())))
	} else {
		span.AddEvent("message published successfully",
			trace.WithAttributes(
				attribute.String("exchange", p.Exchange.name),
				attribute.String("routing_key", routingKey),
				attribute.String("message_id", messageID),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			))
	}
	return err
}

// PublishHeaders 发送headers类型消息
// ctx: 上下文
// headersKeys: 消息头键值对
// body: 消息体
// 返回可能的错误
func (p *Producer) PublishHeaders(ctx context.Context, headersKeys map[string]interface{}, body []byte, messageID string) (err error) {
	spanName := fmt.Sprintf("rabbitmq.publish.headers.%s", p.Exchange.name)
	ctx, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	startTime := time.Now()

	// 提取 Context 中的 RequestID（大厂标准：关联业务日志和 Trace）
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			span.SetAttributes(attribute.String(string(logger.ContextKeyForRequestID()), reqIDStr))
		}
	}

	// 设置 OpenTelemetry Messaging Semantic Conventions 标准属性
	span.SetAttributes(
		attribute.String("messaging.system", "rabbitmq"),
		attribute.String("messaging.operation", "publish"),
		attribute.String("messaging.destination.name", p.Exchange.name),
		attribute.String("messaging.destination.kind", "exchange"),
		attribute.String("messaging.rabbitmq.exchange.type", "headers"),
		attribute.Int("messaging.message.body.size", len(body)),
		attribute.String("messaging.message.id", messageID),
		attribute.Int("messaging.rabbitmq.delivery_mode", int(p.deliveryMode)),
		attribute.Bool("messaging.rabbitmq.mandatory", p.mandatory),
		attribute.Int("messaging.rabbitmq.headers_count", len(headersKeys)),
	)

	if p.Exchange.eType != exchangeTypeHeaders {
		err = fmt.Errorf("invalid exchange type (%s), only supports headers type", p.Exchange.eType)
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", "configuration-error"),
				attribute.String("error.context", "exchange-type-validation"),
			),
		)
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "[rabbitmq producer] invalid exchange type",
			logger.Err(err),
			logger.String("exchange", p.Exchange.name))
		return err
	}

	span.AddEvent("preparing message for publish",
		trace.WithAttributes(
			attribute.String("exchange", p.Exchange.name),
			attribute.String("routing_key", p.Exchange.routingKey),
			attribute.String("message_id", messageID),
			attribute.Int("body_size", len(body)),
			attribute.Int("delivery_mode", int(p.deliveryMode)),
			attribute.Bool("mandatory", p.mandatory),
			attribute.Int("headers_count", len(headersKeys)),
		))

	// 注入 Trace Context 到消息头（与用户自定义 headers 合并）
	headersMap := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headersMap))

	// 将 request_id 也注入到消息 Header，以便 Consumer 可以获取
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			headersMap[string(logger.ContextKeyRequestID)] = reqIDStr
		}
	}

	// 转换为 amqp.Table 并合并用户自定义 headers
	headers := amqp.Table{}
	for k, v := range headersMap {
		headers[k] = v
	}
	for k, v := range headersKeys {
		headers[k] = v
	}

	err = p.channel.PublishWithContext(
		ctx,
		p.Exchange.name,
		p.Exchange.routingKey,
		p.mandatory,
		false,
		amqp.Publishing{
			DeliveryMode: p.deliveryMode,
			Headers:      headers, // 包含 Trace + 用户自定义 headers
			ContentType:  "text/plain",
			Body:         body,
			Timestamp:    time.Now(),
			MessageId:    messageID,
		},
	)

	duration := time.Since(startTime)
	span.SetAttributes(attribute.Float64("messaging.operation.duration_ms", float64(duration.Milliseconds())))

	if err != nil {
		errorMsg := fmt.Sprintf("publish failed: %v | exchange=%s | routing_key=%s | exchange_type=headers | message_id=%s | body_size=%d | headers_count=%d",
			err, p.Exchange.name, p.Exchange.routingKey, messageID, len(body), len(headersKeys))
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", fmt.Sprintf("%T", err)),
				attribute.String("error.context", "publish-headers-failed"),
				attribute.String("error.exchange", p.Exchange.name),
				attribute.String("error.routing_key", p.Exchange.routingKey),
				attribute.String("error.exchange_type", "headers"),
			),
		)
		span.SetStatus(codes.Error, errorMsg)
		span.AddEvent("publish failed",
			trace.WithAttributes(
				attribute.String("error.message", err.Error()),
				attribute.String("exchange", p.Exchange.name),
				attribute.String("routing_key", p.Exchange.routingKey),
				attribute.String("message_id", messageID),
				attribute.Int("headers_count", len(headersKeys)),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			),
		)
		logger.WarnWithCtx(ctx, "[rabbitmq producer] publish headers failed",
			logger.Err(err),
			logger.String("exchange", p.Exchange.name),
			logger.String("routing_key", p.Exchange.routingKey),
			logger.String("message_id", messageID),
			logger.Int("headers_count", len(headersKeys)),
			logger.Float64("duration_ms", float64(duration.Milliseconds())))
	} else {
		span.AddEvent("message published successfully",
			trace.WithAttributes(
				attribute.String("exchange", p.Exchange.name),
				attribute.String("routing_key", p.Exchange.routingKey),
				attribute.String("message_id", messageID),
				attribute.Int("headers_count", len(headersKeys)),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			))
	}
	return err
}

// Close 关闭生产者通道
// 返回可能的错误
func (p *Producer) Close() error {
	return p.channel.Close()
}

func logFields(exchange *Exchange, data map[string]any) []logger.Field {
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
	return []logger.Field{
		logger.Any("body", body),
	}
}
