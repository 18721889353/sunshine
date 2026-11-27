package metrics

import (
	"context"
	"fmt"
	"log"
	"math"
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
	// maxReportInterval 最大报告间隔：超过 1 天的周期报告没有运维意义，
	// 且 intervalSec*time.Second 在超大值时 int64 溢出会静默产生异常 ticker
	// （监控自己停摆却无感知）。钳到上限并留 WARN。
	maxReportInterval = 24 * time.Hour
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
	}, []string{labelResult})

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

// 报告类型枚举（yml webhookRoutes 的 types 值，配置与代码匹配的单一来源）。
const (
	ReportTypePeriodic = "periodic" // 周期体检报告
	ReportTypeAlert    = "alert"    // 异常即时告警
)

// 诊断级别键枚举（yml webhookRoutes 的 levels 值）。
const (
	LevelKeyOK       = "ok"
	LevelKeyWarn     = "warn"
	LevelKeyCritical = "critical"
)

// WebhookRoute 一条推送路由规则（对齐 upstreamRoutes 的"按顺序先命中先生效"语义）。
// types / levels 为空表示不限制该维度；urls 可多个（同时推送到多个群）。
type WebhookRoute struct {
	Types    []string // 报告类型：periodic（周期体检）/ alert（异常即时告警），空=不限
	Levels   []string // 诊断级别：ok / warn / critical，空=不限
	Urls     []string // 目标 webhook 地址（多个则全部推送）
	Mentions []string // @ 人列表（企微 userid 或 "@all"）
}

// matches 判断本规则是否命中当前推送场景。
func (r WebhookRoute) matches(reportType string, level DiagnosisLevel) bool {
	if len(r.Types) > 0 && !containsString(r.Types, reportType) {
		return false
	}
	if len(r.Levels) > 0 && !containsString(r.Levels, levelKey(level)) {
		return false
	}
	return len(r.Urls) > 0
}

// levelKey 把诊断级别转成稳定的配置键（ok/warn/critical）。
// 不用 level.String()——那是中文展示文案（"正常"/"警告"/"严重"），
// 配置枚举必须是稳定的英文，否则改文案就会破坏线上配置。
func levelKey(level DiagnosisLevel) string {
	switch level {
	case DiagnosisOK:
		return LevelKeyOK
	case DiagnosisWarn:
		return LevelKeyWarn
	case DiagnosisCritical:
		return LevelKeyCritical
	}
	return "unknown"
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Reporter 持有报告器生命周期状态。
// prev/lastCode/lastPushAt 等只在 push 成功后推进（失败保留现场）。
type Reporter struct {
	routes              []WebhookRoute // 推送路由规则（按顺序先命中先生效）
	interval            time.Duration
	prev                *MetricSnapshot                 // 上一窗口累计快照，仅 run goroutine 访问
	lastCode            string                          // 上次诊断 code，仅 run goroutine 访问
	lastPushAt          time.Time                       // 上次推送时间（算窗口标签）
	lastImmediatePushAt time.Time                       // 上次即时推送时间（防抖）
	lastFailAt          time.Time                       // 上次推送失败时间（失败退避）
	instance            string                          // 实例标识
	thresholds          DiagnosisThresholds             // 诊断阈值（零值 → 用默认）
	hook                DiagnosisHook                   // 业务定制诊断钩子（nil → 走框架通用诊断）
	gatherFn            func() (*MetricSnapshot, error) // 测试注入；nil 走 GatherFast
	done                chan struct{}
	// ctx/cancel：push 的生命周期上下文——Stop 时 cancel 可立即中断在途 webhook 请求，
	// 不必等满 webhookTimeout(5s)；context.Background() 不感知 done，无法取消
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
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

// validateThresholds 校验最终生效阈值的 warn < critical 关系。
func validateThresholds(th DiagnosisThresholds) {
	check := func(name string, warn, critical float64) {
		if warn >= critical {
			log.Printf("[metrics-report] WARN: %s 阈值 warn(%.3f) >= critical(%.3f)，告警级别可能失效，请检查 yml", name, warn, critical)
		}
	}
	check("错误率", th.ErrRateWarn, th.ErrRateCritical)
	check("P99", th.P99Warn, th.P99Critical)

	// 绝对值范围：错误率语义 0~1，配置 >1 等于永不告警（静默失效）；
	// P99 必须是有限正数（NaN/Inf 会让比较恒 false，告警永不触发）。
	if th.ErrRateWarn > 1 || th.ErrRateCritical > 1 {
		log.Printf("[metrics-report] WARN: 错误率阈值应 0~1，当前 warn=%.3f critical=%.3f（>1 将永不告警）",
			th.ErrRateWarn, th.ErrRateCritical)
	}
	if math.IsNaN(th.P99Warn) || math.IsInf(th.P99Warn, 0) ||
		math.IsNaN(th.P99Critical) || math.IsInf(th.P99Critical, 0) {
		log.Printf("[metrics-report] WARN: P99 阈值非有限数（warn=%v critical=%v），告警将永不触发",
			th.P99Warn, th.P99Critical)
	}
}

// ReporterOption 报告器可选项（大厂标准：默认 + 可覆盖）。
type ReporterOption func(*Reporter)

// WithDiagnosisThresholds 覆盖诊断阈值（不同业务容忍度不同：
// 支付 P99 1s 即严重，静态资源 5s 仍正常）。未覆盖的字段回落默认值。
func WithDiagnosisThresholds(th DiagnosisThresholds) ReporterOption {
	return func(r *Reporter) {
		r.thresholds = th
	}
}

// WithDiagnosisHook 注入业务定制诊断钩子。
// 返回 nil 表示不接管（继续框架通用诊断）；hook panic 会被保护并回落通用诊断，
// 业务诊断 bug 不应打挂整个报告链。
func WithDiagnosisHook(h DiagnosisHook) ReporterOption {
	return func(r *Reporter) {
		r.hook = h
	}
}

// WithWebhookRoutes 配置推送路由规则（按顺序先命中先生效）。
// 由 yml 的 webhookRoutes 经 internal/config 转换传入。
func WithWebhookRoutes(routes []WebhookRoute) ReporterOption {
	return func(r *Reporter) {
		r.routes = routes
	}
}

// diagnose 先跑业务 hook，nil 或 panic 则回落框架通用诊断。
func (r *Reporter) diagnose(s *MetricSnapshot) (d Diagnosis) {
	if r.hook != nil {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[metrics-report] WARN: diagnosis hook panic, fallback to default: %v", rec)
				d = DiagnoseWithThresholds(s, r.thresholds)
			}
		}()
		if custom := r.hook(s, r.thresholds); custom != nil {
			return *custom
		}
	}
	return DiagnoseWithThresholds(s, r.thresholds)
}

// ReporterSettings 报告器启动设置（业务配置 → 框架的映射）。
// 业务侧（如网关）无法 import 框架 internal/config（Go internal 限制），
// 用本结构体传配置，避免业务复制"总开关 + webhook 校验 + 阈值回落"逻辑。
type ReporterSettings struct {
	Enabled     bool                // 指标体系总开关（false → 不启动，与采集端装配语义一致）
	Routes      []WebhookRoute      // 推送路由规则（按顺序先命中先生效）
	IntervalSec int                 // 周期报告间隔（<=0 默认 60s，<10s 钳到 10s）
	Thresholds  DiagnosisThresholds // 诊断阈值（零值字段自动回落框架默认，见 normalize）
}

// StartWithSettings 按设置启动报告器（幂等）。
// 与框架 internal/config.StartMetricsReporter 同语义——后者供框架模板用，
// 本函数供无法 import internal 的业务模块用；业务 opts 追加在后（可覆盖设置）。
func StartWithSettings(st ReporterSettings, opts ...ReporterOption) {
	if !st.Enabled {
		log.Println("[metrics-report] WARN: 指标体系总开关关闭（enabled=false），报告器不启动")
		return
	}
	all := []ReporterOption{WithWebhookRoutes(st.Routes), WithDiagnosisThresholds(st.Thresholds)}
	all = append(all, opts...)
	StartReporter(st.IntervalSec, all...)
}

// StartReporter 按配置启动指标报告器（幂等，重复调用只生效一次）。
// 推送目标由 WithWebhookRoutes 注入（路由规则数组）；无有效路由则 no-op 并留 WARN。
// interval<=0 取默认 60s，低于 10s 钳到 10s（企微限频）。
func StartReporter(intervalSec int, opts ...ReporterOption) {
	// 先钳制秒数再乘 time.Second：超大 intervalSec 直接相乘会 int64 溢出，
	// 溢出成负数会被 <=0 分支兜底成 60s（语义错误），溢出成正数则 ticker 异常。
	maxSec := int(maxReportInterval / time.Second)
	if intervalSec > maxSec {
		log.Printf("[metrics-report] WARN: intervalSec=%d 超过上限（%d 秒），钳制为 %d 秒——请检查配置单位（是否误填毫秒）",
			intervalSec, maxSec, maxSec)
		intervalSec = maxSec
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
	pushCtx, pushCancel := context.WithCancel(context.Background())
	r := &Reporter{
		interval: interval,
		instance: instanceHostname(),
		done:     make(chan struct{}),
		ctx:      pushCtx,
		cancel:   pushCancel,
	}
	for _, opt := range opts {
		opt(r)
	}
	// 路由校验：非法 URL 的路由直接丢弃（配置错误不该静默运行，更不能推到外部地址）。
	// 校验在 opt 应用之后——routes 由 WithWebhookRoutes 注入，此时才有值。
	routes := r.routes[:0]
	for _, route := range r.routes {
		validUrls := route.Urls[:0]
		for _, u := range route.Urls {
			u = strings.TrimSpace(u)
			host, err := extractWebhookHost(u)
			if err != nil {
				log.Printf("[metrics-report] ERROR: 路由 webhook URL 非法，跳过: %v", err)
				continue
			}
			if !IsWeComWebhookHost(host) {
				// 非企微域名仍可用（私有化部署/测试 server），但与主 webhook 同标准留 WARN
				log.Printf("[metrics-report] WARN: 路由 webhook 非企微地址 host=%s（私有化/测试环境可忽略）", host)
			}
			validUrls = append(validUrls, u)
		}
		if len(validUrls) > 0 {
			route.Urls = validUrls
			routes = append(routes, route)
		}
	}
	r.routes = routes
	if len(r.routes) == 0 {
		log.Println("[metrics-report] WARN: 无有效 webhook 路由，指标报告未启动（群机器人不会收到任何消息）")
		return
	}
	// Gauge 规则 kind 校验：配置错误必须在启动时暴露（未知 kind 的规则会永久失效）。
	for name, spec := range componentSpecs {
		if spec.gaugeRule == nil {
			continue
		}
		switch spec.gaugeRule.kind {
		case gaugeRuleEqZero, gaugeRuleAbove:
		default:
			log.Printf("[metrics-report] ERROR: 指标 %s 的 gaugeRule.kind=%q 未知（仅支持 eqZero/above），该规则永久失效",
				name, spec.gaugeRule.kind)
		}
	}
	// 路由类型覆盖检查：fastCheck 只在"进入异常"时即时推送，若无 alert 类型路由，
	// 异常告警会被静默跳过且 lastCode 仍会更新（下次不再提醒）——运维完全无感知。
	// 启动时显式 WARN，把配置缺失暴露出来。
	var hasPeriodic, hasAlert bool
	for _, route := range r.routes {
		if len(route.Types) == 0 {
			// 空 types = 不限类型，视为同时覆盖
			hasPeriodic, hasAlert = true, true
			break
		}
		if containsString(route.Types, ReportTypePeriodic) {
			hasPeriodic = true
		}
		if containsString(route.Types, ReportTypeAlert) {
			hasAlert = true
		}
	}
	if !hasPeriodic {
		log.Println("[metrics-report] WARN: webhook 路由未覆盖 periodic 类型，周期体检报告不会推送")
	}
	if !hasAlert {
		log.Println("[metrics-report] WARN: webhook 路由未覆盖 alert 类型，异常即时告警不会推送（静默）")
	}
	// 配置校验（大厂标准：启动时快速发现错误配置，不能静默运行）。
	// 校验 normalize 后的【最终生效值】——只覆盖 warn 而没覆盖 critical 时，
	// critical 回落默认值可能与 warn 反转（如 P99Warn=10s、critical 默认 5s），
	// 告警级别将永远失效。只留 WARN 不阻塞启动：报告器是旁路监控，
	// 不该因阈值配错拖垮主服务。
	validateThresholds(r.thresholds.normalize())
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
	// 先 cancel：中断在途 webhook 请求（不必等满 5s webhookTimeout），
	// 再 close(done) 让 run 主循环退出
	r.cancel()
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
	cur, err := r.gatherPeriodic()
	if err != nil {
		logGatherError("gather", err)
		return
	}
	delta := WindowDeltaMetric(r.prev, cur)
	window := r.windowSinceLastPush()
	diag := r.diagnose(delta)
	text := mergeDiagnosisIntoReport(RenderReport(delta, window, r.instance), diag)

	if err := r.push(text, ReportTypePeriodic, diag.Level); err != nil {
		log.Printf("[metrics-report] WARN: push failed, window kept for retry: %s", err)
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
	cur, err := r.gatherFast()
	if err != nil {
		logGatherError("fast check gather", err)
		return
	}
	delta := WindowDeltaMetric(r.prev, cur)
	diag := r.diagnose(delta)
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
	if err := r.push(text, ReportTypeAlert, diag.Level); err != nil {
		log.Printf("[metrics-report] WARN: immediate push failed, will retry: %s", err)
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

// gatherFast 快速巡检（5s）：只读 Fast Registry（自定义指标，无 go_*），
// 避免 Go collector 的 ReadMemStats 触发 runtime STW 停顿。
// 测试可注入 gatherFn 替换真实采集。
func (r *Reporter) gatherFast() (*MetricSnapshot, error) {
	if r.gatherFn != nil {
		return r.gatherFn()
	}
	fams, err := Default().GatherFast()
	if err != nil {
		return nil, err
	}
	return CollectFromFamilies(fams), nil
}

// gatherPeriodic 周期报告：读 Default Registry（含 go_goroutines/go_memstats
// 运行时指标，Fast 不含）——60s 一次的 ReadMemStats STW 可接受，且运维报告
// 依赖 goroutine/内存（能力对齐旧版自建报告器，否则报告丢运行时数据）。
func (r *Reporter) gatherPeriodic() (*MetricSnapshot, error) {
	if r.gatherFn != nil {
		return r.gatherFn()
	}
	fams, err := Default().GatherDefault()
	if err != nil {
		return nil, err
	}
	return CollectFromFamilies(fams), nil
}

// push 推送报告文本到 webhook，并统一打自监控埋点。
// 埋点集中在此，避免两处调用点漏打（自监控必须与真实推送严格同步）。
// push 按路由规则推送报告：按顺序先命中先生效，命中规则的 urls 全部推送。
// reportType 为 ReportTypePeriodic/ReportTypeAlert，level 为本次诊断级别。
// 推送失败返回聚合错误（error 已经 webhook 脱敏，见 PushToWebhookWithMentions）。
func (r *Reporter) push(text, reportType string, level DiagnosisLevel) error {
	// ctx 可能为 nil（测试直接构造 &Reporter{}），降级 Background 防 WithTimeout panic
	pushCtx := r.ctx
	if pushCtx == nil {
		pushCtx = context.Background()
	}
	route, ok := r.matchRoute(reportType, level)
	if !ok {
		log.Printf("[metrics-report] WARN: 无匹配路由 type=%s level=%s，跳过推送", reportType, level)
		return nil
	}
	var errs []string
	for _, webhookURL := range route.Urls {
		err := PushToWebhookWithMentions(pushCtx, webhookURL, text, route.Mentions)
		// 自监控：必须与真实推送严格同步（报告器是"监控的监控"，自身故障必须可见）
		observeReporterPush(err)
		if err != nil {
			// 错误统一脱敏（URL 含 key 不能进日志）
			errs = append(errs, RedactWebhookError(err, webhookURL))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("推送失败 %d/%d: %s", len(errs), len(route.Urls), strings.Join(errs, "; "))
	}
	return nil
}

// matchRoute 按顺序返回第一条命中的路由。
func (r *Reporter) matchRoute(reportType string, level DiagnosisLevel) (WebhookRoute, bool) {
	for _, route := range r.routes {
		if route.matches(reportType, level) {
			return route, true
		}
	}
	return WebhookRoute{}, false
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
