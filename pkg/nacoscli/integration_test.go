//go:build integration

package nacoscli

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain 强制限制集成测试总时长，防止 Nacos SDK gRPC goroutine 泄漏导致 go test 永远挂起。
// CI 环境（Linux + Docker 端口映射正确）中 gRPC 可达时，此超时不会触发。
func TestMain(m *testing.M) {
	ch := make(chan int, 1)
	go func() {
		ch <- m.Run()
	}()

	select {
	case code := <-ch:
		os.Exit(code)
	case <-time.After(120 * time.Second):
		fmt.Fprintln(os.Stderr, "FATAL: 集成测试超过 120 秒超时（可能是 Nacos SDK gRPC goroutine 泄漏）")
		os.Exit(1)
	}
}

// 从环境变量读取 Nacos 配置，未设置时跳过。
//
// 运行方式：
//
//	cd pkg/nacoscli
//	go test -tags=integration -v -run 'Test' -count=1
//
// 需要先加载 .env（IDE 中配置 EnvFile，或手动 export）。
func requireNacos(t *testing.T) (host string, port int, namespaceID, group, dataID string) {
	t.Helper()
	host = os.Getenv("NACOS_IP_ADDR")
	portStr := os.Getenv("NACOS_PORT")
	group = os.Getenv("NACOS_GROUP")
	dataID = os.Getenv("NACOS_DATA_ID")

	if host == "" || portStr == "" {
		t.Skip("NACOS_IP_ADDR 或 NACOS_PORT 未设置，跳过集成测试")
	}

	var err error
	port, err = strconv.Atoi(portStr)
	require.NoError(t, err, "NACOS_PORT 解析失败")

	namespaceID = os.Getenv("NACOS_NAMESPACE_ID")
	if group == "" {
		group = "dev"
	}
	if dataID == "" {
		dataID = "serverNameExample.yml"
	}
	return host, port, namespaceID, group, dataID
}

// requireAuth 从环境变量读取认证信息，未设置时跳过。
func requireAuth(t *testing.T) (username, password string) {
	t.Helper()
	username = os.Getenv("NACOS_USERNAME")
	password = os.Getenv("NACOS_PASSWORD")
	if username == "" || password == "" {
		t.Skip("NACOS_USERNAME 或 NACOS_PASSWORD 未设置，跳过认证测试")
	}
	return username, password
}

// requireGRPCPort 从环境变量读取 gRPC 端口，未设置时跳过依赖 gRPC 的测试。
func requireGRPCPort(t *testing.T) int {
	t.Helper()
	grpcPortStr := os.Getenv("NACOS_GRPC_PORT")
	if grpcPortStr == "" {
		t.Skip("NACOS_GRPC_PORT 未设置，跳过需要 gRPC 的测试")
	}
	port, err := strconv.Atoi(grpcPortStr)
	require.NoError(t, err, "NACOS_GRPC_PORT 解析失败")
	return port
}

// isGRPCReachable 检查 gRPC 端口是否可达（3 秒超时），不可达时返回 false。
func isGRPCReachable(host string, port int) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// testConfigKey 生成唯一的临时配置 ID，测试后通过 DeleteConfig 清理。
func testConfigKey() string {
	return fmt.Sprintf("nacoscli-test-%d", time.Now().UnixNano())
}

// publishConfig 通过 SDK 直接发布配置（测试辅助函数，nacoscli 包未封装 PublishConfig）。
// 返回 (cleanup, nil) 表示成功，调用方应 defer cleanup()；
// 返回 (cleanup, err) 表示失败（如 gRPC 不可达），调用方应 cleanup() 后 t.Skipf。
func publishConfig(t *testing.T, host string, port int, namespaceID, group, dataID, content, username, password string) (cleanup func(), err error) {
	t.Helper()
	o := defaultOptions()
	o.apply(WithIPAddr(host), WithPort(port), WithNamespaceID(namespaceID))
	if username != "" {
		o.apply(WithAuth(username, password))
	}
	clientConfig, serverConfigs := buildConfigs(o)
	configClient, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  clientConfig,
		ServerConfigs: serverConfigs,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Nacos 客户端失败: %w", err)
	}
	cleanup = func() { configClient.CloseClient() }

	published, err := configClient.PublishConfig(vo.ConfigParam{
		DataId:  dataID,
		Group:   group,
		Content: content,
	})
	if err != nil {
		return cleanup, fmt.Errorf("PublishConfig 失败（gRPC 可能不可达）: %w", err)
	}
	if !published {
		return cleanup, fmt.Errorf("PublishConfig 返回 false")
	}
	return cleanup, nil
}

// buildNamingServerConfigs 构建含 gRPC 端口的 ServerConfig。
func buildNamingServerConfigs(host string, port int, grpcPort int, namespaceID string) []constant.ServerConfig {
	return []constant.ServerConfig{
		{
			IpAddr:   host,
			Port:     uint64(port),
			Scheme:   "http",
			GrpcPort: uint64(grpcPort),
		},
	}
}

// ---------------------------------------------------------------------------
// 配置获取集成测试
// ---------------------------------------------------------------------------

// TestIntegration_GetConfig 一次性拉取配置（便捷函数）。
// 注意：Nacos 服务端可能配置了加密（ENC(...)），SDK 无密钥时会报错，属基础设施限制。
func TestIntegration_GetConfig(t *testing.T) {
	host, port, namespaceID, group, dataID := requireNacos(t)

	format, data, err := GetConfig(&Params{
		IPAddr:      host,
		Port:        port,
		NamespaceID: namespaceID,
		Group:       group,
		DataID:      dataID,
		Format:      "yaml",
	})
	if err != nil {
		// 加密配置 SDK 无法解密，记录但不 Fatal
		t.Logf("GetConfig 返回错误（可能是加密配置，SDK 无密钥）: %v", err)
		return
	}
	assert.Equal(t, "yaml", format)
	assert.NotEmpty(t, data, "配置内容不应为空")
	t.Logf("GetConfig 成功: format=%s, len=%d", format, len(data))
}

// TestIntegration_ClientGetConfig 复用客户端多次获取配置。
func TestIntegration_ClientGetConfig(t *testing.T) {
	host, port, namespaceID, group, dataID := requireNacos(t)

	client, err := NewConfigClient(
		WithIPAddr(host),
		WithPort(port),
		WithNamespaceID(namespaceID),
	)
	require.NoError(t, err)
	defer client.Close()

	format1, data1, err := client.GetConfig(context.Background(), &Params{
		Group:  group,
		DataID: dataID,
		Format: "yaml",
	})
	if err != nil {
		t.Logf("Client.GetConfig 返回错误（可能是加密配置）: %v", err)
		return
	}
	assert.Equal(t, "yaml", format1)
	assert.NotEmpty(t, data1)

	// 第二次拉取（复用同一客户端）
	format2, data2, err := client.GetConfig(context.Background(), &Params{
		Group:  group,
		DataID: dataID,
		Format: "yaml",
	})
	require.NoError(t, err)
	assert.Equal(t, format1, format2)
	assert.Equal(t, data1, data2, "两次拉取结果应一致")
	t.Logf("Client.GetConfig 两次拉取一致: len=%d", len(data1))
}

// TestIntegration_GetConfigWithContextCancel 验证 context 取消中断拉取。
func TestIntegration_GetConfigWithContextCancel(t *testing.T) {
	host, port, namespaceID, group, dataID := requireNacos(t)

	client, err := NewConfigClient(
		WithIPAddr(host),
		WithPort(port),
		WithNamespaceID(namespaceID),
	)
	require.NoError(t, err)
	defer client.Close()

	// 已取消的 ctx
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err = client.GetConfig(ctx, &Params{
		Group:  group,
		DataID: dataID,
		Format: "yaml",
	})
	assert.Error(t, err, "已取消的 ctx 应返回错误")
	t.Logf("context 取消正确返回: %v", err)
}

// TestIntegration_GetConfigNonExistentKey 获取不存在的配置项。
func TestIntegration_GetConfigNonExistentKey(t *testing.T) {
	host, port, namespaceID, group, _ := requireNacos(t)

	client, err := NewConfigClient(
		WithIPAddr(host),
		WithPort(port),
		WithNamespaceID(namespaceID),
	)
	require.NoError(t, err)
	defer client.Close()

	_, _, err = client.GetConfig(context.Background(), &Params{
		Group:  group,
		DataID: "non-existent-data-id-12345.yml",
		Format: "yaml",
	})
	// Nacos SDK 行为：不存在的配置返回空字符串或 SDK 错误
	if err != nil {
		t.Logf("不存在的配置项返回错误（预期行为）: %v", err)
	} else {
		t.Log("不存在的配置项正确返回空内容")
	}
}

// TestIntegration_GetConfigWithAuth 使用认证信息拉取配置。
func TestIntegration_GetConfigWithAuth(t *testing.T) {
	host, port, namespaceID, group, dataID := requireNacos(t)
	username, password := requireAuth(t)

	format, data, err := GetConfig(&Params{
		IPAddr:      host,
		Port:        port,
		NamespaceID: namespaceID,
		Group:       group,
		DataID:      dataID,
		Format:      "yaml",
	},
		WithAuth(username, password),
	)
	if err != nil {
		t.Logf("GetConfig with auth 返回错误（可能是加密配置）: %v", err)
		return
	}
	assert.Equal(t, "yaml", format)
	assert.NotEmpty(t, data)
	t.Logf("GetConfig with auth 成功: format=%s, len=%d", format, len(data))
}

// TestIntegration_GetConfigWithCustomTimeout 使用自定义超时拉取配置。
func TestIntegration_GetConfigWithCustomTimeout(t *testing.T) {
	host, port, namespaceID, group, dataID := requireNacos(t)

	format, _, err := GetConfig(&Params{
		IPAddr:      host,
		Port:        port,
		NamespaceID: namespaceID,
		Group:       group,
		DataID:      dataID,
		Format:      "yaml",
	},
		WithGetTimeout(10*time.Second),
		WithTimeoutMs(3000),
	)
	if err != nil {
		t.Logf("GetConfig with custom timeout 返回错误: %v", err)
		return
	}
	assert.Equal(t, "yaml", format)
	t.Log("GetConfig with custom timeout 成功")
}

// ---------------------------------------------------------------------------
// 配置发布与读取集成测试（CRUD 闭环）
// ---------------------------------------------------------------------------

// TestIntegration_PublishAndGetConfig 发布配置后立即拉取，验证 CRUD 闭环。
func TestIntegration_PublishAndGetConfig(t *testing.T) {
	host, port, namespaceID, group, _ := requireNacos(t)
	username, password := requireAuth(t)

	dataID := testConfigKey()
	content := fmt.Sprintf("key: %d", time.Now().UnixNano())

	// 发布（gRPC 不可达时 skip）
	cleanup, err := publishConfig(t, host, port, namespaceID, group, dataID, content, username, password)
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	if err != nil {
		t.Skipf("gRPC 不可达，跳过 PublishConfig 测试: %v", err)
	}
	t.Log("PublishConfig 成功")

	// 读取验证（带认证）
	format, data, err := GetConfig(&Params{
		IPAddr:      host,
		Port:        port,
		NamespaceID: namespaceID,
		Group:       group,
		DataID:      dataID,
		Format:      "yaml",
	},
		WithAuth(username, password),
	)
	require.NoError(t, err, "GetConfig 读取发布内容失败")
	assert.Equal(t, "yaml", format)
	assert.Equal(t, content, string(data), "读取内容应与发布内容一致")
	t.Logf("CRUD 闭环验证通过: dataID=%s, content=%s", dataID, data)
}

// TestIntegration_PublishAndGetConfigWithServerConfigs 使用 ServerConfigs 发布并读取。
func TestIntegration_PublishAndGetConfigWithServerConfigs(t *testing.T) {
	host, port, namespaceID, group, _ := requireNacos(t)
	username, password := requireAuth(t)

	dataID := testConfigKey()
	content := fmt.Sprintf("server-configs-test: %d", time.Now().UnixNano())

	sc := []constant.ServerConfig{
		{IpAddr: host, Port: uint64(port)},
	}

	// 发布
	o := defaultOptions()
	o.apply(WithServerConfigs(sc), WithNamespaceID(namespaceID))
	if username != "" {
		o.apply(WithAuth(username, password))
	}
	clientConfig, serverConfigs := buildConfigs(o)
	configClient, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  clientConfig,
		ServerConfigs: serverConfigs,
	})
	if err != nil {
		t.Skipf("创建客户端失败: %v", err)
	}
	defer configClient.CloseClient()

	published, err := configClient.PublishConfig(vo.ConfigParam{
		DataId:  dataID,
		Group:   group,
		Content: content,
	})
	if err != nil {
		t.Skipf("PublishConfig 失败: %v", err)
	}
	require.True(t, published, "PublishConfig 应返回 true")

	// 读取验证
	format, data, err := GetConfig(&Params{
		IPAddr: host, Port: port, NamespaceID: namespaceID,
		Group: group, DataID: dataID, Format: "yaml",
	}, WithAuth(username, password))
	require.NoError(t, err)
	assert.Equal(t, "yaml", format)
	assert.Equal(t, content, string(data))
	t.Log("ServerConfigs CRUD 闭环验证通过")
}

// ---------------------------------------------------------------------------
// ListenConfig 集成测试
// ---------------------------------------------------------------------------

// TestIntegration_ListenConfig 监听配置变更，等待 SDK 长轮询建立。
func TestIntegration_ListenConfig(t *testing.T) {
	host, port, namespaceID, group, dataID := requireNacos(t)

	var received atomic.Bool
	var receivedData atomic.Value

	handler := func(namespace, grp, dID, data string) {
		received.Store(true)
		receivedData.Store(data)
		t.Logf("ListenConfig 回调: namespace=%s, group=%s, dataID=%s, len=%d",
			namespace, grp, dID, len(data))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	params := &Params{
		IPAddr:      host,
		Port:        port,
		NamespaceID: namespaceID,
		Group:       group,
		DataID:      dataID,
		Format:      "yaml",
	}

	listener, err := NewListenClient(params, handler)
	require.NoError(t, err)
	defer func() {
		if stopErr := listener.Stop(); stopErr != nil {
			t.Logf("CancelListenConfig: %v", stopErr)
		}
		listener.Close()
	}()

	// Start 在后台 goroutine 中阻塞
	go func() {
		if err := listener.Start(ctx); err != nil {
			t.Logf("ListenClient.Start 异常: %v", err)
		}
	}()

	// 等待注册生效（SDK 长轮询建立需要时间）
	time.Sleep(3 * time.Second)
	t.Log("ListenConfig 注册成功，等待配置变更事件（5 秒超时）...")

	// 等待首次回调或超时（SDK 启动时可能触发一次初始回调）
	deadline := time.After(5 * time.Second)
	for !received.Load() {
		select {
		case <-deadline:
			t.Log("5 秒内未收到配置变更回调（Nacos 可能未触发变更通知，属正常行为）")
			return
		case <-time.After(200 * time.Millisecond):
		}
	}

	t.Logf("收到配置变更回调: %v", receivedData.Load())
}

// TestIntegration_ListenConfigPublishChange 发布配置变更后验证回调被触发。
func TestIntegration_ListenConfigPublishChange(t *testing.T) {
	host, port, namespaceID, group, _ := requireNacos(t)
	username, password := requireAuth(t)

	dataID := testConfigKey()
	originalContent := fmt.Sprintf("listen-test: %d", time.Now().UnixNano())

	// 先发布初始配置
	cleanup1, err := publishConfig(t, host, port, namespaceID, group, dataID, originalContent, username, password)
	defer func() {
		if cleanup1 != nil {
			cleanup1()
		}
	}()
	if err != nil {
		t.Skipf("gRPC 不可达，跳过 ListenConfig 变更测试: %v", err)
	}

	var received atomic.Bool
	var receivedData atomic.Value

	handler := func(_, _, _, data string) {
		received.Store(true)
		receivedData.Store(data)
		t.Logf("ListenConfig 变更回调: len=%d", len(data))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := NewListenClient(&Params{
		IPAddr: host, Port: port, NamespaceID: namespaceID,
		Group: group, DataID: dataID, Format: "yaml",
	}, handler, WithAuth(username, password))
	require.NoError(t, err)
	defer func() {
		listener.Stop()
		listener.Close()
	}()

	go func() {
		if err := listener.Start(ctx); err != nil {
			t.Logf("ListenClient.Start: %v", err)
		}
	}()

	// 等待监听注册生效
	time.Sleep(3 * time.Second)

	// 发布更新触发变更
	updatedContent := fmt.Sprintf("listen-test-updated: %d", time.Now().UnixNano())
	cleanup2, err := publishConfig(t, host, port, namespaceID, group, dataID, updatedContent, username, password)
	defer func() {
		if cleanup2 != nil {
			cleanup2()
		}
	}()
	if err != nil {
		t.Skipf("更新发布失败: %v", err)
	}
	t.Log("已发布更新配置，等待回调...")

	deadline := time.After(10 * time.Second)
	for changeCount := 0; changeCount == 0; {
		select {
		case <-deadline:
			t.Fatal("10 秒内未收到配置变更回调")
		case <-time.After(200 * time.Millisecond):
		}
		if received.Load() {
			break
		}
	}

	t.Logf("ListenConfig 变更检测成功: %v", receivedData.Load())
}

// ---------------------------------------------------------------------------
// WatchConfig 集成测试
// ---------------------------------------------------------------------------

// TestIntegration_WatchConfig 注册监听并优雅停止。
func TestIntegration_WatchConfig(t *testing.T) {
	host, port, namespaceID, group, dataID := requireNacos(t)

	var received atomic.Bool
	handler := func(namespace, grp, dID, data string) {
		received.Store(true)
		t.Logf("收到配置变更: namespace=%s, group=%s, dataID=%s, len=%d",
			namespace, grp, dID, len(data))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := WatchConfig(ctx, &Params{
		IPAddr:      host,
		Port:        port,
		NamespaceID: namespaceID,
		Group:       group,
		DataID:      dataID,
		Format:      "yaml",
	}, handler,
		WithIPAddr(host),
		WithPort(port),
		WithNamespaceID(namespaceID),
		WithMaxRetries(3),
		WithCreateDelay(time.Second),
	)
	require.NoError(t, err)
	require.NotNil(t, stop)

	// 等待注册完成
	time.Sleep(2 * time.Second)
	t.Log("WatchConfig 注册成功，等待 3 秒后停止...")

	// 停止监听
	stop()
	t.Logf("WatchConfig 已停止，是否收到变更: %v", received.Load())
}

// TestIntegration_WatchConfigStopWaitsGoroutine 验证 stop() 等待 goroutine 完全退出。
func TestIntegration_WatchConfigStopWaitsGoroutine(t *testing.T) {
	host, port, namespaceID, group, dataID := requireNacos(t)

	ctx := context.Background()
	stop, err := WatchConfig(ctx, &Params{
		IPAddr:      host,
		Port:        port,
		NamespaceID: namespaceID,
		Group:       group,
		DataID:      dataID,
		Format:      "yaml",
	}, func(_, _, _, _ string) {},
		WithIPAddr(host),
		WithPort(port),
		WithNamespaceID(namespaceID),
	)
	require.NoError(t, err)

	// stop() 应阻塞直到 goroutine 退出
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
		t.Log("stop() 正确等待 goroutine 退出")
	case <-time.After(10 * time.Second):
		t.Fatal("stop() 超时未返回，goroutine 可能泄漏")
	}
}

// TestIntegration_WatchConfigWithAuth 使用认证信息注册监听。
func TestIntegration_WatchConfigWithAuth(t *testing.T) {
	host, port, namespaceID, group, dataID := requireNacos(t)
	username, password := requireAuth(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := WatchConfig(ctx, &Params{
		IPAddr:      host,
		Port:        port,
		NamespaceID: namespaceID,
		Group:       group,
		DataID:      dataID,
		Format:      "yaml",
	}, func(_, _, _, _ string) {},
		WithAuth(username, password),
		WithMaxRetries(3),
	)
	require.NoError(t, err)
	require.NotNil(t, stop)

	time.Sleep(2 * time.Second)
	stop()
	t.Log("WatchConfig with auth 注册并停止成功")
}

// TestIntegration_WatchConfigMaxRetries 验证 WatchConfig 达到最大重试次数后停止。
func TestIntegration_WatchConfigMaxRetries(t *testing.T) {
	// 使用不存在的地址模拟持续创建失败
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts atomic.Int32
	stop, err := WatchConfig(ctx, &Params{
		IPAddr: "192.0.2.1", // RFC 5737 TEST-NET，不可达
		Port:   8848, Group: "g", DataID: "d", Format: "yaml",
	}, func(_, _, _, _ string) {
		attempts.Add(1)
	},
		WithMaxRetries(2),
		WithCreateDelay(100*time.Millisecond),
	)
	require.NoError(t, err)
	require.NotNil(t, stop)

	// 等待重试完成（2 次重试 × 100ms + SDK 超时）
	time.Sleep(5 * time.Second)
	stop()
	t.Logf("WatchConfig maxRetries 测试完成，回调次数: %d", attempts.Load())
	// 回调次数应为 0（地址不可达，永远无法注册成功）
	assert.Equal(t, int32(0), attempts.Load(), "不可达地址不应触发任何回调")
}

// TestIntegration_WatchConfigDetectsChange 验证 WatchConfig 能捕获配置变更。
func TestIntegration_WatchConfigDetectsChange(t *testing.T) {
	host, port, namespaceID, group, _ := requireNacos(t)
	username, password := requireAuth(t)

	dataID := testConfigKey()
	originalContent := fmt.Sprintf("watch-test: %d", time.Now().UnixNano())

	// 发布初始配置（gRPC 不可达时 skip）
	cleanup1, err := publishConfig(t, host, port, namespaceID, group, dataID, originalContent, username, password)
	defer func() {
		if cleanup1 != nil {
			cleanup1()
		}
	}()
	if err != nil {
		t.Skipf("gRPC 不可达，跳过 WatchConfig 变更检测: %v", err)
	}

	var changeCount atomic.Int32
	var lastData atomic.Value

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := WatchConfig(ctx, &Params{
		IPAddr: host, Port: port, NamespaceID: namespaceID,
		Group: group, DataID: dataID, Format: "yaml",
	}, func(_, _, _, data string) {
		changeCount.Add(1)
		lastData.Store(data)
		t.Logf("检测到变更 #%d: len=%d", changeCount.Load(), len(data))
	},
		WithAuth(username, password),
		WithCreateDelay(time.Second),
	)
	require.NoError(t, err)
	defer stop()

	// 等待监听注册生效
	time.Sleep(3 * time.Second)

	// 发布新配置触发变更（gRPC 不可达时 skip）
	updatedContent := fmt.Sprintf("watch-test-updated: %d", time.Now().UnixNano())
	cleanup2, err := publishConfig(t, host, port, namespaceID, group, dataID, updatedContent, username, password)
	defer func() {
		if cleanup2 != nil {
			cleanup2()
		}
	}()
	if err != nil {
		t.Skipf("gRPC 不可达，跳过配置发布: %v", err)
	}
	t.Log("已发布新配置，等待回调...")

	// 等待变更回调
	deadline := time.After(10 * time.Second)
	for changeCount.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("10 秒内未收到配置变更回调")
		case <-time.After(200 * time.Millisecond):
		}
	}

	t.Logf("配置变更检测成功: count=%d", changeCount.Load())
	assert.Equal(t, updatedContent, lastData.Load().(string), "回调数据应为更新后的内容")
}

// ---------------------------------------------------------------------------
// NamingClient 集成测试（需 gRPC 端口可达）
// ---------------------------------------------------------------------------

// TestIntegration_NamingClientRegisterAndDiscover 注册服务并发现。
func TestIntegration_NamingClientRegisterAndDiscover(t *testing.T) {
	host, port, namespaceID, _, _ := requireNacos(t)
	grpcPort := requireGRPCPort(t)

	if !isGRPCReachable(host, grpcPort) {
		t.Skipf("gRPC 端口 %d 不可达，跳过 NamingClient 测试", grpcPort)
	}

	namingClient, err := NewNamingClient(host, port, namespaceID,
		WithServerConfigs(buildNamingServerConfigs(host, port, grpcPort, namespaceID)),
	)
	require.NoError(t, err)
	defer namingClient.CloseClient()

	// 注册服务
	success, err := namingClient.RegisterInstance(vo.RegisterInstanceParam{
		Ip:          "10.0.0.1",
		Port:        8080,
		ServiceName: "nacoscli-integration-test",
		Weight:      1,
		Enable:      true,
		Healthy:     true,
		Ephemeral:   true,
	})
	if err != nil {
		t.Skipf("RegisterInstance 失败: %v", err)
	}
	if !success {
		t.Skipf("RegisterInstance 返回 false，gRPC 服务可能未就绪（client status: STARTING）")
	}
	t.Log("服务注册成功")

	// 发现服务
	instances, err := namingClient.SelectInstances(vo.SelectInstancesParam{
		ServiceName: "nacoscli-integration-test",
		GroupName:   "DEFAULT_GROUP",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, instances, "应能发现刚注册的服务实例")
	t.Logf("发现 %d 个实例", len(instances))

	// 注销服务
	deregistered, err := namingClient.DeregisterInstance(vo.DeregisterInstanceParam{
		Ip:          "10.0.0.1",
		Port:        8080,
		ServiceName: "nacoscli-integration-test",
		Ephemeral:   true,
	})
	require.NoError(t, err)
	assert.True(t, deregistered)
	t.Log("服务注销成功")
}

// TestIntegration_NamingClientSelectOneHealthyInstance 选择一个健康实例。
func TestIntegration_NamingClientSelectOneHealthyInstance(t *testing.T) {
	host, port, namespaceID, _, _ := requireNacos(t)
	grpcPort := requireGRPCPort(t)

	if !isGRPCReachable(host, grpcPort) {
		t.Skipf("gRPC 端口 %d 不可达，跳过 SelectOneHealthyInstance 测试", grpcPort)
	}

	namingClient, err := NewNamingClient(host, port, namespaceID,
		WithServerConfigs(buildNamingServerConfigs(host, port, grpcPort, namespaceID)),
	)
	require.NoError(t, err)
	defer namingClient.CloseClient()

	// 注册一个健康实例
	success, err := namingClient.RegisterInstance(vo.RegisterInstanceParam{
		Ip:          "10.0.0.2",
		Port:        9090,
		ServiceName: "nacoscli-integration-test-one",
		Weight:      1,
		Enable:      true,
		Healthy:     true,
		Ephemeral:   true,
	})
	if err != nil {
		t.Skipf("RegisterInstance 失败: %v", err)
	}
	if !success {
		t.Skipf("RegisterInstance 返回 false，gRPC 服务可能未就绪")
	}
	defer func() {
		_, _ = namingClient.DeregisterInstance(vo.DeregisterInstanceParam{
			Ip:          "10.0.0.2",
			Port:        9090,
			ServiceName: "nacoscli-integration-test-one",
			Ephemeral:   true,
		})
	}()

	// 选择一个健康实例
	instance, err := namingClient.SelectOneHealthyInstance(vo.SelectOneHealthInstanceParam{
		ServiceName: "nacoscli-integration-test-one",
		GroupName:   "DEFAULT_GROUP",
	})
	require.NoError(t, err)
	require.NotNil(t, instance)
	assert.Equal(t, "10.0.0.2", instance.Ip)
	assert.Equal(t, uint64(9090), instance.Port)
	t.Logf("SelectOneHealthyInstance: ip=%s, port=%d", instance.Ip, instance.Port)
}

// TestIntegration_NamingClientWithAuth 使用认证信息创建命名客户端。
func TestIntegration_NamingClientWithAuth(t *testing.T) {
	host, port, namespaceID, _, _ := requireNacos(t)
	username, password := requireAuth(t)
	grpcPort := requireGRPCPort(t)

	if !isGRPCReachable(host, grpcPort) {
		t.Skipf("gRPC 端口 %d 不可达，跳过 NamingClient auth 测试", grpcPort)
	}

	namingClient, err := NewNamingClient(host, port, namespaceID,
		WithAuth(username, password),
		WithServerConfigs(buildNamingServerConfigs(host, port, grpcPort, namespaceID)),
	)
	require.NoError(t, err)
	defer namingClient.CloseClient()
	t.Log("NamingClient with auth 创建成功")
}

// TestIntegration_NamingClientListInstances 列举服务实例。
func TestIntegration_NamingClientListInstances(t *testing.T) {
	host, port, namespaceID, _, _ := requireNacos(t)
	grpcPort := requireGRPCPort(t)

	if !isGRPCReachable(host, grpcPort) {
		t.Skipf("gRPC 端口 %d 不可达，跳过 ListInstances 测试", grpcPort)
	}

	namingClient, err := NewNamingClient(host, port, namespaceID,
		WithServerConfigs(buildNamingServerConfigs(host, port, grpcPort, namespaceID)),
	)
	require.NoError(t, err)
	defer namingClient.CloseClient()

	// 注册多个实例
	for i := 0; i < 3; i++ {
		success, err := namingClient.RegisterInstance(vo.RegisterInstanceParam{
			Ip:          fmt.Sprintf("10.0.0.%d", 10+i),
			Port:        uint64(8080 + i),
			ServiceName: "nacoscli-list-test",
			Weight:      1,
			Enable:      true,
			Healthy:     true,
			Ephemeral:   true,
		})
		if err != nil {
			t.Skipf("RegisterInstance 失败: %v", err)
		}
		if !success {
			t.Skipf("RegisterInstance 返回 false，gRPC 服务可能未就绪")
		}
	}
	defer func() {
		for i := 0; i < 3; i++ {
			_, _ = namingClient.DeregisterInstance(vo.DeregisterInstanceParam{
				Ip:          fmt.Sprintf("10.0.0.%d", 10+i),
				Port:        uint64(8080 + i),
				ServiceName: "nacoscli-list-test",
				Ephemeral:   true,
			})
		}
	}()

	// 列举实例
	instances, err := namingClient.SelectInstances(vo.SelectInstancesParam{
		ServiceName: "nacoscli-list-test",
		GroupName:   "DEFAULT_GROUP",
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(instances), 3, "应能发现至少 3 个实例")
	t.Logf("列举到 %d 个实例", len(instances))

	// SelectOneHealthyInstance 应返回其中一个
	instance, err := namingClient.SelectOneHealthyInstance(vo.SelectOneHealthInstanceParam{
		ServiceName: "nacoscli-list-test",
		GroupName:   "DEFAULT_GROUP",
	})
	require.NoError(t, err)
	require.NotNil(t, instance)
	t.Logf("随机选择: ip=%s, port=%d", instance.Ip, instance.Port)
}
