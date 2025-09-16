package database

import (
	"context"
	"fmt"
	"github.com/18721889353/sunshine/internal/config"
	"sync"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
	"time"
)

var (
	rabbitmqInstance *RabbitMQ
	once             sync.Once
)

// RabbitMQ 封装了 RabbitMQ 连接池及相关操作
type RabbitMQ struct {
	pool          *gorabbitmq.Pool
	exchangeCache map[string]*gorabbitmq.Exchange // 缓存exchange实例
	producerCache map[string]*gorabbitmq.Producer // 缓存producer实例
	cacheMutex    sync.RWMutex                    // 保护缓存的读写锁
}

// InitRabbitmq 初始化 RabbitMQ 连接池
func InitRabbitmq() {
	poolCfg := config.Get().Rabbitmq.Pool
	once.Do(func() {
		var pool *gorabbitmq.Pool
		pool, err := gorabbitmq.NewPool(
			context.Background(),
			poolCfg.URL,
			gorabbitmq.WithInitialCap(poolCfg.InitialCap),                                          // 初始连接数
			gorabbitmq.WithMaxCap(poolCfg.MaxCap),                                                  // 最大连接数
			gorabbitmq.WithMaxIdle(time.Second*time.Duration(poolCfg.MaxIdle)),                     // 最大空闲时间
			gorabbitmq.WithPoolLogger(logger.Get()),                                                // 日志记录器
			gorabbitmq.WithAntsPoolSize(poolCfg.AntsCap),                                           // 配置 ants 协程池大小
			gorabbitmq.WithHealthCheckPeriod(time.Second*time.Duration(poolCfg.HealthCheckPeriod)), // 健康检查间隔秒                // 健康检查周期30秒
			gorabbitmq.WithConnOptions( // 连接选项
				gorabbitmq.WithLogger(logger.Get()),
				gorabbitmq.WithReconnectTime(time.Second*time.Duration(poolCfg.ReconnectTime)),
				gorabbitmq.WithDialTimeout(time.Second*time.Duration(poolCfg.DialTimeout)),
				gorabbitmq.WithHeartbeat(time.Second*time.Duration(poolCfg.Heartbeat)),
			),
		)
		if err != nil {
			panic("Failed to create RabbitMQ pool" + err.Error())
		}

		rabbitmqInstance = &RabbitMQ{
			pool:          pool,
			exchangeCache: make(map[string]*gorabbitmq.Exchange),
			producerCache: make(map[string]*gorabbitmq.Producer),
		}
	})

}

// GetRabbitMQ 获取 RabbitMQ 实例
func GetRabbitMQ() *RabbitMQ {
	if rabbitmqInstance == nil {
		InitRabbitmq()
	}
	return rabbitmqInstance
}

// GetConnection 从连接池获取一个 RabbitMQ 连接
func (r *RabbitMQ) GetConnection(ctx context.Context) (*gorabbitmq.Connection, error) {
	if r.pool == nil {
		return nil, nil
	}

	// 使用带重试机制的连接获取方法
	return r.pool.GetWithRetry(ctx, 3)
}

// getExchangeFromCache 从缓存中获取或创建exchange
func (r *RabbitMQ) getExchangeFromCache(exchangeName, routingKey string) *gorabbitmq.Exchange {
	cacheKey := fmt.Sprintf("%s:%s", exchangeName, routingKey)

	r.cacheMutex.RLock()
	exchange, exists := r.exchangeCache[cacheKey]
	r.cacheMutex.RUnlock()

	if exists {
		return exchange
	}
	// 创建新的exchange
	exchange = gorabbitmq.NewDirectExchange(exchangeName, routingKey)

	// 存入缓存
	r.cacheMutex.Lock()
	r.exchangeCache[cacheKey] = exchange
	r.cacheMutex.Unlock()

	return exchange
}

// getProducerFromCache 从缓存中获取或创建producer
func (r *RabbitMQ) getProducerFromCache(ctx context.Context, exchangeName, routingKey string) (*gorabbitmq.Producer, error) {
	cacheKey := fmt.Sprintf("%s:%s", exchangeName, routingKey)

	r.cacheMutex.RLock()
	producer, exists := r.producerCache[cacheKey]
	r.cacheMutex.RUnlock()

	// 检查缓存中的producer是否有效
	if exists && producer != nil {
		// 检查producer关联的连接是否有效
		if producer.Connection != nil && producer.Connection.CheckConnected(ctx) {
			return producer, nil
		}
		// 如果连接无效，从缓存中删除无效的producer
		r.cacheMutex.Lock()
		delete(r.producerCache, cacheKey)
		r.cacheMutex.Unlock()
	}

	// 从连接池获取连接
	conn, err := r.GetConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = r.PutConnection(ctx, conn)
	}()

	// 获取exchange
	exchange := r.getExchangeFromCache(exchangeName, routingKey)

	// 创建新的producer
	producer, err = gorabbitmq.NewProducer(ctx, exchange, conn)
	if err != nil {
		// 如果创建失败，尝试重新获取连接并重试一次
		conn, retryErr := r.GetConnection(ctx)
		if retryErr != nil {
			return nil, fmt.Errorf("failed to get connection: %v, original error: %w", retryErr, err)
		}
		defer func() {
			_ = r.PutConnection(ctx, conn)
		}()

		producer, err = gorabbitmq.NewProducer(ctx, exchange, conn)
		if err != nil {
			return nil, fmt.Errorf("failed to create producer after retry: %w", err)
		}
	}

	// 存入缓存
	r.cacheMutex.Lock()
	r.producerCache[cacheKey] = producer
	r.cacheMutex.Unlock()

	return producer, nil
}

// PutConnection 将 RabbitMQ 连接放回连接池
func (r *RabbitMQ) PutConnection(ctx context.Context, conn *gorabbitmq.Connection) error {
	if r.pool == nil || conn == nil {
		return nil
	}

	return r.pool.Put(ctx, conn)
}

// Close 关闭 RabbitMQ 连接池
func (r *RabbitMQ) Close(ctx context.Context) error {
	if r.pool == nil {
		return nil
	}
	logger.Info("Closing RabbitMQ connection pool")
	return r.pool.Close(ctx)
}

// SendMessage 发送消息到指定的交换机和路由键
func (r *RabbitMQ) SendMessage(ctx context.Context, exchangeName, normalQueueName string, message string) error {
	// 获取producer
	producer, err := r.getProducerFromCache(ctx, exchangeName, normalQueueName)
	if err != nil {
		return err
	}
	normalRoutineKey := exchangeName + "." + normalQueueName

	// 发送消息
	return producer.PublishDirect(ctx, normalRoutineKey, []byte(message))
}

// GetPoolStats 获取连接池统计信息
func (r *RabbitMQ) GetPoolStats(ctx context.Context) map[string]interface{} {
	if r.pool == nil {
		return nil
	}
	return r.pool.Stats(ctx)
}
