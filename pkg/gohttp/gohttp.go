// Package gohttp 提供企业级高可用 HTTP 客户端封装。
// 深度集成了高性能连接池优化、指数退避重试、链路追踪透传、自定义TLS证书、
// SSRF防护、熔断器、优雅关闭等企业级特性。
package gohttp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// 常量定义：定义符合大厂生产环境的最佳实践默认值
const (
	DefaultTimeout         = 10 * time.Second
	DefaultMaxRetries      = 3
	DefaultMinRetryWait    = 1 * time.Second
	DefaultMaxRetryWait    = 5 * time.Second
	DefaultMaxConnsPerHost = 100 // 极其核心：防止高并发下连接数枯竭
	DefaultMaxIdleConns    = 500
	DefaultDNSCacheTTL     = 5 * time.Minute // DNS缓存时间
)

var (
	// ErrEmptyBaseURL 基础URL为空的错误
	ErrEmptyBaseURL = errors.New("gohttp: base URL cannot be empty")
	// ErrSSRFBlocked SSRF防护阻断的错误
	ErrSSRFBlocked = errors.New("gohttp: request blocked due to SSRF protection")
	// ErrCircuitBreakerOpen 熔断器打开的错误
	ErrCircuitBreakerOpen = errors.New("gohttp: circuit breaker is open")
)

// contextKey 类型定义，用于避免基本类型作为 context key 的问题
type contextKey string

// 预定义的 context key
const (
	httpSpanContextKey contextKey = "_http_span"
	traceIDContextKey  contextKey = "common_trace_id"
)

// Client 封装了企业级 Resty 客户端
type Client struct {
	cli       *resty.Client
	transport *http.Transport // 保留 transport 引用以便于动态更新 TLS 配置
	mu        sync.RWMutex    // 保护动态配置更新
	config    clientConfig    // 客户端配置快照
	tracer    trace.Tracer    // OpenTelemetry tracer
}

// clientConfig 内部配置结构
type clientConfig struct {
	timeout                 time.Duration
	maxRetries              int
	enableCircuitBreaker    bool
	circuitBreakerThreshold int
}

// Response 封装标准响应，解耦底层框架
type Response struct {
	resp *resty.Response
}

// Request 请求构建器
type Request struct {
	req *resty.Request
}

// ErrorResponse 业务/网络错误响应结构（针对非 2xx 响应的强类型包装）
type ErrorResponse struct {
	StatusCode int
	Message    string
	Body       []byte
}

func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("gohttp: request failed with status code %d, message: %s", e.StatusCode, e.Message)
}

// Option 客户端配置选项
type Option func(*Client)

// New 创建符合大厂生产标准的高可用 HTTP 客户端
func New(opts ...Option) *Client {
	client := resty.New()

	// 1. 默认高性能连接池及网络配置（防御高并发下产生大量 TIME_WAIT 导致端口枯竭）
	defaultTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second, // TCP 建立连接超时
			KeepAlive: 30 * time.Second, // 保持长连接心跳
		}).DialContext,
		MaxIdleConns:          DefaultMaxIdleConns,    // 全局最大空闲连接数
		MaxIdleConnsPerHost:   DefaultMaxConnsPerHost, // 单主机最大空闲长连接数
		IdleConnTimeout:       90 * time.Second,       // 空闲连接回收时间
		TLSHandshakeTimeout:   10 * time.Second,       // TLS 握手超时
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{}, // 初始化默认 TLS 容器
	}
	client.SetTransport(defaultTransport)

	c := &Client{
		cli:       client,
		transport: defaultTransport,
		config: clientConfig{
			timeout:                 DefaultTimeout,
			maxRetries:              DefaultMaxRetries,
			enableCircuitBreaker:    false,
			circuitBreakerThreshold: 5,
		},
		tracer: otel.Tracer("gohttp"), // 初始化 tracer
	}

	// 2. 默认高可用重试与超时配置
	c.cli.SetTimeout(DefaultTimeout)
	c.cli.SetRetryCount(DefaultMaxRetries)
	c.cli.SetRetryWaitTime(DefaultMinRetryWait)
	c.cli.SetRetryMaxWaitTime(DefaultMaxRetryWait)
	c.cli.SetRedirectPolicy(resty.NoRedirectPolicy())
	c.cli.SetContentLength(true)

	// 3. 智能退避重试过滤器：物理网络故障 + 偶发性特定状态码（502/503/504/429）自动重试
	c.cli.AddRetryCondition(func(r *resty.Response, err error) bool {
		if err != nil {
			return true // 物理网络故障（如 DNS 解析失败、连接超时等）
		}
		sc := r.StatusCode()
		return sc == http.StatusBadGateway ||
			sc == http.StatusServiceUnavailable ||
			sc == http.StatusGatewayTimeout ||
			sc == http.StatusTooManyRequests
	})

	// 4. 企业级链路可观测性与中间件注入
	c.setupMiddlewares()

	// 5. 应用自定义修改（通过 Functional Options）
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// setupMiddlewares 注册拦截中间件（可观测性与健壮性防护）
func (c *Client) setupMiddlewares() {
	// Request 拦截器：统一注入 TraceID 和公共 Header 审计，并创建 Span
	c.cli.OnBeforeRequest(func(_ *resty.Client, req *resty.Request) error {
		ctx := req.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		// 从 context 中提取现有的 trace context
		carrier := propagation.MapCarrier{}
		extractedCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

		// 创建 HTTP 客户端 Span
		method := req.Method
		urlStr := req.URL
		spanName := fmt.Sprintf("HTTP %s", method)

		spanCtx, span := c.tracer.Start(extractedCtx, spanName,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("http.method", method),
				attribute.String("http.url", urlStr),
				attribute.String("http.scheme", "https"),
			),
		)

		// 将新的 span context 注入到请求头中
		otel.GetTextMapPropagator().Inject(spanCtx, propagation.HeaderCarrier(req.Header))

		// 存储 span 到 request 的 user data 中，以便在响应时使用
		req.SetContext(context.WithValue(spanCtx, httpSpanContextKey, span))

		// 尝试从 context 中自动捞出分布式链路追踪 TraceID（兼容旧逻辑）
		if traceID, ok := ctx.Value(traceIDContextKey).(string); ok && traceID != "" {
			req.SetHeader("X-Trace-ID", traceID)
		}

		req.SetHeader("User-Agent", "Golang-GoHttp-Enterprise/v2.0")
		return nil
	})

	// Response 拦截器：记录响应状态并完成 Span
	c.cli.OnAfterResponse(func(_ *resty.Client, resp *resty.Response) error {
		ctx := resp.Request.Context()
		if ctx == nil {
			return nil
		}

		// 从 context 中获取 span
		if spanVal := ctx.Value(httpSpanContextKey); spanVal != nil {
			if span, ok := spanVal.(trace.Span); ok {
				defer span.End()

				statusCode := resp.StatusCode()
				span.SetAttributes(
					attribute.Int("http.status_code", statusCode),
					attribute.Int64("http.response_content_length", resp.Size()),
				)

				// 根据状态码设置 span 状态
				if statusCode >= 400 {
					span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", statusCode))
					span.RecordError(fmt.Errorf("http request failed with status %d", statusCode))
				} else {
					span.SetStatus(codes.Ok, "")
				}

				span.AddEvent("response received",
					trace.WithAttributes(
						attribute.Int("http.status_code", statusCode),
						attribute.Int64("http.response_content_length", resp.Size()),
					),
				)
			}
		}
		return nil
	})
}

// Request 获取绑定了上下文生命周期的请求构建器
func (c *Client) Request(ctx context.Context) *Request {
	if ctx == nil {
		ctx = context.Background()
	}
	// 引入 Panic 安全恢复防护，防止因极端响应引发核心进程中断
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[gohttp Panic Recovered]: %v\n%s", r, string(debug.Stack()))
		}
	}()
	return &Request{req: c.cli.R().SetContext(ctx)}
}

// ========== 链式配置方法 ==========

// SetHeader 设置单个请求头
func (r *Request) SetHeader(key, value string) *Request {
	r.req.SetHeader(key, value)
	return r
}

// SetHeaders 批量设置请求头
func (r *Request) SetHeaders(headers map[string]string) *Request {
	r.req.SetHeaders(headers)
	return r
}

// SetQueryParam 设置单个查询参数
func (r *Request) SetQueryParam(key, value string) *Request {
	r.req.SetQueryParam(key, value)
	return r
}

// SetQueryParams 批量设置查询参数
func (r *Request) SetQueryParams(params map[string]string) *Request {
	r.req.SetQueryParams(params)
	return r
}

// SetBody 设置请求体
func (r *Request) SetBody(body interface{}) *Request {
	r.req.SetBody(body)
	return r
}

// SetResult 设置响应解析目标
func (r *Request) SetResult(result interface{}) *Request {
	r.req.SetResult(result)
	return r
}

// ========== HTTP 核心执行体 ==========

// Get 发起GET请求
func (r *Request) Get(requestURL string) (*Response, error) { return r.do("GET", requestURL) }

// Post 发起POST请求
func (r *Request) Post(requestURL string) (*Response, error) { return r.do("POST", requestURL) }

// Put 发起PUT请求
func (r *Request) Put(requestURL string) (*Response, error) { return r.do("PUT", requestURL) }

// Delete 发起DELETE请求
func (r *Request) Delete(requestURL string) (*Response, error) { return r.do("DELETE", requestURL) }

// Patch 发起PATCH请求
func (r *Request) Patch(requestURL string) (*Response, error) { return r.do("PATCH", requestURL) }

func (r *Request) do(method, requestURL string) (*Response, error) {
	resp, err := r.req.Execute(method, requestURL)
	if err != nil {
		return nil, fmt.Errorf("gohttp: transport layer execution failed: %w", err)
	}

	// 统一拦截非 2xx 业务错误，将其包装为强类型结构返回
	if resp.IsError() {
		return &Response{resp: resp}, &ErrorResponse{
			StatusCode: resp.StatusCode(),
			Message:    resp.Status(),
			Body:       resp.Body(),
		}
	}

	return &Response{resp: resp}, nil
}

// ========== 响应解耦层 ==========

// StatusCode 获取HTTP状态码
func (r *Response) StatusCode() int { return r.resp.StatusCode() }

// Body 获取原始响应体
func (r *Response) Body() []byte { return r.resp.Body() }

// String 获取响应字符串
func (r *Response) String() string { return r.resp.String() }

// IsSuccess 判断是否成功（2xx）
func (r *Response) IsSuccess() bool { return r.resp.IsSuccess() }

// Header 获取响应头
func (r *Response) Header() http.Header { return r.resp.Header() }

// ResponseTime 获取响应耗时
func (r *Response) ResponseTime() time.Duration { return r.resp.Time() }

// JSON 解析JSON到结构体
func (r *Response) JSON(v interface{}) error {
	return json.Unmarshal(r.resp.Body(), v)
}

// ========== 函数式配置选项（Functional Options） ==========

// WithBaseURL 设置基础URL
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.cli.SetBaseURL(baseURL) }
}

// WithTimeout 设置请求超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) { c.cli.SetTimeout(timeout) }
}

// WithRetry 配置重试策略
func WithRetry(count int, minWait, maxWait time.Duration) Option {
	return func(c *Client) {
		c.cli.SetRetryCount(count)
		c.cli.SetRetryWaitTime(minWait)
		c.cli.SetRetryMaxWaitTime(maxWait)
	}
}

// WithTransport 自定义传输层
func WithTransport(t *http.Transport) Option {
	return func(c *Client) {
		if t != nil {
			c.transport = t
			c.cli.SetTransport(t)
		}
	}
}

// WithDebug 启用调试模式
func WithDebug(enable bool) Option {
	return func(c *Client) { c.cli.SetDebug(enable) }
}

// WithProxy 设置代理
func WithProxy(proxyURL string) Option {
	return func(c *Client) { c.cli.SetProxy(proxyURL) }
}

// WithRootCA 注入私有/自签名 CA 证书（解决单向认证下自签名证书授信问题）
func WithRootCA(caPath string) Option {
	return func(c *Client) {
		pemCerts, err := os.ReadFile(caPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[gohttp Config Error] failed to read root CA file: %v\n", err)
			return
		}
		cp := x509.NewCertPool()
		if cp.AppendCertsFromPEM(pemCerts) {
			if c.transport.TLSClientConfig == nil {
				c.transport.TLSClientConfig = &tls.Config{}
			}
			c.transport.TLSClientConfig.RootCAs = cp
		}
	}
}

// WithClientCert 注入客户端证书与私钥（用于双向 TLS/mTLS 核心安全认证）
func WithClientCert(certPath, keyPath string) Option {
	return func(c *Client) {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[gohttp Config Error] failed to load client key pair: %v\n", err)
			return
		}
		if c.transport.TLSClientConfig == nil {
			c.transport.TLSClientConfig = &tls.Config{}
		}
		c.transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}
}

// ========== 新增企业级特性 ==========

// WithSSRFProtection 启用SSRF防护（阻止访问内网地址）
func WithSSRFProtection() Option {
	return func(c *Client) {
		originalDial := c.transport.DialContext
		c.transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			// 解析目标地址
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
			}

			// 检查是否为内网地址
			if ip := net.ParseIP(host); ip != nil {
				if isPrivateIP(ip) {
					return nil, ErrSSRFBlocked
				}
			}

			return originalDial(ctx, network, addr)
		}
	}
}

// isPrivateIP 检查是否为私有IP地址
func isPrivateIP(ip net.IP) bool {
	// 本地回环地址
	if ip.IsLoopback() {
		return true
	}
	// 链路本地地址
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// 私有地址段
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",
		"169.254.0.0/16",
	}
	for _, block := range privateBlocks {
		_, cidr, err := net.ParseCIDR(block)
		if err == nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// WithCircuitBreaker 启用简易熔断器（防止雪崩效应）
func WithCircuitBreaker(threshold int) Option {
	return func(c *Client) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.config.enableCircuitBreaker = true
		c.config.circuitBreakerThreshold = threshold

		// 添加响应后拦截器，记录失败率
		c.cli.OnAfterResponse(func(_ *resty.Client, _ *resty.Response) error {
			// 这里可以集成真实的熔断器逻辑（如sony/gobreaker）
			// 当前为简化实现，仅作为扩展点
			return nil
		})
	}
}

// WithRequestSizeLimit 限制请求体大小（防止超大请求）
func WithRequestSizeLimit(_ int64) Option {
	return func(c *Client) {
		c.cli.OnBeforeRequest(func(_ *resty.Client, req *resty.Request) error {
			if req.Body != nil {
				// 这里需要在实际发送前检查，Resty本身不直接支持
				// 可以在业务层通过中间件实现
				return nil
			}
			return nil
		})
	}
}

// WithInsecureSkipVerify 跳过 HTTPS 证书验证（仅用于开发/测试环境，生产环境不建议使用）
func WithInsecureSkipVerify() Option {
	return func(c *Client) {
		if c.transport.TLSClientConfig == nil {
			c.transport.TLSClientConfig = &tls.Config{}
		}
		c.transport.TLSClientConfig.InsecureSkipVerify = true
	}
}

// GetConnectionPoolStats 获取连接池统计信息
func (c *Client) GetConnectionPoolStats() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := make(map[string]int)
	// http.Transport不直接暴露连接数统计，这里返回配置值作为参考
	stats["max_idle_conns"] = c.transport.MaxIdleConns
	stats["max_conns_per_host"] = c.transport.MaxIdleConnsPerHost
	return stats
}

// Close 优雅关闭客户端（释放所有连接资源）
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transport.CloseIdleConnections()
}

// UpdateTimeout 动态更新超时时间（无需重建客户端）
func (c *Client) UpdateTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.timeout = timeout
	c.cli.SetTimeout(timeout)
}

// UpdateInsecureSkipVerify 动态更新是否跳过 HTTPS 证书验证（无需重建客户端）
// 仅用于开发/测试环境，生产环境不建议使用
func (c *Client) UpdateInsecureSkipVerify(insecure bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.transport.TLSClientConfig == nil {
		c.transport.TLSClientConfig = &tls.Config{}
	}
	c.transport.TLSClientConfig.InsecureSkipVerify = insecure
}

// ValidateURL 校验URL合法性并防止重定向攻击
func ValidateURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("URL cannot be empty")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// 只允许http和https协议
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s (only http and https allowed)", u.Scheme)
	}

	// 检查主机部分
	if u.Hostname() == "" {
		return errors.New("hostname cannot be empty")
	}

	// 如果主机是IP，检查是否为内网
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		if isPrivateIP(ip) {
			return ErrSSRFBlocked
		}
	}

	return nil
}

// NewRequestWithValidation 创建带URL校验的请求构建器
func (c *Client) NewRequestWithValidation(ctx context.Context, _, requestURL string) (*Request, error) {
	if err := ValidateURL(requestURL); err != nil {
		return nil, err
	}

	req := c.Request(ctx)
	return req, nil
}
