// Package tracer 封装 opentelemetry.io/otel 链路追踪库，提供 TracerProvider 初始化、
// 动态采样率控制、OTLP 导出及 Span 创建等能力。
//
// 核心功能：
//   - 初始化：Init 接管自定义 exporter；InitWithOTLP / InitWithOTLPBatch 一步完成 OTLP 导出配置。
//   - 全局治理：首次初始化保存 OTel 全局现场（provider/propagator/handler），Close 时恢复，
//     不污染其他库的全局配置；WithGlobalRegistration(false) 可完全不触碰全局状态。
//   - 生命周期：exporter 由本包接管，被替换或 Close 时恰好关闭一次（复用同一 exporter 不误关）。
//   - 动态采样：SetSamplingRate / GetSamplingRate 运行期热更新采样率，无需重建 TracerProvider。
//   - Span 创建：NewSpan 以 map 标签简化 Span 创建；SetTraceName 设置进程级追踪名称。
//   - 选项模式：OTLPOption 配置 exporter（端点/TLS/头部/超时/协议/全局注册/错误处理器）；
//     ResourceOption 配置服务资源属性；BatchConfig 调整导出批次参数。
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
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

const defaultShutdownTimeout = 5 * time.Second

// globalSnapshot OTel 全局状态快照，首次注册时保存，Close 时恢复。
type globalSnapshot struct {
	provider   oteltrace.TracerProvider
	propagator propagation.TextMapPropagator
	handler    otel.ErrorHandler
}

var (
	tpMu    sync.RWMutex
	tp      *trace.TracerProvider
	sampler *dynamicSampler
	exp     trace.SpanExporter // 追踪当前 exporter，替换时关闭一次

	// 以下全局状态均只在 tpMu 临界区内读写，与 tp 的指针替换保持原子一致。
	savedGlobal globalSnapshot // 注册全局前的 OTel 全局现场
	globalSaved bool           // 是否已保存现场（即本包是否正持有全局注册权）
)

// Init 初始化链路追踪器，接管传入 exporter 的生命周期。
// fractions 为可变参数，取第一个元素作为采样比例；缺省时继承当前采样率（未初始化过为 1.0），
// 避免「SetSamplingRate 热更新后被重新 Init 静默重置为全量采样」的陷阱。
// 采样比例 >= 1.0 表示全量采样，<= 0 表示不采样，0 < 比例 < 1 按比例采样。
//
// Init 会接管传入 exporter 的生命周期：当 exporter 被替换或 Close 时，
// 若新旧 exporter 不是同一实例，则旧 exporter 会被 Shutdown 且恰好一次；若复用同一 exporter，
// 则不会被误关。OpenTelemetry SDK 中 TracerProvider.Shutdown 会级联关闭 exporter，
// 本包已拦截该级联，因此直接调用 GetProvider().Shutdown 不会关闭 exporter，真正关闭只发生在替换/Close。
//
// 与 SetSamplingRate 并发时无先后保证，以最后完成者为准。
func Init(exporter trace.SpanExporter, res *resource.Resource, fractions ...float64) error {
	return initInternal(exporter, res, "", defaultGlobalConfig(), fractions...)
}

// SetSamplingRate 动态修改链路追踪采样率（0~1）。
// 0 = 不采样（等效关闭追踪），1 = 全量采样。
// 修改后立即对所有新创建的 Span 生效，无需重启服务。
// 与 Init 并发调用时无先后保证，以最后完成者为准（Init 传入的采样率会重置热更新值）。
func SetSamplingRate(rate float64) {
	tpMu.RLock()
	s := sampler
	tpMu.RUnlock()
	if s != nil {
		s.SetSamplingRate(rate)
	}
}

// GetSamplingRate 获取当前采样率。
// 未初始化或已 Close 时返回 1.0（全量采样，fail-open，避免静默丢追踪）。
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
// 同时把 OTel 全局状态（provider/propagator/handler）恢复为 Init 注册前的现场，
// 保证 Close 后 otel.GetTracerProvider() 不会指向已关闭的实例，也不残留本包的全局配置。
// 契约：Close 与 Init* 应串行调用，不应并发——全局现场保存/恢复是包级单快照，
// 无法配对「哪次 Close 对应哪次 Init」，三路交错（Init/Close/Init）下可能互相销毁对方的现场；
// 如确需并发管理全局状态，请使用 WithGlobalRegistration(false)，本包不触碰 OTel 全局。
func Close(ctx context.Context) error {
	tpMu.Lock()
	oldTp, oldExp := tp, exp
	tp = nil
	exp = nil
	sampler = nil
	// 重置 traceName 也在锁内完成：与 Init* 的 traceName 设置互斥，
	// 保证「Close 完成后 traceName 不会被并发 Init 的写入覆盖」
	traceName.Store("unknown")
	// 恢复全局现场：与 tp 清空同临界区完成，且先于旧 provider 的关闭，
	// 避免「业务并发拿全局 provider 时拿到正在被 Shutdown 的实例」。
	if globalSaved {
		otel.SetTracerProvider(savedGlobal.provider)
		otel.SetTextMapPropagator(savedGlobal.propagator)
		otel.SetErrorHandler(savedGlobal.handler)
		savedGlobal = globalSnapshot{}
		globalSaved = false
	}
	tpMu.Unlock()

	var err error
	if oldTp != nil {
		err = oldTp.Shutdown(ctx)
	}
	// SDK 级联关闭已被 ownedExporter 拦截，这里负责真正关闭 exporter（幂等，恰好一次）
	if oldExp != nil {
		if shutdownErr := closeExporter(oldExp, defaultShutdownTimeout); shutdownErr != nil {
			logger.WarnWithCtx(ctx, "[tracer] 关闭 exporter 失败", logger.Err(shutdownErr))
		}
	}
	return err
}

// InitWithOTLP 使用 OTLP 协议初始化 tracer。
// OTLP 使用 Protobuf 序列化，性能优越。
// otlpEndpoint 支持两种格式：
//   - HTTP URL：以 http:// 或 https:// 开头，如 "http://tracing-analysis-dc-sh.aliyuncs.com/xxx/api/otlp/traces"
//   - gRPC 地址：host:port 格式，如 "cn-shanghai.tracing-api.aliyuncs.com:80"
//
// opts 可选配置项，如 WithInsecure、WithHeaders、WithTimeout、WithProtocol、WithErrorHandler 等。
// appName 与 otlpEndpoint 为必填，为空返回错误。
func InitWithOTLP(appName string, appEnv string, appVersion string,
	samplingRate float64, otlpEndpoint string, opts ...OTLPOption) error {
	if err := validateInitArgs(appName, otlpEndpoint); err != nil {
		return err
	}

	res := NewResource(
		WithServiceName(appName),
		WithEnvironment(appEnv),
		WithServiceVersion(appVersion),
	)

	// 合并 endpoint 和用户 opts，确保 endpoint 传递给 NewOTLPExporter
	allOpts := append([]OTLPOption{WithEndpoint(otlpEndpoint)}, opts...)
	gcfg := resolveGlobalConfig(allOpts)

	exporter, err := NewOTLPExporter(allOpts...)
	if err != nil {
		return err
	}

	if err = initInternal(exporter, res, appName, gcfg, samplingRate); err != nil {
		if shutdownErr := shutdownWithTimeout(exporter, defaultShutdownTimeout); shutdownErr != nil {
			logger.WarnWithCtx(context.Background(), "[tracer] 初始化失败后关闭 exporter 失败", logger.Err(shutdownErr))
		}
		return err
	}
	return nil
}

// BatchConfig 自定义 BatchSpanProcessor 的导出批次参数。
// 每个字段的零值表示使用 OpenTelemetry SDK 默认值，只需覆盖关心的字段。
type BatchConfig struct {
	MaxQueueSize       int           // Span 队列上限，0 表示 SDK 默认值
	MaxExportBatchSize int           // 单次导出的 Span 数量上限，0 表示 SDK 默认值
	BatchTimeout       time.Duration // 批次凑批超时，0 表示 SDK 默认值
	ExportTimeout      time.Duration // 单次导出超时，0 表示 SDK 默认值
}

// InitWithOTLPBatch 使用 OTLP 协议初始化 tracer，并支持自定义 BatchSpanProcessor 参数。
// 适用于需要精细控制导出批次大小和频率的场景。
// otlpEndpoint 支持 HTTP URL 和 gRPC host:port 两种格式，详见 InitWithOTLP。
// batchCfg 的零值字段使用 SDK 默认值，可整体传 BatchConfig{} 等效 InitWithOTLP。
// appName 与 otlpEndpoint 为必填，为空返回错误。
func InitWithOTLPBatch(appName string, appEnv string, appVersion string,
	samplingRate float64, otlpEndpoint string,
	batchCfg BatchConfig,
	opts ...OTLPOption) error {
	if err := validateInitArgs(appName, otlpEndpoint); err != nil {
		return err
	}

	res := NewResource(
		WithServiceName(appName),
		WithEnvironment(appEnv),
		WithServiceVersion(appVersion),
	)

	// 合并 endpoint 和用户 opts，确保 endpoint 传递给 NewOTLPExporter
	allOpts := append([]OTLPOption{WithEndpoint(otlpEndpoint)}, opts...)
	gcfg := resolveGlobalConfig(allOpts)

	exporter, err := NewOTLPExporter(allOpts...)
	if err != nil {
		return err
	}

	fraction := clampRate(samplingRate)

	batchOpts := buildBatchOpts(batchCfg)
	if err = initInternalWithBatch(exporter, res, appName, gcfg, fraction, batchOpts...); err != nil {
		if shutdownErr := shutdownWithTimeout(exporter, defaultShutdownTimeout); shutdownErr != nil {
			logger.WarnWithCtx(context.Background(), "[tracer] 初始化失败后关闭 exporter 失败", logger.Err(shutdownErr))
		}
		return err
	}
	return nil
}

// GetProvider 获取全局 TracerProvider 实例。
// 未初始化或已 Close 时 panic（属于编程错误：未初始化就使用，panic 语义即为快速失败）。
// 需要「不 panic」的安全获取方式时使用 Provider。
func GetProvider() *trace.TracerProvider {
	tpMu.RLock()
	defer tpMu.RUnlock()
	if tp == nil {
		panic("tracer: provider 未初始化或已关闭，请先调用 Init 或 InitWithOTLP")
	}
	return tp
}

// Provider 返回全局 TracerProvider 实例与初始化状态。
// 未初始化或已 Close 时返回 (nil, false)，不会 panic，适合库代码与运行期探测。
func Provider() (*trace.TracerProvider, bool) {
	tpMu.RLock()
	defer tpMu.RUnlock()
	if tp == nil {
		return nil, false
	}
	return tp, true
}

// validateInitArgs 校验 InitWithOTLP* 的必填参数。
// 空 appName 会写入错误的 service.name（生产漏配时静默上错误服务名），
// 空 endpoint 会落到 exporter 默认端点（localhost:4317），都是必须显式失败的配置错误。
func validateInitArgs(appName, otlpEndpoint string) error {
	if appName == "" {
		return errors.New("tracer: appName 不能为空")
	}
	if otlpEndpoint == "" {
		return errors.New("tracer: otlpEndpoint 不能为空")
	}
	return nil
}

// globalConfig 本包对 OTel 全局状态的接管配置。
type globalConfig struct {
	register   bool                          // 是否注册为全局
	propagator propagation.TextMapPropagator // 自定义传播器，nil 时注册 W3C 复合
	handler    otel.ErrorHandler             // 自定义错误处理器，nil 时使用默认警告日志处理器
}

// defaultGlobalConfig 默认全局配置：注册全局 + W3C 复合传播器 + 默认错误处理器。
func defaultGlobalConfig() globalConfig {
	return globalConfig{register: true}
}

// initInternal 所有初始化路径的公共实现。
// fractions 缺省时继承当前采样率（见 Init 注释）。
// name 非空时同步设置进程级 traceName（与 provider 替换在同一临界区，见 installProvider）；
// 空串表示不改动 traceName（Init 自定义 exporter 路径）。
func initInternal(exporter trace.SpanExporter, res *resource.Resource, name string,
	gcfg globalConfig, fractions ...float64) error {
	if exporter == nil {
		return errors.New("tracer: exporter 不能为 nil")
	}
	if res == nil {
		res = NewResource()
	}

	fraction := 1.0
	if len(fractions) > 0 {
		fraction = clampRate(fractions[0])
	} else {
		// 继承当前采样率，避免热更新后的采样率被缺省 Init 静默重置
		fraction = GetSamplingRate()
	}

	installProvider(exporter, res, name, fraction, gcfg)
	return nil
}

// initInternalWithBatch 带 BatchSpanProcessor 参数的初始化公共实现。
func initInternalWithBatch(exporter trace.SpanExporter, res *resource.Resource, name string,
	gcfg globalConfig, fraction float64,
	batchOpts ...trace.BatchSpanProcessorOption) error {
	if exporter == nil {
		return errors.New("tracer: exporter 不能为 nil")
	}
	if res == nil {
		res = NewResource()
	}
	installProvider(exporter, res, name, fraction, gcfg, batchOpts...)
	return nil
}

// installProvider 统一完成 TracerProvider 的替换与全局注册。
// 耗时的 sampler/provider 创建放在锁外，减少对 SetSamplingRate 等热更新路径的锁竞争；
// 临界区内只做指针替换与全局注册，保证二者的原子一致性（均为纳秒级 atomic store），
// 杜绝并发 Init 导致 global provider 指向已关闭旧 provider 的竞态。
func installProvider(
	exporter trace.SpanExporter,
	res *resource.Resource,
	name string,
	fraction float64,
	gcfg globalConfig,
	batchOpts ...trace.BatchSpanProcessorOption,
) {
	owned := newOwnedExporter(exporter)
	newSampler := newDynamicSampler(fraction)
	newTp := trace.NewTracerProvider(
		trace.WithBatcher(owned, batchOpts...),
		trace.WithResource(res),
		trace.WithSampler(trace.ParentBased(newSampler)),
	)

	tpMu.Lock()
	oldTp, oldExp := tp, exp
	sampler = newSampler
	exp = owned
	tp = newTp
	// traceName 与 provider 替换同临界区更新：若放在锁外，
	// 并发 Close 的 traceName 重置会被后完成的锁外写入覆盖，
	// 导致「Close 完成后 traceName 仍为 appName」与契约矛盾
	if name != "" {
		traceName.Store(name)
	}
	if gcfg.register {
		registerGlobal(newTp, gcfg)
	}
	tpMu.Unlock()

	// 只把耗时的 shutdown 放锁外，避免阻塞 SetSamplingRate 等热更新路径
	shutdownOld(oldTp, oldExp, owned)
}

// registerGlobal 将 TracerProvider、传播器、ErrorHandler 注册为全局默认。
// 必须在 tpMu 内调用，保证与 tp 赋值的原子一致性。
//
// 全局现场治理：
//   - 首次注册时保存 OTel 全局现场（savedGlobal），Close 时恢复，
//     避免本包关闭后全局 provider 悬空在已 Shutdown 的实例上，或永久覆盖其他库的配置；
//   - 传播器与 ErrorHandler 注册期为「最后写入胜出」，不做链式组合：
//     OTel 默认 ErrorHandler（ErrDelegator）在首次 SetErrorHandler 时会把委托指针指向新注册者，
//     若把捕获的旧处理器链回新处理器会形成无限递归回环，因此组合方案不可行，
//     以「注册期覆盖 + Close 恢复」作为等效的安全语义。
func registerGlobal(p *trace.TracerProvider, gcfg globalConfig) {
	if !globalSaved {
		savedGlobal = globalSnapshot{
			provider:   otel.GetTracerProvider(),
			propagator: otel.GetTextMapPropagator(),
			handler:    otel.GetErrorHandler(),
		}
		globalSaved = true
	}

	otel.SetTracerProvider(p)

	prop := gcfg.propagator
	if prop == nil {
		prop = propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{}, propagation.Baggage{},
		)
	}
	otel.SetTextMapPropagator(prop)

	handler := gcfg.handler
	if handler == nil {
		handler = defaultErrorHandler()
	}
	otel.SetErrorHandler(handler)
}

// defaultErrorHandler 返回默认的 OTel 错误处理器，将导出失败等错误写入警告日志（含 ctx 支持）。
func defaultErrorHandler() otel.ErrorHandler {
	return otel.ErrorHandlerFunc(func(err error) {
		logger.WarnWithCtx(context.Background(), "[tracer] otel error", logger.Err(err))
	})
}

// ownedExporter 包装调用方传入的 exporter，收回 OpenTelemetry SDK 级联关闭的控制权，
// 保证底层 exporter 恰好关闭一次：
//   - SDK 中 TracerProvider.Shutdown 会经 BatchSpanProcessor 级联调用 exporter.Shutdown，
//     本包替换/Close 时还会显式关闭，非幂等的自定义 exporter 不能被关闭两次；
//   - 复用同一 exporter 初始化多次时，旧 provider 的级联关闭不应误关仍被新 provider 使用的 exporter。
//
// 因此级联 Shutdown 降级为 no-op，真正关闭由本包在「被替换」或「Close」时执行一次。
type ownedExporter struct {
	trace.SpanExporter
	closeOnce sync.Once
	closeErr  error
}

// newOwnedExporter 包装调用方 exporter，接管其生命周期。
func newOwnedExporter(inner trace.SpanExporter) *ownedExporter {
	return &ownedExporter{SpanExporter: inner}
}

// Shutdown 拦截来自 TracerProvider/BatchSpanProcessor 的级联关闭，不触碰底层 exporter。
func (e *ownedExporter) Shutdown(_ context.Context) error {
	return nil
}

// closeExporterOnce 幂等关闭底层 exporter，返回值仅首次调用有意义。
func (e *ownedExporter) closeExporterOnce(ctx context.Context) error {
	e.closeOnce.Do(func() {
		e.closeErr = e.SpanExporter.Shutdown(ctx)
	})
	return e.closeErr
}

// unwrapExporter 返回最内层的调用方 exporter，用于判断两个引用是否为同一底层实例。
func unwrapExporter(e trace.SpanExporter) trace.SpanExporter {
	if oe, ok := e.(*ownedExporter); ok {
		return oe.SpanExporter
	}
	return e
}

// closeExporter 关闭 exporter（幂等，恰好一次）：ownedExporter 走 closeOnce，裸 exporter 直接 Shutdown。
func closeExporter(e trace.SpanExporter, d time.Duration) error {
	if oe, ok := e.(*ownedExporter); ok {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		return oe.closeExporterOnce(ctx)
	}
	return shutdownWithTimeout(e, d)
}

// shutdownOld 在锁外安全关闭旧的 TracerProvider 与旧 exporter。
// currentExp 为新 provider 正在使用的 exporter：
//   - TracerProvider.Shutdown 的级联关闭已被 ownedExporter 拦截，真正关闭由本函数执行一次；
//   - 复用同一底层 exporter 时不关闭（仍被新 provider 使用），由最后持有者（Close）关闭。
func shutdownOld(oldTp *trace.TracerProvider, oldExp trace.SpanExporter, currentExp trace.SpanExporter) {
	if oldTp != nil {
		if err := shutdownWithTimeout(oldTp, defaultShutdownTimeout); err != nil {
			logger.WarnWithCtx(context.Background(), "[tracer] 关闭旧 TracerProvider 失败", logger.Err(err))
		}
	}
	if oldExp == nil || unwrapExporter(oldExp) == unwrapExporter(currentExp) {
		return
	}
	if err := closeExporter(oldExp, defaultShutdownTimeout); err != nil {
		logger.WarnWithCtx(context.Background(), "[tracer] 关闭旧 exporter 失败", logger.Err(err))
	}
}

// shutdownWithTimeout 对支持 Shutdown(ctx) 的对象执行带超时的关闭。
func shutdownWithTimeout(sd interface{ Shutdown(context.Context) error }, d time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return sd.Shutdown(ctx)
}

// resolveGlobalConfig 从 OTLPOption 列表中提取全局状态接管配置。
func resolveGlobalConfig(opts []OTLPOption) globalConfig {
	o := defaultOTLPOptions()
	for _, opt := range opts {
		opt(o)
	}
	return globalConfig{
		register:   o.registerGlobal,
		propagator: o.propagator,
		handler:    o.errorHandler,
	}
}

// buildBatchOpts 根据 BatchConfig 构建 BatchSpanProcessor 配置。
// 零值字段不生成对应 Option，由 OpenTelemetry SDK 使用默认值。
func buildBatchOpts(cfg BatchConfig) []trace.BatchSpanProcessorOption {
	var batchOpts []trace.BatchSpanProcessorOption
	if cfg.MaxQueueSize > 0 {
		batchOpts = append(batchOpts, trace.WithMaxQueueSize(cfg.MaxQueueSize))
	}
	if cfg.MaxExportBatchSize > 0 {
		batchOpts = append(batchOpts, trace.WithMaxExportBatchSize(cfg.MaxExportBatchSize))
	}
	if cfg.BatchTimeout > 0 {
		batchOpts = append(batchOpts, trace.WithBatchTimeout(cfg.BatchTimeout))
	}
	if cfg.ExportTimeout > 0 {
		batchOpts = append(batchOpts, trace.WithExportTimeout(cfg.ExportTimeout))
	}
	return batchOpts
}
