package gocron

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

// TestNewStartsScheduler 验证 New 创建的调度器可直接注册并查询任务。
func TestNewStartsScheduler(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s, countingTask("task-a", fastSpec, &count, false))

	if !s.IsRunningTask("task-a") {
		t.Fatal("注册后 IsRunningTask 应为 true")
	}
	got := s.GetRunningTasks()
	if len(got) != 1 || got[0] != "task-a" {
		t.Fatalf("GetRunningTasks 应为 [task-a]，实际 %v", got)
	}
	if s.IsPaused() {
		t.Fatal("新建调度器不应处于暂停状态")
	}
}

// TestNewInstancesIsolated 验证工厂创建的多个实例状态互不干扰（实例化的核心收益）。
func TestNewInstancesIsolated(t *testing.T) {
	s1 := newTestScheduler(t)
	s2 := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s1, countingTask("only-in-s1", fastSpec, &count, false))

	if s2.IsRunningTask("only-in-s1") {
		t.Fatal("s2 不应看到 s1 注册的任务")
	}
	if got := s2.GetRunningTasks(); len(got) != 0 {
		t.Fatalf("s2 的任务列表应为空，实际 %v", got)
	}

	// 停掉 s1 不影响 s2
	s1.Stop()
	if got := s2.GetRunningTasks(); len(got) != 0 {
		t.Fatalf("s1.Stop 不应清空 s2 的任务列表，实际 %v", got)
	}
	if err := s2.Run(countingTask("in-s2", fastSpec, &count, false)); err != nil {
		t.Fatalf("s1 停止后 s2 仍应可注册任务：%v", err)
	}
}

// TestRunValidationErrors 验证参数校验：nil/空名/空函数/重复/非法表达式均返回中文错误。
func TestRunValidationErrors(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s, countingTask("dup", fastSpec, &count, false))

	cases := []struct {
		name    string
		task    *Task
		wantSub string
	}{
		{"nil任务", nil, "任务不能为空"},
		{"空任务名", &Task{TimeSpec: fastSpec, Fn: func() {}}, "任务名不能为空"},
		{"空函数", &Task{Name: "no-fn", TimeSpec: fastSpec}, "的函数为空"},
		{"重复任务", countingTask("dup", fastSpec, &count, false), "已存在"},
		{"非法表达式", &Task{Name: "bad-spec", TimeSpec: "not-a-spec", Fn: func() {}}, "运行任务"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Run(tc.task)
			if err == nil {
				t.Fatal("预期返回错误，实际为 nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("错误信息应包含 %q，实际 %q", tc.wantSub, err.Error())
			}
		})
	}
}

// TestRunAggregatesErrors 验证批量注册时任一失败不中断其余任务，错误用 " || " 聚合。
func TestRunAggregatesErrors(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	err := s.Run(
		nil, // 失败 1
		&Task{Name: "bad-spec", TimeSpec: "x", Fn: func() {}}, // 失败 2
		countingTask("good", fastSpec, &count, false),         // 成功
	)
	if err == nil {
		t.Fatal("预期返回聚合错误")
	}
	if !strings.Contains(err.Error(), " || ") {
		t.Fatalf("错误应用 \" || \" 聚合，实际 %q", err.Error())
	}
	if !s.IsRunningTask("good") {
		t.Fatal("失败任务不应中断其余任务的注册")
	}
}

// TestRunAfterStopReturnsError 验证 Stop 后实例不可复用，且重复 Stop 幂等。
func TestRunAfterStopReturnsError(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s, countingTask("task-a", fastSpec, &count, false))
	s.Stop()

	err := s.Run(countingTask("task-b", fastSpec, &count, false))
	if err == nil {
		t.Fatal("Stop 后 Run 应返回错误")
	}
	if !strings.Contains(err.Error(), "已停止") {
		t.Fatalf("错误信息应包含「已停止」，实际 %q", err.Error())
	}

	// 幂等：重复 Stop 及 Stop 后的 Pause/Resume 不 panic、不改状态、返回 nil
	s.Stop()
	s.Pause()
	if err := s.Resume(); err != nil {
		t.Fatalf("已停止的调度器 Resume 应返回 nil，实际 %v", err)
	}
	if s.IsPaused() {
		t.Fatal("已停止的调度器不应报告暂停")
	}
}

// TestStopResetsState 验证 Stop 清空任务注册表与暂停标记。
func TestStopResetsState(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s,
		countingTask("task-a", fastSpec, &count, false),
		countingTask("task-b", fastSpec, &count, false),
	)

	s.Stop()

	if got := s.GetRunningTasks(); len(got) != 0 {
		t.Fatalf("Stop 后任务列表应为空，实际 %v", got)
	}
	if s.IsRunningTask("task-a") {
		t.Fatal("Stop 后任务应不可见")
	}
	if s.IsPaused() {
		t.Fatal("Stop 后不应处于暂停状态")
	}
}

// TestShutdownWaitsInflightTasks 验证 Shutdown 等待在途任务全部结束后才返回，
// 与 Stop 的「立即返回、不截断在途任务」语义形成对照。
func TestShutdownWaitsInflightTasks(t *testing.T) {
	s := newTestScheduler(t)

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFn := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFn) // 断言失败时也释放阻塞任务，避免 goroutine 泄漏

	// jobOnce 与 releaseOnce 必须分开：若释放方复用 job 的 once，
	// job 闭包阻塞在 <-release 时释放方的 Do 会等它完成，形成自锁。
	var jobOnce sync.Once
	mustRun(t, s, &Task{
		Name:     "blocking",
		TimeSpec: fastSpec,
		Fn: func() {
			jobOnce.Do(func() {
				close(started)
				<-release // 阻塞至释放，模拟长耗时任务
			})
		},
	})
	waitForCondition(t, conditionTimeout, "阻塞任务开始执行", func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})

	// 异步发起 Shutdown，在观察窗口内不应返回（在途任务未结束）
	errCh := make(chan error, 1)
	go func() { errCh <- s.Shutdown(context.Background()) }()
	select {
	case err := <-errCh:
		t.Fatalf("在途任务未结束时 Shutdown 不应返回：%v", err)
	case <-time.After(shutdownObserveWindow):
		// 预期：仍在等待排空
	}

	// 释放任务后应在超时上界内返回且状态已清空
	releaseFn()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("释放任务后 Shutdown 应成功：%v", err)
		}
	case <-time.After(conditionTimeout):
		t.Fatal("释放任务后 Shutdown 应在超时上界内返回")
	}
	if got := s.GetRunningTasks(); len(got) != 0 {
		t.Fatalf("Shutdown 后任务列表应为空，实际 %v", got)
	}
}

// TestShutdownTimeoutStillStops 验证 ctx 超时后 Shutdown 返回包裹 DeadlineExceeded 的错误，
// 且仍清空状态（调度已停，残留任务不会继续执行）。
func TestShutdownTimeoutStillStops(t *testing.T) {
	s := newTestScheduler(t)

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	var jobOnce sync.Once
	mustRun(t, s, &Task{
		Name:     "blocking",
		TimeSpec: fastSpec,
		Fn: func() {
			jobOnce.Do(func() {
				close(started)
				<-release // 始终不主动释放，等待 ctx 超时
			})
		},
	})
	waitForCondition(t, conditionTimeout, "阻塞任务开始执行", func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), shutdownObserveWindow)
	defer cancel()
	err := s.Shutdown(ctx)
	if err == nil {
		t.Fatal("在途任务未结束且 ctx 超时，Shutdown 应返回错误")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("错误应包裹 context.DeadlineExceeded，实际 %v", err)
	}
	if got := s.GetRunningTasks(); len(got) != 0 {
		t.Fatalf("超时后 Shutdown 仍应清空任务列表，实际 %v", got)
	}
}

// TestShutdownAfterStopIdempotent 验证已停止的调度器 Shutdown 幂等返回 nil。
func TestShutdownAfterStopIdempotent(t *testing.T) {
	s := newTestScheduler(t)
	s.Stop()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("已停止的调度器 Shutdown 应返回 nil，实际 %v", err)
	}
}

// TestShutdownRejectsConcurrentRunResume 验证 Shutdown 等待期间置 shuttingDown 标记，
// 并发的 Run/Resume 均被拒绝（否则 Resume 重建新实例会让 Shutdown 等待错对象，
// Run 会把任务挂上即将被清空的旧实例造成静默丢失）。
func TestShutdownRejectsConcurrentRunResume(t *testing.T) {
	s := newTestScheduler(t)

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFn := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFn)

	var jobOnce sync.Once
	mustRun(t, s, &Task{
		Name:     "blocking",
		TimeSpec: fastSpec,
		Fn: func() {
			jobOnce.Do(func() {
				close(started)
				<-release // 阻塞至释放，维持 Shutdown 等待窗口
			})
		},
	})
	waitForCondition(t, conditionTimeout, "阻塞任务开始执行", func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})

	// Shutdown 挂起等待在途任务
	errCh := make(chan error, 1)
	go func() { errCh <- s.Shutdown(context.Background()) }()

	// 先用无副作用的探测确认 shuttingDown 已生效：未暂停时 Resume 本是空操作（返回 nil），
	// 被拒返回「正在关闭」错误——轮询不改变任何状态，满足条件函数可重复调用的要求
	waitForCondition(t, conditionTimeout, "Shutdown 期间 Resume 被拒绝", func() bool {
		err := s.Resume()
		return err != nil && strings.Contains(err.Error(), "正在关闭")
	})

	// 标记已生效，Run 此刻必被拒绝：单次确定性断言，避免在条件函数里反复注册任务
	var count atomic.Int64
	if err := s.Run(countingTask("late", fastSpec, &count, false)); err == nil ||
		!strings.Contains(err.Error(), "正在关闭") {
		t.Fatalf("Shutdown 期间 Run 应被拒绝，实际 %v", err)
	}

	// 释放任务后 Shutdown 正常返回，标记随之解除
	releaseFn()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Shutdown 应成功：%v", err)
		}
	case <-time.After(conditionTimeout):
		t.Fatal("释放任务后 Shutdown 应在超时上界内返回")
	}
	if err := s.Resume(); err != nil { // 已停止的空操作，标记已解除
		t.Fatalf("Shutdown 结束后 shuttingDown 标记应解除，实际 %v", err)
	}
}

// TestStatsReflectsState 验证 Stats 在各生命周期阶段返回正确快照，
// 且 ResumeFailures 累计计数跨 Stop 不重置。
func TestStatsReflectsState(t *testing.T) {
	s := newTestScheduler(t)

	// 初始：空调度器
	if st := s.Stats(); st.Registered != 0 || st.Paused || st.Stopped || st.ResumeFailures != 0 {
		t.Fatalf("初始状态不符：%+v", st)
	}

	// 注册 2 个任务
	var count atomic.Int64
	mustRun(t, s,
		countingTask("a", fastSpec, &count, false),
		countingTask("b", fastSpec, &count, false),
	)
	if got := s.Stats().Registered; got != 2 {
		t.Fatalf("注册后 Registered 应为 2，实际 %d", got)
	}

	// 暂停
	s.Pause()
	if !s.Stats().Paused {
		t.Fatal("Pause 后 Paused 应为 true")
	}

	// 白盒注入失败任务 → Resume 失败 → ResumeFailures 累计 1
	s.mu.Lock()
	s.taskFuncs["bad"] = taskFunc{spec: "not-a-spec", fn: func() {}}
	s.mu.Unlock()
	if err := s.Resume(); err == nil {
		t.Fatal("预期 Resume 失败")
	}
	if got := s.Stats().ResumeFailures; got != 1 {
		t.Fatalf("ResumeFailures 应为 1，实际 %d", got)
	}

	// 停止：清空但不重置累计计数
	s.Stop()
	st := s.Stats()
	if !st.Stopped || st.Registered != 0 || st.Paused {
		t.Fatalf("Stop 后状态不符：%+v", st)
	}
	if st.ResumeFailures != 1 {
		t.Fatalf("Stop 不应重置累计计数，应为 1，实际 %d", st.ResumeFailures)
	}
}

// TestRunGranularitySpec 验证粒度选项对表达式字段数的约束：秒级 6 字段、分钟级 5 字段。
func TestRunGranularitySpec(t *testing.T) {
	secondSpec := "0/5 * * * * *" // 6 字段，含秒位
	minuteSpec := "*/5 * * * *"   // 5 字段，标准格式
	fn := func() {}

	t.Run("秒级接受6字段拒绝5字段", func(t *testing.T) {
		s := newTestScheduler(t) // 默认 SecondType
		if err := s.Run(&Task{Name: "sec", TimeSpec: secondSpec, Fn: fn}); err != nil {
			t.Fatalf("秒级应接受 6 字段表达式：%v", err)
		}
		if err := s.Run(&Task{Name: "min", TimeSpec: minuteSpec, Fn: fn}); err == nil {
			t.Fatal("秒级应拒绝 5 字段表达式")
		}
	})

	t.Run("分钟级接受5字段拒绝6字段", func(t *testing.T) {
		s := newTestScheduler(t, WithGranularity(MinuteType))
		if err := s.Run(&Task{Name: "min", TimeSpec: minuteSpec, Fn: fn}); err != nil {
			t.Fatalf("分钟级应接受 5 字段表达式：%v", err)
		}
		if err := s.Run(&Task{Name: "sec", TimeSpec: secondSpec, Fn: fn}); err == nil {
			t.Fatal("分钟级应拒绝 6 字段表达式")
		}
	})
}

// TestRunOnceAutoDelete 验证单次任务执行一次后自动注销且只执行一次。
func TestRunOnceAutoDelete(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s, countingTask("once", fastSpec, &count, true))

	waitForCondition(t, conditionTimeout, "单次任务执行后自动注销", func() bool {
		return !s.IsRunningTask("once")
	})
	if got := count.Load(); got != 1 {
		t.Fatalf("单次任务应恰好执行 1 次，实际 %d 次", got)
	}
}

// TestDeleteTask 验证删除任务与幂等性。
func TestDeleteTask(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s,
		countingTask("task-a", fastSpec, &count, false),
		countingTask("task-b", fastSpec, &count, false),
	)

	s.DeleteTask("task-a")
	if s.IsRunningTask("task-a") {
		t.Fatal("删除后任务不应可见")
	}
	if !s.IsRunningTask("task-b") {
		t.Fatal("删除 task-a 不应影响 task-b")
	}

	// 幂等：重复删除与删除不存在的任务均不 panic、不报错
	s.DeleteTask("task-a")
	s.DeleteTask("never-registered")
}

// TestPauseResumeCycle 验证暂停期间任务停止执行、恢复后继续执行。
func TestPauseResumeCycle(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s, countingTask("tick", fastSpec, &count, false))
	waitForCondition(t, conditionTimeout, "任务开始执行", func() bool {
		return count.Load() >= 1
	})

	s.Pause()
	if !s.IsPaused() {
		t.Fatal("Pause 后 IsPaused 应为 true")
	}
	// 收尾窗口：容纳 Pause 瞬间已异步启动的 job 执行完毕
	time.Sleep(drainWindow)
	before := count.Load()

	// 观察窗口内计数必须不变
	time.Sleep(pauseObserveWindow)
	if got := count.Load(); got != before {
		t.Fatalf("暂停期间任务不应执行：暂停前 %d，观察后 %d", before, got)
	}

	mustResume(t, s)
	if s.IsPaused() {
		t.Fatal("Resume 后 IsPaused 应为 false")
	}
	waitForCondition(t, conditionTimeout, "恢复后任务继续执行", func() bool {
		return count.Load() > before
	})
}

// TestResumeKeepsDeletedTaskGone 验证暂停期间删除的任务在恢复后不会被复活（核心回归）。
func TestResumeKeepsDeletedTaskGone(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s,
		countingTask("keeper", fastSpec, &count, false),
		countingTask("victim", fastSpec, &count, false),
	)

	s.Pause()
	s.DeleteTask("victim")
	mustResume(t, s)

	// 给足执行窗口后确认 victim 未复活
	time.Sleep(pauseObserveWindow)
	if s.IsRunningTask("victim") {
		t.Fatal("Resume 后被删除的任务不应复活")
	}
	if !s.IsRunningTask("keeper") {
		t.Fatal("Resume 后保留的任务应仍在")
	}
}

// TestRunDuringPauseThenResume 验证暂停期间注册的任务登记成功、恢复后生效。
func TestRunDuringPauseThenResume(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s, countingTask("early", fastSpec, &count, false))
	s.Pause()
	time.Sleep(drainWindow)

	// 暂停期间注册：应成功且立即可见（登记在已停止的实例上）
	if err := s.Run(countingTask("late", fastSpec, &count, false)); err != nil {
		t.Fatalf("暂停期间注册应成功：%v", err)
	}
	if !s.IsRunningTask("late") {
		t.Fatal("暂停期间注册的任务应立即可见")
	}

	mustResume(t, s)
	waitForCondition(t, conditionTimeout, "恢复后暂停期间注册的任务开始执行", func() bool {
		return s.IsRunningTask("late") && count.Load() > 0
	})
}

// TestResumeNoopWhenNotPaused 验证未暂停时 Resume、未初始化时 Pause 均为空操作。
func TestResumeNoopWhenNotPaused(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s, countingTask("task-a", fastSpec, &count, false))
	before := s.GetRunningTasks()

	if err := s.Resume(); err != nil { // 未暂停，应为空操作且返回 nil
		t.Fatalf("未暂停时 Resume 应返回 nil，实际 %v", err)
	}

	if got := s.GetRunningTasks(); len(got) != len(before) || got[0] != before[0] {
		t.Fatalf("未暂停时 Resume 不应改变状态，之前 %v，之后 %v", before, got)
	}
}

// TestResumeAggregatesErrors 验证恢复时重挂失败会聚合成 error 对外暴露（不再静默丢任务），
// 且单个任务失败不中断其余任务恢复。
func TestResumeAggregatesErrors(t *testing.T) {
	s := newTestScheduler(t)

	var count atomic.Int64
	mustRun(t, s, countingTask("good", fastSpec, &count, false))

	// 白盒注入两条非法表达式（故意按反字典序命名，验证按名字典序重挂、错误顺序确定）
	s.mu.Lock()
	s.taskFuncs["bad-z"] = taskFunc{spec: "not-a-spec", fn: func() {}}
	s.taskFuncs["bad-a"] = taskFunc{spec: "also-not-a-spec", fn: func() {}}
	s.mu.Unlock()

	s.Pause()
	err := s.Resume()
	if err == nil {
		t.Fatal("存在重挂失败任务时 Resume 应返回错误")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Fatalf("错误应包含失败任务名，实际 %q", err.Error())
	}
	// 字典序断言：bad-a 先于 bad-z 被重挂，聚合顺序因此确定
	if ia, iz := strings.Index(err.Error(), "bad-a"), strings.Index(err.Error(), "bad-z"); ia < 0 || ia > iz {
		t.Fatalf("失败任务应按名字典序聚合（bad-a 先于 bad-z），实际 %q", err.Error())
	}
	if !s.IsRunningTask("good") {
		t.Fatal("单个任务失败不应中断其余任务恢复")
	}
}

// TestEveryHelpers 验证时间表达式便捷函数的输出格式。
func TestEveryHelpers(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"EverySecond", EverySecond(5), "@every 5s"},
		{"EveryMinute", EveryMinute(2), "@every 2m"},
		{"EveryHour", EveryHour(3), "@every 3h"},
		{"Everyday换算为小时", Everyday(1), "@every 24h"},
		{"Everyday两天", Everyday(2), "@every 48h"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("期望 %q，实际 %q", tc.want, tc.got)
			}
		})
	}
}

// TestOptionDefaultsAndApply 验证 Option 默认值与覆盖行为。
func TestOptionDefaultsAndApply(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		o := defaultOptions()
		if o.isOnlyPrintError {
			t.Fatal("默认不应仅打印错误")
		}
		if o.granularity != SecondType {
			t.Fatalf("默认粒度应为秒级，实际 %d", o.granularity)
		}
	})

	t.Run("应用选项", func(t *testing.T) {
		o := defaultOptions()
		o.apply(WithOnlyPrintError(true), WithGranularity(MinuteType))
		if !o.isOnlyPrintError {
			t.Fatal("WithOnlyPrintError(true) 应生效")
		}
		if o.granularity != MinuteType {
			t.Fatalf("WithGranularity(MinuteType) 应生效，实际 %d", o.granularity)
		}
	})

	t.Run("粒度越界归一化", func(t *testing.T) {
		o := defaultOptions()
		o.apply(WithGranularity(99))
		if o.granularity != MinuteType {
			t.Fatalf(">= MinuteType 应归一化为分钟级，实际 %d", o.granularity)
		}
		o.apply(WithGranularity(-1))
		if o.granularity != SecondType {
			t.Fatalf("< MinuteType 应归一化为秒级，实际 %d", o.granularity)
		}
	})
}

// TestSchedulerParseKVs 验证日志键值转换：entry ID 还原任务名、奇数对丢弃、非字符串键跳过。
func TestSchedulerParseKVs(t *testing.T) {
	s := &Scheduler{}

	t.Run("字符串值存于String字段", func(t *testing.T) {
		fields := s.parseKVs([]any{"job", "testJob"})
		if len(fields) != 1 {
			t.Fatalf("应产出 1 个字段，实际 %d", len(fields))
		}
		if got := fieldValue(fields[0]); got != "testJob" {
			t.Fatalf("字段值应为 testJob，实际 %#v（直接读 Interface 会拿到 nil）", got)
		}
	})

	t.Run("已映射entry还原为任务名", func(t *testing.T) {
		s.idName.Store(cron.EntryID(99), "mappedTask")
		fields := s.parseKVs([]any{"entry", cron.EntryID(99)})
		if len(fields) != 1 {
			t.Fatalf("应产出 1 个字段，实际 %d", len(fields))
		}
		if fields[0].Key != "task" {
			t.Fatalf("entry 键应重命名为 task，实际 %q", fields[0].Key)
		}
		if got := fieldValue(fields[0]); got != "mappedTask" {
			t.Fatalf("字段值应为 mappedTask，实际 %#v", got)
		}
	})

	t.Run("未映射entry保留原值", func(t *testing.T) {
		fields := s.parseKVs([]any{"entry", cron.EntryID(100)})
		if len(fields) != 1 {
			t.Fatalf("应产出 1 个字段，实际 %d", len(fields))
		}
		if got := fieldValue(fields[0]); got != cron.EntryID(100) {
			t.Fatalf("未映射时应保留 EntryID，实际 %#v", got)
		}
	})

	t.Run("奇数参数对丢弃", func(t *testing.T) {
		if fields := s.parseKVs([]any{"a", "b", "c"}); fields != nil {
			t.Fatalf("奇数参数对应返回 nil，实际 %v", fields)
		}
	})

	t.Run("非字符串键跳过", func(t *testing.T) {
		fields := s.parseKVs([]any{1, "v", "k", "v2"})
		if len(fields) != 1 || fields[0].Key != "k" {
			t.Fatalf("非字符串键应跳过，实际 %v", fields)
		}
	})
}

// TestConcurrentOperations 验证同一实例并发注册/删除/查询/暂停恢复无竞态（-race 主用例）。
func TestConcurrentOperations(t *testing.T) {
	s := newTestScheduler(t)

	const (
		workers    = 6
		iterations = 20
	)

	var count atomic.Int64
	var wg sync.WaitGroup
	errCh := make(chan error, workers*iterations)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				name := fmt.Sprintf("task-%d-%d", worker, i)
				task := countingTask(name, fastSpec, &count, false)

				switch i % 4 {
				case 0:
					if err := s.Run(task); err != nil {
						errCh <- fmt.Errorf("worker %d 注册 %s 失败: %w", worker, name, err)
					}
				case 1:
					s.DeleteTask(name)
				case 2:
					s.IsRunningTask(name)
				case 3:
					s.GetRunningTasks()
				}
				// 穿插暂停/恢复，覆盖锁竞争最激烈的路径
				if i%7 == 0 {
					s.Pause()
					if err := s.Resume(); err != nil {
						errCh <- fmt.Errorf("worker %d 恢复失败: %w", worker, err)
					}
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("并发操作不应失败：%v", err)
	}
	if got := len(s.GetRunningTasks()); got == workers*iterations {
		t.Fatalf("部分任务已被删除，注册数不应为满额 %d", got)
	}
}
