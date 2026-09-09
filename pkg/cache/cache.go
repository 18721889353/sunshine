// Package cache 提供通用缓存驱动接口
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/go-redsync/redsync/v4"
)

var (
	// ErrPlaceholder 表示缓存命中了占位符（表示数据不存在）。
	ErrPlaceholder = errors.New("cache: placeholder hit")
	// NotFoundPlaceholder 占位符的具体值，用于标记空数据。
	NotFoundPlaceholder = "*"
	// DefaultNotFoundExpireTime 占位符的默认过期时间。
	DefaultNotFoundExpireTime = time.Minute * 1
)

// Cache 通用缓存接口（不依赖任何业务包）
type Cache interface {
	// GetLoopLock 获取一个阻塞式的分布式锁（循环等待直到获取锁或上下文取消）。
	GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	// GetLock 获取一个非阻塞的分布式锁（尝试一次，失败立即返回错误）。
	GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)
	// WatchDogLock 获取一个带看门狗自动续期的分布式锁（非阻塞）。
	WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error
	// WatchDogLoopLock 获取一个带看门狗自动续期的分布式锁（阻塞式）。
	WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error

	// Set 将一个值序列化后写入缓存，并设置过期时间。
	Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) error
	// Get 从缓存中获取指定 key 的值，并反序列化到 val 中。
	Get(ctx context.Context, key string, val interface{}) error
	// Del 删除一个或多个缓存 key。
	Del(ctx context.Context, keys ...string) error

	// MultiSet 批量设置多个 key-value 到缓存。
	MultiSet(ctx context.Context, valMap map[string]interface{}, expireTime time.Duration) error
	// MultiGet 批量获取多个 key 的值，并填充到 valueMap 中。
	MultiGet(ctx context.Context, keys []string, valueMap interface{}) error

	// DelByPrefix 根据前缀批量删除缓存（使用 SCAN 遍历）。
	DelByPrefix(ctx context.Context, prefix string) error

	// SetCacheWithNotFound 为指定 key 设置占位符，防止缓存穿透。
	SetCacheWithNotFound(ctx context.Context, key string) error
}
