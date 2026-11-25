package metrics

import (
	"net/http"
	"sync"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"google.golang.org/grpc"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

var (
	// client side default router
	clientPattern = "/rpc_client/metrics"

	// initialize the client's default metrics
	// 与 server 侧对称：启用 handling time histogram（grpc_client_handling_seconds），
	// 客户端调用耗时分布与服务端一致可查（componentSpecs 有对应展示条目）。
	grpcClientMetrics = grpc_prometheus.NewClientMetrics(grpc_prometheus.WithClientHandlingTimeHistogram())

	cliOnce sync.Once
)

func cliRegisterMetrics() {
	cliOnce.Do(func() {
		// 统一注册中心（删除旧的 cliReg 独立 registry）
		// grpcClientConnectionGauge：客户端活跃连接数（与 server 侧对称，
		// 值由 NewClientConnCountingHandler 的 stats 回调维护）
		pkgmetrics.MustRegister(grpcClientMetrics, grpcClientConnectionGauge)
	})
}

// SetClientPattern set the client pattern
func SetClientPattern(pattern string) {
	if pattern != "" {
		clientPattern = pattern
	}
}

// ClientRegister for http routing and grpc methods
func ClientRegister(mux *http.ServeMux) {
	// register for http routing
	mux.Handle(clientPattern, pkgmetrics.Handler())
}

// ClientHTTPService initialize the client's prometheus exporter service and use http://ip:port/metrics to fetch data
func ClientHTTPService(addr string) *http.Server {
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

	return httpServer
}

// ---------------------------------- client interceptor ----------------------------------

// UnaryClientMetrics metrics unary interceptor
func UnaryClientMetrics() grpc.UnaryClientInterceptor {
	cliRegisterMetrics()
	return grpcClientMetrics.UnaryClientInterceptor()
}

// StreamClientMetrics metrics stream interceptor
func StreamClientMetrics() grpc.StreamClientInterceptor {
	cliRegisterMetrics()
	return grpcClientMetrics.StreamClientInterceptor()
}
