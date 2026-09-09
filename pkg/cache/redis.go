// Package cache 提供基于 Redis 的缓存驱动实现。
// 包含分布式锁（非阻塞/阻塞）、缓存读写、批量操作、批量删除、占位符以及操作日志等能力。
package cache

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/redis/go-redis/v9"

	"github.com/18721889353/sunshine/pkg/encoding"
	"github.com/18721889353/sunshine/pkg/logger"
)

// CacheNotFound 表示 Redis 中 key 不存在的错误（由 go-redis 返回的 redis.Nil）
var CacheNotFound = redis.Nil

// NewRedisCacheOption 用于配置 NewRedisCache 的可选参数（目前未实现具体选项）
type NewRedisCacheOption func(*redisCache)

// redisCache 是 Redis 缓存驱动结构，实现了 Cache 接口
type redisCache struct {
	client            *redis.Client      // Redis 客户端
	KeyPrefix         string             // 所有缓存 key 的统一前缀（通常为空，由上层业务层控制）
	encoding          encoding.Encoding  // 序列化/反序列化编码器（如 JSON）
	DefaultExpireTime time.Duration      // 默认过期时间（当 Set 传入 0 时使用）
	newObject         func() interface{} // 用于 MultiGet 反序列化时创建新对象的工厂函数
	redsSync          *redsync.Redsync   // 分布式锁同步器（redsync 实例）
}

// NewRedisCache 创建并返回一个新的 Redis 缓存驱动实例。
// client: Redis 客户端；keyPrefix: 全局 key 前缀（通常为空）；encode: 编码器；newObject: 对象工厂。
// 可选参数 _ 暂未使用。
func NewRedisCache(client *redis.Client, keyPrefix string, encode encoding.Encoding, newObject func() interface{}, _ ...NewRedisCacheOption) Cache {
	redisPool := goredis.NewPool(client) // 创建 Redis 连接池（供 redsync 使用）
	rs := redsync.New(redisPool)         // 创建 redsync 分布式锁同步器
	return &redisCache{
		client:            client,
		KeyPrefix:         keyPrefix,
		encoding:          encode,
		newObject:         newObject,
		redsSync:          rs,
		DefaultExpireTime: time.Second * 5, // 默认过期时间 5 秒
	}
}

// GetLoopLock 获取一个阻塞式的分布式锁（循环等待直到获取锁或上下文取消）。
// 底层调用 redsync.Mutex.LockContext，会按照 options 中的重试策略（如 WithTries、WithRetryDelay）不断重试。
// 若获取成功返回 *redsync.Mutex，否则返回错误。
func (c *redisCache) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	var err error
	start := time.Now()
	lockKey := c.buildLockKey(key) // 构建完整的锁 key
	defer func() {
		c.logOperation(ctx, "GetLoopLock", start, err, logger.String("lockKey", lockKey))
	}()
	mutex := c.redsSync.NewMutex(lockKey, options...)
	// 阻塞等待锁
	err = mutex.LockContext(ctx)
	if err != nil {
		return nil, err
	}
	return mutex, nil
}

// GetLock 获取一个非阻塞式的分布式锁（仅尝试一次，失败立即返回）。
// 底层调用 redsync.Mutex.TryLockContext，不会重试。
// 若锁已被占用，返回 error（通常是 redsync.ErrFailed）。
// 适用于需要快速失败的场景。
func (c *redisCache) GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	start := time.Now()
	var err error
	lockKey := c.buildLockKey(key)
	defer func() {
		c.logOperation(ctx, "GetLock", start, err, logger.String("lockKey", lockKey))
	}()

	mutex := c.redsSync.NewMutex(lockKey, options...)
	err = mutex.TryLockContext(ctx)
	if err != nil {
		return nil, err
	}
	return mutex, nil
}

// WatchDogLock 获取一个带看门狗自动续期的分布式锁（非阻塞）。
// 内部委托给 executeWithWatchdog，仅获取锁的方式为 GetLock（非阻塞）。
func (c *redisCache) WatchDogLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	lock, err := c.GetLock(ctx, key, options...)
	if err != nil {
		return err
	}
	return c.executeWithWatchdog(ctx, key, expiry, task, lock)
}

// WatchDogLoopLock 获取一个带看门狗自动续期的分布式锁（阻塞式）。
// 内部委托给 executeWithWatchdog，仅获取锁的方式为 GetLoopLock（阻塞等待）。
func (c *redisCache) WatchDogLoopLock(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, options ...redsync.Option) error {
	lock, err := c.GetLoopLock(ctx, key, options...)
	if err != nil {
		return err
	}
	return c.executeWithWatchdog(ctx, key, expiry, task, lock)
}

// executeWithWatchdog 看门狗的核心执行逻辑，供 WatchDogLock 和 WatchDogLoopLock 共用。
// 流程：启动看门狗 goroutine 定时续期 -> 执行 task -> 释放锁并停止看门狗。
func (c *redisCache) executeWithWatchdog(ctx context.Context, key string, expiry time.Duration, task func(ctx context.Context) error, lock *redsync.Mutex) error {
	if expiry <= 0 {
		expiry = 10 * time.Second // 默认兜底
	}
	if task == nil {
		logger.WarnWithCtx(ctx, "executeWithWatchdog: task 参数为 nil", logger.String("key", key))
		return errors.New("task function cannot be nil")
	}

	// 启动看门狗协程，每 expiry/3 时间续期一次
	watchdogCtx, stopWatchdog := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(expiry / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ok, extendErr := lock.ExtendContext(ctx)
				if extendErr != nil || !ok {
					if extendErr != nil {
						logger.WarnWithCtx(ctx, "看门狗续期异常", logger.Err(extendErr), logger.String("key", key))
					} else {
						logger.WarnWithCtx(ctx, "看门狗续期失败：锁已过期", logger.String("key", key))
					}
					stopWatchdog()
					return
				}
			case <-watchdogCtx.Done():
				return
			}
		}
	}()

	// 执行业务逻辑并在结束时释放所有资源
	defer func() {
		stopWatchdog()
		if _, releaseErr := lock.UnlockContext(ctx); releaseErr != nil {
			if !strings.Contains(releaseErr.Error(), "lock was already expired") {
				logger.WarnWithCtx(ctx, "释放分布式锁失败", logger.Err(releaseErr))
			}
		}
	}()
	return task(watchdogCtx)
}

// Set 将一个值序列化后写入 Redis，并设置过期时间。
// 如果 expireTime 为 0，则使用 DefaultExpireTime。
// key 为业务 key（不含前缀），内部会通过 BuildCacheKey 拼接前缀。
func (c *redisCache) Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) (err error) {
	start := time.Now()
	defer func() {
		c.logOperation(ctx, "Set", start, err, logger.String("key", key), logger.Any("expireTime", expireTime))
	}()

	buf, err := encoding.Marshal(c.encoding, val)
	if err != nil {
		return fmt.Errorf("encoding.Marshal error: %v, key=%s, val=%+v ", err, key, val)
	}

	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}
	if expireTime == 0 {
		expireTime = c.DefaultExpireTime
	}
	err = c.client.Set(ctx, cacheKey, buf, expireTime).Err()
	if err != nil {
		return fmt.Errorf("c.client.Set error: %v, cacheKey=%s", err, cacheKey)
	}
	return nil
}

// Get 从 Redis 获取指定 key 的值，并反序列化到 val 中。
// val 必须是指针。若 key 不存在，返回 redis.Nil（即 CacheNotFound）。
// 若存储的是空字符串，直接返回 nil（不反序列化）。
// 若存储的是占位符（NotFoundPlaceholder），返回 ErrPlaceholder。
func (c *redisCache) Get(ctx context.Context, key string, val interface{}) (err error) {
	start := time.Now()
	defer func() {
		c.logOperation(ctx, "Get", start, err, logger.String("key", key), logger.Any("val", val))
	}()

	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}

	bytes, err := c.client.Get(ctx, cacheKey).Bytes()
	if err != nil {
		return err // 包括 redis.Nil
	}

	if string(bytes) == "" {
		return nil
	}
	if string(bytes) == NotFoundPlaceholder {
		return ErrPlaceholder
	}
	err = encoding.Unmarshal(c.encoding, bytes, val)
	if err != nil {
		return fmt.Errorf("encoding.Unmarshal error: %v, key=%s, cacheKey=%s, type=%v, json=%+v ",
			err, key, cacheKey, reflect.TypeOf(val), string(bytes))
	}
	return nil
}

// MultiSet 批量设置多个 key-value 对到 Redis，使用 Pipeline 提高性能。
// valueMap 的 key 为业务 key（不含前缀），value 为要存储的值。
// 如果某个值序列化失败，会跳过该 key 并记录错误，最后返回聚合错误（如有）。
func (c *redisCache) MultiSet(ctx context.Context, valueMap map[string]interface{}, expireTime time.Duration) (err error) {
	start := time.Now()
	defer func() {
		c.logOperation(ctx, "MultiSet", start, err, logger.Any("expireTime", expireTime), logger.Any("valueMap", valueMap))
	}()

	if len(valueMap) == 0 {
		return nil
	}
	if expireTime == 0 {
		expireTime = c.DefaultExpireTime
	}

	pipeline := c.client.Pipeline()
	var errs []error
	validKeys := 0

	for key, value := range valueMap {
		buf, cacheErr := encoding.Marshal(c.encoding, value)
		if cacheErr != nil {
			errs = append(errs, fmt.Errorf("marshal key %s failed: %w", key, cacheErr))
			continue
		}
		cacheKey, buildErr := BuildCacheKey(c.KeyPrefix, key)
		if buildErr != nil {
			errs = append(errs, fmt.Errorf("build key %s failed: %w", key, buildErr))
			continue
		}
		pipeline.Set(ctx, cacheKey, buf, expireTime)
		validKeys++
	}

	if validKeys == 0 {
		if len(errs) > 0 {
			return fmt.Errorf("all keys failed: %v", errs)
		}
		return nil
	}

	_, execErr := pipeline.Exec(ctx)
	if execErr != nil {
		return fmt.Errorf("pipeline.Exec error: %w (partial failures: %v)", execErr, errs)
	}

	if len(errs) > 0 {
		logger.WarnWithCtx(ctx, "MultiSet partial failures", logger.Any("errors", errs))
		return fmt.Errorf("partial failures: %v", errs)
	}
	return nil
}

// MultiGet 批量获取多个 key 的值，并反序列化到 value（必须是 map[string]interface{} 或可赋值的 map 类型）。
// 传入的 keys 为业务 key（不含前缀），内部会拼接前缀。
// 返回的 value 会被填充，缺失的 key 不会出现在 map 中。
// 如果某个 key 对应的值为占位符，则解组会失败并跳过（不填充该 key）。
func (c *redisCache) MultiGet(ctx context.Context, keys []string, value interface{}) (err error) {
	start := time.Now()
	defer func() {
		c.logOperation(ctx, "MultiGet", start, err, logger.Any("keys", keys), logger.Any("value", value))
	}()

	if len(keys) == 0 {
		return nil
	}
	cacheKeys := make([]string, len(keys))
	for index, key := range keys {
		cacheKey, getErr := BuildCacheKey(c.KeyPrefix, key)
		if getErr != nil {
			return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
		}
		cacheKeys[index] = cacheKey
	}
	values, err := c.client.MGet(ctx, cacheKeys...).Result()
	if err != nil {
		if errors.Is(err, CacheNotFound) {
			return err
		}
		return fmt.Errorf("c.client.MGet error: %v, keys=%+v", err, cacheKeys)
	}

	// 情况1：value 是 map[string]interface{}（常用）
	if m, ok := value.(map[string]interface{}); ok {
		for i, v := range values {
			if v == nil {
				continue
			}
			strVal, ok := v.(string)
			if !ok {
				logger.WarnWithCtx(ctx, "unexpected value type in cache", logger.Any("type", reflect.TypeOf(v)), logger.String("key", keys[i]))
				continue
			}
			object := c.newObject()
			if unmarshalErr := encoding.Unmarshal(c.encoding, []byte(strVal), object); unmarshalErr != nil {
				logger.WarnWithCtx(ctx, "unmarshal data error", logger.Err(unmarshalErr), logger.String("key", keys[i]), logger.String("cacheKey", cacheKeys[i]))
				continue
			}
			m[keys[i]] = object
		}
	} else {
		// 情况2：使用反射，value 必须是 map 类型
		valueMap := reflect.ValueOf(value)
		if valueMap.Kind() != reflect.Map {
			return fmt.Errorf("value is not a map")
		}
		for i, v := range values {
			if v == nil {
				continue
			}
			strVal, ok := v.(string)
			if !ok {
				logger.WarnWithCtx(ctx, "unexpected value type in cache", logger.Any("type", reflect.TypeOf(v)), logger.String("key", keys[i]))
				continue
			}
			object := c.newObject()
			if unmarshalErr := encoding.Unmarshal(c.encoding, []byte(strVal), object); unmarshalErr != nil {
				logger.WarnWithCtx(ctx, "unmarshal data error", logger.Err(unmarshalErr), logger.String("key", keys[i]), logger.String("cacheKey", cacheKeys[i]))
				continue
			}
			valueMap.SetMapIndex(reflect.ValueOf(keys[i]), reflect.ValueOf(object))
		}
	}
	return nil
}

// Del 删除一个或多个 Redis key。
// keys 为业务 key（不含前缀），内部会拼接前缀。
// 如果某个 key 构建失败，会跳过该 key。
func (c *redisCache) Del(ctx context.Context, keys ...string) (err error) {
	start := time.Now()
	defer func() {
		c.logOperation(ctx, "Del", start, err, logger.Any("keys", keys))
	}()
	if len(keys) == 0 {
		return nil
	}

	cacheKeys := make([]string, len(keys))
	for index, key := range keys {
		cacheKey, setErr := BuildCacheKey(c.KeyPrefix, key)
		if setErr != nil {
			continue
		}
		cacheKeys[index] = cacheKey
	}
	err = c.client.Del(ctx, cacheKeys...).Err()
	if err != nil {
		return fmt.Errorf("c.client.Del error: %v, keys=%+v", err, cacheKeys)
	}
	return nil
}

// DelByPrefix 根据前缀批量删除 Redis key。
// 使用 SCAN 命令遍历所有匹配 prefix* 的 key，每扫描 batchSize 个后立即执行 Pipeline 删除，避免内存堆积。
// 注意：该操作可能耗时较长，且会遍历整个 Redis 键空间，请谨慎使用。
func (c *redisCache) DelByPrefix(ctx context.Context, prefix string) (err error) {
	start := time.Now()
	defer func() {
		c.logOperation(ctx, "DelByPrefix", start, err, logger.String("prefix", prefix))
	}()

	var cursor uint64
	const scanBatch = 100       // SCAN 每次扫描的 key 数量
	const pipelineBatch = 100   // Pipeline 每批执行的删除命令数

	var totalDeleted int64

	for {
		keys, nextCursor, scanErr := c.client.Scan(ctx, cursor, prefix+"*", scanBatch).Result()
		if scanErr != nil {
			return fmt.Errorf("c.client.Scan error: %v, prefix=%s", scanErr, prefix)
		}

		if len(keys) > 0 {
			pipeline := c.client.Pipeline()
			for _, key := range keys {
				pipeline.Del(ctx, key)
			}
			_, execErr := pipeline.Exec(ctx)
			if execErr != nil {
				return fmt.Errorf("pipeline.Del error: %v", execErr)
			}
			totalDeleted += int64(len(keys))
		}

		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}

	return nil
}

// SetCacheWithNotFound 为指定的 key 设置占位符（NotFoundPlaceholder），过期时间为 DefaultNotFoundExpireTime（1分钟）。
// 用于防止缓存穿透：当查询结果为空时，设置占位符以短时间阻挡后续相同请求。
func (c *redisCache) SetCacheWithNotFound(ctx context.Context, key string) (err error) {
	start := time.Now()
	defer func() {
		c.logOperation(ctx, "SetCacheWithNotFound", start, err, logger.String("key", key))
	}()
	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}
	return c.client.Set(ctx, cacheKey, NotFoundPlaceholder, DefaultNotFoundExpireTime).Err()
}

// BuildCacheKey 构建完整的缓存 key：如果 keyPrefix 为空则返回 key，否则返回 "keyPrefix:key"。
func BuildCacheKey(keyPrefix string, key string) (string, error) {
	if key == "" {
		return "", errors.New("[cache] key should not be empty")
	}
	if keyPrefix == "" {
		return key, nil
	}
	var builder strings.Builder
	builder.Grow(len(keyPrefix) + 1 + len(key))
	builder.WriteString(keyPrefix)
	builder.WriteString(":")
	builder.WriteString(key)
	return builder.String(), nil
}

// buildLockKey 构建分布式锁的完整 key。
// 如果 KeyPrefix 为空，直接返回传入的 key（业务层应已包含完整前缀）。
// 否则返回 "KeyPrefix:key"。
func (c *redisCache) buildLockKey(key string) string {
	if c.KeyPrefix == "" {
		return key
	}
	var builder strings.Builder
	builder.Grow(len(c.KeyPrefix) + 1 + len(key))
	builder.WriteString(c.KeyPrefix)
	builder.WriteString(":")
	builder.WriteString(key)
	return builder.String()
}

// logOperation 统一的缓存操作日志记录函数。
// 只记录耗时超过 20ms 或发生错误（CacheNotFound 除外）的操作，避免日志过多。
// 根据耗时和是否错误，使用不同的日志级别（Warn/Info/Debug）。
func (c *redisCache) logOperation(ctx context.Context, operation string, start time.Time, err error, fields ...logger.Field) {
	if errors.Is(err, CacheNotFound) {
		return
	}
	duration := time.Since(start)
	if duration < 20*time.Millisecond && err == nil {
		return
	}

	loggerFields := []logger.Field{
		logger.String("operation", operation),
		logger.String("ms", fmt.Sprintf("%v", float64(duration)/float64(time.Millisecond))),
	}
	if err != nil {
		loggerFields = append(loggerFields, logger.Err(err))
	}
	loggerFields = append(loggerFields, fields...)

	if err != nil {
		logger.WarnWithCtx(ctx, "cache_operation", loggerFields...)
	} else if duration > 50*time.Millisecond {
		logger.InfoWithCtx(ctx, "cache_slow_operation", loggerFields...)
	} else {
		logger.DebugWithCtx(ctx, "cache_operation", loggerFields...)
	}
}
