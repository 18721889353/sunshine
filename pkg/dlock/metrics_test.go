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
	observeLockAcquire("redis", nil, time.Millisecond)
	observeLockAcquire("etcd", errors.New("session expired"), time.Millisecond)
}
