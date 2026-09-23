package nacoscli

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/utils"
)

var (
	ipAddr      = "192.168.3.37"
	port        = 8848
	namespaceID = "3454d2b5-2455-4d0e-bf6d-e033b086bb4c"
)

// skipIfNoNacos 集成测试前置条件：NACOS_ADDR 环境变量未设置时跳过。
func skipIfNoNacos(t *testing.T) {
	t.Helper()
	if os.Getenv("NACOS_ADDR") == "" {
		t.Skip("NACOS_ADDR 未设置，跳过集成测试")
	}
}

// TestNewNamingClient 验证命名客户端的创建。
func TestNewNamingClient(t *testing.T) {
	skipIfNoNacos(t)
	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		cli, err := NewNamingClient(ipAddr, port, namespaceID)
		t.Log(err, cli)
	})
}

// TestNewConfigClient 验证配置客户端的创建与关闭。
func TestNewConfigClient(t *testing.T) {
	skipIfNoNacos(t)
	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		client, err := NewConfigClient(
			WithIPAddr(ipAddr),
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
	skipIfNoNacos(t)
	client, err := NewConfigClient(
		WithIPAddr(ipAddr),
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
	skipIfNoNacos(t)
	params := &Params{
		IPAddr:      ipAddr,
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
	skipIfNoNacos(t)
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
			IpAddr: ipAddr,
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

// TestGetConfigNilParams 验证 GetConfig 对 nil params 的处理。
func TestGetConfigNilParams(t *testing.T) {
	_, _, err := GetConfig(nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNilParams)
}

// TestValid 验证 Params.valid() 的参数校验逻辑。
func TestValid(t *testing.T) {
	// Group 为空
	p := &Params{}
	p.Group = ""
	_, err := p.valid()
	assert.Error(t, err)

	// DataID 为空
	p.Group = "group"
	p.DataID = ""
	_, err = p.valid()
	assert.Error(t, err)

	// Format 为空
	p.Group = "group"
	p.DataID = "id"
	p.Format = ""
	_, err = p.valid()
	assert.Error(t, err)

	// yml 归一化为 yaml（不修改原始 Params）
	p.Group = "group"
	p.DataID = "id"
	p.Format = "yml"
	format, err := p.valid()
	assert.NoError(t, err)
	assert.Equal(t, "yaml", format)
	assert.Equal(t, "yml", p.Format) // 原始值不变

	// 不支持的 Format
	p.Group = "group"
	p.DataID = "id"
	p.Format = "unknown"
	_, err = p.valid()
	assert.Error(t, err)

	// GetConfig 必填项缺失
	_, _, err = GetConfig(&Params{})
	assert.Error(t, err)

	// GetConfig nil params
	_, _, err = GetConfig(nil)
	assert.Error(t, err)
}

// TestNewNamingClientMissingAddress 验证 NewNamingClient 缺少服务器地址时返回错误。
func TestNewNamingClientMissingAddress(t *testing.T) {
	_, err := NewNamingClient("", 0, "")
	assert.Error(t, err)
}

// TestNewConfigClientMissingAddress 验证 NewConfigClient 缺少服务器地址时返回错误。
func TestNewConfigClientMissingAddress(t *testing.T) {
	_, err := NewConfigClient()
	assert.Error(t, err)
}

// TestWatchConfigNilParams 验证 WatchConfig 对 nil params 的处理。
func TestWatchConfigNilParams(t *testing.T) {
	_, err := WatchConfig(context.Background(), nil,
		func(_, _, _, _ string) {})
	assert.ErrorIs(t, err, ErrNilParams)
}

// TestWatchConfigNilHandler 验证 WatchConfig 对 nil handler 的处理。
func TestWatchConfigNilHandler(t *testing.T) {
	_, err := WatchConfig(context.Background(),
		&Params{Group: "g", DataID: "d", Format: "yaml"}, nil)
	assert.Error(t, err)
}

// TestWatchConfigCancelledCtx 验证 WatchConfig 对已取消 ctx 的前置短路。
func TestWatchConfigCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := WatchConfig(ctx, &Params{Group: "g", DataID: "d", Format: "yaml"},
		func(_, _, _, _ string) {})
	assert.ErrorIs(t, err, context.Canceled)
}
