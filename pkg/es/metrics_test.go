package es

import (
	"errors"
	"strings"
	"testing"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestESMetricRegistered 指标进 Fast Registry（巡检可达）
func TestESMetricRegistered(t *testing.T) {
	observeES("bulk", nil, time.Millisecond)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "es_operation_duration_seconds") {
			found = true
			break
		}
	}
	if !found {
		t.Error("es 指标应进 Fast Registry")
	}
}

// TestObserveES_Result 口径：nil=ok、错误=error
func TestObserveES_Result(t *testing.T) {
	okBefore := readDurResult(t, "es_operation_duration_seconds", "ok")
	errBefore := readDurResult(t, "es_operation_duration_seconds", "error")

	observeES("bulk", nil, time.Millisecond)
	observeES("bulk", errors.New("timeout"), time.Millisecond)

	if got := readDurResult(t, "es_operation_duration_seconds", "ok") - okBefore; got != 1 {
		t.Errorf("nil 应记 ok (+1), got %d", got)
	}
	if got := readDurResult(t, "es_operation_duration_seconds", "error") - errBefore; got != 1 {
		t.Errorf("错误应记 error (+1), got %d", got)
	}
}

// readDurResult 从 Fast Registry 读 histogram 指定 Result 的样本数。
func readDurResult(t *testing.T, metric, result string) uint64 {
	t.Helper()
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	for _, f := range fams {
		if f.GetName() != metric || f.Type == nil || f.GetType().String() != "HISTOGRAM" {
			continue
		}
		var total uint64
		for _, m := range f.Metric {
			for _, l := range m.Label {
				if l.GetName() == "result" && l.GetValue() == result && m.Histogram != nil {
					total += m.Histogram.GetSampleCount()
				}
			}
		}
		return total
	}
	return 0
}
