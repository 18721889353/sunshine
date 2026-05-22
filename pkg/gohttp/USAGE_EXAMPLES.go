package gohttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ExampleBasicUsage 基础使用示例
func ExampleBasicUsage() {
	client := New(
		WithBaseURL("https://api.example.com"),
		WithTimeout(10*time.Second),
	)
	defer client.Close()

	ctx := context.Background()
	resp, err := client.Request(ctx).Get("/users/123")
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
		return
	}

	fmt.Printf("Status: %d\n", resp.StatusCode())
	fmt.Printf("Response time: %v\n", resp.ResponseTime())
}

// ExampleWithSSRFProtection SSRF防护示例
func ExampleWithSSRFProtection() {
	client := New(
		WithBaseURL("https://api.example.com"),
		WithSSRFProtection(), // 启用SSRF防护
	)

	// 正常请求不受影响
	ctx := context.Background()
	_, err := client.Request(ctx).Get("/users")
	if err != nil {
		fmt.Printf("Request error: %v\n", err)
	}

	// 尝试访问内网会被阻止
	err = ValidateURL("http://192.168.1.1/admin")
	if err == ErrSSRFBlocked {
		fmt.Println("SSRF attack blocked!")
	}
}

// ExampleWithCircuitBreaker 熔断器示例
func ExampleWithCircuitBreaker() {
	client := New(
		WithBaseURL("https://api.example.com"),
		WithCircuitBreaker(5), // 连续失败5次触发熔断
		WithRetry(3, 1*time.Second, 5*time.Second),
	)
	defer client.Close()

	ctx := context.Background()
	resp, err := client.Request(ctx).Post("/orders")
	if err != nil {
		fmt.Printf("Order creation failed: %v\n", err)
		return
	}

	fmt.Printf("Order created: %d\n", resp.StatusCode())
}

// ExampleDynamicTimeout 动态调整超时示例
func ExampleDynamicTimeout() {
	// 初始设置较短超时
	client := New(WithTimeout(5 * time.Second))

	// 运行时根据业务需求调整
	client.UpdateTimeout(30 * time.Second) // 大文件上传需要更长时间

	ctx := context.Background()
	_, err := client.Request(ctx).Post("/upload/large-file")
	if err != nil {
		fmt.Printf("Upload failed: %v\n", err)
	}
}

// ExampleConnectionPoolStats 连接池监控示例
func ExampleConnectionPoolStats() {
	client := New(
		WithBaseURL("https://api.example.com"),
	)

	// 发起一些请求
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, err := client.Request(ctx).Get(fmt.Sprintf("/items/%d", i))
		if err != nil {
			fmt.Printf("Request %d error: %v\n", i, err)
		}
	}

	// 查看连接池状态
	stats := client.GetConnectionPoolStats()
	fmt.Printf("Max idle connections: %d\n", stats["max_idle_conns"])
	fmt.Printf("Max connections per host: %d\n", stats["max_conns_per_host"])
}

// ExampleRetryMechanism 智能重试示例
func ExampleRetryMechanism() {
	client := New(
		WithBaseURL("https://api.example.com"),
		WithRetry(3, 1*time.Second, 5*time.Second), // 最多重试3次，指数退避
	)

	ctx := context.Background()
	// 以下情况会自动重试：
	// - 网络故障（DNS失败、连接超时）
	// - HTTP 502/503/504/429
	resp, err := client.Request(ctx).Get("/unstable-endpoint")
	if err != nil {
		fmt.Printf("Failed after retries: %v\n", err)
		return
	}

	fmt.Printf("Success after retry: %d\n", resp.StatusCode())
}

// ExampleTLSMTLS TLS/mTLS双向认证示例
func ExampleTLSMTLS() {
	client := New(
		WithBaseURL("https://secure-api.example.com"),
		WithRootCA("/path/to/ca.crt"),                                // 授信自签名CA
		WithClientCert("/path/to/client.crt", "/path/to/client.key"), // 客户端证书
	)
	defer client.Close()

	ctx := context.Background()
	resp, err := client.Request(ctx).Get("/protected-resource")
	if err != nil {
		fmt.Printf("mTLS request failed: %v\n", err)
		return
	}

	fmt.Printf("mTLS authenticated: %d\n", resp.StatusCode())
}

// ExampleErrorHandling 错误处理示例
func ExampleErrorHandling() {
	client := New(WithBaseURL("https://api.example.com"))

	ctx := context.Background()
	_, err := client.Request(ctx).Post("/validate")
	if err != nil {
		var httpErr *ErrorResponse
		if errors.As(err, &httpErr) {
			fmt.Printf("HTTP Error %d: %s\n", httpErr.StatusCode, httpErr.Message)
			fmt.Printf("Response body: %s\n", string(httpErr.Body))
		} else {
			fmt.Printf("Network error: %v\n", err)
		}
	}
}

// ExampleTraceInjection 链路追踪注入示例
func ExampleTraceInjection() {
	client := New(WithBaseURL("https://api.example.com"))

	// 使用自定义类型作为context key，避免lint警告
	type contextKey string
	const traceIDKey contextKey = "common_trace_id"

	// 从context自动提取TraceID并注入到请求头
	ctx := context.WithValue(context.Background(), traceIDKey, "trace-abc-123-xyz")

	resp, err := client.Request(ctx).Get("/users")
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
		return
	}

	// 服务端会收到 X-Trace-ID: trace-abc-123-xyz
	fmt.Printf("Trace injected, status: %d\n", resp.StatusCode())
}

// ExampleHighConcurrency 高并发场景配置
func ExampleHighConcurrency() {
	// 自定义高性能传输层
	transport := createOptimizedTransport()

	client := New(
		WithBaseURL("https://api.example.com"),
		WithTransport(transport),
		WithTimeout(5*time.Second),
		WithRetry(2, 500*time.Millisecond, 2*time.Second),
	)
	defer client.Close()

	// 模拟高并发请求
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			resp, err := client.Request(ctx).Get(fmt.Sprintf("/items/%d", id))
			if err != nil {
				fmt.Printf("Request %d failed: %v\n", id, err)
				return
			}
			fmt.Printf("Request %d success: %d\n", id, resp.StatusCode())
		}(i)
	}
	wg.Wait()
}

// createOptimizedTransport 创建优化的传输层配置
func createOptimizedTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          1000,             // 全局最大空闲连接
		MaxIdleConnsPerHost:   200,              // 单主机最大空闲连接
		IdleConnTimeout:       90 * time.Second, // 空闲连接回收时间
		TLSHandshakeTimeout:   5 * time.Second,  // TLS握手超时
		ExpectContinueTimeout: 1 * time.Second,
	}
}
