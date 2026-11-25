package metrics

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ============================================================================
// 指标报告器：周期采集 → 窗口差分 → 诊断 → 渲染 → 企微推送。
//
// 双节奏（从网关移植，线上验证）：
//   - 周期推送（默认 60s）：完整报告，含运行时指标
//   - 快速巡检（5s）：诊断结论变化就即时补推，异常秒级发现，不等下个周期
//
// 关键不变量：
//   - prev/lastCode/lastPushAt 只在【推送成功】后推进——失败保留现场，
//     下个巡检点重试，否则一次推送失败会吞掉异常告警。
//   - 防抖：两次即时推送至少间隔 30s（企微限频 20 条/分钟）。
// ============================================================================

const (
	// defaultReportInterval 默认报告间隔。
	defaultReportInterval = 60 * time.Second
	// minReportInterval 最小间隔：企微限频 20 条/分钟，低于 10s 有风险，钳制。
	minReportInterval = 10 * time.Second
	// fastCheckInterval 快速巡检节奏：异常要秒级发现。
	fastCheckInterval = 5 * time.Second
	// fastPushDebounce 即时推送防抖（周期推送不刷新该计时）。
	fastPushDebounce = 30 * time.Second
)

// ============================================================================
// 报告器自监控指标（大厂标准：监控系统必须自监控）
//
// 为什么必须有：报告器是"监控的监控"。它自身故障（webhook 持续失败/被限频/
// 网络断）时只有被限流的 WARN 日志，Prometheus 无法告警"报告器挂了"——
// 运维分不清"群里没消息"是业务正常还是报告器故障。
//
// 两个指标即可覆盖：
//   - push_total{result}：失败率突增 = webhook 故障
//   - last_push_timestamp_seconds：time()-值 过大 = 报告器停摆/从未成功推送
//
// ============================================================================
var (
	// reporterPushTotal 推送尝试计数（result=ok/error）。
	reporterPushTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "metrics_reporter_push_total",
		Help: "Metric reporter webhook push attempts by result (ok/error).",
	}, []string{"result"})

	// reporterLastPushTimestamp 上次【成功】推送的 Unix 时间戳（gauge）。
	// 监控表达式：time() - metrics_reporter_last_push_timestamp_seconds > 180
	// = 报告器停摆（或从未成功推送，值为 0）。
	reporterLastPushTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "metrics_reporter_last_push_timestamp_seconds",
		Help: "Unix timestamp of last successful metrics report push (0 if never).",
	})
)

func init() {
	MustRegister(reporterPushTotal, reporterLastPushTimestamp)
}

// observeReporterPush 记录一次推送结果（自监控埋点）。
func observeReporterPush(err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	reporterPushTotal.WithLabelValues(result).Inc()
	if err == nil {
		reporterLastPushTimestamp.SetToCurrentTime()
	}
}

// Reporter 持有报告器生命周期状态。
// prev/lastCode/lastPushAt 等只在 push 成功后推进（失败保留现场）。
type Reporter struct {
	webhook             string
	interval            time.Duration
	prev                *MetricSnapshot                 // 上一窗口累计快照，仅 run goroutine 访问
	lastCode            string                          // 上次诊断 code，仅 run goroutine 访问
	lastPushAt          time.Time                       // 上次推送时间（算窗口标签）
	lastImmediatePushAt time.Time                       // 上次即时推送时间（防抖）
	lastFailAt          time.Time                       // 上次推送失败时间（失败退避）
	instance            string                          // 实例标识
	thresholds          DiagnosisThresholds             // 诊断阈值（零值 → 用默认）
	gatherFn            func() (*MetricSnapshot, error) // 测试注入；nil 走 GatherFast
	done                chan struct{}
	wg                  sync.WaitGroup
}

var (
	reportMu       sync.Mutex
	reportInstance *Reporter

	// 日志限流：gather 持续失败时（如 registry 异常），periodic+fastCheck
	// 每 5s 会刷一条 WARN——日志风暴会淹没其他告警，且浪费 IO。
	// 大厂标准：同类错误周期内只打一条，被抑制的计数在下一条日志里汇总。
	gatherLogMu      sync.Mutex
	gatherLogLastAt  time.Time
	gatherLogSkipped int
)

// logGatherError 限流输出 gather 失败日志（同一类错误 logRateLimit 内只打一条）。
func logGatherError(stage string, err error) {
	gatherLogMu.Lock()
	defer gatherLogMu.Unlock()
	now := time.Now()
	if !gatherLogLastAt.IsZero() && now.Sub(gatherLogLastAt) < logRateLimit {
		gatherLogSkipped++
		return
	}
	skipped := gatherLogSkipped
	gatherLogSkipped = 0
	gatherLogLastAt = now
	if skipped > 0 {
		log.Printf("[metrics-report] WARN: %s failed: %v ( suppressed %d similar errors in last %s )",
			stage, err, skipped, logRateLimit)
		return
	}
	log.Printf("[metrics-report] WARN: %s failed: %v", stage, err)
}

// logRateLimit 同类 gather 错误日志的最小间隔。
const logRateLimit = time.Minute

// ReporterOption 报告器可选项（大厂标准：默认 + 可覆盖）。
type ReporterOption func(*Reporter)

// WithDiagnosisThresholds 覆盖诊断阈值（不同业务容忍度不同：
// 支付 P99 1s 即严重，静态资源 5s 仍正常）。未覆盖的字段回落默认值。
func WithDiagnosisThresholds(th DiagnosisThresholds) ReporterOption {
	return func(r *Reporter) {
		r.thresholds = th
	}
}

// StartReporter 按配置启动指标报告器（幂等，重复调用只生效一次）。
// webhook 留空即 no-op；interval<=0 取默认 60s，低于 10s 钳到 10s（企微限频）。
func StartReporter(webhookURL string, intervalSec int, opts ...ReporterOption) {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		// 必须留痕：yml 不入库，生产漏配时若静默返回，
		// 运维会以为"报告器在跑"，实际一条都收不到。
		log.Println("[metrics-report] WARN: webhook 未配置，指标报告未启动（群机器人不会收到任何消息）")
		return
	}
	host, err := extractWebhookHost(webhookURL)
	if err != nil {
		// 配置错误必须拒绝启动并留痕：带病运行只会让运维误以为报告器在干活。
		log.Printf("[metrics-report] ERROR: webhook URL 非法，指标报告未启动: %v", err)
		return
	}
	if !IsWeComWebhookHost(host) {
		log.Printf("[metrics-report] WARN: webhook 不是企业微信群机器人地址，指标可能被推到外部地址 host=%s", host)
	}
	interval := time.Duration(intervalSec) * time.Second
	if interval <= 0 {
		interval = defaultReportInterval
	}
	if interval < minReportInterval {
		interval = minReportInterval
	}

	reportMu.Lock()
	defer reportMu.Unlock()
	if reportInstance != nil {
		return
	}
	r := &Reporter{
		webhook:  webhookURL,
		interval: interval,
		instance: instanceHostname(),
		done:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(r)
	}
	r.wg.Add(1)
	go r.run()
	reportInstance = r
}

// StopReporter 停止报告器并等待在途推送结束。幂等。
func StopReporter() {
	reportMu.Lock()
	r := reportInstance
	reportInstance = nil
	reportMu.Unlock()
	if r == nil {
		return
	}
	close(r.done)
	r.wg.Wait()
}

// run 主循环：周期报告 + 快速巡检两个 ticker。
func (r *Reporter) run() {
	defer r.wg.Done()
	periodic := time.NewTicker(r.interval)
	fast := time.NewTicker(fastCheckInterval)
	defer periodic.Stop()
	defer fast.Stop()

	// 启动后立即做一次周期报告（首推 prev=nil，报告全量）
	r.pushReportOnce()

	for {
		select {
		case <-r.done:
			return
		case <-periodic.C:
			r.pushReportOnce()
		case <-fast.C:
			r.fastCheck()
		}
	}
}

// pushReportOnce 周期报告：采集 → 差分 → 诊断 → 渲染 → 推送。
// 推送成功才推进 prev/lastCode/lastPushAt；失败保留现场下轮重试。
func (r *Reporter) pushReportOnce() {
	cur, err := r.gather()
	if err != nil {
		logGatherError("gather", err)
		return
	}
	delta := WindowDeltaMetric(r.prev, cur)
	window := r.windowSinceLastPush()
	diag := DiagnoseWithThresholds(delta, r.thresholds)
	text := mergeDiagnosisIntoReport(RenderReport(delta, window, r.instance), diag)

	if err := r.push(text); err != nil {
		log.Printf("[metrics-report] WARN: push failed, window kept for retry: %s", RedactWebhookError(err, r.webhook))
		r.lastFailAt = time.Now()
		return
	}
	// 推送成功才提交状态
	r.prev = cur
	r.lastCode = diag.Code
	r.lastPushAt = time.Now()
	// 周期推送成功说明 webhook 已恢复，清掉失败退避
	r.lastFailAt = time.Time{}
}

// fastCheck 快速巡检：诊断结论一变化就立刻补推，不等下个报告周期。
//
// 三重门防刷屏：
//  1. code 变了才算变化（code 不含计数，同级计数增长不算）；
//  2. 只在【进入异常】时即时推——恢复交给周期报告兜底，避免重复；
//  3. 距上次即时推送至少 fastPushDebounce。
//
// 关键：状态提交发生在【推送成功之后】——失败既不前移 prev 也不更新 lastCode。
func (r *Reporter) fastCheck() {
	cur, err := r.gather()
	if err != nil {
		logGatherError("fast check gather", err)
		return
	}
	delta := WindowDeltaMetric(r.prev, cur)
	diag := DiagnoseWithThresholds(delta, r.thresholds)
	if diag.Code == r.lastCode {
		return // 结论没变
	}
	if diag.Level == DiagnosisOK {
		// 恢复：不即时推，只更新 code，由周期报告兜底告知
		r.lastCode = diag.Code
		return
	}
	if time.Since(r.lastImmediatePushAt) < fastPushDebounce {
		return // 防抖中：不更新 code，下个巡检点重试
	}
	if !r.lastFailAt.IsZero() && time.Since(r.lastFailAt) < fastPushDebounce {
		return // 上次推送失败，退避期内不再打（避免风暴）
	}

	window := r.windowSinceLastPush()
	text := mergeDiagnosisIntoReport(RenderReport(delta, window, r.instance), diag)
	if err := r.push(text); err != nil {
		log.Printf("[metrics-report] WARN: immediate push failed, will retry: %s", RedactWebhookError(err, r.webhook))
		r.lastFailAt = time.Now()
		return // 现场保留：prev/lastCode 不动，下个巡检点按同一窗口重试
	}
	r.prev = cur
	r.lastCode = diag.Code
	now := time.Now()
	r.lastPushAt = now
	r.lastImmediatePushAt = now
	r.lastFailAt = time.Time{}
}

// gather 采集快照；测试可注入 gatherFn 替换真实采集。
// 走 Fast Registry（只含自定义指标，无 go_*），避免巡检触发 STW。
func (r *Reporter) gather() (*MetricSnapshot, error) {
	if r.gatherFn != nil {
		return r.gatherFn()
	}
	fams, err := Default().GatherFast()
	if err != nil {
		return nil, err
	}
	return CollectFromFamilies(fams), nil
}

// push 推送报告文本到 webhook，并统一打自监控埋点。
// 埋点集中在此，避免两处调用点漏打（自监控必须与真实推送严格同步）。
func (r *Reporter) push(text string) error {
	err := PushToWebhook(context.Background(), r.webhook, text)
	observeReporterPush(err)
	return err
}

// windowSinceLastPush 距上次推送的实际窗口时长；首推用名义间隔。
func (r *Reporter) windowSinceLastPush() time.Duration {
	if r.lastPushAt.IsZero() {
		return r.interval
	}
	return time.Since(r.lastPushAt)
}

// extractWebhookHost 从 URL 提取 host（判是否企微域名）。
// 用标准库 url.Parse 而非字符串切割：正确处理 userinfo/端口/编码/大小写。
// 非法 URL 返回 error，调用方据此拒绝启动（配置错误不该静默运行）。
func extractWebhookHost(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse webhook URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("webhook URL scheme 必须是 http/https, got %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("webhook URL 缺少 host")
	}
	// Hostname() 已剥离端口和 userinfo；DNS 大小写不敏感，统一转小写便于比较
	return strings.ToLower(u.Hostname()), nil
}

// instanceHostname 返回实例标识（hostname）。
func instanceHostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}
