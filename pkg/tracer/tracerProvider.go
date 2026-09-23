// Package tracer is a library wrapped in go.opentelemetry.io/otel.
package tracer

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

var (
	tp      *trace.TracerProvider
	sampler *dynamicSampler
)

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

	// 创建动态 sampler，支持运行时修改采样率
	sampler = newDynamicSampler(fraction)
	sampler.SetSamplingRate(fraction)

	tp = trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
		trace.WithSampler(sampler), // 动态采样率
	)
	// register the TracerProvider as global so that any future imports of package go.opentelemetry.io/otel/trace will use it by default.
	otel.SetTracerProvider(tp)
	// propagation of context across processes
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
}

// SetSamplingRate 动态修改链路追踪采样率（0~1）。
// 0 = 不采样（等效关闭追踪），1 = 全量采样。
// 修改后立即对所有新创建的 Span 生效，无需重启服务。
func SetSamplingRate(rate float64) {
	if sampler != nil {
		sampler.SetSamplingRate(rate)
	}
}

// GetSamplingRate 获取当前采样率。
func GetSamplingRate() float64 {
	if sampler != nil {
		return sampler.GetSamplingRate()
	}
	return 1.0
}

// Close tracer
func Close(ctx context.Context) error {
	if tp == nil {
		return nil
	}
	return tp.Shutdown(ctx)
}

// InitWithOTLP 使用 OTLP 协议初始化 tracer。
// OTLP 使用 Protobuf 序列化，性能优越。
// otlpEndpoint 支持两种格式：
//   - HTTP URL：以 http:// 或 https:// 开头，如 "http://tracing-analysis-dc-sh.aliyuncs.com/xxx/api/otlp/traces"
//   - gRPC 地址：host:port 格式，如 "cn-shanghai.tracing-api.aliyuncs.com:80"
//
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
//
//nolint:revive // 参数数量为业务需求，保持与 InitWithOTLP 一致的扁平签名
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

	// 创建动态 sampler，支持运行时修改采样率
	sampler = newDynamicSampler(fraction)
	sampler.SetSamplingRate(fraction)

	tp = trace.NewTracerProvider(
		trace.WithBatcher(exporter, batchOpts...),
		trace.WithResource(res),
		trace.WithSampler(sampler), // 动态采样率
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
