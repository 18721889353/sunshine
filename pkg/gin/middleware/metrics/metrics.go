// Package metrics is gin metrics library, collect five metrics, "uptime_total", "http_request_count_total"（含统一 result label）,
// "http_request_duration_seconds", "http_request_size_bytes", "http_response_size_bytes".
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// 指标名常量：外部（守护测试/告警规则/文档）引用指标时必须用常量，
// 避免与定义处字符串双写漂移。
const (
	MetricNameUptime          = "uptime_total"
	MetricNameRequestCount    = "http_request_count_total"
	MetricNameRequestDuration = "http_request_duration_seconds"
	MetricNameRequestSize     = "http_request_size_bytes"
	MetricNameResponseSize    = "http_response_size_bytes"
)

var (
	// 统一口径：result 与全仓一致（ok/error），按 HTTP 状态码判断
	labels = []string{"status", "path", "method", "result"}

	uptime = pkgmetrics.Counter(MetricNameUptime, "HTTP service uptime, updated every minute")

	reqCount = pkgmetrics.Counter(MetricNameRequestCount, "Total number of HTTP requests made.", labels...)

	reqDuration = pkgmetrics.HistogramDefault(MetricNameRequestDuration, "HTTP request latencies in seconds.", labels...)

	reqSizeBytes = pkgmetrics.Summary(MetricNameRequestSize, "HTTP request sizes in bytes.", labels...)

	respSizeBytes = pkgmetrics.Summary(MetricNameResponseSize, "HTTP response sizes in bytes.", labels...)
)

// init registers the prometheus metrics
func initPrometheus() {
	// 指标已在 var 定义时通过 pkgmetrics.Counter/HistogramDefault/Summary
	// 自动完成双注册（Default + Fast）；这里只启动 uptime 分钟更新协程。
	go recordUptime()
}

// recordUptime increases service uptime per 1 minute.
func recordUptime() {
	for range time.Tick(time.Minute) {
		uptime.WithLabelValues().Inc()
	}
}

// calcRequestSize returns the size of request object.
func calcRequestSize(r *http.Request) float64 {
	size := 0
	if r.URL != nil {
		size = len(r.URL.String())
	}

	size += len(r.Method)
	size += len(r.Proto)

	for name, values := range r.Header {
		size += len(name)
		for _, value := range values {
			size += len(value)
		}
	}
	size += len(r.Host)

	// r.Form and r.MultipartForm are assumed to be included in r.URL.
	if r.ContentLength != -1 {
		size += int(r.ContentLength)
	}
	return float64(size)
}

// ------------------------------------------------------------------------------------------

// metricsHandler wrappers the standard http.Handler to gin.HandlerFunc
func metricsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		handler := pkgmetrics.Handler()
		handler.ServeHTTP(c.Writer, c.Request)
	}
}

// Metrics returns a gin.HandlerFunc for exporting some Web metrics
func Metrics(r *gin.Engine, opts ...Option) gin.HandlerFunc {
	o := defaultOptions()
	o.apply(opts...)

	// init prometheus
	initPrometheus()

	r.GET(o.metricsPath, metricsHandler())

	// 纯埋点：是否装配由调用方（routers）按 metrics.enabled 决定，运行时无热更新开关
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		ok := o.isIgnoreCodeStatus(c.Writer.Status()) ||
			o.isIgnorePath(c.Request.URL.Path) ||
			o.checkIgnoreMethod(c.Request.Method)
		if ok {
			return
		}

		// no response content will return -1
		respSize := c.Writer.Size()
		if respSize < 0 {
			respSize = 0
		}

		// 统一口径：result=ok/error（4xx/5xx=error）
		result := "ok"
		if c.Writer.Status() >= 400 {
			result = "error"
		}

		// 高基数防护：path 必须用路由模板（如 /users/:id）而非原始路径
		// （/users/12345 会为每个 ID 创建一条 series，撑爆 Prometheus）。
		// c.Next() 之后 FullPath 已可用；无匹配路由（404）时回退固定标签。
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		lvs := []string{strconv.Itoa(c.Writer.Status()), path, c.Request.Method, result}
		reqCount.WithLabelValues(lvs...).Inc()
		reqDuration.WithLabelValues(lvs...).Observe(time.Since(start).Seconds())
		reqSizeBytes.WithLabelValues(lvs...).Observe(calcRequestSize(c.Request))
		respSizeBytes.WithLabelValues(lvs...).Observe(float64(respSize))
	}
}
