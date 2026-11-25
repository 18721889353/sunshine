package metrics

import "math"

// ============================================================================
// 窗口差分：把累计快照转成窗口增量。
//
// 口径（与网关 WindowDelta 一致，是"报告数字准确"的根基）：
//   - Durations/Counters：逐项 cur - prev（累计值差分）；
//     若 cur < prev（进程重启/指标重置），取 cur 兜底，避免负数。
//   - Gauges：当前值，不差分（始终是采集瞬间的值）。
//
// prev 为 nil 表示首个窗口，此时返回 cur 的副本（首推报告全量）。
// ============================================================================

// WindowDeltaMetric 计算窗口增量（MetricSnapshot 版） delta = cur - prev。
func WindowDeltaMetric(prev, cur *MetricSnapshot) *MetricSnapshot {
	if cur == nil {
		return NewMetricSnapshot()
	}
	if prev == nil {
		return cur.Clone()
	}

	delta := NewMetricSnapshot()

	// Durations：逐指标差分（helper 与 DurationsByLabel 复用，避免口径漂移）
	for name, c := range cur.Durations {
		delta.Durations[name] = deltaDurationAgg(c, prev.Durations[name])
	}
	// DurationsByLabel：同款差分（label 维度下钻用，业务诊断按 entry/replica 取窗口 P99）
	for name, cm := range cur.DurationsByLabel {
		pm := prev.DurationsByLabel[name]
		dm := make(map[string]*DurationAgg, len(cm))
		for lk, c := range cm {
			var p *DurationAgg
			if pm != nil {
				p = pm[lk]
			}
			dm[lk] = deltaDurationAgg(c, p)
		}
		delta.DurationsByLabel[name] = dm
	}
	// cur 没有但 prev 有（指标消失）：跳过，无窗口数据

	// Counters：逐 key 差分
	for name, cm := range cur.Counters {
		pm := prev.Counters[name]
		d := make(map[string]float64, len(cm))
		for k, cv := range cm {
			if pm == nil {
				d[k] = cv
				continue
			}
			d[k] = subFloat(cv, pm[k])
		}
		delta.Counters[name] = d
	}

	// Summaries：与 Durations 同样做累计差分（sum 单位由指标决定，不参与 P99 诊断）
	for name, c := range cur.Summaries {
		p := prev.Summaries[name]
		if p == nil {
			delta.Summaries[name] = c.Clone()
			continue
		}
		d := &DurationAgg{Result: make(map[string]uint64)}
		d.Count = subCount(c.Count, p.Count)
		d.SumSec = subFloat(c.SumSec, p.SumSec)
		for r, cc := range c.Result {
			d.Result[r] = subCount(cc, p.Result[r])
		}
		delta.Summaries[name] = d
	}

	// Gauges：当前值，原样取 cur
	for name, gm := range cur.Gauges {
		d := make(map[string]float64, len(gm))
		for k, v := range gm {
			d[k] = v
		}
		delta.Gauges[name] = d
	}

	return delta
}

// deltaDurationAgg 单个 DurationAgg 的窗口差分（累计→窗口）。
// prev 为 nil（新注册）时整段是窗口增量。
func deltaDurationAgg(c, p *DurationAgg) *DurationAgg {
	if p == nil {
		return c.Clone()
	}
	d := &DurationAgg{Result: make(map[string]uint64)}
	d.Count = subCount(c.Count, p.Count)
	d.SumSec = subFloat(c.SumSec, p.SumSec)
	// result 维度差分
	for r, cc := range c.Result {
		d.Result[r] = subCount(cc, p.Result[r])
	}
	// prev 有而 cur 无的 result（如本窗口无 error）：补 0，保持 key 存在
	for r := range p.Result {
		if _, ok := d.Result[r]; !ok {
			d.Result[r] = 0
		}
	}
	// bucket：bounds 不差分（同指标固定），counts 逐桶差分（累计→窗口）
	if len(c.Bounds) > 0 {
		// 独立拷贝，避免与 cur 共享底层数组
		d.Bounds = append([]float64(nil), c.Bounds...)
		d.Counts = make([]uint64, len(c.Counts))
		for i, cc := range c.Counts {
			var pc uint64
			if i < len(p.Counts) {
				pc = p.Counts[i]
			}
			d.Counts[i] = subCount(cc, pc)
		}
	}
	return d
}

// subCount 无符号差分：cur >= prev 时 cur-prev，否则（重置）取 cur 兜底。
func subCount(cur, prev uint64) uint64 {
	if cur < prev {
		return cur
	}
	return cur - prev
}

// subFloat 浮点差分：cur >= prev 时 cur-prev，否则（重置）取 cur 兜底。
func subFloat(cur, prev float64) float64 {
	if cur < prev {
		return cur
	}
	return cur - prev
}

// Clone 深拷贝快照（避免采集方与报告器共享底层 map）。
func (s *MetricSnapshot) Clone() *MetricSnapshot {
	if s == nil {
		return NewMetricSnapshot()
	}
	c := NewMetricSnapshot()
	for name, d := range s.Durations {
		c.Durations[name] = d.Clone()
	}
	for name, m := range s.Counters {
		d := make(map[string]float64, len(m))
		for k, v := range m {
			d[k] = v
		}
		c.Counters[name] = d
	}
	for name, m := range s.Gauges {
		d := make(map[string]float64, len(m))
		for k, v := range m {
			d[k] = v
		}
		c.Gauges[name] = d
	}
	for name, d := range s.Summaries {
		c.Summaries[name] = d.Clone()
	}
	for name, m := range s.DurationsByLabel {
		d := make(map[string]*DurationAgg, len(m))
		for k, v := range m {
			d[k] = v.Clone()
		}
		c.DurationsByLabel[name] = d
	}
	return c
}

// Clone 深拷贝 DurationAgg。
func (d *DurationAgg) Clone() *DurationAgg {
	if d == nil {
		return &DurationAgg{Result: make(map[string]uint64)}
	}
	c := &DurationAgg{
		Count:  d.Count,
		SumSec: d.SumSec,
		Result: make(map[string]uint64, len(d.Result)),
		// 必须深拷 Bounds/Counts：首推（prev=nil）走 Clone，
		// 漏拷会导致首份报告算不出 P99。
		Bounds: append([]float64(nil), d.Bounds...),
		Counts: append([]uint64(nil), d.Counts...),
	}
	for k, v := range d.Result {
		c.Result[k] = v
	}
	return c
}

// ErrorCount 取窗口 error 样本数（result=error；无 result 维度返回 0）。
func (d *DurationAgg) ErrorCount() uint64 {
	if d == nil {
		return 0
	}
	return d.Result["error"]
}

// ErrorRate 取窗口错误率（0~1）；无样本返回 0。
func (d *DurationAgg) ErrorRate() float64 {
	if d == nil || d.Count == 0 {
		return 0
	}
	return float64(d.ErrorCount()) / float64(d.Count)
}

// MeanSec 取窗口均值耗时（秒）；无样本返回 0。
func (d *DurationAgg) MeanSec() float64 {
	if d == nil || d.Count == 0 {
		return 0
	}
	return d.SumSec / float64(d.Count)
}

// P99Sec 取窗口 P99 耗时（秒），线性插值（Prometheus histogram_quantile 语义）；
// 无 bucket 或无样本返回 false。
//
// 前提：d 是窗口差分后的桶（Counts 为窗口增量）。
func (d *DurationAgg) P99Sec() (float64, bool) {
	return d.quantile(0.99)
}

// P95Sec 取窗口 P95 耗时（秒）。
func (d *DurationAgg) P95Sec() (float64, bool) {
	return d.quantile(0.95)
}

// quantile 从窗口桶做线性插值分位数。q ∈ (0,1]。
// 与网关 histogramQuantile 同口径（已线上验证）。
func (d *DurationAgg) quantile(q float64) (float64, bool) {
	if d == nil || len(d.Bounds) == 0 || len(d.Counts) == 0 || d.Count == 0 {
		return 0, false
	}
	// Bounds/Counts 必须一一对应（同指标桶固定）。防御外部构造不一致导致的
	// 越界 panic——报告器不该因单个畸形快照崩掉整个推送 goroutine。
	n := len(d.Bounds)
	if len(d.Counts) < n {
		n = len(d.Counts)
	}
	total := float64(d.Count)
	rank := q * total

	// 逐桶找 rank 落入的桶，线性插值。
	// 关键：Prometheus counts[i] 本身是累计值（≤bounds[i] 的样本数），
	// 直接用即可，不能再累加（否则分位数全错）。
	for i := 0; i < n; i++ {
		b := d.Counts[i]
		cum := float64(b) // 累计值本身
		if cum < rank {
			continue
		}
		// rank 落入第 i 桶：[lowerBound, upperBound]
		var prevCum float64 // 本桶之前的累计（上一桶顶部）
		if i > 0 {
			prevCum = float64(d.Counts[i-1])
		}
		bucketCount := cum - prevCum // 本桶新增样本数
		var lower float64
		if i > 0 {
			lower = d.Bounds[i-1]
		}
		upper := d.Bounds[i]
		// +Inf 桶：上界是 math.Inf(1) 或序列化为 1e300（float64 有限值上限附近）。
		// 必须用 >= 而非 >，否则 upper==1e300 时会走插值算出天文数字 P99。
		if upper >= 1e300 || math.IsInf(upper, 1) {
			// 无法插值上界，返回该桶下界（退化但可用）
			return lower, true
		}
		if bucketCount == 0 {
			return upper, true
		}
		frac := (rank - prevCum) / bucketCount
		return lower + frac*(upper-lower), true
	}
	// rank 超过所有桶（理论上不该发生，因最后一桶是 +Inf）
	return d.Bounds[len(d.Bounds)-1], true
}
