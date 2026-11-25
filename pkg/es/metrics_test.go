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

// TestObserveES_Result 口径
func TestObserveES_Result(t *testing.T) {
	observeES("bulk", nil, time.Millisecond)
	observeES("bulk", errors.New("timeout"), time.Millisecond)
}
