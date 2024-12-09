// Package etcdcli 用于连接到 etcd 服务
package etcdcli

import (
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Init 连接到 etcd 服务
// 注意: 如果设置了 WithConfig(*clientv3.Config) 参数，则 endpoints 参数将被忽略！
func Init(endpoints []string, opts ...Option) (*clientv3.Client, error) {
	// 初始化默认选项
	o := defaultOptions()
	// 应用传递的选项
	o.apply(opts...)

	// 如果配置不为空，直接使用配置创建客户端
	if o.config != nil {
		//return clientv3.New(*o.config)
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
		Endpoints:            endpoints,          // etcd 服务器地址
		DialTimeout:          o.dialTimeout,      // 连接超时时间
		DialKeepAliveTime:    20 * time.Second,   // 保持连接的时间间隔
		DialKeepAliveTimeout: 10 * time.Second,   // 保持连接的超时时间
		AutoSyncInterval:     o.autoSyncInterval, // 自动同步间隔
		Logger:               o.logger,           // 日志记录器
		Username:             o.username,         // 用户名
		Password:             o.password,         // 密码
	}

	// 根据是否启用安全模式设置 gRPC 的传输凭证
	if !o.isSecure {
		// 使用不安全的凭证
		conf.DialOptions = append(conf.DialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		// 使用安全的凭证
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

	// 返回客户端
	return cli, nil
}
