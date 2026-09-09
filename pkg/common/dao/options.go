// Package dao 提供泛型数据访问层基础组件
package dao

import (
	"reflect"
	"time"

	"gorm.io/gorm"
)

// ============================================================================
// 第一部分：查询选项（Query Options）
// 用于查询时的配置，如强制主库、忽略软删除等
// ============================================================================

// QueryOptions 定义了本次查询的"配置单"
// ForceMaster: true 表示强制走主库，false 表示走默认路由（通常是从库）
// Unscoped:    true 表示忽略软删除（查出已删数据），false 表示只查未删的
type QueryOptions struct {
	ForceMaster bool
	Unscoped    bool
}

// QueryOption 是一个函数类型，它接收配置单指针，可以在里面修改任意字段
// 它的作用是：把"怎么改"这个动作封装成一个变量，方便传递和组合
type QueryOption func(*QueryOptions)

// WithForceMaster 返回一个"把手"：把配置单的 ForceMaster 设置为 true
func WithForceMaster() QueryOption {
	return func(o *QueryOptions) {
		o.ForceMaster = true
	}
}

// WithForceSlave 返回一个"把手"：把配置单的 ForceMaster 设置为 false
// 注意：这里只是关闭"强制主库"，实际走主还是从由 GORM 的 dbresolver 决定
func WithForceSlave() QueryOption {
	return func(o *QueryOptions) {
		o.ForceMaster = false
	}
}

// WithUnscoped 返回一个"把手"：把配置单的 Unscoped 设置为 true
// 启用后，GORM 的查询将不会自动添加 deleted_at IS NULL 条件
func WithUnscoped() QueryOption {
	return func(o *QueryOptions) {
		o.Unscoped = true
	}
}

// DefaultQueryOptions 返回默认的查询配置
// 默认：走主库（确保刚写入的数据能读到），不查已删数据
func DefaultQueryOptions() *QueryOptions {
	return &QueryOptions{
		ForceMaster: true,
		Unscoped:    false,
	}
}

// ApplyOptions 是"配置车间"：先造一份默认配置单，然后用传进来的所有把手（opts）逐一修改它
// 参数 opts ...QueryOption 是可变参数，调用方可以传 0 个或多个选项函数
// 返回值是一个最终定稿的配置单指针
func ApplyOptions(opts ...QueryOption) *QueryOptions {
	o := DefaultQueryOptions() // 直接调用，避免硬编码
	// 遍历所有选项函数，依次执行，每次传入配置单指针供修改
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// ============================================================================
// 第二部分：DAO 创建选项（DAO Options）
// 用于创建 DAO 实例时的配置，如缓存配置、禁用缓存等
// ============================================================================

// Option 泛型 DAO 配置选项函数
// 用于配置 BaseDao 的行为，如缓存配置、禁用缓存等
type Option[T any] func(*Config[T])

// Config 泛型 DAO 内部配置结构
type Config[T any] struct {
	// cacheConfig 缓存配置，nil 表示使用默认配置
	cacheConfig *CacheConfig

	// disableCache 是否禁用缓存
	disableCache bool
}

// WithCacheConfig 传入自定义缓存配置
// 参数：cfg - 自定义的缓存配置结构体
// 用法：dao.NewDao(db, cache, tableName, updateBuilder, dao.WithCacheConfig(dao.CacheConfig{DefaultExpireTime: 10 * time.Minute}))
func WithCacheConfig[T any](cfg CacheConfig) Option[T] {
	return func(o *Config[T]) {
		o.cacheConfig = &cfg
	}
}

// WithNoCache 禁用缓存（适用于测试或临时场景）
// 用法：dao.NewDao(db, cache, tableName, updateBuilder, dao.WithNoCache[T]())
func WithNoCache[T any]() Option[T] {
	return func(o *Config[T]) {
		o.disableCache = true
	}
}

// WithLongCache 使用长时间缓存（2 小时），适用于低频变更数据（如字典表、配置表）
// 用法：dao.NewDao(db, cache, tableName, updateBuilder, dao.WithLongCache[T]())
func WithLongCache[T any]() Option[T] {
	return func(o *Config[T]) {
		o.cacheConfig = &CacheConfig{
			DefaultExpireTime: 2 * time.Hour,
		}
	}
}

// WithShortCache 使用短时间缓存（5 分钟），适用于高频变更数据（如实时状态、库存）
// 用法：dao.NewDao(db, cache, tableName, updateBuilder, dao.WithShortCache[T]())
func WithShortCache[T any]() Option[T] {
	return func(o *Config[T]) {
		o.cacheConfig = &CacheConfig{
			DefaultExpireTime: 5 * time.Minute,
		}
	}
}

// ============================================================================
// 第三部分：泛型 DAO 创建函数
// ============================================================================

// NewDao 创建泛型 DAO 实例（简化版）
// 相比 NewBaseDao，这个函数自动处理 ID 提取器，简化调用方式
//
// 参数：
//   - db: GORM 数据库连接（必需）
//   - cache: 缓存适配器，实现了 Cache[T] 接口。若传 nil，则禁用缓存
//   - tableName: 数据库表名（字符串），如 "users"
//   - primaryKey: 主键列名（字符串），如 "id"、"user_id"。若传空字符串，默认为 "id"
//   - updateBuilder: 更新字段构建函数，将实体中需要更新的字段转为 map，零值字段会被忽略
//   - opts: 可变配置选项（Functional Options）
//
// 返回：
//   - *BaseDao[T] 实例
//
// 示例：
//
//	// 基础用法（使用默认主键 "id"）
//	userDao := dao.NewDao(db, userCache, "users", "", func(u *User) map[string]interface{} {
//	    update := map[string]interface{}{}
//	    if u.Name != "" { update["name"] = u.Name }
//	    return update
//	})
//
//	// 自定义主键（如 user_id）
//	userDao := dao.NewDao(db, userCache, "users", "user_id", updateBuilder)
//
//	// 禁用缓存
//	userDao := dao.NewDao(db, nil, "users", "id", updateBuilder, dao.WithNoCache[User]())
func NewDao[T any](
	db *gorm.DB,
	cache Cache[T],
	tableName string,
	primaryKey string,
	updateBuilder func(*T) map[string]interface{},
	opts ...Option[T],
) *BaseDao[T] {
	// 1. 创建默认配置
	config := &Config[T]{}

	// 2. 应用所有选项
	for _, opt := range opts {
		opt(config)
	}

	// 3. 创建 ID 提取器（自动从实体中提取 ID）
	idExtractor := defaultIDExtractor[T]()

	// 4. 确定最终的缓存配置
	var finalConfig *CacheConfig
	if config.cacheConfig != nil {
		finalConfig = config.cacheConfig
	}

	// 5. 如果禁用缓存，传入 nil
	var cacheDriver Cache[T]
	if !config.disableCache {
		cacheDriver = cache
	}

	// 6. 调用 NewBaseDao 创建实例
	return NewBaseDao[T](
		db,
		cacheDriver,
		tableName,
		primaryKey,
		updateBuilder,
		idExtractor,
		getCacheConfigValue(finalConfig),
	)
}

// getCacheConfigValue 将指针转换为值（如果为 nil 则返回零值）
func getCacheConfigValue(cfg *CacheConfig) CacheConfig {
	if cfg == nil {
		return CacheConfig{}
	}
	return *cfg
}

// defaultIDExtractor 创建默认的 ID 提取器
// 自动从实体的 ID 字段提取 uint64 类型的 ID
func defaultIDExtractor[T any]() func(*T) uint64 {
	return func(entity *T) uint64 {
		if entity == nil {
			return 0
		}
		v := reflect.ValueOf(entity).Elem()
		field := v.FieldByName("ID")
		if !field.IsValid() {
			return 0
		}
		if field.Kind() == reflect.Uint64 {
			return field.Uint()
		}
		return 0
	}
}
