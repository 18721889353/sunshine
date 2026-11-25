package nacoscli

import (
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// nacosConfigFetch 记录配置拉取耗时（result）。
//
// 为什么必须有：配置拉取失败是【静默故障】——服务会继续用旧配置跑，
// 不加指标就发现不了"配置中心挂了但服务还活着"。
var (
	nacosConfigFetch = pkgmetrics.HistogramDefault("nacos_config_fetch_duration_seconds", "Nacos config fetch latency by result.", []string{"result"}...)

	// nacosConfigCallback 记录配置变更回调结果（counter）。
	// 回调 panic 是静默故障——配置已变更但业务没生效，无指标发现不了。
	nacosConfigCallback = pkgmetrics.Counter("nacos_config_callback_total", "Nacos config change callback attempts by result.", []string{"result"}...)
)

// observeNacosConfigFetch 记录一次配置拉取耗时。
func observeNacosConfigFetch(err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	nacosConfigFetch.WithLabelValues(result).Observe(cost.Seconds())
}

// observeNacosConfigCallback 记录一次配置变更回调结果（panic 记 error）。
func observeNacosConfigCallback(panicked bool) {
	result := "ok"
	if panicked {
		result = "error"
	}
	nacosConfigCallback.WithLabelValues(result).Inc()
}
