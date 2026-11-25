package gohttp

import (
	"strings"
	"testing"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestHTTPClientMetricRegistered 指标进 Fast Registry（巡检可达）
func TestHTTPClientMetricRegistered(t *testing.T) {
	observeHTTPClient("GET", 200, time.Millisecond)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "http_client_duration_seconds") {
			found = true
			break
		}
	}
	if !found {
		t.Error("http_client 指标应进 Fast Registry")
	}
}

// TestObserveHTTPClient_Result 口径：status 派生 result（4xx/5xx/transport=error）。
// gohttp 是全仓唯一"result 由协议状态码派生"而非 err 直接映射的埋点，缺守护易回归。
func TestObserveHTTPClient_Result(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{"2xx-ok", 200, "ok"},
		{"3xx-ok", 302, "ok"},
		{"4xx-error", 404, "error"},
		{"5xx-error", 500, "error"},
		{"transport-failure", 0, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := durationCountByResult(t, "http_client_duration_seconds", tc.want)
			observeHTTPClient("GET", tc.status, time.Millisecond)
			after := durationCountByResult(t, "http_client_duration_seconds", tc.want)
			if after <= before {
				t.Errorf("status=%d 应记 result=%s, before=%d after=%d", tc.status, tc.want, before, after)
			}
		})
	}
}

// durationCountByResult 从 Fast Registry 取指定指标指定 result 的样本数。
func durationCountByResult(t *testing.T, name, result string) uint64 {
	t.Helper()
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	var total uint64
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		// 按 result 聚合全部样本（status 不同会分成多行，不能只取第一个）
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "result" && l.GetValue() == result {
					total += m.GetHistogram().GetSampleCount()
					break
				}
			}
		}
	}
	return total
}
