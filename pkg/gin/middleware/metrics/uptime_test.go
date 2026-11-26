package metrics

import (
	"runtime"
	"testing"
)

// TestStartUptimeCollector_Idempotent startUptimeCollector 必须幂等：
// Metrics() 是中间件工厂，多引擎/测试重复调用若每次都起协程，
// uptime（累计分钟数）会翻倍计数且协程泄漏。
func TestStartUptimeCollector_Idempotent(t *testing.T) {
	before := runtime.NumGoroutine()
	startUptimeCollector()
	startUptimeCollector()
	startUptimeCollector()
	after := runtime.NumGoroutine()
	// 至多新增 1 个 uptime 协程（实际可能因调度未立即体现，不小于 before）
	if after-before > 1 {
		t.Errorf("多次调用只应启动 1 个协程, 新增 %d 个", after-before)
	}
}
