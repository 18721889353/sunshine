// Package dao 数据访问层
// 提供数据库操作的统一接口，包含缓存管理、事务支持、延迟双删等高级特性
package dao

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/metadata"

	"github.com/18721889353/sunshine/pkg/grpc/interceptor"

	"github.com/18721889353/sunshine/internal/cache"
	"github.com/18721889353/sunshine/internal/consts"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/model"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"

	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sgorm/query"
	"github.com/18721889353/sunshine/pkg/utils"
)

// {{.TableNameCamel}}RandPool 全局随机数生成器池（线程安全，无锁竞争）
// 使用 sync.Pool 为每个 goroutine 提供独立的随机数生成器，避免锁竞争
var {{.TableNameCamel}}RandPool = sync.Pool{
	New: func() interface{} {
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	},
}

// {{.TableNameCamel}}QueryOption 查询选项函数类型（使用选项模式替代变长布尔参数）
type {{.TableNameCamel}}QueryOption func(*{{.TableNameCamel}}QueryOptions)

// {{.TableNameCamel}}QueryOptions 查询选项配置
type {{.TableNameCamel}}QueryOptions struct {
	forceMaster bool // 是否强制使用主库
}

// {{.TableNameCamel}}WithForceMaster 强制使用主库查询
// 示例：dao.GetByID(ctx, id, dao.{{.TableNameCamel}}WithForceMaster())
func {{.TableNameCamel}}WithForceMaster() {{.TableNameCamel}}QueryOption {
	return func(o *{{.TableNameCamel}}QueryOptions) {
		o.forceMaster = true
	}
}

// {{.TableNameCamel}}ApplyOptions 应用选项配置（默认强制主库）
func {{.TableNameCamel}}ApplyOptions(opts ...{{.TableNameCamel}}QueryOption) *{{.TableNameCamel}}QueryOptions {
	o := &{{.TableNameCamel}}QueryOptions{
		forceMaster: true,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// {{.TableNameCamel}}GetRandomExpireTime 生成随机化的缓存过期时间，防止缓存雪崩
// 基础时间 ±5 分钟随机偏移，避免大量缓存在同一时间失效
// 返回值：baseDuration + random(-300s ~ +300s)
func {{.TableNameCamel}}GetRandomExpireTime(base time.Duration) time.Duration {
	// 从池中获取随机数生成器（无锁竞争，高性能）
	randGen := {{.TableNameCamel}}RandPool.Get().(*rand.Rand) //nolint:errcheck // sync.Pool 保证返回 *rand.Rand 类型
	defer {{.TableNameCamel}}RandPool.Put(randGen)            // 确保放回池中供复用
	// 生成 -300 ~ +300 秒的随机偏移（对应 CacheExpireTimeOffsetSeconds）
	offsetSeconds := randGen.Int63n(601) - 300 // 0-600 秒范围，减去 300 得到 -300~+300 秒

	offset := time.Duration(offsetSeconds) * time.Second
	return base + offset
}

var _ {{.TableNameCamel}}Dao = (*{{.TableNameCamelFCL}}Dao)(nil)


// {{.TableNameCamel}}Dao defining the dao interface（所有查询方法支持选项模式）
type {{.TableNameCamel}}Dao interface {
	Create(ctx context.Context, table *model.{{.TableNameCamel}}) error
	CreateInBatches(ctx context.Context, tables []*model.{{.TableNameCamel}}, batchSize int) error
	CreateByTx(ctx context.Context, tx *gorm.DB, table *model.{{.TableNameCamel}}) (uint64, error)
	CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, tables []*model.{{.TableNameCamel}}, batchSize int) error

	DeleteByID(ctx context.Context, id uint64) error
	DeleteByIDs(ctx context.Context, ids []uint64) error
	DeleteByCondition(ctx context.Context, c *query.Conditions) error
	DeleteByTx(ctx context.Context, tx *gorm.DB, id uint64) error
	DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) error
	DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error
	ClearCache(ctx context.Context) error

	UpdateByID(ctx context.Context, table *model.{{.TableNameCamel}}) error
	UpdateByCondition(ctx context.Context, c *query.Conditions, updates *model.{{.TableNameCamel}}) error
	UpdateByTx(ctx context.Context, tx *gorm.DB, table *model.{{.TableNameCamel}}) error
	UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, updates *model.{{.TableNameCamel}}) error
	ExecByCustomFunc(ctx context.Context, updateFunc func(*gorm.DB) *gorm.DB) error

	GetByID(ctx context.Context, id uint64, opts ...{{.TableNameCamel}}QueryOption) (*model.{{.TableNameCamel}}, error)
	GetByColumns(ctx context.Context, params *query.Params, opts ...{{.TableNameCamel}}QueryOption) ([]*model.{{.TableNameCamel}}, int64, error)
	GetOneByColumns(ctx context.Context, params *query.Params, opts ...{{.TableNameCamel}}QueryOption) (*model.{{.TableNameCamel}}, error)
	GetByCondition(ctx context.Context, c *query.Conditions, opts ...{{.TableNameCamel}}QueryOption) (ids []uint64, err error)
	GetByIDs(ctx context.Context, ids []uint64, opts ...{{.TableNameCamel}}QueryOption) (map[uint64]*model.{{.TableNameCamel}}, error)
	CountByCondition(ctx context.Context, c *query.Conditions, opts ...{{.TableNameCamel}}QueryOption) (int64, error)
	ExistsByCondition(ctx context.Context, c *query.Conditions, opts ...{{.TableNameCamel}}QueryOption) (bool, error)
	GetByCustomQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, result interface{}, page, limit int, opts ...{{.TableNameCamel}}QueryOption) (int64, error)
}

// {{.TableNameCamelFCL}}CacheManager 统一管理缓存操作
type {{.TableNameCamelFCL}}CacheManager struct {
	cache cache.{{.TableNameCamel}}Cache
	sfg   *singleflight.Group
}

// new{{.TableNameCamel}}CacheManager 创建缓存管理器
func new{{.TableNameCamel}}CacheManager(c cache.{{.TableNameCamel}}Cache) *{{.TableNameCamelFCL}}CacheManager {
	return &{{.TableNameCamelFCL}}CacheManager{
		cache: c,
		sfg:   new(singleflight.Group),
	}
}

// getCacheKey 生成基于 ID 的缓存键
// 格式：prefix + id
func (m *{{.TableNameCamelFCL}}CacheManager) getCacheKey(id uint64) string {
	return cache.{{.TableNameCamel}}CachePrefixKey + utils.Uint64ToStr(id)
}

// getConditionCacheKey 生成基于查询条件的缓存键
// 用于 GetOneByColumns、GetByCondition 等按条件查询的方法
// 格式：prefix + "condition:" + keyMD5
func (m *{{.TableNameCamelFCL}}CacheManager) getConditionCacheKey(key string) string {
	return cache.{{.TableNameCamel}}CachePrefixKey + "condition:" + key
}

// getColumnsCacheKey 生成基于分页查询条件的缓存键
// 专用于 GetByColumns 方法的分页查询缓存
// 格式：prefix + "columns:" + keyMD5
func (m *{{.TableNameCamelFCL}}CacheManager) getColumnsCacheKey(key string) string {
	return cache.{{.TableNameCamel}}CachePrefixKey + "columns:" + key
}

// get 通过 singleflight 和缓存获取数据
// 错误处理原则：
//   - 缓存读取错误：仅记录日志，回退到数据库查询（缓存是优化手段，不应影响主流程）
//   - 数据库错误：必须返回给调用方
//   - 缓存写入错误：仅记录日志，不影响返回值
//
// handleCacheFallback 处理缓存回退逻辑
func (m *{{.TableNameCamelFCL}}CacheManager) handleCacheFallback(ctx context.Context, id uint64, err error, queryFunc func() (*model.{{.TableNameCamel}}, error)) (*model.{{.TableNameCamel}}, error) {
	// 如果是占位符错误，返回记录未找到
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}

	// 回退到数据库查询
	val, sfErr, _ := m.sfg.Do(m.getCacheKey(id), func() (interface{}, error) {
		table, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}
		// 尝试设置缓存（失败仅记录日志）
		expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
		if cacheErr := m.cache.Set(ctx, id, table, expireTime); cacheErr != nil {
			logger.WarnWithCtx(ctx, "cache.Set error after fallback", logger.Err(cacheErr), logger.Any("id", id))
		}
		return table, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	table, ok := val.(*model.{{.TableNameCamel}})
	if !ok {
		return nil, database.ErrRecordNotFound
	}
	return table, nil
}

func (m *{{.TableNameCamelFCL}}CacheManager) get(ctx context.Context, id uint64, queryFunc func() (*model.{{.TableNameCamel}}, error)) (*model.{{.TableNameCamel}}, error) {
	// 先从缓存获取
	record, err := m.cache.Get(ctx, id)
	if err == nil {
		return record, nil
	}

	// 缓存未命中，从数据库获取
	if errors.Is(err, database.ErrCacheNotFound) {
		// 使用 singleflight 防止并发请求同时访问数据库
		val, sfErr, _ := m.sfg.Do(m.getCacheKey(id), func() (interface{}, error) {
			table, dbErr := queryFunc()
			if dbErr != nil {
				// 设置占位符缓存防止缓存穿透
				if errors.Is(dbErr, gorm.ErrRecordNotFound) {
					if placeholderErr := m.cache.SetPlaceholder(ctx, id); placeholderErr != nil {
						logger.WarnWithCtx(ctx, "cache.SetPlaceholder error", logger.Err(placeholderErr), logger.Any("id", id))
					}
					return nil, database.ErrRecordNotFound
				}
				return nil, dbErr
			}
			// 设置缓存（使用随机化过期时间防止雪崩）
			expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
			if cacheErr := m.cache.Set(ctx, id, table, expireTime); cacheErr != nil {
				logger.WarnWithCtx(ctx, "cache.Set error", logger.Err(cacheErr), logger.Any("id", id))
			}
			return table, nil
		})
		if sfErr != nil {
			return nil, sfErr
		}
		table, ok := val.(*model.{{.TableNameCamel}})
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return table, nil
	}

	// 其他缓存错误（如 Redis 连接失败等），仅记录日志并回退到数据库查询
	logger.WarnWithCtx(ctx, "cache.Get error, falling back to database", logger.Err(err), logger.Any("id", id))

	return m.handleCacheFallback(ctx, id, err, queryFunc)
}

// handleConditionCacheHit 处理条件查询缓存命中的情况
// 从缓存中获取 ID，然后通过 ID 获取完整记录
func (m *{{.TableNameCamelFCL}}CacheManager) handleConditionCacheHit(ctx context.Context, cacheKey string, cachedID uint64) (*model.{{.TableNameCamel}}, bool, error) {
	// 通过 ID 获取完整信息
	record, getErr := m.get(ctx, cachedID, func() (*model.{{.TableNameCamel}}, error) {
		// 直接从数据库获取完整记录
		table := &model.{{.TableNameCamel}}{}
		err := database.GetDB().WithContext(ctx).Where("id = ?", cachedID).First(table).Error
		if err != nil {
			return nil, err
		}
		return table, nil
	})
	if getErr == nil && record != nil && record.ID == cachedID {
		return record, true, nil // 缓存命中
	}
	// 如果通过 ID 获取失败（可能是记录已删除），清除条件缓存中的 ID
	if getErr != nil {
		if delErr := m.cache.DelByKey(ctx, cacheKey); delErr != nil {
			logger.WarnWithCtx(ctx, "cache.DelByKey error", logger.Err(delErr), logger.Any("key", cacheKey))
		}
	}
	return nil, false, nil // 缓存未命中或失效
}

// cacheConditionResult 缓存条件查询结果
// 包括 ID 和完整记录
func (m *{{.TableNameCamelFCL}}CacheManager) cacheConditionResult(ctx context.Context, cacheKey string, record *model.{{.TableNameCamel}}) {
	if record == nil {
		return
	}
	expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
	// 缓存 ID
	if cacheErr := m.cache.SetIDByKey(ctx, cacheKey, record.ID, expireTime); cacheErr != nil {
		logger.WarnWithCtx(ctx, "cache.SetIDByKey error", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("id", record.ID))
	}
	// 缓存完整记录
	if cacheErr := m.cache.Set(ctx, record.ID, record, expireTime); cacheErr != nil {
		logger.WarnWithCtx(ctx, "cache.Set error", logger.Err(cacheErr), logger.Any("id", record.ID))
	}
}

// executeConditionQueryWithSingleflight 使用 singleflight 执行条件查询并缓存结果
func (m *{{.TableNameCamelFCL}}CacheManager) executeConditionQueryWithSingleflight(ctx context.Context, key string, cacheKey string, queryFunc func() (*model.{{.TableNameCamel}}, error)) (*model.{{.TableNameCamel}}, error) {
	val, sfErr, _ := m.sfg.Do("one_condition:"+key, func() (interface{}, error) {
		record, dbErr := queryFunc()
		if dbErr != nil {
			// 设置占位符缓存防止缓存穿透
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				if placeholderErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); placeholderErr != nil {
					logger.WarnWithCtx(ctx, "cache.SetPlaceholderByKey error", logger.Err(placeholderErr), logger.Any("key", cacheKey))
				}
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}

		// 如果记录存在，缓存结果
		m.cacheConditionResult(ctx, cacheKey, record)
		return record, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	record, ok := val.(*model.{{.TableNameCamel}})
	if !ok {
		return nil, database.ErrRecordNotFound
	}
	return record, nil
}

// getCondition 通过条件获取单条记录
// 错误处理原则：
//   - 缓存读取错误：仅记录日志，回退到数据库查询
//   - 数据库错误：必须返回给调用方
//   - 缓存写入错误：仅记录日志，不影响返回值
func (m *{{.TableNameCamelFCL}}CacheManager) getCondition(ctx context.Context, key string, queryFunc func() (*model.{{.TableNameCamel}}, error)) (*model.{{.TableNameCamel}}, error) {
	cacheKey := m.getConditionCacheKey(key)

	// 先尝试从缓存获取 ID
	cachedID, err := m.cache.GetIDByKey(ctx, cacheKey)
	if err == nil && cachedID != 0 {
		// 尝试从缓存命中获取结果
		if record, hit, hitErr := m.handleConditionCacheHit(ctx, cacheKey, cachedID); hit {
			return record, hitErr
		}
		// 回退到直接查询
	}

	// 缓存未命中或通过 ID 获取失败，从数据库获取
	if errors.Is(err, database.ErrCacheNotFound) || cachedID == 0 {
		return m.executeConditionQueryWithSingleflight(ctx, key, cacheKey, queryFunc)
	}

	// 其他缓存错误（如 Redis 连接失败等），仅记录日志并回退到数据库查询
	logger.WarnWithCtx(ctx, "cache.GetIDByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey))

	// 如果是占位符错误，返回记录未找到
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}

	// 回退到数据库查询
	return m.executeConditionQueryWithSingleflight(ctx, key, cacheKey, queryFunc)
}

// getByCondition 通过条件获取 ID 列表
// 错误处理原则：
//   - 缓存读取错误：仅记录日志，回退到数据库查询
//   - 数据库错误：必须返回给调用方
//   - 缓存写入错误：仅记录日志，不影响返回值
func (m *{{.TableNameCamelFCL}}CacheManager) getByCondition(ctx context.Context, key string, queryFunc func() ([]uint64, error)) ([]uint64, error) {
	cacheKey := m.getConditionCacheKey(key)

	// 先从缓存获取
	ids, err := m.cache.GetIDsByKey(ctx, cacheKey)
	if err == nil {
		// 检查 ID 列表大小，如果过大则不使用缓存，直接查询数据库
		if len(ids) > consts.DaoMaxCacheableIDs {
			logger.WarnWithCtx(ctx, "cached id list too large, querying database directly", logger.Any("count", len(ids)), logger.Any("key", key))
			return queryFunc()
		}
		return ids, nil
	}

	// 缓存未命中，从数据库获取
	if errors.Is(err, database.ErrCacheNotFound) {
		// 使用 singleflight 防止并发请求同时访问数据库
		val, sfErr, _ := m.sfg.Do("ids_condition:"+key, func() (interface{}, error) {
			result, dbErr := queryFunc()
			if dbErr != nil {
				// 设置占位符缓存防止缓存穿透
				if placeholderErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); placeholderErr != nil {
					logger.WarnWithCtx(ctx, "cache.SetPlaceholderByKey error", logger.Err(placeholderErr), logger.Any("key", cacheKey))
				}
				return nil, dbErr
			}

			// 对于大数据量的结果集，不进行缓存，直接返回
			if len(result) > consts.DaoMaxCacheableIDs {
				logger.WarnWithCtx(ctx, "result set too large to cache", logger.Any("count", len(result)), logger.Any("key", key))
				return result, nil
			}

			// 设置缓存（使用随机化过期时间）
			expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
			if cacheErr := m.cache.SetIDsByKey(ctx, cacheKey, result, expireTime); cacheErr != nil {
				logger.WarnWithCtx(ctx, "cache.SetIDsByKey error", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("ids", result))
			}
			return result, nil
		})
		if sfErr != nil {
			return nil, sfErr
		}
		result, ok := val.([]uint64)
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return result, nil
	}

	// 其他缓存错误（如 Redis 连接失败等），仅记录日志并回退到数据库查询
	logger.WarnWithCtx(ctx, "cache.GetIDsByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey))

	// 如果是占位符错误，返回记录未找到
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}

	// 回退到数据库查询
	val, sfErr, _ := m.sfg.Do("ids_condition:"+key, func() (interface{}, error) {
		result, dbErr := queryFunc()
		if dbErr != nil {
			return nil, dbErr
		}

		// 对于大数据量的结果集，不进行缓存，直接返回
		if len(result) > consts.DaoMaxCacheableIDs {
			logger.WarnWithCtx(ctx, "result set too large to cache (fallback)", logger.Any("count", len(result)), logger.Any("key", key))
			return result, nil
		}

		// 尝试设置缓存（失败仅记录日志）
		expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
		if cacheErr := m.cache.SetIDsByKey(ctx, cacheKey, result, expireTime); cacheErr != nil {
			logger.WarnWithCtx(ctx, "cache.SetIDsByKey error after fallback", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("ids", result))
		}
		return result, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	result, ok := val.([]uint64)
	if !ok {
		return nil, database.ErrRecordNotFound
	}
	return result, nil
}

// getByIDs 批量获取记录
func (m *{{.TableNameCamelFCL}}CacheManager) getByIDs(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*model.{{.TableNameCamel}}, error)) (map[uint64]*model.{{.TableNameCamel}}, error) {
	// 对于大数据量请求，分批处理以避免内存峰值
	if len(ids) > consts.DaoMaxBatchSize {
		result := make(map[uint64]*model.{{.TableNameCamel}})
		// 分批处理，每批 consts.DaoMaxBatchSize 个 ID
		for i := 0; i < len(ids); i += consts.DaoMaxBatchSize {
			end := i + consts.DaoMaxBatchSize
			if end > len(ids) {
				end = len(ids)
			}

			batch := ids[i:end]
			batchResult, err := m.getByIDsBatch(ctx, batch, queryFunc)
			if err != nil {
				return nil, err
			}

			// 合并结果
			for id, record := range batchResult {
				result[id] = record
			}
		}
		return result, nil
	}

	// 小数据量直接处理
	return m.getByIDsBatch(ctx, ids, queryFunc)
}

// findMissedIDs 查找缓存未命中的 ID
func findMissedIDs(ids []uint64, itemMap map[uint64]*model.{{.TableNameCamel}}) []uint64 {
	var missedIDs []uint64
	for _, id := range ids {
		if _, ok := itemMap[id]; !ok {
			missedIDs = append(missedIDs, id)
		}
	}
	return missedIDs
}

// setPlaceholdersForMissingRecords 为数据库中不存在的记录设置占位符
func (m *{{.TableNameCamelFCL}}CacheManager) setPlaceholdersForMissingRecords(ctx context.Context, records []*model.{{.TableNameCamel}}, missedIDs []uint64) {
	existingIDs := make(map[uint64]bool)
	for _, record := range records {
		existingIDs[record.ID] = true
	}

	for _, id := range missedIDs {
		if !existingIDs[id] {
			if placeholderErr := m.cache.SetPlaceholder(ctx, id); placeholderErr != nil {
				logger.WarnWithCtx(ctx, "cache.SetPlaceholder error", logger.Err(placeholderErr), logger.Any("id", id))
			}
		}
	}
}

// getByIDsBatch 批量获取记录的实际实现
// 错误处理原则：
//   - 缓存读取错误：仅记录日志，回退到数据库查询
//   - 数据库错误：必须返回给调用方
//   - 缓存写入错误：仅记录日志，不影响返回值
func (m *{{.TableNameCamelFCL}}CacheManager) getByIDsBatch(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*model.{{.TableNameCamel}}, error)) (map[uint64]*model.{{.TableNameCamel}}, error) {
	// 先从缓存获取
	itemMap, err := m.cache.MultiGet(ctx, ids)
	if err != nil {
		// 缓存错误时，记录日志并回退到直接数据库查询
		logger.WarnWithCtx(ctx, "cache.MultiGet error, falling back to database", logger.Err(err), logger.Any("ids", ids))
		// 返回空 map，让后续逻辑从数据库获取
		itemMap = make(map[uint64]*model.{{.TableNameCamel}})
	}

	// 查找未命中的 ID
	missedIDs := findMissedIDs(ids, itemMap)

	// 获取未命中的数据
	if len(missedIDs) > 0 {
		// 从数据库获取未命中的数据（MultiGet 已经过滤了占位符，直接查询即可）
		records, err := queryFunc(missedIDs)
		if err != nil {
			return nil, err
		}

		if len(records) > 0 {
			// 添加到结果映射中
			for _, record := range records {
				itemMap[record.ID] = record
			}
			// 批量设置缓存（使用随机化过期时间）
			expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
			if cacheErr := m.cache.MultiSet(ctx, records, expireTime); cacheErr != nil {
				logger.WarnWithCtx(ctx, "cache.MultiSet error", logger.Err(cacheErr), logger.Any("ids", missedIDs))
			}
		}

		// 对于数据库中也不存在的记录，设置占位符
		if len(records) < len(missedIDs) {
			m.setPlaceholdersForMissingRecords(ctx, records, missedIDs)
		}
	}

	return itemMap, nil
}

type {{.TableNameCamelFCL}}Dao struct {
	db           *gorm.DB
	cache        cache.{{.TableNameCamel}}Cache   // if nil, the cache is not used.
	cacheManager *{{.TableNameCamelFCL}}CacheManager // 缓存管理器
	sfg          *singleflight.Group      // if cache is nil, the sfg is not used.
}

// New{{.TableNameCamel}}Dao creating the dao interface
func New{{.TableNameCamel}}Dao(db *gorm.DB, xCache cache.{{.TableNameCamel}}Cache) {{.TableNameCamel}}Dao {
	dao := &{{.TableNameCamelFCL}}Dao{
		db:    db,
		cache: xCache,
		sfg:   new(singleflight.Group),
	}

	if xCache != nil {
		dao.cacheManager = new{{.TableNameCamel}}CacheManager(xCache)
	}

	return dao
}

func (d *{{.TableNameCamelFCL}}Dao) Create(ctx context.Context, table *model.{{.TableNameCamel}}) error {
	defer func() {
		// 创建操作只清除条件查询缓存，保留单条记录缓存
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "Create: failed to delete condition cache", logger.Err(err))
		}
	}()
	return d.db.WithContext(ctx).Create(table).Error
}
func (d *{{.TableNameCamelFCL}}Dao) CreateInBatches(ctx context.Context, tables []*model.{{.TableNameCamel}}, batchSize int) error {
	defer func() {
		// 批量创建操作只清除条件查询缓存
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "CreateInBatches: failed to delete condition cache", logger.Err(err))
		}
	}()
	return d.db.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}
func (d *{{.TableNameCamelFCL}}Dao) CreateByTx(ctx context.Context, tx *gorm.DB, table *model.{{.TableNameCamel}}) (uint64, error) {
	defer func() {
		// 事务创建操作只清除条件查询缓存
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "CreateByTx: failed to delete condition cache", logger.Err(err))
		}
	}()
	err := tx.WithContext(ctx).Create(table).Error
	return table.ID, err
}
func (d *{{.TableNameCamelFCL}}Dao) CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, tables []*model.{{.TableNameCamel}}, batchSize int) error {
	defer func() {
		// 事务批量创建操作只清除条件查询缓存
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "CreateByInBatchesTx: failed to delete condition cache", logger.Err(err))
		}
	}()
	return tx.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}

// deleteCache 删除缓存的统一入口，封装错误处理
// deleteType 支持：
//   - consts.DaoDeleteTypeSingle: 删除单个 ID 缓存
//   - consts.DaoDeleteTypeCondition: 删除条件查询缓存（包括 condition、columns、count、exists）
//   - consts.DaoDeleteTypeAll: 删除所有缓存（最彻底，谨慎使用）
func (d *{{.TableNameCamelFCL}}Dao) deleteCache(ctx context.Context, id uint64, deleteType string) error {
	if d.cache == nil {
		return nil
	}

	var errs []error
	switch deleteType {
	case consts.DaoDeleteTypeSingle:
		// 删除单个 ID 缓存
		if err := d.cache.Del(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("delete single cache failed: %w", err))
		}
	case consts.DaoDeleteTypeCondition:
		// 删除条件查询相关的所有缓存（包括 condition、columns、count、exists）
		prefixes := []string{
			cache.{{.TableNameCamel}}CachePrefixKey + "condition",
			cache.{{.TableNameCamel}}CachePrefixKey + "columns",
			cache.{{.TableNameCamel}}CachePrefixKey + "count",
			cache.{{.TableNameCamel}}CachePrefixKey + "exists",
		}
		for _, prefix := range prefixes {
			if err := d.cache.DelByPrefix(ctx, prefix); err != nil {
				errs = append(errs, fmt.Errorf("delete %s cache failed: %w", prefix, err))
			}
		}
	case consts.DaoDeleteTypeAll:
		// 删除所有缓存（最彻底）：使用统一的前缀删除
		// 注意：会删除单条记录、条件查询、分页查询等所有缓存，仅在全量更新时使用
		if err := d.cache.DelByPrefix(ctx, cache.{{.TableNameCamel}}CachePrefixKey); err != nil {
			errs = append(errs, fmt.Errorf("delete all cache failed: %w", err))
		}
	default:
		return nil
	}

	// 记录缓存删除错误，但不影响主流程
	if len(errs) > 0 {
		for _, err := range errs {
			logger.WarnWithCtx(ctx, "cache: failed to delete",
				logger.String("type", deleteType),
				logger.Any("id", id),
				logger.Err(err))
		}
		// 返回第一个错误，供调用方参考
		return errs[0]
	}
	return nil
}

// executeDelayedDelete 执行延迟删除逻辑
func (d *{{.TableNameCamelFCL}}Dao) executeDelayedDelete(ctx context.Context, id uint64, ids []uint64, deleteType string) {
	// 单个 ID 删除
	if id > 0 {
		if err := d.deleteCache(ctx, id, consts.DaoDeleteTypeSingle); err != nil {
			logger.WarnWithCtx(ctx, "delayedDoubleDelete: failed to delete single cache",
				logger.Err(err),
				logger.Any("id", id))
		}
	}

	// 聚合批量删除失败的 ID，避免日志刷屏
	var failedIDs []uint64
	var firstErr error
	for _, batchID := range ids {
		if err := d.deleteCache(ctx, batchID, consts.DaoDeleteTypeSingle); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failedIDs = append(failedIDs, batchID)
		}
	}
	if len(failedIDs) > 0 {
		logger.WarnWithCtx(ctx, "delayedDoubleDelete: failed to delete batch cache",
			logger.Err(firstErr),
			logger.Any("failed_ids", failedIDs),
			logger.Int("total_failed", len(failedIDs)))
	}

	// 按条件删除（根据 deleteType 参数决定删除范围）
	switch deleteType {
	case consts.DaoDeleteTypeAll:
		// 删除所有缓存（包括 single、condition、columns、count、exists）
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
			logger.WarnWithCtx(ctx, "delayedDoubleDelete: failed to delete all cache",
				logger.Err(err),
				logger.String("type", consts.DaoDeleteTypeAll))
		}
	case consts.DaoDeleteTypeCondition:
		// 只删除条件缓存（condition、columns、count、exists）
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "delayedDoubleDelete: failed to delete condition cache",
				logger.Err(err),
				logger.String("type", consts.DaoDeleteTypeCondition))
		}
	}
}

// delayedDoubleDelete 延迟双删缓存，确保缓存一致性
// 参数：
//   - ctx: 背景上下文（应使用 context.Background() 避免受原始请求影响）
//   - id: 记录 ID（0 表示不按 ID 删除）
//   - ids: 批量 ID 列表（nil 表示不批量删除）
//   - deleteType: 删除类型（"condition"=只删除条件缓存，"all"=删除所有缓存）
func (d *{{.TableNameCamelFCL}}Dao) delayedDoubleDelete(ctx context.Context, id uint64, ids []uint64, deleteType string) {
	if d.cache == nil {
		return
	}

	go func() {
		// panic 保护，防止异步 goroutine 崩溃
		defer func() {
			if r := recover(); r != nil {
				logger.WarnWithCtx(ctx, "delayedDoubleDelete panic recovered",
					logger.Any("recover", r),
					logger.Any("id", id),
					logger.String("deleteType", deleteType))
			}
		}()

		// 使用 select 监听 ctx.Done()，避免 ctx 被取消后 goroutine 仍在睡眠
		// 高并发场景下防止 goroutine 堆积
		select {
		case <-time.After(consts.DaoDelayedDeleteInterval):
			// 延迟 consts.DaoDelayedDeleteInterval 后再次删除缓存，防止主从同步延迟导致的脏数据
			d.executeDelayedDelete(ctx, id, ids, deleteType)
		case <-ctx.Done():
			// ctx 被取消或超时，直接退出 goroutine
			logger.InfoWithCtx(ctx, "delayedDoubleDelete: context canceled before sleep completed",
				logger.Any("id", id),
				logger.String("deleteType", deleteType),
				logger.Err(ctx.Err()))
			return
		}
	}()
}

func (d *{{.TableNameCamelFCL}}Dao) DeleteByID(ctx context.Context, id uint64) error {
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	if err := d.deleteCache(ctx, id, consts.DaoDeleteTypeSingle); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete single cache failed", logger.Err(err), logger.Any("id", id))
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete condition cache failed", logger.Err(err), logger.Any("id", id))
	}

	// 执行数据库删除
	err := d.db.WithContext(ctx).Where("id = ?", id).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByID: delete from database failed, id=%d: %w", id, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	})), id, nil, consts.DaoDeleteTypeCondition)
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByIDs(ctx context.Context, ids []uint64) error {
	// 先删除缓存（第一次删除）
	// 聚合批量删除失败的 ID，避免日志刷屏
	var failedIDs []uint64
	var firstErr error
	for _, id := range ids {
		if err := d.deleteCache(ctx, id, consts.DaoDeleteTypeSingle); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failedIDs = append(failedIDs, id)
		}
	}
	if len(failedIDs) > 0 {
		logger.WarnWithCtx(ctx, "pre-delete batch single cache failed",
			logger.Err(firstErr),
			logger.Any("failed_ids", failedIDs),
			logger.Int("total_failed", len(failedIDs)),
			logger.Any("ids", ids))
	} else {
		// 全部成功时记录调试日志
		logger.DebugWithCtx(ctx, "pre-delete batch single cache success",
			logger.Any("count", len(ids)))
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete condition cache failed", logger.Err(err), logger.Any("ids", ids))
	}

	// 执行数据库删除
	err := d.db.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByIDs: delete from database failed, ids=%v: %w", ids, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只批量删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	})), 0, ids, consts.DaoDeleteTypeCondition)
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByCondition(ctx context.Context, c *query.Conditions) error {
	// 先删除缓存（第一次删除）
	// 按条件删除会影响多条记录，需要删除所有相关缓存：
	// 1. condition: 条件查询缓存
	// 2. columns: 分页查询缓存
	// 3. count: 计数缓存
	// 4. exists: 存在性检查缓存
	// 5. single: 单条记录缓存（防止删除后读到已删除的数据）
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete all cache failed", logger.Err(err))
	}

	// 构建查询条件
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("DeleteByCondition: convert conditions to gorm failed: %w", err)
	}

	// 执行数据库删除
	err = d.db.WithContext(ctx).Where(queryStr, args...).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByCondition: delete from database failed, query=%s, args=%v: %w", queryStr, args, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有缓存，防止主从同步延迟
	// 注意：必须删除所有类型缓存（包括 single），因为首次删除后可能有读请求从从库读到旧数据并回填
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	})), 0, nil, consts.DaoDeleteTypeAll)
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByTx(ctx context.Context, tx *gorm.DB, id uint64) error {
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	if err := d.deleteCache(ctx, id, consts.DaoDeleteTypeSingle); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete single cache failed", logger.Err(err), logger.Any("id", id))
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete condition cache failed", logger.Err(err), logger.Any("id", id))
	}

	// 执行数据库软删除
	update := map[string]interface{}{
		"deleted_at": time.Now(),
	}
	err := tx.WithContext(ctx).Model(&model.{{.TableNameCamel}}{}).Where("id = ?", id).Updates(update).Error
	if err != nil {
		return fmt.Errorf("DeleteByTx: soft delete failed, id=%d: %w", id, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	})), id, nil, consts.DaoDeleteTypeCondition)
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) error {
	// 先删除缓存（第一次删除）
	// 聚合批量删除失败的 ID，避免日志刷屏
	var failedIDs []uint64
	var firstErr error
	for _, id := range ids {
		if err := d.deleteCache(ctx, id, consts.DaoDeleteTypeSingle); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failedIDs = append(failedIDs, id)
		}
	}
	if len(failedIDs) > 0 {
		logger.WarnWithCtx(ctx, "pre-delete batch single cache failed",
			logger.Err(firstErr),
			logger.Any("failed_ids", failedIDs),
			logger.Int("total_failed", len(failedIDs)),
			logger.Any("ids", ids))
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete condition cache failed", logger.Err(err), logger.Any("ids", ids))
	}

	// 执行数据库删除
	err := tx.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByIDsTx: delete from database failed, ids=%v: %w", ids, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只批量删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	})), 0, ids, consts.DaoDeleteTypeCondition)
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error {
	// 先删除缓存（第一次删除）
	// 按条件删除会影响多条记录，需要删除所有相关缓存：
	// 1. condition: 条件查询缓存
	// 2. columns: 分页查询缓存
	// 3. count: 计数缓存
	// 4. exists: 存在性检查缓存
	// 5. single: 单条记录缓存（防止删除后读到已删除的数据）
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete all cache failed", logger.Err(err))
	}

	// 构建查询条件
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("DeleteByTxCondition: convert conditions to gorm failed: %w", err)
	}

	// 执行数据库删除
	err = tx.WithContext(ctx).Where(queryStr, args...).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByTxCondition: delete from database failed, query=%s, args=%v: %w", queryStr, args, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有缓存，防止主从同步延迟
	// 注意：必须删除所有类型缓存（包括 single），因为首次删除后可能有读请求从从库读到旧数据并回填
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	})), 0, nil, consts.DaoDeleteTypeAll)
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) ClearCache(ctx context.Context) error {
	if d.cache != nil {
		return d.cache.DelByPrefix(ctx, cache.{{.TableNameCamel}}CachePrefixKey)
	}
	return nil
}

func (d *{{.TableNameCamelFCL}}Dao) updateDataByID(db *gorm.DB, table *model.{{.TableNameCamel}}) error {
	if table.ID < 1 {
		return errors.New("id cannot be 0")
	}

	update := map[string]interface{}{}
	// todo generate the update fields code to here

	return db.Model(table).Updates(update).Error
}
func (d *{{.TableNameCamelFCL}}Dao) UpdateByID(ctx context.Context, table *model.{{.TableNameCamel}}) error {
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	if err := d.deleteCache(ctx, table.ID, consts.DaoDeleteTypeSingle); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete single cache failed", logger.Err(err), logger.Any("id", table.ID))
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete condition cache failed", logger.Err(err), logger.Any("id", table.ID))
	}

	// 执行数据库更新
	err := d.updateDataByID(d.db.WithContext(ctx), table)
	if err != nil {
		return fmt.Errorf("UpdateByID: update database failed, id=%d: %w", table.ID, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	})), table.ID, nil, "condition")
	return nil
}
// buildDelayedDeleteContext 构建用于延迟删除的背景上下文
func (d *{{.TableNameCamelFCL}}Dao) buildDelayedDeleteContext(ctx context.Context) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	}))
}

// executeUpdateByCondition 执行按条件更新的核心逻辑
func (d *{{.TableNameCamelFCL}}Dao) executeUpdateByCondition(ctx context.Context, db *gorm.DB, c *query.Conditions, update map[string]interface{}) error {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("convert conditions to gorm failed: %w", err)
	}

	err = db.WithContext(ctx).Model(&model.{{.TableNameCamel}}{}).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return fmt.Errorf("update database failed, query=%s, args=%v: %w", queryStr, args, err)
	}

	return nil
}

// UpdateByCondition 根据条件更新记录
//
//nolint:revive // table 参数用于自动生成代码，在 // todo generate the update fields code to here 位置使用
func (d *{{.TableNameCamelFCL}}Dao) UpdateByCondition(ctx context.Context, c *query.Conditions, table *model.{{.TableNameCamel}}) error {
	// 先删除所有缓存（第一次删除）
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete all cache failed", logger.Err(err))
	}

	// 构建更新映射
	update := map[string]interface{}{}
	// todo generate the update fields code to here

	// 执行数据库更新
	if err := d.executeUpdateByCondition(ctx, d.db, c, update); err != nil {
		return fmt.Errorf("UpdateByCondition: %w", err)
	}

	// 延迟双删（第二次删除）
	d.delayedDoubleDelete(d.buildDelayedDeleteContext(ctx), 0, nil, consts.DaoDeleteTypeAll)
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) UpdateByTx(ctx context.Context, tx *gorm.DB, table *model.{{.TableNameCamel}}) error {
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	if err := d.deleteCache(ctx, table.ID, consts.DaoDeleteTypeSingle); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete single cache failed", logger.Err(err), logger.Any("id", table.ID))
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete condition cache failed", logger.Err(err), logger.Any("id", table.ID))
	}

	// 执行数据库更新
	err := d.updateDataByID(tx.WithContext(ctx), table)
	if err != nil {
		return fmt.Errorf("UpdateByTx: update database failed, id=%d: %w", table.ID, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	})), table.ID, nil, "condition")
	return nil
}
// UpdateByConditionTx 在事务中根据条件更新记录
//
//nolint:revive // table 参数用于自动生成代码，在 // todo generate the update fields code to here 位置使用
func (d *{{.TableNameCamelFCL}}Dao) UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, table *model.{{.TableNameCamel}}) error {
	// 先删除所有缓存（第一次删除）
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete all cache failed", logger.Err(err))
	}

	// 构建更新映射
	update := map[string]interface{}{}
	// todo generate the update fields code to here

	// 执行数据库更新
	if err := d.executeUpdateByCondition(ctx, tx, c, update); err != nil {
		return fmt.Errorf("UpdateByConditionTx: %w", err)
	}

	// 延迟双删（第二次删除）
	d.delayedDoubleDelete(d.buildDelayedDeleteContext(ctx), 0, nil, consts.DaoDeleteTypeAll)
	return nil
}

// ExecByCustomFunc 执行自定义更新操作，接受一个函数参数来执行自定义的更新、插入或其他数据库操作
// 使用示例：事务
//	err = s.iCpDealerDao.ExecByCustomFunc(ctx, func(db *gorm.DB) *gorm.DB {
//		err := db.Transaction(func(tx *gorm.DB) error {
//			// 执行操作
//			if err := tx.Model(&model.CpDealer{}).Where("id = ?", 1).Update("name", "前端测试商户").Error; err != nil {
//				return err
//			}
//			if err := tx.Model(&model.CpDealerOrder{}).Where("id = ?", 647401).Update("dealer_name", "前端测试商户").Error; err != nil {
//				return err
//			}
//			// 成功提交事务
//			return nil
//		})
//		// 将事务中的错误传递出去
//		if err != nil {
//			db.Error = err
//		}
//		return db
//	})
// 普通更新
// err = s.iCpDealerDao.ExecByCustomFunc(ctx, func(db *gorm.DB) *gorm.DB {
//
//	if err := db.Model(&model.CpDealer{}).Where("id = ?", 1).Update("name", "前端测试商户 1").Error; err != nil {
//		db.Error = err
//		return db
//	}
//
//	if err := db.Model(&model.CpDealerOrder{}).Where("id = ?", 647401).Update("dealer_name", "前端测试商户").Error; err != nil {
//		db.Error = err
//		return db
//	}
//
//	return db
//})

func (d *{{.TableNameCamelFCL}}Dao) ExecByCustomFunc(ctx context.Context, updateFunc func(*gorm.DB) *gorm.DB) error {
	// 先清除所有缓存（自定义函数可能影响任意数据，必须全量清理）
	// 包括 single、condition、columns、count、exists
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
		logger.WarnWithCtx(ctx, "ExecByCustomFunc: failed to delete all cache", logger.Err(err))
	}

	db := d.db.WithContext(ctx)
	// 应用自定义更新函数
	db = updateFunc(db)

	// 执行更新操作
	var err error
	if db.Statement != nil && db.Statement.SQL.Len() > 0 {
		// 对于原始 SQL 查询，直接执行
		err = db.Exec(db.Statement.SQL.String(), db.Statement.Vars...).Error
	} else {
		// 对于常规查询，执行操作
		err = db.Error
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有缓存，防止主从同步延迟
	// 注意：自定义函数可能影响任意数据，必须删除所有类型缓存
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		string(logger.ContextKeyRequestID): interceptor.CtxRequestIDField(ctx).String,
	})), 0, nil, consts.DaoDeleteTypeAll)

	return err
}

func (d *{{.TableNameCamelFCL}}Dao) GetByID(ctx context.Context, id uint64, opts ...{{.TableNameCamel}}QueryOption) (*model.{{.TableNameCamel}}, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)
	// 无缓存模式直接查询（支持强制主库查询）
	if d.cacheManager == nil {
		record := &model.{{.TableNameCamel}}{}
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id = ?", id).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, fmt.Errorf("GetByID: query database failed, id=%d: %w", id, err)
		}
		return record, nil
	}

	// 使用缓存管理器获取数据（包含 singleflight、防击穿、防穿透机制）
	return d.cacheManager.get(ctx, id, func() (*model.{{.TableNameCamel}}, error) {
		table := &model.{{.TableNameCamel}}{}
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id = ?", id).First(table).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, fmt.Errorf("GetByID: query database failed, id=%d: %w", id, err)
		}
		return table, nil
	})
}

// queryByColumnsWithDB 执行基于列条件的数据库分页查询
// 参数:
//   - db: GORM 数据库实例
//   - params: 查询参数（包含分页、排序信息）
//   - queryStr: GORM 查询条件字符串
//   - args: 查询参数值
//
// 返回:
//   - records: 查询结果集
//   - total: 符合条件的总记录数
//   - error: 查询错误
func (d *{{.TableNameCamelFCL}}Dao) queryByColumnsWithDB(db *gorm.DB, params *query.Params, queryStr string, args []interface{}) (interface{}, error) {
	var total int64
	var records []*model.{{.TableNameCamel}}
	// 统计总数（若需要）
	if params.Sort != consts.DaoSortIgnoreCount {
		err := db.Model(&model.{{.TableNameCamel}}{}).Where(queryStr, args...).Count(&total).Error
		if err != nil {
			return nil, err
		}
		if total == 0 {
			return struct {
				records []*model.{{.TableNameCamel}}
				total   int64
			}{records: []*model.{{.TableNameCamel}}{}, total: 0}, nil
		}
	}

	// 分页查询
	order, limit, offset := params.ConvertToPage()
	err := db.Order(order).Limit(limit).Offset(offset).Where(queryStr, args...).Find(&records).Error
	if err != nil {
		return nil, err
	}

	return struct {
		records []*model.{{.TableNameCamel}}
		total   int64
	}{records: records, total: total}, nil
}

// GetByColumns 根据列信息进行分页查询
// 注意：
//   - 使用 OFFSET 分页，当页码较大时（如 page > 1000）查询性能会下降
//   - 对于深度分页场景，建议使用基于游标的分页方式（Cursor-based Pagination）
//   - 结果集过大时（>1000 条）不会缓存到 Redis，避免内存压力
//
// params 包含查询参数和分页参数（必需）:
//   - page: 页码，从 0 开始
//   - limit: 每页行数
//   - sort: 排序字段，默认 id 倒序，可在字段前加 - 表示倒序，不加表示正序，多个字段用逗号分隔
//
// 查询参数（可选）:
//   - name: 列名
//   - exp: 表达式，默认为 "=", 支持 =, !=, >, >=, <, <=, like, in, notin, isnull, isnotnull
//   - value: 列值，如果 exp=in 则多个值用逗号分隔
//   - logic: 逻辑类型，value 为 nil 时默认为 and，仅支持 &(and), ||(or)
//
// 示例：搜索年龄大于 20 的男性
//
//	params = &query.Params{
//	    Page: 0,
//	    Limit: 20,
//	    Columns: []query.Column{
//		{
//			Name:    "age",
//			Exp: ">",
//			Value:   20,
//		},
//		{
//			Name:  "gender",
//			Value: "male",
//		},
//	}
//

// handleColumnsCacheHit 处理分页查询缓存命中的情况
// 从缓存中获取总数和 ID 列表，然后通过 ID 批量获取完整记录
func (d *{{.TableNameCamelFCL}}Dao) handleColumnsCacheHit(ctx context.Context, fullCacheKey string, optsConfig *{{.TableNameCamel}}QueryOptions, cachedTotal uint64) (interface{}, bool, error) {
	ids, idsErr := d.cache.GetIDsByKey(ctx, fullCacheKey+":ids")
	if idsErr != nil || len(ids) == 0 {
		return nil, false, nil // 缓存未完全命中
	}

	// 通过 ID 批量获取记录（利用已有的缓存机制）
	recordsMap, getErr := d.cacheManager.getByIDs(ctx, ids, func(missedIDs []uint64) ([]*model.{{.TableNameCamel}}, error) {
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		var records []*model.{{.TableNameCamel}}
		dbErr := db.Where("id IN (?)", missedIDs).Find(&records).Error
		return records, dbErr
	})
	if getErr != nil || len(recordsMap) == 0 {
		return nil, false, nil // 获取失败，回退到数据库查询
	}

	// 按 ID 顺序返回结果
	records := make([]*model.{{.TableNameCamel}}, 0, len(ids))
	for _, id := range ids {
		if record, ok := recordsMap[id]; ok {
			records = append(records, record)
		}
	}
	return struct {
		records []*model.{{.TableNameCamel}}
		total   int64
	}{records: records, total: int64(cachedTotal)}, true, nil
}

// cacheColumnsResult 缓存分页查询结果
// 包括总数、ID 列表和单条记录
func (d *{{.TableNameCamelFCL}}Dao) cacheColumnsResult(ctx context.Context, fullCacheKey string, res struct {
	records []*model.{{.TableNameCamel}}
	total   int64
}, _ string) {
	if len(res.records) > consts.DaoMaxCacheableRecords || res.total <= 0 {
		return // 数据量过大或无数据，不缓存
	}

	// 生成随机化过期时间
	expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)

	// 缓存总数
	if setErr := d.cache.SetIDByKey(ctx, fullCacheKey+":total", uint64(res.total), expireTime); setErr != nil {
		logger.WarnWithCtx(ctx, "cache: failed to set total count",
			logger.Err(setErr),
			logger.String("key", fullCacheKey+":total"),
			logger.Uint64("total", uint64(res.total)))
	}

	// 提取并缓存 ID 列表
	ids := make([]uint64, 0, len(res.records))
	for _, record := range res.records {
		ids = append(ids, record.ID)
	}
	if setErr := d.cache.SetIDsByKey(ctx, fullCacheKey+":ids", ids, expireTime); setErr != nil {
		logger.WarnWithCtx(ctx, "cache: failed to set ID list",
			logger.Err(setErr),
			logger.String("key", fullCacheKey+":ids"),
			logger.Int("id_count", len(ids)))
	}

	// 同时缓存单条记录
	if setErr := d.cache.MultiSet(ctx, res.records, expireTime); setErr != nil {
		logger.WarnWithCtx(ctx, "cache: failed to multi-set records",
			logger.Err(setErr),
			logger.Any("count", len(res.records)),
			logger.Int("total_records", len(res.records)))
	}
}

// queryColumnsWithoutCache 无缓存模式查询
func (d *{{.TableNameCamelFCL}}Dao) queryColumnsWithoutCache(ctx context.Context, singleflightKey string, params *query.Params, queryStr string, args []interface{}, optsConfig *{{.TableNameCamel}}QueryOptions) ([]*model.{{.TableNameCamel}}, int64, error) {
	val, sfErr, _ := d.sfg.Do(singleflightKey, func() (interface{}, error) {
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		return d.queryByColumnsWithDB(db, params, queryStr, args)
	})
	if sfErr != nil {
		return nil, 0, sfErr
	}
	columnsResult, ok := val.(struct {
		records []*model.{{.TableNameCamel}}
		total   int64
	})
	if !ok {
		return nil, 0, errors.New("type assertion failed: expected columns result struct")
	}

	// 大数据量警告
	if len(columnsResult.records) > consts.DaoMaxCacheableRecords {
		logger.WarnWithCtx(ctx, "GetByColumns: result set too large",
			logger.Any("count", len(columnsResult.records)),
			logger.String("cache_key", gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%d_%d_%s", queryStr, args, params.Page, params.Limit, params.Sort)))))
	}
	return columnsResult.records, columnsResult.total, nil
}

// executeColumnsQueryWithCache 使用缓存执行查询
func (d *{{.TableNameCamelFCL}}Dao) executeColumnsQueryWithCache(ctx context.Context, singleflightKey, fullCacheKey, cacheKey string, params *query.Params, queryStr string, args []interface{}, optsConfig *{{.TableNameCamel}}QueryOptions) (interface{}, error) {
	val, sfErr, _ := d.sfg.Do(singleflightKey, func() (interface{}, error) {
		// 先从缓存获取总数和 ID 列表
		cachedTotal, cacheErr := d.cache.GetIDByKey(ctx, fullCacheKey+":total")
		if cacheErr == nil {
			// 尝试从缓存命中获取结果
			if result, hit, hitErr := d.handleColumnsCacheHit(ctx, fullCacheKey, optsConfig, cachedTotal); hit {
				return result, hitErr
			}
		}

		// 缓存未命中，从数据库查询（支持强制主库查询）
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		result, dbErr := d.queryByColumnsWithDB(db, params, queryStr, args)
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				return struct {
					records []*model.{{.TableNameCamel}}
					total   int64
				}{records: []*model.{{.TableNameCamel}}{}, total: 0}, nil
			}
			return nil, fmt.Errorf("GetByColumns: query database failed, page=%d, limit=%d: %w", params.Page, params.Limit, dbErr)
		}

		// 大数据量警告
		res, ok := result.(struct {
			records []*model.{{.TableNameCamel}}
			total   int64
		})
		if !ok {
			return nil, fmt.Errorf("GetByColumns: query database failed, page=%d, limit=%d: type assertion failed", params.Page, params.Limit)
		}
		if len(res.records) > consts.DaoMaxCacheableRecords {
			logger.WarnWithCtx(ctx, "GetByColumns: result set too large",
				logger.Any("count", len(res.records)),
				logger.String("cache_key", cacheKey))
		}

		// 缓存结果
		d.cacheColumnsResult(ctx, fullCacheKey, res, cacheKey)

		return result, nil
	})
	return val, sfErr
}

func (d *{{.TableNameCamelFCL}}Dao) GetByColumns(ctx context.Context, params *query.Params, opts ...{{.TableNameCamel}}QueryOption) ([]*model.{{.TableNameCamel}}, int64, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)

	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, 0, fmt.Errorf("GetByColumns: convert query conditions failed, params=%+v: %w", params, err)
	}

	// 生成唯一缓存键（必须包含分页和排序信息，避免不同分页/排序共享同一缓存）
	// 格式：queryStr + args + page + limit + sort
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%d_%d_%s", queryStr, args, params.Page, params.Limit, params.Sort)))
	singleflightKey := "columns:" + cacheKey

	var result struct {
		records []*model.{{.TableNameCamel}}
		total   int64
	}

	// 无缓存模式直接查询（支持强制主库查询）
	if d.cacheManager == nil {
		return d.queryColumnsWithoutCache(ctx, singleflightKey, params, queryStr, args, optsConfig)
	}

	// 使用缓存管理器优化查询（增加 singleflight 保护，避免并发穿透）
	fullCacheKey := d.cacheManager.getColumnsCacheKey(cacheKey)

	// 使用 singleflight 包裹整个查询过程，包括缓存读取和数据库查询
	val, sfErr := d.executeColumnsQueryWithCache(ctx, singleflightKey, fullCacheKey, cacheKey, params, queryStr, args, optsConfig)

	// 处理错误情况
	if sfErr != nil {
		if errors.Is(sfErr, gorm.ErrRecordNotFound) {
			return []*model.{{.TableNameCamel}}{}, 0, nil
		}
		return nil, 0, sfErr
	}

	// 类型断言获取查询结果
	result, ok := val.(struct {
		records []*model.{{.TableNameCamel}}
		total   int64
	})
	if !ok {
		return nil, 0, errors.New("type assertion failed: expected columns result struct")
	}

	// 只需要检查大数据量警告即可，不需要再设置缓存
	if len(result.records) > consts.DaoMaxCacheableRecords {
		logger.WarnWithCtx(ctx, "GetByColumns: result set too large",
			logger.Any("count", len(result.records)),
			logger.String("cache_key", cacheKey))
	}

	return result.records, result.total, nil
}

// GetOneByColumns 根据列信息获取单条记录
// 参数同 GetByColumns，返回第一条匹配的记录
func (d *{{.TableNameCamelFCL}}Dao) GetOneByColumns(ctx context.Context, params *query.Params, opts ...{{.TableNameCamel}}QueryOption) (*model.{{.TableNameCamel}}, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, fmt.Errorf("GetOneByColumns: convert query conditions failed, params=%+v: %w", params, err)
	}
	order, _, _ := params.ConvertToPage()

	// 生成唯一缓存键（必须包含排序信息，避免不同排序共享同一缓存）
	// 格式：queryStr + args + sort
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%s", queryStr, args, params.Sort)))

	// no cache（支持强制主库查询）
	if d.cacheManager == nil {
		record := &model.{{.TableNameCamel}}{}
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, fmt.Errorf("GetOneByColumns: query database failed, sort=%s: %w", params.Sort, err)
		}
		return record, nil
	}

	// 使用缓存管理器获取数据（复用单条记录缓存逻辑，支持强制主库查询）
	return d.cacheManager.getCondition(ctx, cacheKey, func() (*model.{{.TableNameCamel}}, error) {
		record := &model.{{.TableNameCamel}}{}
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, fmt.Errorf("GetOneByColumns: query database failed, sort=%s: %w", params.Sort, err)
		}
		return record, nil
	})
}

// GetByCondition 根据条件获取记录 ID 列表
// 注意：当结果集过大时（>consts.DaoMaxCacheableIDs），建议改用分页查询以避免内存压力
//
// 查询条件:
//   - name: 列名
//   - exp: 表达式，默认为 "=", 支持 =, !=, >, >=, <, <=, like, in, notin, isnull, isnotnull
//   - value: 列值，如果 exp=in 则多个值用逗号分隔
//   - logic: 逻辑类型，value 为 nil 时默认为 and，仅支持 &(and), ||(or)
//
// 示例：查找年龄为 20 的男性
//
//	condition = &query.Conditions{
//	    Columns: []query.Column{
//		{
//			Name:    "age",
//			Value:   20,
//		},
//		{
//			Name:  "gender",
//			Value: "male",
//		},
//	}
func (d *{{.TableNameCamelFCL}}Dao) GetByCondition(ctx context.Context, c *query.Conditions, opts ...{{.TableNameCamel}}QueryOption) (ids []uint64, err error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return nil, fmt.Errorf("GetByCondition: convert conditions to gorm failed, conditions=%+v: %w", c, err)
	}

	var tables []*model.{{.TableNameCamel}}
	// 生成唯一缓存键（必须包含 forceMaster 选项，避免不同选项共享同一缓存）
	// 格式：queryStr + args + forceMaster
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v", queryStr, args, optsConfig.forceMaster)))

	// no cache（支持强制主库查询）
	if d.cacheManager == nil {
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Where(queryStr, args...).Find(&tables).Error
		if err != nil {
			return nil, fmt.Errorf("GetByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		// 大数据量警告
		if len(tables) > consts.DaoMaxCacheableIDs {
			logger.WarnWithCtx(ctx, "GetByCondition: result set too large",
				logger.Any("count", len(tables)),
				logger.String("cache_key", cacheKey))
		}
		// 提取 ID 列表
		result := make([]uint64, 0, len(tables))
		for _, table := range tables {
			result = append(result, table.ID)
		}
		return result, nil
	}

	// 使用缓存管理器获取数据（已包含 10000 条限制检查，支持强制主库查询）
	return d.cacheManager.getByCondition(ctx, cacheKey, func() ([]uint64, error) {
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Where(queryStr, args...).Find(&tables).Error
		if err != nil {
			return nil, fmt.Errorf("GetByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		// 提取 ID 列表
		result := make([]uint64, 0, len(tables))
		for _, table := range tables {
			result = append(result, table.ID)
		}
		return result, nil
	})
}

func (d *{{.TableNameCamelFCL}}Dao) GetByIDs(ctx context.Context, ids []uint64, opts ...{{.TableNameCamel}}QueryOption) (map[uint64]*model.{{.TableNameCamel}}, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)
	// 无缓存模式直接查询（支持强制主库查询）
	if d.cacheManager == nil {
		var records []*model.{{.TableNameCamel}}
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id IN (?)", ids).Find(&records).Error
		if err != nil {
			return nil, fmt.Errorf("GetByIDs: query database failed, ids=%v: %w", ids, err)
		}
		itemMap := make(map[uint64]*model.{{.TableNameCamel}}, len(records))
		for _, record := range records {
			itemMap[record.ID] = record
		}
		return itemMap, nil
	}

	// 使用缓存管理器获取数据（支持强制主库查询）
	return d.cacheManager.getByIDs(ctx, ids, func(missedIDs []uint64) ([]*model.{{.TableNameCamel}}, error) {
		var records []*model.{{.TableNameCamel}}
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id IN (?)", missedIDs).Find(&records).Error
		if err != nil {
			return nil, fmt.Errorf("GetByIDs: query database failed, missed_ids=%v: %w", missedIDs, err)
		}
		return records, nil
	})
}

func (d *{{.TableNameCamelFCL}}Dao) CountByCondition(ctx context.Context, c *query.Conditions, opts ...{{.TableNameCamel}}QueryOption) (int64, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return 0, fmt.Errorf("CountByCondition: convert conditions to gorm failed, conditions=%+v: %w", c, err)
	}

	// 生成唯一缓存键（必须包含 forceMaster 选项，避免不同选项共享同一缓存）
	// 格式：queryStr + args + forceMaster
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v", queryStr, args, optsConfig.forceMaster)))
	countCacheKey := "count:" + cacheKey

	// 无缓存模式直接查询（支持强制主库查询）
	if d.cacheManager == nil {
		var count int64
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Model(&model.{{.TableNameCamel}}{}).Where(queryStr, args...).Count(&count).Error
		if err != nil {
			return 0, fmt.Errorf("CountByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		return count, nil
	}

	// 尝试从缓存获取
	cachedCount, err := d.cache.GetIDByKey(ctx, countCacheKey)
	if err == nil {
		return int64(cachedCount), nil
	}

	// 缓存未命中，使用 singleflight 防止并发重复查询（支持强制主库查询）
	val, sfErr, _ := d.sfg.Do(countCacheKey, func() (interface{}, error) {
		var count int64
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Model(&model.{{.TableNameCamel}}{}).Where(queryStr, args...).Count(&count).Error
		if err != nil {
			return 0, fmt.Errorf("CountByCondition: query database failed, conditions=%+v: %w", c, err)
		}

		// 缓存计数结果（包括 0，避免重复查询，使用随机化过期时间）
		expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
		if setErr := d.cache.SetIDByKey(ctx, countCacheKey, uint64(count), expireTime); setErr != nil {
			logger.WarnWithCtx(ctx, "cache: failed to set count", logger.Err(setErr), logger.String("key", countCacheKey))
		}
		return count, nil
	})
	if sfErr != nil {
		return 0, sfErr
	}
	count, ok := val.(int64)
	if !ok {
		return 0, errors.New("type assertion failed: expected count value")
	}
	return count, nil
}

func (d *{{.TableNameCamelFCL}}Dao) ExistsByCondition(ctx context.Context, c *query.Conditions, opts ...{{.TableNameCamel}}QueryOption) (bool, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return false, fmt.Errorf("ExistsByCondition: convert conditions to gorm failed, conditions=%+v: %w", c, err)
	}

	// 生成唯一缓存键（必须包含 forceMaster 选项，避免不同选项共享同一缓存）
	// 格式：queryStr + args + forceMaster
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v", queryStr, args, optsConfig.forceMaster)))
	existsCacheKey := "exists:" + cacheKey

	// 无缓存模式直接查询（支持强制主库查询）
	if d.cacheManager == nil {
		var exists bool
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		// 使用 SELECT 1 LIMIT 1 优化存在性检查，性能优于 COUNT
		err = db.Model(&model.{{.TableNameCamel}}{}).Where(queryStr, args...).Select("1").Limit(1).Scan(&exists).Error
		if err != nil {
			return false, fmt.Errorf("ExistsByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		return exists, nil
	}

	// 尝试从缓存获取
	cachedValue, err := d.cache.GetIDByKey(ctx, existsCacheKey)
	if err == nil {
		return cachedValue > 0, nil
	}

	// 缓存未命中，使用 singleflight 防止并发重复查询（支持强制主库查询）
	val, sfErr, _ := d.sfg.Do(existsCacheKey, func() (interface{}, error) {
		var exists bool
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		// 使用 SELECT 1 LIMIT 1 优化存在性检查
		err = db.Model(&model.{{.TableNameCamel}}{}).Where(queryStr, args...).Select("1").Limit(1).Scan(&exists).Error
		if err != nil {
			return false, fmt.Errorf("ExistsByCondition: query database failed, conditions=%+v: %w", c, err)
		}

		// 缓存结果（1 表示存在，0 表示不存在，使用随机化过期时间）
		cacheValue := uint64(0)
		if exists {
			cacheValue = 1
		}
		expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
		if setErr := d.cache.SetIDByKey(ctx, existsCacheKey, cacheValue, expireTime); setErr != nil {
			logger.WarnWithCtx(ctx, "cache: failed to set exists result", logger.Err(setErr), logger.String("key", existsCacheKey))
		}
		return exists, nil
	})
	if sfErr != nil {
		return false, sfErr
	}
	exists, ok := val.(bool)
	if !ok {
		return false, errors.New("type assertion failed: expected exists value")
	}
	return exists, nil
}

// 1. 不分页查询（page=-1 或 limit<=0）
//var result1 []map[string]interface{}
//total, err := s.iCpDealerDao.GetByCustomQuery(ctx, func(db *gorm.DB) *gorm.DB {
//	return db.Model(&model.CpDealer{}).
//		Select("cp_dealer.id, cp_dealer.name, cp_dealer_order.order_sn").
//		Joins("LEFT JOIN cp_dealer_order ON cp_dealer.id = cp_dealer_order.dealer_id").
//		Where("cp_dealer.status = ?", 1).
//		Order("cp_dealer.id DESC")
//}, &result1, -1, 0)

//原始 SQL
//var result1 []map[string]interface{}
//total, err := s.iCpDealerDao.GetByCustomQuery(ctx, func(db *gorm.DB) *gorm.DB {
//	return db.Raw("SELECT cp_dealer.id, cp_dealer.name FROM cp_dealer WHERE cp_dealer.status = ? ORDER BY cp_dealer.id DESC", 1)
//}, &result1, 0, 10)

// 2. 分页查询（page>=0 且 limit>0）
//var result1 []map[string]interface{}
//total, err := s.iCpDealerDao.GetByCustomQuery(ctx, func(db *gorm.DB) *gorm.DB {
//	return db.Model(&model.CpDealer{}).
//		Select("cp_dealer.id, cp_dealer.name, cp_dealer_order.order_sn").
//		Joins("LEFT JOIN cp_dealer_order ON cp_dealer.id = cp_dealer_order.dealer_id").
//		Where("cp_dealer.status = ?", 1).
//		Order("cp_dealer.id DESC")
//}, &result1, 0, 10)

func (d *{{.TableNameCamelFCL}}Dao) GetByCustomQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, result interface{}, page, limit int, opts ...{{.TableNameCamel}}QueryOption) (int64, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)
	var total int64 = -1 // 使用 -1 表示未计算总数

	// 构建查询（支持强制主库查询）
	db := d.db.WithContext(ctx)
	if optsConfig.forceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	// 应用自定义查询函数
	db = queryFunc(db)

	// 判断是否需要分页
	if page >= 0 && limit > 0 {
		// 需要分页，先计算总数
		stmt := db.Statement
		if stmt != nil {
			if stmt.Table != "" || stmt.Model != nil {
				// 对于常规查询，使用GORM内置的Count方法
				err := db.Count(&total).Error
				if err != nil {
					return 0, fmt.Errorf("GetByCustomQuery: count failed: %w", err)
				}
				// 应用分页
				offset := page * limit
				db = db.Offset(offset).Limit(limit)
			} else if stmt.SQL.Len() > 0 {
				// 对于原始 SQL 查询，手动构造 COUNT 查询
				originalSQL := stmt.SQL.String()
				countSQL := d.convertToCountSQL(ctx, originalSQL)

				var count int64
				err := db.Raw(countSQL, stmt.Vars...).Scan(&count).Error
				if err != nil {
					return 0, fmt.Errorf("GetByCustomQuery: count raw sql failed: %w", err)
				}
				total = count

				// 对于原始 SQL 查询，手动应用分页
				pagedSQL := originalSQL + " LIMIT ? OFFSET ?"
				offset := page * limit
				db = db.Raw(pagedSQL, append(stmt.Vars, limit, offset)...)
			}
		}
	}

	// 执行查询
	var err error
	if db.Statement != nil && db.Statement.SQL.Len() > 0 {
		// 对于原始SQL查询，使用Scan方法
		err = db.Scan(result).Error
	} else {
		// 对于常规查询，使用Find方法
		err = db.Find(result).Error
	}

	if err != nil {
		return 0, fmt.Errorf("GetByCustomQuery: execute query failed: %w", err)
	}

	return total, nil
}

// convertToCountSQL 将普通 SQL SELECT 查询转换为 COUNT 查询 SQL
// 用途：为自定义分页查询计算总记录数
// 优化点:
// 1. 使用更精确的匹配，避免误判字符串中的 ORDER BY/LIMIT（如字段名包含这些词）
// 2. 支持移除 OFFSET、FOR UPDATE 等子句
// 3. 添加输入校验，防止空 SQL 或非法 SQL
// 4. 从后往前匹配 ORDER BY，避免误判子查询中的 ORDER BY
// 适用范围：常规 SELECT 查询，不支持 UNION、复杂子查询嵌套等极端场景
// 参数:
//   - ctx: 上下文
//   - sql: 原始 SELECT SQL 语句
//
// 返回:
//   - 转换后的 COUNT SQL，格式：SELECT COUNT(*) FROM (原 SQL) AS count_query
//
// 示例:
//
//	输入：SELECT * FROM users WHERE age > 18 ORDER BY id DESC LIMIT 10
//	输出：SELECT COUNT(*) FROM (SELECT * FROM users WHERE age > 18) AS count_query
func (d *{{.TableNameCamelFCL}}Dao) convertToCountSQL(ctx context.Context, sql string) string {
	if sql == "" {
		return consts.DaoEmptyCountSQL
	}

	sql = strings.TrimSpace(sql)
	lowerSQL := strings.ToLower(sql)

	// 1. 移除 FOR UPDATE（如果有）
	if idx := strings.Index(lowerSQL, " for update"); idx != -1 {
		sql = sql[:idx]
		lowerSQL = strings.ToLower(sql)
	}

	// 2. 移除 OFFSET 子句（如果有）
	if idx := strings.Index(lowerSQL, " offset "); idx != -1 {
		sql = sql[:idx]
		lowerSQL = strings.ToLower(sql)
	}

	// 3. 移除 LIMIT 子句（如果有）
	if idx := strings.Index(lowerSQL, " limit "); idx != -1 {
		sql = sql[:idx]
		lowerSQL = strings.ToLower(sql)
	}

	// 4. 移除 ORDER BY 子句（从后往前找，避免误判子查询中的 ORDER BY）
	orderByIndex := strings.LastIndex(lowerSQL, " order by")
	if orderByIndex != -1 {
		// 简单验证：统计括号数量，确保在最外层
		afterOrderBy := sql[orderByIndex:]
		openParens := strings.Count(afterOrderBy, "(")
		closeParens := strings.Count(afterOrderBy, ")")
		if openParens == closeParens || openParens-closeParens <= 1 {
			sql = sql[:orderByIndex]
		}
	}

	trimmedSQL := strings.TrimSpace(sql)

	// 最终校验：确保是有效的 SELECT 语句
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(trimmedSQL)), "select") {
		// 非 SELECT 语句属于预期内的边界情况，使用 Info 级别记录
		logger.InfoWithCtx(ctx, "convertToCountSQL: received non-SELECT SQL, returning empty result",
			logger.String("original_sql", sql),
			logger.String("processed_sql", trimmedSQL))
		return consts.DaoEmptyCountSQL
	}

	return "SELECT COUNT(*) FROM (" + trimmedSQL + ") AS count_query"
}
