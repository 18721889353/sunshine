// Package consts 定义项目通用常量
package consts

import "time"

// 缓存类型常量
const (
	// CacheTypeRedis Redis 缓存类型
	CacheTypeRedis = "redis"
)

// DAO 层通用常量（所有表共享）
const (
	// DaoMaxCacheableIDs 最大可缓存的 ID 数量，超过此值则不缓存 ID 列表，直接查库
	DaoMaxCacheableIDs = 10000

	// DaoMaxBatchSize 批量操作的最大批次大小，超过此值则分批处理
	DaoMaxBatchSize = 1000

	// DaoMaxCacheableRecords 最大可缓存的记录数，超过此值则不缓存或仅警告
	DaoMaxCacheableRecords = 1000

	// DaoDelayedDeleteInterval 延迟双删的等待时间，用于确保主从同步完成
	// 默认值：100ms，可通过配置文件 database.cache{TableName}DelayedDeleteInterval 覆盖
	DaoDelayedDeleteInterval = 100 * time.Millisecond
)

// DAO 层缓存删除类型常量
const (
	// DaoDeleteTypeSingle 删除单个 ID 缓存
	DaoDeleteTypeSingle = "single"

	// DaoDeleteTypeCondition 删除条件查询缓存（包括 condition、columns、count、exists）
	DaoDeleteTypeCondition = "condition"

	// DaoDeleteTypeAll 删除所有缓存（最彻底，谨慎使用）
	DaoDeleteTypeAll = "all"
)

// DAO 层排序忽略统计常量
const (
	// DaoSortIgnoreCount 排序时忽略计数统计
	DaoSortIgnoreCount = "ignore count"
)

// DAO 层 SQL 空查询常量
const (
	// DaoEmptyCountSQL 空查询的 COUNT SQL，用于返回 0 计数
	DaoEmptyCountSQL = "SELECT COUNT(*) FROM (SELECT 1) AS count_query WHERE 1=0"
)
