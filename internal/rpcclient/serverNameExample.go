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
			cli, err := etcdcli.Init(cfg.EtcdInfo.ServerEndpoint(), cfg.EtcdInfo.EtcdClient.BuildClientOptions()...)
			if err != nil {
				panic(fmt.Sprintf("etcdcli.Init error: %v, addr: %v", err, cfg.EtcdInfo.ServerEndpoint()))
			}
			iDiscovery := etcd.New(cli)
			discoverOption = grpccli.WithDiscovery(iDiscovery)
		}

		if grpcClientCfg.RegistryDiscoveryType == "nacos" {
			discoveryEndpoint = "discovery:///" + grpcClientCfg.Name
			ipAddr, port, namespaceID := cfg.NacosInfo.ServerEndpoint()
			cli, err := nacoscli.NewNamingClient(ipAddr, port, namespaceID, cfg.NacosInfo.BuildNamingClientOptions()...)
			if err != nil {
				panic(fmt.Sprintf("nacoscli.NewNamingClient error: %v, addr: %s:%d", err, cfg.NacosInfo.NacosServer.IPAddr, cfg.NacosInfo.NacosServer.Port))
			}
			iDiscovery := nacosRegistry.New(cli, cfg.NacosInfo.NacosRegistry.BuildRegistryOptions()...)
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
