package tracer

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
)

// 基准口径：全部基于包内 mock / 内存操作，不含任何网络 RTT，定位是回归基线（性能劣化对比），
// 不代表生产上报耗时。运行方式（默认 benchtime 即可，如需更稳读数可显式指定）：
//
//	go test -bench=. -benchmem -run=^$ ./pkg/tracer/
//	go test -bench=BenchmarkNewSpan -benchtime=2s -run=^$ ./pkg/tracer/

// BenchmarkClampRate 基准测试 clampRate 钳位开销。
func BenchmarkClampRate(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = clampRate(0.5)
	}
}

// BenchmarkDynamicSamplerShouldSample 基准测试采样决策开销（比例采样分支）。
func BenchmarkDynamicSamplerShouldSample(b *testing.B) {
	s := newDynamicSampler(0.5)
	params := trace.SamplingParameters{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.ShouldSample(params)
	}
}

// BenchmarkGetSamplingRate 基准测试采样率读取（atomic 路径）。
func BenchmarkGetSamplingRate(b *testing.B) {
	s := newDynamicSampler(0.5)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.GetSamplingRate()
	}
}

// BenchmarkSetSamplingRate 基准测试采样率热更新（atomic 路径）。
func BenchmarkSetSamplingRate(b *testing.B) {
	s := newDynamicSampler(0.5)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.SetSamplingRate(0.5)
	}
}

// BenchmarkNewSpanWithTags 基准测试带标签的 Span 创建开销（不含导出，导出由 BatchSpanProcessor 异步完成）。
func BenchmarkNewSpanWithTags(b *testing.B) {
	tags := map[string]interface{}{
		"service": "bench",
		"count":   1,
		"latency": 1.5,
		"ok":      true,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, span := NewSpan(context.Background(), "benchSpan", tags)
		span.End()
	}
}

// BenchmarkNewSpanNoTags 基准测试无标签的 Span 创建开销。
func BenchmarkNewSpanNoTags(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, span := NewSpan(context.Background(), "benchSpan", nil)
		span.End()
	}
}

// BenchmarkNewSpanParallel 高并发打点基准（b.RunParallel）：完整热路径
// （采样决策 + Span 录制 + BatchSpanProcessor 入队，含丢弃路径），
// 验证并发下无锁竞争放大与分配暴涨（allocs/op 应与串行口径同量级）。
func BenchmarkNewSpanParallel(b *testing.B) {
	b.Cleanup(func() {
		if err := Close(context.Background()); err != nil {
			b.Logf("cleanup 阶段 Close 失败: %v", err)
		}
	})
	if err := Init(&mockExporter{}, NewResource(), 1.0); err != nil {
		b.Fatalf("Init 失败: %v", err)
	}
	tags := map[string]interface{}{
		"service": "bench",
		"count":   1,
		"latency": 1.5,
		"ok":      true,
	}
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := NewSpan(context.Background(), "benchSpan", tags)
			span.End()
		}
	})
}

// BenchmarkNewSpanParallelChild 高并发子 Span 基准：ctx 携带父 Span（贴近生产
// 「上游 ctx 逐层传递」用法），覆盖 ParentBased 采样器的「继承父级决策」分支。
// 口径限定于注册全局模式（NewSpan 走 otel.Tracer 全局查找）；
// WithGlobalRegistration(false) 模式下业务直接用 Provider().Tracer(name)，不经 NewSpan，无对应基准。
func BenchmarkNewSpanParallelChild(b *testing.B) {
	b.Cleanup(func() {
		if err := Close(context.Background()); err != nil {
			b.Logf("cleanup 阶段 Close 失败: %v", err)
		}
	})
	if err := Init(&mockExporter{}, NewResource(), 1.0); err != nil {
		b.Fatalf("Init 失败: %v", err)
	}
	tags := map[string]interface{}{
		"service": "bench",
		"count":   1,
		"latency": 1.5,
		"ok":      true,
	}
	rootCtx, rootSpan := NewSpan(context.Background(), "benchRoot", nil)
	defer rootSpan.End()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := NewSpan(rootCtx, "benchSpan", tags)
			span.End()
		}
	})
}

// BenchmarkInitReplaceProvider 基准测试 TracerProvider 原子替换路径
// （含旧 provider 关闭，mock exporter 无网络开销）。
func BenchmarkInitReplaceProvider(b *testing.B) {
	b.Cleanup(func() {
		if err := Close(context.Background()); err != nil {
			b.Logf("cleanup 阶段 Close 失败: %v", err)
		}
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Init(&mockExporter{}, NewResource(), 1.0); err != nil {
			b.Fatalf("Init 失败: %v", err)
		}
	}
}
