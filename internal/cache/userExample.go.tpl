lopackage cache

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
    {{.TableNameCamel}}CachePrefixKeyLock = "lock:{{.TableNameCamelFCL}}:"
	// cache prefix key, must end with a colon
	{{.TableNameCamel}}CachePrefixKey = "data:{{.TableNameCamelFCL}}:"
	// {{.TableNameCamel}}ExpireTime expire time
	{{.TableNameCamel}}ExpireTime = 30 * time.Minute
)

var _ {{.TableNameCamel}}Cache = (*{{.TableNameCamelFCL}}Cache)(nil)

// {{.TableNameCamel}}Cache cache interface
type {{.TableNameCamel}}Cache interface {
    GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	// 封装了锁的获取、看门狗自动续期、业务执行及释放逻辑
	WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error
	WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error

	Set(ctx context.Context, id uint64, data *model.{{.TableNameCamel}}, duration time.Duration) error
	SetIdByKey(ctx context.Context, key string, id uint64, duration time.Duration) error
	SetIdsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error

	Get(ctx context.Context, id uint64) (*model.{{.TableNameCamel}}, error)
	GetIdByKey(ctx context.Context, key string) (id uint64, err error)
	GetIdsByKey(ctx context.Context, key string) (ids []uint64, err error)

	MultiGet(ctx context.Context, ids []uint64) (map[uint64]*model.{{.TableNameCamel}}, error)
	MultiSet(ctx context.Context, data []*model.{{.TableNameCamel}}, duration time.Duration) error

	Del(ctx context.Context, id uint64) error
	DelByPrefix(ctx context.Context, prefix string) error
	DelByKey(ctx context.Context, key string) error

	SetPlaceholder(ctx context.Context, id uint64) error
	SetPlaceholderByKey(ctx context.Context, key string) error
	IsPlaceholderErr(err error) bool

}

// {{.TableNameCamelFCL}}Cache define a cache struct
type {{.TableNameCamelFCL}}Cache struct {
	cache cache.Cache
}

// New{{.TableNameCamel}}Cache new a cache
func New{{.TableNameCamel}}Cache(cacheType *database.CacheType) {{.TableNameCamel}}Cache {
	jsonEncoding := encoding.JSONEncoding{}
	cachePrefix := ""

	cType := strings.ToLower(cacheType.CType)
	switch cType {
	case "redis":
		c := cache.NewRedisCache(cacheType.Rdb, cachePrefix, jsonEncoding, func() interface{} {
			return &model.{{.TableNameCamel}}{}
		})
		return &{{.TableNameCamelFCL}}Cache{cache: c}
	}

	return nil // no cache
}



func (c *{{.TableNameCamelFCL}}Cache) getLockCacheKey(key string) string {
	return fmt.Sprintf("%s%v", {{.TableNameCamel}}CachePrefixKeyLock, key)
}


func (c *{{.TableNameCamelFCL}}Cache) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	lockCacheKey := c.getLockCacheKey(key)
	return c.cache.GetLoopLock(ctx, lockCacheKey, options...)
}

func (c *{{.TableNameCamelFCL}}Cache) GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	lockCacheKey := c.getLockCacheKey(key)
	return c.cache.GetLock(ctx, lockCacheKey, options...)
}

func (c *{{.TableNameCamelFCL}}Cache) WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
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

func (c *{{.TableNameCamelFCL}}Cache) WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
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


// Get{{.TableNameCamel}}CacheKey cache key
func (c *{{.TableNameCamelFCL}}Cache) Get{{.TableNameCamel}}CacheKey(id uint64) string {
	return {{.TableNameCamel}}CachePrefixKey + utils.Uint64ToStr(id)
}

func (c *{{.TableNameCamelFCL}}Cache) Get{{.TableNameCamel}}CacheKeyString(key string) string {
	return {{.TableNameCamel}}CachePrefixKey + key
}


// Set write to cache
func (c *{{.TableNameCamelFCL}}Cache) Set(ctx context.Context, id uint64, data *model.{{.TableNameCamel}}, duration time.Duration) error {
	if data == nil || id == 0 {
		return nil
	}
	cacheKey := c.Get{{.TableNameCamel}}CacheKey(id)
	err := c.cache.Set(ctx, cacheKey, data, duration)
	if err != nil {
		return err
	}
	return nil
}

func (c *{{.TableNameCamelFCL}}Cache) SetIdByKey(ctx context.Context, key string, id uint64, duration time.Duration) error {
	if key == "" || id == 0 {
		return nil
	}
	cacheKey := c.Get{{.TableNameCamel}}CacheKeyString(key)
	err := c.cache.Set(ctx, cacheKey, &id, duration)
	if err != nil {
		return err
	}
	return nil
}

func (c *{{.TableNameCamelFCL}}Cache) SetIdsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error {
	if key == "" || ids == nil {
		return nil
	}
	cacheKey := c.Get{{.TableNameCamel}}CacheKeyString(key)
	err := c.cache.Set(ctx, cacheKey, &ids, duration)
	if err != nil {
		return err
	}
	return nil
}

// Get cache value
func (c *{{.TableNameCamelFCL}}Cache) Get(ctx context.Context, id uint64) (*model.{{.TableNameCamel}}, error) {
	var data *model.{{.TableNameCamel}}
	cacheKey := c.Get{{.TableNameCamel}}CacheKey(id)
	err := c.cache.Get(ctx, cacheKey, &data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *{{.TableNameCamelFCL}}Cache) GetIdByKey(ctx context.Context, key string) (id uint64, err error) {
	cacheKey := c.Get{{.TableNameCamel}}CacheKeyString(key)
	err = c.cache.Get(ctx, cacheKey, &id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (c *{{.TableNameCamelFCL}}Cache) GetIdsByKey(ctx context.Context, key string) (ids []uint64, err error) {
	cacheKey := c.Get{{.TableNameCamel}}CacheKeyString(key)
	err = c.cache.Get(ctx, cacheKey, &ids)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// MultiSet multiple set cache
func (c *{{.TableNameCamelFCL}}Cache) MultiSet(ctx context.Context, data []*model.{{.TableNameCamel}}, duration time.Duration) error {
	valMap := make(map[string]interface{})
	for _, v := range data {
		cacheKey := c.Get{{.TableNameCamel}}CacheKey(v.ID)
		valMap[cacheKey] = v
	}

	err := c.cache.MultiSet(ctx, valMap, duration)
	if err != nil {
		return err
	}

	return nil
}

// MultiGet multiple get cache, return key in map is id value
func (c *{{.TableNameCamelFCL}}Cache) MultiGet(ctx context.Context, ids []uint64) (map[uint64]*model.{{.TableNameCamel}}, error) {
	var keys []string
	for _, v := range ids {
		cacheKey := c.Get{{.TableNameCamel}}CacheKey(v)
		keys = append(keys, cacheKey)
	}

	itemMap := make(map[string]*model.{{.TableNameCamel}})
	err := c.cache.MultiGet(ctx, keys, itemMap)
	if err != nil {
		return nil, err
	}

	retMap := make(map[uint64]*model.{{.TableNameCamel}})
	for _, id := range ids {
		val, ok := itemMap[c.Get{{.TableNameCamel}}CacheKey(id)]
		if ok {
			retMap[id] = val
		}
	}

	return retMap, nil
}

// Del delete cache
func (c *{{.TableNameCamelFCL}}Cache) Del(ctx context.Context, id uint64) error {
	cacheKey := c.Get{{.TableNameCamel}}CacheKey(id)
	err := c.cache.Del(ctx, cacheKey)
	if err != nil {
		return err
	}
	return nil
}

func (c *{{.TableNameCamelFCL}}Cache) DelByPrefix(ctx context.Context, prefix string) error {
	err := c.cache.DelByPrefix(ctx, prefix)
	if err != nil {
		return err
	}
	return nil
}

func (c *{{.TableNameCamelFCL}}Cache) DelByKey(ctx context.Context, key string) error {
	cacheKey := c.Get{{.TableNameCamel}}CacheKeyString(key)
	err := c.cache.Del(ctx, cacheKey)
	if err != nil {
		return err
	}
	return nil
}

// SetPlaceholder set placeholder value to cache
func (c *{{.TableNameCamelFCL}}Cache) SetPlaceholder(ctx context.Context, id uint64) error {
	cacheKey := c.Get{{.TableNameCamel}}CacheKey(id)
	return c.cache.SetCacheWithNotFound(ctx, cacheKey)
}
func (c *{{.TableNameCamelFCL}}Cache) SetPlaceholderByKey(ctx context.Context, key string) error {
	cacheKey := c.Get{{.TableNameCamel}}CacheKeyString(key)
	return c.cache.SetCacheWithNotFound(ctx, cacheKey)
}

// IsPlaceholderErr check if cache is placeholder error
func (c *{{.TableNameCamelFCL}}Cache) IsPlaceholderErr(err error) bool {
	return errors.Is(err, cache.ErrPlaceholder)
}
