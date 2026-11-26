package gorabbitmq

import (
	"errors"
	"strings"
	"testing"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestRabbitmqMetricRegistered 指标进 Fast Registry（巡检可达）
func TestRabbitmqMetricRegistered(t *testing.T) {
	observeRabbitmqPublish("ex", nil, time.Millisecond)
	observeRabbitmqConsume("q", nil, time.Millisecond)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	pub, cons := false, false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "rabbitmq_publish_duration_seconds") {
			pub = true
		}
		if strings.Contains(f.GetName(), "rabbitmq_consume_duration_seconds") {
			cons = true
		}
	}
	if !pub || !cons {
		t.Errorf("rabbitmq 指标应进 Fast Registry (pub=%v cons=%v)", pub, cons)
	}
}

// TestObserveRabbitmq_Result 口径
func TestObserveRabbitmq_Result(t *testing.T) {
	pubOk := readDurResult(t, "rabbitmq_publish_duration_seconds", "ok")
	pubErr := readDurResult(t, "rabbitmq_publish_duration_seconds", "error")
	conOk := readDurResult(t, "rabbitmq_consume_duration_seconds", "ok")
	conErr := readDurResult(t, "rabbitmq_consume_duration_seconds", "error")

	observeRabbitmqPublish("ex", nil, time.Millisecond)
	observeRabbitmqPublish("ex", errors.New("conn closed"), time.Millisecond)
	observeRabbitmqConsume("q", nil, time.Millisecond)
	observeRabbitmqConsume("q", errors.New("handler failed"), time.Millisecond)

	checks := []struct {
		name   string
		before uint64
		after  uint64
		want   uint64
	}{
		{"publish ok", pubOk, readDurResult(t, "rabbitmq_publish_duration_seconds", "ok"), 1},
		{"publish error", pubErr, readDurResult(t, "rabbitmq_publish_duration_seconds", "error"), 1},
		{"consume ok", conOk, readDurResult(t, "rabbitmq_consume_duration_seconds", "ok"), 1},
		{"consume error", conErr, readDurResult(t, "rabbitmq_consume_duration_seconds", "error"), 1},
	}
	for _, c := range checks {
		if got := c.after - c.before; got != c.want {
			t.Errorf("%s 应 +%d, got %d", c.name, c.want, got)
		}
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
