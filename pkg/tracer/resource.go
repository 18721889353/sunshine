package tracer

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

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
	return func(o *resourceConfig) { o.serviceName = name }
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
func WithAttributes(attributes map[string]string) ResourceOption {
	return func(o *resourceConfig) { o.attributes = attributes }
}

type resourceConfig struct {
	serviceName    string
	serviceVersion string
	environment    string

	attributes map[string]string
}

// NewResource 创建描述当前应用的资源对象。
// 未设置的字段使用默认值：serviceName="demo-service", serviceVersion="v0.0.0", environment="dev"。
// Merge 失败时降级为仅使用自定义属性，不中断调用方。
func NewResource(opts ...ResourceOption) *resource.Resource {
	rc := &resourceConfig{
		serviceName:    "demo-service",
		serviceVersion: "v0.0.0",
		environment:    "dev",
	}
	applyResourceOptions(rc, opts...)

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
