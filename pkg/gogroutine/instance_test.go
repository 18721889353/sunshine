package gogroutine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// 本文件对应 instance.go（池实例实现）的一对一测试映射。

// memorySpanExporter 内存 Span 导出器。
// 不用 otel 的 tracertest 子模块：其不在本模块依赖图中（go list -m all 无该模块，
// import 报 no required module provides package），sdktrace 包本身可用，故自带实现。
type memorySpanExporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

// ExportSpans 简单内存追加（SimpleSpanProcessor 同步调用，无需批量语义）。
func (e *memorySpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	e.spans = append(e.spans, spans...)
	e.mu.Unlock()
	return nil
}

// Shutdown 无资源可释放。
func (e *memorySpanExporter) Shutdown(context.Context) error { return nil }

// names 返回当前已导出的 Span 名快照。
func (e *memorySpanExporter) names() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	names := make([]string, 0, len(e.spans))
	for _, s := range e.spans {
		names = append(names, s.Name())
	}
	return names
}

// newTestInstance 创建池实例并在失败时终止测试，注册 Release 清理。
func newTestInstance(t *testing.T, name string, capacity int, opts ...Option) *poolInstance {
	t.Helper()
	p, err := newInstance(name, capacity, opts...)
	if err != nil {
		t.Fatalf("newInstance(%s) failed: %v", name, err)
	}
	t.Cleanup(p.Release)
	return p
}

// TestPoolInstanceSubmit 验证 Submit 提交的任务被真实执行。
func TestPoolInstanceSubmit(t *testing.T) {
	p := newTestInstance(t, "test-submit", 100)

	done := make(chan struct{})
	if err := p.Submit(func() {
		close(done)
	}); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	waitDone(t, done, "Submit 任务未执行")
}

// TestPoolInstanceGo 验证 Go 提交的任务被真实执行。
func TestPoolInstanceGo(t *testing.T) {
	p := newTestInstance(t, "test-go", 100)

	var executed atomic.Bool
	done := make(chan struct{})
	p.Go(func() {
		executed.Store(true)
		close(done)
	})
	waitDone(t, done, "Go 任务未执行")

	if !executed.Load() {
		t.Error("task was not executed")
	}
}

// TestPoolInstanceCtxGo 验证 CtxGo 提交的任务被真实执行。
func TestPoolInstanceCtxGo(t *testing.T) {
	p := newTestInstance(t, "test-ctxgo", 100)

	var executed atomic.Bool
	done := make(chan struct{})
	p.CtxGo(t.Context(), func() {
		executed.Store(true)
		close(done)
	})
	waitDone(t, done, "CtxGo 任务未执行")

	if !executed.Load() {
		t.Error("task was not executed")
	}
}

// TestPoolInstanceCap 验证 GetCap 返回构造时的容量。
func TestPoolInstanceCap(t *testing.T) {
	p := newTestInstance(t, "test-cap", 200)

	if v := p.GetCap(); v != 200 {
		t.Errorf("GetCap() = %d, want 200", v)
	}
}

// TestPoolInstanceName 验证 Name 返回构造时的池名。
func TestPoolInstanceName(t *testing.T) {
	p := newTestInstance(t, "test-name", 100)

	if v := p.Name(); v != "test-name" {
		t.Errorf("Name() = %s, want test-name", v)
	}
}

// TestPoolInstanceRelease 验证 Release 后运行中任务数归零。
func TestPoolInstanceRelease(t *testing.T) {
	p := newTestInstance(t, "test-release", 100)
	p.Release()

	if v := p.GetRunningNum(); v != 0 {
		t.Errorf("GetRunningNum() after Release = %d, want 0", v)
	}
}

// TestPoolInstanceBasicIsFull 验证空池不判满。
func TestPoolInstanceBasicIsFull(t *testing.T) {
	p := newTestInstance(t, "test-isfull", 2)

	if p.IsFull() {
		t.Error("IsFull() should be false for empty pool")
	}
}

// TestPoolInstanceBasicStats 验证 Stats 携带名称与容量。
func TestPoolInstanceBasicStats(t *testing.T) {
	p := newTestInstance(t, "test-stats", 100)

	stats := p.Stats()
	if stats.Name != "test-stats" {
		t.Errorf("Stats().Name = %s, want test-stats", stats.Name)
	}
	if stats.Cap != 100 {
		t.Errorf("Stats().Cap = %d, want 100", stats.Cap)
	}
}

// TestPoolInstanceNilCtxFallsBackToBackground 验证实例级 CtxGo 的 nil ctx 防御
// （原实现在 select <-ctx.Done() 上对 nil 接口直接 panic）。
func TestPoolInstanceNilCtxFallsBackToBackground(t *testing.T) {
	p := newTestInstance(t, "test-nil-ctx", 10)

	done := make(chan struct{})
	p.CtxGo(nil, func() { close(done) }) // 故意传 nil
	waitDone(t, done, "nil ctx 下 CtxGo 任务未执行")
}

// TestPoolInstanceReleaseIdempotent 验证重复 Release 幂等且后续查询不 panic。
func TestPoolInstanceReleaseIdempotent(t *testing.T) {
	p := newTestInstance(t, "test-release-twice", 10)

	p.Release()
	p.Release() // 第二次不应 panic

	if v := p.GetWaitingNum(); v != 0 {
		t.Errorf("GetWaitingNum() after Release = %d, want 0", v)
	}
	if p.IsFull() {
		t.Error("已释放的池不应判满")
	}
	if v := p.GetCap(); v != 10 {
		t.Errorf("GetCap() after Release = %d, want 10（容量记录保留）", v)
	}
}

// TestPoolInstanceSetCapAfterRelease 验证池释放后 SetCap 只更新记录、不 panic。
func TestPoolInstanceSetCapAfterRelease(t *testing.T) {
	p := newTestInstance(t, "test-setcap-released", 10)
	p.Release()

	p.SetCap(20) // 底层池已为 nil，不应进入 ants.Tune

	if v := p.GetCap(); v != 20 {
		t.Errorf("GetCap() = %d, want 20", v)
	}
}

// ============================================================================
// 测试统一执行管线的可观测性（评审 R2-P1-3）
// ============================================================================

// TestPoolInstanceTaskEmitsMetrics 验证实例池任务同样触发 SetMetrics 注入的采集器
// （IncRunning/DecRunning/ObserveTaskDuration），与全局池口径一致——
// 原双实现下实例池完全不触达该接口。
func TestPoolInstanceTaskEmitsMetrics(t *testing.T) {
	resetForTest()
	mm := &mockMetrics{}
	SetMetrics(mm)
	defer SetMetrics(nil)

	p := newTestInstance(t, "metrics-obs", 20)

	started := make(chan struct{})
	release := make(chan struct{})
	p.CtxGo(context.Background(), func() {
		close(started)
		<-release
	})
	<-started
	// 任务体已进入：IncRunning 必然早于任务体发生，此处直读是安全的
	if v := mm.running.Load(); v != 1 {
		t.Errorf("实例任务执行中 running = %d, want 1（IncRunning 未触发）", v)
	}
	close(release)
	waitUntil(t, func() bool {
		return mm.running.Load() == 0 && mm.durations.Load() == 1
	}, "实例任务未完成 Metrics 指标回收")
}

// TestPoolInstancePanicCountsScopedToInstance 验证实例池 panic 计入实例计数与
// Metrics 接口，但不写全局 metricsMgr——PoolStats 的 Panic 口径保持全局池专属。
func TestPoolInstancePanicCountsScopedToInstance(t *testing.T) {
	resetForTest()
	mm := &mockMetrics{}
	SetMetrics(mm)
	defer SetMetrics(nil)

	p := newTestInstance(t, "panic-scope", 20)
	done := make(chan struct{})
	p.CtxGo(context.Background(), func() {
		defer close(done)
		panic("instance panic for scope test")
	})
	<-done

	waitUntil(t, func() bool { return p.metrics.panicCount.Load() == 1 }, "实例 panic 未计数")
	if v := metricsMgr.panicCount.Load(); v != 0 {
		t.Errorf("metricsMgr.panicCount = %d, want 0（实例 panic 不应污染全局 PoolStats 口径）", v)
	}
	waitUntil(t, func() bool { return mm.panicCount.Load() == 1 }, "Metrics 接口未收到实例 panic")
}

// TestPoolInstanceFallbackEmitsMetrics 验证实例池降级路径同样回调 IncFallback
// （原实现降级只有日志，不触达注入的采集器）。
func TestPoolInstanceFallbackEmitsMetrics(t *testing.T) {
	resetForTest()
	mm := &mockMetrics{}
	SetMetrics(mm)
	defer SetMetrics(nil)

	p := newTestInstance(t, "fallback-obs", 20)
	p.Release() // 释放后 CtxGo 走降级路径

	done := make(chan struct{})
	p.CtxGo(context.Background(), func() { close(done) })
	<-done
	waitUntil(t, func() bool { return mm.fallback.Load() == 1 }, "实例降级未触达 Metrics 接口")
}

// TestPoolInstanceTaskCreatesSpan 验证实例池任务创建 OpenTelemetry Span
// （gogroutine.task.{池名}），与全局池同走 observeTask 管线。
// 方法：临时替换全局 TracerProvider 为内存导出器，按 Span 名轮询匹配——
// span.End 是管线首个 defer（最后执行），close(done) 时可能尚未导出，故轮询而非直读。
func TestPoolInstanceTaskCreatesSpan(t *testing.T) {
	exporter := &memorySpanExporter{}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	}()

	p := newTestInstance(t, "span-obs", 20)
	done := make(chan struct{})
	p.CtxGo(context.Background(), func() { close(done) })
	<-done

	want := "gogroutine.task.span-obs"
	waitUntil(t, func() bool {
		for _, n := range exporter.names() {
			if n == want {
				return true
			}
		}
		return false
	}, "实例池任务未产出 Span")
}

// TestPoolInstanceConcurrentReleaseAndSubmit 并发提交与释放互斥守护。
// 原实现 p.pool 无锁读、Release 无锁置 nil，二者并发构成数据竞争
// （本机 Windows/MinGW 无法跑 -race 取证，见 README「竞态检测」诚实声明）。
func TestPoolInstanceConcurrentReleaseAndSubmit(t *testing.T) {
	p, err := newInstance("test-concurrent-release", 8)
	if err != nil {
		t.Fatalf("newInstance failed: %v", err)
	}

	var executed atomic.Int64
	var wg sync.WaitGroup
	wg.Add(3)

	go func() { // 提交方
		defer wg.Done()
		for i := 0; i < 200; i++ {
			p.CtxGo(context.Background(), func() {
				executed.Add(1)
				time.Sleep(overlapPad)
			})
		}
	}()
	go func() { // 释放方
		defer wg.Done()
		time.Sleep(overlapPad)
		p.Release()
	}()
	go func() { // 容量调整方（写 p.capacity 的另一路径）
		defer wg.Done()
		for i := 0; i < 200; i++ {
			p.SetCap(int32(4 + i%16))
			p.GetCap()
		}
	}()

	wg.Wait()

	// 释放后再查询不应 panic，容量为最后一次 SetCap 的值
	if v := p.GetCap(); v <= 0 {
		t.Errorf("GetCap() = %d, want > 0", v)
	}
	// 至少应有部分任务被受理（提交先于释放到达的部分）
	if v := executed.Load(); v == 0 {
		t.Error("released-before-any-submit: 至少应有任务被执行")
	}
}
