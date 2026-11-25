package metrics

import "testing"

// TestWindowDelta_CounterHistogram 差分：累计值 cur-prev，重置取 cur 兜底
func TestWindowDelta_CounterHistogram(t *testing.T) {
	prev := NewMetricSnapshot()
	prev.Durations["redis_op"] = &DurationAgg{Count: 100, SumSec: 2.0, Result: map[string]uint64{"ok": 95, "error": 5}}

	cur := NewMetricSnapshot()
	cur.Durations["redis_op"] = &DurationAgg{Count: 150, SumSec: 3.0, Result: map[string]uint64{"ok": 140, "error": 10}}

	d := WindowDeltaMetric(prev, cur)
	got := d.Durations["redis_op"]
	if got.Count != 50 {
		t.Errorf("窗口 count 应 50, got %d", got.Count)
	}
	if got.SumSec != 1.0 {
		t.Errorf("窗口 sum 应 1.0, got %v", got.SumSec)
	}
	if got.Result["error"] != 5 {
		t.Errorf("窗口 error 应 5, got %d", got.Result["error"])
	}
	if got.Result["ok"] != 45 {
		t.Errorf("窗口 ok 应 45, got %d", got.Result["ok"])
	}
}

// TestWindowDelta_Reset 兜底：cur < prev（进程重启）时取 cur，避免负数
func TestWindowDelta_Reset(t *testing.T) {
	prev := NewMetricSnapshot()
	prev.Durations["x"] = &DurationAgg{Count: 1000, SumSec: 100}
	cur := NewMetricSnapshot()
	cur.Durations["x"] = &DurationAgg{Count: 3, SumSec: 0.5}

	d := WindowDeltaMetric(prev, cur)
	if d.Durations["x"].Count != 3 {
		t.Errorf("重置时应取 cur=3, got %d", d.Durations["x"].Count)
	}
}

// TestWindowDelta_FirstWindow prev=nil 首窗返回 cur 副本
func TestWindowDelta_FirstWindow(t *testing.T) {
	cur := NewMetricSnapshot()
	cur.Durations["x"] = &DurationAgg{Count: 42}
	d := WindowDeltaMetric(nil, cur)
	if d.Durations["x"].Count != 42 {
		t.Errorf("首窗应返回 cur=42, got %d", d.Durations["x"].Count)
	}
	// 副本独立
	cur.Durations["x"].Count = 999
	if d.Durations["x"].Count != 42 {
		t.Error("应为深拷贝，不受 cur 后续修改影响")
	}
}

// TestWindowDelta_Gauge 不差分，取 cur 当前值
func TestWindowDelta_Gauge(t *testing.T) {
	prev := NewMetricSnapshot()
	prev.Gauges["g"] = map[string]float64{"k": 10}
	cur := NewMetricSnapshot()
	cur.Gauges["g"] = map[string]float64{"k": 7}

	d := WindowDeltaMetric(prev, cur)
	if d.Gauges["g"]["k"] != 7 {
		t.Errorf("gauge 应取 cur=7, got %v", d.Gauges["g"]["k"])
	}
}

// TestWindowDelta_Counter 差分
func TestWindowDelta_Counter(t *testing.T) {
	prev := NewMetricSnapshot()
	prev.Counters["cache_hit_total"] = map[string]float64{"op=Get": 100}
	cur := NewMetricSnapshot()
	cur.Counters["cache_hit_total"] = map[string]float64{"op=Get": 160}

	d := WindowDeltaMetric(prev, cur)
	if d.Counters["cache_hit_total"]["op=Get"] != 60 {
		t.Errorf("counter 窗口应 60, got %v", d.Counters["cache_hit_total"]["op=Get"])
	}
}

// TestDurationAgg 计算辅助
func TestDurationAgg(t *testing.T) {
	d := &DurationAgg{Count: 100, SumSec: 2.0, Result: map[string]uint64{"ok": 95, "error": 5}}
	if d.ErrorCount() != 5 {
		t.Errorf("ErrorCount 应 5, got %d", d.ErrorCount())
	}
	if d.ErrorRate() != 0.05 {
		t.Errorf("ErrorRate 应 0.05, got %v", d.ErrorRate())
	}
	if d.MeanSec() != 0.02 {
		t.Errorf("MeanSec 应 0.02, got %v", d.MeanSec())
	}
}

// TestDurationAgg_P99 线性插值分位数
func TestDurationAgg_P99(t *testing.T) {
	// 桶: [0.01, 0.05, 0.1, 0.5, 1, 5, 10, +Inf]
	bounds := []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10, 1e300}
	// 100 样本分布跨桶：50 在 [0.01,0.05]、40 在 [0.05,0.1]、10 在 [1,5]
	// 关键：分布跨桶，P50 与 P99 落在不同桶，才能区分分位数是否正确
	counts := []uint64{50, 90, 90, 90, 90, 100, 100, 100}
	d := &DurationAgg{Count: 100, Bounds: bounds, Counts: counts}

	p50, ok := d.quantile(0.5)
	if !ok {
		t.Fatal("P50 应可计算")
	}
	p99, ok := d.P99Sec()
	if !ok {
		t.Fatal("P99 应可计算")
	}
	// P50 rank=50 → 落入第1桶 [0.01,0.05]（前50累计）
	if p50 < 0.01 || p50 > 0.05 {
		t.Errorf("P50 应在 [0.01,0.05], got %v", p50)
	}
	// P99 rank=99 → 落入第6桶 [1,5]（cum 到 100）
	if p99 < 1 || p99 > 5 {
		t.Errorf("P99 应在 [1,5], got %v", p99)
	}
	// P99 必须显著大于 P50（若实现把分位数写死为 0.5 会在这里失败）
	if p99 <= p50 {
		t.Errorf("P99 应大于 P50: p50=%v p99=%v", p50, p99)
	}
}

// TestDurationAgg_NoBuckets 无 bucket 返回 false
func TestDurationAgg_NoBuckets(t *testing.T) {
	d := &DurationAgg{Count: 10}
	if _, ok := d.P99Sec(); ok {
		t.Error("无 bucket 应返回 false")
	}
}

// TestDurationAgg_CloneBuckets 回归守护：Clone 必须深拷 Bounds/Counts。
// 首推（prev=nil）走 Clone，漏拷会导致首份报告 P99 缺失。
func TestDurationAgg_CloneBuckets(t *testing.T) {
	orig := &DurationAgg{
		Count:  100,
		Bounds: []float64{0.1, 0.5, 1, 1e300},
		Counts: []uint64{10, 50, 90, 100},
		Result: map[string]uint64{"ok": 95, "error": 5},
	}
	c := orig.Clone()
	if len(c.Bounds) != len(orig.Bounds) || len(c.Counts) != len(orig.Counts) {
		t.Fatalf("Clone 应拷贝桶: bounds=%v counts=%v", c.Bounds, c.Counts)
	}
	if _, ok := c.P99Sec(); !ok {
		t.Error("Clone 后 P99 应可计算（首推场景）")
	}
	// 深拷贝验证：改副本不影响原
	c.Counts[0] = 999
	if orig.Counts[0] == 999 {
		t.Error("应为深拷贝，改副本不影响原")
	}
	c.Bounds[0] = 999
	if orig.Bounds[0] == 999 {
		t.Error("Bounds 应为深拷贝")
	}
}

// TestWindowDeltaMetric_FirstWindowP99 首窗差分后 P99 可用
func TestWindowDeltaMetric_FirstWindowP99(t *testing.T) {
	cur := NewMetricSnapshot()
	cur.Durations["x"] = &DurationAgg{
		Count:  100,
		Bounds: []float64{0.1, 0.5, 1, 1e300},
		Counts: []uint64{10, 50, 90, 100},
	}
	d := WindowDeltaMetric(nil, cur)
	if _, ok := d.Durations["x"].P99Sec(); !ok {
		t.Error("首窗（prev=nil）应能算 P99")
	}
}

// TestWindowDelta_DurationsByLabel 按 label 的直方图也要逐桶差分
// （hook 拿到的必须是窗口增量，不是累计值）。
func TestWindowDelta_DurationsByLabel(t *testing.T) {
	bounds := []float64{1, 10}
	prev := NewMetricSnapshot()
	prev.DurationsByLabel["demo"] = map[string]*DurationAgg{
		"entry=api": {Count: 100, SumSec: 100, Bounds: bounds, Counts: []uint64{80, 100}},
	}
	cur := NewMetricSnapshot()
	cur.DurationsByLabel["demo"] = map[string]*DurationAgg{
		"entry=api":    {Count: 150, SumSec: 200, Bounds: bounds, Counts: []uint64{110, 150}},
		"entry=notify": {Count: 10, SumSec: 20, Bounds: bounds, Counts: []uint64{5, 10}}, // 新 label
	}

	d := WindowDeltaMetric(prev, cur)
	api := d.DurationsByLabel["demo"]["entry=api"]
	if api == nil || api.Count != 50 || api.SumSec != 100 {
		t.Fatalf("api 窗口差分不符: %+v", api)
	}
	if len(api.Counts) != 2 || api.Counts[0] != 30 || api.Counts[1] != 50 {
		t.Errorf("api 桶差分不符: %v", api.Counts)
	}
	// 新出现的 label：整段作为窗口增量
	n := d.DurationsByLabel["demo"]["entry=notify"]
	if n == nil || n.Count != 10 || n.SumSec != 20 {
		t.Errorf("新 label 应整段计入: %+v", n)
	}
	// 深拷贝：改 delta 不影响 cur
	api.Counts[0] = 999
	if cur.DurationsByLabel["demo"]["entry=api"].Counts[0] != 110 {
		t.Error("delta 桶应深拷贝，不能与 cur 共享底层数组")
	}
}
