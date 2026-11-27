package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/collectors"
)

// resetReporterForTest 测试隔离：StopReporter 清理全局实例，避免测试间相互污染。
// 必须在操作 reportInstance 的测试首尾调用。
func resetReporterForTest() {
	StopReporter()
}

// TestStartReporter_NoWebhook webhook 留空 no-op（不 panic，不启动）
func TestStartReporter_NoWebhook(t *testing.T) {
	resetReporterForTest()
	defer resetReporterForTest()

	StartReporter(60)
	reportMu.Lock()
	inst := reportInstance
	reportMu.Unlock()
	if inst != nil {
		t.Error("webhook 空不应启动")
	}
}

// TestStartReporter_Idempotent 幂等：重复启动只生效一次。
//
// 必须用本地 httptest webhook——之前用真实企微 URL，run() 启动后立即
// pushReportOnce() 会向 qyapi.weixin.qq.com 发真实请求（CI 断网即失败、
// 污染生产系统、留下 errcode 报错）。本地 server 完全隔离外网依赖。
func TestStartReporter_Idempotent(t *testing.T) {
	resetReporterForTest()
	defer resetReporterForTest()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	StartReporter(60, WithWebhookRoutes([]WebhookRoute{{Urls: []string{srv.URL}}}))
	reportMu.Lock()
	first := reportInstance
	reportMu.Unlock()
	if first == nil {
		t.Fatal("应启动")
	}
	StartReporter(60, WithWebhookRoutes([]WebhookRoute{{Urls: []string{srv.URL + "/different"}}}))
	reportMu.Lock()
	second := reportInstance
	reportMu.Unlock()
	if second != first {
		t.Error("重复启动应幂等（同一实例）")
	}
}

// TestReporter_WindowDelta 采集→差分流程
func TestReporter_WindowDelta(t *testing.T) {
	r := &Reporter{interval: time.Second}
	call := 0
	r.gatherFn = func() (*MetricSnapshot, error) {
		call++
		s := NewMetricSnapshot()
		// 每次采集累计 +100
		s.Durations["redis_operation_duration_seconds"] = &DurationAgg{
			Count: uint64(call * 100), Result: map[string]uint64{"ok": uint64(call * 100)},
		}
		return s, nil
	}
	// 首采
	cur1, _ := r.gatherFast()
	// 窗口差分（prev=nil → 全量）
	d1 := WindowDeltaMetric(nil, cur1)
	if d1.Durations["redis_operation_duration_seconds"].Count != 100 {
		t.Errorf("首窗应 100, got %d", d1.Durations["redis_operation_duration_seconds"].Count)
	}
	// 第二次采集，差分
	r.prev = cur1
	cur2, _ := r.gatherFast()
	d2 := WindowDeltaMetric(r.prev, cur2)
	if d2.Durations["redis_operation_duration_seconds"].Count != 100 {
		t.Errorf("窗口增量应 100, got %d", d2.Durations["redis_operation_duration_seconds"].Count)
	}
}

// TestReporter_PushFailureKeepsState 推送失败不推进 prev（现场保留）。
// 用本地 httptest 返回 errcode!=0 模拟企微拒绝——不依赖外网（CI 断网照样跑）。
func TestReporter_PushFailureKeepsState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook"}`))
	}))
	defer srv.Close()

	r := &Reporter{
		interval: time.Second,
		routes:   []WebhookRoute{{Urls: []string{srv.URL}}},
		done:     make(chan struct{}),
	}
	// 注入采集
	r.gatherFn = func() (*MetricSnapshot, error) {
		s := NewMetricSnapshot()
		s.Durations["x_duration_seconds"] = &DurationAgg{Count: 100}
		return s, nil
	}
	// 推送到返回 errcode!=0 的本地 server → 失败
	r.pushReportOnce()
	// prev 不应推进（仍 nil）
	if r.prev != nil {
		t.Error("推送失败 prev 不应推进")
	}
	if r.lastPushAt.IsZero() == false {
		t.Error("推送失败 lastPushAt 不应更新")
	}
}

// TestExtractWebhookHost host 提取
func TestExtractWebhookHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x", "qyapi.weixin.qq.com", false},
		{"https://evil.com/x", "evil.com", false},
		{"https://QYAPI.Weixin.QQ.COM:443/x", "qyapi.weixin.qq.com", false},       // 大写+端口归一
		{"https://user:pass@qyapi.weixin.qq.com/x", "qyapi.weixin.qq.com", false}, // userinfo
		{"no-scheme", "", true},
		{"ftp://qyapi.weixin.qq.com/x", "", true}, // 非法 scheme
		{"https:///no-host", "", true},            // 缺 host
	}
	for _, c := range cases {
		got, err := extractWebhookHost(c.in)
		if (err != nil) != c.err {
			t.Errorf("extractWebhookHost(%q) err=%v, want err=%v", c.in, err, c.err)
			continue
		}
		if got != c.want {
			t.Errorf("extractWebhookHost(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

// TestIsWeComWebhookHost_PortAndCase 端口/大写归一后仍识别为企微域名
func TestIsWeComWebhookHost_PortAndCase(t *testing.T) {
	host, err := extractWebhookHost("https://QYAPI.Weixin.QQ.COM:8443/x")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !IsWeComWebhookHost(host) {
		t.Errorf("归一后的 host 应识别为企微: %q", host)
	}
}

// errFake 复用（webhook_test 已定义 errFakeWebhook，此处独立）
var errGatherFake = errors.New("gather failed")

// TestLogGatherError_NoPanic 日志限流 helper 幂等安全（并发下不 panic）
func TestLogGatherError_NoPanic(t *testing.T) {
	// 重置状态隔离测试
	gatherLogMu.Lock()
	gatherLogLastAt = time.Time{}
	gatherLogSkipped = 0
	gatherLogMu.Unlock()
	for i := 0; i < 5; i++ {
		logGatherError("test", errors.New("fake"))
	}
	gatherLogMu.Lock()
	skipped := gatherLogSkipped
	gatherLogMu.Unlock()
	if skipped != 4 {
		t.Errorf("5 次调用应抑制 4 次, got %d", skipped)
	}
}

// TestReporterSelfMetrics 自监控指标必须进 Fast Registry 且埋点正确。
// 大厂标准：监控系统自身故障（webhook 持续失败）必须能被 Prometheus 告警，
// 否则运维分不清"群里没消息"是业务正常还是报告器挂了。
func TestReporterSelfMetrics(t *testing.T) {
	// 埋点：一次失败 + 一次成功
	observeReporterPush(errors.New("fake"))
	observeReporterPush(nil)

	fams, err := Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	got := map[string]bool{}
	for _, f := range fams {
		got[f.GetName()] = true
	}
	for _, n := range []string{"metrics_reporter_push_total", "metrics_reporter_last_push_timestamp_seconds"} {
		if !got[n] {
			t.Errorf("%s 应进 Fast Registry（自监控必须可巡检）", n)
		}
	}

	// 校验 push_total 的 result label 有 ok 与 error 两类样本
	for _, f := range fams {
		if f.GetName() != "metrics_reporter_push_total" {
			continue
		}
		results := map[string]bool{}
		for _, m := range f.Metric {
			for _, l := range m.Label {
				if l.GetName() == "result" {
					results[l.GetValue()] = true
				}
			}
		}
		if !results["ok"] || !results["error"] {
			t.Errorf("push_total 应有 ok/error 两类, got %v", results)
		}
	}
}

// TestReporter_Diagnose_Hook Hook 三态：生效、nil 回落、panic 保护。
func TestReporter_Diagnose_Hook(t *testing.T) {
	s := NewMetricSnapshot()

	// 1. hook 生效：返回定制诊断
	r := &Reporter{thresholds: DiagnosisThresholds{}, hook: func(*MetricSnapshot, DiagnosisThresholds) *Diagnosis {
		return &Diagnosis{Level: DiagnosisCritical, Code: "biz:502", Desc: "下游异常", Evidence: []string{"502=9"}}
	}}
	d := r.diagnose(s)
	if d.Code != "biz:502" || len(d.Evidence) != 1 {
		t.Errorf("hook 应生效, got %+v", d)
	}

	// 2. nil 回落：走框架通用诊断（无样本 → OK）
	r2 := &Reporter{thresholds: DiagnosisThresholds{}, hook: func(*MetricSnapshot, DiagnosisThresholds) *Diagnosis {
		return nil
	}}
	if d := r2.diagnose(s); d.Level != DiagnosisOK {
		t.Errorf("nil hook 应回落通用诊断, got %+v", d)
	}

	// 3. panic 保护：hook panic 不打挂报告链，回落通用诊断
	r3 := &Reporter{thresholds: DiagnosisThresholds{}, hook: func(*MetricSnapshot, DiagnosisThresholds) *Diagnosis {
		panic("hook bug")
	}}
	if d := r3.diagnose(s); d.Level != DiagnosisOK {
		t.Errorf("panic 应保护并回落, got %+v", d)
	}

	// 4. 无 hook：直接通用诊断
	r4 := &Reporter{thresholds: DiagnosisThresholds{}}
	if d := r4.diagnose(s); d.Level != DiagnosisOK {
		t.Errorf("无 hook 应走通用诊断, got %+v", d)
	}
}

// TestReporter_GatherPeriodic_HasRuntime 周期报告读 Default（含 go_* 运行时），
// 快速巡检读 Fast（无 go_*，避免 STW）——两节奏采集源必须分离，
// 否则周期报告丢 goroutine/内存（能力退化，对齐旧版报告器的守护）。
func TestReporter_GatherPeriodic_HasRuntime(t *testing.T) {
	// 注册 GoCollector 到 Default only（与框架 gRPC server 同款）
	// GoCollector 无状态、重复注册幂等（AlreadyRegistered 被忽略），无需清理
	MustRegisterDefaultOnly(collectors.NewGoCollector())

	r := &Reporter{}
	periodic, err := r.gatherPeriodic()
	if err != nil {
		t.Fatalf("gatherPeriodic: %v", err)
	}
	if _, ok := periodic.Gauges["go_goroutines"]; !ok {
		t.Errorf("周期报告应含 go_goroutines（运维依赖）, got gauges keys: %v", keysOf(periodic.Gauges))
	}

	fast, err := r.gatherFast()
	if err != nil {
		t.Fatalf("gatherFast: %v", err)
	}
	if _, ok := fast.Gauges["go_goroutines"]; ok {
		t.Errorf("快速巡检不应含 go_goroutines（避免 STW）")
	}
}

func keysOf(m map[string]map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestValidateThresholds_Reversed 部分覆盖导致 warn >= critical 时必须留 WARN。
// 场景：只设 P99Warn=10s，critical 回落默认 5s → 级别静默反转。
func TestValidateThresholds_Reversed(t *testing.T) {
	th := DiagnosisThresholds{P99Warn: 10}.normalize()
	if th.P99Warn <= th.P99Critical {
		t.Fatalf("前置条件：部分覆盖应造成反转, warn=%v critical=%v", th.P99Warn, th.P99Critical)
	}
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	validateThresholds(th)
	if !strings.Contains(buf.String(), "P99 阈值 warn") || !strings.Contains(buf.String(), "WARN") {
		t.Errorf("反转阈值应留 WARN, got %q", buf.String())
	}
	// 正常阈值不应告警
	buf.Reset()
	validateThresholds(DefaultDiagnosisThresholds())
	if buf.Len() != 0 {
		t.Errorf("正常阈值不应告警, got %q", buf.String())
	}
}

// TestStartReporter_IntervalClamp intervalSec 超大（误填毫秒等）必须钳到上限，
// 不能让 time.Duration 溢出导致 ticker 静默或兜底成 60s。
func TestStartReporter_IntervalClamp(t *testing.T) {
	// 先注册 srv.Close 再注册 resetReporter：LIFO 下先停 reporter 再关 server，
	// 避免 run() 的首推打到已关闭的 server 产生 WARN 噪声。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()
	resetReporterForTest()
	defer resetReporterForTest()

	// 超大值：fmt 中 int 上限（9223372036854775807）会导致 time.Duration*Second 溢出
	StartReporter(int(^uint(0)>>1), WithWebhookRoutes([]WebhookRoute{{Urls: []string{srv.URL}}}))
	reportMu.Lock()
	inst := reportInstance
	reportMu.Unlock()
	if inst == nil {
		t.Fatal("应启动")
	}
	if inst.interval != maxReportInterval {
		t.Errorf("超大 intervalSec 应钳到 %v, got %v", maxReportInterval, inst.interval)
	}
}

// TestValidateThresholds_Range 绝对值越界必须留 WARN：
// 错误率 >1 或 P99 非有限数会让告警永不触发（静默失效）。
func TestValidateThresholds_Range(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	validateThresholds(DiagnosisThresholds{ErrRateWarn: 2}.normalize())
	if !strings.Contains(buf.String(), "错误率阈值应 0~1") {
		t.Errorf("错误率 >1 应告警, got %q", buf.String())
	}
	buf.Reset()

	validateThresholds(DiagnosisThresholds{P99Warn: math.Inf(1)}.normalize())
	if !strings.Contains(buf.String(), "P99 阈值非有限数") {
		t.Errorf("P99=Inf 应告警, got %q", buf.String())
	}
	buf.Reset()

	validateThresholds(DiagnosisThresholds{P99Warn: 1}.normalize())
	if buf.Len() != 0 {
		t.Errorf("正常阈值不应告警, got %q", buf.String())
	}
}

// TestStopReporter_CancelsInflightPush Stop 应 cancel 在途 webhook 请求，
// 不必等满 5s webhookTimeout——用慢 server（sleep 10s）验证 Stop 快速返回。
func TestStopReporter_CancelsInflightPush(t *testing.T) {
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		// 模拟 webhook 挂起，但客户端断开（Stop cancel）时立即退出——
		// 否则 httptest 的 Close 会等 handler 挂起窗口（2s，窗口留够 Stop<3s 断言的区分度）
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resetReporterForTest()
	StartReporter(60, WithWebhookRoutes([]WebhookRoute{{Urls: []string{srv.URL}}}))

	// 等 push 到达 server（在途）
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("push 未到达 webhook server")
	}

	// Stop 应因 ctx cancel 快速返回，而非等满 10s sleep / 5s timeout
	done := make(chan struct{})
	go func() {
		StopReporter()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StopReporter 应取消在途 push 快速返回（>3s 说明未感知 ctx cancel）")
	}
	// 强制断开服务端连接，让 handler 立即退出——
	// 否则 httptest.Close 会等 handler 的 10s 挂起窗口（客户端 cancel 后
	// r.Context() 在 Windows 下可能未及时感知）
	srv.CloseClientConnections()
}

// TestWebhookRoute_Matches 路由匹配：types/levels 空=不限，顺序先命中先生效。
func TestWebhookRoute_Matches(t *testing.T) {
	tests := []struct {
		name    string
		route   WebhookRoute
		repType string
		level   DiagnosisLevel
		want    bool
	}{
		{"types 命中", WebhookRoute{Types: []string{"alert"}, Urls: []string{"u"}}, "alert", DiagnosisWarn, true},
		{"types 不命中", WebhookRoute{Types: []string{"periodic"}, Urls: []string{"u"}}, "alert", DiagnosisWarn, false},
		{"levels 命中", WebhookRoute{Levels: []string{"critical"}, Urls: []string{"u"}}, "alert", DiagnosisCritical, true},
		{"levels 不命中", WebhookRoute{Levels: []string{"critical"}, Urls: []string{"u"}}, "alert", DiagnosisWarn, false},
		{"空条件不限", WebhookRoute{Urls: []string{"u"}}, "periodic", DiagnosisOK, true},
		{"无 urls 不命中", WebhookRoute{Types: []string{"alert"}}, "alert", DiagnosisWarn, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.route.matches(tt.repType, tt.level); got != tt.want {
				t.Errorf("matches(%s, %s) = %v, want %v", tt.repType, tt.level, got, tt.want)
			}
		})
	}
}

// TestReporter_PushRoutes 分群推送：周期报告走体检群、告警走值班群+@人。
func TestReporter_PushRoutes(t *testing.T) {
	var periodicHits, alertHits int
	var alertMentions []string
	periodicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		periodicHits++
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer periodicSrv.Close()
	alertSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alertHits++
		var payload struct {
			Text struct {
				MentionedList []string `json:"mentioned_list"`
			} `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		alertMentions = payload.Text.MentionedList
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer alertSrv.Close()

	r := &Reporter{
		routes: []WebhookRoute{
			{Types: []string{"periodic"}, Urls: []string{periodicSrv.URL}},
			{Types: []string{"alert"}, Levels: []string{"warn", "critical"},
				Urls: []string{alertSrv.URL}, Mentions: []string{"zhangsan", "@all"}},
		},
	}

	if err := r.push("体检", "periodic", DiagnosisOK); err != nil {
		t.Fatalf("周期推送失败: %v", err)
	}
	if err := r.push("告警", "alert", DiagnosisCritical); err != nil {
		t.Fatalf("告警推送失败: %v", err)
	}
	if periodicHits != 1 || alertHits != 1 {
		t.Errorf("路由错误: periodic=%d alert=%d, want 1/1", periodicHits, alertHits)
	}
	if len(alertMentions) != 2 || alertMentions[0] != "zhangsan" || alertMentions[1] != "@all" {
		t.Errorf("告警 @ 人错误: %v", alertMentions)
	}
}

// TestReporter_PushNoMatch 无匹配路由时跳过推送（返回 nil，不报错）。
func TestReporter_PushNoMatch(t *testing.T) {
	r := &Reporter{routes: []WebhookRoute{{Types: []string{"alert"}, Urls: []string{"http://x"}}}}
	if err := r.push("体检", "periodic", DiagnosisOK); err != nil {
		t.Errorf("无匹配路由应跳过（nil），got %v", err)
	}
}

// TestPushToWebhookWithMentions_nil 无 @ 人时 payload 与旧版一致（无 mentioned_list 字段）。
func TestPushToWebhookWithMentions_nil(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	if err := PushToWebhookWithMentions(context.Background(), srv.URL, "hello", nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	if strings.Contains(string(body), "mentioned_list") {
		t.Errorf("无 @ 人时不应有 mentioned_list 字段: %s", body)
	}
}

// TestValidateThresholds_NegativeAndNaN 负值/NaN 必须留 WARN（防刷屏与静默失效）。
func TestValidateThresholds_NegativeAndNaN(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	validateRawThresholds(DiagnosisThresholds{ErrRateWarn: -0.1})
	if !strings.Contains(buf.String(), "非有限数或负数") {
		t.Errorf("负错误率阈值应告警, got %q", buf.String())
	}
	buf.Reset()

	validateRawThresholds(DiagnosisThresholds{P99Warn: math.NaN()})
	if !strings.Contains(buf.String(), "非有限数或负数") {
		t.Errorf("NaN P99 阈值应告警, got %q", buf.String())
	}
	buf.Reset()

	// 零值（未配置）合法，不应告警
	validateRawThresholds(DiagnosisThresholds{})
	if buf.Len() != 0 {
		t.Errorf("零值配置不应告警, got %q", buf.String())
	}
}

// TestIsPlaceholderWebhookKey 生产就绪守卫：占位 key / 缺 key 必须被识别，真实 key 不误判。
func TestIsPlaceholderWebhookKey(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=REPLACE_WITH_YOUR_WEBHOOK_KEY", true},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=YOUR_KEY", true},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send", true}, // 缺 key
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=693a91f6-7abc-4bd4-b7d1-1234567890ab", false},
		{"http://127.0.0.1:8080/hook?key=local-test-key", false},
	}
	for _, c := range cases {
		if got := isPlaceholderWebhookKey(c.url); got != c.want {
			t.Errorf("%q: got %v, want %v", c.url, got, c.want)
		}
	}
}

// TestExtractWebhookHost_NoKeyLeak URL 解析错误不能把原始 URL（含 key）带进日志。
func TestExtractWebhookHost_NoKeyLeak(t *testing.T) {
	const secret = "SUPER_SECRET_WEBHOOK_KEY_12345"
	badURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + secret + "\x00invalid"

	_, err := extractWebhookHost(badURL)
	if err == nil {
		t.Fatal("非法 URL 应返回错误")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "key=") {
		t.Errorf("错误信息不能含 webhook key（会泄露进日志）, got %q", err.Error())
	}
}
