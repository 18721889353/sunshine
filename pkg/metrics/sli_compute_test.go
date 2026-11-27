package metrics

import "testing"

// helper：构造一个 DurationAgg（count 样本 + result 分布 + 桶）
func mkAgg(count, errCount uint64, p99Buckets bool) *DurationAgg {
	a := &DurationAgg{
		Count:  count,
		Result: map[string]uint64{"ok": count - errCount, "error": errCount},
	}
	if p99Buckets {
		a.Bounds = []float64{0.1, 0.5, 1.0, 5.0}
		a.Counts = []uint64{count - errCount, count - errCount, count - errCount, count}
		a.SumSec = float64(count) * 0.2
	}
	return a
}

// TestComputeSLI_Aggregation 多指标汇总：好/坏事件相加后再算可用性（非平均）。
func TestComputeSLI_Aggregation(t *testing.T) {
	s := NewMetricSnapshot()
	s.Durations["api"] = mkAgg(100, 1, false) // 99% 可用
	s.Durations["tiny"] = mkAgg(10, 5, false) // 50% 可用（小流量）

	got := ComputeSLI(s, "", 10)
	if got.TotalEvents != 110 || got.GoodEvents != 104 || got.BadEvents != 6 {
		t.Errorf("事件数错误: total=%d good=%d bad=%d", got.TotalEvents, got.GoodEvents, got.BadEvents)
	}
	// 加权可用性 = 104/110 ≈ 0.945，而非 (0.99+0.5)/2=0.745
	want := 104.0 / 110.0
	if diff := got.Availability - want; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("可用性应加权=%.4f, got %.4f", want, got.Availability)
	}
	if got.Rate != 11 { // 110/10s
		t.Errorf("吞吐应为 11, got %v", got.Rate)
	}
}

// TestComputeSLI_Scope 前缀匹配只汇总匹配指标。
func TestComputeSLI_Scope(t *testing.T) {
	s := NewMetricSnapshot()
	s.Durations["http_request_duration_seconds"] = mkAgg(100, 0, false)
	s.Durations["grpc_server_handling"] = mkAgg(50, 5, false)

	got := ComputeSLI(s, "grpc_server", 10)
	if got.TotalEvents != 50 || got.BadEvents != 5 {
		t.Errorf("scope 过滤错误: total=%d bad=%d", got.TotalEvents, got.BadEvents)
	}
}

// TestComputeSLI_Empty 空快照：无样本视为 100% 可用（不误报）。
func TestComputeSLI_Empty(t *testing.T) {
	got := ComputeSLI(nil, "", 10)
	if got.Availability != 1 || got.TotalEvents != 0 {
		t.Errorf("空快照应 100%% 可用, got %+v", got)
	}
}
