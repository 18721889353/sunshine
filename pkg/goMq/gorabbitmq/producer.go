package gorabbitmq

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// Producer RabbitMQ 生产者，封装 exchange + channel 提供消息发布能力。
// 使用 NewProducer 创建，使用完毕后调用 Close 释放 channel。
// 高频发布场景建议使用 ProducerPool 复用实例。
type Producer struct {
	Exchange   *Exchange     // 交换机配置（名称、类型、路由键）
	Connection *Connection   // RabbitMQ 连接（含自动重连）
	mqChannel  *amqp.Channel // AMQP 通道，从 Connection 的底层连接创建

	*producerOptions              // 嵌入配置（提供 mandatory/isDelay/msgDurable 等字段）
	tracer           trace.Tracer // OpenTelemetry 链路追踪器
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

	// 校验交换机配置
	if err := exchange.Validate(); err != nil {
		return nil, err
	}

	// 校验死信选项互斥关系（必须在创建 channel 之前，避免资源泄漏）
	if o.customerDeadLetter.exchangeName != defaultExchangeName && o.normalLetter.exchangeName != defaultExchangeName {
		return nil, fmt.Errorf("cannot set both customerDeadLetter and normalLetter")
	}
	if o.customerDeadLetter.exchangeName != defaultExchangeName && o.deadLetter.exchangeName != defaultExchangeName {
		return nil, fmt.Errorf("cannot set both customerDeadLetter and deadLetter")
	}
	if o.normalLetter.exchangeName != defaultExchangeName && o.deadLetter.exchangeName != defaultExchangeName {
		return nil, fmt.Errorf("cannot set both normalLetter and deadLetter")
	}

	// crate a new channel
	mqConn := connection.GetConn(ctx)
	if mqConn == nil {
		return nil, fmt.Errorf("rabbitmq connection is not ready")
	}
	channel, err := mqConn.Channel()
	if err != nil {
		return nil, err
	}

	if o.customerDeadLetter.exchangeName != defaultExchangeName {
		if err := setupProducerCustomerDeadLetter(channel, exchange, o); err != nil {
			closeChannel(channel)
			return nil, err
		}
	}

	// 处理标准死信队列
	if o.deadLetter.exchangeName != defaultExchangeName {
		if err := setupProducerStandardDeadLetter(channel, exchange, o); err != nil {
			closeChannel(channel)
			return nil, err
		}
	}

	// 处理正常队列
	if o.normalLetter.exchangeName != defaultExchangeName {
		if err := setupProducerNormalLetter(channel, exchange, o); err != nil {
			closeChannel(channel)
			return nil, err
		}
	}

	// 确定消息持久化模式：amqp.Persistent(2) 写入磁盘，amqp.Transient(1) 仅内存

	return &Producer{
		Connection:      connection,
		mqChannel:       channel,
		Exchange:        exchange,
		producerOptions: o,
		tracer:          otel.Tracer("gorabbitmq"),
	}, nil
}

// deliveryMode 返回 AMQP 投递模式，由 msgDurable 决定
func (p *Producer) deliveryMode() uint8 {
	if !p.msgDurable {
		return amqp.Transient
	}
	return amqp.Persistent
}

// closeChannel 安全关闭 channel，日志记录错误
func closeChannel(channel *amqp.Channel) {
	if closeErr := channel.Close(); closeErr != nil {
		fmt.Printf("close channel error: %v\n", closeErr)
	}
}

// setupProducerCustomerDeadLetter 设置生产者的自定义死信队列，在 exchange 上声明三条消息路径。
// 参数:
//   - channel: AMQP 通道，用于声明交换机、队列和绑定
//   - exchange: 目标交换机配置
//   - o: 生产者配置选项，其中的 customerDeadLetter 包含具体队列和路由参数
//
// 返回:
//   - error: 任意一步失败时返回错误（调用方负责关闭 channel）
//
// 消息流转路径：
//
//	TTL 超时 → deadQueue → deadRoutingKey → exchange
//	消费失败 → errQueue  → errRoutingKey  → exchange
//	正常消费 → normalQueue → normalRoutingKey → exchange
func setupProducerCustomerDeadLetter(channel *amqp.Channel, exchange *Exchange, o *producerOptions) error {
	// 第一步：声明主交换机
	err := channel.ExchangeDeclare(
		exchange.name,
		exchange.eType,
		o.customerDeadLetter.exchangeDeclare.durable,
		o.customerDeadLetter.exchangeDeclare.autoDelete,
		o.customerDeadLetter.exchangeDeclare.internal,
		o.customerDeadLetter.exchangeDeclare.noWait,
		o.customerDeadLetter.exchangeDeclare.args,
	)
	if err != nil {
		return err
	}

	// 第二步：声明死信队列（TTL 超时消息入队）并绑定到交换机
	if o.customerDeadLetter.deadQueueDeclare.args == nil {
		o.customerDeadLetter.deadQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchange.name,
			"x-dead-letter-routing-key": o.customerDeadLetter.errRoutingKey,
			"x-message-ttl":             int32(600000), // 600秒后过期
		}
	}
	// 创建死信队列
	dlq, err := channel.QueueDeclare(
		o.customerDeadLetter.deadQueueName,
		o.customerDeadLetter.deadQueueDeclare.durable,
		o.customerDeadLetter.deadQueueDeclare.autoDelete,
		o.customerDeadLetter.deadQueueDeclare.exclusive,
		o.customerDeadLetter.deadQueueDeclare.noWait,
		o.customerDeadLetter.deadQueueDeclare.args,
	)
	if err != nil {
		return err
	}
	// 绑定死信队列
	err = channel.QueueBind(
		dlq.Name,
		o.customerDeadLetter.deadRoutingKey,
		exchange.name,
		o.customerDeadLetter.deadQueueBind.noWait,
		o.customerDeadLetter.deadQueueBind.args,
	)
	if err != nil {
		return err
	}

	// 第三步：声明异常队列（消费者 NACK/Reject 消息入队）并绑定到交换机
	if o.customerDeadLetter.errQueueDeclare.args == nil {
		o.customerDeadLetter.errQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchange.name,
			"x-dead-letter-routing-key": o.customerDeadLetter.deadRoutingKey,
		}
	}
	// 创建异常队列
	elq, err := channel.QueueDeclare(
		o.customerDeadLetter.errQueueName,
		o.customerDeadLetter.errQueueDeclare.durable,
		o.customerDeadLetter.errQueueDeclare.autoDelete,
		o.customerDeadLetter.errQueueDeclare.exclusive,
		o.customerDeadLetter.errQueueDeclare.noWait,
		o.customerDeadLetter.errQueueDeclare.args,
	)
	if err != nil {
		return err
	}
	// 绑定异常队列
	err = channel.QueueBind(
		elq.Name,
		o.customerDeadLetter.errRoutingKey,
		exchange.name,
		o.customerDeadLetter.errQueueBind.noWait,
		o.customerDeadLetter.errQueueBind.args,
	)
	if err != nil {
		return err
	}

	// 第四步：声明普通队列（正常消费消息入队）并绑定到交换机
	if o.customerDeadLetter.normalQueueDeclare.args == nil {
		o.customerDeadLetter.normalQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchange.name,
			"x-dead-letter-routing-key": o.customerDeadLetter.deadRoutingKey,
		}
	}
	// 创建普通队列
	nlq, err := channel.QueueDeclare(
		o.customerDeadLetter.normalQueueName,
		o.customerDeadLetter.normalQueueDeclare.durable,
		o.customerDeadLetter.normalQueueDeclare.autoDelete,
		o.customerDeadLetter.normalQueueDeclare.exclusive,
		o.customerDeadLetter.normalQueueDeclare.noWait,
		o.customerDeadLetter.normalQueueDeclare.args,
	)
	if err != nil {
		return err
	}
	// 绑定普通队列
	return channel.QueueBind(
		nlq.Name,
		o.customerDeadLetter.normalRoutingKey,
		exchange.name,
		o.customerDeadLetter.normalQueueDeclare.noWait,
		o.customerDeadLetter.normalQueueDeclare.args,
	)
}

// setupProducerStandardDeadLetter 设置生产者的标准死信队列，在 exchange 上声明两条消息路径。
// 参数:
//   - channel: AMQP 通道，用于声明交换机、队列和绑定
//   - exchange: 目标交换机配置
//   - o: 生产者配置选项，其中的 deadLetter 包含具体队列和路由参数
// 返回:
//   - error: 任意一步失败时返回错误（调用方负责关闭 channel）
//
// 消息流转路径：
//
//	TTL 超时 → deadQueue → deadRoutingKey → exchange
//	正常消费 → normalQueue → normalRoutingKey → exchange
func setupProducerStandardDeadLetter(channel *amqp.Channel, exchange *Exchange, o *producerOptions) error {
	// 第一步：声明主交换机
	err := channel.ExchangeDeclare(
		exchange.name,
		exchange.eType,
		o.deadLetter.exchangeDeclare.durable,
		o.deadLetter.exchangeDeclare.autoDelete,
		o.deadLetter.exchangeDeclare.internal,
		o.deadLetter.exchangeDeclare.noWait,
		o.deadLetter.exchangeDeclare.args,
	)
	if err != nil {
		return err
	}

	// 第二步：声明死信队列（TTL 超时消息入队）并绑定到交换机
	if o.deadLetter.deadQueueDeclare.args == nil {
		o.deadLetter.deadQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchange.name,
			"x-dead-letter-routing-key": o.deadLetter.normalRoutingKey,
			"x-message-ttl":             int32(600000), // 600秒后过期
		}
	}
	dlq, err := channel.QueueDeclare(
		o.deadLetter.deadQueueName,
		o.deadLetter.deadQueueDeclare.durable,
		o.deadLetter.deadQueueDeclare.autoDelete,
		o.deadLetter.deadQueueDeclare.exclusive,
		o.deadLetter.deadQueueDeclare.noWait,
		o.deadLetter.deadQueueDeclare.args,
	)
	if err != nil {
		return err
	}

	err = channel.QueueBind(
		dlq.Name,
		o.deadLetter.deadRoutingKey,
		exchange.name,
		o.deadLetter.deadQueueBind.noWait,
		o.deadLetter.deadQueueBind.args,
	)
	if err != nil {
		return err
	}

	// 第三步：声明普通队列（正常消费消息入队）并绑定到交换机
	if o.deadLetter.normalQueueDeclare.args == nil {
		o.deadLetter.normalQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchange.name,
			"x-dead-letter-routing-key": o.deadLetter.deadRoutingKey,
		}
	}
	nlq, err := channel.QueueDeclare(
		o.deadLetter.normalQueueName,
		o.deadLetter.normalQueueDeclare.durable,
		o.deadLetter.normalQueueDeclare.autoDelete,
		o.deadLetter.normalQueueDeclare.exclusive,
		o.deadLetter.normalQueueDeclare.noWait,
		o.deadLetter.normalQueueDeclare.args,
	)
	if err != nil {
		return err
	}

	return channel.QueueBind(
		nlq.Name,
		o.deadLetter.normalRoutingKey,
		exchange.name,
		o.deadLetter.normalQueueDeclare.noWait,
		o.deadLetter.normalQueueDeclare.args,
	)
}

// setupProducerNormalLetter 设置生产者的正常队列，在 exchange 上声明一条消息路径。
// 参数:
//   - channel: AMQP 通道，用于声明交换机、队列和绑定
//   - exchange: 目标交换机配置
//   - o: 生产者配置选项，其中的 normalLetter 包含队列和路由参数
// 返回:
//   - error: 任意一步失败时返回错误（调用方负责关闭 channel）
//
// 与 setupProducerCustomerDeadLetter 和 setupProducerStandardDeadLetter 不同，
// 此函数不涉及死信机制，仅包含交换机声明 + 队列声明 + 绑定的标准三步。
func setupProducerNormalLetter(channel *amqp.Channel, exchange *Exchange, o *producerOptions) error {
	// 第一步：声明主交换机
	err := channel.ExchangeDeclare(
		exchange.name,
		exchange.eType,
		o.normalLetter.exchangeDeclare.durable,
		o.normalLetter.exchangeDeclare.autoDelete,
		o.normalLetter.exchangeDeclare.internal,
		o.normalLetter.exchangeDeclare.noWait,
		o.normalLetter.exchangeDeclare.args,
	)
	if err != nil {
		return err
	}

	// 第二步：声明队列并绑定到交换机
	nlq, err := channel.QueueDeclare(
		o.normalLetter.normalQueueName,
		o.normalLetter.normalQueueDeclare.durable,
		o.normalLetter.normalQueueDeclare.autoDelete,
		o.normalLetter.normalQueueDeclare.exclusive,
		o.normalLetter.normalQueueDeclare.noWait,
		o.normalLetter.normalQueueDeclare.args,
	)
	if err != nil {
		return err
	}

	return channel.QueueBind(
		nlq.Name,
		o.normalLetter.normalRoutingKey,
		exchange.name,
		o.normalLetter.normalQueueDeclare.noWait,
		o.normalLetter.normalQueueDeclare.args,
	)
}

// PublishDirect 向 Direct 类型交换机发布消息。
// 参数:
//   - ctx: 上下文，用于链路追踪和超时控制
//   - routingKey: 路由键，决定消息投递到哪个队列
//   - body: 消息体
//   - messageID: 消息唯一标识，用于幂等消费和追踪
// 返回:
//   - error: 发布失败时返回 amqp 错误，成功返回 nil
func (p *Producer) PublishDirect(ctx context.Context, routingKey string, body []byte, messageID string) (err error) {
	return p.publish(ctx, exchangeTypeDirect, routingKey, body, messageID, nil)
}

// PublishFanout 向 Fanout 类型交换机发布消息（广播到所有绑定的队列，routingKey 被忽略）。
// 参数:
//   - ctx: 上下文，用于链路追踪和超时控制
//   - body: 消息体
//   - messageID: 消息唯一标识，用于幂等消费和追踪
// 返回:
//   - error: 发布失败时返回 amqp 错误，成功返回 nil
//
// 当 isDelay=true 时，实际使用死信队列的路由键，将消息路由到延迟队列。
func (p *Producer) PublishFanout(ctx context.Context, body []byte, messageID string) (err error) {
	routingKey := p.Exchange.routingKey
	if p.isDelay {
		routingKey = p.deadLetter.deadRoutingKey
	}
	return p.publish(ctx, exchangeTypeFanout, routingKey, body, messageID, nil,
		attribute.Bool("messaging.rabbitmq.is_delay", p.isDelay),
	)
}

// PublishTopic 向 Topic 类型交换机发布消息。
// 参数:
//   - ctx: 上下文，用于链路追踪和超时控制
//   - routingKey: 路由键，支持通配符匹配（* 匹配一级，# 匹配多级）
//   - body: 消息体
//   - messageID: 消息唯一标识，用于幂等消费和追踪
// 返回:
//   - error: 发布失败时返回 amqp 错误，成功返回 nil
func (p *Producer) PublishTopic(ctx context.Context, routingKey string, body []byte, messageID string) (err error) {
	return p.publish(ctx, exchangeTypeTopic, routingKey, body, messageID, nil)
}

// PublishHeaders 向 Headers 类型交换机发布消息（根据消息头属性匹配队列，忽略 routingKey）。
// 参数:
//   - ctx: 上下文，用于链路追踪和超时控制
//   - headersKeys: 消息头键值对，交换机根据 x-match（all/any）决定是否路由
//   - body: 消息体
//   - messageID: 消息唯一标识，用于幂等消费和追踪
// 返回:
//   - error: 发布失败时返回 amqp 错误，成功返回 nil
func (p *Producer) PublishHeaders(ctx context.Context, headersKeys map[string]interface{}, body []byte, messageID string) (err error) {
	return p.publish(ctx, exchangeTypeHeaders, p.Exchange.routingKey, body, messageID, headersKeys,
		attribute.Int("messaging.rabbitmq.headers_count", len(headersKeys)),
	)
}

// publish 通用消息发布核心逻辑，被 PublishDirect/PublishFanout/PublishTopic/PublishHeaders 调用。
// 负责完整生命周期：创建 span → 校验交换机类型 → 注入 Trace 传播头 → 发布 → 记录结果。
// 参数:
//   - ctx: 上下文，用于链路追踪和超时控制
//   - exchangeType: 交换机类型常量（如 exchangeTypeDirect），用于校验和 span 属性
//   - routingKey: 路由键
//   - body: 消息体
//   - messageID: 消息唯一标识
//   - extraHeaders: 额外消息头（PublishHeaders 使用合并用户自定义 headers，其余传 nil）
//   - extraAttrs: 额外 span 属性，由各 Publish* 方法传入类型专属属性
// 返回:
//   - error: 发布失败时记录 span error 并返回 amqp 错误，成功返回 nil
func (p *Producer) publish(ctx context.Context, exchangeType, routingKey string, body []byte, messageID string, extraHeaders map[string]interface{}, extraAttrs ...attribute.KeyValue) (err error) {
	spanName := fmt.Sprintf("rabbitmq.publish.%s.%s", exchangeType, p.Exchange.name)
	ctx, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	startTime := time.Now()

	// 提取 Context 中的 RequestID，关联业务日志和 Trace
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
		attribute.String("messaging.rabbitmq.exchange.type", exchangeType),
		attribute.Int("messaging.message.body.size", len(body)),
		attribute.String("messaging.message.id", messageID),
		attribute.Int("messaging.rabbitmq.delivery_mode", int(p.deliveryMode())),
		attribute.Bool("messaging.rabbitmq.mandatory", p.mandatory),
	)
	span.SetAttributes(extraAttrs...)

	// 校验交换机类型
	if p.Exchange.eType != exchangeType {
		err = fmt.Errorf("invalid exchange type (%s), only supports %s type", p.Exchange.eType, exchangeType)
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
			attribute.Int("delivery_mode", int(p.deliveryMode())),
			attribute.Bool("mandatory", p.mandatory),
		))

	// 注入 Trace Context 到消息头
	headersMap := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headersMap))
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			headersMap[string(logger.ContextKeyRequestID)] = reqIDStr
		}
	}

	headers := amqp.Table{}
	for k, v := range headersMap {
		headers[k] = v
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	err = p.mqChannel.PublishWithContext(
		ctx,
		p.Exchange.name,
		routingKey,
		p.mandatory,
		false,
		amqp.Publishing{
			DeliveryMode: p.deliveryMode(),
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
		errorMsg := fmt.Sprintf("publish failed: %v | exchange=%s | routing_key=%s | exchange_type=%s | message_id=%s | body_size=%d",
			err, p.Exchange.name, routingKey, exchangeType, messageID, len(body))
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", fmt.Sprintf("%T", err)),
				attribute.String("error.context", "publish-"+exchangeType+"-failed"),
				attribute.String("error.exchange", p.Exchange.name),
				attribute.String("error.routing_key", routingKey),
				attribute.String("error.exchange_type", exchangeType),
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
		logger.WarnWithCtx(ctx, "[rabbitmq producer] publish "+exchangeType+" failed",
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

// Close 关闭生产者通道
// 返回可能的错误
func (p *Producer) Close() error {
	return p.mqChannel.Close()
}
