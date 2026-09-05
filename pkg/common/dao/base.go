// Package dao 提供泛型数据访问层基础组件
package dao

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/18721889353/sunshine/pkg/sgorm/query"
)

// BaseDao 泛型数据访问基类
// 封装了数据库 CRUD 操作和缓存管理，所有业务表 DAO 应嵌入此基类以获得通用能力。
// 泛型参数 T 为具体的模型类型（如 model.UserExample）。
//
// 功能特性：
//   - 单条/批量/分页/条件查询（自动缓存）
//   - 创建/更新/删除（自动缓存清理 + 延迟双删）
//   - 分布式锁 + singleflight 防缓存击穿
//   - 占位符防缓存穿透
//   - 随机过期时间防缓存雪崩
//   - 可配置缓存行为（过期时间、批量大小等）
type BaseDao[T any] struct {
	db            *gorm.DB                        // GORM 数据库连接实例
	cacheManager  *cacheManager[T]                // 缓存管理器（nil 表示禁用缓存）
	tableName     string                          // 数据库表名（需与模型映射一致）
	updateBuilder func(*T) map[string]interface{} // 更新字段构建器，将实体转为 map[string]interface{}
	idExtractor   func(*T) uint64                 // ID 提取器，从实体中获取 uint64 类型的 ID
}

// NewBaseDao 创建泛型基类实例
// 参数：
//   - db: GORM 数据库连接（必需）
//   - cache: 缓存适配器，实现了 Cache[T] 接口。若传 nil，则禁用缓存
//   - tableName: 数据库表名（字符串），如 "users"
//   - updateBuilder: 更新字段构建函数，将实体中需要更新的字段转为 map，零值字段会被忽略（避免误更新）
//   - idExtractor: ID 提取函数，从实体中返回 uint64 类型的 ID
//   - config: 可选缓存配置，不传则使用默认配置（DefaultCacheConfig）
//
// 返回：
//   - *BaseDao[T] 实例
//
// 示例：
//
//	base := dao.NewBaseDao(db, cacheAdapter, "users", updateBuilder, idExtractor)
//
// 自定义配置：
//
//	cfg := dao.CacheConfig{DefaultExpireTime: 10 * time.Minute}
//	base := dao.NewBaseDao(db, cacheAdapter, "users", updateBuilder, idExtractor, cfg)
func NewBaseDao[T any](
	db *gorm.DB,
	cache Cache[T],
	tableName string,
	updateBuilder func(*T) map[string]interface{},
	idExtractor func(*T) uint64,
	config ...CacheConfig,
) *BaseDao[T] {
	dao := &BaseDao[T]{
		db:            db,
		tableName:     tableName,
		updateBuilder: updateBuilder,
		idExtractor:   idExtractor,
	}
	if cache != nil {
		cfg := DefaultCacheConfig()
		if len(config) > 0 {
			c := config[0]
			if c.DefaultExpireTime != 0 {
				cfg.DefaultExpireTime = c.DefaultExpireTime
			}
			if c.DefaultNotFoundExpireTime != 0 {
				cfg.DefaultNotFoundExpireTime = c.DefaultNotFoundExpireTime
			}
			if c.LockRefreshSleepMs != 0 {
				cfg.LockRefreshSleepMs = c.LockRefreshSleepMs
			}
			if c.DelayedDeleteInterval != 0 {
				cfg.DelayedDeleteInterval = c.DelayedDeleteInterval
			}
			if c.MaxCacheableRecords != 0 {
				cfg.MaxCacheableRecords = c.MaxCacheableRecords
			}
			if c.MaxCacheableIDs != 0 {
				cfg.MaxCacheableIDs = c.MaxCacheableIDs
			}
			if c.MaxBatchSize != 0 {
				cfg.MaxBatchSize = c.MaxBatchSize
			}
			if c.PlaceholderValue != "" {
				cfg.PlaceholderValue = c.PlaceholderValue
			}
		}
		dao.cacheManager = newCacheManager(cache, cfg, tableName)
	}
	return dao
}

// ---- 查询方法 ----

// GetByID 根据 ID 获取单条记录（支持缓存）
// 参数：
//   - ctx: 上下文
//   - id: 记录 ID (uint64)
//   - opts: 查询选项（如强制主库、忽略软删除）
//
// 返回：
//   - *T: 记录指针，若不存在返回 database.ErrRecordNotFound
//   - error: 执行错误
//
// 缓存行为：
//   - 优先从缓存读取，若命中则直接返回
//   - 未命中则使用 singleflight + 分布式锁查 DB，并回填缓存
//   - 记录不存在时设置短时占位符，防止穿透
//
// 示例：
//
//	user, err := dao.GetByID(ctx, 1)
//	user, err := dao.GetByID(ctx, 1, dao.WithForceMaster()) // 强制主库
func (d *BaseDao[T]) GetByID(ctx context.Context, id uint64, opts ...QueryOption) (*T, error) {
	cfg := ApplyOptions(opts...)
	if d.cacheManager == nil {
		return d.queryDBByID(ctx, id, cfg)
	}
	return d.cacheManager.get(ctx, id, func() (*T, error) {
		return d.queryDBByID(ctx, id, cfg)
	})
}

// queryDBByID 执行数据库查询（内部方法）
// 直接查库，不涉及缓存。由 GetByID 在缓存未命中或禁用时调用。
func (d *BaseDao[T]) queryDBByID(ctx context.Context, id uint64, cfg *QueryOptions) (*T, error) {
	var entity T
	db := d.db.WithContext(ctx).Model(new(T))
	if cfg.ForceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		db = db.Unscoped()
	}
	err := db.Where("id = ?", id).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, database.ErrRecordNotFound
		}
		return nil, err
	}
	return &entity, nil
}

// GetByColumns 根据列条件进行分页查询（支持缓存）
// 参数：
//   - ctx: 上下文
//   - params: 分页查询参数（包含 page, limit, sort, columns 等）
//   - opts: 查询选项
//
// 返回：
//   - []*T: 当前页的记录列表
//   - int64: 总记录数
//   - error: 执行错误
//
// 缓存行为：
//   - 分页结果会按条件+分页+排序+forceMaster+unscoped 组合生成唯一缓存键
//   - 缓存中存储 total 和 ID 列表，完整记录通过批量缓存读取
//   - 当单页记录数超过 MaxCacheableRecords 时，放弃缓存直接查库
//
// 示例：
//
//	params := &query.Params{
//	    Page: 0,
//	    Limit: 20,
//	    Sort: "-id",
//	    Columns: []query.Column{{Name: "status", Value: 1}},
//	}
//	records, total, err := dao.GetByColumns(ctx, params)
func (d *BaseDao[T]) GetByColumns(ctx context.Context, params *query.Params, opts ...QueryOption) ([]*T, int64, error) {
	cfg := ApplyOptions(opts...)
	if d.cacheManager == nil {
		return d.queryByColumnsDB(ctx, params, cfg)
	}

	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, 0, fmt.Errorf("GetByColumns: convert query conditions failed: %w", err)
	}
	// 生成唯一缓存键，包含所有影响查询结果的因素
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%d_%d_%s_%v_%v",
		queryStr, args, params.Page, params.Limit, params.Sort,
		cfg.ForceMaster, cfg.Unscoped)))

	return d.cacheManager.getColumns(ctx,
		func() ([]*T, int64, error) {
			return d.queryByColumnsDB(ctx, params, cfg)
		},
		func(missedIDs []uint64) ([]*T, error) {
			return d.queryByIDsDB(ctx, missedIDs, cfg)
		},
		cacheKey)
}

// queryByColumnsDB 执行分页数据库查询（内部方法）
// 直接查库，不涉及缓存。
func (d *BaseDao[T]) queryByColumnsDB(ctx context.Context, params *query.Params, cfg *QueryOptions) ([]*T, int64, error) {
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, 0, fmt.Errorf("convert conditions failed: %w", err)
	}
	var total int64
	var records []*T
	db := d.db.WithContext(ctx).Model(new(T))
	if cfg.ForceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		db = db.Unscoped()
	}
	// 若 sort 为 "ignore count"，则跳过计数（性能优化）
	if params.Sort != SortIgnoreCount {
		if countErr := db.Where(queryStr, args...).Count(&total).Error; countErr != nil {
			return nil, 0, countErr
		}
		if total == 0 {
			return []*T{}, 0, nil
		}
	}
	order, limit, offset := params.ConvertToPage()
	err = db.Order(order).Limit(limit).Offset(offset).Where(queryStr, args...).Find(&records).Error
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

// GetOneByColumns 根据列条件获取单条记录（支持缓存）
// 参数与 GetByColumns 相同，但只返回第一条匹配记录
// 返回：
//   - *T: 记录指针，若不存在返回 database.ErrRecordNotFound
//   - error: 执行错误
//
// 缓存行为：
//   - 缓存键包含条件、排序、forceMaster、unscoped
//   - 缓存中存储 ID，完整记录通过 GetByID 缓存获取
//
// 示例：
//
//	params := &query.Params{
//	    Sort: "-id",
//	    Columns: []query.Column{{Name: "name", Value: "张三"}},
//	}
//	user, err := dao.GetOneByColumns(ctx, params)
func (d *BaseDao[T]) GetOneByColumns(ctx context.Context, params *query.Params, opts ...QueryOption) (*T, error) {
	cfg := ApplyOptions(opts...)
	if d.cacheManager == nil {
		return d.queryOneByColumnsDB(ctx, params, cfg)
	}

	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, fmt.Errorf("GetOneByColumns: convert query conditions failed: %w", err)
	}
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%s_%v_%v",
		queryStr, args, params.Sort,
		cfg.ForceMaster, cfg.Unscoped)))

	return d.cacheManager.getCondition(ctx, cacheKey, func() (*T, error) {
		return d.queryOneByColumnsDB(ctx, params, cfg)
	})
}

// queryOneByColumnsDB 执行单条数据库查询（内部方法）
func (d *BaseDao[T]) queryOneByColumnsDB(ctx context.Context, params *query.Params, cfg *QueryOptions) (*T, error) {
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, err
	}
	order, _, _ := params.ConvertToPage()
	var entity T
	db := d.db.WithContext(ctx).Model(new(T))
	if cfg.ForceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		db = db.Unscoped()
	}
	err = db.Order(order).Where(queryStr, args...).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, database.ErrRecordNotFound
		}
		return nil, err
	}
	return &entity, nil
}

// GetByCondition 根据条件获取 ID 列表（支持缓存）
// 参数：
//   - c: 查询条件（Conditions）
//   - opts: 查询选项
//
// 返回：
//   - []uint64: 满足条件的 ID 列表
//   - error: 执行错误
//
// 缓存行为：
//   - 缓存整个 ID 列表，当数量超过 MaxCacheableIDs 时放弃缓存
//   - 适合用于获取所有符合条件的 ID，便于后续批量处理
//
// 示例：
//
//	cond := &query.Conditions{
//	    Columns: []query.Column{{Name: "status", Value: 1}},
//	}
//	ids, err := dao.GetByCondition(ctx, cond)
func (d *BaseDao[T]) GetByCondition(ctx context.Context, c *query.Conditions, opts ...QueryOption) ([]uint64, error) {
	cfg := ApplyOptions(opts...)
	if d.cacheManager == nil {
		return d.queryByConditionDB(ctx, c, cfg)
	}
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return nil, fmt.Errorf("GetByCondition: convert conditions failed: %w", err)
	}
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v_%v", queryStr, args, cfg.ForceMaster, cfg.Unscoped)))
	return d.cacheManager.getByCondition(ctx, cacheKey, func() ([]uint64, error) {
		return d.queryByConditionDB(ctx, c, cfg)
	})
}

// queryByConditionDB 执行条件查询（内部方法）
func (d *BaseDao[T]) queryByConditionDB(ctx context.Context, c *query.Conditions, cfg *QueryOptions) ([]uint64, error) {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return nil, err
	}
	var entities []*T
	db := d.db.WithContext(ctx).Model(new(T))
	if cfg.ForceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		db = db.Unscoped()
	}
	if err := db.Where(queryStr, args...).Find(&entities).Error; err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(entities))
	for _, e := range entities {
		if id := d.idExtractor(e); id != 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// GetByIDs 根据 ID 列表批量获取记录（支持缓存）
// 参数：
//   - ids: ID 列表 (uint64)
//   - opts: 查询选项
//
// 返回：
//   - map[uint64]*T: ID -> 记录的映射（只包含成功获取的记录）
//   - error: 执行错误
//
// 缓存行为：
//   - 先尝试从缓存批量读取，未命中的 ID 统一查询 DB 并回填缓存
//   - 自动分批处理（每批 MaxBatchSize），避免内存峰值
//   - 不存在的 ID 会设置占位符，防止穿透
//
// 示例：
//
//	ids := []uint64{1, 2, 3}
//	recordMap, err := dao.GetByIDs(ctx, ids)
func (d *BaseDao[T]) GetByIDs(ctx context.Context, ids []uint64, opts ...QueryOption) (map[uint64]*T, error) {
	cfg := ApplyOptions(opts...)
	if d.cacheManager == nil {
		return d.queryByIDsMapDB(ctx, ids, cfg)
	}
	return d.cacheManager.getByIDs(ctx, ids, func(missedIDs []uint64) ([]*T, error) {
		return d.queryByIDsDB(ctx, missedIDs, cfg)
	})
}

// queryByIDsDB 按 ID 列表查询 DB（返回 []*T）
func (d *BaseDao[T]) queryByIDsDB(ctx context.Context, ids []uint64, cfg *QueryOptions) ([]*T, error) {
	var entities []*T
	db := d.db.WithContext(ctx).Model(new(T))
	if cfg.ForceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		db = db.Unscoped()
	}
	err := db.Where("id IN (?)", ids).Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// queryByIDsMapDB 按 ID 列表查询 DB（返回 map[uint64]*T）
func (d *BaseDao[T]) queryByIDsMapDB(ctx context.Context, ids []uint64, cfg *QueryOptions) (map[uint64]*T, error) {
	entities, err := d.queryByIDsDB(ctx, ids, cfg)
	if err != nil {
		return nil, err
	}
	result := make(map[uint64]*T, len(entities))
	for _, e := range entities {
		if id := d.idExtractor(e); id != 0 {
			result[id] = e
		}
	}
	return result, nil
}

// CountByCondition 根据条件统计记录数（支持缓存）
// 参数：
//   - c: 查询条件
//   - opts: 查询选项
//
// 返回：
//   - int64: 符合条件的记录总数
//   - error: 执行错误
//
// 缓存行为：
//   - 缓存计数结果（包括 0），避免重复统计
//
// 示例：
//
//	cond := &query.Conditions{
//	    Columns: []query.Column{{Name: "status", Value: 1}},
//	}
//	count, err := dao.CountByCondition(ctx, cond)
func (d *BaseDao[T]) CountByCondition(ctx context.Context, c *query.Conditions, opts ...QueryOption) (int64, error) {
	cfg := ApplyOptions(opts...)
	if d.cacheManager == nil {
		return d.countByConditionDB(ctx, c, cfg)
	}
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return 0, err
	}
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v_%v", queryStr, args, cfg.ForceMaster, cfg.Unscoped)))
	return d.cacheManager.getCount(ctx, cacheKey, func() (int64, error) {
		return d.countByConditionDB(ctx, c, cfg)
	})
}

// countByConditionDB 执行计数查询（内部方法）
func (d *BaseDao[T]) countByConditionDB(ctx context.Context, c *query.Conditions, cfg *QueryOptions) (int64, error) {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return 0, err
	}
	var count int64
	db := d.db.WithContext(ctx).Model(new(T))
	if cfg.ForceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		db = db.Unscoped()
	}
	err = db.Where(queryStr, args...).Count(&count).Error
	return count, err
}

// ExistsByCondition 检查是否存在满足条件的记录（支持缓存）
// 参数：
//   - c: 查询条件
//   - opts: 查询选项
//
// 返回：
//   - bool: 是否存在
//   - error: 执行错误
//
// 缓存行为：
//   - 缓存存在性结果（1/0），提高查询效率
//   - 内部使用 SELECT 1 LIMIT 1 优化性能
//
// 示例：
//
//	cond := &query.Conditions{
//	    Columns: []query.Column{{Name: "email", Value: "test@example.com"}},
//	}
//	exists, err := dao.ExistsByCondition(ctx, cond)
func (d *BaseDao[T]) ExistsByCondition(ctx context.Context, c *query.Conditions, opts ...QueryOption) (bool, error) {
	cfg := ApplyOptions(opts...)
	if d.cacheManager == nil {
		return d.existsByConditionDB(ctx, c, cfg)
	}
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return false, err
	}
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v_%v", queryStr, args, cfg.ForceMaster, cfg.Unscoped)))
	return d.cacheManager.getExists(ctx, cacheKey, func() (bool, error) {
		return d.existsByConditionDB(ctx, c, cfg)
	})
}

// existsByConditionDB 执行存在性查询（内部方法）
func (d *BaseDao[T]) existsByConditionDB(ctx context.Context, c *query.Conditions, cfg *QueryOptions) (bool, error) {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return false, err
	}
	var exists bool
	db := d.db.WithContext(ctx).Model(new(T))
	if cfg.ForceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		db = db.Unscoped()
	}
	err = db.Where(queryStr, args...).Select("1").Limit(1).Scan(&exists).Error
	return exists, err
}

// GetByCustomQuery 执行自定义查询，支持分页和原始 SQL（无缓存）
// 参数：
//   - queryFunc: 自定义查询构建函数，接收 *gorm.DB 返回 *gorm.DB
//   - result: 查询结果接收器（切片或结构体指针）
//   - page: 页码（从 0 开始），若 page < 0 或 limit <= 0 则不分页
//   - limit: 每页条数
//   - opts: 查询选项
//
// 返回：
//   - int64: 总记录数（不分页时返回 -1）
//   - error: 执行错误
//
// 注意：
//   - 此方法不涉及缓存，适合复杂的联表查询或聚合查询
//   - 原始 SQL 查询时，SQL 中不能包含 LIMIT 子句（会报错）
//   - 原始 SQL 会被包裹为子查询以安全添加分页
//
// 示例：
//
//	// 常规 ORM 分页
//	var users []model.User
//	total, err := dao.GetByCustomQuery(ctx, func(db *gorm.DB) *gorm.DB {
//	    return db.Model(&model.User{}).Where("status = ?", 1).Order("id DESC")
//	}, &users, 0, 20)
//
//	// 原始 SQL 分页
//	var results []map[string]interface{}
//	total, err := dao.GetByCustomQuery(ctx, func(db *gorm.DB) *gorm.DB {
//	    return db.Raw("SELECT id, name FROM users WHERE status = ? ORDER BY id DESC", 1)
//	}, &results, 0, 20)
func (d *BaseDao[T]) GetByCustomQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, result interface{}, page, limit int, opts ...QueryOption) (int64, error) {
	cfg := ApplyOptions(opts...)
	var total int64 = -1

	baseDB := d.db.WithContext(ctx)
	if cfg.ForceMaster {
		baseDB = baseDB.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		baseDB = baseDB.Unscoped()
	}

	db := queryFunc(baseDB)

	if page >= 0 && limit > 0 {
		stmt := db.Statement
		if stmt != nil {
			if stmt.Table != "" || stmt.Model != nil {
				// 常规 ORM 链式查询
				if err := db.Count(&total).Error; err != nil {
					return 0, fmt.Errorf("GetByCustomQuery: count failed: %w", err)
				}
				offset := page * limit
				db = db.Offset(offset).Limit(limit)
			} else if stmt.SQL.Len() > 0 {
				originalSQL := strings.TrimRight(stmt.SQL.String(), ";")
				vars := stmt.Vars

				// 直接调用 ConvertToCountSQL，若失败则返回错误
				countSQL, err := ConvertToCountSQL(ctx, originalSQL)
				if err != nil {
					return 0, fmt.Errorf("GetByCustomQuery: convert to count SQL failed: %w", err)
				}

				// 确保 countSQL 中的占位符数量与 vars 长度匹配
				placeholderCount := strings.Count(countSQL, "?")
				if placeholderCount != len(vars) {
					if placeholderCount < len(vars) {
						// 只取需要的参数，截断多余参数（例如 limit/offset 等）
						vars = vars[:placeholderCount]
					} else {
						return 0, fmt.Errorf("count SQL expects %d placeholders but got %d args", placeholderCount, len(vars))
					}
				}

				// 尝试执行 COUNT 查询
				var count int64
				errCount := baseDB.Raw(countSQL, vars...).Scan(&count).Error
				if errCount != nil {
					// 若错误为参数数量不匹配，回退到子查询方式
					if strings.Contains(errCount.Error(), "expected") && strings.Contains(errCount.Error(), "arguments") {
						subQuerySQL := "SELECT COUNT(*) FROM (" + originalSQL + ") AS t"
						if err := baseDB.Raw(subQuerySQL, vars...).Scan(&count).Error; err != nil {
							return 0, fmt.Errorf("GetByCustomQuery: count subquery failed: %w", err)
						}
						total = count
					} else {
						return 0, fmt.Errorf("GetByCustomQuery: count raw sql failed: %w", errCount)
					}
				} else {
					total = count
				}

				// 构建分页查询 SQL
				wrappedSQL := "SELECT * FROM (" + originalSQL + ") AS t LIMIT ? OFFSET ?"
				offset := page * limit
				params := make([]interface{}, 0, len(vars)+2)
				params = append(params, vars...)
				params = append(params, limit, offset)
				db = baseDB.Raw(wrappedSQL, params...)
			}
		}
	}

	var err error
	if db.Statement != nil && db.Statement.SQL.Len() > 0 {
		err = db.Scan(result).Error
	} else {
		err = db.Find(result).Error
	}
	if err != nil {
		return 0, fmt.Errorf("GetByCustomQuery: execute query failed: %w", err)
	}
	return total, nil
}

// ---- 创建方法 ----

// Create 创建单条记录
// 参数：
//   - ctx: 上下文
//   - entity: 要创建的实体指针
//
// 返回：
//   - error: 执行错误
//
// 注意：
//   - 创建成功后，实体的 ID 会被自动回填（GORM 特性）
//   - 自动清理条件缓存（condition 类型）并触发延迟双删
//
// 示例：
//
//	user := &model.User{Name: "张三", Age: 25}
//	err := dao.Create(ctx, user)
//	fmt.Println(user.ID) // 回填的 ID
func (d *BaseDao[T]) Create(ctx context.Context, entity *T) error {
	err := d.db.WithContext(ctx).Table(d.tableName).Create(entity).Error
	if err == nil && d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeCondition)
	}
	return err
}

// CreateInBatches 批量创建记录
// 参数：
//   - ctx: 上下文
//   - entities: 实体切片
//   - batchSize: 每批数量
//
// 返回：
//   - error: 执行错误
func (d *BaseDao[T]) CreateInBatches(ctx context.Context, entities []*T, batchSize int) error {
	err := d.db.WithContext(ctx).Table(d.tableName).CreateInBatches(entities, batchSize).Error
	if err == nil && d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeCondition)
	}
	return err
}

// CreateByTx 在事务中创建单条记录
// 参数：
//   - ctx: 上下文
//   - tx: GORM 事务实例
//   - entity: 实体指针
//
// 返回：
//   - uint64: 新记录的 ID
//   - error: 执行错误
func (d *BaseDao[T]) CreateByTx(ctx context.Context, tx *gorm.DB, entity *T) (uint64, error) {
	err := tx.WithContext(ctx).Table(d.tableName).Create(entity).Error
	if err == nil && d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeCondition)
	}
	return d.idExtractor(entity), err
}

// CreateByInBatchesTx 在事务中批量创建记录
// 参数：
//   - ctx: 上下文
//   - tx: GORM 事务实例
//   - entities: 实体切片
//   - batchSize: 每批数量
//
// 返回：
//   - error: 执行错误
func (d *BaseDao[T]) CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, entities []*T, batchSize int) error {
	err := tx.WithContext(ctx).Table(d.tableName).CreateInBatches(entities, batchSize).Error
	if err == nil && d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeCondition)
	}
	return err
}

// ---- 更新方法 ----

// UpdateByID 根据 ID 更新记录
// 参数：
//   - ctx: 上下文
//   - entity: 实体指针（必须包含 ID）
//
// 返回：
//   - error: 执行错误
//
// 注意：
//   - 只更新 updateBuilder 中返回的字段（非零值）
//   - 自动清理单条缓存（single）和条件缓存（condition），并触发延迟双删
//   - 若 updateBuilder 返回空 map 或 ID 无效，则返回错误
func (d *BaseDao[T]) UpdateByID(ctx context.Context, entity *T) error {
	if d.updateBuilder == nil {
		return errors.New("updateBuilder is nil")
	}
	update := d.updateBuilder(entity)
	if len(update) == 0 {
		return errors.New("no fields to update")
	}
	id := d.idExtractor(entity)
	if id == 0 {
		return errors.New("invalid id")
	}
	// 使用 Model 以自动添加 deleted_at IS NULL 条件
	err := d.db.WithContext(ctx).Model(new(T)).Where("id = ?", id).Updates(update).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, id, DeleteDaoTypeSingle)
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, id, nil, DeleteDaoTypeCondition)
	}
	return nil
}

// UpdateByCondition 根据条件批量更新
// 参数：
//   - ctx: 上下文
//   - c: 查询条件
//   - entity: 包含更新字段的实体（只更新非零值）
//
// 返回：
//   - error: 执行错误
//
// 注意：
//   - 会更新所有满足条件的记录
//   - 更新后清空所有缓存（all），并触发延迟双删（all）
func (d *BaseDao[T]) UpdateByCondition(ctx context.Context, c *query.Conditions, entity *T) error {
	if d.updateBuilder == nil {
		return errors.New("updateBuilder is nil")
	}
	update := d.updateBuilder(entity)
	if len(update) == 0 {
		return errors.New("no fields to update")
	}
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}
	err = d.db.WithContext(ctx).Model(new(T)).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeAll)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeAll)
	}
	return nil
}

// UpdateByTx 在事务中根据 ID 更新记录
// 参数：
//   - ctx: 上下文
//   - tx: GORM 事务实例
//   - entity: 包含 ID 和更新字段的实体
//
// 返回：
//   - error: 执行错误
func (d *BaseDao[T]) UpdateByTx(ctx context.Context, tx *gorm.DB, entity *T) error {
	if d.updateBuilder == nil {
		return errors.New("updateBuilder is nil")
	}
	update := d.updateBuilder(entity)
	if len(update) == 0 {
		return errors.New("no fields to update")
	}
	id := d.idExtractor(entity)
	if id == 0 {
		return errors.New("invalid id")
	}
	err := tx.WithContext(ctx).Model(new(T)).Where("id = ?", id).Updates(update).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, id, DeleteDaoTypeSingle)
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, id, nil, DeleteDaoTypeCondition)
	}
	return nil
}

// UpdateByConditionTx 在事务中根据条件批量更新
// 参数：
//   - ctx: 上下文
//   - tx: GORM 事务实例
//   - c: 查询条件
//   - entity: 包含更新字段的实体
//
// 返回：
//   - error: 执行错误
func (d *BaseDao[T]) UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, entity *T) error {
	if d.updateBuilder == nil {
		return errors.New("updateBuilder is nil")
	}
	update := d.updateBuilder(entity)
	if len(update) == 0 {
		return errors.New("no fields to update")
	}
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}
	err = tx.WithContext(ctx).Model(new(T)).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeAll)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeAll)
	}
	return nil
}

// ---- 删除方法 ----

// DeleteByID 根据 ID 删除记录（软删除）
// 参数：
//   - ctx: 上下文
//   - id: 记录 ID
//
// 返回：
//   - error: 执行错误
//
// 注意：
//   - 若表有软删除字段（deleted_at），则执行软删除；否则物理删除
//   - 自动清理单条缓存和条件缓存，并触发延迟双删
func (d *BaseDao[T]) DeleteByID(ctx context.Context, id uint64) error {
	// 直接使用 Delete(new(T))，GORM 从参数获取模型信息以启用软删除
	err := d.db.WithContext(ctx).Where("id = ?", id).Delete(new(T)).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, id, DeleteDaoTypeSingle)
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, id, nil, DeleteDaoTypeCondition)
	}
	return nil
}

// DeleteByIDs 根据 ID 列表批量删除（软删除）
// 参数：
//   - ctx: 上下文
//   - ids: ID 列表
//
// 返回：
//   - error: 执行错误
func (d *BaseDao[T]) DeleteByIDs(ctx context.Context, ids []uint64) error {
	err := d.db.WithContext(ctx).Where("id IN (?)", ids).Delete(new(T)).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		for _, id := range ids {
			d.cacheManager.deleteCache(ctx, id, DeleteDaoTypeSingle)
		}
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, ids, DeleteDaoTypeCondition)
	}
	return nil
}

// DeleteByCondition 根据条件删除记录（软删除）
// 参数：
//   - ctx: 上下文
//   - c: 查询条件
//
// 返回：
//   - error: 执行错误
//
// 注意：
//   - 会删除所有满足条件的记录
//   - 删除后清空所有缓存（all），并触发延迟双删（all）
func (d *BaseDao[T]) DeleteByCondition(ctx context.Context, c *query.Conditions) error {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}
	err = d.db.WithContext(ctx).Where(queryStr, args...).Delete(new(T)).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeAll)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeAll)
	}
	return nil
}

// DeleteByTx 在事务中根据 ID 删除记录（软删除）
// 参数：
//   - ctx: 上下文
//   - tx: GORM 事务实例
//   - id: 记录 ID
//
// 返回：
//   - error: 执行错误
func (d *BaseDao[T]) DeleteByTx(ctx context.Context, tx *gorm.DB, id uint64) error {
	err := tx.WithContext(ctx).Where("id = ?", id).Delete(new(T)).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, id, DeleteDaoTypeSingle)
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, id, nil, DeleteDaoTypeCondition)
	}
	return nil
}

// DeleteByIDsTx 在事务中根据 ID 列表批量删除（软删除）
// 参数：
//   - ctx: 上下文
//   - tx: GORM 事务实例
//   - ids: ID 列表
//
// 返回：
//   - error: 执行错误
func (d *BaseDao[T]) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) error {
	err := tx.WithContext(ctx).Where("id IN (?)", ids).Delete(new(T)).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		for _, id := range ids {
			d.cacheManager.deleteCache(ctx, id, DeleteDaoTypeSingle)
		}
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, ids, DeleteDaoTypeCondition)
	}
	return nil
}

// DeleteByTxCondition 在事务中根据条件删除记录（软删除）
// 参数：
//   - ctx: 上下文
//   - tx: GORM 事务实例
//   - c: 查询条件
//
// 返回：
//   - error: 执行错误
func (d *BaseDao[T]) DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}
	err = tx.WithContext(ctx).Where(queryStr, args...).Delete(new(T)).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeAll)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeAll)
	}
	return nil
}

// ClearCache 清空该表所有缓存
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - error: 执行错误
//
// 注意：
//   - 此操作会删除该表所有缓存键（通过 DelByPrefix 实现）
//   - 谨慎使用，避免影响其他表的缓存
func (d *BaseDao[T]) ClearCache(ctx context.Context) error {
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeAll)
	}
	return nil
}

// ---- 自定义执行 ----

// ExecByCustomFunc 执行自定义更新操作
// 参数：
//   - ctx: 上下文
//   - updateFunc: 自定义函数，接收 *gorm.DB 返回 *gorm.DB（需将错误赋给 db.Error）
//
// 返回：
//   - error: 执行错误
//
// 注意：
//   - 适用于复杂更新（如多表联合更新、存储过程调用等）
//   - 执行成功后清空所有缓存（all）并触发延迟双删（all）
//   - 调用方需确保 updateFunc 正确设置 db.Error
//
// 示例：
//
//	err := dao.ExecByCustomFunc(ctx, func(db *gorm.DB) *gorm.DB {
//	    err := db.Transaction(func(tx *gorm.DB) error {
//	        if err := tx.Model(&model.User{}).Where("id = ?", 1).Update("name", "新名称").Error; err != nil {
//	            return err
//	        }
//	        return nil
//	    })
//	    if err != nil {
//	        db.Error = err
//	    }
//	    return db
//	})
func (d *BaseDao[T]) ExecByCustomFunc(ctx context.Context, updateFunc func(*gorm.DB) *gorm.DB) error {
	db := d.db.WithContext(ctx).Table(d.tableName)
	db = updateFunc(db)
	if err := db.Error; err != nil {
		return err
	}
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeAll)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeAll)
	}
	return nil
}
