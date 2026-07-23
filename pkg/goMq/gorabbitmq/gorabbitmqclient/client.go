// Package gorabbitmqclient 提供 RabbitMQ 客户端封装。
package gorabbitmqclient

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jinzhu/copier"
	"golang.org/x/sync/singleflight"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	rabbitmqInstances sync.Map
	sfGroup           singleflight.Group
	initCtx           = context.Background() // 初始化阶段使用的 context
)

// getContextForLog 智能选择用于日志记录的 context
func getContextForLog(ctx context.Context) context.Context {
	if ctx == nil {
		return initCtx
	}
	return ctx
}

// RabbitMQ 封装了 RabbitMQ 连接池及 ProducerPool 管理。
type RabbitMQ struct {
	pool           *gorabbitmq.Pool
	producerPools  sync.Map // exchangeName → *gorabbitmq.ProducerPool
	poolMaxCap     int32    // 连接池最大容量缓存
	poolMaxCapOnce sync.Once
	cancel         context.CancelFunc
}

// InitRabbitmq 初始化 RabbitMQ 连接池
func InitRabbitmq(name string, mqCfg any) error {
	if name == "" {
		return fmt.Errorf("RabbitMQ 模块初始化失败: 模块名称为空")
	}
	if mqCfg == nil {
		return fmt.Errorf("RabbitMQ 初始化失败: 配置对象为 nil")
	}

	// 使用 singleflight 防止重复初始化同一个 name
	_, err, _ := sfGroup.Do("init_"+name, func() (interface{}, error) {
		if _, ok := rabbitmqInstances.Load(name); ok {
			return nil, nil
		}
		var cfg struct {
			Pool config.Pool
		}
		if err := copier.Copy(&cfg, mqCfg); err != nil {
			return nil, fmt.Errorf("RabbitMQ 配置拷贝失败: %w", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		poolCfg := cfg.Pool

		if poolCfg.URL == "" {
			cancel()
			return nil, fmt.Errorf("RabbitMQ 初始化失败: URL 不能为空")
		}
		if !strings.HasPrefix(poolCfg.URL, "amqp://") && !strings.HasPrefix(poolCfg.URL, "amqps://") {
			cancel()
			return nil, fmt.Errorf("RabbitMQ 初始化失败: URL 协议非法 %s", poolCfg.URL)
		}

		// --- 为 Pool 内部的所有字段设置默认值 ---
		if poolCfg.InitialCap <= 0 {
			poolCfg.InitialCap = 2
		}
		if poolCfg.MaxCap <= 0 {
			poolCfg.MaxCap = 10
		}
		if poolCfg.MaxIdle <= 0 {
			poolCfg.MaxIdle = 60
		}
		if poolCfg.HealthCheckPeriod <= 0 {
			poolCfg.HealthCheckPeriod = 10
		}
		if poolCfg.ReconnectTime <= 0 {
			poolCfg.ReconnectTime = 5
		}
		if poolCfg.DialTimeout <= 0 {
			poolCfg.DialTimeout = 10
		}
		if poolCfg.Heartbeat <= 0 {
			poolCfg.Heartbeat = 10
		}

		// 创建连接池
		poolOpts := []gorabbitmq.PoolOption{
			gorabbitmq.WithInitialCap(poolCfg.InitialCap),
			gorabbitmq.WithMaxCap(poolCfg.MaxCap),
			gorabbitmq.WithMaxIdle(time.Second * time.Duration(poolCfg.MaxIdle)),
			gorabbitmq.WithHealthCheckPeriod(time.Second * time.Duration(poolCfg.HealthCheckPeriod)),
			gorabbitmq.WithConnOptions(
				gorabbitmq.WithReconnectTime(time.Second*time.Duration(poolCfg.ReconnectTime)),
				gorabbitmq.WithDialTimeout(time.Second*time.Duration(poolCfg.DialTimeout)),
				gorabbitmq.WithHeartbeat(time.Second*time.Duration(poolCfg.Heartbeat)),
			),
			gorabbitmq.WithTraceEnabled(true),
		}

		pool, err := gorabbitmq.NewPool(ctx, poolCfg.URL, poolOpts...)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("Failed to create RabbitMQ pool: %w", err)
		}

		instance := &RabbitMQ{
			pool:   pool,
			cancel: cancel,
		}
		rabbitmqInstances.Store(name, instance)
		logger.InfoWithCtx(getContextForLog(initCtx), "RabbitMQ module initialized successfully")

		return nil, nil
	})
	return err
}

// GetRabbitMQ 获取 RabbitMQ 实例
func GetRabbitMQ(name string) *RabbitMQ {
	val, ok := rabbitmqInstances.Load(name)
	if !ok {
		return nil
	}
	rabbitmq, ok := val.(*RabbitMQ)
	if !ok {
		return nil
	}
	return rabbitmq
}

// getOrCreateProducerPool 按 exchangeName 获取或创建 ProducerPool
func (r *RabbitMQ) getOrCreateProducerPool(exchangeName string) (*gorabbitmq.ProducerPool, error) {
	if val, ok := r.producerPools.Load(exchangeName); ok {
		if pp, ok := val.(*gorabbitmq.ProducerPool); ok {
			return pp, nil
		}
	}

	// 使用 singleflight 防止并发重复创建
	val, err, _ := sfGroup.Do("pool:"+exchangeName, func() (interface{}, error) {
		if v, ok := r.producerPools.Load(exchangeName); ok {
			return v, nil
		}
		exchange := gorabbitmq.NewDirectExchange(exchangeName, exchangeName)
		pp, err := gorabbitmq.NewProducerPool(r.pool, exchange, r.getPoolMaxCap(),
			gorabbitmq.WithProducerMsgDurable(true),
		)
		if err != nil {
			return nil, fmt.Errorf("create producer pool: %w", err)
		}
		r.producerPools.Store(exchangeName, pp)
		return pp, nil
	})
	if err != nil {
		return nil, err
	}
	pp, ok := val.(*gorabbitmq.ProducerPool)
	if !ok {
		return nil, fmt.Errorf("invalid producer pool type")
	}
	return pp, nil
}

// getPoolMaxCap 获取连接池最大容量（惰性缓存，ProducerPool maxSize 使用）
func (r *RabbitMQ) getPoolMaxCap() int {
	if r.pool == nil {
		return 10
	}
	r.poolMaxCapOnce.Do(func() {
		stats := r.pool.Stats(context.Background())
		if v, ok := stats["maxCap"].(int); ok && v > 0 {
			r.poolMaxCap = int32(v)
		} else {
			r.poolMaxCap = 10
		}
	})
	return int(r.poolMaxCap)
}

// GetConnection 从连接池获取一个连接（带重试），使用后必须调用 PutConnection 归还
func (r *RabbitMQ) GetConnection(ctx context.Context) (*gorabbitmq.Connection, error) {
	conn, err := r.pool.GetConnWithRetry(ctx, 3)
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	return conn, nil
}

// PutConnection 归还连接到连接池
func (r *RabbitMQ) PutConnection(ctx context.Context, conn *gorabbitmq.Connection) error {
	return r.pool.Put(ctx, conn)
}

// Close 优雅关闭：关闭所有 ProducerPool 和连接池
func (r *RabbitMQ) Close(ctx context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}

	// 关闭所有 ProducerPool
	r.producerPools.Range(func(_, value interface{}) bool {
		if pp, ok := value.(*gorabbitmq.ProducerPool); ok {
			pp.Close(ctx)
		}
		return true
	})

	if r.pool != nil {
		logger.InfoWithCtx(getContextForLog(ctx), "Closing RabbitMQ connection pool")
		return r.pool.Close(ctx)
	}
	return nil
}

// SendMessage 发送消息到指定的交换机和路由键
func (r *RabbitMQ) SendMessage(ctx context.Context, exchangeName, routingKey string, message string, messageID string) error {
	start := time.Now()
	var err error
	defer func() {
		if err != nil {
			logger.WarnWithCtx(getContextForLog(ctx), "SendMessage failed",
				logger.String("exchangeName", exchangeName),
				logger.String("routingKey", routingKey),
				logger.String("message_id", messageID),
				logger.String("params", message),
				logger.String("ms", fmt.Sprintf("%.4f", float64(time.Since(start).Nanoseconds())/1e6)),
				logger.Err(err))
		}
	}()

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pp, pErr := r.getOrCreateProducerPool(exchangeName)
		if pErr != nil {
			err = pErr
			r.handleRetry(ctx, attempt, maxRetries)
			continue
		}

		producer, pErr := pp.Get(ctx)
		if pErr != nil {
			err = pErr
			r.handleRetry(ctx, attempt, maxRetries)
			continue
		}

		err = producer.PublishDirect(ctx, routingKey, []byte(message), messageID)
		pp.Put(ctx, producer)

		if err == nil {
			return nil
		}

		// 连接/通道错误时重试（Producer 已归还 Pool，Pool 内部会检测失效并丢弃）
		if isConnectionError(err) {
			logger.WarnWithCtx(getContextForLog(ctx), "SendMessage connection error, retrying",
				logger.Int("attempt", attempt),
				logger.String("routingKey", routingKey),
				logger.Err(err))
			r.handleRetry(ctx, attempt, maxRetries)
			continue
		}

		// 业务逻辑错误（如 AccessRefused）重试通常无用
		return fmt.Errorf("rabbitmq_business_error: %w", err)
	}
	return err
}

// handleRetry 封装退避逻辑
func (r *RabbitMQ) handleRetry(ctx context.Context, attempt, maxRetries int) {
	if attempt >= maxRetries {
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
	if _, ok := err.(net.Error); ok {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "closed") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "not open") ||
		strings.Contains(msg, "504") || // channel error
		strings.Contains(msg, "320") // connection forced close
}

// Stats 返回连接池的实时统计信息。
// 可用于监控、调试或通过 HTTP 接口暴露。
func (r *RabbitMQ) Stats(ctx context.Context) map[string]interface{} {
	if r.pool == nil {
		return map[string]interface{}{
			"error": "pool is nil",
		}
	}
	return r.pool.Stats(ctx)
}
