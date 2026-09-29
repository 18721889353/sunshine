package tracer

import (
	"context"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
)

// defaultTracesPath OTLP/HTTP 协议默认的 traces 上报路径。
const defaultTracesPath = "/v1/traces"

// Protocol OTLP 传输协议。
type Protocol int

const (
	// ProtocolAuto 按 endpoint 形态自动判定：http(s):// 开头走 HTTP，其余走 gRPC。
	ProtocolAuto Protocol = iota
	// ProtocolHTTP 强制 OTLP/HTTP（otlptracehttp）。
	ProtocolHTTP
	// ProtocolGRPC 强制 OTLP/gRPC（otlptracegrpc）。
	ProtocolGRPC
)

// OTLPOption 配置 OTLP exporter 的选项
type OTLPOption func(*otlpOptions)

type otlpOptions struct {
	endpoint       string                        // OTLP collector 端点，host:port 或完整 URL
	insecure       bool                          // 是否禁用 TLS
	insecureSet    bool                          // 是否显式设置过 insecure（显式优先于 scheme 推断）
	headers        map[string]string             // 额外的 gRPC 头部（如认证 token）
	timeout        time.Duration                 // 导出超时时间
	errorHandler   otel.ErrorHandler             // 自定义 OTel 错误处理器，nil 表示使用默认
	propagator     propagation.TextMapPropagator // 自定义全局传播器，nil 时注册 W3C 复合传播器
	registerGlobal bool                          // 是否注册为 OTel 全局（false 则完全不触碰全局状态）
	protocol       Protocol                      // 传输协议，ProtocolAuto 按 endpoint 形态判定
}

func defaultOTLPOptions() *otlpOptions {
	return &otlpOptions{
		endpoint: "localhost:4317",
		// 安全默认：启用 TLS。明文传输需显式 WithInsecure(true) 或传 http:// 开头的 endpoint（按 scheme 推断）。
		// 此前默认 insecure=true，会在调用方忘记 WithInsecure(false) 时把 https 链路降级为明文。
		insecure:       false,
		timeout:        10 * time.Second, // 导出超时时间
		registerGlobal: true,
		protocol:       ProtocolAuto,
	}
}

// WithEndpoint 设置 OTLP collector 端点。
// gRPC 为 host:port 格式；HTTP 可传完整 URL（含路径与 token），无路径时自动补 /v1/traces。
func WithEndpoint(endpoint string) OTLPOption {
	return func(o *otlpOptions) {
		o.endpoint = endpoint
	}
}

// WithInsecure 设置是否禁用 TLS。
// insecure=true 表示禁用 TLS（明文），insecure=false 表示启用 TLS。
// 显式调用本选项后不再按 endpoint scheme 推断；未调用时的推断规则见 resolveInsecure。
func WithInsecure(insecure bool) OTLPOption {
	return func(o *otlpOptions) {
		o.insecure = insecure
		o.insecureSet = true
	}
}

// WithHeaders 设置额外的请求头部（gRPC metadata / HTTP headers），如认证 token。
// 传入的 map 会被复制，调用方后续修改原 map 不影响已保存的配置（避免并发读写 data race）。
func WithHeaders(headers map[string]string) OTLPOption {
	return func(o *otlpOptions) {
		o.headers = copyStringMap(headers)
	}
}

// WithTimeout 设置导出超时时间
func WithTimeout(timeout time.Duration) OTLPOption {
	return func(o *otlpOptions) {
		o.timeout = timeout
	}
}

// WithErrorHandler 设置自定义的 OTel 错误处理器。
// 若不设置，Init/InitWithOTLP* 会注册默认的警告日志处理器（写入 pkg/logger）。
// 注意：注册期间为「最后写入胜出」——会替换全局 otel.ErrorHandler；
// Close 时本包会恢复注册前的全局处理器，不会永久污染其他库的配置。
func WithErrorHandler(h otel.ErrorHandler) OTLPOption {
	return func(o *otlpOptions) {
		o.errorHandler = h
	}
}

// WithPropagator 设置自定义的全局 TextMapPropagator。
// 若不设置，注册 W3C 复合传播器（TraceContext + Baggage）。
// 与 WithErrorHandler 相同：注册期最后写入胜出，Close 时恢复注册前的全局传播器。
func WithPropagator(p propagation.TextMapPropagator) OTLPOption {
	return func(o *otlpOptions) {
		o.propagator = p
	}
}

// WithGlobalRegistration 控制是否把 provider/propagator/handler 注册为 OTel 全局（默认 true）。
// 设为 false 时完全不触碰 OTel 全局状态，适合多库共存或自行管理全局状态的场景；
// 注意：此时业务侧 otel.Tracer(name) 不会拿到本 provider，需改用 GetProvider().Tracer(name)。
func WithGlobalRegistration(enabled bool) OTLPOption {
	return func(o *otlpOptions) {
		o.registerGlobal = enabled
	}
}

// WithProtocol 显式指定传输协议，覆盖 endpoint 形态的自动判定。
// 场景：endpoint 同时可能被解读为两种协议时消除歧义（如仅给 host:port 却要走 OTLP/HTTP）。
func WithProtocol(p Protocol) OTLPOption {
	return func(o *otlpOptions) {
		o.protocol = p
	}
}

// resolveInsecure 解析是否禁用 TLS。
// 显式 WithInsecure 优先；未显式设置时按 endpoint scheme 推断：
// http:// 走明文，https:// 与无 scheme（gRPC host:port）默认 TLS。
func (o *otlpOptions) resolveInsecure() bool {
	if o.insecureSet {
		return o.insecure
	}
	return strings.HasPrefix(o.endpoint, "http://")
}

// NewOTLPExporter 创建 OTLP span exporter。
// 传输协议选择优先级：WithProtocol 显式指定 > endpoint 形态自动判定
// （http:// 或 https:// 开头 → OTLP/HTTP，支持完整 URL 含路径和认证 token；其余 host:port → OTLP/gRPC）。
//
// OTLP 使用 Protobuf 序列化，性能优越。
func NewOTLPExporter(opts ...OTLPOption) (*otlptrace.Exporter, error) {
	o := defaultOTLPOptions()
	for _, opt := range opts {
		opt(o)
	}

	switch {
	case o.protocol == ProtocolHTTP:
		return newOTLPHTTPExporter(o)
	case o.protocol == ProtocolGRPC:
		return newOTLPGRPCExporter(o)
	case isHTTPEndpoint(o.endpoint):
		return newOTLPHTTPExporter(o)
	default:
		return newOTLPGRPCExporter(o)
	}
}

// isHTTPEndpoint 判断 endpoint 是否为 HTTP URL 格式（scheme 为 http/https 且 host 非空）。
// path 不参与判定：http://host:4318 这类无 path 的端点同样属于 HTTP，
// 缺失的路径由 splitHTTPURL 补默认值 /v1/traces。
// 此前要求 path 非空，导致 http://host:4318 被误判为 gRPC，把 gRPC 流量打到 HTTP 端口。
func isHTTPEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// splitHTTPURL 把 HTTP endpoint 拆分为 host:port 与 URL path。
// path 为空或 "/" 时补默认 traces 路径 /v1/traces（OTLP/HTTP 规范默认值）。
// 不使用 SDK 的 WithEndpointURL：它会按 URL scheme 强制覆盖 insecure 配置
// （otlpconfig.WithEndpointURL 内部 Insecure = scheme != "https"），
// 无法做到「显式 WithInsecure 优先于 scheme 推断」，故拆开后自主控制 TLS。
func splitHTTPURL(endpoint string) (hostPort, urlPath string) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		// 无 scheme 的 host:port（WithProtocol(ProtocolHTTP) 强制场景）
		return endpoint, defaultTracesPath
	}
	path := u.Path
	if path == "" || path == "/" {
		path = defaultTracesPath
	}
	return u.Host, path
}

// normalizeGRPCEndpoint 返回 gRPC 的 host:port 端点。
// 若误传了完整 URL，剥离 scheme 与 path 后使用（WithProtocol(ProtocolGRPC) 强制场景）。
func normalizeGRPCEndpoint(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return endpoint
}

// copyStringMap 复制 map，nil 输入返回空 map。
func copyStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// newOTLPHTTPExporter 使用 OTLP HTTP 协议创建 exporter。
// 适用于阿里云等提供 HTTP OTLP 端点的场景，endpoint 可为完整 URL（含路径和 token）。
func newOTLPHTTPExporter(o *otlpOptions) (*otlptrace.Exporter, error) {
	ctx := context.Background()

	hostPort, urlPath := splitHTTPURL(o.endpoint)
	exporterOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(hostPort),
		otlptracehttp.WithURLPath(urlPath),
		otlptracehttp.WithTimeout(o.timeout),
	}
	if o.resolveInsecure() {
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
		otlptracegrpc.WithEndpoint(normalizeGRPCEndpoint(o.endpoint)),
		otlptracegrpc.WithTimeout(o.timeout),
	}
	if o.resolveInsecure() {
		exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
	}
	if len(o.headers) > 0 {
		exporterOpts = append(exporterOpts, otlptracegrpc.WithHeaders(o.headers))
	}

	return otlptracegrpc.New(ctx, exporterOpts...)
}
