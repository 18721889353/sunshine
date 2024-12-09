// Package dlock 提供分布式锁原语，支持 Redis 和 etcd。
package dlock

import "context"

// Locker 是一个接口，封装了基本的锁定操作。
type Locker interface {
	// Lock 阻塞直到获取到锁或上下文被取消。
	// 参数：
	// - ctx: 上下文，用于控制锁的获取操作。
	// 返回值：
	// - error: 如果获取锁失败则返回错误。
	Lock(ctx context.Context) error

	// Unlock 释放锁，如果解锁成功，键将被自动删除。
	// 参数：
	// - ctx: 上下文，用于控制锁的释放操作。
	// 返回值：
	// - error: 如果释放锁失败则返回错误。
	Unlock(ctx context.Context) error

	// TryLock 尝试获取锁而不阻塞。
	// 参数：
	// - ctx: 上下文，用于控制锁的获取操作。
	// 返回值：
	// - bool: 如果成功获取锁则返回 true，否则返回 false。
	// - error: 如果发生错误则返回错误。
	TryLock(ctx context.Context) (bool, error)

	// Close 关闭锁资源。
	// 返回值：
	// - error: 如果关闭资源失败则返回错误。
	Close() error
}
