package goredis

import (
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// redisOperation 记录 Redis 操作耗时（对齐网关 coupon_gateway_redis_operation_duration_seconds
// 的口径：{op, result}，result 区分 ok/error，redis.Nil 缓存 miss 计 ok）。
//
// 埋点在 requestIDHook.ProcessHook——每次命令都经过，无需业务侧改动。
var redisOperation = pkgmetrics.HistogramDefault("redis_operation_duration_seconds", "Redis operation latency on the request hot path.", []string{"op", "result"}...)

// observeRedisOperation 记录一次 Redis 命令耗时。
// redis.Nil（GET 不存在的 key）是正常缓存 miss，result 记 ok——
// 生产 miss 率高，若计 error 会把错误率打满、掩盖真实故障。
func observeRedisOperation(op string, err error, cost time.Duration) {
	result := "ok"
	if err != nil && !errors.Is(err, redis.Nil) {
		result = "error"
	}
	redisOperation.WithLabelValues(op, result).Observe(cost.Seconds())
}
