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

// TestNewClient 验证命名客户端的创建。
func TestNewClient(t *testing.T) {
	skipIfNoNacos(t)
	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		cli, err := NewClient(ipAddr, port, namespaceID)
		t.Log(err, cli)
	})
}

// TestNewConfigClient 验证配置客户端的创建与关闭。
func TestNewConfigClient(t *testing.T) {
	skipIfNoNacos(t)
	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		client, err := newConfigClient(
			WithIPAddr(ipAddr),
			WithPort(port),
			WithNamespaceID(namespaceID),
		)
		if err != nil {
			t.Skipf("Nacos 服务不可用: %v", err)
			return
		}
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("Close() 产生意外错误: %v", closeErr)
		}
	})
}

// TestClient_GetConfig 验证 Client.getConfig 方法。
func TestClient_GetConfig(t *testing.T) {
	skipIfNoNacos(t)
	client, err := newConfigClient(
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
		format, data, err := client.getConfig(context.Background(), params)
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
}

// TestValid 验证 Params.valid() 的参数校验逻辑。
func TestValid(t *testing.T) {
	// Group 为空
	p := &Params{}
	p.Group = ""
	err := p.valid()
	assert.Error(t, err)

	// DataID 为空
	p.Group = "group"
	p.DataID = ""
	err = p.valid()
	assert.Error(t, err)

	// Format 为空
	p.Group = "group"
	p.DataID = "id"
	p.Format = ""
	err = p.valid()
	assert.Error(t, err)

	// yml 归一化为 yaml
	p.Group = "group"
	p.DataID = "id"
	p.Format = "yml"
	err = p.valid()
	assert.NoError(t, err)
	assert.Equal(t, "yaml", p.Format)

	// 不支持的 Format
	p.Group = "group"
	p.DataID = "id"
	p.Format = "unknown"
	err = p.valid()
	assert.Error(t, err)

	// GetConfig 必填项缺失
	_, _, err = GetConfig(&Params{})
	assert.Error(t, err)

	// GetConfig nil params
	_, _, err = GetConfig(nil)
	assert.Error(t, err)
}

// TestNewClientMissingAddress 验证 NewClient 缺少服务器地址时返回错误。
func TestNewClientMissingAddress(t *testing.T) {
	_, err := NewClient("", 0, "")
	assert.Error(t, err)
}
