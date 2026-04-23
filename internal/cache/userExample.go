package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/go-redsync/redsync/v4"

	"github.com/18721889353/sunshine/pkg/cache"
	"github.com/18721889353/sunshine/pkg/encoding"
	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/model"
)

const (
	// cache prefix key, must end with a colon
	UserExampleCachePrefixKeyLock = "lock:userExample:"
	UserExampleCachePrefixKey     = "data:userExample:"
	// UserExampleExpireTime expire time
	UserExampleExpireTime = 30 * time.Minute
)

var _ UserExampleCache = (*userExampleCache)(nil)

// UserExampleCache cache interface
type UserExampleCache interface {
	GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	// 封装了锁的获取、看门狗自动续期、业务执行及释放逻辑
	WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error
	WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error

	Set(ctx context.Context, id uint64, data *model.UserExample, duration time.Duration) error
	SetIdByKey(ctx context.Context, key string, id uint64, duration time.Duration) error
	SetIdsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error

	Get(ctx context.Context, id uint64) (*model.UserExample, error)
	GetIdByKey(ctx context.Context, key string) (id uint64, err error)
	GetIdsByKey(ctx context.Context, key string) (ids []uint64, err error)

	MultiGet(ctx context.Context, ids []uint64) (map[uint64]*model.UserExample, error)
	MultiSet(ctx context.Context, data []*model.UserExample, duration time.Duration) error

	Del(ctx context.Context, id uint64) error
	DelByPrefix(ctx context.Context, prefix string) error
	DelByKey(ctx context.Context, key string) error

	SetPlaceholder(ctx context.Context, id uint64) error
	SetPlaceholderByKey(ctx context.Context, key string) error
	IsPlaceholderErr(err error) bool
}

// userExampleCache define a cache struct
type userExampleCache struct {
	cache cache.Cache
}

// NewUserExampleCache new a cache
func NewUserExampleCache(cacheType *database.CacheType) UserExampleCache {
	jsonEncoding := encoding.JSONEncoding{}
	cachePrefix := ""

	cType := strings.ToLower(cacheType.CType)
	if cType == "redis" {
		c := cache.NewRedisCache(cacheType.Rdb, cachePrefix, jsonEncoding, func() interface{} {
			return &model.UserExample{}
		})
		return &userExampleCache{cache: c}
	}

	return nil // no cache
}

// GetUserExampleCacheKey cache key
func (c *userExampleCache) GetUserExampleCacheKey(id uint64) string {
	return UserExampleCachePrefixKey + utils.Uint64ToStr(id)
}
func (c *userExampleCache) GetUserExampleCacheKeyString(key string) string {
	return UserExampleCachePrefixKey + key
}

func (c *userExampleCache) getLockCacheKey(key string) string {
	return fmt.Sprintf("%s%v", UserExampleCachePrefixKeyLock, key)
}

func (c *userExampleCache) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	cacheKey := c.getLockCacheKey(key)
	return c.cache.GetLoopLock(ctx, cacheKey, options...)
}

func (c *userExampleCache) GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	cacheKey := c.getLockCacheKey(key)
	return c.cache.GetLock(ctx, cacheKey, options...)
}
func (c *userExampleCache) WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	// 1. 获取普通锁
	lock, err := c.GetLock(ctx, key, options...)
	if err != nil {
		return err
	}

	// 2. 获取当前的过期时间，用于计算续期频率
	if expiry <= 0 {
		expiry = 10 * time.Second // 默认兜底
	}
	// --- 看门狗实现开始 ---
	// 3. 启动看门狗协程
	watchdogCtx, stopWatchdog := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(expiry / 3) // 建议三分之一时间续期一次，更安全
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 使用原始 ctx 确保续期动作本身不被 task 的取消所影响
				ok, err := lock.ExtendContext(ctx)
				if err != nil || !ok {
					// 关键点：续期失败，立即取消任务 context
					if err != nil {
						logger.WarnWithCtx(ctx, "看门狗续期异常", logger.Err(err), logger.String("key", key))
					} else {
						logger.WarnWithCtx(ctx, "看门狗续期失败：锁已过期", logger.String("key", key))
					}
					stopWatchdog() // 续期失败，通知业务中断
					return
				}
			case <-watchdogCtx.Done(): // 业务执行完或被取消，看门狗退出
				return
			}
		}
	}()
	// --- 看门狗实现结束 ---
	// 4. 执行业务逻辑并在结束时释放所有资源
	defer func() {
		stopWatchdog() // 确保退出时关闭协程
		if _, releaseErr := lock.UnlockContext(ctx); releaseErr != nil {
			if !strings.Contains(releaseErr.Error(), "lock was already expired") {
				logger.WarnWithCtx(ctx, "释放分布式锁失败", logger.Err(releaseErr))
			}
		}
	}()
	if task == nil {
		logger.WarnWithCtx(ctx, "WatchDogLock: task 参数为 nil", logger.String("key", key))
		return errors.New("task function cannot be nil")
	}
	return task(watchdogCtx)
}

func (c *userExampleCache) WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	// 1. 获取循环锁 (复用已有的 GetLoopLock 逻辑)
	lock, err := c.GetLoopLock(ctx, key, options...)
	if err != nil {
		return err
	}

	// 2. 获取当前的过期时间，用于计算续期频率
	if expiry <= 0 {
		expiry = 10 * time.Second // 默认兜底
	}
	// --- 看门狗实现开始 ---
	// 3. 启动看门狗协程
	watchdogCtx, stopWatchdog := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(expiry / 3) // 建议三分之一时间续期一次，更安全
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 使用原始 ctx 确保续期动作本身不被 task 的取消所影响
				ok, err := lock.ExtendContext(ctx)
				if err != nil || !ok {
					// 关键点：续期失败，立即取消任务 context
					if err != nil {
						logger.WarnWithCtx(ctx, "看门狗续期异常", logger.Err(err), logger.String("key", key))
					} else {
						logger.WarnWithCtx(ctx, "看门狗续期失败：锁已过期", logger.String("key", key))
					}
					stopWatchdog() // 续期失败，通知业务中断
					return
				}
			case <-watchdogCtx.Done(): // 业务执行完或被取消，看门狗退出
				return
			}
		}
	}()
	// --- 看门狗实现结束 ---
	// 4. 执行业务逻辑并在结束时释放所有资源
	defer func() {
		stopWatchdog() // 确保退出时关闭协程
		if _, releaseErr := lock.UnlockContext(ctx); releaseErr != nil {
			if !strings.Contains(releaseErr.Error(), "lock was already expired") {
				logger.WarnWithCtx(ctx, "释放分布式锁失败", logger.Err(releaseErr))
			}
		}
	}()
	if task == nil {
		logger.WarnWithCtx(ctx, "WatchDogLoopLock: task 参数为 nil", logger.String("key", key))
		return errors.New("task function cannot be nil")
	}
	return task(watchdogCtx)
}

// Set write to cache
func (c *userExampleCache) Set(ctx context.Context, id uint64, data *model.UserExample, duration time.Duration) error {
	if data == nil || id == 0 {
		return nil
	}
	cacheKey := c.GetUserExampleCacheKey(id)
	err := c.cache.Set(ctx, cacheKey, data, duration)
	if err != nil {
		return err
	}
	return nil
}
// SetIDByKey set id by key (deprecated: use SetIdByKey for consistency)
func (c *userExampleCache) SetIdByKey(ctx context.Context, key string, id uint64, duration time.Duration) error {
	if key == "" || id == 0 {
		return nil
	}
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err := c.cache.Set(ctx, cacheKey, &id, duration)
	if err != nil {
		return err
	}
	return nil
}

func (c *userExampleCache) SetIdsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error {
	if key == "" || ids == nil {
		return nil
	}
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err := c.cache.Set(ctx, cacheKey, &ids, duration)
	if err != nil {
		return err
	}
	return nil
}

// Get cache value
func (c *userExampleCache) Get(ctx context.Context, id uint64) (*model.UserExample, error) {
	var data *model.UserExample
	cacheKey := c.GetUserExampleCacheKey(id)
	err := c.cache.Get(ctx, cacheKey, &data)
	if err != nil {
		return nil, err
	}
	return data, nil
}
func (c *userExampleCache) GetIdByKey(ctx context.Context, key string) (id uint64, err error) {
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err = c.cache.Get(ctx, cacheKey, &id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (c *userExampleCache) GetIdsByKey(ctx context.Context, key string) (ids []uint64, err error) {
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err = c.cache.Get(ctx, cacheKey, &ids)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// MultiSet multiple set cache
func (c *userExampleCache) MultiSet(ctx context.Context, data []*model.UserExample, duration time.Duration) error {
	valMap := make(map[string]interface{})
	for _, v := range data {
		cacheKey := c.GetUserExampleCacheKey(v.ID)
		valMap[cacheKey] = v
	}

	err := c.cache.MultiSet(ctx, valMap, duration)
	if err != nil {
		return err
	}

	return nil
}

// MultiGet multiple get cache, return key in map is id value
func (c *userExampleCache) MultiGet(ctx context.Context, ids []uint64) (map[uint64]*model.UserExample, error) {
	var keys []string
	for _, v := range ids {
		cacheKey := c.GetUserExampleCacheKey(v)
		keys = append(keys, cacheKey)
	}

	itemMap := make(map[string]*model.UserExample)
	err := c.cache.MultiGet(ctx, keys, itemMap)
	if err != nil {
		return nil, err
	}

	retMap := make(map[uint64]*model.UserExample)
	for _, id := range ids {
		val, ok := itemMap[c.GetUserExampleCacheKey(id)]
		if ok {
			retMap[id] = val
		}
	}

	return retMap, nil
}

// Del delete cache
func (c *userExampleCache) Del(ctx context.Context, id uint64) error {
	cacheKey := c.GetUserExampleCacheKey(id)
	err := c.cache.Del(ctx, cacheKey)
	if err != nil {
		return err
	}
	return nil
}

func (c *userExampleCache) DelByPrefix(ctx context.Context, prefix string) error {
	err := c.cache.DelByPrefix(ctx, prefix)
	if err != nil {
		return err
	}
	return nil
}
func (c *userExampleCache) DelByKey(ctx context.Context, key string) error {
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err := c.cache.Del(ctx, cacheKey)
	if err != nil {
		return err
	}
	return nil
}

// SetPlaceholder set placeholder value to cache
func (c *userExampleCache) SetPlaceholder(ctx context.Context, id uint64) error {
	cacheKey := c.GetUserExampleCacheKey(id)
	return c.cache.SetCacheWithNotFound(ctx, cacheKey)
}
func (c *userExampleCache) SetPlaceholderByKey(ctx context.Context, key string) error {
	cacheKey := c.GetUserExampleCacheKeyString(key)
	return c.cache.SetCacheWithNotFound(ctx, cacheKey)
}

// IsPlaceholderErr check if cache is placeholder error
func (c *userExampleCache) IsPlaceholderErr(err error) bool {
	return errors.Is(err, cache.ErrPlaceholder)
}
