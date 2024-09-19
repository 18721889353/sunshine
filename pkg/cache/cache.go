// Package cache is  redis cache libraries.
package cache

import (
	"context"
	"errors"
	"github.com/go-redsync/redsync/v4"
	"time"
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
	GetLoopLock(ctx context.Context, key string, expireTime, loopWaitTime time.Duration, loopNum int) (*redsync.Mutex, error)
	GetLock(ctx context.Context, key string, expireTime time.Duration) (*redsync.Mutex, error)
	ReleaseLock(ctx context.Context, mutex *redsync.Mutex) error
	Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) error
	Get(ctx context.Context, key string, val interface{}) error
	MultiSet(ctx context.Context, valMap map[string]interface{}, expireTime time.Duration) error
	MultiGet(ctx context.Context, keys []string, valueMap interface{}) error
	Del(ctx context.Context, keys ...string) error
	SetCacheWithNotFound(ctx context.Context, key string) error
}

func GetLoopLock(ctx context.Context, key string, expireTime, loopWaitTime time.Duration, loopNum int) (*redsync.Mutex, error) {
	return DefaultClient.GetLoopLock(ctx, key, expireTime, loopWaitTime, loopNum)
}
func GetLock(ctx context.Context, key string, expireTime time.Duration) (*redsync.Mutex, error) {
	return DefaultClient.GetLock(ctx, key, expireTime)
}
func ReleaseLock(ctx context.Context, mutex *redsync.Mutex) error {
	return DefaultClient.ReleaseLock(ctx, mutex)
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

// SetCacheWithNotFound .
func SetCacheWithNotFound(ctx context.Context, key string) error {
	return DefaultClient.SetCacheWithNotFound(ctx, key)
}
