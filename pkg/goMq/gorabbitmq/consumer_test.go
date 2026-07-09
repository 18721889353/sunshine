package gorabbitmq

import (
	"context"
	"errors"
	"github.com/18721889353/sunshine/pkg/logger"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
)

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

// newTestConsumer 创建测试用消费者，连接失败则跳过测试
func newTestConsumer(t testing.TB, ctx context.Context, queueName string, opts ...ConsumerOption) (*Consumer, *Connection) {
	t.Helper()
	conn := newTestConn(t, ctx)
	exchange := NewDirectExchange("test-consumer-"+queueName, "test-key")
	consumer, err := NewConsumer(exchange, queueName, conn, opts...)
	if err != nil {
		conn.Close()
		t.Fatalf("NewConsumer failed: %v", err)
	}
	return consumer, conn
}

// declareTestQueue 声明测试队列并绑定到交换机，返回队列名称
func declareTestQueue(t testing.TB, amqpConn *amqp.Connection, exchangeName, exchangeType, routingKey string) string {
	t.Helper()
	ch, err := amqpConn.Channel()
	if err != nil {
		t.Fatalf("failed to open channel: %v", err)
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(exchangeName, exchangeType, true, false, false, false, nil); err != nil {
		t.Fatalf("failed to declare exchange %s: %v", exchangeName, err)
	}

	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	if err := ch.QueueBind(q.Name, routingKey, exchangeName, false, nil); err != nil {
		t.Fatalf("failed to bind queue: %v", err)
	}

	return q.Name
}

// ===================================================================
// NewConsumer 前置校验（无需 RabbitMQ）
// ===================================================================

func TestNewConsumer_NilExchange(t *testing.T) {
	_, err := NewConsumer(nil, "test-queue", &Connection{})
	if err == nil {
		t.Error("expected error for nil exchange, got nil")
	}
}

func TestNewConsumer_NilConnection(t *testing.T) {
	exchange := NewDirectExchange("test-nil-conn", "key")
	_, err := NewConsumer(exchange, "test-queue", nil)
	if err == nil {
		t.Error("expected error for nil connection, got nil")
	}
}

func TestNewConsumer_EmptyQueueName(t *testing.T) {
	exchange := NewDirectExchange("test-empty-queue", "key")
	_, err := NewConsumer(exchange, "", &Connection{})
	if err == nil {
		t.Error("expected error for empty queue name, got nil")
	}
}

func TestNewConsumer_InvalidExchangeType(t *testing.T) {
	exchange := &Exchange{name: "test-invalid", eType: "unsupported"}
	_, err := NewConsumer(exchange, "q", &Connection{})
	if err == nil {
		t.Error("expected error for invalid exchange type, got nil")
	}
}

func TestNewConsumer_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchange := NewDirectExchange("test-consumer-success", "key")
	consumer, err := NewConsumer(exchange, "test-q", conn,
		WithConsumerName("test-consumer"),
		WithConsumerAutoAck(true),
		WithConsumerMsgDurable(true),
	)
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	if consumer == nil {
		t.Fatal("NewConsumer returned nil")
	}
	if consumer.name != "test-consumer" {
		t.Errorf("expected name 'test-consumer', got '%s'", consumer.name)
	}
	if !consumer.isAutoAck {
		t.Error("expected isAutoAck=true")
	}
	if !consumer.msgDurable {
		t.Error("expected msgDurable=true")
	}
}

// ===================================================================
// validateQueueConfig 配置校验（无需 RabbitMQ）
// ===================================================================

func TestValidateQueueConfig_ConflictingCustomerAndNormal(t *testing.T) {
	c := &Consumer{
		consumerOptions: &consumerOptions{
			customerDeadLetter: &CustomerDeadLetterOptions{
				DeadLetterOptions: DeadLetterOptions{exchangeName: "custom-dlx"},
			},
			normalLetter: &NormalLetterOptions{
				exchangeName: "normal-ex",
			},
			deadLetter: &DeadLetterOptions{
				exchangeName: defaultExchangeName,
			},
		},
	}
	err := c.validateQueueConfig()
	if err == nil {
		t.Error("expected error for both customerDeadLetter and normalLetter")
	}
}

func TestValidateQueueConfig_ConflictingCustomerAndStandard(t *testing.T) {
	c := &Consumer{
		consumerOptions: &consumerOptions{
			customerDeadLetter: &CustomerDeadLetterOptions{
				DeadLetterOptions: DeadLetterOptions{exchangeName: "custom-dlx"},
			},
			deadLetter: &DeadLetterOptions{
				exchangeName: "std-dlx",
			},
			normalLetter: &NormalLetterOptions{
				exchangeName: defaultExchangeName,
			},
		},
	}
	err := c.validateQueueConfig()
	if err == nil {
		t.Error("expected error for both customerDeadLetter and deadLetter")
	}
}

func TestValidateQueueConfig_ConflictingNormalAndStandard(t *testing.T) {
	c := &Consumer{
		consumerOptions: &consumerOptions{
			normalLetter: &NormalLetterOptions{
				exchangeName: "normal-ex",
			},
			deadLetter: &DeadLetterOptions{
				exchangeName: "std-dlx",
			},
			customerDeadLetter: &CustomerDeadLetterOptions{
				DeadLetterOptions: DeadLetterOptions{exchangeName: defaultExchangeName},
			},
		},
	}
	err := c.validateQueueConfig()
	if err == nil {
		t.Error("expected error for both normalLetter and deadLetter")
	}
}

func TestValidateQueueConfig_DefaultConfigPass(t *testing.T) {
	c := &Consumer{
		consumerOptions: defaultConsumerOptions(),
	}
	err := c.validateQueueConfig()
	if err != nil {
		t.Errorf("expected no error for default config, got %v", err)
	}
}

// ===================================================================
// Close 幂等性
// ===================================================================

func TestConsumer_CloseIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "close-idempotent")
	defer conn.Close()

	// 多次 Close 不 panic
	consumer.Close()
	consumer.Close()
	consumer.Close()
}

func TestConsumer_CloseNeverInitialized(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchange := NewDirectExchange("test-close-never-init", "key")
	consumer, err := NewConsumer(exchange, "never-init-q", conn)
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}

	// 未调用 initialize 直接 Close，不应 panic
	consumer.Close()
}

// ===================================================================
// safeChannelClose 安全关闭
// ===================================================================

func TestConsumer_SafeChannelClose_NilChannel(t *testing.T) {
	consumer := &Consumer{mu: sync.RWMutex{}}
	// channel 为 nil 时调用不应 panic
	consumer.safeChannelClose()
}

func TestConsumer_SafeChannelClose_MultipleTimes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "safe-close-multi")
	defer conn.Close()

	// 初始化后获取 channel
	if err := consumer.setupChannel(ctx); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	// 多次安全关闭
	consumer.safeChannelClose()
	consumer.safeChannelClose()
	consumer.safeChannelClose()
}

// ===================================================================
// waitRetry 等待退出逻辑
// ===================================================================

func TestConsumer_WaitRetry_CtxDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	consumeCtx, consumeCancel := context.WithCancel(context.Background())
	consumeCancel() // 立即取消

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	consumer := &Consumer{conn: conn}
	result := consumer.waitRetry(consumeCtx, ticker)
	if result {
		t.Error("expected false when ctx is done, got true")
	}
}

func TestConsumer_WaitRetry_Ticker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	ticker := time.NewTicker(time.Millisecond * 10)
	defer ticker.Stop()

	consumer := &Consumer{conn: conn}
	result := consumer.waitRetry(ctx, ticker)
	if !result {
		t.Error("expected true when ticker fires, got false")
	}
}

// ===================================================================
// initialize 初始化流程
// ===================================================================

func TestConsumer_Initialize_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "init-success")
	defer conn.Close()

	if err := consumer.setupChannel(ctx); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	defer consumer.safeChannelClose()

	if consumer.ch == nil {
		t.Fatal("channel should not be nil after initialize")
	}
}

func TestConsumer_Initialize_Twice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "init-twice")
	defer conn.Close()

	// 第一次初始化
	if err := consumer.setupChannel(ctx); err != nil {
		t.Fatalf("first initialize failed: %v", err)
	}
	consumer.safeChannelClose()

	// 第二次初始化
	if err := consumer.setupChannel(ctx); err != nil {
		t.Fatalf("second initialize failed: %v", err)
	}
	defer consumer.safeChannelClose()

	if consumer.ch == nil {
		t.Fatal("channel should not be nil after second initialize")
	}
}

// ===================================================================
// Consume 消费循环 — 核心场景
// ===================================================================

func TestConsumer_Consume_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "consume-cancel",
		WithConsumerName("cancel-test"),
		WithConsumerAutoAck(true),
	)
	defer conn.Close()

	// 用可取消的 context 启动消费
	consumeCtx, consumeCancel := context.WithCancel(context.Background())
	handlerCalled := make(chan struct{}, 1)

	consumer.Consume(consumeCtx, func(ctx context.Context, data []byte, messageId, tagID string) error {
		handlerCalled <- struct{}{}
		return nil
	})

	// 立即取消，消费循环应退出
	consumeCancel()

	// 等待 goroutine 退出
	time.Sleep(time.Millisecond * 100)

	// 验证 Close 不 panic
	consumer.Close()
}

func TestConsumer_Consume_AfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "consume-after-close")
	defer conn.Close()

	consumer.Close()

	// Close 后再 Consume 不应 panic
	consumeCtx, consumeCancel := context.WithCancel(context.Background())
	consumeCancel()

	consumer.Consume(consumeCtx, func(ctx context.Context, data []byte, messageId, tagID string) error {
		return nil
	})

	time.Sleep(time.Millisecond * 50)
}

// ===================================================================
// 并发场景
// ===================================================================

func TestConsumer_ConcurrentSafeChannelClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "concurrent-close")
	defer conn.Close()

	if err := consumer.setupChannel(ctx); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			consumer.safeChannelClose()
		}()
	}
	wg.Wait()
}

// ===================================================================
// 边界：空 Handler 不应 panic（调用方责任，但至少不崩溃）
// ===================================================================

func TestConsumer_Consume_NilHandler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "nil-handler")
	defer conn.Close()

	consumeCtx, consumeCancel := context.WithCancel(context.Background())
	consumeCancel()

	// 传 nil handler 不应 panic（handler 调用由 processMessages 延迟到 handleSingleMessage）
	consumer.Consume(consumeCtx, nil)

	time.Sleep(time.Millisecond * 50)
}

// ===================================================================
// setupQoS 设置
// ===================================================================

func TestConsumer_SetupQoS_Disabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "qos-disabled")
	defer conn.Close()

	consumer.qos.enable = false

	// 用 nil channel 测试，qos disabled 时应直接返回 nil
	err := consumer.setupQoS(ctx, nil)
	if err != nil {
		t.Errorf("expected nil when qos disabled, got %v", err)
	}
}

func TestConsumer_SetupQoS_EnabledWithTempChannel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer, conn := newTestConsumer(t, ctx, "qos-enabled")
	defer conn.Close()

	// 创建一个临时 channel 测试 QoS 设置
	ch, err := conn.GetConn(ctx).Channel()
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}
	defer ch.Close()

	consumer.qos.enable = true
	consumer.qos.prefetchCount = 10

	err = consumer.setupQoS(ctx, ch)
	if err != nil {
		t.Errorf("setupQoS with valid channel failed: %v", err)
	}
}

// ===================================================================
// handleSingleMessage — 全面测试
// 使用 mock Acker 模拟 Ack/Reject，无需真实 RabbitMQ 连接
// ===================================================================

// mockAcker 模拟 RabbitMQ 的 Acknowledger 接口，用于测试 Ack/Reject 的多种场景
type mockAcker struct {
	rejectErr     error // Reject 返回的错误，nil 表示成功
	ackErr        error // Ack 返回的错误，nil 表示成功
	ackCalled     bool  // Ack 是否被调用
	rejectCalled  bool  // Reject 是否被调用
	calledWithTag uint64 // 最近一次调用的 DeliveryTag
}

func (m *mockAcker) Ack(tag uint64, multiple bool) error {
	m.ackCalled = true
	m.calledWithTag = tag
	return m.ackErr
}
func (m *mockAcker) Nack(tag uint64, multiple bool, requeue bool) error { return nil }
func (m *mockAcker) Reject(tag uint64, requeue bool) error {
	m.rejectCalled = true
	m.calledWithTag = tag
	return m.rejectErr
}

var (
	errMockChannelClosed = errors.New("channel closed")
	errHandlerFailed     = errors.New("handler failed")
	errSentinel          = errors.New("sentinel error")
)

// newHandleSingleMsgConsumer 创建专用于 handleSingleMessage 测试的 Consumer
func newHandleSingleMsgConsumer(t *testing.T, autoAck bool, name string) *Consumer {
	t.Helper()
	return &Consumer{
		QueueName: "test-queue",
		consumerOptions: &consumerOptions{
			isAutoAck: autoAck,
			name:      name,
		},
		tracer: otel.Tracer("gomq"),
	}
}

// makeDelivery 创建测试用 amqp.Delivery
func makeDelivery(t *testing.T, acker amqp.Acknowledger, headers amqp.Table, body []byte) amqp.Delivery {
	t.Helper()
	return amqp.Delivery{
		Acknowledger: acker,
		Headers:     headers,
		Body:        body,
		DeliveryTag: 1,
		Exchange:    "test-exchange",
		RoutingKey:  "test-key",
		MessageId:   "test-msg-1",
	}
}

// ---------------------------------------------------------------------------
// Auto-ack 模式
// ---------------------------------------------------------------------------

func TestHandleSingleMessage_AutoAck_HandlerSuccess(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test-consumer")
	d := makeDelivery(t, &mockAcker{}, nil, []byte("hello"))

	var gotData []byte
	var gotMessageID, gotTagID string
	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			gotData = data
			gotMessageID = messageId
			gotTagID = tagID
			return nil
		},
	)

	if string(gotData) != "hello" {
		t.Errorf("expected body 'hello', got '%s'", string(gotData))
	}
	if gotMessageID != "test-msg-1" {
		t.Errorf("expected messageId 'test-msg-1', got '%s'", gotMessageID)
	}
	expectedTagID := "test-exchange/test-queue/1"
	if gotTagID != expectedTagID {
		t.Errorf("expected tagID '%s', got '%s'", expectedTagID, gotTagID)
	}
}

func TestHandleSingleMessage_AutoAck_HandlerError(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test-consumer")
	d := makeDelivery(t, &mockAcker{}, nil, []byte("hello"))

	handlerCalled := false
	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			handlerCalled = true
			return errHandlerFailed
		},
	)

	if !handlerCalled {
		t.Error("handler was not called")
	}
}

func TestHandleSingleMessage_AutoAck_HandlerErrorTypes(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"sentinel error", errSentinel},
		{"dynamic error", errors.New("dynamic error")},
		{"fmt error", errHandlerFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newHandleSingleMsgConsumer(t, true, "test-consumer")
			d := makeDelivery(t, &mockAcker{}, nil, []byte("data"))

			c.handleSingleMessage(context.Background(), d,
				func(ctx context.Context, data []byte, messageId, tagID string) error {
					return tt.err
				},
			)
		})
	}
}

// ---------------------------------------------------------------------------
// Manual-ack 模式 — Ack 路径
// ---------------------------------------------------------------------------

func TestHandleSingleMessage_ManualAck_HandlerSuccess_AckOK(t *testing.T) {
	acker := &mockAcker{}
	c := newHandleSingleMsgConsumer(t, false, "test")
	d := makeDelivery(t, acker, nil, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return nil
		},
	)

	if !acker.ackCalled {
		t.Error("expected Ack to be called on successful handler")
	}
	if acker.calledWithTag != 1 {
		t.Errorf("expected Ack called with tag=1, got %d", acker.calledWithTag)
	}
}

func TestHandleSingleMessage_ManualAck_HandlerSuccess_AckFail_SafeChannelClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 需要真实 channel 验证 safeChannelClose 执行
	conn := newTestConn(t, ctx)
	defer conn.Close()

	amqpCh, err := conn.GetConn(ctx).Channel()
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	c := newHandleSingleMsgConsumer(t, false, "test")
	c.ch = amqpCh // 设置 channel 以便 safeChannelClose 关闭它

	d := makeDelivery(t, &mockAcker{ackErr: errMockChannelClosed}, nil, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return nil
		},
	)

	// safeChannelClose 应将 c.ch 置为 nil
	if c.ch != nil {
		t.Error("expected c.ch to be nil after safeChannelClose on ack failure")
	}
}

// ---------------------------------------------------------------------------
// Manual-ack 模式 — Reject 路径
// ---------------------------------------------------------------------------

func TestHandleSingleMessage_ManualAck_HandlerError_RejectOK(t *testing.T) {
	acker := &mockAcker{}
	c := newHandleSingleMsgConsumer(t, false, "test")
	d := makeDelivery(t, acker, nil, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return errHandlerFailed
		},
	)

	if !acker.rejectCalled {
		t.Error("expected Reject to be called on handler error")
	}
	if acker.calledWithTag != 1 {
		t.Errorf("expected Reject called with tag=1, got %d", acker.calledWithTag)
	}
}

func TestHandleSingleMessage_ManualAck_HandlerError_RejectFail_SafeChannelClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	amqpCh, err := conn.GetConn(ctx).Channel()
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	c := newHandleSingleMsgConsumer(t, false, "test")
	c.ch = amqpCh

	d := makeDelivery(t, &mockAcker{rejectErr: errMockChannelClosed}, nil, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return errHandlerFailed
		},
	)

	if c.ch != nil {
		t.Error("expected c.ch to be nil after safeChannelClose on reject failure")
	}
}

// ---------------------------------------------------------------------------
// Header 传播测试
// ---------------------------------------------------------------------------

func TestHandleSingleMessage_WithRequestID(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, amqp.Table{
		"request_id": "req-123",
	}, []byte("hello"))

	var gotReqID interface{}
	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			gotReqID = ctx.Value(logger.ContextKeyForRequestID())
			return nil
		},
	)

	if gotReqID == nil {
		t.Fatal("expected request_id in context")
	}
	if gotReqID.(string) != "req-123" {
		t.Errorf("expected request_id 'req-123', got '%v'", gotReqID)
	}
}

func TestHandleSingleMessage_WithProducerTraceIDs(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, amqp.Table{
		"x-trace-id": "trace-abc",
		"x-span-id":  "span-xyz",
	}, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return nil
		},
	)
}

func TestHandleSingleMessage_WithMixedHeaders(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, amqp.Table{
		"int-key":    int32(42),
		"bool-key":   true,
		"string-key": "valid",
	}, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return nil
		},
	)
}

func TestHandleSingleMessage_EmptyHeaders(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, amqp.Table{}, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return nil
		},
	)
}

func TestHandleSingleMessage_NilHeaders(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, nil, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return nil
		},
	)
}

func TestHandleSingleMessage_WithOnlyTraceID(t *testing.T) {
	// x-trace-id 存在但 x-span-id 不存在，验证独立检查
	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, amqp.Table{
		"x-trace-id": "trace-abc",
	}, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return nil
		},
	)
}

func TestHandleSingleMessage_WithOnlySpanID(t *testing.T) {
	// x-span-id 存在但 x-trace-id 不存在，验证独立检查不会遗漏
	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, amqp.Table{
		"x-span-id": "span-xyz",
	}, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return nil
		},
	)
}

// ---------------------------------------------------------------------------
// 边界条件
// ---------------------------------------------------------------------------

func TestHandleSingleMessage_EmptyConsumerName(t *testing.T) {
	// 空 name 应使用默认 span name "rabbitmq.consume"
	c := newHandleSingleMsgConsumer(t, true, "")
	d := makeDelivery(t, &mockAcker{}, nil, []byte("hello"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return nil
		},
	)
}

func TestHandleSingleMessage_EmptyBody(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, nil, []byte(""))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			if len(data) != 0 {
				t.Errorf("expected empty body, got %d bytes", len(data))
			}
			return nil
		},
	)
}

func TestHandleSingleMessage_LargeBody(t *testing.T) {
	body := make([]byte, 1024*1024) // 1MB
	for i := range body {
		body[i] = byte(i % 256)
	}

	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, nil, body)

	var gotLen int
	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			gotLen = len(data)
			return nil
		},
	)

	if gotLen != len(body) {
		t.Errorf("expected body length %d, got %d", len(body), gotLen)
	}
}

func TestHandleSingleMessage_DeliveryTagFormat(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")
	// 测试不同的 DeliveryTag 值
	d := makeDelivery(t, &mockAcker{}, nil, []byte("data"))
	d.DeliveryTag = 42

	var gotTagID string
	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			gotTagID = tagID
			return nil
		},
	)

	expectedTagID := "test-exchange/test-queue/42"
	if gotTagID != expectedTagID {
		t.Errorf("expected tagID '%s', got '%s'", expectedTagID, gotTagID)
	}
}

func TestHandleSingleMessage_DefaultQueueName(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")
	c.QueueName = "custom-queue"
	d := makeDelivery(t, &mockAcker{}, nil, []byte("data"))

	var gotTagID string
	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			gotTagID = tagID
			return nil
		},
	)

	expectedTagID := "test-exchange/custom-queue/1"
	if gotTagID != expectedTagID {
		t.Errorf("expected tagID '%s', got '%s'", expectedTagID, gotTagID)
	}
}

// ---------------------------------------------------------------------------
// 并发场景 — 多个消息同时处理，验证 WaitGroup 正确计数
// ---------------------------------------------------------------------------

func TestHandleSingleMessage_ConcurrentMessages(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")

	var wg sync.WaitGroup
	numMessages := 20
	errs := make(chan error, numMessages)

	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d := makeDelivery(t, &mockAcker{}, nil, []byte("hello"))
			c.handleSingleMessage(context.Background(), d,
				func(ctx context.Context, data []byte, messageId, tagID string) error {
					return nil
				},
			)
		}(i)
	}

	wg.Wait()
	close(errs)

	// 验证 WaitGroup 已归零（Close 不阻塞）
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// wg 已归零
	case <-time.After(time.Second):
		t.Fatal("WaitGroup did not reach zero after all messages processed, possible wg.Add/Done mismatch")
	}
}

// ---------------------------------------------------------------------------
// 异常场景 — 多种错误类型的 handler
// ---------------------------------------------------------------------------

type customError struct {
	msg string
}

func (e *customError) Error() string { return "custom: " + e.msg }

func TestHandleSingleMessage_HandlerCustomError(t *testing.T) {
	c := newHandleSingleMsgConsumer(t, true, "test")
	d := makeDelivery(t, &mockAcker{}, nil, []byte("data"))

	c.handleSingleMessage(context.Background(), d,
		func(ctx context.Context, data []byte, messageId, tagID string) error {
			return &customError{msg: "test error"}
		},
	)
}
