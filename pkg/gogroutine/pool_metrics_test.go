package gogroutine

import (
	"testing"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// fakePoolStats 实现 PoolStatsProvider，无需真实 ants 池。
type fakePoolStats struct {
	info PoolStatsInfo
}

func (f *fakePoolStats) Stats() PoolStatsInfo { return f.info }

// TestRegisterPoolStats_Collect 协程池水位指标 emit + 登记幂等。
func TestRegisterPoolStats_Collect(t *testing.T) {
	fake := &fakePoolStats{info: PoolStatsInfo{
		Name: "test", Running: 8, Cap: 10, Waiting: 3,
	}}
	poolStatsMu.Lock()
	orig := poolStatsRegister
	poolStatsRegister = map[string]PoolStatsProvider{}
	poolStatsMu.Unlock()
	t.Cleanup(func() {
		poolStatsMu.Lock()
		poolStatsRegister = orig
		poolStatsMu.Unlock()
	})

	RegisterPoolStats("test-pool", fake)
	RegisterPoolStats("test-pool", &fakePoolStats{}) // 幂等

	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	got := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			switch f.GetName() {
			case "gogroutine_pool_running":
				got["running"] = m.GetGauge().GetValue()
			case "gogroutine_pool_capacity":
				got["cap"] = m.GetGauge().GetValue()
			case "gogroutine_pool_waiting":
				got["waiting"] = m.GetGauge().GetValue()
			}
		}
	}
	if got["running"] != 8 || got["cap"] != 10 || got["waiting"] != 3 {
		t.Errorf("水位指标错误: running=%v cap=%v waiting=%v, want 8/10/3", got["running"], got["cap"], got["waiting"])
	}
}

// TestUnregisterPoolStats 池释放后注销登记，Collector 不再 emit 该序列。
func TestUnregisterPoolStats(t *testing.T) {
	fake := &fakePoolStats{info: PoolStatsInfo{Name: "x", Running: 1, Cap: 1}}
	poolStatsMu.Lock()
	orig := poolStatsRegister
	poolStatsRegister = map[string]PoolStatsProvider{}
	poolStatsMu.Unlock()
	t.Cleanup(func() {
		poolStatsMu.Lock()
		poolStatsRegister = orig
		poolStatsMu.Unlock()
	})

	RegisterPoolStats("temp-pool", fake)
	UnregisterPoolStats("temp-pool")

	poolStatsMu.Lock()
	_, exists := poolStatsRegister["temp-pool"]
	poolStatsMu.Unlock()
	if exists {
		t.Error("注销后条目应已删除")
	}

	// 再次注销不存在的名字不 panic
	UnregisterPoolStats("temp-pool")
	UnregisterPoolStats("")
}
