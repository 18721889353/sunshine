package gogroutine

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/ants/v2"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ============================================================================
// 池实例实现 - 参考字节跳动 gopool 设计
// ============================================================================

// poolInstance 池实例，实现 Pool 接口。
// 每个实例独立管理自己的 goroutine 池、指标和生命周期。
type poolInstance struct {
	name         string                                   // 池名称（唯一标识）
	pool         *ants.Pool                               // 底层 ants 协程池
	panicHandler func(ctx context.Context, r interface{}) // 自定义 panic 处理器
	metrics      *instanceMetrics                         // 实例级指标
	capacity     int32                                    // 池容量
	createdAt    time.Time                                // 创建时间
	mu           sync.RWMutex                             // 保护 panicHandler
}

// instanceMetrics 实例级指标。
type instanceMetrics struct {
	running      atomic.Int32 // 当前运行中任务数
	successCount atomic.Int64 // 累计成功任务数
	panicCount   atomic.Int64 // 累计 panic 次数
	submitCount  atomic.Int64 // 累计提交任务数
}

// ============================================================================
// Pool 接口实现
// ============================================================================

// Name 返回池名称。
func (p *poolInstance) Name() string {
	return p.name
}

// SetCap 设置池容量（动态调整）。
// 注意：仅调整容量上限，不会缩减已运行的 worker。
func (p *poolInstance) SetCap(capacity int32) {
	if p.pool == nil {
		return
	}
	p.pool.Tune(int(capacity))
	p.capacity = capacity
}

// Go 提交任务到池中执行（无 Context）。
func (p *poolInstance) Go(f func()) {
	p.CtxGo(context.Background(), f)
}

// CtxGo 提交带 Context 的任务到池中执行。
// 如果池已满，自动降级为原生 goroutine。
func (p *poolInstance) CtxGo(ctx context.Context, f func()) {
	if f == nil {
		return
	}

	// 检查 Context 是否已取消
	select {
	case <-ctx.Done():
		return
	default:
	}

	p.metrics.submitCount.Add(1)

	if p.pool == nil {
		// 池已释放，降级为原生 goroutine
		p.handleSubmitFallback(ctx, f)
		return
	}

	if err := p.pool.Submit(func() {
		p.executeTask(ctx, f)
	}); err != nil {
		// 池满或已关闭，降级为原生 goroutine
		p.handleSubmitFallback(ctx, f)
	}
}

// SetPanicHandler 设置自定义 panic 处理器。
func (p *poolInstance) SetPanicHandler(f func(ctx context.Context, r interface{})) {
	p.mu.Lock()
	p.panicHandler = f
	p.mu.Unlock()
}

// ============================================================================
// 扩展接口实现
// ============================================================================

// Submit 提交任务到池中（兼容旧接口）。
func (p *poolInstance) Submit(task func()) error {
	if task == nil {
		return fmt.Errorf("gogroutine: nil task")
	}
	p.metrics.submitCount.Add(1)
	if p.pool == nil {
		return ants.ErrPoolClosed
	}
	return p.pool.Submit(func() {
		p.executeTask(context.Background(), task)
	})
}

// GetRunningNum 返回当前运行中的任务数。
func (p *poolInstance) GetRunningNum() int {
	return int(p.metrics.running.Load())
}

// GetWaitingNum 返回等待队列中的任务数。
func (p *poolInstance) GetWaitingNum() int {
	if p.pool == nil {
		return 0
	}
	return p.pool.Waiting()
}

// GetCap 返回池容量。
func (p *poolInstance) GetCap() int {
	return int(p.capacity)
}

// Release 释放池资源。
func (p *poolInstance) Release() {
	if p.pool != nil {
		p.pool.Release()
		p.pool = nil
	}
}

// IsFull 检查池是否已满。
func (p *poolInstance) IsFull() bool {
	if p.pool == nil {
		return false
	}
	return p.pool.Running() >= p.pool.Cap()
}

// Stats 返回实例统计信息。
func (p *poolInstance) Stats() PoolStatsInfo {
	return PoolStatsInfo{
		Name:    p.name,
		Running: p.GetRunningNum(),
		Waiting: p.GetWaitingNum(),
		Cap:     p.GetCap(),
		Submit:  p.metrics.submitCount.Load(),
		Success: p.metrics.successCount.Load(),
		Panic:   p.metrics.panicCount.Load(),
		Created: p.createdAt,
	}
}

// ============================================================================
// 内部方法
// ============================================================================

// executeTask 执行任务，包含 panic 恢复和指标采集。
func (p *poolInstance) executeTask(ctx context.Context, task func()) {
	p.metrics.running.Add(1)
	start := time.Now()

	defer func() {
		p.metrics.running.Add(-1)
		duration := time.Since(start)

		if r := recover(); r != nil {
			p.metrics.panicCount.Add(1)
			p.handlePanic(ctx, r, duration)
		} else {
			p.metrics.successCount.Add(1)
			_ = duration // 可扩展：记录任务耗时到指标
		}
	}()

	task()
}

// handlePanic 处理 panic。
func (p *poolInstance) handlePanic(ctx context.Context, r interface{}, duration time.Duration) {
	p.mu.RLock()
	handler := p.panicHandler
	p.mu.RUnlock()

	if handler != nil {
		handler(ctx, r)
		return
	}

	// 默认处理：记录日志
	logger.WarnWithCtx(ctx, "gogroutine: task panic recovered",
		logger.String("pool", p.name),
		logger.Any("panic", r),
		logger.String("duration", duration.String()),
		logger.String("stack", string(getStack())),
	)
}

// handleSubmitFallback 池满时降级处理。
func (p *poolInstance) handleSubmitFallback(ctx context.Context, task func()) {
	logger.WarnWithCtx(ctx, "gogroutine: pool full, fallback to raw goroutine",
		logger.String("pool", p.name),
		logger.Int("running", p.GetRunningNum()),
		logger.Int("cap", p.GetCap()),
	)

	go func() {
		p.executeTask(ctx, task)
	}()
}

// getStack 获取当前 goroutine 的栈信息。
func getStack() []byte {
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	return buf[:n]
}

// ============================================================================
// 池实例创建
// ============================================================================

// newInstance 创建新的池实例。
func newInstance(name string, capacity int, opts ...Option) (*poolInstance, error) {
	if name == "" {
		return nil, fmt.Errorf("gogroutine: pool name cannot be empty")
	}

	cfg := defaultPoolConfig()
	cfg.apply(opts...)

	// 应用容量限制
	capacity = clampPoolSize(capacity)
	if capacity < MinPoolSize {
		capacity = MinPoolSize
	}

	// 构建 ants 选项
	antsOpts := []ants.Option{
		ants.WithNonblocking(cfg.NonBlocking),
	}
	if cfg.PreAlloc {
		antsOpts = append(antsOpts, ants.WithPreAlloc(true))
	}
	if cfg.DisablePurge {
		antsOpts = append(antsOpts, ants.WithDisablePurge(true))
	}

	// 创建 ants 池
	pool, err := ants.NewPool(capacity, antsOpts...)
	if err != nil {
		return nil, fmt.Errorf("gogroutine: create pool %s: %w", name, err)
	}

	p := &poolInstance{
		name:      name,
		pool:      pool,
		capacity:  int32(capacity),
		createdAt: time.Now(),
		metrics:   &instanceMetrics{},
	}

	return p, nil
}
