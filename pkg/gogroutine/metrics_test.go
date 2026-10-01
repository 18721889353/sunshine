package gogroutine

import (
	"testing"
	"time"
)

// ============================================================================
// 测试 SetMetrics / getMetrics
// ============================================================================

func TestSetMetricsGetMetrics(t *testing.T) {
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

func TestAtomicCountersInitial(t *testing.T) {
	resetMetrics()

	if v := metricsMgr.successCount.Load(); v != 0 {
		t.Errorf("successCount = %d, want 0", v)
	}
	if v := metricsMgr.panicCount.Load(); v != 0 {
		t.Errorf("panicCount = %d, want 0", v)
	}
	if v := metricsMgr.fallbackCount.Load(); v != 0 {
		t.Errorf("fallbackCount = %d, want 0", v)
	}
}

func TestAtomicCountersIncrementSuccess(t *testing.T) {
	resetMetrics()

	// 创建最小池并提交一个简单任务
	p, err := newInstance("test-metrics", MinPoolSize)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}
	defer p.Release()

	done := make(chan struct{})
	if err := p.Submit(func() {
		metricsMgr.successCount.Add(1)
		close(done)
	}); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	<-done
	if v := metricsMgr.successCount.Load(); v != 1 {
		t.Errorf("successCount = %d, want 1", v)
	}
}

func TestAtomicCountersIncrementMultiple(t *testing.T) {
	resetMetrics()

	for i := 0; i < 10; i++ {
		metricsMgr.successCount.Add(1)
	}

	if v := metricsMgr.successCount.Load(); v != 10 {
		t.Errorf("successCount = %d, want 10", v)
	}
}

// ============================================================================
// 测试 mockMetrics - 指标回调正确触发
// ============================================================================

func TestMockMetricsIncRunning(t *testing.T) {
	mm := &mockMetrics{}
	mm.IncRunning("test-task")

	if v := mm.running.Load(); v != 1 {
		t.Errorf("running = %d, want 1", v)
	}
}

func TestMockMetricsDecRunning(t *testing.T) {
	mm := &mockMetrics{}
	mm.IncRunning("test-task")
	mm.DecRunning("test-task")

	if v := mm.running.Load(); v != 0 {
		t.Errorf("running = %d, want 0", v)
	}
}

func TestMockMetricsIncPanic(t *testing.T) {
	mm := &mockMetrics{}
	mm.IncPanic("test-task")

	if v := mm.panicCount.Load(); v != 1 {
		t.Errorf("panicCount = %d, want 1", v)
	}
}

func TestMockMetricsIncFallback(t *testing.T) {
	mm := &mockMetrics{}
	mm.IncFallback("test-task")

	if v := mm.fallback.Load(); v != 1 {
		t.Errorf("fallback = %d, want 1", v)
	}
}

// ============================================================================
// 测试 ObserveTaskDuration - 记录任务耗时
// ============================================================================

func TestMockMetricsObserveTaskDuration(t *testing.T) {
	mm := &mockMetrics{}

	// 此方法无返回值，验证不 panic 即可
	mm.ObserveTaskDuration("test-task", 100*time.Millisecond)
	mm.ObserveTaskDuration("test-task", 5*time.Second)
}
