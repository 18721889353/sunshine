package metrics

// SLI 服务等级指标：从 RED 原始数据计算的"可承诺"指标。
//
// 大厂口径：SLI 是对用户可感知服务质量的量化测量（Google SRE Book）。
// 一个 SLI = 好事件 / 有效事件，天然归一到 0~1，可直接对 SLO 目标。
// 本框架的好事件 = result=ok 的请求，有效事件 = 全部请求。
type SLI struct {
	Availability float64 // 可用性 = 成功请求 / 总请求（0~1）
	LatencyP99   float64 // P99 耗时（秒）—— 无 histogram 桶时为 0
	Rate         float64 // 吞吐（请求数/窗口秒）
	GoodEvents   uint64  // 成功请求数（result=ok）
	BadEvents    uint64  // 失败请求数（result!=ok）
	TotalEvents  uint64  // 总请求数
}

// ComputeSLI 从快照计算指定 scope 的 SLI。
//
// scope:
//   - 精确指标名（如 "http_request_duration_seconds"）→ 只算该指标
//   - 空串 → 汇总所有 Durations（全服务视角）
//   - 前缀匹配（如 "grpc_server"）→ 汇总匹配的所有指标
//
// 汇总口径：好/坏事件数相加后**再算可用性**（不能对各指标可用性求平均——
// 小流量指标会与大流量指标等权，扭曲真实用户体验）。
func ComputeSLI(s *MetricSnapshot, scope string, windowSec float64) SLI {
	out := SLI{Availability: 1.0} // 无样本视为 100% 可用（无故障信号不误报）
	if s == nil {
		return out
	}

	for name, agg := range s.Durations {
		if agg == nil || !matchScope(name, scope) {
			continue // nil 防护：map value 可为 nil，直接访问字段会 panic
		}
		one := sliFromAgg(agg)
		out.GoodEvents += one.GoodEvents
		out.BadEvents += one.BadEvents
		out.TotalEvents += one.TotalEvents
		// P99 取所有匹配指标的最大值（最慢链路主导用户体验，不能平均）
		if one.LatencyP99 > out.LatencyP99 {
			out.LatencyP99 = one.LatencyP99
		}
	}

	if out.TotalEvents > 0 {
		out.Availability = float64(out.GoodEvents) / float64(out.TotalEvents)
	}
	if windowSec > 0 {
		out.Rate = float64(out.TotalEvents) / windowSec
	}
	return out
}

// sliFromAgg 从单个 DurationAgg 构造 SLI（ComputeSLI 与诊断引擎共用，口径唯一）。
// 包含防御：Result 样本数 > Count 时钳制，避免 uint64 下溢。
func sliFromAgg(agg *DurationAgg) SLI {
	var out SLI
	if agg == nil {
		return out
	}
	bad := agg.ErrorCount()
	if bad > agg.Count {
		bad = agg.Count
	}
	out.GoodEvents = agg.Count - bad
	out.BadEvents = bad
	out.TotalEvents = agg.Count
	if p99, ok := agg.P99Sec(); ok {
		out.LatencyP99 = p99
	}
	return out
}

// matchScope scope 匹配指标名：空串全匹配，否则前缀匹配。
func matchScope(name, scope string) bool {
	if scope == "" {
		return true
	}
	return len(name) >= len(scope) && name[:len(scope)] == scope
}
