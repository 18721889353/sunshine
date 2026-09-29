package tracer

import (
	"context"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
)

// mockExporter 用于验证 exporter 生命周期管理的内存 exporter，不产生任何真实 I/O。
type mockExporter struct {
	shutdownCount atomic.Int32 // Shutdown 被调用次数，用于断言「恰好关闭一次」
}

func (m *mockExporter) ExportSpans(_ context.Context, _ []trace.ReadOnlySpan) error {
	return nil
}

func (m *mockExporter) Shutdown(_ context.Context) error {
	m.shutdownCount.Add(1)
	return nil
}

// slowShutdownExporter 故意放慢 Shutdown，放大 registerGlobal 在锁外的竞态窗口。
type slowShutdownExporter struct {
	delay time.Duration
}

func (s *slowShutdownExporter) ExportSpans(_ context.Context, _ []trace.ReadOnlySpan) error {
	return nil
}

func (s *slowShutdownExporter) Shutdown(_ context.Context) error {
	time.Sleep(s.delay)
	return nil
}

// cleanupTracer 注册测试结束后的清理：关闭全局 TracerProvider，避免状态泄漏到其它用例。
func cleanupTracer(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if err := Close(context.Background()); err != nil {
			t.Logf("cleanup 阶段 Close 失败: %v", err)
		}
	})
}

// TestInit 验证 Init 正常初始化：默认采样率、显式采样率、越界采样率均可成功。
func TestInit(t *testing.T) {
	cleanupTracer(t)

	exporter, err := newExporter(io.Discard)
	require.NoError(t, err)

	assert.NoError(t, Init(exporter, NewResource()))
	assert.NoError(t, Init(exporter, NewResource(), 0.5))
	assert.NoError(t, Init(exporter, NewResource(), -1.0))
}

// TestInitNilExporter 验证 exporter 为 nil 时 Init 返回错误而不是 panic。
func TestInitNilExporter(t *testing.T) {
	err := Init(nil, NewResource())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exporter")
}

// TestInitNilResource 验证 res 为 nil 时 Init 自动补全默认 Resource，不 panic。
func TestInitNilResource(t *testing.T) {
	cleanupTracer(t)

	exporter, err := newExporter(io.Discard)
	require.NoError(t, err)
	assert.NoError(t, Init(exporter, nil))
}

// TestCloseIdempotent 验证 Close 幂等：首次关闭后再次调用安全返回 nil。
func TestCloseIdempotent(t *testing.T) {
	cleanupTracer(t)

	exporter, err := newExporter(io.Discard)
	require.NoError(t, err)
	require.NoError(t, Init(exporter, NewResource()))
	assert.NotNil(t, GetProvider())

	assert.NoError(t, Close(context.Background()))
	// Close 后 tp 已置 nil，再次 Close 应安全返回 nil
	assert.NoError(t, Close(context.Background()))
}

// TestGetProviderPanicsBeforeInit 验证未初始化时 GetProvider 按约定 panic（编程错误快速失败）。
func TestGetProviderPanicsBeforeInit(t *testing.T) {
	cleanupTracer(t)
	// 先显式关闭，保证 tp 为 nil，不受其它用例执行顺序影响
	require.NoError(t, Close(context.Background()))

	assert.Panics(t, func() { GetProvider() })
}

// TestGetProviderReturnsInstance 验证 Init 后 GetProvider 返回非 nil 实例。
func TestGetProviderReturnsInstance(t *testing.T) {
	cleanupTracer(t)

	exporter, err := newExporter(io.Discard)
	require.NoError(t, err)
	require.NoError(t, Init(exporter, NewResource()))

	assert.NotNil(t, GetProvider())
}

// TestSetSamplingRateBeforeInit 验证未初始化时 SetSamplingRate 是安全 no-op，GetSamplingRate 返回默认 1.0。
func TestSetSamplingRateBeforeInit(t *testing.T) {
	cleanupTracer(t)
	require.NoError(t, Close(context.Background()))

	SetSamplingRate(0.3) // 不应 panic
	assert.Equal(t, 1.0, GetSamplingRate())
}

// TestSetSamplingRateAfterInit 验证初始化后热更新采样率立即生效。
func TestSetSamplingRateAfterInit(t *testing.T) {
	cleanupTracer(t)

	exporter, err := newExporter(io.Discard)
	require.NoError(t, err)
	require.NoError(t, Init(exporter, NewResource(), 1.0))
	assert.Equal(t, 1.0, GetSamplingRate())

	SetSamplingRate(0.3)
	assert.Equal(t, 0.3, GetSamplingRate())

	SetSamplingRate(2.0) // 越界值应被钳位到 1
	assert.Equal(t, 1.0, GetSamplingRate())
}

// TestInitWithOTLPInvalidEndpoint 验证 InitWithOTLP 遇不可达 endpoint 不 panic（连接异步建立），Close 不挂。
func TestInitWithOTLPInvalidEndpoint(t *testing.T) {
	cleanupTracer(t)

	// 192.0.2.1 为 TEST-NET-1 保留地址，不会产生真实流量
	err := InitWithOTLP("svc", "dev", "v1", 1.0, "192.0.2.1:1",
		WithTimeout(1*time.Second), WithInsecure(true))
	assert.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	assert.NoError(t, Close(ctx))
}

// TestInitRegistersGlobalProvider 验证 Init 后全局 otel.TracerProvider 已注册且与包内 tp 一致。
func TestInitRegistersGlobalProvider(t *testing.T) {
	cleanupTracer(t)

	exp, err := newExporter(io.Discard)
	require.NoError(t, err)
	require.NoError(t, Init(exp, NewResource()))

	// 加锁读 tp，与生产代码保持一致
	tpMu.RLock()
	localTp := tp
	tpMu.RUnlock()

	// 从接口提取具体类型后比较指针
	got, ok := otel.GetTracerProvider().(*trace.TracerProvider)
	assert.True(t, ok, "otel.GetTracerProvider() 应返回 *trace.TracerProvider")
	assert.Same(t, localTp, got, "全局 provider 应与本地 tp 一致")
}

// TestInitRace 验证并发 Init 不会导致 installProvider 竞态。
func TestInitRace(t *testing.T) {
	cleanupTracer(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exp, expErr := newExporter(io.Discard)
			if expErr != nil {
				t.Errorf("创建 exporter 失败: %v", expErr)
				return
			}
			if initErr := Init(exp, NewResource(), 1.0); initErr != nil {
				t.Errorf("并发 Init 失败: %v", initErr)
			}
		}()
	}
	wg.Wait()

	// 验证 tp 和 exp 均已正确同步
	tpMu.RLock()
	localTp := tp
	localExp := exp
	tpMu.RUnlock()
	assert.NotNil(t, localTp)
	assert.NotNil(t, localExp)

	got, ok := otel.GetTracerProvider().(*trace.TracerProvider)
	assert.True(t, ok)
	assert.Same(t, localTp, got, "全局 provider 应与本地 tp 一致")
}

// TestInitWithOTLPRace 验证并发 InitWithOTLP 不会导致竞态（覆盖 registerGlobal 路径）。
func TestInitWithOTLPRace(t *testing.T) {
	cleanupTracer(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if initErr := InitWithOTLP("svc", "dev", "v1", 1.0, "192.0.2.1:1",
				WithTimeout(time.Second), WithInsecure(true)); initErr != nil {
				t.Errorf("并发 InitWithOTLP 失败: %v", initErr)
			}
		}()
	}
	wg.Wait()

	// 验证 tp 和 exp 均已正确同步
	tpMu.RLock()
	localTp := tp
	localExp := exp
	tpMu.RUnlock()
	assert.NotNil(t, localTp)
	assert.NotNil(t, localExp)

	got, ok := otel.GetTracerProvider().(*trace.TracerProvider)
	assert.True(t, ok)
	assert.Same(t, localTp, got, "全局 provider 应与本地 tp 一致")
}

// TestInitDifferentExporterShutdown 验证：传入不同 exporter 时旧的被关闭，新的不被误关。
func TestInitDifferentExporterShutdown(t *testing.T) {
	cleanupTracer(t)

	exp1 := &mockExporter{}
	exp2 := &mockExporter{}
	require.NoError(t, Init(exp1, NewResource()))
	require.NoError(t, Init(exp2, NewResource())) // 替换

	// 被替换的旧 exporter 应被本库显式关闭恰好一次（不被 SDK 级联重复关闭）
	assert.Equal(t, int32(1), exp1.shutdownCount.Load(), "被替换的 exporter 应被关闭恰好一次")
	assert.Equal(t, int32(0), exp2.shutdownCount.Load(), "新 exporter 不应被关闭")
}

// TestInstallProviderRegisterGlobalAtomic 验证 registerGlobal 与 tp 赋值是原子的。
// 用 slowShutdownExporter 放大竞态窗口：即使 Shutdown 慢，也不应出现
// tp 与 otel.GetTracerProvider() 不一致的情况。
func TestInstallProviderRegisterGlobalAtomic(t *testing.T) {
	cleanupTracer(t)

	const iterations = 50
	var wg sync.WaitGroup
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exp := &slowShutdownExporter{delay: 1 * time.Millisecond}
			if initErr := Init(exp, NewResource()); initErr != nil {
				t.Errorf("并发 Init 失败: %v", initErr)
			}
		}()
	}
	wg.Wait()

	tpMu.RLock()
	localTp := tp
	tpMu.RUnlock()

	got, ok := otel.GetTracerProvider().(*trace.TracerProvider)
	assert.True(t, ok)
	assert.Same(t, localTp, got,
		"tp 与全局 provider 必须一致；不一致说明 registerGlobal 未与 tp 赋值原子化")
}

// TestInitWithOTLPBatchInvalidEndpoint 验证 InitWithOTLPBatch 传 BatchConfig 零值且 endpoint 不可达时不 panic。
func TestInitWithOTLPBatchInvalidEndpoint(t *testing.T) {
	cleanupTracer(t)

	err := InitWithOTLPBatch("svc", "dev", "v1", 1.0, "192.0.2.1:1",
		BatchConfig{}, // 零值使用 SDK 默认 Batch 参数
		WithTimeout(time.Second), WithInsecure(true))
	assert.NoError(t, err)
}

// TestInitWithOTLPBatchCustomConfig 验证 InitWithOTLPBatch 自定义 Batch 参数路径可正常初始化。
func TestInitWithOTLPBatchCustomConfig(t *testing.T) {
	cleanupTracer(t)

	err := InitWithOTLPBatch("svc", "dev", "v1", 0.5, "192.0.2.1:1",
		BatchConfig{
			MaxQueueSize:       2048,
			MaxExportBatchSize: 512,
			BatchTimeout:       5 * time.Second,
			ExportTimeout:      30 * time.Second,
		},
		WithTimeout(time.Second), WithInsecure(true))
	assert.NoError(t, err)
	assert.Equal(t, 0.5, GetSamplingRate())
}

// TestBuildBatchOpts 验证 buildBatchOpts 的零值跳过与正值映射。
func TestBuildBatchOpts(t *testing.T) {
	// 零值字段不生成任何 Option，交由 SDK 使用默认值
	assert.Empty(t, buildBatchOpts(BatchConfig{}))

	opts := buildBatchOpts(BatchConfig{
		MaxQueueSize:       2048,
		MaxExportBatchSize: 512,
		BatchTimeout:       5 * time.Second,
		ExportTimeout:      30 * time.Second,
	})
	assert.Len(t, opts, 4)
}

// TestResolveGlobalConfig 验证全局接管配置的提取：默认注册全局，选项可覆盖。
func TestResolveGlobalConfig(t *testing.T) {
	cfg := resolveGlobalConfig(nil)
	assert.True(t, cfg.register)
	assert.Nil(t, cfg.propagator)
	assert.Nil(t, cfg.handler)

	handler := otel.ErrorHandlerFunc(func(error) {})
	prop := propagation.Baggage{}
	cfg = resolveGlobalConfig([]OTLPOption{
		WithGlobalRegistration(false),
		WithPropagator(prop),
		WithErrorHandler(handler),
	})
	assert.False(t, cfg.register)
	assert.Equal(t, prop, cfg.propagator)
	assert.NotNil(t, cfg.handler)
}

// TestCloseResetsTraceName 验证 Close 后 traceName 重置为 unknown，避免旧服务名残留。
func TestCloseResetsTraceName(t *testing.T) {
	cleanupTracer(t)

	exp, err := newExporter(io.Discard)
	require.NoError(t, err)
	SetTraceName("my-service")
	assert.Equal(t, "my-service", getTraceName())

	require.NoError(t, Init(exp, NewResource()))
	assert.NoError(t, Close(context.Background()))

	assert.Equal(t, "unknown", getTraceName())
}

// TestCloseRestoresGlobalState 验证 Close 恢复 OTel 全局现场（P0-1 验收）：
// Close 后 otel.GetTracerProvider() 不再指向已关闭实例，propagator/handler 恢复注册前的值。
func TestCloseRestoresGlobalState(t *testing.T) {
	cleanupTracer(t)

	// 预置可辨识的全局现场（用真实 sdk provider 保证指针可比较）
	prevProvider := trace.NewTracerProvider()
	prevPropagator := propagation.Baggage{}
	var prevHandlerCalled atomic.Bool
	prevHandler := otel.ErrorHandlerFunc(func(error) { prevHandlerCalled.Store(true) })
	otel.SetTracerProvider(prevProvider)
	otel.SetTextMapPropagator(prevPropagator)
	otel.SetErrorHandler(prevHandler)
	t.Cleanup(func() {
		_ = prevProvider.Shutdown(context.Background())
	})

	exp := &mockExporter{}
	require.NoError(t, Init(exp, NewResource(), 1.0))

	// 注册期：全局 provider 指向本包的实例
	localTp := GetProvider()
	got, ok := otel.GetTracerProvider().(*trace.TracerProvider)
	require.True(t, ok)
	assert.Same(t, localTp, got, "Init 后全局 provider 应指向本包实例")

	require.NoError(t, Close(context.Background()))

	// P0-1 验收：全局恢复为注册前现场
	got, ok = otel.GetTracerProvider().(*trace.TracerProvider)
	require.True(t, ok)
	assert.Same(t, prevProvider, got, "Close 后全局 provider 应恢复为注册前的实例")
	assert.Equal(t, prevPropagator, otel.GetTextMapPropagator(), "Close 后全局传播器应恢复")

	otel.Handle(assert.AnError)
	assert.True(t, prevHandlerCalled.Load(), "Close 后全局 ErrorHandler 应恢复为注册前的处理器")
}

// TestShutdownExporterExactlyOnce 验证 exporter 恰好关闭一次（P0-2 验收）：
// SDK 的 TracerProvider.Shutdown 会级联调用 exporter.Shutdown，加上本包显式关闭，
// 不得出现重复 Shutdown（非幂等的自定义 exporter 会报错甚至 panic）。
func TestShutdownExporterExactlyOnce(t *testing.T) {
	cleanupTracer(t)

	exp1 := &mockExporter{}
	exp2 := &mockExporter{}
	require.NoError(t, Init(exp1, NewResource(), 1.0))
	require.NoError(t, Init(exp2, NewResource(), 1.0)) // 替换：exp1 应关闭恰好一次
	require.NoError(t, Close(context.Background()))    // 关闭：exp2 应关闭恰好一次

	assert.Equal(t, int32(1), exp1.shutdownCount.Load(), "被替换的 exporter 应恰好关闭一次（不被级联重复关闭）")
	assert.Equal(t, int32(1), exp2.shutdownCount.Load(), "Close 时当前 exporter 应恰好关闭一次")
}

// TestInitReuseExporterNotClosed 验证复用同一 exporter 时旧 provider 的关闭不误关它
// （仍被新 provider 使用，由最后持有者 Close 关闭），保持 Init 契约。
func TestInitReuseExporterNotClosed(t *testing.T) {
	cleanupTracer(t)

	exp := &mockExporter{}
	require.NoError(t, Init(exp, NewResource(), 1.0))
	require.NoError(t, Init(exp, NewResource(), 1.0))

	assert.Equal(t, int32(0), exp.shutdownCount.Load(), "复用同一 exporter 时不应被误关")

	require.NoError(t, Close(context.Background()))
	assert.Equal(t, int32(1), exp.shutdownCount.Load(), "Close 后应被关闭恰好一次")
}

// TestInitWithOTLPInvalidArgs 验证必填参数校验（P0-5）：
// 空 appName 会上错误服务名、空 endpoint 会落到默认端点，都必须显式报错。
func TestInitWithOTLPInvalidArgs(t *testing.T) {
	cleanupTracer(t)

	err := InitWithOTLP("", "dev", "v1", 1.0, "127.0.0.1:4317", WithInsecure(true))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "appName")

	err = InitWithOTLP("svc", "dev", "v1", 1.0, "", WithInsecure(true))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "otlpEndpoint")

	err = InitWithOTLPBatch("", "dev", "v1", 1.0, "127.0.0.1:4317", BatchConfig{}, WithInsecure(true))
	assert.Error(t, err)

	err = InitWithOTLPBatch("svc", "dev", "v1", 1.0, "", BatchConfig{}, WithInsecure(true))
	assert.Error(t, err)
}

// TestProviderSafeAPI 验证 Provider 安全获取（P1-6）：未初始化/已关闭返回 (nil, false) 而不 panic。
func TestProviderSafeAPI(t *testing.T) {
	cleanupTracer(t)
	require.NoError(t, Close(context.Background()))

	p, ok := Provider()
	assert.Nil(t, p)
	assert.False(t, ok)

	exp := &mockExporter{}
	require.NoError(t, Init(exp, NewResource(), 1.0))
	p, ok = Provider()
	assert.True(t, ok)
	assert.NotNil(t, p)

	require.NoError(t, Close(context.Background()))
	p, ok = Provider()
	assert.Nil(t, p)
	assert.False(t, ok)
}

// TestInitOmitsFractionKeepsSamplingRate 验证 Init 缺省采样率时继承当前热更新值，
// 避免 SetSamplingRate 后被缺省 Init 静默重置为全量采样（并发语义竞态的文档化行为）。
func TestInitOmitsFractionKeepsSamplingRate(t *testing.T) {
	cleanupTracer(t)

	exp1 := &mockExporter{}
	require.NoError(t, Init(exp1, NewResource(), 1.0))
	SetSamplingRate(0.3)

	exp2 := &mockExporter{}
	require.NoError(t, Init(exp2, NewResource())) // 缺省 → 继承 0.3
	assert.Equal(t, 0.3, GetSamplingRate())
}

// TestWithGlobalRegistrationDisabled 验证 WithGlobalRegistration(false) 完全不触碰 OTel 全局状态。
func TestWithGlobalRegistrationDisabled(t *testing.T) {
	cleanupTracer(t)

	before := otel.GetTracerProvider()
	prev := trace.NewTracerProvider()
	otel.SetTracerProvider(prev)
	t.Cleanup(func() {
		_ = prev.Shutdown(context.Background())
		otel.SetTracerProvider(before)
	})

	err := InitWithOTLP("svc", "dev", "v1", 1.0, "127.0.0.1:4317",
		WithInsecure(true), WithGlobalRegistration(false))
	require.NoError(t, err)

	got, ok := otel.GetTracerProvider().(*trace.TracerProvider)
	require.True(t, ok)
	assert.Same(t, prev, got, "WithGlobalRegistration(false) 不应覆盖全局 provider")
}

// blockingExporter 模拟「导出端持续缓慢」的采集端：每次导出固定阻塞一段时间，
// 用于验证 BatchSpanProcessor 背压——队列打满后丢弃新 Span，而不是无界堆积内存。
type blockingExporter struct {
	exportDelay time.Duration
	exported    atomic.Int64 // 已导出的 Span 计数（各批次求和）
}

func (b *blockingExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	select {
	case <-time.After(b.exportDelay):
	case <-ctx.Done():
		return ctx.Err()
	}
	b.exported.Add(int64(len(spans)))
	return nil
}

func (b *blockingExporter) Shutdown(_ context.Context) error { return nil }

// TestNewSpanHighConcurrencyMemoryBounded 验证高并发打点后内存可正常回收、无常驻暴涨：
// 阶段一 burst：100 协程 × 1000 次带标签 NewSpan 共 10 万 Span；
// 阶段二 sustained：20 协程持续打点 1 秒，验证「GC 边打边收」的稳态而非仅瞬时 burst。
// 断言口径：GC 后常驻增量 < 50MB 上限（故意放宽避免机器差异 flaky），
// 且常驻增量 < 累计分配量的 1/10（证明「垃圾被回收」而非「分配本身少」），
// 累计分配量 > 10MB（防止 NewSpan 因故不分配导致测试空转失效）。
func TestNewSpanHighConcurrencyMemoryBounded(t *testing.T) {
	cleanupTracer(t)

	exp := &mockExporter{}
	require.NoError(t, Init(exp, NewResource(), 1.0))

	tags := map[string]interface{}{
		"user.id": 1001,
		"ok":      true,
		"detail":  "下单成功",
		"cost":    12.5,
	}

	const (
		goroutines   = 100
		spansPerGoro = 1000
	)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < spansPerGoro; j++ {
				_, span := NewSpan(context.Background(), "stress.span", tags)
				span.End()
			}
		}()
	}
	wg.Wait()

	// 未 GC 的瞬时读数：量化打点产生的「待回收垃圾」规模（真实峰值略高于此）
	var transient runtime.MemStats
	runtime.ReadMemStats(&transient)

	// 连做两次 GC 提高回收收敛性（Go GC 并发，单次返回不保证全部回收）
	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	allocDelta := int64(after.TotalAlloc) - int64(before.TotalAlloc) // 累计分配量，GC 不影响该计数器
	garbage := int64(transient.HeapAlloc) - int64(before.HeapAlloc)
	if garbage < 0 {
		garbage = 0
	}
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if retained < 0 {
		retained = 0
	}
	t.Logf("读数[burst]：并发 %d 协程 × %d Span = %d Span；累计分配=%dKB，瞬时残余垃圾=%dKB（GC 前待回收），GC 后常驻增量=%dKB",
		goroutines, spansPerGoro, goroutines*spansPerGoro, allocDelta/1024, garbage/1024, retained/1024)

	assert.Greater(t, allocDelta, int64(10<<20), "打点应产生可观的累计分配，否则测试本身失效（如采样率意外为 0）")
	assert.Less(t, retained, int64(50<<20), "GC 后常驻堆增量应远小于 50MB，否则存在随打点量累积的泄漏")
	assert.Less(t, retained*10, allocDelta, "常驻增量应远小于累计分配量（<1/10），证明垃圾被回收而非分配本身少")

	// 阶段二 sustained：持续打点 1 秒后再次 GC 测量，
	// 覆盖 README「打点速率再高也不会堆积」结论的稳态口径（而非仅 burst）
	sustainEnd := time.Now().Add(1 * time.Second)
	var wg2 sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			for time.Now().Before(sustainEnd) {
				_, span := NewSpan(context.Background(), "stress.span", tags)
				span.End()
			}
		}()
	}
	wg2.Wait()

	runtime.GC()
	runtime.GC()
	var afterSustain runtime.MemStats
	runtime.ReadMemStats(&afterSustain)
	sustained := int64(afterSustain.HeapAlloc) - int64(before.HeapAlloc)
	if sustained < 0 {
		sustained = 0
	}
	allocSustainDelta := int64(afterSustain.TotalAlloc) - int64(after.TotalAlloc) // sustained 阶段的累计分配量
	t.Logf("读数[sustained]：20 协程持续打点 1s；累计分配=%dMB，GC 后常驻增量=%dKB（稳态，应与 burst 后同量级）",
		allocSustainDelta/1024/1024, sustained/1024)
	assert.Less(t, sustained, int64(50<<20), "持续打点稳态下 GC 后常驻堆增量仍应有界")
	assert.Less(t, sustained*10, allocSustainDelta, "持续打点稳态下同样应证明「垃圾被回收」而非「分配本身少」（常驻 < 累计分配的 1/10）")
}

// TestInitSlowExporterMemoryBounded 验证「导出端持续缓慢」时内存有界（背压生效）：
// 队列收紧为 256 后高并发打 5 万 Span，BatchSpanProcessor 非阻塞入队、打满即丢弃，
// 内存上限由队列容量决定（GC 后常驻增量断言 < 32MB），
// 即背压表现为丢弃 Span 而不是内存暴涨；同时确认导出数远小于打点数（丢弃确实发生）。
// 背压断言口径取「导出数 < 打点数的 1/2」而非「< 总数」：
// 极快机器上导出端可能一次未阻塞就导完小批次，极慢机器上导出时间拉长会导出更多，
// 取半数口径对两端都鲁棒（导出端吞吐上限 64 Span/20ms，5 万打点不可能导出过半）。
func TestInitSlowExporterMemoryBounded(t *testing.T) {
	cleanupTracer(t)

	exp := &blockingExporter{exportDelay: 20 * time.Millisecond}
	// 走内部路径：公共 API 无法同时传 mockExporter（无真实 I/O）与小队列组合，
	// 而背压验证必须两者兼得（mock 慢导出 + 队列收紧才可观测）
	installProvider(exp, NewResource(), "", 1.0, defaultGlobalConfig(),
		buildBatchOpts(BatchConfig{MaxQueueSize: 256, MaxExportBatchSize: 64})...)

	const (
		goroutines   = 100
		spansPerGoro = 500
		total        = goroutines * spansPerGoro
	)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < spansPerGoro; j++ {
				_, span := NewSpan(context.Background(), "stress.span", nil)
				span.End()
			}
		}()
	}
	wg.Wait()

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if retained < 0 {
		retained = 0
	}
	exported := exp.exported.Load()
	t.Logf("读数：打点 %d Span（队列上限 256 / 批次 64 / 单次导出阻塞 20ms）；实际导出 %d Span，GC 后常驻增量=%dKB",
		total, exported, retained/1024)

	assert.Less(t, retained, int64(32<<20), "慢导出下 GC 后常驻堆增量应有界（远小于 32MB）")
	assert.Less(t, exported, int64(total/2), "背压应导致多数 Span 被丢弃（导出数应不足打点数一半），否则背压未生效")
}
