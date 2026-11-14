package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap/zapcore"

	"github.com/18721889353/sunshine/pkg/logger"
)

// TestWithSentinelRequestFields 验证「拦截入参取证」扩展点：
//  1. 仅在【拦截发生时】调用提取器，正常放行请求零开销；
//  2. 提取器返回的字段确实写进 Warn 日志（可被 hook 捕获）；
//  3. 不注入时行为不变（向后兼容）。
func TestWithSentinelRequestFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var mu sync.Mutex
	var captured []logger.Field
	_, err := logger.Init(
		logger.WithSave(false),
		logger.WithCustomHooksWithCtx(func(_ context.Context, entry zapcore.Entry, fields []logger.Field) error {
			if entry.Level >= zapcore.WarnLevel && entry.Message == "sentinel block" {
				mu.Lock()
				captured = append([]logger.Field(nil), fields...)
				mu.Unlock()
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("init logger: %v", err)
	}
	t.Cleanup(func() { _ = logger.Shutdown(context.Background()) })

	var calls int64
	r := gin.New()
	// 阈值 1 QPS：第一次放行，后续被限流。
	r.Use(SentinelMiddleware(
		WithSentinelResourceExtractor(func(c *gin.Context) string { return c.Request.URL.Path }),
		WithSentinelFlowRules([]*flow.Rule{{
			Resource:               "/test/forensics",
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject,
			Threshold:              1,
			StatIntervalInMs:       1000,
		}}),
		WithSentinelRequestFields(func(c *gin.Context) []logger.Field {
			atomic.AddInt64(&calls, 1)
			return []logger.Field{
				logger.String("cipher_data", "CIPHER-FORENSICS-CANARY"),
				logger.String("nonce", "n-001"),
			}
		}),
	))
	r.GET("/test/forensics", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 连发若干请求，确保既有放行也有拦截。
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test/forensics", nil))
	}

	mu.Lock()
	snapshot := append([]logger.Field(nil), captured...)
	mu.Unlock()

	if len(snapshot) == 0 {
		t.Fatal("被限流时必须产出 Warn 日志")
	}
	gotCalls := atomic.LoadInt64(&calls)
	if gotCalls == 0 {
		t.Fatal("拦截时提取器必须被调用")
	}
	// 5 次请求里至少 1 次放行，因此调用次数必须小于请求总数（放行零开销）。
	if gotCalls >= 5 {
		t.Fatalf("提取器在放行请求上不应被调用, calls=%d", gotCalls)
	}

	found := false
	for _, f := range snapshot {
		if f.Key == "cipher_data" && f.String == "CIPHER-FORENSICS-CANARY" {
			found = true
		}
	}
	if !found {
		var keys []string
		for _, f := range snapshot {
			keys = append(keys, f.Key)
		}
		t.Fatalf("拦截日志缺少取证字段 cipher_data, 实际字段=%s", strings.Join(keys, ","))
	}
}

// TestSentinelMiddlewareWithoutRequestFields 不注入扩展点时行为不变（向后兼容）。
func TestSentinelMiddlewareWithoutRequestFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SentinelMiddleware(
		WithSentinelResourceExtractor(func(c *gin.Context) string { return c.Request.URL.Path }),
		WithSentinelFlowRules([]*flow.Rule{{
			Resource:               "/test/compat",
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject,
			Threshold:              1000,
			StatIntervalInMs:       1000,
		}}),
	))
	r.GET("/test/compat", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test/compat", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("不注入扩展点应正常放行, got %d", w.Code)
	}
}
