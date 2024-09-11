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

var (
	_ registry.Registry  = &Registry{}
	_ registry.Discovery = &Registry{}
)

// Option is etcd registry option.
type Option func(o *options)

type options struct {
	ctx       context.Context
	namespace string
	ttl       time.Duration
	maxRetry  int
}

func defaultOptions() *options {
	return &options{
		ctx:       context.Background(),
		namespace: "/microservices",
		ttl:       time.Second * 15,
		maxRetry:  5,
	}
}

// WithContext with registry context.
func WithContext(ctx context.Context) Option {
	return func(o *options) { o.ctx = ctx }
}

// WithNamespace with registry namespace.
func WithNamespace(ns string) Option {
	return func(o *options) { o.namespace = ns }
}

// WithRegisterTTL with register ttl.
func WithRegisterTTL(ttl time.Duration) Option {
	return func(o *options) { o.ttl = ttl }
}

// WithMaxRetry set max retry times.
func WithMaxRetry(num int) Option {
	return func(o *options) { o.maxRetry = num }
}

// NewRegistry instantiating the etcd registry
// Note: If the etcdcli.WithConfig(*clientv3.Config) parameter is set, the etcdEndpoints parameter is ignored!
func NewRegistry(etcdEndpoints []string, id string, instanceName string, instanceEndpoints []string, opts ...etcdcli.Option) (registry.Registry, *registry.ServiceInstance, error) {
	serviceInstance := registry.NewServiceInstance(id, instanceName, instanceEndpoints)

	cli, err := etcdcli.Init(etcdEndpoints, opts...)
	if err != nil {
		return nil, nil, err
	}

	return New(cli), serviceInstance, nil
}

// Registry is etcd registry.
type Registry struct {
	opts       *options
	EtcdClient *clientv3.Client
	kv         clientv3.KV
	lease      clientv3.Lease
}

// New create a etcd registry
func New(client *clientv3.Client, opts ...Option) (r *Registry) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}
	return &Registry{
		opts:       o,
		EtcdClient: client,
		kv:         clientv3.NewKV(client),
	}
}

// IsServiceRegistered 检查给定的服务实例是否已注册。
func (r *Registry) IsServiceRegistered(ctx context.Context, service *registry.ServiceInstance) (bool, error) {
	key := fmt.Sprintf("%s/%s/%s", r.opts.namespace, service.Name, service.ID)
	resp, err := r.kv.Get(ctx, key)
	if err != nil {
		return false, err
	}
	return len(resp.Kvs) > 0, nil
}

// Register the registration.
func (r *Registry) Register(ctx context.Context, service *registry.ServiceInstance) error {
	// 检查服务是否已注册
	if registered, err := r.IsServiceRegistered(ctx, service); err != nil {
		return err
	} else if registered {
		return fmt.Errorf("service already registered")
	}
	key := fmt.Sprintf("%s/%s/%s", r.opts.namespace, service.Name, service.ID)
	value, err := marshal(service)
	if err != nil {
		return err
	}
	if r.lease != nil {
		_ = r.lease.Close()
	}
	r.lease = clientv3.NewLease(r.EtcdClient)
	leaseID, err := r.registerWithKV(ctx, key, value)
	if err != nil {
		return err
	}

	go r.heartBeat(r.opts.ctx, leaseID, key, value)
	return nil
}

// Deregister the registration.
func (r *Registry) Deregister(ctx context.Context, service *registry.ServiceInstance) error {
	defer func() {
		if r.lease != nil {
			_ = r.lease.Close()
		}
	}()
	key := fmt.Sprintf("%s/%s/%s", r.opts.namespace, service.Name, service.ID)
	_, err := r.EtcdClient.Delete(ctx, key)
	return err
}

// GetService return the service instances in memory according to the service name.
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

// Watch creates a watcher according to the service name.
func (r *Registry) Watch(ctx context.Context, name string) (registry.Watcher, error) {
	key := fmt.Sprintf("%s/%s", r.opts.namespace, name)
	return newWatcher(ctx, key, name, r.EtcdClient)
}

// registerWithKV create a new lease, return current leaseID
func (r *Registry) registerWithKV(ctx context.Context, key string, value string) (clientv3.LeaseID, error) {
	grant, err := r.lease.Grant(ctx, int64(r.opts.ttl.Seconds()))
	if err != nil {
		return 0, err
	}
	_, err = r.EtcdClient.Put(ctx, key, value, clientv3.WithLease(grant.ID))
	if err != nil {
		return 0, err
	}
	return grant.ID, nil
}

func (r *Registry) heartBeat(ctx context.Context, leaseID clientv3.LeaseID, key string, value string) {
	curLeaseID := leaseID
	kac, err := r.EtcdClient.KeepAlive(ctx, leaseID)
	if err != nil {
		curLeaseID = 0
	}
	rand.Seed(time.Now().Unix()) //nolint

	for {
		if curLeaseID == 0 {
			// try to registerWithKV
			retreat := []int{}
			for retryCnt := 0; retryCnt < r.opts.maxRetry; retryCnt++ {
				if ctx.Err() != nil {
					return
				}
				// prevent infinite blocking
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

				kac, err = r.EtcdClient.KeepAlive(ctx, curLeaseID)
				if err == nil {
					break
				}
				retreat = append(retreat, 1<<retryCnt)
				time.Sleep(time.Duration(retreat[rand.Intn(len(retreat))]) * time.Second)
			}
			if _, ok := <-kac; !ok {
				// retry failed
				return
			}
		}

		select {
		case _, ok := <-kac:
			if !ok {
				if ctx.Err() != nil {
					// channel closed due to context cancel
					return
				}
				// need to retry registration
				curLeaseID = 0
				continue
			}
		case <-r.opts.ctx.Done():
			return
		}
	}
}
