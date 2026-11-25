package metrics

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestHandler_DefaultIncludesGoRuntime Handler 应暴露 go_* 运行时指标（与 promhttp.Handler 等价）
func TestHandler_DefaultIncludesGoRuntime(t *testing.T) {
	// 确保 DefaultGatherer 有 go collector（prometheus init 自动注册）
	h := Handler()
	if h == nil {
		t.Fatal("Handler 不应为 nil")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("状态码应 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Error("Default Handler 应含 go_* 运行时指标")
	}
}

// TestFastHandler_ExcludesGoRuntime FastHandler 不含 go_*（避免巡检 STW）
func TestFastHandler_ExcludesGoRuntime(t *testing.T) {
	h := FastHandler()
	if h == nil {
		t.Fatal("FastHandler 不应为 nil")
	}
	// 注册一个自定义指标确保 Fast Registry 非空
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "handler_test_counter", Help: "test"})
	MustRegister(c)
	c.Inc()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fast-metrics", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("状态码应 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "go_goroutines") {
		t.Error("FastHandler 不应含 go_* 运行时指标（避免 STW）")
	}
	if !strings.Contains(body, "handler_test_counter") {
		t.Error("FastHandler 应含自定义指标")
	}
}

// TestHandler_Singleton Handler 返回单例（避免每次重建 HandlerOpts）。
// 用反射比较函数指针——http.HandlerFunc 底层是 func，不可直接用 == 比较。
func TestHandler_Singleton(t *testing.T) {
	fnPtr := func(h http.Handler) uintptr {
		return reflect.ValueOf(h).Pointer()
	}
	if fnPtr(Handler()) != fnPtr(Handler()) {
		t.Error("Handler 应返回同一单例")
	}
	if fnPtr(FastHandler()) != fnPtr(FastHandler()) {
		t.Error("FastHandler 应返回同一单例")
	}
}
