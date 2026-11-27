package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

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
