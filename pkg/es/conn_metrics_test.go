package es

import (
	"testing"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// esConnectedValue Gather 出 es_connected 的当前值（default cluster label）。
func esConnectedValue(t *testing.T) float64 {
	t.Helper()
	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "es_connected" {
			continue
		}
		for _, m := range f.GetMetric() {
			return m.GetGauge().GetValue()
		}
	}
	t.Fatal("es_connected 未找到")
	return 0
}

// TestObserveESConnected 连通性 Gauge 记录。
// 关键：先断言 true→1（1≠默认值0，能真正验证函数生效），
// 再断言 false→0（验证状态翻转）——只测最终 0 是假阳性（Gauge 默认即 0）。
func TestObserveESConnected(t *testing.T) {
	ObserveESConnected(true)
	if got := esConnectedValue(t); got != 1 {
		t.Fatalf("连通时应为 1, got %v", got)
	}

	ObserveESConnected(false)
	if got := esConnectedValue(t); got != 0 {
		t.Errorf("断开时应为 0, got %v", got)
	}
}
