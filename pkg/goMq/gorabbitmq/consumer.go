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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
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
		logger:             defaultLogger,
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

// WithConsumerName 设置消费者名称，用于 trace span
func WithConsumerName(name string) ConsumerOption {
	return func(o *consumerOptions) {
		o.name = name
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
		// 1. 使用固定的重试间隔 ticker，避免在循环内频繁创建/停止
		reconnectInterval := time.Second * 2
		ticker := time.NewTicker(reconnectInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			// 2. 检查连接状态
			if !c.conn.CheckConnected(ctx) {
				c.zapLog.Warn("[rabbitmq consumer] connection not ready, retrying...", zap.String("queue", c.QueueName))
				if !c.waitRetry(ctx, ticker) {
					return
				}
				continue
			}
			// 3. 初始化资源 (Declare & Bind)
			if err := c.initialize(); err != nil {
				c.zapLog.Warn("[rabbitmq consumer] initialize consumer error", zap.String("err", err.Error()), zap.String("queue", c.QueueName))
				// 初始化失败通常需要等待，防止 CPU 空转
				if !c.waitRetry(ctx, ticker) {
					return
				}
				continue
			}
			// 4. 获取消费 Channel (chan amqp.Delivery)
			delivery, err := c.consumeWithContext(ctx)
			if err != nil {
				c.zapLog.Warn("[rabbitmq consumer] execution of consumption error", zap.String("err", err.Error()), zap.String("queue", c.QueueName))
				c.safeChannelClose()
				if !c.waitRetry(ctx, ticker) {
					return
				}
				continue
			}
			// 5.进入阻塞监听循环
			// 如果返回 true，说明是连接断开导致的退出，循环会继续执行重连逻辑
			// 如果返回 false，说明是 Context 取消或显式退出，协程结束
			shouldRetry := c.processMessages(ctx, delivery, handler)
			// 6. 循环结束清理本轮资源
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
// 如果返回 true，说明是连接断开导致的退出，循环会继续执行重连逻辑
// 如果返回 false，说明是 Context 取消或显式退出，协程结束
func (c *Consumer) processMessages(ctx context.Context, delivery <-chan amqp.Delivery, handler Handler) bool {
	for {
		select {
		case <-ctx.Done():
			c.zapLog.Warn("[rabbitmq consumer] context done, stopping processMessages", zap.String("queue", c.QueueName))
			return false // 显式停止，不重试
		case <-c.conn.exit:
			c.Close()
			return false // 全局退出，不需要继续重连消费
		case d, ok := <-delivery:
			if !ok {
				c.zapLog.Warn("[rabbitmq consumer] delivery channel closed, queue=" + c.QueueName)
				return true // 通道断开，返回 true 告知外层需要触发重连逻辑
			}
			c.handleSingleMessage(context.Background(), d, handler)
		}
	}
}

// handleSingleMessage 处理单条消息的逻辑封装（包含 Trace 和 Ack）
func (c *Consumer) handleSingleMessage(ctx context.Context, d amqp.Delivery, handler Handler) {
	// 1. 预检查：如果系统已经发出停止信号，直接将消息塞回队列，不启动业务处理
	select {
	case <-ctx.Done():
		c.zapLog.Warn("Context已取消，丢弃当前消息处理", zap.Uint64("tag", d.DeliveryTag))
		_ = d.Reject(true) // requeue=true，让其他节点消费
		return
	default:
		// 执行原有 handler 逻辑...
	}
	c.wg.Add(1)
	defer c.wg.Done()

	// 2. 从消息头提取 Trace Context（大厂标准做法）
	// 将 amqp.Table 转换为 map[string]string 以适配 Propagator
	headersMap := make(map[string]string)
	for k, v := range d.Headers {
		if strVal, ok := v.(string); ok {
			headersMap[k] = strVal
		}
	}

	// 3. 从 Header 提取 Trace Context（关键：用于保持 TraceId 一致）
	extractedCtx := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(headersMap))

	// 4. 开始 Trace Span（使用提取的 Context 作为 Parent，确保 TraceId 一致）
	spanName := c.name
	if spanName == "" {
		spanName = "rabbitmq.consume"
	}

	// 使用 extractedCtx 作为 Parent Context，而不是 context.Background()
	// 这样 Consumer Span 与 Producer Span 共享同一个 TraceId
	msgCtx, span := c.tracer.Start(extractedCtx, spanName, trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()

	// 从消息 Header 中提取 request_id（大厂标准：关联业务日志和 Trace）
	var reqIDStr string
	if reqIDVal, ok := d.Headers["request_id"].(string); ok && reqIDVal != "" {
		reqIDStr = reqIDVal
		span.SetAttributes(attribute.String("request_id", reqIDStr))
		// 将 request_id 注入到 Context，供下游组件（Redis/MySQL）使用
		msgCtx = context.WithValue(msgCtx, "request_id", reqIDStr)
	}

	// 设置语义化属性（遵循 OpenTelemetry Messaging Semantic Conventions）
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

	// 如果消息携带了 Trace 信息，记录为链接关系
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

	// 添加事件标记
	span.AddEvent("message received")

	// 3. 将集成了【系统退出信号】+【Trace信息】的 msgCtx 传给业务 handler
	// 业务代码内部如果调用了 DB 或 HTTP 请求，应使用这个 msgCtx
	err := handler(msgCtx, d.Body, d.MessageId, tagID)

	// 4. 自动确认模式直接返回
	if c.isAutoAck {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "handler error (auto-ack mode)")
		} else {
			span.AddEvent("message processed successfully (auto-ack)")
		}
		return
	}

	// 5. 手动确认模式逻辑
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "handler error")
		//如果设置为 true，则将消息重新排队，以便稍后再次尝试处理。
		//如果设置为 false，则将消息从队列中移除，不再重新排队
		// 这样即使程序崩溃，消息也会回到队列
		if rejectErr := d.Reject(false); rejectErr != nil {
			span.RecordError(rejectErr)
			c.zapLog.Warn("[rabbitmq consumer] manual Reject error",
				zap.String("err", rejectErr.Error()),
				zap.String("tagID", tagID))
		} else {
			span.AddEvent("message rejected and requeued")
		}
		return
	}

	// 6. 成功处理，尝试 Ack
	if ackErr := d.Ack(false); ackErr != nil {
		// 如果此时连接已关，Ack 会失败
		// 此时不必惊慌，因为没 Ack 成功，RabbitMQ 会在连接断开后将消息重新放回队列
		// 保证了“不丢失”，但下次消费时需要处理“幂等性”
		span.RecordError(ackErr)
		c.zapLog.Warn("[rabbitmq consumer] manual ack error",
			zap.String("err", ackErr.Error()),
			zap.String("tagID", tagID))
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
		// 1. 等待此消费者实例正在处理的消息完成
		c.wg.Wait()
		// 2. 关闭通道
		if c.ch != nil {
			// 避免重复关闭报错
			_ = c.ch.Close()
			c.ch = nil
		}
		c.zapLog.Info("[rabbitmq consumer] 资源已释放", zap.String("queue", c.QueueName))
	})
	// 注意：Connection 的关闭由外部 Pool 或 BaseConsumer 管理
}

// getHeadersKeys 获取消息头的键列表（用于调试）
func getHeadersKeys(headers amqp.Table) []string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	return keys
}
