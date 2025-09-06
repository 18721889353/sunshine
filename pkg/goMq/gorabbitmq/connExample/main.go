package main

import (
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

func main() {
	fmt.Println("Starting RabbitMQ connection test...")

	// 首先测试单个连接的创建
	fmt.Println("1. Testing single connection creation...")
	singleConn, err := createConnectionWithRetry("amqp://sunjianguo:jianguo123@43.143.78.234:5672/", 0)
	if err != nil {
		log.Printf("Failed to create single connection after retries: %v", err)
	} else {
		defer singleConn.Close()
		status := singleConn.GetConnectionStatus()
		fmt.Printf("Single connection status: %+v\n", status)
		// 注意：这里不再显式调用Close()，因为defer会处理
	}
	time.Sleep(time.Second * 600)

}

// createConnectionWithRetry 尝试创建连接，带重试机制
func createConnectionWithRetry(url string, maxRetries int) (*gorabbitmq.Connection, error) {
	var conn *gorabbitmq.Connection
	var err error
	conn, err = gorabbitmq.NewConnection(url,
		gorabbitmq.WithMaxRetries(maxRetries), // 无限重试
		gorabbitmq.WithReconnectTime(2*time.Second),
		gorabbitmq.WithDialTimeout(5*time.Second))

	if err == nil {
		return conn, nil
	}
	return nil, err
}
