package database

import (
	"context"
	"sync"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
	"time"
)

var (
	rabbitmqPool *gorabbitmq.Pool
	once         sync.Once
)

// InitRabbitmq connect rabbitmq
func InitRabbitmq(ctx context.Context) (*gorabbitmq.Pool, error) {
	var err error
	once.Do(func() {
		rabbitmqPool, err = gorabbitmq.NewPool(
			ctx,
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
			panic("Failed to create RabbitMQ pool" + err.Error())
		}
	})

	return rabbitmqPool, err
}

// GetRabbitmqPool 获取 RabbitMQ 连接池实例
func GetRabbitmqPool() *gorabbitmq.Pool {
	return rabbitmqPool
}

// GetRabbitmqConnection 从连接池获取一个 RabbitMQ 连接
func GetRabbitmqConnection(ctx context.Context) (*gorabbitmq.Connection, error) {
	if rabbitmqPool == nil {
		return nil, nil
	}

	return rabbitmqPool.Get(ctx)
}

// PutRabbitmqConnection 将 RabbitMQ 连接放回连接池
func PutRabbitmqConnection(ctx context.Context, conn *gorabbitmq.Connection) error {
	if rabbitmqPool == nil || conn == nil {
		return nil
	}

	return rabbitmqPool.Put(ctx, conn)
}

// CloseRabbitmqPool 关闭 RabbitMQ 连接池
func CloseRabbitmqPool(ctx context.Context) error {
	if rabbitmqPool == nil {
		return nil
	}
	logger.Info("Closing RabbitMQ connection pool")
	return rabbitmqPool.Close(ctx)
}
