package gogroutine

import (
	"sync"
	"sync/atomic"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// ============================================================================
// 指标监控接口 - 支持 Prometheus 等监控系统
// ============================================================================

// Metrics 指标接口（可注入 Prometheus 等实现）。
type Metrics interface {
	// IncRunning 增加运行中任务数
	IncRunning(name string)
	// DecRunning 减少运行中任务数
	DecRunning(name string)
	// ObserveTaskDuration 记录任务耗时
	ObserveTaskDuration(name string, duration time.Duration)
	// IncPanic 增加 panic 计数
	IncPanic(name string)
	// IncFallback 增加降级计数
	IncFallback(name string)
}

// ============================================================================
// 指标管理器 - 内置计数器 + 外部采集器
// ============================================================================

// metricsManager 聚合所有指标相关状态。
// 内置计数器始终工作（用于 PoolStats），外部采集器可选注入。
type metricsManager struct {
	successCount  atomic.Int64 // 累计成功任务数
	panicCount    atomic.Int64 // 累计 panic 次数
	fallbackCount atomic.Int64 // 累计降级次数

	collector Metrics // 外部指标采集器（可选）
	mu        sync.RWMutex
}

// ============================================================================
// 全局指标管理器
// ============================================================================

var metricsMgr metricsManager

// SetMetrics 设置自定义指标采集器（可在运行时替换，用于测试或动态切换）。
func SetMetrics(m Metrics) {
	metricsMgr.mu.Lock()
	metricsMgr.collector = m
	metricsMgr.mu.Unlock()
}

// getMetrics 获取当前指标采集器（线程安全）。
func getMetrics() Metrics {
	metricsMgr.mu.RLock()
	c := metricsMgr.collector
	metricsMgr.mu.RUnlock()
	return c
}

// ============================================================================
// 池水位 Prometheus 指标（复用 dbpool.go 的 Collector 模式）
// ============================================================================
//
// 为什么必须有：协程池是吞吐天花板——池满时任务排队（Submit 阻塞/降级），
// 但进程没挂、错误率可能还没起来，只有水位指标能暴露"池饱和"。
// 与 DB/Redis 连接池同理：事前信号，不是事后报错。
var (
	descPoolRunning  = prom.NewDesc("gogroutine_pool_running", "Number of running tasks in the goroutine pool.", []string{"name"}, nil)
	descPoolCapacity = prom.NewDesc("gogroutine_pool_capacity", "Capacity of the goroutine pool.", []string{"name"}, nil)
	descPoolWaiting  = prom.NewDesc("gogroutine_pool_waiting", "Number of waiting tasks in the goroutine pool.", []string{"name"}, nil)
)

var (
	poolStatsMu       sync.Mutex
	poolStatsRegister = map[string]PoolStatsProvider{}
)

// PoolStatsProvider 池水位读取接口（poolInstance 实现，测试可 mock）。
type PoolStatsProvider interface {
	Stats() PoolStatsInfo
}

// RegisterPoolStats 登记协程池供指标采集（幂等，重复登记同名忽略）。
// name 为池名（业务命名，基数可控）。应在 newInstance 成功后调用。
func RegisterPoolStats(name string, p PoolStatsProvider) {
	if p == nil || name == "" {
		return
	}
	poolStatsMu.Lock()
	defer poolStatsMu.Unlock()
	if _, exists := poolStatsRegister[name]; exists {
		return
	}
	poolStatsRegister[name] = p
}

// UnregisterPoolStats 池释放后注销指标登记。
// 必须与 ReleasePool 配对调用：否则已释放池的条目残留，Collector 会继续
// emit 0 值序列（误导"池仍存在"且 label 永久保留）。
func UnregisterPoolStats(name string) {
	if name == "" {
		return
	}
	poolStatsMu.Lock()
	delete(poolStatsRegister, name)
	poolStatsMu.Unlock()
}

func init() {
	pkgmetrics.MustRegister(prom.CollectorFunc(collectGogroutinePoolStats))
}

func collectGogroutinePoolStats(ch chan<- prom.Metric) {
	poolStatsMu.Lock()
	pools := make(map[string]PoolStatsProvider, len(poolStatsRegister))
	for k, v := range poolStatsRegister {
		pools[k] = v
	}
	poolStatsMu.Unlock()

	for name, p := range pools {
		st := p.Stats()
		ch <- prom.MustNewConstMetric(descPoolRunning, prom.GaugeValue, float64(st.Running), name)
		ch <- prom.MustNewConstMetric(descPoolCapacity, prom.GaugeValue, float64(st.Cap), name)
		// 排队数是"池饱和"最直接的信号——yml gaugeAbove 可配阈值告警
		ch <- prom.MustNewConstMetric(descPoolWaiting, prom.GaugeValue, float64(st.Waiting), name)
	}
}
