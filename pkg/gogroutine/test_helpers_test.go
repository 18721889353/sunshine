package gogroutine

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 本文件集中存放跨测试文件共享的 helper（遵循 package-quality-baseline 交付物矩阵）。
// 文件名必须带 `_test.go` 后缀：本仓 golangci-lint 的 run.tests=false，
// 不带后缀的 helper 文件会被 unused 误报（见 skill 第八节）。

// taskWaitTimeout 等待异步任务完成信号的上界。
//   - 为何需要上界：任务完成靠 channel 信号判定，超时只是防御 goroutine 泄漏时测试挂死；
//   - 取值依据：5s 远大于本包任务实际耗时（微秒级），且远小于 Go 测试默认超时；
//   - 为何不 flaky：结果判定始终是「收到信号才算通过」，超时直接 Fatal，不存在"睡够久再看"的读数依赖。
const taskWaitTimeout = 5 * time.Second

// waitDone 等待任务完成信号，超时即 t.Fatalf（不是 Skip：超时是被测代码的问题）。
func waitDone(t *testing.T, done <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(taskWaitTimeout):
		t.Fatalf("%s: 等待超过 %s 仍未完成", msg, taskWaitTimeout)
	}
}

// conditionPollInterval waitUntil 的轮询间隔。
// 取值依据：相对 5s 上界最多空转 ~2500 次、开销可忽略；又远小于任务耗时，命中延迟不掩盖问题。
const conditionPollInterval = 2 * time.Millisecond

// waitUntil 轮询等待条件成立，超时即 t.Fatalf（附最后一次读数，便于定位）。
//   - 为何轮询而非固定 sleep：条件何时成立取决于任务调度时机，固定窗口只能「大概率」覆盖，
//     是 flaky 的经典来源；轮询命中即返回，既快又确定。
//   - 取值依据：上界复用 taskWaitTimeout（5s，远大于本包任务实际耗时），间隔 2ms。
//   - 为何不 flaky：结果由「条件成立」或「超时 Fatal」二分判定，超时是被测代码的问题
//     （与 waitDone 同语义），不存在「睡够了没有」的灰区。
func waitUntil(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(taskWaitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: 等待超过 %s 条件仍未成立", msg, taskWaitTimeout)
		}
		time.Sleep(conditionPollInterval)
	}
}

// negativeAssertionWindow 负向断言（验证「不会发生」）的观察窗口。
//   - 为何只能固定等待：「不发生」没有完成信号可监听，只能给足窗口后直读一次；
//   - 取值依据：100ms 是数量级余量而非实测值——负向场景的被测路径是「跳过提交」，
//     任务若被错误受理会立即开始执行并置位标志；本轮未做窗口敏感性实验；
//   - 为何不 flaky：窗口只影响漏报概率，不产生假阳性——窗口内未执行即通过，
//     而错误受理的置位发生在微秒级，100ms 内必被观测到。
const negativeAssertionWindow = 100 * time.Millisecond

// taskStartPad 提交后给任务调度起跑的让路窗口（仅用于「释放时任务应已入队/在跑」的时序构造）。
// 为何固定：任务起跑无公开信号，但可轮询 PoolStats().Running 替代的场景应优先用 waitUntil；
// 本常量只用于释放类用例中「不关心是否已跑、只保证不 panic」的防御性让路，
// 结果判定不依赖它（超时/不阻塞由被测函数自身行为断言），故不会因而 flaky。
const taskStartPad = 10 * time.Millisecond

// simulatedTaskWork 任务体内的模拟耗时，制造并发重叠窗口（如「任务运行中去 Release」）。
// 取值依据：10ms 相对 5s 上界足够产生交叠，又不至于拖慢单测总时长；
// 不参与任何结果判定——任务完成信号一律由 channel/WaitGroup/waitUntil 捕获，故不会因而 flaky。
const simulatedTaskWork = 10 * time.Millisecond

// overlapPad 高频并发用例里的微交叠让路（任务体微工作/释放方延迟起跑）。
// 为何是 1ms：这类用例循环上百次，窗口只需「足以让 goroutine 交叠」，取大只会线性拉慢单测；
// 不参与结果判定（断言只看计数器终值或并发安全不 panic），故不会因而 flaky。
const overlapPad = time.Millisecond

// resetForTest 重置全局状态，确保测试隔离。
// globalPool 已由 pool.go 的 globalPoolMu 保护（修复并发竞争），
// 此处的读写同样持锁，保持「受锁字段永不裸访问」的一致性；
// 调用方均为串行的 Test/Benchmark，加锁是防御未来 t.Parallel 化而非解除已知竞争。
func resetForTest() {
	// 释放旧池
	globalPoolMu.Lock()
	p := globalPool
	globalPool = nil
	globalPoolMu.Unlock()
	if p != nil {
		p.Release()
	}

	// 重置 sync.Once（重新赋值为零值）
	globalPoolOnce = sync.Once{}

	// 重置全局指标管理器
	resetMetrics()
}

// resetMetrics 重置全局指标状态（仅用于测试）。
func resetMetrics() {
	metricsMgr.successCount.Store(0)
	metricsMgr.panicCount.Store(0)
	metricsMgr.fallbackCount.Store(0)
	metricsMgr.mu.Lock()
	metricsMgr.collector = nil
	metricsMgr.mu.Unlock()
}

// mockMetrics 用于测试的指标采集器。
type mockMetrics struct {
	running    atomic.Int64
	panicCount atomic.Int64
	fallback   atomic.Int64
	durations  atomic.Int64 // ObserveTaskDuration 触发次数（P1-3 实例池耗时观测守护）
}

func (m *mockMetrics) IncRunning(_ string) { m.running.Add(1) }
func (m *mockMetrics) DecRunning(_ string) { m.running.Add(-1) }
func (m *mockMetrics) ObserveTaskDuration(_ string, _ time.Duration) {
	m.durations.Add(1)
}
func (m *mockMetrics) IncPanic(_ string)    { m.panicCount.Add(1) }
func (m *mockMetrics) IncFallback(_ string) { m.fallback.Add(1) }
