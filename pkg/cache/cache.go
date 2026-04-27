// Package cache is  redis cache libraries.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/go-redsync/redsync/v4"
)

var (
	// DefaultExpireTime default expiry time
	DefaultExpireTime = time.Hour * 24
	// DefaultNotFoundExpireTime expiry time when result is empty 1 minute,
	// often used for cache time when data is empty (cache pass-through)
	DefaultNotFoundExpireTime = time.Minute * 1
	// NotFoundPlaceholder placeholder
	NotFoundPlaceholder = "*"

	// DefaultClient generate a cache client, where keyPrefix is generally the business prefix
	DefaultClient Cache

	// ErrPlaceholder .
	ErrPlaceholder = errors.New("cache: placeholder")
)

// Cache driver interface
type Cache interface {
	GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) error
	Get(ctx context.Context, key string, val interface{}) error
	MultiSet(ctx context.Context, valMap map[string]interface{}, expireTime time.Duration) error
	MultiGet(ctx context.Context, keys []string, valueMap interface{}) error
	Del(ctx context.Context, keys ...string) error
	DelByPrefix(ctx context.Context, prefix string) error
	SetCacheWithNotFound(ctx context.Context, key string) error
}

// GetLoopLock 获取循环等待的分布式锁
func GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	return DefaultClient.GetLoopLock(ctx, key, options...)
}

// GetLock 获取一次性分布式锁
func GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	return DefaultClient.GetLock(ctx, key, options...)
}

// Set data
func Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) error {
	return DefaultClient.Set(ctx, key, val, expireTime)
}

// Get data
func Get(ctx context.Context, key string, val interface{}) error {
	return DefaultClient.Get(ctx, key, val)
}

// MultiSet multiple set data
func MultiSet(ctx context.Context, valMap map[string]interface{}, expireTime time.Duration) error {
	return DefaultClient.MultiSet(ctx, valMap, expireTime)
}

// MultiGet multiple get data
func MultiGet(ctx context.Context, keys []string, valueMap interface{}) error {
	return DefaultClient.MultiGet(ctx, keys, valueMap)
}

// Del multiple delete data
func Del(ctx context.Context, keys ...string) error {
	return DefaultClient.Del(ctx, keys...)
}

// DelByPrefix multiple delete data
func DelByPrefix(ctx context.Context, prefix string) error {
	return DefaultClient.DelByPrefix(ctx, prefix)
}

// SetCacheWithNotFound .
func SetCacheWithNotFound(ctx context.Context, key string) error {
	return DefaultClient.SetCacheWithNotFound(ctx, key)
}
