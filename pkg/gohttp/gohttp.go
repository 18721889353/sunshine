// Package gohttp 提供企业级高可用 HTTP 客户端封装。
// 深度集成了高性能连接池优化、指数退避重试、链路追踪透传、自定义TLS证书、
// SSRF防护、熔断器、优雅关闭等企业级特性。
//
// 构造约定：New 在构造期完成全部配置校验与证书装配，非法配置返回错误
// （对齐 pkg/nacoscli NewConfigClient、pkg/goredis Init 的快速失败模式）。
package gohttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
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
	//
	// Deprecated: WithBaseURL("") 已改为合法输入（等价于不设置基础 URL，
	// 由每次请求传入完整地址），validate 不再返回本错误；保留仅为兼容
	// 既有 errors.Is(ErrEmptyBaseURL) 判断，后续大版本移除。
	ErrEmptyBaseURL = errors.New("gohttp: base URL cannot be empty")
	// ErrSSRFBlocked SSRF防护阻断的错误
	ErrSSRFBlocked = errors.New("gohttp: request blocked due to SSRF protection")
	// ErrCircuitBreakerOpen 熔断器打开的错误
	ErrCircuitBreakerOpen = errors.New("gohttp: circuit breaker is open")
	// ErrRequestTooLarge 请求体超过 WithRequestSizeLimit 上限的错误
	ErrRequestTooLarge = errors.New("gohttp: request body too large")
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
	breaker   *circuitBreaker // 熔断器（未启用时为 nil，方法 nil 安全）
	sizeLimit int64           // 请求体上限（未启用时为 0）
}

// clientConfig 内部配置结构（运行期可变的少量快照，受 mu 保护）
type clientConfig struct {
	timeout time.Duration
}

// Response 封装标准响应，解耦底层框架
type Response struct {
	resp *resty.Response
}

// Request 请求构建器
type Request struct {
	c   *Client
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

// New 创建符合大厂生产标准的高可用 HTTP 客户端。
// 配置非法（越界数值/非法协议/证书文件缺失等）时返回错误，此时不会返回可用客户端。
func New(opts ...Option) (*Client, error) {
	// 1. 解析并校验配置（构造期快速失败，杜绝非法值被 resty 静默接受）
	o := defaultOptions()
	o.apply(opts...)
	if err := o.validate(); err != nil {
		return nil, err
	}

	// 2. 默认高性能连接池及网络配置（防御高并发下产生大量 TIME_WAIT 导致端口枯竭）
	transport := &http.Transport{
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
	}
	if o.transportSet && o.transport != nil {
		transport = o.transport
	}

	// 3. TLS 配置装配（克隆后修改，避免污染调用方持有的 tls.Config）
	tlsCfg := &tls.Config{}
	if transport.TLSClientConfig != nil {
		// Clone 而非值拷贝：tls.Config 内含 sync.RWMutex，值拷贝会被 go vet copylocks 拒绝
		tlsCfg = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig = tlsCfg

	// 3.1 注入根证书：与系统根证书池**合并**（原实现直接赋值会丢弃系统信任链，
	// 导致注入自签 CA 后所有公网 HTTPS 证书校验失败）
	if o.rootCASet {
		pemCerts, err := os.ReadFile(o.rootCAPath)
		if err != nil {
			return nil, fmt.Errorf("gohttp: 读取根证书文件 %q 失败: %w", o.rootCAPath, err)
		}
		rootCAs, err := x509.SystemCertPool()
		if err != nil || rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}
		if !rootCAs.AppendCertsFromPEM(pemCerts) {
			return nil, fmt.Errorf("gohttp: 根证书文件 %q 中没有可解析的 PEM 证书", o.rootCAPath)
		}
		tlsCfg.RootCAs = rootCAs
	}

	// 3.2 注入客户端证书（mTLS）
	if o.clientCertSet {
		cert, err := tls.LoadX509KeyPair(o.certPath, o.keyPath)
		if err != nil {
			return nil, fmt.Errorf("gohttp: 加载客户端证书 %q 与私钥 %q 失败: %w", o.certPath, o.keyPath, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// 3.3 跳过证书验证（仅开发/测试环境）
	if o.insecureSkip {
		tlsCfg.InsecureSkipVerify = true
	}

	// 3.4 SSRF 双层防护（IP 字面量拨号拦截 + 域名解析结果校验）
	if o.ssrf {
		applySSRFProtection(transport)
	}

	// 4. 构建 resty 客户端
	restyClient := resty.New()
	restyClient.SetTransport(transport)
	restyClient.SetTimeout(o.timeout)
	restyClient.SetRetryCount(o.retryCount)
	restyClient.SetRetryWaitTime(o.retryWait)
	restyClient.SetRetryMaxWaitTime(o.retryMax)
	restyClient.SetRedirectPolicy(resty.NoRedirectPolicy())
	restyClient.SetContentLength(true)
	if o.baseURLSet {
		restyClient.SetBaseURL(o.baseURL)
	}
	if o.proxySet {
		restyClient.SetProxy(o.proxy) // 空字符串等价于清除代理
	}
	if o.debug {
		restyClient.SetDebug(true)
	}

	// 4.1 智能退避重试过滤器：物理网络故障 + 偶发性特定状态码（502/503/504/429）自动重试
	restyClient.AddRetryCondition(func(r *resty.Response, err error) bool {
		if err != nil {
			return true // 物理网络故障（如 DNS 解析失败、连接超时等）
		}
		sc := r.StatusCode()
		return sc == http.StatusBadGateway ||
			sc == http.StatusServiceUnavailable ||
			sc == http.StatusGatewayTimeout ||
			sc == http.StatusTooManyRequests
	})

	// 5. TracerProvider：优先使用注入的实现（接口，nil/typed nil 回退全局）
	tracer := otel.Tracer("gohttp")
	if isUsableTracerProvider(o.tracerProvider) {
		tracer = o.tracerProvider.Tracer("gohttp")
	}

	c := &Client{
		cli:       restyClient,
		transport: transport,
		config: clientConfig{
			timeout: o.timeout,
		},
		tracer:    tracer,
		sizeLimit: o.sizeLimit,
	}
	if o.breakerSet {
		c.breaker = newCircuitBreaker(o.breakerThreshold, defaultCircuitBreakerCooldown)
	}

	// 6. 企业级链路可观测性与中间件注入
	c.setupMiddlewares()

	return c, nil
}

// setupMiddlewares 注册拦截中间件（可观测性与健壮性防护，实现见 tracing.go）
func (c *Client) setupMiddlewares() {
	// Request 拦截器：创建 Span、注入传播头、执行熔断/大小守卫
	c.cli.OnBeforeRequest(c.onBeforeRequest)
	// Response 拦截器：记录响应状态（Span 的 End 统一由 do() 兜底收尾）
	c.cli.OnAfterResponse(c.onAfterResponse)
	// 重试钩子：为 transport 失败的中间 attempt 补记错误状态
	c.cli.AddRetryHook(c.onRetryAttempt)
}

// checkSizeLimit 校验请求体大小（发送前拦截，超过上限返回包裹 ErrRequestTooLarge 的错误）。
//
// 实现说明（历史缺陷修复）：resty 的**用户自定义** OnBeforeRequest 中间件先于其内置的
// parseRequestBody/createHTTPRequest 执行（见 resty client.go execute：udBeforeRequest
// 循环在 beforeRequest 循环之前），拦截点上 req.RawRequest 尚未创建——原实现读
// RawRequest.ContentLength 恒为 nil，上限从未生效。现按 resty Request.Body 的具体类型
// 估算字节数：nil/[]byte/string/*bytes.Buffer/*bytes.Reader/*strings.Reader 长度已知可比对；
// 其他类型（含 io.Reader 流式体、待序列化的 struct）无法预知，不拦截（边界与
// WithRequestSizeLimit 注释一致，单一事实源在本函数）。
func (c *Client) checkSizeLimit(req *resty.Request) error {
	if c.sizeLimit <= 0 || req == nil {
		return nil
	}
	size, known := bodySize(req.Body)
	if !known || size <= c.sizeLimit {
		return nil
	}
	return fmt.Errorf("gohttp: 请求体 %d 字节超过上限 %d 字节: %w", size, c.sizeLimit, ErrRequestTooLarge)
}

// bodySize 估算请求体字节数；第二个返回值表示长度是否可知
func bodySize(body interface{}) (int64, bool) {
	switch b := body.(type) {
	case nil:
		return 0, true
	case []byte:
		return int64(len(b)), true
	case string:
		return int64(len(b)), true
	case *bytes.Buffer:
		return int64(b.Len()), true
	case *bytes.Reader:
		return int64(b.Len()), true
	case *strings.Reader:
		return int64(b.Len()), true
	default:
		return 0, false
	}
}

// Request 获取绑定了上下文生命周期的请求构建器
func (c *Client) Request(ctx context.Context) *Request {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Request{c: c, req: c.cli.R().SetContext(ctx)}
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

// do HTTP 请求执行主体：负责 panic 恢复、错误脱敏、熔断计数与 Span 统一收尾。
//
// 收尾顺序（defer LIFO）：recover 先于 finish 执行，保证 panic 被转成错误后
// finish 仍能看到最终错误并把 Span 标记为 Error，不会留下永不 End 的 Span。
func (r *Request) do(method, requestURL string) (resp *Response, err error) {
	defer func() {
		r.finish(err)
	}()

	// Panic 安全恢复防护：resty 执行链上的 panic 转为错误返回，防止进程中断。
	// （原实现把 recover 放在 Request() 中，其函数体内几乎不会 panic，防护形同虚设）
	defer func() {
		if rec := recover(); rec != nil {
			resp = nil
			err = fmt.Errorf("gohttp: 请求执行期间发生 panic 已恢复: %v", rec)
			logger.ErrorWithCtx(r.req.Context(),
				fmt.Sprintf("gohttp: 请求执行期间发生 panic 已恢复: %v", rec),
				logger.String("stack", string(debug.Stack())))
		}
	}()

	restyResp, execErr := r.req.Execute(method, requestURL)
	if execErr != nil {
		// transport 层错误：URL 中的 userinfo 密码与敏感 query 参数脱敏后对外呈现，
		// 错误链通过 redactedError.Unwrap 保留（errors.Is/As 可穿透）
		return nil, &redactedError{
			err: execErr,
			msg: "gohttp: transport layer execution failed: " + redactURL(execErr.Error()),
		}
	}

	// 统一拦截非 2xx 业务错误，将其包装为强类型结构返回
	if restyResp.IsError() {
		return &Response{resp: restyResp}, &ErrorResponse{
			StatusCode: restyResp.StatusCode(),
			Message:    restyResp.Status(),
			Body:       restyResp.Body(),
		}
	}

	return &Response{resp: restyResp}, nil
}

// finish 一次请求的最终收尾：熔断计数 + 全部 Span 兜底 End。只由 do() 调用。
//
// 熔断计数口径（一次 do() 调用计一次，内部重试不拆分）：
//   - 本地拒绝（熔断打开/大小超限/调用方取消上下文）不计入，它们不代表下游故障
//   - 4xx（含 422/404/429）记为成功：下游可达，故障在调用方
//   - transport 错误与 5xx 记为失败
func (r *Request) finish(err error) {
	if r.c.breaker != nil && !isLocalRejection(err) {
		var httpErr *ErrorResponse
		switch {
		case err == nil:
			r.c.breaker.onSuccess()
		case errors.As(err, &httpErr) && httpErr.StatusCode < 500:
			r.c.breaker.onSuccess()
		default:
			r.c.breaker.onFailure()
		}
	}

	tracker, ok := r.req.Context().Value(httpSpanContextKey).(*trackedSpanList)
	if !ok || tracker == nil {
		return
	}
	if err != nil {
		tracker.finishAllError(err)
	} else {
		tracker.endAll()
	}
}

// isLocalRejection 判断错误是否属于「本端主动拒绝」而非下游故障
func isLocalRejection(err error) bool {
	return errors.Is(err, ErrCircuitBreakerOpen) ||
		errors.Is(err, ErrRequestTooLarge) ||
		errors.Is(err, context.Canceled)
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

// ========== 运行期管理 ==========

// GetConnectionPoolStats 获取连接池统计信息
// 并发安全：内部持有读锁（返回的是配置值快照，非实时连接数）
func (c *Client) GetConnectionPoolStats() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := make(map[string]int)
	// http.Transport不直接暴露连接数统计，这里返回配置值作为参考
	stats["max_idle_conns"] = c.transport.MaxIdleConns
	stats["max_conns_per_host"] = c.transport.MaxIdleConnsPerHost
	return stats
}

// Close 优雅关闭客户端（释放所有连接资源）。幂等，可重复调用。
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transport.CloseIdleConnections()
}

// UpdateTimeout 动态更新超时时间（无需重建客户端）。
// 并发约束：内部修改 resty 客户端字段，应在无在途请求时调用（如启动阶段或排空后），
// 与在途请求并发存在数据竞争。
func (c *Client) UpdateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.timeout = timeout
	c.cli.SetTimeout(timeout)
}

// UpdateInsecureSkipVerify 动态更新是否跳过 HTTPS 证书验证（无需重建客户端）。
// 仅用于开发/测试环境，生产环境不建议使用。
// 并发约束：直接修改 TLS 配置结构，应在无在途请求时调用，与在途请求并发存在数据竞争。
func (c *Client) UpdateInsecureSkipVerify(insecure bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.transport.TLSClientConfig == nil {
		c.transport.TLSClientConfig = &tls.Config{}
	}
	// Clone 后替换，避免修改共享的 tls.Config 结构体（内含锁，禁止值拷贝）
	clone := c.transport.TLSClientConfig.Clone()
	clone.InsecureSkipVerify = insecure
	c.transport.TLSClientConfig = clone
}

// NewRequestWithValidation 创建带URL校验的请求构建器。
// 注意：ValidateURL 只做字面量校验，域名指向内网需在 New 时启用 WithSSRFProtection。
func (c *Client) NewRequestWithValidation(ctx context.Context, _, requestURL string) (*Request, error) {
	if err := ValidateURL(requestURL); err != nil {
		return nil, err
	}

	req := c.Request(ctx)
	return req, nil
}
