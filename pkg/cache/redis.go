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

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"go.uber.org/zap"

	"github.com/redis/go-redis/v9"
)

// CacheNotFound no hit cache
var CacheNotFound = redis.Nil

// NewRedisCacheOption 是NewRedisCache的可选配置
type NewRedisCacheOption func(*redisCache)

// WithCacheLog set log
func WithCacheLog(log *zap.Logger) NewRedisCacheOption {
	return func(rc *redisCache) {
		if log != nil {
			rc.log = log
		}
	}
}

// redisCache redis cache object
type redisCache struct {
	log               *zap.Logger
	client            *redis.Client
	KeyPrefix         string
	encoding          encoding.Encoding
	DefaultExpireTime time.Duration
	newObject         func() interface{}
	redsSync          *redsync.Redsync
}

// NewRedisCache new a cache, client parameter can be passed in for unit testing
func NewRedisCache(client *redis.Client, keyPrefix string, encode encoding.Encoding, newObject func() interface{}, options ...NewRedisCacheOption) Cache {
	redisPool := goredis.NewPool(client) // 创建 Redis 连接池
	rs := redsync.New(redisPool)         // 创建 redsync 实例
	return &redisCache{
		log:               logger.Get(),
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
		c.logOperation(ctx, "GetLoopLock", start, err, zap.String("lockKey", lockKey))
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
		c.logOperation(ctx, "GetLock", start, err, zap.String("lockKey", lockKey))
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
		c.logOperation(ctx, "Set", start, err, zap.String("key", key), zap.Duration("expireTime", expireTime))
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
		c.logOperation(ctx, "Get", start, err, zap.String("key", key), zap.Any("val", val))
	}()

	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}

	bytes, err := c.client.Get(ctx, cacheKey).Bytes()
	// NOTE: don't handle the case where redis value is nil
	// but leave it to the upstream for processing
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return err
		}
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
		c.logOperation(ctx, "MultiSet", start, err, zap.Duration("expireTime", expireTime), zap.Any("valueMap", valueMap))
	}()

	if len(valueMap) == 0 {
		return nil
	}
	if expireTime == 0 {
		expireTime = c.DefaultExpireTime
	}

	// 使用pipeline优化批量操作，减少网络往返
	pipeline := c.client.Pipeline()
	// 预估容量以减少重新分配
	cmds := make([]redis.Cmder, 0, 2*len(valueMap))

	for key, value := range valueMap {
		buf, err := encoding.Marshal(c.encoding, value)
		if err != nil {
			fmt.Printf("encoding.Marshal error, %v, value:%v\n", err, value)
			continue
		}
		cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
		if err != nil {
			fmt.Printf("BuildCacheKey error, %v, key:%v\n", err, key)
			continue
		}
		// 直接添加命令到pipeline，避免中间数组
		cmds = append(cmds, pipeline.Set(ctx, cacheKey, buf, expireTime))
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
		c.logOperation(ctx, "MultiGet", start, err, zap.Any("keys", keys), zap.Any("value", value))
	}()

	if len(keys) == 0 {
		return nil
	}
	cacheKeys := make([]string, len(keys))
	for index, key := range keys {
		cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
		if err != nil {
			return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
		}
		cacheKeys[index] = cacheKey
	}
	values, err := c.client.MGet(ctx, cacheKeys...).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
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
				fmt.Printf("unmarshal data error: %+v, key=%s, cacheKey=%s type=%v\n", err, keys[i], cacheKeys[i], reflect.TypeOf(value))
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
				fmt.Printf("unmarshal data error: %+v, key=%s, cacheKey=%s type=%v\n", err, keys[i], cacheKeys[i], reflect.TypeOf(value))
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
		c.logOperation(ctx, "Del", start, err, zap.Any("keys", keys))
	}()
	if len(keys) == 0 {
		return nil
	}

	cacheKeys := make([]string, len(keys))
	for index, key := range keys {
		cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
		if err != nil {
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
		c.logOperation(ctx, "DelByPrefix", start, err, zap.String("prefix", prefix))
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

	// 记录删除统计信息
	c.log.Info("DelByPrefix completed", zap.String("prefix", prefix), zap.Int64("deleted_keys", totalDeleted))
	return nil
}

// SetCacheWithNotFound set value for notfound
func (c *redisCache) SetCacheWithNotFound(ctx context.Context, key string) (err error) {
	start := time.Now()
	defer func() {
		c.logOperation(ctx, "SetCacheWithNotFound", start, err, zap.String("key", key))
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
func (c *redisCache) logOperation(ctx context.Context, operation string, start time.Time, err error, fields ...zap.Field) {
	duration := time.Since(start)
	// 只记录慢操作（超过10ms）或错误
	if duration < 10*time.Millisecond && err == nil {
		return
	}

	// 检查日志级别，避免不必要的计算
	logLevel := zap.DebugLevel
	if err != nil {
		logLevel = zap.WarnLevel
	} else if duration > 50*time.Millisecond { // 慢操作
		logLevel = zap.WarnLevel
	}
	// 只有在需要记录日志时才构建字段
	if !c.log.Core().Enabled(logLevel) {
		return
	}

	logFields := make([]zap.Field, 0, len(fields)+4)
	logFields = append(logFields,
		zap.String("request_id", metautils.ExtractIncoming(ctx).Get("request_id")),
		zap.String("operation", operation),
		zap.String("ms", fmt.Sprintf("%v", float64(duration)/float64(time.Millisecond))),
	)

	if err != nil {
		logFields = append(logFields, zap.Error(err))
	}

	logFields = append(logFields, fields...)

	if err != nil {
		c.log.Warn("cache_operation", logFields...)
	} else if duration > 50*time.Millisecond { // 慢操作
		c.log.Info("cache_slow_operation", logFields...)
	} else {
		c.log.Info("cache_operation", logFields...)
	}
}
