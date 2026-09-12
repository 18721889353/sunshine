package gogroutine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// 测试多实例池管理器 - 基础功能
// ============================================================================

func TestNew_Success(t *testing.T) {
	pool := New("test-new", 100)
	defer pool.Release()

	if pool == nil {
		t.Fatal("New() returned nil")
	}
	if pool.Name() != "test-new" {
		t.Errorf("Name() = %s, want test-new", pool.Name())
	}
	if pool.GetCap() != 100 {
		t.Errorf("GetCap() = %d, want 100", pool.GetCap())
	}
}

func TestNew_WithOptions(t *testing.T) {
	pool := New("test-opts", 100,
		WithPreAlloc(true),
		WithDisablePurge(true),
	)
	defer pool.Release()

	if pool == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNewWithContext_Success(t *testing.T) {
	ctx := context.Background()
	pool, err := NewWithContext(ctx, "test-ctx", 100)
	if err != nil {
		t.Fatalf("NewWithContext() error = %v", err)
	}
	defer pool.Release()

	if pool == nil {
		t.Fatal("NewWithContext() returned nil")
	}
	if pool.Name() != "test-ctx" {
		t.Errorf("Name() = %s, want test-ctx", pool.Name())
	}
}

func TestGet_Success(t *testing.T) {
	pool := New("test-get", 100)
	defer pool.Release()

	got := Get("test-get")
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.Name() != "test-get" {
		t.Errorf("Name() = %s, want test-get", got.Name())
	}
}

func TestGet_NotFound(t *testing.T) {
	got := Get("nonexistent-pool")
	if got != nil {
		t.Error("Get() should return nil for nonexistent pool")
	}
}

func TestMustGet_Success(t *testing.T) {
	pool := New("test-must", 100)
	defer pool.Release()

	got := MustGet("test-must")
	if got == nil {
		t.Fatal("MustGet() returned nil")
	}
}

func TestMustGet_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustGet() should panic for nonexistent pool")
		}
	}()

	MustGet("nonexistent-pool")
}

// ============================================================================
// 测试多实例池管理器 - 边界条件
// ============================================================================

func TestNew_EmptyName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("New() with empty name should panic")
		}
	}()

	New("", 100)
}

func TestNewWithContext_EmptyName(t *testing.T) {
	_, err := NewWithContext(context.Background(), "", 100)
	if err == nil {
		t.Error("NewWithContext() with empty name should return error")
	}
}

func TestNew_DuplicateName(t *testing.T) {
	pool1 := New("test-dup", 100)
	defer pool1.Release()

	defer func() {
		if r := recover(); r == nil {
			t.Error("New() with duplicate name should panic")
		}
	}()

	New("test-dup", 100)
}

func TestNewWithContext_DuplicateName(t *testing.T) {
	pool1, _ := NewWithContext(context.Background(), "test-dup-ctx", 100)
	defer pool1.Release()

	_, err := NewWithContext(context.Background(), "test-dup-ctx", 100)
	if err == nil {
		t.Error("NewWithContext() with duplicate name should return error")
	}
}

func TestNew_ZeroCapacity(t *testing.T) {
	pool := New("test-zero", 0)
	defer pool.Release()

	// 应该使用最小池大小
	if pool.GetCap() < MinPoolSize {
		t.Errorf("GetCap() = %d, want >= %d", pool.GetCap(), MinPoolSize)
	}
}

func TestNew_NegativeCapacity(t *testing.T) {
	pool := New("test-neg", -100)
	defer pool.Release()

	if pool.GetCap() < MinPoolSize {
		t.Errorf("GetCap() = %d, want >= %d", pool.GetCap(), MinPoolSize)
	}
}

func TestNew_VeryLargeCapacity(t *testing.T) {
	pool := New("test-large", 100000)
	defer pool.Release()

	if pool.GetCap() > MaxPoolSize {
		t.Errorf("GetCap() = %d, want <= %d", pool.GetCap(), MaxPoolSize)
	}
}

// ============================================================================
// 测试多实例池管理器 - 生命周期管理
// ============================================================================

func TestReleasePool_Success(t *testing.T) {
	New("test-release", 100)
	ReleasePool("test-release")

	got := Get("test-release")
	if got != nil {
		t.Error("Get() should return nil after ReleasePool()")
	}
}

func TestReleasePool_NotFound(t *testing.T) {
	// 不存在的池应该静默处理
	ReleasePool("nonexistent-pool")
}

func TestReleasePool_DoubleRelease(t *testing.T) {
	New("test-double", 100)
	ReleasePool("test-double")
	ReleasePool("test-double") // 第二次释放应该静默处理
}

func TestDelete_Success(t *testing.T) {
	New("test-delete", 100)
	Delete("test-delete")

	got := Get("test-delete")
	if got != nil {
		t.Error("Get() should return nil after Delete()")
	}
}

func TestReleaseAllPools_Success(t *testing.T) {
	New("test-all-1", 100)
	New("test-all-2", 100)
	New("test-all-3", 100)

	ReleaseAllPools()

	if Count() != 0 {
		t.Errorf("Count() = %d, want 0", Count())
	}
}

// ============================================================================
// 测试多实例池管理器 - 并发安全
// ============================================================================

func TestConcurrentCreate(t *testing.T) {
	const n = 100
	var wg sync.WaitGroup
	done := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-done
			name := "concurrent-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
			pool := New(name, 10)
			defer pool.Release()
		}(i)
	}

	close(done)
	wg.Wait()
}

func TestConcurrentGet(t *testing.T) {
	New("test-concurrent-get", 100)
	defer ReleasePool("test-concurrent-get")

	const n = 100
	var wg sync.WaitGroup
	done := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-done
			pool := Get("test-concurrent-get")
			if pool == nil {
				t.Error("Get() returned nil in concurrent access")
			}
		}()
	}

	close(done)
	wg.Wait()
}

func TestConcurrentCreateAndRelease(t *testing.T) {
	const n = 100
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-cr-%d", i)
			pool := New(name, 10)
			time.Sleep(time.Millisecond)
			pool.Release()
		}(i)
	}

	wg.Wait()
}

// ============================================================================
// 测试多实例池管理器 - 查询功能
// ============================================================================

func TestListNames(t *testing.T) {
	New("test-list-1", 100)
	New("test-list-2", 100)
	defer ReleaseAllPools()

	names := ListNames()
	if len(names) < 2 {
		t.Errorf("ListNames() returned %d names, want >= 2", len(names))
	}
}

func TestCount(t *testing.T) {
	ReleaseAllPools()
	InitialCount := Count()

	New("test-count-1", 100)
	New("test-count-2", 100)
	defer ReleaseAllPools()

	if got := Count(); got < InitialCount+2 {
		t.Errorf("Count() = %d, want >= %d", got, InitialCount+2)
	}
}

func TestStats(t *testing.T) {
	ReleaseAllPools()
	New("test-stats-1", 100)
	New("test-stats-2", 200)
	defer ReleaseAllPools()

	stats := Stats()
	if stats.TotalPools < 2 {
		t.Errorf("Stats().TotalPools = %d, want >= 2", stats.TotalPools)
	}
	if len(stats.Pools) < 2 {
		t.Errorf("len(Stats().Pools) = %d, want >= 2", len(stats.Pools))
	}
}

// ============================================================================
// 测试 Pool 实例方法 - 边界条件
// ============================================================================

func TestPoolInstance_Go_NilTask(t *testing.T) {
	p := New("test-nil-go", 100)
	defer p.Release()

	// nil 任务不应该 panic
	p.Go(nil)
}

func TestPoolInstance_CtxGo_NilTask(t *testing.T) {
	p := New("test-nil-ctxgo", 100)
	defer p.Release()

	// nil 任务不应该 panic
	p.CtxGo(context.Background(), nil)
}

func TestPoolInstance_CtxGo_CancelledCtx(t *testing.T) {
	p := New("test-cancel-ctxgo", 100)
	defer p.Release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var executed atomic.Bool
	p.CtxGo(ctx, func() {
		executed.Store(true)
	})

	time.Sleep(50 * time.Millisecond)

	if executed.Load() {
		t.Error("task should not be executed with cancelled context")
	}
}

func TestPoolInstance_Submit_NilTask(t *testing.T) {
	p := New("test-nil-submit", 100)
	defer p.Release()

	err := p.Submit(nil)
	if err == nil {
		t.Error("Submit(nil) should return error")
	}
}

func TestPoolInstance_Submit_AfterRelease(t *testing.T) {
	p := New("test-submit-released", 100)
	p.Release()

	err := p.Submit(func() {})
	if err == nil {
		t.Error("Submit() after Release() should return error")
	}
}

func TestPoolInstance_Go_AfterRelease(t *testing.T) {
	p := New("test-go-released", 100)
	p.Release()

	// Go 在 Release 后应该静默处理（降级为原生 goroutine）
	var executed atomic.Bool
	p.Go(func() {
		executed.Store(true)
	})

	time.Sleep(50 * time.Millisecond)
	// 不验证 executed，因为 Release 后的行为取决于实现
}

func TestPoolInstance_SetCap(t *testing.T) {
	p := New("test-setcap", 100)
	defer p.Release()

	p.SetCap(200)
	if p.GetCap() != 200 {
		t.Errorf("GetCap() after SetCap(200) = %d, want 200", p.GetCap())
	}
}

func TestPoolInstance_IsFull(t *testing.T) {
	p := New("test-isfull", 2) // 小池
	defer p.Release()

	// 初始状态不应该是满的
	if p.IsFull() {
		t.Error("IsFull() should be false for empty pool")
	}
}

func TestPoolInstance_Stats(t *testing.T) {
	p := New("test-stats-method", 100)
	defer p.Release()

	stats := p.Stats()
	if stats.Name != "test-stats-method" {
		t.Errorf("Stats().Name = %s, want test-stats-method", stats.Name)
	}
	if stats.Cap != 100 {
		t.Errorf("Stats().Cap = %d, want 100", stats.Cap)
	}
}

func TestPoolInstance_SetPanicHandler(t *testing.T) {
	p := New("test-panic-handler", 100)
	defer p.Release()

	var panicCalled atomic.Bool
	p.SetPanicHandler(func(ctx context.Context, r interface{}) {
		panicCalled.Store(true)
	})

	done := make(chan struct{})
	p.Go(func() {
		defer close(done)
		panic("test panic")
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("task did not complete within timeout")
	}

	time.Sleep(100 * time.Millisecond)
	if !panicCalled.Load() {
		t.Error("custom panic handler was not called")
	}
}

// ============================================================================
// 测试 Pool 实例 - 任务执行
// ============================================================================

func TestPoolInstance_TaskExecution(t *testing.T) {
	p := New("test-exec", 100)
	defer p.Release()

	var counter atomic.Int32
	const n = 100

	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		p.Go(func() {
			defer wg.Done()
			counter.Add(1)
		})
	}

	wg.Wait()

	if v := counter.Load(); v != n {
		t.Errorf("counter = %d, want %d", v, n)
	}
}

func TestPoolInstance_PanicRecovery(t *testing.T) {
	p := New("test-panic-recovery", 100)
	defer p.Release()

	var recovered atomic.Bool
	p.SetPanicHandler(func(ctx context.Context, r interface{}) {
		recovered.Store(true)
	})

	done := make(chan struct{})
	p.Go(func() {
		defer close(done)
		panic("test panic")
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("task did not complete within timeout")
	}

	time.Sleep(100 * time.Millisecond)
	if !recovered.Load() {
		t.Error("panic was not recovered")
	}
}

func TestPoolInstance_ConcurrentSubmit(t *testing.T) {
	p := New("test-concurrent-submit", 100)
	defer p.Release()

	const n = 1000
	var counter atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		p.Go(func() {
			defer wg.Done()
			counter.Add(1)
		})
	}

	wg.Wait()

	if v := counter.Load(); v != n {
		t.Errorf("counter = %d, want %d", v, n)
	}
}

// ============================================================================
// 测试全局池与多实例池共存
// ============================================================================

func TestGlobalPool_CoexistWithMultiplePools(t *testing.T) {
	// 使用全局池
	var globalExecuted atomic.Bool
	Go(context.Background(), func() {
		globalExecuted.Store(true)
	})

	// 使用多实例池
	pool := New("coexist-pool", 100)
	defer pool.Release()

	var poolExecuted atomic.Bool
	pool.Go(func() {
		poolExecuted.Store(true)
	})

	time.Sleep(100 * time.Millisecond)

	if !globalExecuted.Load() {
		t.Error("global pool task was not executed")
	}
	if !poolExecuted.Load() {
		t.Error("multi-instance pool task was not executed")
	}
}

// ============================================================================
// 测试生产场景模拟
// ============================================================================

func TestProductionScenario_OrderProcessor(t *testing.T) {
	// 模拟订单处理场景
	pool := New("order-processor", 50)
	defer pool.Release()

	var processed atomic.Int32
	const orders = 100

	var wg sync.WaitGroup
	wg.Add(orders)

	for i := 0; i < orders; i++ {
		pool.Go(func() {
			defer wg.Done()
			time.Sleep(time.Millisecond) // 模拟处理
			processed.Add(1)
		})
	}

	wg.Wait()

	if v := processed.Load(); v != orders {
		t.Errorf("processed = %d, want %d", v, orders)
	}

	stats := pool.Stats()
	if stats.Success < int64(orders) {
		t.Errorf("Stats().Success = %d, want >= %d", stats.Success, orders)
	}
}

func TestProductionScenario_MultiService(t *testing.T) {
	// 模拟多服务场景
	orders := New("orders", 50)
	payments := New("payments", 30)
	notifications := New("notifications", 20)
	defer func() {
		orders.Release()
		payments.Release()
		notifications.Release()
	}()

	var orderCount, paymentCount, notifyCount atomic.Int32
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n * 3)

	for i := 0; i < n; i++ {
		orders.Go(func() {
			defer wg.Done()
			orderCount.Add(1)
		})
		payments.Go(func() {
			defer wg.Done()
			paymentCount.Add(1)
		})
		notifications.Go(func() {
			defer wg.Done()
			notifyCount.Add(1)
		})
	}

	wg.Wait()

	if v := orderCount.Load(); v != n {
		t.Errorf("orderCount = %d, want %d", v, n)
	}
	if v := paymentCount.Load(); v != n {
		t.Errorf("paymentCount = %d, want %d", v, n)
	}
	if v := notifyCount.Load(); v != n {
		t.Errorf("notifyCount = %d, want %d", v, n)
	}
}

// ============================================================================
// 测试边界情况 - 极端场景
// ============================================================================

func TestPoolInstance_Go_WithTimeout_ContextExpiry(t *testing.T) {
	p := New("test-timeout-expiry", 100)
	defer p.Release()

	// Context 超时后提交任务
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	time.Sleep(20*time.Millisecond) // 等待 context 过期

	var executed atomic.Bool
	p.CtxGo(ctx, func() {
		executed.Store(true)
	})

	time.Sleep(50 * time.Millisecond)
	if executed.Load() {
		t.Error("task should not be executed with expired context")
	}
}

func TestPoolInstance_Submit_WhilePoolIsDraining(t *testing.T) {
	p := New("test-draining", 5)
	defer p.Release()

	// 填满池并开始释放
	var wg sync.WaitGroup
	wg.Add(10)

	blockCh := make(chan struct{})
	for i := 0; i < 5; i++ {
		p.Go(func() {
			<-blockCh
			wg.Done()
		})
	}

	// 在池满时提交更多任务（应该降级）
	for i := 0; i < 5; i++ {
		p.Go(func() {
			wg.Done()
		})
	}

	close(blockCh)
	wg.Wait()
}

func TestPoolInstance_Stats_AfterRelease(t *testing.T) {
	p := New("test-stats-after-release", 100)
	p.Release()

	// Release 后获取 stats 不应该 panic
	stats := p.Stats()
	if stats.Name != "test-stats-after-release" {
		t.Errorf("Stats().Name = %s, want test-stats-after-release", stats.Name)
	}
}

func TestPoolInstance_GetCap_AfterSetCap(t *testing.T) {
	p := New("test-getcap-setcap", 100)
	defer p.Release()

	// 测试多次 SetCap
	p.SetCap(200)
	if p.GetCap() != 200 {
		t.Errorf("GetCap() after SetCap(200) = %d, want 200", p.GetCap())
	}

	p.SetCap(50)
	if p.GetCap() != 50 {
		t.Errorf("GetCap() after SetCap(50) = %d, want 50", p.GetCap())
	}
}

func TestPoolManager_ReleaseAllPools_Consistency(t *testing.T) {
	// 创建多个池
	New("release-all-1", 10)
	New("release-all-2", 20)
	New("release-all-3", 30)

	// 释放全部
	ReleaseAllPools()

	// 验证全部释放
	if Count() != 0 {
		t.Errorf("Count() after ReleaseAllPools = %d, want 0", Count())
	}

	// 获取应该返回 nil
	if pool := Get("release-all-1"); pool != nil {
		t.Error("Get() after ReleaseAllPools should return nil")
	}
}

func TestPoolInstance_CtxGo_MultipleContexts(t *testing.T) {
	p := New("test-multi-ctx", 100)
	defer p.Release()

	var counter atomic.Int32
	var wg sync.WaitGroup

	// 使用不同的 context 提交任务
	for i := 0; i < 10; i++ {
		wg.Add(1)
		ctx := context.WithValue(context.Background(), "key", i)
		p.CtxGo(ctx, func() {
			defer wg.Done()
			counter.Add(1)
		})
	}

	wg.Wait()

	if v := counter.Load(); v != 10 {
		t.Errorf("counter = %d, want %d", v, 10)
	}
}

func TestPoolInstance_SetCap_DuringExecution(t *testing.T) {
	p := New("test-setcap-during", 10)
	defer p.Release()

	var wg sync.WaitGroup
	wg.Add(20)

	// 并发执行任务和调整容量
	for i := 0; i < 10; i++ {
		p.Go(func() {
			time.Sleep(time.Millisecond)
			wg.Done()
		})
	}

	// 并发调整容量
	for i := 0; i < 10; i++ {
		p.SetCap(int32(5 + i))
		wg.Done()
	}

	wg.Wait()
}
