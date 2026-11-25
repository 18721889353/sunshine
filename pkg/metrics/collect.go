// Package metrics 为 sunshine 框架提供统一的指标采集、窗口差分、诊断与企业微信推送能力。
//
// 设计：业务侧只做"注册"（定义即注册或 MustRegister），框架负责采集/差分/诊断/渲染/推送。
//
// 核心不变量（来自网关线上验证，改动需回归）：
//   - 窗口差分：counter/histogram 做 cur-prev，gauge 取当前值不差分
//   - 均值可加：逐样本「网关+上游=端到端」⇒ sum 相加、count 相同 ⇒ 均值相加
//   - P99 不可加：独立直方图各自插值，数学上不可加，故括号分解用均值
package metrics

import (
	"strings"

	dto "github.com/prometheus/client_model/go"
)

// ============================================================================
// 通用组件指标采集：从 GatherFast() 的 []dto.MetricFamily 构建结构化快照。
//
// 与网关的差异：网关硬编码指标名白名单（entry/upstream 固定），
// sunshine 是框架——遍历所有 Histogram/Counter/Gauge 家族自动聚合，
// 任何注册到统一中心的组件指标（redis/cache/db/es/rabbitmq/...）
// 无需改采集代码即可进入报告。
//
// 口径（与网关一致，窗口差分正确性的根基）：
//   - Histogram/Counter：count/sum 是累计值，差分得窗口值
//   - Gauge：当前值，不差分
// ============================================================================

// DurationAgg 单个耗时指标（Histogram 家族）的累计聚合。
// 所有 label 组合聚合成一个总数（算 QPS/总耗时），并按 result 拆分（算错误率）。
//
// Bounds/Counts 跨 label 组合累加（同指标 bounds 相同），用于差分后算窗口 P99。
type DurationAgg struct {
	Count  uint64            // 累计样本数
	SumSec float64           // 累计耗时秒
	Result map[string]uint64 // result label -> 累计样本数（ok/error，算错误率）
	Bounds []float64         // 直方图桶上界（累计口径，同指标固定）
	Counts []uint64          // 各桶累计计数（差分得窗口桶分布）
}

// MetricSnapshot 一次采集的组件级快照（通用，无网关特有字段）。
//
// 字段口径：
//   - Durations：Histogram 家族（*_duration_seconds），累计值，差分得窗口
//   - Counters：Counter 家族（*_total），累计值，差分得窗口
//   - Gauges：Gauge 家族（如 uptime_total 是 counter、logger_router_active_loggers 是 gauge），当前值
//   - Summaries：Summary 家族（如 http_request_size_bytes），累计 count/sum 但
//     sum 单位由指标决定（字节等），绝不能混入 Durations——否则字节被当秒渲染成
//     ms、并按错误率/P99 误诊。与 Durations 物理隔离，诊断只看 Durations。
type MetricSnapshot struct {
	Durations map[string]*DurationAgg       // 指标名 -> 累计聚合（秒，所有 label 合并）
	Counters  map[string]map[string]float64 // 指标名 -> labelKey -> 累计值
	Gauges    map[string]map[string]float64 // 指标名 -> labelKey -> 当前值
	Summaries map[string]*DurationAgg       // 指标名 -> 累计聚合（单位由指标名决定）
	// DurationsByLabel 指标名 -> labelKey -> 累计聚合（秒）。
	// Durations 是全 label 合并值（报告主口径），本字段保留 label 维度
	// 供业务诊断下钻（如按 entry 区分 api/notify 链、按 replica 找最慢副本）——
	// 聚合口径会把多条链的 P99 混在一起，改变判定语义。
	DurationsByLabel map[string]map[string]*DurationAgg
}

// NewMetricSnapshot 构造空快照（所有 map 已初始化）。
func NewMetricSnapshot() *MetricSnapshot {
	return &MetricSnapshot{
		Durations:        make(map[string]*DurationAgg),
		Counters:         make(map[string]map[string]float64),
		Gauges:           make(map[string]map[string]float64),
		Summaries:        make(map[string]*DurationAgg),
		DurationsByLabel: make(map[string]map[string]*DurationAgg),
	}
}

// CollectFromFamilies 从 Gather 结果构建快照。通用：不依赖指标名白名单。
func CollectFromFamilies(fams []*dto.MetricFamily) *MetricSnapshot {
	s := NewMetricSnapshot()
	for _, f := range fams {
		if f == nil || f.Name == nil {
			continue
		}
		name := f.GetName()
		// go_* 运行时指标只在周期报告（Default）出现，Fast 不含；
		// 若出现也归入 Gauges（goroutines/内存是当前值）。
		switch f.GetType() {
		case dto.MetricType_HISTOGRAM:
			s.fillHistogram(name, f)
		case dto.MetricType_COUNTER:
			s.fillCounter(name, f)
		case dto.MetricType_GAUGE:
			s.fillGauge(name, f)
		case dto.MetricType_SUMMARY:
			// Summary 的 sum 单位由指标决定（字节/大小等），与耗时隔离存放，
			// 否则 renderUnrecognized 会把字节当毫秒、诊断会算错误率/P99 误诊
			s.fillSummary(name, f)
		}
	}
	return s
}

// fillHistogram 聚合 Histogram 家族：所有 label 组合的 count/sum 累加，
// 并按 result label 拆分（result 是全仓统一口径 ok/error）。
// bucket 跨 label 组合累加（同指标 bounds 相同，按桶索引对齐），供 P99 计算。
func (s *MetricSnapshot) fillHistogram(name string, f *dto.MetricFamily) {
	agg := s.Durations[name]
	if agg == nil {
		agg = &DurationAgg{Result: make(map[string]uint64)}
		s.Durations[name] = agg
	}
	byLabel := s.DurationsByLabel[name]
	if byLabel == nil {
		byLabel = make(map[string]*DurationAgg)
		s.DurationsByLabel[name] = byLabel
	}
	for _, m := range f.Metric {
		if m.Histogram == nil {
			continue
		}
		h := m.Histogram
		// 聚合（报告主口径）+ 按 label（诊断下钻）双写
		accumulateHistogram(agg, m, h)
		lk := labelsKey(m)
		lagg := byLabel[lk]
		if lagg == nil {
			lagg = &DurationAgg{Result: make(map[string]uint64)}
			byLabel[lk] = lagg
		}
		accumulateHistogram(lagg, m, h)
	}
}

// accumulateHistogram 把单条 histogram 样本累加进 agg（count/sum/result/bucket）。
// 聚合与按 label 两条路径复用，避免口径漂移。
func accumulateHistogram(agg *DurationAgg, m *dto.Metric, h *dto.Histogram) {
	agg.Count += h.GetSampleCount()
	agg.SumSec += h.GetSampleSum()
	// 按统一 result 口径拆分（兼容 grpc_code 归一化；无 result 的不进 Result）
	if r := resultOfMetric(m); r != "" {
		agg.Result[r] += h.GetSampleCount()
	}
	// 累加 bucket：bounds 首次记录，counts 按桶索引累加
	bounds := h.GetBucket()
	if len(bounds) == 0 {
		return
	}
	if len(agg.Bounds) == 0 {
		agg.Bounds = make([]float64, 0, len(bounds))
		agg.Counts = make([]uint64, len(bounds))
		for _, b := range bounds {
			agg.Bounds = append(agg.Bounds, b.GetUpperBound())
		}
	}
	// counts 是累计值（+Inf 桶含全部样本），逐桶累加
	for i, b := range bounds {
		if i < len(agg.Counts) {
			agg.Counts[i] += b.GetCumulativeCount()
		}
	}
}

// fillSummary 聚合 Summary 家族（与 Durations 物理隔离，sum 单位不一定是秒）。
func (s *MetricSnapshot) fillSummary(name string, f *dto.MetricFamily) {
	agg := s.Summaries[name]
	if agg == nil {
		agg = &DurationAgg{Result: make(map[string]uint64)}
		s.Summaries[name] = agg
	}
	for _, m := range f.Metric {
		if m.Summary == nil {
			continue
		}
		agg.Count += m.Summary.GetSampleCount()
		agg.SumSec += m.Summary.GetSampleSum()
		if r := resultOfMetric(m); r != "" {
			agg.Result[r] += m.Summary.GetSampleCount()
		}
	}
}

// fillCounter 聚合 Counter 家族：按 labelKey 累加。
// labelKey 用「label值拼接」（如 result=ok、op=Get），便于窗口差分与下钻。
func (s *MetricSnapshot) fillCounter(name string, f *dto.MetricFamily) {
	m := s.Counters[name]
	if m == nil {
		m = make(map[string]float64)
		s.Counters[name] = m
	}
	for _, metric := range f.Metric {
		if metric.Counter == nil {
			continue
		}
		m[labelsKey(metric)] += metric.Counter.GetValue()
	}
}

// fillGauge 聚合 Gauge 家族（当前值，不差分）。
func (s *MetricSnapshot) fillGauge(name string, f *dto.MetricFamily) {
	m := s.Gauges[name]
	if m == nil {
		m = make(map[string]float64)
		s.Gauges[name] = m
	}
	for _, metric := range f.Metric {
		if metric.Gauge == nil {
			continue
		}
		m[labelsKey(metric)] = metric.Gauge.GetValue()
	}
}

// labelValue 取指标样本的指定 label 值；不存在返回 ""。
func labelValue(m *dto.Metric, key string) string {
	for _, l := range m.Label {
		if l.GetName() == key {
			return l.GetValue()
		}
	}
	return ""
}

// resultOfMetric 取样本的统一 result 口径（ok/error），兼容第三方 provider：
//   - 原生 result label（全仓业务指标）优先；
//   - gRPC provider 用 grpc_code（OK/NotFound/Internal/...）而非 result——
//     在此归一化为 ok（OK）/error（其余），无需改 provider 和 dashboard 指标名，
//     grpc 指标才能进全仓错误率诊断与分组渲染。
//   - 都不存在返回 ""（该指标不参与 result 维度）。
func resultOfMetric(m *dto.Metric) string {
	if r := labelValue(m, "result"); r != "" {
		return r
	}
	if code := labelValue(m, "grpc_code"); code != "" {
		if code == "OK" {
			return "ok"
		}
		return "error"
	}
	return ""
}

// labelsKey 把样本所有 label 拼成稳定 key（按 label 名排序，保证差分时 key 一致）。
// 无 label 时返回 ""（单值指标）。
func labelsKey(m *dto.Metric) string {
	if len(m.Label) == 0 {
		return ""
	}
	// 收集并按名字排序
	type kv struct{ k, v string }
	pairs := make([]kv, 0, len(m.Label))
	for _, l := range m.Label {
		pairs = append(pairs, kv{l.GetName(), l.GetValue()})
	}
	// 插入排序（label 数量小，避免引入 sort 依赖开销）
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j].k < pairs[j-1].k; j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(p.k)
		b.WriteByte('=')
		b.WriteString(p.v)
	}
	return b.String()
}

// labelValueFromKey 从 labelsKey 生成的拼接串中精确取指定 label 的值。
// labelsKey 格式 "k=v,k=v"（按 k 排序），精确匹配 "key=" 后到下一个 ',' 之前，
// 避免 strings.Contains(key, "error") 误把 reason=error_timeout 当作 result=error。
func labelValueFromKey(key, label string) string {
	prefix := label + "="
	// 按 ',' 分段逐段精确匹配
	for _, seg := range strings.Split(key, ",") {
		if strings.HasPrefix(seg, prefix) {
			return seg[len(prefix):]
		}
	}
	return ""
}

// isErrorLabels 判断 labelsKey 拼接串是否表示失败（统一口径精确匹配）。
// 优先原生 result=error；兼容 grpc_code != OK（gRPC provider 不用 result label）。
func isErrorLabels(key string) bool {
	if labelValueFromKey(key, "result") == "error" {
		return true
	}
	if code := labelValueFromKey(key, "grpc_code"); code != "" && code != "OK" {
		return true
	}
	return false
}
