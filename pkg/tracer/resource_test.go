package tracer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

// TestNewResourceDefaults 验证 NewResource 未设置字段时使用文档约定的默认值。
func TestNewResourceDefaults(t *testing.T) {
	r := NewResource()
	assert.NotNil(t, r)

	attrs := r.Attributes()
	assert.Contains(t, attrs, attribute.String("service.name", "demo-service"))
	assert.Contains(t, attrs, attribute.String("service.version", "v0.0.0"))
	assert.Contains(t, attrs, attribute.String("env", "dev"))
}

// TestNewResourceReadsOTELServiceName 验证服务名兜底顺序（P1-11）：
// 未显式设置时优先读环境变量 OTEL_SERVICE_NAME，生产漏配时不再静默上错误服务名。
func TestNewResourceReadsOTELServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "svc-from-env")
	r := NewResource()
	assert.Contains(t, r.Attributes(), attribute.String("service.name", "svc-from-env"))

	// 显式 WithServiceName 优先于环境变量
	r = NewResource(WithServiceName("explicit-svc"))
	assert.Contains(t, r.Attributes(), attribute.String("service.name", "explicit-svc"))
}

// TestNewResourceCustomAttributes 验证 ResourceOption 覆盖默认字段并追加自定义属性。
func TestNewResourceCustomAttributes(t *testing.T) {
	r := NewResource(
		WithServiceName("foo"),
		WithServiceVersion("v1.0"),
		WithEnvironment("prod"),
		WithAttributes(map[string]string{"instance.id": "pod-001"}),
	)

	attrs := r.Attributes()
	assert.Contains(t, attrs, attribute.String("service.name", "foo"))
	assert.Contains(t, attrs, attribute.String("service.version", "v1.0"))
	assert.Contains(t, attrs, attribute.String("env", "prod"))
	assert.Contains(t, attrs, attribute.String("instance.id", "pod-001"))
}

// TestWithAttributes 验证 WithAttributes 写入自定义属性字段，并复制 map（P1-9）：
// 调用方后续修改原 map 不影响已保存的配置，避免并发读写 data race。
func TestWithAttributes(t *testing.T) {
	testData := map[string]string{"k": "v"}
	o := new(resourceConfig)
	applyResourceOptions(o, WithAttributes(testData))
	assert.Equal(t, testData, o.attributes)

	testData["k"] = "changed"
	assert.Equal(t, "v", o.attributes["k"], "原 map 的后续修改不应影响配置")
}

// TestWithEnvironment 验证 WithEnvironment 写入环境字段。
func TestWithEnvironment(t *testing.T) {
	testData := "env"
	o := new(resourceConfig)
	applyResourceOptions(o, WithEnvironment(testData))
	assert.Equal(t, testData, o.environment)
}

// TestWithServiceName 验证 WithServiceName 写入服务名字段。
func TestWithServiceName(t *testing.T) {
	testData := "foo"
	o := new(resourceConfig)
	applyResourceOptions(o, WithServiceName(testData))
	assert.Equal(t, testData, o.serviceName)
}

// TestWithServiceVersion 验证 WithServiceVersion 写入版本字段。
func TestWithServiceVersion(t *testing.T) {
	testData := "v1.0"
	o := new(resourceConfig)
	applyResourceOptions(o, WithServiceVersion(testData))
	assert.Equal(t, testData, o.serviceVersion)
}

// TestApplyResourceOptions 验证 applyResourceOptions 按顺序应用多个 Option（后设置者覆盖先设置者）。
func TestApplyResourceOptions(t *testing.T) {
	o := new(resourceConfig)
	applyResourceOptions(o,
		WithServiceVersion("v0.1"),
		WithServiceVersion("v1.0"),
	)
	assert.Equal(t, "v1.0", o.serviceVersion)
}
