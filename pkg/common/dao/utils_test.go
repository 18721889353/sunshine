package dao

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// 测试用例：SQL 转 COUNT SQL
// ============================================================================

// TestConvertToCountSQL 测试 ConvertToCountSQL 函数
// 注意：TiDB parser 需要导入驱动，单元测试中跳过
func TestConvertToCountSQL(t *testing.T) {
	ctx := context.Background()

	// 测试空 SQL
	result, err := ConvertToCountSQL(ctx, "")
	if err != nil {
		t.Fatalf("ConvertToCountSQL failed: %v", err)
	}
	if result != "SELECT COUNT(*) FROM (SELECT 1) AS count_query WHERE 1=0" {
		t.Errorf("unexpected result for empty SQL: %s", result)
	}

	t.Log("ConvertToCountSQL tests require TiDB parser driver, skipping non-trivial tests")
}

// ============================================================================
// 测试用例：构建分布式锁 Key
// ============================================================================

// TestBuildLockKey 测试 BuildLockKey 函数
func TestBuildLockKey(t *testing.T) {
	t.Run("正常拼接", func(t *testing.T) {
		key := BuildLockKey("lock:refresh", "123")
		if key != "lock:refresh:123" {
			t.Errorf("expected 'lock:refresh:123', got '%s'", key)
		}
	})

	t.Run("空前缀", func(t *testing.T) {
		key := BuildLockKey("", "123")
		if key != "123" {
			t.Errorf("expected '123', got '%s'", key)
		}
	})

	t.Run("空 key", func(t *testing.T) {
		key := BuildLockKey("lock:refresh", "")
		if key != "lock:refresh" {
			t.Errorf("expected 'lock:refresh', got '%s'", key)
		}
	})

	t.Run("都为空", func(t *testing.T) {
		key := BuildLockKey("", "")
		if key != "" {
			t.Errorf("expected empty string, got '%s'", key)
		}
	})

	t.Run("带冒号的 key", func(t *testing.T) {
		key := BuildLockKey("lock:refresh", "123:456")
		if key != "lock:refresh:123:456" {
			t.Errorf("expected 'lock:refresh:123:456', got '%s'", key)
		}
	})
}

// ============================================================================
// 测试用例：反射获取 ID
// ============================================================================

// TestGetObjectID 测试 GetObjectID 函数
func TestGetObjectID(t *testing.T) {
	type TestStruct struct {
		ID   uint64
		Name string
	}

	t.Run("正常提取", func(t *testing.T) {
		obj := &TestStruct{ID: 42, Name: "test"}
		id := GetObjectID(obj)
		if id != 42 {
			t.Errorf("expected 42, got %d", id)
		}
	})

	t.Run("非指针", func(t *testing.T) {
		obj := TestStruct{ID: 42, Name: "test"}
		id := GetObjectID(obj)
		if id != 42 {
			t.Errorf("expected 42, got %d", id)
		}
	})

	t.Run("零值 ID", func(t *testing.T) {
		obj := &TestStruct{ID: 0, Name: "test"}
		id := GetObjectID(obj)
		if id != 0 {
			t.Errorf("expected 0, got %d", id)
		}
	})

	t.Run("没有 ID 字段", func(t *testing.T) {
		type NoID struct {
			Name string
		}
		obj := &NoID{Name: "test"}
		id := GetObjectID(obj)
		if id != 0 {
			t.Errorf("expected 0, got %d", id)
		}
	})

	t.Run("nil 指针", func(t *testing.T) {
		var obj *TestStruct
		id := GetObjectID(obj)
		if id != 0 {
			t.Errorf("expected 0, got %d", id)
		}
	})
}

// ============================================================================
// 测试用例：命名转换
// ============================================================================

// TestUnderscoreToCamel 测试 UnderscoreToCamel 函数
func TestUnderscoreToCamel(t *testing.T) {
	t.Run("单个单词", func(t *testing.T) {
		result := UnderscoreToCamel("users")
		if result != "users" {
			t.Errorf("expected 'users', got '%s'", result)
		}
	})

	t.Run("两个单词", func(t *testing.T) {
		result := UnderscoreToCamel("user_example")
		if result != "userExample" {
			t.Errorf("expected 'userExample', got '%s'", result)
		}
	})

	t.Run("多个单词", func(t *testing.T) {
		result := UnderscoreToCamel("sys_user_example")
		if result != "sysUserExample" {
			t.Errorf("expected 'sysUserExample', got '%s'", result)
		}
	})

	t.Run("空字符串", func(t *testing.T) {
		result := UnderscoreToCamel("")
		if result != "" {
			t.Errorf("expected empty string, got '%s'", result)
		}
	})

	t.Run("只有下划线", func(t *testing.T) {
		result := UnderscoreToCamel("_")
		if result != "" {
			t.Errorf("expected empty string, got '%s'", result)
		}
	})

	t.Run("连续下划线", func(t *testing.T) {
		result := UnderscoreToCamel("user__example")
		if result != "userExample" {
			t.Errorf("expected 'userExample', got '%s'", result)
		}
	})

	t.Run("以下划线开头", func(t *testing.T) {
		result := UnderscoreToCamel("_user_example")
		if result != "UserExample" {
			t.Errorf("expected 'UserExample', got '%s'", result)
		}
	})

	t.Run("以下划线结尾", func(t *testing.T) {
		result := UnderscoreToCamel("user_example_")
		if result != "userExample" {
			t.Errorf("expected 'userExample', got '%s'", result)
		}
	})
}

// ============================================================================
// 测试用例：随机过期时间
// ============================================================================

// TestGetRandomExpireTime 测试 GetRandomExpireTime 函数
func TestGetRandomExpireTime(t *testing.T) {
	base := 30 * time.Minute

	// 多次调用，验证结果在合理范围内
	for i := 0; i < 100; i++ {
		result := GetRandomExpireTime(base)

		// 结果应该在 [base - 5分钟, base + 5分钟] 范围内
		minExpected := base - 5*time.Minute
		maxExpected := base + 5*time.Minute

		if result < minExpected || result > maxExpected {
			t.Errorf("GetRandomExpireTime(%v) = %v, expected between %v and %v",
				base, result, minExpected, maxExpected)
		}
	}
}

// TestGetRandomExpireTimeZeroBase 测试零值基础时间
func TestGetRandomExpireTimeZeroBase(t *testing.T) {
	result := GetRandomExpireTime(0)

	// 结果应该在 [-5分钟, +5分钟] 范围内
	minExpected := -5 * time.Minute
	maxExpected := 5 * time.Minute

	if result < minExpected || result > maxExpected {
		t.Errorf("GetRandomExpireTime(0) = %v, expected between %v and %v",
			result, minExpected, maxExpected)
	}
}

// TestGetRandomExpireTimeLargeBase 测试大值基础时间
func TestGetRandomExpireTimeLargeBase(t *testing.T) {
	base := 24 * time.Hour

	result := GetRandomExpireTime(base)

	// 结果应该在 [base - 5分钟, base + 5分钟] 范围内
	minExpected := base - 5*time.Minute
	maxExpected := base + 5*time.Minute

	if result < minExpected || result > maxExpected {
		t.Errorf("GetRandomExpireTime(%v) = %v, expected between %v and %v",
			base, result, minExpected, maxExpected)
	}
}

// ============================================================================
// 测试用例：缓存常量
// ============================================================================

// TestCacheConstants 测试缓存常量
func TestCacheConstants(t *testing.T) {
	// 验证缓存键前缀
	if CacheKeyPrefixCondition != "condition:" {
		t.Errorf("CacheKeyPrefixCondition should be 'condition:', got '%s'", CacheKeyPrefixCondition)
	}

	if CacheKeyPrefixColumns != "columns:" {
		t.Errorf("CacheKeyPrefixColumns should be 'columns:', got '%s'", CacheKeyPrefixColumns)
	}

	if CacheKeyPrefixExists != "exists:" {
		t.Errorf("CacheKeyPrefixExists should be 'exists:', got '%s'", CacheKeyPrefixExists)
	}

	if CacheKeyPrefixCount != "count:" {
		t.Errorf("CacheKeyPrefixCount should be 'count:', got '%s'", CacheKeyPrefixCount)
	}

	// 验证 Singleflight 去重键前缀
	if SFKeyPrefixOneCondition != "one_condition:" {
		t.Errorf("SFKeyPrefixOneCondition should be 'one_condition:', got '%s'", SFKeyPrefixOneCondition)
	}

	if SFKeyPrefixIDsCondition != "ids_condition:" {
		t.Errorf("SFKeyPrefixIDsCondition should be 'ids_condition:', got '%s'", SFKeyPrefixIDsCondition)
	}

	if SFKeyPrefixBatchIDs != "batch_ids:" {
		t.Errorf("SFKeyPrefixBatchIDs should be 'batch_ids:', got '%s'", SFKeyPrefixBatchIDs)
	}

	if SFKeyPrefixColumns != "columns:" {
		t.Errorf("SFKeyPrefixColumns should be 'columns:', got '%s'", SFKeyPrefixColumns)
	}

	// 验证分布式锁键前缀
	if LockKeyPrefixRefresh != "lock:refresh" {
		t.Errorf("LockKeyPrefixRefresh should be 'lock:refresh', got '%s'", LockKeyPrefixRefresh)
	}

	if LockKeyPrefixRefreshBatch != "lock:refresh:batch" {
		t.Errorf("LockKeyPrefixRefreshBatch should be 'lock:refresh:batch', got '%s'", LockKeyPrefixRefreshBatch)
	}

	// 验证随机偏移量范围
	if RandomOffsetMaxSeconds != 600 {
		t.Errorf("RandomOffsetMaxSeconds should be 600, got %d", RandomOffsetMaxSeconds)
	}

	if RandomOffsetMinSeconds != 300 {
		t.Errorf("RandomOffsetMinSeconds should be 300, got %d", RandomOffsetMinSeconds)
	}

	// 验证缓存删除类型
	if DeleteDaoTypeSingle != "single" {
		t.Errorf("DeleteDaoTypeSingle should be 'single', got '%s'", DeleteDaoTypeSingle)
	}

	if DeleteDaoTypeCondition != "condition" {
		t.Errorf("DeleteDaoTypeCondition should be 'condition', got '%s'", DeleteDaoTypeCondition)
	}

	if DeleteDaoTypeAll != "all" {
		t.Errorf("DeleteDaoTypeAll should be 'all', got '%s'", DeleteDaoTypeAll)
	}

	// 验证排序忽略统计常量
	if SortIgnoreCount != "ignore count" {
		t.Errorf("SortIgnoreCount should be 'ignore count', got '%s'", SortIgnoreCount)
	}
}

// ============================================================================
// 测试用例：SQL 模板常量
// ============================================================================

// TestSQLTemplateConstants 测试 SQL 模板常量
func TestSQLTemplateConstants(t *testing.T) {
	// 验证空 COUNT SQL
	if emptyCountSQL != "SELECT COUNT(*) FROM (SELECT 1) AS count_query WHERE 1=0" {
		t.Errorf("emptyCountSQL is incorrect")
	}

	// 验证 COUNT SQL 包装器
	if countSQLWrapper != "SELECT COUNT(*) FROM (%s) AS count_query" {
		t.Errorf("countSQLWrapper is incorrect")
	}
}
