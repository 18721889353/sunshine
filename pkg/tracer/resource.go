package tracer

import (
	"context"
	"os"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/18721889353/sunshine/pkg/logger"
)

// warnDefaultServiceNameOnce 限制「默认服务名」警告只输出一次：
// NewResource 可能在多处调用（测试、多组件初始化），逐次告警会刷屏；
// 该警告的语义是「提示配置缺失」，首次出现已足够引起注意。
var warnDefaultServiceNameOnce sync.Once

// ResourceOption 函数选项，用于修改 resourceConfig 字段值。
type ResourceOption func(*resourceConfig)

// applyResourceOptions 依次应用所有 ResourceOption 到目标配置。
func applyResourceOptions(cfg *resourceConfig, opts ...ResourceOption) {
	for _, opt := range opts {
		opt(cfg)
	}
}

// WithServiceName 设置服务名称。
func WithServiceName(name string) ResourceOption {
	return func(o *resourceConfig) {
		o.serviceName = name
		o.serviceNameSet = true
	}
}

// WithServiceVersion 设置服务版本。
func WithServiceVersion(version string) ResourceOption {
	return func(o *resourceConfig) { o.serviceVersion = version }
}

// WithEnvironment 设置服务环境（如 dev/staging/prod）。
func WithEnvironment(environment string) ResourceOption {
	return func(o *resourceConfig) { o.environment = environment }
}

// WithAttributes 设置自定义服务属性（键值对）。
// 传入的 map 会被复制，调用方后续修改原 map 不影响已保存的配置（避免并发读写 data race）。
func WithAttributes(attributes map[string]string) ResourceOption {
	return func(o *resourceConfig) { o.attributes = copyStringMap(attributes) }
}

type resourceConfig struct {
	serviceName    string
	serviceVersion string
	environment    string

	// serviceNameSet 标记服务名是否被显式设置，决定兜底策略是否生效
	serviceNameSet bool

	attributes map[string]string
}

// NewResource 创建描述当前应用的资源对象。
// 未设置的字段使用默认值：serviceName="demo-service", serviceVersion="v0.0.0", environment="dev"。
// 服务名兜底顺序：WithServiceName 显式设置 > 环境变量 OTEL_SERVICE_NAME > 默认值（打警告日志）——
// 生产漏配时静默上错误服务名比启动失败更难排查，故降级为警告而非报错。
// Merge 失败时降级为仅使用自定义属性，不中断调用方。
func NewResource(opts ...ResourceOption) *resource.Resource {
	rc := &resourceConfig{
		serviceName:    "demo-service",
		serviceVersion: "v0.0.0",
		environment:    "dev",
	}
	applyResourceOptions(rc, opts...)

	if !rc.serviceNameSet {
		if env := os.Getenv("OTEL_SERVICE_NAME"); env != "" {
			rc.serviceName = env
		} else {
			warnDefaultServiceNameOnce.Do(func() {
				logger.WarnWithCtx(context.Background(),
					"[tracer] 未显式设置服务名且未检测到 OTEL_SERVICE_NAME，使用默认 demo-service，生产环境请通过 WithServiceName 显式设置（本警告仅输出一次）")
			})
		}
	}

	kvs := []attribute.KeyValue{
		attribute.String("service.name", rc.serviceName),
		attribute.String("service.version", rc.serviceVersion),
		attribute.String("env", rc.environment),
	}
	for k, v := range rc.attributes {
		kvs = append(kvs, attribute.String(k, v))
	}

	r, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("", kvs...),
	)
	if err != nil {
		// Merge 冲突时降级为仅使用自定义属性，避免中断业务
		return resource.NewWithAttributes("", kvs...)
	}
	return r
}
