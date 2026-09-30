package gohttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// Option 函数选项模式，用于设置 HTTP 客户端配置。
//
// 类型收敛为 func(*options)：选项只写入暂存结构 options，由 New 统一校验后再
// 应用到底层 resty 客户端。非法配置（负超时、nil transport、非法代理协议、
// 非法熔断阈值等）在构造期返回错误，而不是被 resty 静默接受后在运行期产生
// 不可预期行为——这与 pkg/goredis 的 Init+validate、pkg/nacoscli 的
// NewConfigClient 模式保持一致。
type Option func(*options)

// options 客户端配置暂存结构（仅在 New 构造阶段使用，不进入 Client 运行态）
type options struct {
	// 请求行为
	baseURL    string
	baseURLSet bool
	timeout    time.Duration
	timeoutSet bool
	retryCount int
	retryWait  time.Duration
	retryMax   time.Duration
	retrySet   bool

	// 传输层
	transport    *http.Transport
	transportSet bool
	debug        bool
	proxy        string
	proxySet     bool
	insecureSkip bool

	// TLS 证书（路径延迟到 New 阶段读取，IO 失败随构造错误返回）
	rootCAPath    string
	rootCASet     bool
	certPath      string
	keyPath       string
	clientCertSet bool

	// 企业级防护
	ssrf             bool
	breakerThreshold int
	breakerSet       bool
	sizeLimit        int64
	sizeLimitSet     bool

	// 可观测性（接口类型，SDK 实现与自定义实现均可注入；nil/typed nil 时回退全局 TracerProvider）
	tracerProvider oteltrace.TracerProvider
}

// defaultOptions 返回默认配置，取值与包级 Default* 常量一致
func defaultOptions() *options {
	return &options{
		timeout:    DefaultTimeout,
		retryCount: DefaultMaxRetries,
		retryWait:  DefaultMinRetryWait,
		retryMax:   DefaultMaxRetryWait,
	}
}

// apply 应用配置选项。
// nil Option 防御：跳过以避免 New(nil)、opts 切片含 nil 元素等场景 panic（与 nacoscli/goredis 一致）。
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
}

// validate 校验配置的合法性，构造期快速失败。
// 覆盖四类问题：空/非法 baseURL、越界数值（超时/重试/熔断阈值/大小上限）、
// 非法枚举（代理协议）、显式传入非法引用（nil transport）。
func (o *options) validate() error {
	if o.baseURLSet {
		if o.baseURL == "" {
			return ErrEmptyBaseURL
		}
		u, err := url.Parse(o.baseURL)
		if err != nil {
			return fmt.Errorf("gohttp: WithBaseURL 无法解析 %q: %w", o.baseURL, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("gohttp: WithBaseURL 仅支持 http/https 协议，当前为 %q", u.Scheme)
		}
		if u.Host == "" {
			return fmt.Errorf("gohttp: WithBaseURL 缺少主机名: %q", o.baseURL)
		}
	}

	if o.timeoutSet && o.timeout <= 0 {
		return fmt.Errorf("gohttp: WithTimeout 必须为正数，当前值 %s", o.timeout)
	}

	if o.retrySet {
		if o.retryCount < 0 {
			return fmt.Errorf("gohttp: WithRetry 的重试次数不可为负数，当前值 %d", o.retryCount)
		}
		if o.retryWait < 0 {
			return fmt.Errorf("gohttp: WithRetry 的最小等待时间不可为负数，当前值 %s", o.retryWait)
		}
		if o.retryMax < o.retryWait {
			return fmt.Errorf("gohttp: WithRetry 的最大等待时间(%s)不可小于最小等待时间(%s)", o.retryMax, o.retryWait)
		}
	}

	if o.transportSet && o.transport == nil {
		return errors.New("gohttp: WithTransport 不可传入 nil，不设置时使用内置高性能 Transport")
	}

	if o.proxySet && o.proxy != "" {
		u, err := url.Parse(o.proxy)
		if err != nil {
			return fmt.Errorf("gohttp: WithProxy 无法解析 %q: %w", o.proxy, err)
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("gohttp: WithProxy 仅支持 http/https/socks5/socks5h 协议，当前为 %q", u.Scheme)
		}
	}

	if o.breakerSet && o.breakerThreshold <= 0 {
		return fmt.Errorf("gohttp: WithCircuitBreaker 的失败阈值必须为正数，当前值 %d", o.breakerThreshold)
	}

	if o.sizeLimitSet && o.sizeLimit <= 0 {
		return fmt.Errorf("gohttp: WithRequestSizeLimit 的上限必须为正数，当前值 %d", o.sizeLimit)
	}

	return nil
}

// ========== 函数式配置选项（Functional Options） ==========

// WithBaseURL 设置基础URL
func WithBaseURL(baseURL string) Option {
	return func(o *options) {
		o.baseURL = baseURL
		o.baseURLSet = true
	}
}

// WithTimeout 设置请求超时时间（必须为正数）
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.timeout = timeout
		o.timeoutSet = true
	}
}

// WithRetry 配置重试策略（count >= 0，0 <= minWait <= maxWait）
func WithRetry(count int, minWait, maxWait time.Duration) Option {
	return func(o *options) {
		o.retryCount = count
		o.retryWait = minWait
		o.retryMax = maxWait
		o.retrySet = true
	}
}

// WithTransport 自定义传输层（nil 会在 New 阶段报错；传入的实例会被就地配置
// TLS/代理/SSRF 包装，不要与其他 Client 共享同一实例）
func WithTransport(t *http.Transport) Option {
	return func(o *options) {
		o.transport = t
		o.transportSet = true
	}
}

// WithDebug 启用调试模式（resty 会把请求头与响应体打印到标准输出，生产环境禁用）
func WithDebug(enable bool) Option {
	return func(o *options) { o.debug = enable }
}

// WithProxy 设置代理（空字符串表示不使用代理；仅支持 http/https/socks5/socks5h）
func WithProxy(proxyURL string) Option {
	return func(o *options) {
		o.proxy = proxyURL
		o.proxySet = true
	}
}

// WithRootCA 注入私有/自签名 CA 证书（解决单向认证下自签名证书授信问题）。
// 会与系统根证书池合并，不覆盖系统信任链；文件不存在或无可解析 PEM 时 New 返回错误。
func WithRootCA(caPath string) Option {
	return func(o *options) {
		o.rootCAPath = caPath
		o.rootCASet = true
	}
}

// WithClientCert 注入客户端证书与私钥（用于双向 TLS/mTLS 核心安全认证）。
// 加载失败（文件缺失/证书与私钥不匹配）时 New 返回错误。
func WithClientCert(certPath, keyPath string) Option {
	return func(o *options) {
		o.certPath = certPath
		o.keyPath = keyPath
		o.clientCertSet = true
	}
}

// WithSSRFProtection 启用SSRF防护（拦截内网地址访问）。
// 双层防护：IP 字面量在拨号时直接拦截；域名先完成解析、逐个校验解析结果，
// 全部为公网地址后才对已校验的 IP 拨号（避免解析与拨号之间的 DNS 重绑定窗口）。
func WithSSRFProtection() Option {
	return func(o *options) { o.ssrf = true }
}

// WithCircuitBreaker 启用熔断器（threshold 次连续失败后打开，冷却期后放行半开探测；
// threshold 必须为正数）
func WithCircuitBreaker(threshold int) Option {
	return func(o *options) {
		o.breakerThreshold = threshold
		o.breakerSet = true
	}
}

// WithRequestSizeLimit 限制请求体大小（超过上限的请求在发送前被拒绝，
// 返回错误并包裹 ErrRequestTooLarge；上限必须为正数）。
// 能拦截哪些类型见 checkSizeLimit 注释（单一事实源），流式 io.Reader 无法预知大小不拦截
func WithRequestSizeLimit(limit int64) Option {
	return func(o *options) {
		o.sizeLimit = limit
		o.sizeLimitSet = true
	}
}

// WithInsecureSkipVerify 跳过 HTTPS 证书验证（仅用于开发/测试环境，生产环境不建议使用）
func WithInsecureSkipVerify() Option {
	return func(o *options) { o.insecureSkip = true }
}

// WithTracerProvider 注入自定义 OpenTelemetry TracerProvider（接口类型，SDK 具体类型
// 与自定义实现均可注入；传入 nil 或 typed nil 时回退全局 TracerProvider，不启用自定义追踪）。
// 未注入时默认使用全局 otel.Tracer("gohttp")。
func WithTracerProvider(tp oteltrace.TracerProvider) Option {
	return func(o *options) { o.tracerProvider = tp }
}
