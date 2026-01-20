package gorabbitmqClient

import (
	"context"
	"fmt"
	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/jinzhu/copier"
	"github.com/spf13/cast"
	"golang.org/x/sync/singleflight"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"go.uber.org/zap"
)

var (
	rabbitmqInstance atomic.Value       // 使用 atomic.Value 确保单例在并发读取时的安全性
	once             sync.Once          // 确保初始化逻辑只执行一次
	sfGroup          singleflight.Group // 用于抑制并发创建 Producer，防止缓存击穿
)

// RabbitMQ 封装了 RabbitMQ 连接池及相关操作
type RabbitMQ struct {
	pool          *gorabbitmq.Pool
	exchangeCache *sync.Map // 缓存exchange实例
	producerCache *sync.Map // 缓存producer实例
	ctx           context.Context
	cancel        context.CancelFunc // 用于优雅关闭后台维护任务
}

// InitRabbitmq 初始化 RabbitMQ 连接池
func InitRabbitmq(mqCfg any) {
	if mqCfg == nil {
		logger.Error("RabbitMQ 初始化失败: 配置对象为 nil")
		return
	}

	once.Do(func() {
		var cfg struct {
			Pool config.Pool
		}
		if err := copier.Copy(&cfg, mqCfg); err != nil {
			panic("RabbitMQ 配置拷贝失败" + err.Error())
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		poolCfg := cfg.Pool

		if poolCfg.URL == "" {
			panic("RabbitMQ 初始化失败: URL 不能为空")
		}
		if !strings.HasPrefix(poolCfg.URL, "amqp://") && !strings.HasPrefix(poolCfg.URL, "amqps://") {
			panic("RabbitMQ 初始化失败: URL 协议非法" + poolCfg.URL)
		}

		// --- 为 Pool 内部的所有字段设置默认值 (防止零值导致的问题) ---
		if poolCfg.InitialCap <= 0 {
			poolCfg.InitialCap = 2
		}
		if poolCfg.MaxCap <= 0 {
			poolCfg.MaxCap = 10
		}
		if poolCfg.MaxIdle <= 0 {
			poolCfg.MaxIdle = 60 // 60秒
		}
		if poolCfg.AntsCap < 0 {
			poolCfg.AntsCap = 100
		}
		if poolCfg.HealthCheckPeriod <= 0 {
			poolCfg.HealthCheckPeriod = 10 // 10秒
		}
		if poolCfg.ReconnectTime <= 0 {
			poolCfg.ReconnectTime = 5 // 5秒
		}
		if poolCfg.DialTimeout <= 0 {
			poolCfg.DialTimeout = 10 // 10秒
		}
		if poolCfg.Heartbeat <= 0 {
			poolCfg.Heartbeat = 10 // 10秒
		}
		if poolCfg.StatsLogTime <= 0 {
			poolCfg.StatsLogTime = 60 // 60秒
		}
		// 1. 创建底层连接池
		pool, err := gorabbitmq.NewPool(
			ctx,
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
			cancel()
			panic("Failed to create RabbitMQ pool" + err.Error())
		}
		// 2. 构造实例
		instance := &RabbitMQ{
			pool:          pool,
			exchangeCache: &sync.Map{},
			producerCache: &sync.Map{},
			ctx:           ctx,
			cancel:        cancel,
		}
		// 3. 核心改进：先存储实例，确保 GetRabbitMQ 能立即拿到可用对象
		rabbitmqInstance.Store(instance)
		// 4. 启动后台维护任务（自动清理过期 Producer 和打印状态）
		go instance.startBackgroundMaintenance(time.Second * time.Duration(poolCfg.StatsLogTime))

		logger.Info("RabbitMQ module initialized successfully")
	})
}

// GetRabbitMQ 获取 RabbitMQ 实例
func GetRabbitMQ(mqCfg any) *RabbitMQ {
	val := rabbitmqInstance.Load()
	if val == nil {
		InitRabbitmq(mqCfg)
		val = rabbitmqInstance.Load().(*RabbitMQ) // 二次读取确保获取到 Store 后的值
	}
	// 如果由于 Pool 创建失败导致 Load 还是 nil，这里应增加判断
	if val == nil {
		return nil
	}
	return val.(*RabbitMQ)
}

// safeCloseProducer 安全清理 Producer 并将其持有的连接归还连接池
func (r *RabbitMQ) safeCloseProducer(ctx context.Context, key string, p *gorabbitmq.Producer) {
	if p == nil {
		return
	}
	r.producerCache.Delete(key)

	// 核心修复：必须手动将连接归还给 Pool，否则在高并发删除 Producer 时会导致连接泄露
	if p.Connection != nil {
		if err := r.pool.Put(ctx, p.Connection); err != nil {
			logger.Warn("Failed to return connection to pool during cleanup", zap.Error(err))
		}
	}
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
	cacheKey := exchangeName + ":" + routingKey
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

	// 1. 快速路径
	if val, ok := r.producerCache.Load(cacheKey); ok {
		p := val.(*gorabbitmq.Producer)
		if r.isProducerValid(ctx, p) {
			return p, nil
		}
		r.safeCloseProducer(ctx, cacheKey, p)
	}

	// 2. 慢速路径：并发抑制
	val, err, _ := sfGroup.Do(cacheKey, func() (interface{}, error) {
		if v, ok := r.producerCache.Load(cacheKey); ok {
			return v, nil
		}
		producer, err := r.createNewProducer(ctx, exchangeName, routingKey)
		if err != nil {
			return nil, err
		}
		r.producerCache.Store(cacheKey, producer)
		return producer, nil
	})

	if err != nil {
		return nil, err
	}
	return val.(*gorabbitmq.Producer), nil
}

// isProducerValid 检查producer是否有效
func (r *RabbitMQ) isProducerValid(ctx context.Context, producer *gorabbitmq.Producer) bool {
	if producer == nil || producer.Connection == nil {
		return false
	}
	return producer.Connection.CheckConnected(ctx)
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
		// 只有创建失败才在这里 Put，创建成功后连接被 Producer 持有
		_ = r.PutConnection(ctx, conn)
		return nil, err
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

// Close 优雅关闭：停止后台任务并清理所有连接资源
func (r *RabbitMQ) Close(ctx context.Context) error {
	if r.cancel != nil {
		r.cancel() // 触发 ctx.Done()，停止 StartBackgroundMaintenance 中的循环
	}

	// 清理缓存中的所有 Producer 及其连接，防止连接泄露
	r.producerCache.Range(func(key, value interface{}) bool {
		if p, ok := value.(*gorabbitmq.Producer); ok {
			r.safeCloseProducer(ctx, cast.ToString(key), p)
		}
		return true
	})

	if r.pool != nil {
		logger.Info("Closing RabbitMQ connection pool")
		return r.pool.Close(ctx) // 最后关闭物理连接池
	}
	return nil
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
	maxRetries := 3
	// 尝试最多3次发送
	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		// 获取producer
		producer, pErr := r.getProducerFromCache(ctx, exchangeName, normalQueueName)
		if pErr != nil {
			err = pErr
			r.handleRetry(ctx, attempt, maxRetries)
			continue
		}

		// 发送消息
		err = producer.PublishDirect(ctx, exchangeName+"."+normalQueueName, []byte(message), messageId)
		if err == nil {
			return nil
		}
		// 如果发生连接错误，清理缓存并重试
		if isConnectionError(err) {
			logger.Warn("Connection error detected, evicting producer", zap.String("key", routingKey), zap.Error(err))
			r.safeCloseProducer(ctx, routingKey, producer)
		} else {
			// 业务逻辑错误（如 AccessRefused）重试通常无用
			return fmt.Errorf("rabbitmq_business_error: %w", err)
		}

		r.handleRetry(ctx, attempt, maxRetries)
	}
	return err
}

// handleRetry 封装退避逻辑
func (r *RabbitMQ) handleRetry(ctx context.Context, attempt, max int) {
	if attempt >= max {
		return
	}
	timer := time.NewTimer(time.Duration(attempt*attempt*100) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// isConnectionError 判断是否是连接/通道错误
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	// 增加对 net.Error 的判断（如超时或连接重置）
	if _, ok := err.(net.Error); ok {
		return true
	}
	msg := strings.ToLower(err.Error())
	// 常见的连接关闭、超时、通道异常
	return strings.Contains(msg, "closed") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "not open") ||
		strings.Contains(msg, "504") || // channel error
		strings.Contains(msg, "320") // connection forced close
}

// cleanupInvalidProducers 清理无效的producer缓存
func (r *RabbitMQ) cleanupInvalidProducers() {
	cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	r.producerCache.Range(func(key, value interface{}) bool {
		p, ok := value.(*gorabbitmq.Producer)
		if !ok || !r.isProducerValid(cleanCtx, p) {
			logger.Info("Cleanup: Evicting invalid producer", zap.Any("key", key))
			r.safeCloseProducer(cleanCtx, cast.ToString(key), p)
		}
		return true
	})
}

// clearProducerCache 清理指定或全部的producer缓存
func (r *RabbitMQ) clearProducerCache(exchangeName, routingKey string) {
	if exchangeName != "" && routingKey != "" {
		cacheKey := exchangeName + ":" + routingKey
		if val, ok := r.producerCache.Load(cacheKey); ok {
			r.safeCloseProducer(context.Background(), cacheKey, val.(*gorabbitmq.Producer))
		}
	} else {
		r.producerCache.Range(func(key, value interface{}) bool {
			r.safeCloseProducer(context.Background(), cast.ToString(key), value.(*gorabbitmq.Producer))
			return true
		})
		logger.Info("Safely cleared all producer cache and returned connections")
	}
}

// getPoolStats 获取连接池统计信息
func (r *RabbitMQ) getPoolStats(ctx context.Context) map[string]interface{} {
	if r.pool == nil {
		return nil
	}
	return r.pool.Stats(ctx)
}

// getCacheStats 获取缓存统计信息
func (r *RabbitMQ) getCacheStats() map[string]int {
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

// printStats 启动定时状态监控日志
func (r *RabbitMQ) printStats() {
	ctx, cancel := context.WithTimeout(r.ctx, time.Second*2)
	defer cancel()

	stats := r.pool.Stats(ctx)
	logger.Info("RabbitMQ Pool Stats", zap.Any("stats", stats))
}

// startBackgroundMaintenance 启动统一的后台维护和监控任务
func (r *RabbitMQ) startBackgroundMaintenance(interval time.Duration) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				logger.Warn("BackgroundMaintenance panic", zap.Any("err", err))
				// 指数退避重启，防止死循环导致 CPU 暴涨
				time.Sleep(time.Second * 5)
				r.startBackgroundMaintenance(interval)
			}
		}()

		// 假设 interval 为 10秒
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		iteration := uint64(0)
		cleanupMultiplier := uint64(30) // 假设 interval=10s，则每5分钟执行一次清理

		logger.Info("RabbitMQ maintenance worker started",
			zap.Duration("stats_interval", interval),
			zap.Duration("cleanup_interval", interval*time.Duration(cleanupMultiplier)))
		for {
			select {
			case <-r.ctx.Done(): // 响应优雅关闭
				return
			case <-ticker.C:
				iteration++
				r.printStats()

				if iteration%cleanupMultiplier == 0 {
					r.cleanupInvalidProducers()
					iteration = 0
				}
			}
		}
	}()
}
