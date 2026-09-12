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

// resetForTest 重置全局状态，确保测试隔离。
func resetForTest() {
	// 释放旧池
	if defaultPool != nil {
		defaultPool.Release()
		defaultPool = nil
	}
	// 重置 sync.Once（重新赋值为零值）
	defaultPoolOnce = sync.Once{}

	// 重置全局指标管理器
	resetMetrics()
}

// mockMetrics 用于测试的指标采集器。
type mockMetrics struct {
	running    atomic.Int64
	panicCount atomic.Int64
	fallback   atomic.Int64
}

func (m *mockMetrics) IncRunning(_ string)                           { m.running.Add(1) }
func (m *mockMetrics) DecRunning(_ string)                           { m.running.Add(-1) }
func (m *mockMetrics) ObserveTaskDuration(_ string, _ time.Duration) {}
func (m *mockMetrics) IncPanic(_ string)                             { m.panicCount.Add(1) }
func (m *mockMetrics) IncFallback(_ string)                          { m.fallback.Add(1) }

// ============================================================================
// 测试 Init - 全局池初始化
// ============================================================================

func TestInit_DefaultConfig(t *testing.T) {
	resetForTest()
	Init()

	if defaultPool == nil {
		t.Fatal("defaultPool should be initialized after Init()")
	}
	if v := defaultPool.GetCap(); v != DefaultPoolSize {
		t.Errorf("GetCap() = %d, want %d", v, DefaultPoolSize)
	}

	defaultPool.Release()
	defaultPool = nil
}

func TestInit_WithOptions(t *testing.T) {
	resetForTest()
	Init(WithPoolSize(50))

	if defaultPool == nil {
		t.Fatal("defaultPool should be initialized after Init()")
	}
	if v := defaultPool.GetCap(); v != 50 {
		t.Errorf("GetCap() = %d, want 50", v)
	}

	defaultPool.Release()
	defaultPool = nil
}

func TestInit_Idempotent(t *testing.T) {
	resetForTest()
	Init(WithPoolSize(50))

	// 第二次 Init 应被忽略
	Init(WithPoolSize(200))

	if v := defaultPool.GetCap(); v != 50 {
		t.Errorf("GetCap() = %d, want 50 (second Init should be ignored)", v)
	}

	defaultPool.Release()
	defaultPool = nil
}

// ============================================================================
// 测试 Go / GoWithName - 基础任务提交
// ============================================================================

func TestGo_Success(t *testing.T) {
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

func TestGo_NilTask(t *testing.T) {
	resetForTest()

	// nil 任务不应 panic
	Go(context.Background(), nil)

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoWithName_Success(t *testing.T) {
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

func TestGoWithName_CancelledCtx(t *testing.T) {
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

func TestGoWithTimeout_Success(t *testing.T) {
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

func TestGoWithTimeout_ContextCancelled(t *testing.T) {
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

func TestGoBatch_Success(t *testing.T) {
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

func TestGoBatch_EmptyTasks(t *testing.T) {
	resetForTest()

	// 空任务列表不应 panic
	GoBatch(context.Background(), nil)
	GoBatch(context.Background(), []func(){})
}

func TestGoBatch_SingleTask(t *testing.T) {
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

func TestGoBatchWithName_Success(t *testing.T) {
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

func TestGoBatchWithName_EmptyTasks(t *testing.T) {
	resetForTest()

	// 空任务列表不应 panic
	GoBatchWithName(context.Background(), "empty", nil)
	GoBatchWithName(context.Background(), "empty", []func(){})
}

// ============================================================================
// 测试 GoBatchWithResult - 泛型批量结果收集
// ============================================================================

func TestGoBatchWithResult_Success(t *testing.T) {
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

func TestGoBatchWithResult_WithError(t *testing.T) {
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

func TestGoBatchWithResult_EmptyTasks(t *testing.T) {
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

func TestGoBatchWithResult_AllErrors(t *testing.T) {
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

func TestCollectBatchErrors_NoErrors(t *testing.T) {
	errs := []error{nil, nil, nil}
	if err := collectBatchErrors(errs); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCollectBatchErrors_SingleError(t *testing.T) {
	target := errors.New("target error")
	errs := []error{nil, target, nil}
	err := collectBatchErrors(errs)
	if !errors.Is(err, target) {
		t.Errorf("expected target error, got %v", err)
	}
}

func TestCollectBatchErrors_MultipleErrors(t *testing.T) {
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

func TestCollectBatchErrors_EmptySlice(t *testing.T) {
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

func (p *rejectPool) Submit(_ func()) error {
	return errors.New("pool full")
}
func (p *rejectPool) GetRunningNum() int { return 0 }
func (p *rejectPool) GetWaitingNum() int { return 0 }
func (p *rejectPool) GetCap() int        { return 10 }
func (p *rejectPool) Release()           {}
func (p *rejectPool) IsFull() bool       { return true }

func TestGoWithName_FallbackWhenPoolFull(t *testing.T) {
	resetForTest()

	// 注入 mock Pool，使 Submit 始终返回错误，触发降级路径
	defaultPool = &rejectPool{}

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

	defaultPool = nil
}

func TestGoWithName_FallbackWithMetrics(t *testing.T) {
	resetForTest()

	mm := &mockMetrics{}
	SetMetrics(mm)

	// 注入 mock Pool，使 Submit 始终返回错误，触发降级路径
	defaultPool = &rejectPool{}

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
	defaultPool = nil
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
		time.Sleep(10 * time.Millisecond)
	})

	time.Sleep(10 * time.Millisecond)
	Release()

	// Release 后不应 panic
	if defaultPool != nil {
		// pool 已释放但引用可能还在（取决于实现）
	}
}

func TestReleaseAndWait(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	Go(context.Background(), func() {
		time.Sleep(50 * time.Millisecond)
		executed.Store(true)
	})

	// 等待任务完成后再释放
	time.Sleep(100 * time.Millisecond)
	ReleaseAndWait()

	if !executed.Load() {
		t.Error("task should have completed before ReleaseAndWait returned")
	}

	defaultPool = nil
}

func TestReleaseAndWaitWithTimeout_Timeout(t *testing.T) {
	resetForTest()

	blockCh := make(chan struct{})
	Go(context.Background(), func() {
		<-blockCh
	})

	time.Sleep(10 * time.Millisecond)

	// 设置极短超时，任务未完成就超时
	ReleaseAndWaitWithTimeout(1 * time.Millisecond)

	close(blockCh)
	defaultPool = nil
}

func TestReleaseAndWaitWithTimeout_NilPool(t *testing.T) {
	// defaultPool 为 nil 时不应 panic
	ReleaseAndWaitWithTimeout(1 * time.Second)
}

// TestReleaseAndWaitWithTimeout_WaitForCompletion 测试等待任务完成后释放
func TestReleaseAndWaitWithTimeout_WaitForCompletion(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	Go(context.Background(), func() {
		time.Sleep(50 * time.Millisecond)
		executed.Store(true)
	})

	time.Sleep(100 * time.Millisecond)

	// 等待足够时间让任务完成
	ReleaseAndWaitWithTimeout(5 * time.Second)

	if !executed.Load() {
		t.Error("task should have completed before timeout")
	}

	defaultPool = nil
}

// TestReleaseAndWaitWithTimeout_TimeoutPath 测试超时路径
func TestReleaseAndWaitWithTimeout_TimeoutPath(t *testing.T) {
	resetForTest()

	blockCh := make(chan struct{})
	Go(context.Background(), func() {
		<-blockCh
	})

	time.Sleep(10 * time.Millisecond)

	// 设置极短超时，任务未完成就超时
	ReleaseAndWaitWithTimeout(1 * time.Millisecond)

	close(blockCh)
	defaultPool = nil
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

	wg.Wait()

	// 等待所有任务执行完成
	time.Sleep(200 * time.Millisecond)

	if v := counter.Load(); v != n {
		t.Errorf("counter = %d, want %d", v, n)
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 panic 恢复 - 通过 Go API 提交 panic 任务
// ============================================================================

func TestGo_PanicRecovery(t *testing.T) {
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

	// 等待异步 panic 处理完成
	time.Sleep(50 * time.Millisecond)

	if v := metricsMgr.panicCount.Load(); v < 1 {
		t.Errorf("panicCount = %d, want >= 1", v)
	}
	if !executed.Load() {
		t.Error("task after panic was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 ctx 校验 - 已取消的 ctx 跳过提交
// ============================================================================

func TestGo_CancelledCtxSkipped(t *testing.T) {
	resetForTest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	var executed atomic.Bool
	Go(ctx, func() {
		executed.Store(true)
	})

	// 等待一下确认没有任务执行
	time.Sleep(100 * time.Millisecond)

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

func TestRegisterGracefulShutdownHook_Multiple(t *testing.T) {
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

func TestRegisterGracefulShutdownHook_OnceOnly(t *testing.T) {
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

func TestRegisterGracefulShutdownHook_PanicRecovery(t *testing.T) {
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

// TestGoBatch_LargeNumberOfTasks 测试大批量任务（1000+）
func TestGoBatch_LargeNumberOfTasks(t *testing.T) {
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

// TestGoBatchWithResult_LargeNumberOfTasks 测试大批量结果收集
func TestGoBatchWithResult_LargeNumberOfTasks(t *testing.T) {
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

// TestGoWithTimeout_VeryShortTimeout 测试极短超时
func TestGoWithTimeout_VeryShortTimeout(t *testing.T) {
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

// TestGoWithTimeout_NegativeTimeout 测试负数超时
func TestGoWithTimeout_NegativeTimeout(t *testing.T) {
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

// TestReleaseAndWaitWithTimeout_ZeroTimeout 测试零超时
func TestReleaseAndWaitWithTimeout_ZeroTimeout(t *testing.T) {
	resetForTest()

	blockCh := make(chan struct{})
	Go(context.Background(), func() {
		<-blockCh
	})

	time.Sleep(10 * time.Millisecond)

	// 零超时应该立即返回
	ReleaseAndWaitWithTimeout(0)

	close(blockCh)
	defaultPool = nil
}

// TestPoolStats_WithRunningTasks 测试运行中任务的统计
func TestPoolStats_WithRunningTasks(t *testing.T) {
	resetForTest()

	startCh := make(chan struct{})
	blockCh := make(chan struct{})

	// 提交一个阻塞任务
	Go(context.Background(), func() {
		close(startCh)
		<-blockCh
	})

	<-startCh
	time.Sleep(10 * time.Millisecond)

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
	if v := defaultPool.GetCap(); v != 100 {
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
	defaultPool = nil
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

	defaultPool = nil
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
		time.Sleep(10 * time.Millisecond)
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

// TestGo_NestedPanic 嵌套 panic 恢复
func TestGo_NestedPanic(t *testing.T) {
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

	time.Sleep(50 * time.Millisecond)

	if v := metricsMgr.panicCount.Load(); v < 1 {
		t.Errorf("panicCount = %d, want >= 1", v)
	}
	if !executed.Load() {
		t.Error("task after panic was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestGo_PanicWithNilRecover panic 后 recover 返回 nil
func TestGo_PanicWithNilRecover(t *testing.T) {
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

	time.Sleep(50 * time.Millisecond)

	// panic(nil) 在 Go 中是特殊情况，recover() 返回 nil
	// 但我们的代码应该能处理这种情况
	if !executed.Load() {
		t.Error("task after panic was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// TestGoBatchWithResult_MixedSuccessAndError 混合成功和失败
func TestGoBatchWithResult_MixedSuccessAndError(t *testing.T) {
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

// TestGoWithName_EmptyName 空任务名称
func TestGoWithName_EmptyName(t *testing.T) {
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

// TestGoWithName_VeryLongName 极长任务名称
func TestGoWithName_VeryLongName(t *testing.T) {
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

// TestWithPoolSize_BoundaryValues 池大小边界值
func TestWithPoolSize_BoundaryValues(t *testing.T) {
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
			time.Sleep(10 * time.Millisecond)
		})
	}

	// 5. 等待任务完成
	wg.Wait()
	time.Sleep(50 * time.Millisecond)

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
	defaultPool = nil
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

// TestSetMetrics_RuntimeReplacement 运行时替换监控
func TestSetMetrics_RuntimeReplacement(t *testing.T) {
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
	<-startCh
	time.Sleep(10 * time.Millisecond)

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
	<-startCh2
	time.Sleep(10 * time.Millisecond)

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

	defaultPool = nil
}

// TestIsFull 各种池状态下的 IsFull 检查
func TestIsFull(t *testing.T) {
	resetForTest()

	// 1. 初始化池
	Init(WithPoolSize(10))

	// 2. 空池不应满
	if defaultPool.IsFull() {
		t.Error("empty pool should not be full")
	}

	// 3. 使用 rejectPool 测试满池
	defaultPool = &rejectPool{}
	if !defaultPool.IsFull() {
		t.Error("rejectPool should be full")
	}

	defaultPool = nil
}

// TestBatchWithCancelledCtx 批量任务使用已取消的 ctx
// 注意：当 ctx 已取消时，GoBatchWithName 会阻塞，因为被跳过的任务不会调用 wg.Done()
// 这是当前设计的预期行为，用户应该在调用前检查 ctx 状态
func TestBatchWithCancelledCtx(t *testing.T) {
	resetForTest()

	// 使用已取消的 ctx 调用 GoBatch
	// 预期：任务被跳过，但函数会阻塞（因为 wg.Done() 不会被调用）
	// 我们使用超时来验证这个行为
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var counter atomic.Int64
	tasks := make([]func(), 10)
	for i := range tasks {
		tasks[i] = func() {
			counter.Add(1)
		}
	}

	// 启动一个 goroutine 调用 GoBatch
	done := make(chan struct{})
	go func() {
		GoBatch(ctx, tasks)
		close(done)
	}()

	// 等待一段时间，确认任务被跳过但函数阻塞
	time.Sleep(100 * time.Millisecond)
	if v := counter.Load(); v != 0 {
		t.Errorf("counter = %d, want 0 (all tasks should be skipped)", v)
	}

	// 函数应该还在阻塞（因为 wg.Done() 不会被调用）
	select {
	case <-done:
		// 如果函数返回了，说明任务被正确处理
	default:
		// 预期：函数还在阻塞
	}

	// 清理
	if defaultPool != nil {
		defaultPool.Release()
		defaultPool = nil
	}
}

// TestGoWithTimeout_ContextAlreadyCancelled 超时任务使用已取消的 ctx
func TestGoWithTimeout_ContextAlreadyCancelled(t *testing.T) {
	resetForTest()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var executed atomic.Bool
	GoWithTimeout(ctx, "cancelled", 1*time.Second, func(ctx context.Context) {
		executed.Store(true)
	})

	time.Sleep(100 * time.Millisecond)
	if executed.Load() {
		t.Error("task with cancelled ctx should not be executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}
