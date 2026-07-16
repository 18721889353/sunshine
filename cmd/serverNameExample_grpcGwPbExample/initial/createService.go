package initial

import (
	"fmt"
	"strconv"
	"time"

	"github.com/18721889353/sunshine/pkg/servicerd/registry/nacos"

	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/pkg/etcdcli"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/nacoscli"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
	"github.com/18721889353/sunshine/pkg/servicerd/registry/etcd"

	//// Import cron tasks for initialization
	//_ "github.com/18721889353/sunshine/internal/cron/tasks"
	//// Import rabbitmq consumers for initialization
	//_ "github.com/18721889353/sunshine/internal/mq/rabbitmq/consumers"

	"github.com/18721889353/sunshine/pkg/app"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/server"
)

// CreateServices create http service
func CreateServices() []app.IServer {
	var cfg = config.Get()
	var servers []app.IServer

	// create a http service
	httpAddr := ":" + strconv.Itoa(cfg.HTTP.Port)
	var httpOpts []server.HTTPOption
	if cfg.App.RegistryDiscoveryType != "" {
		httpRegistry, httpInstance := registerService("http", cfg.App.Host, cfg.HTTP.Port)
		httpOpts = append(httpOpts, server.WithHTTPRegistry(httpRegistry, httpInstance))
	}
	httpOpts = append(httpOpts, server.WithHTTPIsProd(cfg.App.Env == "prod"))
	readTimeout := cfg.HTTP.Timeout
	if cfg.HTTP.ReadTimeout > 0 {
		readTimeout = cfg.HTTP.ReadTimeout
	}
	writeTimeout := cfg.HTTP.Timeout
	if cfg.HTTP.WriteTimeout > 0 {
		writeTimeout = cfg.HTTP.WriteTimeout
	}
	if readTimeout > 0 || writeTimeout > 0 || cfg.HTTP.ReadHeaderTimeout > 0 || cfg.HTTP.IdleTimeout > 0 {
		httpOpts = append(httpOpts, server.WithHTTPReadTimeout(time.Duration(readTimeout)*time.Second))
		httpOpts = append(httpOpts, server.WithHTTPWriteTimeout(time.Duration(writeTimeout)*time.Second))
	}
	if cfg.HTTP.ReadHeaderTimeout > 0 {
		httpOpts = append(httpOpts, server.WithHTTPReadHeaderTimeout(time.Duration(cfg.HTTP.ReadHeaderTimeout)*time.Second))
	}
	if cfg.HTTP.IdleTimeout > 0 {
		httpOpts = append(httpOpts, server.WithHTTPIdleTimeout(time.Duration(cfg.HTTP.IdleTimeout)*time.Second))
	}
	httpServer := server.NewHTTPServer(httpAddr, httpOpts...)
	servers = append(servers, httpServer)

	//if cfg.App.OpenCron {
	//	// 添加cron服务示例
	//	servers = append(servers, server.NewCronServer(cron.GetTasks()))
	//}
	//if cfg.Rabbitmq.Enable {
	//	// 添加mq消费者服务示例
	//	servers = append(servers, server.NewRabbitmqConsumerServer(mq.GetConsumers()))
	//}
	return servers
}

// register service with etcd, select one of them to use
func registerService(scheme string, host string, port int) (registry.Registry, *registry.ServiceInstance) {
	instanceEndpoint := fmt.Sprintf("%s://%s:%d", scheme, host, port)
	cfg := config.Get()

	id := cfg.App.Name + "_" + scheme + "_" + utils.GetLocalIP() + "_" + strconv.Itoa(port)
	instance := registry.NewServiceInstance(id, cfg.App.Name, []string{instanceEndpoint})

	var (
		iRegistry registry.Registry
		logField  logger.Field
	)

	switch cfg.App.RegistryDiscoveryType {
	case "etcd":
		cli, err := etcdcli.NewClient(cfg.EtcdInfo.ServerEndpoint(), cfg.EtcdInfo.EtcdClient.BuildClientOptions()...)
		if err != nil {
			panic(err)
		}
		iRegistry = etcd.New(cli, cfg.EtcdInfo.EtcdRegistry.BuildRegistryOptions()...)
		logField = logger.String("etcdAddress", cfg.EtcdInfo.AddrDisplay())

	case "nacos":
		ipAddr, port, namespaceID := cfg.NacosInfo.ServerEndpoint()
		cli, err := nacoscli.NewClient(ipAddr, port, namespaceID, cfg.NacosInfo.BuildNamingClientOptions()...)
		if err != nil {
			panic(err)
		}
		iRegistry = nacos.New(cli, cfg.NacosInfo.NacosRegistry.BuildRegistryOptions()...)
		logField = logger.String("nacosAddress", cfg.NacosInfo.AddrDisplay())
	}

	if iRegistry != nil {
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
