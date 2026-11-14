package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// findAttr 从 span 属性中按键取值。
func findAttr(span sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// TestTracingWithRouteExtractor 验证：
//  1. 默认（不注入）行为不变：http.route 仍是 Gin 路由模板，保证向后兼容；
//  2. 注入后 http.route 与 spanName 都变成【真实请求路径】，
//     解决通配路由把多个业务接口聚成一条、链路排查失去价值的问题。
func TestTracingWithRouteExtractor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const realPath = "/Api/Audiovisual/Order/Recharge"

	cases := []struct {
		name      string
		opts      []TraceOption
		wantRoute string
	}{
		{"default_fullpath", nil, "/Api/*innerPath"},
		{"real_path", []TraceOption{WithRouteExtractor(func(c *gin.Context) string {
			return c.Request.URL.Path
		})}, realPath},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
			defer func() { _ = tp.Shutdown(context.Background()) }()

			opts := append([]TraceOption{WithTracerProvider(tp)}, tc.opts...)
			r := gin.New()
			r.Use(Tracing("test-svc", opts...))
			r.POST("/Api/*innerPath", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, realPath, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", w.Code)
			}

			ended := sr.Ended()
			if len(ended) != 1 {
				t.Fatalf("应产生 1 个 span, got %d", len(ended))
			}
			span := ended[0]
			if got, ok := findAttr(span, "http.route"); !ok || got != tc.wantRoute {
				t.Fatalf("http.route=%q, want %q", got, tc.wantRoute)
			}
			if span.Name() != tc.wantRoute {
				t.Fatalf("spanName=%q, want %q", span.Name(), tc.wantRoute)
			}
			// url.path 无论哪种模式都应是真实路径（排查时用）。
			if got, ok := findAttr(span, "url.path"); !ok || got != realPath {
				t.Fatalf("url.path=%q, want %q", got, realPath)
			}
		})
	}
}

// TestTracingRouteExtractorEmptyFallsBack 提取器返回空串时必须回退到 FullPath，
// 不能产生空 spanName 导致链路数据异常。
func TestTracingRouteExtractorEmptyFallsBack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	r := gin.New()
	r.Use(Tracing("test-svc",
		WithTracerProvider(tp),
		WithRouteExtractor(func(_ *gin.Context) string { return "" }),
	))
	r.GET("/Api/*innerPath", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Api/Common/Auth/getToken", nil))

	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("应产生 1 个 span, got %d", len(ended))
	}
	if got := ended[0].Name(); got != "/Api/*innerPath" {
		t.Fatalf("空提取结果应回退到 FullPath, got %q", got)
	}
}
