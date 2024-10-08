package cache

import (
	"context"
	"github.com/go-redsync/redsync/v4"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/cache"
	"github.com/18721889353/sunshine/pkg/encoding"
	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/model"
)

const (
	// cache prefix key, must end with a colon
	userExampleCachePrefixKey = "userExample:"
	// UserExampleExpireTime expire time
	UserExampleExpireTime = 5 * time.Minute
)

var _ UserExampleCache = (*userExampleCache)(nil)

// UserExampleCache cache interface
type UserExampleCache interface {
	GetLoopLock(ctx context.Context, id uint64, expireTime, loopWaitTime time.Duration, loopNum int) (*redsync.Mutex, error)
	GetLock(ctx context.Context, id uint64, timeout time.Duration) (*redsync.Mutex, error)
	ReleaseLock(ctx context.Context, mutex *redsync.Mutex) error
	Set(ctx context.Context, id uint64, data *model.UserExample, duration time.Duration) error
	Get(ctx context.Context, id uint64) (*model.UserExample, error)
	MultiGet(ctx context.Context, ids []uint64) (map[uint64]*model.UserExample, error)
	MultiSet(ctx context.Context, data []*model.UserExample, duration time.Duration) error
	Del(ctx context.Context, id uint64) error
	SetCacheWithNotFound(ctx context.Context, id uint64) error
}

// userExampleCache define a cache struct
type userExampleCache struct {
	cache cache.Cache
}

// NewUserExampleCache new a cache
func NewUserExampleCache(cacheType *model.CacheType) UserExampleCache {
	jsonEncoding := encoding.JSONEncoding{}
	cachePrefix := ""

	cType := strings.ToLower(cacheType.CType)
	switch cType {
	case "redis":
		c := cache.NewRedisCache(cacheType.Rdb, cachePrefix, jsonEncoding, func() interface{} {
			return &model.UserExample{}
		})
		return &userExampleCache{cache: c}
	}

	return nil // no cache
}

// GetUserExampleCacheKey cache key
func (c *userExampleCache) GetUserExampleCacheKey(id uint64) string {
	return userExampleCachePrefixKey + utils.Uint64ToStr(id)
}

func (c *userExampleCache) GetLoopLock(ctx context.Context, id uint64, timeout, loopWaitTime time.Duration, loopNum int) (*redsync.Mutex, error) {
	cacheKey := c.GetUserExampleCacheKey(id)
	lock, err := c.cache.GetLoopLock(ctx, cacheKey, timeout, loopWaitTime, loopNum)
	if err != nil {
		return nil, err
	}
	return lock, nil
}
func (c *userExampleCache) GetLock(ctx context.Context, id uint64, timeout time.Duration) (*redsync.Mutex, error) {
	cacheKey := c.GetUserExampleCacheKey(id)
	lock, err := c.cache.GetLock(ctx, cacheKey, timeout)
	if err != nil {
		return nil, err
	}
	return lock, nil
}
func (c *userExampleCache) ReleaseLock(ctx context.Context, mutex *redsync.Mutex) error {
	return c.cache.ReleaseLock(ctx, mutex)
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

// SetCacheWithNotFound set empty cache
func (c *userExampleCache) SetCacheWithNotFound(ctx context.Context, id uint64) error {
	cacheKey := c.GetUserExampleCacheKey(id)
	err := c.cache.SetCacheWithNotFound(ctx, cacheKey)
	if err != nil {
		return err
	}
	return nil
}
