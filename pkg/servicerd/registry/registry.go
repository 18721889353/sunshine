// Package registry 是一个服务注册库，支持 etcd、consul 和 nacos。
package registry

import (
	"context"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Registry 是服务注册器接口。
type Registry interface {
	// Register 注册服务实例。
	Register(ctx context.Context, service *ServiceInstance) (*clientv3.Client, error)
	// Deregister 取消注册服务实例。
	Deregister(ctx context.Context, service *ServiceInstance) error
}

// Discovery 是服务发现接口。
type Discovery interface {
	// GetService 根据服务名称返回内存中的服务实例列表。
	GetService(ctx context.Context, serviceName string) ([]*ServiceInstance, error)
	// Watch 根据服务名称创建一个观察者。
	Watch(ctx context.Context, serviceName string) (Watcher, error)
}

// Watcher 是服务观察者接口。
type Watcher interface {
	// Next 返回服务实例列表，在以下两种情况下：
	// 1. 第一次观察且服务实例列表不为空。
	// 2. 发现任何服务实例变化。
	// 如果上述两个条件都不满足，则会阻塞，直到 context 到期或取消。
	Next() ([]*ServiceInstance, error)
	// Stop 关闭观察者。
	Stop() error
}

// ServiceInstance 是发现系统中的一个服务实例。
type ServiceInstance struct {
	// ID 是注册时的唯一实例 ID。
	ID string `json:"id"`
	// Name 是注册时的服务名称。
	Name string `json:"name"`
	// Version 是编译版本。
	Version string `json:"version"`
	// Metadata 是与服务实例关联的键值对元数据。
	Metadata map[string]string `json:"metadata"`
	// Endpoints 是服务实例的端点地址。
	// 格式示例：
	//   http://127.0.0.1:8000?isSecure=false
	//   grpc://127.0.0.1:9000?isSecure=false
	Endpoints []string `json:"endpoints"`
}

// NewServiceInstance 创建一个新的服务实例。
func NewServiceInstance(id string, name string, endpoints []string, opts ...Option) *ServiceInstance {
	o := defaultOptions()
	o.apply(opts...)

	return &ServiceInstance{
		ID:        id,
		Name:      name,
		Endpoints: endpoints,
		Version:   o.version,
		Metadata:  o.metadata,
	}
}
