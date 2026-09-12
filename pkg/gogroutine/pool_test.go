package gogroutine

import (
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// 测试 Pool 接口实现
// ============================================================================

func TestPoolInstance_Submit(t *testing.T) {
	p, err := newInstance("test-submit", 100)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}
	defer p.Release()

	done := make(chan struct{})
	if err := p.Submit(func() {
		close(done)
	}); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	select {
	case <-done:
		// 任务执行完成
	case <-time.After(5 * time.Second):
		t.Fatal("task did not complete within timeout")
	}
}

func TestPoolInstance_Go(t *testing.T) {
	p, err := newInstance("test-go", 100)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}
	defer p.Release()

	var executed atomic.Bool
	done := make(chan struct{})

	p.Go(func() {
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
}

func TestPoolInstance_CtxGo(t *testing.T) {
	p, err := newInstance("test-ctxgo", 100)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}
	defer p.Release()

	var executed atomic.Bool
	done := make(chan struct{})

	p.CtxGo(t.Context(), func() {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CtxGo task did not complete within timeout")
	}

	if !executed.Load() {
		t.Error("task was not executed")
	}
}

func TestPoolInstance_Cap(t *testing.T) {
	p, err := newInstance("test-cap", 200)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}
	defer p.Release()

	if v := p.GetCap(); v != 200 {
		t.Errorf("GetCap() = %d, want 200", v)
	}
}

func TestPoolInstance_Name(t *testing.T) {
	p, err := newInstance("test-name", 100)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}
	defer p.Release()

	if v := p.Name(); v != "test-name" {
		t.Errorf("Name() = %s, want test-name", v)
	}
}

func TestPoolInstance_Release(t *testing.T) {
	p, err := newInstance("test-release", 100)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}

	p.Release()

	// Release 后 GetRunningNum 应返回 0
	if v := p.GetRunningNum(); v != 0 {
		t.Errorf("GetRunningNum() after Release = %d, want 0", v)
	}
}

func TestPoolInstance_BasicIsFull(t *testing.T) {
	p, err := newInstance("test-isfull", 2)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}
	defer p.Release()

	// 初始状态
	if p.IsFull() {
		t.Error("IsFull() should be false for empty pool")
	}
}

func TestPoolInstance_BasicStats(t *testing.T) {
	p, err := newInstance("test-stats", 100)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}
	defer p.Release()

	stats := p.Stats()
	if stats.Name != "test-stats" {
		t.Errorf("Stats().Name = %s, want test-stats", stats.Name)
	}
	if stats.Cap != 100 {
		t.Errorf("Stats().Cap = %d, want 100", stats.Cap)
	}
}
