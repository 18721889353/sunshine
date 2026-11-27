package metrics

import (
	"strings"
	"testing"
)

// TestDiagnose_GaugeAbove Gauge 阈值诊断：above 规则超阈值即 warn，未超不告警。
// 这是"埋了 Gauge 但只展示不告警"的闭环测试。
func TestDiagnose_GaugeAbove(t *testing.T) {
	s := NewMetricSnapshot()
	s.Gauges["db_pool_in_use_connections"] = map[string]float64{"db=mysql": 150} // >100

	d := Diagnose(s)
	if d.Level != DiagnosisWarn {
		t.Errorf("超过 Gauge 阈值应 warn, got %v (%s)", d.Level, d.Desc)
	}
	if len(d.Details) == 0 || !strings.Contains(d.Details[0], "DB连接池在用") {
		t.Errorf("应含中文名诊断, got %v", d.Details)
	}

	// 未超阈值不告警
	s2 := NewMetricSnapshot()
	s2.Gauges["db_pool_in_use_connections"] = map[string]float64{"db=mysql": 50}
	if d2 := Diagnose(s2); d2.Level != DiagnosisOK {
		t.Errorf("未超阈值应 OK, got %v", d2.Level)
	}
}

// TestDiagnose_GaugeAboveConfigured yml 配置阈值覆盖 componentSpecs 内置默认值。

func TestDiagnose_GaugeAboveConfigured(t *testing.T) {
	// 配置 50（内置是 100）：值 60 应告警（配置更严格）
	s := NewMetricSnapshot()
	s.Gauges["db_pool_in_use_connections"] = map[string]float64{"db=mysql": 60}
	th := DiagnosisThresholds{GaugeAbove: map[string]float64{"db_pool_in_use_connections": 50}}
	if d := DiagnoseWithThresholds(s, th); d.Level != DiagnosisWarn {
		t.Errorf("配置阈值 50 时 60 应告警, got %v (%s)", d.Level, d.Desc)
	}
	// 值 60，配置阈值 100 → 不告警
	th2 := DiagnosisThresholds{GaugeAbove: map[string]float64{"db_pool_in_use_connections": 100}}
	if d := DiagnoseWithThresholds(s, th2); d.Level != DiagnosisOK {
		t.Errorf("阈值 100 时 60 不应告警, got %v", d.Level)
	}
	// 未配置的指标回落内置值（100）：50 不告警
	s2 := NewMetricSnapshot()
	s2.Gauges["db_pool_in_use_connections"] = map[string]float64{"db=mysql": 50}
	if d := DiagnoseWithThresholds(s2, DiagnosisThresholds{}); d.Level != DiagnosisOK {
		t.Errorf("内置阈值 100 时 50 不应告警, got %v", d.Level)
	}
}

// TestDiagnose_CounterOnlyError 只有 error 样本时不显示误导性的"100% 失败率"。

func TestDiagnose_CounterOnlyError(t *testing.T) {
	s := NewMetricSnapshot()
	s.Counters["panic_recovered_total"] = map[string]float64{
		"component=gin-http,result=error": 5,
	}
	d := Diagnose(s)
	if len(d.Details) != 1 {
		t.Fatalf("应有 1 条诊断, got %v", d.Details)
	}
	if strings.Contains(d.Details[0], "100%") || strings.Contains(d.Details[0], "失败 5/5") {
		t.Errorf("只有 error 样本时不应显示失败率, got %q", d.Details[0])
	}
	if !strings.Contains(d.Details[0], "累计异常 5 次") {
		t.Errorf("应显示累计异常次数, got %q", d.Details[0])
	}
}

// TestDiagnose_SentinelBlock 限流/熔断拦截必须进诊断（result=error）。
func TestDiagnose_SentinelBlock(t *testing.T) {
	s := NewMetricSnapshot()
	s.Counters["sentinel_block_total"] = map[string]float64{
		"type=flow,result=error": 20,
	}
	d := Diagnose(s)
	if d.Level != DiagnosisWarn {
		t.Errorf("限流拦截应触发 warn, got %v (%s)", d.Level, d.Desc)
	}
}
