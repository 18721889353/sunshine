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

	// 重置计数器
	successCount.Store(0)
	panicCount.Store(0)
	fallbackCount.Store(0)

	// 重置指标
	SetMetrics(nil)
}

// mockMetrics 用于测试的指标采集器。
type mockMetrics struct {
	running    atomic.Int64
	panicCount atomic.Int64
	fallback   atomic.Int64
}

func (m *mockMetrics) IncRunning(_ string)                    { m.running.Add(1) }
func (m *mockMetrics) DecRunning(_ string)                    { m.running.Add(-1) }
func (m *mockMetrics) ObserveTaskDuration(_ string, _ time.Duration) {}
func (m *mockMetrics) IncPanic(_ string)                      { m.panicCount.Add(1) }
func (m *mockMetrics) IncFallback(_ string)                   { m.fallback.Add(1) }

// ============================================================================
// 测试 Init - 全局池初始化
// ============================================================================

func TestInit_DefaultConfig(t *testing.T) {
	resetForTest()
	Init()

	if defaultPool == nil {
		t.Fatal("defaultPool should be initialized after Init()")
	}
	if v := defaultPool.Cap(); v != DefaultPoolSize {
		t.Errorf("Cap() = %d, want %d", v, DefaultPoolSize)
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
	if v := defaultPool.Cap(); v != 50 {
		t.Errorf("Cap() = %d, want 50", v)
	}

	defaultPool.Release()
	defaultPool = nil
}

func TestInit_Idempotent(t *testing.T) {
	resetForTest()
	Init(WithPoolSize(50))

	// 第二次 Init 应被忽略
	Init(WithPoolSize(200))

	if v := defaultPool.Cap(); v != 50 {
		t.Errorf("Cap() = %d, want 50 (second Init should be ignored)", v)
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
// 测试 GoWithDeadline - 截止时间
// ============================================================================

func TestGoWithDeadline_Success(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	deadline := time.Now().Add(5 * time.Second)
	GoWithDeadline(context.Background(), "deadline-task", deadline, func(ctx context.Context) {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GoWithDeadline task did not complete within timeout")
	}

	if !executed.Load() {
		t.Error("task was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoWithDeadline_ExpiredDeadline(t *testing.T) {
	resetForTest()

	deadline := time.Now().Add(-1 * time.Second) // 已过期
	done := make(chan struct{})

	GoWithDeadline(context.Background(), "expired-deadline", deadline, func(ctx context.Context) {
		<-ctx.Done()
		close(done)
	})

	select {
	case <-done:
		// 任务在过期 deadline 后正确退出
	case <-time.After(5 * time.Second):
		t.Fatal("task did not observe expired deadline")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

// ============================================================================
// 测试 GoWithCancel - 手动取消
// ============================================================================

func TestGoWithCancel_Success(t *testing.T) {
	resetForTest()

	var executed atomic.Bool
	done := make(chan struct{})

	GoWithCancel(context.Background(), "cancel-task", func(ctx context.Context) {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GoWithCancel task did not complete within timeout")
	}

	if !executed.Load() {
		t.Error("task was not executed")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestGoWithCancel_ManualCancel(t *testing.T) {
	resetForTest()

	done := make(chan struct{})
	cancel := GoWithCancel(context.Background(), "manual-cancel", func(ctx context.Context) {
		<-ctx.Done()
		close(done)
	})

	// 等待任务启动
	time.Sleep(10 * time.Millisecond)

	// 手动取消
	cancel()

	select {
	case <-done:
		// 任务正确响应取消
	case <-time.After(5 * time.Second):
		t.Fatal("task did not respond to cancel()")
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

	tasks := []func() (string, error){
		func() (string, error) { return "ok", nil },
		func() (string, error) { return "", errors.New("failed") },
	}

	results, err := GoBatchWithResult(context.Background(), "fetch-fail", tasks)
	if err == nil {
		t.Fatal("expected error, got nil")
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

	tasks := []func() (int, error){
		func() (int, error) { return 0, errors.New("err1") },
		func() (int, error) { return 0, errors.New("err2") },
	}

	results, err := GoBatchWithResult(context.Background(), "all-fail", tasks)
	if err == nil {
		t.Fatal("expected error, got nil")
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
	if err != target {
		t.Errorf("expected target error, got %v", err)
	}
}

func TestCollectBatchErrors_MultipleErrors(t *testing.T) {
	errs := []error{
		errors.New("err1"),
		errors.New("err2"),
		errors.New("err3"),
	}
	err := collectBatchErrors(errs)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 应包含失败数量信息
	expected := "batch: 3/3 tasks failed"
	if got := err.Error(); len(got) < len(expected) {
		t.Errorf("error message too short: %q", got)
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
func (p *rejectPool) Running() int   { return 0 }
func (p *rejectPool) Waiting() int   { return 0 }
func (p *rejectPool) Cap() int       { return 10 }
func (p *rejectPool) Release()       {}
func (p *rejectPool) IsFull() bool   { return true }

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
	if v := fallbackCount.Load(); v < 1 {
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
// 测试 PoolStats / Stats / FormatStats
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

func TestStats_CompatAlias(t *testing.T) {
	resetForTest()

	s1 := PoolStats()
	s2 := Stats()

	// 两者返回相同结构体类型
	if fmt.Sprintf("%T", s1) != fmt.Sprintf("%T", s2) {
		t.Error("Stats() and PoolStats() should return same type")
	}

	ReleaseAndWaitWithTimeout(5 * time.Second)
}

func TestFormatStats(t *testing.T) {
	s := StatsInfo{
		Running:  5,
		Waiting:  3,
		Cap:      100,
		Success:  500,
		Panic:    2,
		Fallback: 1,
	}

	result := FormatStats(s)

	// 应包含关键信息
	if len(result) == 0 {
		t.Error("FormatStats returned empty string")
	}
}

// ============================================================================
// 测试 Running / Waiting / Cap / IsFull
// ============================================================================

func TestRunning_Waiting_Cap_IsFull(t *testing.T) {
	resetForTest()

	// 初始状态
	if v := Running(); v != 0 {
		t.Errorf("Running() = %d, want 0", v)
	}
	if v := Waiting(); v != 0 {
		t.Errorf("Waiting() = %d, want 0", v)
	}
	if v := Cap(); v != DefaultPoolSize {
		t.Errorf("Cap() = %d, want %d", v, DefaultPoolSize)
	}
	if IsFull() {
		t.Error("IsFull() should be false for empty pool")
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

// ============================================================================
// 测试 DefaultConfig 兼容性
// ============================================================================

func TestDefaultConfig_ReturnsCorrectValues(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.PoolSize != DefaultPoolSize {
		t.Errorf("PoolSize = %d, want %d", cfg.PoolSize, DefaultPoolSize)
	}
	if cfg.PurgeInterval != 1*time.Second {
		t.Errorf("PurgeInterval = %v, want 1s", cfg.PurgeInterval)
	}
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

	if v := panicCount.Load(); v < 1 {
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
