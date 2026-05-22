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

// MQ日志状态常量 (cp_mq_log.status)
const (
	// MqLogStatusSuccess 成功
	MqLogStatusSuccess = 1
	// MqLogStatusFailed 失败
	MqLogStatusFailed = 2
)

// 分布式锁配置常量 (redsync/InnoDB通用配置)
const (
	// LockExpirySeconds Redis分布式锁过期时间(秒) (redsync.WithExpiry)
	LockExpirySeconds = 6
	// LockRetryDelayMs Redis分布式锁重试延迟(毫秒) (redsync.WithRetryDelay)
	LockRetryDelayMs = 25
	// LockMaxTries Redis分布式锁最大尝试次数 (redsync.WithTries)
	LockMaxTries = 400
	// InnoDBLockWaitTimeout MySQL InnoDB锁等待超时时间(秒) (SET SESSION innodb_lock_wait_timeout)
	InnoDBLockWaitTimeout = 5
)

// MQ业务类型常量 (cp_mq_log.bus_type / rabbitmq routing_key)
const (
	// BusMQLogStatusSuccess MQ日志成功状态标识
	BusMQLogStatusSuccess = "success"
)

// MQ消费者通用常量 (rabbitmq/consumers业务逻辑配置)
const (
	// MqUnknownOrderSn JSON解析失败时的默认订单号 (日志追踪用)
	MqUnknownOrderSn = "unknown"
	// MqMaxRetryErrorNum MQ最大重试错误次数 (cp_mq_log.error_num统计阈值)
	MqMaxRetryErrorNum = 5
)
