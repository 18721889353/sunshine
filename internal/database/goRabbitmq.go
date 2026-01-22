package database

import (
	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq/gorabbitmqClient"
)

const (
	MQMAIN = "main" // 本项目 MQ
)

// InitRabbitmq 确保在程序启动时配置被加载并初始化连接池
func InitRabbitmq() {
	cfg := config.Get()
	// 1. 初始化本项目 MQ
	if cfg.Rabbitmq.Enable { //
		gorabbitmqClient.InitRabbitmq(MQMAIN, &cfg.Rabbitmq)
	}
}

// GetMainRabbitMQ 获取本项目 MQ 实例
func GetMainRabbitMQ() *gorabbitmqClient.RabbitMQ {
	return gorabbitmqClient.GetRabbitMQ(MQMAIN)
}
