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

	"github.com/18721889353/sunshine/pkg/logger"
)

// Consumer 消费者会话
type Consumer struct {
	exchange         *Exchange      // 交换机
	QueueName        string         // 队列名称
	*consumerOptions                // 嵌入消费者配置（消除字段重复）
	conn             *Connection    // 连接
	ch               *amqp.Channel  // 通道
	mu               sync.RWMutex   // 读写锁，保护所有字段访问
	wg               sync.WaitGroup // 等待组，用于等待正在处理的消息完成
	tracer           trace.Tracer   // OpenTelemetry tracer for reuse
	closeOnce        sync.Once      // 确保关闭操作只执行一次
}

// Handler 消息处理函数类型
type Handler func(ctx context.Context, data []byte, messageId string, tagID string) error

// NewConsumer 创建一个消费者
func NewConsumer(exchange *Exchange, queueName string, conn *Connection, opts ...ConsumerOption) (*Consumer, error) {
	// 前置校验
	if exchange == nil {
		return nil, fmt.Errorf("exchange cannot be nil")
	}
	if conn == nil {
		return nil, fmt.Errorf("connection cannot be nil")
	}
	if queueName == "" {
		return nil, fmt.Errorf("queue name cannot be empty")
	}

	// 校验交换机配置
	if err := exchange.Validate(); err != nil {
		return nil, err
	}

	o := defaultConsumerOptions()
	o.apply(opts...)

	c := &Consumer{
		exchange:        exchange,
		QueueName:       queueName,
		conn:            conn,
		consumerOptions: o,
		tracer:          otel.Tracer("gomq"), // 初始化 tracer
	}

	return c, nil
}

// validateQueueConfig 验证队列配置合法性
func (c *Consumer) validateQueueConfig() error {
	if c.customerDeadLetter.exchangeName != defaultExchangeName && c.normalLetter.exchangeName != defaultExchangeName {
		return fmt.Errorf("cannot set both customerDeadLetter and normalLetter")
	}
	if c.customerDeadLetter.exchangeName != defaultExchangeName && c.deadLetter.exchangeName != defaultExchangeName {
		return fmt.Errorf("cannot set both customerDeadLetter and deadLetter")
	}
	if c.normalLetter.exchangeName != defaultExchangeName && c.deadLetter.exchangeName != defaultExchangeName {
		return fmt.Errorf("cannot set both normalLetter and deadLetter")
	}
	return nil
}

// setupQoS 设置消费者的 QoS（服务质量）参数。
// 若 QoS 未启用则直接返回，不进行任何设置。
// 设置失败时会关闭传入的 channel 以避免资源泄漏。
//
// 参数:
//   - ctx: 上下文，用于日志记录
//   - channel: 待设置 QoS 的 AMQP 通道
//
// 返回值:
//   - error: 设置成功或 QoS 未启用时返回 nil，否则返回错误信息
func (c *Consumer) setupQoS(ctx context.Context, channel *amqp.Channel) error {
	if !c.qos.enable {
		return nil
	}

	// 设置 QoS：prefetchCount（预取消息数量）、prefetchSize（预取消息大小）、global（是否全局生效）
	err := channel.Qos(
		c.qos.prefetchCount,
		c.qos.prefetchSize,
		c.qos.global,
	)
	if err != nil {
		logger.ErrorWithCtx(ctx, "设置QoS失败", logger.Err(err))
		if closeErr := channel.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "关闭channel失败", logger.Err(closeErr))
		}
		return err
	}
	return nil
}

// setupQueues 按优先级依次设置消费者队列。
// 优先级顺序：自定义死信队列 → 标准死信队列 → 正常队列，三种模式互斥。
// 配置项通过 `consumerOptions` 中对应的 `exchangeName` 是否等于默认值来判断是否启用。
//
// 参数:
//   - channel: AMQP 通道，用于声明交换机、队列和绑定
//
// 返回值:
//   - error: 任一队列设置失败时返回错误，成功返回 nil
func (c *Consumer) setupQueues(channel *amqp.Channel) error {
	// 处理自定义死信队列
	if c.customerDeadLetter.exchangeName != defaultExchangeName {
		if err := c.setupCustomerDeadLetter(channel); err != nil {
			return err
		}
	}

	// 处理标准死信队列
	if c.deadLetter.exchangeName != defaultExchangeName {
		if err := c.setupStandardDeadLetter(channel); err != nil {
			return err
		}
	}

	// 处理正常队列
	if c.normalLetter.exchangeName != defaultExchangeName {
		if err := c.setupNormalLetter(channel); err != nil {
			return err
		}
	}

	return nil
}

// setupChannel 创建 AMQP 通道并完成消费者会话配置。
// 依次执行：获取可用连接 → 创建通道 → 验证队列配置 → 设置 QoS → 声明并绑定队列。
// 任一步骤失败时立即返回错误，确保不会继续执行后续操作。
//
// 参数:
//   - ctx: 上下文，用于日志追踪
//
// 返回值:
//   - error: 初始化成功返回 nil，否则返回具体错误信息
func (c *Consumer) setupChannel(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 创建一个新的通道
	amqpConn := c.conn.GetConn(ctx)
	if amqpConn == nil {
		return fmt.Errorf("amqp connection is not available")
	}
	channel, err := amqpConn.Channel()
	if err != nil {
		return err
	}
	c.ch = channel

	// 以下步骤失败时清理已创建的 channel
	defer func() {
		if err != nil {
			if closeErr := channel.Close(); closeErr != nil {
				logger.WarnWithCtx(ctx, "关闭初始化失败的 channel 出错", logger.Err(closeErr))
			}
			c.ch = nil
		}
	}()

	// 验证队列配置
	if err = c.validateQueueConfig(); err != nil {
		return err
	}

	// 设置 QoS
	if err = c.setupQoS(ctx, channel); err != nil {
		return err
	}

	// 设置所有队列
	return c.setupQueues(channel)
}

// setupCustomerDeadLetter 设置自定义死信队列，委托共享函数执行。
func (c *Consumer) setupCustomerDeadLetter(channel *amqp.Channel) error {
	return setupCustomerDeadLetterDeclare(channel, c.exchange.name, c.exchange.eType, c.customerDeadLetter)
}

// setupStandardDeadLetter 设置标准死信队列，委托共享函数执行。
func (c *Consumer) setupStandardDeadLetter(channel *amqp.Channel) error {
	return setupStandardDeadLetterDeclare(channel, c.exchange.name, c.exchange.eType, c.deadLetter)
}

// setupNormalLetter 设置正常队列，委托共享函数执行。
func (c *Consumer) setupNormalLetter(channel *amqp.Channel) error {
	return setupNormalLetterDeclare(channel, c.exchange.name, c.exchange.eType, c.normalLetter)
}

// subscribe 向 RabbitMQ 注册消费者订阅并返回消息投递通道。
// 封装对 amqp.Channel.ConsumeWithContext 的调用。
//
// 参数:
//   - ctx: 上下文，取消时取消订阅并关闭 delivery 通道
//
// 返回值:
//   - <-chan amqp.Delivery: 消息投递通道，用于读取 RabbitMQ 推送的消息
//   - error: 注册订阅失败时返回错误
func (c *Consumer) subscribe(ctx context.Context) (<-chan amqp.Delivery, error) {
	return c.ch.ConsumeWithContext(
		ctx,
		c.QueueName,
		c.consume.consumer,
		c.isAutoAck,
		c.consume.exclusive,
		c.consume.noLocal,
		c.consume.noWait,
		c.consume.args,
	)
}

// Consume 在后台 goroutine 中循环消费 RabbitMQ 消息，支持自动重连和重试。
//
// 参数:
//   - ctx: 上下文，用于控制消费循环的生命周期，取消 ctx 会退出消费循环
//   - handler: 消息处理回调，接收消息上下文、消息体、消息 ID 和标签 ID
//
// 行为说明:
//   - 启动独立的 goroutine 执行消费循环
//   - 连接未就绪或初始化失败时自动重试（间隔 2 秒）
//   - subscribe 失败时关闭当前 channel 后重试
//   - processMessages 返回 false 时退出循环（如 ctx 取消或连接关闭）
func (c *Consumer) Consume(ctx context.Context, handler Handler) {
	go func() {
		// panic 保护，防止单个消费者崩溃导致整个进程退出
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorWithCtx(ctx, "[rabbitmq consumer] consumer goroutine panicked",
					logger.Any("panic", r),
					logger.String("queue", c.QueueName))
			}
		}()

		ticker := time.NewTicker(time.Second * 2) // 2秒重试间隔
		defer ticker.Stop()

		// 消费主循环：初始化 → 消费 → 处理消息 → 清理
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// 检查连接是否就绪
			if !c.conn.CheckConnected(ctx) {
				logger.WarnWithCtx(ctx, "[rabbitmq consumer] connection not ready, retrying...",
					logger.String("queue", c.QueueName))
				if !c.waitRetry(ctx, ticker) {
					return
				}
				continue
			}

			// 初始化消费者会话（创建 channel、设置 QoS、声明队列）
			if err := c.setupChannel(ctx); err != nil {
				logger.WarnWithCtx(ctx, "[rabbitmq consumer] setupChannel consumer error",
					logger.Err(err),
					logger.String("queue", c.QueueName))
				if !c.waitRetry(ctx, ticker) {
					return
				}
				continue
			}

			// 订阅队列获取消息投递通道
			delivery, err := c.subscribe(ctx)
			if err != nil {
				logger.WarnWithCtx(ctx, "[rabbitmq consumer] subscribe error",
					logger.Err(err),
					logger.String("queue", c.QueueName))
				c.safeChannelClose()
				if !c.waitRetry(ctx, ticker) {
					return
				}
				continue
			}

			// 持续处理消息流直到结束或出错
			shouldRetry := c.processMessages(ctx, delivery, handler)
			c.safeChannelClose()

			if !shouldRetry {
				return
			}

			// delivery 通道关闭，等待重试间隔后重新消费
			if !c.waitRetry(ctx, ticker) {
				return
			}
		}
	}()
}

// waitRetry 等待重试间隔或退出信号。
// 在重试前等待 ticker 触发，同时监听 ctx 取消和连接关闭信号，任一信号到达则退出等待。
//
// 参数:
//   - ctx: 上下文，取消时返回 false 表示不再重试
//   - ticker: 定时器，触发时返回 true 表示可以重试
//
// 返回值:
//   - bool: true 表示可以重试，false 表示应退出消费循环
func (c *Consumer) waitRetry(ctx context.Context, ticker *time.Ticker) bool {
	select {
	case <-ctx.Done():
		return false
	case <-c.conn.Done():
		c.Close()
		return false
	case <-ticker.C:
		return true
	}
}

// processMessages 循环处理消息投递通道中的消息，直到上下文取消、连接关闭或 delivery 通道关闭。
// 通过 select 多路复用监听三个事件，优先响应退出信号。delivery 通道关闭时返回 true
// 通知调用方可重试（重新创建 channel 继续消费），其他情况返回 false 表示应退出消费循环。
//
// 参数:
//   - ctx: 上下文，取消时停止消息处理并返回 false
//   - delivery: RabbitMQ 消息投递通道，从中读取消息
//   - handler: 用户自定义消息处理回调
//
// 返回值:
//   - bool: true 表示可重试（delivery 通道关闭），false 表示应退出（ctx 取消或连接关闭）
func (c *Consumer) processMessages(ctx context.Context, delivery <-chan amqp.Delivery, handler Handler) bool {
	for {
		select {
		case <-ctx.Done():
			logger.WarnWithCtx(ctx, "[rabbitmq consumer] context done, stopping processMessages",
				logger.Err(ctx.Err()),
				logger.String("queue", c.QueueName))
			return false
		case <-c.conn.Done():
			c.Close()
			return false
		case d, ok := <-delivery:
			if !ok {
				logger.WarnWithCtx(ctx, "[rabbitmq consumer] delivery channel closed",
					logger.String("queue", c.QueueName))
				return true
			}
			// 消息已从队列取出，隔离上游取消信号，保证消息被完整处理
			c.handleSingleMessage(context.WithoutCancel(ctx), d, handler)
		}
	}
}

// handleSingleMessage 处理单条 AMQP 消息的完整生命周期，包括链路追踪、消息头传播、
// 请求 ID 提取以及消息确认（Ack/Reject）。
//
// 参数:
//   - ctx: 上层上下文，用于 OpenTelemetry 传播和日志追踪。
//   - d: RabbitMQ 投递的消息体，包含消息头、路由键、投递标签等信息。
//   - handler: 用户定义的消息处理回调，接收消息上下文、消息体、消息 ID 和标识标签。
//
// 处理流程:
//  1. 从消息头提取 OpenTelemetry 传播上下文，启动消费 Span 并注入请求 ID。
//  2. 执行用户 handler。
//  3. 根据消费模式（自动/手动 Ack）执行不同的确认逻辑。
func (c *Consumer) handleSingleMessage(ctx context.Context, d amqp.Delivery, handler Handler) {
	c.wg.Add(1)
	defer c.wg.Done()

	// 从消息 Header 中提取 OpenTelemetry 传播上下文，恢复链路追踪
	headersMap := make(map[string]string)
	for k, v := range d.Headers {
		if strVal, ok := v.(string); ok {
			headersMap[k] = strVal
		}
	}
	extractedCtx := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(headersMap))

	// 启动消费 Span，继承生产者链路
	spanName := c.name
	if spanName == "" {
		spanName = "rabbitmq.consume"
	}
	msgCtx, span := c.tracer.Start(extractedCtx, spanName, trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()

	// 从消息头提取 request_id 并注入到消息上下文，确保日志追踪可关联
	if reqIDVal, ok := d.Headers[string(logger.ContextKeyRequestID)].(string); ok && reqIDVal != "" {
		span.SetAttributes(attribute.String(string(logger.ContextKeyForRequestID()), reqIDVal))
		msgCtx = context.WithValue(msgCtx, logger.ContextKeyForRequestID(), reqIDVal)
	}

	// 构造消息唯一标识（Exchange/Queue/DeliveryTag），标记消息维度的 Span 属性
	tagID := strings.Join([]string{d.Exchange, c.QueueName, strconv.FormatUint(d.DeliveryTag, 10)}, "/")
	span.SetAttributes(
		attribute.String("messaging.system", "rabbitmq"),
		attribute.String("messaging.destination", c.QueueName),
		attribute.String("messaging.destination_kind", "queue"),
		attribute.String("messaging.rabbitmq.routing_key", d.RoutingKey),
		attribute.String("messaging.message_id", d.MessageId),
		attribute.String("messaging.rabbitmq.delivery_tag", strconv.FormatUint(d.DeliveryTag, 10)),
		attribute.String("messaging.operation", "process"),
	)

	// 关联生产者侧 Trace 信息，串联生产-消费完整调用链
	if traceIDStr, ok := d.Headers["x-trace-id"].(string); ok && traceIDStr != "" {
		span.SetAttributes(
			attribute.String("messaging.rabbitmq.producer_trace_id", traceIDStr),
		)
	}
	if spanIDStr, ok := d.Headers["x-span-id"].(string); ok && spanIDStr != "" {
		span.SetAttributes(
			attribute.String("messaging.rabbitmq.producer_span_id", spanIDStr),
		)
	}

	// 标记消息已到达消费者，开始正式处理
	span.AddEvent("message received")

	err := handler(msgCtx, d.Body, d.MessageId, tagID)

	// 自动确认模式：仅记录处理结果，不执行消息确认（由 RabbitMQ 自动 Ack）
	if c.isAutoAck {
		if err != nil {
			span.RecordError(err,
				trace.WithAttributes(
					attribute.String("error.type", fmt.Sprintf("%T", err)),
					attribute.String("error.context", "auto-ack-mode"),
				),
			)
			span.SetStatus(codes.Error, fmt.Sprintf("handler execution failed: %v", err))
			span.AddEvent("handler error occurred")
		} else {
			span.AddEvent("message processed successfully (auto-ack)")
		}
		return
	}

	// 手动确认模式 - handler 执行失败：Reject 消息（不重新入队），记录完整错误信息
	if err != nil {
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", fmt.Sprintf("%T", err)),
				attribute.String("error.context", "manual-ack-mode"),
				attribute.String("error.action", "reject"),
			),
		)
		span.SetStatus(codes.Error, fmt.Sprintf("handler execution failed: %v", err))
		span.AddEvent("handler error occurred")
		//false	不重新入队（路由到 DLQ 或丢弃）	业务逻辑错误、消息格式异常、不可重试
		//true	重新入队（放回原队列）					上下文取消、临时故障、可重试
		if rejectErr := d.Reject(false); rejectErr != nil {
			span.RecordError(rejectErr,
				trace.WithAttributes(
					attribute.String("error.type", "reject-error"),
					attribute.String("error.context", "manual-reject-failed"),
				),
			)
			span.AddEvent("message reject failed")
			logger.WarnWithCtx(ctx, "[rabbitmq consumer] manual Reject error",
				logger.Err(rejectErr),
				logger.String("tagID", tagID),
				logger.String("queue", c.QueueName),
				logger.String("message_id", d.MessageId))
			// Reject 失败说明 channel 可能已损坏，关闭 channel 触发 RabbitMQ 重投递该消息
			c.safeChannelClose()
		} else {
			span.AddEvent("message rejected (not requeued)")
		}
		return
	}

	// 手动确认模式 - handler 执行成功：确认消息已处理，确认失败时记录告警
	//false	仅确认当前消息	逐条处理、精确控制、需要死信/重试的场景
	//true	批量确认当前及之前所有未确认消息	批量消费、高吞吐、允许批量确认的场景
	if ackErr := d.Ack(false); ackErr != nil {
		span.RecordError(ackErr,
			trace.WithAttributes(
				attribute.String("error.type", "ack-error"),
				attribute.String("error.context", "manual-ack-failed"),
				attribute.String("error.impact", "message-will-be-requeued-by-rabbitmq"),
			),
		)
		span.AddEvent("acknowledgment failed")
		logger.WarnWithCtx(ctx, "[rabbitmq consumer] manual ack error",
			logger.Err(ackErr),
			logger.String("tagID", tagID),
			logger.String("queue", c.QueueName),
			logger.String("message_id", d.MessageId))
		// Ack 失败说明 channel 可能已损坏，关闭 channel 触发 RabbitMQ 重投递该消息
		c.safeChannelClose()
	} else {
		span.AddEvent("message acknowledged successfully")
	}
}

// safeChannelClose 安全关闭当前 AMQP 通道并清空引用。
// 在加锁保护下执行，关闭后将 c.ch 置为 nil 防止重复关闭。
// 关闭失败时仅记录警告日志，不中断调用方流程。
// 该方法可多次调用，第二次及以后为无操作。
func (c *Consumer) safeChannelClose() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ch != nil {
		if err := c.ch.Close(); err != nil {
			logger.WarnWithCtx(context.Background(), "关闭 channel 出错",
				logger.Err(err),
				logger.String("queue", c.QueueName))
		}
		c.ch = nil
	}
}

// Close 关闭消费者，释放所有关联资源。
// 使用 sync.Once 保证只执行一次，支持多次调用且幂等。
//
// 关闭顺序:
//  1. 等待所有正在处理的消息完成（wg.Wait）
//  2. 安全关闭 AMQP 通道（safeChannelClose）
//  3. 记录资源释放日志
//
// 注意事项:
//   - 可在未初始化的 Consumer 上安全调用（c.ch 为 nil 时 safeChannelClose 为空操作）
//   - 调用后不可再用于消费新消息
func (c *Consumer) Close() {
	c.closeOnce.Do(func() {
		c.wg.Wait()
		c.safeChannelClose()
		logger.InfoWithCtx(context.Background(), "[rabbitmq consumer] 资源已释放",
			logger.String("queue", c.QueueName))
	})
}
