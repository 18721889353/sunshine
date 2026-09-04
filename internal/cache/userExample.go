// Package cache 缓存层
// 提供 Redis 和本地缓存的统一接口，支持分布式锁、防击穿、防穿透等功能
package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/go-redsync/redsync/v4"

	"github.com/18721889353/sunshine/internal/consts"

	"github.com/18721889353/sunshine/pkg/cache"
	"github.com/18721889353/sunshine/pkg/encoding"
	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/model"
)

const (
	// UserExampleCachePrefixKeyLock 分布式锁的 Redis key 前缀，必须以冒号结尾
	UserExampleCachePrefixKeyLock = "lock:userExample:"
	// UserExampleCachePrefixKey 业务缓存数据的 Redis key 前缀，必须以冒号结尾
	UserExampleCachePrefixKey = "data:userExample:"
	// UserExampleExpireTime 缓存数据的默认过期时间
	UserExampleExpireTime = 30 * time.Minute
)

// 确保 userExampleCache 实现了 UserExampleCache 接口
var _ UserExampleCache = (*userExampleCache)(nil)

// UserExampleCache 定义了针对 UserExample 模型的缓存操作接口
// 包含分布式锁、看门狗自动续期、缓存读写、批量操作、占位符等功能
type UserExampleCache interface {
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
	Set(ctx context.Context, id uint64, data *model.UserExample, duration time.Duration) error

	// SetIDByKey 按自定义 key 缓存一个 uint64 类型的 ID
	SetIDByKey(ctx context.Context, key string, id uint64, duration time.Duration) error

	// SetIDsByKey 按自定义 key 缓存一个 uint64 切片
	SetIDsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error

	// Get 根据 id 获取单个对象缓存
	Get(ctx context.Context, id uint64) (*model.UserExample, error)

	// GetIDByKey 根据自定义 key 获取缓存的 uint64 类型 ID
	GetIDByKey(ctx context.Context, key string) (id uint64, err error)

	// GetIDsByKey 根据自定义 key 获取缓存的 uint64 切片
	GetIDsByKey(ctx context.Context, key string) (ids []uint64, err error)

	// MultiGet 批量根据 id 列表获取对象缓存，返回 map[id]*UserExample
	MultiGet(ctx context.Context, ids []uint64) (map[uint64]*model.UserExample, error)

	// MultiSet 批量设置对象缓存，key 由每个对象的 ID 生成
	MultiSet(ctx context.Context, data []*model.UserExample, duration time.Duration) error

	// Del 根据 id 删除单个对象缓存
	Del(ctx context.Context, id uint64) error

	// DelByPrefix 根据前缀批量删除缓存（通过 SCAN 遍历）
	DelByPrefix(ctx context.Context, prefix string) error

	// DelByKey 根据自定义 key 删除缓存
	DelByKey(ctx context.Context, key string) error

	// SetPlaceholder 为不存在的 id 设置占位符，防止缓存穿透
	SetPlaceholder(ctx context.Context, id uint64) error

	// SetPlaceholderByKey 为自定义 key 设置占位符
	SetPlaceholderByKey(ctx context.Context, key string) error

	// IsPlaceholderErr 判断错误是否为占位符错误
	IsPlaceholderErr(err error) bool
}

// userExampleCache 是 UserExampleCache 接口的实现
type userExampleCache struct {
	cache cache.Cache // 底层缓存驱动（如 Redis）
}

// NewUserExampleCache 创建一个新的 UserExampleCache 实例
// 如果缓存类型为 Redis，则初始化 Redis 缓存驱动；否则返回 nil
// 参数 cacheType 包含了 Redis 客户端和缓存类型信息
func NewUserExampleCache(cacheType *database.CacheType) UserExampleCache {
	jsonEncoding := encoding.JSONEncoding{}
	cachePrefix := ""

	cType := strings.ToLower(cacheType.CType)
	if cType == consts.CacheTypeRedis {
		c := cache.NewRedisCache(cacheType.Rdb, cachePrefix, jsonEncoding, func() interface{} {
			return &model.UserExample{}
		})
		return &userExampleCache{cache: c}
	}

	return nil // 无缓存
}

// GetUserExampleCacheKey 根据 id 生成完整的缓存 key（包含前缀）
func (c *userExampleCache) GetUserExampleCacheKey(id uint64) string {
	return UserExampleCachePrefixKey + utils.Uint64ToStr(id)
}

// GetUserExampleCacheKeyString 根据字符串 key 生成完整的缓存 key（包含前缀）
func (c *userExampleCache) GetUserExampleCacheKeyString(key string) string {
	return UserExampleCachePrefixKey + key
}

// getLockCacheKey 生成分布式锁的完整 key（包含锁前缀）
func (c *userExampleCache) getLockCacheKey(key string) string {
	return fmt.Sprintf("%s%v", UserExampleCachePrefixKeyLock, key)
}

// GetLoopLock 获取阻塞式分布式锁（透传到底层缓存驱动）
func (c *userExampleCache) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	cacheKey := c.getLockCacheKey(key)
	return c.cache.GetLoopLock(ctx, cacheKey, options...)
}

// GetLock 获取非阻塞式分布式锁（透传到底层缓存驱动）
func (c *userExampleCache) GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	cacheKey := c.getLockCacheKey(key)
	return c.cache.GetLock(ctx, cacheKey, options...)
}

// WatchDogLock 获取带看门狗的非阻塞式分布式锁
// 1. 调用 GetLock 尝试获取锁，若失败则直接返回错误
// 2. 启动一个看门狗协程，定时（expiry/3）续期锁，续期失败则取消任务上下文
// 3. 执行用户提供的 task 函数（使用 watchDogCtx），task 应监听 ctx.Done() 以处理取消
// 4. 无论 task 成功或失败，都会释放锁并停止看门狗
// 参数 expiry 为锁的过期时间和续期周期基准（实际续期间隔为 expiry/3）
func (c *userExampleCache) WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	// 1. 获取普通锁
	lock, err := c.GetLock(ctx, key, options...)
	if err != nil {
		return err
	}

	// 2. 获取当前的过期时间，用于计算续期频率
	if expiry <= 0 {
		expiry = 10 * time.Second // 默认兜底
	}
	// --- 看门狗实现开始 ---
	// 3. 启动看门狗协程
	watchdogCtx, stopWatchdog := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(expiry / 3) // 建议三分之一时间续期一次，更安全
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 使用原始 ctx 确保续期动作本身不被 task 的取消所影响
				ok, err := lock.ExtendContext(ctx)
				if err != nil || !ok {
					// 关键点：续期失败，立即取消任务 context
					if err != nil {
						logger.WarnWithCtx(ctx, "看门狗续期异常", logger.Err(err), logger.String("key", key))
					} else {
						logger.WarnWithCtx(ctx, "看门狗续期失败：锁已过期", logger.String("key", key))
					}
					stopWatchdog() // 续期失败，通知业务中断
					return
				}
			case <-watchdogCtx.Done(): // 业务执行完或被取消，看门狗退出
				return
			}
		}
	}()
	// --- 看门狗实现结束 ---
	// 4. 执行业务逻辑并在结束时释放所有资源
	defer func() {
		stopWatchdog() // 确保退出时关闭协程
		if _, releaseErr := lock.UnlockContext(ctx); releaseErr != nil {
			if !strings.Contains(releaseErr.Error(), "lock was already expired") {
				logger.WarnWithCtx(ctx, "释放分布式锁失败", logger.Err(releaseErr))
			}
		}
	}()
	if task == nil {
		logger.WarnWithCtx(ctx, "WatchDogLock: task 参数为 nil", logger.String("key", key))
		return errors.New("task function cannot be nil")
	}
	return task(watchdogCtx)
}

// WatchDogLoopLock 获取带看门狗的阻塞式分布式锁
// 与 WatchDogLock 的区别在于：它会阻塞等待锁释放（调用 GetLoopLock），而非快速失败
// 其他行为（续期、任务执行、释放）与 WatchDogLock 相同
func (c *userExampleCache) WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	// 1. 获取循环锁 (复用已有的 GetLoopLock 逻辑)
	lock, err := c.GetLoopLock(ctx, key, options...)
	if err != nil {
		return err
	}

	// 2. 获取当前的过期时间，用于计算续期频率
	if expiry <= 0 {
		expiry = 10 * time.Second // 默认兜底
	}
	// --- 看门狗实现开始 ---
	// 3. 启动看门狗协程
	watchdogCtx, stopWatchdog := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(expiry / 3) // 建议三分之一时间续期一次，更安全
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 使用原始 ctx 确保续期动作本身不被 task 的取消所影响
				ok, err := lock.ExtendContext(ctx)
				if err != nil || !ok {
					// 关键点：续期失败，立即取消任务 context
					if err != nil {
						logger.WarnWithCtx(ctx, "看门狗续期异常", logger.Err(err), logger.String("key", key))
					} else {
						logger.WarnWithCtx(ctx, "看门狗续期失败：锁已过期", logger.String("key", key))
					}
					stopWatchdog() // 续期失败，通知业务中断
					return
				}
			case <-watchdogCtx.Done(): // 业务执行完或被取消，看门狗退出
				return
			}
		}
	}()
	// --- 看门狗实现结束 ---
	// 4. 执行业务逻辑并在结束时释放所有资源
	defer func() {
		stopWatchdog() // 确保退出时关闭协程
		if _, releaseErr := lock.UnlockContext(ctx); releaseErr != nil {
			if !strings.Contains(releaseErr.Error(), "lock was already expired") {
				logger.WarnWithCtx(ctx, "释放分布式锁失败", logger.Err(releaseErr))
			}
		}
	}()
	if task == nil {
		logger.WarnWithCtx(ctx, "WatchDogLoopLock: task 参数为 nil", logger.String("key", key))
		return errors.New("task function cannot be nil")
	}
	return task(watchdogCtx)
}

// Set 写入单个对象的缓存
// 如果 data 为 nil 或 id 为 0，则直接返回 nil（不操作）
// 使用 id 生成缓存 key，并调用底层 Set 方法存储序列化后的数据
func (c *userExampleCache) Set(ctx context.Context, id uint64, data *model.UserExample, duration time.Duration) error {
	if data == nil || id == 0 {
		return nil
	}
	cacheKey := c.GetUserExampleCacheKey(id)
	err := c.cache.Set(ctx, cacheKey, data, duration)
	if err != nil {
		return err
	}
	return nil
}

// SetIDByKey 按自定义 key 缓存一个 uint64 类型的 ID
// key 不能为空，id 不能为 0
func (c *userExampleCache) SetIDByKey(ctx context.Context, key string, id uint64, duration time.Duration) error {
	if key == "" || id == 0 {
		return nil
	}
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err := c.cache.Set(ctx, cacheKey, &id, duration)
	if err != nil {
		return err
	}
	return nil
}

// SetIDsByKey 按自定义 key 缓存一个 uint64 切片
// key 不能为空，ids 不能为 nil
func (c *userExampleCache) SetIDsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error {
	if key == "" || ids == nil {
		return nil
	}
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err := c.cache.Set(ctx, cacheKey, &ids, duration)
	if err != nil {
		return err
	}
	return nil
}

// Get 根据 id 获取单个对象缓存
// 如果缓存不存在或发生错误，则返回错误（具体错误由底层驱动提供）
func (c *userExampleCache) Get(ctx context.Context, id uint64) (*model.UserExample, error) {
	var data *model.UserExample
	cacheKey := c.GetUserExampleCacheKey(id)
	err := c.cache.Get(ctx, cacheKey, &data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// GetIDByKey 根据自定义 key 获取缓存的 uint64 类型 ID
// 如果缓存不存在，返回 0 和错误
func (c *userExampleCache) GetIDByKey(ctx context.Context, key string) (id uint64, err error) {
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err = c.cache.Get(ctx, cacheKey, &id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// GetIDsByKey 根据自定义 key 获取缓存的 uint64 切片
// 如果缓存不存在，返回 nil 和错误
func (c *userExampleCache) GetIDsByKey(ctx context.Context, key string) (ids []uint64, err error) {
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err = c.cache.Get(ctx, cacheKey, &ids)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// MultiSet 批量设置对象缓存
// 遍历 data，为每个对象生成 key，然后调用底层 MultiSet 一次写入多个 key
func (c *userExampleCache) MultiSet(ctx context.Context, data []*model.UserExample, duration time.Duration) error {
	valMap := make(map[string]interface{})
	for _, v := range data {
		cacheKey := c.GetUserExampleCacheKey(v.ID)
		valMap[cacheKey] = v
	}

	err := c.cache.MultiSet(ctx, valMap, duration)
	if err != nil {
		return err
	}

	return nil
}

// MultiGet 批量根据 id 列表获取对象缓存
// 返回 map[id]*UserExample，只包含成功获取的项
// 如果某个 id 缓存缺失，则不会出现在返回 map 中
func (c *userExampleCache) MultiGet(ctx context.Context, ids []uint64) (map[uint64]*model.UserExample, error) {
	var keys []string
	for _, v := range ids {
		cacheKey := c.GetUserExampleCacheKey(v)
		keys = append(keys, cacheKey)
	}

	itemMap := make(map[string]*model.UserExample)
	err := c.cache.MultiGet(ctx, keys, itemMap)
	if err != nil {
		return nil, err
	}

	retMap := make(map[uint64]*model.UserExample)
	for _, id := range ids {
		val, ok := itemMap[c.GetUserExampleCacheKey(id)]
		if ok {
			retMap[id] = val
		}
	}

	return retMap, nil
}

// Del 根据 id 删除单个对象缓存
func (c *userExampleCache) Del(ctx context.Context, id uint64) error {
	cacheKey := c.GetUserExampleCacheKey(id)
	err := c.cache.Del(ctx, cacheKey)
	if err != nil {
		return err
	}
	return nil
}

// DelByPrefix 根据前缀批量删除缓存
// 底层使用 SCAN 命令遍历匹配的 key，然后批量删除
func (c *userExampleCache) DelByPrefix(ctx context.Context, prefix string) error {
	err := c.cache.DelByPrefix(ctx, prefix)
	if err != nil {
		return err
	}
	return nil
}

// DelByKey 根据自定义 key 删除缓存
func (c *userExampleCache) DelByKey(ctx context.Context, key string) error {
	cacheKey := c.GetUserExampleCacheKeyString(key)
	err := c.cache.Del(ctx, cacheKey)
	if err != nil {
		return err
	}
	return nil
}

// SetPlaceholder 为不存在的 id 设置占位符，防止缓存穿透
// 占位符为一个特殊值，过期时间较短（由底层决定）
func (c *userExampleCache) SetPlaceholder(ctx context.Context, id uint64) error {
	cacheKey := c.GetUserExampleCacheKey(id)
	return c.cache.SetCacheWithNotFound(ctx, cacheKey)
}

// SetPlaceholderByKey 为自定义 key 设置占位符
func (c *userExampleCache) SetPlaceholderByKey(ctx context.Context, key string) error {
	cacheKey := c.GetUserExampleCacheKeyString(key)
	return c.cache.SetCacheWithNotFound(ctx, cacheKey)
}

// IsPlaceholderErr 判断错误是否为占位符错误
// 当缓存命中占位符时，底层 Get 方法会返回 cache.ErrPlaceholder
func (c *userExampleCache) IsPlaceholderErr(err error) bool {
	return errors.Is(err, cache.ErrPlaceholder)
}
