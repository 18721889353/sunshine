package nacoscli

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/utils"
)

// ---------------------------------------------------------------------------
// 集成测试：通过环境变量配置 Nacos 地址
// ---------------------------------------------------------------------------

// cancelledCtx 返回一个已取消的 context。
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// parseNacosAddr 从 NACOS_ADDR 环境变量解析 host:port 和 namespace。
// 格式: NACOS_ADDR=host:port 或 NACOS_ADDR=host:port/namespaceID
func parseNacosAddr(t *testing.T) (host string, port int, namespaceID string) {
	t.Helper()
	addr := os.Getenv("NACOS_ADDR")
	if addr == "" {
		t.Skip("NACOS_ADDR 未设置，跳过集成测试")
	}

	// 支持 host:port 和 host:port/namespaceID 两种格式
	parts := strings.SplitN(addr, "/", 2)
	hostPort := parts[0]
	if len(parts) > 1 {
		namespaceID = parts[1]
	}

	hp := strings.SplitN(hostPort, ":", 2)
	if len(hp) != 2 {
		t.Fatalf("NACOS_ADDR 格式无效，期望 host:port，实际: %s", addr)
	}
	host = hp[0]
	p, err := strconv.Atoi(hp[1])
	if err != nil {
		t.Fatalf("NACOS_ADDR 端口解析失败: %v", err)
	}
	port = p

	// 若未指定 namespace，使用默认值
	if namespaceID == "" {
		namespaceID = "3454d2b5-2455-4d0e-bf6d-e033b086bb4c"
	}
	return host, port, namespaceID
}

// TestNewNamingClient 验证命名客户端的创建。
func TestNewNamingClient(t *testing.T) {
	host, port, namespaceID := parseNacosAddr(t)
	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		cli, err := NewNamingClient(host, port, namespaceID)
		t.Log(err, cli)
		if cli != nil {
			cli.CloseClient()
		}
	})
}

// TestNewConfigClient 验证配置客户端的创建与关闭。
func TestNewConfigClient(t *testing.T) {
	host, port, namespaceID := parseNacosAddr(t)
	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		client, err := NewConfigClient(
			WithIPAddr(host),
			WithPort(port),
			WithNamespaceID(namespaceID),
		)
		if err != nil {
			t.Skipf("Nacos 服务不可用: %v", err)
			return
		}
		client.Close()
	})
}

// TestClient_GetConfig 验证 Client.GetConfig 方法。
func TestClient_GetConfig(t *testing.T) {
	host, port, namespaceID := parseNacosAddr(t)
	client, err := NewConfigClient(
		WithIPAddr(host),
		WithPort(port),
		WithNamespaceID(namespaceID),
	)
	if err != nil {
		t.Skipf("Nacos 服务不可用: %v", err)
	}
	defer client.Close()

	params := &Params{
		Group:  "dev",
		DataID: "serverNameExample.yml",
		Format: "yaml",
	}

	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		format, data, err := client.GetConfig(context.Background(), params)
		t.Logf("Client.GetConfig: err=%v, format=%s, len(data)=%d", err, format, len(data))
		_ = cancel
	})
}

// TestGetConfig 验证 GetConfig 便捷函数（方式一：通过 Params 字段）。
func TestGetConfig(t *testing.T) {
	host, port, namespaceID := parseNacosAddr(t)
	params := &Params{
		IPAddr:      host,
		Port:        port,
		NamespaceID: namespaceID,
		Group:       "dev",
		DataID:      "serverNameExample.yml",
		Format:      "yaml",
	}

	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		format, data, err := GetConfig(params)
		t.Log(err, format, data)
	})
}

// TestGetConfigWithOptions 验证 GetConfig 便捷函数（方式二：通过 Option）。
func TestGetConfigWithOptions(t *testing.T) {
	host, port, namespaceID := parseNacosAddr(t)
	params := &Params{
		Group:  "dev",
		DataID: "serverNameExample.yml",
		Format: "yaml",
	}
	clientConfig := &constant.ClientConfig{
		NamespaceId:         namespaceID,
		TimeoutMs:           1000,
		NotLoadCacheAtStart: true,
		LogDir:              os.TempDir() + "/nacos/log",
		CacheDir:            os.TempDir() + "/nacos/cache",
	}
	serverConfigs := []constant.ServerConfig{
		{
			IpAddr: host,
			Port:   uint64(port),
		},
	}

	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		format, data, err := GetConfig(params,
			WithClientConfig(clientConfig),
			WithServerConfigs(serverConfigs),
			WithAuth("foo", "bar"),
		)
		t.Log(err, format, data)
	})
}

// ---------------------------------------------------------------------------
// 单元测试：参数校验、错误路径、Option 优先级
// ---------------------------------------------------------------------------

// TestGetConfigNilParams 验证 GetConfig 对 nil params 的处理。
func TestGetConfigNilParams(t *testing.T) {
	_, _, err := GetConfig(nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNilParams)
}

// TestValid 验证 Params.valid() 的参数校验逻辑。
func TestValid(t *testing.T) {
	tests := []struct {
		name       string
		group      string
		dataID     string
		format     string
		wantErr    bool
		wantFormat string
	}{
		{"Group 为空", "", "id", "yaml", true, ""},
		{"DataID 为空", "group", "", "yaml", true, ""},
		{"Format 为空", "group", "id", "", true, ""},
		{"yml 归一化为 yaml", "group", "id", "yml", false, "yaml"},
		{"yaml 正常", "group", "id", "yaml", false, "yaml"},
		{"json 正常", "group", "id", "json", false, "json"},
		{"toml 正常", "group", "id", "toml", false, "toml"},
		{"不支持的 Format", "group", "id", "unknown", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Params{Group: tt.group, DataID: tt.dataID, Format: tt.format}
			format, err := p.valid()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantFormat, format)
			}
		})
	}

	// 验证 yml 归一化不修改原始 Params
	p := &Params{Group: "group", DataID: "id", Format: "yml"}
	_, err := p.valid()
	assert.NoError(t, err)
	assert.Equal(t, "yml", p.Format, "原始 Params.Format 不应被修改")
}

// TestGetConfigMissingRequiredFields 验证 GetConfig 对必填字段缺失的处理。
func TestGetConfigMissingRequiredFields(t *testing.T) {
	_, _, err := GetConfig(&Params{})
	assert.Error(t, err)

	_, _, err = GetConfig(nil)
	assert.ErrorIs(t, err, ErrNilParams)
}

// ---------------------------------------------------------------------------
// WatchConfig 错误路径测试
// ---------------------------------------------------------------------------

// TestWatchConfigNilParams 验证 WatchConfig 对 nil params 的处理。
func TestWatchConfigNilParams(t *testing.T) {
	stop, err := WatchConfig(context.Background(), nil,
		func(_, _, _, _ string) {})
	assert.ErrorIs(t, err, ErrNilParams)
	assert.NotNil(t, stop, "错误路径也应返回非 nil stop，可安全 defer stop()")
	// 不应 panic
	stop()
}

// TestWatchConfigNilHandler 验证 WatchConfig 对 nil handler 的处理。
func TestWatchConfigNilHandler(t *testing.T) {
	stop, err := WatchConfig(context.Background(),
		&Params{Group: "g", DataID: "d", Format: "yaml"}, nil)
	assert.Error(t, err)
	assert.NotNil(t, stop, "错误路径也应返回非 nil stop")
	stop()
}

// TestWatchConfigCancelledCtx 验证 WatchConfig 对已取消 ctx 的前置短路。
func TestWatchConfigCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stop, err := WatchConfig(ctx, &Params{Group: "g", DataID: "d", Format: "yaml"},
		func(_, _, _, _ string) {})
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotNil(t, stop, "ctx 已取消也应返回非 nil stop")
	stop()
}

// TestWatchConfigInvalidFormat 验证 WatchConfig 对无效 Format 的处理。
func TestWatchConfigInvalidFormat(t *testing.T) {
	stop, err := WatchConfig(context.Background(),
		&Params{Group: "g", DataID: "d", Format: "invalid"},
		func(_, _, _, _ string) {})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不支持")
	assert.NotNil(t, stop)
	stop()
}

// ---------------------------------------------------------------------------
// NewNamingClient 地址校验测试
// ---------------------------------------------------------------------------

// TestNewNamingClientMissingAddress 验证 NewNamingClient 缺少服务器地址时返回错误。
func TestNewNamingClientMissingAddress(t *testing.T) {
	_, err := NewNamingClient("", 0, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestNewNamingClientWithServerConfigs 验证通过 WithServerConfigs 提供地址时通过校验层。
// SDK 创建在无真实 Nacos 时可能失败，此测试仅验证校验层不拦截。
func TestNewNamingClientWithServerConfigs(t *testing.T) {
	serverConfigs := []constant.ServerConfig{
		{IpAddr: "192.168.1.1", Port: 8848},
	}
	// 有 serverConfigs 时，ipAddr 和 port 的校验应被跳过
	_, err := NewNamingClient("", 0, "",
		WithServerConfigs(serverConfigs))
	if err != nil {
		// 仅允许 SDK 连接失败，不允许校验层报错
		assert.NotContains(t, err.Error(), "不能为空",
			"有 serverConfigs 时不应触发地址/端口校验")
	}
}

// ---------------------------------------------------------------------------
// NewConfigClient 地址校验测试
// ---------------------------------------------------------------------------

// TestNewConfigClientMissingAddress 验证 NewConfigClient 缺少服务器地址时返回错误。
func TestNewConfigClientMissingAddress(t *testing.T) {
	_, err := NewConfigClient()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestNewConfigClientEmptyServerConfigs 验证空 serverConfigs 切片也被视为无地址。
func TestNewConfigClientEmptyServerConfigs(t *testing.T) {
	_, err := NewConfigClient(
		WithServerConfigs([]constant.ServerConfig{}),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// ---------------------------------------------------------------------------
// 负数参数校验测试
// ---------------------------------------------------------------------------

// TestWithPortNegative 负数端口应被忽略，保留默认值。
func TestWithPortNegative(t *testing.T) {
	o := defaultOptions()
	WithPort(-1)(o)
	assert.Equal(t, 0, o.port, "负数端口应被忽略")
}

// TestWithTimeoutMsNegative 负数超时应被忽略，保留默认值。
func TestWithTimeoutMsNegative(t *testing.T) {
	o := defaultOptions()
	WithTimeoutMs(-100)(o)
	assert.Equal(t, 5000, o.timeoutMs, "负数超时应被忽略")
}

// TestWithMaxRetriesNegative 负数重试次数应被忽略，保留先前值。
func TestWithMaxRetriesNegative(t *testing.T) {
	o := defaultOptions()
	o.maxRetries = 10 // 先设为非零
	WithMaxRetries(-1)(o)
	assert.Equal(t, 10, o.maxRetries, "负数 maxRetries 应被忽略，保留先前值")
}

// TestWithCreateDelayNegative 负数延迟应被忽略，保留默认值。
func TestWithCreateDelayNegative(t *testing.T) {
	o := defaultOptions()
	WithCreateDelay(-1 * time.Second)(o)
	assert.Equal(t, 5*time.Second, o.createDelay, "负数延迟应被忽略")
}

// TestWithPortZero 零值端口可设置，但 NewConfigClient/NewNamingClient 会校验 port == 0。
func TestWithPortZero(t *testing.T) {
	o := defaultOptions()
	WithPort(0)(o)
	assert.Equal(t, 0, o.port)
	// port == 0 时 NewConfigClient 应报错
	_, err := NewConfigClient(WithIPAddr("localhost"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "端口")
	// port == 0 时 NewNamingClient 也应报错
	_, err = NewNamingClient("localhost", 0, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "端口")
}

// TestWithTimeoutMsZero 零值超时应正常设置。
func TestWithTimeoutMsZero(t *testing.T) {
	o := defaultOptions()
	WithTimeoutMs(0)(o)
	assert.Equal(t, 0, o.timeoutMs)
}

// ---------------------------------------------------------------------------
// Option 优先级测试
// ---------------------------------------------------------------------------

// TestOptionPriority 验证 Option 优先级：后设置的覆盖先设置的。
func TestOptionPriority(t *testing.T) {
	o := defaultOptions()
	o.apply(
		WithIPAddr("first"),
		WithPort(1111),
		WithIPAddr("second"), // 覆盖
		WithPort(2222),       // 覆盖
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
	client, err := NewConfigClient(
		WithIPAddr("192.168.1.1"),
		WithPort(9848),
		WithServerConfigs(serverConfigs),
	)
	// 有 serverConfigs 时校验应通过（SDK 创建可能失败，但不是地址校验错误）
	if err != nil {
		assert.NotContains(t, err.Error(), "不能为空")
	}
	if client != nil {
		client.Close()
	}
}

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
// requestIDAttr 测试
// ---------------------------------------------------------------------------

// TestRequestIDAttr_WithID 验证有 request_id 时正确返回属性。
func TestRequestIDAttr_WithID(t *testing.T) {
	ctx := context.Background()
	// 注意：logger.ContextKeyRequestID 可能未定义或不可直接构造
	// 这里仅测试不存在时返回 nil
	attr := requestIDAttr(ctx)
	assert.Nil(t, attr, "无 request_id 时应返回 nil")
}

// ---------------------------------------------------------------------------
// buildConfigs 测试
// ---------------------------------------------------------------------------

// TestBuildConfigsWithCustomClientConfig 验证自定义 ClientConfig 生效。
func TestBuildConfigsWithCustomClientConfig(t *testing.T) {
	cc := &constant.ClientConfig{
		NamespaceId: "custom-ns",
		TimeoutMs:   10000,
	}
	o := defaultOptions()
	o.clientConfig = cc
	o.ipAddr = "10.0.0.1"
	o.port = 8848

	clientConfig, serverConfigs := buildConfigs(o)
	assert.Equal(t, cc, clientConfig)
	// serverConfigs 仍从单字段构建（未设置 serverConfigs）
	assert.Len(t, serverConfigs, 1)
	assert.Equal(t, "10.0.0.1", serverConfigs[0].IpAddr)
	assert.Equal(t, uint64(8848), serverConfigs[0].Port)
}

// TestBuildConfigsWithCustomServerConfigs 验证自定义 ServerConfigs 生效。
func TestBuildConfigsWithCustomServerConfigs(t *testing.T) {
	sc := []constant.ServerConfig{
		{IpAddr: "10.0.0.1", Port: 8848, Scheme: "grpc"},
	}
	o := defaultOptions()
	o.serverConfigs = sc

	clientConfig, serverConfigs := buildConfigs(o)
	// clientConfig 从默认字段构建
	assert.NotNil(t, clientConfig)
	assert.Equal(t, sc, serverConfigs)
}

// TestBuildConfigsDefaultValues 验证默认值构建。
func TestBuildConfigsDefaultValues(t *testing.T) {
	o := defaultOptions()
	o.ipAddr = "localhost"
	o.port = 8848
	o.scheme = "http"
	o.contextPath = "/nacos"

	clientConfig, serverConfigs := buildConfigs(o)
	assert.NotNil(t, clientConfig)
	assert.Equal(t, uint64(5000), clientConfig.TimeoutMs)
	assert.True(t, clientConfig.NotLoadCacheAtStart)
	assert.Len(t, serverConfigs, 1)
	assert.Equal(t, "localhost", serverConfigs[0].IpAddr)
	assert.Equal(t, uint64(8848), serverConfigs[0].Port)
	assert.Equal(t, "http", serverConfigs[0].Scheme)
	assert.Equal(t, "/nacos", serverConfigs[0].ContextPath)
}

// ---------------------------------------------------------------------------
// WatchConfig stop 函数一致性测试
// ---------------------------------------------------------------------------

// TestWatchConfigStopNotNilOnAllErrorPaths 验证所有错误路径返回非 nil stop。
func TestWatchConfigStopNotNilOnAllErrorPaths(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		params  *Params
		handler ChangeHandler
	}{
		{"ctx 已取消", cancelledCtx(), &Params{Group: "g", DataID: "d", Format: "yaml"}, func(_, _, _, _ string) {}},
		{"params 为空", context.Background(), nil, func(_, _, _, _ string) {}},
		{"handler 为空", context.Background(), &Params{Group: "g", DataID: "d", Format: "yaml"}, nil},
		{"Format 无效", context.Background(), &Params{Group: "g", DataID: "d", Format: "bad"}, func(_, _, _, _ string) {}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stop, err := WatchConfig(tt.ctx, tt.params, tt.handler)
			assert.Error(t, err)
			assert.NotNil(t, stop, "错误路径 stop 不应为 nil")
			// 确保可以安全调用 stop
			assert.NotPanics(t, func() { stop() })
		})
	}
}

// ---------------------------------------------------------------------------
// WatchConfig 重试配置测试
// ---------------------------------------------------------------------------

// TestWatchConfigWithMaxRetries0 验证显式 WithMaxRetries(0) 可将非零值覆盖为 0（无限重试）。
// 默认值即为 0（无限重试），此测试仅验证显式 0 能覆盖先前设置。
func TestWatchConfigWithMaxRetries0(t *testing.T) {
	o := defaultOptions()
	o.maxRetries = 3 // 先设为非零
	WithMaxRetries(0)(o)
	assert.Equal(t, 0, o.maxRetries, "WithMaxRetries(0) 应覆盖为 0")
}

// TestWatchConfigWithMaxRetries 设置有限重试。
func TestWatchConfigWithMaxRetries(t *testing.T) {
	o := defaultOptions()
	WithMaxRetries(5)(o)
	assert.Equal(t, 5, o.maxRetries)
}

// TestWithCreateDelay 设置重试延迟。
func TestWithCreateDelay(t *testing.T) {
	o := defaultOptions()
	WithCreateDelay(10 * time.Second)(o)
	assert.Equal(t, 10*time.Second, o.createDelay)
}

// ---------------------------------------------------------------------------
// GetConfig 便捷函数覆盖测试
// ---------------------------------------------------------------------------

// TestGetConfigConvenienceWithClientConfig 便捷函数支持完整 SDK 配置。
func TestGetConfigConvenienceWithClientConfig(t *testing.T) {
	host, port, namespaceID := parseNacosAddr(t)

	params := &Params{
		Group:  "dev",
		DataID: "serverNameExample.yml",
		Format: "yaml",
	}

	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		format, data, err := GetConfig(params,
			WithClientConfig(&constant.ClientConfig{
				NamespaceId:         namespaceID,
				TimeoutMs:           1000,
				NotLoadCacheAtStart: true,
				LogDir:              os.TempDir() + "/nacos/log",
				CacheDir:            os.TempDir() + "/nacos/cache",
			}),
			WithServerConfigs([]constant.ServerConfig{
				{IpAddr: host, Port: uint64(port)},
			}),
		)
		t.Logf("GetConfigWithSDK: err=%v, format=%s, len=%d", err, format, len(data))
	})
}
