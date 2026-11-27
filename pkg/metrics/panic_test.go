package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestObservePanic panic 必须记为 result=error，且 component 区分来源。
func TestObservePanic(t *testing.T) {
	beforeGrpc := testutil.ToFloat64(panicRecovered.WithLabelValues("grpc-unary-server", "error"))
	beforeGin := testutil.ToFloat64(panicRecovered.WithLabelValues("gin-http", "error"))

	ObservePanic("grpc-unary-server")
	ObservePanic("gin-http")

	if got := testutil.ToFloat64(panicRecovered.WithLabelValues("grpc-unary-server", "error")); got != beforeGrpc+1 {
		t.Errorf("grpc panic counter 应 +1, got %v", got)
	}
	if got := testutil.ToFloat64(panicRecovered.WithLabelValues("gin-http", "error")); got != beforeGin+1 {
		t.Errorf("gin panic counter 应 +1, got %v", got)
	}
}

// TestObserveTimeoutAndRetry 超时/重试指标：超时记 error，重试独立计数。
func TestObserveTimeoutAndRetry(t *testing.T) {
	beforeTO := testutil.ToFloat64(requestTimeout.WithLabelValues("grpc-unary-client", "error"))
	beforeRT := testutil.ToFloat64(requestRetry.WithLabelValues("grpc-unary-client", "error"))

	ObserveTimeout("grpc-unary-client")
	ObserveRetry("grpc-unary-client")

	if got := testutil.ToFloat64(requestTimeout.WithLabelValues("grpc-unary-client", "error")); got != beforeTO+1 {
		t.Errorf("timeout counter 应 +1, got %v", got)
	}
	if got := testutil.ToFloat64(requestRetry.WithLabelValues("grpc-unary-client", "error")); got != beforeRT+1 {
		t.Errorf("retry counter 应 +1, got %v", got)
	}
}

// TestDiagnose_NewResilienceCounters 验证新 resilience 指标能进诊断引擎——
// "埋了但不告警"是静默失效：retry 之前没有 result label，永远不触发。
func TestDiagnose_NewResilienceCounters(t *testing.T) {
	s := NewMetricSnapshot()
	s.Counters["request_retry_total"] = map[string]float64{"component=grpc-unary-client,result=error": 5}
	s.Counters["panic_recovered_total"] = map[string]float64{"component=gin-http,result=error": 2}

	d := Diagnose(s)
	if d.Level != DiagnosisWarn {
		t.Errorf("有 error 样本的 Counter 应触发 warn, got %v (%s)", d.Level, d.Desc)
	}
	if len(d.Details) < 2 {
		t.Errorf("两个异常 Counter 都应在 details 中, got %v", d.Details)
	}
}

// TestComponentSpecs_NewMetrics 验证所有新指标的 base 都在 componentSpecs——
// 否则报告渲染走【其他】兜底，无中文名/分组（数据不丢但可读性差）。
func TestComponentSpecs_NewMetrics(t *testing.T) {
	names := []string{
		"panic_recovered_total", "request_timeout_total", "request_retry_total",
		"kafka_publish_duration_seconds", "kafka_consume_duration_seconds",
		"db_pool_open_connections", "db_pool_in_use_connections",
		"db_pool_wait_count_total", "db_pool_wait_duration_seconds_total",
		"gows_local_connections", "gows_remote_uids",
	}
	for _, name := range names {
		if _, ok := componentSpecs[metricBase(name)]; !ok {
			t.Errorf("指标 %s 的 base=%s 未在 componentSpecs 登记", name, metricBase(name))
		}
	}
}

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

// TestConsistency_MetricsInComponentSpecs 所有自定义指标的 base 都必须在
// componentSpecs 登记——报告渲染有中文名/分组、诊断 componentLabel 有中文。
// 跨文件一致性的自动校验：指标注册名与展示规格表在多轮修改中容易失配。
func TestConsistency_MetricsInComponentSpecs(t *testing.T) {
	fams, err := Default().GatherDefault()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, f := range fams {
		base := metricBase(f.GetName())
		if len(base) >= 3 && base[:3] == "go_" {
			continue // 运行时指标走 runtime 分组，不需登记
		}
		if len(base) >= 8 && base[:8] == "process_" || base == "promhttp" {
			continue // 进程级/HTTP 库指标，与 go_* 同类
		}
		if strings.Contains(base, "test") {
			continue // 其他测试注册的指标（测试污染 GatherDefault，非生产指标）
		}
		if _, ok := componentSpecs[base]; !ok {
			t.Errorf("指标 %s 的 base=%s 未在 componentSpecs 登记", f.GetName(), base)
		}
	}
}
