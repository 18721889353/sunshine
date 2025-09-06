package main

import (
	"context"
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
		gorabbitmq.WithInitialCap(5),            // 初始连接数
		gorabbitmq.WithMaxCap(20),               // 最大连接数
		gorabbitmq.WithMaxIdle(time.Minute*1),   // 最大空闲时间
		gorabbitmq.WithPoolLogger(logger.Get()), // 日志记录器
		gorabbitmq.WithAntsPoolSize(50),         // 配置 ants 协程池大小为 50
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

	// 再次打印连接池状态
	printAntsExampleStats(pool)

	// 演示如何创建生产者
	conn, err := pool.Get()
	if err != nil {
		logger.Error("Failed to get connection from pool", zap.Error(err))
		return
	}

	// 创建生产者配置
	config := gorabbitmq.ProducerConfig{
		Exchange:     "test_exchange",
		ExchangeType: "direct",
		QueueName:    "test_queue",
		RoutingKey:   "test_routing_key",
		Durable:      true,
	}

	// 创建生产者
	producer, err := gorabbitmq.NewProducer(conn, config)
	if err != nil {
		logger.Error("Failed to create producer", zap.Error(err))
		pool.Put(conn) // 记得将连接放回池中
		return
	}

	// 发送消息
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	err = producer.Publish(ctx, fmt.Sprintf("Hello, RabbitMQ! Message time: %s", time.Now().String()))
	if err != nil {
		logger.Error("Failed to publish message", zap.Error(err))
	} else {
		fmt.Println("Message published successfully")
	}

	// 将连接放回池中
	pool.Put(conn)

	// 最后打印连接池状态
	printAntsExampleStats(pool)

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
