package dlock

import (
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// lockAcquireDuration 记录分布式锁获取耗时（backend, result）。
// 埋点在 RedisLock/EtcdLock 的 Lock/TryLock 统一入口。
var lockAcquireDuration = pkgmetrics.HistogramDefault("lock_acquire_duration_seconds", "Distributed lock acquire latency by backend and result.", []string{"backend", "result"}...)

// observeLockAcquire 记录一次锁获取耗时。
func observeLockAcquire(backend string, err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	lockAcquireDuration.WithLabelValues(backend, result).Observe(cost.Seconds())
}
