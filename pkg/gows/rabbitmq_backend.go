package gows

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
)

const (
	defaultProducerPoolSize = 8 // Producer 缓存池默认大小
)

// RabbitMQBackend 基于 RabbitMQ Fanout 交换机的分布式后端实现。
//
// 工作原理:
//   - 所有实例订阅同一个 Fanout 交换机
//   - 每个实例创建一个独占的自动删除队列绑定到该交换机
//   - 生产者向 Fanout 交换机发布消息，所有队列均收到一份副本
//   - 断线重连由 gorabbitmq.Connection 保障，消费端自动恢复
//
// 适用场景:
//   - 已有 RabbitMQ 基础设施的团队
//   - 需要消息持久化保障
//   - 消息量极大（MQ 可水平扩展）
//
// 实现说明:
//   - Publish 使用 Producer 缓存池，避免高频场景下反复创建/销毁 AMQP Channel
//   - Receive 使用 gorabbitmq.Consumer.Consume（自带自动重连和消息分发）
type RabbitMQBackend struct {
	url      string                                // RabbitMQ URL (amqp://user:pass@host:port/vhost)
	exchange string                                // Fanout 交换机名称
	conn     atomic.Pointer[gorabbitmq.Connection] // 带自动重连的连接
	connOpts []gorabbitmq.ConnectionOption

	exchangeObj atomic.Pointer[gorabbitmq.Exchange] // 缓存的 Fanout Exchange 对象
	mu          sync.Mutex
	consumeCh   chan *PubSubMessage // 解码后输出的消息通道
	cancel      context.CancelFunc
	started     atomic.Bool

	// 消费者关闭同步
	consumer   *gorabbitmq.Consumer // 创建的消费者实例
	consumerWg sync.WaitGroup       // 等待消费者协程退出

	// Producer 缓存池（避免高频场景创建/销毁 Channel）
	producerPoolSize int                       // 池大小
	producerPool     chan *gorabbitmq.Producer // Producer 缓存通道
	poolOnce         sync.Once                 // 确保池只初始化一次

	// 关闭状态机
	closeOnce sync.Once // 确保 Close 只执行一次完整流程
}

// NewRabbitMQBackend 创建基于 RabbitMQ Fanout 的分布式后端（通过 URL 创建连接）。
//
// 参数:
//   - url: RabbitMQ 连接地址，如 "amqp://user:pass@host:5672/vhost"
//   - exchange: Fanout 交换机名，所有实例共用
//   - connOpts: 连接选项（如 WithReconnectTime、WithHeartbeat 等）
//
// 示例:
//
//	backend := gows.NewRabbitMQBackend("amqp://guest:guest@localhost:5672/", "ws:messages")
//	dd := gows.NewDispatcher(backend)
//	dd.Start(ctx)
func NewRabbitMQBackend(url string, exchange string, connOpts ...gorabbitmq.ConnectionOption) *RabbitMQBackend {
	return &RabbitMQBackend{
		url:              url,
		exchange:         exchange,
		connOpts:         connOpts,
		producerPoolSize: defaultProducerPoolSize,
	}
}

// NewRabbitMQBackendFromConn 创建基于 RabbitMQ Fanout 的分布式后端（使用已有连接）。
//
// 参数:
//   - conn: 已有的 gorabbitmq.Connection 实例（支持断线重连）
//   - exchange: Fanout 交换机名
//
// 示例:
//
//	conn, _ := gorabbitmq.NewConnection(ctx, "amqp://guest:guest@localhost:5672/")
//	backend := gows.NewRabbitMQBackendFromConn(conn, "ws:messages")
func NewRabbitMQBackendFromConn(conn *gorabbitmq.Connection, exchange string) *RabbitMQBackend {
	b := &RabbitMQBackend{
		exchange:         exchange,
		producerPoolSize: defaultProducerPoolSize,
	}
	b.conn.Store(conn)
	return b
}

// getExchangeObj 惰性创建并缓存 Fanout Exchange 对象。
func (b *RabbitMQBackend) getExchangeObj() *gorabbitmq.Exchange {
	if v := b.exchangeObj.Load(); v != nil {
		return v
	}
	v := gorabbitmq.NewFanoutExchange(b.exchange)
	b.exchangeObj.Store(v)
	return v
}

// ensureConn 惰性初始化 RabbitMQ 连接（如果尚未创建）。
func (b *RabbitMQBackend) ensureConn(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.conn.Load() != nil {
		return nil
	}
	if b.url == "" {
		return fmt.Errorf("rabbitmq: no URL provided, use NewRabbitMQBackend(url,...) or NewRabbitMQBackendFromConn(conn,...)")
	}

	conn, err := gorabbitmq.NewConnection(ctx, b.url, b.connOpts...)
	if err != nil {
		return fmt.Errorf("rabbitmq connection: %w", err)
	}
	b.conn.Store(conn)
	return nil
}

// initProducerPool 惰性初始化 Producer 缓存池。
// 池中的 Producer 复用底层 AMQP Channel，避免高频创建/销毁。
func (b *RabbitMQBackend) initProducerPool(ctx context.Context) {
	b.poolOnce.Do(func() {
		pool := make(chan *gorabbitmq.Producer, b.producerPoolSize)
		for i := 0; i < b.producerPoolSize; i++ {
			producer, err := gorabbitmq.NewProducer(ctx, b.getExchangeObj(), b.conn.Load(),
				gorabbitmq.WithProducerMsgDurable(false),
			)
			if err != nil {
				logger.WarnWithCtx(ctx, "rabbitmq init producer pool failed",
					logger.Int("index", i),
					logger.Err(err),
				)
				continue
			}
			pool <- producer
		}
		b.producerPool = pool
		logger.InfoWithCtx(ctx, "rabbitmq producer pool initialized",
			logger.Int("size", b.producerPoolSize),
			logger.Int("actual", len(pool)),
		)
	})
}

// getProducer 从缓存池获取一个 Producer，池为空时临时创建。
func (b *RabbitMQBackend) getProducer(ctx context.Context) *gorabbitmq.Producer {
	select {
	case p := <-b.producerPool:
		return p
	default:
		// 池空时临时创建（兜底）
		producer, err := gorabbitmq.NewProducer(ctx, b.getExchangeObj(), b.conn.Load(),
			gorabbitmq.WithProducerMsgDurable(false),
		)
		if err != nil {
			logger.WarnWithCtx(ctx, "rabbitmq create temp producer failed", logger.Err(err))
			return nil
		}
		return producer
	}
}

// putProducer 归还 Producer 到缓存池，池满时关闭。
func (b *RabbitMQBackend) putProducer(p *gorabbitmq.Producer) {
	if p == nil {
		return
	}
	select {
	case b.producerPool <- p:
	default:
		if err := p.Close(); err != nil {
			logger.WarnWithCtx(context.Background(), "rabbitmq put producer close failed",
				logger.Err(err),
			)
		}
	}
}

// Publish 向 Fanout 交换机发布一条跨实例消息。
// 使用 Producer 缓存池，避免高频场景下反复创建/销毁 AMQP Channel。
// 自动处理:
//   - Exchange 声明（幂等）
//   - OpenTelemetry Trace 上下文注入
//   - request_id 注入消息头
//
// 自动惰性初始化连接（如果尚未创建）。
func (b *RabbitMQBackend) Publish(ctx context.Context, msg *PubSubMessage) error {
	if err := b.ensureConn(ctx); err != nil {
		return err
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("rabbitmq marshal: %w", err)
	}

	// 惰性初始化 Producer 缓存池
	b.initProducerPool(ctx)

	// 从池中获取 Producer
	producer := b.getProducer(ctx)
	if producer == nil {
		return fmt.Errorf("rabbitmq: failed to get producer")
	}

	if err := producer.PublishFanout(ctx, data, uuid.New().String()); err != nil {
		// 发送失败时关闭此 Producer（可能 channel 已损坏），下次创建新的
		if closeErr := producer.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "rabbitmq close failed producer on publish error",
				logger.Err(closeErr),
			)
		}
		return fmt.Errorf("rabbitmq publish: %w", err)
	}

	// 归还到池
	b.putProducer(producer)
	return nil
}

// Receive 启动消费并返回消息通道。
// 使用 gorabbitmq.Consumer 自动处理:
//   - 独占自动删除队列声明 + 绑定到 Fanout 交换机
//   - 断线重连（gorabbitmq.Connection 保障）
//   - 消息自动确认（auto-ack）
func (b *RabbitMQBackend) Receive(ctx context.Context) (<-chan *PubSubMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.started.Load() {
		return b.consumeCh, nil
	}

	ctx, b.cancel = context.WithCancel(ctx)
	b.consumeCh = make(chan *PubSubMessage, 64)

	// 允许重启：重置一次性守卫
	b.closeOnce = sync.Once{}
	b.poolOnce = sync.Once{}

	// 初始化连接：优先复用已有连接（如 ensureConn/Publish 已创建的）
	conn := b.conn.Load()
	if conn == nil {
		var err error
		conn, err = gorabbitmq.NewConnection(ctx, b.url, b.connOpts...)
		if err != nil {
			return nil, fmt.Errorf("rabbitmq connection: %w", err)
		}
		b.conn.Store(conn)
	}

	// 每个实例使用唯一队列名（避免多实例冲突）
	queueName := fmt.Sprintf("ws:%s:%d", b.exchange, time.Now().UnixNano())

	// 创建 Consumer（自带自动重连和消息分发）
	consumer, err := gorabbitmq.NewConsumer(
		b.getExchangeObj(),
		queueName,
		b.conn.Load(),
		gorabbitmq.WithConsumerNormalLetterOptions(
			gorabbitmq.WithNormalLetter(b.exchange, queueName, ""),
			gorabbitmq.WithNormalLetterExchangeDeclareOptions(
				gorabbitmq.WithExchangeDeclareDurable(true),
			),
			gorabbitmq.WithNormalLetterNormalQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(false),
				gorabbitmq.WithQueueDeclareAutoDelete(true),
				gorabbitmq.WithQueueDeclareExclusive(true),
			),
		),
		gorabbitmq.WithConsumerAutoAck(true),
		gorabbitmq.WithConsumerMsgDurable(false),
	)
	if err != nil {
		b.cancel()
		return nil, fmt.Errorf("rabbitmq create consumer: %w", err)
	}

	// 启动消费（goroutine 内自动重连）
	b.consumer = consumer
	b.consumerWg.Add(1)
	go func() {
		defer b.consumerWg.Done()
		consumer.Consume(ctx, func(ctx context.Context, data []byte, messageId string, tagID string) error {
			b.handleMessage(ctx, data, messageId, tagID)
			return nil
		})
	}()

	b.started.Store(true)
	return b.consumeCh, nil
}

// handleMessage 处理一条 RabbitMQ 消息，反序列化后投递到输出通道。
func (b *RabbitMQBackend) handleMessage(ctx context.Context, data []byte, messageId, tagID string) {
	var msg PubSubMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.WarnWithCtx(ctx, "rabbitmq unmarshal failed",
			logger.Err(err),
			logger.Int("body_size", len(data)),
			logger.String("message_id", messageId),
			logger.String("tag_id", tagID),
		)
		return
	}

	select {
	case b.consumeCh <- &msg:
	default:
		logger.WarnWithCtx(ctx, "rabbitmq consumer channel full, dropping message",
			logger.String("message_id", messageId),
			logger.String("tag_id", tagID),
		)
	}
}

// Close 关闭连接，释放资源。
// 关闭顺序: 取消消费 → 关闭消费者 → 等待消费者协程退出 → 清理 Producer 池 → 关闭 AMQP 连接。
// 幂等安全，多次调用返回 nil。
func (b *RabbitMQBackend) Close() error {
	b.closeOnce.Do(func() {
		// 1. 取消消费上下文
		b.mu.Lock()
		if b.cancel != nil {
			b.cancel()
			b.cancel = nil
		}
		b.mu.Unlock()

		// 2. 关闭 AMQP 消费者
		b.mu.Lock()
		consumer := b.consumer
		b.consumer = nil
		b.mu.Unlock()

		if consumer != nil {
			consumer.Close()
		}

		// 3. 等待消费者 goroutine 退出（带上限）
		if b.started.Load() {
			done := make(chan struct{}, 1)
			go func() {
				b.consumerWg.Wait()
				done <- struct{}{}
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
			}
		}

		// 4. 关闭 Producer 缓存池
		b.mu.Lock()
		pool := b.producerPool
		b.producerPool = nil
		b.mu.Unlock()

		if pool != nil {
			close(pool)
			for p := range pool {
				if err := p.Close(); err != nil {
					logger.WarnWithCtx(context.Background(), "rabbitmq close producer pool item failed",
						logger.Err(err),
					)
				}
			}
		}

		// 5. 关闭 AMQP 连接
		b.mu.Lock()
		conn := b.conn.Load()
		b.conn.Store(nil)
		b.mu.Unlock()

		if conn != nil {
			conn.Close()
		}

		// 6. 重置状态，支持重启
		b.started.Store(false)
		b.mu.Lock()
		b.consumeCh = nil
		b.mu.Unlock()
	})
	return nil
}
