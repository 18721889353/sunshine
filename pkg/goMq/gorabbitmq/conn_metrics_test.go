package gorabbitmq

import (
	"testing"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestRegisterConnectionStats_Collect 连接状态指标 emit + 登记幂等。
func TestRegisterConnectionStats_Collect(t *testing.T) {
	connected := &Connection{}
	connected.isConnected.Store(true)
	connected.reconnectCount.Store(7)

	disconnected := &Connection{}
	disconnected.isConnected.Store(false)

	connMu.Lock()
	orig := connRegister
	connRegister = map[string]*Connection{}
	connMu.Unlock()
	t.Cleanup(func() {
		connMu.Lock()
		connRegister = orig
		connMu.Unlock()
	})

	RegisterConnectionStats("ok-pool", connected)
	RegisterConnectionStats("ok-pool", disconnected) // 幂等：不覆盖

	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	got := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			switch f.GetName() {
			case "rabbitmq_connected":
				got["connected"] = m.GetGauge().GetValue()
			case "rabbitmq_reconnect_count_total":
				got["reconnect"] = m.GetCounter().GetValue()
			}
		}
	}
	if got["connected"] != 1 {
		t.Errorf("connected 应 1（首次登记生效）, got %v", got["connected"])
	}
	if got["reconnect"] != 7 {
		t.Errorf("reconnect 应 7, got %v", got["reconnect"])
	}
}

// TestObserveQueueDepth 队列积压深度 Gauge 记录。
func TestObserveQueueDepth(t *testing.T) {
	ObserveQueueDepth("test-queue", 42)

	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range families {
		if f.GetName() != "rabbitmq_queue_messages" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "queue" && l.GetValue() == "test-queue" {
					if m.GetGauge().GetValue() != 42 {
						t.Errorf("queue_messages 应 42, got %v", m.GetGauge().GetValue())
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("rabbitmq_queue_messages{queue=test-queue} 未找到")
	}
}
