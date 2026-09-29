package tracer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// TestIsHTTPEndpointFormats 验证 isHTTPEndpoint 对各类 endpoint 格式的协议判定。
func TestIsHTTPEndpointFormats(t *testing.T) {
	cases := map[string]bool{
		"localhost:4317":                       false,
		"http://localhost:4318":                true, // 无 path 也是 HTTP，路径由 splitHTTPURL 补默认值
		"http://host:4318/v1/traces":           true,
		"https://tracing.aliyuncs.com/x/api/y": true,
		"http:///foo":                          false, // host 为空
		"http://host:4318/":                    true,  // path 仅 "/" 也是 HTTP
		"":                                     false,
		"grpc://host:4317":                     false, // 非 http/https scheme
	}
	for input, want := range cases {
		assert.Equal(t, want, isHTTPEndpoint(input), "endpoint=%s", input)
	}
}

// TestSplitHTTPURL 验证 HTTP endpoint 拆分：无路径自动补 /v1/traces，有路径原样保留。
func TestSplitHTTPURL(t *testing.T) {
	host, path := splitHTTPURL("http://host:4318")
	assert.Equal(t, "host:4318", host)
	assert.Equal(t, defaultTracesPath, path)

	host, path = splitHTTPURL("https://tracing.aliyuncs.com/adapt_x@y/api/otlp/traces")
	assert.Equal(t, "tracing.aliyuncs.com", host)
	assert.Equal(t, "/adapt_x@y/api/otlp/traces", path)

	// 无 scheme 的 host:port（WithProtocol(ProtocolHTTP) 强制场景）
	host, path = splitHTTPURL("host:4318")
	assert.Equal(t, "host:4318", host)
	assert.Equal(t, defaultTracesPath, path)
}

// TestNormalizeGRPCEndpoint 验证 gRPC 端点归一：误传 URL 时剥离 scheme 与 path。
func TestNormalizeGRPCEndpoint(t *testing.T) {
	assert.Equal(t, "host:4317", normalizeGRPCEndpoint("host:4317"))
	assert.Equal(t, "host:4317", normalizeGRPCEndpoint("http://host:4317/x"))
}

// TestResolveInsecure 验证 TLS 推断规则（P0-3 验收）：显式 WithInsecure 优先，
// 未显式时 https 强制 TLS、http 走明文、无 scheme 默认 TLS。
func TestResolveInsecure(t *testing.T) {
	// 未显式设置：按 scheme 推断
	o := defaultOTLPOptions()
	o.endpoint = "https://tracing.aliyuncs.com/x/api/y"
	assert.False(t, o.resolveInsecure(), "https:// 未显式设置时必须走 TLS")

	o.endpoint = "http://host:4318"
	assert.True(t, o.resolveInsecure(), "http:// 未显式设置时走明文")

	o.endpoint = "host:4317"
	assert.False(t, o.resolveInsecure(), "无 scheme 默认 TLS（安全默认）")

	// 显式设置优先于 scheme 推断
	o = defaultOTLPOptions()
	o.endpoint = "https://tracing.aliyuncs.com/x"
	WithInsecure(true)(o)
	assert.True(t, o.resolveInsecure())

	o = defaultOTLPOptions()
	o.endpoint = "http://host:4318"
	WithInsecure(false)(o)
	assert.False(t, o.resolveInsecure())
}

// TestDefaultOTLPOptionsValues 验证 defaultOTLPOptions 的默认值符合文档约定（安全默认）。
func TestDefaultOTLPOptionsValues(t *testing.T) {
	o := defaultOTLPOptions()
	assert.Equal(t, "localhost:4317", o.endpoint)
	assert.False(t, o.insecure, "默认应启用 TLS（安全默认）")
	assert.False(t, o.insecureSet)
	assert.Equal(t, 10*time.Second, o.timeout)
	assert.Nil(t, o.errorHandler)
	assert.Empty(t, o.headers)
	assert.True(t, o.registerGlobal)
	assert.Equal(t, ProtocolAuto, o.protocol)
}

// TestOTLPOptionApplyAllFields 验证全部 OTLPOption 均可覆盖默认字段。
func TestOTLPOptionApplyAllFields(t *testing.T) {
	o := defaultOTLPOptions()

	WithEndpoint("custom:4317")(o)
	assert.Equal(t, "custom:4317", o.endpoint)

	WithInsecure(false)(o)
	assert.False(t, o.insecure)
	assert.True(t, o.insecureSet, "调用 WithInsecure 后应标记为显式设置")

	headers := map[string]string{"key": "val"}
	WithHeaders(headers)(o)
	assert.Equal(t, headers, o.headers)

	WithTimeout(5 * time.Second)(o)
	assert.Equal(t, 5*time.Second, o.timeout)

	handler := otel.ErrorHandlerFunc(func(error) {})
	WithErrorHandler(handler)(o)
	assert.NotNil(t, o.errorHandler)

	WithProtocol(ProtocolGRPC)(o)
	assert.Equal(t, ProtocolGRPC, o.protocol)

	WithGlobalRegistration(false)(o)
	assert.False(t, o.registerGlobal)
}

// TestWithHeadersCopiesMap 验证 WithHeaders 复制 map（P1-9）：
// 调用方后续修改原 map 不影响已保存的配置，避免并发读写 data race。
func TestWithHeadersCopiesMap(t *testing.T) {
	src := map[string]string{"Authorization": "Bearer old"}
	o := defaultOTLPOptions()
	WithHeaders(src)(o)

	src["Authorization"] = "Bearer new"
	assert.Equal(t, "Bearer old", o.headers["Authorization"], "原 map 的后续修改不应影响配置")
}

// TestNewOTLPExporterHTTPAndGRPC 验证 NewOTLPExporter 按 endpoint 格式创建 HTTP/gRPC exporter 均成功。
// 连接为异步建立，创建过程不发起真实网络请求。
func TestNewOTLPExporterHTTPAndGRPC(t *testing.T) {
	// HTTP URL（含路径）走 otlptracehttp
	httpExp, err := NewOTLPExporter(
		WithEndpoint("http://127.0.0.1:4318/v1/traces"),
		WithInsecure(true),
		WithTimeout(time.Second),
	)
	require.NoError(t, err)
	assert.NotNil(t, httpExp)

	// host:port 走 otlptracegrpc
	grpcExp, err := NewOTLPExporter(
		WithEndpoint("127.0.0.1:4317"),
		WithInsecure(true),
		WithHeaders(map[string]string{"Authorization": "Bearer xxx"}),
		WithTimeout(time.Second),
	)
	require.NoError(t, err)
	assert.NotNil(t, grpcExp)

	// 无 path 的 HTTP URL（P1-10 修复后正确判为 HTTP）
	bareExp, err := NewOTLPExporter(
		WithEndpoint("http://127.0.0.1:4318"),
		WithTimeout(time.Second),
	)
	require.NoError(t, err)
	assert.NotNil(t, bareExp)

	// WithProtocol 强制指定，覆盖 endpoint 形态判定
	forcedExp, err := NewOTLPExporter(
		WithEndpoint("127.0.0.1:4318"),
		WithProtocol(ProtocolHTTP),
		WithInsecure(true),
		WithTimeout(time.Second),
	)
	require.NoError(t, err)
	assert.NotNil(t, forcedExp)

	// 清理后台连接
	assert.NoError(t, grpcExp.Shutdown(t.Context()))
	assert.NoError(t, httpExp.Shutdown(t.Context()))
	assert.NoError(t, bareExp.Shutdown(t.Context()))
	assert.NoError(t, forcedExp.Shutdown(t.Context()))
}
