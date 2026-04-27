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

// --- 优化点 1: 定义常量和错误类型 ---
var (
	ErrPoolClosed = errors.New("pool is closed")
	ErrGetTimeout = errors.New("get connection timeout")
	// 性能优化：连接在 3 秒内被使用过，则跳过深度验证 (verifyConnection)
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
	traceSampleRate   float64            // Trace 采样率 (0.0-1.0)
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
		antsCap:           0,           // 默认使用ants库的默认容量
		healthCheckPeriod: time.Minute, // 默认健康检查周期为1分钟
		enableTrace:       true,        // 默认启用 Trace
		traceSampleRate:   1.0,         // 默认全量采样
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

// WithTraceSampleRate 设置 Trace 采样率 (0.0-1.0)
func WithTraceSampleRate(rate float64) PoolOption {
	return func(o *poolOptions) {
		if rate >= 0.0 && rate <= 1.0 {
			o.traceSampleRate = rate
		}
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
	totalConns int64        // 原子计数器，跟踪总连接数
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

	pool := &Pool{
		conns:    make([]*poolConn, 0, poolOpts.maxCap),
		url:      url,
		poolOpts: poolOpts,
		antsPool: antsPool,
		tracer:   otel.Tracer("gorabbitmq"), // 初始化 tracer
	}

	pool.cond = sync.NewCond(&pool.mutex)

	// 初始化连接
	for i := 0; i < poolOpts.initialCap; i++ {
		conn, err := NewConnection(ctx, url, poolOpts.connOpts...)
		if err != nil {
			// 关闭已经创建的连接
			if closeErr := pool.Close(ctx); closeErr != nil {
				fmt.Printf("close pool error: %v\n", closeErr)
			}
			return nil, err
		}

		pool.conns = append(pool.conns, &poolConn{
			conn:       conn,
			createTime: time.Now(),
			lastUsed:   time.Now(),
		})
		atomic.AddInt64(&pool.totalConns, 1)
	}

	// 启动空闲连接清理协程
	go pool.idleCleanup(ctx)

	//pool.poolOpts.zapLog.Info("[rabbitmq pool] created successfully",
	//	logger.String("url", url),
	//	logger.Int("initialCap", poolOpts.initialCap),
	//	logger.Int("maxCap", poolOpts.maxCap),
	//	logger.Duration("healthCheckPeriod", poolOpts.healthCheckPeriod))

	return pool, nil
}

// tryGetFromPool 尝试从连接池获取一个可用连接
// 返回连接、是否成功、是否需要继续循环
func (p *Pool) tryGetFromPool(ctx context.Context, startTime time.Time, span trace.Span) (conn *Connection, success bool, shouldContinue bool) {
	if len(p.conns) == 0 {
		return nil, false, true // 池为空，继续循环
	}

	lastIdx := len(p.conns) - 1
	pc := p.conns[lastIdx]
	p.conns = p.conns[:lastIdx]
	p.mutex.Unlock()

	// 检查连接健康度
	isFresh := time.Since(pc.lastUsed) < fastVerifyThreshold
	if pc.conn.CheckConnected(ctx) {
		if isFresh {
			if span != nil {
				span.SetAttributes(
					attribute.Bool("rabbitmq.pool.connection_reused", true),
					attribute.Bool("rabbitmq.pool.connection_verified_fast", true),
					attribute.Float64("rabbitmq.pool.get_duration_ms", float64(time.Since(startTime).Milliseconds())),
				)
				span.AddEvent("connection acquired from pool (fast path)")
			}
			return pc.conn, true, false // 成功获取
		}
		// 深度检查
		if verified, verifyErr := p.verifyConnection(ctx, pc.conn); verifyErr == nil && verified {
			if span != nil {
				span.SetAttributes(
					attribute.Bool("rabbitmq.pool.connection_reused", true),
					attribute.Bool("rabbitmq.pool.connection_verified_full", true),
					attribute.Float64("rabbitmq.pool.get_duration_ms", float64(time.Since(startTime).Milliseconds())),
				)
				span.AddEvent("connection acquired from pool (full verification)")
			}
			return pc.conn, true, false // 成功获取
		}
	}

	// 连接失效，销毁并减少计数
	pc.conn.Close()
	atomic.AddInt64(&p.totalConns, -1)
	if span != nil {
		span.AddEvent("stale connection removed from pool")
	}
	return nil, false, true // 连接失效，继续循环
}

// tryCreateNewConnection 尝试创建新连接（当池为空且未达到最大容量时）
func (p *Pool) tryCreateNewConnection(ctx context.Context, startTime time.Time, span trace.Span) (*Connection, bool, error) {
	if int(atomic.LoadInt64(&p.totalConns)) >= p.poolOpts.maxCap {
		return nil, false, nil // 已达到最大容量，需要等待
	}

	atomic.AddInt64(&p.totalConns, 1)
	p.mutex.Unlock()

	if span != nil {
		span.AddEvent("creating new connection")
	}
	conn, err := NewConnection(ctx, p.url, p.poolOpts.connOpts...)
	if err != nil {
		atomic.AddInt64(&p.totalConns, -1)
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return nil, true, err
	}
	if span != nil {
		span.SetAttributes(
			attribute.Bool("rabbitmq.pool.connection_reused", false),
			attribute.Float64("rabbitmq.pool.get_duration_ms", float64(time.Since(startTime).Milliseconds())),
		)
		span.AddEvent("new connection created")
	}
	return conn, true, nil
}

// waitForAvailableConnection 等待可用连接（使用 goroutine + channel 实现可取消的 Wait）
func (p *Pool) waitForAvailableConnection(ctx context.Context, span trace.Span) error {
	if span != nil {
		span.AddEvent("waiting for available connection")
	}
	waitChan := make(chan struct{}, 1)
	go func() {
		p.mutex.Lock()
		p.cond.Wait()
		p.mutex.Unlock()
		waitChan <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		if span != nil {
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, ctx.Err().Error())
		}
		return ctx.Err()
	case <-waitChan:
		// 被 Signal 唤醒，进入下一轮循环获取连接
		return nil
	}
}

// Get 从连接池获取一个连接
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

		// 阶段 1: 尝试从池中取连接
		if conn, success, shouldContinue := p.tryGetFromPool(ctx, startTime, span); success {
			return conn, nil
		} else if !shouldContinue {
			continue
		}

		// 阶段 2: 池空，尝试新建
		if conn, created, err := p.tryCreateNewConnection(ctx, startTime, span); created {
			return conn, err
		}

		// 阶段 3: 等待可用连接
		if waitErr := p.waitForAvailableConnection(ctx, span); waitErr != nil {
			return nil, waitErr
		}
		// 被唤醒后继续循环
	}
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

		conn, err := p.Get(ctx)
		if err == nil {
			if span != nil {
				span.SetAttributes(
					attribute.Int("rabbitmq.pool.retry_attempts_used", i),
				)
				span.AddEvent("connection acquired after retries")
			}
			return conn, nil
		}

		lastErr = err
		// 如果是连接池关闭错误，则不重试
		if err.Error() == "pool is closed" {
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, "pool closed")
			}
			break
		}

		// 指数退避延迟
		if i < maxRetries {
			delay := time.Duration(1<<uint(i)) * time.Millisecond * 100
			if delay > time.Second*3 {
				delay = time.Second * 3
			}

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
				return nil, ctx.Err()
			case <-timer.C:
				timer.Stop()
			}
		}
	}

	if span != nil {
		span.RecordError(lastErr)
		span.SetStatus(codes.Error, "all retries exhausted")
	}
	return nil, lastErr
}

// verifyConnection 验证连接是否真正可用
func (p *Pool) verifyConnection(ctx context.Context, conn *Connection) (bool, error) {
	// 通过获取底层AMQP连接并创建一个临时通道来验证连接
	amqpConn := conn.GetConn(ctx)
	if amqpConn == nil {
		return false, errors.New("underlying amqp connection is nil")
	}

	// 创建一个临时通道来验证连接
	ch, err := amqpConn.Channel()
	if err != nil {
		return false, err
	}

	// 关闭临时通道
	err = ch.Close()
	if err != nil {
		return false, err
	}

	return true, nil
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
		atomic.AddInt64(&p.totalConns, -1)
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

// idleCleanup 定期清理空闲连接
func (p *Pool) idleCleanup(ctx context.Context) {
	ticker := time.NewTicker(p.poolOpts.healthCheckPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.antsPool.Submit(func() {
				p.doCleanup(ctx)
				p.HealthCheck(ctx)
			}); err != nil {
				fmt.Printf("submit cleanup task error: %v\n", err)
			}
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
			atomic.AddInt64(&p.totalConns, -1)
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
		//nolint:staticcheck // ctx is reassigned for tracing consistency, though not directly used afterward
		ctx, span = p.tracer.Start(ctx, "rabbitmq.pool.close", trace.WithSpanKind(trace.SpanKindClient))
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
		atomic.AddInt64(&p.totalConns, -1)
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
		"totalConns":      atomic.LoadInt64(&p.totalConns),
		"available":       available,
		"poolSize":        len(p.conns),
		"maxCap":          p.poolOpts.maxCap,
		"closed":          p.closed,
		"antsPoolRunning": p.antsPool.Running(),
		"antsPoolCap":     p.antsPool.Cap(),
	}

	return stats
}

// HealthCheck 健康检查连接池中的连接
func (p *Pool) HealthCheck(ctx context.Context) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		return
	}

	removedCount := 0
	// 检查连接池中的连接是否仍然有效
	for i := len(p.conns) - 1; i >= 0; i-- {
		pc := p.conns[i]
		// 检查连接是否有效
		if !pc.conn.CheckConnected(ctx) {
			// 连接无效，关闭并移除
			pc.conn.Close()
			atomic.AddInt64(&p.totalConns, -1)
			p.conns = append(p.conns[:i], p.conns[i+1:]...)
			removedCount++
			continue
		}

		// 执行更严格的验证
		verified, err := p.verifyConnection(ctx, pc.conn)
		if !verified || err != nil {
			// 连接实际上不可用，关闭并移除
			pc.conn.Close()
			atomic.AddInt64(&p.totalConns, -1)
			p.conns = append(p.conns[:i], p.conns[i+1:]...)
			removedCount++
		}
	}

	if removedCount > 0 {
		logger.InfoWithCtx(ctx, "[rabbitmq pool] health check removed invalid connections",
			logger.Int("removedCount", removedCount))
	}
}
