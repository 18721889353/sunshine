package gorabbitmqClient

import (
	"context"
	"fmt"
	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/jinzhu/copier"
	"github.com/spf13/cast"
	"golang.org/x/sync/singleflight"
	"net"
	"strings"
	"sync"
	"time"
)

var (
	rabbitmqInstances sync.Map
	sfGroup           singleflight.Group
	initCtx           = context.Background() // 初始化阶段使用的 context
)

// getContextForLog 智能选择用于日志记录的 context
// 如果外部传递了 ctx（非 nil），则使用传入的 ctx
// 如果没有传递 ctx（nil），则使用 initCtx
func getContextForLog(ctx context.Context) context.Context {
	if ctx == nil {
		return initCtx
	}
	return ctx
}

// RabbitMQ 封装了 RabbitMQ 连接池及相关操作
type RabbitMQ struct {
	pool          *gorabbitmq.Pool
	exchangeCache *sync.Map // 缓存exchange实例
	producerCache *sync.Map // 缓存producer实例
	ctx           context.Context
	cancel        context.CancelFunc // 用于优雅关闭后台维护任务
}

// InitRabbitmq 初始化 RabbitMQ 连接池
func InitRabbitmq(name string, mqCfg any) {
	if name == "" {
		panic("RabbitMQ 模块初始化失败: 模块名称为空")
		return
	}
	if mqCfg == nil {
		panic("RabbitMQ 初始化失败: 配置对象为 nil")
		return
	}

	// 使用 singleflight 防止重复初始化同一个 name
	sfGroup.Do("init_"+name, func() (interface{}, error) {
		if _, ok := rabbitmqInstances.Load(name); ok {
			return nil, nil
		}
		var cfg struct {
			Pool config.Pool
		}
		if err := copier.Copy(&cfg, mqCfg); err != nil {
			panic("RabbitMQ 配置拷贝失败" + err.Error())
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

		// 1. 创建底层连接池
		poolOpts := []gorabbitmq.PoolOption{
			gorabbitmq.WithInitialCap(poolCfg.InitialCap),                                            // 初始连接数
			gorabbitmq.WithMaxCap(poolCfg.MaxCap),                                                    // 最大连接数
			gorabbitmq.WithMaxIdle(time.Second * time.Duration(poolCfg.MaxIdle)),                     // 最大空闲时间
			gorabbitmq.WithAntsPoolSize(poolCfg.AntsCap),                                             // 配置 ants 协程池大小
			gorabbitmq.WithHealthCheckPeriod(time.Second * time.Duration(poolCfg.HealthCheckPeriod)), // 健康检查间隔秒
			gorabbitmq.WithConnOptions( // 连接选项
				gorabbitmq.WithReconnectTime(time.Second*time.Duration(poolCfg.ReconnectTime)),
				gorabbitmq.WithDialTimeout(time.Second*time.Duration(poolCfg.DialTimeout)),
				gorabbitmq.WithHeartbeat(time.Second*time.Duration(poolCfg.Heartbeat)),
			),
			// RabbitMQ Trace 始终启用，采样率由全局 app.tracingSamplingRate 控制
			gorabbitmq.WithTraceEnabled(true),
		}

		pool, err := gorabbitmq.NewPool(ctx, poolCfg.URL, poolOpts...)
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
		rabbitmqInstances.Store(name, instance)
		// 4. 启动后台维护任务（自动清理过期 Producer 和打印状态）
		go instance.startBackgroundMaintenance(poolCfg.StatsLogOpen)
		logger.InfoWithCtx(getContextForLog(initCtx), "RabbitMQ module initialized successfully")

		return nil, nil
	})
}

// GetRabbitMQ 获取 RabbitMQ 实例
func GetRabbitMQ(name string) *RabbitMQ {
	val, ok := rabbitmqInstances.Load(name)
	if !ok {
		return nil
	}
	return val.(*RabbitMQ)
}

// safeCloseProducer 安全清理 Producer 并将其持有的连接归还连接池
func (r *RabbitMQ) safeCloseProducer(ctx context.Context, routingKey string, p *gorabbitmq.Producer) {
	if p == nil {
		return
	}
	r.producerCache.Delete(routingKey)

	// 核心修复：必须手动将连接归还给 Pool，否则在高并发删除 Producer 时会导致连接泄露
	if p.Connection != nil {
		if err := r.pool.Put(ctx, p.Connection); err != nil {
			logger.WarnWithCtx(getContextForLog(ctx), "Failed to return connection to pool during cleanup", logger.Err(err))
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
	if exchange, ok := r.exchangeCache.Load(routingKey); ok {
		return exchange.(*gorabbitmq.Exchange)
	}

	// 创建新的exchange
	exchange := gorabbitmq.NewDirectExchange(exchangeName, routingKey)

	// 存入缓存，使用LoadOrStore确保并发安全
	if actual, loaded := r.exchangeCache.LoadOrStore(routingKey, exchange); loaded {
		// 如果已存在，返回已存在的exchange
		return actual.(*gorabbitmq.Exchange)
	}

	return exchange
}

// getProducerFromCache 从缓存中获取或创建producer
func (r *RabbitMQ) getProducerFromCache(ctx context.Context, exchangeName, routingKey string) (*gorabbitmq.Producer, error) {

	// 1. 快速路径
	if val, ok := r.producerCache.Load(routingKey); ok {
		p := val.(*gorabbitmq.Producer)
		if r.isProducerValid(ctx, p) {
			return p, nil
		}
		r.safeCloseProducer(ctx, routingKey, p)
	}

	// 2. 慢速路径：并发抑制
	val, err, _ := sfGroup.Do(routingKey, func() (interface{}, error) {
		if v, ok := r.producerCache.Load(routingKey); ok {
			return v, nil
		}
		producer, err := r.createNewProducer(ctx, exchangeName, routingKey)
		if err != nil {
			return nil, err
		}
		r.producerCache.Store(routingKey, producer)
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
	r.producerCache.Range(func(routingKey, value interface{}) bool {
		if p, ok := value.(*gorabbitmq.Producer); ok {
			r.safeCloseProducer(ctx, cast.ToString(routingKey), p)
		}
		return true
	})

	if r.pool != nil {
		logger.InfoWithCtx(getContextForLog(ctx), "Closing RabbitMQ connection pool")
		return r.pool.Close(ctx) // 最后关闭物理连接池
	}
	return nil
}

// SendMessage 发送消息到指定的交换机和路由键
func (r *RabbitMQ) SendMessage(ctx context.Context, exchangeName, routingKey string, message string, messageId string) error {
	start := time.Now()
	var err error
	defer func() {
		// 记录耗时（注意：不记录完整消息内容，避免敏感信息泄露）
		if err != nil {
			logger.WarnWithCtx(getContextForLog(ctx), "SendMessage failed",
				logger.String("exchangeName", exchangeName),
				logger.String("routingKey", routingKey),
				logger.Int("message_size_bytes", len(message)), // 只记录消息大小
				logger.String("message_id", messageId),
				logger.String("ms", fmt.Sprintf("%.4f", float64(time.Since(start).Nanoseconds())/1e6)), // 毫秒浮点数，便于SLS数值查询
				logger.Err(err))
		} else {
			logger.InfoWithCtx(getContextForLog(ctx), "SendMessage success",
				logger.String("exchangeName", exchangeName),
				logger.String("routingKey", routingKey),
				logger.Int("message_size_bytes", len(message)),
				logger.String("message_id", messageId),
				logger.String("ms", fmt.Sprintf("%.4f", float64(time.Since(start).Nanoseconds())/1e6)), // 毫秒浮点数，便于SLS数值查询
			)
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
		producer, pErr := r.getProducerFromCache(ctx, exchangeName, routingKey)
		if pErr != nil {
			err = pErr
			r.handleRetry(ctx, attempt, maxRetries)
			continue
		}

		// 发送消息
		err = producer.PublishDirect(ctx, routingKey, []byte(message), messageId)
		if err == nil {
			return nil
		}
		// 如果发生连接错误，清理缓存并重试
		if isConnectionError(err) {
			logger.WarnWithCtx(getContextForLog(ctx), "Connection error detected, evicting producer",
				logger.String("routingKey", routingKey), logger.Err(err))
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

	r.producerCache.Range(func(routingKey, value interface{}) bool {
		p, ok := value.(*gorabbitmq.Producer)
		if !ok || !r.isProducerValid(cleanCtx, p) {
			logger.InfoWithCtx(getContextForLog(cleanCtx), "Cleanup: Evicting invalid producer", logger.String("routingKey", cast.ToString(routingKey)))
			r.safeCloseProducer(cleanCtx, cast.ToString(routingKey), p)
		}
		return true
	})
}

// clearProducerCache 清理指定或全部的producer缓存
func (r *RabbitMQ) clearProducerCache(exchangeName, routingKey string) {
	if exchangeName != "" && routingKey != "" {
		if val, ok := r.producerCache.Load(routingKey); ok {
			r.safeCloseProducer(context.Background(), routingKey, val.(*gorabbitmq.Producer))
		}
	} else {
		r.producerCache.Range(func(routingKey, value interface{}) bool {
			r.safeCloseProducer(context.Background(), cast.ToString(routingKey), value.(*gorabbitmq.Producer))
			return true
		})
		logger.InfoWithCtx(getContextForLog(initCtx), "Safely cleared all producer cache and returned connections")
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

	logger.InfoWithCtx(getContextForLog(initCtx), "RabbitMQ Pool Stats", logger.Any("stats", r.pool.Stats(ctx)))
}

// startBackgroundMaintenance 启动统一的后台维护和监控任务
func (r *RabbitMQ) startBackgroundMaintenance(open bool) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				logger.WarnWithCtx(getContextForLog(initCtx), "BackgroundMaintenance panic", logger.Any("err", err))
				// 指数退避重启，防止死循环导致 CPU 暴涨
				time.Sleep(time.Second * 5)
				r.startBackgroundMaintenance(open)
			}
		}()

		// 假设 interval 为 10秒
		interval := 30 * time.Second
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		iteration := uint64(0)
		cleanupMultiplier := uint64(4) // 假设 interval=10s，则每5分钟执行一次清理

		logger.InfoWithCtx(getContextForLog(initCtx), "RabbitMQ maintenance worker started",
			logger.String("stats_interval", interval.String()),
			logger.String("cleanup_interval", (interval*time.Duration(cleanupMultiplier)).String()))
		for {
			select {
			case <-r.ctx.Done(): // 响应优雅关闭
				return
			case <-ticker.C:
				iteration++
				if open {
					r.printStats()
				}

				if iteration%cleanupMultiplier == 0 {
					r.cleanupInvalidProducers()
					iteration = 0
				}
			}
		}
	}()
}
