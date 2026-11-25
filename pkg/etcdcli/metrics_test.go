package etcdcli

import (
	"strings"
	"testing"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestEtcdMetricRegistered 指标进 Fast Registry（巡检可达）
func TestEtcdMetricRegistered(t *testing.T) {
	observeEtcdTokenRefreshResult("ok")
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

// TestObserveEtcdTokenRefresh_Result 口径
func TestObserveEtcdTokenRefresh_Result(t *testing.T) {
	observeEtcdTokenRefreshResult("ok")
	observeEtcdTokenRefreshResult("error")
}
