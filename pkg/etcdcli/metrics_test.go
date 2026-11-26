package etcdcli

import (
	"strings"
	"testing"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestEtcdMetricRegistered 指标进 Fast Registry（巡检可达）
func TestEtcdMetricRegistered(t *testing.T) {
	observeEtcdTokenRefresh("ok")
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "etcd_token_refresh_total") {
			found = true
			break
		}
	}
	if !found {
		t.Error("etcd 指标应进 Fast Registry")
	}
}

// TestObserveEtcdTokenRefresh_Result 口径：ok/error 两个 result 各计数
func TestObserveEtcdTokenRefresh_Result(t *testing.T) {
	okBefore := readCounter(t, "etcd_token_refresh_total", "result=ok")
	errBefore := readCounter(t, "etcd_token_refresh_total", "result=error")

	observeEtcdTokenRefresh("ok")
	observeEtcdTokenRefresh("error")

	if got := readCounter(t, "etcd_token_refresh_total", "result=ok") - okBefore; got != 1 {
		t.Errorf("ok 应 +1, got %.0f", got)
	}
	if got := readCounter(t, "etcd_token_refresh_total", "result=error") - errBefore; got != 1 {
		t.Errorf("error 应 +1, got %.0f", got)
	}
}

// readCounter 从 Fast Registry 读 counter 当前值（labelKey 精确匹配）。
func readCounter(t *testing.T, metric, labelKey string) float64 {
	t.Helper()
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	for _, f := range fams {
		if f.GetName() != metric {
			continue
		}
		for _, m := range f.Metric {
			if m.Counter == nil {
				continue
			}
			key := ""
			for _, l := range m.Label {
				if key != "" {
					key += ","
				}
				key += l.GetName() + "=" + l.GetValue()
			}
			if key == labelKey {
				return m.Counter.GetValue()
			}
		}
	}
	return 0
}
