// Package dao 提供泛型数据访问层基础组件
package dao

import "time"

// CacheConfig 缓存配置
// 所有字段均有默认值，用户只需传入需要修改的字段即可
type CacheConfig struct {
	// DefaultExpireTime 正常缓存过期时间，默认 30 分钟
	DefaultExpireTime time.Duration
	// DefaultNotFoundExpireTime 占位符缓存过期时间，默认 1 分钟
	DefaultNotFoundExpireTime time.Duration
	// LockRefreshSleepMs 锁获取失败后的等待毫秒数，默认 50ms
	LockRefreshSleepMs int
	// DelayedDeleteInterval 延迟双删间隔，默认 100ms
	DelayedDeleteInterval time.Duration
	// MaxCacheableRecords 分页查询最大缓存记录数，默认 1000
	MaxCacheableRecords int
	// MaxCacheableIDs 条件查询最大缓存 ID 数，默认 10000
	MaxCacheableIDs int
	// MaxBatchSize 批量操作批次大小，默认 1000
	MaxBatchSize int
	// PlaceholderValue 占位符值，默认 "*"
	PlaceholderValue string
}

// DefaultCacheConfig 返回默认配置
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		DefaultExpireTime:         30 * time.Minute,
		DefaultNotFoundExpireTime: 1 * time.Minute,
		LockRefreshSleepMs:        50,
		DelayedDeleteInterval:     100 * time.Millisecond,
		MaxCacheableRecords:       1000,
		MaxCacheableIDs:           10000,
		MaxBatchSize:              1000,
		PlaceholderValue:          "*",
	}
}
