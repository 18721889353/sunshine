package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/18721889353/sunshine/pkg/encoding"
)

// ============================================================================
// 测试模型
// ============================================================================

type testUser struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// ============================================================================
// 辅助函数
// ============================================================================

func newTestCache(t *testing.T) (Cache, *miniredis.Miniredis) {
	t.Helper()
	// 创建内存 Redis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	// 创建 Redis 客户端
	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	// 创建缓存实例
	c := NewRedisCache(
		rdb,
		"",
		encoding.JSONEncoding{},
		func() interface{} { return new(testUser) },
	)

	return c, mr
}

func TestNewRedisCache(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	if c == nil {
		t.Fatal("cache should not be nil")
	}
}

// ============================================================================
// Set/Get/Del 测试
// ============================================================================

func TestSetAndGet(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx := context.Background()

	t.Run("Set and Get", func(t *testing.T) {
		user := &testUser{ID: 1, Name: "张三", Age: 25}
		err := c.Set(ctx, "user:1", user, time.Minute)
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		var got testUser
		err = c.Get(ctx, "user:1", &got)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got.Name != "张三" || got.Age != 25 {
			t.Errorf("expected {张三, 25}, got %+v", got)
		}
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		var got testUser
		err := c.Get(ctx, "user:999", &got)
		if !errors.Is(err, CacheNotFound) {
			t.Errorf("expected CacheNotFound, got %v", err)
		}
	})
}

func TestDel(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx := context.Background()

	// 设置数据
	_ = c.Set(ctx, "user:1", &testUser{ID: 1}, time.Minute)
	_ = c.Set(ctx, "user:2", &testUser{ID: 2}, time.Minute)

	// 删除单个 key
	err := c.Del(ctx, "user:1")
	if err != nil {
		t.Fatalf("Del failed: %v", err)
	}

	// 验证 user:1 已删除
	var got testUser
	err = c.Get(ctx, "user:1", &got)
	if !errors.Is(err, CacheNotFound) {
		t.Errorf("user:1 should be deleted, got %v", err)
	}

	// 验证 user:2 还在
	err = c.Get(ctx, "user:2", &got)
	if err != nil {
		t.Errorf("user:2 should still exist, got %v", err)
	}

	// 删除多个 key
	err = c.Del(ctx, "user:2")
	if err != nil {
		t.Fatalf("Del failed: %v", err)
	}
}

func TestDelEmptyKeys(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx := context.Background()

	// 删除空 key 列表
	err := c.Del(ctx)
	if err != nil {
		t.Fatalf("Del with empty keys should not fail: %v", err)
	}
}

// ============================================================================
// MultiSet/MultiGet 测试
// ============================================================================

func TestMultiSetAndGet(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx := context.Background()

	// 批量设置
	valMap := map[string]interface{}{
		"user:1": &testUser{ID: 1, Name: "张三"},
		"user:2": &testUser{ID: 2, Name: "李四"},
		"user:3": &testUser{ID: 3, Name: "王五"},
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

	user1, ok := resultMap["user:1"].(*testUser)
	if !ok || user1.Name != "张三" {
		t.Errorf("user:1 should be {张三}, got %+v", resultMap["user:1"])
	}
}

func TestMultiSetEmpty(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx := context.Background()

	// 批量设置空 map
	err := c.MultiSet(ctx, map[string]interface{}{}, time.Minute)
	if err != nil {
		t.Fatalf("MultiSet with empty map should not fail: %v", err)
	}
}

func TestMultiGetEmpty(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx := context.Background()

	// 批量获取空 keys
	resultMap := make(map[string]interface{})
	err := c.MultiGet(ctx, []string{}, resultMap)
	if err != nil {
		t.Fatalf("MultiGet with empty keys should not fail: %v", err)
	}
}

// ============================================================================
// DelByPrefix 测试
// ============================================================================

func TestDelByPrefix(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx := context.Background()

	// 设置数据
	_ = c.Set(ctx, "user:1", &testUser{ID: 1}, time.Minute)
	_ = c.Set(ctx, "user:2", &testUser{ID: 2}, time.Minute)
	_ = c.Set(ctx, "order:1", &testUser{ID: 3}, time.Minute)

	// 按前缀删除
	err := c.DelByPrefix(ctx, "user:")
	if err != nil {
		t.Fatalf("DelByPrefix failed: %v", err)
	}

	// 验证 user:* 已删除
	var got testUser
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
// 占位符测试
// ============================================================================

func TestSetCacheWithNotFound(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx := context.Background()

	// 设置占位符
	err := c.SetCacheWithNotFound(ctx, "user:999")
	if err != nil {
		t.Fatalf("SetCacheWithNotFound failed: %v", err)
	}

	// 读取应该返回 ErrPlaceholder
	var got testUser
	err = c.Get(ctx, "user:999", &got)
	if !errors.Is(err, ErrPlaceholder) {
		t.Errorf("expected ErrPlaceholder, got %v", err)
	}
}

// ============================================================================
// 分布式锁测试
// ============================================================================

func TestGetLock(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

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

func TestGetLoopLock(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

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

// ============================================================================
// Key 构建测试
// ============================================================================

func TestBuildCacheKey(t *testing.T) {
	tests := []struct {
		prefix   string
		key      string
		expected string
		wantErr  bool
	}{
		{"prefix:", "user:1", "prefix::user:1", false}, // prefix 已包含冒号，key 会再加一个
		{"prefix", "user:1", "prefix:user:1", false},   // prefix 不包含冒号，会自动添加
		{"", "user:1", "user:1", false},                // 空前缀
		{"prefix:", "", "", true},                      // 空 key 会报错
		{"", "", "", true},                             // 都为空会报错
	}

	for _, tt := range tests {
		result, err := BuildCacheKey(tt.prefix, tt.key)
		if (err != nil) != tt.wantErr {
			t.Errorf("BuildCacheKey(%q, %q) error = %v, wantErr %v", tt.prefix, tt.key, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && result != tt.expected {
			t.Errorf("BuildCacheKey(%q, %q) = %q, want %q", tt.prefix, tt.key, result, tt.expected)
		}
	}
}

// ============================================================================
// WatchDogLock 测试
// ============================================================================

func TestWatchDogLock(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func TestWatchDogLockNilTask(t *testing.T) {
	c, mr := newTestCache(t)
	defer mr.Close()

	ctx := context.Background()

	err := c.WatchDogLock(ctx, "test-watchdog-nil", 10*time.Second, nil)
	if err == nil {
		t.Error("WatchDogLock with nil task should return error")
	}
}
