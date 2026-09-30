package goredis

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/18721889353/sunshine/pkg/logger"
)

// 本文件是 goredis 的性能基线（Benchmark）。
//
// 运行方式：
//
//	go test -bench=. -benchmem -run='^$' ./pkg/goredis/
//
// 口径说明：
//   - DSN 解析、Span 文本增强等用例是纯 CPU 开销，可直接用于回归「改动是否让热路径变慢」；
//   - 端到端用例基于 miniredis，包含回环网络往返，只能比较相对变化，不是生产绝对延迟；
//   - 复用同一个 Span 做循环：目的是隔离本包逻辑的开销，SetAttributes 对同键是覆盖语义，
//     因此属性数量不会随迭代次数增长。

// benchRequestIDCtx 构造带 request_id 的上下文，贴近线上请求链路的真实形态。
func benchRequestIDCtx() context.Context {
	return context.WithValue(context.Background(), logger.ContextKeyRequestID, "req-benchmark-id-0123456789")
}

// BenchmarkGetRedisOptDSNParse 测量 DSN 归一化 + 解析开销，覆盖三种常见写法。
// 该路径在配置热更新重建客户端时执行，不在每条命令上，因此成本可接受但要避免无谓放大。
func BenchmarkGetRedisOptDSNParse(b *testing.B) {
	cases := []struct {
		name string
		dsn  string
	}{
		{"仅地址", "127.0.0.1:6379"},
		{"带密码与库号", "user:pass@127.0.0.1:6379/2"},
		{"完整URL带查询", "redis://default:123456@localhost:6379/0?max_retries=3&dial_timeout=3s"},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				opt, err := getRedisOpt(c.dsn, defaultOptions())
				if err != nil {
					b.Fatalf("DSN 解析失败: dsn=%q err=%v", c.dsn, err)
				}
				if opt.Addr == "" {
					b.Fatalf("解析结果地址为空: dsn=%q", c.dsn)
				}
			}
		})
	}
}

// BenchmarkEnsureDSNPathNormalize 测量纯字符串补全开销（不经过 net/url 往返是它的设计目标）。
func BenchmarkEnsureDSNPathNormalize(b *testing.B) {
	cases := []struct {
		name string
		dsn  string
	}{
		{"需补全", "redis://127.0.0.1:6379"},
		{"已含路径", "redis://127.0.0.1:6379/2"},
		{"带查询需补全", "redis://user:pwd@host:6379?dial_timeout=3s"},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := ensureDSNPath(c.dsn); got == "" {
					b.Fatalf("补全结果不应为空: dsn=%q", c.dsn)
				}
			}
		})
	}
}

// BenchmarkEnhanceRedisSpanTracingState 对比「未开启追踪」与「开启追踪但不导出」两种部署形态下
// 每条命令的 Span 增强成本：前者应只有一次 SpanFromContext + IsRecording 判断。
func BenchmarkEnhanceRedisSpanTracingState(b *testing.B) {
	b.Run("未开启追踪", func(b *testing.B) {
		tr := noop.NewTracerProvider().Tracer("bench")
		ctx, span := tr.Start(context.Background(), "redis.set")
		defer span.End()
		cmd := redis.NewCmd(ctx, "SET", "user:1001:name", "v")

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			enhanceRedisSpan(ctx, cmd)
		}
	})

	b.Run("开启追踪不导出", func(b *testing.B) {
		tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
		defer func() { _ = tp.Shutdown(context.Background()) }()
		tr := tp.Tracer("bench")
		ctx, span := tr.Start(context.Background(), "redis.set")
		cmd := redis.NewCmd(ctx, "SET", "user:1001:name", "v")

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			enhanceRedisSpan(ctx, cmd)
		}
		span.End()
	})

	b.Run("脚本命令走锁分支", func(b *testing.B) {
		tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
		defer func() { _ = tp.Shutdown(context.Background()) }()
		tr := tp.Tracer("bench")
		ctx, span := tr.Start(context.Background(), "redis.evalsha")
		cmd := redis.NewCmd(ctx, "evalsha", "abc123def456", "1", "lock:order:12345678")

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			enhanceRedisSpan(ctx, cmd)
		}
		span.End()
	})
}

// BenchmarkSetRequestIDToRedisSpanRequestIDExtract 测量从 context 提取 request_id 并写属性的成本差异。
func BenchmarkSetRequestIDToRedisSpanRequestIDExtract(b *testing.B) {
	b.Run("ctx带request_id", func(b *testing.B) {
		tr := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample())).Tracer("bench")
		ctx, span := tr.Start(benchRequestIDCtx(), "redis.set")
		defer span.End()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			setRequestIDToRedisSpan(ctx)
		}
	})

	b.Run("ctx无request_id", func(b *testing.B) {
		tr := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample())).Tracer("bench")
		ctx, span := tr.Start(context.Background(), "redis.set")
		defer span.End()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			setRequestIDToRedisSpan(ctx)
		}
	})
}

// BenchmarkTruncateKeyLengths 测量按 rune 截断的开销。
// 长 Key 分支会构造 []rune 并复制，是「Key 很长时每条命令都要付」的成本，故需盯住不劣化。
func BenchmarkTruncateKeyLengths(b *testing.B) {
	cases := []struct {
		name string
		key  string
	}{
		{"短Key", "user:1001:name"},
		{"恰好上限", strings.Repeat("a", maxKeyDisplayLen)},
		{"超长截断", strings.Repeat("a", maxKeyDisplayLen*4)},
		{"中文超长截断", strings.Repeat("汉", maxKeyDisplayLen*4)},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := truncateKey(c.key); got == "" {
					b.Fatal("截断结果不应为空")
				}
			}
		})
	}
}

// BenchmarkCommandEndToEndMiniredis 测量「一条命令穿过全部 Hook」的相对开销（含 miniredis 回环 RTT）。
// 与纯函数用例的区别：这里包含 go-redis 自身的编解码与网络，用于观察 Hook 叠加后的整体量级。
func BenchmarkCommandEndToEndMiniredis(b *testing.B) {
	server, err := miniredis.Run()
	if err != nil {
		b.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer server.Close()

	rdb, err := Init(server.Addr(), WithPoolSize(10), WithDialTimeout(time.Second), WithReadTimeout(time.Second))
	if err != nil {
		b.Fatalf("初始化客户端失败: %v", err)
	}
	defer func() { _ = Close(rdb) }()

	cases := []struct {
		name string
		ctx  context.Context
	}{
		{"无request_id", context.Background()},
		{"带request_id", benchRequestIDCtx()},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			value := strings.Repeat("v", 128)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if setErr := rdb.Set(c.ctx, "bench:key", value, 0).Err(); setErr != nil {
					b.Fatalf("SET 失败: %v", setErr)
				}
				if _, getErr := rdb.Get(c.ctx, "bench:key").Result(); getErr != nil {
					b.Fatalf("GET 失败: %v", getErr)
				}
			}
		})
	}
}
