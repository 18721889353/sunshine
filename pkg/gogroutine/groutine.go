// Package gogroutine 提供生产级 goroutine 管理工具。
//
// 设计理念：
//   - 安全性：自动 panic 恢复，不会导致程序崩溃
//   - 可控性：并发控制，防止 goroutine 泄漏；context 校验避免无效入队
//   - 可观测：内置指标监控，支持 Prometheus；统一 panic 恢复策略
//   - 可追踪：任务命名，链路追踪；context 贯穿任务生命周期
//   - 高性能：基于 ants 协程池，经过生产验证；预分配内存减少 GC 压力
//
// 使用示例：
//
//	// 基础用法
//	gogroutine.Go(ctx, func() {
//	    // 异步任务
//	})
//
//	// 带名称（便于日志追踪和监控）
//	gogroutine.GoWithName(ctx, "sendMQ", func() {
//	    // 异步任务
//	})
//
//	// 带超时控制
//	gogroutine.GoWithTimeout(ctx, "download", 30*time.Second, func(ctx context.Context) {
//	    // 支持 ctx.Done() 检查的任务
//	})
//
//	// 批量任务并收集结果
//	results, err := gogroutine.GoBatchWithResult(ctx, "fetchData", tasks)
//
//	// 监控
//	s := gogroutine.PoolStats()
//	fmt.Printf("running: %d, waiting: %d\n", s.Running, s.Waiting)
//
//	// 优雅关闭
//	gogroutine.ReleaseAndWait()
package gogroutine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ============================================================================
// 初始化
// ============================================================================

// Init 初始化全局协程池（可选，不调用则使用默认配置）。
// 使用 sync.Once 确保并发安全，多次调用只有首次生效。
func Init(opts ...Option) {
	defaultPoolOnce.Do(func() {
		cfg := defaultPoolConfig()
		cfg.apply(opts...)
		cfg.normalize()
		_ = getOrCreatePool(cfg)
	})
}

// ============================================================================
// 任务提交 API
// ============================================================================

// Go 提交一个任务到全局协程池（fire-and-forget）。
func Go(ctx context.Context, task func()) {
	GoWithName(ctx, "", task)
}

// GoWithName 提交一个带名称的任务（便于日志追踪和监控）。
// 如果 ctx 已取消，任务将被跳过而非入队浪费资源。
// 如果池已满，自动降级为原生 goroutine 执行。
func GoWithName(ctx context.Context, name string, task func()) {
	if task == nil || shouldSkipSubmit(ctx, name, task) {
		return
	}

	p := initAndGetPool()

	if err := p.Submit(func() {
		executeTask(ctx, name, task)
	}); err != nil {
		handleSubmitFallback(ctx, name, task)
	}
}

// GoWithTimeout 提交一个带超时控制的任务。
// 任务接收的 ctx 会在超时后自动取消。
func GoWithTimeout(ctx context.Context, name string, timeout time.Duration, task func(ctx context.Context)) {
	GoWithName(ctx, name, func() {
		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		task(timeoutCtx)
	})
}

// GoWithDeadline 提交一个带截止时间的任务。
// 任务接收的 ctx 会在到达 deadline 后自动取消。
func GoWithDeadline(ctx context.Context, name string, deadline time.Time, task func(ctx context.Context)) {
	GoWithName(ctx, name, func() {
		deadlineCtx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		task(deadlineCtx)
	})
}

// GoWithCancel 提交一个支持取消的任务，返回取消函数。
// 调用返回的 cancel 函数可以提前终止任务的 ctx。
func GoWithCancel(ctx context.Context, name string, task func(ctx context.Context)) context.CancelFunc {
	taskCtx, cancel := context.WithCancel(ctx)
	GoWithName(ctx, name, func() {
		task(taskCtx)
	})
	return cancel
}

// ============================================================================
// 批量任务辅助函数
// ============================================================================

// GoBatch 批量提交任务并等待全部完成。
// 所有任务并发执行，单个任务 panic 不影响其他任务。
func GoBatch(ctx context.Context, tasks []func()) {
	if len(tasks) == 0 {
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i := range tasks {
		task := tasks[i] // 显式捕获循环变量（兼容 Go <1.22）
		GoWithName(ctx, fmt.Sprintf("batch_%d", i), func() {
			defer wg.Done()
			task()
		})
	}
	wg.Wait()
}

// GoBatchWithName 批量提交带统一名称前缀的任务并等待全部完成。
// 任务名称格式：{name}_{index}，便于监控系统按前缀聚合。
func GoBatchWithName(ctx context.Context, name string, tasks []func()) {
	if len(tasks) == 0 {
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i := range tasks {
		task := tasks[i] // 显式捕获循环变量（兼容 Go <1.22）
		taskName := fmt.Sprintf("%s_%d", name, i)
		GoWithName(ctx, taskName, func() {
			defer wg.Done()
			task()
		})
	}
	wg.Wait()
}

// GoBatchWithResult 批量提交任务并收集结果（泛型版本）。
// 所有任务并发执行，返回按索引对应的结果切片。
// 如果有任何任务失败，返回聚合后的第一个错误。
func GoBatchWithResult[T any](ctx context.Context, name string, tasks []func() (T, error)) ([]T, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	results := make([]T, len(tasks))
	errs := make([]error, len(tasks))

	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i := range tasks {
		task := tasks[i] // 显式捕获循环变量（兼容 Go <1.22）
		idx := i
		taskName := fmt.Sprintf("%s_%d", name, i)
		GoWithName(ctx, taskName, func() {
			defer wg.Done()
			results[idx], errs[idx] = task()
		})
	}
	wg.Wait()

	return results, collectBatchErrors(errs)
}

// ============================================================================
// 降级处理和错误聚合
// ============================================================================

// handleSubmitFallback 处理池满时的降级逻辑。
// 记录降级指标和日志后，降级为原生 goroutine 执行任务。
func handleSubmitFallback(ctx context.Context, name string, task func()) {
	fallbackCount.Add(1)
	m := getMetrics()
	if m != nil {
		m.IncFallback(name)
	}

	p := initAndGetPool()
	logger.WarnWithCtx(ctx, "gogroutine: pool full, fallback to raw goroutine",
		logger.String("name", name),
		logger.Int("running", p.Running()),
		logger.Int("waiting", p.Waiting()),
	)

	go func() {
		executeTask(ctx, name, task)
	}()
}

// collectBatchErrors 从错误切片中聚合错误。
// 返回第一个非 nil 错误作为代表，附带总数信息。
func collectBatchErrors(errs []error) error {
	var (
		firstErr   error
		errorCount int
	)
	for _, err := range errs {
		if err != nil {
			errorCount++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if errorCount == 0 {
		return nil
	}
	if errorCount == 1 {
		return firstErr
	}
	return fmt.Errorf("batch: %d/%d tasks failed, first: %w", errorCount, len(errs), firstErr)
}

// ============================================================================
// 生命周期管理
// ============================================================================

// StatsInfo 协程池统计信息。
type StatsInfo struct {
	Running  int   // 当前运行中的任务数
	Waiting  int   // 等待队列中的任务数
	Cap      int   // 协程池容量
	Success  int64 // 累计成功任务数
	Panic    int64 // 累计 panic 次数
	Fallback int64 // 累计降级次数
}

// PoolStats 返回协程池统计信息。
func PoolStats() StatsInfo {
	p := initAndGetPool()
	return StatsInfo{
		Running:  p.Running(),
		Waiting:  p.Waiting(),
		Cap:      p.Cap(),
		Success:  successCount.Load(),
		Panic:    panicCount.Load(),
		Fallback: fallbackCount.Load(),
	}
}

// Running 返回当前运行中的任务数量。
func Running() int {
	return initAndGetPool().Running()
}

// Waiting 返回等待中的任务数量。
func Waiting() int {
	return initAndGetPool().Waiting()
}

// Cap 返回协程池容量。
func Cap() int {
	return initAndGetPool().Cap()
}

// IsFull 检查协程池是否已满。
func IsFull() bool {
	return initAndGetPool().IsFull()
}

// Release 释放协程池资源（非阻塞）。
func Release() {
	if defaultPool != nil {
		defaultPool.Release()
	}
}

// ReleaseAndWait 释放协程池并等待所有任务完成。
// 设置最大等待时间，避免无限阻塞。
func ReleaseAndWait() {
	ReleaseAndWaitWithTimeout(30 * time.Second)
}

// ReleaseAndWaitWithTimeout 释放协程池并在指定超时内等待所有任务完成。
func ReleaseAndWaitWithTimeout(timeout time.Duration) {
	if defaultPool == nil {
		return
	}
	defaultPool.Release()

	deadline := time.Now().Add(timeout)
	for defaultPool.Running() > 0 {
		if time.Now().After(deadline) {
			logger.WarnWithCtx(context.Background(), "gogroutine: ReleaseAndWait timeout",
				logger.Int("remaining", defaultPool.Running()),
				logger.String("timeout", timeout.String()),
			)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ============================================================================
// 内部辅助
// ============================================================================

// initAndGetPool 确保全局池已初始化并返回。
// 首次调用时使用默认配置创建池。
func initAndGetPool() Pool {
	p := defaultPool
	if p != nil {
		return p
	}
	// 二次检查，防止并发重复创建
	defaultPoolOnce.Do(func() {
		_ = getOrCreatePool(defaultPoolConfig())
	})
	return defaultPool
}

// ============================================================================
// 兼容性导出 - Config 和 DefaultConfig
// ============================================================================

// Config 协程池配置（导出给外部使用，兼容旧 API）。
type Config struct {
	PoolSize      int           // 协程池大小
	NonBlocking   bool          // 是否非阻塞模式
	PreAlloc      bool          // 是否预分配内存
	DisablePurge  bool          // 是否禁用自动清理
	PurgeInterval time.Duration // 清理间隔
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	cfg := defaultPoolConfig()
	return Config{
		PoolSize:      cfg.PoolSize,
		NonBlocking:   cfg.NonBlocking,
		PreAlloc:      cfg.PreAlloc,
		DisablePurge:  cfg.DisablePurge,
		PurgeInterval: cfg.PurgeInterval,
	}
}

// FormatStats 格式化统计信息为可读字符串（调试/日志用途）。
func FormatStats(s StatsInfo) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("pool[running=%d waiting=%d cap=%d]", s.Running, s.Waiting, s.Cap))
	b.WriteString(fmt.Sprintf(" counters[success=%d panic=%d fallback=%d]", s.Success, s.Panic, s.Fallback))
	return b.String()
}

// Stats 返回协程池统计信息（兼容旧 API，等价于 PoolStats）。
func Stats() StatsInfo {
	return PoolStats()
}
