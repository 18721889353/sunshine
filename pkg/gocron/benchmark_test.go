package gocron

import (
	"fmt"
	"testing"

	"github.com/robfig/cron/v3"
)

// benchmarkFieldSink 防止编译器消除被测调用。
var benchmarkFieldSink int

// BenchmarkSchedulerRunDelete 基准任务注册-删除往返（纯进程内，无网络 RTT），
// 定位是互斥锁与注册表操作的回归基线，不作为绝对性能承诺。
// WithOnlyPrintError(true) 剔除日志输出开销，读数只反映注册表与锁成本。
func BenchmarkSchedulerRunDelete(b *testing.B) {
	s := New(WithOnlyPrintError(true))
	defer s.Stop()

	task := &Task{Name: "bench", TimeSpec: fastSpec, Fn: func() {}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Run(task); err != nil {
			b.Fatalf("注册失败：%v", err)
		}
		s.DeleteTask("bench")
	}
}

// BenchmarkSchedulerGetRunningTasks 基准查询接口（持锁排序）。
func BenchmarkSchedulerGetRunningTasks(b *testing.B) {
	s := New(WithOnlyPrintError(true))
	defer s.Stop()

	for i := 0; i < 64; i++ {
		name := fmt.Sprintf("bench-%d", i)
		if err := s.Run(&Task{Name: name, TimeSpec: fastSpec, Fn: func() {}}); err != nil {
			b.Fatalf("注册失败：%v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkFieldSink = len(s.GetRunningTasks())
	}
}

// BenchmarkParseKVs 基准日志键值转换与 EntryID 还原。
func BenchmarkParseKVs(b *testing.B) {
	s := &Scheduler{}
	s.idName.Store(cron.EntryID(1), "bench-task")
	kvs := []any{"entry", cron.EntryID(1), "job", "testJob", "run", "now"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkFieldSink = len(s.parseKVs(kvs))
	}
}
