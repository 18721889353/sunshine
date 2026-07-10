package rpcclient

import (
	"context"
	"fmt"
	"github.com/18721889353/sunshine/pkg/nacoscli"
	nacosRegistry "github.com/18721889353/sunshine/pkg/servicerd/registry/nacos"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/etcdcli"
	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
	"github.com/18721889353/sunshine/pkg/servicerd/registry/etcd"
	"google.golang.org/grpc"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/pkg/grpc/grpccli"
	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	serverNameExampleConn *grpc.ClientConn
	serverNameExampleOnce sync.Once
	initCtx               = context.Background() // 初始化阶段使用的 context
)

// NewServerNameExampleRPCConn instantiate rpc client connection
func NewServerNameExampleRPCConn() {
	cfg := config.Get()

	serverName := "serverNameExample"
	var grpcClientCfg config.GrpcClient
	for _, cli := range cfg.GrpcClient {
		if strings.EqualFold(cli.Name, serverName) {
			grpcClientCfg = cli
			break
		}
	}
	if grpcClientCfg.Name == "" {
		panic(fmt.Sprintf("not found grpc service name '%v' in configuration file(yaml), "+
			"please add gprc service configuration in the configuration file(yaml) under the field grpcClient.", serverName))
	}

	var cliOptions = []grpccli.Option{
		grpccli.WithUnaryInterceptors(
			interceptor.UnaryClientLog(
				interceptor.WithLogFrom(config.Get().App.Name+strconv.Itoa(config.Get().App.MachineID)),
				interceptor.WithMaxLen(config.Get().Logger.MaxLen),
				interceptor.WithReplaceGRPCLogger(),
			),
			interceptor.UnaryClientRequestID(),
		),
		grpccli.WithEnableRequestID(),
		grpccli.WithEnableLog(logger.Get()),
	}

	// if service discovery is not used, connect directly to the rpc service using the ip and port
	endpoint := fmt.Sprintf("%s:%d", grpcClientCfg.Host, grpcClientCfg.Port)
	isUseDiscover := false

	// using service discovery
	if cfg.App.RegistryDiscoveryType != "" {
		var (
			discoveryEndpoint string
			discoverOption    grpccli.Option
		)
		if grpcClientCfg.RegistryDiscoveryType == "etcd" {
			discoveryEndpoint = "discovery:///" + grpcClientCfg.Name // format: discovery:///serverName
			var etcdOpts []etcdcli.Option
			if cfg.Etcd.EtcdClient.DialTimeout != "" {
				d, _ := time.ParseDuration(cfg.Etcd.EtcdClient.DialTimeout)
				if d > 0 {
					etcdOpts = append(etcdOpts, etcdcli.WithDialTimeout(d))
				}
			} else {
				etcdOpts = append(etcdOpts, etcdcli.WithDialTimeout(time.Second*5))
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
				panic(fmt.Sprintf("etcdcli.Init error: %v, addr: %v", err, cfg.Etcd.Addrs))
			}
			iDiscovery := etcd.New(cli)
			discoverOption = grpccli.WithDiscovery(iDiscovery)
		}

		if grpcClientCfg.RegistryDiscoveryType == "nacos" {
			discoveryEndpoint = "discovery:///" + grpcClientCfg.Name
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
				panic(fmt.Sprintf("nacoscli.NewNamingClient error: %v, addr: %s:%d", err, cfg.NacosRegistry.NacosServer.IPAddr, cfg.NacosRegistry.NacosServer.Port))
			}

			var nacosRegistryOpts []nacosRegistry.Option
			nacosRegistryOpts = append(nacosRegistryOpts,
				nacosRegistry.WithGroupName(cfg.NacosRegistry.NacosRegistration.GroupName),
				nacosRegistry.WithClusterName(cfg.NacosRegistry.NacosRegistration.ClusterName),
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
			iDiscovery := nacosRegistry.New(cli, nacosRegistryOpts...)
			discoverOption = grpccli.WithDiscovery(iDiscovery)
		}

		if discoverOption != nil {
			isUseDiscover = true
			endpoint = discoveryEndpoint
			cliOptions = append(cliOptions, discoverOption)
			cliOptions = append(cliOptions, grpccli.WithEnableLoadBalance()) // load balance
		}
	}

	// secure
	cliOptions = append(cliOptions, grpccli.WithSecure(
		grpcClientCfg.ClientSecure.Type,
		grpcClientCfg.ClientSecure.ServerName,
		grpcClientCfg.ClientSecure.CaFile,
		grpcClientCfg.ClientSecure.CertFile,
		grpcClientCfg.ClientSecure.KeyFile,
	))

	// token
	cliOptions = append(cliOptions, grpccli.WithToken(
		grpcClientCfg.ClientToken.Enable,
		grpcClientCfg.ClientToken.AppID,
		grpcClientCfg.ClientToken.AppKey,
	))

	if cfg.App.EnableTrace {
		cliOptions = append(cliOptions, grpccli.WithEnableTrace())
	}
	if cfg.App.EnableCircuitBreaker {
		cliOptions = append(cliOptions, grpccli.WithEnableCircuitBreaker())
	}
	if cfg.App.EnableMetrics {
		cliOptions = append(cliOptions, grpccli.WithEnableMetrics())
	}
	if grpcClientCfg.Timeout > 0 {
		cliOptions = append(cliOptions, grpccli.WithTimeout(time.Second*time.Duration(grpcClientCfg.Timeout)))
	}

	msg := "dial grpc server"
	if isUseDiscover {
		msg += " with service discovery from " + grpcClientCfg.RegistryDiscoveryType
	}
	logger.InfoWithCtx(initCtx, msg,
		logger.String("name", serverName),
		logger.String("endpoint", endpoint),
	)

	var err error
	serverNameExampleConn, err = grpccli.NewClient(endpoint, cliOptions...)
	if err != nil {
		panic(fmt.Sprintf("grpccli.NewClient error: %v, name: %s, endpoint: %s", err, serverName, endpoint))
	}
}

// GetServerNameExampleRPCConn get client conn
func GetServerNameExampleRPCConn() *grpc.ClientConn {
	if serverNameExampleConn == nil {
		serverNameExampleOnce.Do(func() {
			NewServerNameExampleRPCConn()
		})
	}

	return serverNameExampleConn
}

// CloseServerNameExampleRPCConn Close tears down the ClientConn and all underlying connections.
func CloseServerNameExampleRPCConn() error {
	if serverNameExampleConn == nil {
		return nil
	}

	return serverNameExampleConn.Close()
}
