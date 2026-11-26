package dlock

import (
	"errors"
	"strings"
	"testing"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestLockMetricRegistered 指标进 Fast Registry（巡检可达）
func TestLockMetricRegistered(t *testing.T) {
	observeLockAcquire("redis", nil, time.Millisecond)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "lock_acquire_duration_seconds") {
			found = true
			break
		}
	}
	if !found {
		t.Error("lock 指标应进 Fast Registry")
	}
}

// TestObserveLockAcquire_Result 口径：nil=ok、错误=error
func TestObserveLockAcquire_Result(t *testing.T) {
	okBefore := readDurResult(t, "lock_acquire_duration_seconds", "ok")
	errBefore := readDurResult(t, "lock_acquire_duration_seconds", "error")

	observeLockAcquire("redis", nil, time.Millisecond)
	observeLockAcquire("etcd", errors.New("session expired"), time.Millisecond)

	okAfter := readDurResult(t, "lock_acquire_duration_seconds", "ok")
	errAfter := readDurResult(t, "lock_acquire_duration_seconds", "error")
	if okAfter-okBefore != 1 {
		t.Errorf("nil 应记 ok (+1), got %d", okAfter-okBefore)
	}
	if errAfter-errBefore != 1 {
		t.Errorf("错误应记 error (+1), got %d", errAfter-errBefore)
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
