package gorabbitmq

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// ConsumerOption 消费者选项配置函数类型
type ConsumerOption func(*consumerOptions)

// consumerOptions 消费者配置选项
type consumerOptions struct {
	customerDeadLetter *CustomerDeadLetterOptions // 自定义死信队列选项
	normalLetter       *NormalLetterOptions       // 正常队列选项
	deadLetter         *DeadLetterOptions         // 死信队列选项
	qos                *qosOptions                // QoS选项
	consume            *consumeOptions            // 消费选项

	msgDurable bool   // 消息是否持久化
	isAutoAck  bool   // 是否自动确认消息
	name       string // 消费者名称，用于 trace span
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
		customerDeadLetter: defaultCustomerDeadLetterOptions(),
		normalLetter:       defaultNormalLetterOptions(),
		deadLetter:         defaultDeadLetterOptions(),
		qos:                defaultQosOptions(),
		consume:            defaultConsumeOptions(),
		msgDurable:         true,
		isAutoAck:          true,
		name:               "",
	}
}

// WithConsumerCustomerDeadLetterOptions set dead letter options.
func WithConsumerCustomerDeadLetterOptions(opts ...CustomerDeadLetterOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.customerDeadLetter.apply(opts...)
	}
}

// WithConsumerNormalLetterOptions set dead letter options.
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

// WithConsumerName 设置消费者名称，用于 trace span
func WithConsumerName(name string) ConsumerOption {
	return func(o *consumerOptions) {
		o.name = name
	}
}

// -------------------------------------------------------------------------------------------

// Consumer 消费者会话
type Consumer struct {
	exchange  *Exchange     // 交换机
	QueueName string        // 队列名称
	conn      *Connection   // 连接
	ch        *amqp.Channel // 通道

	customerDeadLetter *CustomerDeadLetterOptions // 自定义死信队列选项
	normalLetter       *NormalLetterOptions
	deadLetter         *DeadLetterOptions
	qosOption          *qosOptions     // QoS选项
	consumeOption      *consumeOptions // 消费选项

	msgDurable bool           // 消息是否持久化
	isAutoAck  bool           // 是否自动确认
	mu         sync.RWMutex   // 读写锁，保护所有字段访问
	wg         sync.WaitGroup // 等待组，用于等待正在处理的消息完成

	tracer    trace.Tracer // OpenTelemetry tracer for reuse
	closeOnce sync.Once    // 新增：确保关闭操作只执行一次
	name      string       // 消费者名称，用于 trace span
}

// Handler 消息处理函数类型
type Handler func(ctx context.Context, data []byte, messageId string, tagID string) error

// NewConsumer 创建一个消费者
func NewConsumer(exchange *Exchange, queueName string, conn *Connection, opts ...ConsumerOption) (*Consumer, error) {
	o := defaultConsumerOptions()
	o.apply(opts...)
	c := &Consumer{
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
		name:       o.name,

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
	// 添加 QoS 设置
	if c.qosOption.enable {
		err = c.ch.Qos(
			c.qosOption.prefetchCount,
			c.qosOption.prefetchSize,
			c.qosOption.global,
		)
		if err != nil {
			_ = channel.Close()
			return err
		}
	}
	//--------------------------------自定义死信队列队列----------------------------------------------------
	if c.customerDeadLetter.exchangeName != "sunshine" {
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
		reconnectInterval := time.Second * 2
		ticker := time.NewTicker(reconnectInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !c.conn.CheckConnected(ctx) {
				logger.WarnWithCtx(ctx, "[rabbitmq consumer] connection not ready, retrying...",
					logger.String("queue", c.QueueName))
				if !c.waitRetry(ctx, ticker) {
					return
				}
				continue
			}
			if err := c.initialize(); err != nil {
				logger.WarnWithCtx(ctx, "[rabbitmq consumer] initialize consumer error",
					logger.Err(err),
					logger.String("queue", c.QueueName))
				if !c.waitRetry(ctx, ticker) {
					return
				}
				continue
			}
			delivery, err := c.consumeWithContext(ctx)
			if err != nil {
				logger.WarnWithCtx(ctx, "[rabbitmq consumer] execution of consumption error",
					logger.Err(err),
					logger.String("queue", c.QueueName))
				c.safeChannelClose()
				if !c.waitRetry(ctx, ticker) {
					return
				}
				continue
			}
			shouldRetry := c.processMessages(ctx, delivery, handler)
			c.safeChannelClose()

			if !shouldRetry {
				return
			}
		}
	}()
}

// waitRetry 抽取统一的等待退出逻辑
func (c *Consumer) waitRetry(ctx context.Context, ticker *time.Ticker) bool {
	select {
	case <-ctx.Done():
		return false
	case <-c.conn.exit:
		c.Close()
		return false
	case <-ticker.C:
		return true
	}
}

// processMessages 处理从 RabbitMQ 接收到的消息流
func (c *Consumer) processMessages(ctx context.Context, delivery <-chan amqp.Delivery, handler Handler) bool {
	for {
		select {
		case <-ctx.Done():
			logger.WarnWithCtx(ctx, "[rabbitmq consumer] context done, stopping processMessages",
				logger.String("queue", c.QueueName))
			return false
		case <-c.conn.exit:
			c.Close()
			return false
		case d, ok := <-delivery:
			if !ok {
				logger.WarnWithCtx(ctx, "[rabbitmq consumer] delivery channel closed",
					logger.String("queue", c.QueueName))
				return true
			}
			c.handleSingleMessage(context.Background(), d, handler)
		}
	}
}

// handleSingleMessage 处理单条消息的逻辑封装（包含 Trace 和 Ack）
func (c *Consumer) handleSingleMessage(ctx context.Context, d amqp.Delivery, handler Handler) {
	select {
	case <-ctx.Done():
		logger.WarnWithCtx(ctx, "Context已取消，丢弃当前消息处理",
			logger.Uint64("tag", d.DeliveryTag))
		_ = d.Reject(true)
		return
	default:
	}
	c.wg.Add(1)
	defer c.wg.Done()

	headersMap := make(map[string]string)
	for k, v := range d.Headers {
		if strVal, ok := v.(string); ok {
			headersMap[k] = strVal
		}
	}

	extractedCtx := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(headersMap))

	spanName := c.name
	if spanName == "" {
		spanName = "rabbitmq.consume"
	}

	msgCtx, span := c.tracer.Start(extractedCtx, spanName, trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()

	var reqIDStr string
	// 从 RabbitMQ headers 中读取 request_id（headers key 必须是 string 类型）
	if reqIDVal, ok := d.Headers[string(logger.ContextKeyRequestID)].(string); ok && reqIDVal != "" {
		reqIDStr = reqIDVal
		span.SetAttributes(attribute.String("request_id", reqIDStr))
		// 注入到 context 时使用 logger 统一的 ContextKey 类型，确保 logger 能正确提取
		msgCtx = context.WithValue(msgCtx, logger.ContextKeyForRequestID(), reqIDStr)
	}

	tagID := strings.Join([]string{d.Exchange, c.QueueName, strconv.FormatUint(d.DeliveryTag, 10)}, "/")
	span.SetAttributes(
		attribute.String("messaging.system", "rabbitmq"),
		attribute.String("messaging.destination", c.QueueName),
		attribute.String("messaging.destination_kind", "queue"),
		attribute.String("messaging.rabbitmq.routing_key", d.RoutingKey),
		attribute.String("messaging.message_id", d.MessageId),
		attribute.Int("messaging.message_payload_size_bytes", len(d.Body)),
		attribute.String("messaging.rabbitmq.delivery_tag", strconv.FormatUint(d.DeliveryTag, 10)),
		attribute.String("messaging.operation", "process"),
	)

	if traceIDStr, ok := d.Headers["x-trace-id"].(string); ok && traceIDStr != "" {
		span.SetAttributes(
			attribute.String("messaging.rabbitmq.producer_trace_id", traceIDStr),
		)
		if spanIDStr, ok := d.Headers["x-span-id"].(string); ok && spanIDStr != "" {
			span.SetAttributes(
				attribute.String("messaging.rabbitmq.producer_span_id", spanIDStr),
			)
		}
	}

	span.AddEvent("message received")

	err := handler(msgCtx, d.Body, d.MessageId, tagID)

	if c.isAutoAck {
		if err != nil {
			errorMsg := fmt.Sprintf("handler execution failed: %v | queue=%s | message_id=%s | delivery_tag=%d | routing_key=%s",
				err, c.QueueName, d.MessageId, d.DeliveryTag, d.RoutingKey)
			span.RecordError(err,
				trace.WithAttributes(
					attribute.String("error.type", fmt.Sprintf("%T", err)),
					attribute.String("error.context", "auto-ack-mode"),
				),
			)
			span.SetStatus(codes.Error, errorMsg)
			span.AddEvent("handler error occurred",
				trace.WithAttributes(
					attribute.String("error.message", err.Error()),
					attribute.String("queue", c.QueueName),
					attribute.String("message_id", d.MessageId),
				),
			)
		} else {
			span.AddEvent("message processed successfully (auto-ack)")
		}
		return
	}

	if err != nil {
		errorMsg := fmt.Sprintf("handler execution failed: %v | queue=%s | message_id=%s | delivery_tag=%d | routing_key=%s",
			err, c.QueueName, d.MessageId, d.DeliveryTag, d.RoutingKey)
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", fmt.Sprintf("%T", err)),
				attribute.String("error.context", "manual-ack-mode"),
				attribute.String("error.action", "reject-and-requeue"),
			),
		)
		span.SetStatus(codes.Error, errorMsg)
		span.AddEvent("handler error occurred",
			trace.WithAttributes(
				attribute.String("error.message", err.Error()),
				attribute.String("queue", c.QueueName),
				attribute.String("message_id", d.MessageId),
				attribute.Bool("requeue", false),
			),
		)
		if rejectErr := d.Reject(false); rejectErr != nil {
			span.RecordError(rejectErr,
				trace.WithAttributes(
					attribute.String("error.type", "reject-error"),
					attribute.String("error.context", "manual-reject-failed"),
				),
			)
			span.AddEvent("message reject failed",
				trace.WithAttributes(
					attribute.String("error.message", rejectErr.Error()),
					attribute.String("tagID", tagID),
				),
			)
			logger.WarnWithCtx(ctx, "[rabbitmq consumer] manual Reject error",
				logger.Err(rejectErr),
				logger.String("tagID", tagID),
				logger.String("queue", c.QueueName),
				logger.String("message_id", d.MessageId))
		} else {
			span.AddEvent("message rejected and requeued (requeue=false)")
		}
		return
	}

	if ackErr := d.Ack(false); ackErr != nil {
		span.RecordError(ackErr,
			trace.WithAttributes(
				attribute.String("error.type", "ack-error"),
				attribute.String("error.context", "manual-ack-failed"),
				attribute.String("error.impact", "message-will-be-requeued-by-rabbitmq"),
			),
		)
		span.AddEvent("acknowledgment failed",
			trace.WithAttributes(
				attribute.String("error.message", ackErr.Error()),
				attribute.String("queue", c.QueueName),
				attribute.String("message_id", d.MessageId),
				attribute.String("error.description", fmt.Sprintf("acknowledgment failed: %v | queue=%s | message_id=%s | delivery_tag=%d",
					ackErr, c.QueueName, d.MessageId, d.DeliveryTag)),
			),
		)
		logger.WarnWithCtx(ctx, "[rabbitmq consumer] manual ack error",
			logger.Err(ackErr),
			logger.String("tagID", tagID),
			logger.String("queue", c.QueueName),
			logger.String("message_id", d.MessageId))
	} else {
		span.AddEvent("message acknowledged successfully")
	}
}

// 辅助方法：安全关闭当前 Channel
func (c *Consumer) safeChannelClose() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ch != nil {
		_ = c.ch.Close()
		c.ch = nil
	}
}

// Close 关闭消费者
func (c *Consumer) Close() {
	c.closeOnce.Do(func() {
		c.wg.Wait()
		if c.ch != nil {
			_ = c.ch.Close()
			c.ch = nil
		}
		logger.InfoWithCtx(context.Background(), "[rabbitmq consumer] 资源已释放",
			logger.String("queue", c.QueueName))
	})
}

// getHeadersKeys 获取消息头的键列表（用于调试）
func getHeadersKeys(headers amqp.Table) []string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	return keys
}
