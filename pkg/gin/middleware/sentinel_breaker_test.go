package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/gin-gonic/gin"
)

// TestSentinelBreakerTripsOn5xx 回归测试：验证下游持续返回 5xx 时，框架修复后的
// SentinelMiddleware 会把错误上报给 errorRatio 熔断器，并在错误率超过阈值后返回 503。
//
// 这正是原 sentinel-golang gin 适配器的缺陷：其 entry.Exit() 从不带错误，
// 导致 errorRatio 熔断永远统计不到上游 5xx，熔断形同虚设。本中间件改为自管理
// entry.Exit(base.WithError(...))，仅 5xx 上报（4xx 不计入，避免误伤健康下游）。
func TestSentinelBreakerTripsOn5xx(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resource := "/Api/Common/Auth/getToken"
	rules := []*circuitbreaker.Rule{
		{
			Resource:         resource,
			Strategy:         circuitbreaker.ErrorRatio,
			RetryTimeoutMs:   5000,
			MinRequestAmount: 200,
			StatIntervalMs:   20000,
			Threshold:        0.5,
		},
	}

	r := gin.New()
	r.Use(SentinelMiddleware(
		WithSentinelResourceExtractor(func(c *gin.Context) string { return c.Request.URL.Path }),
		WithSentinelCircuitBreakerRules(rules),
	))
	r.GET("/Api/Common/Auth/getToken", func(c *gin.Context) {
		// 模拟下游故障：返回 502
		c.Writer.WriteHeader(http.StatusBadGateway)
	})

	var mu sync.Mutex
	counts := map[int]int{}

	var wg sync.WaitGroup
	for i := 0; i < 260; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/Api/Common/Auth/getToken", nil)
			r.ServeHTTP(w, req)
			mu.Lock()
			counts[w.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	t.Logf("status code distribution: %v", counts)
	if counts[http.StatusServiceUnavailable] == 0 {
		t.Fatalf("熔断器未生效：260 个 5xx 请求后没有任何 503 返回，分布=%v", counts)
	}
}

// TestSentinelBreakerIgnores4xx 回归测试：4xx（客户端错误）不应触发 errorRatio 熔断，
// 避免把健康下游的"客户端参数错误"误判为服务故障。
func TestSentinelBreakerIgnores4xx(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resource := "/Api/Coupon/Coupon/detail"
	rules := []*circuitbreaker.Rule{
		{
			Resource:         resource,
			Strategy:         circuitbreaker.ErrorRatio,
			RetryTimeoutMs:   5000,
			MinRequestAmount: 200,
			StatIntervalMs:   20000,
			Threshold:        0.5,
		},
	}

	r := gin.New()
	r.Use(SentinelMiddleware(
		WithSentinelResourceExtractor(func(c *gin.Context) string { return c.Request.URL.Path }),
		WithSentinelCircuitBreakerRules(rules),
	))
	r.GET("/Api/Coupon/Coupon/detail", func(c *gin.Context) {
		// 模拟客户端参数错误：返回 400
		c.Writer.WriteHeader(http.StatusBadRequest)
	})

	var mu sync.Mutex
	counts := map[int]int{}

	var wg sync.WaitGroup
	for i := 0; i < 260; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/Api/Coupon/Coupon/detail", nil)
			r.ServeHTTP(w, req)
			mu.Lock()
			counts[w.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	t.Logf("status code distribution: %v", counts)
	if counts[http.StatusServiceUnavailable] != 0 {
		t.Fatalf("4xx 不应触发熔断，却出现了 503，分布=%v", counts)
	}
}
