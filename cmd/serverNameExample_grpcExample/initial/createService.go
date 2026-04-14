package initial

import (
	"fmt"
	"github.com/18721889353/sunshine/internal/cron"
	_ "github.com/18721889353/sunshine/internal/cron/tasks"
	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"
	_ "github.com/18721889353/sunshine/internal/mq/rabbitmq/consumers"
	"github.com/18721889353/sunshine/internal/server"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
	"github.com/18721889353/sunshine/pkg/servicerd/registry/etcd"
	"strconv"

	"github.com/18721889353/sunshine/pkg/app"

	"github.com/18721889353/sunshine/internal/config"
)

// CreateServices create grpc service
func CreateServices() []app.IServer {
	var cfg = config.Get()
	var servers []app.IServer
	var grpcAddr = ":" + strconv.Itoa(cfg.Grpc.Port)

	// case 1, create a grpc service without registry
	grpcServer := server.NewGRPCServer(grpcAddr)

	//// case 2, create a grpc service and register it with etcd
	//grpcRegistry, grpcInstance := registerService("grpc", cfg.App.Host, cfg.Grpc.Port)
	//grpcServer := server.NewGRPCServer(grpcAddr,
	//	server.WithGrpcRegistry(grpcRegistry, grpcInstance),
	//)

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

	switch cfg.App.RegistryDiscoveryType {
	case "etcd":
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
		logger.InfoWithCtx(initCtx, msg, logger.String("name", cfg.App.Name), logger.String("endpoint", instanceEndpoint), logger.String("id", id), logField)
		return iRegistry, instance
	}

	return nil, nil
}
