// Package dao 提供泛型数据访问层基础组件
package dao

import (
	"context"
	"time"

	"github.com/go-redsync/redsync/v4"
)

// Cache 泛型缓存接口，定义了业务缓存层所需的所有操作
// 由各业务表的缓存适配器实现，底层委托给具体的缓存实现（如 Redis）
//
// 这个接口是缓存框架的"契约"：上层（cacheManager）依赖这个接口，
// 下层（具体的 Redis 实现）实现这个接口。通过接口隔离，实现了依赖倒置。
//
// 泛型参数 T 是业务模型类型（如 User、Order）
type Cache[T any] interface {
	// ============================================================
	// 第一组：分布式锁（基于 Redis 实现，跨服务器互斥）
	// ============================================================

	// GetLoopLock 获取一个阻塞式的分布式锁（循环等待直到获取锁或上下文取消）
	// 适用于需要等待锁释放、且任务较短（无需续期）的场景
	//
	// 大白话：如果锁被别人占着，我就一直等着（循环尝试），直到拿到锁或超时。
	// 适用场景：缓存刷新任务（锁持有时间很短，几十毫秒，不需要续期）
	//
	// 参数：
	//   - ctx：上下文，用于传递超时和取消信号
	//   - key：锁的唯一标识（如 "lock:refresh:123"）
	//   - options：redsync 的可选配置（如重试策略、过期时间等）
	// 返回：
	//   - *redsync.Mutex：锁对象，业务方需要手动调用 Unlock 释放
	//   - error：获取锁失败的错误
	GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)

	// GetLock 获取一个非阻塞的分布式锁（尝试一次，失败立即返回错误）
	// 适用于需要快速失败、且任务较短（无需续期）的场景
	//
	// 大白话：试着拿一下锁，拿到了就用，拿不到就立刻返回错误，不等待。
	// 适用场景：缓存双删中的锁竞争（拿不到就算了，等 50ms 再读缓存）
	//
	// 和 GetLoopLock 的区别：
	//   - GetLock：尝试一次，失败立即返回
	//   - GetLoopLock：循环尝试直到成功或上下文取消
	GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error)

	// WatchDogLock 获取一个带看门狗自动续期的分布式锁（非阻塞）
	// 适用于需要执行长任务且不能等待锁（锁被占用时立即返回错误）的场景
	// 任务执行期间，锁会自动续期，续期失败则会取消任务上下文
	//
	// 大白话：我要干一个可能需要很长时间的活（比如批量导入数据），
	// 拿锁时只试一次，拿到了就干，干的过程中锁会自动续期防止过期。
	// 如果续期失败了，说明锁被抢走了，任务会被取消。
	//
	// 和 GetLock 的区别：
	//   - GetLock：手动管理锁的生命周期（拿锁→干活→解锁）
	//   - WatchDogLock：自动续期 + 自动解锁，适合长任务
	WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error

	// WatchDogLoopLock 获取一个带看门狗自动续期的分布式锁（阻塞式）
	// 适用于需要执行长任务且愿意等待锁释放的场景
	// 任务执行期间，锁会自动续期，续期失败则会取消任务上下文
	//
	// 大白话：和 WatchDogLock 一样，但等锁时是阻塞的（一直在那等直到拿到锁）。
	// 适合那种"这个长任务很重要，我必须执行，等多久都行"的场景。
	//
	// 和 WatchDogLock 的区别：
	//   - WatchDogLock：非阻塞，拿不到立即返回错误
	//   - WatchDogLoopLock：阻塞，一直等到拿到锁为止
	WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error

	// ============================================================
	// 第二组：单条缓存读写（key 由 ID 生成）
	// ============================================================

	// Set 写入单个对象的缓存，key 由 id 生成
	//
	// 大白话：把一条数据存进缓存，key 就是 ID（如 "123" -> 用户对象）
	// 底层存储格式：data:Users:123 -> JSON(用户数据)
	Set(ctx context.Context, id uint64, data *T, duration time.Duration) error

	// Get 根据 id 获取单个对象缓存
	//
	// 大白话：通过 ID 从缓存里把对象取出来
	// 返回：
	//   - *T：取到的对象
	//   - error：如果 key 不存在返回 database.ErrCacheNotFound
	Get(ctx context.Context, id uint64) (*T, error)

	// Del 根据 id 删除单个对象缓存
	//
	// 大白话：从缓存里删掉 ID 对应的那条数据
	// 场景：更新或删除数据后，立即删缓存
	Del(ctx context.Context, id uint64) error

	// ============================================================
	// 第三组：自定义 Key 缓存（存 ID、ID 列表、任意值）
	// ============================================================

	// SetIDByKey 按自定义 key 缓存一个 uint64 类型的 ID
	//
	// 大白话：缓存一个 key -> ID 的映射
	// 场景：条件查询缓存（如 "condition:abc" -> 123）
	SetIDByKey(ctx context.Context, key string, id uint64, duration time.Duration) error

	// GetIDByKey 根据自定义 key 获取缓存的 uint64 类型 ID
	//
	// 大白话：通过自定义 key 取出缓存的 ID
	// 场景：条件查询命中时，取出对应的记录 ID
	GetIDByKey(ctx context.Context, key string) (id uint64, err error)

	// SetIDsByKey 按自定义 key 缓存一个 uint64 切片
	//
	// 大白话：缓存一个 key -> ID列表 的映射
	// 场景：条件查询缓存（如 "condition:abc" -> [1,2,3,4,5]）
	SetIDsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error

	// GetIDsByKey 根据自定义 key 获取缓存的 uint64 切片
	//
	// 大白话：通过自定义 key 取出缓存的 ID 列表
	GetIDsByKey(ctx context.Context, key string) (ids []uint64, err error)

	// DelByKey 根据自定义 key 删除缓存
	//
	// 大白话：精确删除某个自定义 key
	// 场景：清理单个条件缓存（如 "condition:abc"）
	DelByKey(ctx context.Context, key string) error

	// ============================================================
	// 第四组：批量缓存读写
	// ============================================================

	// MultiGet 批量根据 id 列表获取对象缓存，返回 map[id]*T
	//
	// 大白话：一次传入多个 ID，一次性把对应的对象全取出来
	// 场景：分页查询时，拿到当前页的 ID 列表，批量去取对象详情
	//
	// 底层实现：使用 Redis 的 MGET 命令，一次网络往返取多个 key
	MultiGet(ctx context.Context, ids []uint64) (map[uint64]*T, error)

	// MultiSet 批量设置对象缓存，key 由每个对象的 ID 生成
	//
	// 大白话：一次传入多个对象，批量写入缓存
	// 场景：批量查询数据库后，一次性回填缓存
	//
	// 底层实现：使用 Redis 的 MSET 或 Pipeline，一次网络往返写多个 key
	MultiSet(ctx context.Context, data []*T, duration time.Duration) error

	// ============================================================
	// 第五组：按前缀批量删除（SCAN 遍历）
	// ============================================================

	// DelByPrefix 根据前缀批量删除缓存（通过 SCAN 遍历）
	//
	// 大白话：把某个前缀下的所有缓存全部删掉
	// 场景：数据变更后，清除所有条件查询缓存（如 "data:Users:condition:*"）
	//
	// 底层实现：使用 Redis 的 SCAN 命令迭代查找，再用 DEL 批量删除
	// 注意：不能用 KEYS 命令（会阻塞 Redis），必须用 SCAN
	DelByPrefix(ctx context.Context, prefix string) error

	// ============================================================
	// 第六组：占位符（防缓存穿透）
	// ============================================================

	// SetPlaceholder 为不存在的 id 设置占位符，防止缓存穿透
	//
	// 大白话：在缓存里标记"这个 ID 在数据库里不存在"
	// 场景：用户查询一个不存在的 ID（如 id=99999），设置占位符后下次再查直接返回"不存在"
	//
	// 占位符值：config.PlaceholderValue（默认 "*"）
	// 过期时间：config.DefaultNotFoundExpireTime（默认 1 分钟）
	SetPlaceholder(ctx context.Context, id uint64) error

	// SetPlaceholderByKey 为自定义 key 设置占位符
	//
	// 大白话：和 SetPlaceholder 一样，但 key 是自定义的
	// 场景：条件查询结果为空时，给这个条件设置占位符
	SetPlaceholderByKey(ctx context.Context, key string) error

	// IsPlaceholderErr 判断错误是否为占位符错误
	//
	// 大白话：检查缓存返回的错误是不是"占位符"错误
	// 场景：Get 或 GetIDByKey 返回错误后，判断是否为占位符，如果是就返回"记录不存在"
	//
	// 实现方式：检查错误信息是否包含 config.PlaceholderValue（"*"）
	IsPlaceholderErr(err error) bool
}
