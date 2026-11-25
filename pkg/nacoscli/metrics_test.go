package nacoscli

import (
	"errors"
	"strings"
	"testing"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestNacosMetricRegistered 指标进 Fast Registry（巡检可达）
func TestNacosMetricRegistered(t *testing.T) {
	observeNacosConfigFetch(nil, time.Millisecond)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "nacos_config_fetch_duration_seconds") {
			found = true
			break
		}
	}
	if !found {
		t.Error("nacos 指标应进 Fast Registry")
	}
}

// TestObserveNacosConfigFetch_Result 口径
func TestObserveNacosConfigFetch_Result(t *testing.T) {
	observeNacosConfigFetch(nil, time.Millisecond)
	observeNacosConfigFetch(errors.New("config not found"), time.Millisecond)
}

// TestNacosCallbackMetric 回调指标进 Fast Registry + panic 记 error
func TestNacosCallbackMetric(t *testing.T) {
	observeNacosConfigCallback(false)
	observeNacosConfigCallback(true)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "nacos_config_callback_total") {
			found = true
			break
		}
	}
	if !found {
		t.Error("nacos callback 指标应进 Fast Registry")
	}
}
