## discovery

Service discovery, corresponding to the service [registry](../registry), supports etcd, consul and nacos.

### Example of use

```go
    import "github.com/18721889353/sunshine/pkg/servicerd/discovery"

    var cliOptions = []grpccli.Option{}
    var endpoint string

	switch grpcClientCfg.RegistryDiscoveryType {
	case "etcd":
		endpoint = "discovery:///" + grpcClientCfg.Name // Connecting to grpc services by service name
		cli, err := etcdcli.Init(cfg.Etcd.Addrs, etcdcli.WithDialTimeout(time.Second*5))
		if err != nil {
			panic(fmt.Sprintf("etcdcli.Init error: %v, addr: %s", err, cfg.Etcd.Addrs))
		}
		iDiscovery := etcd.New(cli)
		cliOptions = append(cliOptions, grpccli.WithDiscovery(iDiscovery))
	}

    serverNameExampleConn, err = grpccli.DialInsecure(context.Background(), endpoint, cliOptions...)
    if err != nil {
        panic(fmt.Sprintf("dial rpc server failed: %v, endpoint: %s", err, endpoint))
    }
```