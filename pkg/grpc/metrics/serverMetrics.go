// Package metrics is grpc's server-side and client-side metrics can continue to be captured using prometheus.
package metrics

import (
	"net/http"
	"sync"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"google.golang.org/grpc"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// https://github.com/grpc-ecosystem/go-grpc-middleware/tree/master/providers/prometheus

var (
	// server side default router
	serverPattern = "/metrics"

	// initialize server-side default metrics
	grpcServerMetrics = grpc_prometheus.NewServerMetrics(grpc_prometheus.WithServerHandlingTimeHistogram())

	// go metrics
	goMetrics = collectors.NewGoCollector()

	grpcConnectionGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "grpc_server_active_connections",
			Help: "Current number of active gRPC client connections",
		},
	)

	srvOnce sync.Once
)

// Option set metrics
type Option func(*options)

// options 承载业务扩展指标（Option 纯函数写入此处，不做包级全局副作用——
// 包级 slice 无锁 append 在并发 UnaryServerMetrics 时会 race）。
type options struct {
	counterMetrics   []*prometheus.CounterVec
	summaryMetrics   []*prometheus.SummaryVec
	gaugeMetrics     []*prometheus.GaugeVec
	histogramMetrics []*prometheus.HistogramVec
}

func defaultMetricsOptions() *options {
	return &options{}
}

func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithCounterMetrics add Counter type indicator
func WithCounterMetrics(metrics ...*prometheus.CounterVec) Option {
	return func(o *options) {
		o.counterMetrics = append(o.counterMetrics, metrics...)
	}
}

// WithSummaryMetrics add Summary type indicator
func WithSummaryMetrics(metrics ...*prometheus.SummaryVec) Option {
	return func(o *options) {
		o.summaryMetrics = append(o.summaryMetrics, metrics...)
	}
}

// WithGaugeMetrics add Gauge type indicator
func WithGaugeMetrics(metrics ...*prometheus.GaugeVec) Option {
	return func(o *options) {
		o.gaugeMetrics = append(o.gaugeMetrics, metrics...)
	}
}

// WithHistogramMetrics adding Histogram type indicators
func WithHistogramMetrics(metrics ...*prometheus.HistogramVec) Option {
	return func(o *options) {
		o.histogramMetrics = append(o.histogramMetrics, metrics...)
	}
}

// srvRegisterMetrics 注册基础指标与业务扩展指标（sync.Once 保证幂等）。
func srvRegisterMetrics(o *options) {
	srvOnce.Do(func() {
		// 统一注册中心（删除旧的 srvReg 独立 registry）：
		//   - goMetrics（GoCollector）只进 Default——巡检读 go_memstats 会触发
		//     runtime STW 停顿，不能进 Fast。注意 prometheus 默认注册器 init 时
		//     已自动注册 GoCollector，此处显式注册是防御性表达意图，幂等保证无害；
		//   - 其余 gRPC 指标进 Default + Fast，/metrics 与 5s 巡检都能采到。
		pkgmetrics.MustRegisterDefaultOnly(goMetrics)
		pkgmetrics.MustRegister(grpcServerMetrics, grpcConnectionGauge)
		for _, metric := range o.counterMetrics {
			pkgmetrics.MustRegister(metric)
		}
		for _, metric := range o.summaryMetrics {
			pkgmetrics.MustRegister(metric)
		}
		for _, metric := range o.gaugeMetrics {
			pkgmetrics.MustRegister(metric)
		}
		for _, metric := range o.histogramMetrics {
			pkgmetrics.MustRegister(metric)
		}
	})
}

// SetServerPattern set the server pattern
func SetServerPattern(pattern string) {
	if pattern != "" {
		serverPattern = pattern
	}
}

// Register for http routing and grpc methods
func Register(mux *http.ServeMux, grpcServer *grpc.Server) {
	// 统一从 Default Registry 暴露（gRPC 指标已注册进去，/metrics 端点可拉到）
	mux.Handle(serverPattern, pkgmetrics.Handler())

	// register all gRPC methods to metrics
	grpcServerMetrics.InitializeMetrics(grpcServer)
}

// ServerHTTPService initialize the prometheus exporter service on the server side and fetch data using http://ip:port/metrics
func ServerHTTPService(addr string, grpcServer *grpc.Server) *http.Server {
	httpServer := &http.Server{
		Addr:    addr,
		Handler: pkgmetrics.Handler(),
	}

	// run http server
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic("listen and serve error: " + err.Error())
		}
	}()

	// initialize gRPC methods Metrics
	grpcServerMetrics.InitializeMetrics(grpcServer)

	return httpServer
}

// ---------------------------------- server interceptor ----------------------------------

// UnaryServerMetrics metrics unary interceptor
func UnaryServerMetrics(opts ...Option) grpc.UnaryServerInterceptor {
	o := defaultMetricsOptions()
	o.apply(opts...)
	srvRegisterMetrics(o)
	return grpcServerMetrics.UnaryServerInterceptor()
}

// StreamServerMetrics metrics stream interceptor
func StreamServerMetrics(opts ...Option) grpc.StreamServerInterceptor {
	o := defaultMetricsOptions()
	o.apply(opts...)
	srvRegisterMetrics(o)
	return grpcServerMetrics.StreamServerInterceptor()
}
