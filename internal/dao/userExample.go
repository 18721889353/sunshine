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

// getCacheKey 生成基于ID的缓存键
func (m *userExampleCacheManager) getCacheKey(id uint64) string {
	return cache.UserExampleCachePrefixKey + utils.Uint64ToStr(id)
}

func (m *userExampleCacheManager) getOneConditionCacheKey(key string) string {
	return cache.UserExampleCachePrefixKey + "condition:" + key
}

// getConditionCacheKey 生成基于条件的缓存键
func (m *userExampleCacheManager) getConditionCacheKey(key string) string {
	return cache.UserExampleCachePrefixKey + "conditions:" + key
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

// getOneByConditionKey 通过条件获取单条记录
func (m *userExampleCacheManager) getOneByConditionKey(ctx context.Context, key string, queryFunc func() (*model.UserExample, error)) (*model.UserExample, error) {
	cacheKey := m.getOneConditionCacheKey(key)

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
func (d *userExampleDao) deleteCache(ctx context.Context, id uint64, deleteType string) error {
	if d.cache == nil {
		return nil
	}

	switch deleteType {
	case "single":
		return d.cache.Del(ctx, id)
	case "all":
		return d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey)
	case "condition":
		return d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey+"condition")
	default:
		return nil
	}
}

func (d *userExampleDao) DeleteByID(ctx context.Context, id uint64) error {
	// 先删除缓存
	_ = d.deleteCache(ctx, id, "single")
	_ = d.deleteCache(ctx, 0, "condition")
	err := d.db.WithContext(ctx).Where("id = ?", id).Delete(&model.UserExample{}).Error
	if err != nil {
		return err
	}
	if d.cache != nil {
		// 延迟双删
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 使用背景上下文避免上下文泄露
			bgCtx := context.Background()
			_ = d.deleteCache(bgCtx, id, "single")
			_ = d.deleteCache(bgCtx, 0, "condition")
		}()
	}
	return nil
}
func (d *userExampleDao) DeleteByIDs(ctx context.Context, ids []uint64) error {
	// 先删除缓存
	for _, id := range ids {
		_ = d.deleteCache(ctx, id, "single")
	}
	_ = d.deleteCache(ctx, 0, "condition")

	err := d.db.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.UserExample{}).Error
	if err != nil {
		return err
	}

	// 延迟双删
	if d.cache != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 使用背景上下文避免上下文泄露
			bgCtx := context.Background()
			for _, id := range ids {
				_ = d.deleteCache(bgCtx, id, "single")
			}
			_ = d.deleteCache(bgCtx, 0, "condition")
		}()
	}
	return nil
}
func (d *userExampleDao) DeleteByCondition(ctx context.Context, c *query.Conditions) error {
	// 先删除缓存
	_ = d.deleteCache(ctx, 0, "all")

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}
	err = d.db.WithContext(ctx).Where(queryStr, args...).Delete(&model.UserExample{}).Error
	if err != nil {
		return err
	}

	// 延迟双删
	if d.cache != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 使用背景上下文避免上下文泄露
			bgCtx := context.Background()
			_ = d.deleteCache(bgCtx, 0, "all")
		}()
	}
	return nil
}
func (d *userExampleDao) DeleteByTx(ctx context.Context, tx *gorm.DB, id uint64) error {
	// 先删除缓存
	_ = d.deleteCache(ctx, id, "single")
	_ = d.deleteCache(ctx, 0, "condition")

	update := map[string]interface{}{
		"deleted_at": time.Now(),
	}
	err := tx.WithContext(ctx).Model(&model.UserExample{}).Where("id = ?", id).Updates(update).Error
	if err != nil {
		return err
	}

	// 延迟双删
	go func() {
		time.Sleep(100 * time.Millisecond)
		// 使用背景上下文避免上下文泄露
		bgCtx := context.Background()
		_ = d.deleteCache(bgCtx, id, "single")
		_ = d.deleteCache(bgCtx, 0, "condition")
	}()
	return nil
}
func (d *userExampleDao) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) error {
	// 先删除缓存
	for _, id := range ids {
		_ = d.deleteCache(ctx, id, "single")
	}
	_ = d.deleteCache(ctx, 0, "condition")

	err := tx.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.UserExample{}).Error
	if err != nil {
		return err
	}

	// 延迟双删
	if d.cache != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 使用背景上下文避免上下文泄露
			bgCtx := context.Background()
			if d.cache != nil {
				for _, id := range ids {
					_ = d.deleteCache(bgCtx, id, "single")
				}
			}
			_ = d.deleteCache(bgCtx, 0, "condition")
		}()
	}
	return nil
}
func (d *userExampleDao) DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error {
	// 先删除缓存
	_ = d.deleteCache(ctx, 0, "all")

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}
	err = tx.WithContext(ctx).Where(queryStr, args...).Delete(&model.UserExample{}).Error
	if err != nil {
		return err
	}

	// 延迟双删
	if d.cache != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 使用背景上下文避免上下文泄露
			bgCtx := context.Background()
			_ = d.deleteCache(bgCtx, 0, "all")
		}()
	}
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
	// 先删除缓存
	_ = d.deleteCache(ctx, table.ID, "single")
	_ = d.deleteCache(ctx, 0, "condition")

	err := d.updateDataByID(d.db, table)
	if err != nil {
		return err
	}

	// 延迟双删
	if d.cache != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 使用背景上下文避免上下文泄露
			bgCtx := context.Background()
			_ = d.deleteCache(bgCtx, table.ID, "single")
			_ = d.deleteCache(bgCtx, 0, "condition")
		}()
	}
	return nil
}
func (d *userExampleDao) UpdateByCondition(ctx context.Context, c *query.Conditions, table *model.UserExample) error {
	// 先删除缓存
	_ = d.deleteCache(ctx, 0, "condition")

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}

	// 构建更新映射
	update := map[string]interface{}{}
	// todo generate the update fields code to here

	err = d.db.WithContext(ctx).Model(&model.UserExample{}).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return err
	}

	// 延迟双删
	if d.cache != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 使用背景上下文避免上下文泄露
			bgCtx := context.Background()
			_ = d.deleteCache(bgCtx, 0, "condition")
		}()
	}
	return nil
}
func (d *userExampleDao) UpdateByTx(ctx context.Context, tx *gorm.DB, table *model.UserExample) error {
	// 先删除缓存
	_ = d.deleteCache(ctx, table.ID, "single")
	_ = d.deleteCache(ctx, 0, "condition")

	err := d.updateDataByID(tx, table)
	if err != nil {
		return err
	}

	// 延迟双删
	if d.cache != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 使用背景上下文避免上下文泄露
			bgCtx := context.Background()
			_ = d.deleteCache(bgCtx, table.ID, "single")
			_ = d.deleteCache(bgCtx, 0, "condition")
		}()
	}
	return nil
}
func (d *userExampleDao) UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, table *model.UserExample) error {
	// 先删除缓存
	_ = d.deleteCache(ctx, 0, "condition")

	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}

	// 构建更新映射
	update := map[string]interface{}{}
	// todo generate the update fields code to here

	err = tx.WithContext(ctx).Model(&model.UserExample{}).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return err
	}

	// 延迟双删
	if d.cache != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 使用背景上下文避免上下文泄露
			bgCtx := context.Background()
			_ = d.deleteCache(bgCtx, 0, "condition")
		}()
	}
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
	// no cache
	if d.cacheManager == nil {
		record := &model.UserExample{}
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id = ?", id).First(record).Error
		return record, err
	}

	// 使用缓存管理器获取数据
	return d.cacheManager.get(ctx, id, func() (*model.UserExample, error) {
		table := &model.UserExample{}
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id = ?", id).First(table).Error
		return table, err
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

// GetByColumns get paging records by column information,
// Note: query performance degrades when table rows are very large because of the use of offset.
//
// params includes paging parameters and query parameters
// paging parameters (required):
//
//	page: page number, starting from 0
//	limit: lines per page
//	sort: sort fields, default is id backwards, you can add - sign before the field to indicate reverse order, no - sign to indicate ascending order, multiple fields separated by comma
//
// query parameters (not required):
//
//		name: column name
//	 exp: expressions, which default is "=",  support =, !=, >, >=, <, <=, like, in, notin, isnull, isnotnull
//		value: column value, if exp=in, multiple values are separated by commas
//		logic: logical type, defaults to and when value is null, only &(and), ||(or)
//
// example: search for a male over 20 years of age
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
		return nil, 0, errors.New("query params error: " + err.Error())
	}

	// 生成唯一 key
	key := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))

	var result struct {
		records []*model.UserExample
		total   int64
	}

	// 使用缓存管理器或 singleflight 避免并发重复查询
	var val interface{}
	//仅使用 singleflight
	val, err, _ = d.sfg.Do("columns:"+key, func() (interface{}, error) {
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		return d.queryByColumnsWithDB(db, params, queryStr, args)
	})
	// 处理错误情况
	if err != nil {
		// 如果是数据库记录未找到的错误，返回空结果而非错误
		if errors.Is(err, database.ErrRecordNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			return []*model.UserExample{}, 0, nil
		}
		return nil, 0, err
	}

	// 类型断言获取查询结果
	result = val.(struct {
		records []*model.UserExample
		total   int64
	})

	return result.records, result.total, nil
}

func (d *userExampleDao) GetOneByColumns(ctx context.Context, params *query.Params, forceMaster ...bool) (*model.UserExample, error) {
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, errors.New("query params error: " + err.Error())
	}
	order, _, _ := params.ConvertToPage()

	// 生成唯一 key
	key := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))

	// no cache
	if d.cacheManager == nil {
		record := &model.UserExample{}
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		return record, err
	}

	// 使用缓存管理器获取数据
	return d.cacheManager.getOneByConditionKey(ctx, key, func() (*model.UserExample, error) {
		record := &model.UserExample{}
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		return record, err
	})
}

// GetByCondition get a record by condition
// query conditions:
//
//	name: column name
//	exp: expressions, which default is "=",  support =, !=, >, >=, <, <=, like, in, notin, isnull, isnotnull
//	value: column value, if exp=in, multiple values are separated by commas
//	logic: logical type, defaults to and when value is null, only &(and), ||(or)
//
// example: find a male aged 20
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
		return nil, err
	}

	var tables []*model.UserExample
	key := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))

	// no cache
	if d.cacheManager == nil {
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Where(queryStr, args...).Find(&tables).Error
		if err != nil {
			return nil, err
		}
		var result []uint64
		for _, table := range tables {
			result = append(result, table.ID)
		}
		return result, nil
	}

	// 使用缓存管理器获取数据
	return d.cacheManager.getByCondition(ctx, key, func() ([]uint64, error) {
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err = db.Where(queryStr, args...).Find(&tables).Error
		if err != nil {
			return nil, err
		}
		var result []uint64
		for _, table := range tables {
			result = append(result, table.ID)
		}
		return result, nil
	})
}

func (d *userExampleDao) GetByIDs(ctx context.Context, ids []uint64, forceMaster ...bool) (map[uint64]*model.UserExample, error) {
	// no cache
	if d.cacheManager == nil {
		var records []*model.UserExample
		db := d.db.WithContext(ctx)
		if len(forceMaster) > 0 && forceMaster[0] {
			db = db.Clauses(dbresolver.Write)
		}
		err := db.Where("id IN (?)", ids).Find(&records).Error
		if err != nil {
			return nil, err
		}
		itemMap := make(map[uint64]*model.UserExample)
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
		return records, err
	})
}

func (d *userExampleDao) CountByCondition(ctx context.Context, c *query.Conditions, forceMaster ...bool) (int64, error) {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return 0, err
	}

	var count int64
	db := d.db.WithContext(ctx)
	if len(forceMaster) > 0 && forceMaster[0] {
		db = db.Clauses(dbresolver.Write)
	}
	err = db.Model(&model.UserExample{}).Where(queryStr, args...).Count(&count).Error
	return count, err
}

func (d *userExampleDao) ExistsByCondition(ctx context.Context, c *query.Conditions, forceMaster ...bool) (bool, error) {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return false, err
	}

	var count int64
	db := d.db.WithContext(ctx)
	if len(forceMaster) > 0 && forceMaster[0] {
		db = db.Clauses(dbresolver.Write)
	}
	err = db.Model(&model.UserExample{}).Where(queryStr, args...).Limit(1).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
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

	db := d.db.WithContext(ctx)
	// 应用自定义查询函数
	db = queryFunc(db)

	var total int64 = -1 // 使用-1表示未计算总数

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

	return total, nil
}

// convertToCountSQL 将普通SQL查询转换为COUNT查询SQL
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
