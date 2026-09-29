package tracer

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/sdk/trace"
)

// TestNewDynamicSamplerClampsInitialRate 验证 newDynamicSampler 对初始采样率做 [0,1] 钳位。
func TestNewDynamicSamplerClampsInitialRate(t *testing.T) {
	assert.Equal(t, float64(0), newDynamicSampler(-1).GetSamplingRate())
	assert.Equal(t, float64(1), newDynamicSampler(1.5).GetSamplingRate())
	assert.Equal(t, 0.5, newDynamicSampler(0.5).GetSamplingRate())
}

// TestDynamicSamplerRateClamp 验证 SetSamplingRate/GetSamplingRate 的钳位与读写一致性。
func TestDynamicSamplerRateClamp(t *testing.T) {
	s := newDynamicSampler(0)
	assert.Equal(t, float64(0), s.GetSamplingRate())

	s.SetSamplingRate(1.5) // 应被 clamp 到 1
	assert.Equal(t, float64(1), s.GetSamplingRate())

	s.SetSamplingRate(-1) // 应被 clamp 到 0
	assert.Equal(t, float64(0), s.GetSamplingRate())

	s.SetSamplingRate(0.5)
	assert.Equal(t, 0.5, s.GetSamplingRate())
}

// TestDynamicSamplerDescription 验证 Description 返回稳定标识，便于在采样器列表中辨认。
func TestDynamicSamplerDescription(t *testing.T) {
	s := newDynamicSampler(0.5)
	assert.Equal(t, "DynamicRatioBasedSampler", s.Description())
}

// TestDynamicSamplerConcurrentAccess 验证并发 SetSamplingRate/GetSamplingRate 无数据竞争。
func TestDynamicSamplerConcurrentAccess(t *testing.T) {
	s := newDynamicSampler(0.5)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.SetSamplingRate(float64(i%100) / 100)
			_ = s.GetSamplingRate()
		}(i)
	}
	wg.Wait()
}

// TestDynamicSamplerShouldSampleBoundary 验证采样率边界值的决策：0 全丢弃、1 全采集。
func TestDynamicSamplerShouldSampleBoundary(t *testing.T) {
	drop := newDynamicSampler(0).ShouldSample(trace.SamplingParameters{})
	assert.Equal(t, trace.Drop, drop.Decision)

	sample := newDynamicSampler(1).ShouldSample(trace.SamplingParameters{})
	assert.Equal(t, trace.RecordAndSample, sample.Decision)
}

// TestClampRateBounds 验证 clampRate 将任意输入映射到 [0,1]。
func TestClampRateBounds(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{"负数钳位到零", -1, 0},
		{"零保持为零", 0, 0},
		{"中间值原样保留", 0.5, 0.5},
		{"一保持为一", 1, 1},
		{"超过一钳位到一", 2, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, clampRate(tc.in))
		})
	}
}
