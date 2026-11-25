package query

import (
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// queryBuildDuration 记录查询条件构建耗时（result）。
//
// 注意：这是【条件构造器】的埋点，不执行 SQL——SQL 执行耗时由
// pkg/sgorm/glog.Trace 覆盖（gorm logger 回调，Find/First/Take/Raw/Exec 都触发）。
// 此处观测的是业务传参错误率（checkValid/convert 失败），是入参质量问题的信号。
var queryBuildDuration = pkgmetrics.HistogramDefault("query_build_duration_seconds", "Query condition build latency by result (ok/error).", []string{"result"}...)

// observeQueryBuild 记录一次条件构建耗时。
func observeQueryBuild(err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	queryBuildDuration.WithLabelValues(result).Observe(cost.Seconds())
}
