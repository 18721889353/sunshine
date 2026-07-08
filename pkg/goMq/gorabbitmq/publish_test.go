package gorabbitmq

import (
	"context"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// newTestConn 创建测试用连接，连接失败则跳过测试
func newTestConn(t testing.TB, ctx context.Context) *Connection {
	t.Helper()
	conn, err := NewConnection(ctx, testRabbitMQURL, defaultTestConnOpts...)
	if err != nil {
		t.Skipf("skip: cannot connect to RabbitMQ: %v", err)
	}
	return conn
}

// declareTestExchange 声明测试交换机和队列并绑定，返回队列名称
func declareTestExchange(t testing.TB, amqpConn *amqp.Connection, exchangeName, exchangeType, routingKey string) string {
	t.Helper()
	ch, err := amqpConn.Channel()
	if err != nil {
		t.Fatalf("failed to open channel for exchange declare: %v", err)
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(exchangeName, exchangeType, true, false, false, false, nil); err != nil {
		t.Fatalf("failed to declare test exchange %s: %v", exchangeName, err)
	}

	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("failed to declare test queue: %v", err)
	}

	if err := ch.QueueBind(q.Name, routingKey, exchangeName, false, nil); err != nil {
		t.Fatalf("failed to bind queue to exchange: %v", err)
	}

	return q.Name
}

// ---------------------------------------------------------------------------
// 测试面：NewProducer 创建
// ---------------------------------------------------------------------------

func TestNewProducer_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchange := NewDirectExchange("test-producer-success", "test-key")
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	if producer == nil {
		t.Fatal("NewProducer returned nil")
	}

	if err := producer.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestNewProducer_InvalidExchangeName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	// 空名称
	_, err := NewProducer(ctx, &Exchange{name: "", eType: exchangeTypeDirect}, conn)
	if err == nil {
		t.Error("expected error for empty exchange name, got nil")
	}
}

func TestNewProducer_InvalidExchangeType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	// 不支持的交换机类型
	_, err := NewProducer(ctx, &Exchange{name: "test-invalid-type", eType: "invalid"}, conn)
	if err == nil {
		t.Error("expected error for invalid exchange type, got nil")
	}
}

func TestNewProducer_ConflictingDeadLetterOptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchange := NewDirectExchange("test-conflict", "key")

	// 同时设置 customerDeadLetter 和 normalLetter 应报错
	customerOpts := WithProducerCustomerDeadLetterOptions(
		WithCustomerDeadLetter("dlx-exchange", "dlq", "dl-key", "err-q", "err-key", "norm-q", "norm-key"),
	)
	normalOpts := WithProducerNormalLetterOptions(
		WithNormalLetter("normal-exchange", "nq", "nrk"),
	)

	_, err := NewProducer(ctx, exchange, conn, customerOpts, normalOpts)
	if err == nil {
		t.Error("expected error for conflicting dead letter options, got nil")
	}
}

// ---------------------------------------------------------------------------
// 测试面：PublishDirect
// ---------------------------------------------------------------------------

func TestPublishDirect_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchangeName := "test-publish-direct"
	routingKey := "direct-key"
	declareTestExchange(t, conn.GetConn(ctx), exchangeName, exchangeTypeDirect, routingKey)

	exchange := NewDirectExchange(exchangeName, routingKey)
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	err = producer.PublishDirect(ctx, routingKey, []byte("hello direct"), "msg-001")
	if err != nil {
		t.Errorf("PublishDirect failed: %v", err)
	}
}

func TestPublishDirect_WrongExchangeType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	// Producer 是 fanout 类型，但调用 PublishDirect 应报错
	exchange := NewFanoutExchange("test-publish-direct-wrong")
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	err = producer.PublishDirect(ctx, "key", []byte("should fail"), "msg-002")
	if err == nil {
		t.Error("expected error for exchange type mismatch, got nil")
	}
}

func TestPublishDirect_EmptyBody(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchangeName := "test-publish-direct-empty"
	routingKey := "empty-key"
	declareTestExchange(t, conn.GetConn(ctx), exchangeName, exchangeTypeDirect, routingKey)

	exchange := NewDirectExchange(exchangeName, routingKey)
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	// 发送空消息体
	err = producer.PublishDirect(ctx, routingKey, []byte{}, "msg-empty")
	if err != nil {
		t.Errorf("PublishDirect with empty body failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 测试面：PublishFanout
// ---------------------------------------------------------------------------

func TestPublishFanout_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchangeName := "test-publish-fanout"
	declareTestExchange(t, conn.GetConn(ctx), exchangeName, exchangeTypeFanout, "")

	exchange := NewFanoutExchange(exchangeName)
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	err = producer.PublishFanout(ctx, []byte("hello fanout"), "msg-003")
	if err != nil {
		t.Errorf("PublishFanout failed: %v", err)
	}
}

func TestPublishFanout_WrongExchangeType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	// Producer 是 topic 类型，但调用 PublishFanout 应报错
	exchange := NewTopicExchange("test-publish-fanout-wrong", "key")
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	err = producer.PublishFanout(ctx, []byte("should fail"), "msg-004")
	if err == nil {
		t.Error("expected error for exchange type mismatch, got nil")
	}
}

// ---------------------------------------------------------------------------
// 测试面：PublishTopic
// ---------------------------------------------------------------------------

func TestPublishTopic_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchangeName := "test-publish-topic"
	routingKey := "topic.#"
	declareTestExchange(t, conn.GetConn(ctx), exchangeName, exchangeTypeTopic, routingKey)

	exchange := NewTopicExchange(exchangeName, routingKey)
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	err = producer.PublishTopic(ctx, "topic.test", []byte("hello topic"), "msg-005")
	if err != nil {
		t.Errorf("PublishTopic failed: %v", err)
	}
}

func TestPublishTopic_WrongExchangeType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	// Producer 是 headers 类型，但调用 PublishTopic 应报错
	exchange := NewHeadersExchange("test-publish-topic-wrong", HeadersTypeAll, nil)
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	err = producer.PublishTopic(ctx, "key", []byte("should fail"), "msg-006")
	if err == nil {
		t.Error("expected error for exchange type mismatch, got nil")
	}
}

// ---------------------------------------------------------------------------
// 测试面：PublishHeaders
// ---------------------------------------------------------------------------

func TestPublishHeaders_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchangeName := "test-publish-headers"
	routingKey := ""
	declareTestExchange(t, conn.GetConn(ctx), exchangeName, exchangeTypeHeaders, routingKey)

	exchange := NewHeadersExchange(exchangeName, HeadersTypeAll, nil)
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	headersKeys := map[string]interface{}{
		"x-match": "all",
		"region":  "us-east",
	}
	err = producer.PublishHeaders(ctx, headersKeys, []byte("hello headers"), "msg-007")
	if err != nil {
		t.Errorf("PublishHeaders failed: %v", err)
	}
}

func TestPublishHeaders_WrongExchangeType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	// Producer 是 direct 类型，但调用 PublishHeaders 应报错
	exchange := NewDirectExchange("test-publish-headers-wrong", "key")
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	err = producer.PublishHeaders(ctx, nil, []byte("should fail"), "msg-008")
	if err == nil {
		t.Error("expected error for exchange type mismatch, got nil")
	}
}

func TestPublishHeaders_WithNilHeadersKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchangeName := "test-publish-headers-nil"
	routingKey := ""
	declareTestExchange(t, conn.GetConn(ctx), exchangeName, exchangeTypeHeaders, routingKey)

	exchange := NewHeadersExchange(exchangeName, HeadersTypeAll, nil)
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	// PublishHeaders 传 nil headersKeys 不应 panic
	err = producer.PublishHeaders(ctx, nil, []byte("nil headers"), "msg-009")
	if err != nil {
		t.Errorf("PublishHeaders with nil headers failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 边界：Context 取消导致 publish 失败
// ---------------------------------------------------------------------------

func TestPublish_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchange := NewDirectExchange("test-publish-cancel", "key")
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	// 使用已取消的 context 发布
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err = producer.PublishDirect(cancelCtx, "key", []byte("cancel test"), "msg-cancel")
	// context 取消后，amqp publish 可能成功也可能返回 ctx.Err()
	// 只需确认不 panic 且行为可预测
	if err != nil {
		t.Logf("PublishDirect with cancelled ctx returned: %v (expected behaviour)", err)
	}
}

// ---------------------------------------------------------------------------
// 边界：多个 Publish 调用的幂等性
// ---------------------------------------------------------------------------

func TestPublish_MultipleCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchangeName := "test-publish-multiple"
	routingKey := "multi-key"
	declareTestExchange(t, conn.GetConn(ctx), exchangeName, exchangeTypeDirect, routingKey)

	exchange := NewDirectExchange(exchangeName, routingKey)
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}
	defer producer.Close()

	// 连续多次发布
	for i := 0; i < 5; i++ {
		err := producer.PublishDirect(ctx, routingKey, []byte("multi"), "msg-multi")
		if err != nil {
			t.Errorf("PublishDirect iteration %d failed: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// 边界：Close 后再次 Publish 应返回错误
// ---------------------------------------------------------------------------

func TestPublish_AfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchange := NewDirectExchange("test-publish-after-close", "key")
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}

	// 关闭生产者
	if err := producer.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// 关闭后发布应失败
	err = producer.PublishDirect(ctx, "key", []byte("after close"), "msg-after-close")
	if err == nil {
		t.Error("expected error when publishing after close, got nil")
	}
}

func TestPublish_CloseIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConn(t, ctx)
	defer conn.Close()

	exchange := NewDirectExchange("test-close-idempotent", "key")
	producer, err := NewProducer(ctx, exchange, conn)
	if err != nil {
		t.Fatalf("NewProducer failed: %v", err)
	}

	// 多次关闭不 panic
	if err := producer.Close(); err != nil {
		t.Logf("first Close: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Logf("second Close: %v", err)
	}
}

// ===================================================================
// 测试面：ProducerPool
// ===================================================================

func TestProducerPool_New(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(2),
		WithMaxCap(10),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	exchange := NewDirectExchange("test-pp-new", "key")

	pp, err := NewProducerPool(pool, exchange, 5)
	if err != nil {
		t.Fatalf("NewProducerPool failed: %v", err)
	}
	if pp == nil {
		t.Fatal("NewProducerPool returned nil")
	}
	if pp.Len() != 0 {
		t.Errorf("expected empty pool, got %d", pp.Len())
	}
}

func TestProducerPool_New_InvalidExchange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// 空名称应报错
	_, err := NewProducerPool(pool, &Exchange{name: "", eType: exchangeTypeDirect}, 5)
	if err == nil {
		t.Error("expected error for empty exchange name, got nil")
	}

	// 不支持的类型应报错
	_, err = NewProducerPool(pool, &Exchange{name: "test-pp-invalid", eType: "invalid"}, 5)
	if err == nil {
		t.Error("expected error for invalid exchange type, got nil")
	}

	// nil exchange 应报错
	_, err = NewProducerPool(pool, nil, 5)
	if err == nil {
		t.Error("expected error for nil exchange, got nil")
	}
}

func TestProducerPool_GetPut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(2),
		WithMaxCap(10),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	exchangeName := "test-pp-getput"
	routingKey := "pp-key"
	{
		conn, err := pool.GetConn(ctx)
		if err != nil {
			t.Fatalf("GetConn for exchange declare failed: %v", err)
		}
		declareTestExchange(t, conn.GetConn(ctx), exchangeName, exchangeTypeDirect, routingKey)
		pool.Put(ctx, conn)
	}

	exchange := NewDirectExchange(exchangeName, routingKey)
	pp, err := NewProducerPool(pool, exchange, 5)
	if err != nil {
		t.Fatalf("NewProducerPool failed: %v", err)
	}
	defer pp.Close(ctx)

	// Get 一个 Producer
	producer, err := pp.Get(ctx)
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}
	if producer == nil {
		t.Fatal("Get returned nil")
	}
	if pp.Len() != 0 {
		t.Errorf("expected pool size 0 after Get, got %d", pp.Len())
	}

	// Put 归还
	pp.Put(ctx, producer)
	if pp.Len() != 1 {
		t.Errorf("expected pool size 1 after Put, got %d", pp.Len())
	}

	// 再次 Get，应复用
	producer2, err := pp.Get(ctx)
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}
	if producer2 == nil {
		t.Fatal("second Get returned nil")
	}
	if pp.Len() != 0 {
		t.Errorf("expected pool size 0 after second Get, got %d", pp.Len())
	}

	pp.Put(ctx, producer2)
}

func TestProducerPool_Publish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(2),
		WithMaxCap(10),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	exchangeName := "test-pp-publish"
	routingKey := "pp-pub-key"
	{
		conn, err := pool.GetConn(ctx)
		if err != nil {
			t.Fatalf("GetConn for exchange declare failed: %v", err)
		}
		declareTestExchange(t, conn.GetConn(ctx), exchangeName, exchangeTypeDirect, routingKey)
		pool.Put(ctx, conn)
	}

	exchange := NewDirectExchange(exchangeName, routingKey)
	pp, err := NewProducerPool(pool, exchange, 5)
	if err != nil {
		t.Fatalf("NewProducerPool failed: %v", err)
	}
	defer pp.Close(ctx)

	producer, err := pp.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// 使用 Producer 发布消息
	err = producer.PublishDirect(ctx, routingKey, []byte("pooled publish"), "msg-pp-001")
	if err != nil {
		t.Errorf("PublishDirect from ProducerPool failed: %v", err)
	}

	pp.Put(ctx, producer)
}

func TestProducerPool_ConcurrentGetPut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(5),
		WithMaxCap(20),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	exchange := NewDirectExchange("test-pp-concurrent", "key")
	pp, err := NewProducerPool(pool, exchange, 10)
	if err != nil {
		t.Fatalf("NewProducerPool failed: %v", err)
	}
	defer pp.Close(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			producer, err := pp.Get(ctx)
			if err != nil {
				t.Errorf("Get failed: %v", err)
				return
			}
			pp.Put(ctx, producer)
		}()
	}
	wg.Wait()
}

func TestProducerPool_GetAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	exchange := NewDirectExchange("test-pp-after-close", "key")
	pp, err := NewProducerPool(pool, exchange, 5)
	if err != nil {
		t.Fatalf("NewProducerPool failed: %v", err)
	}

	pp.Close(ctx)

	_, err = pp.Get(ctx)
	if err == nil {
		t.Error("expected error Get after Close, got nil")
	}
}

func TestProducerPool_PutAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	exchange := NewDirectExchange("test-pp-put-after-close", "key")
	pp, err := NewProducerPool(pool, exchange, 5)
	if err != nil {
		t.Fatalf("NewProducerPool failed: %v", err)
	}

	// Get 一个 Producer
	producer, err := pp.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// 关闭池
	pp.Close(ctx)

	// Put 已关闭的池应安全处理
	pp.Put(ctx, producer)
}

func TestProducerPool_PutNil(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	exchange := NewDirectExchange("test-pp-putnil", "key")
	pp, err := NewProducerPool(pool, exchange, 5)
	if err != nil {
		t.Fatalf("NewProducerPool failed: %v", err)
	}
	defer pp.Close(ctx)

	// Put nil 不应 panic
	pp.Put(ctx, nil)
}
