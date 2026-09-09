//go:build integration
// +build integration

package cache

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/18721889353/sunshine/pkg/encoding"
)

// ============================================================================
// 测试配置 - 需要真实 Redis 环境
// ============================================================================

var (
	testRedisAddr     = getEnvOrDefault("TEST_REDIS_ADDR", "127.0.0.1:6379")
	testRedisPassword = getEnvOrDefault("TEST_REDIS_PASSWORD", "jianguo123")
	testRedisDB       = 0
)

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ============================================================================
// 辅助函数
// ============================================================================

type intTestUser struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func newIntegrationCache(t *testing.T) Cache {
	t.Helper()
	// 创建真实 Redis 客户端
	rdb := redis.NewClient(&redis.Options{
		Addr:     testRedisAddr,
		Password: testRedisPassword,
		DB:       testRedisDB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("skipping integration test: Redis not available at %s: %v", testRedisAddr, err)
	}

	// 创建缓存实例
	c := NewRedisCache(
		rdb,
		"integration_test:",
		encoding.JSONEncoding{},
		func() interface{} { return new(intTestUser) },
	)

	t.Cleanup(func() {
		// 清理测试数据
		_ = c.DelByPrefix(ctx, "integration_test:")
		_ = rdb.Close()
	})

	return c
}

// ============================================================================
// 集成测试 - Set/Get/Del
// ============================================================================

func TestIntegration_SetAndGet(t *testing.T) {
	c := newIntegrationCache(t)
	ctx := context.Background()

	// 清理测试数据
	_ = c.DelByPrefix(ctx, "integration_test::user:")

	t.Run("Set and Get", func(t *testing.T) {
		user := &intTestUser{ID: 1, Name: "张三", Age: 25}
		err := c.Set(ctx, "user:1", user, time.Minute)
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		var got intTestUser
		err = c.Get(ctx, "user:1", &got)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got.Name != "张三" || got.Age != 25 {
			t.Errorf("expected {张三, 25}, got %+v", got)
		}
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		var got intTestUser
		err := c.Get(ctx, "user:999", &got)
		if !errors.Is(err, CacheNotFound) {
			t.Errorf("expected CacheNotFound, got %v", err)
		}
	})
}

func TestIntegration_Del(t *testing.T) {
	c := newIntegrationCache(t)
	ctx := context.Background()

	// 设置数据
	_ = c.Set(ctx, "user:1", &intTestUser{ID: 1}, time.Minute)
	_ = c.Set(ctx, "user:2", &intTestUser{ID: 2}, time.Minute)

	// 删除单个 key
	err := c.Del(ctx, "user:1")
	if err != nil {
		t.Fatalf("Del failed: %v", err)
	}

	// 验证 user:1 已删除
	var got intTestUser
	err = c.Get(ctx, "user:1", &got)
	if !errors.Is(err, CacheNotFound) {
		t.Errorf("user:1 should be deleted, got %v", err)
	}

	// 验证 user:2 还在
	err = c.Get(ctx, "user:2", &got)
	if err != nil {
		t.Errorf("user:2 should still exist, got %v", err)
	}
}

// ============================================================================
// 集成测试 - MultiSet/MultiGet
// ============================================================================

func TestIntegration_MultiSetAndGet(t *testing.T) {
	c := newIntegrationCache(t)
	ctx := context.Background()

	// 批量设置
	valMap := map[string]interface{}{
		"user:1": &intTestUser{ID: 1, Name: "张三"},
		"user:2": &intTestUser{ID: 2, Name: "李四"},
		"user:3": &intTestUser{ID: 3, Name: "王五"},
	}
	err := c.MultiSet(ctx, valMap, time.Minute)
	if err != nil {
		t.Fatalf("MultiSet failed: %v", err)
	}

	// 批量获取
	resultMap := make(map[string]interface{})
	err = c.MultiGet(ctx, []string{"user:1", "user:2", "user:3"}, resultMap)
	if err != nil {
		t.Fatalf("MultiGet failed: %v", err)
	}

	// 验证
	if len(resultMap) != 3 {
		t.Errorf("expected 3 results, got %d", len(resultMap))
	}

	user1, ok := resultMap["user:1"].(*intTestUser)
	if !ok || user1.Name != "张三" {
		t.Errorf("user:1 should be {张三}, got %+v", resultMap["user:1"])
	}
}

// ============================================================================
// 集成测试 - DelByPrefix
// ============================================================================

func TestIntegration_DelByPrefix(t *testing.T) {
	c := newIntegrationCache(t)
	ctx := context.Background()

	// 设置数据
	_ = c.Set(ctx, "user:1", &intTestUser{ID: 1}, time.Minute)
	_ = c.Set(ctx, "user:2", &intTestUser{ID: 2}, time.Minute)
	_ = c.Set(ctx, "order:1", &intTestUser{ID: 3}, time.Minute)

	// 按前缀删除（使用业务层的前缀，内部会拼接 KeyPrefix）
	err := c.DelByPrefix(ctx, "integration_test::user:")
	if err != nil {
		t.Fatalf("DelByPrefix failed: %v", err)
	}

	// 验证 user:* 已删除
	var got intTestUser
	err = c.Get(ctx, "user:1", &got)
	if !errors.Is(err, CacheNotFound) {
		t.Errorf("user:1 should be deleted, got %v", err)
	}

	// 验证 order:1 还在
	err = c.Get(ctx, "order:1", &got)
	if err != nil {
		t.Errorf("order:1 should still exist, got %v", err)
	}
}

// ============================================================================
// 集成测试 - 占位符
// ============================================================================

func TestIntegration_SetCacheWithNotFound(t *testing.T) {
	c := newIntegrationCache(t)
	ctx := context.Background()

	// 设置占位符
	err := c.SetCacheWithNotFound(ctx, "user:999")
	if err != nil {
		t.Fatalf("SetCacheWithNotFound failed: %v", err)
	}

	// 读取应该返回 ErrPlaceholder
	var got intTestUser
	err = c.Get(ctx, "user:999", &got)
	if !errors.Is(err, ErrPlaceholder) {
		t.Errorf("expected ErrPlaceholder, got %v", err)
	}
}

// ============================================================================
// 集成测试 - 分布式锁
// ============================================================================

func TestIntegration_GetLock(t *testing.T) {
	c := newIntegrationCache(t)
	ctx := context.Background()

	// 获取锁
	lock, err := c.GetLock(ctx, "test-lock")
	if err != nil {
		t.Fatalf("GetLock failed: %v", err)
	}
	if lock == nil {
		t.Fatal("lock should not be nil")
	}

	// 释放锁
	_, err = lock.UnlockContext(ctx)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
}

func TestIntegration_GetLoopLock(t *testing.T) {
	c := newIntegrationCache(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 获取阻塞式锁
	lock, err := c.GetLoopLock(ctx, "test-loop-lock")
	if err != nil {
		t.Fatalf("GetLoopLock failed: %v", err)
	}
	if lock == nil {
		t.Fatal("lock should not be nil")
	}

	// 释放锁
	_, err = lock.UnlockContext(ctx)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
}

func TestIntegration_WatchDogLock(t *testing.T) {
	c := newIntegrationCache(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	executed := false
	err := c.WatchDogLock(ctx, "test-watchdog", 10*time.Second, func(ctx context.Context) error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("WatchDogLock failed: %v", err)
	}
	if !executed {
		t.Error("task should have been executed")
	}
}

// ============================================================================
// 集成测试 - 过期时间
// ============================================================================

func TestIntegration_Expiration(t *testing.T) {
	c := newIntegrationCache(t)
	ctx := context.Background()

	// 设置 2 秒过期
	err := c.Set(ctx, "expire-test", &testUser{ID: 1}, 2*time.Second)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// 立即读取应该存在
	var got testUser
	err = c.Get(ctx, "expire-test", &got)
	if err != nil {
		t.Fatalf("Get should succeed before expiration: %v", err)
	}

	// 等待过期
	time.Sleep(3 * time.Second)

	// 再次读取应该不存在
	err = c.Get(ctx, "expire-test", &got)
	if !errors.Is(err, CacheNotFound) {
		t.Errorf("expected CacheNotFound after expiration, got %v", err)
	}
}

// ============================================================================
// 集成测试 - 批量操作性能
// ============================================================================

func TestIntegration_BatchPerformance(t *testing.T) {
	c := newIntegrationCache(t)
	ctx := context.Background()

	// 批量写入 1000 条数据
	valMap := make(map[string]interface{}, 1000)
	for i := uint64(1); i <= 1000; i++ {
		valMap[string(rune(i))] = &testUser{ID: i, Name: "user"}
	}

	start := time.Now()
	err := c.MultiSet(ctx, valMap, time.Minute)
	if err != nil {
		t.Fatalf("MultiSet failed: %v", err)
	}
	writeDuration := time.Since(start)

	// 批量读取
	keys := make([]string, 0, 1000)
	for i := uint64(1); i <= 1000; i++ {
		keys = append(keys, string(rune(i)))
	}

	start = time.Now()
	resultMap := make(map[string]interface{}, 1000)
	err = c.MultiGet(ctx, keys, resultMap)
	if err != nil {
		t.Fatalf("MultiGet failed: %v", err)
	}
	readDuration := time.Since(start)

	t.Logf("Batch Write 1000 items: %v", writeDuration)
	t.Logf("Batch Read 1000 items: %v", readDuration)

	if len(resultMap) != 1000 {
		t.Errorf("expected 1000 results, got %d", len(resultMap))
	}
}
