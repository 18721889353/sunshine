package nacoscli

import (
	"context"
	"fmt"
	"testing"

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
	getConfigFn              func(param vo.ConfigParam) (string, error)
	listenConfigFn           func(params vo.ConfigParam) error
	listenConfigCalled       bool
	lastListenParam          vo.ConfigParam
	cancelListenConfigFn     func(params vo.ConfigParam) error
	cancelListenConfigCalled bool
	lastCancelParam          vo.ConfigParam
	closeCalled              bool
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
	m.listenConfigCalled = true
	m.lastListenParam = params
	if m.listenConfigFn != nil {
		return m.listenConfigFn(params)
	}
	return nil
}

func (m *mockConfigClient) CancelListenConfig(params vo.ConfigParam) error {
	m.cancelListenConfigCalled = true
	m.lastCancelParam = params
	if m.cancelListenConfigFn != nil {
		return m.cancelListenConfigFn(params)
	}
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


