package gows

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
)

const (
	defaultReceiveChanSize = 1024 // 接收消息通道默认缓冲区大小
	defaultPoolInitialCap  = 5    // 连接池初始连接数（确保首波广播不等待建连）
	defaultPoolMaxCap      = 50   // 连接池最大连接数（匹配 gorabbitmq 默认 maxCap，应对高频广播+心跳）
)

// RabbitMQBackend 基于 RabbitMQ 的分布式后端实现。
//
// 使用两个交换机分离广播和单播流量：
//   - Fanout 交换机 (exchange+":broadcast"): 广播消息，所有实例订阅
//   - Direct 交换机 (exchange): UID 消息，按 routing_key=uid 投递
//
// 工作原理:
//   - 广播消息: PublishFanout → Fanout 交换机 → 每个实例的广播队列
//   - 按 UID 消息: PublishDirect → Direct 交换机 → routing_key=uid
//   - 用户断线后队列保留，重连后继续消费积压消息
//
// 适用场景:
//   - 已有 RabbitMQ 基础设施的团队
//   - 需要离线消息补推（消息不丢失）
//   - WebSocket 分布式架构
type RabbitMQBackend struct {
	url        string                                // RabbitMQ URL (amqp://user:pass@host:port/vhost)
	exchange   string                                // Direct 交换机名称
	mqConn     atomic.Pointer[gorabbitmq.Connection] // 带自动重连的 RabbitMQ 连接(供消费者使用)
	connMu     sync.Mutex                            // 保护 mqConn 初始化
	mqConnOpts []gorabbitmq.ConnectionOption         // RabbitMQ 连接选项

	// Direct Exchange (UID 消息路由)
	directExchangeOnce sync.Once
	directExchange     *gorabbitmq.Exchange

	// Fanout Exchange (广播消息)
	fanoutExchangeOnce sync.Once
	fanoutExchange     *gorabbitmq.Exchange

	// 连接池 + ProducerPool（广播+UID 路径复用 Producer，避免每次 NewProducer）
	poolMu         sync.Mutex
	connPool       *gorabbitmq.Pool         // RabbitMQ 连接池
	fanoutProdPool *gorabbitmq.ProducerPool // Fanout 交换机生产者池（广播消息）
	directProdPool *gorabbitmq.ProducerPool // Direct 交换机生产者池（UID 消息）
	poolInited     bool                     // pool 是否已初始化

	// 广播消费相关字段
	broadcastMu       sync.Mutex           // 保护广播消费生命周期
	broadcastMsgCh    chan *PubSubMessage  // 广播消息输出通道
	broadcastStarted  atomic.Bool          // 广播消费者是否已启动
	broadcastCancel   context.CancelFunc   // 取消广播消费上下文的 cancel
	broadcastConsumer *gorabbitmq.Consumer // 广播消息的 AMQP 消费者

	// 按 UID 的消费者管理（Direct 模式，队列持久化，支持离线消息积压）
	uidConsumers sync.Map

	rabbitMQClosed atomic.Bool // 确保 Close 只执行一次
}

type uidConsumerState struct {
	cancel  context.CancelFunc
	msgCh   chan *PubSubMessage
	started atomic.Bool
}

// NewRabbitMQBackend 创建基于 RabbitMQ 的分布式后端。
// 自动创建两个交换机：
//   - exchange (Direct): 用于 UID 消息路由
//   - exchange+":broadcast" (Fanout): 用于广播消息
func NewRabbitMQBackend(url string, exchange string, connOpts ...gorabbitmq.ConnectionOption) *RabbitMQBackend {
	return &RabbitMQBackend{
		url:            url,
		exchange:       exchange,
		mqConnOpts:     connOpts,
		broadcastMsgCh: make(chan *PubSubMessage, defaultReceiveChanSize),
	}
}

// NewRabbitMQBackendFromConn 使用已有连接创建后端。
// 注意：此方式无法创建 ProducerPool（无 URL），所有路径使用 per-publish Producer。
func NewRabbitMQBackendFromConn(conn *gorabbitmq.Connection, exchange string) *RabbitMQBackend {
	b := &RabbitMQBackend{
		exchange:       exchange,
		broadcastMsgCh: make(chan *PubSubMessage, defaultReceiveChanSize),
	}
	b.mqConn.Store(conn)
	return b
}

// getDirectExchange 惰性创建并缓存 Direct Exchange 对象（UID 消息路由）
func (b *RabbitMQBackend) getDirectExchange() *gorabbitmq.Exchange {
	b.directExchangeOnce.Do(func() {
		b.directExchange = gorabbitmq.NewDirectExchange(b.exchange, b.exchange)
	})
	return b.directExchange
}

// getFanoutExchange 惰性创建并缓存 Fanout Exchange 对象（广播消息）
func (b *RabbitMQBackend) getFanoutExchange() *gorabbitmq.Exchange {
	b.fanoutExchangeOnce.Do(func() {
		exchangeName := b.exchange + ":broadcast"
		b.fanoutExchange = gorabbitmq.NewFanoutExchange(exchangeName)
	})
	return b.fanoutExchange
}

// broadcastExchangeName 返回 Fanout 交换机名称
func (b *RabbitMQBackend) broadcastExchangeName() string {
	return b.exchange + ":broadcast"
}

// getOrCreateConn 惰性初始化 RabbitMQ 连接，返回连接对象。
func (b *RabbitMQBackend) getOrCreateConn(ctx context.Context) (*gorabbitmq.Connection, error) {
	b.connMu.Lock()
	defer b.connMu.Unlock()
	if conn := b.mqConn.Load(); conn != nil {
		return conn, nil
	}
	if b.url == "" {
		return nil, fmt.Errorf("rabbitmq: no URL provided")
	}
	conn, err := gorabbitmq.NewConnection(ctx, b.url, b.mqConnOpts...)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq connection: %w", err)
	}
	b.mqConn.Store(conn)
	return conn, nil
}

// getOrCreatePool 惰性创建连接池（仅当有 URL 且首次广播发布时）
func (b *RabbitMQBackend) getOrCreatePool(ctx context.Context) (*gorabbitmq.Pool, error) {
	b.poolMu.Lock()
	defer b.poolMu.Unlock()
	if b.poolInited {
		return b.connPool, nil
	}
	if b.url == "" {
		return nil, fmt.Errorf("rabbitmq: no URL for pool")
	}

	pool, err := gorabbitmq.NewPool(ctx, b.url,
		gorabbitmq.WithInitialCap(defaultPoolInitialCap),
		gorabbitmq.WithMaxCap(defaultPoolMaxCap),
		gorabbitmq.WithConnOptions(b.mqConnOpts...),
	)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq create pool: %w", err)
	}

	// 创建 Fanout + Direct ProducerPool（共享同一连接池）
	fanoutPP, err := gorabbitmq.NewProducerPool(pool, b.getFanoutExchange(), defaultPoolMaxCap,
		gorabbitmq.WithProducerMsgDurable(true),
	)
	if err != nil {
		if closeErr := pool.Close(ctx); closeErr != nil {
			logger.WarnWithCtx(context.Background(), "rabbitmq close pool on init failed",
				logger.Err(closeErr))
		}
		return nil, fmt.Errorf("rabbitmq create fanout producer pool: %w", err)
	}

	directPP, err := gorabbitmq.NewProducerPool(pool, b.getDirectExchange(), defaultPoolMaxCap,
		gorabbitmq.WithProducerMsgDurable(true),
	)
	if err != nil {
		fanoutPP.Close(ctx)
		if closeErr := pool.Close(ctx); closeErr != nil {
			logger.WarnWithCtx(context.Background(), "rabbitmq close pool on init failed",
				logger.Err(closeErr))
		}
		return nil, fmt.Errorf("rabbitmq create direct producer pool: %w", err)
	}

	b.connPool = pool
	b.fanoutProdPool = fanoutPP
	b.directProdPool = directPP
	b.poolInited = true
	return pool, nil
}

// Publish 发布消息到 RabbitMQ。
// 根据消息类型路由:
//   - broadcast/client_online/client_offline: 使用 ProducerPool + PublishFanout
//   - send_to_uid: 使用 PublishDirect，routing_key=uid
func (b *RabbitMQBackend) Publish(ctx context.Context, msg *PubSubMessage) error {
	if _, err := b.getOrCreateConn(ctx); err != nil {
		return err
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("rabbitmq marshal: %w", err)
	}

	// SendToUID: 每个 UID 独立发布到 Direct 交换机
	if msg.Type == MsgTypeSendToUID {
		if len(msg.UIDs) == 0 {
			return nil
		}
		return b.publishToDirectUIDs(ctx, data, msg.UIDs)
	}

	// 广播&协调消息：通过 Fanout 交换机广播到所有实例
	switch msg.Type {
	case MsgTypeBroadcast, MsgTypeClientOnline, MsgTypeClientOffline:
		return b.publishBroadcastFanout(ctx, data)
	default:
		return fmt.Errorf("rabbitmq: unknown message type %q", msg.Type)
	}
}

// publishToDirectUIDs 将消息逐个 UID 发布到 Direct 交换机
func (b *RabbitMQBackend) publishToDirectUIDs(ctx context.Context, data []byte, uids []string) error {
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		if err := b.publishWithRoutingKey(ctx, data, uid); err != nil {
			return fmt.Errorf("rabbitmq publish to uid %s: %w", uid, err)
		}
	}
	return nil
}

// publishWithRoutingKey 使用 ProducerPool 发布消息到 Direct 交换机（UID 消息路径）
// 优先使用 ProducerPool 复用 Producer；若 Pool 不可用则回退到 per-publish Producer。
func (b *RabbitMQBackend) publishWithRoutingKey(ctx context.Context, data []byte, routingKey string) error {
	if prodPool, err := b.getDirectProducerPool(ctx); err == nil && prodPool != nil {
		prod, err := prodPool.Get(ctx)
		if err != nil {
			return fmt.Errorf("rabbitmq get producer: %w", err)
		}
		if err := prod.PublishDirect(ctx, routingKey, data, uuid.New().String()); err != nil {
			prodPool.Put(ctx, prod)
			return fmt.Errorf("rabbitmq publish: %w", err)
		}
		prodPool.Put(ctx, prod)
		return nil
	}

	// 回退路径：per-publish Producer（无 url 的初始化路径，如 NewRabbitMQBackendFromConn）
	producer, err := gorabbitmq.NewProducer(ctx, b.getDirectExchange(), b.mqConn.Load(),
		gorabbitmq.WithProducerMsgDurable(true),
	)
	if err != nil {
		return fmt.Errorf("rabbitmq create producer: %w", err)
	}
	defer func() {
		if closeErr := producer.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "rabbitmq close producer failed", logger.Err(closeErr))
		}
	}()
	if err := producer.PublishDirect(ctx, routingKey, data, uuid.New().String()); err != nil {
		return fmt.Errorf("rabbitmq publish: %w", err)
	}
	return nil
}

// publishBroadcastFanout 使用 ProducerPool 发布广播消息到 Fanout 交换机。
// 优先使用 ProducerPool 复用 Producer；若 Pool 不可用则回退到 per-publish Producer。
func (b *RabbitMQBackend) publishBroadcastFanout(ctx context.Context, data []byte) error {
	// 尝试使用 ProducerPool（有 url 的初始化路径）
	if prodPool, err := b.getProducerPool(ctx); err == nil && prodPool != nil {
		prod, err := prodPool.Get(ctx)
		if err != nil {
			return fmt.Errorf("rabbitmq get producer: %w", err)
		}
		if err := prod.PublishFanout(ctx, data, uuid.New().String()); err != nil {
			prodPool.Put(ctx, prod)
			return fmt.Errorf("rabbitmq publish fanout: %w", err)
		}
		prodPool.Put(ctx, prod)
		return nil
	}

	// 回退路径：per-publish Producer（无 url 的初始化路径，如 NewRabbitMQBackendFromConn）
	producer, err := gorabbitmq.NewProducer(ctx, b.getFanoutExchange(), b.mqConn.Load(),
		gorabbitmq.WithProducerMsgDurable(true),
	)
	if err != nil {
		return fmt.Errorf("rabbitmq create producer: %w", err)
	}
	defer func() {
		if closeErr := producer.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "rabbitmq close producer failed", logger.Err(closeErr))
		}
	}()

	if err := producer.PublishFanout(ctx, data, uuid.New().String()); err != nil {
		return fmt.Errorf("rabbitmq publish: %w", err)
	}
	return nil
}

// getProducerPool 返回 Fanout ProducerPool（惰性初始化）
func (b *RabbitMQBackend) getProducerPool(ctx context.Context) (*gorabbitmq.ProducerPool, error) {
	if _, err := b.getOrCreatePool(ctx); err != nil {
		return nil, err
	}
	b.poolMu.Lock()
	pp := b.fanoutProdPool
	b.poolMu.Unlock()
	return pp, nil
}

// getDirectProducerPool 返回 Direct ProducerPool（惰性初始化）
func (b *RabbitMQBackend) getDirectProducerPool(ctx context.Context) (*gorabbitmq.ProducerPool, error) {
	if _, err := b.getOrCreatePool(ctx); err != nil {
		return nil, err
	}
	b.poolMu.Lock()
	pp := b.directProdPool
	b.poolMu.Unlock()
	return pp, nil
}

// SubscribeBroadcast 订阅广播消息，返回消息通道。
// 创建独立队列绑定到 Fanout 交换机，接收所有广播消息。
func (b *RabbitMQBackend) SubscribeBroadcast(ctx context.Context) (<-chan *PubSubMessage, error) {
	b.broadcastMu.Lock()
	defer b.broadcastMu.Unlock()

	if b.broadcastStarted.Load() {
		return b.broadcastMsgCh, nil
	}

	if b.broadcastMsgCh == nil {
		b.broadcastMsgCh = make(chan *PubSubMessage, defaultReceiveChanSize)
	}

	b.rabbitMQClosed.Store(false)

	ctx, b.broadcastCancel = context.WithCancel(ctx)

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

	// 实例唯一广播队列名（exclusive+auto-delete）
	broadcastQueue := fmt.Sprintf("ws:%s:broadcast:%p", b.exchange, b)
	fanoutExchange := b.getFanoutExchange()

	consumer, err := gorabbitmq.NewConsumer(
		fanoutExchange,
		broadcastQueue,
		conn,
		gorabbitmq.WithConsumerNormalLetterOptions(
			gorabbitmq.WithNormalLetter(b.broadcastExchangeName(), broadcastQueue, ""),
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
	b.broadcastStarted.Store(true)
	consumer.Consume(ctx, b.handleMessage)
	return b.broadcastMsgCh, nil
}

// Subscribe 订阅指定 UID 的持久化消息队列。
// 使用 Direct 交换机，routing_key=uid。
func (b *RabbitMQBackend) Subscribe(ctx context.Context, uid string) (<-chan *PubSubMessage, error) {
	if uid == "" {
		return nil, fmt.Errorf("rabbitmq: uid is empty")
	}

	if v, ok := b.uidConsumers.Load(uid); ok {
		state, ok := v.(*uidConsumerState)
		if ok && state.started.Load() {
			return state.msgCh, nil
		}
	}

	conn, err := b.getOrCreateConn(ctx)
	if err != nil {
		return nil, err
	}
	ch := make(chan *PubSubMessage, defaultReceiveChanSize)
	subCtx, subCancel := context.WithCancel(ctx)

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
		subCancel()
		return nil, fmt.Errorf("rabbitmq subscribe uid %s: %w", uid, err)
	}

	// 关闭旧的同 UID 消费者
	if v, ok := b.uidConsumers.Load(uid); ok {
		if state, ok := v.(*uidConsumerState); ok {
			state.cancel()
		}
	}

	state := &uidConsumerState{
		cancel:  subCancel,
		msgCh:   ch,
		started: atomic.Bool{},
	}
	state.started.Store(true)
	b.uidConsumers.Store(uid, state)

	consumer.Consume(subCtx, func(ctx context.Context, data []byte, messageID, tagID string) error {
		return b.handleUIDMessage(ctx, data, messageID, tagID, ch)
	})
	return ch, nil
}

// Unsubscribe 取消订阅指定 UID 的消息队列。
// 关闭消费者但保留队列及消息，支持离线消息积压。
func (b *RabbitMQBackend) Unsubscribe(_ context.Context, uid string) error {
	if v, ok := b.uidConsumers.LoadAndDelete(uid); ok {
		if state, ok := v.(*uidConsumerState); ok {
			state.cancel()
		}
	}
	return nil
}

// handleMessage 处理广播消息回调
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

func (b *RabbitMQBackend) handleUIDMessage(ctx context.Context, data []byte, messageID, tagID string, ch chan<- *PubSubMessage) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var msg PubSubMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.WarnWithCtx(ctx, "rabbitmq uid unmarshal failed",
			logger.Err(err),
			logger.Int("body_size", len(data)),
			logger.String("message_id", messageID),
			logger.String("tag_id", tagID),
		)
		return err
	}
	select {
	case ch <- &msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close 关闭所有消费者、ProducerPool 和 AMQP 连接。
func (b *RabbitMQBackend) Close() error {
	if !b.rabbitMQClosed.CompareAndSwap(false, true) {
		return nil
	}

	// 1. 关闭所有 UID 订阅消费者
	b.uidConsumers.Range(func(key, value any) bool {
		if state, ok := value.(*uidConsumerState); ok {
			state.cancel()
		}
		b.uidConsumers.Delete(key)
		return true
	})

	// 2. 取消广播消费上下文
	b.broadcastMu.Lock()
	if b.broadcastCancel != nil {
		b.broadcastCancel()
		b.broadcastCancel = nil
	}
	consumer := b.broadcastConsumer
	b.broadcastConsumer = nil
	conn := b.mqConn.Load()
	b.mqConn.Store(nil)
	b.broadcastStarted.Store(false)
	b.broadcastMsgCh = nil
	b.broadcastMu.Unlock()

	// 3. 关闭广播消费者
	if consumer != nil {
		consumer.Close()
	}

	// 4. 关闭 ProducerPool + 连接池
	b.poolMu.Lock()
	fanoutPP := b.fanoutProdPool
	b.fanoutProdPool = nil
	directPP := b.directProdPool
	b.directProdPool = nil
	pool := b.connPool
	b.connPool = nil
	b.poolInited = false
	b.poolMu.Unlock()

	if fanoutPP != nil {
		fanoutPP.Close(context.Background())
	}
	if directPP != nil {
		directPP.Close(context.Background())
	}
	if pool != nil {
		if err := pool.Close(context.Background()); err != nil {
			logger.WarnWithCtx(context.Background(), "rabbitmq pool close failed",
				logger.Err(err))
		}
	}

	// 5. 关闭连接
	if conn != nil {
		conn.Close()
	}
	return nil
}
