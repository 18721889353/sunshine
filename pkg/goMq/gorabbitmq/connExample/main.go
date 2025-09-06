package main

import (
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

func main() {
	fmt.Println("开始测试 RabbitMQ 连接...")

	// 首先测试单个连接的创建
	fmt.Println("1. 测试单个连接创建...")
	singleConn, err := createConnectionWithRetry("amqp://sunjianguo:jianguo123@43.143.78.234:5672/", 0)
	if err != nil {
		log.Printf("重试后仍无法创建单个连接: %v", err)
	} else {
		defer singleConn.Close()
		status := singleConn.GetConnectionStatus()
		fmt.Printf("单个连接状态: %+v\n", status)
		// 注意：这里不再显式调用Close()，因为defer会处理
	}
	time.Sleep(time.Second * 600)

}

// createConnectionWithRetry 尝试创建连接，带重试机制
func createConnectionWithRetry(url string, maxRetries int) (*gorabbitmq.Connection, error) {
	var conn *gorabbitmq.Connection
	var err error
	conn, err = gorabbitmq.NewConnection(url,
		gorabbitmq.WithLogger(logger.Get()),
		gorabbitmq.WithMaxRetries(maxRetries), // 无限重试
		gorabbitmq.WithReconnectTime(2*time.Second),
		gorabbitmq.WithDialTimeout(5*time.Second))

	if err == nil {
		return conn, nil
	}
	return nil, err
}