package dlock

import (
	"context"
	"errors"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/redis/go-redis/v9"
)

// RedisLock 使用 Redis 实现的分布式锁
type RedisLock struct {
	mutex *redsync.Mutex // Redis 分布式锁
}

// NewRedisLock 创建一个新的 Redis 分布式锁
// 参数：
// - client: Redis 客户端实例，不能为空
// - key: 锁的键，不能为空
// - options: 可选的 redsync 选项
// 返回值：
// - Locker: 分布式锁接口实例
// - error: 如果创建失败则返回错误
func NewRedisLock(client *redis.Client, key string, options ...redsync.Option) (Locker, error) {
	if client == nil {
		return nil, errors.New("redis client is nil")
	}
	if key == "" {
		return nil, errors.New("key is empty")
	}
	return newLocker(client, key, options...), nil
}

// NewRedisClusterLock 创建一个新的 Redis 集群分布式锁
// 参数：
// - clusterClient: Redis 集群客户端实例，不能为空
// - key: 锁的键，不能为空
// - options: 可选的 redsync 选项
// 返回值：
// - Locker: 分布式锁接口实例
// - error: 如果创建失败则返回错误
func NewRedisClusterLock(clusterClient *redis.ClusterClient, key string, options ...redsync.Option) (Locker, error) {
	if clusterClient == nil {
		return nil, errors.New("cluster redis client is nil")
	}
	if key == "" {
		return nil, errors.New("key is empty")
	}
	return newLocker(clusterClient, key, options...), nil
}

// newLocker 创建一个新的 Redis 分布式锁实例
// 参数：
// - delegate: Redis 客户端或集群客户端
// - key: 锁的键
// - options: 可选的 redsync 选项
// 返回值：
// - Locker: 分布式锁接口实例
func newLocker(delegate redis.UniversalClient, key string, options ...redsync.Option) Locker {
	pool := goredis.NewPool(delegate)     // 创建 Redis 连接池
	rs := redsync.New(pool)               // 创建 redsync 实例
	mutex := rs.NewMutex(key, options...) // 创建分布式互斥锁

	return &RedisLock{
		mutex: mutex,
	}
}

// TryLock 尝试获取锁而不阻塞
// 参数：
// - ctx: 上下文，用于控制锁的获取操作
// 返回值：
// - bool: 如果成功获取锁则返回 true，否则返回 false
// - error: 如果发生错误则返回错误
func (l *RedisLock) TryLock(ctx context.Context) (bool, error) {
	err := l.mutex.TryLockContext(ctx)
	if err == nil {
		return true, nil
	}
	return false, err
}

// Lock 阻塞直到获取到锁或上下文被取消
// 参数：
// - ctx: 上下文，用于控制锁的获取操作
// 返回值：
// - error: 如果获取锁失败则返回错误
func (l *RedisLock) Lock(ctx context.Context) error {
	return l.mutex.LockContext(ctx)
}

// Unlock 释放锁，如果解锁成功，键将被自动删除
// 参数：
// - ctx: 上下文，用于控制锁的释放操作
// 返回值：
// - error: 如果释放锁失败则返回错误
func (l *RedisLock) Unlock(ctx context.Context) error {
	_, err := l.mutex.UnlockContext(ctx)
	return err
}

// Close 对于 RedisLock 是一个空操作
// 返回值：
// - error: 始终返回 nil
func (l *RedisLock) Close() error {
	return nil
}
