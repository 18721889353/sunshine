// Package nacos 提供基于 Nacos 的服务注册与发现实现。
package nacos

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// 确保 Registry 实现了 registry.Registry 和 registry.Discovery 接口
var (
	_ registry.Registry  = &Registry{}
	_ registry.Discovery = &Registry{}
)

// Registry 是基于 Nacos 的服务注册表，实现了 registry.Registry 接口。
type Registry struct {
	client      naming_client.INamingClient // Nacos 命名客户端
	scheme      string                      // 端点 scheme（grpc / http）
	clusterName string                      // 集群名称
	groupName   string                      // 分组名称

	opts          *options           // 全部选项
	checkInterval time.Duration      // 实例存在性校验间隔
	cancelCheck   context.CancelFunc // 用于停止检查 goroutine
	stored        *storedInstance    // 最近一次注册的实例信息（供 verify 使用）
}

// storedInstance 保存已注册的服务实例快照，供 verify 和 re-register 使用。
type storedInstance struct {
	serviceName string
	host        string
	port        uint64
	metadata    map[string]string
}

// Option 是 Nacos 注册表的选项函数。
type Option func(o *options)

type options struct {
	clusterName     string
	groupName       string
	scheme          string
	checkInterval   time.Duration
	weight          float64
	ephemeral       bool
	healthy         bool
	registerEnabled bool
	backoffInit     time.Duration
	backoffMax      time.Duration
}

// WithClusterName 设置 Nacos 集群名称。
func WithClusterName(name string) Option {
	return func(o *options) { o.clusterName = name }
}

// WithGroupName 设置 Nacos 分组名称。
func WithGroupName(name string) Option {
	return func(o *options) { o.groupName = name }
}

// WithScheme 设置端点地址的 scheme（如 "grpc" 或 "http"），用于构建 Endpoints。
func WithScheme(scheme string) Option {
	return func(o *options) { o.scheme = scheme }
}

// WithCheckInterval 设置实例存在性校验间隔，0 表示不启用自动校验。
func WithCheckInterval(d time.Duration) Option {
	return func(o *options) { o.checkInterval = d }
}

// WithWeight 设置注册实例的权重，默认 1。
func WithWeight(weight float64) Option {
	return func(o *options) { o.weight = weight }
}

// WithEphemeral 设置是否为临时实例，默认 true。
func WithEphemeral(ephemeral bool) Option {
	return func(o *options) { o.ephemeral = ephemeral }
}

// WithHealthy 设置注册时实例的健康状态，默认 true。
func WithHealthy(healthy bool) Option {
	return func(o *options) { o.healthy = healthy }
}

// WithRegisterEnabled 设置注册时实例是否启用，默认 true。
func WithRegisterEnabled(enabled bool) Option {
	return func(o *options) { o.registerEnabled = enabled }
}

// WithBackoffInit 设置重注册指数退避的初始间隔，默认 1s。
func WithBackoffInit(d time.Duration) Option {
	return func(o *options) { o.backoffInit = d }
}

// WithBackoffMax 设置重注册指数退避的最大间隔，默认 30s。
func WithBackoffMax(d time.Duration) Option {
	return func(o *options) { o.backoffMax = d }
}

// New 创建一个基于 Nacos 的服务注册表。
// client 为已创建的 Nacos 命名客户端，可通过 nacoscli.NewNamingClient 获得。
func New(client naming_client.INamingClient, opts ...Option) *Registry {
	o := &options{
		clusterName:     "DEFAULT",
		groupName:       "DEFAULT_GROUP",
		scheme:          "grpc",
		checkInterval:   30 * time.Second,
		weight:          1,
		ephemeral:       true,
		healthy:         true,
		registerEnabled: true,
		backoffInit:     time.Second,
		backoffMax:      30 * time.Second,
	}
	for _, opt := range opts {
		opt(o)
	}

	return &Registry{
		client:        client,
		opts:          o,
		clusterName:   o.clusterName,
		groupName:     o.groupName,
		scheme:        o.scheme,
		checkInterval: o.checkInterval,
	}
}

// Register 向 Nacos 注册一个服务实例。
// 注册成功后启动后台校验 goroutine，定期检查实例是否仍存在（防误删）。
func (r *Registry) Register(ctx context.Context, service *registry.ServiceInstance) error {
	tracer := otel.Tracer("nacos_registry")
	ctx, span := tracer.Start(ctx, "nacos.register", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("nacos.service_name", service.Name),
		attribute.String("nacos.service_id", service.ID),
		requestIDAttr(ctx),
	)
	if len(service.Endpoints) == 0 {
		return fmt.Errorf("服务实例端点地址不能为空")
	}

	host, port, err := parseEndpoint(service.Endpoints[0])
	if err != nil {
		return fmt.Errorf("解析服务端点地址失败: %w", err)
	}

	_, err = r.client.RegisterInstance(vo.RegisterInstanceParam{
		Ip:          host,
		Port:        port,
		ServiceName: service.Name,
		GroupName:   r.groupName,
		ClusterName: r.clusterName,
		Weight:      r.opts.weight,
		Enable:      r.opts.registerEnabled,
		Healthy:     r.opts.healthy,
		Ephemeral:   r.opts.ephemeral,
		Metadata:    service.Metadata,
	})
	if err != nil {
		return err
	}

	// 取消前一次校验 goroutine
	r.stopCheck()

	// 保存当前注册实例快照
	r.stored = &storedInstance{
		serviceName: service.Name,
		host:        host,
		port:        port,
		metadata:    service.Metadata,
	}

	// 启用后台校验
	if r.checkInterval > 0 {
		var checkCtx context.Context
		checkCtx, r.cancelCheck = context.WithCancel(context.Background())
		go r.checkInstanceLoop(checkCtx)
	}

	span.SetStatus(codes.Ok, "registered")
	return nil
}

// Deregister 从 Nacos 注销一个服务实例。
func (r *Registry) Deregister(ctx context.Context, service *registry.ServiceInstance) error {
	tracer := otel.Tracer("nacos_registry")
	_, span := tracer.Start(ctx, "nacos.deregister", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("nacos.service_name", service.Name),
		attribute.String("nacos.service_id", service.ID),
		requestIDAttr(ctx),
	)
	// 停止校验 goroutine
	r.stopCheck()
	r.stored = nil

	if len(service.Endpoints) == 0 {
		return fmt.Errorf("服务实例端点地址不能为空")
	}

	host, port, err := parseEndpoint(service.Endpoints[0])
	if err != nil {
		return fmt.Errorf("解析服务端点地址失败: %w", err)
	}

	_, err = r.client.DeregisterInstance(vo.DeregisterInstanceParam{
		Ip:          host,
		Port:        port,
		ServiceName: service.Name,
		GroupName:   r.groupName,
		Ephemeral:   true,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "deregistered")
	return nil
}

// Close 关闭 Nacos 服务注册表。
func (r *Registry) Close() error {
	r.stopCheck()
	r.stored = nil
	return nil
}

// stopCheck 停止后台校验 goroutine。
func (r *Registry) stopCheck() {
	if r.cancelCheck != nil {
		r.cancelCheck()
		r.cancelCheck = nil
	}
}

// checkInstanceLoop 定期校验注册的实例是否存在，不存在则自动重新注册。
func (r *Registry) checkInstanceLoop(ctx context.Context) {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.verifyAndReRegister(ctx)
		}
	}
}

// verifyAndReRegister 检查已注册实例是否存在，不存在则指数退避无限重试重新注册。
// Select 查询失败（网络问题）时自动进入退避重试循环，不等下次 tick。
func (r *Registry) verifyAndReRegister(ctx context.Context) {
	inst := r.stored
	if inst == nil {
		return
	}

	instances, err := r.client.SelectInstances(vo.SelectInstancesParam{
		ServiceName: inst.serviceName,
		GroupName:   r.groupName,
		HealthyOnly: true,
		Clusters:    []string{r.clusterName},
	})
	if err != nil {
		logger.WarnWithCtx(ctx, "nacos: verify instance failed, retry with backoff",
			logger.String("service", inst.serviceName), logger.Err(err))
		r.reVerifyWithBackoff(ctx, inst)
		return
	}

	// 查找本实例是否仍在列表中
	for _, ins := range instances {
		if ins.Ip == inst.host && ins.Port == inst.port {
			return // 实例存在，无需处理
		}
	}

	// 实例不存在（可能被控制台误删），指数退避无限重试
	logger.WarnWithCtx(ctx, "nacos: instance missing, re-register with backoff",
		logger.String("service", inst.serviceName),
		logger.String("host", inst.host),
		logger.Uint64("port", inst.port))

	r.reRegisterInstanceWithBackoff(ctx, inst)
}

// reVerifyWithBackoff 在 SelectInstances 查询失败时启动退避重试循环。
// 避免网络抖动导致错过下次 tick 的 30 秒盲区。
func (r *Registry) reVerifyWithBackoff(ctx context.Context, inst *storedInstance) {
	backoff := r.opts.backoffInit
	maxBackoff := r.opts.backoffMax

	for {
		if ctx.Err() != nil {
			return
		}

		instances, err := r.client.SelectInstances(vo.SelectInstancesParam{
			ServiceName: inst.serviceName,
			GroupName:   r.groupName,
			HealthyOnly: true,
			Clusters:    []string{r.clusterName},
		})
		if err == nil {
			// 查询成功，检查实例是否仍在
			for _, ins := range instances {
				if ins.Ip == inst.host && ins.Port == inst.port {
					logger.InfoWithCtx(ctx, "nacos: re-verify succeeded, instance exists")
					return
				}
			}
			// 实例已不存在，走重注册
			logger.WarnWithCtx(ctx, "nacos: instance not found after re-verify, re-registering")
			r.reRegisterInstanceWithBackoff(ctx, inst)
			return
		}

		logger.WarnWithCtx(ctx, "nacos: re-verify failed, retry after backoff",
			logger.Duration("backoff", backoff), logger.Err(err))

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// reRegisterInstanceWithBackoff 无限重试注册实例，直到成功或 context 取消。
// 每次失败后指数退避：backoffInit, *2, *4, ..., backoffMax(max)。
func (r *Registry) reRegisterInstanceWithBackoff(ctx context.Context, inst *storedInstance) {
	backoff := r.opts.backoffInit
	maxBackoff := r.opts.backoffMax

	for {
		if ctx.Err() != nil {
			return
		}

		logger.InfoWithCtx(ctx, "nacos: re-registering instance",
			logger.String("service", inst.serviceName),
			logger.String("host", inst.host),
			logger.Uint64("port", inst.port))

		_, err := r.client.RegisterInstance(vo.RegisterInstanceParam{
			Ip:          inst.host,
			Port:        inst.port,
			ServiceName: inst.serviceName,
			GroupName:   r.groupName,
			ClusterName: r.clusterName,
			Weight:      r.opts.weight,
			Enable:      r.opts.registerEnabled,
			Healthy:     r.opts.healthy,
			Ephemeral:   r.opts.ephemeral,
			Metadata:    inst.metadata,
		})
		if err == nil {
			logger.InfoWithCtx(ctx, "nacos: re-register succeeded")
			return
		}

		logger.WarnWithCtx(ctx, "nacos: re-register failed, retry after backoff",
			logger.Duration("backoff", backoff),
			logger.Err(err))

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// GetService 根据服务名称获取服务实例列表。
func (r *Registry) GetService(ctx context.Context, name string) ([]*registry.ServiceInstance, error) {
	tracer := otel.Tracer("nacos_registry")
	_, span := tracer.Start(ctx, "nacos.get_service", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("nacos.service_name", name),
		requestIDAttr(ctx),
	)
	instances, err := r.client.SelectInstances(vo.SelectInstancesParam{
		ServiceName: name,
		GroupName:   r.groupName,
		HealthyOnly: true,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	result := instancesToServiceInstances(instances, r.scheme)
	span.SetAttributes(attribute.Int("nacos.instance_count", len(result)))
	span.SetStatus(codes.Ok, "service queried")
	return result, nil
}

// Watch 根据服务名称创建一个观察者，使用 Nacos Subscribe 机制监听服务变化。
func (r *Registry) Watch(ctx context.Context, name string) (registry.Watcher, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	w := &watcher{
		serviceName: name,
		groupName:   r.groupName,
		clusterName: r.clusterName,
		scheme:      r.scheme,
		client:      r.client,
		ch:          make(chan []*registry.ServiceInstance, 64),
	}
	w.ctx, w.cancel = context.WithCancel(ctx)

	// 订阅 Nacos 服务变更
	err := r.client.Subscribe(&vo.SubscribeParam{
		ServiceName:       name,
		GroupName:         r.groupName,
		SubscribeCallback: w.callback,
	})
	if err != nil {
		w.cancel()
		return nil, fmt.Errorf("nacos subscribe failed: %w", err)
	}

	return w, nil
}

// parseEndpoint 解析端点地址，返回 host、port 和 error。
// 支持的格式：grpc://host:port 或 http://host:port 或 host:port。
func parseEndpoint(endpoint string) (string, uint64, error) {
	raw := strings.TrimPrefix(endpoint, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "grpc://")

	host, portStr, err := net.SplitHostPort(raw)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, uint64(port), nil
}

// instancesToServiceInstances 将 Nacos model.Instance 列表转换为 registry.ServiceInstance 列表。
func instancesToServiceInstances(instances []model.Instance, scheme string) []*registry.ServiceInstance {
	result := make([]*registry.ServiceInstance, 0, len(instances))
	for _, inst := range instances {
		endpoint := fmt.Sprintf("%s://%s:%d", scheme, inst.Ip, inst.Port)
		result = append(result, &registry.ServiceInstance{
			ID:        inst.InstanceId,
			Name:      inst.ServiceName,
			Endpoints: []string{endpoint},
			Metadata:  inst.Metadata,
		})
	}
	return result
}

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("nacos.request_id", reqID)
		}
	}
	return attribute.String("nacos.request_id", "")
}
