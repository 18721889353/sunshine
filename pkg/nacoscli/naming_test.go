package nacoscli

import (
	"testing"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NewNamingClient 工厂测试（不依赖真实 Nacos 服务器）
// ---------------------------------------------------------------------------

// TestNewNamingClientMissingAddress 验证 NewNamingClient 地址与端口均为空时返回错误。
func TestNewNamingClientMissingAddress(t *testing.T) {
	_, err := NewNamingClient("", 0, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestNewNamingClientMissingAddressOnly 验证有端口但无地址时返回地址校验错误。
func TestNewNamingClientMissingAddressOnly(t *testing.T) {
	_, err := NewNamingClient("", 8848, "ns")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "地址")
}

// TestNewNamingClientMissingPort 验证有地址但无端口时返回端口校验错误。
func TestNewNamingClientMissingPort(t *testing.T) {
	_, err := NewNamingClient("127.0.0.1", 0, "ns")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "端口")
}

// TestNewNamingClientWithServerConfigs 验证 WithServerConfigs 优先于位置参数，不触发地址/端口校验。
func TestNewNamingClientWithServerConfigs(t *testing.T) {
	serverConfigs := []constant.ServerConfig{
		{IpAddr: "127.0.0.1", Port: 8848},
	}
	client, err := NewNamingClient("", 0, "", WithServerConfigs(serverConfigs), WithTimeoutMs(500))
	if err != nil {
		// 校验层不应拦截：失败只能来自 SDK 客户端创建（如无网络环境）
		assert.NotContains(t, err.Error(), "不能为空", "有 serverConfigs 时不应触发地址/端口校验")
		return
	}
	require.NotNil(t, client)
	client.CloseClient()
}

// TestNewNamingClientReturnsSDKClient 验证正常参数下返回 SDK 命名客户端接口实例。
// 工厂定位为「返回 naming_client.INamingClient」，可直接注入 servicerd/registry/nacos.New。
func TestNewNamingClientReturnsSDKClient(t *testing.T) {
	client, err := NewNamingClient("127.0.0.1", 8848, "ns")
	if err != nil {
		t.Skipf("当前环境无法创建 SDK 命名客户端，跳过: %v", err)
	}
	require.NotNil(t, client, "应返回非 nil 的 naming_client.INamingClient")
	client.CloseClient()
}

// TestNewNamingClientNilOption 验证 nil Option 被跳过而不 panic（动态拼接选项场景）。
func TestNewNamingClientNilOption(t *testing.T) {
	assert.NotPanics(t, func() {
		client, err := NewNamingClient("127.0.0.1", 8848, "ns", nil, WithTimeoutMs(3000))
		if err != nil {
			t.Skipf("当前环境无法创建 SDK 命名客户端，跳过: %v", err)
		}
		require.NotNil(t, client)
		client.CloseClient()
	})
}
