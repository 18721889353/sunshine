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

// releasePollInterval ReleaseAndWaitWithTimeout 轮询运行中任务数的间隔。
// 为何轮询而非事件：ants 未暴露「全部任务完成」信号，只能读 Running 计数；
// 取值依据：10ms 相对默认 30s 关闭超时足够细，轮询本身开销可忽略；
// 为何不 flaky：结果由 deadline 兜底判定（超时即返回并告警），间隔只影响收尾延迟。
const releasePollInterval = 10 * time.Millisecond

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
//
// 重要：必须先于任何 Go/GoWithName/GoBatch* 等会触发池创建的调用。
// 若池已由惰性路径（如某包 init() 里先调了 Go）以默认配置创建，
// 或已有另一次 Init 先行生效，本次 Option 会被忽略并打 WARN
// （评审 R2-P1-1）——避免用户以为 WithPoolSize(5000) 生效而实际仍为 1000。
func Init(opts ...Option) {
	// firstInit 仅在本次调用真正执行了初始化闭包时为 true。
	// sync.Once 保证闭包对首个进入者执行一次；后续调用（含并发调用）
	// 会阻塞至首个完成然后跳过闭包，此时 firstInit 保持 false → 触发 WARN。
	firstInit := false
	globalPoolOnce.Do(func() {
		firstInit = true
		cfg := defaultPoolConfig()
		cfg.apply(opts...)
		if _, err := getOrCreateGlobalPool(cfg); err != nil {
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
	if !firstInit {
		// 池已初始化（可能来自惰性 Go* 路径或先前的 Init），本次 Option 被忽略——
		// 不打这条 WARN，用户无从得知真实池大小与预期不符（评审 R2-P1-1）。
		logger.WarnWithCtx(context.Background(),
			"gogroutine: pool already initialized, Init options ignored",
			logger.Int("ignored_options", len(opts)),
		)
	}
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
// ctx 为 nil 时按 context.Background() 处理（不跳过任务）。
func Go(ctx context.Context, task func()) {
	GoWithName(ctx, "", task)
}

// GoWithName 提交一个带名称的任务（便于日志追踪和监控）。
// 如果 ctx 已取消，任务将被跳过而非入队浪费资源；ctx 为 nil 时按 context.Background() 处理。
// 如果池已满，自动降级为原生 goroutine 执行。
func GoWithName(ctx context.Context, name string, task func()) {
	submitTask(ctx, name, task)
}

// submitTask 提交任务并返回是否受理。
// 返回 false 表示任务被跳过（task 为 nil 或 ctx 已取消），此时闭包从未执行——
// 批量提交场景必须据此自行归还 WaitGroup 计数，否则被跳过的任务会让 wg.Wait 永久阻塞
// （历史缺陷：GoBatch* 传入已取消的 ctx 会死锁，守护用例 TestGoBatchWithResultCancelledCtx）。
func submitTask(ctx context.Context, name string, task func()) bool {
	if task == nil {
		return false
	}
	ctx = normalizeCtx(ctx)
	if shouldSkipSubmit(ctx, name) {
		return false
	}

	p := initAndGetPool()
	wrapper := func() {
		executeTask(ctx, name, task)
	}
	var err error
	if rs, ok := p.(rawSubmitter); ok {
		// 全局池走 submitRaw：闭包自带 executeTask，绕过 Submit 的实例级包装，
		// 避免同一任务被双重埋点（双 Span、双计数；评审 R2-P1-3 前置改造）
		err = rs.submitRaw(wrapper)
	} else {
		// mock 或其他 Pool 实现（如测试注入的 rejectPool）保持原 Submit 路径
		err = p.Submit(wrapper)
	}
	if err != nil {
		handleSubmitFallback(ctx, name, task)
	}
	return true
}

// GoWithTimeout 提交一个带超时控制的任务。
// 任务接收的 ctx 会在超时后自动取消；ctx 为 nil 时按 context.Background() 处理。
func GoWithTimeout(ctx context.Context, name string, timeout time.Duration, task func(ctx context.Context)) {
	ctx = normalizeCtx(ctx)
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
// 所有任务并发执行，单个任务 panic 不影响其他任务；nil 任务条目会被跳过（不执行、不报错）。
// ctx 已取消时任务全部被跳过，函数立即返回（不会阻塞）。
func GoBatch(ctx context.Context, tasks []func()) {
	GoBatchWithName(ctx, "batch", tasks)
}

// GoBatchWithName 批量提交带统一名称前缀的任务并等待全部完成。
// 任务名称格式：{name}_{index}，便于监控系统按前缀聚合。
// 注意：ctx 已取消时任务被跳过，但函数仍立即返回——被跳过的任务会当场归还 WaitGroup 计数。
// nil 任务条目同样当场跳过（不提交、不产生 panic 噪声），与 GoBatchWithResult 的 errNilTask 口径一致。
func GoBatchWithName(ctx context.Context, name string, tasks []func()) {
	if len(tasks) == 0 {
		return
	}
	ctx = normalizeCtx(ctx)

	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i := range tasks {
		if tasks[i] == nil {
			// nil 条目无法执行：不提交，当场归还计数。
			// 否则包装闭包非 nil 会被受理，执行时 panic 虽被 recover 但徒增 WARN 噪声。
			wg.Done()
			continue
		}
		taskName := fmt.Sprintf("%s_%d", name, i)
		if !submitTask(ctx, taskName, func() {
			defer wg.Done()
			tasks[i]()
		}) {
			wg.Done() // 任务被跳过，闭包未执行：当场归还计数，避免 wg.Wait 永久阻塞
		}
	}
	wg.Wait()
}

// GoBatchWithResult 批量提交任务并收集结果（泛型版本）。
// 所有任务并发执行，返回按索引对应的结果切片。
// 如果有任何任务失败，返回所有错误的聚合结果；
// ctx 已取消时被跳过的任务记 errs[i] = ctx.Err()，结果位为零值，函数不会阻塞。
// nil 任务条目记 errs[i] = errNilTask（可用 errors.Is 判定），结果位为零值——
// 不会静默返回 err==nil（评审 R2-P1-4）。
func GoBatchWithResult[T any](ctx context.Context, name string, tasks []func() (T, error)) ([]T, error) {
	if len(tasks) == 0 {
		return nil, nil
	}
	ctx = normalizeCtx(ctx)

	results := make([]T, len(tasks))
	errs := make([]error, len(tasks))

	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i := range tasks {
		if tasks[i] == nil {
			// 必须在 submitTask 之前检查：包装闭包本身非 nil，若照常受理，
			// 执行时 tasks[i]() 会 panic 被 recover，errs[i] 保持 nil →
			// 调用方拿到 err==nil 但结果位零值的静默失败（评审 R2-P1-4）。
			errs[i] = errNilTask
			wg.Done()
			continue
		}
		if !submitTask(ctx, fmt.Sprintf("%s_%d", name, i), func() {
			defer wg.Done()
			results[i], errs[i] = tasks[i]()
		}) {
			// 任务被跳过必因 ctx 已取消（闭包本身非 nil），记录 ctx 错误而非静默留零值
			errs[i] = ctx.Err()
			wg.Done()
		}
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

// errNilTask 批量任务中的 nil 项错误，提交跳过与错误聚合共用同一值，便于调用方 errors.Is 判定。
var errNilTask = errors.New("gogroutine: nil task")

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
	if p := currentGlobalPool(); p != nil {
		p.Release()
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
	p := currentGlobalPool()
	if p == nil {
		return
	}
	p.Release()

	deadline := time.Now().Add(timeout)
	for p.GetRunningNum() > 0 {
		if time.Now().After(deadline) {
			logger.WarnWithCtx(context.Background(), "gogroutine: ReleaseAndWait timeout",
				logger.Int("remaining", p.GetRunningNum()),
				logger.String("timeout", timeout.String()),
			)
			return
		}
		time.Sleep(releasePollInterval)
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
	globalPoolOnce.Do(func() {
		if _, err := getOrCreateGlobalPool(defaultPoolConfig()); err != nil {
			panic(fmt.Sprintf("gogroutine: init pool failed: %v", err))
		}
	})
	return currentGlobalPool()
}
