package gorabbitmq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/ants/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// PoolOption 连接池配置选项函数类型
type PoolOption func(*poolOptions)

// poolOptions 连接池配置选项
type poolOptions struct {
	initialCap        int                // 初始连接数
	maxCap            int                // 最大连接数
	maxIdle           time.Duration      // 连接最大空闲时间
	zapLog            *zap.Logger        // 日志记录器
	connOpts          []ConnectionOption // 连接选项
	antsCap           int                // ants协程池容量
	healthCheckPeriod time.Duration      // 健康检查周期
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
		zapLog:            defaultLogger,
		connOpts:          []ConnectionOption{},
		antsCap:           0,           // 默认使用ants库的默认容量
		healthCheckPeriod: time.Minute, // 默认健康检查周期为1分钟
	}
}

// WithInitialCap 设置初始连接数
func WithInitialCap(cap int) PoolOption {
	return func(o *poolOptions) {
		if cap > 0 {
			o.initialCap = cap
		}
	}
}

// WithMaxCap 设置最大连接数
func WithMaxCap(cap int) PoolOption {
	return func(o *poolOptions) {
		if cap > 0 {
			o.maxCap = cap
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

// WithPoolLogger 设置连接池日志记录器
func WithPoolLogger(zapLog *zap.Logger) PoolOption {
	return func(o *poolOptions) {
		if zapLog != nil {
			o.zapLog = zapLog
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
func WithAntsPoolSize(cap int) PoolOption {
	return func(o *poolOptions) {
		if cap >= 0 {
			o.antsCap = cap
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
			pool.Close(ctx)
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
	//	zap.String("url", url),
	//	zap.Int("initialCap", poolOpts.initialCap),
	//	zap.Int("maxCap", poolOpts.maxCap),
	//	zap.Duration("healthCheckPeriod", poolOpts.healthCheckPeriod))

	return pool, nil
}

// Get 从连接池获取一个连接
func (p *Pool) Get(ctx context.Context) (*Connection, error) {
	// 创建追踪 span
	ctx, span := p.tracer.Start(ctx, "pool.get")
	defer span.End()

	span.SetAttributes(
		attribute.String("url", p.url),
	)

	for {
		// --- 阶段 1：快速尝试从池中提取现有连接 ---
		p.mutex.Lock()
		if p.closed {
			p.mutex.Unlock()
			err := errors.New("pool is closed")
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		var pc *poolConn
		if len(p.conns) > 0 {
			// 从切片末尾弹出，效率最高
			lastIdx := len(p.conns) - 1
			pc = p.conns[lastIdx]
			p.conns = p.conns[:lastIdx]
		}
		p.mutex.Unlock() // 拿到对象后立即释放锁，不阻塞其他协程获取连接
		// --- 阶段 2：在锁外进行耗时的连接验证 ---
		if pc != nil {
			// 执行网络探测和状态检查 (耗时操作)
			if pc.conn.CheckConnected(ctx) {
				verified, err := p.verifyConnection(pc.conn)
				if verified && err == nil {
					pc.lastUsed = time.Now()
					span.SetAttributes(attribute.Bool("reused", true))
					return pc.conn, nil
				}
			}

			// 验证失败，销毁该连接并更新计数
			pc.conn.Close()
			atomic.AddInt64(&p.totalConns, -1)

			// 继续循环尝试获取下一个连接
			continue
		}
		// --- 阶段 3：池中无可用连接，判断是否需要创建 ---
		p.mutex.Lock()
		// 使用原子操作检查当前总连接数
		if int(atomic.LoadInt64(&p.totalConns)) < p.poolOpts.maxCap {
			p.mutex.Unlock() // 创建连接是重 IO 操作，解锁以防阻塞
			conn, err := NewConnection(ctx, p.url, p.poolOpts.connOpts...)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}

			atomic.AddInt64(&p.totalConns, 1)
			span.SetAttributes(attribute.Bool("reused", false))
			return conn, nil
		}
		// --- 阶段 4：池已满且无可用，进入等待状态 ---
		// 等待 Release 方法通过 p.cond.Signal() 唤醒
		p.cond.Wait()
		p.mutex.Unlock()
	}
}

// GetWithRetry 从连接池获取一个连接，带重试机制
func (p *Pool) GetWithRetry(ctx context.Context, maxRetries int) (*Connection, error) {
	var lastErr error

	for i := 0; i <= maxRetries; i++ {
		conn, err := p.Get(ctx)
		if err == nil {
			return conn, nil
		}

		lastErr = err
		// 如果是连接池关闭错误，则不重试
		if err.Error() == "pool is closed" {
			break
		}

		// 指数退避延迟
		if i < maxRetries {
			delay := time.Duration(1<<uint(i)) * time.Millisecond * 100
			if delay > time.Second*3 {
				delay = time.Second * 3
			}

			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
				timer.Stop()
			}
		}
	}

	return nil, lastErr
}

// verifyConnection 验证连接是否真正可用
func (p *Pool) verifyConnection(conn *Connection) (bool, error) {
	// 通过获取底层AMQP连接并创建一个临时通道来验证连接
	amqpConn := conn.GetConn(context.Background())
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
	// 创建追踪 span
	ctx, span := p.tracer.Start(ctx, "pool.put")
	defer span.End()

	if conn == nil {
		span.SetStatus(codes.Ok, "nil connection")
		return nil
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		conn.Close()
		span.SetStatus(codes.Ok, "pool closed, connection closed")
		return nil
	}

	// 检查连接是否有效
	if !conn.CheckConnected(ctx) {
		atomic.AddInt64(&p.totalConns, -1)
		conn.Close()
		span.SetStatus(codes.Ok, "connection invalid, closed")
		return nil
	}

	// 将连接添加到池中
	pc := &poolConn{
		conn:     conn,
		lastUsed: time.Now(),
	}
	p.conns = append(p.conns, pc)

	//p.poolOpts.zapLog.Info("[rabbitmq pool] put connection back to pool", zap.Int("poolSize", len(p.conns)))

	span.SetAttributes(
		attribute.Int("pool_size", len(p.conns)),
	)

	// 通知等待的goroutine
	p.cond.Signal()
	return nil
}

// idleCleanup 定期清理空闲连接
func (p *Pool) idleCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	// 使用配置的健康检查周期
	healthCheckTicker := time.NewTicker(p.poolOpts.healthCheckPeriod)
	defer healthCheckTicker.Stop()

	for {
		// 使用 defer/recover 防止清理 goroutine 因异常而退出
		func() {
			defer func() {
				if r := recover(); r != nil {
					p.poolOpts.zapLog.Error("[rabbitmq pool] idleCleanup recovered from panic",
						zap.Any("panic", r))
				}
			}()

			select {
			case <-ticker.C:
				// 使用ants协程池处理空闲连接清理任务
				_ = p.antsPool.Submit(func() {
					p.doCleanup(ctx)
				})
			case <-healthCheckTicker.C:
				// 定期执行健康检查
				_ = p.antsPool.Submit(func() {
					p.HealthCheck(ctx)
				})
			case <-ctx.Done():
				// 上下文取消，退出清理循环
				return
			}
		}()

		// 防止过快重试
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Millisecond * 100):
			// 继续下一次循环
		}
	}
}

// doCleanup 实际执行清理工作的函数
func (p *Pool) doCleanup(ctx context.Context) {
	// 创建追踪 span
	var span trace.Span
	ctx, span = p.tracer.Start(ctx, "pool.cleanup")
	defer span.End()

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		span.SetStatus(codes.Ok, "pool closed")
		return
	}

	now := time.Now()
	// 保留的连接数不能少于初始容量
	minKeep := p.poolOpts.initialCap
	if minKeep > len(p.conns) {
		minKeep = len(p.conns)
	}

	removedCount := 0
	// 从后往前遍历，移除空闲时间过长的连接
	for i := len(p.conns) - 1; i >= minKeep; i-- {
		pc := p.conns[i]
		if now.Sub(pc.lastUsed) > p.poolOpts.maxIdle {
			// 关闭连接
			pc.conn.Close()
			atomic.AddInt64(&p.totalConns, -1)
			removedCount++

			// 从池中移除
			p.conns = append(p.conns[:i], p.conns[i+1:]...)

			p.poolOpts.zapLog.Debug("[rabbitmq pool] removed idle connection",
				zap.Duration("idleTime", now.Sub(pc.lastUsed)))
		}
	}

	span.SetAttributes(
		attribute.Int("removed_count", removedCount),
		attribute.Int("final_pool_size", len(p.conns)),
	)
}

// Close 关闭连接池
func (p *Pool) Close(ctx context.Context) error {
	// 创建追踪 span
	ctx, span := p.tracer.Start(ctx, "pool.close")
	defer span.End()

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		span.SetStatus(codes.Ok, "already closed")
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

	//p.poolOpts.zapLog.Info("[rabbitmq pool] closed")
	span.SetAttributes(
		attribute.Int("closed_connections", closedCount),
	)

	return nil
}

// Stats 返回连接池统计信息
func (p *Pool) Stats(ctx context.Context) map[string]interface{} {
	// 创建追踪 span
	ctx, span := p.tracer.Start(ctx, "pool.stats")
	defer span.End()

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

	span.SetAttributes(
		attribute.Int64("total_conns", stats["totalConns"].(int64)),
		attribute.Int("available", stats["available"].(int)),
		attribute.Int("pool_size", stats["poolSize"].(int)),
		attribute.Bool("closed", stats["closed"].(bool)),
	)

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
		verified, err := p.verifyConnection(pc.conn)
		if !verified || err != nil {
			// 连接实际上不可用，关闭并移除
			pc.conn.Close()
			atomic.AddInt64(&p.totalConns, -1)
			p.conns = append(p.conns[:i], p.conns[i+1:]...)
			removedCount++
		}
	}

	if removedCount > 0 {
		p.poolOpts.zapLog.Info("[rabbitmq pool] health check removed invalid connections",
			zap.Int("removedCount", removedCount))
	}
}
