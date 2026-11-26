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
	okBefore := readDurResult(t, "cron_task_duration_seconds", "ok")
	errBefore := readDurResult(t, "cron_task_duration_seconds", "error")

	observeCronTask("t1", false, time.Millisecond) // 正常 → ok
	observeCronTask("t1", true, time.Millisecond)  // panic → error

	if got := readDurResult(t, "cron_task_duration_seconds", "ok") - okBefore; got != 1 {
		t.Errorf("正常任务应记 ok (+1), got %d", got)
	}
	if got := readDurResult(t, "cron_task_duration_seconds", "error") - errBefore; got != 1 {
		t.Errorf("panic 任务应记 error (+1), got %d", got)
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
