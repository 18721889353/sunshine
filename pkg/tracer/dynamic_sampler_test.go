package tracer

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDynamicSampler_Rate(t *testing.T) {
	s := newDynamicSampler(0)
	assert.Equal(t, float64(0), s.GetSamplingRate())

	s.SetSamplingRate(1.5) // 应被 clamp 到 1
	assert.Equal(t, float64(1), s.GetSamplingRate())

	s.SetSamplingRate(-1) // 应被 clamp 到 0
	assert.Equal(t, float64(0), s.GetSamplingRate())

	s.SetSamplingRate(0.5)
	assert.Equal(t, float64(0.5), s.GetSamplingRate())
}

func TestDynamicSampler_Description(t *testing.T) {
	s := newDynamicSampler(0.5)
	assert.Equal(t, "DynamicRatioBasedSampler", s.Description())
}

func TestDynamicSampler_Concurrent(t *testing.T) {
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

func TestClampRate(t *testing.T) {
	assert.Equal(t, float64(0), clampRate(-1))
	assert.Equal(t, float64(0), clampRate(0))
	assert.Equal(t, float64(0.5), clampRate(0.5))
	assert.Equal(t, float64(1), clampRate(1))
	assert.Equal(t, float64(1), clampRate(2))
}
