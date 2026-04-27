package database

import (
	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq/gorabbitmqclient"
)

const (
	// MQMAIN 本项目主MQ实例名称
	MQMAIN = "main"
)

// InitRabbitmq 确保在程序启动时配置被加载并初始化连接池
func InitRabbitmq() {
	cfg := config.Get()
	// 1. 初始化本项目 MQ
	if cfg.Rabbitmq.Enable { //
		gorabbitmqclient.InitRabbitmq(MQMAIN, &cfg.Rabbitmq)
	}
}

// GetMainRabbitMQ 获取本项目 MQ 实例
func GetMainRabbitMQ() *gorabbitmqclient.RabbitMQ {
	return gorabbitmqclient.GetRabbitMQ(MQMAIN)
}
