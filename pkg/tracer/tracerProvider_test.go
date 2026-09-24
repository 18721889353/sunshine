package tracer

import (
	"context"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
)

func TestInit(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	exporter, err := newExporter(os.Stdout)
	assert.NoError(t, err)
	resource := NewResource()

	assert.NoError(t, Init(exporter, resource))
	assert.NoError(t, Init(exporter, resource, 0.5))
	assert.NoError(t, Init(exporter, resource, -1.0))
}

func TestInit_NilExporter(t *testing.T) {
	err := Init(nil, NewResource())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exporter")
}

func TestInit_NilResource(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	exporter, err := newExporter(os.Stdout)
	assert.NoError(t, err)
	// res == nil 时应自动补全，不应 panic
	assert.NoError(t, Init(exporter, nil))
}

func TestClose(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	exporter, err := newExporter(os.Stdout)
	assert.NoError(t, err)
	resource := NewResource()
	assert.NoError(t, Init(exporter, resource))
	GetProvider()

	_ = Close(context.Background())

	// Close 后 tp 已置 nil，再次 Close 应安全返回 nil
	_ = Close(context.Background())
}

func TestInitWithOTLP_InvalidEndpoint(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	// 不可达 endpoint 不应 panic（OTLP exporter 创建是异步的）
	err := InitWithOTLP("svc", "dev", "v1", 1.0, "192.0.2.1:1",
		WithTimeout(1*time.Second), WithInsecure(true))
	// OTLP exporter 创建成功（连接异步），但不 panic 即可
	assert.NoError(t, err)

	// 验证 Close 不挂
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = Close(ctx)
	assert.NoError(t, err)
}

// TestInit_RegistersGlobalProvider 验证 Init 后全局 otel.TracerProvider 已注册。
func TestInit_RegistersGlobalProvider(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	exp, _ := newExporter(io.Discard)
	assert.NoError(t, Init(exp, NewResource()))

	// 加锁读 tp，与生产代码保持一致
	tpMu.RLock()
	localTp := tp
	tpMu.RUnlock()

	// 从接口提取具体类型后比较指针
	got, ok := otel.GetTracerProvider().(*trace.TracerProvider)
	assert.True(t, ok, "otel.GetTracerProvider() 应返回 *trace.TracerProvider")
	assert.Same(t, localTp, got, "全局 provider 应与本地 tp 一致")
}

// TestInit_Race 验证并发 Init 不会导致 installProvider 竞态。
func TestInit_Race(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exp, _ := newExporter(io.Discard)
			_ = Init(exp, NewResource(), 1.0)
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

// TestInitWithOTLP_Race 验证并发 InitWithOTLP 不会导致竞态（覆盖 registerGlobal 路径）。
func TestInitWithOTLP_Race(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = InitWithOTLP("svc", "dev", "v1", 1.0, "192.0.2.1:1",
				WithTimeout(time.Second), WithInsecure(true))
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

// mockExporter 用于验证 exporter 生命周期管理。
type mockExporter struct {
	shutdownCalled atomic.Bool
}

func (m *mockExporter) ExportSpans(_ context.Context, _ []trace.ReadOnlySpan) error {
	return nil
}

func (m *mockExporter) Shutdown(_ context.Context) error {
	m.shutdownCalled.Store(true)
	return nil
}

// TestInit_DifferentExporterShutdown 验证：传入不同 exporter 时，旧的应被关闭。
func TestInit_DifferentExporterShutdown(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	exp1 := &mockExporter{}
	exp2 := &mockExporter{}
	assert.NoError(t, Init(exp1, NewResource()))
	assert.NoError(t, Init(exp2, NewResource())) // 替换

	// 被替换的旧 exporter 应被本库显式关闭（shutdownOld 同步调用）
	assert.True(t, exp1.shutdownCalled.Load(), "被替换的 exporter 应被关闭")
	assert.False(t, exp2.shutdownCalled.Load(), "新 exporter 不应被关闭")
}

// TestInstallProvider_RegisterGlobalAtomic 验证 registerGlobal 与 tp 赋值是原子的。
// slowShutdownExporter 故意放慢 Shutdown，放大 registerGlobal 在锁外的竞态窗口。
// 修复后 registerGlobal 在锁内，即使 Shutdown 慢也不会出现
// tp 与 otel.GetTracerProvider() 不一致的情况。
func TestInstallProvider_RegisterGlobalAtomic(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	const N = 50
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			exp := &slowShutdownExporter{delay: 1 * time.Millisecond}
			_ = Init(exp, NewResource())
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

// TestInitWithOTLPBatch_InvalidEndpoint 验证 InitWithOTLPBatch 不可达 endpoint 不 panic。
func TestInitWithOTLPBatch_InvalidEndpoint(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	err := InitWithOTLPBatch("svc", "dev", "v1", 1.0, "192.0.2.1:1",
		0, 0, 0, 0, // 使用默认 Batch 参数
		WithTimeout(time.Second), WithInsecure(true))
	assert.NoError(t, err)
}

func TestClose_ResetsTraceName(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	exp, _ := newExporter(os.Stdout)
	SetTraceName("my-service")
	assert.Equal(t, "my-service", getTraceName())

	assert.NoError(t, Init(exp, NewResource()))
	_ = Close(context.Background())

	assert.Equal(t, "unknown", getTraceName())
}
