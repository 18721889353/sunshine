// Package dao 提供泛型数据访问层基础组件
package dao

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/18721889353/sunshine/pkg/logger"
)

// ============================================================================
// 第一部分：结构体定义和构造函数
// ============================================================================

// cacheManager 是整个缓存系统的"大脑"。
// 它负责协调 Redis 缓存、singleflight（单机请求合并）、分布式锁（跨服务器互斥）。
// 泛型参数 T 是业务模型类型（如 User、Order）。
type cacheManager[T any] struct {
	// cache：底层的缓存适配器（通常是 Redis 实现），负责实际的读写操作
	cache Cache[T]

	// sfg：singleflight.Group，用于在单台服务器内合并并发请求
	// 作用：当 1000 个请求同时查同一个 key，只让 1 个请求去查数据库，其他 999 个等待结果
	sfg *singleflight.Group

	// config：缓存配置，包含过期时间、批次大小、锁等待时间等
	config CacheConfig

	// basePrefix：缓存键的基础前缀，格式为 "data:表名:"
	// 例如：表名 "users" -> basePrefix = "data:Users:"
	basePrefix string
}

// newCacheManager 创建一个新的缓存管理器实例
// 参数：
//   - c：缓存适配器（实现 Cache[T] 接口）
//   - config：缓存配置（过期时间、批次大小等）
//   - tableName：表名（如 "users"）
//
// 返回：*cacheManager[T] 实例
func newCacheManager[T any](c Cache[T], config CacheConfig, tableName string) *cacheManager[T] {
	// 把表名从蛇形（snake_case）转成驼峰（CamelCase）
	// 例如："user_example" -> "UserExample"
	camelName := UnderscoreToCamel(tableName)

	// 创建并返回 cacheManager 实例
	return &cacheManager[T]{
		cache:      c,                         // 缓存适配器
		sfg:        new(singleflight.Group),   // 新建 singleflight 组
		config:     config,                    // 配置
		basePrefix: "data:" + camelName + ":", // 基础前缀：data:UserExample:
	}
}

// getSingleCachePrefix 获取单条记录的缓存前缀
// 格式："data:表名:"
// 例如：表名 "users" -> "data:Users:"
// 所有单个 ID 缓存（如 key="123"）都存储在这个前缀下
func (m *cacheManager[T]) getSingleCachePrefix() string {
	return m.basePrefix
}

// ============================================================================
// 第二部分：单条缓存 get（最核心的流程）
// ============================================================================

// get 是单条缓存查询的入口方法。
// 它是整个框架最核心的函数，把「缓存 -> singleflight -> 分布式锁 -> 数据库」串联起来。
// 参数：
//   - ctx：上下文
//   - id：要查询的记录 ID（uint64）
//   - queryFunc：当缓存未命中时，调用这个函数去数据库查询
//   - opts：查询选项（如 ForceMaster、Unscoped）
//
// 返回：记录指针 *T，或错误
func (m *cacheManager[T]) get(ctx context.Context, id uint64, queryFunc func() (*T, error), opts ...QueryOption) (*T, error) {
	// 应用查询选项
	cfg := ApplyOptions(opts...)

	// 【特殊处理】如果设置了 Unscoped，跳过缓存直接查数据库
	// 因为 Unscoped 查询需要获取包含软删除的记录，缓存无法区分
	if cfg.Unscoped {
		return queryFunc()
	}

	// 【步骤1】先尝试从缓存中读取
	record, err := m.cache.Get(ctx, id)
	if err == nil {
		// 缓存命中，直接返回
		return record, nil
	}

	// 【步骤2】如果缓存返回的是"未找到"（ErrCacheNotFound），说明缓存里没有这个 key
	if errors.Is(err, database.ErrCacheNotFound) {
		return m.executeSingleflight(ctx, id, queryFunc)
	}

	// 【步骤3】如果缓存返回的是其他错误（非 ErrCacheNotFound），进入 fallback 流程
	// 可能原因：Redis 连接超时、网络中断等
	logger.WarnWithCtx(ctx, "cache.Get error, falling back to database", logger.Err(err), logger.Any("id", id))
	return m.handleFallback(ctx, id, err, queryFunc)
}

// waitForCachePoll 轮询等待缓存被其他节点/实例写入，避免未获锁时只等一次就穿透到 DB。
// interval: 每次轮询间隔（默认 50ms）
// maxWait:  最大等待时间（默认 5s）
// queryCache: 每轮调用的缓存读取函数，返回 (*T, error)，缓存命中时 err == nil
func (m *cacheManager[T]) waitForCachePoll(ctx context.Context, interval, maxWait time.Duration, queryCache func() (*T, error)) (*T, bool) {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		if record, cacheErr := queryCache(); cacheErr == nil {
			return record, true
		}
	}
	return nil, false
}

// executeSingleflight 执行 singleflight 流程，把针对同一个 ID 的并发请求合并成一个
func (m *cacheManager[T]) executeSingleflight(ctx context.Context, id uint64, queryFunc func() (*T, error)) (*T, error) {
	val, sfErr, _ := m.sfg.Do(fmt.Sprintf("%d", id), func() (interface{}, error) {
		// 构建分布式锁的 Key
		lockKey := BuildLockKey(LockKeyPrefixRefresh, fmt.Sprintf("%d", id))

		// 尝试获取分布式锁（非阻塞，只尝试一次）
		lock, lockErr := m.cache.GetLock(ctx, lockKey)

		// 如果拿到了锁（lock != nil），确保函数退出时释放锁
		if lock != nil {
			defer func() {
				// 使用独立的上下文来释放锁，避免因原上下文超时而无法释放
				unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				defer cancel()
				if _, unlockErr := lock.UnlockContext(unlockCtx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "cache: unlock refresh lock failed", logger.Err(unlockErr))
				}
			}()
		}

		// 如果成功拿到锁，说明当前节点是"主执行者"
		if lockErr == nil {
			// 再次检查缓存（Double-Check）：可能在获取锁的过程中，别的节点已经把数据写进缓存了
			if record, cacheErr := m.cache.Get(ctx, id); cacheErr == nil {
				return record, nil
			}
		} else {
			// 未拿到锁，轮询等待其他实例完成 DB 查询并回填缓存
			interval := time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond
			if record, ok := m.waitForCachePoll(ctx, interval, 5*time.Second, func() (*T, error) {
				return m.cache.Get(ctx, id)
			}); ok {
				return record, nil
			}
			logger.WarnWithCtx(ctx, "cache poll timeout, falling back to database",
				logger.Any("id", id), logger.String("lockKey", lockKey))
		}

		// 缓存依然没有数据，执行 queryFunc 去数据库查询
		table, dbErr := queryFunc()

		if dbErr != nil {
			// 如果数据库返回"记录不存在"
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				// 设置占位符，防止缓存穿透
				if setErr := m.cache.SetPlaceholder(ctx, id); setErr != nil {
					logger.WarnWithCtx(ctx, "set placeholder failed", logger.Err(setErr))
				}
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}

		// 数据库查询成功，把结果写入缓存（生成随机过期时间，防雪崩）
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

// ============================================================================
// 第四部分：handleFallback（缓存异常时的降级处理）
// ============================================================================

// handleFallback 是当缓存出现非"未找到"错误时的降级处理
// 比如 Redis 连接超时、网络闪断等，直接查数据库保证可用性
func (m *cacheManager[T]) handleFallback(ctx context.Context, id uint64, err error, queryFunc func() (*T, error)) (*T, error) {
	// 【步骤1】如果错误是占位符错误，直接返回"记录不存在"
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}

	// 【步骤2】进入 singleflight 合并请求
	val, sfErr, _ := m.sfg.Do(fmt.Sprintf("%d", id), func() (interface{}, error) {
		lockKey := BuildLockKey(LockKeyPrefixRefresh, fmt.Sprintf("%d", id))

		// 【步骤3】尝试获取分布式锁
		if lock, lockErr := m.cache.GetLock(ctx, lockKey); lockErr == nil {
			// 拿到锁后，Double-Check 缓存
			if record, cacheErr := m.cache.Get(ctx, id); cacheErr == nil {
				// 释放锁并返回
				if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
					logger.WarnWithCtx(ctx, "unlock failed in handleFallback", logger.Err(unlockErr))
				}
				return record, nil
			}
			// 释放锁
			if _, unlockErr := lock.UnlockContext(ctx); unlockErr != nil {
				logger.WarnWithCtx(ctx, "unlock failed in handleFallback", logger.Err(unlockErr))
			}
		}

		// 【步骤4】直接从数据库查询
		table, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				// 设置占位符
				if setErr := m.cache.SetPlaceholder(ctx, id); setErr != nil {
					logger.WarnWithCtx(ctx, "set placeholder failed", logger.Err(setErr))
				}
				return nil, database.ErrRecordNotFound
			}
			return nil, dbErr
		}

		// 【步骤5】回填缓存（带随机过期时间）
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

// ============================================================================
// 第四部分：条件单条缓存 getCondition
// ============================================================================

// getCondition 根据条件（如 name = '张三'）查询单条记录。
// 缓存策略：缓存 key -> ID，ID 再通过 get() 方法获取完整对象。
// 这样做的好处是：如果对象更新了，只需更新 ID 对应的对象缓存，条件缓存的 key->ID 依然有效。
func (m *cacheManager[T]) getCondition(ctx context.Context, key string, queryFunc func() (*T, error)) (*T, error) {
	// 【步骤1】构建完整的缓存 Key，格式："condition:MD5(条件)"
	cacheKey := CacheKeyPrefixCondition + key

	// 【步骤2】从缓存获取缓存的 ID
	cachedID, err := m.cache.GetIDByKey(ctx, cacheKey)

	// 【步骤3】如果成功拿到 ID，且 ID != 0
	if err == nil && cachedID != 0 {
		// 调用 getByCachedID，本质上是调 m.cache.Get(cachedID) 取对象
		if record, hit, getErr := m.getByCachedID(ctx, cachedID, nil); hit && getErr == nil {
			return record, nil
		}
	}

	// 【步骤4】如果是"缓存未找到"或 ID 为 0，进入 singleflight 流程
	if errors.Is(err, database.ErrCacheNotFound) || cachedID == 0 {
		return m.executeConditionSingleflight(ctx, key, cacheKey, queryFunc)
	}

	// 【步骤5】其他错误，记录日志并进入 singleflight
	logger.WarnWithCtx(ctx, "cache.GetIDByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey))
	if m.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}
	return m.executeConditionSingleflight(ctx, key, cacheKey, queryFunc)
}

// getByCachedID 根据缓存的 ID 获取完整对象
// 参数 queryFunc 保留用于接口一致性，但实际未使用
func (m *cacheManager[T]) getByCachedID(ctx context.Context, cachedID uint64, _ func() (*T, error)) (*T, bool, error) {
	record, err := m.cache.Get(ctx, cachedID)
	if err == nil {
		return record, true, nil
	}
	return nil, false, nil
}

// executeConditionSingleflight 执行条件单条查询的 singleflight 流程
func (m *cacheManager[T]) executeConditionSingleflight(ctx context.Context, key, cacheKey string, queryFunc func() (*T, error)) (*T, error) {
	val, sfErr, _ := m.sfg.Do(SFKeyPrefixOneCondition+key, func() (interface{}, error) {
		// 构建分布式锁 Key： "lock:refresh:condition:xxx"
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

		// 双检：拿到锁后再检查一次缓存
		if lockErr == nil {
			if cachedID, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil && cachedID != 0 {
				if record, hit, getErr := m.getByCachedID(ctx, cachedID, nil); hit && getErr == nil {
					return record, nil
				}
			}
		} else {
			// 未拿到锁，轮询等待其他实例写入缓存
			interval := time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond
			if record, ok := m.waitForCachePoll(ctx, interval, 5*time.Second, func() (*T, error) {
				if cachedID, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil && cachedID != 0 {
					if r, hit, getErr := m.getByCachedID(ctx, cachedID, nil); hit && getErr == nil {
						return r, nil
					}
				}
				return nil, database.ErrCacheNotFound
			}); ok {
				return record, nil
			}
		}

		// 查数据库
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

		// 缓存结果：key -> ID，同时缓存 ID -> 对象
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

// cacheConditionResult 缓存条件查询的结果
// 同时设置两个缓存：
//  1. cacheKey -> ID（条件映射）
//  2. ID -> 对象（单条缓存）
func (m *cacheManager[T]) cacheConditionResult(ctx context.Context, cacheKey string, record *T) {
	if record == nil {
		return
	}
	// 通过反射获取 ID
	id := GetObjectID(*record)
	if id == 0 {
		return
	}

	expire := GetRandomExpireTime(m.config.DefaultExpireTime)

	// 缓存 key -> ID
	if setErr := m.cache.SetIDByKey(ctx, cacheKey, id, expire); setErr != nil {
		logger.WarnWithCtx(ctx, "set id by key failed", logger.Err(setErr))
	}

	// 缓存 ID -> 对象
	if setErr := m.cache.Set(ctx, id, record, expire); setErr != nil {
		logger.WarnWithCtx(ctx, "cache set failed", logger.Err(setErr))
	}
}

// ============================================================================
// 第五部分：条件 ID 列表缓存 getByCondition
// ============================================================================

// getByCondition 根据条件获取满足条件的 ID 列表。
// 缓存策略：key -> []uint64（ID 列表）
// 例如：状态为 1 的所有用户 ID 列表
func (m *cacheManager[T]) getByCondition(ctx context.Context, key string, queryFunc func() ([]uint64, error)) ([]uint64, error) {
	cacheKey := CacheKeyPrefixCondition + key

	// 【步骤1】尝试从缓存读取 ID 列表
	ids, err := m.cache.GetIDsByKey(ctx, cacheKey)

	// 【步骤2】如果缓存命中
	if err == nil {
		// 检查列表长度是否超过最大缓存限制（默认 10000）
		if len(ids) <= m.config.MaxCacheableIDs {
			return ids, nil
		}
		// 列表太大，删除过期缓存（避免 Redis 内存浪费）
		logger.WarnWithCtx(ctx, "cached id list too large, deleting stale cache",
			logger.Any("count", len(ids)), logger.Any("key", key))
		if delErr := m.cache.DelByKey(ctx, cacheKey); delErr != nil {
			logger.WarnWithCtx(ctx, "delete stale cache failed", logger.Err(delErr))
		}
	} else if !errors.Is(err, database.ErrCacheNotFound) {
		// 【步骤3】如果不是"缓存未找到"错误，记录日志并检查是否是占位符
		logger.WarnWithCtx(ctx, "cache.GetIDsByKey error, falling back to database", logger.Err(err), logger.Any("key", cacheKey))
		if m.cache.IsPlaceholderErr(err) {
			return nil, database.ErrRecordNotFound
		}
	}

	// 【步骤4】进入 singleflight 流程
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

		// 双检
		if lockErr == nil {
			if cachedIDs, cacheErr := m.cache.GetIDsByKey(ctx, cacheKey); cacheErr == nil && len(cachedIDs) <= m.config.MaxCacheableIDs {
				return cachedIDs, nil
			}
		} else {
			// 未拿到锁，轮询等待其他实例写入缓存
			interval := time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(interval)
				if cachedIDs, cacheErr := m.cache.GetIDsByKey(ctx, cacheKey); cacheErr == nil && len(cachedIDs) <= m.config.MaxCacheableIDs {
					return cachedIDs, nil
				}
			}
		}

		// 查数据库
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

		// 如果结果集太大，不缓存，直接返回
		if len(result) > m.config.MaxCacheableIDs {
			logger.WarnWithCtx(ctx, "result set too large to cache", logger.Any("count", len(result)), logger.Any("key", key))
			return result, nil
		}

		// 缓存结果
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

// ============================================================================
// 第六部分：批量缓存 getByIDs
// ============================================================================

// getByIDs 根据一批 ID 批量获取记录。
// 这是所有查询方法中最复杂的之一，涉及：
//  1. 分批处理（防止单次查询过大）
//  2. MultiGet 批量读缓存
//  3. 找出缺失的 ID 去查数据库
//  4. singleflight 合并相同批次的请求
func (m *cacheManager[T]) getByIDs(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*T, error)) (map[uint64]*T, error) {
	// 【步骤1】如果 ID 列表超过 MaxBatchSize（默认 1000），拆分成多个批次
	if len(ids) > m.config.MaxBatchSize {
		result := make(map[uint64]*T)
		for i := 0; i < len(ids); i += m.config.MaxBatchSize {
			end := i + m.config.MaxBatchSize
			if end > len(ids) {
				end = len(ids)
			}
			batch := ids[i:end]
			// 递归调用自身，但每个批次都在限额内
			batchResult, err := m.getByIDsBatch(ctx, batch, queryFunc)
			if err != nil {
				return nil, err
			}
			// 合并结果
			for id, rec := range batchResult {
				result[id] = rec
			}
		}
		return result, nil
	}
	// 【步骤2】列表在限额内，直接调用批次方法
	return m.getByIDsBatch(ctx, ids, queryFunc)
}

// fetchBatchFromDB 从数据库获取批量数据并回填缓存
// 这是 singleflight 实际调用的函数
func (m *cacheManager[T]) fetchBatchFromDB(ctx context.Context, missed []uint64, queryFunc func([]uint64) ([]*T, error), hashKey string) (interface{}, error) {
	// 构建分布式锁 Key："lock:refresh:batch:MD5"
	lockKey := BuildLockKey(LockKeyPrefixRefreshBatch, hashKey)
	lock, lockErr := m.cache.GetLock(ctx, lockKey)

	if lock != nil {
		defer func() {
			unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			if _, unlockErr := lock.UnlockContext(unlockCtx); unlockErr != nil {
				logger.WarnWithCtx(ctx, "unlock failed in fetchBatchFromDB", logger.Err(unlockErr))
			}
		}()
	}

	// 【步骤1】尝试从缓存获取全部
	if m.tryGetAllFromCache(ctx, lockErr) {
		itemMapTmp, tmpErr := m.cache.MultiGet(ctx, missed)
		if tmpErr == nil {
			// 检查是否所有 ID 都命中了缓存
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

	// 【步骤2】从数据库查询
	records, dbErr := queryFunc(missed)
	if dbErr != nil {
		return nil, dbErr
	}

	// 【步骤3】批量写入缓存
	if len(records) > 0 {
		expire := GetRandomExpireTime(m.config.DefaultExpireTime)
		if setErr := m.cache.MultiSet(ctx, records, expire); setErr != nil {
			logger.WarnWithCtx(ctx, "multi set failed", logger.Err(setErr))
		}
	}

	// 【步骤4】为缺失的 ID 设置占位符
	m.setPlaceholdersForMissing(ctx, records, missed)

	// 【步骤5】构建 ID -> 对象的映射并返回
	resultMap := make(map[uint64]*T)
	for _, rec := range records {
		id := GetObjectID(*rec)
		if id != 0 {
			resultMap[id] = rec
		}
	}
	return resultMap, nil
}

// getByIDsBatch 单批次批量查询
func (m *cacheManager[T]) getByIDsBatch(ctx context.Context, ids []uint64, queryFunc func([]uint64) ([]*T, error)) (map[uint64]*T, error) {
	// 【步骤1】从缓存批量读取
	itemMap, err := m.cache.MultiGet(ctx, ids)
	if err != nil {
		logger.WarnWithCtx(ctx, "cache.MultiGet error, falling back to database", logger.Err(err), logger.Any("ids", ids))
		itemMap = make(map[uint64]*T)
	}

	// 【步骤2】找出缓存中没有命中的 ID
	missed := m.findMissedIDs(ids, itemMap)
	if len(missed) == 0 {
		// 全部命中，直接返回
		return itemMap, nil
	}

	// 【步骤3】对缺失的 ID 排序，生成固定的哈希值
	// 目的：相同的 ID 列表（无论顺序如何）生成相同的 MD5，方便 singleflight 去重
	sorted := make([]uint64, len(missed))
	copy(sorted, missed)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	hashKey := gocrypto.Md5([]byte(fmt.Sprintf("%v", sorted)))

	// 【步骤4】进入 singleflight，合并相同哈希值的请求
	val, sfErr, _ := m.sfg.Do(SFKeyPrefixBatchIDs+hashKey, func() (interface{}, error) {
		return m.fetchBatchFromDB(ctx, missed, queryFunc, hashKey)
	})

	if sfErr != nil {
		return nil, sfErr
	}
	recordsMap, ok := val.(map[uint64]*T)
	if !ok {
		return nil, errors.New("type assertion failed for batch records map")
	}

	// 【步骤5】合并结果
	for id, rec := range recordsMap {
		itemMap[id] = rec
	}
	return itemMap, nil
}

// tryGetAllFromCache 尝试从缓存获取所有缺失 ID
// 注意：missed 参数未使用，保留是为了接口清晰
func (m *cacheManager[T]) tryGetAllFromCache(_ context.Context, lockErr error) bool {
	if lockErr == nil {
		return true
	}
	time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
	return true
}

// findMissedIDs 找出 itemMap 中没有的 ID
func (m *cacheManager[T]) findMissedIDs(ids []uint64, itemMap map[uint64]*T) []uint64 {
	var missed []uint64
	for _, id := range ids {
		if _, ok := itemMap[id]; !ok {
			missed = append(missed, id)
		}
	}
	return missed
}

// setPlaceholdersForMissing 为数据库中不存在的 ID 设置占位符
func (m *cacheManager[T]) setPlaceholdersForMissing(ctx context.Context, records []*T, missedIDs []uint64) {
	// 构建存在的 ID 集合
	existing := make(map[uint64]bool)
	for _, rec := range records {
		id := GetObjectID(*rec)
		if id != 0 {
			existing[id] = true
		}
	}

	// 遍历所有缺失的 ID，如果不在 existing 中，设置占位符
	for _, id := range missedIDs {
		if !existing[id] {
			if setErr := m.cache.SetPlaceholder(ctx, id); setErr != nil {
				logger.WarnWithCtx(ctx, "set placeholder for missing id failed", logger.Err(setErr))
			}
		}
	}
}

// ============================================================================
// 第七部分：分页查询缓存 getColumns
// ============================================================================

// getColumns 分页查询缓存
// 缓存策略：存储两个 key：
//   - fullKey + ":total" -> 总记录数
//   - fullKey + ":ids"   -> 当前页的 ID 列表
//
// 读取时：先读 total 和 ids，然后通过 getByIDs 获取完整记录
func (m *cacheManager[T]) getColumns(ctx context.Context, queryFunc func() ([]*T, int64, error), idsQueryFunc func([]uint64) ([]*T, error), cacheKey string) ([]*T, int64, error) {
	fullKey := CacheKeyPrefixColumns + cacheKey

	// 【步骤1】尝试从缓存获取（无锁）
	if total, ids, ok := m.tryGetColumnsFromCache(ctx, fullKey, idsQueryFunc); ok {
		return ids, total, nil
	}

	// 【步骤2】进入 singleflight
	val, sfErr, _ := m.sfg.Do(SFKeyPrefixColumns+cacheKey, func() (interface{}, error) {
		// 【步骤3】带锁再次尝试从缓存获取
		if total, ids, ok := m.tryGetColumnsFromCacheWithLock(ctx, fullKey, idsQueryFunc); ok {
			return struct {
				records []*T
				total   int64
			}{records: ids, total: total}, nil
		}

		// 【步骤4】从数据库查询
		records, total, dbErr := queryFunc()
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				// 没有数据，返回空列表
				return struct {
					records []*T
					total   int64
				}{records: []*T{}, total: 0}, nil
			}
			return nil, dbErr
		}

		// 【步骤5】缓存结果
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
func (m *cacheManager[T]) tryGetColumnsFromCache(ctx context.Context, fullKey string, idsQueryFunc func([]uint64) ([]*T, error)) (int64, []*T, bool) {
	// 读取 total
	cachedTotal, err := m.cache.GetIDByKey(ctx, fullKey+":total")
	if err != nil {
		return 0, nil, false
	}

	// 读取 ID 列表
	ids, idsErr := m.cache.GetIDsByKey(ctx, fullKey+":ids")
	if idsErr != nil || len(ids) == 0 || len(ids) > m.config.MaxCacheableRecords {
		// 如果缓存超过限制，删除过期的缓存
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

	// 通过 getByIDs 获取完整记录
	recordsMap, getErr := m.getByIDs(ctx, ids, idsQueryFunc)
	if getErr != nil {
		return 0, nil, false
	}

	// 按 ID 顺序构建记录列表
	records := make([]*T, 0, len(ids))
	for _, id := range ids {
		if rec, ok := recordsMap[id]; ok {
			records = append(records, rec)
		}
	}

	// 如果所有 ID 都有记录，返回
	if len(records) == len(ids) {
		return int64(cachedTotal), records, true
	}
	return 0, nil, false
}

// tryGetColumnsFromCacheWithLock 带锁尝试从缓存获取
func (m *cacheManager[T]) tryGetColumnsFromCacheWithLock(ctx context.Context, fullKey string, idsQueryFunc func([]uint64) ([]*T, error)) (int64, []*T, bool) {
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
		return m.tryGetColumnsFromCache(ctx, fullKey, idsQueryFunc)
	}
	time.Sleep(time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond)
	return m.tryGetColumnsFromCache(ctx, fullKey, idsQueryFunc)
}

// cacheColumnsResult 缓存分页查询结果
func (m *cacheManager[T]) cacheColumnsResult(ctx context.Context, fullKey string, records []*T, total int64) {
	// 只有记录数在限制内且 total > 0 时才缓存
	if len(records) <= m.config.MaxCacheableRecords && total > 0 {
		expire := GetRandomExpireTime(m.config.DefaultExpireTime)

		// 缓存 total
		if setErr := m.cache.SetIDByKey(ctx, fullKey+":total", uint64(total), expire); setErr != nil {
			logger.WarnWithCtx(ctx, "set total key failed", logger.Err(setErr))
		}

		// 提取 ID 列表
		ids := make([]uint64, 0, len(records))
		for _, rec := range records {
			id := GetObjectID(*rec)
			if id != 0 {
				ids = append(ids, id)
			}
		}

		if len(ids) > 0 {
			// 缓存 ID 列表
			if setErr := m.cache.SetIDsByKey(ctx, fullKey+":ids", ids, expire); setErr != nil {
				logger.WarnWithCtx(ctx, "set ids key failed", logger.Err(setErr))
			}
			// 批量缓存完整记录
			if setErr := m.cache.MultiSet(ctx, records, expire); setErr != nil {
				logger.WarnWithCtx(ctx, "multi set failed", logger.Err(setErr))
			}
		}
	}
}

// ============================================================================
// 第八部分：计数缓存 getCount
// ============================================================================

// getCount 缓存计数查询结果
// 缓存策略：key -> uint64(计数)
func (m *cacheManager[T]) getCount(ctx context.Context, key string, queryFunc func() (int64, error)) (int64, error) {
	cacheKey := CacheKeyPrefixCount + key

	// 尝试从缓存读取
	cached, err := m.cache.GetIDByKey(ctx, cacheKey)
	if err == nil {
		return int64(cached), nil
	}

	// 进入 singleflight
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

		// 双检
		if lockErr == nil {
			if cached, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil {
				return int64(cached), nil
			}
		} else {
			// 未拿到锁，轮询等待其他实例写入缓存
			interval := time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(interval)
				if cached, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil {
					return int64(cached), nil
				}
			}
		}

		// 查数据库
		count, dbErr := queryFunc()
		if dbErr != nil {
			return 0, dbErr
		}

		// 缓存结果
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

// ============================================================================
// 第九部分：存在缓存 getExists
// ============================================================================

// getExists 缓存存在性查询结果
// 缓存策略：key -> 0（不存在）或 1（存在）
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
			// 未拿到锁，轮询等待其他实例写入缓存
			interval := time.Duration(m.config.LockRefreshSleepMs) * time.Millisecond
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(interval)
				if cached, cacheErr := m.cache.GetIDByKey(ctx, cacheKey); cacheErr == nil {
					return cached > 0, nil
				}
			}
		}

		exists, dbErr := queryFunc()
		if dbErr != nil {
			return false, dbErr
		}

		// 0 或 1
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

// ============================================================================
// 第十部分：缓存删除（核心）
// ============================================================================

// deleteCache 立即删除缓存
// 参数：
//   - id：单个 ID（如果是 DeleteDaoTypeSingle 时使用）
//   - ids：ID 列表（DeleteDaoTypeSingle 时支持批量删除）
//   - deleteType：删除类型（Single / Condition / All）
func (m *cacheManager[T]) deleteCache(ctx context.Context, id uint64, ids []uint64, deleteType string) {
	if m.cache == nil {
		return
	}
	singlePrefix := m.getSingleCachePrefix() // "data:Users:"

	switch deleteType {
	case DeleteDaoTypeSingle:
		// 合并单个 ID 和 ID 列表，一次性批量删除
		allIDs := make([]uint64, 0, len(ids)+1)
		if id > 0 {
			allIDs = append(allIDs, id)
		}
		allIDs = append(allIDs, ids...)
		if len(allIDs) > 0 {
			if delErr := m.cache.DelByIDs(ctx, allIDs); delErr != nil {
				logger.WarnWithCtx(ctx, "cache DelByIDs failed", logger.Err(delErr), logger.Any("ids", allIDs))
			}
		}

	case DeleteDaoTypeCondition:
		// 删除所有条件相关的缓存（condition、columns、count、exists）
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
		// 删除所有缓存（最彻底）
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
		// 额外删除所有单条缓存
		if delErr := m.cache.DelByPrefix(ctx, singlePrefix); delErr != nil {
			logger.WarnWithCtx(ctx, "cache DelByPrefix failed for singlePrefix", logger.Err(delErr), logger.String("prefix", singlePrefix))
		}

	default:
		// 未知类型，删除所有（有风险，仅作兜底）
		if delErr := m.cache.DelByPrefix(ctx, ""); delErr != nil {
			logger.WarnWithCtx(ctx, "cache DelByPrefix failed with empty prefix", logger.Err(delErr))
		}
	}
}

// delayedDoubleDelete 延迟双删
// 在数据变更后，先立即删除缓存（deleteCache），过一段时间（DelayedDeleteInterval）再删一次
// 目的：防止在第一次删除后到第二次删除之间，有其他读请求把旧数据写回缓存
func (m *cacheManager[T]) delayedDoubleDelete(ctx context.Context, id uint64, ids []uint64, deleteType string) {
	if m.cache == nil {
		return
	}

	// 创建独立的后台上下文，不受原始 ctx 取消的影响
	bgCtx := context.WithoutCancel(ctx)

	go func() {
		// 捕获 panic，防止影响主流程
		defer func() {
			if r := recover(); r != nil {
				logger.WarnWithCtx(bgCtx, "delayedDoubleDelete panic recovered", logger.Any("recover", r))
			}
		}()

		// 等待设定的间隔（默认 100ms）
		time.Sleep(m.config.DelayedDeleteInterval)

		singlePrefix := m.getSingleCachePrefix()

		// 删除单个 ID
		if id > 0 {
			if delErr := m.cache.Del(bgCtx, id); delErr != nil {
				logger.WarnWithCtx(bgCtx, "delayed delete single id failed", logger.Err(delErr), logger.Any("id", id))
			}
		}

		// 删除批量 ID（一次 Redis DEL 调用）
		if len(ids) > 0 {
			if delErr := m.cache.DelByIDs(bgCtx, ids); delErr != nil {
				logger.WarnWithCtx(bgCtx, "delayed delete batch ids failed", logger.Err(delErr), logger.Any("ids", ids))
			}
		}

		// 按类型删除前缀
		switch deleteType {
		case DeleteDaoTypeCondition:
			prefixes := []string{
				singlePrefix + CacheKeyPrefixCondition,
				singlePrefix + CacheKeyPrefixColumns,
				singlePrefix + CacheKeyPrefixCount,
				singlePrefix + CacheKeyPrefixExists,
			}
			for _, p := range prefixes {
				if delErr := m.cache.DelByPrefix(bgCtx, p); delErr != nil {
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
				if delErr := m.cache.DelByPrefix(bgCtx, p); delErr != nil {
					logger.WarnWithCtx(bgCtx, "delayed delete prefix failed", logger.Err(delErr), logger.String("prefix", p))
				}
			}
			if delErr := m.cache.DelByPrefix(bgCtx, singlePrefix); delErr != nil {
				logger.WarnWithCtx(bgCtx, "delayed delete singlePrefix failed", logger.Err(delErr), logger.String("prefix", singlePrefix))
			}
		default:
			if delErr := m.cache.DelByPrefix(bgCtx, ""); delErr != nil {
				logger.WarnWithCtx(bgCtx, "delayed delete empty prefix failed", logger.Err(delErr))
			}
		}
	}()
}
