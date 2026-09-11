package gogroutine

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panjf2000/ants/v2"
)

// ============================================================================
// 测试 newAntsPool - 创建协程池
// ============================================================================

func TestNewAntsPool_Success(t *testing.T) {
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}
	defer p.Release()

	if p.Cap() != 100 {
		t.Errorf("Cap() = %d, want 100", p.Cap())
	}
}

func TestNewAntsPool_WithOpts(t *testing.T) {
	// 测试基本创建（不传 ants.Option）
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}
	defer p.Release()
}

// ============================================================================
// 测试 antsPool 方法 - Submit/Running/Waiting/Cap/Release/IsFull
// ============================================================================

func TestAntsPool_Submit(t *testing.T) {
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
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

func TestAntsPool_SubmitAfterRelease(t *testing.T) {
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}

	p.Release()

	if err := p.Submit(func() {}); err == nil {
		t.Error("Submit after Release should return error")
	}
}

func TestAntsPool_Running(t *testing.T) {
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}
	defer p.Release()

	// 初始状态
	if v := p.Running(); v != 0 {
		t.Errorf("Running() = %d, want 0", v)
	}

	// 提交阻塞任务
	startCh := make(chan struct{})
	blockCh := make(chan struct{})

	if err := p.Submit(func() {
		close(startCh)
		<-blockCh // 阻塞直到测试释放
	}); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	<-startCh
	time.Sleep(10 * time.Millisecond)

	if v := p.Running(); v != 1 {
		t.Errorf("Running() = %d, want 1", v)
	}

	close(blockCh)
	time.Sleep(10 * time.Millisecond)
}

func TestAntsPool_RunningAfterRelease(t *testing.T) {
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}

	p.Release()

	// Release 后 Running 应返回 0
	if v := p.Running(); v != 0 {
		t.Errorf("Running() after Release = %d, want 0", v)
	}
}

func TestAntsPool_Waiting(t *testing.T) {
	// 使用非阻塞模式避免 Submit 阻塞
	p, err := newAntsPool(1, ants.WithNonblocking(true))
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}
	defer p.Release()

	blockCh := make(chan struct{})

	// 第一个任务占满池
	if err := p.Submit(func() {
		<-blockCh
	}); err != nil {
		t.Fatalf("Submit 1 failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// 在非阻塞模式下，池满时 Submit 返回错误，无法直接测试 Waiting
	// 改为验证池状态
	if v := p.Running(); v != 1 {
		t.Errorf("Running() = %d, want 1", v)
	}

	close(blockCh)
}

func TestAntsPool_WaitingAfterRelease(t *testing.T) {
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}

	p.Release()

	if v := p.Waiting(); v != 0 {
		t.Errorf("Waiting() after Release = %d, want 0", v)
	}
}

func TestAntsPool_Cap(t *testing.T) {
	p, err := newAntsPool(200)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}
	defer p.Release()

	if v := p.Cap(); v != 200 {
		t.Errorf("Cap() = %d, want 200", v)
	}
}

func TestAntsPool_CapAfterRelease(t *testing.T) {
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}

	p.Release()

	if v := p.Cap(); v != 0 {
		t.Errorf("Cap() after Release = %d, want 0", v)
	}
}

func TestAntsPool_IsFull(t *testing.T) {
	p, err := newAntsPool(2) // 小池
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}
	defer p.Release()

	// 初始状态
	if p.IsFull() {
		t.Error("IsFull() should be false for empty pool")
	}

	// 占满池
	startCh := make(chan struct{})
	blockCh := make(chan struct{})

	for i := 0; i < 2; i++ {
		if err := p.Submit(func() {
			<-blockCh
		}); err != nil {
			t.Fatalf("Submit %d failed: %v", i, err)
		}
	}
	_ = startCh

	time.Sleep(10 * time.Millisecond)
	if !p.IsFull() {
		t.Error("IsFull() should be true when pool is full")
	}

	close(blockCh)
}

func TestAntsPool_Release(t *testing.T) {
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}

	// 释放一次
	p.Release()
	// 再次释放不 panic
	p.Release()
}

func TestAntsPool_ConcurrentSubmit(t *testing.T) {
	p, err := newAntsPool(100)
	if err != nil {
		t.Fatalf("newAntsPool failed: %v", err)
	}
	defer p.Release()

	var counter atomic.Int64
	var wg sync.WaitGroup

	// 并发提交 100 个任务
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Submit(func() {
				counter.Add(1)
			}); err != nil {
				t.Errorf("Submit failed: %v", err)
			}
		}()
	}

	wg.Wait()
	// 等待所有任务在池中执行完成
	time.Sleep(100 * time.Millisecond)

	if v := counter.Load(); v != 100 {
		t.Errorf("counter = %d, want 100", v)
	}
}
