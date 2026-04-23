// Package main 是 RabbitMQ 连接池使用示例程序。
// 该程序演示如何使用连接池管理 RabbitMQ 连接，提高资源利用率。
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

func main() {
	// 创建一个 zap logger 实例
	_, _ = logger.Init()
	ctx := context.Background()
	// 创建连接池，配置 ants 协程池大小
	pool, err := gorabbitmq.NewPool(
		ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		gorabbitmq.WithInitialCap(10),                    // 初始连接数
		gorabbitmq.WithMaxCap(1000),                      // 最大连接数
		gorabbitmq.WithMaxIdle(time.Minute*1),            // 最大空闲时间
		gorabbitmq.WithHealthCheckPeriod(time.Second*30), // 健康检查间隔
		gorabbitmq.WithAntsPoolSize(10),                  // 配置 ants 协程池大小为 10
		gorabbitmq.WithConnOptions( // 连接选项
			gorabbitmq.WithReconnectTime(time.Second*3),
			gorabbitmq.WithDialTimeout(time.Second*5),
			gorabbitmq.WithHeartbeat(time.Second*3),
		),
	)

	if err != nil {
		logger.FatalWithCtx(ctx, "Failed to create connection pool", logger.Err(err))
	}
	defer func() { _ = pool.Close(ctx) }()

	// 打印连接池状态
	printAntsExampleStats(ctx, pool)

	// 使用连接池中的连接
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// 从连接池获取连接
			conn, err := pool.Get(ctx)
			if err != nil {
				logger.ErrorWithCtx(ctx, "Failed to get connection from pool", logger.Err(err))
				return
			}

			// 使用连接（这里只是模拟使用）
			fmt.Printf("Goroutine %d got connection\n", id)
			time.Sleep(time.Millisecond * 100)

			// 将连接放回连接池
			err = pool.Put(ctx, conn)
			if err != nil {
				logger.ErrorWithCtx(ctx, "Failed to put connection back to pool", logger.Err(err))
				return
			}

			fmt.Printf("Goroutine %d returned connection\n", id)
		}(i)
	}

	// 等待所有 goroutine 完成
	wg.Wait()

	// 等待一段时间以便观察ants协程池的状态
	for range time.NewTicker(time.Second).C {
		printAntsExampleStats(ctx, pool)
	}
}

func printAntsExampleStats(ctx context.Context, pool *gorabbitmq.Pool) {
	stats := pool.Stats(ctx)
	fmt.Printf("Pool Stats: %+v\n", stats)
}
