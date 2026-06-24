package gows

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
)

const (
	defaultProducerPoolSize = 4  // Producer 缓存池默认大小
	defaultConsumeChSize    = 64 // 消费通道默认缓冲区大小
)

// RabbitMQBackendOption RabbitMQBackend 配置选项函数类型
type RabbitMQBackendOption func(*RabbitMQBackend)

// WithProducerPoolSize 设置 Producer 缓存池大小（默认 4）。
// 增加此值可提高并发 Publish 吞吐，但会占用更多 RabbitMQ Channel 资源。
func WithProducerPoolSize(n int) RabbitMQBackendOption {
	return func(b *RabbitMQBackend) {
		if n > 0 {
			b.producerPoolSize = n
		}
	}
}

// WithBackendCloseNoWait 设置关闭时不等待消费者协程退出（仅测试用）。
func WithBackendCloseNoWait() RabbitMQBackendOption {
	return func(b *RabbitMQBackend) {
		b.closeNoWait = true
	}
}

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
	url      string                 // RabbitMQ URL (amqp://user:pass@host:port/vhost)
	exchange string                 // Fanout 交换机名称
	conn     *gorabbitmq.Connection // 带自动重连的连接
	connOpts []gorabbitmq.ConnectionOption

	exchangeObj *gorabbitmq.Exchange // 缓存的 Fanout Exchange 对象
	mu          sync.Mutex
	consumeCh   chan *PubSubMessage // 解码后输出的消息通道
	cancel      context.CancelFunc
	started     bool

	// 消费者关闭同步
	consumer    *gorabbitmq.Consumer // 创建的消费者实例
	consumerWg  sync.WaitGroup       // 等待消费者协程退出
	closeNoWait bool                 // 关闭时不等待消费者（用于测试快速退出）

	// Producer 缓存池（避免高频场景创建/销毁 Channel）
	producerPoolSize int                  // 池大小
	producerPool     chan *gorabbitmq.Producer // Producer 缓存通道
	poolOnce         sync.Once            // 确保池只初始化一次

	// 关闭状态机
	closed    bool         // 是否已关闭
	closeOnce sync.Once    // 确保 Close 只执行一次完整流程
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
	return &RabbitMQBackend{
		conn:             conn,
		exchange:         exchange,
		producerPoolSize: defaultProducerPoolSize,
	}
}

// getExchangeObj 惰性创建并缓存 Fanout Exchange 对象。
func (b *RabbitMQBackend) getExchangeObj() *gorabbitmq.Exchange {
	if b.exchangeObj == nil {
		b.exchangeObj = gorabbitmq.NewFanoutExchange(b.exchange)
	}
	return b.exchangeObj
}

// ensureConn 惰性初始化 RabbitMQ 连接（如果尚未创建）。
func (b *RabbitMQBackend) ensureConn(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.conn != nil {
		return nil
	}
	if b.url == "" {
		return fmt.Errorf("rabbitmq: no URL provided, use NewRabbitMQBackend(url,...) or NewRabbitMQBackendFromConn(conn,...)")
	}

	conn, err := gorabbitmq.NewConnection(ctx, b.url, b.connOpts...)
	if err != nil {
		return fmt.Errorf("rabbitmq connection: %w", err)
	}
	b.conn = conn
	return nil
}

// initProducerPool 惰性初始化 Producer 缓存池。
// 池中的 Producer 复用底层 AMQP Channel，避免高频创建/销毁。
func (b *RabbitMQBackend) initProducerPool(ctx context.Context) {
	b.poolOnce.Do(func() {
		pool := make(chan *gorabbitmq.Producer, b.producerPoolSize)
		for i := 0; i < b.producerPoolSize; i++ {
			producer, err := gorabbitmq.NewProducer(ctx, b.getExchangeObj(), b.conn,
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
		producer, err := gorabbitmq.NewProducer(ctx, b.getExchangeObj(), b.conn,
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
		p.Close()
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

	if err := producer.PublishFanout(ctx, data, ""); err != nil {
		// 发送失败时关闭此 Producer（可能 channel 已损坏），下次创建新的
		producer.Close()
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

	if b.started {
		return b.consumeCh, nil
	}

	ctx, b.cancel = context.WithCancel(ctx)
	b.consumeCh = make(chan *PubSubMessage, 64)

	// 初始化连接
	conn, err := gorabbitmq.NewConnection(ctx, b.url, b.connOpts...)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq connection: %w", err)
	}
	b.conn = conn

	// 每个实例使用唯一队列名（避免多实例冲突）
	queueName := fmt.Sprintf("ws:%s:%d", b.exchange, time.Now().UnixNano())

	// 创建 Consumer（自带自动重连和消息分发）
	consumer, err := gorabbitmq.NewConsumer(
		b.getExchangeObj(),
		queueName,
		b.conn,
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
		consumer.Consume(ctx, func(cctx context.Context, data []byte, messageID, tagID string) error {
			b.handleMessage(data)
			return nil
		})
	}()

	b.started = true
	return b.consumeCh, nil
}

// handleMessage 处理一条 RabbitMQ 消息，反序列化后投递到输出通道。
func (b *RabbitMQBackend) handleMessage(data []byte) {
	var msg PubSubMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.WarnWithCtx(context.Background(), "rabbitmq unmarshal failed",
			logger.Err(err),
			logger.Int("body_size", len(data)),
		)
		return
	}

	select {
	case b.consumeCh <- &msg:
	default:
		logger.WarnWithCtx(context.Background(), "rabbitmq consumer channel full, dropping message")
	}
}

// Close 关闭连接，释放资源。
// 关闭顺序: 取消消费 → 关闭消费者 → 等待消费者协程退出 → 清理 Producer 池 → 关闭 AMQP 连接。
// 幂等安全，多次调用返回 nil。
func (b *RabbitMQBackend) Close() error {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()

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
		if !b.closeNoWait && b.started {
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
				p.Close()
			}
		}

		// 5. 关闭 AMQP 连接
		b.mu.Lock()
		conn := b.conn
		b.conn = nil
		b.mu.Unlock()

		if conn != nil {
			conn.Close()
		}
	})
	return nil
}
