package tracer

import (
	"math/rand"
	"sync/atomic"

	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// dynamicSampler 支持动态修改采样率的 sampler。
// 通过 atomic.Pointer 存储采样率，每次 IsSampled() 调用时读取最新值，
// 无需重建 TracerProvider，适用于 Nacos 配置热更新场景。
type dynamicSampler struct {
	samplingRate atomic.Pointer[float64]
}

// newDynamicSampler 创建动态 sampler，初始采样率为给定值。
func newDynamicSampler(rate float64) *dynamicSampler {
	r := clampRate(rate)
	s := &dynamicSampler{}
	s.samplingRate.Store(&r)
	return s
}

// SetSamplingRate 动态设置采样率（0~1）。
// 0 = 不采样（等效关闭追踪），1 = 全量采样。
func (s *dynamicSampler) SetSamplingRate(rate float64) {
	r := clampRate(rate)
	s.samplingRate.Store(&r)
}

// GetSamplingRate 获取当前采样率。
func (s *dynamicSampler) GetSamplingRate() float64 {
	if p := s.samplingRate.Load(); p != nil {
		return *p
	}
	return 1.0
}

// ShouldSample 实现 trace.Sampler 接口。
// 每次创建 Span 时调用，读取最新的采样率进行决策。
func (s *dynamicSampler) ShouldSample(params trace.SamplingParameters) trace.SamplingResult {
	rate := s.GetSamplingRate()

	// 采样率 <= 0，不采样
	if rate <= 0 {
		return trace.SamplingResult{Decision: trace.Drop}
	}

	// 采样率 >= 1，全量采样
	if rate >= 1 {
		return trace.SamplingResult{
			Decision:   trace.RecordAndSample,
			Tracestate: oteltrace.SpanContextFromContext(params.ParentContext).TraceState(),
		}
	}

	// 按比例采样
	if rand.Float64() < rate {
		return trace.SamplingResult{
			Decision:   trace.RecordAndSample,
			Tracestate: oteltrace.SpanContextFromContext(params.ParentContext).TraceState(),
		}
	}
	return trace.SamplingResult{Decision: trace.Drop}
}

// Description 返回 sampler 描述，实现 trace.Sampler 接口。
func (s *dynamicSampler) Description() string {
	return "DynamicRatioBasedSampler"
}

// clampRate 将采样率限制在 [0, 1] 范围内。
func clampRate(rate float64) float64 {
	if rate <= 0 {
		return 0
	}
	if rate >= 1 {
		return 1
	}
	return rate
}
