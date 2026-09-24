package tracer

import (
	"context"
	"net/url"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

// OTLPOption 配置 OTLP exporter 的选项
type OTLPOption func(*otlpOptions)

type otlpOptions struct {
	endpoint     string            // OTLP collector 端点，格式为 host:port
	insecure     bool              // 是否禁用 TLS
	headers      map[string]string // 额外的 gRPC 头部（如认证 token）
	timeout      time.Duration     // 导出超时时间
	errorHandler otel.ErrorHandler // 自定义 OTel 错误处理器，nil 表示使用默认
}

func defaultOTLPOptions() *otlpOptions {
	return &otlpOptions{
		endpoint: "localhost:4317",
		insecure: true,             // 是否禁用 TLS，true 表示禁用，false 表示启用
		timeout:  10 * time.Second, // 导出超时时间
	}
}

// WithEndpoint 设置 OTLP collector 端点，格式为 host:port
func WithEndpoint(endpoint string) OTLPOption {
	return func(o *otlpOptions) {
		o.endpoint = endpoint
	}
}

// WithInsecure 设置是否禁用 TLS。
// insecure=true 表示禁用 TLS，insecure=false 表示启用 TLS。
func WithInsecure(insecure bool) OTLPOption {
	return func(o *otlpOptions) {
		o.insecure = insecure
	}
}

// WithHeaders 设置额外的请求头部（gRPC metadata / HTTP headers），如认证 token。
func WithHeaders(headers map[string]string) OTLPOption {
	return func(o *otlpOptions) {
		o.headers = headers
	}
}

// WithTimeout 设置导出超时时间
func WithTimeout(timeout time.Duration) OTLPOption {
	return func(o *otlpOptions) {
		o.timeout = timeout
	}
}

// WithErrorHandler 设置自定义的 OTel 错误处理器。
// 若不设置，Init/InitWithOTLP* 会注册默认的 stderr 输出处理器。
// 注意：Init/InitWithOTLP* 调用时会覆盖全局 otel.ErrorHandler。
func WithErrorHandler(h otel.ErrorHandler) OTLPOption {
	return func(o *otlpOptions) {
		o.errorHandler = h
	}
}

// NewOTLPExporter 创建 OTLP span exporter。
// 自动根据 endpoint 格式选择传输协议：
//   - 以 http:// 或 https:// 开头 → 使用 HTTP 协议（otlptracehttp），支持完整 URL 含路径和认证 token
//   - 其他格式（host:port）→ 使用 gRPC 协议（otlptracegrpc），兼容原有行为
//
// OTLP 使用 Protobuf 序列化，性能优越。
func NewOTLPExporter(opts ...OTLPOption) (*otlptrace.Exporter, error) {
	o := defaultOTLPOptions()
	for _, opt := range opts {
		opt(o)
	}

	// 根据 endpoint 格式自动选择 HTTP 或 gRPC 协议
	if isHTTPEndpoint(o.endpoint) {
		return newOTLPHTTPExporter(o)
	}
	return newOTLPGRPCExporter(o)
}

// isHTTPEndpoint 判断 endpoint 是否为 HTTP URL 格式。
// 判断规则：scheme 为 http/https 且 host 和 path 均非空。
func isHTTPEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.Path != "" && u.Path != "/"
}

// newOTLPHTTPExporter 使用 OTLP HTTP 协议创建 exporter。
// 适用于阿里云等提供 HTTP OTLP 端点的场景，endpoint 为完整 URL。
func newOTLPHTTPExporter(o *otlpOptions) (*otlptrace.Exporter, error) {
	ctx := context.Background()

	exporterOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(o.endpoint),
		otlptracehttp.WithTimeout(o.timeout),
	}
	if o.insecure {
		exporterOpts = append(exporterOpts, otlptracehttp.WithInsecure())
	}
	if len(o.headers) > 0 {
		exporterOpts = append(exporterOpts, otlptracehttp.WithHeaders(o.headers))
	}

	return otlptracehttp.New(ctx, exporterOpts...)
}

// newOTLPGRPCExporter 使用 OTLP gRPC 协议创建 exporter。
// endpoint 格式为 host:port，如 "localhost:4317"。
func newOTLPGRPCExporter(o *otlpOptions) (*otlptrace.Exporter, error) {
	ctx := context.Background()

	exporterOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(o.endpoint),
		otlptracegrpc.WithTimeout(o.timeout),
	}
	if o.insecure {
		exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
	}
	if len(o.headers) > 0 {
		exporterOpts = append(exporterOpts, otlptracegrpc.WithHeaders(o.headers))
	}

	return otlptracegrpc.New(ctx, exporterOpts...)
}
