// Package dao 提供泛型数据访问层基础组件
package dao

import (
	"context"
	"time"

	"github.com/go-redsync/redsync/v4"
)

// Cache 泛型缓存接口，定义了业务缓存层所需的所有操作
// 由各业务表的缓存适配器实现，底层委托给具体的缓存实现（如 Redis）
type Cache[T any] interface {
	// GetLoopLock 获取一个阻塞式的分布式锁（循环等待直到获取锁或上下文取消）
	// 适用于需要等待锁释放、且任务较短（无需续期）的场景
	GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)

	// GetLock 获取一个非阻塞的分布式锁（尝试一次，失败立即返回错误）
	// 适用于需要快速失败、且任务较短（无需续期）的场景
	GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)

	// WatchDogLock 获取一个带看门狗自动续期的分布式锁（非阻塞）
	// 适用于需要执行长任务且不能等待锁（锁被占用时立即返回错误）的场景
	// 任务执行期间，锁会自动续期，续期失败则会取消任务上下文
	WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error

	// WatchDogLoopLock 获取一个带看门狗自动续期的分布式锁（阻塞式）
	// 适用于需要执行长任务且愿意等待锁释放的场景
	// 任务执行期间，锁会自动续期，续期失败则会取消任务上下文
	WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error

	// Set 写入单个对象的缓存，key 由 id 生成
	Set(ctx context.Context, id uint64, data *T, duration time.Duration) error

	// Get 根据 id 获取单个对象缓存
	Get(ctx context.Context, id uint64) (*T, error)

	// Del 根据 id 删除单个对象缓存
	Del(ctx context.Context, id uint64) error

	// SetIDByKey 按自定义 key 缓存一个 uint64 类型的 ID
	SetIDByKey(ctx context.Context, key string, id uint64, duration time.Duration) error

	// GetIDByKey 根据自定义 key 获取缓存的 uint64 类型 ID
	GetIDByKey(ctx context.Context, key string) (id uint64, err error)

	// SetIDsByKey 按自定义 key 缓存一个 uint64 切片
	SetIDsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error

	// GetIDsByKey 根据自定义 key 获取缓存的 uint64 切片
	GetIDsByKey(ctx context.Context, key string) (ids []uint64, err error)

	// DelByKey 根据自定义 key 删除缓存
	DelByKey(ctx context.Context, key string) error

	// MultiGet 批量根据 id 列表获取对象缓存，返回 map[id]*T
	MultiGet(ctx context.Context, ids []uint64) (map[uint64]*T, error)

	// MultiSet 批量设置对象缓存，key 由每个对象的 ID 生成
	MultiSet(ctx context.Context, data []*T, duration time.Duration) error

	// DelByPrefix 根据前缀批量删除缓存（通过 SCAN 遍历）
	DelByPrefix(ctx context.Context, prefix string) error

	// SetPlaceholder 为不存在的 id 设置占位符，防止缓存穿透
	SetPlaceholder(ctx context.Context, id uint64) error

	// SetPlaceholderByKey 为自定义 key 设置占位符
	SetPlaceholderByKey(ctx context.Context, key string) error

	// IsPlaceholderErr 判断错误是否为占位符错误
	IsPlaceholderErr(err error) bool
}
