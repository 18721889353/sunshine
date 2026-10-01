package gogroutine

import (
	"context"
	"testing"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
)

// 测试辅助（resetMetrics/mockMetrics 等）统一集中在 test_helpers_test.go

// ============================================================================
// 测试 requestIDAttr - 链路追踪属性
// ============================================================================

func TestRequestIDAttrWithRequestID(t *testing.T) {
	// 创建带有 request_id 的 context
	ctx := context.WithValue(context.Background(), logger.ContextKeyRequestID, "test-request-123")
	attr := requestIDAttr(ctx)

	if attr.Value.AsString() != "test-request-123" {
		t.Errorf("request_id = %q, want %q", attr.Value.AsString(), "test-request-123")
	}
}

func TestRequestIDAttrWithoutRequestID(t *testing.T) {
	// 创建没有 request_id 的 context
	ctx := context.Background()
	attr := requestIDAttr(ctx)

	if attr.Value.AsString() != "" {
		t.Errorf("request_id = %q, want empty string", attr.Value.AsString())
	}
}

func TestRequestIDAttrNilCtx(t *testing.T) {
	// 测试 nil context
	attr := requestIDAttr(nil)

	if attr.Value.AsString() != "" {
		t.Errorf("request_id = %q, want empty string", attr.Value.AsString())
	}
}

func TestRequestIDAttrEmptyRequestID(t *testing.T) {
	// 创建带有空 request_id 的 context
	ctx := context.WithValue(context.Background(), logger.ContextKeyRequestID, "")
	attr := requestIDAttr(ctx)

	if attr.Value.AsString() != "" {
		t.Errorf("request_id = %q, want empty string", attr.Value.AsString())
	}
}

// ============================================================================
// 测试 shouldSkipSubmit - 提交前校验
// ============================================================================

func TestShouldSkipSubmitNilCtx(t *testing.T) {
	// nil ctx 不 panic 且不跳过（normalizeCtx 归一为 Background 的防御层）
	if shouldSkipSubmit(nil, "test") {
		t.Error("nil ctx 应返回 false（按 Background 处理，不跳过任务）")
	}
}

func TestShouldSkipSubmitCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	if !shouldSkipSubmit(ctx, "test") {
		t.Error("shouldSkipSubmit should return true for cancelled context")
	}
}

func TestShouldSkipSubmitDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()

	if !shouldSkipSubmit(ctx, "test") {
		t.Error("shouldSkipSubmit should return true for expired deadline")
	}
}

// ============================================================================
// 测试 executeTask - 正常执行
// ============================================================================

func TestExecuteTaskSuccess(t *testing.T) {
	resetMetrics()

	done := make(chan struct{})
	executeTask(context.Background(), "test-task", func() {
		close(done)
	})

	select {
	case <-done:
		// 正常执行
	case <-time.After(5 * time.Second):
		t.Fatal("executeTask did not complete within timeout")
	}

	if v := metricsMgr.successCount.Load(); v != 1 {
		t.Errorf("successCount = %d, want 1", v)
	}
}

func TestExecuteTaskWithoutName(t *testing.T) {
	resetMetrics()

	done := make(chan struct{})
	executeTask(context.Background(), "", func() {
		close(done)
	})

	<-done

	// 匿名任务也应计入成功计数
	if v := metricsMgr.successCount.Load(); v != 1 {
		t.Errorf("successCount = %d, want 1", v)
	}
}

// ============================================================================
// 测试 executeTask - panic 恢复
// ============================================================================

func TestExecuteTaskPanicRecovery(t *testing.T) {
	resetMetrics()

	// executeTask 不应传播 panic
	done := make(chan struct{})
	executeTask(context.Background(), "panic-task", func() {
		panic("test panic")
	})
	close(done)

	<-done

	if v := metricsMgr.panicCount.Load(); v != 1 {
		t.Errorf("panicCount = %d, want 1", v)
	}
	// 注意：panic 任务不应计入成功计数
}

// ============================================================================
// 测试 executeTask - 指标集成
// ============================================================================

func TestExecuteTaskWithMetrics(t *testing.T) {
	resetMetrics()

	mm := &mockMetrics{}
	SetMetrics(mm)
	defer SetMetrics(nil)

	done := make(chan struct{})
	executeTask(context.Background(), "metric-task", func() {
		close(done)
	})
	<-done

	if v := mm.running.Load(); v != 0 {
		t.Errorf("running = %d, want 0 (should be dec'd after completion)", v)
	}
	if v := mm.panicCount.Load(); v != 0 {
		t.Errorf("panicCount = %d, want 0", v)
	}
}

func TestExecuteTaskPanicWithMetrics(t *testing.T) {
	resetMetrics()

	mm := &mockMetrics{}
	SetMetrics(mm)
	defer SetMetrics(nil)

	done := make(chan struct{})
	executeTask(context.Background(), "panic-metric-task", func() {
		panic("test panic with metrics")
	})
	close(done)

	<-done

	// panic 后 running 应归零
	if v := mm.running.Load(); v != 0 {
		t.Errorf("running = %d, want 0 (should be dec'd even on panic)", v)
	}
	if v := mm.panicCount.Load(); v != 1 {
		t.Errorf("panicCount = %d, want 1", v)
	}
}

// ============================================================================
// 测试 executeTask - 并发安全
// ============================================================================

func TestExecuteTaskConcurrent(t *testing.T) {
	resetMetrics()

	const goroutines = 50
	done := make(chan struct{}, goroutines)

	for i := 0; i < goroutines; i++ {
		executeTask(context.Background(), "concurrent-task", func() {
			done <- struct{}{}
		})
	}

	for i := 0; i < goroutines; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent tasks did not complete within timeout")
		}
	}

	if v := metricsMgr.successCount.Load(); v != goroutines {
		t.Errorf("successCount = %d, want %d", v, goroutines)
	}
}
