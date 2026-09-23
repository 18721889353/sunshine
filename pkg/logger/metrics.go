package logger

import "github.com/prometheus/client_golang/prometheus"

// loggerCollector 实现 prometheus.Collector 接口
// 每次 Prometheus scrape 时自动调用 GetMetrics() 获取最新值
type loggerCollector struct {
	droppedDesc     *prometheus.Desc
	routerCountDesc *prometheus.Desc
}

// NewLoggerCollector 创建 logger Prometheus Collector
// 用法:
//
//	prometheus.MustRegister(logger.NewLoggerCollector())
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

// RegisterPrometheus 注册 logger 指标到 Prometheus
// 在 initApp.go 中调用一次即可，之后 /metrics 端点自动包含 logger 指标
//
//	logger.RegisterPrometheus()
func RegisterPrometheus() {
	prometheus.MustRegister(NewLoggerCollector())
}
