package metrics

// RED RED 方法（Rate/Errors/Duration）的标准视图。
//
// 为什么单独抽象：现有 renderDurationLine 把 QPS/错误率/P99 直接拼成文本，
// 报告/诊断/告警各自重复算一遍——口径漂移风险。抽成对象后全链路共用。
type RED struct {
	Rate      float64 // 每秒请求数（QPS）
	Errors    float64 // 每秒错误数
	ErrorRate float64 // 错误率（0~1）
	Duration  struct {
		P50, P95, P99 float64 // 秒
	}
	SampleCount uint64 // 窗口样本数
}

// ComputeRED 从快照按 scope 聚合 RED 三元组。
// 使用窗口差分后的快照（与报告/诊断同源），scope 语义同 ComputeSLI。
func ComputeRED(s *MetricSnapshot, scope string, windowSec float64) RED {
	var out RED
	if s == nil {
		return out
	}

	var totalErr uint64
	for name, agg := range s.Durations {
		if agg == nil || !matchScope(name, scope) {
			continue // nil 防护：map value 可为 nil，直接访问字段会 panic
		}
		out.SampleCount += agg.Count
		totalErr += agg.ErrorCount()

		// 分位数取所有匹配指标的最大值（最慢链路主导）
		if p50, ok := agg.quantile(0.50); ok && p50 > out.Duration.P50 {
			out.Duration.P50 = p50
		}
		if p95, ok := agg.P95Sec(); ok && p95 > out.Duration.P95 {
			out.Duration.P95 = p95
		}
		if p99, ok := agg.P99Sec(); ok && p99 > out.Duration.P99 {
			out.Duration.P99 = p99
		}
	}

	if windowSec > 0 {
		out.Rate = float64(out.SampleCount) / windowSec
		out.Errors = float64(totalErr) / windowSec
	}
	if out.SampleCount > 0 {
		out.ErrorRate = float64(totalErr) / float64(out.SampleCount)
	}
	return out
}
