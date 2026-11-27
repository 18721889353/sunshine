package metrics

import (
	"strings"
	"testing"
)

// TestRenderDurationLine_UsesRED 渲染口径与 ComputeRED 一致（QPS/错误率/P99 同源）。
func TestRenderDurationLine_UsesRED(t *testing.T) {
	agg := mkAgg(100, 10, true)
	line := renderDurationLine("api", agg, 10, nil)

	if !strings.Contains(line, "QPS 10.0") {
		t.Errorf("QPS 口径错误: %s", line)
	}
	if !strings.Contains(line, "错误 10.00%") {
		t.Errorf("错误率口径错误: %s", line)
	}
	if !strings.Contains(line, "P99") {
		t.Errorf("应有 P99: %s", line)
	}

	// 与 ComputeRED 对象交叉验证
	one := NewMetricSnapshot()
	one.Durations["api"] = agg
	red := ComputeRED(one, "api", 10)
	if red.Rate != 10 || red.ErrorRate != 0.1 || red.Duration.P99 <= 0 {
		t.Errorf("RED 对象计算错误: %+v", red)
	}
}
