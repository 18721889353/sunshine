package main

import (
	"fmt"
	"log"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

func main() {
	// 连接到 RabbitMQ 服务器
	conn, err := amqp091.Dial("amqp://sunjianguo:jianguo123@43.143.78.234:5672/")
	if err != nil {
		log.Fatal("Failed to connect to RabbitMQ:", err)
	}
	defer conn.Close()

	// 创建通道
	ch, err := conn.Channel()
	if err != nil {
		log.Fatal("Failed to open a channel:", err)
	}
	defer ch.Close()

	// 消费普通队列中的消息
	fmt.Println("开始消费普通队列中的消息...")
	normalMsgs, err := ch.Consume(
		"normal-queue", // queue
		"",             // consumer tag
		false,          // auto-ack (手动确认)
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)
	if err != nil {
		log.Fatal("Failed to register normal queue consumer:", err)
	}

	fmt.Println("开始消费普通队列中的消息...")
	errMsgs, err := ch.Consume(
		"err-queue", // queue
		"",          // consumer tag
		false,       // auto-ack (手动确认)
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // args
	)
	if err != nil {
		log.Fatal("Failed to register normal queue consumer:", err)
	}

	//// 消费死信队列中的消息
	//fmt.Println("开始消费死信队列中的消息...")
	//dlqMsgs, err := ch.Consume(
	//	"dead-letter-queue", // queue
	//	"",                  // consumer tag
	//	false,               // auto-ack (手动确认)
	//	false,               // exclusive
	//	false,               // no-local
	//	false,               // no-wait
	//	nil,                 // args
	//)
	//if err != nil {
	//	log.Fatal("Failed to register DLQ consumer:", err)
	//}

	// 启动普通队列消息处理器
	go handleNormalMessages(normalMsgs)

	// 启动异常队列消息处理器
	go handleErrMessages(errMsgs)

	// 启动死信队列消息处理器
	//go handleDeadLetterMessages(ch, dlqMsgs)

	// 保持程序运行
	fmt.Println("消费者正在运行，按 Ctrl+C 退出...")
	forever := make(chan bool)
	<-forever
}

// 处理普通队列消息
func handleNormalMessages(msgs <-chan amqp091.Delivery) {
	count := 0
	for msg := range msgs {
		count++
		fmt.Printf("[普通队列] 收到消息: %s\n", msg.Body)

		// 模拟处理逻辑
		if count <= 2 {
			// 前两条消息正常处理并确认
			fmt.Printf("[普通队列] 确认消息: %s\n", msg.Body)
			msg.Ack(false)
		} else {
			// 第三条及后续消息拒绝处理，使其变成死信
			fmt.Printf("[普通队列] 拒绝消息(不重新入队): %s\n", msg.Body)
			msg.Nack(false, false) // requeue=false 使消息变成死信
		}
	}
}

// 处理异常队列消息
func handleErrMessages(msgs <-chan amqp091.Delivery) {
	count := 0
	for msg := range msgs {
		count++
		fmt.Printf("[异常队列] 收到消息: %s\n", msg.Body)

		// 模拟处理逻辑
		if count <= 2 {
			// 前两条消息正常处理并确认
			fmt.Printf("[异常队列] 确认消息: %s\n", msg.Body)
			msg.Ack(false)
		} else {
			// 第三条及后续消息拒绝处理，使其变成死信
			fmt.Printf("[异常队列] 拒绝消息(不重新入队): %s\n", msg.Body)
			msg.Nack(false, false) // requeue=false 使消息变成死信
		}
	}
}

// 处理死信队列消息
// 处理死信队列消息（无限次重试，每次间隔1分钟）
func handleDeadLetterMessages(ch *amqp091.Channel, msgs <-chan amqp091.Delivery) {
	for msg := range msgs {
		fmt.Printf("[死信队列] 收到死信消息: %s\n", msg.Body)

		// 等待1分钟后再处理
		fmt.Printf("[死信队列] 消息将在1分钟后重新处理: %s\n", msg.Body)
		time.Sleep(1 * time.Minute)

		// 获取当前重试次数
		retryCount := getRetryCount(msg.Headers)

		// 重新发布到死信队列实现重试
		newHeaders := copyHeaders(msg.Headers)
		newHeaders["x-retry-count"] = retryCount + 1

		// 重新发布到死信队列
		publishErr := ch.Publish(
			"dlx.exchange",
			"dlq.key",
			false,
			false,
			amqp091.Publishing{
				Headers:     newHeaders,
				ContentType: "text/plain",
				Body:        msg.Body,
			},
		)

		if publishErr != nil {
			fmt.Printf("[死信队列] 重新发布消息失败: %v\n", publishErr)
		} else {
			fmt.Printf("[死信队列] 消息已重新发布进行第 %d 次重试\n", retryCount+1)
		}

		// 确认原消息
		msg.Ack(false)
	}
}

// 获取消息重试次数
func getRetryCount(headers amqp091.Table) int {
	if headers == nil {
		return 0
	}

	if count, ok := headers["x-retry-count"]; ok {
		if countVal, ok := count.(int32); ok {
			return int(countVal)
		}
		if countVal, ok := count.(int64); ok {
			return int(countVal)
		}
	}

	return 0
}

// 复制消息头
func copyHeaders(headers amqp091.Table) amqp091.Table {
	if headers == nil {
		return amqp091.Table{}
	}

	newHeaders := amqp091.Table{}
	for k, v := range headers {
		newHeaders[k] = v
	}
	return newHeaders
}
