package gocron

import (
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/18721889353/sunshine/pkg/logger"
)

// 测试等待常量。
// 本包是纯进程内调度（无外部服务），结果判定一律「轮询 + deadline」，
// 固定等待只用于构造观察窗口，预热不足只会让断言晚到，不会误判。
const (
	// taskScheduleInterval 测试任务的调度间隔。
	// 取值依据：500ms 足够短使单测总时长可控，又足够长让「暂停后不应执行」
	// 的观察窗口（2 个周期）不至于因调度抖动误判。
	taskScheduleInterval = 500 * time.Millisecond

	// pauseObserveWindow 暂停后确认任务不再执行的观察窗口 = 2 个调度周期。
	// 为何必须固定等待：暂停没有「已生效」信号，只能等过原定触发点。
	// 为何不 flaky：断言是 before/after 计数相等，观察窗口越长只会越保守。
	pauseObserveWindow = 2 * taskScheduleInterval

	// drainWindow 暂停瞬间的收尾窗口，容纳 Pause 前已异步启动的 job 执行完毕。
	// 取值依据：job 体只做原子自增，100ms 是 goroutine 调度的宽裕余量。
	drainWindow = 100 * time.Millisecond

	// conditionTimeout 轮询判定的统一上界 = 10 个调度周期。
	// 为何不 flaky：回调最晚在一个调度周期后到达，10 倍是宽裕余量；
	// 超时即 t.Fatalf，暴露真实问题而非静默通过。
	conditionTimeout = 10 * taskScheduleInterval

	// pollInterval 轮询间隔。10ms 相对 500ms 调度周期可忽略，判定精度足够。
	pollInterval = 10 * time.Millisecond

	// shutdownObserveWindow Shutdown 排空的观察/超时窗口。
	// 为何固定：Shutdown 的等待由外部 ctx 控制，测试需要一个确定的上界来构造
	// 「任务不释放则必然超时」与「任务不应提前返回」两种断言。
	// 取值依据：100ms 小于一个调度周期，只影响阻塞任务是否再触发一次，不影响结论。
	// 为何不 flaky：超时错误由 ctx 确定性产生；释放后的返回由 conditionTimeout 兜底。
	shutdownObserveWindow = 100 * time.Millisecond
)

// fastSpec 高频测试任务的秒级时间表达式。
var fastSpec = "@every " + taskScheduleInterval.String()

// waitForCondition 轮询等待条件成立，超时则 t.Fatalf。
// 所有「等待任务执行/注销」的判定都必须经由本函数，禁止裸 sleep 后单次读数下结论。
func waitForCondition(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("等待超时（%v）：%s", timeout, desc)
}

// newTestScheduler 创建测试调度器并在测试结束时自动 Stop，保证用例间状态隔离。
func newTestScheduler(t *testing.T, opts ...Option) *Scheduler {
	t.Helper()
	s := New(opts...)
	t.Cleanup(s.Stop)
	return s
}

// countingTask 构造计数任务，Fn 原子自增 count。
func countingTask(name, spec string, count *atomic.Int64, isRunOnce bool) *Task {
	return &Task{
		Name:      name,
		TimeSpec:  spec,
		IsRunOnce: isRunOnce,
		Fn:        func() { count.Add(1) },
	}
}

// mustRun 注册任务并要求全部成功，失败即终止测试。
func mustRun(t *testing.T, s *Scheduler, tasks ...*Task) {
	t.Helper()
	if err := s.Run(tasks...); err != nil {
		t.Fatalf("注册任务失败：%v", err)
	}
}

// mustResume 恢复调度并要求全部任务重挂成功，失败即终止测试。
// 单个任务重挂失败即返回聚合 error，需要验证失败分支的用例请直接调 s.Resume()。
func mustResume(t *testing.T, s *Scheduler) {
	t.Helper()
	if err := s.Resume(); err != nil {
		t.Fatalf("恢复调度失败：%v", err)
	}
}

// fieldValue 取出 zapcore.Field 中实际存储的值。
// zapcore 对 string 走 StringType 的 String 字段，对反射类型走 Interface 字段，
// 直接读 Interface 会把 string 取成 nil（历史测试踩坑）。
func fieldValue(f logger.Field) any {
	if f.Type == zapcore.StringType {
		return f.String
	}
	return f.Interface
}
