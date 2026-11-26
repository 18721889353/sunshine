package goredis

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestRedisOperationMetricRegistered 指标必须进 Fast Registry（巡检可达）。
// 注意：Prometheus 的 HistogramVec 在未 observe 前无样本，Gather 不返回，
// 故先触发一次 observe 再采集。
func TestRedisOperationMetricRegistered(t *testing.T) {
	observeRedisOperation("get", nil, time.Millisecond)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "redis_operation_duration_seconds") {
			found = true
			break
		}
	}
	if !found {
		t.Error("redis 指标应进 Fast Registry（否则 5s 巡检采不到）")
	}
}

// TestObserveRedisOperation_Result 口径：redis.Nil 缓存 miss 计 ok，真错误计 error
func TestObserveRedisOperation_Result(t *testing.T) {
	okBefore := readDurResult(t, "redis_operation_duration_seconds", "ok")
	errBefore := readDurResult(t, "redis_operation_duration_seconds", "error")

	// 正常成功 → ok
	observeRedisOperation("get", nil, time.Millisecond)
	// 缓存 miss（redis.Nil）应算 ok（生产 miss 率高，计 error 会打满错误率）
	observeRedisOperation("get", redis.Nil, time.Millisecond)
	// 真错误算 error
	observeRedisOperation("set", errors.New("connection refused"), time.Millisecond)

	if got := readDurResult(t, "redis_operation_duration_seconds", "ok") - okBefore; got != 2 {
		t.Errorf("成功+miss 应记 ok (+2), got %d", got)
	}
	if got := readDurResult(t, "redis_operation_duration_seconds", "error") - errBefore; got != 1 {
		t.Errorf("真错误应记 error (+1), got %d", got)
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
