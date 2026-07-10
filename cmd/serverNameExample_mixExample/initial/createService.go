package initial

import (
	"fmt"
	"github.com/18721889353/sunshine/pkg/nacoscli"
	nacosRegistry "github.com/18721889353/sunshine/pkg/servicerd/registry/nacos"
	"strconv"
	"time"

	// 导入定时任务包以执行init函数
	_ "github.com/18721889353/sunshine/internal/cron/tasks"

	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"
	// 导入RabbitMQ消费者包以执行init函数
	_ "github.com/18721889353/sunshine/internal/mq/rabbitmq/consumers"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/cron"
	"github.com/18721889353/sunshine/internal/server"
	"github.com/18721889353/sunshine/pkg/app"
	"github.com/18721889353/sunshine/pkg/etcdcli"
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

		id       = cfg.App.Name + "_" + scheme + "_" + host + "_" + strconv.Itoa(port)
		logField logger.Field
	)

	if cfg.App.RegistryDiscoveryType == "etcd" {
		var etcdOpts []etcdcli.Option
		if cfg.Etcd.EtcdClient.DialTimeout != "" {
			d, _ := time.ParseDuration(cfg.Etcd.EtcdClient.DialTimeout)
			if d > 0 {
				etcdOpts = append(etcdOpts, etcdcli.WithDialTimeout(d))
			}
		}
		if cfg.Etcd.EtcdClient.Username != "" {
			etcdOpts = append(etcdOpts, etcdcli.WithAuth(cfg.Etcd.EtcdClient.Username, cfg.Etcd.EtcdClient.Password))
		}
		if cfg.Etcd.EtcdClient.AutoSyncInterval != "" {
			d, _ := time.ParseDuration(cfg.Etcd.EtcdClient.AutoSyncInterval)
			if d > 0 {
				etcdOpts = append(etcdOpts, etcdcli.WithAutoSyncInterval(d))
			}
		}
		if cfg.Etcd.EtcdClient.IsSecure {
			etcdOpts = append(etcdOpts, etcdcli.WithSecure(cfg.Etcd.EtcdClient.ServerNameOverride, cfg.Etcd.EtcdClient.CertFile))
		}
		cli, err := etcdcli.Init(cfg.Etcd.Addrs, etcdOpts...)
		if err != nil {
			panic(err)
		}
		instance = registry.NewServiceInstance(id, cfg.App.Name, []string{instanceEndpoint})

		var etcdRegistryOpts []etcd.Option
		if cfg.Etcd.EtcdRegistry.Namespace != "" {
			etcdRegistryOpts = append(etcdRegistryOpts, etcd.WithNamespace(cfg.Etcd.EtcdRegistry.Namespace))
		}
		if cfg.Etcd.EtcdRegistry.TTL != "" {
			d, _ := time.ParseDuration(cfg.Etcd.EtcdRegistry.TTL)
			if d > 0 {
				etcdRegistryOpts = append(etcdRegistryOpts, etcd.WithRegisterTTL(d))
			}
		}
		if cfg.Etcd.EtcdRegistry.MaxRetry > 0 {
			etcdRegistryOpts = append(etcdRegistryOpts, etcd.WithMaxRetry(cfg.Etcd.EtcdRegistry.MaxRetry))
		}
		if cfg.Etcd.EtcdRegistry.CheckInterval > 0 {
			etcdRegistryOpts = append(etcdRegistryOpts, etcd.WithCheckInterval(cfg.Etcd.EtcdRegistry.CheckInterval))
		}
		if cfg.Etcd.EtcdRegistry.BackoffInit != "" {
			d, _ := time.ParseDuration(cfg.Etcd.EtcdRegistry.BackoffInit)
			if d > 0 {
				etcdRegistryOpts = append(etcdRegistryOpts, etcd.WithBackoffInit(d))
			}
		}
		if cfg.Etcd.EtcdRegistry.BackoffMax != "" {
			d, _ := time.ParseDuration(cfg.Etcd.EtcdRegistry.BackoffMax)
			if d > 0 {
				etcdRegistryOpts = append(etcdRegistryOpts, etcd.WithBackoffMax(d))
			}
		}
		iRegistry = etcd.New(cli, etcdRegistryOpts...)
		logField = logger.Any("etcdAddress", cfg.Etcd.Addrs)
	}

	if cfg.App.RegistryDiscoveryType == "nacos" {
		var nacosOpts []nacoscli.Option
		if cfg.NacosRegistry.NacosClient.Username != "" {
			nacosOpts = append(nacosOpts, nacoscli.WithAuth(cfg.NacosRegistry.NacosClient.Username, cfg.NacosRegistry.NacosClient.Password))
		}
		if cfg.NacosRegistry.NacosServer.Scheme != "" {
			nacosOpts = append(nacosOpts, nacoscli.WithScheme(cfg.NacosRegistry.NacosServer.Scheme))
		}
		if cfg.NacosRegistry.NacosServer.ContextPath != "" {
			nacosOpts = append(nacosOpts, nacoscli.WithContextPath(cfg.NacosRegistry.NacosServer.ContextPath))
		}
		if cfg.NacosRegistry.NacosClient.TimeoutMs > 0 {
			nacosOpts = append(nacosOpts, nacoscli.WithTimeoutMs(uint64(cfg.NacosRegistry.NacosClient.TimeoutMs)))
		}
		cli, err := nacoscli.NewNamingClient(cfg.NacosRegistry.NacosServer.IPAddr, cfg.NacosRegistry.NacosServer.Port, cfg.NacosRegistry.NacosServer.NamespaceID, nacosOpts...)
		if err != nil {
			panic(err)
		}
		instance = registry.NewServiceInstance(id, cfg.App.Name, []string{instanceEndpoint})

		var nacosRegistryOpts []nacosRegistry.Option
		nacosRegistryOpts = append(nacosRegistryOpts,
			nacosRegistry.WithGroupName(cfg.NacosRegistry.NacosRegistration.GroupName),
			nacosRegistry.WithClusterName(cfg.NacosRegistry.NacosRegistration.ClusterName),
			nacosRegistry.WithScheme(scheme),
			nacosRegistry.WithEphemeral(cfg.NacosRegistry.NacosRegistration.Ephemeral),
			nacosRegistry.WithHealthy(cfg.NacosRegistry.NacosRegistration.Healthy),
			nacosRegistry.WithRegisterEnabled(cfg.NacosRegistry.NacosRegistration.RegisterEnabled),
		)
		if cfg.NacosRegistry.NacosRegistration.Weight > 0 {
			nacosRegistryOpts = append(nacosRegistryOpts, nacosRegistry.WithWeight(float64(cfg.NacosRegistry.NacosRegistration.Weight)))
		}
		if cfg.NacosRegistry.NacosRegistration.CheckInterval != "" {
			d, _ := time.ParseDuration(cfg.NacosRegistry.NacosRegistration.CheckInterval)
			if d > 0 {
				nacosRegistryOpts = append(nacosRegistryOpts, nacosRegistry.WithCheckInterval(d))
			}
		}
		if cfg.NacosRegistry.NacosRegistration.BackoffInit != "" {
			d, _ := time.ParseDuration(cfg.NacosRegistry.NacosRegistration.BackoffInit)
			if d > 0 {
				nacosRegistryOpts = append(nacosRegistryOpts, nacosRegistry.WithBackoffInit(d))
			}
		}
		if cfg.NacosRegistry.NacosRegistration.BackoffMax != "" {
			d, _ := time.ParseDuration(cfg.NacosRegistry.NacosRegistration.BackoffMax)
			if d > 0 {
				nacosRegistryOpts = append(nacosRegistryOpts, nacosRegistry.WithBackoffMax(d))
			}
		}
		iRegistry = nacosRegistry.New(cli, nacosRegistryOpts...)
		logField = logger.String("nacosAddress", cfg.NacosRegistry.NacosServer.IPAddr+":"+strconv.Itoa(cfg.NacosRegistry.NacosServer.Port))
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
