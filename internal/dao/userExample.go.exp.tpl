package dao

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
	"google.golang.org/grpc/metadata"

	"github.com/18721889353/sunshine/internal/cache"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/model"

	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sgorm/query"
	"github.com/18721889353/sunshine/pkg/utils"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// 缓存和数据量阈值常量（避免硬编码）
const (
	// {{.TableNameCamel}}MaxCacheableIDs 最大可缓存的 ID 数量，超过此值则不缓存 ID 列表，直接查库
	{{.TableNameCamel}}MaxCacheableIDs = 10000

	// {{.TableNameCamel}}MaxBatchSize 批量操作的最大批次大小，超过此值则分批处理
	{{.TableNameCamel}}MaxBatchSize = 1000

	// {{.TableNameCamel}}MaxCacheableRecords 最大可缓存的记录数，超过此值则不缓存或仅警告
	{{.TableNameCamel}}MaxCacheableRecords = 1000

	// {{.TableNameCamel}}DelayedDeleteInterval 延迟双删的等待时间，用于确保主从同步完成
	// 默认值：100ms，可通过配置文件 database.cache{{.TableNameCamel}}DelayedDeleteInterval 覆盖
	{{.TableNameCamel}}DelayedDeleteInterval = 100 * time.Millisecond
)

// 全局随机数生成器池（线程安全，无锁竞争）
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

// {{.TableNameCamel}}ApplyOptions 应用选项配置（默认不强制主库）
func {{.TableNameCamel}}ApplyOptions(opts ...{{.TableNameCamel}}QueryOption) *{{.TableNameCamel}}QueryOptions {
	o := &{{.TableNameCamel}}QueryOptions{
		forceMaster: false,
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
	randGen := {{.TableNameCamel}}RandPool.Get().(*rand.Rand)
	// 生成 -300 ~ +300 秒的随机偏移（对应 CacheExpireTimeOffsetSeconds）
	offsetSeconds := randGen.Int63n(601) - 300 // 0-600 秒范围，减去 300 得到 -300~+300 秒
	{{.TableNameCamel}}RandPool.Put(randGen)                      // 放回池中供复用

	offset := time.Duration(offsetSeconds) * time.Second
	return base + offset
}

var _ {{.TableNameCamel}}Dao = (*{{.TableNameCamelFCL}}Dao)(nil)


// {{.TableNameCamel}}Dao defining the dao interface（大厂标准：所有查询方法支持选项模式）
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
func (m *{{.TableNameCamelFCL}}CacheManager) get(ctx context.Context, id uint64, queryFunc func() (*model.{{.TableNameCamel}}, error)) (*model.{{.TableNameCamel}}, error) {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先从缓存获取
	record, err := m.cache.Get(ctx, id)
	if err == nil {
		return record, nil
	}

	// 缓存未命中，从数据库获取
	if errors.Is(err, database.ErrCacheNotFound) {
		// 使用 singleflight 防止并发请求同时访问数据库
		val, err, _ := m.sfg.Do(m.getCacheKey(id), func() (interface{}, error) {
			table, dbErr := queryFunc()
			if dbErr != nil {
				// 设置占位符缓存防止缓存穿透
				if errors.Is(dbErr, gorm.ErrRecordNotFound) {
					if placeholderErr := m.cache.SetPlaceholder(ctx, id); placeholderErr != nil {
						logger.Warn("cache.SetPlaceholder error", logger.Err(placeholderErr), logger.Any("id", id), requestId)
					}
					return nil, database.ErrRecordNotFound
				}
				return nil, dbErr
			}
			// 设置缓存（使用随机化过期时间防止雪崩）
			expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
			if cacheErr := m.cache.Set(ctx, id, table, expireTime); cacheErr != nil {
				logger.Warn("cache.Set error", logger.Err(cacheErr), logger.Any("id", id), requestId)
			}
			return table, nil
		})
		if err != nil {
			return nil, err
		}
		table, ok := val.(*model.{{.TableNameCamel}})
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return table, nil
	}

	// 其他缓存错误（如 Redis 连接失败等），仅记录日志并回退到数据库查询
	logger.Warn("cache.Get error, falling back to database", logger.Err(err), logger.Any("id", id), requestId)

	// 如果是占位符错误，返回记录未找到
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}

	// 回退到数据库查询
	val, err, _ := m.sfg.Do(m.getCacheKey(id), func() (interface{}, error) {
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
			logger.Warn("cache.Set error after fallback", logger.Err(cacheErr), logger.Any("id", id), requestId)
		}
		return table, nil
	})
	if err != nil {
		return nil, err
	}
	table, ok := val.(*model.{{.TableNameCamel}})
	if !ok {
		return nil, database.ErrRecordNotFound
	}
	return table, nil
}

// getCondition 通过条件获取单条记录
// 错误处理原则：
//   - 缓存读取错误：仅记录日志，回退到数据库查询
//   - 数据库错误：必须返回给调用方
//   - 缓存写入错误：仅记录日志，不影响返回值
func (m *{{.TableNameCamelFCL}}CacheManager) getCondition(ctx context.Context, key string, queryFunc func() (*model.{{.TableNameCamel}}, error)) (*model.{{.TableNameCamel}}, error) {
	requestId := interceptor.CtxRequestIDField(ctx)
	cacheKey := m.getConditionCacheKey(key)

	// 先尝试从缓存获取 ID
	cachedID, err := m.cache.GetIdByKey(ctx, cacheKey)
	if err == nil && cachedID != 0 {
		// 通过 ID 获取完整信息
		record, getErr := m.get(ctx, cachedID, func() (*model.{{.TableNameCamel}}, error) {
			// 直接从数据库获取完整记录
			table := &model.{{.TableNameCamel}}{}
			err = database.GetDB().WithContext(ctx).Where("id = ?", cachedID).First(table).Error
			if err != nil {
				return nil, err
			}
			return table, nil
		})
		if getErr == nil && record != nil && record.ID == cachedID {
			return record, nil
		}
		// 如果通过 ID 获取失败（可能是记录已删除），清除条件缓存中的 ID，避免无效查询
		if getErr != nil {
			// 忽略删除错误的日志，避免噪音
			_ = m.cache.DelByKey(ctx, cacheKey)
		}
		// 回退到直接查询
	}

	// 缓存未命中或通过 ID 获取失败，从数据库获取
	if errors.Is(err, database.ErrCacheNotFound) || cachedID == 0 {
		// 使用 singleflight 防止并发请求同时访问数据库
		val, err, _ := m.sfg.Do("one_condition:"+key, func() (interface{}, error) {
			record, dbErr := queryFunc()
			if dbErr != nil {
				// 设置占位符缓存防止缓存穿透
				if errors.Is(dbErr, gorm.ErrRecordNotFound) {
					if placeholderErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); placeholderErr != nil {
						logger.Warn("cache.SetPlaceholderByKey error", logger.Err(placeholderErr), logger.Any("key", cacheKey), requestId)
					}
					return nil, database.ErrRecordNotFound
				}
				return nil, dbErr
			}

			// 如果记录存在，将其 ID 缓存起来（使用随机化过期时间）
			if record != nil {
				expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
				if cacheErr := m.cache.SetIdByKey(ctx, cacheKey, record.ID, expireTime); cacheErr != nil {
					logger.Warn("cache.SetIdByKey error", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("id", record.ID), requestId)
				}
				// 同时缓存完整记录（使用随机化过期时间）
				if cacheErr := m.cache.Set(ctx, record.ID, record, expireTime); cacheErr != nil {
					logger.Warn("cache.Set error", logger.Err(cacheErr), logger.Any("id", record.ID), requestId)
				}
			}
			return record, nil
		})
		if err != nil {
			return nil, err
		}
		record, ok := val.(*model.{{.TableNameCamel}})
		if !ok {
			return nil, nil
		}
		return record, nil
	}

	// 其他缓存错误（如 Redis 连接失败等），仅记录日志并回退到数据库查询
	logger.Warn("cache.GetIdByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey), requestId)

	// 如果是占位符错误，返回空
	if m.cache.IsPlaceholderErr(err) {
		return nil, nil
	}

	// 回退到数据库查询
	val, err, _ := m.sfg.Do("one_condition:"+key, func() (interface{}, error) {
		record, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, dbErr
		}

		// 如果记录存在，尝试缓存（失败仅记录日志）
		if record != nil {
			expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
			if cacheErr := m.cache.SetIdByKey(ctx, cacheKey, record.ID, expireTime); cacheErr != nil {
				logger.Warn("cache.SetIdByKey error after fallback", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("id", record.ID), requestId)
			}
			if cacheErr := m.cache.Set(ctx, record.ID, record, expireTime); cacheErr != nil {
				logger.Warn("cache.Set error after fallback", logger.Err(cacheErr), logger.Any("id", record.ID), requestId)
			}
		}
		return record, nil
	})
	if err != nil {
		return nil, err
	}
	record, ok := val.(*model.{{.TableNameCamel}})
	if !ok {
		return nil, nil
	}
	return record, nil
}

// getByCondition 通过条件获取 ID 列表
// 错误处理原则：
//   - 缓存读取错误：仅记录日志，回退到数据库查询
//   - 数据库错误：必须返回给调用方
//   - 缓存写入错误：仅记录日志，不影响返回值
func (m *{{.TableNameCamelFCL}}CacheManager) getByCondition(ctx context.Context, key string, queryFunc func() ([]uint64, error)) ([]uint64, error) {
	requestId := interceptor.CtxRequestIDField(ctx)
	cacheKey := m.getConditionCacheKey(key)

	// 先从缓存获取
	ids, err := m.cache.GetIdsByKey(ctx, cacheKey)
	if err == nil {
		// 检查 ID 列表大小，如果过大则不使用缓存，直接查询数据库
		if len(ids) > {{.TableNameCamel}}MaxCacheableIDs {
			logger.Warn("cached id list too large, querying database directly", logger.Any("count", len(ids)), logger.Any("key", key), requestId)
			return queryFunc()
		}
		return ids, nil
	}

	// 缓存未命中，从数据库获取
	if errors.Is(err, database.ErrCacheNotFound) {
		// 使用 singleflight 防止并发请求同时访问数据库
		val, err, _ := m.sfg.Do("ids_condition:"+key, func() (interface{}, error) {
			result, dbErr := queryFunc()
			if dbErr != nil {
				// 设置占位符缓存防止缓存穿透
				if placeholderErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); placeholderErr != nil {
					logger.Warn("cache.SetPlaceholderByKey error", logger.Err(placeholderErr), logger.Any("key", cacheKey), requestId)
				}
				return nil, dbErr
			}

			// 对于大数据量的结果集，不进行缓存，直接返回
			if len(result) > {{.TableNameCamel}}MaxCacheableIDs {
				logger.Warn("result set too large to cache", logger.Any("count", len(result)), logger.Any("key", key), requestId)
				return result, nil
			}

			// 设置缓存（使用随机化过期时间）
			expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
			if cacheErr := m.cache.SetIdsByKey(ctx, cacheKey, result, expireTime); cacheErr != nil {
				logger.Warn("cache.SetIdsByKey error", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("ids", result), requestId)
			}
			return result, nil
		})
		if err != nil {
			return nil, err
		}
		result, ok := val.([]uint64)
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return result, nil
	}

	// 其他缓存错误（如 Redis 连接失败等），仅记录日志并回退到数据库查询
	logger.Warn("cache.GetIdsByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey), requestId)

	// 如果是占位符错误，返回记录未找到
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}

	// 回退到数据库查询
	val, err, _ := m.sfg.Do("ids_condition:"+key, func() (interface{}, error) {
		result, dbErr := queryFunc()
		if dbErr != nil {
			return nil, dbErr
		}

		// 对于大数据量的结果集，不进行缓存，直接返回
		if len(result) > {{.TableNameCamel}}MaxCacheableIDs {
			logger.Warn("result set too large to cache (fallback)", logger.Any("count", len(result)), logger.Any("key", key), requestId)
			return result, nil
		}

		// 尝试设置缓存（失败仅记录日志）
		expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)
		if cacheErr := m.cache.SetIdsByKey(ctx, cacheKey, result, expireTime); cacheErr != nil {
			logger.Warn("cache.SetIdsByKey error after fallback", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("ids", result), requestId)
		}
		return result, nil
	})
	if err != nil {
		return nil, err
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
	if len(ids) > {{.TableNameCamel}}MaxBatchSize {
		result := make(map[uint64]*model.{{.TableNameCamel}})
		// 分批处理，每批 {{.TableNameCamel}}MaxBatchSize 个 ID
		for i := 0; i < len(ids); i += {{.TableNameCamel}}MaxBatchSize {
			end := i + {{.TableNameCamel}}MaxBatchSize
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

// getByIDsBatch 批量获取记录的实际实现
// 错误处理原则：
//   - 缓存读取错误：仅记录日志，回退到数据库查询
//   - 数据库错误：必须返回给调用方
//   - 缓存写入错误：仅记录日志，不影响返回值
func (m *{{.TableNameCamelFCL}}CacheManager) getByIDsBatch(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*model.{{.TableNameCamel}}, error)) (map[uint64]*model.{{.TableNameCamel}}, error) {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先从缓存获取
	itemMap, err := m.cache.MultiGet(ctx, ids)
	if err != nil {
		// 缓存错误时，记录日志并回退到直接数据库查询
		logger.Warn("cache.MultiGet error, falling back to database", logger.Err(err), logger.Any("ids", ids), requestId)
		// 返回空 map，让后续逻辑从数据库获取
		itemMap = make(map[uint64]*model.{{.TableNameCamel}})
	}

	// 查找未命中的 ID
	var missedIDs []uint64
	for _, id := range ids {
		if _, ok := itemMap[id]; !ok {
			missedIDs = append(missedIDs, id)
		}
	}

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
				logger.Warn("cache.MultiSet error", logger.Err(cacheErr), logger.Any("ids", missedIDs), requestId)
			}
		}

		// 对于数据库中也不存在的记录，设置占位符
		if len(records) < len(missedIDs) {
			existingIDs := make(map[uint64]bool)
			for _, record := range records {
				existingIDs[record.ID] = true
			}

			for _, id := range missedIDs {
				if !existingIDs[id] {
					if placeholderErr := m.cache.SetPlaceholder(ctx, id); placeholderErr != nil {
						logger.Warn("cache.SetPlaceholder error", logger.Err(placeholderErr), logger.Any("id", id), requestId)
					}
				}
			}
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
	requestId := interceptor.CtxRequestIDField(ctx)
	defer func() {
		// 创建操作只清除条件查询缓存，保留单条记录缓存
		if err := d.deleteCache(ctx, 0, "condition"); err != nil {
			logger.Warn("Create: failed to delete condition cache", logger.Err(err), requestId)
		}
	}()
	return d.db.WithContext(ctx).Create(table).Error
}
func (d *{{.TableNameCamelFCL}}Dao) CreateInBatches(ctx context.Context, tables []*model.{{.TableNameCamel}}, batchSize int) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	defer func() {
		// 批量创建操作只清除条件查询缓存
		if err := d.deleteCache(ctx, 0, "condition"); err != nil {
			logger.Warn("CreateInBatches: failed to delete condition cache", logger.Err(err), requestId)
		}
	}()
	return d.db.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}
func (d *{{.TableNameCamelFCL}}Dao) CreateByTx(ctx context.Context, tx *gorm.DB, table *model.{{.TableNameCamel}}) (uint64, error) {
	requestId := interceptor.CtxRequestIDField(ctx)
	defer func() {
		// 事务创建操作只清除条件查询缓存
		if err := d.deleteCache(ctx, 0, "condition"); err != nil {
			logger.Warn("CreateByTx: failed to delete condition cache", logger.Err(err), requestId)
		}
	}()
	err := tx.WithContext(ctx).Create(table).Error
	return table.ID, err
}
func (d *{{.TableNameCamelFCL}}Dao) CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, tables []*model.{{.TableNameCamel}}, batchSize int) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	defer func() {
		// 事务批量创建操作只清除条件查询缓存
		if err := d.deleteCache(ctx, 0, "condition"); err != nil {
			logger.Warn("CreateByInBatchesTx: failed to delete condition cache", logger.Err(err), requestId)
		}
	}()
	return tx.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}

// deleteCache 删除缓存的统一入口，封装错误处理
// deleteType 支持：
//   - "single": 删除单个 ID 缓存
//   - "condition": 删除条件查询缓存（包括 condition、columns、count、exists）
//   - "all": 删除所有缓存（最彻底，谨慎使用）
func (d *{{.TableNameCamelFCL}}Dao) deleteCache(ctx context.Context, id uint64, deleteType string) error {
	requestId := interceptor.CtxRequestIDField(ctx)

	if d.cache == nil {
		return nil
	}

	var errs []error
	switch deleteType {
	case "single":
		// 删除单个 ID 缓存
		if err := d.cache.Del(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("delete single cache failed: %w", err))
		}
	case "condition":
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
	case "all":
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
			logger.Warn("cache: failed to delete",
				logger.String("type", deleteType),
				logger.Any("id", id),
				logger.Err(err), requestId)
		}
		// 返回第一个错误，供调用方参考
		return errs[0]
	}
	return nil
}

// delayedDoubleDelete 延迟双删缓存，确保缓存一致性
// 参数：
//   - ctx: 背景上下文（应使用 context.Background() 避免受原始请求影响）
//   - id: 记录 ID（0 表示不按 ID 删除）
//   - ids: 批量 ID 列表（nil 表示不批量删除）
//   - deleteType: 删除类型（"condition"=只删除条件缓存，"all"=删除所有缓存）
func (d *{{.TableNameCamelFCL}}Dao) delayedDoubleDelete(ctx context.Context, id uint64, ids []uint64, deleteType string) {
	requestId := interceptor.CtxRequestIDField(ctx)

	if d.cache == nil {
		return
	}

	go func() {
		// panic 保护，防止异步 goroutine 崩溃
		defer func() {
			if r := recover(); r != nil {
				logger.Warn("delayedDoubleDelete panic recovered",
					logger.Any("recover", r),
					logger.Any("id", id),
					logger.String("deleteType", deleteType), requestId)
			}
		}()

		// 使用 select 监听 ctx.Done()，避免 ctx 被取消后 goroutine 仍在睡眠
		// 高并发场景下防止 goroutine 堆积
		select {
		case <-time.After({{.TableNameCamel}}DelayedDeleteInterval):
			// 延迟 {{.TableNameCamel}}DelayedDeleteInterval 后再次删除缓存，防止主从同步延迟导致的脏数据

			// 单个 ID 删除
			if id > 0 {
				if err := d.deleteCache(ctx, id, "single"); err != nil {
					logger.Warn("delayedDoubleDelete: failed to delete single cache",
						logger.Err(err),
						logger.Any("id", id), requestId)
				}
			}

			// 聚合批量删除失败的 ID，避免日志刷屏
			var failedIDs []uint64
			var firstErr error
			for _, batchID := range ids { // 使用 batchID 避免变量遮蔽
				if err := d.deleteCache(ctx, batchID, "single"); err != nil {
					if firstErr == nil {
						firstErr = err
					}
					failedIDs = append(failedIDs, batchID)
				}
			}
			if len(failedIDs) > 0 {
				logger.Warn("delayedDoubleDelete: failed to delete batch cache",
					logger.Err(firstErr),
					logger.Any("failed_ids", failedIDs),
					logger.Int("total_failed", len(failedIDs)), requestId)
			}

			// 按条件删除（根据 deleteType 参数决定删除范围）
			switch deleteType {
			case "all":
				// 删除所有缓存（包括 single、condition、columns、count、exists）
				if err := d.deleteCache(ctx, 0, "all"); err != nil {
					logger.Warn("delayedDoubleDelete: failed to delete all cache",
						logger.Err(err),
						logger.String("type", "all"), requestId)
				}
			case "condition":
				// 只删除条件缓存（condition、columns、count、exists）
				if err := d.deleteCache(ctx, 0, "condition"); err != nil {
					logger.Warn("delayedDoubleDelete: failed to delete condition cache",
						logger.Err(err),
						logger.String("type", "condition"), requestId)
				}
			}

		case <-ctx.Done():
			// ctx 被取消或超时，直接退出 goroutine
			logger.Info("delayedDoubleDelete: context canceled before sleep completed",
				logger.Any("id", id),
				logger.String("deleteType", deleteType),
				logger.Err(ctx.Err()), requestId)
			return
		}
	}()
}

func (d *{{.TableNameCamelFCL}}Dao) DeleteByID(ctx context.Context, id uint64) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	if err := d.deleteCache(ctx, id, "single"); err != nil {
		logger.Warn("pre-delete single cache failed", logger.Err(err), logger.Any("id", id), requestId)
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, "condition"); err != nil {
		logger.Warn("pre-delete condition cache failed", logger.Err(err), logger.Any("id", id), requestId)
	}

	// 执行数据库删除
	err := d.db.WithContext(ctx).Where("id = ?", id).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByID: delete from database failed, id=%d: %w", id, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), id, nil, "condition")
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByIDs(ctx context.Context, ids []uint64) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 聚合批量删除失败的 ID，避免日志刷屏
	var failedIDs []uint64
	var firstErr error
	for _, id := range ids {
		if err := d.deleteCache(ctx, id, "single"); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failedIDs = append(failedIDs, id)
		}
	}
	if len(failedIDs) > 0 {
		logger.Warn("pre-delete batch single cache failed",
			logger.Err(firstErr),
			logger.Any("failed_ids", failedIDs),
			logger.Int("total_failed", len(failedIDs)),
			logger.Any("ids", ids), requestId)
	} else {
		// 全部成功时记录调试日志
		logger.Debug("pre-delete batch single cache success",
			logger.Any("count", len(ids)), requestId)
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, "condition"); err != nil {
		logger.Warn("pre-delete condition cache failed", logger.Err(err), logger.Any("ids", ids), requestId)
	}

	// 执行数据库删除
	err := d.db.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByIDs: delete from database failed, ids=%v: %w", ids, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只批量删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), 0, ids, "condition")
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByCondition(ctx context.Context, c *query.Conditions) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 按条件删除会影响多条记录，需要删除所有相关缓存：
	// 1. condition: 条件查询缓存
	// 2. columns: 分页查询缓存
	// 3. count: 计数缓存
	// 4. exists: 存在性检查缓存
	// 5. single: 单条记录缓存（防止删除后读到已删除的数据）
	if err := d.deleteCache(ctx, 0, "all"); err != nil {
		logger.Warn("pre-delete all cache failed", logger.Err(err), requestId)
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
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), 0, nil, "all")
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByTx(ctx context.Context, tx *gorm.DB, id uint64) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	if err := d.deleteCache(ctx, id, "single"); err != nil {
		logger.Warn("pre-delete single cache failed", logger.Err(err), logger.Any("id", id), requestId)
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, "condition"); err != nil {
		logger.Warn("pre-delete condition cache failed", logger.Err(err), logger.Any("id", id), requestId)
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
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), id, nil, "condition")
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 聚合批量删除失败的 ID，避免日志刷屏
	var failedIDs []uint64
	var firstErr error
	for _, id := range ids {
		if err := d.deleteCache(ctx, id, "single"); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failedIDs = append(failedIDs, id)
		}
	}
	if len(failedIDs) > 0 {
		logger.Warn("pre-delete batch single cache failed",
			logger.Err(firstErr),
			logger.Any("failed_ids", failedIDs),
			logger.Int("total_failed", len(failedIDs)),
			logger.Any("ids", ids), requestId)
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, "condition"); err != nil {
		logger.Warn("pre-delete condition cache failed", logger.Err(err), logger.Any("ids", ids), requestId)
	}

	// 执行数据库删除
	err := tx.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByIDsTx: delete from database failed, ids=%v: %w", ids, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只批量删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), 0, ids, "condition")
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 按条件删除会影响多条记录，需要删除所有相关缓存：
	// 1. condition: 条件查询缓存
	// 2. columns: 分页查询缓存
	// 3. count: 计数缓存
	// 4. exists: 存在性检查缓存
	// 5. single: 单条记录缓存（防止删除后读到已删除的数据）
	if err := d.deleteCache(ctx, 0, "all"); err != nil {
		logger.Warn("pre-delete all cache failed", logger.Err(err), requestId)
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
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), 0, nil, "all")
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
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	if err := d.deleteCache(ctx, table.ID, "single"); err != nil {
		logger.Warn("pre-delete single cache failed", logger.Err(err), logger.Any("id", table.ID), requestId)
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, "condition"); err != nil {
		logger.Warn("pre-delete condition cache failed", logger.Err(err), logger.Any("id", table.ID), requestId)
	}

	// 执行数据库更新
	err := d.updateDataByID(d.db, table)
	if err != nil {
		return fmt.Errorf("UpdateByID: update database failed, id=%d: %w", table.ID, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), table.ID, nil, "condition")
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) UpdateByCondition(ctx context.Context, c *query.Conditions, table *model.{{.TableNameCamel}}) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 按条件更新会影响多条记录，需要删除所有相关缓存：
	// 1. condition: 条件查询缓存
	// 2. columns: 分页查询缓存
	// 3. count: 计数缓存
	// 4. exists: 存在性检查缓存
	// 5. single: 单条记录缓存（防止更新后读到旧数据）
	if err := d.deleteCache(ctx, 0, "all"); err != nil {
		logger.Warn("pre-delete all cache failed", logger.Err(err), requestId)
	}

	// 构建查询条件
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("UpdateByCondition: convert conditions to gorm failed: %w", err)
	}

	// 构建更新映射
	update := map[string]interface{}{}
	// todo generate the update fields code to here

	// 执行数据库更新
	err = d.db.WithContext(ctx).Model(&model.{{.TableNameCamel}}{}).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return fmt.Errorf("UpdateByCondition: update database failed, query=%s, args=%v: %w", queryStr, args, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有缓存，防止主从同步延迟
	// 注意：必须删除所有类型缓存（包括 single），因为首次删除后可能有读请求从从库读到旧数据并回填
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), 0, nil, "all")
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) UpdateByTx(ctx context.Context, tx *gorm.DB, table *model.{{.TableNameCamel}}) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	if err := d.deleteCache(ctx, table.ID, "single"); err != nil {
		logger.Warn("pre-delete single cache failed", logger.Err(err), logger.Any("id", table.ID), requestId)
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	if err := d.deleteCache(ctx, 0, "condition"); err != nil {
		logger.Warn("pre-delete condition cache failed", logger.Err(err), logger.Any("id", table.ID), requestId)
	}

	// 执行数据库更新
	err := d.updateDataByID(tx, table)
	if err != nil {
		return fmt.Errorf("UpdateByTx: update database failed, id=%d: %w", table.ID, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理相关缓存，防止主从同步延迟
	// 只删除单条记录缓存 + 条件缓存，避免全量删除
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), table.ID, nil, "condition")
	return nil
}
func (d *{{.TableNameCamelFCL}}Dao) UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, table *model.{{.TableNameCamel}}) error {
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先删除缓存（第一次删除）
	// 按条件更新会影响多条记录，需要删除所有相关缓存：
	// 1. condition: 条件查询缓存
	// 2. columns: 分页查询缓存
	// 3. count: 计数缓存
	// 4. exists: 存在性检查缓存
	// 5. single: 单条记录缓存（防止更新后读到旧数据）
	if err := d.deleteCache(ctx, 0, "all"); err != nil {
		logger.Warn("pre-delete all cache failed", logger.Err(err), requestId)
	}

	// 构建查询条件
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("UpdateByConditionTx: convert conditions to gorm failed: %w", err)
	}

	// 构建更新映射
	update := map[string]interface{}{}
	// todo generate the update fields code to here

	// 执行数据库更新
	err = tx.WithContext(ctx).Model(&model.{{.TableNameCamel}}{}).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return fmt.Errorf("UpdateByConditionTx: update database failed, query=%s, args=%v: %w", queryStr, args, err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有缓存，防止主从同步延迟
	// 注意：必须删除所有类型缓存（包括 single），因为首次删除后可能有读请求从从库读到旧数据并回填
	d.delayedDoubleDelete(metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), 0, nil, "all")
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
	requestId := interceptor.CtxRequestIDField(ctx)
	// 先清除所有缓存（自定义函数可能影响任意数据，必须全量清理）
	// 包括 single、condition、columns、count、exists
	if err := d.deleteCache(ctx, 0, "all"); err != nil {
		logger.Warn("ExecByCustomFunc: failed to delete all cache", logger.Err(err), requestId)
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
		middleware.ContextRequestIDKey: interceptor.CtxRequestIDField(ctx).String,
	})), 0, nil, "all")

	return err
}

func (d *{{.TableNameCamelFCL}}Dao) GetByID(ctx context.Context, id uint64, opts ...{{.TableNameCamel}}QueryOption) (*model.{{.TableNameCamel}}, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)
	// 无缓存模式直接查询（大厂标准：支持强制主库查询）
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
	if params.Sort != "ignore count" {
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
func (d *{{.TableNameCamelFCL}}Dao) GetByColumns(ctx context.Context, params *query.Params, opts ...{{.TableNameCamel}}QueryOption) ([]*model.{{.TableNameCamel}}, int64, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)
	requestId := interceptor.CtxRequestIDField(ctx)

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

	// 无缓存模式直接查询（大厂标准：支持强制主库查询）
	if d.cacheManager == nil {
		val, err, _ := d.sfg.Do(singleflightKey, func() (interface{}, error) {
			db := d.db.WithContext(ctx)
			if optsConfig.forceMaster {
				db = db.Clauses(dbresolver.Write)
			}
			return d.queryByColumnsWithDB(db, params, queryStr, args)
		})
		if err != nil {
			return nil, 0, err
		}
		result, ok := val.(struct {
			records []*model.{{.TableNameCamel}}
			total   int64
		})
		if !ok {
			return nil, 0, errors.New("type assertion failed: expected columns result struct")
		}

		// 大数据量警告
		if len(result.records) > {{.TableNameCamel}}MaxCacheableRecords {
			logger.Warn("GetByColumns: result set too large",
				logger.Any("count", len(result.records)),
				logger.String("cache_key", cacheKey), requestId)
		}
		return result.records, result.total, nil
	}

	// 使用缓存管理器优化查询（增加 singleflight 保护，避免并发穿透）
	fullCacheKey := d.cacheManager.getColumnsCacheKey(cacheKey)

	// 使用 singleflight 包裹整个查询过程，包括缓存读取和数据库查询
	val, err, _ := d.sfg.Do(singleflightKey, func() (interface{}, error) {
		// 先从缓存获取总数和 ID 列表
		cachedTotal, cacheErr := d.cache.GetIdByKey(ctx, fullCacheKey+":total")
		if cacheErr == nil {
			ids, idsErr := d.cache.GetIdsByKey(ctx, fullCacheKey+":ids")
			if idsErr == nil && len(ids) > 0 {
				// 通过 ID 批量获取记录（利用已有的缓存机制）
				recordsMap, getErr := d.cacheManager.getByIDs(ctx, ids, func(missedIDs []uint64) ([]*model.{{.TableNameCamel}}, error) {
					db := d.db.WithContext(ctx)
					if optsConfig.forceMaster {
						db = db.Clauses(dbresolver.Write)
					}
					var records []*model.{{.TableNameCamel}}
					err := db.Where("id IN (?)", missedIDs).Find(&records).Error
					return records, err
				})
				if getErr == nil && len(recordsMap) > 0 {
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
					}{records: records, total: int64(cachedTotal)}, nil
				}
			}
		}

		// 缓存未命中，从数据库查询（大厂标准：支持强制主库查询）
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
		if len(res.records) > {{.TableNameCamel}}MaxCacheableRecords {
			logger.Warn("GetByColumns: result set too large",
				logger.Any("count", len(res.records)),
				logger.String("cache_key", cacheKey), requestId)
		}

		// 缓存结果（控制缓存数据量，使用随机化过期时间）
		if len(res.records) <= {{.TableNameCamel}}MaxCacheableRecords && res.total > 0 {
			// 生成随机化过期时间
			expireTime := {{.TableNameCamel}}GetRandomExpireTime(cache.{{.TableNameCamel}}ExpireTime)

			// 缓存总数（增加数据值信息）
			if setErr := d.cache.SetIdByKey(ctx, fullCacheKey+":total", uint64(res.total), expireTime); setErr != nil {
				logger.Warn("cache: failed to set total count",
					logger.Err(setErr),
					logger.String("key", fullCacheKey+":total"),
					logger.Uint64("total", uint64(res.total)), requestId)
			}

			// 提取并缓存 ID 列表（增加数量信息）
			ids := make([]uint64, 0, len(res.records))
			for _, record := range res.records {
				ids = append(ids, record.ID)
			}
			if setErr := d.cache.SetIdsByKey(ctx, fullCacheKey+":ids", ids, expireTime); setErr != nil {
				logger.Warn("cache: failed to set ID list",
					logger.Err(setErr),
					logger.String("key", fullCacheKey+":ids"),
					logger.Int("id_count", len(ids)), requestId)
			}

			// 同时缓存单条记录（增加记录数信息）
			if setErr := d.cache.MultiSet(ctx, res.records, expireTime); setErr != nil {
				logger.Warn("cache: failed to multi-set records",
					logger.Err(setErr),
					logger.Any("count", len(res.records)),
					logger.Int("total_records", len(res.records)), requestId)
			}
		}

		return result, nil
	})
	// 处理错误情况
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []*model.{{.TableNameCamel}}{}, 0, nil
		}
		return nil, 0, err
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
	if len(result.records) > {{.TableNameCamel}}MaxCacheableRecords {
		logger.Warn("GetByColumns: result set too large",
			logger.Any("count", len(result.records)),
			logger.String("cache_key", cacheKey), requestId)
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

	// no cache（大厂标准：支持强制主库查询）
	if d.cacheManager == nil {
		record := &model.{{.TableNameCamel}}{}
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("GetOneByColumns: query database failed, sort=%s: %w", params.Sort, err)
		}
		return record, nil
	}

	// 使用缓存管理器获取数据（复用单条记录缓存逻辑，大厂标准：支持强制主库查询）
	return d.cacheManager.getCondition(ctx, cacheKey, func() (*model.{{.TableNameCamel}}, error) {
		record := &model.{{.TableNameCamel}}{}
		db := d.db.WithContext(ctx)
		if optsConfig.forceMaster {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("GetOneByColumns: query database failed, sort=%s: %w", params.Sort, err)
		}
		return record, nil
	})
}

// GetByCondition 根据条件获取记录 ID 列表
// 注意：当结果集过大时（>{{.TableNameCamel}}MaxCacheableIDs），建议改用分页查询以避免内存压力
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
	requestId := interceptor.CtxRequestIDField(ctx)

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return nil, fmt.Errorf("GetByCondition: convert conditions to gorm failed, conditions=%+v: %w", c, err)
	}

	var tables []*model.{{.TableNameCamel}}
	// 生成唯一缓存键（必须包含 forceMaster 选项，避免不同选项共享同一缓存）
	// 格式：queryStr + args + forceMaster
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v", queryStr, args, optsConfig.forceMaster)))

	// no cache（大厂标准：支持强制主库查询）
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
		if len(tables) > {{.TableNameCamel}}MaxCacheableIDs {
			logger.Warn("GetByCondition: result set too large",
				logger.Any("count", len(tables)),
				logger.String("cache_key", cacheKey), requestId)
		}
		// 提取 ID 列表
		result := make([]uint64, 0, len(tables))
		for _, table := range tables {
			result = append(result, table.ID)
		}
		return result, nil
	}

	// 使用缓存管理器获取数据（已包含 10000 条限制检查，大厂标准：支持强制主库查询）
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
	// 无缓存模式直接查询（大厂标准：支持强制主库查询）
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

	// 使用缓存管理器获取数据（大厂标准：支持强制主库查询）
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
	requestId := interceptor.CtxRequestIDField(ctx)

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return 0, fmt.Errorf("CountByCondition: convert conditions to gorm failed, conditions=%+v: %w", c, err)
	}

	// 生成唯一缓存键（必须包含 forceMaster 选项，避免不同选项共享同一缓存）
	// 格式：queryStr + args + forceMaster
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v", queryStr, args, optsConfig.forceMaster)))
	countCacheKey := "count:" + cacheKey

	// 无缓存模式直接查询（大厂标准：支持强制主库查询）
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
	cachedCount, err := d.cache.GetIdByKey(ctx, countCacheKey)
	if err == nil {
		return int64(cachedCount), nil
	}

	// 缓存未命中，使用 singleflight 防止并发重复查询（大厂标准：支持强制主库查询）
	val, err, _ := d.sfg.Do(countCacheKey, func() (interface{}, error) {
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
		if setErr := d.cache.SetIdByKey(ctx, countCacheKey, uint64(count), expireTime); setErr != nil {
			logger.Warn("cache: failed to set count", logger.Err(setErr), logger.String("key", countCacheKey), requestId)
		}
		return count, nil
	})
	if err != nil {
		return 0, err
	}
	count, ok := val.(int64)
	if !ok {
		return 0, errors.New("type assertion failed: expected count value")
	}
	return count, nil
}

func (d *{{.TableNameCamelFCL}}Dao) ExistsByCondition(ctx context.Context, c *query.Conditions, opts ...{{.TableNameCamel}}QueryOption) (bool, error) {
	optsConfig := {{.TableNameCamel}}ApplyOptions(opts...)
	requestId := interceptor.CtxRequestIDField(ctx)

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return false, fmt.Errorf("ExistsByCondition: convert conditions to gorm failed, conditions=%+v: %w", c, err)
	}

	// 生成唯一缓存键（必须包含 forceMaster 选项，避免不同选项共享同一缓存）
	// 格式：queryStr + args + forceMaster
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v", queryStr, args, optsConfig.forceMaster)))
	existsCacheKey := "exists:" + cacheKey

	// 无缓存模式直接查询（大厂标准：支持强制主库查询）
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
	cachedValue, err := d.cache.GetIdByKey(ctx, existsCacheKey)
	if err == nil {
		return cachedValue > 0, nil
	}

	// 缓存未命中，使用 singleflight 防止并发重复查询（大厂标准：支持强制主库查询）
	val, err, _ := d.sfg.Do(existsCacheKey, func() (interface{}, error) {
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
		if setErr := d.cache.SetIdByKey(ctx, existsCacheKey, cacheValue, expireTime); setErr != nil {
			logger.Warn("cache: failed to set exists result", logger.Err(setErr), logger.String("key", existsCacheKey), requestId)
		}
		return exists, nil
	})
	if err != nil {
		return false, err
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
	// 生成查询的唯一标识（用于 singleflight）
	// 注意：由于 queryFunc 是函数类型，无法直接序列化到缓存，这里使用调用信息作为 key
	// 仅使用 singleflight 防止并发重复查询，不使用缓存
	queryKey := fmt.Sprintf("custom_query:%p_%d_%d", queryFunc, page, limit)

	var total int64 = -1 // 使用 -1 表示未计算总数

	// 构建查询（大厂标准：支持强制主库查询）
	db := d.db.WithContext(ctx)
	if optsConfig.forceMaster {
		db = db.Clauses(dbresolver.Write)
	}
	// 应用自定义查询函数
	db = queryFunc(db)

	// 使用 singleflight 防止并发重复查询
	val, err, _ := d.sfg.Do(queryKey, func() (interface{}, error) {
		// 判断是否需要分页
		if page >= 0 && limit > 0 {
			// 需要分页，先计算总数
			stmt := db.Statement
			if stmt != nil {
				if stmt.Table != "" || stmt.Model != nil {
					// 对于常规查询，使用GORM内置的Count方法
					err := db.Count(&total).Error
					if err != nil {
						return 0, err
					}
					// 应用分页
					offset := page * limit
					db = db.Offset(offset).Limit(limit)
				} else if stmt.SQL.Len() > 0 {
					// 对于原始 SQL 查询，手动构造 COUNT 查询
					originalSQL := stmt.SQL.String()
					countSQL := d.convertToCountSQL(originalSQL)

					var count int64
					err := db.Raw(countSQL, stmt.Vars...).Scan(&count).Error
					if err != nil {
						return 0, err
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
			return 0, err
		}

		// 构建结果包装
		resultWrapper := struct {
			Result interface{}
			Total  int64
		}{
			Result: result,
			Total:  total,
		}

		return resultWrapper, nil
	})

	if err != nil {
		return 0, err
	}

	// 类型断言获取结果
	resultWrapper, ok := val.(struct {
		Result interface{}
		Total  int64
	})
	if !ok {
		return 0, errors.New("type assertion failed")
	}

	// 将结果复制回传入的 result 指针
	if resultWrapper.Result != nil {
		total = resultWrapper.Total
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
//   - sql: 原始 SELECT SQL 语句
//
// 返回:
//   - 转换后的 COUNT SQL，格式：SELECT COUNT(*) FROM (原 SQL) AS count_query
//
// 示例:
//
//	输入：SELECT * FROM users WHERE age > 18 ORDER BY id DESC LIMIT 10
//	输出：SELECT COUNT(*) FROM (SELECT * FROM users WHERE age > 18) AS count_query
func (d *{{.TableNameCamelFCL}}Dao) convertToCountSQL(sql string) string {
	if sql == "" {
		return "SELECT COUNT(*) FROM (SELECT 1) AS count_query WHERE 1=0"
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
		logger.Info("convertToCountSQL: received non-SELECT SQL, returning empty result",
			logger.String("original_sql", sql),
			logger.String("processed_sql", trimmedSQL))
		return "SELECT COUNT(*) FROM (SELECT 1) AS count_query WHERE 1=0"
	}

	return "SELECT COUNT(*) FROM (" + trimmedSQL + ") AS count_query"
}
