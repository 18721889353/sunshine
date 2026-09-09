package dao

import (
	"testing"
	"time"
)

// ============================================================================
// 测试用例：缓存配置结构体
// ============================================================================

// TestDefaultCacheConfig 测试默认缓存配置
func TestDefaultCacheConfig(t *testing.T) {
	cfg := DefaultCacheConfig()

	// 验证默认过期时间
	if cfg.DefaultExpireTime != 30*time.Minute {
		t.Errorf("DefaultExpireTime should be 30 minutes, got %v", cfg.DefaultExpireTime)
	}

	// 验证默认占位符过期时间
	if cfg.DefaultNotFoundExpireTime != 1*time.Minute {
		t.Errorf("DefaultNotFoundExpireTime should be 1 minute, got %v", cfg.DefaultNotFoundExpireTime)
	}

	// 验证默认锁等待时间
	if cfg.LockRefreshSleepMs != 50 {
		t.Errorf("LockRefreshSleepMs should be 50, got %d", cfg.LockRefreshSleepMs)
	}

	// 验证默认延迟删除间隔
	if cfg.DelayedDeleteInterval != 100*time.Millisecond {
		t.Errorf("DelayedDeleteInterval should be 100ms, got %v", cfg.DelayedDeleteInterval)
	}

	// 验证默认最大缓存记录数
	if cfg.MaxCacheableRecords != 1000 {
		t.Errorf("MaxCacheableRecords should be 1000, got %d", cfg.MaxCacheableRecords)
	}

	// 验证默认最大缓存 ID 数
	if cfg.MaxCacheableIDs != 10000 {
		t.Errorf("MaxCacheableIDs should be 10000, got %d", cfg.MaxCacheableIDs)
	}

	// 验证默认批次大小
	if cfg.MaxBatchSize != 1000 {
		t.Errorf("MaxBatchSize should be 1000, got %d", cfg.MaxBatchSize)
	}

	// 验证默认占位符值
	if cfg.PlaceholderValue != "*" {
		t.Errorf("PlaceholderValue should be '*', got '%s'", cfg.PlaceholderValue)
	}
}

// TestCacheConfigCustomValues 测试自定义缓存配置值
func TestCacheConfigCustomValues(t *testing.T) {
	cfg := CacheConfig{
		DefaultExpireTime:         10 * time.Minute,
		DefaultNotFoundExpireTime: 30 * time.Second,
		LockRefreshSleepMs:        100,
		DelayedDeleteInterval:     200 * time.Millisecond,
		MaxCacheableRecords:       500,
		MaxCacheableIDs:           5000,
		MaxBatchSize:              500,
		PlaceholderValue:          "-",
	}

	if cfg.DefaultExpireTime != 10*time.Minute {
		t.Errorf("DefaultExpireTime should be 10 minutes, got %v", cfg.DefaultExpireTime)
	}

	if cfg.DefaultNotFoundExpireTime != 30*time.Second {
		t.Errorf("DefaultNotFoundExpireTime should be 30 seconds, got %v", cfg.DefaultNotFoundExpireTime)
	}

	if cfg.LockRefreshSleepMs != 100 {
		t.Errorf("LockRefreshSleepMs should be 100, got %d", cfg.LockRefreshSleepMs)
	}

	if cfg.DelayedDeleteInterval != 200*time.Millisecond {
		t.Errorf("DelayedDeleteInterval should be 200ms, got %v", cfg.DelayedDeleteInterval)
	}

	if cfg.MaxCacheableRecords != 500 {
		t.Errorf("MaxCacheableRecords should be 500, got %d", cfg.MaxCacheableRecords)
	}

	if cfg.MaxCacheableIDs != 5000 {
		t.Errorf("MaxCacheableIDs should be 5000, got %d", cfg.MaxCacheableIDs)
	}

	if cfg.MaxBatchSize != 500 {
		t.Errorf("MaxBatchSize should be 500, got %d", cfg.MaxBatchSize)
	}

	if cfg.PlaceholderValue != "-" {
		t.Errorf("PlaceholderValue should be '-', got '%s'", cfg.PlaceholderValue)
	}
}

// TestCacheConfigZeroValues 测试零值配置
func TestCacheConfigZeroValues(t *testing.T) {
	cfg := CacheConfig{}

	// 验证所有字段都是零值
	if cfg.DefaultExpireTime != 0 {
		t.Errorf("DefaultExpireTime should be 0, got %v", cfg.DefaultExpireTime)
	}

	if cfg.DefaultNotFoundExpireTime != 0 {
		t.Errorf("DefaultNotFoundExpireTime should be 0, got %v", cfg.DefaultNotFoundExpireTime)
	}

	if cfg.LockRefreshSleepMs != 0 {
		t.Errorf("LockRefreshSleepMs should be 0, got %d", cfg.LockRefreshSleepMs)
	}

	if cfg.DelayedDeleteInterval != 0 {
		t.Errorf("DelayedDeleteInterval should be 0, got %v", cfg.DelayedDeleteInterval)
	}

	if cfg.MaxCacheableRecords != 0 {
		t.Errorf("MaxCacheableRecords should be 0, got %d", cfg.MaxCacheableRecords)
	}

	if cfg.MaxCacheableIDs != 0 {
		t.Errorf("MaxCacheableIDs should be 0, got %d", cfg.MaxCacheableIDs)
	}

	if cfg.MaxBatchSize != 0 {
		t.Errorf("MaxBatchSize should be 0, got %d", cfg.MaxBatchSize)
	}

	if cfg.PlaceholderValue != "" {
		t.Errorf("PlaceholderValue should be empty, got '%s'", cfg.PlaceholderValue)
	}
}

// TestCacheConfigPartialUpdate 测试配置的部分更新
func TestCacheConfigPartialUpdate(t *testing.T) {
	// 从默认配置开始
	cfg := DefaultCacheConfig()

	// 只更新部分字段
	cfg.DefaultExpireTime = 10 * time.Minute
	cfg.MaxBatchSize = 500

	// 验证更新的字段
	if cfg.DefaultExpireTime != 10*time.Minute {
		t.Errorf("DefaultExpireTime should be 10 minutes, got %v", cfg.DefaultExpireTime)
	}

	if cfg.MaxBatchSize != 500 {
		t.Errorf("MaxBatchSize should be 500, got %d", cfg.MaxBatchSize)
	}

	// 验证未更新的字段保持默认值
	if cfg.DefaultNotFoundExpireTime != 1*time.Minute {
		t.Errorf("DefaultNotFoundExpireTime should still be 1 minute, got %v", cfg.DefaultNotFoundExpireTime)
	}

	if cfg.LockRefreshSleepMs != 50 {
		t.Errorf("LockRefreshSleepMs should still be 50, got %d", cfg.LockRefreshSleepMs)
	}
}

// TestCacheConfigCopy 测试配置的复制
func TestCacheConfigCopy(t *testing.T) {
	// 创建原始配置
	original := DefaultCacheConfig()

	// 复制配置
	copy := original

	// 修改副本
	copy.DefaultExpireTime = 10 * time.Minute
	copy.MaxBatchSize = 500

	// 验证原始配置未被修改
	if original.DefaultExpireTime != 30*time.Minute {
		t.Errorf("Original DefaultExpireTime should still be 30 minutes, got %v", original.DefaultExpireTime)
	}

	if original.MaxBatchSize != 1000 {
		t.Errorf("Original MaxBatchSize should still be 1000, got %d", original.MaxBatchSize)
	}

	// 验证副本已修改
	if copy.DefaultExpireTime != 10*time.Minute {
		t.Errorf("Copy DefaultExpireTime should be 10 minutes, got %v", copy.DefaultExpireTime)
	}

	if copy.MaxBatchSize != 500 {
		t.Errorf("Copy MaxBatchSize should be 500, got %d", copy.MaxBatchSize)
	}
}

// TestCacheConfigEdgeCases 测试边界情况
func TestCacheConfigEdgeCases(t *testing.T) {
	t.Run("零过期时间", func(t *testing.T) {
		cfg := CacheConfig{
			DefaultExpireTime: 0,
		}

		if cfg.DefaultExpireTime != 0 {
			t.Errorf("DefaultExpireTime should be 0, got %v", cfg.DefaultExpireTime)
		}
	})

	t.Run("最大过期时间", func(t *testing.T) {
		cfg := CacheConfig{
			DefaultExpireTime: time.Duration(1<<63 - 1),
		}

		if cfg.DefaultExpireTime != time.Duration(1<<63-1) {
			t.Errorf("DefaultExpireTime should be max duration, got %v", cfg.DefaultExpireTime)
		}
	})

	t.Run("负数过期时间", func(t *testing.T) {
		cfg := CacheConfig{
			DefaultExpireTime: -1 * time.Minute,
		}

		if cfg.DefaultExpireTime != -1*time.Minute {
			t.Errorf("DefaultExpireTime should be -1 minute, got %v", cfg.DefaultExpireTime)
		}
	})

	t.Run("空占位符值", func(t *testing.T) {
		cfg := CacheConfig{
			PlaceholderValue: "",
		}

		if cfg.PlaceholderValue != "" {
			t.Errorf("PlaceholderValue should be empty, got '%s'", cfg.PlaceholderValue)
		}
	})

	t.Run("特殊字符占位符值", func(t *testing.T) {
		cfg := CacheConfig{
			PlaceholderValue: "!@#$%^&*()",
		}

		if cfg.PlaceholderValue != "!@#$%^&*()" {
			t.Errorf("PlaceholderValue should be '!@#$%%^&*()', got '%s'", cfg.PlaceholderValue)
		}
	})
}

// TestCacheConfigComparison 测试配置比较
func TestCacheConfigComparison(t *testing.T) {
	cfg1 := DefaultCacheConfig()
	cfg2 := DefaultCacheConfig()

	// 验证两个默认配置相等
	if cfg1.DefaultExpireTime != cfg2.DefaultExpireTime {
		t.Error("DefaultExpireTime should be equal")
	}

	if cfg1.DefaultNotFoundExpireTime != cfg2.DefaultNotFoundExpireTime {
		t.Error("DefaultNotFoundExpireTime should be equal")
	}

	if cfg1.LockRefreshSleepMs != cfg2.LockRefreshSleepMs {
		t.Error("LockRefreshSleepMs should be equal")
	}

	if cfg1.DelayedDeleteInterval != cfg2.DelayedDeleteInterval {
		t.Error("DelayedDeleteInterval should be equal")
	}

	if cfg1.MaxCacheableRecords != cfg2.MaxCacheableRecords {
		t.Error("MaxCacheableRecords should be equal")
	}

	if cfg1.MaxCacheableIDs != cfg2.MaxCacheableIDs {
		t.Error("MaxCacheableIDs should be equal")
	}

	if cfg1.MaxBatchSize != cfg2.MaxBatchSize {
		t.Error("MaxBatchSize should be equal")
	}

	if cfg1.PlaceholderValue != cfg2.PlaceholderValue {
		t.Error("PlaceholderValue should be equal")
	}
}
