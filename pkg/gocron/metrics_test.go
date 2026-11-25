package gocron

import (
	"strings"
	"testing"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestCronMetricRegistered 指标进 Fast Registry（巡检可达）
func TestCronMetricRegistered(t *testing.T) {
	observeCronTask("t1", false, time.Millisecond)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "cron_task_duration_seconds") {
			found = true
			break
		}
	}
	if !found {
		t.Error("cron 指标应进 Fast Registry")
	}
}

// TestObserveCronTask_Result 口径：ok / error（panic 记 error）
func TestObserveCronTask_Result(t *testing.T) {
	observeCronTask("t1", false, time.Millisecond)
	observeCronTask("t1", true, time.Millisecond)
}
