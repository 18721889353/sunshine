package gogroutine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// 测试辅助
// ============================================================================

// 测试辅助（resetForTest/resetMetrics/mockMetrics/waitDone）统一集中在 test_helpers_test.go

// ============================================================================
// 测试 Init - 全局池初始化
// ============================================================================

func TestInitDefaultConfig(t *testing.T) {
	resetForTest()
	Init()

	if globalPool == nil {
		t.Fatal("globalPool should be initialized after Init()")
	}
	if v := globalPool.GetCap(); v != DefaultPoolSize {
		t.Errorf("GetCap() = %d, want %d", v, DefaultPoolSize)
	}

	globalPool.Release()
	globalPool = nil
}

func TestInitWithOptions(t *testing.T) {
	resetForTest()
	Init(WithPoolSize(50))

	if globalPool == nil {
		t.Fatal("globalPool should be initialized after Init()")
	}
	if v := globalPool.GetCap(); v != 50 {
		t.Errorf("GetCap() = %d, want 50", v)
	}

	globalPool.Release()
	globalPool = nil
}

func TestInitIdempotent(t *testing.T) {
	resetForTest()
	Init(WithPoolSize(50))

	// 第二次 Init 应被忽略
	Init(WithPoolSize(200))

	if v := globalPool.GetCap(); v != 50 {
		t.Errorf("GetCap() = %d, want 50 (second Init should be ignored)", v)
	}

	globalPool.Release()
	globalPool = nil
}

// ============================================================================
// 测试 Go / GoWithName - 基础任务提交
// ============================================================================

func TestGoSuccess(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	Go(context.Background(), func() {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Go task did not complete within timeout")
	}

	if !executed.Load() {
		t.Error("task was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoNilTask(t *testing.T) {
	resetForTest()

	// nil 任务不应 panic
	Go(context.Background(), nil)

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoWithNameSuccess(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	GoWithName(context.Background(), "myTask", func() {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GoWithName task did not complete within timeout")
	}

	if !executed.Load() {
		t.Error("task was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoWithNameCancelledCtx(t *testing.T) {
	resetForTest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	GoWithName(ctx, "cancelled-task", func() {
		t.Error("cancelled task should not be executed")
	})

	// 任务不应被提交到池中
	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 GoWithTimeout - 超时控制
// ============================================================================

func TestGoWithTimeoutSuccess(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	GoWithTimeout(context.Background(), "timeout-task", 1*time.Second, func(ctx context.Context) {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GoWithTimeout task did not complete within timeout")
	}

	if !executed.Load() {
		t.Error("task was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoWithTimeoutContextCancelled(t *testing.T) {
	resetForTest()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	GoWithTimeout(ctx, "timeout-cancel", 10*time.Second, func(ctx context.Context) {
		<-ctx.Done()
		close(done)
	})

	select {
	case <-done:
		// 任务在超时后正确退出
	case <-time.After(5 * time.Second):
		t.Fatal("task did not observe context cancellation")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 GoBatch - 批量任务
// ============================================================================

func TestGoBatchSuccess(t *testing.T) {
	resetForTest()

	var counter atomic.Int64
	tasks := make([]func(), 10)
	for i := range tasks {
		tasks[i] = func() {
			counter.Add(1)
		}
	}

	GoBatch(context.Background(), tasks)

	if v := counter.Load(); v != 10 {
		t.Errorf("counter = %d, want 10", v)
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoBatchEmptyTasks(t *testing.T) {
	resetForTest()

	// 空任务列表不应 panic
	GoBatch(context.Background(), nil)
	GoBatch(context.Background(), []func(){})
}

func TestGoBatchSingleTask(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	GoBatch(context.Background(), []func(){
		func() { executed.Store(true) },
	})

	if !executed.Load() {
		t.Error("single task was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 GoBatchWithName - 带名称前缀的批量任务
// ============================================================================

func TestGoBatchWithNameSuccess(t *testing.T) {
	resetForTest()

	var counter atomic.Int64
	tasks := make([]func(), 5)
	for i := range tasks {
		tasks[i] = func() {
			counter.Add(1)
		}
	}

	GoBatchWithName(context.Background(), "fetch", tasks)

	if v := counter.Load(); v != 5 {
		t.Errorf("counter = %d, want 5", v)
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoBatchWithNameEmptyTasks(t *testing.T) {
	resetForTest()

	// 空任务列表不应 panic
	GoBatchWithName(context.Background(), "empty", nil)
	GoBatchWithName(context.Background(), "empty", []func(){})
}

// ============================================================================
// 测试 GoBatchWithResult - 泛型批量结果收集
// ============================================================================

func TestGoBatchWithResultSuccess(t *testing.T) {
	resetForTest()

	tasks := []func() (string, error){
		func() (string, error) { return "result1", nil },
		func() (string, error) { return "result2", nil },
		func() (string, error) { return "result3", nil },
	}

	results, err := GoBatchWithResult(context.Background(), "fetch", tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}

	expected := []string{"result1", "result2", "result3"}
	for i, r := range results {
		if r != expected[i] {
			t.Errorf("results[%d] = %q, want %q", i, r, expected[i])
		}
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoBatchWithResultWithError(t *testing.T) {
	resetForTest()

	targetErr := errors.New("failed")
	tasks := []func() (string, error){
		func() (string, error) { return "ok", nil },
		func() (string, error) { return "", targetErr },
	}

	results, err := GoBatchWithResult(context.Background(), "fetch-fail", tasks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, targetErr) {
		t.Errorf("expected target error, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoBatchWithResultEmptyTasks(t *testing.T) {
	resetForTest()

	tasks := []func() (int, error){}
	results, err := GoBatchWithResult(context.Background(), "empty", tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("results should be nil for empty tasks, got %v", results)
	}
}

func TestGoBatchWithResultAllErrors(t *testing.T) {
	resetForTest()

	err1 := errors.New("err1")
	err2 := errors.New("err2")
	tasks := []func() (int, error){
		func() (int, error) { return 0, err1 },
		func() (int, error) { return 0, err2 },
	}

	results, err := GoBatchWithResult(context.Background(), "all-fail", tasks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 应包含所有错误
	if !errors.Is(err, err1) || !errors.Is(err, err2) {
		t.Errorf("expected all errors, got %v", err)
	}
	// 结果切片应存在但值为零值
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 collectBatchErrors - 错误聚合
// ============================================================================

func TestCollectBatchErrorsNoErrors(t *testing.T) {
	errs := []error{nil, nil, nil}
	if err := collectBatchErrors(errs); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCollectBatchErrorsSingleError(t *testing.T) {
	target := errors.New("target error")
	errs := []error{nil, target, nil}
	err := collectBatchErrors(errs)
	if !errors.Is(err, target) {
		t.Errorf("expected target error, got %v", err)
	}
}

func TestCollectBatchErrorsMultipleErrors(t *testing.T) {
	err1 := errors.New("err1")
	err2 := errors.New("err2")
	err3 := errors.New("err3")
	errs := []error{err1, err2, err3}
	err := collectBatchErrors(errs)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 应包含所有错误
	if !errors.Is(err, err1) || !errors.Is(err, err2) || !errors.Is(err, err3) {
		t.Errorf("expected all errors, got %v", err)
	}
}

func TestCollectBatchErrorsEmptySlice(t *testing.T) {
	err := collectBatchErrors([]error{})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// ============================================================================
// 测试 handleSubmitFallback - 池满降级
// ============================================================================

// rejectPool 模拟池满时 Submit 返回错误的 Pool 实现。
// 用于测试 handleSubmitFallback 降级路径。
type rejectPool struct{}

func (p *rejectPool) Name() string                                         { return "reject" }
func (p *rejectPool) SetCap(_ int32)                                       {}
func (p *rejectPool) Go(_ func())                                          {}
func (p *rejectPool) CtxGo(_ context.Context, _ func())                    {}
func (p *rejectPool) SetPanicHandler(_ func(context.Context, interface{})) {}
func (p *rejectPool) Submit(_ func()) error                                { return errors.New("pool full") }
func (p *rejectPool) GetRunningNum() int                                   { return 0 }
func (p *rejectPool) GetWaitingNum() int                                   { return 0 }
func (p *rejectPool) GetCap() int                                          { return 10 }
func (p *rejectPool) Release()                                             {}
func (p *rejectPool) IsFull() bool                                         { return true }
func (p *rejectPool) Stats() PoolStatsInfo                                 { return PoolStatsInfo{} }

func TestGoWithNameFallbackWhenPoolFull(t *testing.T) {
	resetForTest()

	// 注入 mock Pool，使 Submit 始终返回错误，触发降级路径
	globalPool = &rejectPool{}

	var fallbackExecuted atomic.Bool
	done := make(chan struct{})

	GoWithName(context.Background(), "fallback-task", func() {
		fallbackExecuted.Store(true)
		close(done)
	})

	// 降级 goroutine 应该执行任务
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fallback task did not complete within timeout")
	}

	if !fallbackExecuted.Load() {
		t.Error("fallback task was not executed")
	}
	if v := metricsMgr.fallbackCount.Load(); v < 1 {
		t.Errorf("fallbackCount = %d, want >= 1", v)
	}

	globalPool = nil
}

func TestGoWithNameFallbackWithMetrics(t *testing.T) {
	resetForTest()

	mm := &mockMetrics{}
	SetMetrics(mm)

	// 注入 mock Pool，使 Submit 始终返回错误，触发降级路径
	globalPool = &rejectPool{}

	// 提交应触发降级
	done := make(chan struct{})
	GoWithName(context.Background(), "fallback-metric", func() {
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fallback task did not complete within timeout")
	}

	if v := mm.fallback.Load(); v < 1 {
		t.Errorf("fallback count = %d, want >= 1", v)
	}

	SetMetrics(nil)
	globalPool = nil
}

// ============================================================================
// 测试 PoolStats
// ============================================================================

func TestPoolStats(t *testing.T) {
	resetForTest()

	s := PoolStats()

	if s.Running < 0 {
		t.Errorf("Running = %d, want >= 0", s.Running)
	}
	if s.Waiting < 0 {
		t.Errorf("Waiting = %d, want >= 0", s.Waiting)
	}
	if s.Cap != DefaultPoolSize {
		t.Errorf("Cap = %d, want %d", s.Cap, DefaultPoolSize)
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 Release / ReleaseAndWait
// ============================================================================

func TestRelease(t *testing.T) {
	resetForTest()

	Go(context.Background(), func() {
		time.Sleep(simulatedTaskWork)
	})

	time.Sleep(taskStartPad)
	Release()

	// Release 后不应 panic
	if globalPool != nil {
		// pool 已释放但引用可能还在（取决于实现）
	}
}

func TestReleaseAndWait(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	Go(context.Background(), func() {
		time.Sleep(simulatedTaskWork)
		executed.Store(true)
	})

	// 先等任务真正跑起来（Running>=1），ReleaseAndWait 才会走到「等待运行中任务」路径；
	// 若任务尚在队列未被调度，Release 后可能被丢弃，断言反而测不到目标语义
	waitUntil(t, func() bool { return PoolStats().Running >= 1 }, "任务未开始执行")
	ReleaseAndWait()

	if !executed.Load() {
		t.Error("task should have completed before ReleaseAndWait returned")
	}

	globalPool = nil
}

func TestReleaseAndWaitWithTimeoutTimeout(t *testing.T) {
	resetForTest()

	blockCh := make(chan struct{})
	Go(context.Background(), func() {
		<-blockCh
	})

	waitUntil(t, func() bool { return PoolStats().Running >= 1 }, "任务未开始执行")

	// 设置极短超时，任务未完成就超时
	ReleaseAndWaitWithTimeout(1 * time.Millisecond)

	close(blockCh)
	globalPool = nil
}

func TestReleaseAndWaitWithTimeoutNilPool(t *testing.T) {
	// globalPool 为 nil 时不应 panic
	ReleaseAndWaitWithTimeout(1 * time.Second)
}

// TestReleaseAndWaitWithTimeoutWaitForCompletion 测试等待任务完成后释放
func TestReleaseAndWaitWithTimeoutWaitForCompletion(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	Go(context.Background(), func() {
		time.Sleep(simulatedTaskWork)
		executed.Store(true)
	})

	// 先等任务真正跑起来（Running>=1），ReleaseAndWaitWithTimeout 才测得到「等待完成」语义
	waitUntil(t, func() bool { return PoolStats().Running >= 1 }, "任务未开始执行")

	// 等待足够时间让任务完成
	ReleaseAndWaitWithTimeout(5 * time.Second)

	if !executed.Load() {
		t.Error("task should have completed before timeout")
	}

	globalPool = nil
}

// TestReleaseAndWaitWithTimeoutTimeoutPath 测试超时路径
func TestReleaseAndWaitWithTimeoutTimeoutPath(t *testing.T) {
	resetForTest()

	blockCh := make(chan struct{})
	Go(context.Background(), func() {
		<-blockCh
	})

	waitUntil(t, func() bool { return PoolStats().Running >= 1 }, "任务未开始执行")

	// 设置极短超时，任务未完成就超时
	ReleaseAndWaitWithTimeout(1 * time.Millisecond)

	close(blockCh)
	globalPool = nil
}

// ============================================================================
// 测试并发安全 - 多 goroutine 同时调用
// ============================================================================

func TestConcurrentGo(t *testing.T) {
	resetForTest()

	var counter atomic.Int64
	var wg sync.WaitGroup

	// 并发提交多个任务
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Go(context.Background(), func() {
				counter.Add(1)
			})
		}()
	}

	wg.Wait() // 只保证提交完成，任务异步执行

	// 轮询等待所有任务执行完成（固定 sleep 后单次读数是 flaky 源，改轮询+deadline）
	waitUntil(t, func() bool { return counter.Load() == n }, "并发任务未全部执行")

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 panic 恢复 - 通过 Go API 提交 panic 任务
// ============================================================================

func TestGoPanicRecovery(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	// 提交一个 panic 任务
	Go(context.Background(), func() {
		panic("test panic through Go API")
	})

	// 提交第二个正常任务
	Go(context.Background(), func() {
		executed.Store(true)
		close(done)
	})

	// 等待两个任务都完成
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("subsequent task after panic did not complete")
	}

	// panic 计数在 recover defer 中落地，与第二个任务无顺序保证，轮询等待
	waitUntil(t, func() bool { return metricsMgr.panicCount.Load() >= 1 }, "panic 未被计数")
	if !executed.Load() {
		t.Error("task after panic was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 ctx 校验 - 已取消的 ctx 跳过提交
// ============================================================================

func TestGoCancelledCtxSkipped(t *testing.T) {
	resetForTest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	var executed atomic.Bool
	Go(ctx, func() {
		executed.Store(true)
	})

	// 负向断言：「不发生」无完成信号可监听，只能给定观察窗口（见 negativeAssertionWindow 注释）
	time.Sleep(negativeAssertionWindow)

	if executed.Load() {
		t.Error("task with cancelled ctx should not be executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试自动释放相关功能
// ============================================================================

func TestRegisterGracefulShutdownHook(t *testing.T) {
	resetForTest()
	gracefulShutdown.once = sync.Once{}
	gracefulShutdown.hooks = nil

	var executed atomic.Bool
	RegisterGracefulShutdownHook(func() {
		executed.Store(true)
	})

	executeGracefulShutdownHooks()

	if !executed.Load() {
		t.Error("shutdown hook was not executed")
	}
}

func TestRegisterGracefulShutdownHookMultiple(t *testing.T) {
	resetForTest()
	gracefulShutdown.once = sync.Once{}
	gracefulShutdown.hooks = nil

	var counter atomic.Int64
	RegisterGracefulShutdownHook(func() {
		counter.Add(1)
	})
	RegisterGracefulShutdownHook(func() {
		counter.Add(10)
	})

	executeGracefulShutdownHooks()

	if v := counter.Load(); v != 11 {
		t.Errorf("counter = %d, want 11", v)
	}
}

func TestRegisterGracefulShutdownHookOnceOnly(t *testing.T) {
	resetForTest()
	gracefulShutdown.once = sync.Once{}
	gracefulShutdown.hooks = nil

	var counter atomic.Int64
	RegisterGracefulShutdownHook(func() {
		counter.Add(1)
	})

	executeGracefulShutdownHooks()
	executeGracefulShutdownHooks() // 第二次调用不应执行

	if v := counter.Load(); v != 1 {
		t.Errorf("counter = %d, want 1 (should only execute once)", v)
	}
}

func TestRegisterGracefulShutdownHookPanicRecovery(t *testing.T) {
	resetForTest()
	gracefulShutdown.once = sync.Once{}
	gracefulShutdown.hooks = nil

	var executed atomic.Bool
	RegisterGracefulShutdownHook(func() {
		panic("test panic in hook")
	})
	RegisterGracefulShutdownHook(func() {
		executed.Store(true)
	})

	// 不应 panic，后续 hook 应继续执行
	executeGracefulShutdownHooks()

	if !executed.Load() {
		t.Error("subsequent hook was not executed after panic")
	}
}

func TestWithGracefulShutdown(t *testing.T) {
	cfg := defaultPoolConfig()
	WithGracefulShutdown(true)(cfg)

	if !cfg.GracefulShutdown {
		t.Error("GracefulShutdown should be true")
	}
}

func TestWithGracefulShutdownTimeout(t *testing.T) {
	cfg := defaultPoolConfig()
	WithGracefulShutdownTimeout(10 * time.Second)(cfg)

	if cfg.GracefulShutdownTimeout != 10*time.Second {
		t.Errorf("GracefulShutdownTimeout = %v, want 10s", cfg.GracefulShutdownTimeout)
	}
}

func TestIsGracefulShutdownEnabled(t *testing.T) {
	resetForTest()

	// 生产环境默认启用优雅关闭
	if !IsGracefulShutdownEnabled() {
		t.Error("IsGracefulShutdownEnabled should be true by default (production config)")
	}
}

// ============================================================================
// 大厂级边界条件测试
// ============================================================================

// TestGoBatchLargeNumberOfTasks 测试大批量任务（1000+）
func TestGoBatchLargeNumberOfTasks(t *testing.T) {
	resetForTest()

	const n = 1000
	var counter atomic.Int64
	tasks := make([]func(), n)
	for i := range tasks {
		tasks[i] = func() {
			counter.Add(1)
		}
	}

	GoBatch(context.Background(), tasks)

	if v := counter.Load(); v != n {
		t.Errorf("counter = %d, want %d", v, n)
	}

	ReleaseAndWaitWithTimeout(10 * time.Second)
}

// TestGoBatchWithResultLargeNumberOfTasks 测试大批量结果收集
func TestGoBatchWithResultLargeNumberOfTasks(t *testing.T) {
	resetForTest()

	const n = 500
	tasks := make([]func() (int, error), n)
	for i := range tasks {
		i := i
		tasks[i] = func() (int, error) {
			return i, nil
		}
	}

	results, err := GoBatchWithResult(context.Background(), "large", tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != n {
		t.Fatalf("len(results) = %d, want %d", len(results), n)
	}

	ReleaseAndWaitWithTimeout(10 * time.Second)
}

// TestGoWithTimeoutVeryShortTimeout 测试极短超时
func TestGoWithTimeoutVeryShortTimeout(t *testing.T) {
	resetForTest()

	done := make(chan struct{})
	GoWithTimeout(context.Background(), "short-timeout", 1*time.Millisecond, func(ctx context.Context) {
		// 任务应该很快超时
		<-ctx.Done()
		close(done)
	})

	select {
	case <-done:
		// 任务正确超时
	case <-time.After(1 * time.Second):
		t.Fatal("task did not timeout as expected")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestGoWithTimeoutNegativeTimeout 测试负数超时
func TestGoWithTimeoutNegativeTimeout(t *testing.T) {
	resetForTest()

	done := make(chan struct{})
	GoWithTimeout(context.Background(), "negative-timeout", -1*time.Second, func(ctx context.Context) {
		// 负数超时应该立即取消
		<-ctx.Done()
		close(done)
	})

	select {
	case <-done:
		// 正确行为
	case <-time.After(1 * time.Second):
		t.Fatal("task did not handle negative timeout")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestReleaseAndWaitWithTimeoutZeroTimeout 测试零超时
func TestReleaseAndWaitWithTimeoutZeroTimeout(t *testing.T) {
	resetForTest()

	blockCh := make(chan struct{})
	Go(context.Background(), func() {
		<-blockCh
	})

	waitUntil(t, func() bool { return PoolStats().Running >= 1 }, "任务未开始执行")

	// 零超时应该立即返回
	ReleaseAndWaitWithTimeout(0)

	close(blockCh)
	globalPool = nil
}

// TestPoolStatsWithRunningTasks 测试运行中任务的统计
func TestPoolStatsWithRunningTasks(t *testing.T) {
	resetForTest()

	startCh := make(chan struct{})
	blockCh := make(chan struct{})

	// 提交一个阻塞任务
	Go(context.Background(), func() {
		close(startCh)
		<-blockCh
	})

	<-startCh // 任务体已执行，ants Running 计数在此之前已 add，直读安全

	s := PoolStats()
	if s.Running < 1 {
		t.Errorf("Running = %d, want >= 1", s.Running)
	}

	close(blockCh)
	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 大厂级并发安全测试
// ============================================================================

// TestConcurrentInit 多 goroutine 同时调用 Init
func TestConcurrentInit(t *testing.T) {
	resetForTest()

	var wg sync.WaitGroup
	done := make(chan struct{})

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-done
			Init(WithPoolSize(100))
		}()
	}

	close(done)
	wg.Wait()

	// 只有第一次 Init 生效
	if v := globalPool.GetCap(); v != 100 {
		t.Errorf("GetCap() = %d, want 100", v)
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestConcurrentRelease 多 goroutine 同时调用 Release
func TestConcurrentRelease(t *testing.T) {
	resetForTest()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Release()
		}()
	}
	wg.Wait()

	// 不应 panic
	globalPool = nil
}

// TestConcurrentReleaseAndWaitWithTimeout 多 goroutine 同时调用 ReleaseAndWaitWithTimeout
func TestConcurrentReleaseAndWaitWithTimeout(t *testing.T) {
	resetForTest()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ReleaseAndWaitWithTimeout(5 * time.Second)
		}()
	}
	wg.Wait()

	globalPool = nil
}

// TestConcurrentPoolStats 多 goroutine 同时访问 PoolStats
func TestConcurrentPoolStats(t *testing.T) {
	resetForTest()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := PoolStats()
			if s.Cap != DefaultPoolSize {
				t.Errorf("GetCap() = %d, want %d", s.Cap, DefaultPoolSize)
			}
		}()
	}
	wg.Wait()

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestConcurrentSubmitAndRelease 并发提交任务和释放池
func TestConcurrentSubmitAndRelease(t *testing.T) {
	resetForTest()

	var counter atomic.Int64
	var wg sync.WaitGroup

	// 并发提交任务
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Go(context.Background(), func() {
				counter.Add(1)
			})
		}()
	}

	// 同时释放
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(taskStartPad)
		Release()
	}()

	wg.Wait()
	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestConcurrentRegisterHooks 并发注册 hooks
func TestConcurrentRegisterHooks(t *testing.T) {
	resetForTest()
	gracefulShutdown.once = sync.Once{}
	gracefulShutdown.hooks = nil

	var counter atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RegisterGracefulShutdownHook(func() {
				counter.Add(1)
			})
		}()
	}
	wg.Wait()

	executeGracefulShutdownHooks()

	if v := counter.Load(); v != 100 {
		t.Errorf("counter = %d, want 100", v)
	}
}

// ============================================================================
// 大厂级异常场景测试
// ============================================================================

// TestGoNestedPanic 嵌套 panic 恢复
func TestGoNestedPanic(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	// 提交一个嵌套 panic 的任务
	Go(context.Background(), func() {
		func() {
			panic("nested panic")
		}()
	})

	// 提交第二个正常任务
	Go(context.Background(), func() {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("subsequent task after nested panic did not complete")
	}

	// panic 计数在 recover defer 中落地，与第二个任务无顺序保证，轮询等待
	waitUntil(t, func() bool { return metricsMgr.panicCount.Load() >= 1 }, "nested panic 未被计数")
	if !executed.Load() {
		t.Error("task after panic was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestGoPanicWithNilRecover panic 后 recover 返回 nil
func TestGoPanicWithNilRecover(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	// 提交一个 panic nil 的任务
	Go(context.Background(), func() {
		panic(nil)
	})

	// 提交第二个正常任务
	Go(context.Background(), func() {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("subsequent task after nil panic did not complete")
	}

	// done 信号已建立 happens-before：executed.Store 在 close(done) 之前，此处直读即可
	// panic(nil) 在 Go 中是特殊情况，recover() 返回 nil，但本包代码必须能处理
	if !executed.Load() {
		t.Error("task after panic was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestGoBatchWithResultMixedSuccessAndError 混合成功和失败
func TestGoBatchWithResultMixedSuccessAndError(t *testing.T) {
	resetForTest()

	err1 := errors.New("error1")
	err2 := errors.New("error2")
	tasks := []func() (int, error){
		func() (int, error) { return 1, nil },
		func() (int, error) { return 0, err1 },
		func() (int, error) { return 3, nil },
		func() (int, error) { return 0, err2 },
		func() (int, error) { return 5, nil },
	}

	results, err := GoBatchWithResult(context.Background(), "mixed", tasks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// 应包含所有错误
	if !errors.Is(err, err1) || !errors.Is(err, err2) {
		t.Errorf("expected all errors, got %v", err)
	}

	// 成功的结果应该保留
	if len(results) != 5 {
		t.Fatalf("len(results) = %d, want 5", len(results))
	}
	if results[0] != 1 || results[2] != 3 || results[4] != 5 {
		t.Errorf("unexpected results: %v", results)
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestGoWithNameEmptyName 空任务名称
func TestGoWithNameEmptyName(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	GoWithName(context.Background(), "", func() {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("task with empty name did not complete")
	}

	if !executed.Load() {
		t.Error("task was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestGoWithNameVeryLongName 极长任务名称
func TestGoWithNameVeryLongName(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	longName := string(make([]byte, 1000))
	GoWithName(context.Background(), longName, func() {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("task with long name did not complete")
	}

	if !executed.Load() {
		t.Error("task was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestWithPoolSizeBoundaryValues 池大小边界值
func TestWithPoolSizeBoundaryValues(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"below_min", 0, MinPoolSize},
		{"at_min", MinPoolSize, MinPoolSize},
		{"above_min", MinPoolSize + 1, MinPoolSize + 1},
		{"below_max", MaxPoolSize - 1, MaxPoolSize - 1},
		{"at_max", MaxPoolSize, MaxPoolSize},
		{"above_max", MaxPoolSize + 1, MaxPoolSize},
		{"negative", -100, MinPoolSize},
		{"very_large", 100000, MaxPoolSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultPoolConfig()
			WithPoolSize(tt.input)(cfg)
			if cfg.PoolSize != tt.expected {
				t.Errorf("PoolSize = %d, want %d", cfg.PoolSize, tt.expected)
			}
		})
	}
}

// ============================================================================
// 大厂级集成测试
// ============================================================================

// TestFullLifecycle 完整生命周期：Init → 提交任务 → 释放
func TestFullLifecycle(t *testing.T) {
	resetForTest()

	// 1. 初始化
	Init(
		WithPoolSize(100),
		WithPreAlloc(true),
		WithDisablePurge(true),
	)

	// 2. 注入监控
	mm := &mockMetrics{}
	SetMetrics(mm)

	// 3. 注册退出回调
	var hookExecuted atomic.Bool
	RegisterGracefulShutdownHook(func() {
		hookExecuted.Store(true)
	})

	// 4. 提交多个任务
	var counter atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		GoWithName(context.Background(), fmt.Sprintf("task_%d", i), func() {
			defer wg.Done()
			counter.Add(1)
			time.Sleep(simulatedTaskWork)
		})
	}

	// 5. 等待任务完成（wg.Wait 只覆盖到闭包 defer，executeTask 的 DecRunning 在其后落地，
	// 因此还需轮询 running 归零才能断言指标）
	wg.Wait()
	waitUntil(t, func() bool { return mm.running.Load() == 0 }, "任务指标未归零")

	// 6. 验证指标
	if v := counter.Load(); v != 10 {
		t.Errorf("counter = %d, want 10", v)
	}
	if v := mm.running.Load(); v != 0 {
		t.Errorf("running = %d, want 0 (all tasks completed)", v)
	}

	// 7. 释放池
	ReleaseAndWaitWithTimeout(5 * time.Second)

	// 8. 验证 hook 执行（如果启用了优雅关闭）
	// 注意：这里不验证 hook，因为信号监听是异步的

	// 9. 清理
	SetMetrics(nil)
	globalPool = nil
}

// TestGracefulShutdownFlow 优雅关闭完整流程
func TestGracefulShutdownFlow(t *testing.T) {
	resetForTest()
	gracefulShutdown.once = sync.Once{}
	gracefulShutdown.hooks = nil

	// 1. 注册多个 hooks
	var hookOrder []int
	var mu sync.Mutex

	RegisterGracefulShutdownHook(func() {
		mu.Lock()
		hookOrder = append(hookOrder, 1)
		mu.Unlock()
	})
	RegisterGracefulShutdownHook(func() {
		mu.Lock()
		hookOrder = append(hookOrder, 2)
		mu.Unlock()
	})
	RegisterGracefulShutdownHook(func() {
		mu.Lock()
		hookOrder = append(hookOrder, 3)
		mu.Unlock()
	})

	// 2. 执行 hooks
	executeGracefulShutdownHooks()

	// 3. 验证执行顺序
	if len(hookOrder) != 3 {
		t.Fatalf("hookOrder len = %d, want 3", len(hookOrder))
	}
	for i, v := range hookOrder {
		if v != i+1 {
			t.Errorf("hookOrder[%d] = %d, want %d", i, v, i+1)
		}
	}

	// 4. 再次执行不应有效果
	executeGracefulShutdownHooks()
	if len(hookOrder) != 3 {
		t.Errorf("hookOrder len = %d after second execute, want 3", len(hookOrder))
	}
}

// TestSetMetricsRuntimeReplacement 运行时替换监控
func TestSetMetricsRuntimeReplacement(t *testing.T) {
	resetForTest()

	// 1. 设置第一个监控
	mm1 := &mockMetrics{}
	SetMetrics(mm1)

	// 2. 提交任务并等待任务开始
	startCh := make(chan struct{})
	blockCh := make(chan struct{})
	Go(context.Background(), func() {
		close(startCh)
		<-blockCh
	})
	<-startCh // IncRunning 先于任务体执行，close(startCh) 已建立 happens-before，直读安全

	// 3. 验证第一个监控记录了数据
	if mm1.running.Load() < 1 {
		t.Error("mm1 should have recorded running tasks")
	}

	// 4. 替换为第二个监控
	mm2 := &mockMetrics{}
	SetMetrics(mm2)

	// 5. 提交新任务并等待任务开始
	startCh2 := make(chan struct{})
	blockCh2 := make(chan struct{})
	Go(context.Background(), func() {
		close(startCh2)
		<-blockCh2
	})
	<-startCh2 // 同上：close(startCh2) 已建立 happens-before，直读安全

	// 6. 验证第二个监控记录了数据
	if mm2.running.Load() < 1 {
		t.Error("mm2 should have recorded running tasks")
	}

	// 7. 清理
	close(blockCh)
	close(blockCh2)
	SetMetrics(nil)
	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestSubmitToReleasedPool 提交任务到已释放的池
func TestSubmitToReleasedPool(t *testing.T) {
	resetForTest()

	// 1. 初始化并释放
	Init(WithPoolSize(10))
	ReleaseAndWaitWithTimeout(5 * time.Second)

	// 2. 提交任务（应该降级为原生 goroutine）
	var executed atomic.Bool
	done := make(chan struct{})
	Go(context.Background(), func() {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("task to released pool did not complete")
	}

	if !executed.Load() {
		t.Error("task was not executed")
	}

	globalPool = nil
}

// TestIsFull 各种池状态下的 IsFull 检查
func TestIsFull(t *testing.T) {
	resetForTest()

	// 1. 初始化池
	Init(WithPoolSize(10))

	// 2. 空池不应满
	if globalPool.IsFull() {
		t.Error("empty pool should not be full")
	}

	// 3. 使用 rejectPool 测试满池
	globalPool = &rejectPool{}
	if !globalPool.IsFull() {
		t.Error("rejectPool should be full")
	}

	globalPool = nil
}

// TestGoBatchCancelledCtxReturns 验证已取消 ctx 下 GoBatch 立即返回且不执行任务。
// 历史缺陷：被跳过的任务不会执行闭包内的 wg.Done()，wg.Wait 永久阻塞（调用方协程泄漏）；
// 原用例只断言 counter=0，对“阻塞”与“立即返回”都不断言，实际是空断言。
func TestGoBatchCancelledCtxReturns(t *testing.T) {
	resetForTest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var counter atomic.Int64
	tasks := make([]func(), 10)
	for i := range tasks {
		tasks[i] = func() {
			counter.Add(1)
		}
	}

	done := make(chan struct{})
	go func() {
		GoBatch(ctx, tasks)
		close(done)
	}()

	// 断言一：函数必须返回（阻塞即死锁回归）
	waitDone(t, done, "已取消 ctx 下 GoBatch 未返回（死锁回归）")

	// 断言二：任务全部被跳过
	if v := counter.Load(); v != 0 {
		t.Errorf("counter = %d, want 0（全部任务应被跳过）", v)
	}

	resetForTest()
}

// TestGoBatchWithResultCancelledCtx 验证已取消 ctx 下 GoBatchWithResult 不阻塞，
// 且被跳过的任务返回 ctx.Err() 聚合错误（而非静默零值）。
func TestGoBatchWithResultCancelledCtx(t *testing.T) {
	resetForTest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tasks := []func() (int, error){
		func() (int, error) { return 1, nil },
		func() (int, error) { return 2, nil },
	}

	type batchOut struct {
		results []int
		err     error
	}
	done := make(chan batchOut, 1)
	go func() {
		results, err := GoBatchWithResult(ctx, "cancelled", tasks)
		done <- batchOut{results: results, err: err}
	}()

	var out batchOut
	select {
	case out = <-done:
	case <-time.After(taskWaitTimeout):
		t.Fatal("已取消 ctx 下 GoBatchWithResult 未返回（死锁回归）")
	}

	if !errors.Is(out.err, context.Canceled) {
		t.Errorf("err = %v, want errors.Is(context.Canceled)", out.err)
	}
	if len(out.results) != 2 {
		t.Errorf("len(results) = %d, want 2（零值切片仍应保持长度）", len(out.results))
	}

	resetForTest()
}

// TestGoNilCtxFallsBackToBackground 验证 nil ctx 不 panic 且任务正常执行。
func TestGoNilCtxFallsBackToBackground(t *testing.T) {
	resetForTest()

	done := make(chan struct{})
	Go(nil, func() { close(done) }) // 故意传 nil，验证 normalizeCtx 防御层
	waitDone(t, done, "nil ctx 下 Go 任务未执行")

	ReleaseAndWaitWithTimeout(taskWaitTimeout)
}

// TestGoWithNameNilCtxFallsBackToBackground 验证 GoWithName 的 nil ctx 防御。
func TestGoWithNameNilCtxFallsBackToBackground(t *testing.T) {
	resetForTest()

	done := make(chan struct{})
	GoWithName(nil, "nil-ctx", func() { close(done) }) // 故意传 nil
	waitDone(t, done, "nil ctx 下 GoWithName 任务未执行")

	ReleaseAndWaitWithTimeout(taskWaitTimeout)
}

// TestGoWithTimeoutNilCtxFallsBackToBackground 验证 GoWithTimeout 入口归一 nil ctx
// （否则闭包内 context.WithTimeout(nil, ...) 会 panic）。
func TestGoWithTimeoutNilCtxFallsBackToBackground(t *testing.T) {
	resetForTest()

	done := make(chan struct{})
	GoWithTimeout(nil, "nil-ctx-timeout", time.Second, func(context.Context) { // 故意传 nil
		close(done)
	})
	waitDone(t, done, "nil ctx 下 GoWithTimeout 任务未执行")

	ReleaseAndWaitWithTimeout(taskWaitTimeout)
}

// TestGoBatchNilCtxFallsBackToBackground 验证批量接口的 nil ctx 防御。
func TestGoBatchNilCtxFallsBackToBackground(t *testing.T) {
	resetForTest()

	var counter atomic.Int64
	done := make(chan struct{})
	GoBatch(nil, []func(){ // 故意传 nil
		func() {
			counter.Add(1)
			close(done)
		},
	})
	waitDone(t, done, "nil ctx 下 GoBatch 任务未执行")

	ReleaseAndWaitWithTimeout(taskWaitTimeout)
}

// ============================================================================
// 测试 Init 迟到调用与批量 nil 任务条目（评审 R2-P1-1 / R2-P1-4）
// ============================================================================

// TestInitAfterLazyInitIgnoresOptions 验证先 Go 后 Init 的隐式陷阱：
// 池已由惰性路径按默认配置创建，迟到 Init 的 Option 不生效（同时打 WARN），
// 守护「以为 WithPoolSize(5000) 已生效而实际仍是 1000」的误判（评审 R2-P1-1）。
func TestInitAfterLazyInitIgnoresOptions(t *testing.T) {
	resetForTest()

	// 惰性路径先触发（模拟某包 init() 里先调 Go）
	done := make(chan struct{})
	Go(context.Background(), func() { close(done) })
	<-done
	waitUntil(t, func() bool { return currentGlobalPool() != nil }, "惰性初始化未创建池")

	// 迟到的 Init：本次 Option 被忽略并打 WARN 日志
	Init(WithPoolSize(5000))

	p := currentGlobalPool()
	if p == nil {
		t.Fatal("全局池不应为 nil")
	}
	if got := p.GetCap(); got != DefaultPoolSize {
		t.Errorf("GetCap() = %d, want %d（迟到 Init 的 WithPoolSize 不应生效）", got, DefaultPoolSize)
	}
}

// TestGoBatchWithResultNilTask 验证 nil 任务条目记 errNilTask 而非静默返回 err==nil
// （评审 R2-P1-4：原实现包装闭包非 nil 被受理，执行时 panic 被 recover，
// errs[i] 保持 nil，调用方误以为全部成功）。
func TestGoBatchWithResultNilTask(t *testing.T) {
	resetForTest()

	tasks := []func() (int, error){
		nil,
		func() (int, error) { return 42, nil },
	}
	results, err := GoBatchWithResult(context.Background(), "nil-entry", tasks)
	if err == nil {
		t.Fatal("nil 任务条目应产生错误，实际 err == nil（静默失败回归）")
	}
	if !errors.Is(err, errNilTask) {
		t.Errorf("err 应可 errors.Is(errNilTask)，实际: %v", err)
	}
	if results[0] != 0 {
		t.Errorf("results[0] = %d, want 0（nil 条目结果位为零值）", results[0])
	}
	if results[1] != 42 {
		t.Errorf("results[1] = %d, want 42（正常条目不受影响）", results[1])
	}
}

// TestGoBatchWithNameNilTaskSkipped 验证 GoBatch* 的 nil 条目当场跳过：
// 不提交、不产生 panic 噪声、不影响其他任务、不挂起（与 GoBatchWithResult 口径一致）。
func TestGoBatchWithNameNilTaskSkipped(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	GoBatch(context.Background(), []func(){
		nil,
		func() { executed.Store(true) },
	})
	if !executed.Load() {
		t.Error("非 nil 任务应正常执行")
	}
}

// TestGoWithTimeoutContextAlreadyCancelled 超时任务使用已取消的 ctx
func TestGoWithTimeoutContextAlreadyCancelled(t *testing.T) {
	resetForTest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var executed atomic.Bool
	GoWithTimeout(ctx, "cancelled", 1*time.Second, func(ctx context.Context) {
		executed.Store(true)
	})

	// 负向断言：「不发生」无完成信号可监听，只能给定观察窗口（见 negativeAssertionWindow 注释）
	time.Sleep(negativeAssertionWindow)
	if executed.Load() {
		t.Error("task with cancelled ctx should not be executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}
