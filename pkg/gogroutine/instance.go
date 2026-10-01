package gogroutine

import (
	"context"
	"fmt"
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
	pool         *ants.Pool                               // 底层 ants 协程池（受 mu 保护，Release 时置 nil）
	panicHandler func(ctx context.Context, r interface{}) // 自定义 panic 处理器（受 mu 保护）
	metrics      *instanceMetrics                         // 实例级指标
	hooks        *taskHooks                               // 任务管线钩子（newInstance 构造一次，热路径零分配）
	capacity     int32                                    // 池容量（受 mu 保护）
	createdAt    time.Time                                // 创建时间
	mu           sync.RWMutex                             // 保护 pool/capacity/panicHandler
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
// 注意：仅调整容量上限，不会缩减已运行的 worker；池已释放时仅更新容量记录。
// 并发安全：与 Release/CtxGo 互斥（原实现无锁写 p.capacity，与并发读构成数据竞争）。
func (p *poolInstance) SetCap(capacity int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pool != nil {
		p.pool.Tune(int(capacity))
	}
	p.capacity = capacity
}

// currentPool 返回当前底层池快照（已释放时为 nil）。
// 所有对 p.pool 的读取都经由它，避免与 Release 的置 nil 写操作构成数据竞争。
func (p *poolInstance) currentPool() *ants.Pool {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	return pool
}

// Go 提交任务到池中执行（无 Context）。
func (p *poolInstance) Go(f func()) {
	p.CtxGo(context.Background(), f)
}

// CtxGo 提交带 Context 的任务到池中执行。
// 如果池已满，自动降级为原生 goroutine；ctx 为 nil 时按 context.Background() 处理。
func (p *poolInstance) CtxGo(ctx context.Context, f func()) {
	if f == nil {
		return
	}
	ctx = normalizeCtx(ctx)

	// 检查 Context 是否已取消
	select {
	case <-ctx.Done():
		return
	default:
	}

	p.metrics.submitCount.Add(1)

	pool := p.currentPool()
	if pool == nil {
		// 池已释放，降级为原生 goroutine
		p.handleSubmitFallback(ctx, f)
		return
	}

	if err := pool.Submit(func() {
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

// Submit 提交任务到池中（兼容旧接口，底层提交语义）。
// 与 CtxGo 的设计分工（评审 R2-P1-5）：
//   - Submit 是「裸提交」：不降级、不校验 ctx——池已 Release 时返回 ants.ErrPoolClosed，
//     池满时由 ants 配置决定（阻塞等待或返回错误），后续处理交由调用方；
//   - CtxGo 是「高级提交」：已取消 ctx 直接跳过，池满/已释放自动降级为原生 goroutine。
//
// 需要「永不失败」语义时请使用 CtxGo 而非 Submit。
func (p *poolInstance) Submit(task func()) error {
	if task == nil {
		return errNilTask
	}
	p.metrics.submitCount.Add(1)
	pool := p.currentPool()
	if pool == nil {
		return ants.ErrPoolClosed
	}
	return pool.Submit(func() {
		p.executeTask(context.Background(), task)
	})
}

// rawSubmitter 允许绕过实例级 executeTask 包装的池（仅 *poolInstance 实现）。
// 全局路径（submitTask）的闭包自带 executeTask，若再经 Submit 包一层，
// 同一任务会被双重埋点（双 Span、双计数）——评审 R2-P1-3 统一管线的前置改造。
type rawSubmitter interface {
	submitRaw(task func()) error
}

// submitRaw 直接提交闭包到 ants 池，不附加实例级 executeTask 包装。
// 计数语义与 Submit 一致（submitCount 先行 +1，与受理结果无关）；
// 池已释放/提交被拒时返回错误，由调用方走降级路径。
func (p *poolInstance) submitRaw(task func()) error {
	if task == nil {
		return errNilTask
	}
	p.metrics.submitCount.Add(1)
	pool := p.currentPool()
	if pool == nil {
		return ants.ErrPoolClosed
	}
	return pool.Submit(task)
}

// GetRunningNum 返回当前运行中的任务数。
func (p *poolInstance) GetRunningNum() int {
	return int(p.metrics.running.Load())
}

// GetWaitingNum 返回等待队列中的任务数。
func (p *poolInstance) GetWaitingNum() int {
	pool := p.currentPool()
	if pool == nil {
		return 0
	}
	return pool.Waiting()
}

// GetCap 返回池容量。
func (p *poolInstance) GetCap() int {
	p.mu.RLock()
	capacity := p.capacity
	p.mu.RUnlock()
	return int(capacity)
}

// Release 释放池资源（幂等）。
// 并发安全：置 nil 在写锁内完成，与 CtxGo/Submit 的读快照互斥。
func (p *poolInstance) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pool != nil {
		p.pool.Release()
		p.pool = nil
	}
}

// IsFull 检查池是否已满。
func (p *poolInstance) IsFull() bool {
	pool := p.currentPool()
	if pool == nil {
		return false
	}
	return pool.Running() >= pool.Cap()
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

// executeTask 执行任务：复用与全局池相同的 observeTask 管线
// （Span、SetMetrics 接口、耗时观测、panic 兜底），实例差异
// （原子计数、SetPanicHandler）经 p.hooks 注入，消除原双实现行为漂移
// （评审 R2-P1-3）。
func (p *poolInstance) executeTask(ctx context.Context, task func()) {
	observeTask(ctx, p.name, task, p.hooks)
}

// handleSubmitFallback 池满时降级处理。
func (p *poolInstance) handleSubmitFallback(ctx context.Context, task func()) {
	// 降级同样触达 SetMetrics 注入的采集器（与全局池口径一致，评审 R2-P1-3）
	if m := getMetrics(); m != nil {
		m.IncFallback(p.name)
	}
	logger.WarnWithCtx(ctx, "gogroutine: pool full, fallback to raw goroutine",
		logger.String("pool", p.name),
		logger.Int("running", p.GetRunningNum()),
		logger.Int("cap", p.GetCap()),
	)

	go func() {
		p.executeTask(ctx, task)
	}()
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

	if cfg.poolSizeSet {
		// WithPoolSize 对 New* 不生效：容量以 capacity 参数为准（评审 R2-P1-2）。
		// 静默忽略会让用户误以为容量已按 Option 调整，这里显式 WARN 告知。
		logger.WarnWithCtx(context.Background(),
			"gogroutine: WithPoolSize not applicable to New*, capacity parameter wins",
			logger.String("pool", name),
			logger.Int("capacity", capacity),
			logger.Int("ignored_pool_size", cfg.PoolSize),
		)
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
	// 实例管线钩子：计数器恒为 p.metrics（同一份，不存在重绑定配对问题）；
	// Span/Metrics 接口/耗时观测由 observeTask 统一处理（评审 R2-P1-3）
	p.hooks = &taskHooks{
		taskCounter: func() *instanceMetrics { return p.metrics },
		onSuccess:   func() { p.metrics.successCount.Add(1) },
		onPanic:     func() { p.metrics.panicCount.Add(1) },
		panicHandler: func(ctx context.Context, r interface{}, d time.Duration) bool {
			p.mu.RLock()
			handler := p.panicHandler
			p.mu.RUnlock()
			if handler == nil {
				return false
			}
			handler(ctx, r)
			return true
		},
	}

	return p, nil
}
