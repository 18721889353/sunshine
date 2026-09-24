// Package tracer 封装 opentelemetry.io/otel 链路追踪库，提供 TracerProvider 初始化、
// 动态采样率控制、OTLP 导出及 Span 创建等能力。
package tracer

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

const defaultShutdownTimeout = 5 * time.Second

var (
	tpMu    sync.RWMutex
	tp      *trace.TracerProvider
	sampler *dynamicSampler
	exp     trace.SpanExporter // 追踪当前 exporter，替换时显式关闭
)

// Init 初始化链路追踪器，参数 fraction 为采样比例，默认 1.0。
// value >= 1.0 表示全量采样，value <= 0 表示不采样，0 < value < 1 按比例采样。
//
// Init 会接管传入 exporter 的生命周期：当 exporter 被替换或 Close 时，
// 若新旧 exporter 不同，则旧 exporter 会被 Shutdown。若复用同一 exporter，
// 则不会被误关。
func Init(exporter trace.SpanExporter, res *resource.Resource, fractions ...float64) error {
	return initInternal(exporter, res, nil, fractions...)
}

// SetSamplingRate 动态修改链路追踪采样率（0~1）。
// 0 = 不采样（等效关闭追踪），1 = 全量采样。
// 修改后立即对所有新创建的 Span 生效，无需重启服务。
func SetSamplingRate(rate float64) {
	tpMu.RLock()
	s := sampler
	tpMu.RUnlock()
	if s != nil {
		s.SetSamplingRate(rate)
	}
}

// GetSamplingRate 获取当前采样率。
func GetSamplingRate() float64 {
	tpMu.RLock()
	s := sampler
	tpMu.RUnlock()
	if s != nil {
		return s.GetSamplingRate()
	}
	return 1.0
}

// Close 优雅关闭 TracerProvider，刷新并关闭所有待导出的 Span 和当前 exporter。
func Close(ctx context.Context) error {
	tpMu.Lock()
	oldTp, oldExp := tp, exp
	tp = nil
	exp = nil
	sampler = nil
	tpMu.Unlock()

	if oldTp == nil {
		return nil
	}
	err := oldTp.Shutdown(ctx)
	if oldExp != nil {
		if shutdownErr := shutdownWithTimeout(oldExp, defaultShutdownTimeout); shutdownErr != nil {
			logger.WarnWithCtx(ctx, "[tracer] 关闭旧 exporter 失败", logger.Err(shutdownErr))
		}
	}
	// 重置 traceName，确保 Close 后重新 Init 时不会残留旧服务名
	traceName.Store("unknown")
	return err
}

// InitWithOTLP 使用 OTLP 协议初始化 tracer。
// OTLP 使用 Protobuf 序列化，性能优越。
// otlpEndpoint 支持两种格式：
//   - HTTP URL：以 http:// 或 https:// 开头，如 "http://tracing-analysis-dc-sh.aliyuncs.com/xxx/api/otlp/traces"
//   - gRPC 地址：host:port 格式，如 "cn-shanghai.tracing-api.aliyuncs.com:80"
//
// opts 可选配置项，如 WithInsecure、WithHeaders、WithTimeout、WithErrorHandler 等。
func InitWithOTLP(appName string, appEnv string, appVersion string,
	samplingRate float64, otlpEndpoint string, opts ...OTLPOption) error {
	res := NewResource(
		WithServiceName(appName),
		WithEnvironment(appEnv),
		WithServiceVersion(appVersion),
	)

	// 合并 endpoint 和用户 opts，确保 endpoint 传递给 NewOTLPExporter
	allOpts := append([]OTLPOption{WithEndpoint(otlpEndpoint)}, opts...)
	handler := resolveErrorHandler(allOpts)

	exporter, err := NewOTLPExporter(allOpts...)
	if err != nil {
		return err
	}

	if err = initInternal(exporter, res, handler, samplingRate); err != nil {
		if shutdownErr := shutdownWithTimeout(exporter, defaultShutdownTimeout); shutdownErr != nil {
			logger.WarnWithCtx(context.Background(), "[tracer] 初始化失败后关闭 exporter 失败", logger.Err(shutdownErr))
		}
		return err
	}

	SetTraceName(appName)
	return nil
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
	opts ...OTLPOption) error {
	res := NewResource(
		WithServiceName(appName),
		WithEnvironment(appEnv),
		WithServiceVersion(appVersion),
	)

	// 合并 endpoint 和用户 opts，确保 endpoint 传递给 NewOTLPExporter
	allOpts := append([]OTLPOption{WithEndpoint(otlpEndpoint)}, opts...)
	handler := resolveErrorHandler(allOpts)

	exporter, err := NewOTLPExporter(allOpts...)
	if err != nil {
		return err
	}

	fraction := clampRate(samplingRate)

	batchOpts := buildBatchOpts(maxQueueSize, maxExportBatchSize, batchTimeout, exportTimeout)
	if err = initInternalWithBatch(exporter, res, handler, fraction, batchOpts...); err != nil {
		if shutdownErr := shutdownWithTimeout(exporter, defaultShutdownTimeout); shutdownErr != nil {
			logger.WarnWithCtx(context.Background(), "[tracer] 初始化失败后关闭 exporter 失败", logger.Err(shutdownErr))
		}
		return err
	}

	SetTraceName(appName)
	return nil
}

// GetProvider 获取全局 TracerProvider 实例。
// 未初始化或已 Close 时 panic。
//
//nolint:revive // panic 表示编程错误（未初始化就使用），保留 panic 语义
func GetProvider() *trace.TracerProvider {
	tpMu.RLock()
	defer tpMu.RUnlock()
	if tp == nil {
		panic("tracer: provider 未初始化或已关闭，请先调用 Init 或 InitWithOTLP")
	}
	return tp
}

// initInternal 所有初始化路径的公共实现。
func initInternal(exporter trace.SpanExporter, res *resource.Resource,
	handler otel.ErrorHandler, fractions ...float64) error {
	if exporter == nil {
		return errors.New("tracer: exporter 不能为 nil")
	}
	if res == nil {
		res = NewResource()
	}

	fraction := 1.0
	if len(fractions) > 0 {
		fraction = clampRate(fractions[0])
	}

	installProvider(exporter, res, fraction, handler)
	return nil
}

// initInternalWithBatch 带 BatchSpanProcessor 参数的初始化公共实现。
func initInternalWithBatch(exporter trace.SpanExporter, res *resource.Resource,
	handler otel.ErrorHandler, fraction float64,
	batchOpts ...trace.BatchSpanProcessorOption) error {
	if exporter == nil {
		return errors.New("tracer: exporter 不能为 nil")
	}
	if res == nil {
		res = NewResource()
	}
	installProvider(exporter, res, fraction, handler, batchOpts...)
	return nil
}

// installProvider 统一完成 TracerProvider 的替换与全局注册。
// handler 为自定义 OTel ErrorHandler，nil 时使用默认 stderr 输出。
func installProvider(
	exporter trace.SpanExporter,
	res *resource.Resource,
	fraction float64,
	handler otel.ErrorHandler,
	batchOpts ...trace.BatchSpanProcessorOption,
) {
	// 锁内完成所有状态写入，包括 registerGlobal。
	// otel.SetTracerProvider/SetTextMapPropagator/SetErrorHandler 均为
	// atomic.Pointer.Store，纳秒级操作，不会引入锁内阻塞。
	// 将 registerGlobal 放在锁内，保证 tp 与全局 OTel 状态的原子一致性，
	// 杜绝并发 Init 导致 global provider 指向已关闭旧 provider 的竞态。
	tpMu.Lock()
	oldTp, oldExp := tp, exp // := 函数作用域，锁外仍可访问
	newSampler := newDynamicSampler(fraction)
	newTp := trace.NewTracerProvider(
		trace.WithBatcher(exporter, batchOpts...),
		trace.WithResource(res),
		trace.WithSampler(trace.ParentBased(newSampler)),
	)
	sampler = newSampler
	exp = exporter
	tp = newTp
	registerGlobal(newTp, handler)
	tpMu.Unlock()

	// 只把耗时的 shutdown 放锁外，避免阻塞 SetSamplingRate 等热更新路径
	shutdownOld(oldTp, oldExp, exporter)
}

// registerGlobal 将 TracerProvider、传播器、ErrorHandler 注册为全局默认。
// handler 为 nil 时使用默认 stderr 输出处理器。
// 必须在 tpMu 内调用，保证与 tp 赋值的原子一致性。
func registerGlobal(p *trace.TracerProvider, handler otel.ErrorHandler) {
	otel.SetTracerProvider(p)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	if handler == nil {
		handler = defaultErrorHandler()
	}
	otel.SetErrorHandler(handler)
}

// defaultErrorHandler 返回默认的 OTel 错误处理器，将错误输出到 stderr。
func defaultErrorHandler() otel.ErrorHandler {
	return otel.ErrorHandlerFunc(func(err error) {
		logger.WarnWithCtx(context.Background(), "[tracer] otel error", logger.Err(err))
	})
}

// shutdownOld 在锁外安全关闭旧的 TracerProvider 和 exporter。
// currentExp 为当前正在使用的新 exporter，若 oldExp 与之相同则跳过关闭，
// 避免复用同一 exporter 时被误关。
func shutdownOld(oldTp *trace.TracerProvider, oldExp trace.SpanExporter, currentExp trace.SpanExporter) {
	if oldTp != nil {
		if err := shutdownWithTimeout(oldTp, defaultShutdownTimeout); err != nil {
			logger.WarnWithCtx(context.Background(), "[tracer] 关闭旧 TracerProvider 失败", logger.Err(err))
		}
	}
	if oldExp != nil && oldExp != currentExp {
		if err := shutdownWithTimeout(oldExp, defaultShutdownTimeout); err != nil {
			logger.WarnWithCtx(context.Background(), "[tracer] 关闭旧 exporter 失败", logger.Err(err))
		}
	}
}

// shutdownWithTimeout 对支持 Shutdown(ctx) 的对象执行带超时的关闭。
func shutdownWithTimeout(sd interface{ Shutdown(context.Context) error }, d time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return sd.Shutdown(ctx)
}

// resolveErrorHandler 从 OTLPOption 列表中提取自定义的 errorHandler。
// 未设置时返回 nil，由 registerGlobal 走默认处理器。
func resolveErrorHandler(opts []OTLPOption) otel.ErrorHandler {
	o := defaultOTLPOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o.errorHandler
}

// buildBatchOpts 根据参数构建 BatchSpanProcessor 配置。
func buildBatchOpts(maxQueueSize, maxExportBatchSize int,
	batchTimeout, exportTimeout time.Duration) []trace.BatchSpanProcessorOption {
	var batchOpts []trace.BatchSpanProcessorOption
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
	return batchOpts
}
