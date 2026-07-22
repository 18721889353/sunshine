// Package interceptor provides commonly used grpc client-side and server-side interceptors,
// including authentication, logging, rate limiting, circuit breaking, and metrics collection.
//
// 本文件实现了基于阿里 Sentinel 的限流和熔断拦截器，支持服务端/客户端的一元和流式调用。
package interceptor

import (
	"context"
	"sync"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/18721889353/sunshine/pkg/errcode"
)

var (
	resourceName = "default"
	sentinelOnce sync.Once
)

// SentinelResourceExtractor 资源提取函数类型，根据方法名返回 Sentinel 资源名称。
type SentinelResourceExtractor func(method string) string

// defaultResourceExtractor 默认资源提取函数，方法名不为空时直接返回，否则返回默认资源名。
func defaultResourceExtractor(method string) string {
	if method != "" {
		return method
	}
	return resourceName
}

// SentinelOption 设置 sentinel 选项的函数类型。
type SentinelOption func(*sentinelOptions)

type sentinelOptions struct {
	resourceExtractor SentinelResourceExtractor
	flowRules         []*flow.Rule
	breakerRules      []*circuitbreaker.Rule
}

func (o *sentinelOptions) apply(opts ...SentinelOption) {
	for _, opt := range opts {
		opt(o)
	}
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

// WithSentinelResourceExtractor 设置资源提取器。
//
// 参数:
//   - fn: 自定义资源提取函数，根据 gRPC 方法名返回 Sentinel 资源名称。
//
// 返回值:
//   - SentinelOption: Sentinel 配置选项。
func WithSentinelResourceExtractor(fn SentinelResourceExtractor) SentinelOption {
	return func(o *sentinelOptions) {
		if fn != nil {
			o.resourceExtractor = fn
		}
	}
}

// WithSentinelFlowRules 直接设置限流规则。
//
// 参数:
//   - rules: Sentinel 限流规则列表，每个规则包含资源名、限流策略、阈值等。
//
// 返回值:
//   - SentinelOption: Sentinel 配置选项。
func WithSentinelFlowRules(rules []*flow.Rule) SentinelOption {
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
//   - SentinelOption: Sentinel 配置选项。
func WithSentinelCircuitBreakerRules(rules []*circuitbreaker.Rule) SentinelOption {
	return func(o *sentinelOptions) {
		if len(rules) > 0 {
			o.breakerRules = rules
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

// WithValidCode 不再生效, Sentinel 通过规则配置熔断条件。
// Deprecated: 请直接调用熔断拦截器, 无需额外配置。
func WithValidCode(_ ...codes.Code) SentinelOption {
	return func(_ *sentinelOptions) {}
}

// WithUnaryServerDegradeHandler 不再生效, Sentinel 通过规则配置降级策略。
// Deprecated: 请直接调用熔断拦截器, 无需额外配置。
func WithUnaryServerDegradeHandler(_ func(ctx context.Context, req interface{}) (reply interface{}, err error)) SentinelOption {
	return func(_ *sentinelOptions) {}
}

// WithGroup 不再需要, Sentinel 使用资源名称隔离。
// Deprecated: 请直接调用熔断拦截器, 无需额外配置。
func WithGroup(_ interface{}) SentinelOption {
	return func(_ *sentinelOptions) {}
}

// ErrNotAllowed 熔断错误（保持向后兼容）
var ErrNotAllowed = errcode.StatusServiceUnavailable.ToRPCErr("circuit breaker triggered")

// ErrLimitExceed 限流错误（保持向后兼容）
var ErrLimitExceed = errcode.StatusLimitExceed.ToRPCErr("rate limit exceeded")

// ========================== 限流拦截器 (Flow) ==========================

// UnaryServerRateLimit 服务端一元限流拦截器。
//
// 在请求处理前通过 Sentinel Entry 检查 QPS 是否超限，
// 超限时返回 StatusLimitExceed 错误，正常则放行到 handler。
//
// 参数:
//   - opts: 可选，Sentinel 配置选项，如 WithSentinelFlowRules、WithSentinelResourceExtractor。
//
// 返回值:
//   - grpc.UnaryServerInterceptor: 一元服务端拦截器。
func UnaryServerRateLimit(opts ...SentinelOption) grpc.UnaryServerInterceptor {
	o := defaultSentinelOptions()
	o.apply(opts...)
	initSentinel(o.flowRules, nil)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		resource := o.resourceExtractor(info.FullMethod)
		entry, e := sentinel.Entry(resource)
		if e != nil {
			return nil, errcode.StatusLimitExceed.ToRPCErr("rate limit exceeded")
		}
		defer entry.Exit()

		return handler(ctx, req)
	}
}

// StreamServerRateLimit 服务端流式限流拦截器。
//
// 在流式请求处理前通过 Sentinel Entry 检查 QPS 是否超限，
// 超限时返回 StatusLimitExceed 错误，正常则放行到 handler。
//
// 参数:
//   - opts: 可选，Sentinel 配置选项，如 WithSentinelFlowRules、WithSentinelResourceExtractor。
//
// 返回值:
//   - grpc.StreamServerInterceptor: 流式服务端拦截器。
func StreamServerRateLimit(opts ...SentinelOption) grpc.StreamServerInterceptor {
	o := defaultSentinelOptions()
	o.apply(opts...)
	initSentinel(o.flowRules, nil)

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		resource := o.resourceExtractor(info.FullMethod)
		entry, err := sentinel.Entry(resource)
		if err != nil {
			return errcode.StatusLimitExceed.ToRPCErr("rate limit exceeded")
		}
		defer entry.Exit()

		return handler(srv, ss)
	}
}

// ========================== 熔断拦截器 (CircuitBreaker) ==========================

// UnaryServerCircuitBreaker 服务端一元熔断拦截器。
//
// 在请求处理前通过 Sentinel Entry 检查断路器状态，
// 断路器打开时返回 StatusServiceUnavailable 错误。
// 当 handler 返回 Internal 或 Unavailable 状态码时，标记为失败以触发熔断。
//
// 参数:
//   - opts: 可选，Sentinel 配置选项，如 WithSentinelCircuitBreakerRules、WithSentinelResourceExtractor。
//
// 返回值:
//   - grpc.UnaryServerInterceptor: 一元服务端拦截器。
func UnaryServerCircuitBreaker(opts ...SentinelOption) grpc.UnaryServerInterceptor {
	o := defaultSentinelOptions()
	o.apply(opts...)
	initSentinel(nil, o.breakerRules)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		resource := o.resourceExtractor(info.FullMethod)
		entry, e := sentinel.Entry(resource)
		if e != nil {
			return nil, errcode.StatusServiceUnavailable.ToRPCErr("circuit breaker triggered")
		}

		reply, err := handler(ctx, req)
		if err != nil {
			if s, ok := status.FromError(err); ok {
				code := s.Code()
				if code == codes.Internal || code == codes.Unavailable {
					entry.SetError(err)
				}
			}
		}
		entry.Exit()
		return reply, err
	}
}

// StreamServerCircuitBreaker 服务端流式熔断拦截器。
//
// 在流式请求处理前通过 Sentinel Entry 检查断路器状态，
// 断路器打开时返回 StatusServiceUnavailable 错误。
//
// 参数:
//   - opts: 可选，Sentinel 配置选项，如 WithSentinelCircuitBreakerRules、WithSentinelResourceExtractor。
//
// 返回值:
//   - grpc.StreamServerInterceptor: 流式服务端拦截器。
func StreamServerCircuitBreaker(opts ...SentinelOption) grpc.StreamServerInterceptor {
	o := defaultSentinelOptions()
	o.apply(opts...)
	initSentinel(nil, o.breakerRules)

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		resource := o.resourceExtractor(info.FullMethod)
		entry, err := sentinel.Entry(resource)
		if err != nil {
			return errcode.StatusServiceUnavailable.ToRPCErr("circuit breaker triggered")
		}
		defer entry.Exit()

		return handler(srv, ss)
	}
}

// UnaryClientCircuitBreaker 客户端一元熔断拦截器。
//
// 在发起 gRPC 调用前通过 Sentinel Entry 检查断路器状态，
// 断路器打开时返回 StatusServiceUnavailable 错误。
// 当 gRPC 调用返回 Internal 或 Unavailable 状态码时，标记为失败以触发熔断。
//
// 参数:
//   - opts: 可选，Sentinel 配置选项，如 WithSentinelCircuitBreakerRules、WithSentinelResourceExtractor。
//
// 返回值:
//   - grpc.UnaryClientInterceptor: 一元客户端拦截器。
func UnaryClientCircuitBreaker(opts ...SentinelOption) grpc.UnaryClientInterceptor {
	o := defaultSentinelOptions()
	o.apply(opts...)
	initSentinel(nil, o.breakerRules)

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		resource := o.resourceExtractor(method)
		entry, err := sentinel.Entry(resource)
		if err != nil {
			return errcode.StatusServiceUnavailable.ToRPCErr("circuit breaker triggered")
		}

		callErr := invoker(ctx, method, req, reply, cc, opts...)
		if callErr != nil {
			if s, ok := status.FromError(callErr); ok {
				code := s.Code()
				if code == codes.Internal || code == codes.Unavailable {
					entry.SetError(callErr)
				}
			}
		}
		entry.Exit()
		return callErr
	}
}

// StreamClientCircuitBreaker 客户端流式熔断拦截器。
//
// 在发起流式 gRPC 调用前通过 Sentinel Entry 检查断路器状态，
// 断路器打开时返回 StatusServiceUnavailable 错误。
//
// 参数:
//   - opts: 可选，Sentinel 配置选项，如 WithSentinelCircuitBreakerRules、WithSentinelResourceExtractor。
//
// 返回值:
//   - grpc.StreamClientInterceptor: 流式客户端拦截器。
func StreamClientCircuitBreaker(opts ...SentinelOption) grpc.StreamClientInterceptor {
	o := defaultSentinelOptions()
	o.apply(opts...)
	initSentinel(nil, o.breakerRules)

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		resource := o.resourceExtractor(method)
		entry, err := sentinel.Entry(resource)
		if err != nil {
			return nil, errcode.StatusServiceUnavailable.ToRPCErr("circuit breaker triggered")
		}
		defer entry.Exit()

		return streamer(ctx, desc, cc, method, opts...)
	}
}
