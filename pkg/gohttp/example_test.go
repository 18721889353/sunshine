package gohttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

// 本文件是 gohttp 的可运行示例（替代原 USAGE_EXAMPLES.go：独立 .go 文件会把示例
// 编译进生产二进制，example_test.go 只在 `go test` 时存在）。
// 输出断言同时充当文档正确性的回归防线——示例与实现漂移会直接让测试失败。

// ExampleNew 演示客户端构造：配置非法时 New 返回错误（构造期快速失败）。
func ExampleNew() {
	// 本地示例服务（示例内联启动，保证离线可运行）
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client, err := New(
		WithBaseURL(ts.URL),
		WithTimeout(5*time.Second),
		WithRetry(2, time.Second, 3*time.Second),
	)
	if err != nil {
		fmt.Println("构造失败:", err)
		return
	}
	defer client.Close()

	resp, err := client.Request(nil).Get("/health")
	if err != nil {
		fmt.Println("请求失败:", err)
		return
	}
	fmt.Println(resp.StatusCode())

	// Output:
	// 200
}

// ExampleRequest_Post 演示链式请求构建：请求头、查询参数、请求体与响应解析。
func ExampleRequest_Post() {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(r.Method, r.URL.Path, r.URL.Query().Get("trace"), r.Header.Get("X-Biz"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	client, err := New(WithBaseURL(ts.URL), WithRetry(0, 0, 0))
	if err != nil {
		fmt.Println("构造失败:", err)
		return
	}
	defer client.Close()

	_, err = client.Request(nil).
		SetHeader("X-Biz", "order").
		SetQueryParam("trace", "on").
		SetBody(map[string]string{"id": "1"}).
		Post("/orders")
	if err != nil {
		fmt.Println("请求失败:", err)
	}

	// Output:
	// POST /orders on order
}

// ExampleValidateURL 演示请求前的 URL 字面量校验（协议白名单 + 内网 IP 拦截）。
// 域名指向内网的攻击需要 WithSSRFProtection 在拨号层拦截，两者互补。
func ExampleValidateURL() {
	fmt.Println(ValidateURL("https://api.example.com/v1"))
	fmt.Println(ValidateURL("http://10.0.0.1/admin"))
	fmt.Println(ValidateURL("ftp://example.com/file"))

	// Output:
	// <nil>
	// gohttp: request blocked due to SSRF protection
	// gohttp: 仅允许 http/https 协议，当前为 "ftp"
}
