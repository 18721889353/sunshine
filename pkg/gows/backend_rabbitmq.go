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

// RabbitMQBackend 基于 RabbitMQ Direct 交换机的分布式后端实现。
//
// 工作原理:
//   - 广播消息: routing_key="" → 每个实例的广播队列（auto-delete）
//   - 按 UID 消息: routing_key=uid → 用户的持久化队列（durable）
//   - 用户断线后队列保留，重连后继续消费积压消息
//
// 适用场景:
//   - 已有 RabbitMQ 基础设施的团队
//   - 需要离线消息补推（消息不丢失）
//   - WebSocket 分布式架构
type RabbitMQBackend struct {
	url        string                                // RabbitMQ URL (amqp://user:pass@host:port/vhost)
	exchange   string                                // Direct 交换机名称，所有消息通过此交换机路由
	mqConn     atomic.Pointer[gorabbitmq.Connection] // 带自动重连的 RabbitMQ 连接
	mqConnOpts []gorabbitmq.ConnectionOption         // RabbitMQ 连接选项（如心跳间隔、重连策略）

	directExchange atomic.Pointer[gorabbitmq.Exchange] // 缓存的 Direct Exchange 对象

	lifecycleMu    sync.Mutex          // 保护生命周期操作（连接初始化/广播消费/关闭）的互斥锁
	broadcastMsgCh chan *PubSubMessage // 广播消息输出通道，供 receiveLoop 消费

	// 广播消费者（routing_key=""，接收所有 broadcast/client_online 类型的消息）
	broadcastStarted  atomic.Bool          // 广播消费者是否已启动
	broadcastCancel   context.CancelFunc   // 取消广播消费上下文的 cancel 函数
	broadcastConsumer *gorabbitmq.Consumer // 广播消息的 AMQP 消费者实例
	broadcastWg       sync.WaitGroup       // 等待广播消费 goroutine 退出
	uidConsumerWg     sync.WaitGroup       // 等待所有 UID 消费者 goroutine 退出

	// 按 UID 的消费者管理（Direct 模式，队列持久化，支持离线消息积压）
	uidConsumers   map[string]*uidConsumerState // uid → 消费者状态，用于离线消息补推
	uidConsumersMu sync.Mutex                   // 保护 uidConsumers 的互斥锁

	// Producer 缓存池（避免高频场景下反复创建/销毁 AMQP Channel）
	producerPool     chan *gorabbitmq.Producer // Producer 缓存通道
	producerPoolSize int                       // 缓存池大小
	producerPoolOnce sync.Once                 // 确保 producer 池只初始化一次

	closeGuardOnce sync.Once // 确保 Close 只执行一次完整流程，支持重启
}

type uidConsumerState struct {
	cancel  context.CancelFunc  // 取消对应 UID 消费上下文的 cancel 函数
	msgCh   chan *PubSubMessage // 该 UID 解码后的消息输出通道
	started atomic.Bool         // 该 UID 消费者是否已启动
}

// NewRabbitMQBackend 创建基于 RabbitMQ Direct 交换机的分布式后端。
func NewRabbitMQBackend(url string, exchange string, connOpts ...gorabbitmq.ConnectionOption) *RabbitMQBackend {
	return &RabbitMQBackend{
		url:              url,
		exchange:         exchange,
		mqConnOpts:       connOpts,
		producerPoolSize: defaultProducerPoolSize,
		broadcastMsgCh:   make(chan *PubSubMessage, defaultReceiveChanSize),
		uidConsumers:     make(map[string]*uidConsumerState),
	}
}

// NewRabbitMQBackendFromConn 使用已有连接创建后端。
func NewRabbitMQBackendFromConn(conn *gorabbitmq.Connection, exchange string) *RabbitMQBackend {
	b := &RabbitMQBackend{
		exchange:         exchange,
		producerPoolSize: defaultProducerPoolSize,
		broadcastMsgCh:   make(chan *PubSubMessage, defaultReceiveChanSize),
		uidConsumers:     make(map[string]*uidConsumerState),
	}
	b.mqConn.Store(conn)
	return b
}

// getDirectExchange 惰性创建并缓存 Direct Exchange 对象。
func (b *RabbitMQBackend) getDirectExchange() *gorabbitmq.Exchange {
	if v := b.directExchange.Load(); v != nil {
		return v
	}
	v := gorabbitmq.NewDirectExchange(b.exchange, b.exchange)
	b.directExchange.Store(v)
	return v
}

// ensureConn 惰性初始化 RabbitMQ 连接。
func (b *RabbitMQBackend) ensureConn(ctx context.Context) error {
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()
	if b.mqConn.Load() != nil {
		return nil
	}
	if b.url == "" {
		return fmt.Errorf("rabbitmq: no URL provided")
	}
	conn, err := gorabbitmq.NewConnection(ctx, b.url, b.mqConnOpts...)
	if err != nil {
		return fmt.Errorf("rabbitmq connection: %w", err)
	}
	b.mqConn.Store(conn)
	return nil
}

// initProducerPool 惰性初始化 Producer 缓存池。
func (b *RabbitMQBackend) initProducerPool(ctx context.Context) {
	b.producerPoolOnce.Do(func() {
		pool := make(chan *gorabbitmq.Producer, b.producerPoolSize)
		for i := 0; i < b.producerPoolSize; i++ {
			producer, err := gorabbitmq.NewProducer(ctx, b.getDirectExchange(), b.mqConn.Load(),
				gorabbitmq.WithProducerMsgDurable(true),
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
	})
}

func (b *RabbitMQBackend) getProducer(ctx context.Context) *gorabbitmq.Producer {
	select {
	case p := <-b.producerPool:
		return p
	default:
		producer, err := gorabbitmq.NewProducer(ctx, b.getDirectExchange(), b.mqConn.Load(),
			gorabbitmq.WithProducerMsgDurable(true),
		)
		if err != nil {
			logger.WarnWithCtx(ctx, "rabbitmq create temp producer failed", logger.Err(err))
			return nil
		}
		return producer
	}
}

func (b *RabbitMQBackend) putProducer(p *gorabbitmq.Producer) {
	if p == nil {
		return
	}
	select {
	case b.producerPool <- p:
	default:
		if err := p.Close(); err != nil {
			logger.WarnWithCtx(context.Background(), "rabbitmq close producer failed", logger.Err(err))
		}
	}
}

// Publish 发布消息到 Direct 交换机。
// 根据消息类型路由:
//   - broadcast/client_online/client_offline: routing_key=""，广播到所有实例
//   - send_to_uid: 为每个 UID 分别发布，routing_key=uid
func (b *RabbitMQBackend) Publish(ctx context.Context, msg *PubSubMessage) error {
	if err := b.ensureConn(ctx); err != nil {
		return err
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("rabbitmq marshal: %w", err)
	}

	b.initProducerPool(ctx)

	if msg.Type == MsgTypeSendToUID && len(msg.UIDs) > 0 {
		// 按 UID 分别发布到 Direct 交换机
		for _, uid := range msg.UIDs {
			if uid == "" {
				continue
			}
			if err := b.publishWithRoutingKey(ctx, data, uid); err != nil {
				return err
			}
		}
		return nil
	}

	// 广播消息：routing_key=""，所有实例的广播队列都会收到
	return b.publishWithRoutingKey(ctx, data, "")
}

func (b *RabbitMQBackend) publishWithRoutingKey(ctx context.Context, data []byte, routingKey string) error {
	producer := b.getProducer(ctx)
	if producer == nil {
		return fmt.Errorf("rabbitmq: failed to get producer")
	}

	if err := producer.PublishDirect(ctx, routingKey, data, uuid.New().String()); err != nil {
		if closeErr := producer.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "rabbitmq close producer after publish failed", logger.Err(closeErr))
		}
		return fmt.Errorf("rabbitmq publish: %w", err)
	}
	b.putProducer(producer)
	return nil
}

// ReceiveBroadcast 返回本实例的广播消息通道。
// 创建独立队列绑定到 Direct 交换机（routing_key=""），接收所有广播消息。
func (b *RabbitMQBackend) ReceiveBroadcast(ctx context.Context) (<-chan *PubSubMessage, error) {
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()

	if b.broadcastStarted.Load() {
		return b.broadcastMsgCh, nil
	}

	ctx, b.broadcastCancel = context.WithCancel(ctx)
	if b.broadcastMsgCh == nil {
		b.broadcastMsgCh = make(chan *PubSubMessage, defaultReceiveChanSize)
	}
	b.closeGuardOnce = sync.Once{}
	b.producerPoolOnce = sync.Once{}

	conn := b.mqConn.Load()
	if conn == nil {
		var err error
		conn, err = gorabbitmq.NewConnection(ctx, b.url, b.mqConnOpts...)
		if err != nil {
			b.broadcastCancel()
			return nil, fmt.Errorf("rabbitmq connection: %w", err)
		}
		b.mqConn.Store(conn)
	}

	// 固定广播队列名（exclusive+auto-delete，每个 connection 独占，不同实例互不干扰）
	broadcastQueue := fmt.Sprintf("ws:%s:broadcast", b.exchange)
	consumer, err := gorabbitmq.NewConsumer(
		b.getDirectExchange(),
		broadcastQueue,
		conn,
		gorabbitmq.WithConsumerNormalLetterOptions(
			gorabbitmq.WithNormalLetter(b.exchange, broadcastQueue, ""),
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
		b.broadcastCancel()
		return nil, fmt.Errorf("rabbitmq create broadcast consumer: %w", err)
	}

	b.broadcastConsumer = consumer
	b.broadcastWg.Add(1)
	go func() {
		defer b.broadcastWg.Done()
		consumer.Consume(ctx, b.handleMessage)
	}()

	b.broadcastStarted.Store(true)
	return b.broadcastMsgCh, nil
}

// Subscribe 订阅指定 UID 的持久化消息队列。
// 创建（或声明）该 UID 的持久化队列，绑定到 Direct 交换机（routing_key=uid）。
// 用户断线后队列保留，消息积压；重连后继续消费。
func (b *RabbitMQBackend) Subscribe(ctx context.Context, uid string) (<-chan *PubSubMessage, error) {
	if uid == "" {
		return nil, fmt.Errorf("rabbitmq: uid is empty")
	}

	b.uidConsumersMu.Lock()
	if state, ok := b.uidConsumers[uid]; ok && state.started.Load() {
		b.uidConsumersMu.Unlock()
		return state.msgCh, nil
	}
	b.uidConsumersMu.Unlock()

	if err := b.ensureConn(ctx); err != nil {
		return nil, err
	}

	conn := b.mqConn.Load()
	ch := make(chan *PubSubMessage, defaultReceiveChanSize)
	uidCtx, cancel := context.WithCancel(ctx)

	// UID 持久化队列：消息自动删除不可用（保留离线消息）
	uidQueue := fmt.Sprintf("ws:%s:uid:%s", b.exchange, uid)
	consumer, err := gorabbitmq.NewConsumer(
		b.getDirectExchange(),
		uidQueue,
		conn,
		gorabbitmq.WithConsumerNormalLetterOptions(
			gorabbitmq.WithNormalLetter(b.exchange, uidQueue, uid),
			gorabbitmq.WithNormalLetterExchangeDeclareOptions(
				gorabbitmq.WithExchangeDeclareDurable(true),
			),
			gorabbitmq.WithNormalLetterNormalQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(true),
				gorabbitmq.WithQueueDeclareAutoDelete(false),
				gorabbitmq.WithQueueDeclareExclusive(false),
			),
		),
		gorabbitmq.WithConsumerAutoAck(false),
		gorabbitmq.WithConsumerMsgDurable(true),
		gorabbitmq.WithConsumerQosOptions(
			gorabbitmq.WithQosEnable(),
			gorabbitmq.WithQosPrefetchCount(256),
		),
		gorabbitmq.WithConsumerName(fmt.Sprintf("ws.uid.%s", uid)),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("rabbitmq subscribe uid %s: %w", uid, err)
	}

	state := &uidConsumerState{
		cancel:  cancel,
		msgCh:   ch,
		started: atomic.Bool{},
	}
	state.started.Store(true)

	b.uidConsumersMu.Lock()
	// 关闭旧的同 UID 消费者（如果有）
	if old, ok := b.uidConsumers[uid]; ok {
		old.cancel()
	}
	b.uidConsumers[uid] = state
	b.uidConsumersMu.Unlock()

	b.uidConsumerWg.Add(1)
	go func() {
		defer b.uidConsumerWg.Done()
		consumer.Consume(uidCtx, func(ctx context.Context, data []byte, _, _ string) error {
			var msg PubSubMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				logger.WarnWithCtx(ctx, "rabbitmq uid unmarshal failed",
					logger.Err(err),
					logger.String("uid", uid),
				)
				return err
			}
			select {
			case ch <- &msg:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		state.started.Store(false)
	}()

	return ch, nil
}

// Unsubscribe 取消订阅指定 UID 的消息队列。
// 关闭消费者但保留队列及消息，支持离线消息积压。
func (b *RabbitMQBackend) Unsubscribe(_ context.Context, uid string) error {
	b.uidConsumersMu.Lock()
	state, ok := b.uidConsumers[uid]
	if ok {
		delete(b.uidConsumers, uid)
	}
	b.uidConsumersMu.Unlock()

	if ok && state != nil {
		state.cancel()
	}
	return nil
}

// handleMessage 处理广播消息回调，反序列化后投递到广播输出通道。
func (b *RabbitMQBackend) handleMessage(ctx context.Context, data []byte, messageID, tagID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var msg PubSubMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.WarnWithCtx(ctx, "rabbitmq unmarshal failed",
			logger.Err(err),
			logger.Int("body_size", len(data)),
			logger.String("message_id", messageID),
			logger.String("tag_id", tagID),
		)
		return err
	}

	select {
	case b.broadcastMsgCh <- &msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitWithTimeout 等待 sync.WaitGroup 完成，超时后不再等待（best-effort 退出）。
func waitWithTimeout(wg *sync.WaitGroup, timeout time.Duration) {
	done := make(chan struct{}, 1)
	go func() {
		wg.Wait()
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// Close 关闭所有消费者、Producer 池和 AMQP 连接。
func (b *RabbitMQBackend) Close() error {
	b.closeGuardOnce.Do(func() {
		// 1. 关闭所有 UID 订阅消费者
		b.uidConsumersMu.Lock()
		for uid, state := range b.uidConsumers {
			state.cancel()
			delete(b.uidConsumers, uid)
		}
		b.uidConsumersMu.Unlock()

		// 2. 取消广播消费上下文
		b.lifecycleMu.Lock()
		if b.broadcastCancel != nil {
			b.broadcastCancel()
			b.broadcastCancel = nil
		}

		consumer := b.broadcastConsumer
		b.broadcastConsumer = nil
		pool := b.producerPool
		b.producerPool = nil
		conn := b.mqConn.Load()
		b.mqConn.Store(nil)
		broadcastStarted := b.broadcastStarted.Swap(false)
		b.broadcastMsgCh = nil
		b.lifecycleMu.Unlock()

		// 3. 关闭广播消费者
		if consumer != nil {
			consumer.Close()
		}
		if broadcastStarted {
			waitWithTimeout(&b.broadcastWg, 5*time.Second)
		}

		// 4. 等待所有 UID 消费者退出
		waitWithTimeout(&b.uidConsumerWg, 5*time.Second)

		// 5. 关闭 Producer 池
		if pool != nil {
			close(pool)
			for p := range pool {
				if err := p.Close(); err != nil {
					logger.WarnWithCtx(context.Background(), "rabbitmq close producer pool failed", logger.Err(err))
				}
			}
		}

		// 6. 关闭连接
		if conn != nil {
			conn.Close()
		}
	})
	return nil
}
