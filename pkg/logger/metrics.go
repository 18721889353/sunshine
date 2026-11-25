package logger

import (
	"github.com/prometheus/client_golang/prometheus"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// loggerCollector 实现 prometheus.Collector 接口
// 每次 Prometheus scrape 时自动调用 GetMetrics() 获取最新值
type loggerCollector struct {
	droppedDesc     *prometheus.Desc
	routerCountDesc *prometheus.Desc
}

// NewLoggerCollector 创建 logger Prometheus Collector
// 用法（统一注册中心，Default + Fast 双注册）:
//
//	pkgmetrics.MustRegister(logger.NewLoggerCollector())
func NewLoggerCollector() prometheus.Collector {
	return &loggerCollector{
		droppedDesc: prometheus.NewDesc(
			"logger_dropped_entries_total",
			"日志系统异步缓冲区满时丢弃的日志条数",
			nil, nil,
		),
		routerCountDesc: prometheus.NewDesc(
			"logger_router_active_loggers",
			"日志路由器中活跃的 logger 数量",
			nil, nil,
		),
	}
}

// Describe 实现 prometheus.Collector 接口
func (c *loggerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.droppedDesc
	ch <- c.routerCountDesc
}

// Collect 实现 prometheus.Collector 接口
// 每次 Prometheus scrape 时调用，返回当前指标快照
func (c *loggerCollector) Collect(ch chan<- prometheus.Metric) {
	m := GetMetrics()

	ch <- prometheus.MustNewConstMetric(
		c.droppedDesc,
		prometheus.CounterValue,
		float64(m.DroppedEntries),
	)
	ch <- prometheus.MustNewConstMetric(
		c.routerCountDesc,
		prometheus.GaugeValue,
		float64(m.RouterLoggerCount),
	)
}

// loggerCollectorInstance 包级单例：init 与兼容入口共用，避免创建被幂等丢弃的重复实例。
var loggerCollectorInstance = NewLoggerCollector()

// init 自动注册 logger 指标到统一中心（Default + Fast）。
//
// 历史问题：原实现只暴露 RegisterPrometheus() 供业务手动调用，但全仓无调用点——
// logger_dropped_entries_total / logger_router_active_loggers 实际从未被注册。
// 改为 init 自动注册，与其他 12 个组件的指标行为一致，5s 巡检也能采到。
func init() {
	pkgmetrics.MustRegister(loggerCollectorInstance)
}
