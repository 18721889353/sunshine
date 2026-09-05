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

// cacheManager 统一管理缓存操作，包含单飞合并、分布式锁、防击穿、防穿透等特性。
type cacheManager[T any] struct {
	cache      Cache[T]
	sfg        *singleflight.Group
	config     CacheConfig
	tableName  string // 用于生成单条缓存前缀（可能需转换）
	basePrefix string // 自动生成，如 "data:userExample:"
}

// newCacheManager 创建缓存管理器实例
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

// underscoreToCamel 将下划线命名转为驼峰（首字母小写，后续单词首字母大写）
// 例如: sys_user_example -> sysUserExample
func underscoreToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// getSingleCachePrefix 获取单条缓存的前缀（含末尾冒号）
func (m *cacheManager[T]) getSingleCachePrefix() string {
	// 将表名转为驼峰以匹配业务适配器前缀
	camelName := underscoreToCamel(m.tableName)
	return "data:" + camelName + ":"
}

// -------------------- 单条缓存 --------------------

// get 从缓存中获取单条记录
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
					_ = m.cache.SetPlaceholder(ctx, id)
					return nil, database.ErrRecordNotFound
				}
				return nil, dbErr
			}
			expire := GetRandomExpireTime(m.config.DefaultExpireTime)
			_ = m.cache.Set(ctx, id, table, expire)
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

// handleFallback ...
func (m *cacheManager[T]) handleFallback(ctx context.Context, id uint64, err error, queryFunc func() (*T, error)) (*T, error) {
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}
	val, sfErr, _ := m.sfg.Do(fmt.Sprintf("%d", id), func() (interface{}, error) {
		lockKey := BuildLockKey(LockKeyPrefixRefresh, fmt.Sprintf("%d", id))
		if lock, lockErr := m.cache.GetLock(ctx, lockKey); lockErr == nil {
			if record, cacheErr := m.cache.Get(ctx, id); cacheErr == nil {
				_, _ = lock.UnlockContext(ctx)
				return record, nil
			}
			_, _ = lock.UnlockContext(ctx)
		}
		table, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				_ = m.cache.SetPlaceholder(ctx, id)
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}
		expire := GetRandomExpireTime(m.config.DefaultExpireTime)
		_ = m.cache.Set(ctx, id, table, expire)
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
		if record, hit, _ := m.getByCachedID(ctx, cachedID, queryFunc); hit {
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

func (m *cacheManager[T]) getByCachedID(ctx context.Context, cachedID uint64, queryFunc func() (*T, error)) (*T, bool, error) {
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
				_, _ = lock.UnlockContext(unlockCtx)
			}()
		}
		if lockErr == nil {
			if cachedID, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil && cachedID != 0 {
				if record, hit, _ := m.getByCachedID(ctx, cachedID, queryFunc); hit {
					return record, nil
				}
			}
		} else {
			time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
			if cachedID, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil && cachedID != 0 {
				if record, hit, _ := m.getByCachedID(ctx, cachedID, queryFunc); hit {
					return record, nil
				}
			}
		}
		record, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				_ = m.cache.SetPlaceholderByKey(ctx, cacheKey)
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
	_ = m.cache.SetIDByKey(ctx, cacheKey, id, expire)
	_ = m.cache.Set(ctx, id, record, expire)
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
		_ = m.cache.DelByKey(ctx, cacheKey)
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
				_, _ = lock.UnlockContext(unlockCtx)
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
				_ = m.cache.SetPlaceholderByKey(ctx, cacheKey)
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}
		if len(result) > m.config.MaxCacheableIDs {
			logger.WarnWithCtx(ctx, "result set too large to cache", logger.Any("count", len(result)), logger.Any("key", key))
			return result, nil
		}
		expire := GetRandomExpireTime(m.config.DefaultExpireTime)
		_ = m.cache.SetIDsByKey(ctx, cacheKey, result, expire)
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
				_, _ = lock.UnlockContext(unlockCtx)
			}()
		}
		if lockErr == nil {
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
		} else {
			time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
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
		records, dbErr := queryFunc(missed)
		if dbErr != nil {
			return nil, dbErr
		}
		if len(records) > 0 {
			expire := GetRandomExpireTime(m.config.DefaultExpireTime)
			_ = m.cache.MultiSet(ctx, records, expire)
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
			_ = m.cache.SetPlaceholder(ctx, id)
		}
	}
}

// -------------------- 分页查询缓存 --------------------

func (m *cacheManager[T]) getColumns(ctx context.Context, queryFunc func() ([]*T, int64, error), idsQueryFunc func([]uint64) ([]*T, error), cacheKey string) ([]*T, int64, error) {
	fullKey := CacheKeyPrefixColumns + cacheKey

	cachedTotal, err := m.cache.GetIDByKey(ctx, fullKey+":total")
	if err == nil {
		ids, idsErr := m.cache.GetIDsByKey(ctx, fullKey+":ids")
		if idsErr == nil && len(ids) > 0 {
			if len(ids) <= m.config.MaxCacheableRecords {
				recordsMap, getErr := m.getByIDs(ctx, ids, idsQueryFunc)
				if getErr == nil && len(recordsMap) > 0 {
					records := make([]*T, 0, len(ids))
					for _, id := range ids {
						if rec, ok := recordsMap[id]; ok {
							records = append(records, rec)
						}
					}
					if len(records) == len(ids) {
						return records, int64(cachedTotal), nil
					}
				}
			} else {
				logger.WarnWithCtx(ctx, "cached ids count exceeds max, deleting stale cache",
					logger.Any("count", len(ids)), logger.String("key", fullKey))
				_ = m.cache.DelByKey(ctx, fullKey+":total")
				_ = m.cache.DelByKey(ctx, fullKey+":ids")
			}
		}
	}

	val, sfErr, _ := m.sfg.Do(SFKeyPrefixColumns+cacheKey, func() (interface{}, error) {
		lockKey := BuildLockKey(LockKeyPrefixRefresh, fullKey)
		lock, lockErr := m.cache.GetLock(ctx, lockKey)
		if lock != nil {
			defer func() {
				unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				defer cancel()
				_, _ = lock.UnlockContext(unlockCtx)
			}()
		}
		if lockErr == nil {
			if total, cacheErr := m.cache.GetIDByKey(ctx, fullKey+":total"); cacheErr == nil {
				if ids, idsErr := m.cache.GetIDsByKey(ctx, fullKey+":ids"); idsErr == nil && len(ids) > 0 && len(ids) <= m.config.MaxCacheableRecords {
					recordsMap, getErr := m.getByIDs(ctx, ids, idsQueryFunc)
					if getErr == nil && len(recordsMap) == len(ids) {
						records := make([]*T, 0, len(ids))
						for _, id := range ids {
							if rec, ok := recordsMap[id]; ok {
								records = append(records, rec)
							}
						}
						return struct {
							records []*T
							total   int64
						}{records: records, total: int64(total)}, nil
					}
				}
			}
		} else {
			time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
			if total, cacheErr := m.cache.GetIDByKey(ctx, fullKey+":total"); cacheErr == nil {
				if ids, idsErr := m.cache.GetIDsByKey(ctx, fullKey+":ids"); idsErr == nil && len(ids) > 0 && len(ids) <= m.config.MaxCacheableRecords {
					recordsMap, getErr := m.getByIDs(ctx, ids, idsQueryFunc)
					if getErr == nil && len(recordsMap) == len(ids) {
						records := make([]*T, 0, len(ids))
						for _, id := range ids {
							if rec, ok := recordsMap[id]; ok {
								records = append(records, rec)
							}
						}
						return struct {
							records []*T
							total   int64
						}{records: records, total: int64(total)}, nil
					}
				}
			}
		}

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
		if len(records) <= m.config.MaxCacheableRecords && total > 0 {
			expire := GetRandomExpireTime(m.config.DefaultExpireTime)
			_ = m.cache.SetIDByKey(ctx, fullKey+":total", uint64(total), expire)
			ids := make([]uint64, 0, len(records))
			for _, rec := range records {
				id := GetObjectID(*rec)
				if id != 0 {
					ids = append(ids, id)
				}
			}
			if len(ids) > 0 {
				_ = m.cache.SetIDsByKey(ctx, fullKey+":ids", ids, expire)
				_ = m.cache.MultiSet(ctx, records, expire)
			}
		}
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
				_, _ = lock.UnlockContext(unlockCtx)
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
		_ = m.cache.SetIDByKey(ctx, cacheKey, uint64(count), expire)
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
				_, _ = lock.UnlockContext(unlockCtx)
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
		_ = m.cache.SetIDByKey(ctx, cacheKey, val, expire)
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
	singlePrefix := m.getSingleCachePrefix() // 例如 "data:userExample:"

	switch deleteType {
	case DeleteDaoTypeSingle:
		_ = m.cache.Del(ctx, id)

	case DeleteDaoTypeCondition:
		// 删除条件类缓存（condition、columns、count、exists）
		prefixes := []string{
			singlePrefix + CacheKeyPrefixCondition,
			singlePrefix + CacheKeyPrefixColumns,
			singlePrefix + CacheKeyPrefixCount,
			singlePrefix + CacheKeyPrefixExists,
		}
		for _, p := range prefixes {
			_ = m.cache.DelByPrefix(ctx, p)
		}

	case DeleteDaoTypeAll:
		// 删除所有缓存（条件类 + 单条）
		// 先删除条件类子前缀
		prefixes := []string{
			singlePrefix + CacheKeyPrefixCondition,
			singlePrefix + CacheKeyPrefixColumns,
			singlePrefix + CacheKeyPrefixCount,
			singlePrefix + CacheKeyPrefixExists,
		}
		for _, p := range prefixes {
			_ = m.cache.DelByPrefix(ctx, p)
		}
		// 再删除整个数据前缀（包括单条缓存和可能遗漏的其他键）
		_ = m.cache.DelByPrefix(ctx, singlePrefix)

	default:
		_ = m.cache.DelByPrefix(ctx, "")
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

		// 删除单条缓存
		if id > 0 {
			_ = m.cache.Del(ctx, id)
		}
		if len(ids) > 0 {
			for _, batchID := range ids {
				_ = m.cache.Del(ctx, batchID)
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
				_ = m.cache.DelByPrefix(ctx, p)
			}

		case DeleteDaoTypeAll:
			prefixes := []string{
				singlePrefix + CacheKeyPrefixCondition,
				singlePrefix + CacheKeyPrefixColumns,
				singlePrefix + CacheKeyPrefixCount,
				singlePrefix + CacheKeyPrefixExists,
			}
			for _, p := range prefixes {
				_ = m.cache.DelByPrefix(ctx, p)
			}
			_ = m.cache.DelByPrefix(ctx, singlePrefix)

		default:
			_ = m.cache.DelByPrefix(ctx, "")
		}
	}()
}
