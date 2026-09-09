package dao

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-redsync/redsync/v4"
)

// ============================================================================
// Mock 缓存实现（用于测试 CacheAdapter）
// ============================================================================

// mockCache 是一个简单的内存缓存实现，用于测试
type mockCache struct {
	data map[string]interface{}
	mu   sync.RWMutex
}

func newMockCache() *mockCache {
	return &mockCache{
		data: make(map[string]interface{}),
	}
}

func (m *mockCache) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	return nil, nil
}

func (m *mockCache) GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	return nil, nil
}

func (m *mockCache) WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	return nil
}

func (m *mockCache) WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	return nil
}

func (m *mockCache) Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 存储值的副本，避免并发问题
	switch v := val.(type) {
	case *string:
		if v != nil {
			copy := *v
			m.data[key] = &copy
		} else {
			m.data[key] = nil
		}
	case *uint64:
		if v != nil {
			copy := *v
			m.data[key] = &copy
		} else {
			m.data[key] = nil
		}
	case *[]uint64:
		if v != nil {
			cp := make([]uint64, len(*v))
			copy(cp, *v)
			m.data[key] = &cp
		} else {
			m.data[key] = nil
		}
	default:
		m.data[key] = val
	}
	return nil
}

func (m *mockCache) Get(ctx context.Context, key string, val interface{}) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.data[key]; ok {
		// 简单的类型赋值
		switch target := val.(type) {
		case *interface{}:
			*target = v
		case **string:
			if s, ok := v.(*string); ok {
				*target = s
			} else {
				return fmt.Errorf("type mismatch: expected *string, got %T", v)
			}
		case *string:
			if s, ok := v.(*string); ok {
				*target = *s
			}
		case *uint64:
			if id, ok := v.(*uint64); ok {
				*target = *id
			}
		case *[]uint64:
			if ids, ok := v.(*[]uint64); ok {
				*target = *ids
			}
		default:
			return fmt.Errorf("unsupported type: %T", val)
		}
		return nil
	}
	return errors.New("cache not found")
}

func (m *mockCache) Del(ctx context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range keys {
		delete(m.data, key)
	}
	return nil
}

func (m *mockCache) MultiSet(ctx context.Context, valMap map[string]interface{}, expireTime time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range valMap {
		m.data[k] = v
	}
	return nil
}

func (m *mockCache) MultiGet(ctx context.Context, keys []string, valueMap interface{}) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if vm, ok := valueMap.(map[string]interface{}); ok {
		for _, key := range keys {
			if v, ok := m.data[key]; ok {
				vm[key] = v
			}
		}
	}
	return nil
}

func (m *mockCache) DelByPrefix(ctx context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.data {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(m.data, key)
		}
	}
	return nil
}

func (m *mockCache) SetCacheWithNotFound(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = "*"
	return nil
}

// ============================================================================
// 测试用例
// ============================================================================

// TestNewCacheAdapter 测试创建 CacheAdapter
func TestNewCacheAdapter(t *testing.T) {
	t.Run("正常创建", func(t *testing.T) {
		mock := newMockCache()
		adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
			return new(string)
		})

		if adapter == nil {
			t.Fatal("adapter should not be nil")
		}
		if adapter.prefix != "test:" {
			t.Errorf("prefix should be 'test:', got '%s'", adapter.prefix)
		}
	})

	t.Run("前缀自动添加冒号", func(t *testing.T) {
		mock := newMockCache()
		adapter := NewCacheAdapter[string](mock, "test", func() interface{} {
			return new(string)
		})

		if adapter.prefix != "test:" {
			t.Errorf("prefix should be 'test:', got '%s'", adapter.prefix)
		}
	})

	t.Run("空前缀", func(t *testing.T) {
		mock := newMockCache()
		adapter := NewCacheAdapter[string](mock, "", func() interface{} {
			return new(string)
		})

		if adapter.prefix != "" {
			t.Errorf("prefix should be empty, got '%s'", adapter.prefix)
		}
	})
}

// TestBuildKey 测试构建缓存 key
func TestBuildKey(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})

	key := adapter.buildKey(123)
	if key != "test:123" {
		t.Errorf("key should be 'test:123', got '%s'", key)
	}
}

// TestBuildKeyString 测试构建字符串缓存 key
func TestBuildKeyString(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})

	key := adapter.buildKeyString("abc")
	if key != "test:abc" {
		t.Errorf("key should be 'test:abc', got '%s'", key)
	}
}

// TestSetGetDel 测试 Set、Get、Del 方法
func TestAdapterSetGetDel(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	t.Run("Set 和 Get", func(t *testing.T) {
		data := "hello"
		err := adapter.Set(ctx, 1, &data, time.Minute)
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		got, err := adapter.Get(ctx, 1)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if *got != "hello" {
			t.Errorf("expected 'hello', got '%s'", *got)
		}
	})

	t.Run("Get 不存在的 key", func(t *testing.T) {
		_, err := adapter.Get(ctx, 999)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("Del", func(t *testing.T) {
		data := "to_delete"
		adapter.Set(ctx, 2, &data, time.Minute)
		adapter.Del(ctx, 2)

		_, err := adapter.Get(ctx, 2)
		if err == nil {
			t.Error("expected error after delete, got nil")
		}
	})

	t.Run("Set nil data", func(t *testing.T) {
		err := adapter.Set(ctx, 3, nil, time.Minute)
		if err != nil {
			t.Errorf("Set with nil data should return nil, got %v", err)
		}
	})

	t.Run("Set zero ID", func(t *testing.T) {
		data := "test"
		err := adapter.Set(ctx, 0, &data, time.Minute)
		if err != nil {
			t.Errorf("Set with zero ID should return nil, got %v", err)
		}
	})
}

// TestAdapterSetIDByKeyGetIDByKey 测试自定义 key 缓存 ID
func TestAdapterSetIDByKeyGetIDByKey(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	t.Run("正常流程", func(t *testing.T) {
		err := adapter.SetIDByKey(ctx, "user_id", 123, time.Minute)
		if err != nil {
			t.Fatalf("SetIDByKey failed: %v", err)
		}

		id, err := adapter.GetIDByKey(ctx, "user_id")
		if err != nil {
			t.Fatalf("GetIDByKey failed: %v", err)
		}

		if id != 123 {
			t.Errorf("expected 123, got %d", id)
		}
	})

	t.Run("空 key", func(t *testing.T) {
		err := adapter.SetIDByKey(ctx, "", 123, time.Minute)
		if err != nil {
			t.Errorf("SetIDByKey with empty key should return nil, got %v", err)
		}
	})

	t.Run("零 ID", func(t *testing.T) {
		err := adapter.SetIDByKey(ctx, "key", 0, time.Minute)
		if err != nil {
			t.Errorf("SetIDByKey with zero ID should return nil, got %v", err)
		}
	})
}

// TestAdapterSetIDsByKeyGetIDsByKey 测试自定义 key 缓存 ID 切片
func TestAdapterSetIDsByKeyGetIDsByKey(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	t.Run("正常流程", func(t *testing.T) {
		ids := []uint64{1, 2, 3}
		err := adapter.SetIDsByKey(ctx, "user_ids", ids, time.Minute)
		if err != nil {
			t.Fatalf("SetIDsByKey failed: %v", err)
		}

		got, err := adapter.GetIDsByKey(ctx, "user_ids")
		if err != nil {
			t.Fatalf("GetIDsByKey failed: %v", err)
		}

		if len(got) != 3 {
			t.Errorf("expected 3 IDs, got %d", len(got))
		}
	})

	t.Run("空 key", func(t *testing.T) {
		err := adapter.SetIDsByKey(ctx, "", []uint64{1}, time.Minute)
		if err != nil {
			t.Errorf("SetIDsByKey with empty key should return nil, got %v", err)
		}
	})

	t.Run("空切片", func(t *testing.T) {
		err := adapter.SetIDsByKey(ctx, "key", []uint64{}, time.Minute)
		if err != nil {
			t.Errorf("SetIDsByKey with empty slice should return nil, got %v", err)
		}
	})
}

// TestAdapterDelByKey 测试按自定义 key 删除
func TestAdapterDelByKey(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	adapter.SetIDByKey(ctx, "to_delete", 123, time.Minute)
	err := adapter.DelByKey(ctx, "to_delete")
	if err != nil {
		t.Fatalf("DelByKey failed: %v", err)
	}

	_, err = adapter.GetIDByKey(ctx, "to_delete")
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

// TestAdapterExtractID 测试 extractID 方法
func TestAdapterExtractID(t *testing.T) {
	type TestStruct struct {
		ID uint64
	}

	mock := newMockCache()
	adapter := NewCacheAdapter[TestStruct](mock, "test:", func() interface{} {
		return new(TestStruct)
	})

	t.Run("正常提取", func(t *testing.T) {
		obj := &TestStruct{ID: 42}
		id := adapter.extractID(obj)
		if id != 42 {
			t.Errorf("expected 42, got %d", id)
		}
	})

	t.Run("nil 对象", func(t *testing.T) {
		id := adapter.extractID(nil)
		if id != 0 {
			t.Errorf("expected 0, got %d", id)
		}
	})

	t.Run("没有 ID 字段", func(t *testing.T) {
		type NoID struct {
			Name string
		}
		mock2 := newMockCache()
		adapter2 := NewCacheAdapter[NoID](mock2, "test:", func() interface{} {
			return new(NoID)
		})
		obj := &NoID{Name: "test"}
		id := adapter2.extractID(obj)
		if id != 0 {
			t.Errorf("expected 0, got %d", id)
		}
	})
}

// TestAdapterIsPlaceholderErr 测试占位符错误判断
func TestAdapterIsPlaceholderErr(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})

	// 由于 mock 的 IsPlaceholderErr 总是返回 false
	err := errors.New("test error")
	if adapter.IsPlaceholderErr(err) {
		t.Error("expected false, got true")
	}
}

// testModel 用于 MultiSet/MultiGet 测试的带 ID 字段的模型
type testModel struct {
	ID   uint64
	Name string
}

// TestAdapterMultiSetMultiGet 测试批量操作
func TestAdapterMultiSetMultiGet(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[testModel](mock, "test:", func() interface{} {
		return new(testModel)
	})
	ctx := context.Background()

	t.Run("批量写入和读取", func(t *testing.T) {
		data := []*testModel{
			{ID: 1, Name: "hello"},
			{ID: 2, Name: "world"},
			{ID: 3, Name: "foo"},
		}
		err := adapter.MultiSet(ctx, data, time.Minute)
		if err != nil {
			t.Fatalf("MultiSet failed: %v", err)
		}

		// 通过 ID 1、2、3 读取
		result, err := adapter.MultiGet(ctx, []uint64{1, 2, 3})
		if err != nil {
			t.Fatalf("MultiGet failed: %v", err)
		}
		if len(result) != 3 {
			t.Errorf("expected 3 results, got %d", len(result))
		}
		if v := result[1]; v == nil || v.Name != "hello" {
			t.Errorf("expected name='hello' for id=1, got %v", v)
		}
		if v := result[2]; v == nil || v.Name != "world" {
			t.Errorf("expected name='world' for id=2, got %v", v)
		}
		if v := result[3]; v == nil || v.Name != "foo" {
			t.Errorf("expected name='foo' for id=3, got %v", v)
		}
	})

	t.Run("空切片", func(t *testing.T) {
		err := adapter.MultiSet(ctx, []*testModel{}, time.Minute)
		if err != nil {
			t.Errorf("MultiSet with empty slice should return nil, got %v", err)
		}
		result, err := adapter.MultiGet(ctx, []uint64{})
		if err != nil {
			t.Fatalf("MultiGet with empty slice failed: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("expected empty result, got %d items", len(result))
		}
	})

	t.Run("部分不存在的 ID", func(t *testing.T) {
		data := []*testModel{{ID: 10, Name: "exists"}}
		err := adapter.MultiSet(ctx, data, time.Minute)
		if err != nil {
			t.Fatalf("MultiSet failed: %v", err)
		}

		result, err := adapter.MultiGet(ctx, []uint64{10, 999})
		if err != nil {
			t.Fatalf("MultiGet failed: %v", err)
		}
		if len(result) != 1 {
			t.Errorf("expected 1 result, got %d", len(result))
		}
	})
}

// TestAdapterDelByPrefix 测试前缀删除
func TestAdapterDelByPrefix(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	t.Run("删除匹配前缀的缓存", func(t *testing.T) {
		// 写入多条数据
		data1 := "data1"
		data2 := "data2"
		adapter.Set(ctx, 1, &data1, time.Minute)
		adapter.Set(ctx, 2, &data2, time.Minute)

		// 确认数据存在
		if _, err := adapter.Get(ctx, 1); err != nil {
			t.Fatalf("Get before delete failed: %v", err)
		}

		// 通过前缀删除
		err := adapter.DelByPrefix(ctx, "test:")
		if err != nil {
			t.Fatalf("DelByPrefix failed: %v", err)
		}

		// 确认数据已删除
		if _, err := adapter.Get(ctx, 1); err == nil {
			t.Error("expected error after DelByPrefix, got nil")
		}
		if _, err := adapter.Get(ctx, 2); err == nil {
			t.Error("expected error after DelByPrefix, got nil")
		}
	})

	t.Run("删除不匹配的前缀", func(t *testing.T) {
		data := "keep_me"
		adapter.Set(ctx, 10, &data, time.Minute)

		err := adapter.DelByPrefix(ctx, "other:")
		if err != nil {
			t.Fatalf("DelByPrefix failed: %v", err)
		}

		// 数据应仍在
		got, err := adapter.Get(ctx, 10)
		if err != nil {
			t.Fatalf("Get should succeed after deleting non-matching prefix: %v", err)
		}
		if *got != "keep_me" {
			t.Errorf("expected 'keep_me', got '%s'", *got)
		}
	})
}

// TestAdapterSetPlaceholder 测试占位符设置
func TestAdapterSetPlaceholder(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	err := adapter.SetPlaceholder(ctx, 123)
	if err != nil {
		t.Fatalf("SetPlaceholder failed: %v", err)
	}

	// 验证占位符已设置
	key := adapter.buildKey(123)
	if val, ok := mock.data[key]; !ok || val != "*" {
		t.Error("placeholder not set correctly")
	}
}

// TestAdapterSetPlaceholderByKey 测试按自定义 key 设置占位符
func TestAdapterSetPlaceholderByKey(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	err := adapter.SetPlaceholderByKey(ctx, "my_key")
	if err != nil {
		t.Fatalf("SetPlaceholderByKey failed: %v", err)
	}

	// 验证占位符已设置
	key := adapter.buildKeyString("my_key")
	if val, ok := mock.data[key]; !ok || val != "*" {
		t.Error("placeholder not set correctly")
	}
}

// TestAdapterBuildLockKey 测试构建分布式锁 key
func TestAdapterBuildLockKey(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})

	key := adapter.buildLockKey("refresh")
	if key != "lock:test:refresh" {
		t.Errorf("expected 'lock:test:refresh', got '%s'", key)
	}
}

// TestAdapterGetIDByKeyNotExist 测试获取不存在的 key
func TestAdapterGetIDByKeyNotExist(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	_, err := adapter.GetIDByKey(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestAdapterGetIDsByKeyNotExist 测试获取不存在的 ID 列表
func TestAdapterGetIDsByKeyNotExist(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	_, err := adapter.GetIDsByKey(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestAdapterConcurrency 测试并发安全性
func TestAdapterConcurrency(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	// 并发写入（从 ID 1 开始，因为 Set 会检查 ID != 0）
	done := make(chan bool)
	for i := 1; i <= 10; i++ {
		go func(id uint64) {
			data := fmt.Sprintf("data_%d", id)
			adapter.Set(ctx, id, &data, time.Minute)
			done <- true
		}(uint64(i))
	}

	// 等待所有写入完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 验证所有数据都已写入（通过直接访问 mock 的内部数据）
	mock.mu.RLock()
	defer mock.mu.RUnlock()
	for i := 1; i <= 10; i++ {
		key := fmt.Sprintf("test:%d", i)
		if _, ok := mock.data[key]; !ok {
			t.Errorf("key %s not found in cache", key)
		}
	}
}

// TestAdapterBuildKeyWithZeroID 测试构建 key（ID 为 0）
func TestAdapterBuildKeyWithZeroID(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})

	key := adapter.buildKey(0)
	if key != "test:0" {
		t.Errorf("expected 'test:0', got '%s'", key)
	}
}

// TestAdapterBuildKeyWithMaxUint64 测试构建 key（ID 为 MaxUint64）
func TestAdapterBuildKeyWithMaxUint64(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})

	key := adapter.buildKey(^uint64(0))
	expected := fmt.Sprintf("test:%d", ^uint64(0))
	if key != expected {
		t.Errorf("expected '%s', got '%s'", expected, key)
	}
}

// TestAdapterNilDriver 测试 nil 驱动处理
func TestAdapterNilDriver(t *testing.T) {
	adapter := NewCacheAdapter[string](nil, "test:", func() interface{} {
		return new(string)
	})

	if adapter == nil {
		t.Fatal("adapter should not be nil")
	}

	// 测试 nil 驱动时的行为
	ctx := context.Background()
	data := "test"

	// Set 应该返回 nil（因为检查 data == nil || id == 0）
	err := adapter.Set(ctx, 0, &data, time.Minute)
	if err != nil {
		t.Errorf("Set should return nil, got %v", err)
	}

	// Get 应该返回错误（因为驱动是 nil）
	// 使用 recover 捕获 panic
	defer func() {
		if r := recover(); r != nil {
			// 预期的 panic，测试通过
			t.Logf("expected panic when driver is nil: %v", r)
		}
	}()
	_, err = adapter.Get(ctx, 1)
	if err == nil {
		t.Error("expected error when driver is nil")
	}
}

// TestAdapterLargeID 测试大 ID
func TestAdapterLargeID(t *testing.T) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "test:", func() interface{} {
		return new(string)
	})
	ctx := context.Background()

	largeID := uint64(18446744073709551615) // MaxUint64
	data := "large_id_data"
	err := adapter.Set(ctx, largeID, &data, time.Minute)
	if err != nil {
		t.Fatalf("Set with large ID failed: %v", err)
	}

	got, err := adapter.Get(ctx, largeID)
	if err != nil {
		t.Fatalf("Get with large ID failed: %v", err)
	}

	if *got != "large_id_data" {
		t.Errorf("expected 'large_id_data', got '%s'", *got)
	}
}
