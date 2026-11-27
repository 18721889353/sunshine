package metrics

import "testing"

// BenchmarkComputeSLI 核心 SLI 计算基准（报告/诊断路径，周期性执行）。
// 关注点：不应出现随指标数线性放大的分配（此前每指标 NewMetricSnapshot 已移除）。
func BenchmarkComputeSLI(b *testing.B) {
	s := NewMetricSnapshot()
	for i := 0; i < 20; i++ {
		s.Durations["metric"] = mkAgg(1000, 10, true)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ComputeSLI(s, "", 10)
	}
}

// BenchmarkEvaluateSLO SLO 评估基准（每指标调用，量级 = 指标数 × 频率）。
func BenchmarkEvaluateSLO(b *testing.B) {
	sli := SLI{Availability: 0.99, GoodEvents: 990, BadEvents: 10, TotalEvents: 1000, LatencyP99: 0.5}
	slo := DiagnosisThresholds{}.ToSLO()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EvaluateSLO(sli, slo)
	}
}

// BenchmarkDiagnose 诊断入口基准（fastCheck 5s 一次 × 指标数）。
func BenchmarkDiagnose(b *testing.B) {
	s := NewMetricSnapshot()
	for i := 0; i < 30; i++ {
		s.Durations["metric"] = mkAgg(100, 5, true)
	}
	th := DefaultDiagnosisThresholds()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DiagnoseWithThresholds(s, th)
	}
}
