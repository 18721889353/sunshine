package gogroutine

import (
	"sync"
	"sync/atomic"
	"time"
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

//
// Stats 协程池统计信息由 PoolStats() 函数返回。
// ============================================================================
// 全局原子计数器 - 使用 atomic.Int64 类型（项目公约十二）
// ============================================================================

var (
	successCount  atomic.Int64
	panicCount    atomic.Int64
	fallbackCount atomic.Int64
)

// ============================================================================
// 全局 Metrics 管理 - 支持运行时替换（非 sync.Once）
// ============================================================================

var (
	globalMetrics   Metrics
	globalMetricsMu sync.RWMutex
)

// SetMetrics 设置自定义指标采集器（可在运行时替换，用于测试或动态切换）。
func SetMetrics(m Metrics) {
	globalMetricsMu.Lock()
	globalMetrics = m
	globalMetricsMu.Unlock()
}

// getMetrics 获取当前指标采集器（线程安全）。
func getMetrics() Metrics {
	globalMetricsMu.RLock()
	m := globalMetrics
	globalMetricsMu.RUnlock()
	return m
}
