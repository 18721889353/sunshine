package goredis

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// recordSpans 在回调中创建 Span 并记录，返回所有已完成的 SpanStub
// 回调签名: func(ctx context.Context, tr trace.Tracer)
// 调用方需自行创建 Span 并 defer span.End()，确保 Span 被正确记录
func recordSpans(t *testing.T, fn func(ctx context.Context, tr trace.Tracer)) tracetest.SpanStubs {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tr := tp.Tracer("test")
	fn(context.Background(), tr)

	// 将 ReadOnlySpan 转换为 SpanStub 便于断言
	stubs := make(tracetest.SpanStubs, 0, len(sr.Ended()))
	for _, s := range sr.Ended() {
		stubs = append(stubs, tracetest.SpanStubFromReadOnlySpan(s))
	}
	return stubs
}

// ============================================================================
// enhanceRedisSpan 测试
// ============================================================================

func TestEnhanceRedisSpan_SET命令(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "SET", "mykey", "myval")
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	span := stubs[0]
	assert.Equal(t, "redis.set", span.Name)

	assertAttr(t, span.Attributes, "db.system", "redis")
	assertAttr(t, span.Attributes, "db.redis.command", "set")
	assertAttr(t, span.Attributes, "db.redis.key", "mykey")
}

func TestEnhanceRedisSpan_GET命令(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "GET", "mykey")
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.get", stubs[0].Name)
	assertAttr(t, stubs[0].Attributes, "db.redis.key", "mykey")
}

func TestEnhanceRedisSpan_EVALSHA_分布式锁(t *testing.T) {
	// evalsha SHA1 numkeys key [key ...] arg [arg ...]
	// 索引: 0=evalsha, 1=SHA1, 2=numkeys, 3=key
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123", "1", "lock:order:12345678")
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	span := stubs[0]
	assert.Equal(t, "redis.lock:12345678", span.Name)
	assertAttr(t, span.Attributes, "db.redis.key", "lock:order:12345678")
}

func TestEnhanceRedisSpan_EVALSHA_dlock前缀(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123", "1", "/dlock/my-lock-key")
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.lock:my-lock-key", stubs[0].Name)
}

func TestEnhanceRedisSpan_EVALSHA_普通脚本(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123def456", "0")
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	// numkeys=0 时无 key，span 名称只显示命令
	assert.Equal(t, "redis.evalsha", stubs[0].Name)
}

func TestEnhanceRedisSpan_EVALSHA_锁名超长截断(t *testing.T) {
	longName := "this-is-a-very-long-lock-name-that-should-be-truncated"
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123", "1", "lock:"+longName)
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	name := stubs[0].Name
	assert.LessOrEqual(t, len(name), len("redis.lock:")+maxLockNameLen)
}

func TestEnhanceRedisSpan_Key超长截断(t *testing.T) {
	longKey := make([]byte, 150)
	for i := range longKey {
		longKey[i] = 'a'
	}
	key := string(longKey)

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "GET", key)
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	for _, a := range stubs[0].Attributes {
		if a.Key == "db.redis.key" {
			val := a.Value.AsString()
			assert.LessOrEqual(t, len(val), maxKeyDisplayLen+3, "key 应被截断")
			assert.Contains(t, val, "...")
			return
		}
	}
	t.Fatal("未找到 db.redis.key 属性")
}

func TestEnhanceRedisSpan_不录制的Span(t *testing.T) {
	ctx := context.Background()
	cmd := redis.NewCmd(ctx, "SET", "key", "val")
	enhanceRedisSpan(ctx, cmd, nil) // 不应 panic
}

// ============================================================================
// setRequestIDToRedisSpan 测试
// ============================================================================

func TestSetRequestIDToRedisSpan_有提取器(t *testing.T) {
	extractor := func(_ context.Context) string { return "req-001" }

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		setRequestIDToRedisSpan(ctx, extractor)
	})

	require.Len(t, stubs, 1)
	assertAttr(t, stubs[0].Attributes, "request_id", "req-001")
}

func TestSetRequestIDToRedisSpan_无提取器(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		setRequestIDToRedisSpan(ctx, nil)
	})

	require.Len(t, stubs, 1)
	for _, a := range stubs[0].Attributes {
		assert.NotEqual(t, attribute.Key("request_id"), a.Key, "无提取器时不应设置 request_id")
	}
}

func TestSetRequestIDToRedisSpan_提取器返回空(t *testing.T) {
	extractor := func(_ context.Context) string { return "" }

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		setRequestIDToRedisSpan(ctx, extractor)
	})

	require.Len(t, stubs, 1)
	for _, a := range stubs[0].Attributes {
		assert.NotEqual(t, attribute.Key("request_id"), a.Key, "提取器返回空时不应设置 request_id")
	}
}

// ============================================================================
// requestIDHook ProcessHook 测试
// ============================================================================

func TestRequestIDHook_ProcessHook_增强属性(t *testing.T) {
	extractor := func(_ context.Context) string { return "hook-req-001" }
	hook := &requestIDHook{extractor: extractor}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "SET", "testkey", "testval")
		err := hook.ProcessHook(func(ctx context.Context, cmd redis.Cmder) error {
			return nil
		})(ctx, cmd)
		assert.NoError(t, err)
	})

	require.Len(t, stubs, 1)
	span := stubs[0]
	assert.Equal(t, "redis.set", span.Name)
	assertAttr(t, span.Attributes, "request_id", "hook-req-001")
	assertAttr(t, span.Attributes, "db.redis.command", "set")
}

func TestRequestIDHook_ProcessHook_记录错误(t *testing.T) {
	hook := &requestIDHook{extractor: nil}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "GET", "key")
		err := hook.ProcessHook(func(ctx context.Context, cmd redis.Cmder) error {
			return errors.New("test error")
		})(ctx, cmd)
		assert.Error(t, err)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "Error", stubs[0].Status.Code.String())
}

// ============================================================================
// requestIDHook ProcessPipelineHook 测试
// ============================================================================

func TestRequestIDHook_ProcessPipelineHook_记录错误(t *testing.T) {
	extractor := func(_ context.Context) string { return "pipeline-req-001" }
	hook := &requestIDHook{extractor: extractor}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()

		cmds := make([]redis.Cmder, 1)
		cmds[0] = redis.NewCmd(ctx, "GET", "key")

		err := hook.ProcessPipelineHook(func(ctx context.Context, cmds []redis.Cmder) error {
			return errors.New("pipeline error")
		})(ctx, cmds)
		assert.Error(t, err)
	})

	require.Len(t, stubs, 1)
	span := stubs[0]
	assert.Equal(t, "Error", span.Status.Code.String())
	assertAttr(t, span.Attributes, "request_id", "pipeline-req-001")
}

func TestRequestIDHook_ProcessPipelineHook_成功(t *testing.T) {
	extractor := func(_ context.Context) string { return "pipeline-req-002" }
	hook := &requestIDHook{extractor: extractor}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()

		cmds := make([]redis.Cmder, 1)
		cmds[0] = redis.NewCmd(ctx, "GET", "key")

		err := hook.ProcessPipelineHook(func(ctx context.Context, cmds []redis.Cmder) error {
			return nil
		})(ctx, cmds)
		assert.NoError(t, err)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "Unset", stubs[0].Status.Code.String())
	assertAttr(t, stubs[0].Attributes, "request_id", "pipeline-req-002")
}

// ============================================================================
// 辅助函数
// ============================================================================

// assertAttr 断言 Span 属性包含指定的 key-value
func assertAttr(t *testing.T, attrs []attribute.KeyValue, key, expected string) {
	t.Helper()
	for _, a := range attrs {
		if a.Key == attribute.Key(key) {
			assert.Equal(t, expected, a.Value.AsString(), "属性 %s 值不匹配", key)
			return
		}
	}
	t.Errorf("未找到属性 %s", key)
}
