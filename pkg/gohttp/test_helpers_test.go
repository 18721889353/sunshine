package gohttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// 本文件集中存放跨测试文件共享的 helper（遵循 package-quality-baseline 交付物矩阵）。

// shortTestCooldown 熔断器单测注入的冷却时长。
//   - 为何需要冷却等待：半开探测的状态转换由「真实时钟到达 cooldown」触发，外部无「已到期」信号，
//     只能固定等待后断言；
//   - 取值依据：50ms 远小于单测整体预算，且状态转换是单向的（时间只会变多），等待只会过量不会不足；
//   - 为何不 flaky：断言的是「等待后状态已推进」，不存在上界；先调本常量再考虑其他（基线 4.4 处置顺序）。
const shortTestCooldown = 50 * time.Millisecond

// mustNew 构造客户端并在失败时立刻终止测试；注册 Close 清理
func mustNew(t *testing.T, opts ...Option) *Client {
	t.Helper()
	c, err := New(opts...)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

// newTestServer 启动本地 httptest 服务并注册关闭清理
func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

// newRecordedProvider 返回带内存 SpanRecorder 的 TracerProvider 并注册关闭清理
func newRecordedProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, sr
}

// withW3CPropagator 临时启用 W3C TraceContext 全局传播器（otel 全局默认是 noop，
// 不设置则 traceparent 不会被注入），测试结束恢复原传播器
func withW3CPropagator(t *testing.T) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
}

// spanStatuses 按 End 顺序返回全部已结束 Span 的状态码，便于断言重试各 attempt 的状态序列
func spanStatuses(sr *tracetest.SpanRecorder) []codes.Code {
	ended := sr.Ended()
	out := make([]codes.Code, 0, len(ended))
	for _, s := range ended {
		out = append(out, s.Status().Code)
	}
	return out
}

// spanNames 按 End 顺序返回全部已结束 Span 的名称
func spanNames(sr *tracetest.SpanRecorder) []string {
	ended := sr.Ended()
	out := make([]string, 0, len(ended))
	for _, s := range ended {
		out = append(out, s.Name())
	}
	return out
}

// contextWithRequestID 向 ctx 注入 request_id（与 logger 的上下文键一致）
func contextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, logger.ContextKeyRequestID, id)
}

// panicTracer 在 Start 时立即 panic，用于验证 do() 的 panic 恢复路径
// （resty 的 Execute 捕获后会 re-panic，最终由 do() 的 recover 转为错误返回）
type panicTracer struct{ trace.Tracer }

func (panicTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	panic("tracer boom")
}
