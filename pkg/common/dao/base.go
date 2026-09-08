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

// ============================================================================
// 第一部分：结构体定义（泛型 DAO 基类）
// ============================================================================

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
//
// 大白话：这是一个"万能模具"，你用 [User]、[Order] 去套它，
// 它就变成专门操作 User 表或 Order 表的工具，所有增删改查和缓存逻辑都自动拥有。
type BaseDao[T any] struct {
	// db：GORM 数据库连接实例，负责所有数据库操作
	db *gorm.DB

	// cacheManager：缓存管理器（nil 表示禁用缓存）
	// 当 cacheManager != nil 时，所有查询都会先走缓存
	cacheManager *cacheManager[T]

	// tableName：数据库表名（需与模型映射一致）
	// 例如："users"、"orders"
	tableName string

	// updateBuilder：更新字段构建器，将实体转为 map[string]interface{}
	// 例如：传入 User{Name: "张三", Age: 18}，返回 map{"name": "张三", "age": 18}
	// 零值字段（如 Age=0）会被忽略，避免误更新
	updateBuilder func(*T) map[string]interface{}

	// idExtractor：ID 提取器，从实体中获取 uint64 类型的 ID
	// 例如：传入 &User{ID: 123}，返回 123
	// 因为泛型 T 在编译时不知道有没有 ID 字段，所以需要业务方传入这个函数
	idExtractor func(*T) uint64
}

// ============================================================================
// 第二部分：构造函数 NewBaseDao
// ============================================================================

// NewBaseDao 创建泛型基类实例
// 这是整个框架的"入口"，所有业务 DAO 都通过这个函数创建。
//
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
//	// 基础用法（使用默认缓存配置）
//	base := dao.NewBaseDao(db, cacheAdapter, "users", updateBuilder, idExtractor)
//
//	// 自定义缓存配置
//	cfg := dao.CacheConfig{DefaultExpireTime: 10 * time.Minute}
//	base := dao.NewBaseDao(db, cacheAdapter, "users", updateBuilder, idExtractor, cfg)
//
//	// 禁用缓存（传 nil）
//	base := dao.NewBaseDao(db, nil, "users", updateBuilder, idExtractor)
func NewBaseDao[T any](
	db *gorm.DB,
	cache Cache[T],
	tableName string,
	updateBuilder func(*T) map[string]interface{},
	idExtractor func(*T) uint64,
	config ...CacheConfig,
) *BaseDao[T] {
	// 【步骤1】创建 BaseDao 实例，先填充非缓存字段
	dao := &BaseDao[T]{
		db:            db,
		tableName:     tableName,
		updateBuilder: updateBuilder,
		idExtractor:   idExtractor,
	}

	// 【步骤2】如果传入了缓存适配器（cache != nil），则初始化缓存管理器
	if cache != nil {
		// 2.1 先加载默认配置
		cfg := DefaultCacheConfig()

		// 2.2 如果用户传入了自定义配置（config 长度 > 0），覆盖默认值
		if len(config) > 0 {
			c := config[0]
			// 只覆盖用户明确指定的非零值字段
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

		// 2.3 创建缓存管理器，并挂载到 dao 上
		dao.cacheManager = newCacheManager(cache, cfg, tableName)
	}

	// 【步骤3】返回完整的 BaseDao 实例
	return dao
}

// ============================================================================
// 第三部分：查询方法 - 单条查询
// ============================================================================

// GetByID 根据 ID 获取单条记录（支持缓存）
//
// 这是最常用的查询方法，流程如下：
//  1. 先查缓存（如果开启了缓存）
//  2. 缓存命中 → 直接返回
//  3. 缓存未命中 → singleflight + 分布式锁 → 查数据库 → 回填缓存
//  4. 记录不存在 → 设置短时占位符（防穿透）
//
// 参数：
//   - ctx: 上下文
//   - id: 记录 ID (uint64)
//   - opts: 查询选项（如强制主库、忽略软删除）
//
// 返回：
//   - *T: 记录指针，若不存在返回 database.ErrRecordNotFound
//   - error: 执行错误
//
// 示例：
//
//	// 普通查询
//	user, err := dao.GetByID(ctx, 1)
//
//	// 强制走主库（默认就是主库，但可以显式声明）
//	user, err := dao.GetByID(ctx, 1, dao.WithForceMaster())
//
//	// 忽略软删除（把已删除的也查出来）
//	user, err := dao.GetByID(ctx, 1, dao.WithUnscoped())
func (d *BaseDao[T]) GetByID(ctx context.Context, id uint64, opts ...QueryOption) (*T, error) {
	// 【步骤1】应用查询选项（如 ForceMaster、Unscoped）
	cfg := ApplyOptions(opts...)

	// 【步骤2】如果缓存管理器为 nil（缓存被禁用），直接查数据库
	if d.cacheManager == nil {
		return d.queryDBByID(ctx, id, cfg)
	}

	// 【步骤3】走缓存流程：调用 cacheManager.get
	// 传入一个闭包函数，当缓存未命中时，执行这个函数查数据库
	return d.cacheManager.get(ctx, id, func() (*T, error) {
		return d.queryDBByID(ctx, id, cfg)
	})
}

// queryDBByID 执行数据库查询（内部方法）
// 直接查库，不涉及缓存。由 GetByID 在缓存未命中或禁用时调用。
//
// 大白话：这是真正去数据库执行 SELECT * FROM table WHERE id = ? 的地方
func (d *BaseDao[T]) queryDBByID(ctx context.Context, id uint64, cfg *QueryOptions) (*T, error) {
	// 【步骤1】声明一个 T 类型的空变量（如 var entity User）
	var entity T

	// 【步骤2】构建查询：从 db 开始，带上上下文，绑定模型 new(T)
	db := d.db.WithContext(ctx).Model(new(T))

	// 【步骤3】如果强制走主库，添加 dbresolver.Write 子句
	if cfg.ForceMaster {
		db = db.Clauses(dbresolver.Write)
	}

	// 【步骤4】如果忽略软删除，添加 Unscoped()
	if cfg.Unscoped {
		db = db.Unscoped()
	}

	// 【步骤5】执行查询：WHERE id = ?，取第一条
	err := db.Where("id = ?", id).First(&entity).Error

	// 【步骤6】处理结果
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 记录不存在，返回标准错误
			return nil, database.ErrRecordNotFound
		}
		// 其他数据库错误，直接返回
		return nil, err
	}

	// 【步骤7】返回查到的记录指针
	return &entity, nil
}

// ============================================================================
// 第四部分：查询方法 - 分页查询
// ============================================================================

// GetByColumns 根据列条件进行分页查询（支持缓存）
//
// 这是最常用的列表查询方法，支持：
//   - 分页（page, limit）
//   - 排序（sort）
//   - 条件过滤（columns）
//   - 自动缓存（total + 当前页 ID 列表）
//
// 缓存策略：
//   - 分页结果会按"条件+分页+排序+forceMaster+unscoped"组合生成唯一缓存键
//   - 缓存中存储 total 和 ID 列表，完整记录通过批量缓存读取
//   - 当单页记录数超过 MaxCacheableRecords 时，放弃缓存直接查库
//
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
// 示例：
//
//	params := &query.Params{
//	    Page:    0,
//	    Limit:   20,
//	    Sort:    "-id",        // 按 id 降序
//	    Columns: []query.Column{
//	        {Name: "status", Value: 1},
//	        {Name: "age", Value: 18, Op: ">"},
//	    },
//	}
//	records, total, err := dao.GetByColumns(ctx, params)
func (d *BaseDao[T]) GetByColumns(ctx context.Context, params *query.Params, opts ...QueryOption) ([]*T, int64, error) {
	// 【步骤1】应用查询选项
	cfg := ApplyOptions(opts...)

	// 【步骤2】如果缓存被禁用，直接查数据库
	if d.cacheManager == nil {
		return d.queryByColumnsDB(ctx, params, cfg)
	}

	// 【步骤3】将查询条件转为 GORM 可执行的 SQL 片段
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, 0, fmt.Errorf("GetByColumns: convert query conditions failed: %w", err)
	}

	// 【步骤4】生成唯一缓存键
	// 包含：SQL 条件、参数、页码、每页条数、排序、ForceMaster、Unscoped
	// 任何一个因素变化，缓存键都会不同
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%d_%d_%s_%v_%v",
		queryStr, args, params.Page, params.Limit, params.Sort,
		cfg.ForceMaster, cfg.Unscoped)))

	// 【步骤5】调用 cacheManager.getColumns 走缓存流程
	return d.cacheManager.getColumns(ctx,
		// 函数1：缓存未命中时，查数据库获取当前页数据
		func() ([]*T, int64, error) {
			return d.queryByColumnsDB(ctx, params, cfg)
		},
		// 函数2：从缓存拿到 ID 列表后，批量取完整记录
		func(missedIDs []uint64) ([]*T, error) {
			return d.queryByIDsDB(ctx, missedIDs, cfg)
		},
		cacheKey,
	)
}

// queryByColumnsDB 执行分页数据库查询（内部方法）
// 直接查库，不涉及缓存。
func (d *BaseDao[T]) queryByColumnsDB(ctx context.Context, params *query.Params, cfg *QueryOptions) ([]*T, int64, error) {
	// 【步骤1】转换查询条件
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, 0, fmt.Errorf("convert conditions failed: %w", err)
	}

	// 【步骤2】声明变量
	var total int64
	var records []*T

	// 【步骤3】构建查询
	db := d.db.WithContext(ctx).Model(new(T))
	if cfg.ForceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		db = db.Unscoped()
	}

	// 【步骤4】计算总数（如果 sort 不是 "ignore count"，则执行 Count）
	// "ignore count" 是性能优化：当业务不需要总条数时，跳过 COUNT 查询
	if params.Sort != SortIgnoreCount {
		if countErr := db.Where(queryStr, args...).Count(&total).Error; countErr != nil {
			return nil, 0, countErr
		}
		// 如果总数为 0，直接返回空列表，不用再查数据了
		if total == 0 {
			return []*T{}, 0, nil
		}
	}

	// 【步骤5】转换分页参数并执行查询
	order, limit, offset := params.ConvertToPage()
	err = db.Order(order).Limit(limit).Offset(offset).Where(queryStr, args...).Find(&records).Error
	if err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

// ============================================================================
// 第五部分：查询方法 - 条件单条查询
// ============================================================================

// GetOneByColumns 根据列条件获取单条记录（支持缓存）
// 和 GetByColumns 类似，但只返回第一条匹配记录
//
// 缓存策略：
//   - 缓存键包含条件、排序、forceMaster、unscoped
//   - 缓存中存储 ID，完整记录通过 GetByID 缓存获取
//
// 返回：
//   - *T: 记录指针，若不存在返回 database.ErrRecordNotFound
//   - error: 执行错误
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

	// 走缓存：调用 cacheManager.getCondition
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

// ============================================================================
// 第六部分：查询方法 - 条件 ID 列表
// ============================================================================

// GetByCondition 根据条件获取 ID 列表（支持缓存）
// 适用场景：只需要 ID 列表，不需要完整对象（比如用于后续批量处理）
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

	// 走缓存：调用 cacheManager.getByCondition
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
	// 使用 idExtractor 提取每个实体的 ID
	ids := make([]uint64, 0, len(entities))
	for _, e := range entities {
		if id := d.idExtractor(e); id != 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// ============================================================================
// 第七部分：查询方法 - 批量 ID 查询
// ============================================================================

// GetByIDs 根据 ID 列表批量获取记录（支持缓存）
// 这是所有批量查询的底层支柱，支持自动分批和占位符
//
// 缓存行为：
//   - 先尝试从缓存批量读取，未命中的 ID 统一查询 DB 并回填缓存
//   - 自动分批处理（每批 MaxBatchSize），避免内存峰值
//   - 不存在的 ID 会设置占位符，防止穿透
//
// 返回：
//   - map[uint64]*T: ID -> 记录的映射（只包含成功获取的记录）
//   - error: 执行错误
//
// 示例：
//
//	ids := []uint64{1, 2, 3}
//	recordMap, err := dao.GetByIDs(ctx, ids)
//	// recordMap[1] -> User{ID:1, Name:"张三"}
//	// recordMap[2] -> User{ID:2, Name:"李四"}
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

// ============================================================================
// 第八部分：查询方法 - 计数和存在性
// ============================================================================

// CountByCondition 根据条件统计记录数（支持缓存）
// 缓存行为：缓存计数结果（包括 0），避免重复统计
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

	// 走缓存：调用 cacheManager.getCount
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
// 缓存行为：缓存存在性结果（1/0），提高查询效率
// 内部使用 SELECT 1 LIMIT 1 优化性能
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

	// 走缓存：调用 cacheManager.getExists
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
	// SELECT 1 LIMIT 1：只查询是否存在，不返回具体数据，性能最优
	err = db.Where(queryStr, args...).Select("1").Limit(1).Scan(&exists).Error
	return exists, err
}

// ============================================================================
// 第九部分：自定义查询（无缓存，支持原始 SQL）
// ============================================================================

// handleORMQuery 处理常规 ORM 查询（有 Model/Table）
// 用于 GetByCustomQuery 中的分页处理
func (d *BaseDao[T]) handleORMQuery(db *gorm.DB, result interface{}, page, limit int) (int64, error) {
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return 0, fmt.Errorf("count failed: %w", err)
	}
	offset := page * limit
	db = db.Offset(offset).Limit(limit)
	if err := db.Find(result).Error; err != nil {
		return 0, fmt.Errorf("find failed: %w", err)
	}
	return total, nil
}

// handleRawSQLQuery 处理原始 SQL 查询
// 用于 GetByCustomQuery 中的原始 SQL 分页处理
func (d *BaseDao[T]) handleRawSQLQuery(ctx context.Context, baseDB *gorm.DB, stmt *gorm.Statement, result interface{}, page, limit int) (int64, error) {
	originalSQL := strings.TrimRight(stmt.SQL.String(), ";")
	vars := stmt.Vars

	// 计算总数
	total, err := d.executeCountQuery(ctx, baseDB, originalSQL, vars)
	if err != nil {
		return 0, err
	}

	// 构建分页查询：把原 SQL 包成子查询，外面加 LIMIT 和 OFFSET
	wrappedSQL := "SELECT * FROM (" + originalSQL + ") AS t LIMIT ? OFFSET ?"
	offset := page * limit
	params := make([]interface{}, 0, len(vars)+2)
	params = append(params, vars...)
	params = append(params, limit, offset)
	db := baseDB.Raw(wrappedSQL, params...)
	if err := db.Scan(result).Error; err != nil {
		return 0, fmt.Errorf("raw sql pagination failed: %w", err)
	}
	return total, nil
}

// executeCountQuery 执行计数查询（原始 SQL 场景）
func (d *BaseDao[T]) executeCountQuery(ctx context.Context, baseDB *gorm.DB, originalSQL string, vars []interface{}) (int64, error) {
	// 使用 ConvertToCountSQL 将 SELECT 转为 SELECT COUNT(*)
	countSQL, err := ConvertToCountSQL(ctx, originalSQL)
	if err != nil {
		return 0, fmt.Errorf("convert to count SQL failed: %w", err)
	}

	// 统计 countSQL 中的占位符数量
	placeholderCount := strings.Count(countSQL, "?")
	if placeholderCount < len(vars) {
		// 如果占位符比参数少，截断参数
		vars = vars[:placeholderCount]
	} else if placeholderCount > len(vars) {
		// 如果占位符比参数多，报错
		return 0, fmt.Errorf("count SQL expects %d placeholders but got %d args", placeholderCount, len(vars))
	}

	var count int64
	errCount := baseDB.Raw(countSQL, vars...).Scan(&count).Error
	if errCount != nil {
		// 若错误为参数数量不匹配，回退到子查询方式
		if strings.Contains(errCount.Error(), "expected") && strings.Contains(errCount.Error(), "arguments") {
			subQuerySQL := "SELECT COUNT(*) FROM (" + originalSQL + ") AS t"
			if err := baseDB.Raw(subQuerySQL, vars...).Scan(&count).Error; err != nil {
				return 0, fmt.Errorf("count subquery failed: %w", err)
			}
			return count, nil
		}
		return 0, fmt.Errorf("count raw sql failed: %w", errCount)
	}
	return count, nil
}

// GetByCustomQuery 执行自定义查询，支持分页和原始 SQL（无缓存）
// 适用于复杂的联表查询或聚合查询，不涉及缓存
//
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
// 注意：此方法不涉及缓存，适合复杂的联表查询或聚合查询
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
	baseDB := d.db.WithContext(ctx)
	if cfg.ForceMaster {
		baseDB = baseDB.Clauses(dbresolver.Write)
	}
	if cfg.Unscoped {
		baseDB = baseDB.Unscoped()
	}

	db := queryFunc(baseDB)

	// 不分页
	if page < 0 || limit <= 0 {
		if db.Statement != nil && db.Statement.SQL.Len() > 0 {
			// 原始 SQL
			if err := db.Scan(result).Error; err != nil {
				return -1, fmt.Errorf("execute raw sql failed: %w", err)
			}
			return -1, nil
		}
		// ORM 查询
		if err := db.Find(result).Error; err != nil {
			return -1, fmt.Errorf("find failed: %w", err)
		}
		return -1, nil
	}

	// 分页
	stmt := db.Statement
	if stmt != nil {
		if stmt.Table != "" || stmt.Model != nil {
			// ORM 查询
			return d.handleORMQuery(db, result, page, limit)
		}
		if stmt.SQL.Len() > 0 {
			// 原始 SQL
			return d.handleRawSQLQuery(ctx, baseDB, stmt, result, page, limit)
		}
	}
	return -1, fmt.Errorf("unsupported query type")
}

// ============================================================================
// 第十部分：创建方法
// ============================================================================

// Create 创建单条记录
// 创建成功后，实体的 ID 会被自动回填（GORM 特性）
// 自动清理条件缓存（condition 类型）并触发延迟双删
//
// 示例：
//
//	user := &model.User{Name: "张三", Age: 25}
//	err := dao.Create(ctx, user)
//	fmt.Println(user.ID) // 回填的 ID
func (d *BaseDao[T]) Create(ctx context.Context, entity *T) error {
	err := d.db.WithContext(ctx).Table(d.tableName).Create(entity).Error
	if err == nil && d.cacheManager != nil {
		// 创建后清理条件缓存（因为新数据可能影响条件查询结果）
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeCondition)
	}
	return err
}

// CreateInBatches 批量创建记录
func (d *BaseDao[T]) CreateInBatches(ctx context.Context, entities []*T, batchSize int) error {
	err := d.db.WithContext(ctx).Table(d.tableName).CreateInBatches(entities, batchSize).Error
	if err == nil && d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeCondition)
	}
	return err
}

// CreateByTx 在事务中创建单条记录
// 返回新记录的 ID
func (d *BaseDao[T]) CreateByTx(ctx context.Context, tx *gorm.DB, entity *T) (uint64, error) {
	err := tx.WithContext(ctx).Table(d.tableName).Create(entity).Error
	if err == nil && d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeCondition)
	}
	return d.idExtractor(entity), err
}

// CreateByInBatchesTx 在事务中批量创建记录
func (d *BaseDao[T]) CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, entities []*T, batchSize int) error {
	err := tx.WithContext(ctx).Table(d.tableName).CreateInBatches(entities, batchSize).Error
	if err == nil && d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeCondition)
	}
	return err
}

// ============================================================================
// 第十一部分：更新方法
// ============================================================================

// UpdateByID 根据 ID 更新记录
// 只更新 updateBuilder 中返回的字段（非零值）
// 自动清理单条缓存（single）和条件缓存（condition），并触发延迟双删
//
// 注意：
//   - 若 updateBuilder 返回空 map 或 ID 无效，则返回错误
//   - 使用 Model 以自动添加 deleted_at IS NULL 条件（软删除兼容）
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
	// 使用 Model(new(T)) 自动添加 deleted_at IS NULL
	err := d.db.WithContext(ctx).Model(new(T)).Where("id = ?", id).Updates(update).Error
	if err != nil {
		return err
	}
	if d.cacheManager != nil {
		// 删除单条缓存 + 条件缓存
		d.cacheManager.deleteCache(ctx, id, DeleteDaoTypeSingle)
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeCondition)
		d.cacheManager.delayedDoubleDelete(ctx, id, nil, DeleteDaoTypeCondition)
	}
	return nil
}

// UpdateByCondition 根据条件批量更新
// 会更新所有满足条件的记录
// 更新后清空所有缓存（all），并触发延迟双删（all）
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
		// 批量更新影响范围未知，清空所有缓存
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeAll)
		d.cacheManager.delayedDoubleDelete(ctx, 0, nil, DeleteDaoTypeAll)
	}
	return nil
}

// UpdateByTx 在事务中根据 ID 更新记录
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

// ============================================================================
// 第十二部分：删除方法
// ============================================================================

// DeleteByID 根据 ID 删除记录（软删除）
// 若表有软删除字段（deleted_at），则执行软删除；否则物理删除
// 自动清理单条缓存和条件缓存，并触发延迟双删
func (d *BaseDao[T]) DeleteByID(ctx context.Context, id uint64) error {
	// 使用 Delete(new(T))，GORM 从参数获取模型信息以启用软删除
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
// 会删除所有满足条件的记录
// 删除后清空所有缓存（all），并触发延迟双删（all）
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

// ============================================================================
// 第十三部分：缓存管理
// ============================================================================

// ClearCache 清空该表所有缓存
// 此操作会删除该表所有缓存键（通过 DelByPrefix 实现）
// 谨慎使用，避免影响其他表的缓存
func (d *BaseDao[T]) ClearCache(ctx context.Context) error {
	if d.cacheManager != nil {
		d.cacheManager.deleteCache(ctx, 0, DeleteDaoTypeAll)
	}
	return nil
}

// ============================================================================
// 第十四部分：自定义执行
// ============================================================================

// ExecByCustomFunc 执行自定义更新操作
// 适用于复杂更新（如多表联合更新、存储过程调用等）
// 执行成功后清空所有缓存（all）并触发延迟双删（all）
//
// 注意：调用方需确保 updateFunc 正确设置 db.Error
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
