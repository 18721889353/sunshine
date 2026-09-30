package gohttp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestGetSuccess 验证标准 GET 全链路：User-Agent 自动注入、状态码读取、并发下的连接复用
func TestRequestGetSuccess(t *testing.T) {
	var (
		mu       sync.Mutex
		gotUA    string
		gotTrace string
	)
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotUA = r.Header.Get("User-Agent")
		gotTrace = r.Header.Get("X-Trace-ID")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "success", "code": 200}`))
	}))

	client := mustNew(t, WithBaseURL(ts.URL), WithTimeout(2*time.Second))

	// 并发请求验证长连接复用
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.WithValue(context.Background(), traceIDContextKey, "test-trace-778899")
			resp, err := client.Request(ctx).Get("/api/v1/user")
			assert.NoError(t, err)
			if err == nil {
				assert.Equal(t, http.StatusOK, resp.StatusCode())
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "Golang-GoHttp-Enterprise/v2.0", gotUA, "User-Agent 应被自动注入")
	assert.Equal(t, "test-trace-778899", gotTrace, "旧版 X-Trace-ID 注入应保持兼容")
}

// TestRequestRetryOn502 验证偶发 502 时的指数退避重试：前两次 502、第三次成功
func TestRequestRetryOn502(t *testing.T) {
	var callCount int32
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("recovered"))
	}))

	client := mustNew(t, WithBaseURL(ts.URL), WithRetry(3, 5*time.Millisecond, 20*time.Millisecond))

	resp, err := client.Request(context.Background()).Get("/retry-test")
	require.NoError(t, err, "第 3 次重试成功后不应返回错误")
	assert.EqualValues(t, 3, atomic.LoadInt32(&callCount), "应恰好请求 3 次（2 次失败 + 1 次成功）")
	assert.Equal(t, "recovered", resp.String())
}

// TestDoErrorResponseTyped 验证非 2xx 响应被强类型包装为 *ErrorResponse
func TestDoErrorResponseTyped(t *testing.T) {
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error": "field_too_short"}`))
	}))

	client := mustNew(t, WithBaseURL(ts.URL))
	_, err := client.Request(context.Background()).Post("/validate")

	require.Error(t, err)
	var httpErr *ErrorResponse
	require.True(t, errors.As(err, &httpErr), "应能通过 errors.As 提取 *ErrorResponse，实际类型: %T", err)
	assert.Equal(t, http.StatusUnprocessableEntity, httpErr.StatusCode)
	assert.Equal(t, `{"error": "field_too_short"}`, string(httpErr.Body))
}

// TestNewClientCustomTLSCertificates 验证通过内存 CertPool 授信自签名证书的单向 TLS 请求
func TestNewClientCustomTLSCertificates(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("TLS verified success"))
	}))
	t.Cleanup(ts.Close)

	// 将 httptest 动态生成的自签名证书注入内存 CertPool（跳过文件 IO）
	cp := x509.NewCertPool()
	cp.AddCert(ts.Certificate())

	client := mustNew(t,
		WithBaseURL(ts.URL),
		WithTransport(&http.Transport{TLSClientConfig: &tls.Config{RootCAs: cp}}),
	)

	resp, err := client.Request(context.Background()).Get("/secure-endpoint")
	require.NoError(t, err, "自签证书授信后请求应成功")
	assert.Equal(t, http.StatusOK, resp.StatusCode())
}

// TestClientCloseIdempotent 验证优雅关闭：发起请求后关闭，重复调用无副作用
func TestClientCloseIdempotent(t *testing.T) {
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL))

	_, err := client.Request(context.Background()).Get("/test")
	require.NoError(t, err)

	client.Close()
	client.Close() // 幂等：多次调用不 panic
}

// TestGetConnectionPoolStats 验证连接池统计信息包含约定的两个键
func TestGetConnectionPoolStats(t *testing.T) {
	client := mustNew(t)
	stats := client.GetConnectionPoolStats()
	assert.Contains(t, stats, "max_idle_conns")
	assert.Contains(t, stats, "max_conns_per_host")
	assert.Equal(t, DefaultMaxIdleConns, stats["max_idle_conns"])
	assert.Equal(t, DefaultMaxConnsPerHost, stats["max_conns_per_host"])
}

// TestUpdateTimeoutDynamicConfig 验证运行期动态更新超时时间写入配置快照
func TestUpdateTimeoutDynamicConfig(t *testing.T) {
	client := mustNew(t, WithTimeout(5*time.Second))

	client.UpdateTimeout(10 * time.Second)

	client.mu.RLock()
	assert.Equal(t, 10*time.Second, client.config.timeout)
	client.mu.RUnlock()

	// 非法值（非正数）被拒绝，保持现值
	client.UpdateTimeout(-1 * time.Second)
	client.mu.RLock()
	assert.Equal(t, 10*time.Second, client.config.timeout, "非正超时不应覆盖现有配置")
	client.mu.RUnlock()
}

// TestRequestNilCtxDefaultsBackground 验证 Request(nil) 归一为 Background 而非 panic
func TestRequestNilCtxDefaultsBackground(t *testing.T) {
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL))

	req := client.Request(nil)
	require.NotNil(t, req)
	resp, err := req.Get("/ok")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
}

// TestDoPanicRecovered 验证执行期 panic 被 do() 恢复为错误返回（防护不再形同虚设）
func TestDoPanicRecovered(t *testing.T) {
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL))
	client.tracer = panicTracer{} // 注入会 panic 的 tracer，resty 捕获后 re-panic 到 do()

	_, err := client.Request(context.Background()).Get("/any")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic 已恢复")
}

// TestResponseHelpers 验证响应解耦层的常规方法：Header/JSON/IsSuccess/ResponseTime
func TestResponseHelpers(t *testing.T) {
	type payload struct {
		Code int `json:"code"`
	}
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "v1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code": 7}`))
	}))
	client := mustNew(t, WithBaseURL(ts.URL))

	resp, err := client.Request(context.Background()).Get("/json")
	require.NoError(t, err)
	assert.True(t, resp.IsSuccess())
	assert.Equal(t, "v1", resp.Header().Get("X-Custom"))
	assert.Greater(t, resp.ResponseTime(), time.Duration(0))

	var got payload
	require.NoError(t, resp.JSON(&got))
	assert.Equal(t, 7, got.Code)
	assert.Equal(t, `{"code": 7}`, resp.String())
	assert.Equal(t, `{"code": 7}`, string(resp.Body()))
}

// TestChainBuilderMethods 验证链式请求构建器：Header/Query/Body 全部送达服务端
func TestChainBuilderMethods(t *testing.T) {
	var (
		gotHeader string
		gotQuery  string
		gotBody   string
		gotMethod string
	)
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Biz")
		gotQuery = r.URL.Query().Get("uid")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL))

	resp, err := client.Request(context.Background()).
		SetHeader("X-Biz", "order").
		SetHeaders(map[string]string{"X-Multi": "yes"}).
		SetQueryParam("uid", "1001").
		SetQueryParams(map[string]string{"src": "app"}).
		SetBody(`{"name":"item"}`).
		Post("/chain")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "POST", gotMethod)
	assert.Equal(t, "order", gotHeader)
	assert.Equal(t, "1001", gotQuery)
	assert.JSONEq(t, `{"name":"item"}`, gotBody)
}

// TestNewRequestWithValidation 验证带 URL 校验的请求构建器：内网地址被拒绝
func TestNewRequestWithValidation(t *testing.T) {
	client := mustNew(t)

	_, err := client.NewRequestWithValidation(context.Background(), "GET", "http://127.0.0.1:8080/admin")
	assert.ErrorIs(t, err, ErrSSRFBlocked)

	req, err := client.NewRequestWithValidation(context.Background(), "GET", "https://api.example.com/x")
	require.NoError(t, err)
	assert.NotNil(t, req)
}
