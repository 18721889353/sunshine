package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// TestFormatIntervalText 窗口标签：整分钟显示"近N分钟"，否则"近N秒"。
func TestFormatIntervalText(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{600 * time.Second, "近10分钟"},
		{600*time.Second + 3*time.Millisecond, "近600秒"}, // 生产实际（含耗时抖动）
		{60 * time.Second, "近1分钟"},
		{5 * time.Second, "近5秒"},
	}
	for _, c := range cases {
		if got := FormatIntervalText(c.in); got != c.want {
			t.Errorf("FormatIntervalText(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestUnifiedRegistry_HTTPMetricsReachableInFast 验证统一注册中心的双注册能力：
// 注册一个 mock HTTP 指标，确认注册后能从 Fast Registry 采到——否则 5s 巡检
// 会漏掉 HTTP 维度（报告/诊断缺数据）。
//
// 注意：这是注册中心【能力验证】，mock 必须用唯一测试名（test_ 前缀）：
// 本测试二进制会 import 各组件包触发"定义即注册"，若 mock 复用生产指标名
// （如 http_request_count_total），会与真实定义因 help/labels 不一致撞名 panic。
// 生产 gin 指标（含 http_request_count_total 真名）的注册由
// integration_registration_test.go 用 ginmetrics.MetricName* 常量守护。
func TestUnifiedRegistry_HTTPMetricsReachableInFast(t *testing.T) {
	httpReqCount := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_http_request_count_total", Help: "test",
	}, []string{"status"})
	MustRegister(httpReqCount)
	httpReqCount.WithLabelValues("200").Inc()

	fams, err := Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "test_http_request_count_total") {
			found = true
			break
		}
	}
	if !found {
		t.Error("注册中心双注册失效：mock 指标应进 Fast Registry（否则 5s 巡检采不到）")
	}
}
