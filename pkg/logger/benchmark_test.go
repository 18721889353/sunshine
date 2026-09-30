package logger

import (
	"context"
	"fmt"
	"testing"
)

// TestMain / isBenchmarkRun / cleanupBenchmarkFiles 已移至 main_test.go（包级 setup/teardown 单独归属，
// 避免本文件未来加构建标签或被拆分时连带 TestMain 丢失）

// BenchmarkInfoWithCtx 基准测试：InfoWithCtx 方法性能（纯 CPU，输出丢弃以隔离磁盘 IO）
func BenchmarkInfoWithCtx(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		// WithNoPrint(true) 丢弃输出：本基准测量日志编码+异步缓冲管线的 CPU/分配开销，不含磁盘 IO
		WithSave(true,
			WithNoPrint(true),
			WithFileName("benchmark.log"),
			WithFileMaxSize(1024), // 1GB，避免轮转
			WithFileMaxBackups(0), // 不备份
			WithFileMaxAge(0),     // 不过期
			WithFileIsCompression(false),
		),
	)

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-req-001")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		InfoWithCtx(ctx, "benchmark info log",
			String("user_id", "user-123"),
			Int("count", i),
		)
	}
}

// BenchmarkErrorWithCtx 基准测试：ErrorWithCtx 方法性能（纯 CPU，输出丢弃以隔离磁盘 IO）
func BenchmarkErrorWithCtx(b *testing.B) {
	Init(
		WithLevel("error"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithNoPrint(true), // 丢弃输出，只测量日志管线 CPU 开销
			WithFileName("benchmark-error.log"),
			WithFileMaxSize(1024), // 1GB
			WithFileMaxBackups(0),
			WithFileMaxAge(0),
		),
	)

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-req-002")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ErrorWithCtx(ctx, "benchmark error log",
			String("operation", "create_order"),
			Int("order_id", i),
		)
	}
}

// BenchmarkModuleLog 基准测试：模块化日志性能（含路由文件写入 IO、非纯 CPU 口径：默认 logger 丢弃、order 路由仍写盘）
func BenchmarkModuleLog(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithNoPrint(true), // 默认 logger 丢弃输出；order 路由 logger 仍写文件（RouteConfig 无丢弃开关），故本基准含路由文件写入 IO
			WithFileName("benchmark-module.log"),
			WithFileMaxSize(1024),
		),
		WithRoutes([]*RouteConfig{
			{
				Module:   "order",
				Filename: "logs/benchmark-order/order.log",
				MaxSize:  1024,
				MaxAge:   0,
				Format:   "json",
				IsAsync:  true,
			},
		}),
	)

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-req-003")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ModuleInfoWithCtx(ctx, "order", "benchmark order log",
			String("order_id", fmt.Sprintf("ORD-%d", i)),
			Int("amount", i*100),
		)
	}
}

// BenchmarkSLSHook 基准测试：SLS Hook 性能（需有效的 SLS 配置）
// 注意：本基准会创建真实 Producer 并向配置的 LogStore 发送日志（含网络 IO），
// 不属于纯 CPU 基准；未配置 SLS 凭据时会自动 Skip，切勿指向生产 LogStore
func BenchmarkSLSHook(b *testing.B) {
	slsConfig := &SLSConfig{
		Endpoint:        getEnv("SLS_ENDPOINT", "cn-shanghai.log.aliyuncs.com"),
		AccessKeyID:     getEnv("SLS_ACCESS_KEY_ID", "test-key"),
		AccessKeySecret: getEnv("SLS_ACCESS_KEY_SECRET", "test-secret"),
		ProjectName:     getEnv("SLS_PROJECT", "test-project"),
		LogStoreName:    getEnv("SLS_LOGSTORE", "test-logstore"),
		Topic:           "benchmark",
		Source:          "benchmark-test",
		MaxRetries:      1,
		Timeout:         10,
	}

	slsHook, err := NewSLSHook(slsConfig)
	if err != nil {
		b.Skipf("SLS Hook creation failed: %v", err)
		return
	}
	defer slsHook.Close()

	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithFileName("benchmark-sls.log"),
			WithFileMaxSize(1024),
		),
		WithCustomHooksWithCtx(slsHook.Hook),
	)

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-req-sls-001")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		InfoWithCtx(ctx, "benchmark SLS log",
			String("iteration", fmt.Sprintf("%d", i)),
			Int("count", i),
		)
	}
}

// BenchmarkConcurrentLogging 基准测试：并发日志性能（含路由文件写入 IO、非纯 CPU 口径）
func BenchmarkConcurrentLogging(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithNoPrint(true), // 默认 logger 丢弃输出；路由 logger 仍写文件，含路由文件写入 IO
			WithFileName("benchmark-concurrent.log"),
			WithFileMaxSize(1024),
		),
		WithRoutes([]*RouteConfig{
			{
				Module:   "order",
				Filename: "logs/benchmark-order/order.log",
				MaxSize:  1024,
				IsAsync:  true,
			},
		}),
	)

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-req-concurrent")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			InfoWithCtx(ctx, "benchmark concurrent log",
				String("goroutine", "parallel"),
				Int("iteration", i),
			)
			ModuleInfoWithCtx(ctx, "order", "benchmark order concurrent",
				String("order_id", fmt.Sprintf("ORD-%d", i)),
			)
			i++
		}
	})
}

// BenchmarkSyncVsAsync 基准测试：同步 vs 异步性能对比
func BenchmarkSyncVsAsync(b *testing.B) {
	b.Run("Sync", func(b *testing.B) {
		Init(
			WithLevel("info"),
			WithFormat("json"),
			WithAsync(false), // 同步模式
			WithSave(true,
				WithNoPrint(true), // 丢弃输出，Sync/Async 对比只反映管线开销而非磁盘 IO
				WithFileName("benchmark-sync.log"),
				WithFileMaxSize(1024),
			),
		)

		ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-sync")

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			InfoWithCtx(ctx, "benchmark sync log",
				Int("iteration", i),
			)
		}
	})

	b.Run("Async", func(b *testing.B) {
		Init(
			WithLevel("info"),
			WithFormat("json"),
			WithAsync(true), // 异步模式
			WithSave(true,
				WithNoPrint(true), // 丢弃输出，Sync/Async 对比只反映管线开销而非磁盘 IO
				WithFileName("benchmark-async.log"),
				WithFileMaxSize(1024),
			),
		)

		ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-async")

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			InfoWithCtx(ctx, "benchmark async log",
				Int("iteration", i),
			)
		}
	})
}

// BenchmarkContextExtraction 基准测试：Context 字段提取性能（纯 CPU，输出丢弃以隔离磁盘 IO）
func BenchmarkContextExtraction(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithNoPrint(true), // 丢弃输出，专注测量 context 字段提取开销
			WithFileName("benchmark-ctx.log"),
			WithFileMaxSize(1024),
		),
	)

	b.Run("WithRequestID", func(b *testing.B) {
		ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-req-001")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			InfoWithCtx(ctx, "benchmark with request_id",
				Int("iteration", i),
			)
		}
	})

	b.Run("EmptyContext", func(b *testing.B) {
		ctx := context.Background()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			InfoWithCtx(ctx, "benchmark empty context",
				Int("iteration", i),
			)
		}
	})
}

// BenchmarkRouteLookup 基准测试：路由查找性能（含路由文件写入 IO、非纯 CPU 口径）
func BenchmarkRouteLookup(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithNoPrint(true), // 默认 logger 丢弃输出；路由 logger 仍写文件，含路由文件写入 IO
			WithFileName("benchmark-route.log"),
			WithFileMaxSize(1024),
		),
		WithRoutes([]*RouteConfig{
			{
				Module:   "order",
				Filename: "logs/benchmark-order/order.log",
				MaxSize:  1024,
				IsAsync:  true,
			},
			{
				Module:   "payment",
				Filename: "logs/benchmark-payment/payment.log",
				MaxSize:  1024,
				IsAsync:  true,
			},
			{
				Module:   "user",
				Filename: "logs/benchmark-user/user.log",
				MaxSize:  1024,
				IsAsync:  true,
			},
		}),
	)

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-route")

	b.Run("ModuleOrder", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ModuleInfoWithCtx(ctx, "order", "benchmark order",
				Int("iteration", i),
			)
		}
	})

	b.Run("ModulePayment", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ModuleInfoWithCtx(ctx, "payment", "benchmark payment",
				Int("iteration", i),
			)
		}
	})

	b.Run("ModuleDefault", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			InfoWithCtx(ctx, "benchmark default",
				Int("iteration", i),
			)
		}
	})
}

// BenchmarkHighConcurrency 基准测试：高并发场景（含路由文件写入 IO、非纯 CPU 口径）
func BenchmarkHighConcurrency(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithNoPrint(true), // 默认 logger 丢弃输出；路由 logger 仍写文件，含路由文件写入 IO
			WithFileName("benchmark-high-concurrency.log"),
			WithFileMaxSize(1024),
			WithFileMaxBackups(0),
			WithFileMaxAge(0),
		),
		WithRoutes([]*RouteConfig{
			{
				Module:   "order",
				Filename: "logs/benchmark-order/order.log",
				MaxSize:  1024,
				IsAsync:  true,
			},
			{
				Module:   "payment",
				Filename: "logs/benchmark-payment/payment.log",
				MaxSize:  1024,
				IsAsync:  true,
			},
		}),
	)

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-high-concurrent")

	b.ResetTimer()
	// 用 RunParallel 让 benchmark 框架自行决定并发度与每个 P 的迭代数，
	// 避免手动 b.N/concurrency 在 b.N<concurrency 时 perGoroutine=0 产生“假基准”
	b.RunParallel(func(pb *testing.PB) {
		i := 0 // 每个 P 独立的局部计数，避免跨 goroutine 共享产生数据竞争
		for pb.Next() {
			InfoWithCtx(ctx, "high concurrency log",
				Int("iteration", i),
			)
			ModuleInfoWithCtx(ctx, "order", "order log",
				String("order_id", fmt.Sprintf("ORD-%d", i)),
			)
			i++
		}
	})
}

// BenchmarkMixedScenarios 基准测试：混合场景（多种日志类型 + 路由 + SLS）
// 注意：当 SLS 凭据可用时会接真实 Hook（含网络 IO）；无凭据时 Skip。不属于纯 CPU 基准，切勿指向生产
func BenchmarkMixedScenarios(b *testing.B) {
	slsConfig := &SLSConfig{
		Endpoint:        getEnv("SLS_ENDPOINT", "cn-shanghai.log.aliyuncs.com"),
		AccessKeyID:     getEnv("SLS_ACCESS_KEY_ID", "test-key"),
		AccessKeySecret: getEnv("SLS_ACCESS_KEY_SECRET", "test-secret"),
		ProjectName:     getEnv("SLS_PROJECT", "test-project"),
		LogStoreName:    getEnv("SLS_LOGSTORE", "test-logstore"),
		Topic:           "benchmark-mixed",
		Source:          "benchmark-test",
		MaxRetries:      1,
		Timeout:         10,
	}

	slsHook, err := NewSLSHook(slsConfig)
	hasSLS := err == nil
	if !hasSLS {
		b.Skipf("SLS Hook creation failed (will skip SLS): %v", err)
	} else {
		defer slsHook.Close()
	}

	opts := []Option{
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithNoPrint(true), // 默认 logger 丢弃输出；路由 logger 仍写文件，含路由文件写入 IO（叠加 SLS 网络 IO）
			WithFileName("benchmark-mixed.log"),
			WithFileMaxSize(1024),
		),
		WithRoutes([]*RouteConfig{
			{
				Module:   "order",
				Filename: "logs/benchmark-order/order.log",
				MaxSize:  1024,
				IsAsync:  true,
			},
			{
				Module:   "payment",
				Filename: "logs/benchmark-payment/payment.log",
				MaxSize:  1024,
				IsAsync:  true,
			},
		}),
	}

	if hasSLS {
		opts = append(opts, WithCustomHooksWithCtx(slsHook.Hook))
	}

	Init(opts...)

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "bench-mixed")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// 默认日志
			InfoWithCtx(ctx, "mixed scenario log",
				Int("iteration", i),
			)

			// 路由日志 - order
			ModuleInfoWithCtx(ctx, "order", "order created",
				String("order_id", fmt.Sprintf("ORD-%d", i)),
				Int("amount", i*100),
			)

			// 路由日志 - payment
			if i%5 == 0 {
				ModuleErrorWithCtx(ctx, "payment", "payment failed",
					Err(fmt.Errorf("timeout")),
					String("order_id", fmt.Sprintf("ORD-%d", i)),
				)
			}

			i++
		}
	})
}

// TestMain、isBenchmarkRun、cleanupBenchmarkFiles 见 main_test.go
