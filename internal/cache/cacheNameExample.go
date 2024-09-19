package cache

import (
	"context"
	"fmt"
	"github.com/go-redsync/redsync/v4"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/cache"
	"github.com/18721889353/sunshine/pkg/encoding"

	"github.com/18721889353/sunshine/internal/model"
)

// delete the templates code start
type keyTypeExample = string
type valueTypeExample = string

// delete the templates code end

const (
	// cache prefix key, must end with a colon
	cacheNameExampleCachePrefixKey = "prefixKeyExample:"
	// CacheNameExampleExpireTime expire time
	CacheNameExampleExpireTime = 5 * time.Minute
)

var _ CacheNameExampleCache = (*cacheNameExampleCache)(nil)

// CacheNameExampleCache cache interface
type CacheNameExampleCache interface {
	GetLoopLock(ctx context.Context, keyNameExample keyTypeExample, expireTime, loopWaitTime time.Duration, loopNum int) (*redsync.Mutex, error)
	GetLock(ctx context.Context, keyNameExample keyTypeExample, expireTime time.Duration) (*redsync.Mutex, error)
	ReleaseLock(ctx context.Context, mutex *redsync.Mutex) error
	Set(ctx context.Context, keyNameExample keyTypeExample, valueNameExample valueTypeExample, expireTime time.Duration) error
	Get(ctx context.Context, keyNameExample keyTypeExample) (valueTypeExample, error)
	Del(ctx context.Context, keyNameExample keyTypeExample) error
}

type cacheNameExampleCache struct {
	cache cache.Cache
}

// NewCacheNameExampleCache create a new cache
func NewCacheNameExampleCache(cacheType *model.CacheType) CacheNameExampleCache {
	newObject := func() interface{} {
		return ""
	}
	cachePrefix := ""
	jsonEncoding := encoding.JSONEncoding{}

	cType := strings.ToLower(cacheType.CType)
	switch cType {
	case "redis":
		c := cache.NewRedisCache(cacheType.Rdb, cachePrefix, jsonEncoding, newObject)
		return &cacheNameExampleCache{cache: c}
	}

	panic(fmt.Sprintf("unsupported cache type='%s'", cacheType.CType))
}

// cache key
func (c *cacheNameExampleCache) getCacheKey(keyNameExample keyTypeExample) string {
	return fmt.Sprintf("%s%v", cacheNameExampleCachePrefixKey, keyNameExample)
}
func (c *cacheNameExampleCache) GetLoopLock(ctx context.Context, keyNameExample keyTypeExample, expireTime, loopWaitTime time.Duration, loopNum int) (*redsync.Mutex, error) {
	cacheKey := c.getCacheKey(keyNameExample)
	lock, err := c.cache.GetLoopLock(ctx, cacheKey, expireTime, loopWaitTime, loopNum)
	if err != nil {
		return nil, err
	}
	return lock, nil
}
func (c *cacheNameExampleCache) GetLock(ctx context.Context, keyNameExample keyTypeExample, expireTime time.Duration) (*redsync.Mutex, error) {
	cacheKey := c.getCacheKey(keyNameExample)
	lock, err := c.cache.GetLock(ctx, cacheKey, expireTime)
	if err != nil {
		return nil, err
	}
	return lock, nil
}
func (c *cacheNameExampleCache) ReleaseLock(ctx context.Context, mutex *redsync.Mutex) error {
	return c.cache.ReleaseLock(ctx, mutex)
}

// Set cache
func (c *cacheNameExampleCache) Set(ctx context.Context, keyNameExample keyTypeExample, valueNameExample valueTypeExample, expireTime time.Duration) error {
	cacheKey := c.getCacheKey(keyNameExample)
	return c.cache.Set(ctx, cacheKey, &valueNameExample, expireTime)
}

// Get cache
func (c *cacheNameExampleCache) Get(ctx context.Context, keyNameExample keyTypeExample) (valueTypeExample, error) {
	var valueNameExample valueTypeExample
	cacheKey := c.getCacheKey(keyNameExample)
	err := c.cache.Get(ctx, cacheKey, &valueNameExample)
	if err != nil {
		return valueNameExample, err
	}
	return valueNameExample, nil
}

// Del delete cache
func (c *cacheNameExampleCache) Del(ctx context.Context, keyNameExample keyTypeExample) error {
	cacheKey := c.getCacheKey(keyNameExample)
	return c.cache.Del(ctx, cacheKey)
}
