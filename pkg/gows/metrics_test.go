package gows

import (
	"sync/atomic"
	"testing"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestRegisterStats_Collect 验证连接数指标 emit，且"最新登记胜出"——
// dispatcher 重建时必须指向新计数器（否则指标冻结在旧实例）。
func TestRegisterStats_Collect(t *testing.T) {
	local, remote := &atomic.Int32{}, &atomic.Int32{}
	local.Store(7)
	remote.Store(3)

	// 重置全局（测试隔离）
	activeStats.Store(nil)
	RegisterStats(local, remote)

	// 模拟 dispatcher 重建：新计数器必须接管（Store 语义，非 CAS）
	newLocal, newRemote := &atomic.Int32{}, &atomic.Int32{}
	newLocal.Store(2)
	newRemote.Store(1)
	RegisterStats(newLocal, newRemote)

	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			switch f.GetName() {
			case "gows_local_connections":
				got["local"] = m.GetGauge().GetValue()
			case "gows_remote_uids":
				got["remote"] = m.GetGauge().GetValue()
			}
		}
	}
	if got["local"] != 2 || got["remote"] != 1 {
		t.Errorf("最新登记应胜出: local=%v remote=%v, want 2/1", got["local"], got["remote"])
	}
}
