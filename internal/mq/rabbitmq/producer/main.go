package main

import (
	"context"
	"fmt"
	"time"

	"github.com/18721889353/sunshine/internal/database"
)

func main() {

	mqClient, err := database.GetClient(context.Background(), &database.MqCfg{
		ExchangeName:    "orderExchange",
		NormalQueueName: "orderQueue",
		Url:             "amqp://sunjianguo:jianguo123@43.143.78.234:5672/",
		DialTimeout:     time.Duration(3) * time.Second,
		IsDeadLetter:    true,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	err = mqClient.Send(context.Background(), "hello world")
	fmt.Println(err)
}
