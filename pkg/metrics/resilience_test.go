package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

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
