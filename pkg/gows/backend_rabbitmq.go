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
	connMu     sync.Mutex                            // 保护 mqConn 初始化
	mqConnOpts []gorabbitmq.ConnectionOption         // RabbitMQ 连接选项（如心跳间隔、重连策略）

	directExchangeOnce sync.Once            // 确保 Direct Exchange 只创建一次
	directExchangeVal  *gorabbitmq.Exchange // 缓存的 Direct Exchange 对象

	broadcastMu    sync.Mutex          // 保护广播消费生命周期
	broadcastMsgCh chan *PubSubMessage // 广播消息输出通道，供 receiveLoop 消费
	// 广播消费者（routing_key=""，接收所有 broadcast/client_online 类型的消息）
	broadcastStarted  atomic.Bool          // 广播消费者是否已启动
	broadcastCancel   context.CancelFunc   // 取消广播消费上下文的 cancel 函数
	broadcastConsumer *gorabbitmq.Consumer // 广播消息的 AMQP 消费者实例
	// 按 UID 的消费者管理（Direct 模式，队列持久化，支持离线消息积压）
	uidConsumers sync.Map // uid → *uidConsumerState

	rabbitMQClosed atomic.Bool // 确保 Close 只执行一次，支持重启（SubscribeBroadcast 重置）
}

type uidConsumerState struct {
	cancel  context.CancelFunc  // 取消对应 UID 消费上下文的 cancel 函数
	msgCh   chan *PubSubMessage // 该 UID 解码后的消息输出通道
	started atomic.Bool         // 该 UID 消费者是否已启动
}

// NewRabbitMQBackend 创建基于 RabbitMQ Direct 交换机的分布式后端。
func NewRabbitMQBackend(url string, exchange string, connOpts ...gorabbitmq.ConnectionOption) *RabbitMQBackend {
	return &RabbitMQBackend{
		url:            url,
		exchange:       exchange,
		mqConnOpts:     connOpts,
		broadcastMsgCh: make(chan *PubSubMessage, defaultReceiveChanSize),
	}
}

// NewRabbitMQBackendFromConn 使用已有连接创建后端。
func NewRabbitMQBackendFromConn(conn *gorabbitmq.Connection, exchange string) *RabbitMQBackend {
	b := &RabbitMQBackend{
		exchange:       exchange,
		broadcastMsgCh: make(chan *PubSubMessage, defaultReceiveChanSize),
	}
	b.mqConn.Store(conn)
	return b
}

// getDirectExchange 惰性创建并缓存 Direct Exchange 对象。
func (b *RabbitMQBackend) getDirectExchange() *gorabbitmq.Exchange {
	b.directExchangeOnce.Do(func() {
		b.directExchangeVal = gorabbitmq.NewDirectExchange(b.exchange, b.exchange)
	})
	return b.directExchangeVal
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

// Publish 发布消息到 Direct 交换机。
// 根据消息类型路由:
//   - broadcast/client_online/client_offline: routing_key=""，广播到所有实例
//   - send_to_uid: 为每个 UID 分别发布，routing_key=uid
func (b *RabbitMQBackend) Publish(ctx context.Context, msg *PubSubMessage) error {
	if _, err := b.getOrCreateConn(ctx); err != nil {
		return err
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("rabbitmq marshal: %w", err)
	}

	// SendToUID 允许单条消息投递到多个 UID，每个 UID 独立发布到 Direct 交换机
	if msg.Type == MsgTypeSendToUID {
		// 没有目标 UID 时直接返回，避免落到广播路径造成透传
		if len(msg.UIDs) == 0 {
			return nil
		}
		for _, uid := range msg.UIDs {
			if uid == "" {
				continue
			}
			if err := b.publishWithRoutingKey(ctx, data, uid); err != nil {
				return fmt.Errorf("rabbitmq publish to uid %s: %w", uid, err)
			}
		}
		return nil
	}

	// 广播&协调消息：routing_key=""，所有实例的广播队列都会收到
	switch msg.Type {
	case MsgTypeBroadcast, MsgTypeClientOnline, MsgTypeClientOffline:
		return b.publishWithRoutingKey(ctx, data, "")
	default:
		return fmt.Errorf("rabbitmq: unknown message type %q", msg.Type)
	}
}

func (b *RabbitMQBackend) publishWithRoutingKey(ctx context.Context, data []byte, routingKey string) error {
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

// SubscribeBroadcast 订阅广播消息，返回消息通道。
// 创建独立队列绑定到 Direct 交换机（routing_key=""），接收所有广播消息。
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

	// 实例唯一广播队列名（exclusive+auto-delete），不同实例使用不同队列名，互不干扰。
	// 使用后端对象地址作为实例标识，确保多实例部署时每个实例有自己的广播队列。
	// 实例退出后队列自动删除，重启后使用新队列名重新声明。
	broadcastQueue := fmt.Sprintf("ws:%s:broadcast:%p", b.exchange, b)
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
	b.broadcastStarted.Store(true)
	consumer.Consume(ctx, b.handleMessage)
	return b.broadcastMsgCh, nil
}

// Subscribe 订阅指定 UID 的持久化消息队列。
// 创建（或声明）该 UID 的持久化队列，绑定到 Direct 交换机（routing_key=uid）。
// 用户断线后队列保留，消息积压；重连后继续消费。
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
		subCancel()
		return nil, fmt.Errorf("rabbitmq subscribe uid %s: %w", uid, err)
	}

	// 关闭旧的同 UID 消费者（如果有）
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

// Close 关闭所有消费者、Producer 池和 AMQP 连接。
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

	// 4. 关闭连接
	if conn != nil {
		conn.Close()
	}
	return nil
}
