package metrics

import "testing"

// TestComputeRED 三元组的计算与 scope 过滤。
func TestComputeRED(t *testing.T) {
	s := NewMetricSnapshot()
	s.Durations["api"] = mkAgg(100, 10, true) // 100 请求，10 错误
	s.Durations["other"] = mkAgg(50, 0, true)

	got := ComputeRED(s, "api", 10)
	if got.SampleCount != 100 {
		t.Errorf("样本数应 100, got %d", got.SampleCount)
	}
	if got.Rate != 10 { // 100/10s
		t.Errorf("QPS 应 10, got %v", got.Rate)
	}
	if got.Errors != 1 { // 10/10s
		t.Errorf("错误率每秒应 1, got %v", got.Errors)
	}
	if diff := got.ErrorRate - 0.1; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("错误率应 0.1, got %v", got.ErrorRate)
	}
	if got.Duration.P99 <= 0 {
		t.Errorf("P99 应从桶算出, got %v", got.Duration.P99)
	}
}

// TestComputeRED_Empty 空快照全零。
func TestComputeRED_Empty(t *testing.T) {
	got := ComputeRED(nil, "", 10)
	if got.Rate != 0 || got.ErrorRate != 0 || got.SampleCount != 0 {
		t.Errorf("空快照应全零, got %+v", got)
	}
}
