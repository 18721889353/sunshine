package etcd

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/18721889353/sunshine/pkg/etcdcli"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// 声明 Registry 和 Discovery 接口的实现
var (
	_ registry.Registry  = &Registry{}
	_ registry.Discovery = &Registry{}
)

// Registry 是 etcd 注册表。
type Registry struct {
	opts   *options         // 选项
	client *clientv3.Client // etcd 客户端
	kv     clientv3.KV      // etcd KV 客户端
	lease  clientv3.Lease   // etcd 租约客户端
}

// Option 是 etcd 注册表的选项类型。
type Option func(o *options)

// options 定义 etcd 注册表的选项结构体。
type options struct {
	ctx       context.Context // 上下文
	namespace string          // 命名空间
	ttl       time.Duration   // TTL（生存时间）
	maxRetry  int             // 最大重试次数
}

// defaultOptions 返回一个默认的 options 实例。
func defaultOptions() *options {
	return &options{
		ctx:       context.Background(), // 默认上下文
		namespace: "/microservices",     // 默认命名空间
		ttl:       time.Second * 15,     // 默认 TTL 为 15 秒
		maxRetry:  5,                    // 默认最大重试次数为 5 次
	}
}

// WithContext 设置注册表的上下文。
func WithContext(ctx context.Context) Option {
	return func(o *options) { o.ctx = ctx }
}

// WithNamespace 设置注册表的命名空间。
func WithNamespace(ns string) Option {
	return func(o *options) { o.namespace = ns }
}

// WithRegisterTTL 设置注册的 TTL。
func WithRegisterTTL(ttl time.Duration) Option {
	return func(o *options) { o.ttl = ttl }
}

// WithMaxRetry 设置最大重试次数。
func WithMaxRetry(num int) Option {
	return func(o *options) { o.maxRetry = num }
}

// New 创建一个新的 etcd 注册表实例。
func New(client *clientv3.Client, opts ...Option) (r *Registry) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}
	return &Registry{
		opts:   o,
		client: client,
		kv:     clientv3.NewKV(client),
	}
}

// NewRegistry 创建一个新的 etcd 注册表实例。
// 注意：如果设置了 etcdcli.WithConfig(*clientv3.Config) 参数，则 etcdEndpoints 参数将被忽略！
func NewRegistry(etcdEndpoints []string, id string, instanceName string, instanceEndpoints []string, opts ...etcdcli.Option) (registry.Registry, *registry.ServiceInstance, error) {
	serviceInstance := registry.NewServiceInstance(id, instanceName, instanceEndpoints)

	cli, err := etcdcli.Init(etcdEndpoints, opts...)
	if err != nil {
		return nil, nil, err
	}

	return New(cli), serviceInstance, nil
}

// IsServiceRegistered 检查给定的服务实例是否已注册。
func (r *Registry) IsServiceRegistered(ctx context.Context, key string) (bool, error) {

	resp, err := r.kv.Get(ctx, key)
	if err != nil {
		return false, err
	}
	return len(resp.Kvs) > 0, nil
}

// Register 注册服务实例。
func (r *Registry) Register(ctx context.Context, service *registry.ServiceInstance) error {
	key := fmt.Sprintf("%s/%s/%s", r.opts.namespace, service.Name, service.ID)
	// 检查服务是否已注册
	if registered, err := r.IsServiceRegistered(ctx, key); err != nil {
		return err
	} else if registered {
		return nil
		//return fmt.Errorf("service %v already registered", key)
	}
	value, err := marshal(service)
	if err != nil {
		return err
	}
	if r.lease != nil {
		_ = r.lease.Close()
	}
	//创建一个新的 lease，用于保持服务的活跃状态
	r.lease = clientv3.NewLease(r.client)
	// etcd 中注册服务，并获取 lease ID
	leaseID, err := r.registerWithKV(ctx, key, value)
	if err != nil {
		return err
	}

	go r.heartBeat(r.opts.ctx, leaseID, key, value)
	return nil
}

// Deregister 注销服务实例。
func (r *Registry) Deregister(ctx context.Context, service *registry.ServiceInstance) error {
	defer func() {
		if r.lease != nil {
			_ = r.lease.Close()
		}
	}()
	key := fmt.Sprintf("%s/%s/%s", r.opts.namespace, service.Name, service.ID)
	_, err := r.client.Delete(ctx, key)
	return err
}

// GetService 根据服务名称获取服务实例列表。
func (r *Registry) GetService(ctx context.Context, name string) ([]*registry.ServiceInstance, error) {
	key := fmt.Sprintf("%s/%s", r.opts.namespace, name)
	resp, err := r.kv.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	items := make([]*registry.ServiceInstance, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		si, err := unmarshal(kv.Value)
		if err != nil {
			return nil, err
		}
		if si.Name != name {
			continue
		}
		items = append(items, si)
	}
	return items, nil
}

// Watch 根据服务名称创建一个观察者。
func (r *Registry) Watch(ctx context.Context, name string) (registry.Watcher, error) {
	key := fmt.Sprintf("%s/%s", r.opts.namespace, name)
	return newWatcher(ctx, key, name, r.client)
}

// registerWithKV 创建一个新的租约，并返回当前的租约 ID。
func (r *Registry) registerWithKV(ctx context.Context, key string, value string) (clientv3.LeaseID, error) {
	// 请求 etcd 创建一个新的 lease，有效期为 r.opts.ttl.Seconds() 秒
	grant, err := r.lease.Grant(ctx, int64(r.opts.ttl.Seconds()))
	if err != nil {
		return 0, err // 如果创建 lease 过程中出现错误，返回错误
	}

	// 将服务信息存储到 etcd 中，并关联到刚刚创建的 lease
	_, err = r.client.Put(ctx, key, value, clientv3.WithLease(grant.ID))
	if err != nil {
		return 0, err // 如果存储过程中出现错误，返回错误
	}

	return grant.ID, nil // 返回 lease ID，表示注册成功
}

// heartBeat 心跳维护，确保租约不被回收。
func (r *Registry) heartBeat(ctx context.Context, leaseID clientv3.LeaseID, key string, value string) {
	curLeaseID := leaseID
	kac, err := r.client.KeepAlive(ctx, leaseID)
	if err != nil {
		curLeaseID = 0
	}
	//rand.Seed(time.Now().Unix()) // 初始化随机数种子
	source := rand.NewSource(time.Now().UnixNano()) // 创建新的随机数源
	rng := rand.New(source)                         // 创建新的随机数生成器

	for {
		if curLeaseID == 0 {
			// 尝试重新注册
			var retreat []int
			for retryCnt := 0; retryCnt < r.opts.maxRetry; retryCnt++ {
				if ctx.Err() != nil {
					return
				}
				// 防止无限阻塞
				idChan := make(chan clientv3.LeaseID, 1)
				errChan := make(chan error, 1)
				cancelCtx, cancel := context.WithCancel(ctx)
				go func() {
					defer cancel()
					id, registerErr := r.registerWithKV(cancelCtx, key, value)
					if registerErr != nil {
						errChan <- registerErr
					} else {
						idChan <- id
					}
				}()

				select {
				case <-time.After(3 * time.Second):
					cancel()
					continue
				case <-errChan:
					continue
				case curLeaseID = <-idChan:
				}

				kac, err = r.client.KeepAlive(ctx, curLeaseID)
				if err == nil {
					break
				}
				retreat = append(retreat, 1<<retryCnt)
				//time.Sleep(time.Duration(retreat[rand.Intn(len(retreat))]) * time.Second)
				time.Sleep(time.Duration(retreat[rng.Intn(len(retreat))]) * time.Second) // 使用新的随机数生成器
			}
			if _, ok := <-kac; !ok {
				// 重试失败
				return
			}
		}

		select {
		case _, ok := <-kac:
			if !ok {
				if ctx.Err() != nil {
					// 通道因上下文取消而关闭
					return
				}
				// 需要重新注册
				curLeaseID = 0
				continue
			}
		case <-r.opts.ctx.Done():
			return
		}
	}
}
