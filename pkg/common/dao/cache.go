// Package dao 提供泛型数据访问层基础组件
package dao

import (
	"context"
	"time"

	"github.com/go-redsync/redsync/v4"
)

// LockDriver 分布式锁驱动接口
// 职责：提供跨服务器互斥的分布式锁能力，基于 Redis + redsync 实现
// 使用者：cacheManager（仅使用 GetLock）、业务层缓存（直接使用）
type LockDriver interface {
	// GetLoopLock 获取一个阻塞式的分布式锁（循环等待直到获取锁或上下文取消）
	GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)

	// GetLock 获取一个非阻塞的分布式锁（尝试一次，失败立即返回错误）
	GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)

	// WatchDogLock 获取一个带看门狗自动续期的分布式锁（非阻塞）
	WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error

	// WatchDogLoopLock 获取一个带看门狗自动续期的分布式锁（阻塞式）
	WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error
}

// CacheDriver 缓存读写驱动接口
// 职责：提供按 ID / 自定义 Key / 批量 / 前缀删除的缓存读写能力
// 使用者：cacheManager（核心缓存操作）、业务层缓存
type CacheDriver[T any] interface {
	// --- 单条缓存（key 由 ID 生成） ---
	Set(ctx context.Context, id uint64, data *T, duration time.Duration) error
	Get(ctx context.Context, id uint64) (*T, error)
	Del(ctx context.Context, id uint64) error
	DelByIDs(ctx context.Context, ids []uint64) error

	// --- 自定义 Key 缓存（存 ID、ID 列表） ---
	SetIDByKey(ctx context.Context, key string, id uint64, duration time.Duration) error
	GetIDByKey(ctx context.Context, key string) (id uint64, err error)
	SetIDsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error
	GetIDsByKey(ctx context.Context, key string) (ids []uint64, err error)
	DelByKey(ctx context.Context, key string) error

	// --- 批量缓存 ---
	MultiGet(ctx context.Context, ids []uint64) (map[uint64]*T, error)
	MultiSet(ctx context.Context, data []*T, duration time.Duration) error

	// --- 前缀批量删除（SCAN 遍历） ---
	DelByPrefix(ctx context.Context, prefix string) error
}

// PlaceholderDriver 占位符驱动接口（防缓存穿透）
// 职责：为不存在的 ID / Key 设置占位符，避免恶意查询直接打到数据库
// 使用者：cacheManager（查询不存在的记录时设置占位符）
type PlaceholderDriver interface {
	// SetPlaceholder 为不存在的 id 设置占位符
	SetPlaceholder(ctx context.Context, id uint64) error

	// SetPlaceholderByKey 为自定义 key 设置占位符
	SetPlaceholderByKey(ctx context.Context, key string) error

	// IsPlaceholderErr 判断错误是否为占位符错误
	IsPlaceholderErr(err error) bool
}

// Cache 泛型缓存接口，由 LockDriver + CacheDriver[T] + PlaceholderDriver 组合而成
//
// 设计说明：
//   - 将单一巨型接口拆分为三个职责明确的小接口（ISP 原则）
//   - Cache[T] 作为组合接口保持向后兼容，现有业务代码无需修改
//   - cacheManager 内部可按需依赖具体子接口，降低耦合
//
// 泛型参数 T 是业务模型类型（如 User、Order）
type Cache[T any] interface {
	LockDriver
	CacheDriver[T]
	PlaceholderDriver
}
