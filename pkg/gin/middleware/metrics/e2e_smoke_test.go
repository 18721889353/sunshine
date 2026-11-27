package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	ginmetrics "github.com/18721889353/sunshine/pkg/gin/middleware/metrics"
)

// TestSmoke_HTTPMetricsEndToEnd 生产前端到端冒烟：
// HTTP 请求 → 指标中间件采集 → /metrics 暴露 → 输出含预期指标名。
func TestSmoke_HTTPMetricsEndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ginmetrics.Metrics(r))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	r.GET("/boom", func(c *gin.Context) { c.String(http.StatusInternalServerError, "err") })

	srv := httptest.NewServer(r)
	defer srv.Close()

	// 打真实请求（触发指标埋点）
	for _, path := range []string{"/ping", "/ping", "/boom"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("请求 %s 失败: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	// 抓 /metrics
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("/metrics 请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics 应 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)

	for _, want := range []string{"http_request_count_total", "http_request_duration_seconds"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("/metrics 输出应含 %q", want)
		}
	}
}
