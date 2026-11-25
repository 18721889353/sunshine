package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ============================================================================
// 统一 metrics HTTP exposition 入口（大厂标准：单一出口）。
//
// 问题背景：gin middleware、grpc client/server 各自直接 promhttp.Handler()，
// 暴露逻辑散落 3 处——未来加认证/限流/超时/统一 HandlerOpts 都要改多处。
//
// 关键认知：promhttp.Handler() 默认采集 prometheus.DefaultGatherer，
// 而本项目所有指标都注册到 DefaultRegisterer（统一注册中心），
// 所以【数据路径本就统一】，这里统一的是【暴露入口】，不是数据源。
// ============================================================================

var (
	// defaultHandler 暴露全部指标（含 go_* 运行时），供常规 /metrics 端点。
	// 单例复用：HandlerOpts 相同，无需每次请求重建。
	defaultHandlerOnce sync.Once
	defaultHandler     http.Handler

	// fastHandler 只暴露自定义指标（无 go_*），供 5s 巡检/内部诊断端点。
	// 注意：Fast Registry 不含 go_memstats_*，避免巡检触发 runtime.ReadMemStats STW。
	fastHandlerOnce sync.Once
	fastHandler     http.Handler
)

// Handler 返回暴露【全部指标】的 HTTP handler（Default Registry，含 go_* 运行时）。
// 供 /metrics 端点使用，等价于原 promhttp.Handler()，但走统一入口。
func Handler() http.Handler {
	defaultHandlerOnce.Do(func() {
		defaultHandler = promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{})
	})
	return defaultHandler
}

// FastHandler 返回只暴露【业务自定义指标】的 HTTP handler（Fast Registry，无 go_*）。
// 供 5s 巡检/内部诊断端点使用——不触发 runtime STW。
//
// 注意：默认 /metrics 端点应继续用 Handler()（含运行时指标）；
// FastHandler 是给需要"轻量采集"的场景额外开的端点。
func FastHandler() http.Handler {
	fastHandlerOnce.Do(func() {
		fastHandler = promhttp.HandlerFor(Default().Fast(), promhttp.HandlerOpts{})
	})
	return fastHandler
}
