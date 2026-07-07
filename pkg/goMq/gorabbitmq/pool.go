package gorabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/ants/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	// ErrPoolClosed 连接池已关闭时返回的错误
	ErrPoolClosed = errors.New("pool is closed")
	// ErrGetTimeout 获取连接超时时返回的错误
	ErrGetTimeout = errors.New("get connection timeout")
	// 连接在 3 秒内被使用过，则跳过深度验证
	fastVerifyThreshold = 3 * time.Second
)

// PoolOption 连接池配置选项函数类型
type PoolOption func(*poolOptions)

// poolOptions 连接池配置选项
type poolOptions struct {
	initialCap        int                // 初始连接数
	maxCap            int                // 最大连接数
	maxIdle           time.Duration      // 连接最大空闲时间
	connOpts          []ConnectionOption // 连接选项
	antsCap           int                // ants协程池容量
	healthCheckPeriod time.Duration      // 健康检查周期
	enableTrace       bool               // 是否启用 Trace
}

// apply 应用连接池配置选项
func (o *poolOptions) apply(opts ...PoolOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultPoolOptions 默认连接池配置选项
func defaultPoolOptions() *poolOptions {
	return &poolOptions{
		initialCap:        5,
		maxCap:            30,
		maxIdle:           time.Minute * 10,
		connOpts:          []ConnectionOption{},
		antsCap:           0,
		healthCheckPeriod: time.Minute,
		enableTrace:       true,
	}
}

// WithInitialCap 设置初始连接数
func WithInitialCap(initialCap int) PoolOption {
	return func(o *poolOptions) {
		if initialCap > 0 {
			o.initialCap = initialCap
		}
	}
}

// WithMaxCap 设置最大连接数
func WithMaxCap(maxCap int) PoolOption {
	return func(o *poolOptions) {
		if maxCap > 0 {
			o.maxCap = maxCap
		}
	}
}

// WithMaxIdle 设置连接最大空闲时间
func WithMaxIdle(d time.Duration) PoolOption {
	return func(o *poolOptions) {
		if d > 0 {
			o.maxIdle = d
		}
	}
}

// WithConnOptions 设置连接选项
func WithConnOptions(connOpts ...ConnectionOption) PoolOption {
	return func(o *poolOptions) {
		o.connOpts = connOpts
	}
}

// WithAntsPoolSize 设置ants协程池大小
func WithAntsPoolSize(antsCap int) PoolOption {
	return func(o *poolOptions) {
		if antsCap >= 0 {
			o.antsCap = antsCap
		}
	}
}

// WithHealthCheckPeriod 设置健康检查周期
func WithHealthCheckPeriod(d time.Duration) PoolOption {
	return func(o *poolOptions) {
		if d > 0 {
			o.healthCheckPeriod = d
		}
	}
}

// WithTraceEnabled 启用或禁用 Trace
func WithTraceEnabled(enabled bool) PoolOption {
	return func(o *poolOptions) {
		o.enableTrace = enabled
	}
}

// poolConn 连接池中的连接
type poolConn struct {
	conn       *Connection // RabbitMQ 连接
	createTime time.Time   // 创建时间
	lastUsed   time.Time   // 最后使用时间
}

// Pool 连接池结构
type Pool struct {
	mutex      sync.Mutex   // 互斥锁
	cond       *sync.Cond   // 条件变量
	conns      []*poolConn  // 连接池中的连接列表
	url        string       // 连接 URL
	poolOpts   *poolOptions // 连接池配置选项
	closed     bool         // 连接池是否已关闭
	totalConns atomic.Int64 // 原子计数器，跟踪总连接数
	antsPool   *ants.Pool   // ants协程池，用于处理后台任务
	tracer     trace.Tracer // OpenTelemetry tracer
}

// NewPool 创建新的连接池
func NewPool(ctx context.Context, url string, opts ...PoolOption) (*Pool, error) {
	if url == "" {
		return nil, errors.New("url is empty")
	}

	poolOpts := defaultPoolOptions()
	poolOpts.apply(opts...)

	// 创建ants协程池
	var antsPool *ants.Pool
	var err error
	if poolOpts.antsCap > 0 {
		antsPool, err = ants.NewPool(poolOpts.antsCap)
	} else {
		// 使用ants库的默认配置
		antsPool, err = ants.NewPool(-1)
	}

	if err != nil {
		return nil, err
	}

	p := &Pool{
		conns:    make([]*poolConn, 0, poolOpts.maxCap),
		url:      url,
		poolOpts: poolOpts,
		antsPool: antsPool,
		tracer:   otel.Tracer("gorabbitmq"), // 初始化 tracer
	}

	p.cond = sync.NewCond(&p.mutex)

	// 初始化连接
	for i := 0; i < poolOpts.initialCap; i++ {
		conn, err := NewConnection(ctx, url, poolOpts.connOpts...)
		if err != nil {
			// 关闭已经创建的连接
			if closeErr := p.Close(ctx); closeErr != nil {
				fmt.Printf("close pool error: %v\n", closeErr)
			}
			return nil, err
		}

		p.conns = append(p.conns, &poolConn{
			conn:       conn,
			createTime: time.Now(),
			lastUsed:   time.Now(),
		})
		p.totalConns.Add(1)
	}

	// 启动空闲连接清理协程
	go p.idleCleanup(ctx)

	//pool.poolOpts.zapLog.Info("[rabbitmq pool] created successfully",
	//	logger.String("url", url),
	//	logger.Int("initialCap", poolOpts.initialCap),
	//	logger.Int("maxCap", poolOpts.maxCap),
	//	logger.Duration("healthCheckPeriod", poolOpts.healthCheckPeriod))

	return p, nil
}

// popFromPoolLocked 从池中弹出最后一个连接（调用方必须持有锁）
// 返回 nil 表示池为空
func (p *Pool) popFromPoolLocked() *poolConn {
	if len(p.conns) == 0 {
		return nil
	}
	lastIdx := len(p.conns) - 1
	pc := p.conns[lastIdx]
	p.conns = p.conns[:lastIdx]
	return pc
}

// isConnectionUsable 检查连接是否可用
func isConnectionUsable(ctx context.Context, pc *poolConn) bool {
	if !pc.conn.CheckConnected(ctx) {
		return false
	}
	// 近期使用过的连接跳过深度验证
	if time.Since(pc.lastUsed) < fastVerifyThreshold {
		return true
	}
	// 深度验证：创建临时 channel 确认连接可用
	amqpConn := pc.conn.GetConn(ctx)
	if amqpConn == nil {
		return false
	}
	ch, err := amqpConn.Channel()
	if err != nil {
		return false
	}
	if closeErr := ch.Close(); closeErr != nil {
		logger.WarnWithCtx(context.Background(), "verify connection: close temp channel failed",
			logger.Err(closeErr),
		)
	}
	return true
}

// Get 从连接池获取一个连接
// 锁管理策略：Get 全程集中管理 mutex，辅助函数不碰锁
//
//nolint:gocognit // 认知复杂度略高于阈值，函数逻辑已有清晰阶段划分
func (p *Pool) Get(ctx context.Context) (*Connection, error) {
	var span trace.Span
	if p.poolOpts.enableTrace {
		ctx, span = p.tracer.Start(ctx, "rabbitmq.pool.get", trace.WithSpanKind(trace.SpanKindClient))
		defer span.End()
	}

	startTime := time.Now()
	for {
		p.mutex.Lock()
		if p.closed {
			p.mutex.Unlock()
			if span != nil {
				span.RecordError(ErrPoolClosed)
				span.SetStatus(codes.Error, ErrPoolClosed.Error())
			}
			return nil, ErrPoolClosed
		}

		// 阶段 1: 从池中弹出连接（持有锁）
		if pc := p.popFromPoolLocked(); pc != nil {
			p.mutex.Unlock()
			if isConnectionUsable(ctx, pc) {
				if span != nil {
					isFresh := time.Since(pc.lastUsed) < fastVerifyThreshold
					verifyType := "full"
					if isFresh {
						verifyType = "fast"
					}
					span.SetAttributes(
						attribute.Bool("rabbitmq.pool.connection_reused", true),
						attribute.Bool("rabbitmq.pool.connection_verified_"+verifyType, true),
						attribute.Float64("rabbitmq.pool.get_duration_ms", float64(time.Since(startTime).Milliseconds())),
					)
					span.AddEvent(fmt.Sprintf("connection acquired from pool (%s path)", verifyType))
				}
				return pc.conn, nil
			}
			// 连接不可用，销毁并继续
			pc.conn.Close()
			p.totalConns.Add(-1)
			if span != nil {
				span.AddEvent("stale connection discarded")
			}
			continue
		}

		// 阶段 2: 池空，尝试创建新连接（持有锁检查容量）
		if int(p.totalConns.Load()) < p.poolOpts.maxCap {
			p.totalConns.Add(1)
			p.mutex.Unlock()

			if span != nil {
				span.AddEvent("creating new connection")
			}
			conn, err := NewConnection(ctx, p.url, p.poolOpts.connOpts...)
			if err != nil {
				p.totalConns.Add(-1)
				if span != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				return nil, err
			}
			if span != nil {
				span.SetAttributes(
					attribute.Bool("rabbitmq.pool.connection_reused", false),
					attribute.Float64("rabbitmq.pool.get_duration_ms", float64(time.Since(startTime).Milliseconds())),
				)
				span.AddEvent("new connection created")
			}
			return conn, nil
		}

		// 阶段 3: 已达最大容量，等待可用连接
		if waitErr := p.waitForConnection(ctx, span); waitErr != nil {
			return nil, waitErr
		}
	}
}

// waitForConnection 在池满时等待可用连接
// 调用方必须持有 mutex，返回后 mutex 已释放
func (p *Pool) waitForConnection(ctx context.Context, span trace.Span) error {
	if span != nil {
		span.AddEvent("waiting for available connection")
	}
	// 先检查 ctx 是否已取消，避免已取消 ctx 的 Signal 在 Wait 前丢失导致死锁
	if ctx.Err() != nil {
		p.mutex.Unlock()
		if span != nil {
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, ctx.Err().Error())
		}
		return ctx.Err()
	}
	waitDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			p.cond.Signal()
		case <-waitDone:
		}
	}()
	p.cond.Wait()
	close(waitDone)
	p.mutex.Unlock()
	if ctx.Err() != nil {
		if span != nil {
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, ctx.Err().Error())
		}
		return ctx.Err()
	}
	return nil
}

// calculateRetryDelay 计算重试延迟时间
func calculateRetryDelay(attempt int) time.Duration {
	delay := time.Duration(1<<uint(attempt)) * time.Millisecond * 100
	if delay > time.Second*3 {
		delay = time.Second * 3
	}
	return delay
}

// handleGetRetry 处理单次获取连接的重试逻辑
func (p *Pool) handleGetRetry(ctx context.Context, i, maxRetries int, span trace.Span) (*Connection, bool, error) {
	conn, err := p.Get(ctx)
	if err == nil {
		if span != nil {
			span.SetAttributes(
				attribute.Int("rabbitmq.pool.retry_attempts_used", i),
			)
			span.AddEvent("connection acquired after retries")
		}
		return conn, false, nil // 成功，不需要继续重试
	}

	// 如果是连接池关闭错误，则不重试
	if errors.Is(err, ErrPoolClosed) {
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "pool closed")
		}
		return nil, false, err // 失败，停止重试
	}

	// 指数退避延迟
	if i < maxRetries {
		delay := calculateRetryDelay(i)

		if span != nil {
			span.AddEvent(fmt.Sprintf("waiting %.0fms before retry", float64(delay.Milliseconds())))
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			if span != nil {
				span.RecordError(ctx.Err())
				span.SetStatus(codes.Error, ctx.Err().Error())
			}
			return nil, false, ctx.Err()
		case <-timer.C:
			timer.Stop()
		}
	}

	return nil, true, err // 失败，需要继续重试
}

// GetWithRetry 从连接池获取一个连接，带重试机制
func (p *Pool) GetWithRetry(ctx context.Context, maxRetries int) (*Connection, error) {
	var span trace.Span
	if p.poolOpts.enableTrace {
		ctx, span = p.tracer.Start(ctx, "rabbitmq.pool.get_with_retry", trace.WithSpanKind(trace.SpanKindClient))
		defer span.End()
		span.SetAttributes(
			attribute.Int("rabbitmq.pool.max_retries", maxRetries),
		)
	}

	var lastErr error
	for i := 0; i <= maxRetries; i++ {
		if span != nil && i > 0 {
			span.AddEvent(fmt.Sprintf("retry attempt %d/%d", i, maxRetries))
		}

		conn, shouldContinue, err := p.handleGetRetry(ctx, i, maxRetries, span)
		if !shouldContinue {
			return conn, err
		}
		lastErr = err
	}

	if span != nil {
		span.RecordError(lastErr)
		span.SetStatus(codes.Error, "all retries exhausted")
	}
	return nil, lastErr
}

// Put 将连接放回连接池
func (p *Pool) Put(ctx context.Context, conn *Connection) error {
	var span trace.Span
	if p.poolOpts.enableTrace {
		ctx, span = p.tracer.Start(ctx, "rabbitmq.pool.put", trace.WithSpanKind(trace.SpanKindClient))
		defer span.End()
	}

	if conn == nil {
		if span != nil {
			span.SetStatus(codes.Ok, "nil connection")
		}
		return nil
	}

	p.mutex.Lock()
	if p.closed || !conn.CheckConnected(ctx) {
		p.mutex.Unlock()
		p.totalConns.Add(-1)
		conn.Close()
		if span != nil {
			span.SetAttributes(attribute.Bool("rabbitmq.pool.connection_returned", false))
			span.SetStatus(codes.Ok, "connection invalid, closed")
		}
		return nil
	}

	// 更新最后使用时间，配合 Get 中的 fastVerifyThreshold
	pc := &poolConn{
		conn:     conn,
		lastUsed: time.Now(),
	}
	p.conns = append(p.conns, pc)

	if span != nil {
		span.SetAttributes(
			attribute.Int("rabbitmq.pool.size", len(p.conns)),
			attribute.Bool("rabbitmq.pool.connection_returned", true),
		)
		span.AddEvent("connection returned to pool")
	}
	// 唤醒 Get 中的 Wait
	p.cond.Signal()
	p.mutex.Unlock()
	return nil
}

// idleCleanup 定期清理空闲连接（后台 goroutine，带 panic recover）
func (p *Pool) idleCleanup(ctx context.Context) {
	ticker := time.NewTicker(p.poolOpts.healthCheckPeriod)
	defer ticker.Stop()

	for {
		if err := func() error {
			defer func() {
				if r := recover(); r != nil {
					logger.WarnWithCtx(context.Background(), "[rabbitmq pool] idleCleanup recovered from panic",
						logger.Any("panic", r))
				}
			}()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				return p.antsPool.Submit(func() {
					p.doCleanup(ctx)
					p.HealthCheck(ctx)
				})
			}
		}(); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			fmt.Printf("submit cleanup task error: %v\n", err)
		}
	}
}

// doCleanup 实际执行清理工作的函数
func (p *Pool) doCleanup(ctx context.Context) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		return
	}

	now := time.Now()
	// 保留的连接数不能少于初始容量
	minKeep := p.poolOpts.initialCap
	if minKeep > len(p.conns) {
		minKeep = len(p.conns)
	}

	// 从后往前遍历，移除空闲时间过长的连接
	removedCount := 0
	for i := len(p.conns) - 1; i >= minKeep; i-- {
		pc := p.conns[i]
		if now.Sub(pc.lastUsed) > p.poolOpts.maxIdle {
			// 关闭连接
			pc.conn.Close()
			p.totalConns.Add(-1)
			// 从池中移除
			p.conns = append(p.conns[:i], p.conns[i+1:]...)
			removedCount++
		}
	}

	if removedCount > 0 {
		logger.InfoWithCtx(ctx, "[rabbitmq pool] cleanup completed",
			logger.Int("removed_count", removedCount),
			logger.Int("remaining_pool_size", len(p.conns)))
	}
}

// Close 关闭连接池
func (p *Pool) Close(ctx context.Context) error {
	var span trace.Span
	if p.poolOpts.enableTrace {
		_, span = p.tracer.Start(ctx, "rabbitmq.pool.close", trace.WithSpanKind(trace.SpanKindClient))
		defer span.End()
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		if span != nil {
			span.SetStatus(codes.Ok, "already closed")
		}
		return nil
	}

	p.closed = true

	// 关闭所有连接
	closedCount := 0
	for _, pc := range p.conns {
		pc.conn.Close()
		p.totalConns.Add(-1)
		closedCount++
	}

	p.conns = nil

	// 通知所有等待的goroutine
	p.cond.Broadcast()

	// 释放ants协程池资源
	p.antsPool.Release()

	if span != nil {
		span.SetAttributes(
			attribute.Int("rabbitmq.pool.closed_connections", closedCount),
		)
		span.AddEvent("pool closed")
	}

	return nil
}

// Stats 返回连接池统计信息
func (p *Pool) Stats(ctx context.Context) map[string]interface{} {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// 计算可用连接数
	available := 0
	for _, pc := range p.conns {
		if pc.conn.CheckConnected(ctx) {
			available++
		}
	}

	stats := map[string]interface{}{
		"totalConns":      p.totalConns.Load(),
		"available":       available,
		"poolSize":        len(p.conns),
		"maxCap":          p.poolOpts.maxCap,
		"closed":          p.closed,
		"antsPoolRunning": p.antsPool.Running(),
		"antsPoolCap":     p.antsPool.Cap(),
	}

	return stats
}

// HealthCheck 健康检查所有连接，移除无效连接
func (p *Pool) HealthCheck(ctx context.Context) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		return
	}

	removedCount := 0
	for i := len(p.conns) - 1; i >= 0; i-- {
		pc := p.conns[i]
		if !isConnectionUsable(ctx, pc) {
			pc.conn.Close()
			p.totalConns.Add(-1)
			p.conns = append(p.conns[:i], p.conns[i+1:]...)
			removedCount++
		}
	}

	if removedCount > 0 {
		logger.InfoWithCtx(ctx, "[rabbitmq pool] health check removed invalid connections",
			logger.Int("removedCount", removedCount))
	}
}
