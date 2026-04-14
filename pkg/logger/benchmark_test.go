package logger

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
)

// BenchmarkInfoWithCtx 基准测试：InfoWithCtx 方法性能
func BenchmarkInfoWithCtx(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithFileName("benchmark.log"),
			WithFileMaxSize(1024), // 1GB，避免轮转
			WithFileMaxBackups(0), // 不备份
			WithFileMaxAge(0),     // 不过期
			WithFileIsCompression(false),
		),
	)

	ctx := context.WithValue(context.Background(), "request_id", "bench-req-001")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		InfoWithCtx(ctx, "benchmark info log",
			String("user_id", "user-123"),
			Int("count", i),
		)
	}
}

// BenchmarkErrorWithCtx 基准测试：ErrorWithCtx 方法性能
func BenchmarkErrorWithCtx(b *testing.B) {
	Init(
		WithLevel("error"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithFileName("benchmark-error.log"),
			WithFileMaxSize(1024), // 1GB
			WithFileMaxBackups(0),
			WithFileMaxAge(0),
		),
	)

	ctx := context.WithValue(context.Background(), "request_id", "bench-req-002")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ErrorWithCtx(ctx, "benchmark error log",
			String("operation", "create_order"),
			Int("order_id", i),
		)
	}
}

// BenchmarkModuleLog 基准测试：模块化日志性能
func BenchmarkModuleLog(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
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

	ctx := context.WithValue(context.Background(), "request_id", "bench-req-003")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ModuleInfoWithCtx(ctx, "order", "benchmark order log",
			String("order_id", fmt.Sprintf("ORD-%d", i)),
			Int("amount", i*100),
		)
	}
}

// BenchmarkSLSHook 基准测试：SLS Hook 性能（需要有效的 SLS 配置）
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

	ctx := context.WithValue(context.Background(), "request_id", "bench-req-sls-001")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		InfoWithCtx(ctx, "benchmark SLS log",
			String("iteration", fmt.Sprintf("%d", i)),
			Int("count", i),
		)
	}
}

// BenchmarkConcurrentLogging 基准测试：并发日志性能
func BenchmarkConcurrentLogging(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
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

	ctx := context.WithValue(context.Background(), "request_id", "bench-req-concurrent")

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
				WithFileName("benchmark-sync.log"),
				WithFileMaxSize(1024),
			),
		)

		ctx := context.WithValue(context.Background(), "request_id", "bench-sync")

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
				WithFileName("benchmark-async.log"),
				WithFileMaxSize(1024),
			),
		)

		ctx := context.WithValue(context.Background(), "request_id", "bench-async")

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			InfoWithCtx(ctx, "benchmark async log",
				Int("iteration", i),
			)
		}
	})
}

// BenchmarkContextExtraction 基准测试：Context 字段提取性能
func BenchmarkContextExtraction(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
			WithFileName("benchmark-ctx.log"),
			WithFileMaxSize(1024),
		),
	)

	b.Run("WithRequestID", func(b *testing.B) {
		ctx := context.WithValue(context.Background(), "request_id", "bench-req-001")
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

// BenchmarkRouteLookup 基准测试：路由查找性能
func BenchmarkRouteLookup(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
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

	ctx := context.WithValue(context.Background(), "request_id", "bench-route")

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

// BenchmarkHighConcurrency 基准测试：高并发场景（100个goroutine）
func BenchmarkHighConcurrency(b *testing.B) {
	Init(
		WithLevel("info"),
		WithFormat("json"),
		WithAsync(true),
		WithSave(true,
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

	ctx := context.WithValue(context.Background(), "request_id", "bench-high-concurrent")

	b.ResetTimer()
	// 模拟100个并发 goroutine
	var wg sync.WaitGroup
	concurrency := 100
	perGoroutine := b.N / concurrency

	for g := 0; g < concurrency; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				InfoWithCtx(ctx, "high concurrency log",
					String("goroutine", fmt.Sprintf("%d", goroutineID)),
					Int("iteration", i),
				)
				ModuleInfoWithCtx(ctx, "order", "order log",
					String("order_id", fmt.Sprintf("ORD-%d-%d", goroutineID, i)),
				)
			}
		}(g)
	}

	wg.Wait()
}

// BenchmarkMixedScenarios 基准测试：混合场景（多种日志类型 + 路由 + SLS）
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

	ctx := context.WithValue(context.Background(), "request_id", "bench-mixed")

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

// cleanupBenchmarkFiles 清理基准测试生成的日志文件
func cleanupBenchmarkFiles(b *testing.B) {
	files := []string{
		"benchmark.log",
		"benchmark-error.log",
		"benchmark-module.log",
		"benchmark-sls.log",
		"benchmark-concurrent.log",
		"benchmark-sync.log",
		"benchmark-async.log",
		"benchmark-ctx.log",
		"benchmark-route.log",
		"benchmark-high-concurrency.log",
		"benchmark-mixed.log",
	}

	for _, file := range files {
		os.Remove(file)
	}

	dirs := []string{
		"logs/benchmark-order",
		"logs/benchmark-payment",
		"logs/benchmark-user",
	}

	for _, dir := range dirs {
		os.RemoveAll(dir)
	}
}

// TestMain 运行基准测试前后的清理
func TestMain(m *testing.M) {
	// 运行测试
	code := m.Run()

	// 清理测试文件
	cleanupBenchmarkFiles(&testing.B{})

	os.Exit(code)
}

// syncWaitGroup 用于并发测试的等待组
var syncWaitGroup sync.WaitGroup
