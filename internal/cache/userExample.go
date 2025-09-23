package cache

import (
	"context"
	"errors"
	"fmt"
	"github.com/18721889353/sunshine/pkg/logger"
	"strings"
	"time"

	"github.com/go-redsync/redsync/v4"

	"github.com/18721889353/sunshine/pkg/cache"
	"github.com/18721889353/sunshine/pkg/encoding"
	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/model"
)

const (
	// cache prefix key, must end with a colon
	UserExampleCachePrefixKeyLock = "userExampleLock:"
	UserExampleCachePrefixKey     = "userExample:"
	// UserExampleExpireTime expire time
	UserExampleExpireTime = 180 * time.Minute
)

var _ UserExampleCache = (*userExampleCache)(nil)

// UserExampleCache cache interface
type UserExampleCache interface {
	GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)

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
	switch cType {
	case "redis":
		c := cache.NewRedisCache(cacheType.Rdb, cachePrefix, jsonEncoding, func() interface{} {
			return &model.UserExample{}
		}, cache.WithCacheLog(logger.Get()))
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
