package dlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/go-redsync/redsync/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// 说明：dlockTracer = otel.Tracer("dlock") 走全局代理，而全局 TracerProvider 一旦
// 被某 tracer 首次使用即绑定，后续 SetTracerProvider 不再回绑既有 tracer。
// 故这里用 TestMain 在整个测试进程内仅设置一次全局 Provider + 共享 SpanRecorder，
// 各用例通过截取“新增 span”做断言，避免逐用例切换 Provider 导致只首个生效。
var testRecorder *tracetest.SpanRecorder

func TestMain(m *testing.M) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	testRecorder = sr
	code := m.Run()
	_ = tp.Shutdown(context.Background())
	os.Exit(code)
}

// captureDlockSpans 执行 fn（内部创建并 End span），返回本次新增的已结束 span。
func captureDlockSpans(t *testing.T, fn func()) tracetest.SpanStubs {
	t.Helper()
	prev := len(testRecorder.Ended())
	fn()
	return toStubs(testRecorder.Ended()[prev:])
}

func toStubs(spans []sdktrace.ReadOnlySpan) tracetest.SpanStubs {
	stubs := make(tracetest.SpanStubs, 0, len(spans))
	for _, s := range spans {
		stubs = append(stubs, tracetest.SpanStubFromReadOnlySpan(s))
	}
	return stubs
}

func assertDlockAttr(t *testing.T, attrs []attribute.KeyValue, key, expected string) {
	t.Helper()
	for _, a := range attrs {
		if a.Key == attribute.Key(key) {
			assert.Equal(t, expected, a.Value.AsString(), "属性 %s 值不匹配", key)
			return
		}
	}
	t.Errorf("未找到属性 %s", key)
}

// TestDlockLockSpanSuccess 断言 Lock 成功 span：名 dlock.lock、Kind Client、含 dlock.key、状态 OK。
func TestDlockLockSpanSuccess(t *testing.T) {
	stubs := captureDlockSpans(t, func() {
		_, span := startLockSpan(context.Background(), "lock", "order:lock:42")
		markLock(span, nil)
		span.End()
	})

	require.Len(t, stubs, 1)
	s := stubs[0]
	assert.Equal(t, "dlock.lock", s.Name)
	assert.Equal(t, trace.SpanKindClient, s.SpanKind)
	assertDlockAttr(t, s.Attributes, "dlock.key", "order:lock:42")
	assertDlockAttr(t, s.Attributes, "dlock.operation", "lock")
	assert.Equal(t, "Ok", s.Status.Code.String())
}

// TestDlockLockSpanError 断言真故障标 Error 且状态描述为错误文本。
func TestDlockLockSpanError(t *testing.T) {
	stubs := captureDlockSpans(t, func() {
		_, span := startLockSpan(context.Background(), "unlock", "k")
		markLock(span, errors.New("redis down"))
		span.End()
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "dlock.unlock", stubs[0].Name)
	assert.Equal(t, "Error", stubs[0].Status.Code.String())
	assert.Equal(t, "redis down", stubs[0].Status.Description)
}

// TestDlockTryLockAcquired 断言抢到锁：dlock.acquired=true 且状态 OK。
func TestDlockTryLockAcquired(t *testing.T) {
	stubs := captureDlockSpans(t, func() {
		_, span := startLockSpan(context.Background(), "trylock", "k")
		markTryLock(span, true, nil)
		span.End()
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "dlock.trylock", stubs[0].Name)
	assert.Contains(t, stubs[0].Attributes, attribute.Bool("dlock.acquired", true))
	assert.Equal(t, "Ok", stubs[0].Status.Code.String())
}

// TestDlockTryLockContentionNotError 断言“未抢到锁”属正常竞争结果标 OK，不染红。
func TestDlockTryLockContentionNotError(t *testing.T) {
	stubs := captureDlockSpans(t, func() {
		_, span := startLockSpan(context.Background(), "trylock", "k")
		markTryLock(span, false, &redsync.ErrTaken{Nodes: []int{0}})
		span.End()
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "Ok", stubs[0].Status.Code.String(), "正常锁竞争应标 OK 而非 Error")
	assert.Contains(t, stubs[0].Attributes, attribute.Bool("dlock.acquired", false))
}

// TestDlockTryLockRealFailureIsError 断言非竞争的真故障仍标 Error。
func TestDlockTryLockRealFailureIsError(t *testing.T) {
	stubs := captureDlockSpans(t, func() {
		_, span := startLockSpan(context.Background(), "trylock", "k")
		markTryLock(span, false, &redsync.RedisError{Node: 0, Err: errors.New("conn refused")})
		span.End()
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "Error", stubs[0].Status.Code.String())
}

// TestIsLockContention 覆盖 redsync v4.13.0 争用错误模型的类型匹配。
func TestIsLockContention(t *testing.T) {
	assert.True(t, isLockContention(&redsync.ErrTaken{Nodes: []int{0, 1}}), "quorum 争用 *ErrTaken")
	assert.True(t, isLockContention(&redsync.ErrNodeTaken{Node: 0}), "单节点占用 *ErrNodeTaken")

	// 包装链透传：redsync 实际会将 *ErrTaken 逐层包裹（multierror / fmt.Errorf）后返回。
	// ErrTaken 的 Error() 为值接收器，%w 直接接 *ErrTaken 具名指针会被 printf 校验提醒会削弱
	// errors.Is 的值语义；本函数走 errors.As 匹配指针类型，与 redsync 返回的 *ErrTaken 一致，
	// 因此先用 error 接口承载再包装，行为不变且不触告警。
	var contention error = &redsync.ErrTaken{Nodes: []int{0}}
	assert.True(t,
		isLockContention(fmt.Errorf("wrap: %w", contention)),
		"包装后的 *ErrTaken 应经 errors.As 透传命中")
	assert.True(t,
		isLockContention(errors.Join(contention, errors.New("node #1: lock already taken"))),
		"多错误链中的 *ErrTaken 也应被 errors.As 命中")

	assert.False(t, isLockContention(errors.New("network timeout")), "普通错误非竞争")
	assert.False(t, isLockContention(&redsync.RedisError{Node: 0, Err: errors.New("io")}), "节点通信故障非竞争")
}
