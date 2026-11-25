package cache

import (
	"errors"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// 统一口径设计（大厂标准）：
//   - 耗时指标 result 与全仓一致（ok/error）；
//   - 命中率拆到独立 counter（cacheHitTotal/cacheMissTotal），避免 hit/miss
//     混进 result 后导致 `result!="ok"` 告警把 Get 命中误判为异常。
//
// 命中率 = hit / (hit + miss)，按 op 维度下钻。
var (
	cacheOperationDuration = pkgmetrics.HistogramDefault("cache_operation_duration_seconds", "Cache operation latency by operation and result (ok/error).", []string{"op", "result"}...)

	cacheHitTotal = pkgmetrics.Counter("cache_hit_total", "Cache read hits by operation.", []string{"op"}...)

	cacheMissTotal = pkgmetrics.Counter("cache_miss_total", "Cache read misses by operation (normal, not an error).", []string{"op"}...)
)

// observeCache 记录一次缓存操作耗时（result=ok/error，统一口径）。
func observeCache(op, result string, cost time.Duration) {
	cacheOperationDuration.WithLabelValues(op, result).Observe(cost.Seconds())
}

// observeCacheRead 记录一次读操作的命中/未命中计数。
//   - nil → hit（命中）
//   - redis.Nil / ErrPlaceholder → miss（key 不存在，正常结果）
//   - 真实错误（连接失败等）→ 不计入 hit/miss（耗时指标已记 error）
func observeCacheRead(op string, err error) {
	switch {
	case err == nil:
		cacheHitTotal.WithLabelValues(op).Inc()
	case isMissError(err):
		cacheMissTotal.WithLabelValues(op).Inc()
	}
}

// cacheGetResult 把 Get 的错误映射为统一的 ok/error：
//   - nil → ok（命中）
//   - redis.Nil / ErrPlaceholder → ok（未命中是正常结果，非故障）
//   - 其他 → error
//
// 命中/未命中的区分走 observeCacheRead（独立 counter），不进 result。
func cacheGetResult(err error) string {
	if err == nil || isMissError(err) {
		return "ok"
	}
	return "error"
}

// isMissError 判断是否为"缓存未命中"（正常结果，非故障）。
// redis.Nil = key 不存在；ErrPlaceholder = 命中占位符（业务标记数据不存在）。
func isMissError(err error) bool {
	return errors.Is(err, CacheNotFound) || errors.Is(err, ErrPlaceholder)
}
