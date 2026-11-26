package metrics

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ============================================================================
// 报告渲染：把窗口增量 MetricSnapshot 渲染成企业微信文本。
//
// 通用设计：按指标名自动识别组件（componentOf），分组展示。
// 任何注册到统一中心的组件指标无需改渲染代码即可出现。
// ============================================================================

// componentSpec 组件展示规格：指标名 -> 中文名 + 分组。
type componentSpec struct {
	label string // 中文名（如 "Redis"）
	group string // 分组（如 "数据层"）
	kind  string // "duration"(耗时) / "counter"(计数) / "cache"(带命中率)
	unit  string // 计量单位（""=次）；如 uptime 的"分钟"
}

// componentSpecs 指标名 -> 展示规格。key 为指标 base 名（去 _duration_seconds/_total 后缀）。
var componentSpecs = map[string]componentSpec{
	// HTTP
	"http_request": {label: "HTTP接入", group: groupHTTP, kind: kindDuration},
	"http_client":  {label: "HTTP出站", group: groupHTTP, kind: kindDuration},
	// gRPC（provider 用 grpc_code，采集层已归一化为 result=ok/error）
	"grpc_server_handling":           {label: "gRPC服务端耗时", group: groupHTTP, kind: kindDuration},
	"grpc_client_handling":           {label: "gRPC客户端耗时", group: groupHTTP, kind: kindDuration},
	"grpc_server_handled":            {label: "gRPC服务端调用", group: groupHTTP, kind: kindCounter},
	"grpc_client_handled":            {label: "gRPC客户端调用", group: groupHTTP, kind: kindCounter},
	"grpc_server_active_connections": {label: "gRPC服务端活跃连接", group: groupHTTP, kind: kindGauge},
	"grpc_client_active_connections": {label: "gRPC客户端活跃连接", group: groupHTTP, kind: kindGauge},
	// 数据层
	"redis_operation": {label: "Redis", group: groupData, kind: kindDuration},
	"cache_operation": {label: "Cache", group: groupData, kind: kindCache},
	"db_query":        {label: "DB查询", group: groupData, kind: kindDuration},
	"es_operation":    {label: "ES", group: groupData, kind: kindDuration},
	"query_build":     {label: "查询条件构建", group: groupData, kind: kindDuration},
	// 消息/任务
	"rabbitmq_publish": {label: "RabbitMQ发布", group: groupMsgTask, kind: kindDuration},
	"rabbitmq_consume": {label: "RabbitMQ消费", group: groupMsgTask, kind: kindDuration},
	"cron_task":        {label: "定时任务", group: groupMsgTask, kind: kindDuration},
	// 基础设施
	"lock_acquire":          {label: "分布式锁", group: groupInfra, kind: kindDuration},
	"nacos_config_fetch":    {label: "Nacos配置拉取", group: groupInfra, kind: kindDuration},
	"etcd_token_refresh":    {label: "etcd token刷新", group: groupInfra, kind: kindCounter},
	"nacos_config_callback": {label: "Nacos配置回调", group: groupInfra, kind: kindCounter},
	// 运行时（gauge，不差分）
	"go_goroutines":                {label: "goroutine", group: groupRuntime, kind: kindGauge},
	"go_memstats_alloc_bytes":      {label: "内存分配", group: groupRuntime, kind: kindGauge},
	"logger_router_active_loggers": {label: "活跃logger", group: groupRuntime, kind: kindGauge},
	"uptime":                       {label: "运行时长", group: groupRuntime, kind: kindCounter, unit: "分钟"},
}

// 分组名常量（goconst：避免重复字符串字面量）。
const (
	groupHTTP    = "HTTP"
	groupData    = "数据层"
	groupMsgTask = "消息/任务"
	groupInfra   = "基础设施"
	groupRuntime = "运行时"
	kindDuration = "duration"
	kindCounter  = "counter"
	kindCache    = "cache"
	kindGauge    = "gauge"
)

// groupOrder 报告分组顺序。
var groupOrder = []string{groupHTTP, groupData, groupMsgTask, groupInfra, groupRuntime}

// metricBase 从指标名提取 base（去单位后缀）。
// 顺序：先裁 _duration_seconds（标准耗时后缀），再裁 _seconds（gRPC 的
// _handling_seconds 等非标准耗时后缀），最后裁 _total。
// 不裁剪 _seconds 会导致 grpc_server_handling_seconds 的 base 不变、
// 无法命中 componentSpecs（走兜底丢中文标签/分组）。
func metricBase(name string) string {
	name = strings.TrimSuffix(name, "_duration_seconds")
	name = strings.TrimSuffix(name, "_seconds")
	name = strings.TrimSuffix(name, "_total")
	return name
}

// RenderReport 把窗口增量渲染成企业微信文本。
// window 为窗口时长（算 QPS）；instance 为实例标识。
func RenderReport(s *MetricSnapshot, window time.Duration, instance string) string {
	if s == nil {
		return "指标快照为空"
	}
	sec := window.Seconds()
	if sec <= 0 {
		sec = 1
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📊 sunshine 指标报告 · %s · %s\n", FormatIntervalText(window), instance)
	b.WriteString(strings.Repeat("─", 28) + "\n")

	// 按分组渲染
	for _, group := range groupOrder {
		lines := renderGroup(s, group, sec)
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "【%s】\n", group)
		for _, ln := range lines {
			b.WriteString(ln + "\n")
		}
	}

	// 未识别的指标（新增组件未加 spec）：兜底展示，避免静默丢失
	if extra := renderUnrecognized(s, sec); len(extra) > 0 {
		b.WriteString("【其他】\n")
		for _, ln := range extra {
			b.WriteString(ln + "\n")
		}
	}

	return b.String()
}

// renderGroup 渲染一个分组的所有组件行。
func renderGroup(s *MetricSnapshot, group string, sec float64) []string {
	var lines []string
	// 收集该分组的指标（按 label 排序保证稳定）
	type item struct {
		base string
		spec componentSpec
	}
	var items []item
	for name := range s.Durations {
		base := metricBase(name)
		spec, ok := componentSpecs[base]
		if ok && spec.group == group {
			items = append(items, item{base, spec})
		}
	}
	for name := range s.Counters {
		base := metricBase(name)
		spec, ok := componentSpecs[base]
		if ok && spec.group == group {
			// 避免与 durations 重复
			dup := false
			for _, it := range items {
				if it.base == base {
					dup = true
					break
				}
			}
			if !dup {
				items = append(items, item{base, spec})
			}
		}
	}
	for name := range s.Gauges {
		base := metricBase(name)
		spec, ok := componentSpecs[base]
		if ok && spec.group == group {
			dup := false
			for _, it := range items {
				if it.base == base {
					dup = true
					break
				}
			}
			if !dup {
				items = append(items, item{base, spec})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].spec.label < items[j].spec.label })

	for _, it := range items {
		if ln := renderItem(s, it.base, it.spec, sec); ln != "" {
			lines = append(lines, ln)
		}
	}
	return lines
}

// renderItem 渲染单个组件行。
func renderItem(s *MetricSnapshot, base string, spec componentSpec, sec float64) string {
	// 优先从 Durations 找（duration/cache 类）
	for name, agg := range s.Durations {
		if metricBase(name) != base {
			continue
		}
		line := renderDurationLine(spec.label, agg, sec, s.DurationsByLabel[name])
		// cache 类追加命中率（从 cache_hit_total/cache_miss_total 取）
		if spec.kind == "cache" {
			if hitRate, ok := cacheHitRate(s); ok {
				line += fmt.Sprintf(" · 命中 %.1f%%", hitRate*100)
			}
		}
		return line
	}
	// Counter 类
	for name, cm := range s.Counters {
		if metricBase(name) != base {
			continue
		}
		return renderCounterLine(spec.label, cm, sec, spec.unit)
	}
	// Gauge 类（运行时）
	for name, gm := range s.Gauges {
		if metricBase(name) != base {
			continue
		}
		return renderGaugeLine(spec.label, gm)
	}
	return ""
}

// renderDurationLine 渲染耗时组件行：QPS · 错误率 · P99。
// cache 类额外显示命中率（从 cache_hit_total/cache_miss_total 取）。
func renderDurationLine(label string, agg *DurationAgg, sec float64, breakdown map[string]*DurationAgg) string {
	if agg == nil || agg.Count == 0 {
		return fmt.Sprintf("  %s: 无样本", label)
	}
	qps := float64(agg.Count) / sec
	errRate := agg.ErrorRate() * 100
	line := fmt.Sprintf("  %s: QPS %.1f · 错误 %.2f%%", label, qps, errRate)
	if p99, ok := agg.P99Sec(); ok {
		line += fmt.Sprintf(" · P99 %s", FormatMs(p99))
	} else if mean := agg.MeanSec(); mean > 0 {
		line += fmt.Sprintf(" · 均值 %s", FormatMs(mean))
	}
	return line + renderBreakdown(breakdown)
}

// renderBreakdown 分链下钻：label 维度的耗时分解（如网关 api/notify 两条链、
// 副本维度的转发耗时）——聚合 P99 会把多条链混在一起，慢链被快链稀释后看不见。
//
// 显示条件：2~6 个 label 组合（1 个无信息量；>6 太细，聚合已够且刷屏），
// 按 P99 降序取 top3（最慢的链是运维最关心的）。
func renderBreakdown(byLabel map[string]*DurationAgg) string {
	// 按第一个业务维度分组：labelsKey 含 result/status 等次要维度，
	// 直接用完整 key 会把同一条链（如 entry=api 的 ok/error）拆成多条。
	// 同维度合并桶后再算 P99，才是"每条链的真实耗时分布"。
	merged := make(map[string]*DurationAgg, len(byLabel))
	for lk, agg := range byLabel {
		if agg == nil || agg.Count == 0 {
			continue
		}
		k := compactLabelKey(lk)
		dst := merged[k]
		if dst == nil {
			dst = &DurationAgg{Result: make(map[string]uint64)}
			merged[k] = dst
		}
		mergeDurationAgg(dst, agg)
	}
	n := len(merged)
	if n < 2 || n > 6 {
		return ""
	}
	type kv struct {
		k   string
		p99 float64
	}
	items := make([]kv, 0, n)
	for k, agg := range merged {
		if p99, ok := agg.P99Sec(); ok {
			items = append(items, kv{k, p99})
		}
	}
	if len(items) < 2 {
		return ""
	}
	sort.Slice(items, func(i, j int) bool { return items[i].p99 > items[j].p99 })
	if len(items) > 3 {
		items = items[:3]
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s=%s", it.k, FormatMs(it.p99)))
	}
	return " · 分链 " + strings.Join(parts, ", ")
}

// mergeDurationAgg 累加窗口增量聚合（同指标同桶定义，Bounds 取首次）。
func mergeDurationAgg(dst, src *DurationAgg) {
	dst.Count += src.Count
	dst.SumSec += src.SumSec
	for r, n := range src.Result {
		dst.Result[r] += n
	}
	if len(dst.Bounds) == 0 && len(src.Bounds) > 0 {
		dst.Bounds = append([]float64(nil), src.Bounds...)
		dst.Counts = make([]uint64, len(src.Counts))
	}
	// 桶按索引对齐（同指标固定），长度不同时取较小值防越界
	for i := 0; i < len(src.Counts) && i < len(dst.Counts); i++ {
		dst.Counts[i] += src.Counts[i]
	}
}

// renderCounterLine 渲染计数组件行（如 etcd token 刷新、nacos 回调）。
func renderCounterLine(label string, cm map[string]float64, _ float64, unit ...string) string {
	u := "次"
	if len(unit) > 0 && unit[0] != "" {
		u = unit[0]
	}
	var total, errs float64
	for k, v := range cm {
		total += v
		// 精确匹配 result=error（避免 reason=error_timeout 等误判）
		if isErrorLabels(k) {
			errs += v
		}
	}
	if total == 0 {
		return fmt.Sprintf("  %s: 0 %s", label, u)
	}
	line := fmt.Sprintf("  %s: %.0f %s", label, total, u)
	if errs > 0 {
		line += fmt.Sprintf(" · 错误 %.0f", errs)
	}
	// 非 result 维度的分布（如 status=502 / reason=xxx）：按值降序展示 top3，
	// 否则多 label counter 被聚合成单值，运维丢关键信号（如 5xx 分布）。
	if dist := labelDistribution(cm, "result", 3); dist != "" {
		line += " · " + dist
	}
	return line
}

// labelDistribution 提取 labelKey 中指定维度的分布，按值降序取前 limit 个，
// 渲染为 "502=17 · 200=100"。skipKey 跳过的维度（如 result 已并入错误率）。
// 只有一个 key 或全为同值时不输出（避免噪音）。
func labelDistribution(cm map[string]float64, skipKey string, limit int) string {
	type kv struct {
		k string
		v float64
	}
	var items []kv
	seen := map[string]bool{}
	for lk, v := range cm {
		// 提取除 skipKey（result，已并入错误率）之外的维度值
		val := nonResultValue(lk, skipKey)
		if val == "" {
			continue // 只有 result 维度或无维度
		}
		if seen[val] {
			// 同值多 key（如多副本同状态）：累加
			for i := range items {
				if items[i].k == val {
					items[i].v += v
					break
				}
			}
			continue
		}
		seen[val] = true
		items = append(items, kv{val, v})
	}
	if len(items) <= 1 {
		return "" // 单值分布无信息量（total 已表达）
	}
	sort.Slice(items, func(i, j int) bool { return items[i].v > items[j].v })
	if len(items) > limit {
		items = items[:limit]
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s=%.0f", it.k, it.v))
	}
	return strings.Join(parts, " · ")
}

// renderGaugeLine 渲染 gauge 行（运行时当前值）。
func renderGaugeLine(label string, gm map[string]float64) string {
	if len(gm) == 0 {
		return fmt.Sprintf("  %s: -", label)
	}
	// 单值（key 为空或唯一）：直接渲染
	if len(gm) == 1 {
		for _, v := range gm {
			if label == "内存分配" {
				return fmt.Sprintf("  %s: %s", label, FormatBytes(v))
			}
			return fmt.Sprintf("  %s: %.0f", label, v)
		}
	}
	// 多 label（如副本健康态 replica=a:1, replica=b:0）：逐 key 渲染，
	// 绝不能只取第一个（会丢维度——4 副本只显示 1 个是信息丢失）。
	keys := make([]string, 0, len(gm))
	for k := range gm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	const maxShow = 4
	parts := make([]string, 0, len(keys))
	for i, k := range keys {
		if i >= maxShow {
			parts = append(parts, fmt.Sprintf("…共%d个", len(keys)))
			break
		}
		parts = append(parts, fmt.Sprintf("%s=%.0f", compactLabelKey(k), gm[k]))
	}
	return fmt.Sprintf("  %s: %s", label, strings.Join(parts, ", "))
}

// compactLabelKey 压缩 labelKey 为可读短键：取第一个维度的值
// （labelsKey 按维度名排序，多 key 场景第一个维度通常是区分主键，
// 如副本健康态的 replica=a:80；重复的公共维度如 route=order 省略）。
func compactLabelKey(lk string) string {
	first, _, ok := strings.Cut(lk, ",")
	if !ok {
		first = lk // 单维度
	}
	if _, v, ok := strings.Cut(first, "="); ok {
		return v
	}
	return first
}

// renderUnrecognized 渲染未在 componentSpecs 中的指标（兜底，防新增组件静默丢失）。
func renderUnrecognized(s *MetricSnapshot, sec float64) []string {
	var lines []string
	var names []string
	seen := map[string]bool{}
	// Durations / Counters / Gauges 三个来源都要兜底，
	// 否则新增的 Counter/Gauge 组件指标会静默丢失。
	for name := range s.Durations {
		if _, ok := componentSpecs[metricBase(name)]; !ok && !seen[metricBase(name)] {
			seen[metricBase(name)] = true
			names = append(names, name)
		}
	}
	for name := range s.Counters {
		if _, ok := componentSpecs[metricBase(name)]; !ok && !seen[metricBase(name)] {
			seen[metricBase(name)] = true
			names = append(names, name)
		}
	}
	for name := range s.Gauges {
		if _, ok := componentSpecs[metricBase(name)]; !ok && !seen[metricBase(name)] {
			seen[metricBase(name)] = true
			names = append(names, name)
		}
	}
	for name := range s.Summaries {
		if _, ok := componentSpecs[metricBase(name)]; !ok && !seen[metricBase(name)] {
			seen[metricBase(name)] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if agg, ok := s.Durations[name]; ok {
			lines = append(lines, renderDurationLine(name, agg, sec, s.DurationsByLabel[name]))
		} else if agg, ok := s.Summaries[name]; ok {
			lines = append(lines, renderSummaryLine(name, agg, sec))
		} else if cm, ok := s.Counters[name]; ok {
			lines = append(lines, renderCounterLine(name, cm, sec))
		} else if gm, ok := s.Gauges[name]; ok {
			lines = append(lines, renderGaugeLine(name, gm))
		}
	}
	return lines
}

// renderSummaryLine 渲染 Summary 家族（如 *_size_bytes）：均值按字节/通用值展示，
// 绝不套用耗时的 ms 或错误率/P99 口径。
func renderSummaryLine(name string, agg *DurationAgg, sec float64) string {
	if agg == nil || agg.Count == 0 {
		return fmt.Sprintf("  %s: 无样本", name)
	}
	mean := agg.SumSec / float64(agg.Count)
	unit := "B"
	if !strings.Contains(name, "_bytes") {
		unit = "" // 非字节 Summary：只渲染裸均值，不臆测单位
	}
	if unit == "B" {
		return fmt.Sprintf("  %s: 均值 %s/次 · QPS %.1f", name, FormatBytes(mean), float64(agg.Count)/sec)
	}
	return fmt.Sprintf("  %s: 均值 %.2f/次 · QPS %.1f", name, mean, float64(agg.Count)/sec)
}

// nonResultValue 提取 labelKey 中最后一个非 skipKey 维度的值。
// labelsKey 按维度名排序，业务惯例把告警关键维度（status/reason）放最后——
// 取末位得到 "200"/"502" 这类干净的分布键；只有 skipKey（result，已并入
// 错误率）时返回空。
func nonResultValue(labelKey, skipKey string) string {
	last := ""
	for _, pair := range strings.Split(labelKey, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == skipKey {
			continue
		}
		last = v
	}
	return last
}

// cacheHitRate 从 cache_hit_total/cache_miss_total 计算命中率。
// 返回 (命中率, 是否有数据)。
func cacheHitRate(s *MetricSnapshot) (float64, bool) {
	var hit, miss float64
	found := false
	for name, cm := range s.Counters {
		base := metricBase(name)
		if base == "cache_hit" {
			for _, v := range cm {
				hit += v
			}
			found = true
		}
		if base == "cache_miss" {
			for _, v := range cm {
				miss += v
			}
			found = true
		}
	}
	if !found || hit+miss == 0 {
		return 0, false
	}
	return hit / (hit + miss), true
}
