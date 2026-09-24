package nacoscli

import (
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// defaultOptions 测试
// ---------------------------------------------------------------------------

// TestDefaultOptions 验证默认配置值。
func TestDefaultOptions(t *testing.T) {
	o := defaultOptions()
	assert.Equal(t, 5000, o.timeoutMs)
	assert.Equal(t, 30*time.Second, o.getTimeout)
	assert.Equal(t, 5*time.Second, o.createDelay)
	assert.Equal(t, 0, o.maxRetries)
	assert.Equal(t, "", o.ipAddr)
	assert.Equal(t, 0, o.port)
}

// ---------------------------------------------------------------------------
// 单个 Option 函数测试
// ---------------------------------------------------------------------------

// TestWithIPAddr 设置服务器地址。
func TestWithIPAddr(t *testing.T) {
	o := defaultOptions()
	WithIPAddr("10.0.0.1")(o)
	assert.Equal(t, "10.0.0.1", o.ipAddr)
}

// TestWithIPAddrEmpty 验证空字符串地址被设置。
func TestWithIPAddrEmpty(t *testing.T) {
	o := defaultOptions()
	WithIPAddr("")(o)
	assert.Equal(t, "", o.ipAddr, "空字符串地址应被设置")
}

// TestWithPort 设置端口。
func TestWithPort(t *testing.T) {
	o := defaultOptions()
	WithPort(8848)(o)
	assert.Equal(t, 8848, o.port)
}

// TestWithScheme 设置协议。
func TestWithScheme(t *testing.T) {
	o := defaultOptions()
	WithScheme("grpc")(o)
	assert.Equal(t, "grpc", o.scheme)
}

// TestWithContextPath 设置上下文路径。
func TestWithContextPath(t *testing.T) {
	o := defaultOptions()
	WithContextPath("/nacos")(o)
	assert.Equal(t, "/nacos", o.contextPath)
}

// TestWithNamespaceID 设置命名空间。
func TestWithNamespaceID(t *testing.T) {
	o := defaultOptions()
	WithNamespaceID("prod")(o)
	assert.Equal(t, "prod", o.namespaceID)
}

// TestWithTimeoutMs 设置请求超时。
func TestWithTimeoutMs(t *testing.T) {
	o := defaultOptions()
	WithTimeoutMs(3000)(o)
	assert.Equal(t, 3000, o.timeoutMs)
}

// TestWithAuth 设置认证信息。
func TestWithAuth(t *testing.T) {
	o := defaultOptions()
	WithAuth("admin", "secret")(o)
	assert.Equal(t, "admin", o.username)
	assert.Equal(t, "secret", o.password)
}

// TestWithClientConfig 设置完整 ClientConfig。
func TestWithClientConfig(t *testing.T) {
	cc := &constant.ClientConfig{NamespaceId: "test-ns"}
	o := defaultOptions()
	WithClientConfig(cc)(o)
	assert.Equal(t, cc, o.clientConfig)
}

// TestWithClientConfigNil 验证 nil ClientConfig 被设置。
func TestWithClientConfigNil(t *testing.T) {
	o := defaultOptions()
	WithClientConfig(nil)(o)
	assert.Nil(t, o.clientConfig, "nil ClientConfig 应被设置")
}

// TestWithServerConfigs 设置完整 ServerConfigs。
func TestWithServerConfigs(t *testing.T) {
	sc := []constant.ServerConfig{{IpAddr: "10.0.0.1", Port: 8848}}
	o := defaultOptions()
	WithServerConfigs(sc)(o)
	assert.Equal(t, sc, o.serverConfigs)
}

// TestWithMaxRetries 设置最大重试次数。
func TestWithMaxRetries(t *testing.T) {
	o := defaultOptions()
	WithMaxRetries(5)(o)
	assert.Equal(t, 5, o.maxRetries)
}

// TestWithMaxRetries0 验证显式 WithMaxRetries(0) 可将非零值覆盖为 0（无限重试）。
func TestWithMaxRetries0(t *testing.T) {
	o := defaultOptions()
	o.maxRetries = 3
	WithMaxRetries(0)(o)
	assert.Equal(t, 0, o.maxRetries, "WithMaxRetries(0) 应覆盖为 0")
}

// TestWithCreateDelay 设置重试延迟。
func TestWithCreateDelay(t *testing.T) {
	o := defaultOptions()
	WithCreateDelay(10 * time.Second)(o)
	assert.Equal(t, 10*time.Second, o.createDelay)
}

// TestWithCreateDelayZero 验证零值延迟被接受。
func TestWithCreateDelayZero(t *testing.T) {
	o := defaultOptions()
	WithCreateDelay(0)(o)
	assert.Equal(t, time.Duration(0), o.createDelay, "零值延迟应被接受")
}

// TestWithGetTimeout 设置 GetConfig 拉取超时。
func TestWithGetTimeout(t *testing.T) {
	o := defaultOptions()
	WithGetTimeout(10 * time.Second)(o)
	assert.Equal(t, 10*time.Second, o.getTimeout)
}

// TestWithGetTimeoutZero 验证零值超时被设置。
func TestWithGetTimeoutZero(t *testing.T) {
	o := defaultOptions()
	WithGetTimeout(0)(o)
	assert.Equal(t, time.Duration(0), o.getTimeout, "零值 getTimeout 应被设置")
}

// ---------------------------------------------------------------------------
// 负数参数校验测试
// ---------------------------------------------------------------------------

// TestWithPortNegative 负数端口应被忽略。
func TestWithPortNegative(t *testing.T) {
	o := defaultOptions()
	WithPort(-1)(o)
	assert.Equal(t, 0, o.port, "负数端口应被忽略")
}

// TestWithTimeoutMsNegative 负数超时应被忽略。
func TestWithTimeoutMsNegative(t *testing.T) {
	o := defaultOptions()
	WithTimeoutMs(-100)(o)
	assert.Equal(t, 5000, o.timeoutMs, "负数超时应被忽略")
}

// TestWithMaxRetriesNegative 负数重试次数应被忽略。
func TestWithMaxRetriesNegative(t *testing.T) {
	o := defaultOptions()
	o.maxRetries = 10
	WithMaxRetries(-1)(o)
	assert.Equal(t, 10, o.maxRetries, "负数 maxRetries 应被忽略")
}

// TestWithCreateDelayNegative 负数延迟应被忽略。
func TestWithCreateDelayNegative(t *testing.T) {
	o := defaultOptions()
	WithCreateDelay(-1 * time.Second)(o)
	assert.Equal(t, 5*time.Second, o.createDelay, "负数延迟应被忽略")
}

// TestWithPortZero 零值端口可设置。
func TestWithPortZero(t *testing.T) {
	o := defaultOptions()
	WithPort(0)(o)
	assert.Equal(t, 0, o.port)
}

// TestWithTimeoutMsZero 零值超时应正常设置。
func TestWithTimeoutMsZero(t *testing.T) {
	o := defaultOptions()
	WithTimeoutMs(0)(o)
	assert.Equal(t, 0, o.timeoutMs)
}

// ---------------------------------------------------------------------------
// Option 优先级与覆盖测试
// ---------------------------------------------------------------------------

// TestOptionPriority 验证 Option 优先级：后设置的覆盖先设置的。
func TestOptionPriority(t *testing.T) {
	o := defaultOptions()
	o.apply(
		WithIPAddr("first"),
		WithPort(1111),
		WithIPAddr("second"),
		WithPort(2222),
		WithNamespaceID("ns"),
		WithAuth("user", "pass"),
		WithTimeoutMs(3000),
	)

	assert.Equal(t, "second", o.ipAddr)
	assert.Equal(t, 2222, o.port)
	assert.Equal(t, "ns", o.namespaceID)
	assert.Equal(t, "user", o.username)
	assert.Equal(t, "pass", o.password)
	assert.Equal(t, 3000, o.timeoutMs)
}

// TestOptionPriorityWithServerConfigs 验证 WithServerConfigs 覆盖单字段。
func TestOptionPriorityWithServerConfigs(t *testing.T) {
	serverConfigs := []constant.ServerConfig{
		{IpAddr: "10.0.0.1", Port: 8848},
	}
	o := defaultOptions()
	o.apply(
		WithIPAddr("192.168.1.1"),
		WithPort(9848),
		WithServerConfigs(serverConfigs),
	)
	// serverConfigs 被设置，单字段仍保留但 buildConfigs 优先使用 serverConfigs
	assert.Equal(t, serverConfigs, o.serverConfigs)
}

// TestOptionApplyMultiple 验证 apply 批量应用多个 Option。
func TestOptionApplyMultiple(t *testing.T) {
	o := defaultOptions()
	o.apply(
		WithIPAddr("10.0.0.1"),
		WithPort(8848),
		WithScheme("http"),
		WithContextPath("/nacos"),
		WithNamespaceID("dev"),
		WithTimeoutMs(3000),
		WithGetTimeout(10*time.Second),
		WithAuth("user", "pass"),
	)
	assert.Equal(t, "10.0.0.1", o.ipAddr)
	assert.Equal(t, 8848, o.port)
	assert.Equal(t, "http", o.scheme)
	assert.Equal(t, "/nacos", o.contextPath)
	assert.Equal(t, "dev", o.namespaceID)
	assert.Equal(t, 3000, o.timeoutMs)
	assert.Equal(t, 10*time.Second, o.getTimeout)
	assert.Equal(t, "user", o.username)
	assert.Equal(t, "pass", o.password)
}

// TestOptionApplyEmpty 验证空 apply 不影响默认值。
func TestOptionApplyEmpty(t *testing.T) {
	o := defaultOptions()
	o.apply()
	assert.Equal(t, 5000, o.timeoutMs)
	assert.Equal(t, 30*time.Second, o.getTimeout)
}
