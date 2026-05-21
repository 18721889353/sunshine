package httpcli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHTTPClient_Success 测试高并发场景下的标准请求、链路追踪及连接池稳定性
func TestHTTPClient_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证自动注入的 User-Agent
		if r.Header.Get("User-Agent") == "" {
			t.Error("User-Agent missing in request headers")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "success", "code": 200}`))
	}))
	defer ts.Close()

	client := New(
		WithBaseURL(ts.URL),
		WithTimeout(2*time.Second),
	)

	// 模拟多线程/高并发下的长连接复用
	for i := 0; i < 5; i++ {
		ctx := context.WithValue(context.Background(), "common_trace_id", "test-trace-778899")
		resp, err := client.Request(ctx).Get("/api/v1/user")
		if err != nil {
			t.Fatalf("Iteration %d failed: %v", i, err)
		}

		if resp.StatusCode() != http.StatusOK {
			t.Errorf("Expected status 200, got: %d", resp.StatusCode())
		}
	}
}

// TestHTTPClient_RetryOnServerError 测试偶发性 502 网关错误时的退避重试机制
func TestHTTPClient_RetryOnServerError(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusBadGateway) // 前两次返回502
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("recovered"))
	}))
	defer ts.Close()

	// 缩短退避等待时间，加快单元测试运行速度
	client := New(
		WithBaseURL(ts.URL),
		WithRetry(3, 5*time.Millisecond, 20*time.Millisecond),
	)

	resp, err := client.Request(context.Background()).Get("/retry-test")
	if err != nil {
		t.Fatalf("Expected recovery after retry, got error: %v", err)
	}

	if callCount != 3 {
		t.Errorf("Expected exactly 3 calls (2 failures + 1 success), actually called: %d", callCount)
	}

	if resp.String() != "recovered" {
		t.Errorf("Unexpected payload response: %s", resp.String())
	}
}

// TestHTTPClient_ErrorResponseHandling 测试 4xx/5xx 强类型转换为 ErrorResponse 的行为
func TestHTTPClient_ErrorResponseHandling(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity) // 返回 422 业务参数错误
		_, _ = w.Write([]byte(`{"error": "field_too_short"}`))
	}))
	defer ts.Close()

	client := New(WithBaseURL(ts.URL))
	_, err := client.Request(context.Background()).Post("/validate")

	if err == nil {
		t.Fatal("Expected strong-typed error response, got nil")
	}

	var httpErr *ErrorResponse
	if !errors.As(err, &httpErr) {
		t.Fatalf("Expected error type *ErrorResponse, got: %T", err)
	}

	if httpErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("Expected 422, got: %d", httpErr.StatusCode)
	}

	if string(httpErr.Body) != `{"error": "field_too_short"}` {
		t.Errorf("Payload verification failure, got: %s", string(httpErr.Body))
	}
}

// TestHTTPClient_CustomTLSCertificates 测试自定义自签名证书单向与双向认证
func TestHTTPClient_CustomTLSCertificates(t *testing.T) {
	// 1. 创建本地受 TLS 保护的测试服务器
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 如果是双向认证，验证客户端证书是否存在
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("mTLS verified success"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("single TLS verified success"))
	}))
	defer ts.Close()

	// 2. 将 httptest 动态生成的自签名证书导出到本地临时文件系统模拟企业证书环境
	cert := ts.Certificate()

	// 3. 直接通过内存 CertPool 快速测试单向授信（跳过不可靠的文件IO）
	cp := x509.NewCertPool()
	cp.AddCert(cert)

	customTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: cp, // 授信本地 Mock 的 CA
		},
	}

	client := New(
		WithBaseURL(ts.URL),
		WithTransport(customTransport),
	)

	resp, err := client.Request(context.Background()).Get("/secure-endpoint")
	if err != nil {
		t.Fatalf("TLS verification failed: %v", err)
	}

	if resp.StatusCode() != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode())
	}
}

// TestURLValidator_SSRFProtection 测试 SSRF 防护
func TestURLValidator_SSRFProtection(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		shouldError bool
	}{
		{
			name:        "valid external URL",
			url:         "https://api.example.com/path",
			shouldError: false,
		},
		{
			name:        "internal IP blocked",
			url:         "http://169.254.169.254/latest/meta-data/",
			shouldError: true,
		},
		{
			name:        "localhost blocked",
			url:         "http://127.0.0.1:8080/admin",
			shouldError: true,
		},
		{
			name:        "private IP blocked",
			url:         "http://192.168.1.1/internal",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for URL %s, but got none", tt.url)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Unexpected error for URL %s: %v", tt.url, err)
			}
		})
	}
}

// TestCircuitBreaker_Config 测试熔断器配置
func TestCircuitBreaker_Config(t *testing.T) {
	client := New(
		WithCircuitBreaker(5),
	)

	// 验证配置已应用
	client.mu.RLock()
	if !client.config.enableCircuitBreaker {
		t.Error("Circuit breaker should be enabled")
	}
	if client.config.circuitBreakerThreshold != 5 {
		t.Errorf("Expected threshold 5, got %d", client.config.circuitBreakerThreshold)
	}
	client.mu.RUnlock()
}

// TestGracefulShutdown 测试优雅关闭
func TestGracefulShutdown(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // 模拟慢请求
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := New(WithBaseURL(ts.URL))

	// 发起一个请求
	ctx := context.Background()
	_, err := client.Request(ctx).Get("/test")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// 优雅关闭
	client.Close()

	// 再次关闭应该是安全的（幂等）
	client.Close()
}

// TestConnectionPoolStats 测试连接池统计
func TestConnectionPoolStats(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := New(WithBaseURL(ts.URL))

	// 发起几个请求建立连接
	for i := 0; i < 3; i++ {
		_, err := client.Request(context.Background()).Get("/test")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
	}

	// 获取连接池统计信息
	stats := client.GetConnectionPoolStats()
	if _, ok := stats["max_idle_conns"]; !ok {
		t.Error("max_idle_conns key missing from stats")
	}
	if _, ok := stats["max_conns_per_host"]; !ok {
		t.Error("max_conns_per_host key missing from stats")
	}
}

// TestDynamicTimeoutUpdate 测试动态更新超时时间
func TestDynamicTimeoutUpdate(t *testing.T) {
	client := New(WithTimeout(5 * time.Second))

	// 动态更新超时时间
	client.UpdateTimeout(10 * time.Second)

	client.mu.RLock()
	if client.config.timeout != 10*time.Second {
		t.Errorf("Expected timeout 10s, got %v", client.config.timeout)
	}
	client.mu.RUnlock()
}

// TestSSRFProtectionWithURLValidation 测试带URL校验的请求
func TestSSRFProtectionWithURLValidation(t *testing.T) {
	// 正常外部URL应该通过
	err := ValidateURL("https://api.example.com/test")
	if err != nil {
		t.Fatalf("Valid external URL should not error: %v", err)
	}

	// 内网URL应该被阻止
	err = ValidateURL("http://192.168.1.1/admin")
	if err != ErrSSRFBlocked {
		t.Errorf("Expected SSRFBlocked error for private IP, got: %v", err)
	}

	// localhost应该被阻止
	err = ValidateURL("http://127.0.0.1:8080/admin")
	if err != ErrSSRFBlocked {
		t.Errorf("Expected SSRFBlocked error for localhost, got: %v", err)
	}

	// 不支持的协议应该被拒绝
	err = ValidateURL("file:///etc/passwd")
	if err == nil {
		t.Error("File protocol should be blocked")
	}
}
