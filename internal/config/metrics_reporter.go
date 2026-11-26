package config

import (
	"log"
	"reflect"
	"strings"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// StartMetricsReporter 按配置启动指标报告器（必须在 logger.Init 之后调用，
// 未配置/配置非法时报告器内部会留 WARN/ERROR 日志）。
//
// 配置关系（原 app.enableMetrics 已物理合并进 metrics 段）：
//   - metrics.enabled 是指标体系【总开关】：false 时入口埋点/路由不装配，
//     报告器也一并不启动（否则推送缺 HTTP/gRPC 维度的残缺报告，且违背"关闭即无外发"）
//   - webhookRoutes/intervalSec/阈值 是总开关打开时的【报告参数】
//
// 行为：
//   - metrics.enabled=false → 不启动，留 WARN
//   - metrics.webhookRoutes 无有效路由 → 不启动（no-op），留 WARN，不产生外发流量
//   - 阈值字段全为 0 → 用框架默认基线；任一非 0 → 覆盖默认（零值回落）
//   - 幂等：重复调用只生效一次
//
// 用法（initApp，logger 初始化之后）：
//
//	config.StartMetricsReporter()
//
// opts 可追加业务扩展（如 WithDiagnosisHook 注入定制诊断），
// 会与 yml 阈值转换出的 options 合并（业务 opts 在后，可覆盖）。
func StartMetricsReporter(opts ...pkgmetrics.ReporterOption) {
	cfg := Get()

	// 总开关关闭：不启动报告器（与采集端装配行为一致，避免残缺报告外发）
	if !cfg.Metrics.Enabled {
		logReportDisabled("metrics.enabled=false（指标体系总开关关闭，报告器不启动）")
		return
	}

	all := buildReporterOptions(cfg.Metrics)
	all = append(all, opts...)
	pkgmetrics.StartReporter(cfg.Metrics.IntervalSec, all...)
}

// logReportDisabled 记录报告器未启动原因（logger 可能未就绪，直接用标准 log 留痕）。
func logReportDisabled(reason string) {
	log.Printf("[metrics-report] WARN: 指标报告未启动: %s", reason)
}

// StopMetricsReporter 停止指标报告器并等待在途推送结束（幂等）。
// 优雅关闭时调用：不中断正在推送的报告，避免半截消息。
func StopMetricsReporter() {
	pkgmetrics.StopReporter()
}

// buildReporterOptions 把配置映射为 ReporterOption。
// 仅当至少覆盖一个阈值时才返回 options——全零时返回空切片，
// 让框架走 DefaultDiagnosisThresholds（与传零值结构经 normalize 回落等效，
// 但显式区分，便于测试断言"是否覆盖"）。
func buildReporterOptions(cfg Metrics) []pkgmetrics.ReporterOption {
	opts := []pkgmetrics.ReporterOption{pkgmetrics.WithWebhookRoutes(buildWebhookRoutes(cfg.WebhookRoutes))}
	th, overridden := buildThresholds(cfg)
	if overridden {
		opts = append(opts, pkgmetrics.WithDiagnosisThresholds(th))
	}
	return opts
}

// buildWebhookRoutes 把生成的 []WebhookRoutes 转为框架 []pkgmetrics.WebhookRoute。
func buildWebhookRoutes(cfgRoutes []WebhookRoutes) []pkgmetrics.WebhookRoute {
	routes := make([]pkgmetrics.WebhookRoute, 0, len(cfgRoutes))
	for _, r := range cfgRoutes {
		routes = append(routes, pkgmetrics.WebhookRoute{
			Types:    r.Types,
			Levels:   r.Levels,
			Urls:     r.Urls,
			Mentions: toStringSlice(r.Mentions), // 生成器对 mentions 产出 []interface{}
		})
	}
	return routes
}

// buildThresholds 把配置阈值转为 DiagnosisThresholds，并返回是否至少覆盖一个字段。
//
// MinSamples 由 yml 生成器产出为 int（生成器统一用有符号类型），
// 这里转 uint64：负数按 0 处理（0 = 用框架默认，见 DiagnosisThresholds.normalize）。
func buildThresholds(cfg Metrics) (pkgmetrics.DiagnosisThresholds, bool) {
	th := pkgmetrics.DiagnosisThresholds{
		ErrRateWarn:     cfg.ErrRateWarn,
		ErrRateCritical: cfg.ErrRateCritical,
		P99Warn:         cfg.P99Warn,
		P99Critical:     cfg.P99Critical,
	}
	if cfg.MinSamples > 0 {
		th.MinSamples = uint64(cfg.MinSamples)
	}
	// GaugeAbove 是 map（DiagnosisThresholds 因此不可比较，不能用 != 判断零值），
	// 改为显式判断"是否有任何字段被覆盖"——语义与原 != 等价。
	th.GaugeAbove = buildGaugeAbove(cfg.GaugeAbove)
	overridden := cfg.ErrRateWarn != 0 || cfg.ErrRateCritical != 0 ||
		cfg.P99Warn != 0 || cfg.P99Critical != 0 || cfg.MinSamples > 0 ||
		len(th.GaugeAbove) > 0
	return th, overridden
}

// toStringSlice 把 []interface{} 转 []string（生成器对混合类型数组产出 interface{}）。
func toStringSlice(v []interface{}) []string {
	if len(v) == 0 {
		return nil
	}
	out := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			out = append(out, s)
		} else {
			// 非字符串元素（如误填数字）静默丢弃会让 @人 失效且无感知
			log.Printf("[metrics-report] WARN: mentions 含非字符串元素 %v（%T），已忽略", item, item)
		}
	}
	return out
}

// buildGaugeAbove 把生成的强类型 GaugeAbove struct 转为 map[key]阈值。
// 用反射遍历 yaml tag：生成器新增指标字段后无需改本函数（零值自动跳过）。
func buildGaugeAbove(cfg GaugeAbove) map[string]float64 {
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Struct || v.NumField() == 0 {
		return nil
	}
	var out map[string]float64
	t := reflect.TypeOf(cfg)
	floatType := reflect.TypeOf(float64(0))
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		// 生成器统一用有符号类型（int/float64），两种都接受
		if (f.Kind() != reflect.Float64 && f.Kind() != reflect.Int && f.Kind() != reflect.Int64) || f.IsZero() {
			continue // 零值 = 未配置
		}
		val := f.Convert(floatType).Float()
		if val < 0 {
			// 负阈值无意义（above 规则会永远命中）——视为未配置，留 WARN
			log.Printf("[metrics-report] WARN: gaugeAbove 阈值 %s=%v 为负数，已忽略", t.Field(i).Name, val)
			continue
		}
		// yaml tag 可能带选项（如 ",omitempty"），取逗号前的名字做 key
		key := strings.SplitN(t.Field(i).Tag.Get("yaml"), ",", 2)[0]
		if key == "" {
			continue
		}
		if out == nil {
			out = make(map[string]float64)
		}
		out[key] = val
	}
	return out
}
