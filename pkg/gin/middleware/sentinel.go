// Package middleware provides gin middleware for request processing, including logging,
// authentication, rate limiting, circuit breaking, and metrics collection.
//
// 本文件实现了基于阿里 Sentinel 的限流和熔断中间件，支持通过配置驱动和独立断路器两种模式。
package middleware

import (
	"errors"
	"net/http"
	"sync"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	SentinelGin "github.com/alibaba/sentinel-golang/pkg/adapters/gin"
	"github.com/gin-gonic/gin"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
)

var (
	resourceName = "default"
	sentinelOnce sync.Once
)

// SentinelOptions 设置 Sentinel 选项的函数类型。
type SentinelOptions func(*sentinelOptions)

type sentinelOptions struct {
	resourceExtractor func(*gin.Context) string
	flowRules         []*flow.Rule
	breakerRules      []*circuitbreaker.Rule
}

// defaultSentinelOptions 创建默认的 Sentinel 选项配置。
//
// 包含一个默认的限流规则：资源名为 "default"，直接阈值策略，
// 拒绝行为，100 QPS，统计窗口 1 秒。
func defaultSentinelOptions() *sentinelOptions {
	return &sentinelOptions{
		resourceExtractor: defaultResourceExtractor,
		flowRules: []*flow.Rule{
			{
				Resource:               resourceName,
				TokenCalculateStrategy: flow.Direct,
				ControlBehavior:        flow.Reject,
				Threshold:              100,
				StatIntervalInMs:       1000,
			},
		},
	}
}

// defaultResourceExtractor 默认资源提取函数，始终返回默认资源名。
func defaultResourceExtractor(_ *gin.Context) string {
	return resourceName
}

// WithSentinelResourceExtractor 设置资源提取器。
//
// 参数:
//   - fn: 自定义资源提取函数，根据 Gin 上下文返回 Sentinel 资源名称。
//
// 返回值:
//   - SentinelOptions: Sentinel 配置选项。
func WithSentinelResourceExtractor(fn func(*gin.Context) string) SentinelOptions {
	return func(o *sentinelOptions) {
		if fn != nil {
			o.resourceExtractor = fn
		}
	}
}

// ParseTokenCalculateStrategy 解析限流策略字符串。
// 支持: direct（直接阈值）, warmUp（预热）
//
// 参数:
//   - s: 策略字符串。
//
// 返回值:
//   - flow.TokenCalculateStrategy: Sentinel 限流策略枚举。
func ParseTokenCalculateStrategy(s string) flow.TokenCalculateStrategy {
	switch s {
	case "warmUp":
		return flow.WarmUp
	default:
		return flow.Direct
	}
}

// ParseControlBehavior 解析流量效果字符串。
// 支持: reject（直接拒绝）, throttling（匀速排队）
//
// 参数:
//   - s: 效果字符串。
//
// 返回值:
//   - flow.ControlBehavior: Sentinel 流量效果枚举。
func ParseControlBehavior(s string) flow.ControlBehavior {
	switch s {
	case "throttling":
		return flow.Throttling
	default:
		return flow.Reject
	}
}

// ParseBreakerStrategy 解析熔断策略字符串。
// 支持: errorRatio（错误比例）, errorCount（错误计数）, slowRequestRatio（慢调用比例）
//
// 参数:
//   - s: 熔断策略字符串。
//
// 返回值:
//   - circuitbreaker.Strategy: Sentinel 熔断策略枚举。
func ParseBreakerStrategy(s string) circuitbreaker.Strategy {
	switch s {
	case "errorCount":
		return circuitbreaker.ErrorCount
	case "slowRequestRatio":
		return circuitbreaker.SlowRequestRatio
	default:
		return circuitbreaker.ErrorRatio
	}
}

// WithSentinelFlowRules 直接设置限流规则。
//
// 参数:
//   - rules: Sentinel 限流规则列表，每个规则包含资源名、限流策略、阈值等。
//
// 返回值:
//   - SentinelOptions: Sentinel 配置选项。
func WithSentinelFlowRules(rules []*flow.Rule) SentinelOptions {
	return func(o *sentinelOptions) {
		if len(rules) > 0 {
			o.flowRules = rules
		}
	}
}

// WithSentinelCircuitBreakerRules 直接设置熔断规则。
//
// 参数:
//   - rules: Sentinel 熔断规则列表，每个规则包含资源名、熔断策略、阈值等。
//
// 返回值:
//   - SentinelOptions: Sentinel 配置选项。
func WithSentinelCircuitBreakerRules(rules []*circuitbreaker.Rule) SentinelOptions {
	return func(o *sentinelOptions) {
		if len(rules) > 0 {
			o.breakerRules = rules
		}
	}
}

func (o *sentinelOptions) apply(opts ...SentinelOptions) {
	for _, opt := range opts {
		opt(o)
	}
}

// initSentinel 初始化 Sentinel 并加载限流和熔断规则。
//
// 使用 sync.Once 保证全局只初始化一次。每次调用都会尝试加载规则，
// 已存在的规则会被追加而非替换。
//
// 参数:
//   - flowRules: 限流规则列表，为空则跳过。
//   - breakerRules: 熔断规则列表，为空则跳过。
func initSentinel(flowRules []*flow.Rule, breakerRules []*circuitbreaker.Rule) {
	sentinelOnce.Do(func() {
		if err := sentinel.InitDefault(); err != nil {
			panic("初始化sentinel失败: " + err.Error())
		}
	})
	if len(flowRules) > 0 {
		if _, err := flow.LoadRules(flowRules); err != nil {
			panic("加载sentinel限流规则失败: " + err.Error())
		}
	}
	if len(breakerRules) > 0 {
		if _, err := circuitbreaker.LoadRules(breakerRules); err != nil {
			panic("加载sentinel熔断规则失败: " + err.Error())
		}
	}
}

// SentinelMiddleware 返回一个 Gin 中间件，提供限流和熔断保护。
//
// 初始化和加载限流及熔断规则，请求到达时通过 Sentinel Entry 检查，
// 被限流或熔断时返回 429 Too Many Requests 并终止请求链。
//
// 参数:
//   - opts: 可选，Sentinel 配置选项，如 WithSentinelFlowRules、WithSentinelCircuitBreakerRules。
//
// 返回值:
//   - gin.HandlerFunc: Gin 中间件处理函数。
func SentinelMiddleware(opts ...SentinelOptions) gin.HandlerFunc {
	o := defaultSentinelOptions()
	o.apply(opts...)

	initSentinel(o.flowRules, o.breakerRules)

	return SentinelGin.SentinelMiddleware(
		SentinelGin.WithResourceExtractor(o.resourceExtractor),
		SentinelGin.WithBlockFallback(func(ctx *gin.Context) {
			response.Out(ctx, errcode.TooManyRequests.WithDetails("请求被限流/熔断"))
			ctx.Abort()
		}),
	)
}

// ========================== 断路器中间件 (Circuit Breaker) ==========================

// ErrNotAllowed 断路器拒绝请求时返回的错误。
var ErrNotAllowed = errors.New("circuitbreaker: not allowed for circuit open")

// CircuitBreakerOption 设置断路器选项的函数类型。
type CircuitBreakerOption func(*circuitBreakerOptions)

type circuitBreakerOptions struct {
	degradeHandler func(c *gin.Context)
	rules          []*circuitbreaker.Rule
}

// defaultCircuitBreakerOptions 创建默认的断路器选项配置。
//
// 默认规则：错误比例策略（ErrorRatio），3 秒恢复探测，
// 最小请求数 10，10 秒统计窗口，50% 错误比例阈值。
func defaultCircuitBreakerOptions() *circuitBreakerOptions {
	return &circuitBreakerOptions{
		rules: []*circuitbreaker.Rule{
			{
				Resource:         resourceName,
				Strategy:         circuitbreaker.ErrorRatio,
				RetryTimeoutMs:   3000,
				MinRequestAmount: 10,
				StatIntervalMs:   10000,
				Threshold:        0.5,
			},
		},
	}
}

func (o *circuitBreakerOptions) apply(opts ...CircuitBreakerOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithCircuitBreakerRules 设置熔断规则。
//
// 参数:
//   - rules: Sentinel 熔断规则列表。
//
// 返回值:
//   - CircuitBreakerOption: 断路器配置选项。
func WithCircuitBreakerRules(rules []*circuitbreaker.Rule) CircuitBreakerOption {
	return func(o *circuitBreakerOptions) {
		if len(rules) > 0 {
			o.rules = rules
		}
	}
}

// WithDegradeHandler 设置熔断降级处理函数。
//
// 当断路器触发拒绝请求时，会调用此函数返回自定义响应。
//
// 参数:
//   - handler: 降级处理函数，接收 Gin 上下文。
//
// 返回值:
//   - CircuitBreakerOption: 断路器配置选项。
func WithDegradeHandler(handler func(c *gin.Context)) CircuitBreakerOption {
	return func(o *circuitBreakerOptions) {
		o.degradeHandler = handler
	}
}

// CircuitBreaker 返回一个断路器中间件，用于保护后端服务不被过载。
//
// 当错误比例超过阈值时，断路器打开并拒绝请求，支持自定义降级处理。
// 请求通过时继续执行后续 handler，完成后退出 Sentinel Entry。
//
// 参数:
//   - opts: 可选，断路器配置选项，如 WithCircuitBreakerRules、WithDegradeHandler。
//
// 返回值:
//   - gin.HandlerFunc: Gin 中间件处理函数。
func CircuitBreaker(opts ...CircuitBreakerOption) gin.HandlerFunc {
	o := defaultCircuitBreakerOptions()
	o.apply(opts...)

	initSentinel(nil, o.rules)

	return func(c *gin.Context) {
		resource := c.FullPath()
		entry, err := sentinel.Entry(resource)
		if err != nil {
			if o.degradeHandler != nil {
				o.degradeHandler(c)
			} else {
				response.Output(c, http.StatusServiceUnavailable, "circuit breaker triggered")
			}
			c.Abort()
			return
		}

		c.Next()
		entry.Exit()
	}
}
