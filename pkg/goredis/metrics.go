package goredis

import (
	"errors"
	"sync"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// redisOperation 记录 Redis 操作耗时（对齐网关 coupon_gateway_redis_operation_duration_seconds
// 的口径：{op, result}，result 区分 ok/error，redis.Nil 缓存 miss 计 ok）。
//
// 埋点在 requestIDHook.ProcessHook——每次命令都经过，无需业务侧改动。
var redisOperation = pkgmetrics.HistogramDefault("redis_operation_duration_seconds", "Redis operation latency on the request hot path.", []string{"op", labelResult}...)

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

// ==================== 连接池水位（复用 dbpool.go 的 Collector 模式）====================
//
// 为什么必须有：连接池耗尽是"事前无信号"故障——请求堆积、超时，
// 但错误率可能还没起来。WaitCount/Timeouts 是最早的预警（与 DB 连接池同理）。
//
// 注意：redisotel.InstrumentMetrics（o.enableMetrics）采集的是 OTel 指标，
// 走 OTLP 导出体系——指标报告器读 Prometheus registry 看不到，
// 故此处独立注册 Prometheus Collector，两套体系并存不冲突（指标名不同）。
// label 常量（避免 goconst 多处重复告警）。
const (
	labelPool   = "pool"
	labelResult = "result"
	// poolResultError 连接池等待类指标固定 result 值（等待=压力信号，进诊断）
	poolResultError = "error"
)

var (
	descPoolTotalConns = prom.NewDesc("redis_pool_total_conns", "Number of total connections in the Redis pool.", []string{labelPool}, nil)
	descPoolIdleConns  = prom.NewDesc("redis_pool_idle_conns", "Number of idle connections in the Redis pool.", []string{labelPool}, nil)
	descPoolWaitCount  = prom.NewDesc("redis_pool_wait_count_total", "Number of times a Redis connection was waited for.", []string{labelPool, labelResult}, nil)
	descPoolWaitTime   = prom.NewDesc("redis_pool_wait_duration_seconds_total", "Total time spent waiting for a Redis connection.", []string{labelPool, labelResult}, nil)
	descPoolTimeouts   = prom.NewDesc("redis_pool_timeouts_total", "Number of Redis connection wait timeouts.", []string{labelPool, labelResult}, nil)
)

var (
	poolMu       sync.Mutex
	poolRegister = map[string]UniversalPoolStats{}
)

// UniversalPoolStats 抽象 PoolStats 读取（与 UniversalClient.PoolStats 签名一致）。
type UniversalPoolStats interface {
	PoolStats() *redis.PoolStats
}

// RegisterPoolStats 登记 Redis 客户端连接池供指标采集（幂等，重复登记同名忽略）。
// name 用于 label 区分（如 "default"）。应在 setupClient 成功后调用。
func RegisterPoolStats(name string, c UniversalPoolStats) {
	if c == nil {
		return
	}
	poolMu.Lock()
	defer poolMu.Unlock()
	if _, exists := poolRegister[name]; exists {
		return
	}
	poolRegister[name] = c
}

func init() {
	pkgmetrics.MustRegister(prom.CollectorFunc(collectRedisPoolStats))
}

func collectRedisPoolStats(ch chan<- prom.Metric) {
	poolMu.Lock()
	clients := make(map[string]UniversalPoolStats, len(poolRegister))
	for k, v := range poolRegister {
		clients[k] = v
	}
	poolMu.Unlock()

	for name, c := range clients {
		st := c.PoolStats()
		if st == nil {
			continue
		}
		// 累计值直接 emit（不能 CounterVec.Add，每次 Gather 会虚高——与 DB 连接池同坑）
		ch <- prom.MustNewConstMetric(descPoolTotalConns, prom.GaugeValue, float64(st.TotalConns), name)
		ch <- prom.MustNewConstMetric(descPoolIdleConns, prom.GaugeValue, float64(st.IdleConns), name)
		ch <- prom.MustNewConstMetric(descPoolWaitCount, prom.CounterValue, float64(st.WaitCount), name, poolResultError)
		ch <- prom.MustNewConstMetric(descPoolWaitTime, prom.CounterValue, float64(st.WaitDurationNs)/1e9, name, poolResultError)
		ch <- prom.MustNewConstMetric(descPoolTimeouts, prom.CounterValue, float64(st.Timeouts), name, poolResultError)
	}
}
