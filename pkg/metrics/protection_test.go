package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestObserveSentinelBlock 限流/熔断拦截计数：type 区分、result=error 进诊断。
func TestObserveSentinelBlock(t *testing.T) {
	beforeFlow := testutil.ToFloat64(sentinelBlock.WithLabelValues("flow", "error"))
	beforeCB := testutil.ToFloat64(sentinelBlock.WithLabelValues("circuit_breaking", "error"))

	ObserveSentinelBlock("flow")
	ObserveSentinelBlock("circuit_breaking")

	if got := testutil.ToFloat64(sentinelBlock.WithLabelValues("flow", "error")); got != beforeFlow+1 {
		t.Errorf("flow counter 应 +1, got %v", got)
	}
	if got := testutil.ToFloat64(sentinelBlock.WithLabelValues("circuit_breaking", "error")); got != beforeCB+1 {
		t.Errorf("circuit_breaking counter 应 +1, got %v", got)
	}
}
