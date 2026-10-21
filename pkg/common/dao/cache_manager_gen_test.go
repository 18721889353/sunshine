package dao

// ============================================================================
// 查询缓存版本号（generation）失效机制单元测试
// 验证目标：
//  1. 查询缓存 key 带 "v{N}:" 版本段，版本随 INCR 推进
//  2. deleteCache(Condition) 一次 INCR 即让旧版本查询缓存逻辑作废（不再 SCAN 物理删除）
//  3. 旧版本 key 不被物理删除（靠 TTL 自然过期），也不会被读到（免疫脏回填）
//  4. Single 类型失效不推进版本号（单条缓存无版本段）
//  5. 升级兼容：无版本段的旧格式 key 不再被读取
// ============================================================================

import (
	"context"
	"testing"
	"time"
)

// newGenTestManager 创建带 mock 驱动的 cacheManager，adapter 前缀为 "data:TestUsers:"
func newGenTestManager() (*cacheManager[string], *mockCache) {
	mock := newMockCache()
	adapter := NewCacheAdapter[string](mock, "data:TestUsers:", func() interface{} {
		return new(string)
	})
	cm := newCacheManager[string](adapter, DefaultCacheConfig(), "test_users")
	return cm, mock
}

// TestGenVersionedKey 验证版本段拼接与版本推进
func TestGenVersionedKey(t *testing.T) {
	cm, mock := newGenTestManager()
	ctx := context.Background()

	// gen key 不存在时视为 0 版本
	if got := cm.versionedKey(ctx, CacheKeyPrefixCount+"k1"); got != "v0:count:k1" {
		t.Errorf("expected 'v0:count:k1', got %q", got)
	}

	// INCR 一次后为 1 版本
	if _, err := mock.Incr(ctx, "data:TestUsers:"+CacheKeyGen); err != nil {
		t.Fatalf("Incr failed: %v", err)
	}
	if got := cm.versionedKey(ctx, CacheKeyPrefixCount+"k1"); got != "v1:count:k1" {
		t.Errorf("expected 'v1:count:k1', got %q", got)
	}

	// 再 INCR 一次后为 2 版本
	if _, err := cm.cache.IncrByKey(ctx, CacheKeyGen); err != nil {
		t.Fatalf("IncrByKey failed: %v", err)
	}
	if got := cm.versionedKey(ctx, CacheKeyPrefixCondition+"abc"); got != "v2:condition:abc" {
		t.Errorf("expected 'v2:condition:abc', got %q", got)
	}
}

// TestGetCountGenInvalidation 验证计数缓存端到端的版本号失效
func TestGetCountGenInvalidation(t *testing.T) {
	cm, mock := newGenTestManager()
	ctx := context.Background()

	dbCalls := 0
	queryFunc := func() (int64, error) {
		dbCalls++
		return int64(dbCalls * 100), nil
	}

	// 第一次查询：穿透 DB，回填 v0 版本 key
	count, err := cm.getCount(ctx, "k1", queryFunc)
	if err != nil || count != 100 {
		t.Fatalf("first getCount got (%d, %v), want (100, nil)", count, err)
	}
	if _, ok := mock.data["data:TestUsers:v0:count:k1"]; !ok {
		t.Fatalf("expected v0 key backfilled, data=%+v", mock.data)
	}

	// 第二次查询：命中 v0 缓存，不再穿透 DB
	count, err = cm.getCount(ctx, "k1", queryFunc)
	if err != nil || count != 100 || dbCalls != 1 {
		t.Fatalf("second getCount got (%d, %v, calls=%d), want (100, nil, 1)", count, err, dbCalls)
	}

	// 写失效：Condition 类型只 INCR 版本游标，不物理删除任何 key
	cm.deleteCache(ctx, 0, nil, DeleteDaoTypeCondition)
	if v := cm.getGenVersion(ctx); v != 1 {
		t.Fatalf("gen version should be 1 after Condition invalidation, got %d", v)
	}
	if _, ok := mock.data["data:TestUsers:v0:count:k1"]; !ok {
		t.Error("v0 key should NOT be physically deleted (relies on TTL)")
	}

	// 失效后查询：读 v1 key miss → 穿透 DB 得新值，并回填 v1 key
	count, err = cm.getCount(ctx, "k1", queryFunc)
	if err != nil || count != 200 || dbCalls != 2 {
		t.Fatalf("getCount after invalidation got (%d, %v, calls=%d), want (200, nil, 2)", count, err, dbCalls)
	}
	if _, ok := mock.data["data:TestUsers:v1:count:k1"]; !ok {
		t.Error("expected v1 key backfilled after re-query")
	}
	// 旧 v0 key 值仍是 100，但已无人读取（逻辑作废）
	if id, err := cm.cache.GetIDByKey(ctx, "v0:count:k1"); err != nil || id != 100 {
		t.Errorf("stale v0 key should keep old value 100, got (%d, %v)", id, err)
	}
}

// TestDeleteCacheSingleKeepsGen 验证 Single 类型失效不推进版本号
func TestDeleteCacheSingleKeepsGen(t *testing.T) {
	cm, mock := newGenTestManager()
	ctx := context.Background()

	// 预置一个查询缓存 key
	if err := cm.cache.SetIDByKey(ctx, "v0:count:k1", 5, time.Minute); err != nil {
		t.Fatalf("SetIDByKey failed: %v", err)
	}

	cm.deleteCache(ctx, 1, nil, DeleteDaoTypeSingle)

	// gen 不存在 → 版本仍为 0，查询缓存 key 不受影响
	if v := cm.getGenVersion(ctx); v != 0 {
		t.Errorf("gen version should stay 0 for Single deletion, got %d", v)
	}
	if _, err := cm.cache.GetIDByKey(ctx, "v0:count:k1"); err != nil {
		t.Errorf("query cache should survive Single invalidation: %v", err)
	}
	// 单条缓存 data:TestUsers:1 被精确删除
	if _, ok := mock.data["data:TestUsers:1"]; ok {
		t.Error("single record cache should be deleted")
	}
}

// TestDelayedDoubleDeleteConditionNoScan 验证 Condition 的延迟双删已退役（不再前缀删除）
func TestDelayedDoubleDeleteConditionNoScan(t *testing.T) {
	cm, mock := newGenTestManager()
	ctx := context.Background()

	// 缩短双删间隔，便于测试
	cfg := DefaultCacheConfig()
	cfg.DelayedDeleteInterval = 10 * time.Millisecond
	cm.config = cfg

	if err := cm.cache.SetIDByKey(ctx, "v0:condition:abc", 9, time.Minute); err != nil {
		t.Fatalf("SetIDByKey failed: %v", err)
	}

	cm.deleteCache(ctx, 0, nil, DeleteDaoTypeCondition)
	cm.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeCondition)

	// 等待双删 goroutine 执行完毕
	time.Sleep(100 * time.Millisecond)

	// v0:condition:abc 不应被物理删除（旧行为是 SCAN 全删；新行为靠版本作废）
	if _, ok := mock.data["data:TestUsers:v0:condition:abc"]; !ok {
		t.Error("Condition delayed-double-delete should no longer physically delete old version keys")
	}
	// 版本只被 deleteCache 推进了一次
	if v := cm.getGenVersion(ctx); v != 1 {
		t.Errorf("gen version should be 1, got %d", v)
	}
}

// TestLegacyUnversionedKeyIgnored 验证升级兼容：无版本段的旧格式 key 不再被读取
func TestLegacyUnversionedKeyIgnored(t *testing.T) {
	cm, mock := newGenTestManager()
	ctx := context.Background()

	// 模拟升级前遗留的旧格式 key（无 v 段），值为过期的 999
	mock.data["data:TestUsers:count:k1"] = uint64PtrForTest(999)

	dbCalled := false
	count, err := cm.getCount(ctx, "k1", func() (int64, error) {
		dbCalled = true
		return 42, nil
	})
	if err != nil || count != 42 {
		t.Fatalf("getCount got (%d, %v), want (42, nil)", count, err)
	}
	if !dbCalled {
		t.Error("legacy unversioned key must NOT be treated as a cache hit")
	}
}

// uint64PtrForTest 生成 *uint64（与 mockCache 存储格式一致）
func uint64PtrForTest(v uint64) *uint64 {
	return &v
}
