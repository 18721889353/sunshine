package main

import (
	"context"
	"fmt"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"go.uber.org/zap"
)

func main() {
	// 创建一个 zap logger 实例

	// 创建连接池
	pool, err := gorabbitmq.NewPool(
		"amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		gorabbitmq.WithInitialCap(1),            // 初始连接数
		gorabbitmq.WithMaxCap(2),                // 最大连接数
		gorabbitmq.WithMaxIdle(time.Second*10),  // 最大空闲时间
		gorabbitmq.WithPoolLogger(logger.Get()), // 日志记录器
		gorabbitmq.WithConnOptions( // 连接选项
			gorabbitmq.WithLogger(logger.Get()),
			gorabbitmq.WithReconnectTime(time.Second*1),
			gorabbitmq.WithDialTimeout(time.Second*5),
			gorabbitmq.WithHeartbeat(time.Second*3),
			gorabbitmq.WithMaxRetries(3),
		),
	)
	if err != nil {
		logger.Fatal("创建连接池失败", zap.Error(err))
	}
	defer pool.Close()

	// 打印连接池状态
	printPoolStats(pool)

	// 使用连接池中的连接
	for i := 0; i < 10; i++ {
		go func(id int) {
			// 从连接池获取连接
			conn, err := pool.Get()
			if err != nil {
				logger.Error("从连接池获取连接失败", zap.Error(err))
				return
			}

			// 使用连接（这里只是模拟使用）
			fmt.Printf("Goroutine %d 获取到连接\n", id)
			time.Sleep(time.Second * 1)

			// 将连接放回连接池
			err = pool.Put(conn)
			if err != nil {
				logger.Error("将连接放回连接池失败", zap.Error(err))
				return
			}

			fmt.Printf("Goroutine %d 归还连接\n", id)
		}(i)
	}

	// 等待所有 goroutine 完成
	time.Sleep(time.Second * 2)

	// 再次打印连接池状态
	printPoolStats(pool)

	// 演示如何创建生产者
	conn, err := pool.Get()
	if err != nil {
		logger.Error("从连接池获取连接失败", zap.Error(err))
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
		logger.Error("创建生产者失败", zap.Error(err))
		pool.Put(conn) // 记得将连接放回池中
		return
	}

	// 发送消息
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	err = producer.Publish(ctx, fmt.Sprintf("Hello, RabbitMQ! 消息时间: %s", time.Now().String()))
	if err != nil {
		logger.Error("发布消息失败", zap.Error(err))
	} else {
		fmt.Println("消息发布成功")
	}

	// 将连接放回池中
	pool.Put(conn)
	for {
		select {
		case <-time.After(time.Second * 1):
			// 最后打印连接池状态
			printPoolStats(pool)
		}

	}

}

// printPoolStats 打印连接池统计信息
func printPoolStats(pool *gorabbitmq.Pool) {
	fmt.Printf("连接池统计信息: %+v\n", pool.Stats())
}