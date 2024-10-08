package cache

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	pkgLogger "github.com/18721889353/sunshine/pkg/logger"
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
	client            *redis.Client
	KeyPrefix         string
	encoding          encoding.Encoding
	DefaultExpireTime time.Duration
	newObject         func() interface{}
	redsSync          *redsync.Redsync
}

// NewRedisCache new a cache, client parameter can be passed in for unit testing

func NewRedisCache(client *redis.Client, keyPrefix string, encode encoding.Encoding, newObject func() interface{}) Cache {
	redisPool := goredis.NewPool(client)
	rs := redsync.New(redisPool)
	return &redisCache{
		client:            client,
		KeyPrefix:         keyPrefix,
		encoding:          encode,
		newObject:         newObject,
		redsSync:          rs,
		DefaultExpireTime: time.Second * 5,
	}
}

// GetLoopLock acquires a distributed lock with the given key
func (c *redisCache) GetLoopLock(ctx context.Context, key string, expireTime, loopWaitTime time.Duration, loopNum int) (*redsync.Mutex, error) {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg GetLoopLock"),
	}
	// 初始化锁
	lockKey := fmt.Sprintf("%slock:%s", c.KeyPrefix, key)
	if expireTime == 0 {
		expireTime = c.DefaultExpireTime
	}
	// 如果循环次数为0，则使用默认值
	if loopNum == 0 {
		loopNum = 10
	}
	if loopWaitTime == 0 {
		loopWaitTime = 200 * time.Millisecond
	}
	mutex := c.redsSync.NewMutex(lockKey, redsync.WithExpiry(expireTime), redsync.WithTries(loopNum), redsync.WithRetryDelay(loopWaitTime))

	// 开始锁定
	if err := mutex.LockContext(ctx); err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return nil, err
	}

	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return mutex, nil
}

// GetLock acquires a distributed lock with the given key
func (c *redisCache) GetLock(ctx context.Context, key string, expireTime time.Duration) (*redsync.Mutex, error) {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg RedisLock"),
	}
	// 初始化锁
	lockKey := fmt.Sprintf("%slock:%s", c.KeyPrefix, key)
	mutex := c.redsSync.NewMutex(lockKey, redsync.WithExpiry(expireTime))
	// 开始锁定
	if err := mutex.TryLockContext(ctx); err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return nil, err
	}
	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return mutex, nil
}

// ReleaseLock releases the distributed lock
func (c *redisCache) ReleaseLock(ctx context.Context, mutex *redsync.Mutex) error {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg ReleaseLock"),
	}
	if ok, err := mutex.UnlockContext(ctx); !ok || err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return err
	}
	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return nil
}

// Set one value
func (c *redisCache) Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) error {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Set"),
	}
	buf, err := encoding.Marshal(c.encoding, val)

	if err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return fmt.Errorf("encoding.Marshal error: %v, key=%s, val=%+v ", err, key, val)
	}

	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}
	if expireTime == 0 {
		expireTime = c.DefaultExpireTime
	}
	err = c.client.Set(ctx, cacheKey, buf, expireTime).Err()
	if err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return fmt.Errorf("c.client.Set error: %v, cacheKey=%s", err, cacheKey)
	}

	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)

	return nil
}

// Get one value
func (c *redisCache) Get(ctx context.Context, key string, val interface{}) error {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Get"),
	}
	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}

	bytes, err := c.client.Get(ctx, cacheKey).Bytes()
	// NOTE: don't handle the case where redis value is nil
	// but leave it to the upstream for processing
	if err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
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
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return fmt.Errorf("encoding.Unmarshal error: %v, key=%s, cacheKey=%s, type=%v, json=%+v ",
			err, key, cacheKey, reflect.TypeOf(val), string(bytes))
	}
	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return nil
}

// MultiSet set multiple values
func (c *redisCache) MultiSet(ctx context.Context, valueMap map[string]interface{}, expireTime time.Duration) error {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg MultiSet"),
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
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
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
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return fmt.Errorf("pipeline.Exec error: %v", err)
	}
	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return nil
}

// MultiGet get multiple values
func (c *redisCache) MultiGet(ctx context.Context, keys []string, value interface{}) error {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg MultiGet"),
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
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
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
			fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
			pkgLogger.Warn("Cache msg", fields...)
			fmt.Printf("unmarshal data error: %+v, key=%s, cacheKey=%s type=%v\n", err, keys[i], cacheKeys[i], reflect.TypeOf(value))
			continue
		}
		valueMap.SetMapIndex(reflect.ValueOf(cacheKeys[i]), reflect.ValueOf(object))
	}
	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return nil
}

// Del delete multiple values
func (c *redisCache) Del(ctx context.Context, keys ...string) error {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Del"),
	}
	if len(keys) == 0 {
		return nil
	}

	cacheKeys := make([]string, len(keys))
	for index, key := range keys {
		cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
		if err != nil {
			fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
			pkgLogger.Warn("Cache msg", fields...)
			continue
		}
		cacheKeys[index] = cacheKey
	}
	err := c.client.Del(ctx, cacheKeys...).Err()
	if err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return fmt.Errorf("c.client.Del error: %v, keys=%+v", err, cacheKeys)
	}
	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return nil
}

// SetCacheWithNotFound set value for notfound
func (c *redisCache) SetCacheWithNotFound(ctx context.Context, key string) error {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg SetCacheWithNotFound"),
	}
	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
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
		cacheKey = strings.Join([]string{keyPrefix, key}, ":")
	}

	return cacheKey, nil
}

// -------------------------------------------------------------------------------------------

// redisClusterCache redis cluster cache object
type redisClusterCache struct {
	client            *redis.ClusterClient
	KeyPrefix         string
	encoding          encoding.Encoding
	DefaultExpireTime time.Duration
	newObject         func() interface{}
	redsSync          *redsync.Redsync
}

// NewRedisClusterCache new a cache
func NewRedisClusterCache(client *redis.ClusterClient, keyPrefix string, encode encoding.Encoding, newObject func() interface{}) Cache {
	redisPool := goredis.NewPool(client)
	rs := redsync.New(redisPool)
	return &redisClusterCache{
		client:            client,
		KeyPrefix:         keyPrefix,
		encoding:          encode,
		newObject:         newObject,
		redsSync:          rs,
		DefaultExpireTime: time.Second * 5,
	}
}

// GetLock acquires a distributed lock with the given key
func (c *redisClusterCache) GetLock(ctx context.Context, key string, timeout time.Duration) (*redsync.Mutex, error) {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg RedisLock"),
	}
	// 初始化锁
	lockKey := fmt.Sprintf("%slock:%s", c.KeyPrefix, key)
	mutex := c.redsSync.NewMutex(lockKey, redsync.WithExpiry(timeout))
	// 开始锁定
	if err := mutex.Lock(); err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return nil, err
	}
	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return mutex, nil
}

// GetLoopLock acquires a distributed lock with the given key
func (c *redisClusterCache) GetLoopLock(ctx context.Context, key string, expireTime, loopWaitTime time.Duration, loopNum int) (*redsync.Mutex, error) {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg GetLoopLock"),
	}
	// 初始化锁
	lockKey := fmt.Sprintf("%slock:%s", c.KeyPrefix, key)
	if expireTime == 0 {
		expireTime = c.DefaultExpireTime
	}
	mutex := c.redsSync.NewMutex(lockKey, redsync.WithExpiry(expireTime))

	// 如果循环次数为0，则使用默认值
	if loopNum == 0 {
		loopNum = 10
	}
	if loopWaitTime == 0 {
		loopWaitTime = 200 * time.Millisecond
	}
	// 设置超时时间
	timeoutTime := time.Now().Add(loopWaitTime * time.Duration(loopNum+1))
	// 开始循环尝试获取锁
	for i := 0; i < loopNum; i++ {
		if err := mutex.Lock(); err != nil {
			// 如果获取锁失败，则等待一段时间再尝试获取
			if time.Now().After(timeoutTime) {
				// 超过超时时间，返回失败
				fields = append(fields, pkgLogger.Err(errors.New("failed to acquire loopLock within specified timeout")), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
				pkgLogger.Warn("Cache msg", fields...)
				return nil, errors.New("failed to acquire loopLock within specified timeout")
			}
			time.Sleep(loopWaitTime)
		} else {
			break
		}
	}
	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return mutex, nil
}

// ReleaseLock releases the distributed lock
func (c *redisClusterCache) ReleaseLock(ctx context.Context, mutex *redsync.Mutex) error {
	begin := time.Now()
	fields := []zap.Field{
		zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg ReleaseLock"),
	}
	if ok, err := mutex.Unlock(); !ok || err != nil {
		fields = append(fields, pkgLogger.Err(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		pkgLogger.Warn("Cache msg", fields...)
		return err
	}
	fields = append(fields, zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	pkgLogger.Info("Cache msg", fields...)
	return nil
}

// Set one value
func (c *redisClusterCache) Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) error {
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
func (c *redisClusterCache) Get(ctx context.Context, key string, val interface{}) error {
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
func (c *redisClusterCache) MultiSet(ctx context.Context, valueMap map[string]interface{}, expireTime time.Duration) error {
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
		return fmt.Errorf("pipeline.Exec error: %v", err)
	}
	return nil
}

// MultiGet get multiple values
func (c *redisClusterCache) MultiGet(ctx context.Context, keys []string, value interface{}) error {
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
			fmt.Printf("unmarshal data error: %+v, key=%s, cacheKey=%s type=%v\n", err, keys[i], cacheKeys[i], reflect.TypeOf(value))
			continue
		}
		valueMap.SetMapIndex(reflect.ValueOf(cacheKeys[i]), reflect.ValueOf(object))
	}
	return nil
}

// Del delete multiple values
func (c *redisClusterCache) Del(ctx context.Context, keys ...string) error {
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
	err := c.client.Del(ctx, cacheKeys...).Err()
	if err != nil {
		return fmt.Errorf("c.client.Del error: %v, keys=%+v", err, cacheKeys)
	}
	return nil
}

// SetCacheWithNotFound set value for notfound
func (c *redisClusterCache) SetCacheWithNotFound(ctx context.Context, key string) error {
	cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
	if err != nil {
		return fmt.Errorf("BuildCacheKey error: %v, key=%s", err, key)
	}

	return c.client.Set(ctx, cacheKey, NotFoundPlaceholder, DefaultNotFoundExpireTime).Err()
}

func requestIDField(ctx context.Context, requestIDKey string) zap.Field {
	if requestIDKey == "" {
		return zap.Skip()
	}

	var field zap.Field
	if requestIDKey != "" {
		if v, ok := ctx.Value(requestIDKey).(string); ok {
			field = zap.String(requestIDKey, v)
		} else {
			field = zap.Skip()
		}
	}
	return field
}
