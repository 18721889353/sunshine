package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// TestCollectorMetricNames_NoDeadlockOnPanicDescribe 自定义 Collector 的
// Describe 若 panic，注册路径不得死锁（close(descCh) 必须 defer 执行）。
func TestCollectorMetricNames_NoDeadlockOnPanicDescribe(t *testing.T) {
	result := make(chan int, 1)
	go func() {
		result <- len(collectorMetricNames(panickyCollector{}))
	}()
	select {
	case n := <-result:
		if n != 0 {
			t.Errorf("Describe panic 应返回空 names, got %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("collectorMetricNames 死锁（Describe panic 未被 recover/descCh 未关闭）")
	}
}

// TestRegister_ConflictingDefinition_Panics 验证关键设计决策：
// 同名但 help/labels 定义冲突的指标注册必须 panic——这是真实配置错误，
// 不能被 isAlreadyRegistered 误吞（否则两个包各定义一版，静默取先注册的那版）。
// prometheus 对这种冲突返回非结构化错误：
// "a previously registered descriptor with the same fully-qualified name as ...
// has different label names or a different help string"，不在 isAlreadyRegistered
// 覆盖的两种形态内。本测试防止后续重构把它错误地纳入幂等忽略。
func TestRegister_ConflictingDefinition_Panics(t *testing.T) {
	r := &Registry{fast: prometheus.NewRegistry(), names: map[string]struct{}{}}
	const name = "test_conflict_definition_total"
	r.Register(prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: name, Help: "first definition",
	}, []string{"a"}))

	defer func() {
		if recover() == nil {
			t.Error("同名不同 help/labels 的指标注册应 panic（真实定义冲突不能静默忽略）")
		}
	}()
	r.Register(prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: name, Help: "conflicting definition",
	}, []string{"b"}))
}

// panickyCollector Describe 时 panic 的测试桩。
type panickyCollector struct{}

func (panickyCollector) Describe(ch chan<- *prometheus.Desc) { panic("boom") }
func (panickyCollector) Collect(ch chan<- prometheus.Metric) {}

// TestRegister_DefaultOnlyThenBoth_RecordsName 验证边界：
// collector 先经 MustRegisterDefaultOnly 注册到 Default，
// 再经 MustRegister 双注册时，虽然 dErr!=nil（Default 已存在），
// 但 Fast 首次成功（fErr==nil）→ names 仍应记录（HasMetric=true）。
func TestRegister_DefaultOnlyThenBoth_RecordsName(t *testing.T) {
	const name = "test_default_then_both_total"
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: "test"}, []string{"x"})

	// 第一步：只注册到 Default
	MustRegisterDefaultOnly(c)
	// 第二步：双注册（Default 已存在 → dErr!=nil，Fast 首次 → fErr==nil）
	MustRegister(c)

	if !Default().HasMetric(name) {
		t.Errorf("%s 应被记录（Fast 首次注册成功也必须记 names）", name)
	}
}
