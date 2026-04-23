package initial

import (
	"fmt"
	"strconv"

	// 导入定时任务包以执行init函数
	_ "github.com/18721889353/sunshine/internal/cron/tasks"

	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"
	// 导入RabbitMQ消费者包以执行init函数
	_ "github.com/18721889353/sunshine/internal/mq/rabbitmq/consumers"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/cron"
	"github.com/18721889353/sunshine/internal/server"
	"github.com/18721889353/sunshine/pkg/app"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
	"github.com/18721889353/sunshine/pkg/servicerd/registry/etcd"
)

// CreateServices create grpc or http service
func CreateServices() []app.IServer {
	var cfg = config.Get()
	var servers []app.IServer

	// create a http service
	httpAddr := ":" + strconv.Itoa(cfg.HTTP.Port)
	httpRegistry, httpInstance := registerService("http", cfg.App.Host, cfg.HTTP.Port)
	httpServer := server.NewHTTPServer(httpAddr,
		server.WithHTTPRegistry(httpRegistry, httpInstance),
		server.WithHTTPIsProd(cfg.App.Env == "prod"),
	)
	servers = append(servers, httpServer)

	// create a grpc service
	grpcAddr := ":" + strconv.Itoa(cfg.Grpc.Port)
	grpcRegistry, grpcInstance := registerService("grpc", cfg.App.Host, cfg.Grpc.Port)
	grpcServer := server.NewGRPCServer(grpcAddr,
		server.WithGrpcRegistry(grpcRegistry, grpcInstance),
	)
	servers = append(servers, grpcServer)
	if cfg.App.OpenCron {
		// 添加cron服务示例
		servers = append(servers, server.NewCronServer(cron.GetTasks()))
	}
	if cfg.Rabbitmq.Enable {
		// 添加mq消费者服务示例
		servers = append(servers, server.NewRabbitmqConsumerServer(mq.GetConsumers()))
	}
	return servers
}

// register service with etcd, select one of them to use
func registerService(scheme string, host string, port int) (registry.Registry, *registry.ServiceInstance) {
	var (
		instanceEndpoint = fmt.Sprintf("%s://%s:%d", scheme, host, port)
		cfg              = config.Get()

		iRegistry registry.Registry
		instance  *registry.ServiceInstance
		err       error

		id       = cfg.App.Name + "_" + scheme + "_" + host + "_" + strconv.Itoa(port)
		logField logger.Field
	)

	if cfg.App.RegistryDiscoveryType == "etcd" {
		iRegistry, instance, err = etcd.NewRegistry(
			cfg.Etcd.Addrs,
			id,
			cfg.App.Name,
			[]string{instanceEndpoint},
		)
		if err != nil {
			panic(err)
		}
		logField = logger.Any("etcdAddress", cfg.Etcd.Addrs)
	}

	if instance != nil {
		msg := fmt.Sprintf("register service address to %s", cfg.App.RegistryDiscoveryType)
		logger.InfoWithCtx(initCtx, msg,
			logger.String("name", cfg.App.Name),
			logger.String("endpoint", instanceEndpoint),
			logger.String("id", id),
			logField,
		)
		return iRegistry, instance
	}

	return nil, nil
}
