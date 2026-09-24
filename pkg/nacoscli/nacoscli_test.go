package nacoscli

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/18721889353/sunshine/pkg/logger"
)

// cancelledCtx 返回一个已取消的 context。
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// ---------------------------------------------------------------------------
// mockConfigClient 模拟 config_client.IConfigClient，用于 Client 方法的单元测试。
// ---------------------------------------------------------------------------

type mockConfigClient struct {
	getConfigFn    func(param vo.ConfigParam) (string, error)
	listenConfigFn func(params vo.ConfigParam) error
	closeCalled    bool
}

func (m *mockConfigClient) GetConfig(param vo.ConfigParam) (string, error) {
	if m.getConfigFn != nil {
		return m.getConfigFn(param)
	}
	return "", fmt.Errorf("mock: not implemented")
}

func (m *mockConfigClient) PublishConfig(_ vo.ConfigParam) (bool, error) {
	return false, fmt.Errorf("mock: not implemented")
}

func (m *mockConfigClient) DeleteConfig(_ vo.ConfigParam) (bool, error) {
	return false, fmt.Errorf("mock: not implemented")
}

func (m *mockConfigClient) ListenConfig(params vo.ConfigParam) error {
	if m.listenConfigFn != nil {
		return m.listenConfigFn(params)
	}
	return nil
}

func (m *mockConfigClient) CancelListenConfig(_ vo.ConfigParam) error {
	return nil
}

func (m *mockConfigClient) SearchConfig(_ vo.SearchConfigParam) (*model.ConfigPage, error) {
	return nil, fmt.Errorf("mock: not implemented")
}

func (m *mockConfigClient) CloseClient() {
	m.closeCalled = true
}

// ---------------------------------------------------------------------------
// GetConfig 参数校验测试
// ---------------------------------------------------------------------------

// TestGetConfigNilParams 验证 GetConfig 便捷函数对 nil params 返回 ErrNilParams。
func TestGetConfigNilParams(t *testing.T) {
	_, _, err := GetConfig(nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNilParams)
}

// TestGetConfigInvalidGroup 验证 GetConfig 便捷函数对空 Group 返回错误。
func TestGetConfigInvalidGroup(t *testing.T) {
	_, _, err := GetConfig(&Params{DataID: "d", Format: "yaml"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Group")
}

// TestGetConfigInvalidDataID 验证 GetConfig 便捷函数对空 DataID 返回错误。
func TestGetConfigInvalidDataID(t *testing.T) {
	_, _, err := GetConfig(&Params{Group: "g", Format: "yaml"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DataID")
}

// TestGetConfigInvalidFormat 验证 GetConfig 便捷函数对不支持的 Format 返回错误。
func TestGetConfigInvalidFormat(t *testing.T) {
	_, _, err := GetConfig(&Params{Group: "g", DataID: "d", Format: "xml"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不支持")
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
// Client.GetConfig 单元测试（通过 mock 验证逻辑）
// ---------------------------------------------------------------------------

// TestClientGetConfigNilParams 验证 Client.GetConfig 对 nil params 返回 ErrNilParams。
func TestClientGetConfigNilParams(t *testing.T) {
	mock := &mockConfigClient{}
	client := &Client{configClient: mock}
	_, _, err := client.GetConfig(context.Background(), nil)
	assert.ErrorIs(t, err, ErrNilParams)
}

// TestClientGetConfigCancelledCtx 验证 Client.GetConfig 对已取消 ctx 提前返回。
func TestClientGetConfigCancelledCtx(t *testing.T) {
	mock := &mockConfigClient{}
	client := &Client{configClient: mock}
	_, _, err := client.GetConfig(cancelledCtx(), &Params{
		Group: "g", DataID: "d", Format: "yaml",
	})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestClientGetConfigInvalidParams 验证 Client.GetConfig 对无效参数返回校验错误。
func TestClientGetConfigInvalidParams(t *testing.T) {
	mock := &mockConfigClient{}
	client := &Client{configClient: mock}
	_, _, err := client.GetConfig(context.Background(), &Params{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestClientGetConfigSuccess 验证 Client.GetConfig 成功路径。
func TestClientGetConfigSuccess(t *testing.T) {
	expected := "key: value"
	mock := &mockConfigClient{
		getConfigFn: func(param vo.ConfigParam) (string, error) {
			assert.Equal(t, "d", param.DataId)
			assert.Equal(t, "g", param.Group)
			return expected, nil
		},
	}
	client := &Client{configClient: mock}
	format, data, err := client.GetConfig(context.Background(), &Params{
		Group: "g", DataID: "d", Format: "yaml",
	})
	assert.NoError(t, err)
	assert.Equal(t, "yaml", format)
	assert.Equal(t, expected, string(data))
}

// TestClientGetConfigSDKError 验证 Client.GetConfig 在 SDK 报错时包装错误信息。
func TestClientGetConfigSDKError(t *testing.T) {
	mock := &mockConfigClient{
		getConfigFn: func(_ vo.ConfigParam) (string, error) {
			return "", fmt.Errorf("connection refused")
		},
	}
	client := &Client{configClient: mock}
	_, _, err := client.GetConfig(context.Background(), &Params{
		Group: "g", DataID: "d", Format: "yaml",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "从 Nacos 获取配置失败")
}

// TestClientGetConfigFormatNormalized 验证 Client.GetConfig 对 yml 格式归一化为 yaml。
func TestClientGetConfigFormatNormalized(t *testing.T) {
	mock := &mockConfigClient{
		getConfigFn: func(_ vo.ConfigParam) (string, error) {
			return "ok", nil
		},
	}
	client := &Client{configClient: mock}
	format, _, err := client.GetConfig(context.Background(), &Params{
		Group: "g", DataID: "d", Format: "yml",
	})
	assert.NoError(t, err)
	assert.Equal(t, "yaml", format, "yml 应归一化为 yaml")
}

// ---------------------------------------------------------------------------
// Client.Close 测试
// ---------------------------------------------------------------------------

// TestClientCloseNormal 验证 Client.Close 正常调用 CloseClient。
func TestClientCloseNormal(t *testing.T) {
	mock := &mockConfigClient{}
	client := &Client{configClient: mock}
	client.Close()
	assert.True(t, mock.closeCalled, "应调用 CloseClient")
}

// TestClientCloseNilConfigClient 验证 Client.Close 对 nil configClient 不 panic。
func TestClientCloseNilConfigClient(t *testing.T) {
	client := &Client{configClient: nil}
	assert.NotPanics(t, func() { client.Close() })
}

// ---------------------------------------------------------------------------
// ListenClient 参数校验测试
// ---------------------------------------------------------------------------

// TestNewListenClientNilHandler 验证 NewListenClient 对 nil handler 返回错误。
func TestNewListenClientNilHandler(t *testing.T) {
	_, err := NewListenClient(&Params{Group: "g", DataID: "d", Format: "yaml"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestNewListenClientNilParams 验证 NewListenClient 对 nil params 返回错误。
func TestNewListenClientNilParams(t *testing.T) {
	_, err := NewListenClient(nil, func(_, _, _, _ string) {})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestNewListenClientInvalidParams 验证 NewListenClient 对无效 params 返回校验错误。
func TestNewListenClientInvalidParams(t *testing.T) {
	_, err := NewListenClient(
		&Params{},
		func(_, _, _, _ string) {},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "校验失败")
}

// TestNewListenClientMissingAddress 验证 NewListenClient 缺少地址时返回错误。
func TestNewListenClientMissingAddress(t *testing.T) {
	_, err := NewListenClient(
		&Params{Group: "g", DataID: "d", Format: "yaml"},
		func(_, _, _, _ string) {},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "创建 Nacos 配置监听客户端失败")
}

// TestListenClientCloseNilConfigClient 验证 ListenClient.Close 对 nil configClient 不 panic。
func TestListenClientCloseNilConfigClient(t *testing.T) {
	listener := &ListenClient{configClient: nil}
	assert.NotPanics(t, func() { listener.Close() })
}

// TestListenClientCloseNormal 验证 ListenClient.Close 正常调用 CloseClient。
func TestListenClientCloseNormal(t *testing.T) {
	mock := &mockConfigClient{}
	listener := &ListenClient{configClient: mock}
	listener.Close()
	assert.True(t, mock.closeCalled, "应调用 CloseClient")
}

// ---------------------------------------------------------------------------
// exceededMaxRetries 测试
// ---------------------------------------------------------------------------

func TestExceededMaxRetries(t *testing.T) {
	tests := []struct {
		name    string
		max     int
		current int
		want    bool
	}{
		{"0 无限重试不超限", 0, 100, false},
		{"负数无限重试不超限", -1, 50, false},
		{"未达上限", 5, 3, false},
		{"恰好达上限", 5, 5, true},
		{"超过上限", 5, 10, true},
		{"首次即超限", 1, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exceededMaxRetries(tt.max, tt.current))
		})
	}
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

// TestWatchConfigWithYMLFormat 验证 WatchConfig 接受 yml 格式（归一化为 yaml）。
// 地址缺失错误在后台 goroutine 异步发生，WatchConfig 本身不报错。
func TestWatchConfigWithYMLFormat(t *testing.T) {
	stop, err := WatchConfig(context.Background(),
		&Params{Group: "g", DataID: "d", Format: "yml"},
		func(_, _, _, _ string) {},
	)
	assert.NoError(t, err, "yml 格式应通过校验")
	assert.NotNil(t, stop)
	stop()
}

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
			assert.NotPanics(t, func() { stop() })
		})
	}
}

// ---------------------------------------------------------------------------
// WatchConfig 重试配置测试
// ---------------------------------------------------------------------------

// TestWatchConfigWithMaxRetries0 验证显式 WithMaxRetries(0) 可将非零值覆盖为 0（无限重试）。
func TestWatchConfigWithMaxRetries0(t *testing.T) {
	o := defaultOptions()
	o.maxRetries = 3
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
// NewNamingClient 地址校验测试
// ---------------------------------------------------------------------------

// TestNewNamingClientMissingAddress 验证 NewNamingClient 缺少服务器地址时返回错误。
func TestNewNamingClientMissingAddress(t *testing.T) {
	_, err := NewNamingClient("", 0, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestNewNamingClientMissingPort 验证 NewNamingClient 有地址无端口时返回错误。
func TestNewNamingClientMissingPort(t *testing.T) {
	_, err := NewNamingClient("localhost", 0, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "端口")
}

// TestNewNamingClientMissingAddressOnly 验证 NewNamingClient 有端口无地址时返回错误。
func TestNewNamingClientMissingAddressOnly(t *testing.T) {
	_, err := NewNamingClient("", 8848, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "地址")
}

// TestNewNamingClientWithServerConfigs 验证通过 WithServerConfigs 提供地址时通过校验层。
func TestNewNamingClientWithServerConfigs(t *testing.T) {
	serverConfigs := []constant.ServerConfig{
		{IpAddr: "192.168.1.1", Port: 8848},
	}
	_, err := NewNamingClient("", 0, "",
		WithServerConfigs(serverConfigs))
	if err != nil {
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

// TestNewConfigClientWithValidAddress 验证 NewConfigClient 有效地址能通过校验层。
func TestNewConfigClientWithValidAddress(t *testing.T) {
	client, err := NewConfigClient(
		WithIPAddr("192.168.1.1"),
		WithPort(8848),
	)
	if err != nil {
		assert.NotContains(t, err.Error(), "不能为空",
			"有地址和端口时不应触发校验层错误")
	}
	if client != nil {
		client.Close()
	}
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

// TestWithPortZero 零值端口可设置，但 NewConfigClient/NewNamingClient 会校验 port == 0。
func TestWithPortZero(t *testing.T) {
	o := defaultOptions()
	WithPort(0)(o)
	assert.Equal(t, 0, o.port)
	_, err := NewConfigClient(WithIPAddr("localhost"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "端口")
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
	client, err := NewConfigClient(
		WithIPAddr("192.168.1.1"),
		WithPort(9848),
		WithServerConfigs(serverConfigs),
	)
	if err != nil {
		assert.NotContains(t, err.Error(), "不能为空")
	}
	if client != nil {
		client.Close()
	}
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

// ---------------------------------------------------------------------------
// 单个 Option 函数测试
// ---------------------------------------------------------------------------

// TestWithGetTimeout 设置 GetConfig 拉取超时。
func TestWithGetTimeout(t *testing.T) {
	o := defaultOptions()
	WithGetTimeout(10 * time.Second)(o)
	assert.Equal(t, 10*time.Second, o.getTimeout)
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

// TestWithServerConfigs 设置完整 ServerConfigs。
func TestWithServerConfigs(t *testing.T) {
	sc := []constant.ServerConfig{{IpAddr: "10.0.0.1", Port: 8848}}
	o := defaultOptions()
	WithServerConfigs(sc)(o)
	assert.Equal(t, sc, o.serverConfigs)
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
	ctx := context.WithValue(context.Background(), logger.ContextKeyRequestID, "req-123")
	attr := requestIDAttr(ctx)
	require.NotNil(t, attr, "有 request_id 时应返回非 nil")
	assert.Equal(t, "nacoscli.request_id", string(attr.Key))
	assert.Equal(t, "req-123", attr.Value.AsString())
}

// TestRequestIDAttr_EmptyID 验证空字符串 request_id 返回 nil。
func TestRequestIDAttr_EmptyID(t *testing.T) {
	ctx := context.WithValue(context.Background(), logger.ContextKeyRequestID, "")
	attr := requestIDAttr(ctx)
	assert.Nil(t, attr, "空 request_id 应返回 nil")
}

// TestRequestIDAttr_NoID 验证无 request_id 时返回 nil。
func TestRequestIDAttr_NoID(t *testing.T) {
	attr := requestIDAttr(context.Background())
	assert.Nil(t, attr, "无 request_id 时应返回 nil")
}

// TestRequestIDAttr_WrongType 验证非 string 类型的 request_id 返回 nil。
func TestRequestIDAttr_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), logger.ContextKeyRequestID, 12345)
	attr := requestIDAttr(ctx)
	assert.Nil(t, attr, "非 string 类型的 request_id 应返回 nil")
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

// TestBuildConfigsWithAuth 验证认证信息传递到 ClientConfig。
func TestBuildConfigsWithAuth(t *testing.T) {
	o := defaultOptions()
	o.username = "admin"
	o.password = "secret"

	clientConfig, _ := buildConfigs(o)
	assert.Equal(t, "admin", clientConfig.Username)
	assert.Equal(t, "secret", clientConfig.Password)
}

// TestBuildConfigsWithNamespaceID 验证 namespaceID 传递到 ClientConfig。
func TestBuildConfigsWithNamespaceID(t *testing.T) {
	o := defaultOptions()
	o.namespaceID = "dev-ns"

	clientConfig, _ := buildConfigs(o)
	assert.Equal(t, "dev-ns", clientConfig.NamespaceId)
}
