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

	"github.com/18721889353/sunshine/internal/database"
)

// delete the templates code start
type keyTypeExample = string
type valueTypeExample = string

// delete the templates code end

const (
	// cache prefix key, must end with a colon
	CacheNameExampleCachePrefixKeyLock = "lock:prefixKeyExample:"
	CacheNameExampleCachePrefixKey     = "data:prefixKeyExample:"
	// CacheNameExampleExpireTime expire time
	CacheNameExampleExpireTime = 30 * time.Minute
)

var _ CacheNameExample = (*cacheNameExample)(nil)

// CacheNameExample cache interface
type CacheNameExample interface {
	GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	// 封装了锁的获取、看门狗自动续期、业务执行及释放逻辑
	WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error
	WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error

	Set(ctx context.Context, key string, data interface{}, duration time.Duration) error

	Get(ctx context.Context, key string) (interface{}, error)
	GetIdByKey(ctx context.Context, key string) (id string, err error)

	Del(ctx context.Context, key string) error
	DelByPrefix(ctx context.Context, prefix string) error
	DelByKey(ctx context.Context, key string) error

	SetPlaceholder(ctx context.Context, key string) error
	SetPlaceholderByKey(ctx context.Context, key string) error
	IsPlaceholderErr(err error) bool

	GetCacheName(ctx context.Context, keyNameExample keyTypeExample) (valueTypeExample, error)
	SetCacheName(ctx context.Context, keyNameExample keyTypeExample, data valueTypeExample, duration time.Duration) error
	DelCacheName(ctx context.Context, keyNameExample keyTypeExample) error
}

// cacheNameExample define a cache struct
type cacheNameExample struct {
	cache cache.Cache
}

// NewCacheNameExample new a cache
func NewCacheNameExample(cacheType *database.CacheType) CacheNameExample {
	jsonEncoding := encoding.JSONEncoding{}
	cachePrefix := ""

	cType := strings.ToLower(cacheType.CType)
	if cType == "redis" {
		c := cache.NewRedisCache(cacheType.Rdb, cachePrefix, jsonEncoding, func() interface{} {
			var valueNameExample interface{}
			return &valueNameExample
		})
		return &cacheNameExample{cache: c}
	}

	return nil // no cache
}

// GetCacheNameExampleCacheKey cache key
func (c *cacheNameExample) GetCacheNameExampleCacheKey(key string) string {
	return CacheNameExampleCachePrefixKey + key
}

func (c *cacheNameExample) getCacheKey(keyNameExample keyTypeExample) string {
	return fmt.Sprintf("%s%v", CacheNameExampleCachePrefixKey, keyNameExample)
}

func (c *cacheNameExample) getLockCacheKey(key string) string {
	return fmt.Sprintf("%s%v", CacheNameExampleCachePrefixKeyLock, key)
}

func (c *cacheNameExample) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	cacheKey := c.getLockCacheKey(key)
	return c.cache.GetLoopLock(ctx, cacheKey, options...)
}

func (c *cacheNameExample) GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	cacheKey := c.getLockCacheKey(key)
	return c.cache.GetLock(ctx, cacheKey, options...)
}
func (c *cacheNameExample) WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
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

func (c *cacheNameExample) WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
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
func (c *cacheNameExample) Set(ctx context.Context, key string, data interface{}, duration time.Duration) error {
	if data == nil || key == "" {
		return nil
	}
	cacheKey := c.GetCacheNameExampleCacheKey(key)
	err := c.cache.Set(ctx, cacheKey, data, duration)
	if err != nil {
		return err
	}
	return nil
}

// Get cache value
func (c *cacheNameExample) Get(ctx context.Context, key string) (interface{}, error) {
	var data interface{}
	cacheKey := c.GetCacheNameExampleCacheKey(key)
	err := c.cache.Get(ctx, cacheKey, &data)
	if err != nil {
		return nil, err
	}
	return data, nil
}
func (c *cacheNameExample) GetIdByKey(ctx context.Context, key string) (id string, err error) {
	cacheKey := c.GetCacheNameExampleCacheKey(key)
	err = c.cache.Get(ctx, cacheKey, &id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// Del delete cache
func (c *cacheNameExample) Del(ctx context.Context, key string) error {
	cacheKey := c.GetCacheNameExampleCacheKey(key)
	err := c.cache.Del(ctx, cacheKey)
	if err != nil {
		return err
	}
	return nil
}

func (c *cacheNameExample) DelByPrefix(ctx context.Context, prefix string) error {
	err := c.cache.DelByPrefix(ctx, prefix)
	if err != nil {
		return err
	}
	return nil
}
func (c *cacheNameExample) DelByKey(ctx context.Context, key string) error {
	cacheKey := c.GetCacheNameExampleCacheKey(key)
	err := c.cache.Del(ctx, cacheKey)
	if err != nil {
		return err
	}
	return nil
}

// SetPlaceholder set placeholder value to cache
func (c *cacheNameExample) SetPlaceholder(ctx context.Context, key string) error {
	cacheKey := c.GetCacheNameExampleCacheKey(key)
	return c.cache.SetCacheWithNotFound(ctx, cacheKey)
}
func (c *cacheNameExample) SetPlaceholderByKey(ctx context.Context, key string) error {
	cacheKey := c.GetCacheNameExampleCacheKey(key)
	return c.cache.SetCacheWithNotFound(ctx, cacheKey)
}

// IsPlaceholderErr check if cache is placeholder error
func (c *cacheNameExample) IsPlaceholderErr(err error) bool {
	return errors.Is(err, cache.ErrPlaceholder)
}

// GetCacheName get cache value by key
func (c *cacheNameExample) GetCacheName(ctx context.Context, keyNameExample keyTypeExample) (valueTypeExample, error) {
	var valueNameExample valueTypeExample
	cacheKey := c.getCacheKey(keyNameExample)
	err := c.cache.Get(ctx, cacheKey, &valueNameExample)
	if err != nil {
		return valueNameExample, err
	}
	return valueNameExample, nil
}

// SetCacheName set cache value
func (c *cacheNameExample) SetCacheName(ctx context.Context, keyNameExample keyTypeExample, data valueTypeExample, duration time.Duration) error {
	cacheKey := c.getCacheKey(keyNameExample)
	return c.cache.Set(ctx, cacheKey, &data, duration)
}

// DelCacheName delete cache
func (c *cacheNameExample) DelCacheName(ctx context.Context, keyNameExample keyTypeExample) error {
	cacheKey := c.getCacheKey(keyNameExample)
	return c.cache.Del(ctx, cacheKey)
}
