package gogroutine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
)

// 本文件是 gogroutine 的性能基线（Benchmark）。
//
// 运行方式：
//
//	go test -bench=. -benchmem -run='^$' ./pkg/gogroutine/
//
// 口径说明：所有用例都是**纯本包路径**（提交/执行/构造/聚合），不发起任何网络请求、
// 不含回环 RTT，测得的是「代码改动是否让热路径变慢」的回归基线，不是绝对吞吐数字。
// 全局 otel TracerProvider 在测试进程里是 noop，Span 成本包含在读数内但无导出开销。

// silenceBenchLogs 把全局 logger 提到 error 级别并停写文件，供基准用例静音。
// 为何静音：SubmitTaskHotPath 的 Init 与 CancelledCtx 的跳过路径会打 INFO/WARN，
// 日志 IO 与 benchmark 输出行交错会切碎读数（未静音时
// BenchmarkSubmitTaskCancelledCtx 的 ns/op 行会被 WARN 日志打散，无法采集）；
// 为何安全：本包仅输出 INFO/WARN 两级，error 级别恰好全部过滤；
// 静音只发生在 benchmark 阶段（testing 包先跑全部 Test 再跑 Benchmark），不影响任何测试的日志断言；
// 进程结束即失效，不产生跨进程副作用。
func silenceBenchLogs(b *testing.B) {
	b.Helper()
	if _, err := logger.Init(logger.WithLevel("error"), logger.WithSave(false), logger.WithFormat("console")); err != nil {
		b.Fatalf("静音日志失败: %v", err)
	}
}

// BenchmarkSubmitTaskHotPath 测量全局池提交热路径（Go/GoWithName 每次调用的固定成本：
// nil 判断 + ctx 归一 + shouldSkipSubmit + ants.Submit + 执行器入队）。
func BenchmarkSubmitTaskHotPath(b *testing.B) {
	silenceBenchLogs(b)
	resetForTest()
	b.Cleanup(resetForTest)
	Init(WithPoolSize(1024))
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		submitTask(ctx, "bench", func() {})
	}
	b.StopTimer()
	ReleaseAndWaitWithTimeout(time.Second)
}

// BenchmarkSubmitTaskCancelledCtx 测量「ctx 已取消即跳过」路径。
// 批量任务被取消时走这条短路：不入队、不分配执行闭包，应显著低于热路径分配。
func BenchmarkSubmitTaskCancelledCtx(b *testing.B) {
	silenceBenchLogs(b)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	defer cancel()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if submitTask(ctx, "bench", func() {}) {
			b.Fatal("已取消 ctx 不应受理任务")
		}
	}
}

// BenchmarkExecuteTask 测量执行器开销（Span 创建 + 指标 + panic 恢复兜底 + 耗时观测）。
// 这是每个任务必经的一层，读数反映观测能力的固定成本。
func BenchmarkExecuteTask(b *testing.B) {
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executeTask(ctx, "bench", func() {})
	}
}

// BenchmarkPoolInstanceCtxGo 测量实例级池提交（含 RLock 快照 + metrics 计数）。
// 与 BenchmarkSubmitTaskHotPath 的差值即实例路径相对全局路径的额外开销。
func BenchmarkPoolInstanceCtxGo(b *testing.B) {
	p, err := newInstance("bench-instance", 1024, WithPreAlloc(true), WithDisablePurge(true))
	if err != nil {
		b.Fatalf("newInstance failed: %v", err)
	}
	b.Cleanup(p.Release)

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.CtxGo(ctx, func() {})
	}
	b.StopTimer()
	p.Release()
}

// BenchmarkPoolConfigApply 测量「默认配置 + Option 归并」开销（Init/New 的第一步）。
// 构造期只执行一次，读数用于发现配置装配是否引入意外分配。
func BenchmarkPoolConfigApply(b *testing.B) {
	opts := []Option{
		WithPoolSize(500),
		WithPreAlloc(true),
		WithDisablePurge(true),
		WithGracefulShutdown(false),
		WithGracefulShutdownTimeout(10 * time.Second),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg := defaultPoolConfig()
		cfg.apply(opts...)
		if cfg.PoolSize != 500 {
			b.Fatalf("PoolSize = %d, want 500", cfg.PoolSize)
		}
	}
}

// BenchmarkNewInstance 测量池实例完整构造开销（ants.NewPool + 预分配 worker 起点）。
// 仅启动期执行；读数用于观察容量配置变化对启动成本的影响。
func BenchmarkNewInstance(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := newInstance("bench-new", 1000, WithPreAlloc(true), WithDisablePurge(true))
		if err != nil {
			b.Fatalf("newInstance failed: %v", err)
		}
		p.Release()
	}
}

// BenchmarkCollectBatchErrors 测量批量错误聚合开销（GoBatchWithResult 的收尾路径）。
func BenchmarkCollectBatchErrors(b *testing.B) {
	errs := make([]error, 64)
	for i := 0; i < len(errs); i += 2 {
		errs[i] = errors.New("bench error")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if collectBatchErrors(errs) == nil {
			b.Fatal("含错误的切片不应聚合为 nil")
		}
	}
}
