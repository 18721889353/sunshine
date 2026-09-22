// Package tracer is a library wrapped in go.opentelemetry.io/otel.
package tracer

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	//nolint:staticcheck // jaeger exporter is deprecated but kept for backward compatibility
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

var tp *trace.TracerProvider

// Init Initialize tracer, parameter fraction is fraction, default is 1.0, value >= 1.0 means all links are sampled,
// value <= 0 means all are not sampled, 0 < value < 1 only samples percentage
func Init(exporter trace.SpanExporter, res *resource.Resource, fractions ...float64) {
	var fraction = 1.0
	if len(fractions) > 0 {
		if fractions[0] <= 0 {
			fraction = 0
		} else if fractions[0] < 1 {
			fraction = fractions[0]
		}
	}

	tp = trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(fraction))), // sampling rate
	)
	// register the TracerProvider as global so that any future imports of package go.opentelemetry.io/otel/trace will use it by default.
	otel.SetTracerProvider(tp)
	// propagation of context across processes
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
}

// Close tracer
func Close(ctx context.Context) error {
	if tp == nil {
		return nil
	}
	return tp.Shutdown(ctx)
}

// InitWithConfig Initialize tracer according to configuration, fraction is fraction, default is 1.0, value >= 1.0 means all links are sampled,
// value <= 0 means all are not sampled, 0 < value < 1 only samples percentage
// If jaegerAgentHost and jaegerAgentPort are provided, use Agent mode (UDP)
// Otherwise, if endpoint is provided, use HTTP mode to report directly to Jaeger collector
//
// Deprecated: Use InitWithOTLP for new services. Jaeger exporter uses Thrift protocol
// which causes excessive memory allocation (~1.2GB cumulative via bytes.growSlice).
// OTLP uses Protobuf serialization which is significantly more memory-efficient.
func InitWithConfig(appName string, appEnv string, appVersion string,
	jaegerAgentHost string, jaegerAgentPort string, jaegerSamplingRate float64, endpoint ...string) {
	res := NewResource(
		WithServiceName(appName),
		WithEnvironment(appEnv),
		WithServiceVersion(appVersion),
	)

	var exporter trace.SpanExporter
	var err error

	// Check if endpoint is provided (HTTP mode)
	if len(endpoint) > 0 && endpoint[0] != "" {
		// Use HTTP collector endpoint
		exporter, err = jaeger.New(
			jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(endpoint[0])),
		)
		if err != nil {
			panic("init trace error (HTTP endpoint):" + err.Error())
		}
	} else if jaegerAgentHost != "" && jaegerAgentPort != "" {
		// Use Agent mode (UDP)
		exporter, err = NewJaegerAgentExporter(jaegerAgentHost, jaegerAgentPort)
		if err != nil {
			panic("init trace error (Agent mode):" + err.Error())
		}
	} else {
		panic("init trace error: either jaegerAgentHost/jaegerAgentPort or endpoint must be provided")
	}

	Init(exporter, res, jaegerSamplingRate)

	SetTraceName(appName)
}

// InitWithOTLP 使用 OTLP 协议初始化 tracer（推荐）。
// OTLP 使用 Protobuf 序列化，相比 Jaeger Thrift 协议可显著降低累积内存分配。
// otlpEndpoint 支持两种格式：
//   - HTTP URL：以 http:// 或 https:// 开头，如 "http://tracing-analysis-dc-sh.aliyuncs.com/xxx/api/otlp/traces"
//   - gRPC 地址：host:port 格式，如 "cn-shanghai.tracing-api.aliyuncs.com:80"
// opts 可选配置项，如 WithInsecure、WithHeaders、WithTimeout 等。
func InitWithOTLP(appName string, appEnv string, appVersion string,
	samplingRate float64, otlpEndpoint string, opts ...OTLPOption) {
	res := NewResource(
		WithServiceName(appName),
		WithEnvironment(appEnv),
		WithServiceVersion(appVersion),
	)

	// 合并用户传入的 endpoint 配置
	allOpts := []OTLPOption{WithEndpoint(otlpEndpoint)}
	allOpts = append(allOpts, opts...)

	exporter, err := NewOTLPExporter(allOpts...)
	if err != nil {
		panic("init trace error (OTLP): " + err.Error())
	}

	Init(exporter, res, samplingRate)

	SetTraceName(appName)
}

// InitWithOTLPBatch 使用 OTLP 协议初始化 tracer，并支持自定义 BatchSpanProcessor 参数。
// 适用于需要精细控制导出批次大小和频率的场景。
// otlpEndpoint 支持 HTTP URL 和 gRPC host:port 两种格式，详见 InitWithOTLP。
func InitWithOTLPBatch(appName string, appEnv string, appVersion string,
	samplingRate float64, otlpEndpoint string,
	maxQueueSize int, maxExportBatchSize int,
	batchTimeout time.Duration, exportTimeout time.Duration,
	opts ...OTLPOption) {
	res := NewResource(
		WithServiceName(appName),
		WithEnvironment(appEnv),
		WithServiceVersion(appVersion),
	)

	allOpts := []OTLPOption{WithEndpoint(otlpEndpoint)}
	allOpts = append(allOpts, opts...)

	exporter, err := NewOTLPExporter(allOpts...)
	if err != nil {
		panic("init trace error (OTLP batch): " + err.Error())
	}

	var fraction = 1.0
	if samplingRate <= 0 {
		fraction = 0
	} else if samplingRate < 1 {
		fraction = samplingRate
	}

	batchOpts := []trace.BatchSpanProcessorOption{}
	if maxQueueSize > 0 {
		batchOpts = append(batchOpts, trace.WithMaxQueueSize(maxQueueSize))
	}
	if maxExportBatchSize > 0 {
		batchOpts = append(batchOpts, trace.WithMaxExportBatchSize(maxExportBatchSize))
	}
	if batchTimeout > 0 {
		batchOpts = append(batchOpts, trace.WithBatchTimeout(batchTimeout))
	}
	if exportTimeout > 0 {
		batchOpts = append(batchOpts, trace.WithExportTimeout(exportTimeout))
	}

	tp = trace.NewTracerProvider(
		trace.WithBatcher(exporter, batchOpts...),
		trace.WithResource(res),
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(fraction))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	SetTraceName(appName)
}

// GetProvider get tracer provider
func GetProvider() *trace.TracerProvider {
	if tp == nil {
		panic("tracer provider is nil, initialize it first with InitWithOTLP(...)")
	}
	return tp
}
