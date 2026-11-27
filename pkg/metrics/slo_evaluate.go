package metrics

import "math"

// SLO 服务等级目标策略（配置语义与 Google SRE 一致）。
//
// 设计要点：
//   - 一套语义：可用性目标 + 延迟目标 + 最小样本——旧的错误率5%/P99 1s
//     就是 Availability=0.95 / LatencyP99=1 的表达（0.95 = 1-0.05）。
//   - BurnRate 燃烧速率：错误预算消耗速度（实际错误率/允许错误率），
//     超 14.4x 表示 1h 预算会在 5min 耗尽（Google SRE 推荐阈值）。
//     注意：当前为单窗口口径；多窗口（1h/5m 短长窗联合）需快照历史，
//     留待业务接入后按需实现（不做无调用方的半成品字段）。
type SLO struct {
	Availability     float64 // 可用性目标（0~1，如 0.999）；零值回落默认 0.99
	LatencyP99       float64 // P99 延迟目标（秒）；零值回落默认 1.0
	MinSamples       uint64  // 最小样本数（防偶发单次失败误报）
	BurnRateCritical float64 // 严重燃烧速率（默认 14.4，Google SRE 推荐）
}

// defaultSLO 大厂基线：99% 可用性 + P99 1s（对齐旧默认阈值的严格度）。
func defaultSLO() SLO {
	return SLO{
		Availability:     0.99,
		LatencyP99:       1.0,
		MinSamples:       10,
		BurnRateCritical: 14.4,
	}
}

func (p SLO) normalize() SLO {
	d := defaultSLO()
	// NaN > 0 为 false 会被静默回落——但脏配置不该被吞（EvaluateSLO 要能检测）。
	// NaN/Inf 原样保留，交给 EvaluateSLO 标记 invalid-availability。
	if p.Availability > 0 || math.IsNaN(p.Availability) || math.IsInf(p.Availability, 0) {
		d.Availability = p.Availability
	}
	if p.LatencyP99 > 0 {
		d.LatencyP99 = p.LatencyP99
	}
	if p.MinSamples > 0 {
		d.MinSamples = p.MinSamples
	}
	if p.BurnRateCritical > 0 {
		d.BurnRateCritical = p.BurnRateCritical
	}
	return d
}

// SLOResult 单个 SLO 的评估结果。
type SLOResult struct {
	Policy      SLO
	SLI         SLI
	Attained    bool    // 当前窗口是否达标
	ErrorBudget float64 // 剩余错误预算比例（0~1，1=完全没消耗）
	BurnRate    float64 // 错误预算燃烧速率（1=按目标速率消耗，>14.4=超速）
	Alert       string  // 告警原因（"burn-rate" / "latency" / ""）
}

// EvaluateSLO 评估 SLI 是否达标 + 燃烧速率。
//
// 燃烧速率 = 实际错误率 / 允许错误率（1 - Availability）。
// 例：目标 99.9%（允许错误率 0.001），实际错误率 0.01 → BurnRate=10（10倍速消耗预算）。
// Google SRE 建议：BurnRate > 14.4 时，1 小时预算会在 5 分钟内耗尽 → 立即告警。
func EvaluateSLO(sli SLI, policy SLO) SLOResult {
	p := policy.normalize()
	st := SLOResult{Policy: p, SLI: sli, ErrorBudget: 1, Attained: true}

	if sli.TotalEvents < p.MinSamples {
		return st // 样本不足不判定（与旧 MinSamples 语义一致）
	}

	// Availability 必须 0~1 有限数：>1 会让 allowedErr<0（静默不告警）；
	// NaN/Inf 让比较恒 false。normalize 只挡了 <=0，这里补上界与非有限数。
	if math.IsNaN(p.Availability) || math.IsInf(p.Availability, 0) || p.Availability > 1 {
		return SLOResult{Policy: p, SLI: sli, Attained: true, Alert: "invalid-availability"}
	}
	allowedErr := 1 - p.Availability
	if allowedErr > 0 {
		actualErr := float64(sli.BadEvents) / float64(sli.TotalEvents)
		st.BurnRate = actualErr / allowedErr
		st.ErrorBudget = math.Max(0, 1-st.BurnRate)
		if st.BurnRate > 1 {
			st.Attained = false
		}
		if st.BurnRate >= p.BurnRateCritical {
			st.Alert = "burn-rate"
		}
	}

	// 延迟目标独立判定（P99 无 histogram 数据时 sli.LatencyP99=0 视为达标）
	if p.LatencyP99 > 0 && sli.LatencyP99 > 0 {
		if sli.LatencyP99 > p.LatencyP99 {
			st.Attained = false
			if st.Alert == "" {
				st.Alert = "latency"
			}
		}
	}
	return st
}
