// Package dao 提供泛型数据访问层基础组件
package dao

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/18721889353/sunshine/pkg/logger"
)

type cacheManager[T any] struct {
	cache      Cache[T]
	sfg        *singleflight.Group
	config     CacheConfig
	tableName  string
	basePrefix string
}

func newCacheManager[T any](c Cache[T], config CacheConfig, tableName string) *cacheManager[T] {
	camelName := underscoreToCamel(tableName)
	return &cacheManager[T]{
		cache:      c,
		sfg:        new(singleflight.Group),
		config:     config,
		tableName:  tableName,
		basePrefix: "data:" + camelName + ":",
	}
}

func underscoreToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func (m *cacheManager[T]) getSingleCachePrefix() string {
	camelName := underscoreToCamel(m.tableName)
	return "data:" + camelName + ":"
}

// -------------------- 单条缓存 --------------------

func (m *cacheManager[T]) get(ctx context.Context, id uint64, queryFunc func() (*T, error)) (*T, error) {
	record, err := m.cache.Get(ctx, id)
	if err == nil {
		return record, nil
	}

	if errors.Is(err, database.ErrCacheNotFound) {
		val, sfErr, _ := m.sfg.Do(fmt.Sprintf("%d", id), func() (interface{}, error) {
			lockKey := BuildLockKey(LockKeyPrefixRefresh, fmt.Sprintf("%d", id))
			lock, lockErr := m.cache.GetLock(ctx, lockKey)

			if lock != nil {
				defer func() {
					unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
					defer cancel()
					if _, unlockErr := lock.UnlockContext(unlockCtx); unlockErr != nil {
						logger.WarnWithCtx(ctx, "cache: unlock refresh lock failed", logger.Err(unlockErr))
					}
				}()
			}

			if lockErr == nil {
				if record, cacheErr := m.cache.Get(ctx, id); cacheErr == nil {
					return record, nil
				}
			} else {
				time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
				if record, cacheErr := m.cache.Get(ctx, id); cacheErr == nil {
					return record, nil
				}
			}

			table, dbErr := queryFunc()
			if dbErr != nil {
				if errors.Is(dbErr, gorm.ErrRecordNotFound) {
					if setErr := m.cache.SetPlaceholder(ctx, id); setErr != nil {
						logger.WarnWithCtx(ctx, "set placeholder failed", logger.Err(setErr))
					}
					return nil, database.ErrRecordNotFound
				}
				return nil, dbErr
			}
			expire := GetRandomExpireTime(m.config.DefaultExpireTime)
			if setErr := m.cache.Set(ctx, id, table, expire); setErr != nil {
				logger.WarnWithCtx(ctx, "cache set failed", logger.Err(setErr))
			}
			return table, nil
		})
		if sfErr != nil {
			return nil, sfErr
		}
		table, ok := val.(*T)
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return table, nil
	}

	logger.WarnWithCtx(ctx, "cache.Get error, falling back to database", logger.Err(err), logger.Any("id", id))
	return m.handleFallback(ctx, id, err, queryFunc)
}

func (m *cacheManager[T]) handleFallback(ctx context.Context, id uint64, err error, queryFunc func() (*T, error)) (*T, error) {
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}
	val, sfErr, _ := m.sfg.Do(fmt.Sprintf("%d", id), func() (interface{}, error) {
		lockKey := BuildLockKey(LockKeyPrefixRefresh, fmt.Sprintf("%d", id))
		if lock, lockErr := m.cache.GetLock(ctx, lockKey); lockErr == nil {
			if record, cacheErr := m.cache.Get(ctx, id); cacheErr == nil {
				if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "unlock failed in handleFallback", logger.Err(unlockErr))
				}
				return record, nil
			}
			if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
				logger.WarnWithCtx(ctx, "unlock failed in handleFallback", logger.Err(unlockErr))
			}
		}
		table, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				if setErr := m.cache.SetPlaceholder(ctx, id); setErr != nil {
					logger.WarnWithCtx(ctx, "set placeholder failed", logger.Err(setErr))
				}
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}
		expire := GetRandomExpireTime(m.config.DefaultExpireTime)
		if setErr := m.cache.Set(ctx, id, table, expire); setErr != nil {
			logger.WarnWithCtx(ctx, "cache set failed", logger.Err(setErr))
		}
		return table, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	table, ok := val.(*T)
	if !ok {
		return nil, database.ErrRecordNotFound
	}
	return table, nil
}

// -------------------- 条件单条缓存 --------------------

func (m *cacheManager[T]) getCondition(ctx context.Context, key string, queryFunc func() (*T, error)) (*T, error) {
	cacheKey := CacheKeyPrefixCondition + key
	cachedID, err := m.cache.GetIDByKey(ctx, cacheKey)
	if err == nil && cachedID != 0 {
		if record, hit, _ := m.getByCachedID(ctx, cachedID, nil); hit {
			return record, nil
		}
	}
	if errors.Is(err, database.ErrCacheNotFound) || cachedID == 0 {
		return m.executeConditionSingleflight(ctx, key, cacheKey, queryFunc)
	}
	logger.WarnWithCtx(ctx, "cache.GetIDByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey))
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}
	return m.executeConditionSingleflight(ctx, key, cacheKey, queryFunc)
}

// getByCachedID 参数 queryFunc 保留用于接口一致性，但实际未使用
func (m *cacheManager[T]) getByCachedID(ctx context.Context, cachedID uint64, _ func() (*T, error)) (*T, bool, error) {
	record, err := m.cache.Get(ctx, cachedID)
	if err == nil {
		return record, true, nil
	}
	return nil, false, nil
}

func (m *cacheManager[T]) executeConditionSingleflight(ctx context.Context, key, cacheKey string, queryFunc func() (*T, error)) (*T, error) {
	val, sfErr, _ := m.sfg.Do(SFKeyPrefixOneCondition+key, func() (interface{}, error) {
		lockKey := BuildLockKey(LockKeyPrefixRefresh, cacheKey)
		lock, lockErr := m.cache.GetLock(ctx, lockKey)
		if lock != nil {
			defer func() {
				unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				defer cancel()
				if _, unlockErr := lock.UnlockContext(unlockCtx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "unlock failed in executeConditionSingleflight", logger.Err(unlockErr))
				}
			}()
		}
		if lockErr == nil {
			if cachedID, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil && cachedID != 0 {
				if record, hit, _ := m.getByCachedID(ctx, cachedID, nil); hit {
					return record, nil
				}
			}
		} else {
			time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
			if cachedID, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil && cachedID != 0 {
				if record, hit, _ := m.getByCachedID(ctx, cachedID, nil); hit {
					return record, nil
				}
			}
		}
		record, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				if setErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); setErr != nil {
					logger.WarnWithCtx(ctx, "set placeholder by key failed", logger.Err(setErr))
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
	record, ok := val.(*T)
	if !ok {
		return nil, database.ErrRecordNotFound
	}
	return record, nil
}

func (m *cacheManager[T]) cacheConditionResult(ctx context.Context, cacheKey string, record *T) {
	if record == nil {
		return
	}
	id := GetObjectID(*record)
	if id == 0 {
		return
	}
	expire := GetRandomExpireTime(m.config.DefaultExpireTime)
	if setErr := m.cache.SetIDByKey(ctx, cacheKey, id, expire); setErr != nil {
		logger.WarnWithCtx(ctx, "set id by key failed", logger.Err(setErr))
	}
	if setErr := m.cache.Set(ctx, id, record, expire); setErr != nil {
		logger.WarnWithCtx(ctx, "cache set failed", logger.Err(setErr))
	}
}

// -------------------- 条件 ID 列表缓存 --------------------

func (m *cacheManager[T]) getByCondition(ctx context.Context, key string, queryFunc func() ([]uint64, error)) ([]uint64, error) {
	cacheKey := CacheKeyPrefixCondition + key

	ids, err := m.cache.GetIDsByKey(ctx, cacheKey)
	if err == nil {
		if len(ids) <= m.config.MaxCacheableIDs {
			return ids, nil
		}
		logger.WarnWithCtx(ctx, "cached id list too large, deleting stale cache",
			logger.Any("count", len(ids)), logger.Any("key", key))
		if delErr := m.cache.DelByKey(ctx, cacheKey); delErr != nil {
			logger.WarnWithCtx(ctx, "delete stale cache failed", logger.Err(delErr))
		}
	} else if !errors.Is(err, database.ErrCacheNotFound) {
		logger.WarnWithCtx(ctx, "cache.GetIDsByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey))
		if m.cache.IsPlaceholderErr(err) {
			return nil, database.ErrRecordNotFound
		}
	}

	val, sfErr, _ := m.sfg.Do(SFKeyPrefixIDsCondition+key, func() (interface{}, error) {
		lockKey := BuildLockKey(LockKeyPrefixRefresh, cacheKey)
		lock, lockErr := m.cache.GetLock(ctx, lockKey)
		if lock != nil {
			defer func() {
				unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				defer cancel()
				if _, unlockErr := lock.UnlockContext(unlockCtx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "unlock failed in getByCondition", logger.Err(unlockErr))
				}
			}()
		}
		if lockErr == nil {
			if cachedIDs, cacheErr := m.cache.GetIDsByKey(ctx, cacheKey); cacheErr == nil && len(cachedIDs) <= m.config.MaxCacheableIDs {
				return cachedIDs, nil
			}
		} else {
			time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
			if cachedIDs, cacheErr := m.cache.GetIDsByKey(ctx, cacheKey); cacheErr == nil && len(cachedIDs) <= m.config.MaxCacheableIDs {
				return cachedIDs, nil
			}
		}
		result, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				if setErr := m.cache.SetPlaceholderByKey(ctx, cacheKey); setErr != nil {
					logger.WarnWithCtx(ctx, "set placeholder by key failed", logger.Err(setErr))
				}
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}
		if len(result) > m.config.MaxCacheableIDs {
			logger.WarnWithCtx(ctx, "result set too large to cache", logger.Any("count", len(result)), logger.Any("key", key))
			return result, nil
		}
		expire := GetRandomExpireTime(m.config.DefaultExpireTime)
		if setErr := m.cache.SetIDsByKey(ctx, cacheKey, result, expire); setErr != nil {
			logger.WarnWithCtx(ctx, "set ids by key failed", logger.Err(setErr))
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

// -------------------- 批量缓存 --------------------

func (m *cacheManager[T]) getByIDs(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*T, error)) (map[uint64]*T, error) {
	if len(ids) > m.config.MaxBatchSize {
		result := make(map[uint64]*T)
		for i := 0; i < len(ids); i += m.config.MaxBatchSize {
			end := i + m.config.MaxBatchSize
			if end > len(ids) {
				end = len(ids)
			}
			batch := ids[i:end]
			batchResult, err := m.getByIDsBatch(ctx, batch, queryFunc)
			if err != nil {
				return nil, err
			}
			for id, rec := range batchResult {
				result[id] = rec
			}
		}
		return result, nil
	}
	return m.getByIDsBatch(ctx, ids, queryFunc)
}

func (m *cacheManager[T]) getByIDsBatch(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*T, error)) (map[uint64]*T, error) {
	itemMap, err := m.cache.MultiGet(ctx, ids)
	if err != nil {
		logger.WarnWithCtx(ctx, "cache.MultiGet error, falling back to database", logger.Err(err), logger.Any("ids", ids))
		itemMap = make(map[uint64]*T)
	}
	missed := m.findMissedIDs(ids, itemMap)
	if len(missed) == 0 {
		return itemMap, nil
	}
	sorted := make([]uint64, len(missed))
	copy(sorted, missed)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	hashKey := gocrypto.Md5([]byte(fmt.Sprintf("%v", sorted)))

	val, sfErr, _ := m.sfg.Do(SFKeyPrefixBatchIDs+hashKey, func() (interface{}, error) {
		lockKey := BuildLockKey(LockKeyPrefixRefreshBatch, hashKey)
		lock, lockErr := m.cache.GetLock(ctx, lockKey)
		if lock != nil {
			defer func() {
				unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				defer cancel()
				if _, unlockErr := lock.UnlockContext(unlockCtx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "unlock failed in getByIDsBatch", logger.Err(unlockErr))
				}
			}()
		}
		// 尝试从缓存获取全部
		if m.tryGetAllFromCache(ctx, missed, lockErr) {
			itemMapTmp, tmpErr := m.cache.MultiGet(ctx, missed)
			if tmpErr == nil {
				foundAll := true
				for _, id := range missed {
					if _, ok := itemMapTmp[id]; !ok {
						foundAll = false
						break
					}
				}
				if foundAll {
					return itemMapTmp, nil
				}
			}
		}
		// 从DB获取
		records, dbErr := queryFunc(missed)
		if dbErr != nil {
			return nil, dbErr
		}
		if len(records) > 0 {
			expire := GetRandomExpireTime(m.config.DefaultExpireTime)
			if setErr := m.cache.MultiSet(ctx, records, expire); setErr != nil {
				logger.WarnWithCtx(ctx, "multi set failed", logger.Err(setErr))
			}
		}
		m.setPlaceholdersForMissing(ctx, records, missed)
		resultMap := make(map[uint64]*T)
		for _, rec := range records {
			id := GetObjectID(*rec)
			if id != 0 {
				resultMap[id] = rec
			}
		}
		return resultMap, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	recordsMap, ok := val.(map[uint64]*T)
	if !ok {
		return nil, errors.New("type assertion failed for batch records map")
	}
	for id, rec := range recordsMap {
		itemMap[id] = rec
	}
	return itemMap, nil
}

// tryGetAllFromCache 辅助函数，尝试从缓存获取所有缺失ID
func (m *cacheManager[T]) tryGetAllFromCache(ctx context.Context, missed []uint64, lockErr error) bool {
	if lockErr == nil {
		return true
	}
	time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
	return true
}

func (m *cacheManager[T]) findMissedIDs(ids []uint64, itemMap map[uint64]*T) []uint64 {
	var missed []uint64
	for _, id := range ids {
		if _, ok := itemMap[id]; !ok {
			missed = append(missed, id)
		}
	}
	return missed
}

func (m *cacheManager[T]) setPlaceholdersForMissing(ctx context.Context, records []*T, missedIDs []uint64) {
	existing := make(map[uint64]bool)
	for _, rec := range records {
		id := GetObjectID(*rec)
		if id != 0 {
			existing[id] = true
		}
	}
	for _, id := range missedIDs {
		if !existing[id] {
			if setErr := m.cache.SetPlaceholder(ctx, id); setErr != nil {
				logger.WarnWithCtx(ctx, "set placeholder for missing id failed", logger.Err(setErr))
			}
		}
	}
}

// -------------------- 分页查询缓存 --------------------

func (m *cacheManager[T]) getColumns(ctx context.Context, queryFunc func() ([]*T, int64, error), idsQueryFunc func([]uint64) ([]*T, error), cacheKey string) ([]*T, int64, error) {
	fullKey := CacheKeyPrefixColumns + cacheKey

	// 尝试从缓存获取
	if total, ids, ok := m.tryGetColumnsFromCache(ctx, fullKey); ok {
		return ids, total, nil
	}

	val, sfErr, _ := m.sfg.Do(SFKeyPrefixColumns+cacheKey, func() (interface{}, error) {
		// 再次尝试从缓存获取（使用锁）
		if total, ids, ok := m.tryGetColumnsFromCacheWithLock(ctx, fullKey); ok {
			return struct {
				records []*T
				total   int64
			}{records: ids, total: total}, nil
		}
		// 从DB查询
		records, total, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				return struct {
					records []*T
					total   int64
				}{records: []*T{}, total: 0}, nil
			}
			return nil, dbErr
		}
		m.cacheColumnsResult(ctx, fullKey, records, total)
		return struct {
			records []*T
			total   int64
		}{records: records, total: total}, nil
	})
	if sfErr != nil {
		return nil, 0, sfErr
	}
	result, ok := val.(struct {
		records []*T
		total   int64
	})
	if !ok {
		return nil, 0, errors.New("type assertion failed for columns result")
	}
	return result.records, result.total, nil
}

// tryGetColumnsFromCache 尝试从缓存获取分页数据（无锁）
func (m *cacheManager[T]) tryGetColumnsFromCache(ctx context.Context, fullKey string) (int64, []*T, bool) {
	cachedTotal, err := m.cache.GetIDByKey(ctx, fullKey+":total")
	if err != nil {
		return 0, nil, false
	}
	ids, idsErr := m.cache.GetIDsByKey(ctx, fullKey+":ids")
	if idsErr != nil || len(ids) == 0 || len(ids) > m.config.MaxCacheableRecords {
		if idsErr == nil && len(ids) > m.config.MaxCacheableRecords {
			logger.WarnWithCtx(ctx, "cached ids count exceeds max, deleting stale cache",
				logger.Any("count", len(ids)), logger.String("key", fullKey))
			if delErr := m.cache.DelByKey(ctx, fullKey+":total"); delErr != nil {
				logger.WarnWithCtx(ctx, "delete total key failed", logger.Err(delErr))
			}
			if delErr := m.cache.DelByKey(ctx, fullKey+":ids"); delErr != nil {
				logger.WarnWithCtx(ctx, "delete ids key failed", logger.Err(delErr))
			}
		}
		return 0, nil, false
	}
	recordsMap, getErr := m.getByIDs(ctx, ids, nil) // 使用空查询，实际会直接走缓存
	if getErr != nil {
		return 0, nil, false
	}
	records := make([]*T, 0, len(ids))
	for _, id := range ids {
		if rec, ok := recordsMap[id]; ok {
			records = append(records, rec)
		}
	}
	if len(records) == len(ids) {
		return int64(cachedTotal), records, true
	}
	return 0, nil, false
}

// tryGetColumnsFromCacheWithLock 带锁尝试从缓存获取
func (m *cacheManager[T]) tryGetColumnsFromCacheWithLock(ctx context.Context, fullKey string) (int64, []*T, bool) {
	lockKey := BuildLockKey(LockKeyPrefixRefresh, fullKey)
	lock, lockErr := m.cache.GetLock(ctx, lockKey)
	if lock != nil {
		defer func() {
			unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			if _, unlockErr := lock.UnlockContext(unlockCtx); unlockErr != nil {
				logger.WarnWithCtx(ctx, "unlock failed in tryGetColumnsFromCacheWithLock", logger.Err(unlockErr))
			}
		}()
	}
	if lockErr == nil {
		return m.tryGetColumnsFromCache(ctx, fullKey)
	}
	time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
	return m.tryGetColumnsFromCache(ctx, fullKey)
}

func (m *cacheManager[T]) cacheColumnsResult(ctx context.Context, fullKey string, records []*T, total int64) {
	if len(records) <= m.config.MaxCacheableRecords && total > 0 {
		expire := GetRandomExpireTime(m.config.DefaultExpireTime)
		if setErr := m.cache.SetIDByKey(ctx, fullKey+":total", uint64(total), expire); setErr != nil {
			logger.WarnWithCtx(ctx, "set total key failed", logger.Err(setErr))
		}
		ids := make([]uint64, 0, len(records))
		for _, rec := range records {
			id := GetObjectID(*rec)
			if id != 0 {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			if setErr := m.cache.SetIDsByKey(ctx, fullKey+":ids", ids, expire); setErr != nil {
				logger.WarnWithCtx(ctx, "set ids key failed", logger.Err(setErr))
			}
			if setErr := m.cache.MultiSet(ctx, records, expire); setErr != nil {
				logger.WarnWithCtx(ctx, "multi set failed", logger.Err(setErr))
			}
		}
	}
}

// -------------------- 计数缓存 --------------------

func (m *cacheManager[T]) getCount(ctx context.Context, key string, queryFunc func() (int64, error)) (int64, error) {
	cacheKey := CacheKeyPrefixCount + key
	cached, err := m.cache.GetIDByKey(ctx, cacheKey)
	if err == nil {
		return int64(cached), nil
	}
	val, sfErr, _ := m.sfg.Do(cacheKey, func() (interface{}, error) {
		lockKey := BuildLockKey(LockKeyPrefixRefresh, cacheKey)
		lock, lockErr := m.cache.GetLock(ctx, lockKey)
		if lock != nil {
			defer func() {
				unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				defer cancel()
				if _, unlockErr := lock.UnlockContext(unlockCtx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "unlock failed in getCount", logger.Err(unlockErr))
				}
			}()
		}
		if lockErr == nil {
			if cached, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil {
				return int64(cached), nil
			}
		} else {
			time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
			if cached, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil {
				return int64(cached), nil
			}
		}
		count, dbErr := queryFunc()
		if dbErr != nil {
			return 0, dbErr
		}
		expire := GetRandomExpireTime(m.config.DefaultExpireTime)
		if setErr := m.cache.SetIDByKey(ctx, cacheKey, uint64(count), expire); setErr != nil {
			logger.WarnWithCtx(ctx, "set count key failed", logger.Err(setErr))
		}
		return count, nil
	})
	if sfErr != nil {
		return 0, sfErr
	}
	count, ok := val.(int64)
	if !ok {
		return 0, errors.New("type assertion failed for count")
	}
	return count, nil
}

// -------------------- 存在缓存 --------------------

func (m *cacheManager[T]) getExists(ctx context.Context, key string, queryFunc func() (bool, error)) (bool, error) {
	cacheKey := CacheKeyPrefixExists + key
	cached, err := m.cache.GetIDByKey(ctx, cacheKey)
	if err == nil {
		return cached > 0, nil
	}
	val, sfErr, _ := m.sfg.Do(cacheKey, func() (interface{}, error) {
		lockKey := BuildLockKey(LockKeyPrefixRefresh, cacheKey)
		lock, lockErr := m.cache.GetLock(ctx, lockKey)
		if lock != nil {
			defer func() {
				unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				defer cancel()
				if _, unlockErr := lock.UnlockContext(unlockCtx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "unlock failed in getExists", logger.Err(unlockErr))
				}
			}()
		}
		if lockErr == nil {
			if cached, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil {
				return cached > 0, nil
			}
		} else {
			time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
			if cached, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil {
				return cached > 0, nil
			}
		}
		exists, dbErr := queryFunc()
		if dbErr != nil {
			return false, dbErr
		}
		val := uint64(0)
		if exists {
			val = 1
		}
		expire := GetRandomExpireTime(m.config.DefaultExpireTime)
		if setErr := m.cache.SetIDByKey(ctx, cacheKey, val, expire); setErr != nil {
			logger.WarnWithCtx(ctx, "set exists key failed", logger.Err(setErr))
		}
		return exists, nil
	})
	if sfErr != nil {
		return false, sfErr
	}
	exists, ok := val.(bool)
	if !ok {
		return false, errors.New("type assertion failed for exists")
	}
	return exists, nil
}

// -------------------- 缓存删除（核心） --------------------

func (m *cacheManager[T]) deleteCache(ctx context.Context, id uint64, deleteType string) {
	if m.cache == nil {
		return
	}
	singlePrefix := m.getSingleCachePrefix()

	switch deleteType {
	case DeleteDaoTypeSingle:
		if delErr := m.cache.Del(ctx, id); delErr != nil {
			logger.WarnWithCtx(ctx, "cache Del failed", logger.Err(delErr), logger.Any("id", id))
		}
	case DeleteDaoTypeCondition:
		prefixes := []string{
			singlePrefix + CacheKeyPrefixCondition,
			singlePrefix + CacheKeyPrefixColumns,
			singlePrefix + CacheKeyPrefixCount,
			singlePrefix + CacheKeyPrefixExists,
		}
		for _, p := range prefixes {
			if delErr := m.cache.DelByPrefix(ctx, p); delErr != nil {
				logger.WarnWithCtx(ctx, "cache DelByPrefix failed", logger.Err(delErr), logger.String("prefix", p))
			}
		}
	case DeleteDaoTypeAll:
		prefixes := []string{
			singlePrefix + CacheKeyPrefixCondition,
			singlePrefix + CacheKeyPrefixColumns,
			singlePrefix + CacheKeyPrefixCount,
			singlePrefix + CacheKeyPrefixExists,
		}
		for _, p := range prefixes {
			if delErr := m.cache.DelByPrefix(ctx, p); delErr != nil {
				logger.WarnWithCtx(ctx, "cache DelByPrefix failed", logger.Err(delErr), logger.String("prefix", p))
			}
		}
		if delErr := m.cache.DelByPrefix(ctx, singlePrefix); delErr != nil {
			logger.WarnWithCtx(ctx, "cache DelByPrefix failed for singlePrefix", logger.Err(delErr), logger.String("prefix", singlePrefix))
		}
	default:
		if delErr := m.cache.DelByPrefix(ctx, ""); delErr != nil {
			logger.WarnWithCtx(ctx, "cache DelByPrefix failed with empty prefix", logger.Err(delErr))
		}
	}
}

func (m *cacheManager[T]) delayedDoubleDelete(ctx context.Context, id uint64, ids []uint64, deleteType string) {
	if m.cache == nil {
		return
	}
	bgCtx := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.WarnWithCtx(bgCtx, "delayedDoubleDelete panic recovered", logger.Any("recover", r))
			}
		}()
		time.Sleep(m.config.DelayedDeleteInterval)

		singlePrefix := m.getSingleCachePrefix()

		if id > 0 {
			if delErr := m.cache.Del(ctx, id); delErr != nil {
				logger.WarnWithCtx(bgCtx, "delayed delete single id failed", logger.Err(delErr), logger.Any("id", id))
			}
		}
		if len(ids) > 0 {
			for _, batchID := range ids {
				if delErr := m.cache.Del(ctx, batchID); delErr != nil {
					logger.WarnWithCtx(bgCtx, "delayed delete batch id failed", logger.Err(delErr), logger.Any("id", batchID))
				}
			}
		}

		switch deleteType {
		case DeleteDaoTypeCondition:
			prefixes := []string{
				singlePrefix + CacheKeyPrefixCondition,
				singlePrefix + CacheKeyPrefixColumns,
				singlePrefix + CacheKeyPrefixCount,
				singlePrefix + CacheKeyPrefixExists,
			}
			for _, p := range prefixes {
				if delErr := m.cache.DelByPrefix(ctx, p); delErr != nil {
					logger.WarnWithCtx(bgCtx, "delayed delete prefix failed", logger.Err(delErr), logger.String("prefix", p))
				}
			}
		case DeleteDaoTypeAll:
			prefixes := []string{
				singlePrefix + CacheKeyPrefixCondition,
				singlePrefix + CacheKeyPrefixColumns,
				singlePrefix + CacheKeyPrefixCount,
				singlePrefix + CacheKeyPrefixExists,
			}
			for _, p := range prefixes {
				if delErr := m.cache.DelByPrefix(ctx, p); delErr != nil {
					logger.WarnWithCtx(bgCtx, "delayed delete prefix failed", logger.Err(delErr), logger.String("prefix", p))
				}
			}
			if delErr := m.cache.DelByPrefix(ctx, singlePrefix); delErr != nil {
				logger.WarnWithCtx(bgCtx, "delayed delete singlePrefix failed", logger.Err(delErr), logger.String("prefix", singlePrefix))
			}
		default:
			if delErr := m.cache.DelByPrefix(ctx, ""); delErr != nil {
				logger.WarnWithCtx(bgCtx, "delayed delete empty prefix failed", logger.Err(delErr))
			}
		}
	}()
}
