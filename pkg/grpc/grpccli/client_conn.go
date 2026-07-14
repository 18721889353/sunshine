// Package grpccli 是 gRPC 客户端封装，支持服务发现、日志、负载均衡、链路追踪、
// 监控指标、重试和熔断器等功能。
package grpccli

import (
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/18721889353/sunshine/pkg/grpc/gtls"
	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
	"github.com/18721889353/sunshine/pkg/servicerd/discovery"
)

// NewClient 创建 gRPC 客户端连接，支持通过 Option 模式配置各项功能。
//
// 参数:
//   - endpoint: 目标地址。直连模式为 "host:port"；服务发现模式为 "discovery:///serviceName"
//   - opts: 可选配置项，如安全认证、日志、追踪等
//
// 返回值:
//   - *grpc.ClientConn: gRPC 客户端连接，使用完后需调用 Close() 释放
//   - error: 创建失败时返回错误信息
func NewClient(endpoint string, opts ...Option) (*grpc.ClientConn, error) {
	o := defaultOptions()
	o.apply(opts...)

	var clientOptions []grpc.DialOption

	// 服务发现：注册自定义 resolver，解析 endpoint 中的服务名为实际地址列表
	if o.discovery != nil {
		clientOptions = append(clientOptions, grpc.WithResolvers(
			discovery.NewBuilder(
				o.discovery,
				discovery.WithInsecure(o.discoveryInsecure),
			)))
	}

	// 负载均衡：通过 gRPC 内置 round_robin 策略轮询分发请求
	if o.enableLoadBalance {
		clientOptions = append(clientOptions, grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`))
	}

	// 安全连接：根据 secureType 选择单向/双向 TLS 认证或明文传输
	so, err := secureOption(o)
	if err != nil {
		return nil, err
	}
	clientOptions = append(clientOptions, so)

	// 链路追踪：添加 gRPC StatsHandler 用于 OpenTelemetry 追踪
	if o.enableTrace {
		statsHandler := interceptor.NewClientStatsHandler()
		clientOptions = append(clientOptions, grpc.WithStatsHandler(statsHandler))
	}

	// Token 认证：在请求头中添加应用凭证
	if o.enableToken {
		clientOptions = append(clientOptions, interceptor.ClientTokenOption(
			o.appID,
			o.appKey,
			o.isSecure(),
		))
	}

	// 一元拦截器链：panic 恢复、超时控制、请求 ID、日志、监控、重试等
	clientOptions = append(clientOptions, unaryClientOptions(o))
	// 流式拦截器链：panic 恢复、请求 ID、日志、监控、重试等
	clientOptions = append(clientOptions, streamClientOptions(o))
	// 自定义拨号选项：由调用方通过 WithDialOptions 注入
	clientOptions = append(clientOptions, o.dialOptions...)

	return grpc.NewClient(endpoint, clientOptions...)
}

// secureOption 根据安全类型配置传输层安全选项。
// 支持三种模式：""（明文）、"one-way"（服务端认证）、"two-way"（双向认证）。
func secureOption(o *options) (grpc.DialOption, error) {
	switch o.secureType {
	case secureOneWay: // 服务端单向认证：客户端验证服务端证书
		if o.certFile == "" {
			return nil, errors.New("证书文件路径为空")
		}
		credentials, err := gtls.GetClientTLSCredentials(o.serverName, o.certFile)
		if err != nil {
			return nil, err
		}
		return grpc.WithTransportCredentials(credentials), nil

	case secureTwoWay: // 双向认证：客户端和服务端互相验证证书
		if o.caFile == "" {
			return nil, errors.New("CA 证书文件路径为空")
		}
		if o.certFile == "" {
			return nil, errors.New("客户端证书文件路径为空")
		}
		if o.keyFile == "" {
			return nil, errors.New("客户端密钥文件路径为空")
		}
		credentials, err := gtls.GetClientTLSCredentialsByCA(
			o.serverName,
			o.caFile,
			o.certFile,
			o.keyFile,
		)
		if err != nil {
			return nil, err
		}
		return grpc.WithTransportCredentials(credentials), nil

	default: // 明文传输（不加密）
		return grpc.WithTransportCredentials(insecure.NewCredentials()), nil
	}
}

// unaryClientOptions 组装一元 gRPC 调用的客户端拦截器链。
// 按以下顺序组装：panic 恢复 → 超时控制 → 请求 ID → 日志 → 监控 → 重试 → 自定义。
func unaryClientOptions(o *options) grpc.DialOption {
	var unaryClientInterceptors []grpc.UnaryClientInterceptor

	// panic 恢复：防止单个 RPC 崩溃导致整个进程退出
	unaryClientInterceptors = append(unaryClientInterceptors, interceptor.UnaryClientRecovery())

	// 超时控制：为一元调用设置请求超时时间
	if o.requestTimeout > 0 {
		unaryClientInterceptors = append(unaryClientInterceptors, interceptor.UnaryClientTimeout(o.requestTimeout))
	}

	// 请求 ID 注入：自动生成或透传请求 ID，用于日志关联和链路追踪
	if o.enableRequestID {
		unaryClientInterceptors = append(unaryClientInterceptors, interceptor.UnaryClientRequestID())
	}

	// 日志记录：打印请求/响应的详细信息
	if o.enableLog {
		unaryClientInterceptors = append(unaryClientInterceptors, interceptor.UnaryClientLog())
	}

	// 监控指标：记录 RPC 调用次数、耗时、错误率等 Prometheus 指标
	if o.enableMetrics {
		unaryClientInterceptors = append(unaryClientInterceptors, interceptor.UnaryClientMetrics())
	}

	// 熔断器（默认关闭）：开启后对连续失败的 RPC 进行快速失败，保护下游
	// 取消下面注释即可启用，默认已包含 codes.Internal 和 codes.Unavailable
	//if o.enableCircuitBreaker {
	//	unaryClientInterceptors = append(unaryClientInterceptors, interceptor.UnaryClientCircuitBreaker(
	//	//interceptor.WithValidCode(codes.PermissionDenied),
	//	))
	//}

	// 重试机制：对可恢复错误自动重试，提高调用成功率
	if o.enableRetry {
		unaryClientInterceptors = append(unaryClientInterceptors, interceptor.UnaryClientRetry())
	}

	// 自定义拦截器：由调用方通过 WithUnaryInterceptors 注入
	unaryClientInterceptors = append(unaryClientInterceptors, o.unaryInterceptors...)

	return grpc.WithChainUnaryInterceptor(unaryClientInterceptors...)
}

// streamClientOptions 组装流式 gRPC 调用的客户端拦截器链。
// 按以下顺序组装：panic 恢复 → 请求 ID → 日志 → 监控 → 重试 → 自定义。
// 注意：流式调用不支持超时拦截器（超时由调用方通过 context 控制）。
func streamClientOptions(o *options) grpc.DialOption {
	var streamClientInterceptors []grpc.StreamClientInterceptor

	// panic 恢复：防止单个流 RPC 崩溃导致整个进程退出
	streamClientInterceptors = append(streamClientInterceptors, interceptor.StreamClientRecovery())

	// 请求 ID 注入
	if o.enableRequestID {
		streamClientInterceptors = append(streamClientInterceptors, interceptor.StreamClientRequestID())
	}

	// 日志记录
	if o.enableLog {
		streamClientInterceptors = append(streamClientInterceptors, interceptor.StreamClientLog())
	}

	// 监控指标
	if o.enableMetrics {
		streamClientInterceptors = append(streamClientInterceptors, interceptor.StreamClientMetrics())
	}

	// 熔断器（默认关闭）
	//if o.enableCircuitBreaker {
	//	streamClientInterceptors = append(streamClientInterceptors, interceptor.StreamClientCircuitBreaker(
	//	//interceptor.WithValidCode(codes.PermissionDenied),
	//	))
	//}

	// 重试机制
	if o.enableRetry {
		streamClientInterceptors = append(streamClientInterceptors, interceptor.StreamClientRetry())
	}

	// 自定义流式拦截器：由调用方通过 WithStreamInterceptors 注入
	streamClientInterceptors = append(streamClientInterceptors, o.streamInterceptors...)

	return grpc.WithChainStreamInterceptor(streamClientInterceptors...)
}
