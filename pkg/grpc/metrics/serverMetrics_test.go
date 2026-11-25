package metrics

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"

	"github.com/18721889353/sunshine/pkg/utils"
)

func Test_srvRegisterMetrics(t *testing.T) {
	opts := []Option{
		WithCounterMetrics(prometheus.NewCounterVec(prometheus.CounterOpts{Name: "demo1"}, []string{})),
		WithGaugeMetrics(prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "demo2"}, []string{})),
		WithHistogramMetrics(prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "demo3"}, []string{})),
		WithSummaryMetrics(prometheus.NewSummaryVec(prometheus.SummaryOpts{Name: "demo4"}, []string{})),
	}
	o := defaultMetricsOptions()
	o.apply(opts...)
	srvRegisterMetrics(o)
}

func TestWithCounterMetrics(t *testing.T) {
	testData := &prometheus.CounterVec{}
	opt := WithCounterMetrics(testData)
	o := new(options)
	o.apply(opt)
	assert.Contains(t, o.counterMetrics, testData)
}

func TestWithGaugeMetrics(t *testing.T) {
	testData := &prometheus.GaugeVec{}
	opt := WithGaugeMetrics(testData)
	o := new(options)
	o.apply(opt)
	assert.Contains(t, o.gaugeMetrics, testData)
}

func TestWithHistogramMetrics(t *testing.T) {
	testData := &prometheus.HistogramVec{}
	opt := WithHistogramMetrics(testData)
	o := new(options)
	o.apply(opt)
	assert.Contains(t, o.histogramMetrics, testData)
}

func TestWithSummaryMetrics(t *testing.T) {
	testData := &prometheus.SummaryVec{}
	opt := WithSummaryMetrics(testData)
	o := new(options)
	o.apply(opt)
	assert.Contains(t, o.summaryMetrics, testData)
}

func Test_defaultMetricsOptions(t *testing.T) {
	o := defaultMetricsOptions()
	assert.NotNil(t, o)
}

func Test_metricsOptions_apply(t *testing.T) {
	testData := &prometheus.SummaryVec{}
	opt := WithSummaryMetrics(testData)
	o := defaultMetricsOptions()
	o.apply(opt)
	assert.Contains(t, o.summaryMetrics, testData)
}

func TestRegister(t *testing.T) {
	SetServerPattern("/rpc_server/metrics")
	Register(http.NewServeMux(), grpc.NewServer())
}

func TestServerHTTPService(t *testing.T) {
	serverAddr, _ := utils.GetLocalHTTPAddrPairs()
	s := ServerHTTPService(serverAddr, grpc.NewServer())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	time.Sleep(time.Millisecond * 100)
	err := s.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestStreamServerMetrics(t *testing.T) {
	metrics := StreamServerMetrics()
	assert.NotNil(t, metrics)
}

func TestUnaryServerMetrics(t *testing.T) {
	metrics := UnaryServerMetrics()
	assert.NotNil(t, metrics)
}

// TestServerMetrics_LegacyMetricNames 迁移到新库后指标描述符名必须与旧库一致，
// 否则现有 dashboard/告警规则全失效（旧库 go-grpc-prometheus 已废弃）。
//
// 注意：CounterVec 无样本时不 emit，故不依赖 Gather——直接检查 Describe 的描述符。
func TestServerMetrics_LegacyMetricNames(t *testing.T) {
	ch := make(chan *prometheus.Desc, 32)
	go func() {
		grpcServerMetrics.Describe(ch)
		close(ch)
	}()
	got := map[string]bool{}
	for d := range ch {
		// Desc.String() 形如 `Desc{fqName: "grpc_server_started_total", ...}`
		for _, name := range []string{
			"grpc_server_started_total",
			"grpc_server_handled_total",
			"grpc_server_msg_received_total",
			"grpc_server_msg_sent_total",
			"grpc_server_handling_seconds",
		} {
			if strings.Contains(d.String(), name) {
				got[name] = true
			}
		}
	}
	for _, name := range []string{
		"grpc_server_started_total",
		"grpc_server_handled_total",
		"grpc_server_msg_received_total",
		"grpc_server_msg_sent_total",
		"grpc_server_handling_seconds",
	} {
		if !got[name] {
			t.Errorf("迁移后应保留旧指标名 %q（dashboard 兼容）", name)
		}
	}
}
