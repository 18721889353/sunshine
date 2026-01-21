package gorabbitmq

import (
	"context"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"
)

// 初始化logger以避免测试中的错误

// BenchmarkPoolGetAndPut 基准测试连接池的Get/Put操作性能
func BenchmarkPoolGetAndPut(b *testing.B) {
	ctx := context.Background()

	// 创建连接池
	pool, err := NewPool(ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		WithInitialCap(50),                    // 初始连接数
		WithMaxCap(500),                       // 最大连接数
		WithMaxIdle(time.Minute*1),            // 最大空闲时间
		WithHealthCheckPeriod(time.Second*30), // 健康检查间隔
		WithPoolLogger(zap.NewNop()),          // 日志记录器
		WithAntsPoolSize(200),                 // 配置 ants 协程池大小为 10
		WithConnOptions( // 连接选项
			WithReconnectTime(time.Second*3),
			WithDialTimeout(time.Second*5),
			WithHeartbeat(time.Second*3),
		),
	)

	// 如果无法连接到RabbitMQ服务器，则跳过此测试
	if err != nil {
		b.Skipf("Skipping benchmark due to connection error: %v", err)
	}
	defer pool.Close(ctx)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			b.Errorf("Failed to get connection: %v", err)
			continue
		}

		// 模拟使用连接（实际使用中会进行RabbitMQ操作）
		time.Sleep(time.Microsecond) // 模拟业务逻辑处理时间

		err = pool.Put(ctx, conn)
		if err != nil {
			b.Errorf("Failed to put connection: %v", err)
		}
	}
}

// BenchmarkPoolConcurrentAccess 并发访问基准测试
func BenchmarkPoolConcurrentAccess(b *testing.B) {

	ctx := context.Background()

	pool, err := NewPool(ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		WithInitialCap(50),                    // 初始连接数
		WithMaxCap(500),                       // 最大连接数
		WithMaxIdle(time.Minute*1),            // 最大空闲时间
		WithHealthCheckPeriod(time.Second*30), // 健康检查间隔
		WithPoolLogger(zap.NewNop()),          // 日志记录器
		WithAntsPoolSize(200),                 // 配置 ants 协程池大小为 10
		WithConnOptions( // 连接选项
			WithReconnectTime(time.Second*3),
			WithDialTimeout(time.Second*5),
			WithHeartbeat(time.Second*3),
		),
	)

	if err != nil {
		b.Skipf("Skipping benchmark due to connection error: %v", err)
	}
	defer pool.Close(ctx)

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := pool.Get(ctx)
			if err != nil {
				b.Errorf("Failed to get connection: %v", err)
				continue
			}

			// 模拟使用连接
			time.Sleep(time.Microsecond)

			err = pool.Put(ctx, conn)
			if err != nil {
				b.Errorf("Failed to put connection: %v", err)
			}
		}
	})
}

// BenchmarkPoolHighConcurrency 高并发场景基准测试
func BenchmarkPoolHighConcurrency(b *testing.B) {

	ctx := context.Background()

	pool, err := NewPool(ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		WithInitialCap(50),                    // 初始连接数
		WithMaxCap(500),                       // 最大连接数
		WithMaxIdle(time.Minute*1),            // 最大空闲时间
		WithHealthCheckPeriod(time.Second*30), // 健康检查间隔
		WithPoolLogger(zap.NewNop()),          // 日志记录器
		WithAntsPoolSize(200),                 // 配置 ants 协程池大小为 10
		WithConnOptions( // 连接选项
			WithReconnectTime(time.Second*3),
			WithDialTimeout(time.Second*5),
			WithHeartbeat(time.Second*3),
		),
	)

	if err != nil {
		b.Skipf("Skipping benchmark due to connection error: %v", err)
	}
	defer pool.Close(ctx)

	// 并发 goroutine 数量
	const concurrency = 100

	var wg sync.WaitGroup
	wg.Add(concurrency)

	b.ResetTimer()

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < b.N/concurrency+1; j++ {
				conn, err := pool.Get(ctx)
				if err != nil {
					b.Errorf("Failed to get connection: %v", err)
					continue
				}

				// 模拟使用连接
				time.Sleep(time.Microsecond * 10)

				err = pool.Put(ctx, conn)
				if err != nil {
					b.Errorf("Failed to put connection: %v", err)
				}
			}
		}()
	}

	wg.Wait()
}

// BenchmarkPoolDifferentSizes 测试不同连接池大小的性能
func BenchmarkPoolDifferentSizes(b *testing.B) {

	testCases := []struct {
		name    string
		initCap int
		maxCap  int
	}{
		{"SmallPool", 5, 20},
		{"MediumPool", 20, 100},
		{"LargePool", 50, 500},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			pool, err := NewPool(ctx,
				"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
				WithInitialCap(50),                    // 初始连接数
				WithMaxCap(500),                       // 最大连接数
				WithMaxIdle(time.Minute*1),            // 最大空闲时间
				WithHealthCheckPeriod(time.Second*30), // 健康检查间隔
				WithPoolLogger(zap.NewNop()),          // 日志记录器
				WithAntsPoolSize(200),                 // 配置 ants 协程池大小为 10
				WithConnOptions( // 连接选项
					WithReconnectTime(time.Second*3),
					WithDialTimeout(time.Second*5),
					WithHeartbeat(time.Second*3),
				),
			)

			if err != nil {
				b.Skipf("Skipping benchmark due to connection error: %v", err)
			}
			defer pool.Close(ctx)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				conn, err := pool.Get(ctx)
				if err != nil {
					b.Errorf("Failed to get connection: %v", err)
					continue
				}

				// 模拟使用连接
				time.Sleep(time.Microsecond * 2)

				err = pool.Put(ctx, conn)
				if err != nil {
					b.Errorf("Failed to put connection: %v", err)
				}
			}
		})
	}
}

// BenchmarkPoolGetOnly 仅测试Get操作的性能
func BenchmarkPoolGetOnly(b *testing.B) {

	ctx := context.Background()

	pool, err := NewPool(ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		WithInitialCap(50),                    // 初始连接数
		WithMaxCap(500),                       // 最大连接数
		WithMaxIdle(time.Minute*1),            // 最大空闲时间
		WithHealthCheckPeriod(time.Second*30), // 健康检查间隔
		WithPoolLogger(zap.NewNop()),          // 日志记录器
		WithAntsPoolSize(200),                 // 配置 ants 协程池大小为 10
		WithConnOptions( // 连接选项
			WithReconnectTime(time.Second*3),
			WithDialTimeout(time.Second*5),
			WithHeartbeat(time.Second*3),
		),
	)

	if err != nil {
		b.Skipf("Skipping benchmark due to connection error: %v", err)
	}
	defer pool.Close(ctx)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			b.Errorf("Failed to get connection: %v", err)
			continue
		}

		// 直接放回连接池，不进行任何操作
		err = pool.Put(ctx, conn)
		if err != nil {
			b.Errorf("Failed to put connection: %v", err)
		}
	}
}

// BenchmarkAntsPoolPerformance 测试 ants 协程池本身的性能作为对比
func BenchmarkAntsPoolPerformance(b *testing.B) {

	ctx := context.Background()
	// 测试 ants 协程池本身的性能
	pool, err := NewPool(ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		WithInitialCap(50),                    // 初始连接数
		WithMaxCap(500),                       // 最大连接数
		WithMaxIdle(time.Minute*1),            // 最大空闲时间
		WithHealthCheckPeriod(time.Second*30), // 健康检查间隔
		WithPoolLogger(zap.NewNop()),          // 日志记录器
		WithAntsPoolSize(200),                 // 配置 ants 协程池大小为 10
		WithConnOptions( // 连接选项
			WithReconnectTime(time.Second*3),
			WithDialTimeout(time.Second*5),
			WithHeartbeat(time.Second*3),
		),
	)

	if err != nil {
		b.Skipf("Skipping benchmark due to connection error: %v", err)
	}
	defer pool.Close(context.Background())

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := pool.antsPool.Submit(func() {
			// 模拟简单工作负载
			time.Sleep(time.Nanosecond)
		})
		if err != nil {
			b.Errorf("Failed to submit task: %v", err)
		}
	}
}
