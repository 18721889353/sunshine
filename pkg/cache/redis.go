package cache

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"go.uber.org/zap"

	"github.com/redis/go-redis/v9"

	"github.com/18721889353/sunshine/pkg/encoding"
)

// CacheNotFound no hit cache
var CacheNotFound = redis.Nil

// redisCache redis cache object
type redisCache struct {
	log               *zap.Logger
	client            *redis.Client
	KeyPrefix         string
	encoding          encoding.Encoding
	DefaultExpireTime time.Duration
	newObject         func() interface{}
	redsSync          *redsync.Redsync
	mutex             *redsync.Mutex // Redis 分布式锁
}

// NewRedisCache new a cache, client parameter can be passed in for unit testing

func NewRedisCache(client *redis.Client, keyPrefix string, encode encoding.Encoding, newObject func() interface{}, log *zap.Logger) Cache {
	redisPool := goredis.NewPool(client) // 创建 Redis 连接池
	rs := redsync.New(redisPool)         // 创建 redsync 实例
	if log == nil {
		log, _ = zap.NewProduction()
	}
	return &redisCache{
		log:               log,
		client:            client,
		KeyPrefix:         keyPrefix,
		encoding:          encode,
		newObject:         newObject,
		redsSync:          rs,
		DefaultExpireTime: time.Second * 5,
	}
}

// GetLoopLock acquires a distributed lock with the given key
func (c *redisCache) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) error {
	begin := time.Now()
	// 初始化锁
	lockKey := fmt.Sprintf("%slock:%s", c.KeyPrefix, key)
	// 构建日志字段
	requestID := requestIDField(ctx, "request_id")
	logFields := []zap.Field{
		requestID,
		zap.String("log_from", "Cache msg GetLoopLock"),
		zap.Any("sql", map[string]any{"key": key, "options": options}),
	}
	//Lock 阻塞直到获取到锁或上下文被取消
	// 开始锁定
	c.mutex = c.redsSync.NewMutex(lockKey, options...) // 创建分布式互斥锁
	err := c.mutex.LockContext(ctx)
	if err != nil {
		logFields = append(logFields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", logFields...)
		return err
	} else {
		elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
		if elapsed > 10 {
			logFields = append(logFields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
			c.log.Info("Cache msg", logFields...)
		}
		return nil
	}
}

// GetLock acquires a distributed lock with the given key
func (c *redisCache) GetLock(ctx context.Context, key string, options ...redsync.Option) error {
	begin := time.Now()
	// 初始化锁
	lockKey := fmt.Sprintf("%slock:%s", c.KeyPrefix, key)
	// 构建日志字段
	requestID := requestIDField(ctx, "request_id")
	logFields := []zap.Field{
		requestID,
		zap.String("log_from", "Cache msg RedisLock"),
		zap.Any("sql", map[string]any{"key": key, "options": options}),
	}
	c.mutex = c.redsSync.NewMutex(lockKey, options...) // 创建分布式互斥锁
	// TryLock 尝试获取锁而不阻塞
	err := c.mutex.TryLockContext(ctx)
	if err != nil {
		logFields = append(logFields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", logFields...)
		return err
	} else {
		elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
		if elapsed > 10 {
			logFields = append(logFields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
			c.log.Info("Cache msg", logFields...)
		}
		return nil
	}

}

// ReleaseLock releases the distributed lock
func (c *redisCache) ReleaseLock(ctx context.Context) error {
	begin := time.Now()
	// 构建日志字段
	requestID := requestIDField(ctx, "request_id")
	logFields := []zap.Field{
		requestID,
		zap.String("log_from", "Cache msg ReleaseLock"),
	}
	// 解锁操作
	_, err := c.mutex.UnlockContext(ctx)
	if err != nil {
		logFields = append(logFields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", logFields...)
		return err
	} else {
		elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
		if elapsed > 10 {
			logFields = append(logFields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
			c.log.Info("Cache msg", logFields...)
		}
		return nil
	}
}

// Set one value
func (c *redisCache) Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) error {
	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Set"),
		zap.Any("sql", map[string]any{"key": key, "val": val, "expireTime": expireTime}),
	}
	buf, err := encoding.Marshal(c.encoding, val)

	if err != nil {
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("encoding.Marshal error: %v, key=%s, val=%+v ", err, key, val)
	}

	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}
	if expireTime == 0 {
		expireTime = c.DefaultExpireTime
	}
	err = c.client.Set(ctx, cacheKey, buf, expireTime).Err()
	if err != nil {
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("c.client.Set error: %v, cacheKey=%s", err, cacheKey)
	}

	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	if elapsed > 10 {
		fields = append(fields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
		c.log.Info("Cache msg", fields...)
	}

	return nil
}

// Get one value
func (c *redisCache) Get(ctx context.Context, key string, val interface{}) error {
	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Get"),
		zap.Any("sql", map[string]any{"key": key, "val": val}),
	}
	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}

	bytes, err := c.client.Get(ctx, cacheKey).Bytes()
	// NOTE: don't handle the case where redis value is nil
	// but leave it to the upstream for processing
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return err
		}
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
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
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("encoding.Unmarshal error: %v, key=%s, cacheKey=%s, type=%v, json=%+v ",
			err, key, cacheKey, reflect.TypeOf(val), string(bytes))
	}

	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	if elapsed > 10 {
		fields = append(fields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
		c.log.Info("Cache msg", fields...)
	}
	return nil
}

// MultiSet set multiple values
func (c *redisCache) MultiSet(ctx context.Context, valueMap map[string]interface{}, expireTime time.Duration) error {
	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg MultiSet"),
		zap.Any("sql", map[string]any{"valueMap": valueMap, "expireTime": expireTime}),
	}
	if len(valueMap) == 0 {
		return nil
	}
	if expireTime == 0 {
		expireTime = c.DefaultExpireTime
	}

	// the key-value is paired and has twice the capacity of a map
	paris := make([]interface{}, 0, 2*len(valueMap))
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
		paris = append(paris, []byte(cacheKey))
		paris = append(paris, buf)
	}
	pipeline := c.client.Pipeline()
	err := pipeline.MSet(ctx, paris...).Err()
	if err != nil {
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("pipeline.MSet error: %v", err)
	}
	for i := 0; i < len(paris); i = i + 2 {
		switch paris[i].(type) {
		case []byte:
			pipeline.Expire(ctx, string(paris[i].([]byte)), expireTime)
		default:
			fmt.Printf("redis expire is unsupported key type: %+v\n", reflect.TypeOf(paris[i]))
		}
	}
	_, err = pipeline.Exec(ctx)
	if err != nil {
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("pipeline.Exec error: %v", err)
	}

	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	if elapsed > 10 {
		fields = append(fields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
		c.log.Info("Cache msg", fields...)
	}
	return nil
}

// MultiGet get multiple values
func (c *redisCache) MultiGet(ctx context.Context, keys []string, value interface{}) error {
	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg MultiGet"),
		zap.Any("sql", map[string]any{"keys": keys, "value": value}),
	}
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
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("c.client.MGet error: %v, keys=%+v", err, cacheKeys)
	}

	// Injection into map via reflection
	valueMap := reflect.ValueOf(value)
	for i, v := range values {
		if v == nil {
			continue
		}
		object := c.newObject()
		err = encoding.Unmarshal(c.encoding, []byte(v.(string)), object)
		if err != nil {
			fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
			c.log.Warn("Cache msg", fields...)
			fmt.Printf("unmarshal data error: %+v, key=%s, cacheKey=%s type=%v\n", err, keys[i], cacheKeys[i], reflect.TypeOf(value))
			continue
		}
		valueMap.SetMapIndex(reflect.ValueOf(cacheKeys[i]), reflect.ValueOf(object))
	}

	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	if elapsed > 10 {
		fields = append(fields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
		c.log.Info("Cache msg", fields...)
	}
	return nil
}

// Del delete multiple values
func (c *redisCache) Del(ctx context.Context, keys ...string) error {
	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Del"),
		zap.Any("sql", map[string]any{"keys": keys}),
	}
	if len(keys) == 0 {
		return nil
	}

	cacheKeys := make([]string, len(keys))
	for index, key := range keys {
		cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
		if err != nil {
			fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
			c.log.Warn("Cache msg", fields...)
			continue
		}
		cacheKeys[index] = cacheKey
	}
	err := c.client.Del(ctx, cacheKeys...).Err()
	if err != nil {
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("c.client.Del error: %v, keys=%+v", err, cacheKeys)
	}

	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	if elapsed > 10 {
		fields = append(fields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
		c.log.Info("Cache msg", fields...)
	}
	return nil
}

// DelByPrefix deletes all keys that start with the given prefix
func (c *redisCache) DelByPrefix(ctx context.Context, prefix string) error {
	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg DelByPrefix"),
		zap.Any("sql", map[string]any{"prefix": prefix}),
	}

	var cursor uint64
	var n int
	for {
		var keys []string
		var err error
		keys, cursor, err = c.client.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
			c.log.Warn("Cache msg", fields...)
			return fmt.Errorf("c.client.Scan error: %v, prefix=%s", err, prefix)
		}
		n += len(keys)
		if len(keys) > 0 {
			err = c.client.Del(ctx, keys...).Err()
			if err != nil {
				fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
				c.log.Warn("Cache msg", fields...)
				return fmt.Errorf("c.client.Del error: %v, keys=%+v", err, keys)
			}
		}
		if cursor == 0 {
			break
		}
	}

	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	if elapsed > 10 {
		fields = append(fields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
		c.log.Info("Cache msg", fields...)
	}
	return nil
}

// SetCacheWithNotFound set value for notfound
func (c *redisCache) SetCacheWithNotFound(ctx context.Context, key string) error {
	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg SetCacheWithNotFound"),
	}
	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}

	return c.client.Set(ctx, cacheKey, NotFoundPlaceholder, DefaultNotFoundExpireTime).Err()
}

// BuildCacheKey construct a cache key with a prefix
func BuildCacheKey(keyPrefix string, key string) (string, error) {
	if key == "" {
		return "", errors.New("[cache] key should not be empty")
	}

	cacheKey := key
	if keyPrefix != "" {
		//cacheKey = strings.Join([]string{keyPrefix, key}, ":")
		var builder strings.Builder
		builder.WriteString(keyPrefix)
		builder.WriteString(":")
		builder.WriteString(key)
		cacheKey = builder.String()
	}

	return cacheKey, nil
}

func requestIDField(ctx context.Context, requestIDKey string) zap.Field {
	return zap.String(requestIDKey, metautils.ExtractIncoming(ctx).Get(requestIDKey))
}
