package goredis

import (
	"context"
	"errors"
	"net"
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

	"github.com/18721889353/sunshine/pkg/logger"
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

// TestEnhanceRedisSpanSetCommand 验证 SET 命令的 Span 名称与诊断属性
func TestEnhanceRedisSpanSetCommand(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "SET", "mykey", "myval")
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	span := stubs[0]
	assert.Equal(t, "redis.set", span.Name)

	assertAttr(t, span.Attributes, "db.system", "redis")
	assertAttr(t, span.Attributes, "db.redis.command", "set")
	assertAttr(t, span.Attributes, "db.operation", "set")
	assertAttr(t, span.Attributes, "db.redis.key", "mykey")
}

// TestEnhanceRedisSpanGetCommand 验证 GET 命令的 key 属性提取
func TestEnhanceRedisSpanGetCommand(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "GET", "mykey")
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.get", stubs[0].Name)
	assertAttr(t, stubs[0].Attributes, "db.redis.key", "mykey")
}

// TestEnhanceRedisSpanEvalshaDistributedLock 验证 evalsha + lock: 前缀识别为分布式锁并按锁名命名
func TestEnhanceRedisSpanEvalshaDistributedLock(t *testing.T) {
	// evalsha SHA1 numkeys key [key ...] arg [arg ...]
	// 索引: 0=evalsha, 1=SHA1, 2=numkeys, 3=key
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123", "1", "lock:order:12345678")
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	span := stubs[0]
	assert.Equal(t, "redis.lock:12345678", span.Name)
	assertAttr(t, span.Attributes, "db.redis.key", "lock:order:12345678")
}

// TestEnhanceRedisSpanEvalshaDlockPrefix 验证 /dlock/ 前缀同样识别为分布式锁
func TestEnhanceRedisSpanEvalshaDlockPrefix(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123", "1", "/dlock/my-lock-key")
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.lock:my-lock-key", stubs[0].Name)
}

// TestEnhanceRedisSpanEvalshaPlainScript 验证 numkeys=0 的普通脚本按命令名命名
func TestEnhanceRedisSpanEvalshaPlainScript(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123def456", "0")
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	// numkeys=0 时无 key，span 名称只显示命令
	assert.Equal(t, "redis.evalsha", stubs[0].Name)
}

// TestEnhanceRedisSpanEvalshaNoKeyWithArgs 验证 numkeys=0 时脚本参数不会被误报为 key
func TestEnhanceRedisSpanEvalshaNoKeyWithArgs(t *testing.T) {
	// evalsha SHA1 0 somearg —— len(args)=4 但 numkeys=0，不应提取为 key
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123def456", "0", "somearg")
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.evalsha", stubs[0].Name, "numkeys=0 时不应按 key 命名")
	for _, a := range stubs[0].Attributes {
		assert.NotEqual(t, attribute.Key("db.redis.key"), a.Key, "numkeys=0 时不应设置 db.redis.key")
	}
}

// TestEnhanceRedisSpanEvalKeyNonLock 验证 eval + 非锁 key 使用命令名命名并提取 key
func TestEnhanceRedisSpanEvalKeyNonLock(t *testing.T) {
	// eval 脚本 + 普通 key（非 lock 前缀）→ redis.eval + key 属性
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "eval", "return redis.call('get', KEYS[1])", "1", "user:1001:name")
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.eval", stubs[0].Name, "非锁 key 应使用命令名命名")
	assertAttr(t, stubs[0].Attributes, "db.redis.key", "user:1001:name")
	assertAttr(t, stubs[0].Attributes, "db.operation", "eval")
}

// TestEnhanceRedisSpanEvalLockPrefix 固化"eval 也识别分布式锁"契约：
// evalsha + lock 与 eval + lock 走同一分支，命令名不同但锁识别一致
func TestEnhanceRedisSpanEvalLockPrefix(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "eval", "return redis.call('set', KEYS[1], ARGV[1])", "1", "lock:order:42")
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "redis.lock:42", stubs[0].Name, "eval + lock 前缀应识别为锁操作")
	assertAttr(t, stubs[0].Attributes, "db.redis.key", "lock:order:42")
	assertAttr(t, stubs[0].Attributes, "db.operation", "eval")
}

// TestEnhanceRedisSpanEvalshaLockNameTruncated 验证超长锁名按上限截断
func TestEnhanceRedisSpanEvalshaLockNameTruncated(t *testing.T) {
	longName := "this-is-a-very-long-lock-name-that-should-be-truncated"
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "evalsha", "abc123", "1", "lock:"+longName)
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	name := stubs[0].Name
	assert.LessOrEqual(t, len(name), len("redis.lock:")+maxLockNameLen)
}

// TestEnhanceRedisSpanKeyTruncated 验证超长 key 按上限截断
func TestEnhanceRedisSpanKeyTruncated(t *testing.T) {
	longKey := make([]byte, 150)
	for i := range longKey {
		longKey[i] = 'a'
	}
	key := string(longKey)

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "GET", key)
		enhanceRedisSpan(ctx, cmd)
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

// TestEnhanceRedisSpanNotRecording 验证非录制 Span 下既不 panic 也不产生副作用
func TestEnhanceRedisSpanNotRecording(t *testing.T) {
	ctx := context.Background()
	cmd := redis.NewCmd(ctx, "SET", "key", "val")
	beforeArgs := append([]any(nil), cmd.Args()...)

	// 已结束的 Span 处于非录制状态（IsRecording()==false），覆盖早退分支
	spanCtx, span := noop.NewTracerProvider().Tracer("test").Start(ctx, "noop-span")
	span.End()

	enhanceRedisSpan(spanCtx, cmd) // 不应 panic
	require.Equal(t, beforeArgs, cmd.Args(), "非录制路径不应修改命令参数")
	require.NoError(t, cmd.Err(), "非录制路径不应设置命令错误")
}

// ============================================================================
// setRequestIDToRedisSpan 测试
// ============================================================================

// TestSetRequestIDToRedisSpanWithRequestID 验证 ctx 携带 request_id 时写入 Span 属性
func TestSetRequestIDToRedisSpanWithRequestID(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		ctx = context.WithValue(ctx, logger.ContextKeyRequestID, "req-001")
		setRequestIDToRedisSpan(ctx)
	})

	require.Len(t, stubs, 1)
	assertAttr(t, stubs[0].Attributes, "request_id", "req-001")
}

// TestSetRequestIDToRedisSpanNoRequestID 验证 ctx 无 request_id 时不设置属性
func TestSetRequestIDToRedisSpanNoRequestID(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		setRequestIDToRedisSpan(ctx)
	})

	require.Len(t, stubs, 1)
	for _, a := range stubs[0].Attributes {
		assert.NotEqual(t, attribute.Key("request_id"), a.Key, "ctx 无 request_id 时不应设置属性")
	}
}

// TestSetRequestIDToRedisSpanEmptyRequestID 验证空 request_id 不上报
func TestSetRequestIDToRedisSpanEmptyRequestID(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		ctx = context.WithValue(ctx, logger.ContextKeyRequestID, "")
		setRequestIDToRedisSpan(ctx)
	})

	require.Len(t, stubs, 1)
	for _, a := range stubs[0].Attributes {
		assert.NotEqual(t, attribute.Key("request_id"), a.Key, "空 request_id 不应上报")
	}
}

// TestSetRequestIDToRedisSpanWrongRequestIDType 验证非 string 类型的 request_id 被安全忽略
func TestSetRequestIDToRedisSpanWrongRequestIDType(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		// 非 string 类型（写入侧异常）应安全忽略而非 panic
		ctx = context.WithValue(ctx, logger.ContextKeyRequestID, 12345)
		setRequestIDToRedisSpan(ctx)
	})

	require.Len(t, stubs, 1)
	for _, a := range stubs[0].Attributes {
		assert.NotEqual(t, attribute.Key("request_id"), a.Key, "非 string 类型不应设置属性")
	}
}

// ============================================================================
// requestIDHook ProcessHook 测试
// ============================================================================

// TestRequestIDHookProcessHookEnhancesAttributes 验证 Hook 设置 request_id 与命令诊断属性
func TestRequestIDHookProcessHookEnhancesAttributes(t *testing.T) {
	hook := &requestIDHook{}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		ctx = context.WithValue(ctx, logger.ContextKeyRequestID, "hook-req-001")
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

// TestRequestIDHookProcessHookRecordsError 验证真实错误被记录为 Error 状态
func TestRequestIDHookProcessHookRecordsError(t *testing.T) {
	hook := &requestIDHook{}

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

// TestRequestIDHookProcessPipelineHookRecordsError 验证 Pipeline 整体错误被记录为 Error 状态
func TestRequestIDHookProcessPipelineHookRecordsError(t *testing.T) {
	hook := &requestIDHook{}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		ctx = context.WithValue(ctx, logger.ContextKeyRequestID, "pipeline-req-001")

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

// TestRequestIDHookProcessPipelineHookSuccess 验证成功时不设置错误状态
func TestRequestIDHookProcessPipelineHookSuccess(t *testing.T) {
	hook := &requestIDHook{}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		ctx = context.WithValue(ctx, logger.ContextKeyRequestID, "pipeline-req-002")

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

// TestRequestIDHookProcessPipelineHookRecordsPerCommandFailure P0-3 回归测试：
// 遍历每个命令，将失败命令以 Span Event 形式记录（哪个命令失败了）
func TestRequestIDHookProcessPipelineHookRecordsPerCommandFailure(t *testing.T) {
	hook := &requestIDHook{}

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

// TestRequestIDHookProcessPipelineHookNoFailureEvents 验证无失败命令时不产生事件
func TestRequestIDHookProcessPipelineHookNoFailureEvents(t *testing.T) {
	hook := &requestIDHook{}

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
// P0-3 回归：redis.Nil 是正常缓存 miss，不计为 Span Error
// ============================================================================

// TestRequestIDHookProcessHookNilNotRecorded 验证 GET 不存在 key 返回 redis.Nil 时
// Span 状态保持 Unset 且不产生 Error Event（生产上 miss 率高，计入错误会打满错误率）
func TestRequestIDHookProcessHookNilNotRecorded(t *testing.T) {
	hook := &requestIDHook{}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "GET", "missing-key")
		err := hook.ProcessHook(func(_ context.Context, cmd redis.Cmder) error {
			cmd.SetErr(redis.Nil)
			return redis.Nil
		})(ctx, cmd)
		assert.ErrorIs(t, err, redis.Nil, "错误本身仍应透传给调用方")
	})

	require.Len(t, stubs, 1)
	span := stubs[0]
	assert.Equal(t, "Unset", span.Status.Code.String(), "缓存 miss 不应把 Span 置为 Error")
	for _, e := range span.Events {
		assert.NotEqual(t, "exception", e.Name, "redis.Nil 不应记录异常事件")
	}
}

// TestRequestIDHookProcessPipelineHookNilNotFailure 验证全 miss 的 Pipeline
// （逐命令与整体错误均为 redis.Nil）不产生失败事件且状态保持 Unset
func TestRequestIDHookProcessPipelineHookNilNotFailure(t *testing.T) {
	hook := &requestIDHook{}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()

		miss1 := redis.NewCmd(ctx, "GET", "miss1")
		miss1.SetErr(redis.Nil)
		miss2 := redis.NewCmd(ctx, "GET", "miss2")
		miss2.SetErr(redis.Nil)
		cmds := []redis.Cmder{miss1, miss2}

		err := hook.ProcessPipelineHook(func(_ context.Context, _ []redis.Cmder) error {
			// go-redis 的 cmdsFirstErr 取首条命令错误且不跳过 Nil，全 miss 时整体 err 即 redis.Nil
			return redis.Nil
		})(ctx, cmds)
		assert.ErrorIs(t, err, redis.Nil)
	})

	require.Len(t, stubs, 1)
	assert.Equal(t, "Unset", stubs[0].Status.Code.String(), "全 miss 不应置 Error")
	assert.Empty(t, stubs[0].Events, "全 miss 不应记录失败事件")
}

// TestRequestIDHookProcessPipelineHookNilFirstRealFailure 验证首条命令 Nil、
// 后续命令真失败时仍置 Error，且状态消息不携带 "redis: nil"（避免误导排障）
func TestRequestIDHookProcessPipelineHookNilFirstRealFailure(t *testing.T) {
	hook := &requestIDHook{}

	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()

		nilCmd := redis.NewCmd(ctx, "GET", "miss")
		nilCmd.SetErr(redis.Nil)
		failCmd := redis.NewCmd(ctx, "SET", "bad", "v")
		failCmd.SetErr(errors.New("WRONGTYPE 错误"))
		cmds := []redis.Cmder{nilCmd, failCmd}

		err := hook.ProcessPipelineHook(func(_ context.Context, _ []redis.Cmder) error {
			return redis.Nil // 模拟 cmdsFirstErr 取到首条命令的 Nil
		})(ctx, cmds)
		assert.ErrorIs(t, err, redis.Nil)
	})

	require.Len(t, stubs, 1)
	span := stubs[0]
	assert.Equal(t, "Error", span.Status.Code.String(), "后续真失败仍应置 Error")
	assert.Equal(t, "redis pipeline command failed", span.Status.Description,
		"状态消息应指向真失败命令，而非透传 redis: nil")

	// 真失败命令应记录事件，Nil 命令不应记录
	var failEvents int
	for _, e := range span.Events {
		if e.Name == "redis.pipeline.command.failed" {
			failEvents++
		}
	}
	assert.Equal(t, 1, failEvents, "仅真失败命令记录事件，redis.Nil 命令跳过")
}

// ============================================================================
// P0-4 回归：参数转字符串处理 []byte
// ============================================================================

// TestArgToString 验证 argToString 的类型分支：string/[]byte/Stringer/default
func TestArgToString(t *testing.T) {
	assert.Equal(t, "hello", argToString("hello"), "string 原样返回")
	assert.Equal(t, "hello", argToString([]byte("hello")), "[]byte 应转为字符串，而非 [104 101 ...] 字节数组")
	assert.Equal(t, "127.0.0.1", argToString(net.IP{127, 0, 0, 1}), "fmt.Stringer 应走 String()")
	assert.Equal(t, "42", argToString(42), "default 分支回退 fmt.Sprintf")
	assert.Equal(t, "<nil>", argToString(nil), "nil 不应 panic")
}

// TestEnhanceRedisSpanByteSliceKey 验证业务传 []byte key 时 Span 属性显示为字符串
func TestEnhanceRedisSpanByteSliceKey(t *testing.T) {
	stubs := recordSpans(t, func(ctx context.Context, tr trace.Tracer) {
		ctx, span := tr.Start(ctx, "test-span")
		defer span.End()
		cmd := redis.NewCmd(ctx, "GET", []byte("user:1001:name"))
		enhanceRedisSpan(ctx, cmd)
	})

	require.Len(t, stubs, 1)
	assertAttr(t, stubs[0].Attributes, "db.redis.key", "user:1001:name")
}

// ============================================================================
// P3 回归：多字节字符（中文/emoji）截断不产生乱码
// ============================================================================

// TestTruncateKeyChineseNoMojibake 按 rune 截断，截断结果必须是合法 UTF-8
func TestTruncateKeyChineseNoMojibake(t *testing.T) {
	key := strings.Repeat("用户昵称", 40) // 160 个 rune，超过 maxKeyDisplayLen=100
	got := truncateKey(key)

	assert.True(t, utf8.ValidString(got), "截断结果必须是合法 UTF-8，不得切断多字节字符")
	assert.Equal(t, maxKeyDisplayLen+3, utf8.RuneCountInString(got), "应为 100 字符 + 省略号")
	assert.True(t, strings.HasSuffix(got, "..."))

	// 未超长时原样返回
	assert.Equal(t, "短键", truncateKey("短键"))
}

// TestTrimLockNameChineseNoMojibake 锁名按 rune 截断
func TestTrimLockNameChineseNoMojibake(t *testing.T) {
	// 超长中文锁名：60 个 rune，应截断为 maxLockNameLen=20 个 rune 且不乱码
	got := trimLockName("lock:" + strings.Repeat("订单锁", 20))
	assert.True(t, utf8.ValidString(got), "截断结果必须是合法 UTF-8")
	assert.Equal(t, maxLockNameLen, utf8.RuneCountInString(got), "应截断为 20 个字符")

	// 未超长锁名：完整保留中文
	assert.Equal(t, "订单锁", trimLockName("lock:订单锁"))
}

// TestTruncateRunesBoundaries 验证截断辅助函数的边界行为
func TestTruncateRunesBoundaries(t *testing.T) {
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
