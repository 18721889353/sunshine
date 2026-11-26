package dlock

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/18721889353/sunshine/pkg/etcdcli"
)

func TestEtcdLock_TryLock(t *testing.T) {
	initLocker := func() Locker {
		return getEtcdLock(t)
	}
	testLockAndUnlock(initLocker, false, t)
}

func TestEtcdLock_Lock(t *testing.T) {
	initLocker := func() Locker {
		return getEtcdLock(t)
	}
	testLockAndUnlock(initLocker, true, t)
}

// testEtcdConfig 测试 etcd 连接：环境变量可覆盖，默认指向共享测试环境。
// 环境未就绪时 Skip（而不是 return nil 后被 testLockAndUnlock 静默跳过——
// 那会让环境故障伪装成通过）。
func testEtcdConfig(t *testing.T) (endpoints []string, username, password string) {
	t.Helper()
	endpoints = []string{"127.0.0.1:2379"}
	username, password = "", ""
	if ep := os.Getenv("TEST_ETCD_ENDPOINTS"); ep != "" {
		endpoints = strings.Split(ep, ",")
	}
	username = os.Getenv("TEST_ETCD_USERNAME")
	password = os.Getenv("TEST_ETCD_PASSWORD")
	return endpoints, username, password
}

func getEtcdLock(t *testing.T) Locker {
	t.Helper()
	endpoints, user, pass := testEtcdConfig(t)
	// 主动探测端口（NewClient 懒连接不报错，直接跑会等到 ctx 超时）
	if conn, err := net.DialTimeout("tcp", endpoints[0], time.Second); err != nil {
		t.Skipf("etcd 未就绪（非必配环境）: %v", err)
	} else {
		_ = conn.Close()
	}
	cli, err := etcdcli.NewClient(endpoints,
		etcdcli.WithDialTimeout(time.Second*2),
		etcdcli.WithAuth(user, pass),
		etcdcli.WithAutoSyncInterval(0),
	)
	if err != nil {
		t.Skipf("etcd 未就绪（非必配环境）: %v", err)
	}

	locker, err := NewEtcd(cli, "sunshine/dlock", 10)
	if err != nil {
		t.Fatalf("NewEtcd: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return locker
}
