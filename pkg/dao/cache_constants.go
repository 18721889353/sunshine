package dao

// 缓存键前缀常量（按业务类型划分）
const (
	CacheKeyPrefixCondition = "condition:"
	CacheKeyPrefixColumns   = "columns:"
	CacheKeyPrefixExists    = "exists:"
	CacheKeyPrefixCount     = "count:"
)

// 内部键前缀（用于去重）
const (
	SFKeyPrefixOneCondition = "one_condition:"
	SFKeyPrefixIDsCondition = "ids_condition:"
	SFKeyPrefixBatchIDs     = "batch_ids:"
	SFKeyPrefixColumns      = "columns:"
)

// 分布式锁键前缀

const (
	LockKeyPrefixRefresh      = "lock:refresh"
	LockKeyPrefixRefreshBatch = "lock:refresh:batch" // 新增
)

// 随机偏移范围（秒）
const (
	RandomOffsetMaxSeconds = 600 // ±300秒，即5分钟
	RandomOffsetMinSeconds = 300
)

// DAO 层缓存删除类型常量
const (
	DeleteDaoTypeSingle = "single" // DeleteDaoTypeSingle 删除单个 ID 缓存
	// DeleteDaoTypeCondition 删除条件查询缓存（包括 condition、columns、count、exists）
	DeleteDaoTypeCondition = "condition"
	// DeleteDaoTypeAll 删除所有缓存（最彻底，谨慎使用）
	DeleteDaoTypeAll = "all"
)

// SortIgnoreCount 排序忽略统计常量
const SortIgnoreCount = "ignore count"
