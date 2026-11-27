package metrics

import (
	"math"
	"testing"
)

func sli(ok, bad uint64, p99 float64) SLI {
	total := ok + bad
	a := 1.0
	if total > 0 {
		a = float64(ok) / float64(total)
	}
	return SLI{Availability: a, GoodEvents: ok, BadEvents: bad, TotalEvents: total, LatencyP99: p99}
}

// TestEvaluateSLO_BurnRate 燃烧速率口径：目标99.9%、实际1%错误 → BurnRate=10。
func TestEvaluateSLO_BurnRate(t *testing.T) {
	p := SLO{Availability: 0.999, MinSamples: 10}
	got := EvaluateSLO(sli(990, 10, 0), p) // 1% 错误率

	if math.Abs(got.BurnRate-10) > 0.01 {
		t.Errorf("BurnRate 应 10, got %v", got.BurnRate)
	}
	if got.Attained {
		t.Error("BurnRate=10 > 1 表示错误预算超速消耗，应不达标")
	}
	if got.Alert != "" { // 10 < Critical 14.4
		t.Errorf("BurnRate=10 未到 Critical(14.4), 不应告警, got %q", got.Alert)
	}
}

// TestEvaluateSLO_CriticalBurn 高燃烧率触发告警（Google SRE 14.4x）。
func TestEvaluateSLO_CriticalBurn(t *testing.T) {
	p := SLO{Availability: 0.999, MinSamples: 10}
	got := EvaluateSLO(sli(900, 100, 0), p) // 10% 错误率 → BurnRate=100

	if got.Alert != "burn-rate" {
		t.Errorf("高燃烧率应告警, got %q", got.Alert)
	}
	if got.Attained {
		t.Error("BurnRate=100 应不达标")
	}
	if got.ErrorBudget != 0 {
		t.Errorf("预算应耗尽, got %v", got.ErrorBudget)
	}
}

// TestEvaluateSLO_Latency 延迟目标独立判定。
func TestEvaluateSLO_Latency(t *testing.T) {
	p := SLO{Availability: 0.99, LatencyP99: 0.5, MinSamples: 10}
	got := EvaluateSLO(sli(100, 0, 1.2), p)
	if got.Attained {
		t.Error("P99=1.2s 超目标 0.5s, 应不达标")
	}
	if got.Alert != "latency" {
		t.Errorf("应告警 latency, got %q", got.Alert)
	}
}

// TestEvaluateSLO_MinSamples 样本不足不判定（防偶发失败误报）。
func TestEvaluateSLO_MinSamples(t *testing.T) {
	p := SLO{Availability: 0.999, MinSamples: 10}
	got := EvaluateSLO(sli(0, 2, 0), p) // 2 个样本全失败

	if !got.Attained || got.BurnRate != 0 {
		t.Errorf("样本不足应跳过判定, got attained=%v burn=%v", got.Attained, got.BurnRate)
	}
}

// TestToSLO_BurnRateCritical 公式守护：BurnRateCritical = ErrRateCritical / ErrRateWarn。
// 默认 5%→20% 应得 4.0（错误率到 20% 时 4 倍速耗预算）。
// 历史 bug：曾误算为 (1-ErrRateWarn)/(1-ErrRateCritical)=1.1875，告警阈值严重偏离。
func TestToSLO_BurnRateCritical(t *testing.T) {
	th := DiagnosisThresholds{} // 用默认 normalize
	slo := th.ToSLO()
	if diff := slo.BurnRateCritical - 4.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("默认 BurnRateCritical 应 4.0（0.20/0.05）, got %v（浮点除法有精度误差，用近似比较）", slo.BurnRateCritical)
	}

	// 自定义：1%→5% 应得 5.0（0.05/0.01）
	custom := DiagnosisThresholds{ErrRateWarn: 0.01, ErrRateCritical: 0.05}
	if got := custom.ToSLO().BurnRateCritical; got-5.0 > 1e-9 || got-5.0 < -1e-9 {
		t.Errorf("自定义 BurnRateCritical 应 5.0, got %v", got)
	}
}

// TestToSLO_Availability 映射关系：errRate 5% → Availability 0.95。
func TestToSLO_Availability(t *testing.T) {
	slo := DiagnosisThresholds{}.ToSLO()
	if slo.Availability != 0.95 {
		t.Errorf("默认 Availability 应 0.95, got %v", slo.Availability)
	}
}

// TestComputeSLI_NilAgg map value 为 nil 时不 panic（曾因 agg.Count 直接访问字段 panic）。
func TestComputeSLI_NilAgg(t *testing.T) {
	s := NewMetricSnapshot()
	s.Durations["broken"] = nil // 模拟脏数据/并发写入
	s.Durations["good"] = mkAgg(10, 0, false)

	got := ComputeSLI(s, "", 10)
	if got.TotalEvents != 10 {
		t.Errorf("nil 条目应跳过, total 应 10, got %d", got.TotalEvents)
	}
}

// TestComputeRED_NilAgg 同上：RED 计算遇 nil aggregator 不 panic。
func TestComputeRED_NilAgg(t *testing.T) {
	s := NewMetricSnapshot()
	s.Durations["broken"] = nil
	s.Durations["good"] = mkAgg(10, 1, true)

	got := ComputeRED(s, "", 10)
	if got.SampleCount != 10 {
		t.Errorf("nil 条目应跳过, samples 应 10, got %d", got.SampleCount)
	}
}

// TestComputeSLI_ErrorCountExceedCount 防御性：bad > Count 时不下溢（uint64 无符号减法）。
func TestComputeSLI_ErrorCountExceedCount(t *testing.T) {
	s := NewMetricSnapshot()
	agg := &DurationAgg{Count: 5, Result: map[string]uint64{"error": 10}} // 脏数据：错误数 > 总数
	s.Durations["dirty"] = agg

	got := ComputeSLI(s, "", 10)
	if got.BadEvents != 5 || got.GoodEvents != 0 {
		t.Errorf("应钳制 bad=Count=5, good=0, got bad=%d good=%d", got.BadEvents, got.GoodEvents)
	}
	if got.Availability != 0 {
		t.Errorf("全错误时可用性应 0, got %v", got.Availability)
	}
}

// TestEvaluateSLO_InvalidAvailability 越界/非有限 Availability 必须显式标记，
// 不能静默当成"达标"（配 1.5 会让 allowedErr<0 跳过判定，配错无人知晓）。
func TestEvaluateSLO_InvalidAvailability(t *testing.T) {
	for _, v := range []float64{1.5, math.NaN(), math.Inf(1)} {
		got := EvaluateSLO(sli(90, 10, 0), SLO{Availability: v})
		if got.Alert != "invalid-availability" {
			t.Errorf("availability=%v 应标记 invalid-availability, got %q", v, got.Alert)
		}
	}
}
