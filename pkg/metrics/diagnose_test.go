package metrics

import (
	"strings"
	"testing"
)

// TestDiagnose_OK 正常
func TestDiagnose_OK(t *testing.T) {
	s := NewMetricSnapshot()
	s.Durations["redis_operation_duration_seconds"] = &DurationAgg{
		Count: 1000, SumSec: 2, Result: map[string]uint64{"ok": 1000},
	}
	d := Diagnose(s)
	if d.Level != DiagnosisOK {
		t.Errorf("应正常, got %v (%s)", d.Level, d.Desc)
	}
}

// TestDiagnose_ErrorRate 错误率超阈值
func TestDiagnose_ErrorRate(t *testing.T) {
	s := NewMetricSnapshot()
	// 100 样本 10% 错误 → warn（>=5%）
	s.Durations["redis_operation_duration_seconds"] = &DurationAgg{
		Count: 100, SumSec: 2, Result: map[string]uint64{"ok": 90, "error": 10},
	}
	d := Diagnose(s)
	if d.Level != DiagnosisWarn {
		t.Errorf("应警告, got %v", d.Level)
	}
	if d.Code == "" || d.Code == "ok" {
		t.Errorf("异常 code 不应为 ok, got %q", d.Code)
	}
}

// TestDiagnose_Critical 严重错误率
func TestDiagnose_Critical(t *testing.T) {
	s := NewMetricSnapshot()
	// 100 样本 30% 错误 → critical（>=20%）
	s.Durations["http_request_duration_seconds"] = &DurationAgg{
		Count: 100, SumSec: 2, Result: map[string]uint64{"ok": 70, "error": 30},
	}
	d := Diagnose(s)
	if d.Level != DiagnosisCritical {
		t.Errorf("应严重, got %v", d.Level)
	}
}

// TestDiagnose_MinSamples 样本太少不诊断
func TestDiagnose_MinSamples(t *testing.T) {
	s := NewMetricSnapshot()
	// 5 样本 100% 错误 → 样本 < 10，不诊断
	s.Durations["x_duration_seconds"] = &DurationAgg{
		Count: 5, Result: map[string]uint64{"error": 5},
	}
	d := Diagnose(s)
	if d.Level != DiagnosisOK {
		t.Errorf("样本太少应不诊断, got %v", d.Level)
	}
}

// TestDiagnose_CodeStable 同一异常 code 稳定（fastCheck 依赖）
func TestDiagnose_CodeStable(t *testing.T) {
	mk := func(errCnt uint64) Diagnosis {
		s := NewMetricSnapshot()
		s.Durations["redis_operation_duration_seconds"] = &DurationAgg{
			Count: 100, Result: map[string]uint64{"ok": 100 - errCnt, "error": errCnt},
		}
		return Diagnose(s)
	}
	d1 := mk(6) // 6% → warn(5%~20%)
	d2 := mk(8) // 8% → 仍 warn，计数变了但 code 应稳定
	if d1.Level != DiagnosisWarn || d2.Level != DiagnosisWarn {
		t.Fatalf("两者应同为 warn: %v / %v", d1.Level, d2.Level)
	}
	if d1.Code != d2.Code {
		t.Errorf("同级异常 code 应稳定: %q vs %q", d1.Code, d2.Code)
	}
}

// TestWorse 取更严重
func TestWorse(t *testing.T) {
	ok := Diagnosis{Level: DiagnosisOK, Code: "ok"}
	warn := Diagnosis{Level: DiagnosisWarn, Code: "w"}
	crit := Diagnosis{Level: DiagnosisCritical, Code: "c"}
	if worse(ok, warn).Level != DiagnosisWarn {
		t.Error("warn 应胜过 ok")
	}
	if worse(warn, crit).Level != DiagnosisCritical {
		t.Error("critical 应胜过 warn")
	}
	if worse(crit, warn).Level != DiagnosisCritical {
		t.Error("critical 应保持")
	}
}

// TestDiagnose_CounterError Counter 类静默故障应触发告警
// （etcd_token_refresh / nacos_config_callback 是 Phase 1 特意加的失败静默指标）
func TestDiagnose_CounterError(t *testing.T) {
	s := NewMetricSnapshot()
	s.Counters["etcd_token_refresh_total"] = map[string]float64{
		"result=ok":    10,
		"result=error": 2, // 刷新失败 2 次 → 静默故障，应告警
	}
	d := Diagnose(s)
	if d.Level != DiagnosisWarn {
		t.Errorf("Counter error 应触发警告, got %v", d.Level)
	}
	if d.Code != "etcd_token_refresh:counter-error" {
		t.Errorf("code 应含 counter-error, got %q", d.Code)
	}
}

// TestDiagnose_CounterNoError Counter 无 error 不告警
func TestDiagnose_CounterNoError(t *testing.T) {
	s := NewMetricSnapshot()
	s.Counters["etcd_token_refresh_total"] = map[string]float64{"result=ok": 10}
	d := Diagnose(s)
	if d.Level != DiagnosisOK {
		t.Errorf("全 ok 不应告警, got %v", d.Level)
	}
}

// TestDiagnose_MultipleIssues 多来源同级异常时，Code 取稳定先者（duration 遍历在 counter 前）
func TestDiagnose_MultipleIssues(t *testing.T) {
	s := NewMetricSnapshot()
	// redis duration warn
	s.Durations["redis_operation_duration_seconds"] = &DurationAgg{
		Count: 100, Result: map[string]uint64{"ok": 90, "error": 10},
	}
	// etcd counter error warn
	s.Counters["etcd_token_refresh_total"] = map[string]float64{"result=error": 1}
	d := Diagnose(s)
	if d.Level != DiagnosisWarn {
		t.Errorf("应警告, got %v", d.Level)
	}
	// 诊断取最严重，同级取先出现（duration 遍历在 counter 前）
	if d.Code != "redis_operation:err-rate>5%" {
		t.Errorf("同级应保留先出现的稳定 code, got %q", d.Code)
	}
}

// TestDiagnoseWithThresholds_Custom 自定义阈值生效（大厂标准：业务可覆盖基线）
func TestDiagnoseWithThresholds_Custom(t *testing.T) {
	s := NewMetricSnapshot()
	// 10% 错误率：默认(5% warn)会告警，自定义阈值(20% warn)不告警
	s.Durations["redis_operation_duration_seconds"] = &DurationAgg{
		Count: 100, Result: map[string]uint64{"ok": 90, "error": 10},
	}
	// 默认阈值 → warn
	if d := Diagnose(s); d.Level != DiagnosisWarn {
		t.Errorf("默认阈值 10%% 应 warn, got %v", d.Level)
	}
	// 自定义更宽松 → ok
	宽松 := DiagnosisThresholds{ErrRateWarn: 0.30, ErrRateCritical: 0.50}
	if d := DiagnoseWithThresholds(s, 宽松); d.Level != DiagnosisOK {
		t.Errorf("自定义阈值 30%% 应 ok, got %v", d.Level)
	}
	// 自定义更严格 → critical
	严格 := DiagnosisThresholds{ErrRateWarn: 0.01, ErrRateCritical: 0.05}
	if d := DiagnoseWithThresholds(s, 严格); d.Level != DiagnosisCritical {
		t.Errorf("自定义阈值 5%% 应 critical, got %v", d.Level)
	}
}

// TestDiagnosisThresholds_Normalize 零值字段回落默认
func TestDiagnosisThresholds_Normalize(t *testing.T) {
	partial := DiagnosisThresholds{ErrRateWarn: 0.10} // 只覆盖一个字段
	n := partial.normalize()
	if n.ErrRateWarn != 0.10 {
		t.Errorf("覆盖的字段应保留, got %v", n.ErrRateWarn)
	}
	if n.MinSamples != 10 {
		t.Errorf("未覆盖字段应回落默认 10, got %v", n.MinSamples)
	}
	if n.P99Warn != 1.0 {
		t.Errorf("未覆盖字段应回落默认 1.0, got %v", n.P99Warn)
	}
}

// TestDiagnose_MultipleDetails 多组件异常全部进 Details（不吞次要异常）
// 但 Code 仍是单一最严重项——fastCheck 变化检测语义不变。
func TestDiagnose_MultipleDetails(t *testing.T) {
	s := NewMetricSnapshot()
	// 三个 warn 项：两个 duration 错误率 + 一个 counter 错误
	s.Durations["redis_operation_duration_seconds"] = &DurationAgg{
		Count: 100, Result: map[string]uint64{"ok": 85, "error": 15}, // 15% → warn
	}
	s.Durations["http_request_duration_seconds"] = &DurationAgg{
		Count: 100, Result: map[string]uint64{"ok": 90, "error": 10}, // 10% → warn
	}
	s.Counters["etcd_token_refresh_total"] = map[string]float64{"result=error": 3} // counter warn

	d := Diagnose(s)
	if d.Level != DiagnosisWarn {
		t.Fatalf("应 warn, got %v", d.Level)
	}
	if len(d.Details) < 3 {
		t.Errorf("应收集全部 3 项异常, got %d: %v", len(d.Details), d.Details)
	}
	// Code 仍是单一值（fastCheck 兼容）
	if strings.Contains(d.Code, ",") {
		t.Errorf("Code 应保持单一稳定值, got %q", d.Code)
	}
	// 渲染应展示全部
	text := RenderDiagnosis(d)
	if !strings.Contains(text, "3 项") {
		t.Errorf("报告应展示异常项数, got %q", text)
	}
	if !strings.Contains(text, "etcd") {
		t.Errorf("报告应含 etcd 异常, got %q", text)
	}
}

// TestRenderDiagnosis_Evidence Evidence 渲染：OK 带证据也要展示，异常时信号跟在明细后。
func TestRenderDiagnosis_Evidence(t *testing.T) {
	// OK + Evidence：业务 hook 正常摘要
	ok := Diagnosis{Level: DiagnosisOK, Evidence: []string{"502=0", "副本=3"}}
	line := RenderDiagnosis(ok)
	if !strings.Contains(line, "502=0") || !strings.Contains(line, "副本=3") {
		t.Errorf("OK+Evidence 应渲染信号, got %q", line)
	}

	// 无 Evidence 的 OK：返回空（框架默认行为不变）
	if got := RenderDiagnosis(Diagnosis{Level: DiagnosisOK}); got != "" {
		t.Errorf("空 OK 应返回空, got %q", got)
	}

	// 异常 + Evidence：明细 + 信号都渲染
	crit := Diagnosis{
		Level: DiagnosisCritical, Code: "downstream_down", Desc: "下游异常",
		Details: []string{"502=180"}, Evidence: []string{"502=180", "摘除=2"},
	}
	line = RenderDiagnosis(crit)
	if !strings.Contains(line, "502=180") || !strings.Contains(line, "摘除=2") {
		t.Errorf("异常应渲染明细+信号, got %q", line)
	}
}

// TestDiagnosisHook Hook 类型契约：返回 nil 表示不接管。
func TestDiagnosisHook(t *testing.T) {
	var h DiagnosisHook = func(s *MetricSnapshot, th DiagnosisThresholds) *Diagnosis {
		if s == nil {
			t.Error("hook 应收到快照")
		}
		return &Diagnosis{Level: DiagnosisCritical, Code: "biz:custom", Desc: "业务定制", Evidence: []string{"x=1"}}
	}
	d := h(NewMetricSnapshot(), DiagnosisThresholds{})
	if d == nil || d.Code != "biz:custom" || len(d.Evidence) != 1 {
		t.Errorf("hook 返回契约不符: %+v", d)
	}
	// nil 回落语义
	var nilHook DiagnosisHook = func(*MetricSnapshot, DiagnosisThresholds) *Diagnosis { return nil }
	if nilHook(NewMetricSnapshot(), DiagnosisThresholds{}) != nil {
		t.Error("nil hook 应回落")
	}
}
