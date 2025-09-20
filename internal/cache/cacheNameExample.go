package cache

import (
	"context"
	"fmt"
	"github.com/go-redsync/redsync/v4"
	"strings"
	"time"

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
	cacheNameExampleCachePrefixKey = "prefixKeyExample:"
	// CacheNameExampleExpireTime expire time
	CacheNameExampleExpireTime = 180 * time.Minute
)

var _ CacheNameExampleCache = (*cacheNameExampleCache)(nil)

// CacheNameExampleCache cache interface
type CacheNameExampleCache interface {
	GetLoopLock(ctx context.Context, keyNameExample keyTypeExample, options ...redsync.Option) (*redsync.Mutex, error)
	GetLock(ctx context.Context, keyNameExample keyTypeExample, options ...redsync.Option) (*redsync.Mutex, error)
	Set(ctx context.Context, keyNameExample keyTypeExample, valueNameExample valueTypeExample, duration time.Duration) error
	Get(ctx context.Context, keyNameExample keyTypeExample) (valueTypeExample, error)
	Del(ctx context.Context, keyNameExample keyTypeExample) error
}

type cacheNameExampleCache struct {
	cache cache.Cache
}

// NewCacheNameExampleCache create a new cache
func NewCacheNameExampleCache(cacheType *database.CacheType) CacheNameExampleCache {
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
func (c *cacheNameExampleCache) GetLoopLock(ctx context.Context, keyNameExample keyTypeExample, options ...redsync.Option) (*redsync.Mutex, error) {
	cacheKey := c.getCacheKey(keyNameExample)
	return c.cache.GetLoopLock(ctx, cacheKey, options...)
}
func (c *cacheNameExampleCache) GetLock(ctx context.Context, keyNameExample keyTypeExample, options ...redsync.Option) (*redsync.Mutex, error) {
	cacheKey := c.getCacheKey(keyNameExample)
	return c.cache.GetLock(ctx, cacheKey, options...)

}

// Set cache
func (c *cacheNameExampleCache) Set(ctx context.Context, keyNameExample keyTypeExample, valueNameExample valueTypeExample, duration time.Duration) error {
	cacheKey := c.getCacheKey(keyNameExample)
	return c.cache.Set(ctx, cacheKey, &valueNameExample, duration)
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
