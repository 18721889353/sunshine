// Package main 是 RabbitMQ 连接池使用示例程序。
// 该程序演示如何使用连接池管理 RabbitMQ 连接，提高资源利用率。
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

func main() {
	// 创建一个 zap logger 实例
	_, err := logger.Init()
	if err != nil {
		log.Fatalf("初始化logger失败: %v", err)
	}

	// 带取消的 context，确保 idleCleanup 能退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建连接池
	pool, err := gorabbitmq.NewPool(
		ctx,
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		gorabbitmq.WithInitialCap(3),
		gorabbitmq.WithMaxCap(10),
		gorabbitmq.WithMaxIdle(time.Minute*1),
		gorabbitmq.WithHealthCheckPeriod(time.Second*30),
		gorabbitmq.WithAntsPoolSize(10),
		gorabbitmq.WithConnOptions(
			gorabbitmq.WithReconnectTime(time.Second*3),
			gorabbitmq.WithDialTimeout(time.Second*5),
			gorabbitmq.WithHeartbeat(time.Second*3),
		),
	)
	if err != nil {
		logger.FatalWithCtx(ctx, "Failed to create connection pool", logger.Err(err))
	}
	defer func() {
		if closeErr := pool.Close(ctx); closeErr != nil {
			log.Printf("close pool error: %v", closeErr)
		}
	}()

	printPoolStats(ctx, pool, "initial")

	// 并发使用连接
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			conn, getErr := pool.Get(ctx)
			if getErr != nil {
				logger.ErrorWithCtx(ctx, "Failed to get connection from pool", logger.Err(getErr))
				return
			}
			fmt.Printf("Goroutine %d got connection\n", id)
			time.Sleep(time.Millisecond * 100)

			if putErr := pool.Put(ctx, conn); putErr != nil {
				logger.ErrorWithCtx(ctx, "Failed to put connection back to pool", logger.Err(putErr))
				return
			}
			fmt.Printf("Goroutine %d returned connection\n", id)
		}(i)
	}
	wg.Wait()

	printPoolStats(ctx, pool, "after use")

	// 演示：Context 取消
	fmt.Println("\n--- Demo: context cancellation ---")
	cancelCtx, demoCancel := context.WithCancel(context.Background())
	demoCancel()
	if _, getErr := pool.Get(cancelCtx); getErr != nil {
		fmt.Printf("Get with cancelled context returned expected error: %v\n", getErr)
	}

	// 演示：放回已关闭的连接
	fmt.Println("\n--- Demo: discard invalid connection ---")
	conn, err := pool.Get(ctx)
	if err == nil {
		conn.Close()
		if err := pool.Put(ctx, conn); err != nil {
			fmt.Printf("Put closed connection error (expected): %v\n", err)
		} else {
			fmt.Println("Invalid connection discarded by pool")
		}
	}

	printPoolStats(ctx, pool, "final")
	fmt.Println("\nPool demo completed successfully")
}

func printPoolStats(ctx context.Context, pool *gorabbitmq.Pool, stage string) {
	stats := pool.Stats(ctx)
	fmt.Printf("[%s] totalConns=%d available=%d poolSize=%d maxCap=%d\n",
		stage, stats["totalConns"], stats["available"], stats["poolSize"], stats["maxCap"])
}
