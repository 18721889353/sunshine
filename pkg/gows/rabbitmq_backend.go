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
	defaultReceiveChanSize  = 1024 // 接收消息通道默认缓冲区大小
	defaultProducerPoolSize = 8    // Producer 缓存池默认大小
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
	url            string                                // RabbitMQ URL (amqp://user:pass@host:port/vhost)
	exchange       string                                // Fanout 交换机名称
	mqConn         atomic.Pointer[gorabbitmq.Connection] // 带自动重连的 RabbitMQ 连接
	mqConnOpts     []gorabbitmq.ConnectionOption         // RabbitMQ 连接选项（如心跳间隔、重连策略）
	fanoutExchange atomic.Pointer[gorabbitmq.Exchange]   // 缓存的 Fanout Exchange 对象

	lifecycleMu    sync.Mutex          // 保护生命周期操作（ensureConn/Receive/Close）的互斥锁
	receiveMsgCh   chan *PubSubMessage // 接收解码后输出的消息通道
	receiveCancel  context.CancelFunc  // 取消接收上下文的 cancel 函数
	receiveStarted atomic.Bool         // 接收是否已启动，确保 Receive 幂等
	// 消费者实例（带自动重连）
	receiveConsumer *gorabbitmq.Consumer // AMQP 消费者实例
	receiveWg       sync.WaitGroup       // 等待接收消费者协程退出

	// Producer 缓存池（避免高频场景创建/销毁 Channel）
	producerPool     chan *gorabbitmq.Producer // Producer 缓存通道
	producerPoolSize int                       // 池大小
	producerPoolOnce sync.Once                 // 确保 producer 池只初始化一次

	// 关闭状态机（幂等守卫）
	closeGuardOnce sync.Once // 确保 Close 只执行一次完整流程
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
		mqConnOpts:       connOpts,
		producerPoolSize: defaultProducerPoolSize,
		receiveMsgCh:     make(chan *PubSubMessage, defaultReceiveChanSize),
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
		receiveMsgCh:     make(chan *PubSubMessage, defaultReceiveChanSize),
	}
	b.mqConn.Store(conn)
	return b
}

// getFanoutExchange 惰性创建并缓存 Fanout Exchange 对象。
func (b *RabbitMQBackend) getFanoutExchange() *gorabbitmq.Exchange {
	if v := b.fanoutExchange.Load(); v != nil {
		return v
	}
	v := gorabbitmq.NewFanoutExchange(b.exchange)
	b.fanoutExchange.Store(v)
	return v
}

// ensureConn 惰性初始化 RabbitMQ 连接（如果尚未创建）。
func (b *RabbitMQBackend) ensureConn(ctx context.Context) error {
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()

	if b.mqConn.Load() != nil {
		return nil
	}
	if b.url == "" {
		return fmt.Errorf("rabbitmq: no URL provided, use NewRabbitMQBackend(url,...) or NewRabbitMQBackendFromConn(conn,...)")
	}

	conn, err := gorabbitmq.NewConnection(ctx, b.url, b.mqConnOpts...)
	if err != nil {
		return fmt.Errorf("rabbitmq connection: %w", err)
	}
	b.mqConn.Store(conn)
	return nil
}

// initProducerPool 惰性初始化 Producer 缓存池。
// 池中的 Producer 复用底层 AMQP Channel，避免高频创建/销毁。
func (b *RabbitMQBackend) initProducerPool(ctx context.Context) {
	b.producerPoolOnce.Do(func() {
		pool := make(chan *gorabbitmq.Producer, b.producerPoolSize)
		for i := 0; i < b.producerPoolSize; i++ {
			producer, err := gorabbitmq.NewProducer(ctx, b.getFanoutExchange(), b.mqConn.Load(),
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
		producer, err := gorabbitmq.NewProducer(ctx, b.getFanoutExchange(), b.mqConn.Load(),
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

// Receive 启动 RabbitMQ 消费并返回消息通道。
//
// 参数:
//   - ctx: 调用方上下文，用于控制消费生命周期；失败时内部派生 context 会被自动取消
//
// 返回值:
//   - <-chan *PubSubMessage: 解码后的分布式消息通道，供 receiveLoop 消费
//   - error: 连接失败或创建 Consumer 失败时返回错误
//
// 使用 gorabbitmq.Consumer 自动处理:
//   - 独占自动删除队列声明 + 绑定到 Fanout 交换机
//   - 断线重连（gorabbitmq.Connection 保障）
//   - 消息自动确认（auto-ack）
func (b *RabbitMQBackend) Receive(ctx context.Context) (<-chan *PubSubMessage, error) {
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()

	if b.receiveStarted.Load() {
		return b.receiveMsgCh, nil
	}

	ctx, b.receiveCancel = context.WithCancel(ctx)

	// 重启场景：Close 会将这些字段置为 nil，需要重建
	if b.receiveMsgCh == nil {
		b.receiveMsgCh = make(chan *PubSubMessage, defaultReceiveChanSize)
	}
	b.closeGuardOnce = sync.Once{}
	b.producerPoolOnce = sync.Once{}

	// 初始化连接：优先复用已有连接（如 ensureConn/Publish 已创建的）
	conn := b.mqConn.Load()
	if conn == nil {
		var err error
		conn, err = gorabbitmq.NewConnection(ctx, b.url, b.mqConnOpts...)
		if err != nil {
			b.receiveCancel()
			return nil, fmt.Errorf("rabbitmq connection: %w", err)
		}
		b.mqConn.Store(conn)
	}

	// 每个实例使用唯一队列名（避免多实例冲突）
	queueName := fmt.Sprintf("ws:%s:%d", b.exchange, time.Now().UnixNano())

	// 创建 Consumer（Manual Ack + QOS 限制，MQ 代存积压消息）
	consumer, err := gorabbitmq.NewConsumer(
		b.getFanoutExchange(),
		queueName,
		conn,
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
		gorabbitmq.WithConsumerAutoAck(false),
		gorabbitmq.WithConsumerMsgDurable(false),
		gorabbitmq.WithConsumerQosOptions(
			gorabbitmq.WithQosEnable(),
			gorabbitmq.WithQosPrefetchCount(1024),
		),
	)
	if err != nil {
		b.receiveCancel()
		return nil, fmt.Errorf("rabbitmq create consumer: %w", err)
	}

	// 启动消费（goroutine 内自动重连）
	// Manual Ack: handler 返回 nil → gorabbitmq 自动 Ack；返回 error → Nack + Requeue
	b.receiveConsumer = consumer
	b.receiveWg.Add(1)
	go func() {
		defer b.receiveWg.Done()
		consumer.Consume(ctx, b.handleMessage)
	}()

	b.receiveStarted.Store(true)
	return b.receiveMsgCh, nil
}

// handleMessage 处理一条 RabbitMQ 消息，反序列化后投递到输出通道。
//
// 参数:
//   - ctx: 消费上下文，关闭时 ctx.Err() 返回非 nil 提前退出
//   - data: RabbitMQ 原始消息体（JSON 格式的 PubSubMessage）
//   - messageId: RabbitMQ 消息 ID，用于日志追踪
//   - tagID: RabbitMQ 消费者标签 ID，用于日志追踪
//
// 返回值:
//   - nil: 已成功推入 receiveMsgCh（gorabbitmq 自动 Ack）
//   - error: 处理失败（gorabbitmq 自动 Nack + Requeue）
//
// 当 receiveMsgCh 满时阻塞等待，消息积存在 RabbitMQ 队列中（磁盘持久化），
// 不会丢失，关闭时由 ctx.Done() 退出，由 Manual Ack 机制确保 MQ 重新投递。
func (b *RabbitMQBackend) handleMessage(ctx context.Context, data []byte, messageId, tagID string) error {
	// 关闭过程中不再处理新消息，由 Manual Ack 机制确保 MQ 重新投递
	if ctx.Err() != nil {
		return ctx.Err()
	}

	var msg PubSubMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.WarnWithCtx(ctx, "rabbitmq unmarshal failed",
			logger.Err(err),
			logger.Int("body_size", len(data)),
			logger.String("exchange", b.exchange),
			logger.String("message_id", messageId),
			logger.String("tag_id", tagID),
		)
		return err
	}

	// 阻塞推入 receiveMsgCh，队列满时反压到 MQ（消息保留在 RabbitMQ 队列中）
	// 关闭时由 ctx.Done() 退出，gobrabbitmq 自动 Nack + Requeue，MQ 重新投递
	select {
	case b.receiveMsgCh <- &msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close 关闭连接，释放资源。
// 关闭顺序: 取消消费 → 关闭消费者 → 等待消费者协程退出 → 清理 Producer 池 → 关闭 AMQP 连接。
// 幂等安全，多次调用返回 nil。
func (b *RabbitMQBackend) Close() error {
	b.closeGuardOnce.Do(func() {
		// 一次加锁，统一收集所有待关闭资源并清理内部状态
		// 避免多次 Lock/Unlock 暴露中间状态给 Receive
		b.lifecycleMu.Lock()

		if b.receiveCancel != nil {
			b.receiveCancel()
			b.receiveCancel = nil
		}

		consumer := b.receiveConsumer
		b.receiveConsumer = nil

		pool := b.producerPool
		b.producerPool = nil

		conn := b.mqConn.Load()
		b.mqConn.Store(nil)

		receiveStarted := b.receiveStarted.Swap(false)
		b.receiveMsgCh = nil

		b.lifecycleMu.Unlock()

		// 关闭 AMQP 消费者（已不再持有锁，不阻塞 Receive）
		if consumer != nil {
			consumer.Close()
		}

		// 等待消费者 goroutine 退出（带上限）
		if receiveStarted {
			done := make(chan struct{}, 1)
			go func() {
				b.receiveWg.Wait()
				done <- struct{}{}
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
		}

		// 关闭 Producer 缓存池
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

		// 关闭 AMQP 连接
		if conn != nil {
			conn.Close()
		}
	})
	return nil
}
