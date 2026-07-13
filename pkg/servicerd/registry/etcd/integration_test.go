// Package etcd 提供基于 etcd 的服务注册与发现集成测试。
package etcd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/18721889353/sunshine/pkg/etcdcli"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

const (
	etcdEndpoint = "43.143.78.234:2379"
	etcdUsername = "root"
	etcdPassword = "sunshine"
	testNS       = "/test_sunshine"
)

// connectEtcd 创建 etcd 客户端连接。
func connectEtcd(t *testing.T) *clientv3.Client {
	t.Helper()
	cli, err := etcdcli.NewClient(
		[]string{etcdEndpoint},
		etcdcli.WithDialTimeout(5*time.Second),
		etcdcli.WithAuth(etcdUsername, etcdPassword),
	)
	require.NoError(t, err, "连接 etcd 失败")
	require.NotNil(t, cli)
	return cli
}

// uniqueID 生成唯一实例 ID。
func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// newTestInstance 创建一个测试用服务实例。
func newTestInstance(svcName, instID string) *registry.ServiceInstance {
	return registry.NewServiceInstance(
		instID,
		svcName,
		[]string{fmt.Sprintf("grpc://127.0.0.1:8282")},
		registry.WithVersion("v1.0.0"),
		registry.WithMetadata(map[string]string{"env": "integration-test"}),
	)
}

// etcdKey 返回 etcd 中存储的完整 key 路径。
func etcdKey(namespace, svcName, instID string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, svcName, instID)
}

// ---------------------------------------------------------------------------
// 基本注册/注销测试
// ---------------------------------------------------------------------------

func TestIntegration_RegisterDeregister(t *testing.T) {
	cli := connectEtcd(t)
	defer cli.Close()

	svcName := "test-svc-reg"
	instID := uniqueID("inst")
	instance := newTestInstance(svcName, instID)
	key := etcdKey(testNS, svcName, instID)

	r := New(cli,
		WithNamespace(testNS),
		WithRegisterTTL(10*time.Second),
		WithCheckInterval(1), // 每次心跳校验
	)
	defer r.Close()

	t.Logf("注册实例: key=%s", key)

	// 注册
	err := r.Register(context.Background(), instance)
	require.NoError(t, err)
	t.Log("注册成功")

	// 验证 key 已写入
	time.Sleep(500 * time.Millisecond)
	resp, err := cli.Get(context.Background(), key)
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 1, "注册后 key 应存在")
	t.Logf("key 存在, value=%s", string(resp.Kvs[0].Value))

	// 注销
	err = r.Deregister(context.Background(), instance)
	require.NoError(t, err)
	t.Log("注销成功")

	// 验证 key 已删除
	time.Sleep(200 * time.Millisecond)
	resp, err = cli.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Len(t, resp.Kvs, 0, "注销后 key 应不存在")
}

// ---------------------------------------------------------------------------
// 自动恢复测试：模拟用户误删 key
// ---------------------------------------------------------------------------

func TestIntegration_AutoRecoverAfterDelete(t *testing.T) {
	cli := connectEtcd(t)
	defer cli.Close()

	svcName := "test-svc-recover"
	instID := uniqueID("inst")
	instance := newTestInstance(svcName, instID)
	key := etcdKey(testNS, svcName, instID)

	// 使用 checkInterval=1（每次心跳都校验），确保快速检测到误删
	r := New(cli,
		WithNamespace(testNS),
		WithRegisterTTL(15*time.Second),
		WithCheckInterval(1),
		WithMaxRetry(3),
	)
	defer r.Close()

	// 注册
	err := r.Register(context.Background(), instance)
	require.NoError(t, err)
	t.Log("注册成功")

	// 等待至少一次心跳完成
	time.Sleep(2 * time.Second)

	// 确认 key 存在
	resp, err := cli.Get(context.Background(), key)
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 1, "注册后 key 应存在")
	t.Log("key 存在，准备模拟误删")

	// 模拟用户误删 key
	_, err = cli.Delete(context.Background(), key)
	require.NoError(t, err)
	t.Log("模拟误删：key 已删除")

	// 确认 key 已被删
	resp, err = cli.Get(context.Background(), key)
	require.NoError(t, err)
	require.Len(t, resp.Kvs, 0, "删除后 key 应不存在")

	// 等待心跳自动恢复（checkInterval=1，最多 2 次 KeepAlive 周期）
	t.Log("等待心跳自动恢复...")
	time.Sleep(5 * time.Second)

	// 验证 key 已被心跳自动恢复
	resp, err = cli.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Len(t, resp.Kvs, 1, "心跳应自动恢复被删的 key")
	t.Logf("自动恢复成功: key=%s, value=%s", key, string(resp.Kvs[0].Value))

	// 清理
	err = r.Deregister(context.Background(), instance)
	require.NoError(t, err)
	t.Log("注销成功")
}

// ---------------------------------------------------------------------------
// 服务查询测试
// ---------------------------------------------------------------------------

func TestIntegration_GetService(t *testing.T) {
	cli := connectEtcd(t)
	defer cli.Close()

	svcName := "test-svc-query"
	inst1 := uniqueID("inst1")
	inst2 := uniqueID("inst2")
	instance1 := newTestInstance(svcName, inst1)
	instance2 := newTestInstance(svcName, inst2)

	r := New(cli,
		WithNamespace(testNS),
		WithRegisterTTL(15*time.Second),
		WithCheckInterval(1),
	)
	defer r.Close()

	// 注册两个实例
	err := r.Register(context.Background(), instance1)
	require.NoError(t, err)
	err = r.Register(context.Background(), instance2)
	require.NoError(t, err)
	t.Log("两个实例注册成功")

	time.Sleep(2 * time.Second)

	// 查询服务实例列表
	instances, err := r.GetService(context.Background(), svcName)
	require.NoError(t, err)
	t.Logf("查询到 %d 个实例", len(instances))

	// 验证实例数
	assert.GreaterOrEqual(t, len(instances), 2, "应查询到至少 2 个实例")

	// 验证实例信息
	found1, found2 := false, false
	for _, inst := range instances {
		if inst.ID == inst1 {
			found1 = true
			assert.Equal(t, svcName, inst.Name)
			assert.Contains(t, inst.Endpoints[0], "grpc://127.0.0.1:8282")
		}
		if inst.ID == inst2 {
			found2 = true
		}
	}
	assert.True(t, found1, "应找到实例1")
	assert.True(t, found2, "应找到实例2")

	// 清理
	_ = r.Deregister(context.Background(), instance1)
	_ = r.Deregister(context.Background(), instance2)
	t.Log("两个实例注销成功")
}

// ---------------------------------------------------------------------------
// 多次注册/注销循环测试
// ---------------------------------------------------------------------------

func TestIntegration_RegisterDeregisterCycle(t *testing.T) {
	cli := connectEtcd(t)
	defer cli.Close()

	svcName := "test-svc-cycle"
	instID := uniqueID("inst")
	instance := newTestInstance(svcName, instID)
	key := etcdKey(testNS, svcName, instID)

	r := New(cli,
		WithNamespace(testNS),
		WithRegisterTTL(10*time.Second),
		WithCheckInterval(1),
	)
	defer r.Close()

	// 执行 3 轮 注册→心跳→注销→验证
	for i := 0; i < 3; i++ {
		t.Logf("===== 第 %d 轮 =====", i+1)

		err := r.Register(context.Background(), instance)
		require.NoError(t, err, "第 %d 轮注册失败", i+1)
		t.Logf("第 %d 轮注册成功", i+1)

		time.Sleep(2 * time.Second)

		// 验证 key 存在
		resp, err := cli.Get(context.Background(), key)
		require.NoError(t, err)
		require.Len(t, resp.Kvs, 1, "第 %d 轮: key 应存在", i+1)
		t.Logf("第 %d 轮: key 存在, lease=%d", i+1, resp.Kvs[0].Lease)

		err = r.Deregister(context.Background(), instance)
		require.NoError(t, err, "第 %d 轮注销失败", i+1)
		t.Logf("第 %d 轮注销成功", i+1)

		time.Sleep(500 * time.Millisecond)

		// 验证 key 已删
		resp, err = cli.Get(context.Background(), key)
		require.NoError(t, err)
		assert.Len(t, resp.Kvs, 0, "第 %d 轮: 注销后 key 应不存在", i+1)

		// 等待 TTL 过期确保旧 lease 清理
		time.Sleep(1 * time.Second)
	}
	t.Log("3 轮循环全部通过")
}
