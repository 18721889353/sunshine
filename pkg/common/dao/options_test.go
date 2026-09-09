package dao

import (
	"testing"
	"time"
)

// ============================================================================
// 测试用例：查询选项（Query Options）
// ============================================================================

// TestDefaultQueryOptions 测试默认查询选项
func TestDefaultQueryOptions(t *testing.T) {
	opts := DefaultQueryOptions()

	if opts.ForceMaster != true {
		t.Error("ForceMaster should be true by default")
	}

	if opts.Unscoped != false {
		t.Error("Unscoped should be false by default")
	}
}

// TestWithForceMaster 测试 WithForceMaster 选项
func TestWithForceMaster(t *testing.T) {
	opts := ApplyOptions(WithForceMaster())

	if opts.ForceMaster != true {
		t.Error("ForceMaster should be true after applying WithForceMaster")
	}
}

// TestWithForceSlave 测试 WithForceSlave 选项
func TestWithForceSlave(t *testing.T) {
	opts := ApplyOptions(WithForceSlave())

	if opts.ForceMaster != false {
		t.Error("ForceMaster should be false after applying WithForceSlave")
	}
}

// TestWithUnscoped 测试 WithUnscoped 选项
func TestWithUnscoped(t *testing.T) {
	opts := ApplyOptions(WithUnscoped())

	if opts.Unscoped != true {
		t.Error("Unscoped should be true after applying WithUnscoped")
	}
}

// TestApplyOptionsMultiple 测试多个选项组合
func TestApplyOptionsMultiple(t *testing.T) {
	opts := ApplyOptions(WithForceMaster(), WithUnscoped())

	if opts.ForceMaster != true {
		t.Error("ForceMaster should be true")
	}

	if opts.Unscoped != true {
		t.Error("Unscoped should be true")
	}
}

// TestApplyOptionsEmpty 测试空选项
func TestApplyOptionsEmpty(t *testing.T) {
	opts := ApplyOptions()

	if opts.ForceMaster != true {
		t.Error("ForceMaster should be true by default")
	}

	if opts.Unscoped != false {
		t.Error("Unscoped should be false by default")
	}
}

// TestApplyOptionsOrder 测试选项应用顺序
func TestApplyOptionsOrder(t *testing.T) {
	// 先设置 ForceMaster=true，再设置 ForceSlave（即 ForceMaster=false）
	opts := ApplyOptions(WithForceMaster(), WithForceSlave())

	if opts.ForceMaster != false {
		t.Error("ForceMaster should be false after applying WithForceSlave last")
	}
}

// ============================================================================
// 测试用例：DAO 创建选项（DAO Options）
// ============================================================================

// TestWithCacheConfig 测试 WithCacheConfig 选项
func TestWithCacheConfig(t *testing.T) {
	type TestEntity struct {
		ID uint64
	}

	cfg := CacheConfig{
		DefaultExpireTime:         10 * time.Minute,
		DefaultNotFoundExpireTime: 5 * time.Minute,
		LockRefreshSleepMs:        100,
		DelayedDeleteInterval:     200 * time.Millisecond,
		MaxCacheableRecords:       500,
		MaxCacheableIDs:           5000,
		MaxBatchSize:              500,
		PlaceholderValue:          "-",
	}

	opt := WithCacheConfig[TestEntity](cfg)
	config := &Config[TestEntity]{}
	opt(config)

	if config.cacheConfig == nil {
		t.Fatal("cacheConfig should not be nil")
	}

	if config.cacheConfig.DefaultExpireTime != 10*time.Minute {
		t.Errorf("DefaultExpireTime should be 10 minutes, got %v", config.cacheConfig.DefaultExpireTime)
	}

	if config.cacheConfig.DefaultNotFoundExpireTime != 5*time.Minute {
		t.Errorf("DefaultNotFoundExpireTime should be 5 minutes, got %v", config.cacheConfig.DefaultNotFoundExpireTime)
	}

	if config.cacheConfig.LockRefreshSleepMs != 100 {
		t.Errorf("LockRefreshSleepMs should be 100, got %d", config.cacheConfig.LockRefreshSleepMs)
	}

	if config.cacheConfig.DelayedDeleteInterval != 200*time.Millisecond {
		t.Errorf("DelayedDeleteInterval should be 200ms, got %v", config.cacheConfig.DelayedDeleteInterval)
	}

	if config.cacheConfig.MaxCacheableRecords != 500 {
		t.Errorf("MaxCacheableRecords should be 500, got %d", config.cacheConfig.MaxCacheableRecords)
	}

	if config.cacheConfig.MaxCacheableIDs != 5000 {
		t.Errorf("MaxCacheableIDs should be 5000, got %d", config.cacheConfig.MaxCacheableIDs)
	}

	if config.cacheConfig.MaxBatchSize != 500 {
		t.Errorf("MaxBatchSize should be 500, got %d", config.cacheConfig.MaxBatchSize)
	}

	if config.cacheConfig.PlaceholderValue != "-" {
		t.Errorf("PlaceholderValue should be '-', got '%s'", config.cacheConfig.PlaceholderValue)
	}
}

// TestWithNoCache 测试 WithNoCache 选项
func TestWithNoCache(t *testing.T) {
	type TestEntity struct {
		ID uint64
	}

	opt := WithNoCache[TestEntity]()
	config := &Config[TestEntity]{}
	opt(config)

	if config.disableCache != true {
		t.Error("disableCache should be true after applying WithNoCache")
	}
}

// TestWithLongCache 测试 WithLongCache 选项
func TestWithLongCache(t *testing.T) {
	type TestEntity struct {
		ID uint64
	}

	opt := WithLongCache[TestEntity]()
	config := &Config[TestEntity]{}
	opt(config)

	if config.cacheConfig == nil {
		t.Fatal("cacheConfig should not be nil")
	}

	if config.cacheConfig.DefaultExpireTime != 2*time.Hour {
		t.Errorf("DefaultExpireTime should be 2 hours, got %v", config.cacheConfig.DefaultExpireTime)
	}
}

// TestWithShortCache 测试 WithShortCache 选项
func TestWithShortCache(t *testing.T) {
	type TestEntity struct {
		ID uint64
	}

	opt := WithShortCache[TestEntity]()
	config := &Config[TestEntity]{}
	opt(config)

	if config.cacheConfig == nil {
		t.Fatal("cacheConfig should not be nil")
	}

	if config.cacheConfig.DefaultExpireTime != 5*time.Minute {
		t.Errorf("DefaultExpireTime should be 5 minutes, got %v", config.cacheConfig.DefaultExpireTime)
	}
}

// TestGetCacheConfigValue 测试 getCacheConfigValue 函数
func TestGetCacheConfigValue(t *testing.T) {
	t.Run("nil 指针", func(t *testing.T) {
		var cfg *CacheConfig
		result := getCacheConfigValue(cfg)

		// 验证返回零值
		if result.DefaultExpireTime != 0 {
			t.Error("DefaultExpireTime should be zero for nil pointer")
		}
	})

	t.Run("非 nil 指针", func(t *testing.T) {
		cfg := &CacheConfig{
			DefaultExpireTime: 30 * time.Minute,
		}
		result := getCacheConfigValue(cfg)

		if result.DefaultExpireTime != 30*time.Minute {
			t.Errorf("DefaultExpireTime should be 30 minutes, got %v", result.DefaultExpireTime)
		}
	})
}

// TestDefaultIDExtractor 测试 defaultIDExtractor 函数
func TestDefaultIDExtractor(t *testing.T) {
	type TestEntity struct {
		ID   uint64
		Name string
	}

	extractor := defaultIDExtractor[TestEntity]()

	t.Run("正常提取", func(t *testing.T) {
		entity := &TestEntity{ID: 42, Name: "test"}
		id := extractor(entity)

		if id != 42 {
			t.Errorf("ID should be 42, got %d", id)
		}
	})

	t.Run("nil 实体", func(t *testing.T) {
		id := extractor(nil)

		if id != 0 {
			t.Errorf("ID should be 0 for nil entity, got %d", id)
		}
	})

	t.Run("零值 ID", func(t *testing.T) {
		entity := &TestEntity{ID: 0, Name: "test"}
		id := extractor(entity)

		if id != 0 {
			t.Errorf("ID should be 0 for zero value, got %d", id)
		}
	})
}

// TestDefaultIDExtractorNoIDField 测试没有 ID 字段的结构体
func TestDefaultIDExtractorNoIDField(t *testing.T) {
	type NoIDEntity struct {
		Name string
	}

	extractor := defaultIDExtractor[NoIDEntity]()

	entity := &NoIDEntity{Name: "test"}
	id := extractor(entity)

	if id != 0 {
		t.Errorf("ID should be 0 for entity without ID field, got %d", id)
	}
}

// TestDefaultIDExtractorWrongType 测试 ID 字段类型错误
func TestDefaultIDExtractorWrongType(t *testing.T) {
	type WrongTypeIDEntity struct {
		ID int32
	}

	extractor := defaultIDExtractor[WrongTypeIDEntity]()

	entity := &WrongTypeIDEntity{ID: 42}
	id := extractor(entity)

	if id != 0 {
		t.Errorf("ID should be 0 for wrong ID type, got %d", id)
	}
}

// TestQueryOptionsImmutability 测试 QueryOptions 的不可变性
func TestQueryOptionsImmutability(t *testing.T) {
	// 创建一个选项函数
	opt1 := WithForceMaster()
	opt2 := WithUnscoped()

	// 应用 opt1
	opts1 := ApplyOptions(opt1)

	// 应用 opt2
	opts2 := ApplyOptions(opt2)

	// 验证 opts1 的 Unscoped 仍然是 false
	if opts1.Unscoped != false {
		t.Error("opts1.Unscoped should be false")
	}

	// 验证 opts2 的 ForceMaster 是 true
	if opts2.ForceMaster != true {
		t.Error("opts2.ForceMaster should be true")
	}
}

// TestCacheConfigPartialUpdate 测试缓存配置的部分更新
func TestOptionsCacheConfigPartialUpdate(t *testing.T) {
	type TestEntity struct {
		ID uint64
	}

	// 只更新部分字段
	cfg := CacheConfig{
		DefaultExpireTime: 10 * time.Minute,
	}

	opt := WithCacheConfig[TestEntity](cfg)
	config := &Config[TestEntity]{}
	opt(config)

	// 验证只更新了 DefaultExpireTime
	if config.cacheConfig.DefaultExpireTime != 10*time.Minute {
		t.Errorf("DefaultExpireTime should be 10 minutes, got %v", config.cacheConfig.DefaultExpireTime)
	}

	// 验证其他字段是零值
	if config.cacheConfig.DefaultNotFoundExpireTime != 0 {
		t.Errorf("DefaultNotFoundExpireTime should be 0, got %v", config.cacheConfig.DefaultNotFoundExpireTime)
	}
}
