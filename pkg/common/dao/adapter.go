// Package dao 提供泛型数据访问层
package dao

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/go-redsync/redsync/v4"

	"github.com/18721889353/sunshine/pkg/cache"
	"github.com/18721889353/sunshine/pkg/utils"
)

// CacheAdapter 将通用缓存适配为泛型缓存接口。
// 业务层可以通过嵌入此适配器获得所有缓存能力，无需手写透传方法。
type CacheAdapter[T any] struct {
	driver  cache.Cache        // 通用缓存驱动
	prefix  string             // 业务前缀（已自动确保以冒号结尾）
	newFunc func() interface{} // 工厂函数，用于反序列化
}

// NewCacheAdapter 创建泛型缓存适配器。
// 将通用的 cache.Cache 适配为 dao.Cache[T]。
func NewCacheAdapter[T any](driver cache.Cache, prefix string, newFunc func() interface{}) *CacheAdapter[T] {
	// 自动确保前缀以冒号结尾
	if prefix != "" && !strings.HasSuffix(prefix, ":") {
		prefix = prefix + ":"
	}
	return &CacheAdapter[T]{
		driver:  driver,
		prefix:  prefix,
		newFunc: newFunc,
	}
}

// buildKey 根据 id 构建缓存 key。
func (a *CacheAdapter[T]) buildKey(id uint64) string {
	return a.prefix + utils.Uint64ToStr(id)
}

// buildKeyString 根据字符串 key 构建缓存 key。
func (a *CacheAdapter[T]) buildKeyString(key string) string {
	return a.prefix + key
}

// buildLockKey 根据业务 key 构建分布式锁 key。
func (a *CacheAdapter[T]) buildLockKey(key string) string {
	return "lock:" + a.prefix + key
}

// ----- 分布式锁实现 -----

// GetLoopLock 获取一个阻塞式的分布式锁（循环等待直到获取锁或上下文取消）。
func (a *CacheAdapter[T]) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	return a.driver.GetLoopLock(ctx, a.buildLockKey(key), options...)
}

// GetLock 获取一个非阻塞的分布式锁（尝试一次，失败立即返回错误）。
func (a *CacheAdapter[T]) GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	return a.driver.GetLock(ctx, a.buildLockKey(key), options...)
}

// WatchDogLock 获取一个带看门狗自动续期的分布式锁（非阻塞）。
func (a *CacheAdapter[T]) WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	return a.driver.WatchDogLock(ctx, a.buildLockKey(key), expiry, task, options...)
}

// WatchDogLoopLock 获取一个带看门狗自动续期的分布式锁（阻塞式）。
func (a *CacheAdapter[T]) WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	return a.driver.WatchDogLoopLock(ctx, a.buildLockKey(key), expiry, task, options...)
}

// ----- 单条缓存实现 -----

// Set 写入单个对象的缓存，key 由 id 生成。
func (a *CacheAdapter[T]) Set(ctx context.Context, id uint64, data *T, duration time.Duration) error {
	if data == nil || id == 0 {
		return nil
	}
	return a.driver.Set(ctx, a.buildKey(id), data, duration)
}

// Get 根据 id 获取单个对象缓存。
func (a *CacheAdapter[T]) Get(ctx context.Context, id uint64) (*T, error) {
	var result interface{} = a.newFunc()
	err := a.driver.Get(ctx, a.buildKey(id), &result)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	typed, ok := result.(*T)
	if !ok {
		return nil, errors.New("cache: type assertion failed")
	}
	return typed, nil
}

// Del 根据 id 删除单个对象缓存。
func (a *CacheAdapter[T]) Del(ctx context.Context, id uint64) error {
	return a.driver.Del(ctx, a.buildKey(id))
}

// ----- 自定义 Key 缓存实现 -----

// SetIDByKey 按自定义 key 缓存一个 uint64 类型的 ID。
func (a *CacheAdapter[T]) SetIDByKey(ctx context.Context, key string, id uint64, duration time.Duration) error {
	if key == "" || id == 0 {
		return nil
	}
	// 传递指针类型，因为 encoding.Marshal 要求指针
	return a.driver.Set(ctx, a.buildKeyString(key), &id, duration)
}

// GetIDByKey 根据自定义 key 获取缓存的 uint64 类型 ID。
func (a *CacheAdapter[T]) GetIDByKey(ctx context.Context, key string) (uint64, error) {
	var id uint64
	err := a.driver.Get(ctx, a.buildKeyString(key), &id)
	return id, err
}

// SetIDsByKey 按自定义 key 缓存一个 uint64 切片。
func (a *CacheAdapter[T]) SetIDsByKey(ctx context.Context, key string, ids []uint64, duration time.Duration) error {
	if key == "" || len(ids) == 0 {
		return nil
	}
	// 传递指针类型，因为 encoding.Marshal 要求指针
	return a.driver.Set(ctx, a.buildKeyString(key), &ids, duration)
}

// GetIDsByKey 根据自定义 key 获取缓存的 uint64 切片。
func (a *CacheAdapter[T]) GetIDsByKey(ctx context.Context, key string) ([]uint64, error) {
	var ids []uint64
	err := a.driver.Get(ctx, a.buildKeyString(key), &ids)
	return ids, err
}

// DelByKey 根据自定义 key 删除缓存。
func (a *CacheAdapter[T]) DelByKey(ctx context.Context, key string) error {
	return a.driver.Del(ctx, a.buildKeyString(key))
}

// ----- 批量操作实现 -----

// MultiSet 批量设置对象缓存，key 由每个对象的 ID 生成。
func (a *CacheAdapter[T]) MultiSet(ctx context.Context, data []*T, duration time.Duration) error {
	if len(data) == 0 {
		return nil
	}
	valMap := make(map[string]interface{})
	for _, v := range data {
		id := a.extractID(v)
		if id == 0 {
			continue
		}
		valMap[a.buildKey(id)] = v
	}
	return a.driver.MultiSet(ctx, valMap, duration)
}

// MultiGet 批量根据 id 列表获取对象缓存，返回 map[id]*T。
func (a *CacheAdapter[T]) MultiGet(ctx context.Context, ids []uint64) (map[uint64]*T, error) {
	if len(ids) == 0 {
		return make(map[uint64]*T), nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = a.buildKey(id)
	}
	rawMap := make(map[string]interface{})
	err := a.driver.MultiGet(ctx, keys, rawMap)
	if err != nil {
		return nil, err
	}
	result := make(map[uint64]*T)
	for _, id := range ids {
		key := a.buildKey(id)
		if val, ok := rawMap[key]; ok {
			if typed, ok := val.(*T); ok {
				result[id] = typed
			}
		}
	}
	return result, nil
}

// ----- 前缀删除 -----

// DelByPrefix 根据前缀批量删除缓存（通过 SCAN 遍历）。
func (a *CacheAdapter[T]) DelByPrefix(ctx context.Context, prefix string) error {
	return a.driver.DelByPrefix(ctx, prefix)
}

// ----- 占位符实现 -----

// SetPlaceholder 为不存在的 id 设置占位符，防止缓存穿透。
func (a *CacheAdapter[T]) SetPlaceholder(ctx context.Context, id uint64) error {
	return a.driver.SetCacheWithNotFound(ctx, a.buildKey(id))
}

// SetPlaceholderByKey 为自定义 key 设置占位符。
func (a *CacheAdapter[T]) SetPlaceholderByKey(ctx context.Context, key string) error {
	return a.driver.SetCacheWithNotFound(ctx, a.buildKeyString(key))
}

// IsPlaceholderErr 判断错误是否为占位符错误。
func (a *CacheAdapter[T]) IsPlaceholderErr(err error) bool {
	return errors.Is(err, cache.ErrPlaceholder)
}

// extractID 从对象中提取 ID 字段（必须为 uint64）。
func (a *CacheAdapter[T]) extractID(obj *T) uint64 {
	if obj == nil {
		return 0
	}
	v := reflect.ValueOf(obj).Elem()
	field := v.FieldByName("ID")
	if !field.IsValid() {
		return 0
	}
	if field.Kind() == reflect.Uint64 {
		return field.Uint()
	}
	return 0
}
