package config

import (
	"log"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// StartMetricsReporter 按配置启动指标报告器（必须在 logger.Init 之后调用，
// 未配置/配置非法时报告器内部会留 WARN/ERROR 日志）。
//
// 配置关系（原 app.enableMetrics 已物理合并进 metrics 段）：
//   - metrics.enabled 是指标体系【总开关】：false 时入口埋点/路由不装配，
//     报告器也一并不启动（否则推送缺 HTTP/gRPC 维度的残缺报告，且违背"关闭即无外发"）
//   - webhookURL/intervalSec/阈值 是总开关打开时的【报告参数】
//
// 行为：
//   - metrics.enabled=false → 不启动，留 WARN
//   - metrics.webhookURL 为空 → 不启动（no-op），留 WARN，不产生外发流量
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
	pkgmetrics.StartReporter(cfg.Metrics.WebhookURL, cfg.Metrics.IntervalSec, all...)
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
	th, overridden := buildThresholds(cfg)
	if !overridden {
		return nil
	}
	return []pkgmetrics.ReporterOption{pkgmetrics.WithDiagnosisThresholds(th)}
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
	return th, th != (pkgmetrics.DiagnosisThresholds{})
}
