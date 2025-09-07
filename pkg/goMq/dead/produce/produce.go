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

	// 普通交换机
	err = ch.ExchangeDeclare(
		"normal.exchange", // name
		"direct",          // type
		true,              // durable
		false,             // auto-deleted
		false,             // internal
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		log.Fatal("Failed to declare dead letter exchange:", err)
	}
	fmt.Println("普通交换机 normal.exchange 声明成功")
	//------------------------------------------------------------------------------------
	// 声明死信队列并设置异常策略
	deadArgs := amqp091.Table{
		"x-dead-letter-exchange":    "normal.exchange",
		"x-dead-letter-routing-key": "el.key",
		"x-message-ttl":             int32(10000), // 10秒后过期
		//"x-max-length":              int32(5),     // 最多5条消息
	}
	// 声明死信队列
	dlq, err := ch.QueueDeclare(
		"dead-letter-queue", // name
		true,                // durable
		false,               // delete when unused
		false,               // exclusive
		false,               // no-wait
		deadArgs,            // arguments
	)
	if err != nil {
		log.Fatal("Failed to declare dead letter queue:", err)
	}
	fmt.Printf("死信队列 %s 声明成功\n", dlq.Name)

	// 绑定死信队列到交换机
	err = ch.QueueBind(
		"dead-letter-queue", // queue name
		"dl.key",            // routing key
		"normal.exchange",   // exchange
		false,               // no-wait
		nil,                 // arguments
	)
	if err != nil {
		log.Fatal("Failed to bind dead letter queue to exchange:", err)
	}
	fmt.Println("死信队列绑定到死信交换机成功")
	//------------------------------------------------------------------------------------
	// 声明异常队列并设置死信策略
	errArgs := amqp091.Table{
		"x-dead-letter-exchange":    "normal.exchange",
		"x-dead-letter-routing-key": "dl.key",
		//"x-message-ttl":             int32(10000), // 10秒后过期
		//"x-max-length":              int32(5),     // 最多5条消息
	}
	// 声明异常队列
	elq, err := ch.QueueDeclare(
		"err-queue", // name
		true,        // durable
		false,       // delete when unused
		false,       // exclusive
		false,       // no-wait
		errArgs,     // arguments
	)
	if err != nil {
		log.Fatal("Failed to declare dead letter queue:", err)
	}
	fmt.Printf("声明异常队列 %s 声明成功\n", elq.Name)
	// 绑定异常队列到交换机
	err = ch.QueueBind(
		"err-queue",       // queue name
		"el.key",          // routing key
		"normal.exchange", // exchange
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		log.Fatal("Failed to bind err letter queue to exchange:", err)
	}
	fmt.Println("异常队列绑定到交换机成功")
	//------------------------------------------------------------------------------------

	// 声明普通队列并设置死信策略
	normalArgs := amqp091.Table{
		"x-dead-letter-exchange":    "normal.exchange",
		"x-dead-letter-routing-key": "dl.key",
		//"x-message-ttl":             int32(10000), // 10秒后过期
		//"x-max-length":              int32(5),     // 最多5条消息
	}

	normalQueue, err := ch.QueueDeclare(
		"normal-queue", // name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		normalArgs,     // arguments
	)
	if err != nil {
		log.Fatal("Failed to declare normal queue:", err)
	}
	fmt.Printf("普通队列 %s 声明成功，并配置了死信策略\n", normalQueue.Name)

	// 绑定普通队列到普通交换机
	err = ch.QueueBind(
		"normal-queue",    // queue name
		"normal.key",      // routing key
		"normal.exchange", // exchange
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		log.Fatal("Failed to bind normal queue to exchange:", err)
	}
	fmt.Println("普通队列绑定到普通交换机成功")

	// 发送测试消息
	messages := []string{
		"消息1 - 正常处理",
		"消息2 - 正常处理",
		"消息3 - 将被拒绝变成死信",
		"消息4 - 超出队列长度限制",
		"消息5 - 超出队列长度限制",
		"消息6 - 超出队列长度限制",
		"消息7 - TTL过期变成死信(如果未被消费)",
	}

	fmt.Println("\n开始发送消息到普通队列...")
	for i, msg := range messages {
		err = ch.Publish(
			"normal.exchange", // exchange
			"normal.key",      // routing key
			false,             // mandatory
			false,             // immediate
			amqp091.Publishing{
				ContentType: "text/plain",
				Body:        []byte(fmt.Sprintf("%s (编号:%d)", msg, i+1)),
			},
		)
		if err != nil {
			log.Printf("发送消息失败: %v", err)
		} else {
			fmt.Printf("已发送: %s\n", msg)
		}
		time.Sleep(500 * time.Millisecond) // 短暂间隔
	}

	fmt.Println("\n消息发送完成，等待消费者处理...")
	fmt.Println("请运行 consumer.go 来处理消息")

	// 保持程序运行以便观察结果
	time.Sleep(30 * time.Second)
}
