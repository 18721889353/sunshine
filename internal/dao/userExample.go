// Package dao 数据访问层
// 提供数据库操作的统一接口，包含缓存管理、事务支持、延迟双删等高级特性
package dao

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/18721889353/sunshine/internal/cache"
	"github.com/18721889353/sunshine/internal/consts"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/model"
	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sgorm/query"
	"github.com/18721889353/sunshine/pkg/sgorm/scopes"
	"github.com/18721889353/sunshine/pkg/utils"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

// ==================== 旧选项模式适配器 ====================

// UserExampleQueryOption 查询选项函数类型（保留以兼容旧调用）
type UserExampleQueryOption func(*userExampleQueryOptions)

type userExampleQueryOptions struct {
	forceMaster bool
	unscoped    bool
}

// UserExampleWithForceMaster 强制使用主库查询
func UserExampleWithForceMaster() UserExampleQueryOption {
	return func(o *userExampleQueryOptions) {
		o.forceMaster = true
	}
}

// UserExampleWithUnscoped 忽略软删除
func UserExampleWithUnscoped() UserExampleQueryOption {
	return func(o *userExampleQueryOptions) {
		o.unscoped = true
	}
}

// userExampleApplyOptions 将旧选项转换为 Scope 切片，同时返回 forceMaster 和 unscoped 标识用于缓存键生成
// 【架构修复】：forceMaster 默认为 false，避免默认读请求全部穿透到主库导致从库闲置
func userExampleApplyOptions(opts ...UserExampleQueryOption) ([]scopes.Scope, bool, bool) {
	cfg := &userExampleQueryOptions{
		forceMaster: true,
		unscoped:    false,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	var s []scopes.Scope
	if cfg.forceMaster {
		s = append(s, scopes.ForceMaster())
	}
	if cfg.unscoped {
		s = append(s, scopes.Unscoped())
	}
	return s, cfg.forceMaster, cfg.unscoped
}

// ==================== 随机过期时间工具 ====================

// 【架构优化】：Go 1.20+ 全局 rand 已具备高性能与线程安全
func UserExampleGetRandomExpireTime(base time.Duration) time.Duration {
	offsetSeconds := rand.Int63n(601) - 300 // [-300, 300] 秒浮动
	return base + time.Duration(offsetSeconds)*time.Second
}

// ==================== DAO 接口 ====================

var _ UserExampleDao = (*userExampleDao)(nil)

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

	GetByID(ctx context.Context, id uint64, opts ...UserExampleQueryOption) (*model.UserExample, error)
	GetByColumns(ctx context.Context, params *query.Params, opts ...UserExampleQueryOption) ([]*model.UserExample, int64, error)
	GetOneByColumns(ctx context.Context, params *query.Params, opts ...UserExampleQueryOption) (*model.UserExample, error)
	GetByCondition(ctx context.Context, c *query.Conditions, opts ...UserExampleQueryOption) (ids []uint64, err error)
	GetByIDs(ctx context.Context, ids []uint64, opts ...UserExampleQueryOption) (map[uint64]*model.UserExample, error)
	CountByCondition(ctx context.Context, c *query.Conditions, opts ...UserExampleQueryOption) (int64, error)
	ExistsByCondition(ctx context.Context, c *query.Conditions, opts ...UserExampleQueryOption) (bool, error)
	GetByCustomQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, result interface{}, page, limit int, opts ...UserExampleQueryOption) (int64, error)

	// 用于事务提交后清理缓存，避免事务未提交时删除缓存导致脏读
	CleanCacheByID(ctx context.Context, id uint64) error
	CleanCacheByIDs(ctx context.Context, ids []uint64) error
	CleanCacheByCondition(ctx context.Context, c *query.Conditions) error
	CleanCacheAll(ctx context.Context) error
}

// ==================== 缓存管理器 ====================

type userExampleCacheManager struct {
	cache cache.UserExampleCache
	sfg   *singleflight.Group
}

func newUserExampleCacheManager(c cache.UserExampleCache) *userExampleCacheManager {
	return &userExampleCacheManager{
		cache: c,
		sfg:   new(singleflight.Group),
	}
}

func (m *userExampleCacheManager) getCacheKey(id uint64) string {
	return cache.UserExampleCachePrefixKey + utils.Uint64ToStr(id)
}

func (m *userExampleCacheManager) getConditionCacheKey(key string) string {
	return cache.UserExampleCachePrefixKey + "condition:" + key
}

func (m *userExampleCacheManager) getColumnsCacheKey(key string) string {
	return cache.UserExampleCachePrefixKey + "columns:" + key
}

// UserExampleColumnsResult 分页查询结果结构体，用于统一类型断言
type UserExampleColumnsResult struct {
	Records []*model.UserExample
	Total   int64
}

// ==================== 缓存管理器方法 ====================

// get 通过缓存 + singleflight 获取单条记录
func (m *userExampleCacheManager) get(ctx context.Context, id uint64, queryFunc func() (*model.UserExample, error)) (*model.UserExample, error) {
	record, err := m.cache.Get(ctx, id)
	if err == nil {
		return record, nil
	}

	if errors.Is(err, database.ErrCacheNotFound) {
		val, sfErr, _ := m.sfg.Do(m.getCacheKey(id), func() (interface{}, error) {
			lockKey := "lock:refresh:" + m.getCacheKey(id)
			if lock, lockErr := m.cache.GetLock(ctx, lockKey); lockErr == nil {
				defer func() {
					if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
						logger.WarnWithCtx(ctx, "cache: unlock refresh lock failed", logger.Err(unlockErr))
					}
				}()

				// Double Check
				if record, cacheErr := m.cache.Get(ctx, id); cacheErr == nil {
					return record, nil
				}
			}
			table, dbErr := queryFunc()
			if dbErr != nil {
				if errors.Is(dbErr, gorm.ErrRecordNotFound) {
					if placeholderErr := m.cache.SetPlaceholder(ctx, id); placeholderErr != nil {
						logger.WarnWithCtx(ctx, "cache.SetPlaceholder error", logger.Err(placeholderErr), logger.Any("id", id))
					}
					return nil, database.ErrRecordNotFound
				}
				return nil, dbErr
			}
			expireTime := UserExampleGetRandomExpireTime(cache.UserExampleExpireTime)
			if cacheErr := m.cache.Set(ctx, id, table, expireTime); cacheErr != nil {
				logger.WarnWithCtx(ctx, "cache.Set error", logger.Err(cacheErr), logger.Any("id", id))
			}
			return table, nil
		})
		if sfErr != nil {
			return nil, sfErr
		}
		table, ok := val.(*model.UserExample)
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return table, nil
	}

	logger.WarnWithCtx(ctx, "cache.Get error, falling back to database", logger.Err(err), logger.Any("id", id))
	return m.handleCacheFallback(ctx, id, err, queryFunc)
}

func (m *userExampleCacheManager) handleCacheFallback(ctx context.Context, id uint64, err error, queryFunc func() (*model.UserExample, error)) (*model.UserExample, error) {
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}
	val, sfErr, _ := m.sfg.Do(m.getCacheKey(id), func() (interface{}, error) {
		lockKey := "lock:refresh:" + m.getCacheKey(id)
		if lock, lockErr := m.cache.GetLock(ctx, lockKey); lockErr == nil {
			defer func() {
				if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "cache: unlock refresh lock failed", logger.Err(unlockErr))
				}
			}()
			if record, cacheErr := m.cache.Get(ctx, id); cacheErr == nil {
				return record, nil
			}
		}
		table, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}
		expireTime := UserExampleGetRandomExpireTime(cache.UserExampleExpireTime)
		if cacheErr := m.cache.Set(ctx, id, table, expireTime); cacheErr != nil {
			logger.WarnWithCtx(ctx, "cache.Set error after fallback", logger.Err(cacheErr), logger.Any("id", id))
		}
		return table, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	table, ok := val.(*model.UserExample)
	if !ok {
		return nil, database.ErrRecordNotFound
	}
	return table, nil
}

func (m *userExampleCacheManager) handleConditionCacheHit(ctx context.Context, cacheKey string, cachedID uint64, s []scopes.Scope) (*model.UserExample, bool, error) {
	record, getErr := m.get(ctx, cachedID, func() (*model.UserExample, error) {
		table := &model.UserExample{}
		db := database.GetDB().WithContext(ctx).Scopes(s...)
		err := db.Where("id = ?", cachedID).First(table).Error
		if err != nil {
			return nil, err
		}
		return table, nil
	})
	if getErr == nil && record != nil && record.ID == cachedID {
		return record, true, nil
	}
	if getErr != nil {
		if delErr := m.cache.DelByKey(ctx, cacheKey); delErr != nil {
			logger.WarnWithCtx(ctx, "cache.DelByKey error", logger.Err(delErr), logger.Any("key", cacheKey))
		}
	}
	return nil, false, nil
}

func (m *userExampleCacheManager) cacheConditionResult(ctx context.Context, cacheKey string, record *model.UserExample) {
	if record == nil {
		return
	}
	expireTime := UserExampleGetRandomExpireTime(cache.UserExampleExpireTime)
	if cacheErr := m.cache.SetIDByKey(ctx, cacheKey, record.ID, expireTime); cacheErr != nil {
		logger.WarnWithCtx(ctx, "cache.SetIDByKey error", logger.Err(cacheErr), logger.Any("key", cacheKey), logger.Any("id", record.ID))
	}
	if cacheErr := m.cache.Set(ctx, record.ID, record, expireTime); cacheErr != nil {
		logger.WarnWithCtx(ctx, "cache.Set error", logger.Err(cacheErr), logger.Any("id", record.ID))
	}
}

func (m *userExampleCacheManager) executeConditionQueryWithSingleflight(ctx context.Context, key string, cacheKey string, queryFunc func() (*model.UserExample, error), s []scopes.Scope) (*model.UserExample, error) {
	val, sfErr, _ := m.sfg.Do("one_condition:"+key, func() (interface{}, error) {
		lockKey := "lock:refresh:" + cacheKey
		if lock, lockErr := m.cache.GetLock(ctx, lockKey); lockErr == nil {
			defer func() {
				if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "cache: unlock refresh lock failed", logger.Err(unlockErr))
				}
			}()
			if cachedID, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil && cachedID != 0 {
				if record, hit, hitErr := m.handleConditionCacheHit(ctx, cacheKey, cachedID, s); hit {
					return record, hitErr
				}
			}
		}
		record, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				if placeholderErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); placeholderErr != nil {
					logger.WarnWithCtx(ctx, "cache.SetPlaceholderByKey error", logger.Err(placeholderErr), logger.Any("key", cacheKey))
				}
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}
		m.cacheConditionResult(ctx, cacheKey, record)
		return record, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	record, ok := val.(*model.UserExample)
	if !ok {
		return nil, database.ErrRecordNotFound
	}
	return record, nil
}

func (m *userExampleCacheManager) getCondition(ctx context.Context, key string, queryFunc func() (*model.UserExample, error), s []scopes.Scope) (*model.UserExample, error) {
	cacheKey := m.getConditionCacheKey(key)

	cachedID, err := m.cache.GetIDByKey(ctx, cacheKey)
	if err == nil && cachedID != 0 {
		if record, hit, hitErr := m.handleConditionCacheHit(ctx, cacheKey, cachedID, s); hit {
			return record, hitErr
		}
	}

	if errors.Is(err, database.ErrCacheNotFound) || cachedID == 0 {
		return m.executeConditionQueryWithSingleflight(ctx, key, cacheKey, queryFunc, s)
	}

	logger.WarnWithCtx(ctx, "cache.GetIDByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey))
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}
	return m.executeConditionQueryWithSingleflight(ctx, key, cacheKey, queryFunc, s)
}

func (m *userExampleCacheManager) getByCondition(ctx context.Context, key string, queryFunc func() ([]uint64, error)) ([]uint64, error) {
	cacheKey := m.getConditionCacheKey(key)

	ids, err := m.cache.GetIDsByKey(ctx, cacheKey)
	if err == nil {
		if len(ids) > consts.DaoMaxCacheableIDs {
			logger.WarnWithCtx(ctx, "cached id list too large, querying database directly", logger.Any("count", len(ids)), logger.Any("key", key))
			return queryFunc()
		}
		if ids == nil {
			return []uint64{}, nil
		}
		return ids, nil
	}

	if errors.Is(err, database.ErrCacheNotFound) {
		val, sfErr, _ := m.sfg.Do("ids_condition:"+key, func() (interface{}, error) {
			result, dbErr := queryFunc()
			if dbErr != nil {
				if placeholderErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); placeholderErr != nil {
					logger.WarnWithCtx(ctx, "cache.SetPlaceholderByKey error", logger.Err(placeholderErr), logger.Any("key", cacheKey))
				}
				return nil, dbErr
			}
			if len(result) > consts.DaoMaxCacheableIDs {
				logger.WarnWithCtx(ctx, "result set too large to cache", logger.Any("count", len(result)), logger.Any("key", key))
				return result, nil
			}
			expireTime := UserExampleGetRandomExpireTime(cache.UserExampleExpireTime)
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
		if result == nil {
			return []uint64{}, nil
		}
		return result, nil
	}

	logger.WarnWithCtx(ctx, "cache.GetIDsByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey))
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}
	val, sfErr, _ := m.sfg.Do("ids_condition:"+key, func() (interface{}, error) {
		result, dbErr := queryFunc()
		if dbErr != nil {
			return nil, dbErr
		}
		if len(result) > consts.DaoMaxCacheableIDs {
			logger.WarnWithCtx(ctx, "result set too large to cache (fallback)", logger.Any("count", len(result)), logger.Any("key", key))
			return result, nil
		}
		expireTime := UserExampleGetRandomExpireTime(cache.UserExampleExpireTime)
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
	if result == nil {
		return []uint64{}, nil
	}
	return result, nil
}

func (m *userExampleCacheManager) getByIDs(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*model.UserExample, error)) (map[uint64]*model.UserExample, error) {
	if len(ids) > consts.DaoMaxBatchSize {
		result := make(map[uint64]*model.UserExample)
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
			for id, record := range batchResult {
				result[id] = record
			}
		}
		return result, nil
	}
	return m.getByIDsBatch(ctx, ids, queryFunc)
}

func (m *userExampleCacheManager) findMissedIDs(ids []uint64, itemMap map[uint64]*model.UserExample) []uint64 {
	var missedIDs []uint64
	for _, id := range ids {
		if _, ok := itemMap[id]; !ok {
			missedIDs = append(missedIDs, id)
		}
	}
	return missedIDs
}

func (m *userExampleCacheManager) setPlaceholdersForMissingRecords(ctx context.Context, records []*model.UserExample, missedIDs []uint64) {
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

func (m *userExampleCacheManager) getByIDsBatch(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*model.UserExample, error)) (map[uint64]*model.UserExample, error) {
	itemMap, err := m.cache.MultiGet(ctx, ids)
	if err != nil {
		logger.WarnWithCtx(ctx, "cache.MultiGet error, falling back to database", logger.Err(err), logger.Any("ids", ids))
		itemMap = make(map[uint64]*model.UserExample)
	}

	missedIDs := m.findMissedIDs(ids, itemMap)
	if len(missedIDs) > 0 {
		records, err := queryFunc(missedIDs)
		if err != nil {
			return nil, err
		}
		if len(records) > 0 {
			for _, record := range records {
				itemMap[record.ID] = record
			}
			expireTime := UserExampleGetRandomExpireTime(cache.UserExampleExpireTime)
			if cacheErr := m.cache.MultiSet(ctx, records, expireTime); cacheErr != nil {
				logger.WarnWithCtx(ctx, "cache.MultiSet error", logger.Err(cacheErr), logger.Any("ids", missedIDs))
			}
		}
		if len(records) < len(missedIDs) {
			m.setPlaceholdersForMissingRecords(ctx, records, missedIDs)
		}
	}
	return itemMap, nil
}

// ==================== DAO 实现 ====================

type userExampleDao struct {
	db           *gorm.DB
	cache        cache.UserExampleCache
	cacheManager *userExampleCacheManager
	sfg          *singleflight.Group
}

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

// -------------------- 写入操作 --------------------

func (d *userExampleDao) Create(ctx context.Context, table *model.UserExample) error {
	defer func() {
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "Create: failed to delete condition cache", logger.Err(err))
		}
	}()
	return d.db.WithContext(ctx).Create(table).Error
}

func (d *userExampleDao) CreateInBatches(ctx context.Context, tables []*model.UserExample, batchSize int) error {
	defer func() {
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "CreateInBatches: failed to delete condition cache", logger.Err(err))
		}
	}()
	return d.db.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}

func (d *userExampleDao) CreateByTx(ctx context.Context, tx *gorm.DB, table *model.UserExample) (uint64, error) {
	defer func() {
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "CreateByTx: failed to delete condition cache", logger.Err(err))
		}
	}()
	err := tx.WithContext(ctx).Create(table).Error
	return table.ID, err
}

func (d *userExampleDao) CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, tables []*model.UserExample, batchSize int) error {
	defer func() {
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "CreateByInBatchesTx: failed to delete condition cache", logger.Err(err))
		}
	}()
	return tx.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}

// -------------------- 缓存删除辅助 --------------------

func (d *userExampleDao) deleteCache(ctx context.Context, id uint64, deleteType string) error {
	if d.cache == nil {
		return nil
	}
	var errs []error
	switch deleteType {
	case consts.DaoDeleteTypeSingle:
		if err := d.cache.Del(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("delete single cache failed: %w", err))
		}
	case consts.DaoDeleteTypeCondition:
		prefixes := []string{
			cache.UserExampleCachePrefixKey + "condition",
			cache.UserExampleCachePrefixKey + "columns",
			cache.UserExampleCachePrefixKey + "count",
			cache.UserExampleCachePrefixKey + "exists",
		}
		for _, prefix := range prefixes {
			if err := d.cache.DelByPrefix(ctx, prefix); err != nil {
				errs = append(errs, fmt.Errorf("delete %s cache failed: %w", prefix, err))
			}
		}
	case consts.DaoDeleteTypeAll:
		if err := d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey); err != nil {
			errs = append(errs, fmt.Errorf("delete all cache failed: %w", err))
		}
	default:
		return nil
	}
	if len(errs) > 0 {
		for _, err := range errs {
			logger.WarnWithCtx(ctx, "cache: failed to delete",
				logger.String("type", deleteType),
				logger.Any("id", id),
				logger.Err(err))
		}
		return errs[0]
	}
	return nil
}

func (d *userExampleDao) executeDelayedDelete(ctx context.Context, id uint64, ids []uint64, deleteType string) {
	if id > 0 {
		if err := d.deleteCache(ctx, id, consts.DaoDeleteTypeSingle); err != nil {
			logger.WarnWithCtx(ctx, "delayedDoubleDelete: failed to delete single cache",
				logger.Err(err), logger.Any("id", id))
		}
	}
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
			logger.Err(firstErr), logger.Any("failed_ids", failedIDs), logger.Int("total_failed", len(failedIDs)))
	}
	switch deleteType {
	case consts.DaoDeleteTypeAll:
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
			logger.WarnWithCtx(ctx, "delayedDoubleDelete: failed to delete all cache",
				logger.Err(err), logger.String("type", consts.DaoDeleteTypeAll))
		}
	case consts.DaoDeleteTypeCondition:
		if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
			logger.WarnWithCtx(ctx, "delayedDoubleDelete: failed to delete condition cache",
				logger.Err(err), logger.String("type", consts.DaoDeleteTypeCondition))
		}
	}
}

func (d *userExampleDao) delayedDoubleDelete(ctx context.Context, id uint64, ids []uint64, deleteType string) {
	if d.cache == nil {
		return
	}
	bgCtx := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.WarnWithCtx(bgCtx, "delayedDoubleDelete panic recovered",
					logger.Any("recover", r), logger.Any("id", id), logger.String("deleteType", deleteType))
			}
		}()
		time.Sleep(consts.DaoDelayedDeleteInterval)
		d.executeDelayedDelete(bgCtx, id, ids, deleteType)
	}()
}

// -------------------- 删除操作 --------------------

func (d *userExampleDao) DeleteByID(ctx context.Context, id uint64) error {
	if err := d.deleteCache(ctx, id, consts.DaoDeleteTypeSingle); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete single cache failed", logger.Err(err), logger.Any("id", id))
	}
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete condition cache failed", logger.Err(err), logger.Any("id", id))
	}
	err := d.db.WithContext(ctx).Where("id = ?", id).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByID: delete from database failed, id=%d: %w", id, err)
	}
	d.delayedDoubleDelete(ctx, id, nil, consts.DaoDeleteTypeCondition)
	return nil
}

func (d *userExampleDao) DeleteByIDs(ctx context.Context, ids []uint64) error {
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
			logger.Err(firstErr), logger.Any("failed_ids", failedIDs), logger.Int("total_failed", len(failedIDs)), logger.Any("ids", ids))
	}
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete condition cache failed", logger.Err(err), logger.Any("ids", ids))
	}
	err := d.db.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByIDs: delete from database failed, ids=%v: %w", ids, err)
	}
	d.delayedDoubleDelete(ctx, 0, ids, consts.DaoDeleteTypeCondition)
	return nil
}

func (d *userExampleDao) DeleteByCondition(ctx context.Context, c *query.Conditions) error {
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete all cache failed", logger.Err(err))
	}
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("DeleteByCondition: convert conditions to gorm failed: %w", err)
	}
	err = d.db.WithContext(ctx).Where(queryStr, args...).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByCondition: delete from database failed, query=%s, args=%v: %w", queryStr, args, err)
	}
	d.delayedDoubleDelete(ctx, 0, nil, consts.DaoDeleteTypeAll)
	return nil
}

// -------------------- 事务删除操作 --------------------

func (d *userExampleDao) DeleteByTx(ctx context.Context, tx *gorm.DB, id uint64) error {
	// 【架构修复】：统一使用 GORM 的 Delete 方法，事务内不清理缓存
	err := tx.WithContext(ctx).Where("id = ?", id).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByTx: delete failed, id=%d: %w", id, err)
	}
	return nil
}

func (d *userExampleDao) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) error {
	err := tx.WithContext(ctx).Where("id IN (?)", ids).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByIDsTx: delete from database failed, ids=%v: %w", ids, err)
	}
	return nil
}

func (d *userExampleDao) DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("DeleteByTxCondition: convert conditions to gorm failed: %w", err)
	}
	err = tx.WithContext(ctx).Where(queryStr, args...).Delete(&model.UserExample{}).Error
	if err != nil {
		return fmt.Errorf("DeleteByTxCondition: delete from database failed, query=%s, args=%v: %w", queryStr, args, err)
	}
	return nil
}

func (d *userExampleDao) ClearCache(ctx context.Context) error {
	if d.cache != nil {
		return d.cache.DelByPrefix(ctx, cache.UserExampleCachePrefixKey)
	}
	return nil
}

// -------------------- 更新操作 --------------------

func (d *userExampleDao) updateDataByID(db *gorm.DB, table *model.UserExample) error {
	if table.ID < 1 {
		return errors.New("id cannot be 0")
	}
	update := map[string]interface{}{}
	// todo generate the update fields code to here
	if len(update) == 0 {
		return errors.New("no fields to update")
	}
	return db.Model(table).Updates(update).Error
}

func (d *userExampleDao) UpdateByID(ctx context.Context, table *model.UserExample) error {
	if err := d.deleteCache(ctx, table.ID, consts.DaoDeleteTypeSingle); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete single cache failed", logger.Err(err), logger.Any("id", table.ID))
	}
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete condition cache failed", logger.Err(err), logger.Any("id", table.ID))
	}
	err := d.updateDataByID(d.db.WithContext(ctx), table)
	if err != nil {
		return fmt.Errorf("UpdateByID: update database failed, id=%d: %w", table.ID, err)
	}
	d.delayedDoubleDelete(ctx, table.ID, nil, consts.DaoDeleteTypeCondition)
	return nil
}

func (d *userExampleDao) executeUpdateByCondition(ctx context.Context, db *gorm.DB, c *query.Conditions, update map[string]interface{}) error {
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return fmt.Errorf("convert conditions to gorm failed: %w", err)
	}
	err = db.WithContext(ctx).Model(&model.UserExample{}).Where(queryStr, args...).Updates(update).Error
	if err != nil {
		return fmt.Errorf("update database failed, query=%s, args=%v: %w", queryStr, args, err)
	}
	return nil
}

func (d *userExampleDao) UpdateByCondition(ctx context.Context, c *query.Conditions, table *model.UserExample) error {
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
		logger.WarnWithCtx(ctx, "pre-delete all cache failed", logger.Err(err))
	}
	update := map[string]interface{}{}
	// todo generate the update fields code to here
	if len(update) == 0 {
		return errors.New("no fields to update")
	}
	if err := d.executeUpdateByCondition(ctx, d.db, c, update); err != nil {
		return fmt.Errorf("UpdateByCondition: %w", err)
	}
	d.delayedDoubleDelete(ctx, 0, nil, consts.DaoDeleteTypeAll)
	return nil
}

// -------------------- 事务更新操作 --------------------

func (d *userExampleDao) UpdateByTx(ctx context.Context, tx *gorm.DB, table *model.UserExample) error {
	err := d.updateDataByID(tx.WithContext(ctx), table)
	if err != nil {
		return fmt.Errorf("UpdateByTx: update database failed, id=%d: %w", table.ID, err)
	}
	return nil
}

func (d *userExampleDao) UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, table *model.UserExample) error {
	update := map[string]interface{}{}
	// todo generate the update fields code to here
	if len(update) == 0 {
		return errors.New("no fields to update")
	}
	if err := d.executeUpdateByCondition(ctx, tx, c, update); err != nil {
		return fmt.Errorf("UpdateByConditionTx: %w", err)
	}
	return nil
}

func (d *userExampleDao) ExecByCustomFunc(ctx context.Context, updateFunc func(*gorm.DB) *gorm.DB) error {
	if err := d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll); err != nil {
		logger.WarnWithCtx(ctx, "ExecByCustomFunc: failed to delete all cache", logger.Err(err))
	}
	db := d.db.WithContext(ctx)
	db = updateFunc(db)
	var err error
	if db.Statement != nil && db.Statement.SQL.Len() > 0 {
		err = db.Exec(db.Statement.SQL.String(), db.Statement.Vars...).Error
	} else {
		err = db.Error
	}
	d.delayedDoubleDelete(ctx, 0, nil, consts.DaoDeleteTypeAll)
	return err
}

// -------------------- 外部缓存清理方法 --------------------

func (d *userExampleDao) CleanCacheByID(ctx context.Context, id uint64) error {
	return d.deleteCache(ctx, id, consts.DaoDeleteTypeSingle)
}

func (d *userExampleDao) CleanCacheByIDs(ctx context.Context, ids []uint64) error {
	var errs []error
	for _, id := range ids {
		if err := d.deleteCache(ctx, id, consts.DaoDeleteTypeSingle); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("CleanCacheByIDs: errors occurred: %v", errs)
	}
	return nil
}

func (d *userExampleDao) CleanCacheByCondition(ctx context.Context, c *query.Conditions) error {
	return d.deleteCache(ctx, 0, consts.DaoDeleteTypeCondition)
}

func (d *userExampleDao) CleanCacheAll(ctx context.Context) error {
	return d.deleteCache(ctx, 0, consts.DaoDeleteTypeAll)
}

// ==================== 查询方法 ====================

func (d *userExampleDao) GetByID(ctx context.Context, id uint64, opts ...UserExampleQueryOption) (*model.UserExample, error) {
	s, _, _ := userExampleApplyOptions(opts...)

	if d.cacheManager == nil {
		record := &model.UserExample{}
		db := d.db.WithContext(ctx).Scopes(s...)
		err := db.Where("id = ?", id).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, fmt.Errorf("GetByID: query database failed, id=%d: %w", id, err)
		}
		return record, nil
	}

	return d.cacheManager.get(ctx, id, func() (*model.UserExample, error) {
		table := &model.UserExample{}
		db := d.db.WithContext(ctx).Scopes(s...)
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

func (d *userExampleDao) queryByColumnsWithDB(db *gorm.DB, params *query.Params, queryStr string, args []interface{}) (*UserExampleColumnsResult, error) {
	var total int64
	var records []*model.UserExample
	if params.Sort != consts.DaoSortIgnoreCount {
		err := db.Model(&model.UserExample{}).Where(queryStr, args...).Count(&total).Error
		if err != nil {
			return nil, err
		}
		if total == 0 {
			return &UserExampleColumnsResult{Records: []*model.UserExample{}, Total: 0}, nil
		}
	}
	order, limit, offset := params.ConvertToPage()
	err := db.Order(order).Limit(limit).Offset(offset).Where(queryStr, args...).Find(&records).Error
	if err != nil {
		return nil, err
	}
	return &UserExampleColumnsResult{Records: records, Total: total}, nil
}

func (d *userExampleDao) handleColumnsCacheHit(ctx context.Context, fullCacheKey string, s []scopes.Scope, cachedTotal uint64) (*UserExampleColumnsResult, bool, error) {
	ids, idsErr := d.cache.GetIDsByKey(ctx, fullCacheKey+":ids")
	if idsErr != nil || len(ids) == 0 {
		return nil, false, nil
	}
	recordsMap, getErr := d.cacheManager.getByIDs(ctx, ids, func(missedIDs []uint64) ([]*model.UserExample, error) {
		db := d.db.WithContext(ctx).Scopes(s...)
		var records []*model.UserExample
		dbErr := db.Where("id IN (?)", missedIDs).Find(&records).Error
		return records, dbErr
	})
	if getErr != nil || len(recordsMap) == 0 {
		return nil, false, nil
	}

	records := make([]*model.UserExample, 0, len(ids))
	for _, id := range ids {
		if record, ok := recordsMap[id]; ok {
			records = append(records, record)
		}
	}

	if len(records) != len(ids) {
		logger.WarnWithCtx(ctx, "handleColumnsCacheHit: records count mismatch ids count, invalidating hit",
			logger.Int("expected", len(ids)), logger.Int("actual", len(records)))
		return nil, false, nil
	}

	return &UserExampleColumnsResult{Records: records, Total: int64(cachedTotal)}, true, nil
}

func (d *userExampleDao) cacheColumnsResult(ctx context.Context, fullCacheKey string, res *UserExampleColumnsResult) {
	if len(res.Records) > consts.DaoMaxCacheableRecords || res.Total <= 0 {
		return
	}
	expireTime := UserExampleGetRandomExpireTime(cache.UserExampleExpireTime)
	if setErr := d.cache.SetIDByKey(ctx, fullCacheKey+":total", uint64(res.Total), expireTime); setErr != nil {
		logger.WarnWithCtx(ctx, "cache: failed to set total count",
			logger.Err(setErr), logger.String("key", fullCacheKey+":total"), logger.Uint64("total", uint64(res.Total)))
	}
	ids := make([]uint64, 0, len(res.Records))
	for _, record := range res.Records {
		ids = append(ids, record.ID)
	}
	if setErr := d.cache.SetIDsByKey(ctx, fullCacheKey+":ids", ids, expireTime); setErr != nil {
		logger.WarnWithCtx(ctx, "cache: failed to set ID list",
			logger.Err(setErr), logger.String("key", fullCacheKey+":ids"), logger.Int("id_count", len(ids)))
	}
	if setErr := d.cache.MultiSet(ctx, res.Records, expireTime); setErr != nil {
		logger.WarnWithCtx(ctx, "cache: failed to multi-set records",
			logger.Err(setErr), logger.Any("count", len(res.Records)), logger.Int("total_records", len(res.Records)))
	}
}

func (d *userExampleDao) queryColumnsWithoutCache(ctx context.Context, singleflightKey string, params *query.Params, queryStr string, args []interface{}, s []scopes.Scope) (*UserExampleColumnsResult, error) {
	val, sfErr, _ := d.sfg.Do(singleflightKey, func() (interface{}, error) {
		db := d.db.WithContext(ctx).Scopes(s...)
		return d.queryByColumnsWithDB(db, params, queryStr, args)
	})
	if sfErr != nil {
		return nil, sfErr
	}
	res, ok := val.(*UserExampleColumnsResult)
	if !ok {
		return nil, errors.New("type assertion failed: expected *UserExampleColumnsResult")
	}
	if len(res.Records) > consts.DaoMaxCacheableRecords {
		logger.WarnWithCtx(ctx, "GetByColumns: result set too large",
			logger.Any("count", len(res.Records)),
			logger.String("cache_key", gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%d_%d_%s", queryStr, args, params.Page, params.Limit, params.Sort)))))
	}
	return res, nil
}

func (d *userExampleDao) executeColumnsQueryWithCache(ctx context.Context, singleflightKey, fullCacheKey, cacheKey string, params *query.Params, queryStr string, args []interface{}, s []scopes.Scope) (*UserExampleColumnsResult, error) {
	val, sfErr, _ := d.sfg.Do(singleflightKey, func() (interface{}, error) {
		cachedTotal, cacheErr := d.cache.GetIDByKey(ctx, fullCacheKey+":total")
		if cacheErr == nil {
			if result, hit, hitErr := d.handleColumnsCacheHit(ctx, fullCacheKey, s, cachedTotal); hit {
				return result, hitErr
			}
		}
		lockKey := "lock:refresh:" + fullCacheKey
		if lock, lockErr := d.cache.GetLock(ctx, lockKey); lockErr == nil {
			defer func() {
				if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "cache: unlock refresh lock failed", logger.Err(unlockErr))
				}
			}()
			if cachedTotal, cacheErr := d.cache.GetIDByKey(ctx, fullCacheKey+":total"); cacheErr == nil {
				if result, hit, hitErr := d.handleColumnsCacheHit(ctx, fullCacheKey, s, cachedTotal); hit {
					return result, hitErr
				}
			}
		}
		db := d.db.WithContext(ctx).Scopes(s...)
		result, dbErr := d.queryByColumnsWithDB(db, params, queryStr, args)
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				return &UserExampleColumnsResult{Records: []*model.UserExample{}, Total: 0}, nil
			}
			return nil, fmt.Errorf("GetByColumns: query database failed, page=%d, limit=%d: %w", params.Page, params.Limit, dbErr)
		}
		if len(result.Records) > consts.DaoMaxCacheableRecords {
			logger.WarnWithCtx(ctx, "GetByColumns: result set too large",
				logger.Any("count", len(result.Records)), logger.String("cache_key", cacheKey))
		}
		d.cacheColumnsResult(ctx, fullCacheKey, result)
		return result, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	res, ok := val.(*UserExampleColumnsResult)
	if !ok {
		return nil, errors.New("type assertion failed: expected *UserExampleColumnsResult")
	}
	return res, nil
}

func (d *userExampleDao) GetByColumns(ctx context.Context, params *query.Params, opts ...UserExampleQueryOption) ([]*model.UserExample, int64, error) {
	s, forceMaster, unscoped := userExampleApplyOptions(opts...)

	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, 0, fmt.Errorf("GetByColumns: convert query conditions failed, params=%+v: %w", params, err)
	}

	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%d_%d_%s_%v_%v", queryStr, args, params.Page, params.Limit, params.Sort, forceMaster, unscoped)))
	singleflightKey := "columns:" + cacheKey

	var result *UserExampleColumnsResult
	if d.cacheManager == nil {
		result, err = d.queryColumnsWithoutCache(ctx, singleflightKey, params, queryStr, args, s)
	} else {
		fullCacheKey := d.cacheManager.getColumnsCacheKey(cacheKey)
		result, err = d.executeColumnsQueryWithCache(ctx, singleflightKey, fullCacheKey, cacheKey, params, queryStr, args, s)
	}
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return []*model.UserExample{}, 0, nil
	}
	return result.Records, result.Total, nil
}

func (d *userExampleDao) GetOneByColumns(ctx context.Context, params *query.Params, opts ...UserExampleQueryOption) (*model.UserExample, error) {
	s, forceMaster, unscoped := userExampleApplyOptions(opts...)
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, fmt.Errorf("GetOneByColumns: convert query conditions failed, params=%+v: %w", params, err)
	}
	order, _, _ := params.ConvertToPage()

	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%s_%v_%v", queryStr, args, params.Sort, forceMaster, unscoped)))

	if d.cacheManager == nil {
		record := &model.UserExample{}
		db := d.db.WithContext(ctx).Scopes(s...)
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, fmt.Errorf("GetOneByColumns: query database failed, sort=%s: %w", params.Sort, err)
		}
		return record, nil
	}

	return d.cacheManager.getCondition(ctx, cacheKey, func() (*model.UserExample, error) {
		record := &model.UserExample{}
		db := d.db.WithContext(ctx).Scopes(s...)
		err := db.Order(order).Where(queryStr, args...).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, database.ErrRecordNotFound
			}
			return nil, fmt.Errorf("GetOneByColumns: query database failed, sort=%s: %w", params.Sort, err)
		}
		return record, nil
	}, s)
}

func (d *userExampleDao) GetByCondition(ctx context.Context, c *query.Conditions, opts ...UserExampleQueryOption) (ids []uint64, err error) {
	s, forceMaster, unscoped := userExampleApplyOptions(opts...)
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return nil, fmt.Errorf("GetByCondition: convert conditions to gorm failed, conditions=%+v: %w", c, err)
	}

	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v_%v", queryStr, args, forceMaster, unscoped)))

	if d.cacheManager == nil {
		var tables []*model.UserExample
		db := d.db.WithContext(ctx).Scopes(s...)
		err = db.Where(queryStr, args...).Find(&tables).Error
		if err != nil {
			return nil, fmt.Errorf("GetByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		if len(tables) > consts.DaoMaxCacheableIDs {
			logger.WarnWithCtx(ctx, "GetByCondition: result set too large",
				logger.Any("count", len(tables)), logger.String("cache_key", cacheKey))
		}
		result := make([]uint64, 0, len(tables))
		for _, table := range tables {
			result = append(result, table.ID)
		}
		return result, nil
	}

	result, err := d.cacheManager.getByCondition(ctx, cacheKey, func() ([]uint64, error) {
		var tables []*model.UserExample
		db := d.db.WithContext(ctx).Scopes(s...)
		err = db.Where(queryStr, args...).Find(&tables).Error
		if err != nil {
			return nil, fmt.Errorf("GetByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		result := make([]uint64, 0, len(tables))
		for _, table := range tables {
			result = append(result, table.ID)
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return []uint64{}, nil
	}
	return result, nil
}

func (d *userExampleDao) GetByIDs(ctx context.Context, ids []uint64, opts ...UserExampleQueryOption) (map[uint64]*model.UserExample, error) {
	s, _, _ := userExampleApplyOptions(opts...)

	if d.cacheManager == nil {
		var records []*model.UserExample
		db := d.db.WithContext(ctx).Scopes(s...)
		err := db.Where("id IN (?)", ids).Find(&records).Error
		if err != nil {
			return nil, fmt.Errorf("GetByIDs: query database failed, ids=%v: %w", ids, err)
		}
		itemMap := make(map[uint64]*model.UserExample, len(records))
		for _, record := range records {
			itemMap[record.ID] = record
		}
		return itemMap, nil
	}

	itemMap, err := d.cacheManager.getByIDs(ctx, ids, func(missedIDs []uint64) ([]*model.UserExample, error) {
		var records []*model.UserExample
		db := d.db.WithContext(ctx).Scopes(s...)
		err := db.Where("id IN (?)", missedIDs).Find(&records).Error
		if err != nil {
			return nil, fmt.Errorf("GetByIDs: query database failed, missed_ids=%v: %w", missedIDs, err)
		}
		return records, nil
	})
	if err != nil {
		return nil, err
	}
	if itemMap == nil {
		return make(map[uint64]*model.UserExample), nil
	}
	return itemMap, nil
}

func (d *userExampleDao) CountByCondition(ctx context.Context, c *query.Conditions, opts ...UserExampleQueryOption) (int64, error) {
	s, forceMaster, unscoped := userExampleApplyOptions(opts...)
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return 0, fmt.Errorf("CountByCondition: convert conditions to gorm failed, conditions=%+v: %w", c, err)
	}

	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v_%v", queryStr, args, forceMaster, unscoped)))
	countCacheKey := "count:" + cacheKey

	if d.cacheManager == nil {
		var count int64
		db := d.db.WithContext(ctx).Scopes(s...)
		err = db.Model(&model.UserExample{}).Where(queryStr, args...).Count(&count).Error
		if err != nil {
			return 0, fmt.Errorf("CountByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		return count, nil
	}

	cachedCount, err := d.cache.GetIDByKey(ctx, countCacheKey)
	if err == nil {
		return int64(cachedCount), nil
	}

	val, sfErr, _ := d.sfg.Do(countCacheKey, func() (interface{}, error) {
		lockKey := "lock:refresh:" + countCacheKey
		if lock, lockErr := d.cache.GetLock(ctx, lockKey); lockErr == nil {
			defer func() {
				if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "cache: unlock refresh lock failed", logger.Err(unlockErr))
				}
			}()
			if cachedCount, cacheErr := d.cache.GetIDByKey(ctx, countCacheKey); cacheErr == nil {
				return int64(cachedCount), nil
			}
		}
		var count int64
		db := d.db.WithContext(ctx).Scopes(s...)
		err = db.Model(&model.UserExample{}).Where(queryStr, args...).Count(&count).Error
		if err != nil {
			return 0, fmt.Errorf("CountByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		expireTime := UserExampleGetRandomExpireTime(cache.UserExampleExpireTime)
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

func (d *userExampleDao) ExistsByCondition(ctx context.Context, c *query.Conditions, opts ...UserExampleQueryOption) (bool, error) {
	s, forceMaster, unscoped := userExampleApplyOptions(opts...)
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return false, fmt.Errorf("ExistsByCondition: convert conditions to gorm failed, conditions=%+v: %w", c, err)
	}

	cacheKey := gocrypto.Md5([]byte(fmt.Sprintf("%s_%v_%v_%v", queryStr, args, forceMaster, unscoped)))
	existsCacheKey := "exists:" + cacheKey

	if d.cacheManager == nil {
		var exists bool
		db := d.db.WithContext(ctx).Scopes(s...)
		err = db.Model(&model.UserExample{}).Where(queryStr, args...).Select("1").Limit(1).Scan(&exists).Error
		if err != nil {
			return false, fmt.Errorf("ExistsByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		return exists, nil
	}

	cachedValue, err := d.cache.GetIDByKey(ctx, existsCacheKey)
	if err == nil {
		return cachedValue > 0, nil
	}

	val, sfErr, _ := d.sfg.Do(existsCacheKey, func() (interface{}, error) {
		lockKey := "lock:refresh:" + existsCacheKey
		if lock, lockErr := d.cache.GetLock(ctx, lockKey); lockErr == nil {
			defer func() {
				if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "cache: unlock refresh lock failed", logger.Err(unlockErr))
				}
			}()
			if cachedValue, cacheErr := d.cache.GetIDByKey(ctx, existsCacheKey); cacheErr == nil {
				return cachedValue > 0, nil
			}
		}
		var exists bool
		db := d.db.WithContext(ctx).Scopes(s...)
		err = db.Model(&model.UserExample{}).Where(queryStr, args...).Select("1").Limit(1).Scan(&exists).Error
		if err != nil {
			return false, fmt.Errorf("ExistsByCondition: query database failed, conditions=%+v: %w", c, err)
		}
		cacheValue := uint64(0)
		if exists {
			cacheValue = 1
		}
		expireTime := UserExampleGetRandomExpireTime(cache.UserExampleExpireTime)
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

func (d *userExampleDao) GetByCustomQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, result interface{}, page, limit int, opts ...UserExampleQueryOption) (int64, error) {
	s, _, _ := userExampleApplyOptions(opts...)
	var total int64 = -1

	db := d.db.WithContext(ctx).Scopes(s...)
	db = queryFunc(db)

	if page >= 0 && limit > 0 {
		stmt := db.Statement
		if stmt != nil {
			if stmt.Table != "" || stmt.Model != nil {
				err := db.Count(&total).Error
				if err != nil {
					return 0, fmt.Errorf("GetByCustomQuery: count failed: %w", err)
				}
				offset := page * limit
				db = db.Offset(offset).Limit(limit)
			} else if stmt.SQL.Len() > 0 {
				originalSQL := stmt.SQL.String()
				countSQL := d.convertToCountSQL(ctx, originalSQL)
				var count int64
				err := db.Raw(countSQL, stmt.Vars...).Scan(&count).Error
				if err != nil {
					return 0, fmt.Errorf("GetByCustomQuery: count raw sql failed: %w", err)
				}
				total = count
				pagedSQL := originalSQL + " LIMIT ? OFFSET ?"
				offset := page * limit
				db = db.Raw(pagedSQL, append(stmt.Vars, limit, offset)...)
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

func (d *userExampleDao) convertToCountSQL(ctx context.Context, sql string) string {
	if sql == "" {
		return consts.DaoEmptyCountSQL
	}

	sql = strings.TrimSpace(sql)
	lowerSQL := strings.ToLower(sql)

	if idx := strings.Index(lowerSQL, " for update"); idx != -1 {
		sql = sql[:idx]
		lowerSQL = strings.ToLower(sql)
	}
	if idx := strings.Index(lowerSQL, " offset "); idx != -1 {
		sql = sql[:idx]
		lowerSQL = strings.ToLower(sql)
	}
	if idx := strings.Index(lowerSQL, " limit "); idx != -1 {
		sql = sql[:idx]
		lowerSQL = strings.ToLower(sql)
	}
	orderByIndex := strings.LastIndex(lowerSQL, " order by")
	if orderByIndex != -1 {
		afterOrderBy := sql[orderByIndex:]
		openParens := strings.Count(afterOrderBy, "(")
		closeParens := strings.Count(afterOrderBy, ")")
		if openParens == closeParens || openParens-closeParens <= 1 {
			sql = sql[:orderByIndex]
		}
	}
	trimmedSQL := strings.TrimSpace(sql)
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(trimmedSQL)), "select") {
		logger.InfoWithCtx(ctx, "convertToCountSQL: received non-SELECT SQL, returning empty result",
			logger.String("original_sql", sql),
			logger.String("processed_sql", trimmedSQL))
		return consts.DaoEmptyCountSQL
	}
	return "SELECT COUNT(*) FROM (" + trimmedSQL + ") AS count_query"
}
