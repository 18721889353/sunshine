// Package config 提供配置初始化辅助方法。
package config

import (
	nacosRegistry "github.com/18721889353/sunshine/pkg/servicerd/registry/nacos"
	"strconv"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/etcdcli"
	"github.com/18721889353/sunshine/pkg/nacoscli"
	"github.com/18721889353/sunshine/pkg/servicerd/registry/etcd"
)

// BuildClientOptions 从 EtcdClient 配置构建 etcdcli.Option 列表。
func (c *EtcdClient) BuildClientOptions() []etcdcli.Option {
	var opts []etcdcli.Option

	if c.DialTimeout != "" {
		if d, err := time.ParseDuration(c.DialTimeout); err == nil && d > 0 {
			opts = append(opts, etcdcli.WithDialTimeout(d))
		}
	}
	if c.Username != "" {
		opts = append(opts, etcdcli.WithAuth(c.Username, c.Password))
	}
	if c.AutoSyncInterval != "" {
		if d, err := time.ParseDuration(c.AutoSyncInterval); err == nil && d > 0 {
			opts = append(opts, etcdcli.WithAutoSyncInterval(d))
		}
	}
	if c.DialKeepAliveTime != "" {
		if d, err := time.ParseDuration(c.DialKeepAliveTime); err == nil && d > 0 {
			opts = append(opts, etcdcli.WithDialKeepAliveTime(d))
		}
	}
	if c.DialKeepAliveTimeout != "" {
		if d, err := time.ParseDuration(c.DialKeepAliveTimeout); err == nil && d > 0 {
			opts = append(opts, etcdcli.WithDialKeepAliveTimeout(d))
		}
	}
	if c.IsSecure {
		opts = append(opts, etcdcli.WithSecure(c.ServerNameOverride, c.CertFile))
	}

	return opts
}

// BuildRegistryOptions 从 EtcdRegistry 配置构建 etcd.Option 列表。
func (r *EtcdRegistry) BuildRegistryOptions() []etcd.Option {
	var opts []etcd.Option

	if r.Namespace != "" {
		opts = append(opts, etcd.WithNamespace(r.Namespace))
	}
	if r.TTL != "" {
		if d, err := time.ParseDuration(r.TTL); err == nil && d > 0 {
			opts = append(opts, etcd.WithRegisterTTL(d))
		}
	}
	if r.MaxRetry > 0 {
		opts = append(opts, etcd.WithMaxRetry(r.MaxRetry))
	}
	if r.CheckInterval > 0 {
		opts = append(opts, etcd.WithCheckInterval(r.CheckInterval))
	}
	if r.BackoffInit != "" {
		if d, err := time.ParseDuration(r.BackoffInit); err == nil && d > 0 {
			opts = append(opts, etcd.WithBackoffInit(d))
		}
	}
	if r.BackoffMax != "" {
		if d, err := time.ParseDuration(r.BackoffMax); err == nil && d > 0 {
			opts = append(opts, etcd.WithBackoffMax(d))
		}
	}

	return opts
}

// BuildClientOptions 从 NacosClient 配置构建 nacoscli.Option 列表。
func (c *NacosClient) BuildClientOptions() []nacoscli.Option {
	var opts []nacoscli.Option

	if c.Username != "" {
		opts = append(opts, nacoscli.WithAuth(c.Username, c.Password))
	}
	if c.TimeoutMs > 0 {
		opts = append(opts, nacoscli.WithTimeoutMs(uint64(c.TimeoutMs)))
	}

	return opts
}

// BuildRegistryOptions 从 NacosRegistry 配置构建 nacosRegistry.Option 列表。
func (r *NacosRegistry) BuildRegistryOptions() []nacosRegistry.Option {
	var opts []nacosRegistry.Option

	opts = append(opts,
		nacosRegistry.WithGroupName(r.GroupName),
		nacosRegistry.WithClusterName(r.ClusterName),
		nacosRegistry.WithEphemeral(r.Ephemeral),
		nacosRegistry.WithHealthy(r.Healthy),
		nacosRegistry.WithRegisterEnabled(r.RegisterEnabled),
	)
	if r.Weight > 0 {
		opts = append(opts, nacosRegistry.WithWeight(float64(r.Weight)))
	}
	if r.CheckInterval != "" {
		if d, err := time.ParseDuration(r.CheckInterval); err == nil && d > 0 {
			opts = append(opts, nacosRegistry.WithCheckInterval(d))
		}
	}
	if r.BackoffInit != "" {
		if d, err := time.ParseDuration(r.BackoffInit); err == nil && d > 0 {
			opts = append(opts, nacosRegistry.WithBackoffInit(d))
		}
	}
	if r.BackoffMax != "" {
		if d, err := time.ParseDuration(r.BackoffMax); err == nil && d > 0 {
			opts = append(opts, nacosRegistry.WithBackoffMax(d))
		}
	}

	return opts
}


// BuildNamingClientOptions 从 NacosInfo 配置构建 nacoscli.Option 列表。
// 组合了 NacosServer（网络连接）和 NacosClient（客户端行为）的配置。
func (n *NacosInfo) BuildNamingClientOptions() []nacoscli.Option {
	var opts []nacoscli.Option

	if n.NacosServer.Scheme != "" {
		opts = append(opts, nacoscli.WithScheme(n.NacosServer.Scheme))
	}
	if n.NacosServer.ContextPath != "" {
		opts = append(opts, nacoscli.WithContextPath(n.NacosServer.ContextPath))
	}
	opts = append(opts, n.NacosClient.BuildClientOptions()...)

	return opts
}

// ServerEndpoint 返回 etcd 服务器地址列表。
func (e *EtcdInfo) ServerEndpoint() []string {
	return e.EtcdServer.Addrs
}

// ServerEndpoint 返回创建 Nacos 命名客户端所需的三个核心参数。
func (n *NacosInfo) ServerEndpoint() (ipAddr string, port int, namespaceID string) {
	return n.NacosServer.IPAddr, n.NacosServer.Port, n.NacosClient.NamespaceID
}

// AddrDisplay 返回格式化的 Nacos 地址字符串。
func (n *NacosInfo) AddrDisplay() string {
	return n.NacosServer.IPAddr + ":" + strconv.Itoa(n.NacosServer.Port)
}

// AddrDisplay 返回格式化的 etcd 地址字符串。
func (e *EtcdInfo) AddrDisplay() string {
	return strings.Join(e.EtcdServer.Addrs, ",")
}
