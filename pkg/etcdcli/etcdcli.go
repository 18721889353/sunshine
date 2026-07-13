// Package etcdcli 用于连接到 etcd 服务
package etcdcli

import (
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// NewClient 创建一个 etcd 客户端连接。
// 注意: 如果设置了 WithConfig(*clientv3.Config) 参数，则 endpoints 参数将被忽略！
func NewClient(endpoints []string, opts ...Option) (*clientv3.Client, error) {
	// 初始化默认选项
	o := defaultOptions()
	// 应用传递的选项
	o.apply(opts...)

	// 如果配置不为空，直接使用配置创建客户端
	if o.config != nil {
		cli, err := clientv3.New(*o.config)
		if err != nil {
			return nil, fmt.Errorf("clientv3.New(*o.config) connecting to the etcd service error: %v", err)
		}
		return cli, nil
	}

	// 检查 endpoints 是否为空
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("etcd endpoints cannot be empty")
	}

	// 配置 etcd 客户端
	conf := clientv3.Config{
		Endpoints:            endpoints,
		DialTimeout:          o.dialTimeout,
		DialKeepAliveTime:    o.dialKeepAliveTime,
		DialKeepAliveTimeout: o.dialKeepAliveTimeout,
		AutoSyncInterval:     o.autoSyncInterval,
		Username:             o.username,
		Password:             o.password,
	}

	// 根据是否启用安全模式设置 gRPC 的传输凭证
	if !o.isSecure {
		conf.DialOptions = append(conf.DialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else if o.caFile != "" {
		// 双向 TLS（mTLS）
		tlsCert, err := credentials.NewClientTLSFromFile(o.caFile, o.serverNameOverride)
		if err != nil {
			return nil, fmt.Errorf("NewClientTLSFromFile(caFile) error: %v", err)
		}
		conf.DialOptions = append(conf.DialOptions, grpc.WithTransportCredentials(tlsCert))
	} else {
		// 单向 TLS
		cred, err := credentials.NewClientTLSFromFile(o.certFile, o.serverNameOverride)
		if err != nil {
			return nil, fmt.Errorf("NewClientTLSFromFile error: %v", err)
		}
		conf.DialOptions = append(conf.DialOptions, grpc.WithTransportCredentials(cred))
	}

	// 创建 etcd 客户端
	cli, err := clientv3.New(conf)
	if err != nil {
		return nil, fmt.Errorf("clientv3.New(conf) connecting to the etcd service error: %v", err)
	}

	return cli, nil
}
