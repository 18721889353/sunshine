//package main
//
//import (
//	"context"
//	"fmt"
//	"time"
//
//	"github.com/18721889353/sunshine/internal/database"
//)
//
//func main() {
//
//	mqClient, err := database.GetClient(context.Background(), &database.MqCfg{
//		ExchangeName:    "orderExchange",
//		NormalQueueName: "orderQueue",
//		Url:             "amqp://hello:hello123@127.0.0.1:5672/",
//		DialTimeout:     time.Duration(3) * time.Second,
//		IsDeadLetter:    true,
//	})
//	if err != nil {
//		fmt.Println(err)
//		return
//	}
//	err = mqClient.Send(context.Background(), "hello world")
//	fmt.Println(err)
//}
