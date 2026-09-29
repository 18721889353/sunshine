package goredis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
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
	assertAttr(t, span.Attributes, "db.operation", "set")
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

func TestEnhanceRedisSpan_EVALSHA_无Key带参数(t *testing.T) {
	// evalsha SHA1 0 somearg —— len(args)=4 但 numkeys=0，不应提取为 key
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123def456", "0", "somearg")
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.evalsha", stubs[0].Name, "numkeys=0 时不应按 key 命名")
	for _, a := range stubs[0].Attributes {
		assert.NotEqual(t, attribute.Key("db.redis.key"), a.Key, "numkeys=0 时不应设置 db.redis.key")
	}
}

func TestEnhanceRedisSpan_EVAL_带Key非锁(t *testing.T) {
	// eval 脚本 + 普通 key（非 lock 前缀）→ redis.eval + key 属性
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "eval", "return redis.call('get', KEYS[1])", "1", "user:1001:name")
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.eval", stubs[0].Name, "非锁 key 应使用命令名命名")
	assertAttr(t, stubs[0].Attributes, "db.redis.key", "user:1001:name")
	assertAttr(t, stubs[0].Attributes, "db.operation", "eval")
}

// TestEnhanceRedisSpan_EVAL_带锁前缀 固化"eval 也识别分布式锁"契约：
// evalsha + lock 与 eval + lock 走同一分支，命令名不同但锁识别一致
func TestEnhanceRedisSpan_EVAL_带锁前缀(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "eval", "return redis.call('set', KEYS[1], ARGV[1])", "1", "lock:order:42")
		enhanceRedisSpan(ctx, cmd, nil)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.lock:42", stubs[0].Name, "eval + lock 前缀应识别为锁操作")
	assertAttr(t, stubs[0].Attributes, "db.redis.key", "lock:order:42")
	assertAttr(t, stubs[0].Attributes, "db.operation", "eval")
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

// TestEnhanceRedisSpan_不录制的Span 验证非录制 Span 下既不 panic 也不产生副作用
func TestEnhanceRedisSpan_不录制的Span(t *testing.T) {
	ctx := context.Background()
	cmd := redis.NewCmd(ctx, "SET", "key", "val")
	beforeArgs := append([]any(nil), cmd.Args()...)

	// 已结束的 Span 处于非录制状态（IsRecording()==false），覆盖早退分支
	spanCtx, span := noop.NewTracerProvider().Tracer("test").Start(ctx, "noop-span")
	span.End()

	enhanceRedisSpan(spanCtx, cmd, nil) // 不应 panic
	require.Equal(t, beforeArgs, cmd.Args(), "非录制路径不应修改命令参数")
	require.NoError(t, cmd.Err(), "非录制路径不应设置命令错误")
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

// TestRequestIDHook_ProcessPipelineHook_逐命令记录失败 P0-3 回归测试：
// 遍历每个命令，将失败命令以 Span Event 形式记录（哪个命令失败了）
func TestRequestIDHook_ProcessPipelineHook_逐命令记录失败(t *testing.T) {
	hook := &requestIDHook{extractor: nil}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()

		okCmd := redis.NewCmd(ctx, "GET", "okkey")
		failCmd := redis.NewCmd(ctx, "SET", "badkey", "v")
		failCmd.SetErr(errors.New("WRONGTYPE 错误"))
		cmds := []redis.Cmder{okCmd, failCmd}

		err := hook.ProcessPipelineHook(func(ctx context.Context, _ []redis.Cmder) error {
			return errors.New("pipeline error")
		})(ctx, cmds)
		assert.Error(t, err)
	})

	require.Len(t, stubs, 1)
	span := stubs[0]
	// 事件包含：整体错误的 exception + 逐命令的 redis.pipeline.command.failed
	var failEvent *sdktrace.Event
	for i := range span.Events {
		if span.Events[i].Name == "redis.pipeline.command.failed" {
			failEvent = &span.Events[i]
			break
		}
	}
	require.NotNil(t, failEvent, "应记录失败命令事件")

	eventAttrs := make([]attribute.KeyValue, 0, len(failEvent.Attributes))
	eventAttrs = append(eventAttrs, failEvent.Attributes...)
	assertAttr(t, eventAttrs, "db.redis.command", "set")
	assertAttr(t, eventAttrs, "error.message", "WRONGTYPE 错误")
}

// TestRequestIDHook_ProcessPipelineHook_全部成功无失败事件 验证无失败命令时不产生事件
func TestRequestIDHook_ProcessPipelineHook_全部成功无失败事件(t *testing.T) {
	hook := &requestIDHook{extractor: nil}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()

		cmds := []redis.Cmder{
			redis.NewCmd(ctx, "GET", "k1"),
			redis.NewCmd(ctx, "SET", "k2", "v"),
		}

		err := hook.ProcessPipelineHook(func(ctx context.Context, _ []redis.Cmder) error {
			return nil
		})(ctx, cmds)
		assert.NoError(t, err)
	})

	require.Len(t, stubs, 1)
	assert.Empty(t, stubs[0].Events, "全部成功时不应记录失败事件")
}

// ============================================================================
// P3 回归：多字节字符（中文/emoji）截断不产生乱码
// ============================================================================

// TestTruncateKey_中文截断不乱码 按 rune 截断，截断结果必须是合法 UTF-8
func TestTruncateKey_中文截断不乱码(t *testing.T) {
	key := strings.Repeat("用户昵称", 40) // 160 个 rune，超过 maxKeyDisplayLen=100
	got := truncateKey(key)

	assert.True(t, utf8.ValidString(got), "截断结果必须是合法 UTF-8，不得切断多字节字符")
	assert.Equal(t, maxKeyDisplayLen+3, utf8.RuneCountInString(got), "应为 100 字符 + 省略号")
	assert.True(t, strings.HasSuffix(got, "..."))

	// 未超长时原样返回
	assert.Equal(t, "短键", truncateKey("短键"))
}

// TestTrimLockName_中文截断不乱码 锁名按 rune 截断
func TestTrimLockName_中文截断不乱码(t *testing.T) {
	// 超长中文锁名：60 个 rune，应截断为 maxLockNameLen=20 个 rune 且不乱码
	got := trimLockName("lock:" + strings.Repeat("订单锁", 20))
	assert.True(t, utf8.ValidString(got), "截断结果必须是合法 UTF-8")
	assert.Equal(t, maxLockNameLen, utf8.RuneCountInString(got), "应截断为 20 个字符")

	// 未超长锁名：完整保留中文
	assert.Equal(t, "订单锁", trimLockName("lock:订单锁"))
}

// TestTruncateRunes_边界 验证截断辅助函数的边界行为
func TestTruncateRunes_边界(t *testing.T) {
	got, truncated := truncateRunes("abc", 3)
	assert.Equal(t, "abc", got)
	assert.False(t, truncated, "长度等于上限不应截断")

	got, truncated = truncateRunes("abcdef", 3)
	assert.Equal(t, "abc", got)
	assert.True(t, truncated)

	got, truncated = truncateRunes("中文字符", 2)
	assert.Equal(t, "中文", got)
	assert.True(t, truncated)
	assert.True(t, utf8.ValidString(got))
}

// BenchmarkRecordFailedPipelineCmds 基准测试 Pipeline 逐命令失败事件记录（P3-B 热路径）
// 使用 noop Span 衡量遍历与属性构造开销（recording Span 会累积事件导致内存增长）；
// 属性切片在传参时即求值构造，与 span 是否 recording 无关，故 noop 下测得的构造成本依然准确
func BenchmarkRecordFailedPipelineCmds(b *testing.B) {
	ctx := context.Background()
	_, span := noop.NewTracerProvider().Tracer("bench").Start(ctx, "bench-span")
	defer span.End()

	cmds := make([]redis.Cmder, 0, 8)
	for i := 0; i < 8; i++ {
		cmd := redis.NewCmd(ctx, "GET", "key")
		cmd.SetErr(errors.New("WRONGTYPE 错误"))
		cmds = append(cmds, cmd)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recordFailedPipelineCmds(span, cmds)
	}
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
