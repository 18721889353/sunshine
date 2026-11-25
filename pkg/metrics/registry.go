package metrics

import (
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Registry 指标注册中心。
//
// 双注册设计（从网关移植，是"5s 快速巡检不触发 Go runtime STW"的关键）：
//   - Default：注册到 prometheus.DefaultRegisterer，供 /metrics 端点拉取
//   - fast：注册到独立 Registry，供 5s 巡检轻量采集
//
// 只采集业务自定义指标（不读 go_* 运行时家族），巡检才不会触发
// go_memstats_* 的 runtime.ReadMemStats STW 停顿。
type Registry struct {
	mu   sync.Mutex
	fast *prometheus.Registry
	// names 记录实际注册成功的指标全名（fqName）。
	// 一个 name 对应一个指标——天然去重，无需维护 collectors slice。
	// 用途：HasMetric 精确查询 + CollectorCount 计数。
	names map[string]struct{}
}

var (
	defaultRegistry *Registry
	registryOnce    sync.Once

	// fqNamePattern 从 prometheus.Desc.String() 提取 fqName。
	fqNamePattern = regexp.MustCompile(`fqName: "([^"]+)"`)
)

// Default 返回全局注册中心（进程内单例）。
func Default() *Registry {
	registryOnce.Do(func() {
		defaultRegistry = &Registry{
			fast:  prometheus.NewRegistry(),
			names: make(map[string]struct{}),
		}
	})
	return defaultRegistry
}

// isAlreadyRegistered 判断是否为"指标已存在"错误，覆盖 prometheus 两种形态：
//  1. AlreadyRegisteredError：同一 collector 实例重复注册
//     （prometheus 恒以值类型返回，Error() 为值接收者，errors.As 可匹配）
//  2. fmt.Errorf("descriptor ... already exists ...")：同名但不同 collector 实例
//     （prometheus registry.go:300，非结构化错误，只能按消息判断）
//
// 统一注册中心必须幂等：多包/多测试注册同名指标是正常场景，不应当 panic。
func isAlreadyRegistered(err error) bool {
	if err == nil {
		return false
	}
	var v prometheus.AlreadyRegisteredError
	if errors.As(err, &v) {
		return true
	}
	// 兜底只认 prometheus registry.go:300 的完整错误前缀，避免 "duplicate"
	// 这类过宽匹配误吞无关错误（如网络连接错误里含 duplicate 字样）。
	msg := err.Error()
	return strings.HasPrefix(msg, "descriptor ") && strings.Contains(msg, "already exists")
}

// Fast 返回轻量采集用的独立 Registry（仅自定义指标）。
func (r *Registry) Fast() *prometheus.Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fast
}

// Register 注册指标到双 Registry（Default + Fast）。
// 幂等：同一 collector 或同名指标重复注册时静默忽略（AlreadyRegistered），
// 不 panic——多个包/测试注册同一 collector 是正常场景，统一中心必须宽容。
func (r *Registry) Register(cols ...prometheus.Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range cols {
		// 非 Must：重复注册返回 AlreadyRegisteredError，忽略即可（幂等）
		dErr := prometheus.Register(c)
		if dErr != nil && !isAlreadyRegistered(dErr) {
			panic(dErr)
		}
		fErr := r.fast.Register(c)
		if fErr != nil && !isAlreadyRegistered(fErr) {
			panic(fErr)
		}
		// 只要任一 registry 首次注册成功，就记录 names。
		// 不能只看 dErr==nil：MustRegisterDefaultOnly 已把某 collector 注册到
		// Default 后，再对它调 MustRegister 时 dErr!=nil 但 Fast 首次成功
		// （fErr==nil）——只看 dErr 会漏记，导致 HasMetric 误报"未注册"。
		// names 用 map，幂等重复注册天然去重。
		if dErr == nil || fErr == nil {
			for _, name := range collectorMetricNames(c) {
				r.names[name] = struct{}{}
			}
		}
	}
}

// collectorMetricNames 通过 Describe 提取 collector 暴露的指标全名（fqName）。
// Desc.String() 格式固定为 `Desc{fqName: "xxx", ...}`，用正则提取；
// 无描述符的 unchecked collector 返回空（这类指标无法预知名字，不影响查询）。
func collectorMetricNames(c prometheus.Collector) []string {
	descCh := make(chan *prometheus.Desc, 16)
	done := make(chan struct{})
	go func() {
		// defer 顺序（LIFO）：recover → close(descCh) → close(done)。
		// 关键：close(descCh) 必须 defer，否则 Describe panic 时主 goroutine
		// 的 range descCh 永久阻塞 → Register 持锁死锁整个注册中心。
		defer close(done)
		defer close(descCh)
		defer func() { recover() }() //nolint:errcheck // 必须吞掉 panic：Describe 异常时返回空 names，避免死锁
		c.Describe(descCh)           // Describe 向 ch 写入所有描述符
	}()
	var names []string
	seen := map[string]bool{}
	// 主 goroutine 持续 drain，避免描述符超过缓冲时 Describe 阻塞。
	for d := range descCh {
		if m := fqNamePattern.FindStringSubmatch(d.String()); len(m) == 2 && !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	<-done
	return names
}

// GatherFast 从轻量 Registry 采集（5s 巡检用），不触发 Go runtime STW。
func (r *Registry) GatherFast() ([]*dto.MetricFamily, error) {
	return r.fast.Gather()
}

// GatherDefault 从 Default Registry 采集（周期报告用，含 go_* 运行时）。
func (r *Registry) GatherDefault() ([]*dto.MetricFamily, error) {
	return prometheus.DefaultGatherer.Gather()
}

// --- 便捷注册函数（业务侧用） ---

// MustRegister 把指标注册到全局双 Registry。
func MustRegister(cols ...prometheus.Collector) {
	Default().Register(cols...)
}

// MustRegisterDefaultOnly 只注册到 Default Registry（不进 Fast）。
//
// 用于 Go runtime 类指标（GoCollector 等）：5s 快速巡检读 go_memstats 会触发
// runtime.ReadMemStats 的 STW 停顿，这正是 Fast Registry 只收自定义指标的原因。
// 这类指标只供 /metrics 端点拉取，不参与巡检采集。
//
// 幂等：同名/同实例重复注册静默忽略（与 Register 一致，不 panic）。
func MustRegisterDefaultOnly(cols ...prometheus.Collector) {
	for _, c := range cols {
		if err := prometheus.Register(c); err != nil && !isAlreadyRegistered(err) {
			panic(err)
		}
	}
}

// Gauge 创建并注册一个 gauge。
func Gauge(name, help string, labels ...string) *prometheus.GaugeVec {
	v := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
	MustRegister(v)
	return v
}

// Counter 创建并注册一个 counter。
func Counter(name, help string, labels ...string) *prometheus.CounterVec {
	v := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	MustRegister(v)
	return v
}

// Histogram 创建并注册一个直方图。
func Histogram(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	v := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets}, labels)
	MustRegister(v)
	return v
}

// DefaultBuckets 默认直方图桶（prometheus.DefBuckets 的别名）。
// 业务侧用 pkgmetrics.DefaultBuckets 即可，无需直接 import prometheus。
var DefaultBuckets = prometheus.DefBuckets

// HistogramDefault 创建并注册一个直方图（用 DefaultBuckets）。
// 这是业务侧最常用的入口——消除各包对 prometheus 包的直接依赖。
func HistogramDefault(name, help string, labels ...string) *prometheus.HistogramVec {
	return Histogram(name, help, DefaultBuckets, labels...)
}

// Summary 创建并注册一个 summary。
func Summary(name, help string, labels ...string) *prometheus.SummaryVec {
	v := prometheus.NewSummaryVec(prometheus.SummaryOpts{Name: name, Help: help}, labels)
	MustRegister(v)
	return v
}

// CollectorCount 返回已注册的 collector 数量（已去重，幂等重复注册不重复计入）。
// 用途：运维/测试观察注册中心规模（Vec 无 label 样本时 Gather 不 emit，
// 无法用 Gather 数注册数）。精确查询单个指标请用 HasMetric。
func (r *Registry) CollectorCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.names)
}

// HasMetric 查询指定指标全名（fqName）是否已注册。
// 精确、无副作用——替代"同名冲突 panic"反证法。
//
// 注意两类指标查不到（设计如此）：
//   - MustRegisterDefaultOnly 注册的指标（如 go_* 运行时类）不进 names——
//     names 只跟踪统一注册中心 Register 路径的指标
//   - unchecked collector（Describe 不发描述符）——无法预知名字
//
// 本项目所有业务指标均走 Register 路径且为标准 Vec/Collector，均可查询。
func (r *Registry) HasMetric(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.names[name]
	return ok
}
