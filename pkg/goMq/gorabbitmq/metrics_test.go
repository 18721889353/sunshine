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
	observeRabbitmqPublish("ex", nil, time.Millisecond)
	observeRabbitmqPublish("ex", errors.New("conn closed"), time.Millisecond)
	observeRabbitmqConsume("q", nil, time.Millisecond)
	observeRabbitmqConsume("q", errors.New("handler failed"), time.Millisecond)
}
