// Package grpccli 提供 gRPC 客户端的功能性配置选项。
//
// 配置项按功能分为以下几类：
//   - 连接安全：单向/双向 TLS 认证
//   - 请求功能：请求 ID、超时控制
//   - 可观测性：日志、监控指标、链路追踪
//   - 容错机制：重试、熔断器
//   - 服务发现与负载均衡
//   - 自定义：扩展拦截器、自定义拨号选项
package grpccli

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// 安全连接类型常量
var (
	secureOneWay = "one-way"  // 单向认证：客户端验证服务端证书
	secureTwoWay = "two-way"  // 双向认证：客户端和服务端互相验证
)

// Option gRPC 客户端配置选项函数类型
type Option func(*options)

// options gRPC 客户端内部配置结构体
type options struct {
	// 超时配置
	requestTimeout time.Duration // 请求超时时间（仅对一元调用有效）

	// 安全连接配置
	secureType string // 安全类型：""（明文）、"one-way"（单向）、"two-way"（双向）
	serverName string // 服务端名称（TLS 验证用）
	caFile     string // CA 证书文件路径（双向认证必填）
	certFile   string // 证书文件路径
	keyFile    string // 密钥文件路径

	// Token 认证配置
	enableToken bool   // 是否启用 Token 认证
	appID       string // 应用 ID
	appKey      string // 应用密钥

	// 拦截器功能开关
	enableLog            bool               // 是否启用 RPC 日志
	log                  *zap.Logger        // 日志记录器实例
	enableRequestID      bool               // 是否启用请求 ID 自动注入
	enableTrace          bool               // 是否启用链路追踪
	enableMetrics        bool               // 是否启用 Prometheus 监控指标
	enableRetry          bool               // 是否启用自动重试
	enableLoadBalance    bool               // 是否启用轮询负载均衡
	enableCircuitBreaker bool               // 是否启用熔断器（拦截器代码默认注释，需手动开启）
	discovery            registry.Discovery // 服务发现实例（非 nil 时启用服务发现模式）

	discoveryInsecure bool // 服务发现连接是否使用非安全传输（默认 true）

	// 自定义扩展
	dialOptions        []grpc.DialOption              // 自定义拨号选项
	unaryInterceptors  []grpc.UnaryClientInterceptor  // 自定义一元拦截器
	streamInterceptors []grpc.StreamClientInterceptor // 自定义流式拦截器
}

func defaultOptions() *options {
	return &options{
		serverName:        "localhost",
		discoveryInsecure: true,
	}
}

func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithTimeout 设置一元 RPC 请求超时时间。
// 当请求超过指定时间未返回时，客户端将返回 DeadlineExceeded 错误。
func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.requestTimeout = d
	}
}

// WithEnableRequestID 启用请求 ID 自动注入。
// 在每个 RPC 请求中自动生成或透传 request_id，用于日志关联和链路追踪。
func WithEnableRequestID() Option {
	return func(o *options) {
		o.enableRequestID = true
	}
}

// WithEnableLog 启用 RPC 请求日志记录。
// 参数 log 为 zap.Logger 实例；传 nil 时会自动创建 Production 级别的日志器。
func WithEnableLog(log *zap.Logger) Option {
	return func(o *options) {
		o.enableLog = true
		if log != nil {
			o.log = log
			return
		}
		var err error
		o.log, err = zap.NewProduction()
		if err != nil {
			fmt.Printf("创建 Production 日志器失败: %v\n", err)
		}
	}
}

// WithEnableTrace 启用 OpenTelemetry 链路追踪。
// 通过 gRPC StatsHandler 注入追踪信息，串联客户端和服务端调用链路。
func WithEnableTrace() Option {
	return func(o *options) {
		o.enableTrace = true
	}
}

// WithEnableMetrics 启用 Prometheus 监控指标收集。
// 记录 RPC 调用次数、耗时分布、错误率等指标，用于 Grafana 监控大盘。
func WithEnableMetrics() Option {
	return func(o *options) {
		o.enableMetrics = true
	}
}

// WithEnableLoadBalance 启用轮询负载均衡。
// 当配合服务发现使用时，将请求均匀分发到多个后端实例。
func WithEnableLoadBalance() Option {
	return func(o *options) {
		o.enableLoadBalance = true
	}
}

// WithEnableRetry 启用自动重试机制。
// 对可恢复的错误（如网络抖动、服务暂时不可用）自动重试，提高调用成功率。
func WithEnableRetry() Option {
	return func(o *options) {
		o.enableRetry = true
	}
}

// WithEnableCircuitBreaker 启用熔断器。
// 注意：熔断器拦截器的代码在 unaryClientOptions 和 streamClientOptions 中默认被注释，
// 取消注释后方可生效。当前保留此选项供后续使用。
func WithEnableCircuitBreaker() Option {
	return func(o *options) {
		o.enableCircuitBreaker = true
	}
}

// WithDiscoveryInsecure 设置服务发现连接是否使用非安全传输。
// 默认值为 true（非安全传输），设为 false 则使用加密传输。
func WithDiscoveryInsecure(b bool) Option {
	return func(o *options) {
		o.discoveryInsecure = b
	}
}

// isSecure 判断当前是否配置了安全连接。
func (o *options) isSecure() bool {
	if o.secureType == secureOneWay || o.secureType == secureTwoWay {
		return true
	}
	return false
}

// WithSecure 统一安全配置入口，根据 secureType 自动选择对应的安全配置方式。
//
// 参数:
//   - t: 安全类型，支持 ""（明文）、"one-way"（单向）、"two-way"（双向）
//   - 其余参数按安全类型传入，不使用的参数可传空字符串
func WithSecure(t string, serverName string, caFile string, certFile string, keyFile string) Option {
	switch t {
	case secureOneWay:
		return WithOneWaySecure(serverName, certFile)
	case secureTwoWay:
		return WithTwoWaySecure(serverName, caFile, certFile, keyFile)
	}

	// 未知类型：直接存储，由 secureOption() 处理为明文传输
	return func(o *options) {
		o.secureType = t
	}
}

// WithOneWaySecure 配置单向 TLS 认证（服务端认证）。
// 客户端通过服务端证书验证服务端身份，服务端不验证客户端。
func WithOneWaySecure(serverName string, certFile string) Option {
	return func(o *options) {
		if serverName == "" {
			serverName = "localhost"
		}
		o.secureType = secureOneWay
		o.serverName = serverName
		o.certFile = certFile
	}
}

// WithTwoWaySecure 配置双向 TLS 认证（mTLS）。
// 客户端和服务端互相验证对方的证书，适用于对安全要求较高的场景。
func WithTwoWaySecure(serverName string, caFile string, certFile string, keyFile string) Option {
	return func(o *options) {
		if serverName == "" {
			serverName = "localhost"
		}
		o.secureType = secureTwoWay
		o.serverName = serverName
		o.caFile = caFile
		o.certFile = certFile
		o.keyFile = keyFile
	}
}

// WithToken 配置 Token 认证。
// 在每个 RPC 请求头中注入 appID 和 appKey 用于服务端身份验证。
func WithToken(enable bool, appID string, appKey string) Option {
	return func(o *options) {
		o.enableToken = enable
		o.appID = appID
		o.appKey = appKey
	}
}

// WithDialOptions 添加自定义 gRPC 拨号选项。
// 用于注入框架未覆盖的 gRPC 原生 DialOption。
func WithDialOptions(dialOptions ...grpc.DialOption) Option {
	return func(o *options) {
		o.dialOptions = append(o.dialOptions, dialOptions...)
	}
}

// WithUnaryInterceptors 添加自定义一元拦截器。
// 自定义拦截器会追加到框架内置拦截器之后执行。
func WithUnaryInterceptors(unaryInterceptors ...grpc.UnaryClientInterceptor) Option {
	return func(o *options) {
		o.unaryInterceptors = append(o.unaryInterceptors, unaryInterceptors...)
	}
}

// WithStreamInterceptors 添加自定义流式拦截器。
// 自定义拦截器会追加到框架内置拦截器之后执行。
func WithStreamInterceptors(streamInterceptors ...grpc.StreamClientInterceptor) Option {
	return func(o *options) {
		o.streamInterceptors = append(o.streamInterceptors, streamInterceptors...)
	}
}

// WithDiscovery 设置服务发现实例。
// 启用后客户端将通过服务发现获取后端地址，而非直连固定的 endpoint。
func WithDiscovery(discovery registry.Discovery) Option {
	return func(o *options) {
		o.discovery = discovery
	}
}
