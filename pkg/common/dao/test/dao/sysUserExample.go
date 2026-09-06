// Package dao 数据访问层
package dao

import (
	"context"
	"time"

	"github.com/go-redsync/redsync/v4" // 分布式锁选项（适配器需要）
	"gorm.io/gorm"

	"github.com/18721889353/sunshine/pkg/common/dao"
	"github.com/18721889353/sunshine/pkg/common/dao/test/cache"
	"github.com/18721889353/sunshine/pkg/common/dao/test/model"
	"github.com/18721889353/sunshine/pkg/sgorm/query"
)

// ----- 查询选项（直接复用公共包） -----

// SysUserExampleQueryOption 查询选项类型别名，复用公共包的 QueryOption
type SysUserExampleQueryOption = dao.QueryOption

// SysUserExampleWithForceMaster 强制使用主库查询
func SysUserExampleWithForceMaster() SysUserExampleQueryOption {
	return dao.WithForceMaster()
}

// SysUserExampleWithForceSlave 强制使用从库查询（显式声明走从库）
func SysUserExampleWithForceSlave() SysUserExampleQueryOption {
	return dao.WithForceSlave()
}

// SysUserExampleWithUnscoped 忽略软删除
func SysUserExampleWithUnscoped() SysUserExampleQueryOption {
	return dao.WithUnscoped()
}

// SysUserExampleDao 数据访问接口
type SysUserExampleDao interface {
	Create(ctx context.Context, table *model.SysUserExample) error
	CreateInBatches(ctx context.Context, tables []*model.SysUserExample, batchSize int) error
	CreateByTx(ctx context.Context, tx *gorm.DB, table *model.SysUserExample) (uint64, error)
	CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, tables []*model.SysUserExample, batchSize int) error

	DeleteByID(ctx context.Context, id uint64) error
	DeleteByIDs(ctx context.Context, ids []uint64) error
	DeleteByCondition(ctx context.Context, c *query.Conditions) error
	DeleteByTx(ctx context.Context, tx *gorm.DB, id uint64) error
	DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) error
	DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error
	ClearCache(ctx context.Context) error

	UpdateByID(ctx context.Context, table *model.SysUserExample) error
	UpdateByCondition(ctx context.Context, c *query.Conditions, updates *model.SysUserExample) error
	UpdateByTx(ctx context.Context, tx *gorm.DB, table *model.SysUserExample) error
	UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, updates *model.SysUserExample) error
	ExecByCustomFunc(ctx context.Context, updateFunc func(*gorm.DB) *gorm.DB) error

	GetByID(ctx context.Context, id uint64, opts ...SysUserExampleQueryOption) (*model.SysUserExample, error)
	GetByColumns(ctx context.Context, params *query.Params, opts ...SysUserExampleQueryOption) ([]*model.SysUserExample, int64, error)
	GetOneByColumns(ctx context.Context, params *query.Params, opts ...SysUserExampleQueryOption) (*model.SysUserExample, error)
	GetByCondition(ctx context.Context, c *query.Conditions, opts ...SysUserExampleQueryOption) ([]uint64, error)
	GetByIDs(ctx context.Context, ids []uint64, opts ...SysUserExampleQueryOption) (map[uint64]*model.SysUserExample, error)
	CountByCondition(ctx context.Context, c *query.Conditions, opts ...SysUserExampleQueryOption) (int64, error)
	ExistsByCondition(ctx context.Context, c *query.Conditions, opts ...SysUserExampleQueryOption) (bool, error)
	GetByCustomQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, result interface{}, page int, limit int, opts ...SysUserExampleQueryOption) (int64, error)
}

// ----- 缓存适配器：将业务缓存接口适配为 dao.Cache[model.SysUserExample] -----
type sysUserExampleCacheAdapter struct {
	cache cache.SysUserExampleCache
}

func (a *sysUserExampleCacheAdapter) Get(ctx context.Context, id uint64) (*model.SysUserExample, error) {
	return a.cache.Get(ctx, id)
}
func (a *sysUserExampleCacheAdapter) Set(ctx context.Context, id uint64, val *model.SysUserExample, dur time.Duration) error {
	return a.cache.Set(ctx, id, val, dur)
}
func (a *sysUserExampleCacheAdapter) MultiGet(ctx context.Context, ids []uint64) (map[uint64]*model.SysUserExample, error) {
	return a.cache.MultiGet(ctx, ids)
}
func (a *sysUserExampleCacheAdapter) MultiSet(ctx context.Context, vals []*model.SysUserExample, dur time.Duration) error {
	return a.cache.MultiSet(ctx, vals, dur)
}
func (a *sysUserExampleCacheAdapter) Del(ctx context.Context, id uint64) error {
	return a.cache.Del(ctx, id)
}
func (a *sysUserExampleCacheAdapter) DelByPrefix(ctx context.Context, prefix string) error {
	return a.cache.DelByPrefix(ctx, prefix)
}
func (a *sysUserExampleCacheAdapter) DelByKey(ctx context.Context, key string) error {
	return a.cache.DelByKey(ctx, key)
}
func (a *sysUserExampleCacheAdapter) SetPlaceholder(ctx context.Context, id uint64) error {
	return a.cache.SetPlaceholder(ctx, id)
}
func (a *sysUserExampleCacheAdapter) SetPlaceholderByKey(ctx context.Context, key string) error {
	return a.cache.SetPlaceholderByKey(ctx, key)
}
func (a *sysUserExampleCacheAdapter) IsPlaceholderErr(err error) bool {
	return a.cache.IsPlaceholderErr(err)
}
func (a *sysUserExampleCacheAdapter) GetIDByKey(ctx context.Context, key string) (uint64, error) {
	return a.cache.GetIDByKey(ctx, key)
}
func (a *sysUserExampleCacheAdapter) SetIDByKey(ctx context.Context, key string, id uint64, dur time.Duration) error {
	return a.cache.SetIDByKey(ctx, key, id, dur)
}
func (a *sysUserExampleCacheAdapter) GetIDsByKey(ctx context.Context, key string) ([]uint64, error) {
	return a.cache.GetIDsByKey(ctx, key)
}
func (a *sysUserExampleCacheAdapter) SetIDsByKey(ctx context.Context, key string, ids []uint64, dur time.Duration) error {
	return a.cache.SetIDsByKey(ctx, key, ids, dur)
}
func (a *sysUserExampleCacheAdapter) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	return a.cache.GetLoopLock(ctx, key, options...)
}
func (a *sysUserExampleCacheAdapter) GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	return a.cache.GetLock(ctx, key, options...)
}
func (a *sysUserExampleCacheAdapter) WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	return a.cache.WatchDogLock(ctx, key, expiry, task, options...)
}
func (a *sysUserExampleCacheAdapter) WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	return a.cache.WatchDogLoopLock(ctx, key, expiry, task, options...)
}

// ----- 用户配置选项（Functional Options）-----

// SysUserExampleDaoOption 定义 DAO 配置选项函数
type SysUserExampleDaoOption func(*sysUserExampleDaoOptions)

// sysUserExampleDaoOptions 内部配置结构
type sysUserExampleDaoOptions struct {
	cacheConfig  *dao.CacheConfig // 自定义缓存配置，nil 表示使用默认
	disableCache bool             // 是否禁用缓存
}

// SysUserExampleWithCacheConfig 传入自定义缓存配置
// 参数：cfg - 自定义的缓存配置结构体
// 用法：dao.NewSysUserExampleDao(db, cache, dao.WithCacheConfig(dao.CacheConfig{DefaultExpireTime: 10 * time.Minute}))
func SysUserExampleWithCacheConfig(cfg dao.CacheConfig) SysUserExampleDaoOption {
	return func(o *sysUserExampleDaoOptions) {
		o.cacheConfig = &cfg
	}
}

// SysUserExampleWithNoCache 禁用缓存（适用于测试或临时场景）
// 用法：dao.NewSysUserExampleDao(db, cache, dao.WithNoCache())
func SysUserExampleWithNoCache() SysUserExampleDaoOption {
	return func(o *sysUserExampleDaoOptions) {
		o.disableCache = true
	}
}

// SysUserExampleWithLongCache 使用长时间缓存（2 小时），适用于低频变更数据（如字典表、配置表）
// 用法：dao.NewSysUserExampleDao(db, cache, dao.WithLongCache())
func SysUserExampleWithLongCache() SysUserExampleDaoOption {
	return func(o *sysUserExampleDaoOptions) {
		o.cacheConfig = &dao.CacheConfig{
			DefaultExpireTime: 2 * time.Hour,
		}
	}
}

// SysUserExampleWithShortCache 使用短时间缓存（5 分钟），适用于高频变更数据（如实时状态、库存）
func SysUserExampleWithShortCache() SysUserExampleDaoOption {
	return func(o *sysUserExampleDaoOptions) {
		o.cacheConfig = &dao.CacheConfig{
			DefaultExpireTime: 5 * time.Minute,
		}
	}
}

// SysUserExampleWithCustomCache 使用自定义缓存配置
func SysUserExampleWithCustomCache(cfg dao.CacheConfig) SysUserExampleDaoOption {
	return SysUserExampleWithCacheConfig(cfg)
}

// ----- sysUserExampleDao 结构体（嵌入 BaseDao）-----
type sysUserExampleDao struct {
	*dao.BaseDao[model.SysUserExample]
}

// NewSysUserExampleDao 创建 SysUserExampleDao 实例
// 参数：
//   - db: GORM 数据库连接
//   - xCache: 业务缓存接口（若为 nil 且未显式禁用，则自动禁用缓存）
//   - opts: 可变配置选项（Functional Options），支持 WithCacheConfig、WithNoCache、WithLongCache、WithShortCache 等
//
// 返回：
//   - SysUserExampleDao 实例
//
// 示例：
//
//	// 默认配置（30 分钟缓存）
//	dao := NewSysUserExampleDao(db, userCache)
//
//	// 自定义配置（10 分钟缓存，且分页缓存上限 500）
//	dao := NewSysUserExampleDao(db, userCache, WithCacheConfig(dao.CacheConfig{
//	    DefaultExpireTime: 10 * time.Minute,
//	    MaxCacheableRecords: 500,
//	}))
//
//	// 长时缓存（2 小时）
//	dao := NewSysUserExampleDao(db, userCache, WithLongCache())
//
//	// 短时缓存（5 分钟）
//	dao := NewSysUserExampleDao(db, userCache, WithShortCache())
//
//	// 禁用缓存
//	dao := NewSysUserExampleDao(db, userCache, WithNoCache())
//	// 或直接传 nil 缓存对象也可禁用，但 WithNoCache 更明确
func NewSysUserExampleDao(
	db *gorm.DB,
	xCache cache.SysUserExampleCache,
	opts ...SysUserExampleDaoOption,
) SysUserExampleDao {
	// 1. 初始化选项
	options := &sysUserExampleDaoOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// 2. 决定缓存适配器
	var cacheAdapter dao.Cache[model.SysUserExample]
	if !options.disableCache && xCache != nil {
		cacheAdapter = &sysUserExampleCacheAdapter{cache: xCache}
	}
	// 如果 options.disableCache 为 true，即使 xCache 不为 nil，也不使用缓存

	// 3. 构建更新字段映射函数
	// 注意：此处必须包含模型的所有可更新字段，且根据业务需求决定零值是否更新
	// 对于时间类型，只更新非零值；对于数字类型，若 0 有意义则需调整判断条件
	updateBuilder := func(table *model.SysUserExample) map[string]interface{} {
		if table == nil || table.ID < 1 {
			return nil
		}
		update := map[string]interface{}{}

		if table.Name != "" {
			update["name"] = table.Name
		}
		if table.Password != "" {
			update["password"] = table.Password
		}
		if table.Email != "" {
			update["email"] = table.Email
		}
		if table.Phone != "" {
			update["phone"] = table.Phone
		}
		if table.Avatar != "" {
			update["avatar"] = table.Avatar
		}
		if table.Age != nil {
			update["age"] = *table.Age
		}
		if table.Gender != nil {
			update["gender"] = *table.Gender
		}
		if table.Status != nil {
			update["status"] = *table.Status
		}
		if table.LoginAt != nil {
			update["login_at"] = *table.LoginAt
		}
		// 生成器会根据表结构自动填充字段映射逻辑
		// 示例：
		// if table.Name != "" { update["name"] = table.Name }
		// if table.Age != 0 { update["age"] = table.Age }
		// if !table.UpdatedAt.IsZero() { update["updated_at"] = table.UpdatedAt }

		return update
	}

	// 4. ID 提取器
	idExtractor := func(e *model.SysUserExample) uint64 {
		if e == nil {
			return 0
		}
		return e.ID
	}

	// 5. 获取表名
	var entity model.SysUserExample
	tableName := entity.TableName()

	// 6. 确定最终缓存配置
	var finalConfig dao.CacheConfig
	if options.cacheConfig != nil {
		finalConfig = *options.cacheConfig // 使用用户传入的配置
	} else {
		finalConfig = dao.DefaultCacheConfig() // 使用默认配置
	}
	// 注：如果 cacheAdapter == nil（即无缓存），BaseDao 会忽略 config，但不影响

	// 7. 创建 BaseDao
	base := dao.NewBaseDao(
		db,
		cacheAdapter,
		tableName,
		updateBuilder,
		idExtractor,
		finalConfig, // 传入配置（仅当 cacheAdapter 非空时生效）
	)

	return &sysUserExampleDao{BaseDao: base}
}
