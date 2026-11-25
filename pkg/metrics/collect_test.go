package metrics

import (
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

func strp(s string) *string               { return &s }
func f64p(f float64) *float64             { return &f }
func u64p(u uint64) *uint64               { return &u }
func mt(t dto.MetricType) *dto.MetricType { return &t }

// TestCollectFromFamilies_EndToEnd 端到端：真实 dto → 快照 → 差分 → 诊断 → 渲染
// 覆盖跨函数集成（此前 collect 无真实 dto 测试，是盲区）。
func TestCollectFromFamilies_EndToEnd(t *testing.T) {
	// 一个 Histogram 家族：redis_operation，带 result label，2 个 label 组合
	redisFam := &dto.MetricFamily{
		Name: strp("redis_operation_duration_seconds"),
		Type: mt(dto.MetricType_HISTOGRAM),
		Metric: []*dto.Metric{
			{
				Label: []*dto.LabelPair{{Name: strp("result"), Value: strp("ok")}},
				Histogram: &dto.Histogram{
					SampleCount: u64p(95),
					SampleSum:   f64p(1.9),
					Bucket: []*dto.Bucket{
						{UpperBound: f64p(0.005), CumulativeCount: u64p(50)},
						{UpperBound: f64p(0.01), CumulativeCount: u64p(80)},
						{UpperBound: f64p(1e300), CumulativeCount: u64p(95)},
					},
				},
			},
			{
				Label: []*dto.LabelPair{{Name: strp("result"), Value: strp("error")}},
				Histogram: &dto.Histogram{
					SampleCount: u64p(5),
					SampleSum:   f64p(0.5),
					Bucket: []*dto.Bucket{
						{UpperBound: f64p(0.005), CumulativeCount: u64p(1)},
						{UpperBound: f64p(0.01), CumulativeCount: u64p(3)},
						{UpperBound: f64p(1e300), CumulativeCount: u64p(5)},
					},
				},
			},
		},
	}
	// 一个 Counter 家族
	etcdFam := &dto.MetricFamily{
		Name: strp("etcd_token_refresh_total"),
		Type: mt(dto.MetricType_COUNTER),
		Metric: []*dto.Metric{
			{
				Label:   []*dto.LabelPair{{Name: strp("result"), Value: strp("error")}},
				Counter: &dto.Counter{Value: f64p(2)},
			},
		},
	}
	// 一个 Gauge 家族
	gaugeFam := &dto.MetricFamily{
		Name: strp("logger_router_active_loggers"),
		Type: mt(dto.MetricType_GAUGE),
		Metric: []*dto.Metric{
			{Gauge: &dto.Gauge{Value: f64p(7)}},
		},
	}

	s := CollectFromFamilies([]*dto.MetricFamily{redisFam, etcdFam, gaugeFam})

	// 校验 Histogram 聚合
	agg := s.Durations["redis_operation_duration_seconds"]
	if agg == nil {
		t.Fatal("应有 redis histogram 聚合")
	}
	if agg.Count != 100 {
		t.Errorf("Count 应 100(95+5), got %d", agg.Count)
	}
	if agg.Result["error"] != 5 || agg.Result["ok"] != 95 {
		t.Errorf("result 拆分错误: %v", agg.Result)
	}
	// bucket 累加：3 桶，ok+error 各累计
	if len(agg.Bounds) != 3 || agg.Counts[2] != 100 {
		t.Errorf("bucket 应 3 桶且末桶=100, bounds=%v counts=%v", agg.Bounds, agg.Counts)
	}

	// 校验 Counter
	if s.Counters["etcd_token_refresh_total"]["result=error"] != 2 {
		t.Errorf("counter error 应 2, got %v", s.Counters["etcd_token_refresh_total"])
	}

	// 校验 Gauge
	if s.Gauges["logger_router_active_loggers"][""] != 7 {
		t.Errorf("gauge 应 7, got %v", s.Gauges["logger_router_active_loggers"])
	}

	// 端到端：差分（首窗）→ 诊断 → 渲染
	d := WindowDeltaMetric(nil, s)
	if _, ok := d.Durations["redis_operation_duration_seconds"].P99Sec(); !ok {
		t.Error("首窗 P99 应可算（Clone 拷桶回归）")
	}
	diag := Diagnose(d)
	// 错误率 5% → warn
	if diag.Level != DiagnosisWarn {
		t.Errorf("错误率 5%% 应 warn, got %v", diag.Level)
	}
	// counter error 也应告警
	if diag.Code != "redis_operation:err-rate>5%" {
		t.Errorf("应取先出现的 duration warn, got %q", diag.Code)
	}

	text := RenderReport(d, 600000000000, "host")
	if text == "" {
		t.Error("渲染不应为空")
	}
}

// TestCollectFromFamilies_NilSafe nil 安全
func TestCollectFromFamilies_NilSafe(t *testing.T) {
	s := CollectFromFamilies([]*dto.MetricFamily{nil, {Name: nil}})
	if s == nil || len(s.Durations) != 0 {
		t.Error("nil family 应安全跳过")
	}
}

// TestCollectSummary_IsolatedFromDurations 守护口径隔离：
// *_size_bytes 等 Summary 的 sum 是字节数，绝不能混入 Durations——
// 否则诊断拿它算错误率/P99、渲染把字节显示成毫秒。
func TestCollectSummary_IsolatedFromDurations(t *testing.T) {
	fam := &dto.MetricFamily{
		Name: strp("http_request_size_bytes"),
		Type: mt(dto.MetricType_SUMMARY),
		Metric: []*dto.Metric{{
			Summary: &dto.Summary{
				SampleCount: u64p(100),
				SampleSum:   f64p(51200), // 平均 512B
			},
			Label: []*dto.LabelPair{{Name: strp("result"), Value: strp("error")}},
		}},
	}
	s := CollectFromFamilies([]*dto.MetricFamily{fam})
	if _, ok := s.Durations["http_request_size_bytes"]; ok {
		t.Fatal("Summary 不应混入 Durations（会被诊断当成耗时误报）")
	}
	agg, ok := s.Summaries["http_request_size_bytes"]
	if !ok || agg.Count != 100 || agg.SumSec != 51200 {
		t.Fatalf("Summary 应进 Summaries 且累计值正确, got %+v", agg)
	}
	// 诊断不应把字节 Summary 算进异常（即使 result=error）
	diag := Diagnose(s)
	if diag.Level != DiagnosisOK {
		t.Errorf("字节 Summary 不应触发耗时/错误率诊断, got %+v", diag)
	}
	// 兜底渲染走字节单位，不出现 ms
	text := RenderReport(s, 60*time.Second, "test")
	if !strings.Contains(text, "512B") {
		t.Errorf("字节 Summary 应按字节渲染, got:/n%s", text)
	}
	if strings.Contains(text, "ms") {
		t.Errorf("字节 Summary 不应显示成毫秒, got:/n%s", text)
	}
}

// TestCollectGRPCCodeNormalized 守护第三方 gRPC provider 的口径统一：
// provider 用 grpc_code（OK/NotFound/Internal）而非全仓的 result=ok/error，
// 采集层必须归一化，否则 grpc 指标进得了统一 Registry 却进不了诊断/分组渲染。
func TestCollectGRPCCodeNormalized(t *testing.T) {
	fam := &dto.MetricFamily{
		Name: strp("grpc_server_handling_seconds"),
		Type: mt(dto.MetricType_HISTOGRAM),
		Metric: []*dto.Metric{
			{
				Histogram: &dto.Histogram{SampleCount: u64p(90), SampleSum: f64p(9)},
				Label:     []*dto.LabelPair{{Name: strp("grpc_code"), Value: strp("OK")}},
			},
			{
				Histogram: &dto.Histogram{SampleCount: u64p(10), SampleSum: f64p(4)},
				Label:     []*dto.LabelPair{{Name: strp("grpc_code"), Value: strp("Internal")}},
			},
		},
	}
	s := CollectFromFamilies([]*dto.MetricFamily{fam})
	agg := s.Durations["grpc_server_handling_seconds"]
	if agg == nil || agg.Count != 100 {
		t.Fatalf("grpc histogram 应被采集, got %+v", agg)
	}
	if agg.Result["ok"] != 90 || agg.Result["error"] != 10 {
		t.Errorf("grpc_code 应归一化为 result: ok=90 error=10, got %+v", agg.Result)
	}
	// 错误率诊断应能看到 grpc 失败（10% > 5% 警告阈值）
	diag := Diagnose(s)
	if diag.Level != DiagnosisWarn {
		t.Errorf("grpc 错误率应触发诊断, got %+v", diag)
	}

	// Counter 侧的 grpc_code 也要被 isErrorLabels 识别
	if !isErrorLabels("grpc_code=Internal,grpc_method=Ping") {
		t.Error("grpc_code!=OK 应判为 error")
	}
	if isErrorLabels("grpc_code=OK,grpc_method=Ping") {
		t.Error("grpc_code=OK 不应判为 error")
	}
	// 原生 result 仍优先，不被 grpc_code 覆盖
	if !isErrorLabels("result=error,grpc_code=OK") {
		t.Error("原生 result=error 应优先")
	}
}

// TestCollectFromFamilies_DurationsByLabel Histogram 按 label 维度保留快照
// （业务诊断按 entry/replica 下钻 P99 的基础设施）。
func TestCollectFromFamilies_DurationsByLabel(t *testing.T) {
	// 同指标两条 label 组合：entry=api / entry=notify，各 10 样本
	fams := []*dto.MetricFamily{
		{
			Name: strp("demo_request_duration_seconds"),
			Type: dto.MetricType_HISTOGRAM.Enum(),
			Metric: []*dto.Metric{
				{
					Label: []*dto.LabelPair{{Name: strp("entry"), Value: strp("api")}},
					Histogram: &dto.Histogram{
						SampleCount: u64p(10), SampleSum: f64p(5),
						Bucket: []*dto.Bucket{
							{UpperBound: f64p(1), CumulativeCount: u64p(8)},
							{UpperBound: f64p(10), CumulativeCount: u64p(10)},
						},
					},
				},
				{
					Label: []*dto.LabelPair{{Name: strp("entry"), Value: strp("notify")}},
					Histogram: &dto.Histogram{
						SampleCount: u64p(10), SampleSum: f64p(50),
						Bucket: []*dto.Bucket{
							{UpperBound: f64p(1), CumulativeCount: u64p(1)},
							{UpperBound: f64p(10), CumulativeCount: u64p(10)},
						},
					},
				},
			},
		},
	}
	s := CollectFromFamilies(fams)

	// 聚合口径：20 样本、sum=55
	agg := s.Durations["demo_request_duration_seconds"]
	if agg == nil || agg.Count != 20 || agg.SumSec != 55 {
		t.Fatalf("聚合口径 count/sum 不符: %+v", agg)
	}
	// label 口径：两条独立记录，各自 count/sum 不同
	byLabel := s.DurationsByLabel["demo_request_duration_seconds"]
	if len(byLabel) != 2 {
		t.Fatalf("应有 2 个 label 组合, got %d", len(byLabel))
	}
	api := byLabel["entry=api"]
	notify := byLabel["entry=notify"]
	if api == nil || api.Count != 10 || api.SumSec != 5 {
		t.Errorf("entry=api 数据不符: %+v", api)
	}
	if notify == nil || notify.Count != 10 || notify.SumSec != 50 {
		t.Errorf("entry=notify 数据不符: %+v", notify)
	}
	// 关键：两条链的 P99 必须不同（证明 label 维度没被合并）
	apiP, _ := api.P99Sec()
	notifyP, _ := notify.P99Sec()
	if apiP >= notifyP {
		t.Errorf("notify 链 P99 应远高于 api 链: api=%v notify=%v", apiP, notifyP)
	}
}
