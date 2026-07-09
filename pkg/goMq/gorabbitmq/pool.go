package gorabbitmq

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	// ErrPoolClosed 连接池已关闭时返回的错误
	ErrPoolClosed = errors.New("pool is closed")
	// 连接在 3 秒内被使用过，则跳过深度验证
	fastVerifyThreshold = 3 * time.Second
)

// poolConn 连接池中的连接
type poolConn struct {
	connInfo   *Connection // RabbitMQ 连接
	createTime time.Time   // 创建时间
	lastUsed   time.Time   // 最后使用时间
}

// Pool 连接池结构
type Pool struct {
	poolMu      sync.Mutex    // 互斥锁
	url         string        // 连接 URL
	poolOpts    *poolOptions  // 连接池配置选项
	poolConns   []*poolConn   // 连接池中的连接列表
	connReadyCh chan struct{} // 连接可用信号（缓冲 1，select 安全唤醒）
	isClosed    atomic.Bool   // 连接池是否已关闭
	totalConns  atomic.Int64  // 原子计数器，跟踪总连接数
	tracer      trace.Tracer  // OpenTelemetry tracer
}

// NewPool 创建新的连接池
func NewPool(ctx context.Context, url string, opts ...PoolOption) (*Pool, error) {
	if url == "" {
		return nil, errors.New("url is empty")
	}
	o := defaultPoolOptions()
	o.apply(opts...)

	p := &Pool{
		poolConns:   make([]*poolConn, 0, o.maxCap),
		url:         url,
		poolOpts:    o,
		connReadyCh: make(chan struct{}, 1),
		tracer:      otel.Tracer("gomq"),
	}

	// 初始化连接
	for i := 0; i < o.initialCap; i++ {
		conn, err := NewConnection(ctx, url, o.connOpts...)
		if err != nil {
			// 关闭已经创建的连接
			if closeErr := p.Close(ctx); closeErr != nil {
				fmt.Printf("close pool error: %v\n", closeErr)
			}
			return nil, err
		}

		p.poolConns = append(p.poolConns, &poolConn{
			connInfo:   conn,
			createTime: time.Now(),
			lastUsed:   time.Now(),
		})
		p.totalConns.Add(1)
	}

	// 启动空闲连接清理协程（使用 WithoutCancel 隔离调用方 ctx 的取消信号）
	go p.idleCleanup(context.WithoutCancel(ctx))

	return p, nil
}

// getConnFromPool 从池中取出一个连接（调用方必须持有锁）
// 返回 nil 表示池为空
func (p *Pool) getConnFromPool() *poolConn {
	if len(p.poolConns) == 0 {
		return nil
	}
	lastIdx := len(p.poolConns) - 1
	pc := p.poolConns[lastIdx]
	p.poolConns = p.poolConns[:lastIdx]
	return pc
}

// checkConnected 检查连接是否可用。
// 采用两级验证策略：
//   - 快速路径：近期使用过的连接（3 秒内）仅检查本地状态，无需网络交互
//   - 完整路径：闲置超过 3 秒的连接创建临时 AMQP channel 做深度验证
//
// 参数:
//   - ctx: 上下文，用于日志追踪
//   - pc: 池内连接对象
//
// 返回:
//   - bool: true 表示连接可用
func checkConnected(ctx context.Context, pc *poolConn) bool {
	if !pc.connInfo.CheckConnected(ctx) {
		return false
	}
	// 近期使用过的连接跳过深度验证
	if time.Since(pc.lastUsed) < fastVerifyThreshold {
		return true
	}
	// 深度验证：创建临时 channel 确认连接可用
	amqpConn := pc.connInfo.GetConn(ctx)
	if amqpConn == nil {
		return false
	}
	ch, err := amqpConn.Channel()
	if err != nil {
		return false
	}
	if closeErr := ch.Close(); closeErr != nil {
		logger.WarnWithCtx(ctx, "verify connection: close temp channel failed",
			logger.Err(closeErr),
		)
	}
	return true
}

// GetConn 从连接池获取一个连接
// 锁管理策略：Get 全程集中管理 mutex，辅助函数不碰锁
//
//nolint:gocognit // 认知复杂度略高于阈值，函数逻辑已有清晰阶段划分
func (p *Pool) GetConn(ctx context.Context) (*Connection, error) {
	var span trace.Span
	if p.poolOpts.enableTrace {
		ctx, span = p.tracer.Start(ctx, "rabbitmq.pool.get", trace.WithSpanKind(trace.SpanKindClient))
		defer span.End()
	}

	startTime := time.Now()
	for {
		p.poolMu.Lock()
		if p.isClosed.Load() {
			p.poolMu.Unlock()
			if span != nil {
				span.RecordError(ErrPoolClosed)
				span.SetStatus(codes.Error, ErrPoolClosed.Error())
			}
			return nil, ErrPoolClosed
		}

		// 阶段 1: 从池中弹出连接（持有锁）
		if pConn := p.getConnFromPool(); pConn != nil {
			p.poolMu.Unlock()
			if checkConnected(ctx, pConn) {
				if span != nil {
					isFresh := time.Since(pConn.lastUsed) < fastVerifyThreshold
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
				return pConn.connInfo, nil
			}
			// 连接不可用，销毁并继续
			pConn.connInfo.Close()
			p.totalConns.Add(-1)
			if span != nil {
				span.AddEvent("stale connection discarded")
			}
			continue
		}

		// 阶段 2: 池空，尝试创建新连接（持有锁检查容量）
		if int(p.totalConns.Load()) < p.poolOpts.maxCap {
			p.totalConns.Add(1)
			p.poolMu.Unlock()
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
// 调用方必须持有 poolMu，返回后 poolMu 已释放
func (p *Pool) waitForConnection(ctx context.Context, span trace.Span) error {
	if span != nil {
		span.AddEvent("waiting for available connection")
	}
	p.poolMu.Unlock()

	select {
	case <-p.connReadyCh:
		// 有连接被归还，回 Get 重新尝试
		return nil
	case <-ctx.Done():
		if span != nil {
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, ctx.Err().Error())
		}
		return ctx.Err()
	}
}

// calculateRetryDelay 计算重试延迟时间，采用指数退避策略。
//   - attempt=0 → 100ms, attempt=1 → 200ms, attempt=2 → 400ms, ...
//   - 最大延迟上限 3 秒
//
// 参数:
//   - attempt: 当前重试次数（从 0 开始）
//
// 返回:
//   - time.Duration: 退避后的基础延迟时间（不含随机抖动）
func calculateRetryDelay(attempt int) time.Duration {
	delay := time.Duration(1<<uint(attempt)) * time.Millisecond * 100
	if delay > time.Second*3 {
		delay = time.Second * 3
	}
	return delay
}

// handleGetConnRetry 执行单次 GetConn 并处理重试逻辑。
// 参数:
//   - ctx: 上下文，用于超时控制
//   - i: 当前尝试次数（从 0 开始）
//   - maxRetries: 最大重试次数
//   - span: OpenTelemetry trace span，用于记录重试事件
//
// 返回:
//   - *Connection: 成功时返回连接
//   - bool: true 表示调用方需要继续重试，false 表示停止
//   - error: 失败时的错误信息
//
// handleGetConnRetry 处理单次获取连接的重试逻辑
func (p *Pool) handleGetConnRetry(ctx context.Context, i, maxRetries int, span trace.Span) (*Connection, bool, error) {
	conn, err := p.GetConn(ctx)
	if err == nil {
		if span != nil {
			span.SetAttributes(
				attribute.Int("rabbitmq.pool.retry_attempts_used", i),
			)
			span.AddEvent("connection acquired after retries")
		}
		return conn, false, nil
	}

	// 如果是连接池关闭错误，则不重试
	if errors.Is(err, ErrPoolClosed) {
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "pool closed")
		}
		return nil, false, err
	}

	// 最后一次尝试失败后不再退避
	if i >= maxRetries {
		return nil, false, err
	}

	// 指数退避 + 随机抖动，避免惊群效应
	delay := calculateRetryDelay(i)
	jitterRange := float64(delay) * 0.15
	actualDelay := time.Duration(float64(delay) + (rand.Float64()*2-1)*jitterRange)

	if span != nil {
		span.AddEvent(fmt.Sprintf("waiting %.0fms before retry", float64(actualDelay.Milliseconds())))
	}

	timer := time.NewTimer(actualDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		if span != nil {
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, ctx.Err().Error())
		}
		return nil, false, ctx.Err()
	case <-timer.C:
	}

	return nil, true, err
}

// GetConnWithRetry 从连接池获取一个连接，失败时按指数退避+随机抖动重试。
// 重试过程中遇到 ErrPoolClosed 或 ctx 取消时立即停止。
// 参数:
//   - ctx: 上下文，用于控制重试生命周期
//   - maxRetries: 最大重试次数（额外尝试次数，总共执行 maxRetries+1 次 Get）
//
// 返回:
//   - *Connection: 成功时返回可用连接
//   - error: 全部重试耗尽后返回最后一次错误
//
// GetConnWithRetry 从连接池获取一个连接，带重试机制
func (p *Pool) GetConnWithRetry(ctx context.Context, maxRetries int) (*Connection, error) {
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

		conn, shouldContinue, err := p.handleGetConnRetry(ctx, i, maxRetries, span)
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

	p.poolMu.Lock()
	defer p.poolMu.Unlock()
	if p.isClosed.Load() || !conn.CheckConnected(ctx) {
		p.totalConns.Add(-1)
		conn.Close()
		if span != nil {
			span.SetAttributes(attribute.Bool("rabbitmq.pool.connection_returned", false))
			span.SetStatus(codes.Ok, "connection invalid, closed")
		}
		return nil
	}

	// 更新最后使用时间，配合 Get 中的 fastVerifyThreshold
	pConn := &poolConn{
		connInfo: conn,
		lastUsed: time.Now(),
	}
	p.poolConns = append(p.poolConns, pConn)

	if span != nil {
		span.SetAttributes(
			attribute.Int("rabbitmq.pool.size", len(p.poolConns)),
			attribute.Bool("rabbitmq.pool.connection_returned", true),
		)
		span.AddEvent("connection returned to pool")
	}
	// 唤醒等待的 Get
	select {
	case p.connReadyCh <- struct{}{}:
	default:
	}
	return nil
}

// idleCleanup 定期清理空闲连接（后台 goroutine，带 panic recover）
func (p *Pool) idleCleanup(ctx context.Context) {
	ticker := time.NewTicker(p.poolOpts.healthCheckPeriod)
	defer ticker.Stop()
	for {
		if p.isClosed.Load() {
			return
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WarnWithCtx(ctx, "[rabbitmq pool] idleCleanup recovered from panic",
						logger.Any("panic", r))
				}
			}()
			<-ticker.C
			p.cleanup(ctx)
		}()
	}
}

// cleanup 清理空闲和无效连接（一次加锁完成两件事）
func (p *Pool) cleanup(ctx context.Context) {
	p.poolMu.Lock()
	defer p.poolMu.Unlock()

	if p.isClosed.Load() {
		return
	}

	now := time.Now()
	minKeep := p.poolOpts.initialCap
	removedCount := 0

	for i := 0; i < len(p.poolConns); {
		pc := p.poolConns[i]
		// 优先移除不可用连接，不受 minKeep 限制
		if !checkConnected(ctx, pc) {
			pc.connInfo.Close()
			p.totalConns.Add(-1)
			p.poolConns = append(p.poolConns[:i], p.poolConns[i+1:]...)
			removedCount++
			continue
		}
		// 再移除超时空闲连接，受 minKeep 限制
		if len(p.poolConns) > minKeep && now.Sub(pc.lastUsed) > p.poolOpts.maxIdle {
			pc.connInfo.Close()
			p.totalConns.Add(-1)
			p.poolConns = append(p.poolConns[:i], p.poolConns[i+1:]...)
			removedCount++
			continue
		}
		i++
	}

	if removedCount > 0 {
		logger.InfoWithCtx(ctx, "[rabbitmq pool] cleanup removed idle/invalid connections",
			logger.Int("removed_count", removedCount),
			logger.Int("remaining_pool_size", len(p.poolConns)))
	}
}

// Close 关闭连接池
func (p *Pool) Close(ctx context.Context) error {
	var span trace.Span
	if p.poolOpts.enableTrace {
		_, span = p.tracer.Start(ctx, "rabbitmq.pool.close", trace.WithSpanKind(trace.SpanKindClient))
		defer span.End()
	}

	p.poolMu.Lock()
	defer p.poolMu.Unlock()

	if p.isClosed.Load() {
		if span != nil {
			span.SetStatus(codes.Ok, "already closed")
		}
		return nil
	}

	p.isClosed.Store(true)

	// 关闭所有连接
	closedCount := 0
	for _, pc := range p.poolConns {
		pc.connInfo.Close()
		p.totalConns.Add(-1)
		closedCount++
	}

	p.poolConns = nil

	// 关闭信号通道，唤醒所有等待的 Get
	close(p.connReadyCh)

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
	p.poolMu.Lock()
	defer p.poolMu.Unlock()

	// 计算可用连接数
	available := 0
	for _, pc := range p.poolConns {
		if pc.connInfo.CheckConnected(ctx) {
			available++
		}
	}

	stats := map[string]interface{}{
		"totalConns": p.totalConns.Load(),
		"available":  available,
		"poolSize":   len(p.poolConns),
		"maxCap":     p.poolOpts.maxCap,
		"closed":     p.isClosed.Load(),
	}

	return stats
}
