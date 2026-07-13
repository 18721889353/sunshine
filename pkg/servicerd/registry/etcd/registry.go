package etcd

import (
	"context"
	"errors"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// 声明 Registry 和 Discovery 接口的实现
var (
	_ registry.Registry  = &Registry{}
	_ registry.Discovery = &Registry{}
)

// Registry 是 etcd 注册表，同时实现了 registry.Registry 和 registry.Discovery。
type Registry struct {
	opts            *options           // 选项
	client          *clientv3.Client   // etcd 客户端
	kv              clientv3.KV        // etcd KV 客户端
	cancelHeartbeat context.CancelFunc // 心跳 goroutine 的取消函数
	verifyFailCount int                // 连续 verifyKey 失败次数，超过阈值触发主动重注册
}

// Option 是 etcd 注册表的选项类型。
type Option func(o *options)

// options 定义 etcd 注册表的选项结构体。
type options struct {
	ctx           context.Context // 上下文
	namespace     string          // 命名空间
	ttl           time.Duration   // TTL（生存时间）
	maxRetry      int             // 最大重试次数
	checkInterval int             // key 存在性校验间隔（心跳响应次数）
	backoffInit   time.Duration   // 重注册指数退避初始间隔
	backoffMax    time.Duration   // 重注册指数退避最大间隔
}

// defaultOptions 返回一个默认的 options 实例。
func defaultOptions() *options {
	return &options{
		ctx:           context.Background(), // 默认上下文
		namespace:     "/microservices",     // 默认命名空间
		ttl:           time.Second * 15,     // 默认 TTL 为 15 秒
		maxRetry:      5,                    // 默认最大重试次数为 5 次
		checkInterval: 3,                    // 默认每 3 次心跳校验一次 key 存在性
		backoffInit:   time.Second,          // 默认初始退避 1 秒
		backoffMax:    30 * time.Second,     // 默认最大退避 30 秒
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

// WithCheckInterval 设置 key 存在性校验间隔（心跳响应次数），0 或 1 表示每次心跳都校验。
func WithCheckInterval(n int) Option {
	return func(o *options) { o.checkInterval = n }
}

// WithBackoffInit 设置重注册指数退避的初始间隔，默认 1s。
func WithBackoffInit(d time.Duration) Option {
	return func(o *options) { o.backoffInit = d }
}

// WithBackoffMax 设置重注册指数退避的最大间隔，默认 30s。
func WithBackoffMax(d time.Duration) Option {
	return func(o *options) { o.backoffMax = d }
}

// New 创建一个新的 etcd 注册表实例。
func New(client *clientv3.Client, opts ...Option) *Registry {
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

// Register 注册服务实例。
func (r *Registry) Register(ctx context.Context, service *registry.ServiceInstance) error {
	tracer := otel.Tracer("etcd_registry")
	ctx, span := tracer.Start(ctx, "etcd.register", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("etcd.service_name", service.Name),
		attribute.String("etcd.service_id", service.ID),
		requestIDAttr(ctx),
	)

	key := fmt.Sprintf("%s/%s/%s", r.opts.namespace, service.Name, service.ID)
	value, err := marshal(service)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// 创建租约并写入 etcd
	leaseID, err := r.registerWithKV(ctx, key, value)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// 取消前一次心跳，防止 goroutine 泄漏
	if r.cancelHeartbeat != nil {
		r.cancelHeartbeat()
	}

	// 重置错误计数器，新注册从头开始统计
	r.verifyFailCount = 0

	// 启动新的心跳 goroutine
	var heartbeatCtx context.Context
	heartbeatCtx, r.cancelHeartbeat = context.WithCancel(r.opts.ctx)
	go r.heartBeat(heartbeatCtx, leaseID, key, value)

	span.SetStatus(codes.Ok, "registered")
	return nil
}

// Deregister 注销服务实例。
func (r *Registry) Deregister(ctx context.Context, service *registry.ServiceInstance) error {
	tracer := otel.Tracer("etcd_registry")
	_, span := tracer.Start(ctx, "etcd.deregister", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("etcd.service_name", service.Name),
		attribute.String("etcd.service_id", service.ID),
		requestIDAttr(ctx),
	)

	// 停止心跳
	if r.cancelHeartbeat != nil {
		r.cancelHeartbeat()
		r.cancelHeartbeat = nil
	}

	key := fmt.Sprintf("%s/%s/%s", r.opts.namespace, service.Name, service.ID)
	_, err := r.client.Delete(ctx, key)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "deregistered")
	return nil
}

// Close 关闭注册表，释放底层资源。
func (r *Registry) Close() error {
	if r.cancelHeartbeat != nil {
		r.cancelHeartbeat()
		r.cancelHeartbeat = nil
	}
	return nil
}

// GetService 根据服务名称获取服务实例列表。
func (r *Registry) GetService(ctx context.Context, name string) ([]*registry.ServiceInstance, error) {
	tracer := otel.Tracer("etcd_registry")
	_, span := tracer.Start(ctx, "etcd.get_service", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("etcd.service_name", name),
		requestIDAttr(ctx),
	)

	key := fmt.Sprintf("%s/%s", r.opts.namespace, name)
	resp, err := r.kv.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	items := make([]*registry.ServiceInstance, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		si, err := unmarshal(kv.Value)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		if si.Name != name {
			continue
		}
		items = append(items, si)
	}

	span.SetAttributes(attribute.Int("etcd.instance_count", len(items)))
	span.SetStatus(codes.Ok, "service queried")
	return items, nil
}

// Watch 根据服务名称创建一个观察者。
func (r *Registry) Watch(ctx context.Context, name string) (registry.Watcher, error) {
	key := fmt.Sprintf("%s/%s", r.opts.namespace, name)
	return newWatcher(ctx, key, name, r.client)
}

// registerWithKV 创建一个新的租约并将数据写入 etcd，返回租约 ID。
func (r *Registry) registerWithKV(ctx context.Context, key string, value string) (clientv3.LeaseID, error) {
	grant, err := r.client.Lease.Grant(ctx, int64(r.opts.ttl.Seconds()))
	if err != nil {
		return 0, err
	}

	_, err = r.client.Put(ctx, key, value, clientv3.WithLease(grant.ID))
	if err != nil {
		return 0, err
	}

	return grant.ID, nil
}

// tryReRegister 尝试重新注册，最多重试 maxRetry 次，每次超时 3 秒。
func (r *Registry) tryReRegister(ctx context.Context, key, value string) (clientv3.LeaseID, error) {
	for retryCnt := 0; retryCnt < r.opts.maxRetry; retryCnt++ {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		retryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		leaseID, err := r.registerWithKV(retryCtx, key, value)
		cancel()
		if err == nil {
			return leaseID, nil
		}
		logger.WarnWithCtx(ctx, "re-register attempt failed", logger.Int("attempt", retryCnt+1), logger.Err(err))
	}
	return 0, errors.New("re-register failed after max retries")
}

// reRegisterWithBackoff 无限重试注册，直到成功或 context 取消。
// 每次失败后指数退避：backoffInit, *2, *4, ..., backoffMax(max)。
// 返回新的 leaseID。
func (r *Registry) reRegisterWithBackoff(ctx context.Context, key, value string) clientv3.LeaseID {
	backoff := r.opts.backoffInit
	maxBackoff := r.opts.backoffMax

	for {
		if ctx.Err() != nil {
			return 0
		}

		newLeaseID, err := r.tryReRegister(ctx, key, value)
		if err == nil {
			logger.InfoWithCtx(ctx, "re-register succeeded after retry")
			return newLeaseID
		}

		logger.WarnWithCtx(ctx, "re-register failed, retry after backoff",
			logger.Duration("backoff", backoff),
			logger.Err(err))

		select {
		case <-ctx.Done():
			return 0
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// verifyKey 检查 etcd 中 key 是否存在，如果不存在则重新写入（防误删）。
// 优先使用已有 leaseID 重写（轻量），失败后回退到全量 tryReRegister。
// 连续 maxRetry 次 Get 失败时主动触发重注册，消除网络盲区。
// 返回新的 leaseID（可能不变）和 error。
func (r *Registry) verifyKey(ctx context.Context, key string, leaseID clientv3.LeaseID, value string) (clientv3.LeaseID, error) {
	resp, err := r.kv.Get(ctx, key)
	if err != nil {
		r.verifyFailCount++
		errMsg := fmt.Sprintf("verifyKey: Get failed (%d/%d)", r.verifyFailCount, r.opts.maxRetry)
		if r.verifyFailCount >= r.opts.maxRetry {
			logger.WarnWithCtx(ctx, "verifyKey: consecutive failures threshold reached, triggering proactive re-register",
				logger.Int("failCount", r.verifyFailCount), logger.Err(err))
			r.verifyFailCount = 0
			return r.tryReRegister(ctx, key, value)
		}
		logger.WarnWithCtx(ctx, errMsg, logger.Err(err))
		return leaseID, nil
	}
	r.verifyFailCount = 0 // Get 成功，重置计数器
	if len(resp.Kvs) > 0 {
		// key 正常存在，无需处理
		return leaseID, nil
	}

	// key 不存在（误删），尝试用当前 leaseID 重写
	logger.WarnWithCtx(ctx, "registration key missing (possibly deleted), re-writing", logger.String("key", key))
	_, putErr := r.client.Put(ctx, key, value, clientv3.WithLease(leaseID))
	if putErr == nil {
		return leaseID, nil
	}

	// 重写失败（lease 可能已过期），回退到全量重新注册
	logger.WarnWithCtx(ctx, "re-write with existing lease failed, fallback to full re-register", logger.Err(putErr))
	return r.tryReRegister(ctx, key, value)
}

// heartBeat 心跳维护，确保租约不被回收。
// 核心设计原则：永不退出（除非 ctx 取消），确保断网/假死/误删后自动恢复。
//   - KeepAlive 续租（防断连）
//   - 定期校验 key 存在性（防误删）
//   - 断开后指数退避无限重试（防假死/长断连）
func (r *Registry) heartBeat(ctx context.Context, leaseID clientv3.LeaseID, key string, value string) {
	checkCounter := 0

	for {
		if ctx.Err() != nil {
			return
		}

		kac, err := r.client.KeepAlive(ctx, leaseID)
		if err != nil {
			// KeepAlive 流建立失败，先用 KeepAliveOnce 轻量验证 lease 是否仍有效
			// 避免网络抖动导致不必要的全量重注册
			onceCtx, onceCancel := context.WithTimeout(ctx, 2*time.Second)
			_, keepOnceErr := r.client.KeepAliveOnce(onceCtx, leaseID)
			onceCancel()

			if keepOnceErr == nil {
				// lease 仍然有效，只是流断了，重试建立即可
				logger.WarnWithCtx(ctx, "keep alive stream failed but lease is still valid, retrying stream")
				continue
			}

			logger.WarnWithCtx(ctx, "keep alive error and lease invalid, re-register with backoff", logger.Err(err))
			leaseID = r.reRegisterWithBackoff(ctx, key, value)
			if ctx.Err() != nil {
				return
			}
			checkCounter = 0
			continue
		}

		// 遍历 KeepAlive 响应，每收到一次续租成功就递增计数器
		// 使用 select 模式确保能响应 context 取消，避免 for-range 阻塞
		for {
			select {
			case <-ctx.Done():
				return
			case kaResp, ok := <-kac:
				if !ok {
					// 通道关闭，需要重新注册（指数退避无限重试）
					logger.WarnWithCtx(ctx, "keep alive channel closed, re-register with backoff")
					leaseID = r.reRegisterWithBackoff(ctx, key, value)
					if ctx.Err() != nil {
						return
					}
					checkCounter = 0
					goto nextLoop
				}
				_ = kaResp
				checkCounter++

				// 达到校验间隔时，检查 key 是否存在（防误删）
				if r.opts.checkInterval <= 0 || checkCounter%r.opts.checkInterval == 0 {
					var verifyErr error
					leaseID, verifyErr = r.verifyKey(ctx, key, leaseID, value)
					if verifyErr != nil {
						logger.WarnWithCtx(ctx, "heartbeat: key verification failed, re-start keepalive", logger.Err(verifyErr))
						goto nextLoop
					}
					// 每次成功校验后重置计数器，确保不因历史失败累积而误触发
					r.verifyFailCount = 0
				}
			}
		}
	nextLoop:
	}
}

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("etcd.request_id", reqID)
		}
	}
	return attribute.String("etcd.request_id", "")
}
