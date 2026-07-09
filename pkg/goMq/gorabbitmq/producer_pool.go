package gorabbitmq

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ErrProducerPoolClosed 生产者池已关闭
var ErrProducerPoolClosed = fmt.Errorf("producer pool is closed")

// producerPoolEntry 池内生产者条目
type producerPoolEntry struct {
	producer     *Producer
	conn         *Connection // 从连接池获取的连接
	createReconn int64       // 创建时的重连次数，用于检测 channel 是否过期
}

// ProducerPool 生产者池，管理可复用的 Producer 实例。
// 每次 Get 借出一个 Producer，Put 归还复用，避免反复创建 channel。
// 当底层连接重连导致 channel 失效时，Get 自动重建。
type ProducerPool struct {
	pool     *Pool            // 底层连接池
	exchange *Exchange        // 交换机配置（所有 Producer 共享）
	opts     []ProducerOption // 生产者配置选项

	mu        sync.Mutex
	producers []*producerPoolEntry
	maxSize   int
	isClosed  bool
	tracer    trace.Tracer
}

// NewProducerPool 创建生产者池。
// pool: 底层连接池
// exchange: 交换机配置
// maxSize: 池中最大 Producer 数量（0 表示不限制，建议设为 Pool 的 maxCap）
// opts: 生产者配置选项
func NewProducerPool(pool *Pool, exchange *Exchange, maxSize int, opts ...ProducerOption) (*ProducerPool, error) {
	if exchange == nil {
		return nil, fmt.Errorf("exchange cannot be nil")
	}
	if err := exchange.Validate(); err != nil {
		return nil, err
	}

	return &ProducerPool{
		pool:      pool,
		exchange:  exchange,
		opts:      opts,
		maxSize:   maxSize,
		producers: make([]*producerPoolEntry, 0, maxSize),
		tracer:    otel.Tracer("gomq"),
	}, nil
}

// Get 从池中借出一个 Producer。
// 优先返回池中缓存且 channel 有效的 Producer；
// 如池空或 channel 已过期（底层重连导致），则创建新的 Producer。
// 使用方调用 Put 归还，勿自行 Close。
func (pp *ProducerPool) Get(ctx context.Context) (*Producer, error) {
	pp.mu.Lock()

	for {
		if pp.isClosed {
			pp.mu.Unlock()
			return nil, ErrProducerPoolClosed
		}

		// 尝试从池中取出一个有效的 Producer
		if len(pp.producers) > 0 {
			lastIdx := len(pp.producers) - 1
			entry := pp.producers[lastIdx]
			pp.producers = pp.producers[:lastIdx]
			pp.mu.Unlock()

			// 检查 channel 是否因重连而失效
			if !pp.isChannelValid(entry) {
				pp.discardEntry(ctx, entry)
				pp.mu.Lock()
				continue
			}

			return entry.producer, nil
		}

		// 池空，退出循环在锁外创建新 Producer
		pp.mu.Unlock()
		break
	}

	// 创建新的 Producer
	return pp.newProducerEntry(ctx)
}

// Put 归还 Producer 到池中。
// 如果 channel 已过期（重连后）或池已满，则关闭 Producer 并归还底层连接。
func (pp *ProducerPool) Put(ctx context.Context, producer *Producer) {
	if producer == nil || producer.Connection == nil {
		return
	}

	pp.mu.Lock()
	defer pp.mu.Unlock()

	if pp.isClosed {
		pp.discardProducer(ctx, producer)
		return
	}

	// 快速路径：检查连接本地状态（无网络交互）
	if !producer.Connection.CheckConnected(ctx) {
		pp.discardProducer(ctx, producer)
		return
	}

	// 检查 channel 有效性（reconnectCount 快照对比）
	reconn := producer.Connection.GetReconnectCount(ctx)
	entry := &producerPoolEntry{
		producer:     producer,
		conn:         producer.Connection,
		createReconn: reconn,
	}
	if !pp.isChannelValid(entry) {
		pp.discardEntry(ctx, entry)
		return
	}

	// 池满则丢弃
	if pp.maxSize > 0 && len(pp.producers) >= pp.maxSize {
		pp.discardEntry(ctx, entry)
		return
	}

	pp.producers = append(pp.producers, entry)
}

// Close 关闭生产者池，释放所有 Producer 和底层连接。
func (pp *ProducerPool) Close(ctx context.Context) {
	pp.mu.Lock()
	if pp.isClosed {
		pp.mu.Unlock()
		return
	}
	pp.isClosed = true
	entries := pp.producers
	pp.producers = nil
	pp.mu.Unlock()

	for _, entry := range entries {
		pp.discardEntry(ctx, entry)
	}
}

// Len 返回当前池中可用的 Producer 数量
func (pp *ProducerPool) Len() int {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return len(pp.producers)
}

// ---------------------------------------------------------------------------
// 内部方法
// ---------------------------------------------------------------------------

// isChannelValid 检查 entry 的 channel 是否仍有效。
// 三级验证：
//   - 快速路径：CheckConnected 检查连接本地状态（原子读）
//   - 快速路径：IsClosed 检查 AMQP channel 自身状态（非网络交互）
//   - 完整路径：reconnectCount 快照对比，检测重连导致的 channel 过期
func (pp *ProducerPool) isChannelValid(entry *producerPoolEntry) bool {
	if entry == nil || entry.conn == nil || entry.producer == nil {
		return false
	}
	if !entry.conn.CheckConnected(context.Background()) {
		return false
	}
	// 检查 AMQP channel 自身是否已关闭（连接正常但 channel 被服务端关闭）
	if entry.producer.mqChannel != nil && entry.producer.mqChannel.IsClosed() {
		return false
	}
	reconn := entry.conn.GetReconnectCount(context.Background())
	return reconn == entry.createReconn
}

// discardEntry 废弃一个池内条目：关闭 Producer，归还连接
func (pp *ProducerPool) discardEntry(ctx context.Context, entry *producerPoolEntry) {
	if err := entry.producer.Close(); err != nil {
		logger.WarnWithCtx(ctx, "[producer pool] close producer failed",
			logger.Err(err))
	}
	if err := pp.pool.Put(ctx, entry.conn); err != nil {
		logger.WarnWithCtx(ctx, "[producer pool] return connection failed",
			logger.Err(err))
	}
}

// discardProducer 废弃一个外部传入的 Producer：关闭，归还连接
func (pp *ProducerPool) discardProducer(ctx context.Context, producer *Producer) {
	if err := producer.Close(); err != nil {
		logger.WarnWithCtx(ctx, "[producer pool] close producer failed",
			logger.Err(err))
	}
	if producer.Connection != nil {
		if err := pp.pool.Put(ctx, producer.Connection); err != nil {
			logger.WarnWithCtx(ctx, "[producer pool] return connection failed",
				logger.Err(err))
		}
	}
}

// newProducerEntry 从连接池获取连接，创建新的 Producer
func (pp *ProducerPool) newProducerEntry(ctx context.Context) (*Producer, error) {
	conn, err := pp.pool.GetConn(ctx)
	if err != nil {
		return nil, err
	}

	amqpConn := conn.GetConn(ctx)
	if amqpConn == nil {
		if putErr := pp.pool.Put(ctx, conn); putErr != nil {
			logger.WarnWithCtx(ctx, "[producer pool] return connection after nil amqp connection",
				logger.Err(putErr))
		}
		return nil, fmt.Errorf("rabbitmq connection is not ready")
	}
	channel, err := amqpConn.Channel()
	if err != nil {
		if putErr := pp.pool.Put(ctx, conn); putErr != nil {
			logger.WarnWithCtx(ctx, "[producer pool] return connection after channel creation failed",
				logger.Err(putErr))
		}
		return nil, err
	}

	o := defaultProducerOptions()
	o.apply(pp.opts...)

	return &Producer{
		Connection:      conn,
		mqChannel:       channel,
		Exchange:        pp.exchange,
		producerOptions: o,
		tracer:          pp.tracer,
	}, nil
}
