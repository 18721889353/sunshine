package nacoscli

import (
	"context"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/utils"
)

var (
	ipAddr      = "192.168.3.37"
	port        = 8848
	namespaceID = "3454d2b5-2455-4d0e-bf6d-e033b086bb4c"
)

// TestNewClient 验证命名客户端的创建。
func TestNewClient(t *testing.T) {
	utils.SafeRunWithTimeout(time.Second*2, func(cancel context.CancelFunc) {
		cli, err := NewClient(ipAddr, port, namespaceID)
		t.Log(err, cli)
	})
}

func TestNewConfigClient(t *testing.T) {
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
		_ = client.Close()
	})
}

// TestClient_GetConfig 验证 Client.GetConfig 方法。
func TestClient_GetConfig(t *testing.T) {
	client, err := newConfigClient(
		WithIPAddr(ipAddr),
		WithPort(port),
		WithNamespaceID(namespaceID),
	)
	if err != nil {
		t.Skipf("Nacos 服务不可用: %v", err)
		return
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

// TestParse 验证向后兼容的 GetConfig 函数（方式一：通过 Params 字段）。
func TestParse(t *testing.T) {
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

// TestParseWithOptions 验证向后兼容的 GetConfig 函数（方式二：通过 Option）。
func TestParseWithOptions(t *testing.T) {
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

func TestError(t *testing.T) {
	// valid() 参数校验
	p := &Params{}
	p.Group = ""
	err := p.valid()
	assert.Error(t, err)

	p.Group = "group"
	p.DataID = ""
	err = p.valid()
	assert.Error(t, err)

	p.Group = "group"
	p.DataID = "id"
	p.Format = ""
	err = p.valid()
	assert.Error(t, err)

	p.Group = "group"
	p.DataID = "id"
	p.Format = "yml"
	err = p.valid()
	assert.NoError(t, err)
	assert.Equal(t, "yaml", p.Format)

	p.Group = "group"
	p.DataID = "id"
	p.Format = "unknown"
	err = p.valid()
	assert.Error(t, err)

	// GetConfig 必填项缺失
	_, _, err = GetConfig(&Params{})
	assert.Error(t, err)

	// NewClient 缺少服务器地址
	_, err = NewClient(ipAddr, port, namespaceID)
	_ = err
}
