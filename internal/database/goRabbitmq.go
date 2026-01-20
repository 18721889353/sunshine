package database

import (
	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq/gorabbitmqClient"
)

// init 确保在程序启动时配置被加载并初始化连接池
func init() {
	gorabbitmqClient.InitRabbitmq(&config.Get().Rabbitmq)
}

// GetRabbitMQ 提供给业务层调用的统一入口
func GetRabbitMQ() *gorabbitmqClient.RabbitMQ { // 注意：这里需要 gorabbitmqClient 中的结构体名是大写的
	return gorabbitmqClient.GetRabbitMQ(&config.Get().Rabbitmq)
}
