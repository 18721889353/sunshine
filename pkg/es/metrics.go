package es

import (
	"context"
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

// esConnected ES 连接健康状态（1=可达，0=不可达）。
//
// 为什么必须有：连接断开时只有操作失败（es_operation_duration error）能间接反映，
// 且错误率可能被正常流量稀释。独立的健康探针给出明确的"连通/不连通"信号。
var esConnected = pkgmetrics.Gauge("es_connected", "Elasticsearch connectivity (1=reachable, 0=unreachable).", "cluster")

// ObserveESConnected 记录 ES 连通性。
func ObserveESConnected(reachable bool) {
	v := 0.0
	if reachable {
		v = 1.0
	}
	esConnected.WithLabelValues("default").Set(v)
}

// StartHealthMonitor 定期 Ping ES 并记录连通性（30s 一次）。
// 需要在客户端初始化成功后调用；ctx 取消时退出。
func StartHealthMonitor(ctx context.Context, client *Client, interval time.Duration) {
	if client == nil || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := client.Ping(pingCtx)
				cancel()
				ObserveESConnected(err == nil)
			}
		}
	}()
}
