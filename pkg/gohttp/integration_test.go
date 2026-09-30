//go:build integration

package gohttp

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 本文件是 gohttp 的集成测试：只依赖真实 HTTP 服务，默认不编译、不执行。
//
// 运行方式：
//
//	cd pkg/gohttp
//	go test -tags=integration -v -count=1 ./...
//
// 环境变量（包内 .env 可自动提供，见 loadDotEnv；CI 用真实环境变量覆盖即可，无需 .env 文件）：
//
//	GOHTTP_TEST_BASE_URL      必需，被测服务的 base URL（如 https://httpbin.org）
//	GOHTTP_TEST_PATH          可选，默认 /，拼在 base URL 后的请求路径
//	GOHTTP_TEST_EXPECT_STATUS  可选，默认 200，期望的响应状态码
//	GOHTTP_TEST_TLS_URL        可选，设置后追加 TLS 握手集成用例
//	GOHTTP_TEST_TIMEOUT        可选，测试总时长上限，默认 120s（Go duration 格式）

// defaultIntegrationTimeout 集成测试总时长的兜底上限，防止客户端/网络异常导致 go test 挂死
const defaultIntegrationTimeout = 120 * time.Second

// integrationTimeout 返回集成测试总时长上限，由 GOHTTP_TEST_TIMEOUT 配置；
// 未设置或取值非法（无法解析 / 非正数）时使用兜底值
func integrationTimeout() time.Duration {
	v := os.Getenv("GOHTTP_TEST_TIMEOUT")
	if v == "" {
		return defaultIntegrationTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		fmt.Fprintf(os.Stderr, "WARN: GOHTTP_TEST_TIMEOUT=%q 不是有效的正 duration，使用兜底值 %s\n", v, defaultIntegrationTimeout)
		return defaultIntegrationTimeout
	}
	return d
}

// loadDotEnv 在包目录存在 .env 时加载测试所需环境变量，仅填充尚未设置的项（显式 export 优先）。
//
// 解析规则（不引入第三方 dotenv 依赖，与 pkg/nacoscli 的实现语义一致）：
//   - 跳过空行与 `#` 注释行；按**第一个** `=` 分割 key/value（值可含 `=` 与括号）
//   - 值成对的引号会被去除，其余内容原样保留（不做变量展开）
//   - **已存在且非空的环境变量不覆盖**，便于 CI 用真实变量覆盖本地 .env
//
// 仅在 `-tags=integration` 的测试进程中生效，不影响生产代码；.env 不存在时静默返回 0。
func loadDotEnv(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var loaded int
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return loaded, err
		}
		loaded++
	}

	return loaded, nil
}

// TestMain 限制集成测试总时长，防止网络/客户端 goroutine 泄漏让 go test 永远挂起
// （挂起的测试拿不到任何读数）。上限可通过 GOHTTP_TEST_TIMEOUT 上调。
func TestMain(m *testing.M) {
	// 先加载包内 .env（不存在时静默跳过），使 `go test -tags=integration` 无需手动 export
	if n, err := loadDotEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: 读取 .env 失败，仅使用进程环境变量: %v\n", err)
	} else if n > 0 {
		fmt.Printf("已从 .env 加载 %d 个环境变量（不覆盖已设置的变量）\n", n)
	}

	timeout := integrationTimeout()
	ch := make(chan int, 1)
	go func() {
		ch <- m.Run()
	}()

	select {
	case code := <-ch:
		os.Exit(code)
	case <-time.After(timeout):
		fmt.Fprintf(os.Stderr,
			"FATAL: 集成测试超过 %s 超时（可用 GOHTTP_TEST_TIMEOUT 上调）\n", timeout)
		os.Exit(1)
	}
}

// requireHTTPService 读取集成测试目标配置并完成可达性前置检查。
// 环境变量缺失或服务不可达时跳过（区分「没配置」「连不上」两类环境问题并打印具体 error），
// 配置合法且可达时返回 base URL、路径与期望状态码。
func requireHTTPService(t *testing.T) (baseURL, path string, expectStatus int) {
	t.Helper()

	baseURL = os.Getenv("GOHTTP_TEST_BASE_URL")
	if baseURL == "" {
		t.Skip("GOHTTP_TEST_BASE_URL 未设置，跳过集成测试")
	}
	path = os.Getenv("GOHTTP_TEST_PATH")
	if path == "" {
		path = "/"
	}

	expectStatus = http.StatusOK
	if v := os.Getenv("GOHTTP_TEST_EXPECT_STATUS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("GOHTTP_TEST_EXPECT_STATUS=%q 不是合法整数: %v", v, err)
		}
		expectStatus = n
	}

	// TCP 可达性探测：不可达时 Skip 并带上具体 error，便于区分
	// 「端口没开」「IP 写错」「防火墙丢包」三类环境问题（基线 4.3）
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("GOHTTP_TEST_BASE_URL 解析失败: %v", err)
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 3*time.Second)
	if err != nil {
		t.Skipf("目标 %s:%s TCP 不可达，跳过: %v", host, port, err)
	}
	_ = conn.Close()

	return baseURL, path, expectStatus
}

// TestIntegration_GetEndpoint 对真实 HTTP 服务执行 GET 并断言状态码。
// 状态码与期望不符时 Fail（被测代码的问题），不可达/未配置时 Skip（环境问题）。
func TestIntegration_GetEndpoint(t *testing.T) {
	baseURL, path, expectStatus := requireHTTPService(t)

	client, err := New(WithBaseURL(baseURL), WithTimeout(10*time.Second), WithRetry(1, time.Second, 2*time.Second))
	if err != nil {
		t.Fatalf("New 构造失败（配置问题应在此暴露）: %v", err)
	}
	defer client.Close()

	resp, err := client.Request(t.Context()).Get(path)
	if err != nil {
		var httpErr *ErrorResponse
		if ok := errors.As(err, &httpErr); ok && httpErr.StatusCode == expectStatus {
			// 期望状态码本身是非 2xx（如 404 探活），包内约定以 ErrorResponse 返回，属通过
			t.Logf("GET %s%s 返回预期状态码 %d（ErrorResponse 形态）", baseURL, path, expectStatus)
			return
		}
		t.Fatalf("GET %s%s 失败: %v", baseURL, path, err)
	}

	if resp.StatusCode() != expectStatus {
		t.Fatalf("GET %s%s 状态码不符: got=%d want=%d body=%s",
			baseURL, path, resp.StatusCode(), expectStatus, snippet(resp.Body()))
	}
	t.Logf("GET %s%s → %d, 耗时 %s, body=%s",
		baseURL, path, resp.StatusCode(), resp.ResponseTime(), snippet(resp.Body()))
}

// TestIntegration_TLSRequest 仅在设置 GOHTTP_TEST_TLS_URL 时执行：
// 对真实 HTTPS 端点发起请求，验证 TLS 握手与系统信任链在真实环境下可用。
// （本包单测用自签证书覆盖装配逻辑，公网信任链只能在真实网络下取证）
func TestIntegration_TLSRequest(t *testing.T) {
	tlsURL := os.Getenv("GOHTTP_TEST_TLS_URL")
	if tlsURL == "" {
		t.Skip("GOHTTP_TEST_TLS_URL 未设置，跳过 TLS 集成测试")
	}

	u, err := url.Parse(tlsURL)
	if err != nil {
		t.Fatalf("GOHTTP_TEST_TLS_URL 解析失败: %v", err)
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), portOrDefault(u)), 3*time.Second)
	if err != nil {
		t.Skipf("TLS 目标 %s TCP 不可达，跳过: %v", u.Hostname(), err)
	}
	_ = conn.Close()

	client, err := New(WithTimeout(10*time.Second), WithRetry(0, 0, 0))
	if err != nil {
		t.Fatalf("New 构造失败: %v", err)
	}
	defer client.Close()

	resp, err := client.Request(t.Context()).Get(tlsURL)
	if err != nil {
		t.Fatalf("TLS 请求 %s 失败（握手或信任链问题）: %v", tlsURL, err)
	}
	if resp.StatusCode() >= 500 {
		t.Fatalf("TLS 请求返回 %d，服务端异常", resp.StatusCode())
	}
	t.Logf("TLS 请求 %s → %d, 耗时 %s", tlsURL, resp.StatusCode(), resp.ResponseTime())
}

// portOrDefault 取 URL 端口，未显式指定时按 scheme 返回默认端口
func portOrDefault(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

// snippet 截取响应体前 200 字节，避免把整页 HTML 错误打进测试日志
func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		return s[:200] + "...(截断)"
	}
	return s
}
