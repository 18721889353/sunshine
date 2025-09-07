package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"go.uber.org/zap"
)

func main() {
	// 创建一个 zap logger 实例
	logger.Init()

	// 创建连接池，配置 ants 协程池大小
	pool, err := gorabbitmq.NewPool(
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		gorabbitmq.WithInitialCap(10),           // 初始连接数
		gorabbitmq.WithMaxCap(1000),             // 最大连接数
		gorabbitmq.WithMaxIdle(time.Minute*10),  // 最大空闲时间
		gorabbitmq.WithPoolLogger(logger.Get()), // 日志记录器
		gorabbitmq.WithAntsPoolSize(10),         // 配置 ants 协程池大小为 10
		gorabbitmq.WithConnOptions( // 连接选项
			gorabbitmq.WithReconnectTime(time.Second*3),
			gorabbitmq.WithDialTimeout(time.Second*5),
			gorabbitmq.WithHeartbeat(time.Second*3),
		),
	)
	if err != nil {
		logger.Fatal("Failed to create connection pool", zap.Error(err))
	}
	defer pool.Close()

	// 打印连接池状态
	printAntsExampleStats(pool)

	// 使用连接池中的连接
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// 从连接池获取连接
			conn, err := pool.Get()
			if err != nil {
				logger.Error("Failed to get connection from pool", zap.Error(err))
				return
			}

			// 使用连接（这里只是模拟使用）
			fmt.Printf("Goroutine %d got connection\n", id)
			time.Sleep(time.Millisecond * 100)

			// 将连接放回连接池
			err = pool.Put(conn)
			if err != nil {
				logger.Error("Failed to put connection back to pool", zap.Error(err))
				return
			}

			fmt.Printf("Goroutine %d returned connection\n", id)
		}(i)
	}

	// 等待所有 goroutine 完成
	wg.Wait()

	// 等待一段时间以便观察ants协程池的状态
	for {
		select {
		case <-time.After(time.Second * 1):
			printAntsExampleStats(pool)
		}
	}
}

func printAntsExampleStats(pool *gorabbitmq.Pool) {
	stats := pool.Stats()
	fmt.Printf("Pool Stats: %+v\n", stats)
}
