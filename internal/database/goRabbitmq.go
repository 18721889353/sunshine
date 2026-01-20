package database

import (
	"context"
	"fmt"
	"github.com/18721889353/sunshine/internal/config"
	"github.com/spf13/cast"
	"strings"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
	"go.uber.org/zap"
)

var (
	rabbitmqInstance *RabbitMQ
	once             sync.Once
)

// RabbitMQ 封装了 RabbitMQ 连接池及相关操作
type RabbitMQ struct {
	pool          *gorabbitmq.Pool
	exchangeCache *sync.Map // 缓存exchange实例
	producerCache *sync.Map // 缓存producer实例
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
			gorabbitmq.WithHealthCheckPeriod(time.Second*time.Duration(poolCfg.HealthCheckPeriod)), // 健康检查间隔秒
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
			exchangeCache: &sync.Map{},
			producerCache: &sync.Map{},
		}

		// 启动定时清理无效缓存的goroutine
		go rabbitmqInstance.startCacheCleanup()
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
		return nil, fmt.Errorf("rabbitmq pool is not initialized")
	}

	// 使用带重试机制的连接获取方法
	return r.pool.GetWithRetry(ctx, 3)
}

// getExchangeFromCache 从缓存中获取或创建exchange
func (r *RabbitMQ) getExchangeFromCache(exchangeName, routingKey string) *gorabbitmq.Exchange {
	cacheKey := fmt.Sprintf("%s:%s", exchangeName, routingKey)

	if exchange, ok := r.exchangeCache.Load(cacheKey); ok {
		return exchange.(*gorabbitmq.Exchange)
	}

	// 创建新的exchange
	exchange := gorabbitmq.NewDirectExchange(exchangeName, routingKey)

	// 存入缓存，使用LoadOrStore确保并发安全
	if actual, loaded := r.exchangeCache.LoadOrStore(cacheKey, exchange); loaded {
		// 如果已存在，返回已存在的exchange
		return actual.(*gorabbitmq.Exchange)
	}

	return exchange
}

// getProducerFromCache 从缓存中获取或创建producer
func (r *RabbitMQ) getProducerFromCache(ctx context.Context, exchangeName, routingKey string) (*gorabbitmq.Producer, error) {
	cacheKey := exchangeName + ":" + routingKey

	// 先尝试从缓存获取
	if producer, ok := r.producerCache.Load(cacheKey); ok {
		p := producer.(*gorabbitmq.Producer)
		if p != nil {
			// 检查producer是否有效
			if r.isProducerValid(ctx, p) {
				return p, nil
			}
			// 如果无效，从缓存中移除
			r.producerCache.Delete(cacheKey)
		}
	}

	// 创建新的producer
	producer, err := r.createNewProducer(ctx, exchangeName, routingKey)
	if err != nil {
		return nil, err
	}

	// 存入缓存，使用LoadOrStore确保并发安全
	if actual, loaded := r.producerCache.LoadOrStore(cacheKey, producer); loaded {
		// 如果已存在，返回已存在的producer
		return actual.(*gorabbitmq.Producer), nil
	}

	return producer, nil
}

// isProducerValid 检查producer是否有效
func (r *RabbitMQ) isProducerValid(ctx context.Context, producer *gorabbitmq.Producer) bool {
	if producer == nil {
		return false
	}

	// 检查连接是否有效
	if producer.Connection == nil {
		return false
	}

	// 检查连接是否处于连接状态
	if !producer.Connection.CheckConnected(ctx) {
		return false
	}

	// 可以添加更多检查，比如检查channel是否打开
	return true
}

// createNewProducer 创建新的producer
func (r *RabbitMQ) createNewProducer(ctx context.Context, exchangeName, routingKey string) (*gorabbitmq.Producer, error) {
	// 从连接池获取连接
	conn, err := r.GetConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// 确保连接有效
	if !conn.CheckConnected(ctx) {
		// 如果连接无效，尝试重新获取
		conn, err = r.GetConnection(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get valid connection: %w", err)
		}
	}

	// 获取exchange
	exchange := r.getExchangeFromCache(exchangeName, routingKey)

	// 创建新的producer
	producer, err := gorabbitmq.NewProducer(ctx, exchange, conn)
	if err != nil {
		// 创建失败，将连接放回池中
		_ = r.PutConnection(ctx, conn)
		return nil, fmt.Errorf("failed to create producer: %w", err)
	}

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
func (r *RabbitMQ) SendMessage(ctx context.Context, exchangeName, normalQueueName string, message string, messageId string) error {
	start := time.Now()
	routingKey := exchangeName + ":" + normalQueueName
	var err error
	defer func() {
		// 记录耗时
		fields := []zap.Field{
			zap.String("exchangeName", exchangeName),
			zap.String("normalQueueName", normalQueueName),
			zap.String("routingKey", routingKey),
			zap.String("message", message),
			zap.String("cost", cast.ToString(time.Since(start).Milliseconds())+"ms"),
		}
		if err != nil {
			fields = append(fields, zap.Error(err))
			logger.Warn("SendMessage failed", fields...)
		} else {
			logger.Info("SendMessage success", fields...)
		}
	}()

	// 尝试最多3次发送
	for attempt := 1; attempt <= 3; attempt++ {
		// 获取producer
		producer, pErr := r.getProducerFromCache(ctx, exchangeName, normalQueueName)
		if pErr != nil {
			err = pErr
			r.backoff(attempt)
			continue
		}

		// 发送消息
		err = producer.PublishDirect(ctx, exchangeName+"."+normalQueueName, []byte(message), messageId)
		if err == nil {
			return nil
		}
		// 如果发生连接错误，清理缓存并重试
		if isConnectionError(err) {
			r.ClearProducerCache(exchangeName, routingKey)
		}

		r.backoff(attempt)
	}
	return err
}

// backoff 指数退避算法
func (r *RabbitMQ) backoff(attempt int) {
	time.Sleep(time.Duration(attempt*attempt*100) * time.Millisecond)
}

// isConnectionError 判断是否是连接/通道错误
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()

	// 1. 优先匹配最高频的连接断开文本
	if strings.Contains(errStr, "closed") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "EOF") {
		return true
	}
	// 2. 匹配关键的状态码（如 504, 505）和未开启状态
	// 将这些较低频的判断放在后面，或者合并在一起
	if strings.Contains(errStr, "504") ||
		strings.Contains(errStr, "505") ||
		strings.Contains(errStr, "320") ||
		strings.Contains(errStr, "not open") {
		return true
	}
	return false
}

// startCacheCleanup 启动定时清理无效缓存的goroutine
func (r *RabbitMQ) startCacheCleanup() {
	ticker := time.NewTicker(5 * time.Minute) // 每5分钟清理一次
	defer ticker.Stop()

	for range ticker.C {
		r.cleanupInvalidProducers()
	}
}

// cleanupInvalidProducers 清理无效的producer缓存
func (r *RabbitMQ) cleanupInvalidProducers() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 遍历producer缓存，清理无效的producer
	r.producerCache.Range(func(key, value interface{}) bool {
		producer := value.(*gorabbitmq.Producer)
		if !r.isProducerValid(ctx, producer) {
			r.producerCache.Delete(key)
			logger.Info("Cleaned up invalid producer from cache", zap.String("key", key.(string)))
		}
		return true
	})
}

// ClearProducerCache 清理指定或全部的producer缓存
func (r *RabbitMQ) ClearProducerCache(exchangeName, routingKey string) {
	if exchangeName != "" && routingKey != "" {
		// 清理指定的producer
		cacheKey := fmt.Sprintf("%s:%s", exchangeName, routingKey)
		r.producerCache.Delete(cacheKey)
		logger.Info("Cleared producer cache", zap.String("key", cacheKey))
	} else {
		// 清理全部producer缓存，重新创建一个新的map
		r.producerCache = &sync.Map{}
		logger.Info("Cleared all producer cache")
	}
}

// GetPoolStats 获取连接池统计信息
func (r *RabbitMQ) GetPoolStats(ctx context.Context) map[string]interface{} {
	if r.pool == nil {
		return nil
	}
	return r.pool.Stats(ctx)
}

// GetCacheStats 获取缓存统计信息
func (r *RabbitMQ) GetCacheStats() map[string]int {
	exchangeCount := 0
	r.exchangeCache.Range(func(_, _ interface{}) bool {
		exchangeCount++
		return true
	})

	producerCount := 0
	r.producerCache.Range(func(_, _ interface{}) bool {
		producerCount++
		return true
	})

	return map[string]int{
		"exchange_cache_size": exchangeCount,
		"producer_cache_size": producerCount,
	}
}
