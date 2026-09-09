// Package dao 提供泛型数据访问层基础组件
package dao

// ==================== 第一组：缓存键前缀（用于 Redis 实际存储） ====================
// 这组前缀会拼接在具体的 Key 前面，用来区分缓存数据的“业务类型”。
// 例如：缓存一个条件查询的结果，Key 就是 "condition:" + MD5(查询条件)。
// 为什么要加前缀？为了在 Redis 里能按类型批量删除（通过 DelByPrefix）。

const (
	// CacheKeyPrefixCondition 条件查询缓存前缀
	// 适用场景：GetByCondition 和 GetOneByColumns，缓存的是“条件 -> ID”的映射。
	// 例如："condition:abc123" -> 对应的 ID 或 ID 列表。
	CacheKeyPrefixCondition = "condition:"

	// CacheKeyPrefixColumns 分页查询缓存前缀
	// 适用场景：GetByColumns（分页查询），缓存的是“分页参数 -> 当前页的 ID 列表”。
	// 例如："columns:def456:total" 存总数，"columns:def456:ids" 存当前页的 ID 列表。
	CacheKeyPrefixColumns = "columns:"

	// CacheKeyPrefixExists 存在性查询缓存前缀
	// 适用场景：ExistsByCondition，缓存的是“条件 -> 是否存在（0或1）”。
	// 例如："exists:ghi789" -> 1（存在）或 0（不存在）。
	CacheKeyPrefixExists = "exists:"

	// CacheKeyPrefixCount 计数查询缓存前缀
	// 适用场景：CountByCondition，缓存的是“条件 -> 总条数”。
	// 例如："count:jkl012" -> 1234（总记录数）。
	CacheKeyPrefixCount = "count:"
)

// ==================== 第二组：Singleflight 去重键前缀（内存中合并请求） ====================
// 这组前缀用在 singleflight.Group 的 Do() 方法里。
// 注意：这些 Key 只存在于当前进程的内存中，不会写入 Redis。
// 目的是把“同一个 key 的并发请求”合并成一个，让数据库只被查一次。

const (
	// SFKeyPrefixOneCondition 条件单条查询的 singleflight 去重键
	// 对应 GetOneByColumns，防止 1000 个并发请求同时去查数据库里同一条数据。
	SFKeyPrefixOneCondition = "one_condition:"

	// SFKeyPrefixIDsCondition 条件 ID 列表查询的 singleflight 去重键
	// 对应 GetByCondition，防止同一个条件被同时查多次。
	SFKeyPrefixIDsCondition = "ids_condition:"

	// SFKeyPrefixBatchIDs 批量 ID 查询的 singleflight 去重键
	// 对应 GetByIDs，例如传入 [1,2,3,4,5]，如果同时有 10 个请求都查这批 ID，
	// 只放行 1 个去数据库，其他 9 个等待结果。
	SFKeyPrefixBatchIDs = "batch_ids:"

	// SFKeyPrefixColumns 分页查询的 singleflight 去重键
	// 对应 GetByColumns，防止同一个分页条件被同时计算 total 和查列表。
	SFKeyPrefixColumns = "columns:"
)

// ==================== 第三组：分布式锁键前缀（跨进程互斥） ====================
// 这组前缀用在 redsync 分布式锁里。
// 和 singleflight（单机内存去重）不同，分布式锁是跨服务器生效的，
// 目的是防止多个服务器节点同时去查数据库。

const (
	// LockKeyPrefixRefresh 单条记录刷新锁的前缀
	// 适用场景：GetByID 缓存失效时，抢到这个锁的节点去查数据库回填缓存。
	// 完整 Key 示例："lock:refresh:123"（锁住 ID=123 的记录）。
	LockKeyPrefixRefresh = "lock:refresh"

	// LockKeyPrefixRefreshBatch 批量刷新锁的前缀
	// 适用场景：GetByIDs 缓存失效时，抢到这个锁的节点去查数据库批量回填。
	// 完整 Key 示例："lock:refresh:batch:md5([1,2,3])"（锁住这批 ID）。
	LockKeyPrefixRefreshBatch = "lock:refresh:batch"
)

// ==================== 第四组：随机偏移量范围（防缓存雪崩） ====================
// 这两个常量用在 GetRandomExpireTime 函数里。
// 作用是把固定的过期时间（如 30 分钟）打散，避免所有缓存同时失效。

const (
	// RandomOffsetMaxSeconds 随机偏移的最大值（600 秒 = 10 分钟）
	// 注意：这个值是 Int63n 的参数，生成的随机数是 [0, 600) 区间。
	RandomOffsetMaxSeconds = 600

	// RandomOffsetMinSeconds 随机偏移的基数（300 秒 = 5 分钟）
	// 实际偏移量 = Int63n(600) - 300，结果范围：[-300, 299] 秒（即 ±5 分钟）。
	// 所以实际过期时间 = 基础时间 + ( -5分钟 ~ +5分钟 )。
	RandomOffsetMinSeconds = 300
)

// ==================== 第五组：缓存删除类型（清理范围控制） ====================
// 这组常量用在 deleteCache 和 delayedDoubleDelete 方法里。
// 当数据发生变更时，需要告诉缓存管理器“要删哪些种类的缓存”。

const (
	// DeleteDaoTypeSingle 删除单个 ID 的缓存
	// 适用场景：更新或删除了一条 ID=123 的记录，只需删除 Redis 里 key 为 "123" 的那条缓存。
	DeleteDaoTypeSingle = "single"

	// DeleteDaoTypeCondition 删除所有条件查询相关的缓存
	// 适用场景：新增或更新了数据后，所有“条件查询”、“分页查询”、“计数”、“存在性”的缓存都可能失效。
	// 例如：新增了一个用户后，之前缓存的“状态为1的用户列表”就过期了，需要批量删除 condition/columns/count/exists 前缀下的所有 Key。
	DeleteDaoTypeCondition = "condition"

	// DeleteDaoTypeAll 删除该表的所有缓存（最彻底）
	// 适用场景：批量导入数据、表结构变更等大规模操作。
	// 注意：会删除该表前缀下的所有缓存，包括单个 ID 缓存，非常“重”，要谨慎使用。
	DeleteDaoTypeAll = "all"
)

// ==================== 第六组：分页查询相关常量 ====================

// SortIgnoreCount 排序忽略统计常量
// 适用场景：GetByColumns 分页查询时，如果传了 Sort = "ignore count"，
// 框架会跳过 SELECT COUNT(*) 这一步，只查当前页的数据。
// 为什么要这么设计？有些前端场景（比如“下拉加载更多”）只需要返回数据列表，不需要知道总条数。
// 跳过 COUNT 可以大幅提升查询性能（尤其是在数据量巨大时）。
//
// 注意：此常量与 query.SortIgnoreCount 相同，用于兼容现有代码。
// 新代码建议直接使用 query.SortIgnoreCount。
const SortIgnoreCount = "ignore count"
