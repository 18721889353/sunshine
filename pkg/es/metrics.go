package es

import (
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// esOperation 记录 ES 操作耗时（op, result）。
// 埋点在 BulkExecute/Search 等执行入口，业务零改动。
var esOperation = pkgmetrics.HistogramDefault("es_operation_duration_seconds", "Elasticsearch operation latency by operation and result.", []string{"op", "result"}...)

// observeES 记录一次 ES 操作耗时。
func observeES(op string, err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	esOperation.WithLabelValues(op, result).Observe(cost.Seconds())
}
