package dao

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

var _ UserExampleDao = (*userExampleDao)(nil)

// UserExampleDao defining the dao interface
type UserExampleDao interface {
	Create(ctx context.Context, table *model.UserExample) error
	CreateInBatches(ctx context.Context, tables []*model.UserExample, batchSize int) error
	CreateByTx(ctx context.Context, tx *gorm.DB, table *model.UserExample) (uint64, error)
	CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, tables []*model.UserExample, batchSize int) error

	DeleteByID(ctx context.Context, id uint64) error
	DeleteByIDs(ctx context.Context, ids []uint64) error
	DeleteByCondition(ctx context.Context, c *query.Conditions) error
	DeleteByTx(ctx context.Context, tx *gorm.DB, id uint64) error
	DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) error
	DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error
	ClearCache(ctx context.Context) error

	UpdateByID(ctx context.Context, table *model.UserExample) error
	UpdateByCondition(ctx context.Context, c *query.Conditions, updates *model.UserExample) error
	UpdateByTx(ctx context.Context, tx *gorm.DB, table *model.UserExample) error
	UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, updates *model.UserExample) error
	ExecByCustomFunc(ctx context.Context, updateFunc func(*gorm.DB) *gorm.DB) error

	GetByID(ctx context.Context, id uint64, forceMaster ...bool) (*model.UserExample, error)
	GetByColumns(ctx context.Context, params *query.Params, forceMaster ...bool) ([]*model.UserExample, int64, error)
	GetOneByColumns(ctx context.Context, params *query.Params, forceMaster ...bool) (*model.UserExample, error)
	GetByCondition(ctx context.Context, c *query.Conditions, forceMaster ...bool) (ids []uint64, err error)
	GetByIDs(ctx context.Context, ids []uint64, forceMaster ...bool) (map[uint64]*model.UserExample, error)
	CountByCondition(ctx context.Context, c *query.Conditions, forceMaster ...bool) (int64, error)
	ExistsByCondition(ctx context.Context, c *query.Conditions, forceMaster ...bool) (bool, error)
	GetByCustomQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, result interface{}, page, limit int) (int64, error)
}

// userExampleCacheManager 统一管理缓存操作
type userExampleCacheManager struct {
	cache cache.UserExampleCache
	sfg   *singleflight.Group
}

// newUserExampleCacheManager 创建缓存管理器
func newUserExampleCacheManager(c cache.UserExampleCache) *userExampleCacheManager {
	return &userExampleCacheManager{
		cache: c,
		sfg:   new(singleflight.Group),
	}
}

// getCacheKey 生成基于 ID 的缓存键
// 格式：prefix + id
func (m *userExampleCacheManager) getCacheKey(id uint64) string {
	return cache.UserExampleCachePrefixKey + utils.Uint64ToStr(id)
}

// getConditionCacheKey 生成基于查询条件的缓存键
// 用于 GetOneByColumns、GetByCondition 等按条件查询的方法
// 格式：prefix + "condition:" + keyMD5
func (m *userExampleCacheManager) getConditionCacheKey(key string) string {
	return cache.UserExampleCachePrefixKey + "condition:" + key
}

// getColumnsCacheKey 生成基于分页查询条件的缓存键
// 专用于 GetByColumns 方法的分页查询缓存
// 格式：prefix + "columns:" + keyMD5
func (m *userExampleCacheManager) getColumnsCacheKey(key string) string {
	return cache.UserExampleCachePrefixKey + "columns:" + key
}

// get 通过singleflight和缓存获取数据
func (m *userExampleCacheManager) get(ctx context.Context, id uint64, queryFunc func() (*model.UserExample, error)) (*model.UserExample, error) {
	// 先从缓存获取
	record, err := m.cache.Get(ctx, id)
	if err == nil {
		return record, nil
	}

	// 缓存未命中，从数据库获取
	if errors.Is(err, database.ErrCacheNotFound) {
		// 使用singleflight防止并发请求同时访问数据库
		val, err, _ := m.sfg.Do(m.getCacheKey(id), func() (interface{}, error) {
			table, dbErr := queryFunc()
			if dbErr != nil {
				// 设置占位符缓存防止缓存穿透
				if errors.Is(dbErr, gorm.ErrRecordNotFound) {
					if placeholderErr := m.cache.SetPlaceholder(ctx, id); placeholderErr != nil {
						logger.Warn("cache.SetPlaceholder error", logger.Err(placeholderErr), logger.Any("id", id))
					}
					return nil, database.ErrRecordNotFound
				}
				return nil, dbErr
			}
			// 设置缓存
			if cacheErr := m.cache.Set(ctx, id, table, cache.UserExampleExpireTime); cacheErr != nil {
				logger.Warn("cache.Set error", logger.Err(cacheErr), logger.Any("id", id))
			}
			return table, nil
		})
		if err != nil {
			return nil, err
		}
		table, ok := val.(*model.UserExample)
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return table, nil
	}

	// 如果是占位符错误，返回记录未找到
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}

	return nil, err
}

// getCondition 通过条件获取单条记录
func (m *userExampleCacheManager) getCondition(ctx context.Context, key string, queryFunc func() (*model.UserExample, error)) (*model.UserExample, error) {
	cacheKey := m.getConditionCacheKey(key)

	// 先尝试从缓存获取ID
	cachedID, err := m.cache.GetIdByKey(ctx, cacheKey)
	if err == nil && cachedID != 0 {
		// 通过ID获取完整信息
		record, getErr := m.get(ctx, cachedID, func() (*model.UserExample, error) {
			// 直接从数据库获取完整记录
			table := &model.UserExample{}
			err = database.GetDB().WithContext(ctx).Where("id = ?", cachedID).First(table).Error
			if err != nil {
				return nil, err
			}
			return table, nil
		})
		if getErr == nil && record.ID == cachedID {
			return record, nil
		}
		// 如果通过ID获取失败，则回退到直接查询
	}

	// 缓存未命中或通过ID获取失败，从数据库获取
	if errors.Is(err, database.ErrCacheNotFound) || cachedID == 0 {
		// 使用singleflight防止并发请求同时访问数据库
		val, err, _ := m.sfg.Do("one_condition:"+key, func() (interface{}, error) {
			record, dbErr := queryFunc()
			if dbErr != nil {
				// 设置占位符缓存防止缓存穿透
				if errors.Is(dbErr, gorm.ErrRecordNotFound) {
					if placeholderErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); placeholderErr != nil {
						logger.Warn("cache.SetPlaceholderByKey error", logger.Err(placeholderErr), logger.Any("key", cacheKey))
					}
					return nil, database.ErrRecordNotFound
				}
				return nil, dbErr
			}

			// 如果记录存在，将其ID缓存起来
			if record != nil {
				if cacheErr := m.cache.SetIdByKey(ctx, cacheKey, record.ID, cache.UserExampleExpireTime); cacheErr != nil {
					logger.Warn("cache.SetIdByKey error", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("id", record.ID))
				}
				// 同时缓存完整记录
				if cacheErr := m.cache.Set(ctx, record.ID, record, cache.UserExampleExpireTime); cacheErr != nil {
					logger.Warn("cache.Set error", logger.Err(cacheErr), logger.Any("id", record.ID))
				}
			}
			return record, nil
		})
		if err != nil {
			return nil, err
		}
		record, ok := val.(*model.UserExample)
		if !ok {
			return nil, nil
		}
		return record, nil
	}

	// 如果是占位符错误，返回空
	if m.cache.IsPlaceholderErr(err) {
		return nil, nil
	}

	// 其他错误直接返回
	return nil, err
}

// getByCondition 通过条件获取ID列表
func (m *userExampleCacheManager) getByCondition(ctx context.Context, key string, queryFunc func() ([]uint64, error)) ([]uint64, error) {
	cacheKey := m.getConditionCacheKey(key)

	// 先从缓存获取
	ids, err := m.cache.GetIdsByKey(ctx, cacheKey)
	if err == nil {
		// 检查ID列表大小，如果过大则不使用缓存，直接查询数据库
		if len(ids) > 10000 {
			logger.Warn("cached id list too large, querying database directly", logger.Any("count", len(ids)), logger.Any("key", key))
			return queryFunc()
		}
		return ids, nil
	}

	// 缓存未命中，从数据库获取
	if errors.Is(err, database.ErrCacheNotFound) {
		// 使用singleflight防止并发请求同时访问数据库
		val, err, _ := m.sfg.Do("ids_condition:"+key, func() (interface{}, error) {
			result, dbErr := queryFunc()
			if dbErr != nil {
				// 设置占位符缓存防止缓存穿透
				if placeholderErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); placeholderErr != nil {
					logger.Warn("cache.SetPlaceholderByKey error", logger.Err(placeholderErr), logger.Any("key", cacheKey))
				}
				return nil, dbErr
			}

			// 对于大数据量的结果集，不进行缓存，直接返回
			if len(result) > 10000 {
				logger.Warn("result set too large to cache", logger.Any("count", len(result)), logger.Any("key", key))
				return result, nil
			}

			// 设置缓存
			if cacheErr := m.cache.SetIdsByKey(ctx, cacheKey, result, cache.UserExampleExpireTime); cacheErr != nil {
				logger.Warn("cache.SetIdsByKey error", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("ids", result))
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

	// 如果是占位符错误，返回记录未找到
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}

	return nil, err
}

// getByIDs 批量获取记录
func (m *userExampleCacheManager) getByIDs(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*model.UserExample, error)) (map[uint64]*model.UserExample, error) {
	// 对于大数据量请求，分批处理以避免内存峰值
	if len(ids) > 1000 {
		result := make(map[uint64]*model.UserExample)
		// 分批处理，每批1000个ID
		for i := 0; i < len(ids); i += 1000 {
			end := i + 1000
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
func (m *userExampleCacheManager) getByIDsBatch(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*model.UserExample, error)) (map[uint64]*model.UserExample, error) {
	// 先从缓存获取
	itemMap, err := m.cache.MultiGet(ctx, ids)
	if err != nil {
		return nil, err
	}

	// 查找未命中的ID
	var missedIDs []uint64
	for _, id := range ids {
		if _, ok := itemMap[id]; !ok {
			missedIDs = append(missedIDs, id)
		}
	}

	// 获取未命中的数据
	if len(missedIDs) > 0 {
		// 过滤掉占位符ID：直接使用 MultiGet 返回的结果来判断
		var realMissedIDs []uint64
		for _, id := range missedIDs {
			// 如果 MultiGet 没有返回且没有错误，则可能是占位符或真正缺失
			_, err := m.cache.Get(ctx, id)
			if err == nil || !m.cache.IsPlaceholderErr(err) {
				// 只有当不是占位符错误时才认为需要查询数据库
				realMissedIDs = append(realMissedIDs, id)
			}
			// 如果是占位符错误，跳过即可
		}

		// 从数据库获取未命中的数据
		if len(realMissedIDs) > 0 {
			records, err := queryFunc(realMissedIDs)
			if err != nil {
				return nil, err
			}

			if len(records) > 0 {
				// 添加到结果映射中
				for _, record := range records {
					itemMap[record.ID] = record
				}
				// 批量设置缓存
				if cacheErr := m.cache.MultiSet(ctx, records, cache.UserExampleExpireTime); cacheErr != nil {
					logger.Warn("cache.MultiSet error", logger.Err(cacheErr), logger.Any("ids", realMissedIDs))
				}
			}

			// 对于数据库中也不存在的记录，设置占位符
			if len(records) < len(realMissedIDs) {
				existingIDs := make(map[uint64]bool)
				for _, record := range records {
					existingIDs[record.ID] = true
				}

				for _, id := range realMissedIDs {
					if !existingIDs[id] {
						if placeholderErr := m.cache.SetPlaceholder(ctx, id); placeholderErr != nil {
							logger.Warn("cache.SetPlaceholder error", logger.Err(placeholderErr), logger.Any("id", id))
						}
					}
				}
			}
		}
	}

	return itemMap, nil
}

type userExampleDao struct {
	db           *gorm.DB
	cache        cache.UserExampleCache   // if nil, the cache is not used.
	cacheManager *userExampleCacheManager // 缓存管理器
	sfg          *singleflight.Group      // if cache is nil, the sfg is not used.
}

// NewUserExampleDao creating the dao interface
func NewUserExampleDao(db *gorm.DB, xCache cache.UserExampleCache) UserExampleDao {
	dao := &userExampleDao{
		db:    db,
		cache: xCache,
		sfg:   new(singleflight.Group),
	}

	if xCache != nil {
		dao.cacheManager = newUserExampleCacheManager(xCache)
	}

	return dao
}

func (d *userExampleDao) Create(ctx context.Context, table *model.UserExample) error {
	defer func() {
		// 创建操作只清除条件查询缓存，保留单条记录缓存
		_ = d.deleteCache(ctx, 0, "condition")
	}()
	return d.db.WithContext(ctx).Create(table).Error
}
func (d *userExampleDao) CreateInBatches(ctx context.Context, tables []*model.UserExample, batchSize int) error {
	defer func() {
		// 批量创建操作只清除条件查询缓存
		_ = d.deleteCache(ctx, 0, "condition")
	}()
	return d.db.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}
func (d *userExampleDao) CreateByTx(ctx context.Context, tx *gorm.DB, table *model.UserExample) (uint64, error) {
	defer func() {
		// 事务创建操作只清除条件查询缓存
		_ = d.deleteCache(ctx, 0, "condition")
	}()
	err := tx.WithContext(ctx).Create(table).Error
	return table.ID, err
}
func (d *userExampleDao) CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, tables []*model.UserExample, batchSize int) error {
	defer func() {
		// 事务批量创建操作只清除条件查询缓存
		_ = d.deleteCache(ctx, 0, "condition")
	}()
	return tx.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}
// deleteCache 删除缓存的统一入口，封装错误处理
// deleteType 支持：
//   - "single": 删除单个 ID 缓存
//   - "condition": 删除条件查询缓存（包括 condition、columns、count、exists）
//   - "all": 删除所有缓存（包括单条记录、条件查询、分页查询、计数、存在性检查）
func (d *userExampleDao) deleteCache(ctx context.Context, id uint64, deleteType string) error {
	if d.cache == nil {
		return nil
	}

	var err error
	switch deleteType {
	case "single":
		// 删除单个 ID 缓存
		err = d.cache.Del(ctx, id)
	case "all":
		// 删除所有缓存（最彻底）
		err = d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey)
	case "condition":
		// 删除条件查询相关的所有缓存
		// 1. 删除 condition 前缀的缓存（GetOneByColumns、GetByCondition）
		_ = d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey+"condition")
		// 2. 删除 columns 前缀的缓存（GetByColumns）
		_ = d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey+"columns")
		// 3. 删除 count 前缀的缓存（CountByCondition）
		_ = d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey+"count")
		// 4. 删除 exists 前缀的缓存（ExistsByCondition）
		_ = d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey+"exists")
		return nil // 已经处理完所有子操作，直接返回
	default:
		return nil
	}

	// 记录缓存删除错误，但不影响主流程
	if err != nil {
		logger.Warn("cache: failed to delete",
			logger.String("type", deleteType),
			logger.Any("id", id),
			logger.Err(err))
	}
	return err
}

// delayedDoubleDelete 延迟双删缓存，确保缓存一致性
// 参数：
//   - ctx: 背景上下文（避免业务上下文泄露）
//   - id: 记录 ID（0 表示不按 ID 删除）
//   - ids: 批量 ID 列表（nil 表示不批量删除）
//   - deleteType: 删除类型（single/all/condition）
func (d *userExampleDao) delayedDoubleDelete(ctx context.Context, id uint64, ids []uint64, deleteType string) {
	if d.cache == nil {
		return
	}

	go func() {
		// 延迟 100ms 后再次删除缓存，防止主从同步延迟导致的脏数据
		time.Sleep(100 * time.Millisecond)

		// 单个 ID 删除
		if id > 0 {
			_ = d.deleteCache(ctx, id, "single")
		}

		// 批量 ID 删除
		if len(ids) > 0 {
			for _, batchID := range ids {
				_ = d.deleteCache(ctx, batchID, "single")
			}
		}

		// 按条件删除
		if deleteType != "" && deleteType != "single" {
			_ = d.deleteCache(ctx, 0, deleteType)
		}
	}()
}

func (d *userExampleDao) DeleteByID(ctx context.Context, id uint64) error {
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	_ = d.deleteCache(ctx, id, "single")
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	_ = d.deleteCache(ctx, 0, "condition")

	// 执行数据库删除
	err := d.db.WithContext(ctx).Where("id = ?", id).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("delete by id failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有相关缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), id, nil, "all")
	return nil
}
func (d *userExampleDao) DeleteByIDs(ctx context.Context, ids []uint64) error {
	// 先删除缓存（第一次删除）
	// 1. 批量删除单条记录缓存（一级缓存）
	for _, id := range ids {
		_ = d.deleteCache(ctx, id, "single")
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	_ = d.deleteCache(ctx, 0, "condition")

	// 执行数据库删除
	err := d.db.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("delete by ids failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有相关缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), 0, ids, "all")
	return nil
}
func (d *userExampleDao) DeleteByCondition(ctx context.Context, c *query.Conditions) error {
	// 先删除缓存（第一次删除）
	// 按条件删除会影响多条记录，需要删除所有缓存（一级 + 二级）
	_ = d.deleteCache(ctx, 0, "all")

	// 构建查询条件
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("convert conditions failed: %w", err)
	}

	// 执行数据库删除
	err = d.db.WithContext(ctx).Where(queryStr, args...).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("delete by condition failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), 0, nil, "all")
	return nil
}
func (d *userExampleDao) DeleteByTx(ctx context.Context, tx *gorm.DB, id uint64) error {
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	_ = d.deleteCache(ctx, id, "single")
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	_ = d.deleteCache(ctx, 0, "condition")

	// 执行数据库软删除
	update := map[string]interface{}{
		"deleted_at": time.Now(),
	}
	err := tx.WithContext(ctx).Model(&model.UserExample{}).Where("id = ?", id).Updates(update).Error
	if err != nil {
		return fmt.Errorf("delete by tx failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有相关缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), id, nil, "all")
	return nil
}
func (d *userExampleDao) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) error {
	// 先删除缓存（第一次删除）
	// 1. 批量删除单条记录缓存（一级缓存）
	for _, id := range ids {
		_ = d.deleteCache(ctx, id, "single")
	}
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	_ = d.deleteCache(ctx, 0, "condition")

	// 执行数据库删除
	err := tx.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("delete by ids tx failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有相关缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), 0, ids, "all")
	return nil
}
func (d *userExampleDao) DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error {
	// 先删除缓存（第一次删除）
	// 按条件删除会影响多条记录，需要删除所有缓存（一级 + 二级）
	_ = d.deleteCache(ctx, 0, "all")

	// 构建查询条件
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("convert conditions failed: %w", err)
	}

	// 执行数据库删除
	err = tx.WithContext(ctx).Where(queryStr, args...).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("delete by tx condition failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), 0, nil, "all")
	return nil
}
func (d *userExampleDao) ClearCache(ctx context.Context) error {
	if d.cache != nil {
		return d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey)
	}
	return nil
}

func (d *userExampleDao) updateDataByID(db *gorm.DB, table *model.UserExample) error {
	if table.ID < 1 {
		return errors.New("id cannot be 0")
	}

	update := map[string]interface{}{}
	// todo generate the update fields code to here

	return db.Model(table).Updates(update).Error
}
func (d *userExampleDao) UpdateByID(ctx context.Context, table *model.UserExample) error {
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	_ = d.deleteCache(ctx, table.ID, "single")
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	_ = d.deleteCache(ctx, 0, "condition")

	// 执行数据库更新
	err := d.updateDataByID(d.db, table)
	if err != nil {
		return fmt.Errorf("update by id failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有相关缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), table.ID, nil, "all")
	return nil
}
func (d *userExampleDao) UpdateByCondition(ctx context.Context, c *query.Conditions, table *model.UserExample) error {
	// 先删除缓存（第一次删除）
	// 按条件更新会影响多条记录，需要删除所有缓存（一级 + 二级）
	_ = d.deleteCache(ctx, 0, "all")

	// 构建查询条件
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("convert conditions failed: %w", err)
	}

	// 构建更新映射
	update := map[string]interface{}{}
	// todo generate the update fields code to here

	// 执行数据库更新
	err = d.db.WithContext(ctx).Model(&model.UserExample{}).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return fmt.Errorf("update by condition failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), 0, nil, "all")
	return nil
}
func (d *userExampleDao) UpdateByTx(ctx context.Context, tx *gorm.DB, table *model.UserExample) error {
	// 先删除缓存（第一次删除）
	// 1. 删除单条记录缓存（一级缓存）
	_ = d.deleteCache(ctx, table.ID, "single")
	// 2. 删除所有条件查询缓存（二级缓存），因为数据变化可能导致条件查询结果不准确
	_ = d.deleteCache(ctx, 0, "condition")

	// 执行数据库更新
	err := d.updateDataByID(tx, table)
	if err != nil {
		return fmt.Errorf("update by tx failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有相关缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), table.ID, nil, "all")
	return nil
}
func (d *userExampleDao) UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, table *model.UserExample) error {
	// 先删除缓存（第一次删除）
	// 按条件更新会影响多条记录，需要删除所有缓存（一级 + 二级）
	_ = d.deleteCache(ctx, 0, "all")

	// 构建查询条件
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("convert conditions failed: %w", err)
	}

	// 构建更新映射
	update := map[string]interface{}{}
	// todo generate the update fields code to here

	// 执行数据库更新
	err = tx.WithContext(ctx).Model(&model.UserExample{}).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return fmt.Errorf("update by condition tx failed: %w", err)
	}

	// 延迟双删（第二次删除）：100ms 后再次清理所有缓存，防止主从同步延迟
	d.delayedDoubleDelete(context.Background(), 0, nil, "all")
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
//	if err := db.Model(&model.CpDealer{}).Where("id = ?", 1).Update("name", "前端测试商户1").Error; err != nil {
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

func (d *userExampleDao) ExecByCustomFunc(ctx context.Context, updateFunc func(*gorm.DB) *gorm.DB) error {
	// 先清除相关缓存
	defer func() {
		_ = d.deleteCache(ctx, 0, "all")
	}()

	db := d.db.WithContext(ctx)
	// 应用自定义更新函数
	db = updateFunc(db)

	// 执行更新操作
	var err error
	if db.Statement != nil && db.Statement.SQL.Len() > 0 {
		// 对于原始SQL查询，直接执行
		err = db.Exec(db.Statement.SQL.String(), db.Statement.Vars...).Error
	} else {
		// 对于常规查询，执行操作
		err = db.Error
	}

	return err
}

func (d *userExampleDao) GetByID(ctx context.Context, id uint64, forceMaster ...bool) (*model.UserExample, error) {
	// 无缓存模式直接查询
	if d.cacheManager == nil {
		record := &model.UserExample{}
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id = ?", id).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, fmt.Errorf("query database failed: %w", err)
		}
		return record, nil
	}

	// 使用缓存管理器获取数据（包含 singleflight、防击穿、防穿透机制）
	return d.cacheManager.get(ctx, id, func() (*model.UserExample, error) {
		table := &model.UserExample{}
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id = ?", id).First(table).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, fmt.Errorf("query database failed: %w", err)
		}
		return table, nil
	})
}

func (d *userExampleDao) queryByColumnsWithDB(db *gorm.DB, params *query.Params, queryStr string, args []interface{}) (interface{}, error) {
	var total int64
	var records []*model.UserExample
	// 统计总数（若需要）
	if params.Sort != "ignore count" {
		err := db.Model(&model.UserExample{}).Where(queryStr, args...).Count(&total).Error
		if err != nil {
			return nil, err
		}
		if total == 0 {
			return struct {
				records []*model.UserExample
				total   int64
			}{records: []*model.UserExample{}, total: 0}, nil
		}
	}

	// 分页查询
	order, limit, offset := params.ConvertToPage()
	err := db.Order(order).Limit(limit).Offset(offset).Where(queryStr, args...).Find(&records).Error
	if err != nil {
		return nil, err
	}

	return struct {
		records []*model.UserExample
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
func (d *userExampleDao) GetByColumns(ctx context.Context, params *query.Params, forceMaster ...bool) ([]*model.UserExample, int64, error) {
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, 0, fmt.Errorf("convert query conditions failed: %w", err)
	}

	// 生成唯一缓存键
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))
	singleflightKey := "columns:" + cacheKey

	var result struct {
		records []*model.UserExample
		total   int64
	}

	// 无缓存模式直接查询
	if d.cacheManager == nil {
		val, err, _ := d.sfg.Do(singleflightKey, func() (interface{}, error) {
			db := d.db.WithContext(ctx)
			if len(forceMaster) > 0 && forceMaster[0] {
				db = db.Clauses(dbresolver.Write)
			}
			return d.queryByColumnsWithDB(db, params, queryStr, args)
		})
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return []*model.UserExample{}, 0, nil
			}
			return nil, 0, fmt.Errorf("query database failed: %w", err)
		}
		result = val.(struct {
			records []*model.UserExample
			total   int64
		})

		// 大数据量警告
		if len(result.records) > 1000 {
			logger.Warn("GetByColumns: result set too large",
				logger.Any("count", len(result.records)),
				logger.String("cache_key", cacheKey))
		}
		return result.records, result.total, nil
	}

	// 使用缓存管理器优化查询
	fullCacheKey := d.cacheManager.getColumnsCacheKey(cacheKey)

	// 尝试从缓存获取总数和 ID 列表
	cachedTotal, err := d.cache.GetIdByKey(ctx, fullCacheKey+":total")
	if err == nil {
		ids, idsErr := d.cache.GetIdsByKey(ctx, fullCacheKey+":ids")
		if idsErr == nil && len(ids) > 0 {
			// 通过 ID 批量获取记录（利用已有的缓存机制）
			recordsMap, getErr := d.cacheManager.getByIDs(ctx, ids, func(missedIDs []uint64) ([]*model.UserExample, error) {
				db := d.db.WithContext(ctx)
				if len(forceMaster) > 0 && forceMaster[0] {
					db = db.Clauses(dbresolver.Write)
				}
				var records []*model.UserExample
				err := db.Where("id IN (?)", missedIDs).Find(&records).Error
				return records, err
			})
			if getErr == nil && len(recordsMap) > 0 {
				// 按 ID 顺序返回结果
				records := make([]*model.UserExample, 0, len(ids))
				for _, id := range ids {
					if record, ok := recordsMap[id]; ok {
						records = append(records, record)
					}
				}
				return records, int64(cachedTotal), nil
			}
		}
	}

	// 缓存未命中，从数据库查询
	val, err, _ := d.sfg.Do(singleflightKey, func() (interface{}, error) {
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		return d.queryByColumnsWithDB(db, params, queryStr, args)
	})
	// 处理错误情况
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []*model.UserExample{}, 0, nil
		}
		return nil, 0, fmt.Errorf("query database failed: %w", err)
	}

	// 类型断言获取查询结果
	result = val.(struct {
		records []*model.UserExample
		total   int64
	})

	// 大数据量警告
	if len(result.records) > 1000 {
		logger.Warn("GetByColumns: result set too large",
			logger.Any("count", len(result.records)),
			logger.String("cache_key", cacheKey))
	}

	// 缓存结果（控制缓存数据量）
	if len(result.records) <= 1000 && result.total > 0 {
		// 缓存总数
		if setErr := d.cache.SetIdByKey(ctx, fullCacheKey+":total", uint64(result.total), cache.UserExampleExpireTime); setErr != nil {
			logger.Warn("cache: failed to set total count",
				logger.Err(setErr),
				logger.String("key", fullCacheKey+":total"))
		}

		// 提取并缓存 ID 列表
		ids := make([]uint64, 0, len(result.records))
		for _, record := range result.records {
			ids = append(ids, record.ID)
		}
		if setErr := d.cache.SetIdsByKey(ctx, fullCacheKey+":ids", ids, cache.UserExampleExpireTime); setErr != nil {
			logger.Warn("cache: failed to set ID list",
				logger.Err(setErr),
				logger.String("key", fullCacheKey+":ids"))
		}

		// 同时缓存单条记录
		if setErr := d.cache.MultiSet(ctx, result.records, cache.UserExampleExpireTime); setErr != nil {
			logger.Warn("cache: failed to multi-set records",
				logger.Err(setErr),
				logger.Any("count", len(result.records)))
		}
	}

	return result.records, result.total, nil
}

// GetOneByColumns 根据列信息获取单条记录
// 参数同 GetByColumns，返回第一条匹配的记录
func (d *userExampleDao) GetOneByColumns(ctx context.Context, params *query.Params, forceMaster ...bool) (*model.UserExample, error) {
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, fmt.Errorf("convert query conditions failed: %w", err)
	}
	order, _, _ := params.ConvertToPage()

	// 生成唯一缓存键
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))

	// no cache
	if d.cacheManager == nil {
		record := &model.UserExample{}
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("query database failed: %w", err)
		}
		return record, nil
	}

	// 使用缓存管理器获取数据（复用单条记录缓存逻辑）
	return d.cacheManager.getCondition(ctx, cacheKey, func() (*model.UserExample, error) {
		record := &model.UserExample{}
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("query database failed: %w", err)
		}
		return record, nil
	})
}

// GetByCondition 根据条件获取记录 ID 列表
// 注意：当结果集过大时（>10000），建议改用分页查询以避免内存压力
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
func (d *userExampleDao) GetByCondition(ctx context.Context, c *query.Conditions, forceMaster ...bool) (ids []uint64, err error) {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return nil, fmt.Errorf("convert query conditions failed: %w", err)
	}

	var tables []*model.UserExample
	// 生成唯一缓存键
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))

	// no cache
	if d.cacheManager == nil {
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Where(queryStr, args...).Find(&tables).Error
		if err != nil {
			return nil, fmt.Errorf("query database failed: %w", err)
		}
		// 大数据量警告
		if len(tables) > 10000 {
			logger.Warn("GetByCondition: result set too large",
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

	// 使用缓存管理器获取数据（已包含 10000 条限制检查）
	return d.cacheManager.getByCondition(ctx, cacheKey, func() ([]uint64, error) {
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Where(queryStr, args...).Find(&tables).Error
		if err != nil {
			return nil, fmt.Errorf("query database failed: %w", err)
		}
		// 提取 ID 列表
		result := make([]uint64, 0, len(tables))
		for _, table := range tables {
			result = append(result, table.ID)
		}
		return result, nil
	})
}

func (d *userExampleDao) GetByIDs(ctx context.Context, ids []uint64, forceMaster ...bool) (map[uint64]*model.UserExample, error) {
	// 无缓存模式直接查询
	if d.cacheManager == nil {
		var records []*model.UserExample
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id IN (?)", ids).Find(&records).Error
		if err != nil {
			return nil, fmt.Errorf("query database failed: %w", err)
		}
		itemMap := make(map[uint64]*model.UserExample, len(records))
		for _, record := range records {
			itemMap[record.ID] = record
		}
		return itemMap, nil
	}

	// 使用缓存管理器获取数据
	return d.cacheManager.getByIDs(ctx, ids, func(missedIDs []uint64) ([]*model.UserExample, error) {
		var records []*model.UserExample
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id IN (?)", missedIDs).Find(&records).Error
		if err != nil {
			return nil, fmt.Errorf("query database failed: %w", err)
		}
		return records, nil
	})
}

func (d *userExampleDao) CountByCondition(ctx context.Context, c *query.Conditions, forceMaster ...bool) (int64, error) {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return 0, fmt.Errorf("convert query conditions failed: %w", err)
	}

	// 生成唯一缓存键
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))
	countCacheKey := "count:" + cacheKey

	// 无缓存模式直接查询
	if d.cacheManager == nil {
		var count int64
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Model(&model.UserExample{}).Where(queryStr, args...).Count(&count).Error
		if err != nil {
			return 0, fmt.Errorf("query database failed: %w", err)
		}
		return count, nil
	}

	// 尝试从缓存获取
	cachedCount, err := d.cache.GetIdByKey(ctx, countCacheKey)
	if err == nil {
		return int64(cachedCount), nil
	}

	// 缓存未命中，使用 singleflight 防止并发重复查询
	val, err, _ := d.sfg.Do(countCacheKey, func() (interface{}, error) {
		var count int64
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Model(&model.UserExample{}).Where(queryStr, args...).Count(&count).Error
		if err != nil {
			return 0, fmt.Errorf("query database failed: %w", err)
		}

		// 缓存计数结果（包括 0，避免重复查询）
		if setErr := d.cache.SetIdByKey(ctx, countCacheKey, uint64(count), cache.UserExampleExpireTime); setErr != nil {
			logger.Warn("cache: failed to set count",
				logger.Err(setErr),
				logger.String("key", countCacheKey))
		}
		return count, nil
	})
	if err != nil {
		return 0, err
	}
	return val.(int64), nil
}

func (d *userExampleDao) ExistsByCondition(ctx context.Context, c *query.Conditions, forceMaster ...bool) (bool, error) {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return false, fmt.Errorf("convert query conditions failed: %w", err)
	}

	// 生成唯一缓存键
	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))
	existsCacheKey := "exists:" + cacheKey

	// 无缓存模式直接查询
	if d.cacheManager == nil {
		var exists bool
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		// 使用 SELECT 1 LIMIT 1 优化存在性检查，性能优于 COUNT
		err = db.Model(&model.UserExample{}).Where(queryStr, args...).Select("1").Limit(1).Scan(&exists).Error
		if err != nil {
			return false, fmt.Errorf("query database failed: %w", err)
		}
		return exists, nil
	}

	// 尝试从缓存获取
	cachedValue, err := d.cache.GetIdByKey(ctx, existsCacheKey)
	if err == nil {
		return cachedValue > 0, nil
	}

	// 缓存未命中，使用 singleflight 防止并发重复查询
	val, err, _ := d.sfg.Do(existsCacheKey, func() (interface{}, error) {
		var exists bool
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		// 使用 SELECT 1 LIMIT 1 优化存在性检查
		err = db.Model(&model.UserExample{}).Where(queryStr, args...).Select("1").Limit(1).Scan(&exists).Error
		if err != nil {
			return false, fmt.Errorf("query database failed: %w", err)
		}

		// 缓存结果（1 表示存在，0 表示不存在）
		cacheValue := uint64(0)
		if exists {
			cacheValue = 1
		}
		if setErr := d.cache.SetIdByKey(ctx, existsCacheKey, cacheValue, cache.UserExampleExpireTime); setErr != nil {
			logger.Warn("cache: failed to set exists result",
				logger.Err(setErr),
				logger.String("key", existsCacheKey))
		}
		return exists, nil
	})
	if err != nil {
		return false, err
	}
	return val.(bool), nil
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

func (d *userExampleDao) GetByCustomQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, result interface{}, page, limit int) (int64, error) {
	// 生成查询的唯一标识（用于 singleflight）
	// 注意：由于 queryFunc 是函数类型，无法直接序列化到缓存，这里使用调用信息作为 key
	// 仅使用 singleflight 防止并发重复查询，不使用缓存
	queryKey := fmt.Sprintf("custom_query:%p_%d_%d", queryFunc, page, limit)

	var total int64 = -1 // 使用 -1 表示未计算总数

	db := d.db.WithContext(ctx)
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

// convertToCountSQL 将普通 SQL 查询转换为 COUNT 查询 SQL
func (d *userExampleDao) convertToCountSQL(sql string) string {
	// 移除ORDER BY子句，因为COUNT查询不需要排序
	orderByIndex := strings.Index(strings.ToLower(sql), "order by")
	if orderByIndex != -1 {
		// 查找ORDER BY之前的部分
		sql = sql[:orderByIndex]
	}
	// 移除LIMIT和OFFSET子句
	limitIndex := strings.Index(strings.ToLower(sql), "limit")
	if limitIndex != -1 {
		sql = sql[:limitIndex]
	}
	// 包装成COUNT查询
	trimmedSQL := strings.TrimSpace(sql)
	countSQL := "SELECT COUNT(*) FROM (" + trimmedSQL + ") AS count_query"
	return countSQL
}
