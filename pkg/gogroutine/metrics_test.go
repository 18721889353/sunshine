package gogroutine

import (
	"testing"
	"time"
)

// ============================================================================
// 测试 SetMetrics / getMetrics
// ============================================================================

func TestSetMetrics_GetMetrics(t *testing.T) {
	// 初始状态应为 nil
	SetMetrics(nil)
	if m := getMetrics(); m != nil {
		t.Error("metrics should be nil by default")
	}

	// 设置 mock 指标
	mm := &mockMetrics{}
	SetMetrics(mm)
	if m := getMetrics(); m == nil {
		t.Error("metrics should not be nil after SetMetrics")
	}

	// 清除
	SetMetrics(nil)
	if m := getMetrics(); m != nil {
		t.Error("metrics should be nil after setting nil")
	}
}

// ============================================================================
// 测试原子计数器 - 独立于池生命周期
// ============================================================================

func TestAtomicCounters_Initial(t *testing.T) {
	successCount.Store(0)
	panicCount.Store(0)
	fallbackCount.Store(0)

	if v := successCount.Load(); v != 0 {
		t.Errorf("successCount = %d, want 0", v)
	}
	if v := panicCount.Load(); v != 0 {
		t.Errorf("panicCount = %d, want 0", v)
	}
	if v := fallbackCount.Load(); v != 0 {
		t.Errorf("fallbackCount = %d, want 0", v)
	}
}

func TestAtomicCounters_IncrementSuccess(t *testing.T) {
	successCount.Store(0)
	panicCount.Store(0)
	fallbackCount.Store(0)

	// 创建最小池并提交一个简单任务
	p, err := newAntsPool(MinPoolSize)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}
	defer p.Release()

	done := make(chan struct{})
	if err := p.Submit(func() {
		successCount.Add(1)
		close(done)
	}); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	<-done
	if v := successCount.Load(); v != 1 {
		t.Errorf("successCount = %d, want 1", v)
	}
}

func TestAtomicCounters_IncrementMultiple(t *testing.T) {
	successCount.Store(0)

	for i := 0; i < 10; i++ {
		successCount.Add(1)
	}

	if v := successCount.Load(); v != 10 {
		t.Errorf("successCount = %d, want 10", v)
	}
}

// ============================================================================
// 测试 mockMetrics - 指标回调正确触发
// ============================================================================

func TestMockMetrics_IncRunning(t *testing.T) {
	mm := &mockMetrics{}
	mm.IncRunning("test-task")

	if v := mm.running.Load(); v != 1 {
		t.Errorf("running = %d, want 1", v)
	}
}

func TestMockMetrics_DecRunning(t *testing.T) {
	mm := &mockMetrics{}
	mm.IncRunning("test-task")
	mm.DecRunning("test-task")

	if v := mm.running.Load(); v != 0 {
		t.Errorf("running = %d, want 0", v)
	}
}

func TestMockMetrics_IncPanic(t *testing.T) {
	mm := &mockMetrics{}
	mm.IncPanic("test-task")

	if v := mm.panicCount.Load(); v != 1 {
		t.Errorf("panicCount = %d, want 1", v)
	}
}

func TestMockMetrics_IncFallback(t *testing.T) {
	mm := &mockMetrics{}
	mm.IncFallback("test-task")

	if v := mm.fallback.Load(); v != 1 {
		t.Errorf("fallback = %d, want 1", v)
	}
}

// ============================================================================
// 测试 ObserveTaskDuration - 记录任务耗时
// ============================================================================

func TestMockMetrics_ObserveTaskDuration(t *testing.T) {
	mm := &mockMetrics{}

	// 此方法无返回值，验证不 panic 即可
	mm.ObserveTaskDuration("test-task", 100*time.Millisecond)
	mm.ObserveTaskDuration("test-task", 5*time.Second)
}
