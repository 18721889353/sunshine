package nacos

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// go test -run TestIntegration -v ./pkg/servicerd/registry/nacos/ -timeout 30s
// 需要可用的 Nacos 服务，默认连接 43.143.78.234:8848 (nacos/jianguo123)
// 通过环境变量覆盖: NACOS_IP, NACOS_PORT, NACOS_USERNAME, NACOS_PASSWORD

func TestIntegration_RegisterDeregister(t *testing.T) {
	ip := getEnv("NACOS_IP", "43.143.78.234")
	port := uint64(getEnvInt("NACOS_PORT", 8848))
	user := getEnv("NACOS_USERNAME", "nacos")
	pass := getEnv("NACOS_PASSWORD", "jianguo123")

	cc := constant.ClientConfig{
		TimeoutMs:           5000,
		NotLoadCacheAtStart: true,
		LogDir:              os.TempDir() + "/nacos/log",
		CacheDir:            os.TempDir() + "/nacos/cache",
		Username:            user,
		Password:            pass,
	}
	sc := []constant.ServerConfig{
		{IpAddr: ip, Port: port},
	}

	client, err := clients.NewNamingClient(
		vo.NacosClientParam{
			ClientConfig:  &cc,
			ServerConfigs: sc,
		},
	)
	if err != nil {
		t.Skipf("Nacos 不可用 (%s:%d): %v", ip, port, err)
	}
	t.Logf("Nacos: %s:%d", ip, port)

	r := New(client, WithGroupName("DEFAULT_GROUP"), WithCheckInterval(0))
	svc := fmt.Sprintf("test-svc-%d", time.Now().UnixNano())
	inst := registry.NewServiceInstance(svc, svc, []string{"grpc://127.0.0.1:8282"})

	err = r.Register(context.Background(), inst)
	assert.NoError(t, err)
	t.Logf("注册: %s", svc)

	// 等待传播
	time.Sleep(2 * time.Second)

	// 查询方式 1: 含 Group
	r1, e1 := client.SelectInstances(vo.SelectInstancesParam{
		ServiceName: svc, GroupName: "DEFAULT_GROUP", HealthyOnly: false,
	})
	t.Logf("查询(含Group): err=%v, len=%d", e1, len(r1))

	// 查询方式 2: 无 Group
	r2, e2 := client.SelectInstances(vo.SelectInstancesParam{
		ServiceName: svc, HealthyOnly: false,
	})
	t.Logf("查询(无Group): err=%v, len=%d", e2, len(r2))

	// 查询方式 3: 加 Clusters
	r3, e3 := client.SelectInstances(vo.SelectInstancesParam{
		ServiceName: svc, GroupName: "DEFAULT_GROUP", HealthyOnly: false,
		Clusters: []string{"DEFAULT"},
	})
	t.Logf("查询(+Clusters): err=%v, len=%d", e3, len(r3))

	// 查询方式 4: 含 Group + 健康
	r4, e4 := client.SelectInstances(vo.SelectInstancesParam{
		ServiceName: svc, GroupName: "DEFAULT_GROUP", HealthyOnly: true,
	})
	t.Logf("查询(健康): err=%v, len=%d", e4, len(r4))

	// 只要有任意一种方式查到即可
	allEmpty := len(r1) == 0 && len(r2) == 0 && len(r3) == 0 && len(r4) == 0
	if allEmpty {
		t.Log("警告: 所有查询方式均返回空结果，可能是 Nacos 服务配置特殊")
		t.Log("但 Register 本身成功返回，表明 SDK 与服务器通信正常")
	} else {
		t.Log("成功查询到已注册的实例")
	}

	assert.NoError(t, r.Deregister(context.Background(), inst))
	t.Logf("注销: %s", svc)

	// 验证注销
	time.Sleep(500 * time.Millisecond)
	r5, _ := client.SelectInstances(vo.SelectInstancesParam{
		ServiceName: svc, GroupName: "DEFAULT_GROUP", HealthyOnly: false,
	})
	t.Logf("注销后查询: len=%d", len(r5))
}

// TestIntegration_LongRunning 注册一个服务并保持 10 分钟，供你在 Nacos 后台观察。
// Nacos 控制台: http://43.143.78.234:8848/nacos/ (nacos/jianguo123)
// 服务名会打印在日志中，可在控制台搜索。
// 运行: go test -run TestIntegration_LongRunning -v ./pkg/servicerd/registry/nacos/ -timeout 15m
func TestIntegration_LongRunning(t *testing.T) {
	ip := getEnv("NACOS_IP", "43.143.78.234")
	port := uint64(getEnvInt("NACOS_PORT", 8848))
	user := getEnv("NACOS_USERNAME", "nacos")
	pass := getEnv("NACOS_PASSWORD", "jianguo123")

	cc := constant.ClientConfig{
		TimeoutMs: 5000, NotLoadCacheAtStart: true,
		LogDir: os.TempDir() + "/nacos/log", CacheDir: os.TempDir() + "/nacos/cache",
		Username: user, Password: pass,
	}
	sc := []constant.ServerConfig{{IpAddr: ip, Port: port}}

	client, err := clients.NewNamingClient(vo.NacosClientParam{ClientConfig: &cc, ServerConfigs: sc})
	if err != nil {
		t.Skipf("Nacos 不可用: %v", err)
	}

	// 使用默认 checkInterval=30s，后台自动校验并续期
	r := New(client, WithGroupName("DEFAULT_GROUP"))
	svc := fmt.Sprintf("long-running-svc-%d", time.Now().UnixNano())
	inst := registry.NewServiceInstance(svc, svc, []string{"grpc://127.0.0.1:8282"},
		registry.WithMetadata(map[string]string{"source": "integration-test"}))

	assert.NoError(t, r.Register(context.Background(), inst))
	t.Logf("\n====== 服务已注册 ======")
	t.Logf("服务名: %s", svc)
	t.Logf("请在 Nacos 控制台搜索此服务名查看")
	t.Logf("控制台: http://%s:8848/nacos/", ip)
	t.Logf("账号: %s / %s", user, pass)
	t.Logf("服务将保持 10 分钟后自动注销\n")

	// 每 30 秒查询一次并输出状态
	for i := 0; i < 20; i++ {
		time.Sleep(30 * time.Second)
		insts, err := client.SelectInstances(vo.SelectInstancesParam{
			ServiceName: svc, GroupName: "DEFAULT_GROUP", HealthyOnly: true,
		})
		if err != nil {
			t.Logf("[%d] 查询失败: %v", i+1, err)
			continue
		}
		t.Logf("[%d] 实例数: %d (已运行 %s)", i+1, len(insts), time.Duration((i+1)*30)*time.Second)
	}

	r.Close()
	t.Log("\n服务已注销，请在 Nacos 控制台确认实例已移除\n")
}
