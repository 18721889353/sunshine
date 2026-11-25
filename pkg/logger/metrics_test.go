package logger

import (
	"testing"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestLoggerMetricAutoRegistered 回归守护：logger 指标必须由 init 自动注册。
// 历史 bug：原实现只暴露 RegisterPrometheus 手动调用，全仓无人调用 → 指标从未注册。
func TestLoggerMetricAutoRegistered(t *testing.T) {
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	need := []string{"logger_dropped_entries_total", "logger_router_active_loggers"}
	got := map[string]bool{}
	for _, f := range fams {
		got[f.GetName()] = true
	}
	for _, n := range need {
		if !got[n] {
			t.Errorf("%s 应由 init 自动注册（无需手动调用 RegisterPrometheus）", n)
		}
	}
}
