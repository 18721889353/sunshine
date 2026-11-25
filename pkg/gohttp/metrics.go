package gohttp

import (
	"strconv"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// httpClientDuration 记录 HTTP 出站请求耗时（method, status, result）。
//
// 大厂标准：HTTP 指标必带 status（Prometheus/OTel 三件套 method+path+status）——
// status 是协议事实维度，排障需按 404/500/502/429 区分错误类型；
// result 是统一口径（ok/error），由 status 派生（>=400=error，0=transport 失败）。
// 与 gin 接入层指标对称。埋点在 do() 的 defer，业务零改动。
var httpClientDuration = pkgmetrics.HistogramDefault("http_client_duration_seconds", "HTTP client request latency by method, status code and result.", []string{"method", "status", "result"}...)

// observeHTTPClient 记录一次 HTTP 出站耗时。
// status 为 HTTP 状态码（transport 失败无状态码记 0）；
// result 统一口径：4xx/5xx/transport 失败均记 error，2xx/3xx 记 ok。
func observeHTTPClient(method string, status int, cost time.Duration) {
	result := "ok"
	if status == 0 || status >= 400 {
		result = "error"
	}
	httpClientDuration.WithLabelValues(method, strconv.Itoa(status), result).Observe(cost.Seconds())
}
