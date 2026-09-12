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
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ============================================================================
// 初始化
// ============================================================================

var (
	// gracefulShutdown 优雅关闭的运行时状态
	gracefulShutdown struct {
		hooks   []func()      // 优雅关闭时执行的钩子函数
		hooksMu sync.Mutex    // 保护 hooks 的互斥锁
		once    sync.Once     // 确保钩子只执行一次
		enabled bool          // 是否启用优雅关闭
		timeout time.Duration // 优雅关闭超时时间
	}
)

// Init 初始化全局协程池（可选，不调用则使用默认配置）。
// 使用 sync.Once 确保并发安全，多次调用只有首次生效。
func Init(opts ...Option) {
	defaultPoolOnce.Do(func() {
		cfg := defaultPoolConfig()
		cfg.apply(opts...)
		if _, err := getOrCreatePool(cfg); err != nil {
			panic(fmt.Sprintf("gogroutine: init pool failed: %v", err))
		}

		// 保存优雅关闭配置
		gracefulShutdown.enabled = cfg.GracefulShutdown
		// 保存优雅关闭超时时间
		gracefulShutdown.timeout = cfg.GracefulShutdownTimeout

		// 启用优雅关闭时，启动信号监听
		if cfg.GracefulShutdown {
			startSignalWatcher(cfg.GracefulShutdownTimeout)
		}
	})
}

// startSignalWatcher 启动信号监听，收到退出信号时自动释放协程池。
//
// 触发条件：
//   - SIGINT  (Ctrl+C / kill -2)
//   - SIGTERM (kill -15 / K8s 终止 Pod)
//
// 执行流程：收到信号 → 执行 shutdown hooks → ReleaseAndWaitWithTimeout → 日志记录
// 注意：仅在 Init() 中启用 GracefulShutdown 时启动，监听 goroutine 常驻直到收到信号。
func startSignalWatcher(timeout time.Duration) {
	// 创建一个信号通道，监听 SIGINT 和 SIGTERM 信号
	quit := make(chan os.Signal, 1)
	// 注册信号监听
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 启动一个 goroutine 监听信号
	go func() {
		// 阻塞等待信号
		<-quit
		logger.InfoWithCtx(context.Background(), "gogroutine: received shutdown signal, cleaning up...")
		// 执行 shutdown hooks
		executeGracefulShutdownHooks()
		// 释放协程池并等待完成
		ReleaseAndWaitWithTimeout(timeout)
		logger.InfoWithCtx(context.Background(), "gogroutine: cleanup completed")
	}()
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
	if task == nil || shouldSkipSubmit(ctx, name) {
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

// ============================================================================
// 批量任务辅助函数
// ============================================================================

// GoBatch 批量提交任务并等待全部完成。
// 所有任务并发执行，单个任务 panic 不影响其他任务。
func GoBatch(ctx context.Context, tasks []func()) {
	GoBatchWithName(ctx, "batch", tasks)
}

// GoBatchWithName 批量提交带统一名称前缀的任务并等待全部完成。
// 任务名称格式：{name}_{index}，便于监控系统按前缀聚合。
// 注意：ctx 已取消时，任务会被跳过，但函数会立即返回。
func GoBatchWithName(ctx context.Context, name string, tasks []func()) {
	if len(tasks) == 0 {
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i := range tasks {
		taskName := fmt.Sprintf("%s_%d", name, i)
		GoWithName(ctx, taskName, func() {
			defer wg.Done()
			tasks[i]()
		})
	}
	wg.Wait()
}

// GoBatchWithResult 批量提交任务并收集结果（泛型版本）。
// 所有任务并发执行，返回按索引对应的结果切片。
// 如果有任何任务失败，返回所有错误的聚合结果。
func GoBatchWithResult[T any](ctx context.Context, name string, tasks []func() (T, error)) ([]T, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	results := make([]T, len(tasks))
	errs := make([]error, len(tasks))

	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i := range tasks {
		GoWithName(ctx, fmt.Sprintf("%s_%d", name, i), func() {
			defer wg.Done()
			results[i], errs[i] = tasks[i]()
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
	// 记录降级次数
	metricsMgr.fallbackCount.Add(1)
	m := getMetrics()
	if m != nil {
		m.IncFallback(name)
	}

	p := initAndGetPool()
	logger.WarnWithCtx(ctx, "gogroutine: pool full, fallback to raw goroutine",
		logger.String("name", name),
		logger.Int("running", p.GetRunningNum()),
		logger.Int("waiting", p.GetWaitingNum()),
	)

	go func() {
		executeTask(ctx, name, task)
	}()
}

// collectBatchErrors 从错误切片中聚合所有错误。
// 返回所有非 nil 错误的聚合结果。
func collectBatchErrors(errs []error) error {
	var collected []error
	for _, err := range errs {
		if err != nil {
			collected = append(collected, err)
		}
	}
	return errors.Join(collected...)
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
		Running:  p.GetRunningNum(),
		Waiting:  p.GetWaitingNum(),
		Cap:      p.GetCap(),
		Success:  metricsMgr.successCount.Load(),
		Panic:    metricsMgr.panicCount.Load(),
		Fallback: metricsMgr.fallbackCount.Load(),
	}
}

// Release 释放协程池资源（非阻塞）。
// 注意：不会执行 shutdown hooks，不会等待运行中的任务完成。
// 如需等待任务完成，请使用 ReleaseAndWait 或 ReleaseAndWaitWithTimeout。
func Release() {
	if defaultPool != nil {
		defaultPool.Release()
	}
}

// ReleaseAndWait 释放协程池并等待所有任务完成（默认超时 30 秒）。
// 等价于 ReleaseAndWaitWithTimeout(30 * time.Second)。
func ReleaseAndWait() {
	ReleaseAndWaitWithTimeout(30 * time.Second)
}

// ReleaseAndWaitWithTimeout 释放协程池并在指定超时内等待所有任务完成。
//
// 触发条件：
//   - 手动调用（程序主动退出时）
//   - 信号监听自动调用（SIGINT/SIGTERM）
//
// 执行流程：Release 停止接收新任务 → 轮询等待运行中任务完成 → 超时则强制返回。
func ReleaseAndWaitWithTimeout(timeout time.Duration) {
	if defaultPool == nil {
		return
	}
	defaultPool.Release()

	deadline := time.Now().Add(timeout)
	for defaultPool.GetRunningNum() > 0 {
		if time.Now().After(deadline) {
			logger.WarnWithCtx(context.Background(), "gogroutine: ReleaseAndWait timeout",
				logger.Int("remaining", defaultPool.GetRunningNum()),
				logger.String("timeout", timeout.String()),
			)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ============================================================================
// 退出回调管理
// ============================================================================

// RegisterGracefulShutdownHook 注册退出时执行的回调函数。
// 回调按注册顺序执行，可多次注册。
//
// 触发时机（以下两种方式都会执行已注册的 hooks）：
//   - 信号触发：收到 SIGINT/SIGTERM 时，由 startSignalWatcher 自动调用
//   - 手动触发：在程序退出前手动调用 executeGracefulShutdownHooks()
//
// 注意：hooks 通过 sync.Once 保证只执行一次，重复调用不会重复执行。
func RegisterGracefulShutdownHook(hook func()) {
	gracefulShutdown.hooksMu.Lock()
	defer gracefulShutdown.hooksMu.Unlock()
	gracefulShutdown.hooks = append(gracefulShutdown.hooks, hook)
}

// IsGracefulShutdownEnabled 返回是否启用了优雅关闭。
func IsGracefulShutdownEnabled() bool {
	return gracefulShutdown.enabled
}

// executeGracefulShutdownHooks 执行所有注册的退出回调。
// 使用 sync.Once 保证只执行一次。
func executeGracefulShutdownHooks() {
	gracefulShutdown.once.Do(func() {
		gracefulShutdown.hooksMu.Lock()
		defer gracefulShutdown.hooksMu.Unlock()

		for i, hook := range gracefulShutdown.hooks {
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.WarnWithCtx(context.Background(),
							"gogroutine: shutdown hook panicked",
							logger.Int("hook_index", i),
							logger.Any("panic", r),
						)
					}
				}()
				hook()
			}()
		}
	})
}

// ============================================================================
// 内部辅助
// ============================================================================

// initAndGetPool 确保全局池已初始化并返回。
// 首次调用时使用默认配置创建池。
func initAndGetPool() Pool {
	defaultPoolOnce.Do(func() {
		if _, err := getOrCreatePool(defaultPoolConfig()); err != nil {
			panic(fmt.Sprintf("gogroutine: init pool failed: %v", err))
		}
	})
	return defaultPool
}
