package metrics

import (
	"fmt"
	"sort"
	"strings"
)

// gaugeRule kind 常量（避免 goconst 多处重复告警）。
const (
	gaugeRuleAbove  = "above"
	gaugeRuleEqZero = "eqZero"
)

// ============================================================================
// 诊断引擎：分析窗口增量，产出诊断结论（code/level/描述）。
//
// 与网关 diagnose 同思路：code 是稳定的异常标识（不含计数），
// 用于 fastCheck 判断"结论是否变化"——code 变了才即时推送，防刷屏。
//
// 通用化：遍历所有组件指标自动诊断（错误率/P99 超阈值），
// 不依赖指标名白名单。
// ============================================================================

// DiagnosisLevel 诊断等级。
type DiagnosisLevel int

const (
	// DiagnosisOK 一切正常
	DiagnosisOK DiagnosisLevel = iota
	// DiagnosisWarn 有异常但不严重（错误率/P99 超警告阈值）
	DiagnosisWarn
	// DiagnosisCritical 严重异常（超严重阈值）
	DiagnosisCritical
)

// String 等级文案。
func (l DiagnosisLevel) String() string {
	switch l {
	case DiagnosisOK:
		return "正常"
	case DiagnosisWarn:
		return "警告"
	case DiagnosisCritical:
		return "严重"
	}
	return "未知"
}

// Diagnosis 诊断结论。
type Diagnosis struct {
	Level DiagnosisLevel
	// Code 稳定的异常标识（不含计数），用于判断"结论是否变化"。
	// 取【最严重的一项】，保持与 fastCheck 变化检测的兼容语义。
	// 例："redis:error-rate>5%"、"http:p99>1s"。正常时为 "ok"。
	Code string
	// Desc 主要异常描述（最严重项，含具体数值）。
	Desc string
	// Details 全部异常描述（多组件同时异常时不吞，供报告展示全景）。
	// 注意：不参与 Code 计算——fastCheck 只对"最严重项变化"做即时推送，
	// 避免某次要异常恢复也触发推送（刷屏）。
	Details []string
	// Evidence 支撑信号列表（如 "502=180, 副本摘除=2"）。
	// 只做解释展示、不参与 Code/Level 判定——结论解释"怎么了"，
	// evidence 给出"凭什么这么判"的原始信号（业务 hook 常用）。
	Evidence []string
}

// DiagnosisHook 业务定制诊断钩子。
// 输入框架快照（含业务注册进统一中心的全部指标），返回定制诊断；
// 返回 nil 表示不接管，继续走框架通用诊断（错误率/慢请求阈值）。
//
// 平台给能力、业务给规则：框架不掺业务语义（如网关的 502 下游异常、
// 三段耗时归因），业务用 hook 注入自己的判定，报告链/推送/防抖/巡检
// 全部复用框架。
type DiagnosisHook func(s *MetricSnapshot, th DiagnosisThresholds) *Diagnosis

// DiagnosisThresholds 诊断阈值（大厂标准：默认基线 + 可按业务覆盖）。
// 不同业务容忍度差异大——支付接口 P99 1s 即严重、静态资源 5s 仍正常，
// 硬编码无法适配，故开放配置。
type DiagnosisThresholds struct {
	// ErrRateWarn 错误率警告阈值（0~1，默认 0.05 = 5%）。
	ErrRateWarn float64
	// ErrRateCritical 错误率严重阈值（默认 0.20 = 20%）。
	ErrRateCritical float64
	// P99Warn P99 耗时警告阈值（秒，默认 1.0）。
	P99Warn float64
	// P99Critical P99 耗时严重阈值（秒，默认 5.0）。
	P99Critical float64
	// MinSamples 最小样本数：样本太少错误率不可信，不诊断（防偶发单次失败刷告警）。
	MinSamples uint64
	// GaugeAbove Gauge 阈值告警规则：key=指标名（base），value=above 阈值。
	// 配置的阈值【覆盖】componentSpecs 内置默认值；未配置的指标用内置值。
	// map 使 DiagnosisThresholds 不可比较（不能用 != 判断零值），
	// 配置层需用显式字段判断，见 internal/config.buildThresholds。
	GaugeAbove map[string]float64
}

// DefaultDiagnosisThresholds 返回大厂常用基线阈值。
func DefaultDiagnosisThresholds() DiagnosisThresholds {
	return DiagnosisThresholds{
		ErrRateWarn:     0.05,
		ErrRateCritical: 0.20,
		P99Warn:         1.0,
		P99Critical:     5.0,
		MinSamples:      10,
	}
}

// normalize 补零值（调用方只覆盖部分字段时，零值回落到默认）。
func (t DiagnosisThresholds) normalize() DiagnosisThresholds {
	d := DefaultDiagnosisThresholds()
	if t.ErrRateWarn > 0 {
		d.ErrRateWarn = t.ErrRateWarn
	}
	if t.ErrRateCritical > 0 {
		d.ErrRateCritical = t.ErrRateCritical
	}
	if t.P99Warn > 0 {
		d.P99Warn = t.P99Warn
	}
	if t.P99Critical > 0 {
		d.P99Critical = t.P99Critical
	}
	if t.MinSamples > 0 {
		d.MinSamples = t.MinSamples
	}
	// GaugeAbove 是 map：normalize 从默认值 d 构造，必须显式复制，
	// 否则配置的 Gauge 阈值在 normalize 时被静默丢弃（配置了等于没配）。
	if t.GaugeAbove != nil {
		d.GaugeAbove = t.GaugeAbove
	}
	return d
}

// Diagnose 分析窗口增量，产出诊断结论（用默认阈值）。
func Diagnose(s *MetricSnapshot) Diagnosis {
	return DiagnoseWithThresholds(s, DefaultDiagnosisThresholds())
}

// DiagnoseWithThresholds 分析窗口增量，产出诊断结论。
// 遍历所有组件，取最严重的一个作为总体结论。
func DiagnoseWithThresholds(s *MetricSnapshot, th DiagnosisThresholds) Diagnosis {
	th = th.normalize()
	if s == nil {
		return Diagnosis{Level: DiagnosisOK, Code: "ok", Desc: "无数据"}
	}

	var worst = Diagnosis{Level: DiagnosisOK, Code: "ok", Desc: "正常"}
	var details []string

	// 遍历所有耗时指标，诊断错误率与 P99
	type cand struct {
		name string
		agg  *DurationAgg
	}
	// 预分配：map 长度已知，消除 append 扩容（诊断在 fastCheck 里周期性执行）
	cands := make([]cand, 0, len(s.Durations))
	for name, agg := range s.Durations {
		cands = append(cands, cand{name, agg})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].name < cands[j].name })

	for _, c := range cands {
		if c.agg == nil || c.agg.Count < th.MinSamples {
			continue // 样本太少不诊断
		}
		base := metricBase(c.name)

		// 错误率诊断
		errRate := c.agg.ErrorRate()
		if errRate >= th.ErrRateCritical {
			d := Diagnosis{
				Level: DiagnosisCritical,
				Code:  fmt.Sprintf("%s:err-rate>%.0f%%", base, th.ErrRateCritical*100),
				Desc:  fmt.Sprintf("%s 错误率 %.1f%%（阈值 %.0f%%）", componentLabel(base), errRate*100, th.ErrRateCritical*100),
			}
			worst = worse(worst, d)
			details = append(details, d.Desc)
		} else if errRate >= th.ErrRateWarn {
			d := Diagnosis{
				Level: DiagnosisWarn,
				Code:  fmt.Sprintf("%s:err-rate>%.0f%%", base, th.ErrRateWarn*100),
				Desc:  fmt.Sprintf("%s 错误率 %.1f%%（阈值 %.0f%%）", componentLabel(base), errRate*100, th.ErrRateWarn*100),
			}
			worst = worse(worst, d)
			details = append(details, d.Desc)
		}

		// P99 诊断
		if p99, ok := c.agg.P99Sec(); ok && c.agg.Count >= th.MinSamples {
			if p99 >= th.P99Critical {
				d := Diagnosis{
					Level: DiagnosisCritical,
					Code:  fmt.Sprintf("%s:p99>%.0fs", base, th.P99Critical),
					Desc:  fmt.Sprintf("%s P99 %s（阈值 %.0fs）", componentLabel(base), FormatDuration(p99), th.P99Critical),
				}
				worst = worse(worst, d)
				details = append(details, d.Desc)
			} else if p99 >= th.P99Warn {
				d := Diagnosis{
					Level: DiagnosisWarn,
					Code:  fmt.Sprintf("%s:p99>%.0fs", base, th.P99Warn),
					Desc:  fmt.Sprintf("%s P99 %s（阈值 %.0fs）", componentLabel(base), FormatDuration(p99), th.P99Warn),
				}
				worst = worse(worst, d)
				details = append(details, d.Desc)
			}
		}
	}

	// Counter 类错误诊断：etcd_token_refresh / nacos_config_callback 等
	// 静默故障指标是 Counter（非 Histogram），上面的 Durations 循环看不到。
	// 这些是 Phase 1 特意加的"失败静默"指标——只展示不告警违背初衷。
	// 口径：窗口内出现 result=error 样本即 warn（此类操作低频，偶发错误就值得关注）。
	counterNames := make([]string, 0, len(s.Counters))
	for name := range s.Counters {
		counterNames = append(counterNames, name)
	}
	sort.Strings(counterNames)
	for _, name := range counterNames {
		cm := s.Counters[name]
		var total, errs float64
		for key, v := range cm {
			total += v
			// 精确匹配 result=error（避免 result=error_timeout 等误判）
			if isErrorLabels(key) {
				errs += v
			}
		}
		if errs == 0 {
			continue
		}
		base := metricBase(name)
		var desc string
		if total <= errs {
			// 只有 error 样本（如 panic_recovered_total / request_timeout_total，
			// 埋点只记 error 不记 ok）→ 没有失败率分母，显示 100% 会误导。
			// 改显示累计异常次数，语义准确。
			desc = fmt.Sprintf("%s 累计异常 %.0f 次", componentLabel(base), errs)
		} else {
			failRate := errs / total * 100
			desc = fmt.Sprintf("%s 失败 %.0f/%.0f 次（%.1f%%）", componentLabel(base), errs, total, failRate)
		}
		d := Diagnosis{
			Level: DiagnosisWarn,
			Code:  fmt.Sprintf("%s:counter-error", base),
			Desc:  desc,
		}
		worst = worse(worst, d)
		details = append(details, d.Desc)
	}

	worst, details = diagnoseGauges(s, th, worst, details)

	worst.Details = details
	return worst
}

// diagnoseGauges Gauge 类阈值诊断（从主诊断函数拆出，控制复杂度）。
// 按 componentSpecs 声明的 gaugeRule 或 yml 配置（th.GaugeAbove）判定。
func diagnoseGauges(s *MetricSnapshot, th DiagnosisThresholds, worst Diagnosis, details []string) (Diagnosis, []string) {
	gaugeNames := make([]string, 0, len(s.Gauges))
	for name := range s.Gauges {
		gaugeNames = append(gaugeNames, name)
	}
	sort.Strings(gaugeNames)
	for _, name := range gaugeNames {
		base := metricBase(name)
		spec, specOK := componentSpecs[base]
		// 规则优先级：yml 配置（th.GaugeAbove）覆盖 componentSpecs 内置默认值；
		// 配置了但内置无规则也生效（配置即声明）；两者都没有则不诊断。
		var rule *gaugeRule
		if th.GaugeAbove != nil {
			if thVal, ok := th.GaugeAbove[base]; ok {
				rule = &gaugeRule{kind: gaugeRuleAbove, threshold: thVal, warnLevel: DiagnosisWarn}
			}
		}
		if rule == nil && specOK {
			rule = spec.gaugeRule
		}
		if rule == nil {
			continue
		}
		for key, value := range s.Gauges[name] {
			hit := false
			switch rule.kind {
			case gaugeRuleEqZero:
				hit = value == 0
			case gaugeRuleAbove:
				hit = value > rule.threshold
			default:
				// 未知 kind：启动时（StartReporter）已校验并打 ERROR，
				// 这里静默跳过——fastCheck 每 5s 跑一次，重复 WARN 会日志风暴
				continue
			}
			if !hit {
				continue
			}
			label := base
			if specOK {
				label = spec.label
			}
			d := Diagnosis{
				Level: rule.warnLevel,
				// Code 不含当前值——只表示"哪类异常"，保证 fastCheck 变化检测稳定
				Code: fmt.Sprintf("%s:gauge-%s", base, rule.kind),
				// Desc 用中文描述 + 阈值：运维看到告警要知道"超了什么、阈值多少"
				Desc: fmt.Sprintf("%s %s（当前 %.1f，阈值 %s，%s）", label,
					gaugeRuleDesc(rule), value, formatGaugeThreshold(rule), key),
			}
			worst = worse(worst, d)
			details = append(details, d.Desc)
		}
	}
	return worst, details
}

// gaugeRuleDesc 把规则类型转成中文告警描述。
func gaugeRuleDesc(rule *gaugeRule) string {
	switch rule.kind {
	case gaugeRuleEqZero:
		return "值异常（归零）"
	case gaugeRuleAbove:
		return "超过阈值"
	}
	return rule.kind
}

// formatGaugeThreshold 输出阈值文案（eqZero 无阈值）。
func formatGaugeThreshold(rule *gaugeRule) string {
	if rule.kind == gaugeRuleAbove {
		return fmt.Sprintf("%.1f", rule.threshold)
	}
	return "0"
}

// worse 返回更严重的诊断（level 更高优先；同级保留先出现的稳定 code）。
func worse(a, b Diagnosis) Diagnosis {
	if b.Level > a.Level {
		return b
	}
	return a
}

// componentLabel 取组件中文名（未知返回 base）。
func componentLabel(base string) string {
	if spec, ok := componentSpecs[base]; ok {
		return spec.label
	}
	return base
}

// RenderDiagnosis 把诊断结论渲染成报告头部（正常不输出，异常输出告警行）。
func RenderDiagnosis(d Diagnosis) string {
	// OK 但带 Evidence：业务 hook 的正常摘要（如"全部信号正常：502=0"）也要展示；
	// 框架默认诊断 OK 时 Evidence 为空，行为不变。
	if d.Level == DiagnosisOK && len(d.Evidence) == 0 {
		return ""
	}
	icon := "⚠️"
	switch d.Level {
	case DiagnosisCritical:
		icon = "🔴"
	case DiagnosisOK:
		icon = "✅"
	}
	items := d.Details
	if len(items) == 0 && d.Level != DiagnosisOK {
		items = []string{d.Desc}
	}
	var b strings.Builder
	if len(items) > 0 {
		fmt.Fprintf(&b, "%s %s（%d 项）：\n", icon, d.Level.String(), len(items))
		for _, item := range items {
			b.WriteString("  · " + item + "\n")
		}
	}
	// Evidence 支撑信号：只展示、不参与判定
	if len(d.Evidence) > 0 {
		fmt.Fprintf(&b, "%s 信号：%s\n", icon, strings.Join(d.Evidence, "，"))
	}
	return b.String()
}

// mergeDiagnosisIntoReport 把诊断行插入报告标题后。
func mergeDiagnosisIntoReport(report string, d Diagnosis) string {
	line := RenderDiagnosis(d)
	if line == "" {
		return report
	}
	// 插入到第一行（标题）之后
	idx := strings.Index(report, "\n")
	if idx < 0 {
		return line + report
	}
	return report[:idx+1] + line + report[idx+1:]
}
