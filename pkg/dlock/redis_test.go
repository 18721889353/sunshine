package dlock

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/18721889353/sunshine/pkg/goredis"
)

// testRedisDSN 测试 Redis DSN：环境变量可覆盖，默认指向本机。
// 密码不硬编码在多处——改环境只需改一处/设环境变量。
func testRedisDSN() string {
	if dsn := os.Getenv("TEST_REDIS_DSN"); dsn != "" {
		return dsn
	}
	pass := os.Getenv("TEST_REDIS_PASSWORD")
	if pass == "" {
		pass = "jianguo123"
	}
	// 空用户名 + 密码：Redis 6.0 以下只支持单参 AUTH password，
	// "user:pass" 会发 AUTH user pass 被老版本拒绝（wrong number of arguments）
	return fmt.Sprintf(":%s@127.0.0.1:6379", pass)
}

func TestRedisLock_TryLock(t *testing.T) {
	initLocker := func() Locker {
		return getRedisLock(t)
	}
	testLockAndUnlock(initLocker, false, t)
}

func TestRedisLock_Lock(t *testing.T) {
	initLocker := func() Locker {
		return getRedisLock(t)
	}
	testLockAndUnlock(initLocker, true, t)
}

func TestClusterRedis_TryLock(t *testing.T) {
	initLocker := func() Locker {
		return getClusterRedisLock(t)
	}
	testLockAndUnlock(initLocker, false, t)
}

func TestClusterRedis_Lock(t *testing.T) {
	initLocker := func() Locker {
		return getClusterRedisLock(t)
	}
	testLockAndUnlock(initLocker, true, t)
}

// getRedisLock 建立测试锁。连不上 Redis 属环境问题必须暴露——
// 原实现 return nil 后 testLockAndUnlock 只 t.Log 静默跳过，
// 环境故障下测试永远"通过"（假阳性）。
func getRedisLock(t *testing.T) Locker {
	t.Helper()
	redisCli, err := goredis.Init(testRedisDSN())
	if err != nil {
		t.Fatalf("redis 连接失败（环境问题）: %v", err)
	}
	locker, err := NewRedisLock(redisCli, "test_lock")
	if err != nil {
		t.Fatalf("NewRedisLock: %v", err)
	}
	t.Cleanup(func() { _ = redisCli.Close() })
	return locker
}

func getClusterRedisLock(t *testing.T) Locker {
	t.Helper()
	addrs := []string{"127.0.0.1:6380", "127.0.0.1:6381", "127.0.0.1:6382"}
	pass := os.Getenv("TEST_REDIS_PASSWORD")
	if pass == "" {
		pass = "jianguo123"
	}
	clusterClient, err := goredis.InitCluster(addrs, "", pass)
	if err != nil {
		t.Skipf("Redis 集群未就绪（非必配环境）: %v", err)
	}
	// InitCluster 懒连接（不立即报错），必须主动 Ping 才能判定就绪
	if err := clusterClient.Ping(context.Background()).Err(); err != nil {
		_ = clusterClient.Close()
		t.Skipf("Redis 集群未就绪（非必配环境）: %v", err)
	}

	locker, err := NewRedisClusterLock(clusterClient, "test_cluster_lock")
	if err != nil {
		t.Fatalf("NewRedisClusterLock: %v", err)
	}
	return locker
}

func testLockAndUnlock(initLocker func() Locker, isBlock bool, t *testing.T) {
	waitGroup := &sync.WaitGroup{}
	for i := 1; i <= 10; i++ {
		waitGroup.Add(1)
		go func(i int) {
			defer waitGroup.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()
			NO := fmt.Sprintf("[NO-%d] ", i)

			locker := initLocker()
			if locker == nil {
				// goroutine 内不能用 Fatal（vet 报错），用 Errorf 暴露并退出该 goroutine
				t.Errorf("locker 不应为 nil（getRedisLock 连不上已 Fatal，此处说明初始化被绕过）")
				return
			}

			var err error
			var ok bool
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				time.Sleep(time.Millisecond * 50)
				if isBlock {
					err = locker.Lock(ctx)
					if err == nil {
						ok = true
					}
				} else {
					ok, err = locker.TryLock(ctx)
				}
				if err != nil {
					//t.Log(NO+"try lock error:", err)
					continue
				}
				if ok {
					t.Log(NO + "acquire lock success, and do something")
					time.Sleep(time.Millisecond * 200)
					err = locker.Unlock(ctx)
					if err != nil {
						return
					}
					t.Log(NO + "unlock done")
					return
				}
			}
		}(i)
	}

	waitGroup.Wait()
}
