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
