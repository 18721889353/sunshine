package cache

import (
	"context"
	"errors"
	"fmt"
	"github.com/18721889353/sunshine/pkg/encoding"
	"reflect"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"

	"github.com/redis/go-redis/v9"
)

// CacheNotFound no hit cache
var CacheNotFound = redis.Nil

// NewRedisCacheOption 是NewRedisCache的可选配置
type NewRedisCacheOption func(*redisCache)

// redisCache redis cache object
type redisCache struct {
	client            *redis.Client
	KeyPrefix         string
	encoding          encoding.Encoding
	DefaultExpireTime time.Duration
	newObject         func() interface{}
	redsSync          *redsync.Redsync
}

// NewRedisCache new a cache, client parameter can be passed in for unit testing
func NewRedisCache(client *redis.Client, keyPrefix string, encode encoding.Encoding, newObject func() interface{}, _ ...NewRedisCacheOption) Cache {
	redisPool := goredis.NewPool(client) // 创建 Redis 连接池
	rs := redsync.New(redisPool)         // 创建 redsync 实例
	return &redisCache{
		client:            client,
		KeyPrefix:         keyPrefix,
		encoding:          encode,
		newObject:         newObject,
		redsSync:          rs,
		DefaultExpireTime: time.Second * 5,
	}
}

// GetLoopLock acquires a distributed lock with the given key (blocking)
func (c *redisCache) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	var err error
	start := time.Now()
	// 初始化锁
	lockKey := c.buildLockKey(key)
	defer func() {
		c.logOperation(ctx, "GetLoopLock", start, err, logger.String("lockKey", lockKey))
	}()
	// 创建新的互斥锁
	mutex := c.redsSync.NewMutex(lockKey, options...)

	// Lock 阻塞直到获取到锁或上下文被取消
	err = mutex.LockContext(ctx)
	if err != nil {
		return nil, err
	}

	return mutex, nil
}

// GetLock acquires a distributed lock with the given key (non-blocking)
func (c *redisCache) GetLock(ctx context.Context, key string, options ...redsync.Option) (*redsync.Mutex, error) {
	start := time.Now()
	var err error
	// 初始化锁
	lockKey := c.buildLockKey(key)
	defer func() {
		c.logOperation(ctx, "GetLock", start, err, logger.String("lockKey", lockKey))
	}()

	// 创建新的互斥锁
	mutex := c.redsSync.NewMutex(lockKey, options...)

	// TryLock 尝试获取锁而不阻塞
	err = mutex.TryLockContext(ctx)
	if err != nil {
		return nil, err
	}

	return mutex, nil
}

// Set one value
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

// Get one value
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
	// NOTE: don't handle the case where redis value is nil
	// but leave it to the upstream for processing
	if err != nil {
		return err
	}

	// prevent Unmarshal from reporting an error if data is empty
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

// MultiSet set multiple values
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

	// 使用pipeline优化批量操作，减少网络往返
	pipeline := c.client.Pipeline()

	for key, value := range valueMap {
		buf, cacheErr := encoding.Marshal(c.encoding, value)
		if cacheErr != nil {
			logger.WarnWithCtx(ctx, "encoding.Marshal error", logger.Err(err), logger.Any("value", value))
			continue
		}
		cacheKey, buildErr := BuildCacheKey(c.KeyPrefix, key)
		if buildErr != nil {
			logger.WarnWithCtx(ctx, "BuildCacheKey error", logger.Err(buildErr), logger.String("key", key))
			continue
		}
		// 直接添加命令到pipeline
		pipeline.Set(ctx, cacheKey, buf, expireTime)
	}

	// 执行所有命令
	_, err = pipeline.Exec(ctx)
	if err != nil {
		return fmt.Errorf("pipeline.Exec error: %v", err)
	}

	return nil
}

// MultiGet get multiple values
// MultiGet get multiple values
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

	// 尝试使用类型断言，如果value是map[string]interface{}类型
	if m, ok := value.(map[string]interface{}); ok {
		for i, v := range values {
			if v == nil {
				continue
			}
			object := c.newObject()
			err = encoding.Unmarshal(c.encoding, []byte(v.(string)), object)
			if err != nil {
				logger.WarnWithCtx(ctx, "unmarshal data error", logger.Err(err), logger.String("key", keys[i]), logger.String("cacheKey", cacheKeys[i]), logger.String("type", reflect.TypeOf(value).String()))
				continue
			}
			m[keys[i]] = object
		}
	} else {
		// 使用反射方式
		valueMap := reflect.ValueOf(value)
		if valueMap.Kind() != reflect.Map {
			return fmt.Errorf("value is not a map")
		}
		for i, v := range values {
			if v == nil {
				continue
			}
			object := c.newObject()
			err = encoding.Unmarshal(c.encoding, []byte(v.(string)), object)
			if err != nil {
				logger.WarnWithCtx(ctx, "unmarshal data error", logger.Err(err), logger.String("key", keys[i]), logger.String("cacheKey", cacheKeys[i]), logger.String("type", reflect.TypeOf(value).String()))
				continue
			}
			valueMap.SetMapIndex(reflect.ValueOf(keys[i]), reflect.ValueOf(object))
		}
	}

	return nil
}

// Del delete multiple values
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

// DelByPrefix deletes all keys that start with the given prefix
func (c *redisCache) DelByPrefix(ctx context.Context, prefix string) (err error) {
	start := time.Now()
	defer func() {
		c.logOperation(ctx, "DelByPrefix", start, err, logger.String("prefix", prefix))
	}()

	var cursor uint64
	const batchSize = 100

	// 使用pipeline优化批量删除操作，减少网络往返
	pipeline := c.client.Pipeline()
	var totalDeleted int64

	for {
		keys, nextCursor, scanErr := c.client.Scan(ctx, cursor, prefix+"*", batchSize).Result()
		if scanErr != nil {
			return fmt.Errorf("c.client.Scan error: %v, prefix=%s", scanErr, prefix)
		}

		if len(keys) > 0 {
			// 批量添加删除命令到pipeline
			for _, key := range keys {
				pipeline.Del(ctx, key)
			}
			totalDeleted += int64(len(keys))
		}

		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}

	// 执行所有删除命令
	_, err = pipeline.Exec(ctx)
	if err != nil {
		return fmt.Errorf("pipeline.Del error: %v", err)
	}

	return nil
}

// SetCacheWithNotFound set value for notfound
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

// BuildCacheKey construct a cache key with a prefix
func BuildCacheKey(keyPrefix string, key string) (string, error) {
	if key == "" {
		return "", errors.New("[cache] key should not be empty")
	}

	// 对于无前缀的情况，直接返回key以避免额外的内存分配
	if keyPrefix == "" {
		return key, nil
	}

	// 对于有前缀的情况，使用strings.Builder并预分配容量
	var builder strings.Builder
	builder.Grow(len(keyPrefix) + 1 + len(key))
	builder.WriteString(keyPrefix)
	builder.WriteString(":")
	builder.WriteString(key)
	return builder.String(), nil
}

// buildLockKey 构建分布式锁的key
func (c *redisCache) buildLockKey(key string) string {
	// 对于无前缀的情况，直接拼接避免创建strings.Builder的开销
	if c.KeyPrefix == "" {
		return "lock:" + key
	}

	// 对于有前缀的情况，使用strings.Builder并预分配容量
	var builder strings.Builder
	builder.Grow(len(c.KeyPrefix) + 5 + len(key))
	builder.WriteString(c.KeyPrefix)
	builder.WriteString("lock:")
	builder.WriteString(key)
	return builder.String()
}

// 统一的日志记录函数
func (c *redisCache) logOperation(ctx context.Context, operation string, start time.Time, err error, fields ...logger.Field) {
	// 缓存未命中是正常情况，不记录日志
	if errors.Is(err, CacheNotFound) {
		return
	}
	duration := time.Since(start)
	// 只记录慢操作（超过20ms）或错误
	if duration < 20*time.Millisecond && err == nil {
		return
	}

	// 构建 logger 字段
	loggerFields := []logger.Field{
		logger.String("operation", operation),
		logger.String("ms", fmt.Sprintf("%v", float64(duration)/float64(time.Millisecond))),
	}

	if err != nil {
		loggerFields = append(loggerFields, logger.Err(err))
	}

	// 添加额外字段
	loggerFields = append(loggerFields, fields...)

	if err != nil {
		logger.WarnWithCtx(ctx, "cache_operation", loggerFields...)
	} else if duration > 50*time.Millisecond { // 慢操作
		logger.InfoWithCtx(ctx, "cache_slow_operation", loggerFields...)
	} else {
		logger.DebugWithCtx(ctx, "cache_operation", loggerFields...)
	}
}
