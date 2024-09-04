// Package cache is memory and redis cache libraries.
package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	pkgLogger "github.com/18721889353/sunshine/pkg/logger"
	"go.uber.org/zap"
)

var (
	// DefaultExpireTime default expiry time
	DefaultExpireTime = time.Hour * 24
	// DefaultNotFoundExpireTime expiry time when result is empty 1 minute,
	// often used for cache time when data is empty (cache pass-through)
	DefaultNotFoundExpireTime = time.Minute * 10
	// NotFoundPlaceholder placeholder
	NotFoundPlaceholder = "*"

	// DefaultClient generate a cache client, where keyPrefix is generally the business prefix
	DefaultClient Cache

	// ErrPlaceholder .
	ErrPlaceholder = errors.New("cache: placeholder")
	// ErrSetMemoryWithNotFound .
	ErrSetMemoryWithNotFound = errors.New("cache: set memory cache err for not found")
)

// Cache driver interface
type Cache interface {
	Set(ctx context.Context, key string, val interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string, val interface{}) error
	MultiSet(ctx context.Context, valMap map[string]interface{}, expiration time.Duration) error
	MultiGet(ctx context.Context, keys []string, valueMap interface{}) error
	Del(ctx context.Context, keys ...string) error
	SetCacheWithNotFound(ctx context.Context, key string) error
}

// Set data
func Set(ctx context.Context, key string, val interface{}, expiration time.Duration) error {
	begin := time.Now()
	res := DefaultClient.Set(ctx, key, val, expiration)
	elapsed := time.Since(begin)
	pkgLogger.Info("Cache msg",
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		zap.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Set"),
	)
	return res
}

// Get data
func Get(ctx context.Context, key string, val interface{}) error {
	begin := time.Now()
	res := DefaultClient.Get(ctx, key, val)
	elapsed := time.Since(begin)
	pkgLogger.Info("Cache msg",
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		zap.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Get"),
	)
	return res
}

// MultiSet multiple set data
func MultiSet(ctx context.Context, valMap map[string]interface{}, expiration time.Duration) error {
	begin := time.Now()
	res := DefaultClient.MultiSet(ctx, valMap, expiration)
	elapsed := time.Since(begin)
	pkgLogger.Info("Cache msg",
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		zap.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg MultiSet"),
	)
	return res
}

// MultiGet multiple get data
func MultiGet(ctx context.Context, keys []string, valueMap interface{}) error {
	begin := time.Now()
	res := DefaultClient.MultiGet(ctx, keys, valueMap)
	elapsed := time.Since(begin)
	pkgLogger.Info("Cache msg",
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		zap.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg MultiGet"),
	)
	return res
}

// Del multiple delete data
func Del(ctx context.Context, keys ...string) error {
	begin := time.Now()
	res := DefaultClient.Del(ctx, keys...)
	elapsed := time.Since(begin)
	pkgLogger.Info("Cache msg",
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		zap.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Del"),
	)
	return res

}

// SetCacheWithNotFound .
func SetCacheWithNotFound(ctx context.Context, key string) error {
	begin := time.Now()
	res := DefaultClient.SetCacheWithNotFound(ctx, key)
	elapsed := time.Since(begin)
	pkgLogger.Info("Cache msg",
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		zap.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg SetCacheWithNotFound"),
	)
	return res

}

func requestIDField(ctx context.Context, requestIDKey string) zap.Field {
	if requestIDKey == "" {
		return zap.Skip()
	}

	var field zap.Field
	if requestIDKey != "" {
		if v, ok := ctx.Value(requestIDKey).(string); ok {
			field = zap.String(requestIDKey, v)
		} else {
			field = zap.Skip()
		}
	}
	return field
}
