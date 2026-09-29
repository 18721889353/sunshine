//go:build integration

package tracer

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// 集成测试运行方式（需要真实 OTLP Collector，如阿里云链路追踪）：
//
//	cd pkg/tracer
//	go test -tags=integration -v -count=1
//
// 环境变量（可在包目录 .env 中配置，TestMain 会自动加载且不覆盖已 export 的变量）：
//
//	OTEL_EXPORTER_OTLP_ENDPOINT_GRPC  gRPC 端点（host:port，勿带 scheme），如 tracing-analysis-dc-bj.aliyuncs.com:8090
//	OTEL_EXPORTER_OTLP_ENDPOINT_HTTP  HTTP 端点（完整 URL 含路径，token 通常嵌在路径中）
//	OTEL_EXPORTER_OTLP_HEADERS        可选，额外鉴权头部，格式 k1=v1,k2=v2（阿里云 gRPC 用 authentication=<token>）
//	OTEL_EXPORTER_OTLP_TOKEN          可选，Bearer token 简写，等效 Authorization=Bearer <token>
//	TRACER_TEST_TIMEOUT               可选，测试总时长上限（Go duration，默认 120s）
//
// 两类端点各自独立成用例，未配置的自动 Skip；鉴权被拒或网络不可达按部署差异 Skip，
// 导出链路出现其它错误必须 Fail（详见 exportErrClassify 的关键词判定表）。
const (
	// defaultIntegrationTimeout 集成测试总时长的兜底上限，防止外部 SDK 的 goroutine 泄漏导致 go test 永远挂起。
	defaultIntegrationTimeout = 120 * time.Second

	// dialProbeTimeout 端点 TCP 探测超时。
	// 为何只能固定等待：拨号超时是探测本身的参数，不依赖外部信号；
	// 取值依据：经验余量（3s 足以区分「拒绝连接」与「防火墙丢包」，未做实测标定）；
	// 为何不 flaky：探测失败走 Skipf 并打印具体 error，不会产生假失败。
	dialProbeTimeout = 3 * time.Second

	// spanRecordSimulateWait 每个 Span 结束前的模拟业务耗时。
	// 为何只能固定等待：仅用于让 Span 具有非零 duration，外部无「已记录」信号可等；
	// 取值依据：经验余量（50ms 对导出链路无实质影响，未做实测标定）；
	// 为何不 flaky：没有任何断言依赖该时长。
	spanRecordSimulateWait = 50 * time.Millisecond

	// exportFlushTimeout Close 时强制刷新待导出 Span 的等待上限。
	// 为何只能固定等待：SDK 未暴露「已全部导出」的可观测信号，只能以 Shutdown 返回值为准；
	// 取值依据：经验余量（与 BatchSpanProcessor 默认 batchTimeout 5s 同数量级，未做实测标定）；
	// 为何不 flaky：成功判据是 Close 返回 nil 且 errorCollector 零错误，不依赖固定读数。
	exportFlushTimeout = 5 * time.Second

	// exportTimeoutSeconds OTLP exporter 单次导出超时（与调用方常见配置一致）。
	exportTimeoutSeconds = 10
)

// integrationTimeout 返回总时长上限，由 TRACER_TEST_TIMEOUT 配置（Go duration，如 "180s"、"5m"）：
// 用例增多或网络较慢时上调即可，无需改代码。未设置或取值非法时使用兜底值。
func integrationTimeout() time.Duration {
	v := os.Getenv("TRACER_TEST_TIMEOUT")
	if v == "" {
		return defaultIntegrationTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		fmt.Fprintf(os.Stderr, "WARN: TRACER_TEST_TIMEOUT=%q 不是有效的正 duration，使用兜底值 %s\n", v, defaultIntegrationTimeout)
		return defaultIntegrationTimeout
	}
	return d
}

// loadDotEnv 在包目录存在 .env 时加载测试所需环境变量，仅填充尚未设置的项（显式 export 优先）。
//
// 解析规则（不引入第三方 dotenv 依赖）：
//   - 跳过空行与 `#` 注释行；按**第一个** `=` 分割 key/value（值可含 `=` 与括号）
//   - 去除成对的引号，其余内容原样保留（不做变量展开）
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

// TestMain 先补 .env，再强制限制总时长。
func TestMain(m *testing.M) {
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
			"FATAL: 集成测试超过 %s 超时（可能是外部 SDK goroutine 泄漏，可用 TRACER_TEST_TIMEOUT 上调）\n", timeout)
		os.Exit(1)
	}
}

// parseHeaders 解析 OTEL_EXPORTER_OTLP_HEADERS，格式 k1=v1,k2=v2。
// 按第一个 = 分割（值可含 @ / _ 等），多个头部用英文逗号分隔（值不能含逗号，与 OTel 官方语义一致）。
func parseHeaders(raw string) map[string]string {
	headers := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.Index(pair, "=")
		if idx <= 0 {
			continue
		}
		headers[strings.TrimSpace(pair[:idx])] = strings.TrimSpace(pair[idx+1:])
	}
	return headers
}

// authHeaders 合并 OTEL_EXPORTER_OTLP_HEADERS 与 OTEL_EXPORTER_OTLP_TOKEN：
// HEADERS 直接透传；TOKEN 是简写，等效 Authorization=Bearer <token>（HEADERS 已显式给出 Authorization 时不覆盖）。
func authHeaders() map[string]string {
	headers := parseHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))
	if token := os.Getenv("OTEL_EXPORTER_OTLP_TOKEN"); token != "" {
		if _, ok := headers["Authorization"]; !ok {
			headers["Authorization"] = "Bearer " + token
		}
	}
	return headers
}

// errorCollector 实现 otel.ErrorHandler，把导出链路的错误从「日志噪音」升级为「可判定断言」：
// BatchSpanProcessor 的导出失败会经过全局 ErrorHandler，Close 强制刷新后统一检查。
type errorCollector struct {
	mu   sync.Mutex
	errs []error
}

// Handle 采集一条 OTel 错误（可能来自 exporter 后台协程，需加锁）。
func (c *errorCollector) Handle(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, err)
}

// snapshot 返回已采集错误的副本。
func (c *errorCollector) snapshot() []error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]error(nil), c.errs...)
}

// authStatusRe 匹配带语境的 HTTP 鉴权状态码（如 `"status":403`、`code = 401`、`error 403`）。
// 裸 401/403 不匹配：端口号（host:403）、trace id 中的数字会造成误判；
// 若服务端仅返回无语境的纯文本 "403"，会被归为 Fail——宁可误报为缺陷也不静默放过。
var authStatusRe = regexp.MustCompile(`(?i)(status|code|error)[^0-9]{0,12}40[13]\b`)

// exportErrClassify 对导出错误做 Skip/Fail 归类，返回 ok=true 表示应 Skip（附归类原因）。
//
// 关键词判定表（每个词都写明「为何加入」，并明确「为何某些词不能加进来」——
// 曾有把基础设施故障误判为预期失败导致问题被静默放过的先例，勿随意扩充）：
//
//   - 鉴权类（unauthenticated/unauthorized/forbidden/permission denied/invalid token/invalid api key，
//     及 authStatusRe 匹配的带语境 401/403）：凭据被服务端拒绝属部署/凭据选择，不是本包代码缺陷 → Skip。
//   - 基础设施类（unavailable/connection refused/i/o timeout/deadline exceeded/no such host/network is unreachable）：
//     端点探测通过后仍可能被防火墙/代理/瞬时网络问题中断，属环境问题 → Skip。
//   - **不能加入**：裸 "401"/"403"（会把端口号、trace id 里的数字误判为鉴权错误）、
//     "timeout"（单独出现可能是本包导出超时配置过小，属配置缺陷，必须 Fail）、
//     "error"（过于宽泛会吞掉真实缺陷）、"exceeded quota"/"resource exhausted"（配额问题需人工介入，
//     静默 Skip 会掩盖容量风险，保持 Fail）。
func exportErrClassify(err error) (skip bool, reason string) {
	msg := strings.ToLower(err.Error())

	authKeywords := []string{
		"unauthenticated", "unauthorized",
		"forbidden", "permission denied", "invalid token", "invalid api key",
	}
	for _, kw := range authKeywords {
		if strings.Contains(msg, kw) {
			return true, "鉴权被拒"
		}
	}
	if authStatusRe.MatchString(msg) {
		return true, "鉴权被拒"
	}

	infraKeywords := []string{
		"unavailable", "connection refused", "i/o timeout",
		"deadline exceeded", "no such host", "network is unreachable",
	}
	for _, kw := range infraKeywords {
		if strings.Contains(msg, kw) {
			return true, "网络/基础设施不可达"
		}
	}

	return false, ""
}

// requireGRPCEndpoint 读取 gRPC 端点配置并做可达性前置检查。
func requireGRPCEndpoint(t *testing.T) (endpoint string) {
	t.Helper()
	endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT_GRPC")
	if endpoint == "" {
		t.Skip("OTEL_EXPORTER_OTLP_ENDPOINT_GRPC 未设置，跳过 gRPC 集成测试")
	}
	if err := dialEndpoint(endpoint); err != nil {
		t.Skipf("gRPC 端点 %s 不可达，跳过: %v", endpoint, err)
	}
	return endpoint
}

// requireHTTPEndpoint 读取 HTTP 端点配置并做可达性前置检查。
func requireHTTPEndpoint(t *testing.T) (endpoint string) {
	t.Helper()
	endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT_HTTP")
	if endpoint == "" {
		t.Skip("OTEL_EXPORTER_OTLP_ENDPOINT_HTTP 未设置，跳过 HTTP 集成测试")
	}
	if err := dialEndpoint(endpoint); err != nil {
		t.Skipf("HTTP 端点 %s 不可达，跳过: %v", endpoint, err)
	}
	return endpoint
}

// dialEndpoint 对 endpoint 建立 TCP 连接探测可达性，可达时返回 nil。
// 返回 error 而不是 bool：跳过日志需带上 connection refused / i/o timeout 等具体原因。
// endpoint 支持完整 URL 与 host:port 两种格式，URL 未显式带端口时按 scheme 取默认端口。
func dialEndpoint(endpoint string) error {
	hostPort := endpoint
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		hostPort = u.Host
	}
	if _, _, err := net.SplitHostPort(hostPort); err != nil {
		// 无端口部分：按 scheme 补默认端口
		port := "80"
		if strings.HasPrefix(endpoint, "https://") {
			port = "443"
		}
		hostPort = net.JoinHostPort(hostPort, port)
	}

	conn, err := net.DialTimeout("tcp", hostPort, dialProbeTimeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

// runExportCycle 执行「初始化 → 创建 Span → Close 强制刷新 → 校验导出零错误」的完整导出周期，
// 并打印各阶段耗时读数。initFn 由用例决定走 InitWithOTLP 还是 InitWithOTLPBatch。
//
// 验证口径（如实标注）：errorCollector 采集的是**客户端侧**导出结果——
// OTLP exporter 返回成功即代表采集端已应答接受本次上报（含鉴权通过），
// 但「入库后可在控制台查询到」属服务端行为，不在本测试约定内。
func runExportCycle(t *testing.T, collector *errorCollector, initFn func() error, spanCount int) {
	t.Helper()

	startInit := time.Now()
	if err := initFn(); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	initCost := time.Since(startInit)

	tr := GetProvider().Tracer("tracer-test")
	startSpans := time.Now()
	for i := 0; i < spanCount; i++ {
		_, span := tr.Start(context.Background(), "test-span",
			oteltrace.WithAttributes(attribute.String("test.index", strconv.Itoa(i))),
		)
		span.SetAttributes(attribute.String("test.env", "integration"))
		time.Sleep(spanRecordSimulateWait)
		span.End()
	}
	spanCost := time.Since(startSpans)

	startClose := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), exportFlushTimeout)
	defer cancel()
	if err := Close(ctx); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	closeCost := time.Since(startClose)

	t.Logf("读数：init=%s，创建 %d 个 Span=%s，Close(强制刷新导出)=%s",
		initCost, spanCount, spanCost, closeCost)

	// 断言导出链路零错误：采集端应答接受（含鉴权通过）才算通过
	errs := collector.snapshot()
	if len(errs) == 0 {
		t.Logf("导出链路 0 错误：采集端已应答接受 %d 个 Span 的上报（客户端侧证据）", spanCount)
		return
	}
	if skip, reason := exportErrClassify(errs[0]); skip {
		t.Skipf("导出被归类为「%s」（共 %d 个错误），按部署/环境差异跳过，首个错误: %v", reason, len(errs), errs[0])
	}
	t.Fatalf("导出过程出现 %d 个错误（非鉴权/非网络类，视为真实缺陷），首个错误: %v", len(errs), errs[0])
}

// TestIntegration_OTLPExportGRPC 验证 gRPC 协议导出链路（InitWithOTLPBatch + 鉴权 metadata）。
// 覆盖场景：阿里云链路追踪 gRPC 端点（host:port）+ authentication 鉴权头部 + BatchConfig 零值（SDK 默认批次参数）。
func TestIntegration_OTLPExportGRPC(t *testing.T) {
	endpoint := requireGRPCEndpoint(t)
	headers := authHeaders()
	t.Log("=== gRPC OTLP 集成测试 ===")
	t.Logf("endpoint: %s, headers 键: %v", endpoint, headerKeys(headers))

	collector := &errorCollector{}
	runExportCycle(t, collector, func() error {
		return InitWithOTLPBatch(
			"tracer-test", "test", "v0.0.1-test",
			1.0,
			endpoint,
			BatchConfig{}, // 与调用方常见配置一致：零值 = SDK 默认批次参数
			WithInsecure(true),
			WithHeaders(headers),
			WithTimeout(exportTimeoutSeconds*time.Second),
			WithErrorHandler(collector),
		)
	}, 3)
}

// TestIntegration_OTLPExportHTTP 验证 HTTP 协议导出链路（InitWithOTLP）。
// 覆盖场景：完整 URL（token 嵌在路径中）+ 可选 Authorization 头。
func TestIntegration_OTLPExportHTTP(t *testing.T) {
	endpoint := requireHTTPEndpoint(t)
	headers := authHeaders()
	t.Log("=== HTTP OTLP 集成测试 ===")
	t.Logf("endpoint: %s, headers 键: %v", endpoint, headerKeys(headers))

	collector := &errorCollector{}
	runExportCycle(t, collector, func() error {
		opts := []OTLPOption{
			WithInsecure(true),
			WithTimeout(exportTimeoutSeconds * time.Second),
			WithErrorHandler(collector),
		}
		if len(headers) > 0 {
			opts = append(opts, WithHeaders(headers))
		}
		return InitWithOTLP(
			"tracer-test", "test", "v0.0.1-test",
			1.0,
			endpoint,
			opts...,
		)
	}, 3)
}

// headerKeys 返回头部键列表（仅键，不打印值，避免把鉴权 token 打进测试日志）。
func headerKeys(headers map[string]string) []string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	return keys
}

// 确保 errorCollector 满足 otel.ErrorHandler 接口（编译期校验）。
var _ otel.ErrorHandler = (*errorCollector)(nil)
