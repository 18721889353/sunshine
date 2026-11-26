package metrics

import (
	"strings"
	"testing"
	"time"
)

// TestRenderReport 组件分组渲染
func TestRenderReport(t *testing.T) {
	s := NewMetricSnapshot()
	// HTTP 接入
	s.Durations["http_request_duration_seconds"] = &DurationAgg{
		Count: 600, SumSec: 30,
		Result: map[string]uint64{"ok": 590, "error": 10},
		Bounds: []float64{0.05, 0.1, 0.5, 1, 5},
		Counts: []uint64{100, 300, 500, 590, 600},
	}
	// Redis
	s.Durations["redis_operation_duration_seconds"] = &DurationAgg{
		Count: 6000, SumSec: 6,
		Result: map[string]uint64{"ok": 5999, "error": 1},
	}
	// Cache
	s.Durations["cache_operation_duration_seconds"] = &DurationAgg{
		Count: 4000, SumSec: 4, Result: map[string]uint64{"ok": 4000},
	}
	s.Counters["cache_hit_total"] = map[string]float64{"op=Get": 3800}
	s.Counters["cache_miss_total"] = map[string]float64{"op=Get": 200}

	text := RenderReport(s, 600*time.Second, "test-host")

	if !strings.Contains(text, " sunshine 指标报告") {
		t.Error("应含报告标题")
	}
	if !strings.Contains(text, "【HTTP】") {
		t.Error("应含 HTTP 分组")
	}
	if !strings.Contains(text, "【数据层】") {
		t.Error("应含 数据层 分组")
	}
	if !strings.Contains(text, "HTTP接入") {
		t.Error("应含 HTTP接入 行")
	}
	if !strings.Contains(text, "Redis") {
		t.Error("应含 Redis 行")
	}
	if !strings.Contains(text, "P99") {
		t.Error("应含 P99")
	}
	// 错误率检查：HTTP 10/600 = 1.67%
	if !strings.Contains(text, "1.67%") {
		t.Errorf("HTTP 错误率应 1.67%%, text=%s", text)
	}
	t.Logf("\n%s", text)
}

// TestRenderReport_Empty 空快照不 panic
func TestRenderReport_Empty(t *testing.T) {
	text := RenderReport(NewMetricSnapshot(), 600*time.Second, "h")
	if text == "" {
		t.Error("空快照也应有标题")
	}
	if RenderReport(nil, 0, "") == "" {
		t.Error("nil 快照应有提示")
	}
}

// TestMetricBase 后缀剥离
func TestMetricBase(t *testing.T) {
	cases := map[string]string{
		"redis_operation_duration_seconds": "redis_operation",
		"cache_hit_total":                  "cache_hit",
		"etcd_token_refresh_total":         "etcd_token_refresh",
		"uptime_total":                     "uptime",
	}
	for in, want := range cases {
		if got := metricBase(in); got != want {
			t.Errorf("metricBase(%q)=%q, want %q", in, got, want)
		}
	}
}

// TestFormatBytes 单位渲染
func TestFormatBytes(t *testing.T) {
	if FormatBytes(512) != "512B" {
		t.Errorf("512B, got %s", FormatBytes(512))
	}
	if FormatBytes(1024*1024) != "1.0MB" {
		t.Errorf("1.0MB, got %s", FormatBytes(1024*1024))
	}
}

// TestRenderReport_UnrecognizedCounterGauge 新增的 Counter/Gauge 组件应兜底展示
// （否则框架新增指标静默丢失）
func TestRenderReport_UnrecognizedCounterGauge(t *testing.T) {
	s := NewMetricSnapshot()
	// 未注册的 Counter / Gauge
	s.Counters["brand_new_component_total"] = map[string]float64{"result=ok": 100}
	s.Gauges["brand_new_gauge"] = map[string]float64{"": 42}
	text := RenderReport(s, 600*time.Second, "h")
	if !strings.Contains(text, "【其他】") {
		t.Error("应有【其他】分组")
	}
	if !strings.Contains(text, "brand_new_component") {
		t.Error("新增 Counter 应兜底展示")
	}
	if !strings.Contains(text, "brand_new_gauge") {
		t.Error("新增 Gauge 应兜底展示")
	}
}

// TestMetricBase_GRPCSuffix 守护 _seconds 后缀裁剪：
// grpc 指标是 _handling_seconds（非 _duration_seconds），不裁剪会导致
// base 不变、无法命中 componentSpecs → 走兜底丢中文标签/分组。
func TestMetricBase_GRPCSuffix(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		// 标准耗时后缀（先裁 _duration_seconds，不会二次裁剪）
		{"http_request_duration_seconds", "http_request"},
		{"redis_operation_duration_seconds", "redis_operation"},
		// gRPC 非标准耗时后缀（_handling_seconds）
		{"grpc_server_handling_seconds", "grpc_server_handling"},
		{"grpc_client_handling_seconds", "grpc_client_handling"},
		{"grpc_client_msg_recv_handling_seconds", "grpc_client_msg_recv_handling"},
		// counter 后缀
		{"grpc_server_handled_total", "grpc_server_handled"},
		// gauge 无后缀
		{"grpc_server_active_connections", "grpc_server_active_connections"},
		// 自监控（timestamp_seconds 不是耗时，但裁剪后仍能走兜底展示）
		{"metrics_reporter_last_push_timestamp_seconds", "metrics_reporter_last_push_timestamp"},
	} {
		if got := metricBase(c.in); got != c.want {
			t.Errorf("metricBase(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// 关键：grpc 核心指标必须命中 componentSpecs（否则报告里无中文标签/分组）
	for _, name := range []string{
		"grpc_server_handling_seconds",
		"grpc_server_handled_total",
		"grpc_client_handled_total",
	} {
		if _, ok := componentSpecs[metricBase(name)]; !ok {
			t.Errorf("%s 的 base 应命中 componentSpecs, base=%q", name, metricBase(name))
		}
	}
}

// TestRenderSummaryLine 字节 Summary 渲染口径：走 FormatBytes，不套耗时 ms/错误率。
func TestRenderSummaryLine(t *testing.T) {
	agg := &DurationAgg{Count: 100, SumSec: 51200} // 平均 512B
	line := renderSummaryLine("http_request_size_bytes", agg, 60)

	if !strings.Contains(line, "512B") {
		t.Errorf("应按字节渲染 512B, got %q", line)
	}
	if strings.Contains(line, "ms") {
		t.Errorf("字节指标不应显示毫秒, got %q", line)
	}
	if strings.Contains(line, "错误") {
		t.Errorf("字节指标不应显示错误率, got %q", line)
	}

	// 无样本走"无样本"分支
	if got := renderSummaryLine("x_bytes", &DurationAgg{}, 60); !strings.Contains(got, "无样本") {
		t.Errorf("无样本应显示'无样本', got %q", got)
	}
}

// TestRenderCounterLine_MultiLabel 多 label counter 必须显示维度分布
// （status=502 等关键信号不能被聚合成单值——对齐旧版报告"状态码: 502=17"）。
func TestRenderCounterLine_MultiLabel(t *testing.T) {
	cm := map[string]float64{
		"entry=api,status=200":    100,
		"entry=api,status=502":    17,
		"entry=notify,status=502": 3,
	}
	line := renderCounterLine("responses", cm, 60)
	if !strings.Contains(line, "120 次") {
		t.Errorf("总量应 120, got %q", line)
	}
	// status 维度分布（末位维度，值降序 top3）：502 累计 20、200=100
	if !strings.Contains(line, "502=20") || !strings.Contains(line, "200=100") {
		t.Errorf("应含 status 分布 502=20/200=100, got %q", line)
	}
	// 纯 result 维度不重复输出（已并入错误率）
	line = renderCounterLine("x", map[string]float64{"result=ok": 5, "result=error": 2}, 60)
	if strings.Contains(line, "ok=5") {
		t.Errorf("result 维度不应进分布（已并入错误率）, got %q", line)
	}
}

// TestRenderGaugeLine_MultiLabel 多 label gauge 必须逐 key 渲染
// （副本健康态 4 个值不能只显示第一个——信息丢失）。
func TestRenderGaugeLine_MultiLabel(t *testing.T) {
	gm := map[string]float64{
		"replica=a:80,route=order": 1,
		"replica=b:80,route=order": 1,
		"replica=c:80,route=order": 0,
		"replica=d:80,route=order": 1,
	}
	line := renderGaugeLine("upstream_healthy", gm)
	for _, want := range []string{"a:80=1", "b:80=1", "c:80=0", "d:80=1"} {
		if !strings.Contains(line, want) {
			t.Errorf("应含 %q（4 副本全显示）, got %q", want, line)
		}
	}
	// 单值 gauge 行为不变
	if got := renderGaugeLine("goroutine", map[string]float64{"": 61}); !strings.Contains(got, "61") {
		t.Errorf("单值 gauge 应显示 61, got %q", got)
	}
}

// TestRenderCounterLine_UptimeUnit uptime 是"分钟"不是"次"——unit 字段生效，
// 且 spec key 必须是 metricBase 裁剪后的 "uptime"（不是 uptime_total，否则永不命中）。
func TestRenderCounterLine_UptimeUnit(t *testing.T) {
	line := renderCounterLine("运行时长", map[string]float64{"": 8}, 509, "分钟")
	if !strings.Contains(line, "8 分钟") {
		t.Errorf("unit 应生效: %q", line)
	}
	// spec key 裁剪检查
	if _, ok := componentSpecs[metricBase("uptime_total")]; !ok {
		t.Errorf("uptime_total 的 base=%q 应命中 componentSpecs", metricBase("uptime_total"))
	}
	// 零值也应带 unit（uptime 为 0 时显示"0 分钟"而非"0 次"）
	if got := renderCounterLine("运行时长", map[string]float64{"": 0}, 509, "分钟"); !strings.Contains(got, "0 分钟") {
		t.Errorf("零值应带 unit: %q", got)
	}
	// 默认仍是"次"
	if got := renderCounterLine("调用", map[string]float64{"": 5}, 60); !strings.Contains(got, "5 次") {
		t.Errorf("默认单位应为次: %q", got)
	}
}

// TestRenderBreakdown 分链下钻：聚合 P99 会稀释慢链，label 维度分解必须可见。
func TestRenderBreakdown(t *testing.T) {
	byLabel := map[string]*DurationAgg{
		"entry=api":    {Count: 100, SumSec: 5, Bounds: []float64{0.1, 1}, Counts: []uint64{95, 100}},
		"entry=notify": {Count: 50, SumSec: 40, Bounds: []float64{0.1, 1}, Counts: []uint64{10, 50}},
	}
	got := renderBreakdown(byLabel)
	if !strings.Contains(got, "分链") || !strings.Contains(got, "notify=") || !strings.Contains(got, "api=") {
		t.Errorf("应含分链两链, got %q", got)
	}
	// notify 更慢应排前
	if strings.Index(got, "notify=") > strings.Index(got, "api=") {
		t.Errorf("P99 高的链应排前, got %q", got)
	}

	// 单链无信息量：不显示
	if got := renderBreakdown(map[string]*DurationAgg{"entry=api": {Count: 1, Bounds: []float64{1}, Counts: []uint64{1}}}); got != "" {
		t.Errorf("单链不应显示分链, got %q", got)
	}
	// 超过 6 链太细：不显示
	many := map[string]*DurationAgg{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		many["entry="+k] = &DurationAgg{Count: 1, Bounds: []float64{1}, Counts: []uint64{1}}
	}
	if got := renderBreakdown(many); got != "" {
		t.Errorf(">6 链不应显示, got %q", got)
	}

	// 空/无样本
	if got := renderBreakdown(nil); got != "" {
		t.Errorf("nil 应返回空, got %q", got)
	}
}

// TestRenderDurationLine_Breakdown 集成：duration 行尾追加分链。
func TestRenderDurationLine_Breakdown(t *testing.T) {
	agg := &DurationAgg{Count: 150, SumSec: 45, Bounds: []float64{1}, Counts: []uint64{150},
		Result: map[string]uint64{"ok": 140, "error": 10}}
	breakdown := map[string]*DurationAgg{
		"entry=api":    {Count: 100, SumSec: 5, Bounds: []float64{1}, Counts: []uint64{100}},
		"entry=notify": {Count: 50, SumSec: 40, Bounds: []float64{1}, Counts: []uint64{50}},
	}
	line := renderDurationLine("网关端到端", agg, 60, breakdown)
	if !strings.Contains(line, "分链") {
		t.Errorf("duration 行应含分链, got %q", line)
	}
	// 不传 breakdown 行为不变
	line2 := renderDurationLine("网关端到端", agg, 60, nil)
	if strings.Contains(line2, "分链") {
		t.Errorf("无 breakdown 不应有分链, got %q", line2)
	}
}

// TestFormatMs_分级 秒级耗时应显示 s 而非数千 ms（7.3s 比 7300.0ms 直观）。
func TestFormatMs_分级(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{0.0005, "500μs"},  // 亚毫秒
		{0.08, "80.0ms"},   // 毫秒级
		{7.3, "7.30s"},     // 秒级
		{0.999, "999.0ms"}, // 1s 以下仍是 ms
		{1.0, "1.00s"},     // 恰 1s
	}
	for _, c := range cases {
		if got := FormatMs(c.sec); got != c.want {
			t.Errorf("FormatMs(%v) = %q, want %q", c.sec, got, c.want)
		}
	}
}

// TestRenderBreakdown_MergesResultDim 同业务链的 result=ok/error 必须合并成一条，
// 否则每条链被拆成多条（且 >6 链时整块不显示，分链能力失效）。
func TestRenderBreakdown_MergesResultDim(t *testing.T) {
	byLabel := map[string]*DurationAgg{
		"entry=api,result=ok":       {Count: 90, SumSec: 4, Bounds: []float64{0.1, 1}, Counts: []uint64{85, 90}},
		"entry=api,result=error":    {Count: 10, SumSec: 1, Bounds: []float64{0.1, 1}, Counts: []uint64{5, 10}},
		"entry=notify,result=ok":    {Count: 40, SumSec: 30, Bounds: []float64{0.1, 1}, Counts: []uint64{5, 40}},
		"entry=notify,result=error": {Count: 10, SumSec: 10, Bounds: []float64{0.1, 1}, Counts: []uint64{0, 10}},
	}
	got := renderBreakdown(byLabel)
	if !strings.Contains(got, "分链") {
		t.Fatalf("应显示分链, got %q", got)
	}
	// 合并后 2 条链：api/notify，不应出现重复键
	if strings.Count(got, "api=") != 1 || strings.Count(got, "notify=") != 1 {
		t.Errorf("同链应合并为一条, got %q", got)
	}
	// notify 慢链排前
	if strings.Index(got, "notify=") > strings.Index(got, "api=") {
		t.Errorf("慢链应排前, got %q", got)
	}
}

// TestFormatDuration_UnifiedWithMs 诊断文案与报告行必须用同一刻度
// （原 FormatDuration 亚毫秒显示 "0.0ms"，与 FormatMs 的 μs 分叉）。
func TestFormatDuration_UnifiedWithMs(t *testing.T) {
	for _, sec := range []float64{0.0005, 0.08, 1.0, 7.3} {
		if got, want := FormatDuration(sec), FormatMs(sec); got != want {
			t.Errorf("FormatDuration(%v)=%q 应与 FormatMs=%q 一致", sec, got, want)
		}
	}
	if got := FormatDuration(0.0005); strings.Contains(got, "0.0ms") {
		t.Errorf("亚毫秒不应丢精度为 0.0ms, got %q", got)
	}
}

// TestFormatBytes_Extremes 超大值不能越界 panic、负值防御为 0B。
func TestFormatBytes_Extremes(t *testing.T) {
	if got := FormatBytes(-5); got != "0B" {
		t.Errorf("负值应防御为 0B, got %q", got)
	}
	// 1<<100 远超后缀表长度（EB 之上），必须安全返回而非 panic
	got := FormatBytes(1 << 100)
	if got == "" || !strings.HasSuffix(got, "B") {
		t.Errorf("超大值应安全渲染, got %q", got)
	}
	// 常规值不受影响
	if got := FormatBytes(1024 * 1024); got != "1.0MB" {
		t.Errorf("1.0MB, got %q", got)
	}
}
