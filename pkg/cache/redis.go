package cache

import (
	"context"
	"errors"
	"fmt"
	"github.com/bits-and-blooms/bloom/v3"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"go.uber.org/zap"

	"github.com/redis/go-redis/v9"

	"github.com/18721889353/sunshine/pkg/encoding"
)

// CacheNotFound no hit cache
var CacheNotFound = redis.Nil

// 布隆过滤器配置
const (
	DefaultBloomFilterExpectedElements  = 10000000 // 预期元素数量
	DefaultBloomFilterFalsePositiveRate = 0.001    // 默认误判率0.1%
)

// redisCache redis cache object
type redisCache struct {
	log               *zap.Logger
	client            *redis.Client
	KeyPrefix         string
	encoding          encoding.Encoding
	DefaultExpireTime time.Duration
	newObject         func() interface{}
	redsSync          *redsync.Redsync
	bloomFilter       *bloom.BloomFilter
	bloomFilterMutex  sync.RWMutex
	bloomFilterStats  *BloomFilterStats
}

// BloomFilterStats 布隆过滤器统计信息
type BloomFilterStats struct {
	Hits           int64 `json:"hits"`
	Misses         int64 `json:"misses"`
	FalsePositives int64 `json:"false_positives"`
	TrueNegatives  int64 `json:"true_negatives"`
	ElementsAdded  int64 `json:"elements_added"`
}

// NewRedisCache new a cache, client parameter can be passed in for unit testing
func NewRedisCache(client *redis.Client, keyPrefix string, encode encoding.Encoding, newObject func() interface{}) Cache {
	redisPool := goredis.NewPool(client) // 创建 Redis 连接池
	rs := redsync.New(redisPool)         // 创建 redsync 实例

	// 初始化布隆过滤器
	bloomFilter := bloom.NewWithEstimates(
		DefaultBloomFilterExpectedElements,
		DefaultBloomFilterFalsePositiveRate,
	)
	return &redisCache{
		log:               logger.Get(),
		client:            client,
		KeyPrefix:         keyPrefix,
		encoding:          encode,
		newObject:         newObject,
		redsSync:          rs,
		DefaultExpireTime: time.Second * 5,
		bloomFilter:       bloomFilter,
		bloomFilterStats:  &BloomFilterStats{},
	}
}

// InitBloomFilter 初始化布隆过滤器（从现有缓存键）
func (c *redisCache) InitBloomFilter(ctx context.Context) (err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg InitBloomFilter"),
			)
			err = fmt.Errorf("panic in InitBloomFilter: %v", r)
		}
	}()

	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg InitBloomFilter"),
	}

	// 使用SCAN迭代所有键
	var cursor uint64
	var count int
	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, c.KeyPrefix+"*", 100).Result()
		if err != nil {
			fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
			c.log.Warn("Cache msg", fields...)
			return fmt.Errorf("c.client.Scan error: %v", err)
		}

		count += len(keys)
		for _, key := range keys {
			// 移除前缀，只添加原始键到布隆过滤器
			originalKey := strings.TrimPrefix(key, c.KeyPrefix+":")
			c.AddToBloomFilter(ctx, originalKey)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	fields = append(fields,
		zap.Int("keys_processed", count),
		zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	c.log.Info("Cache msg", fields...)

	return nil
}

// AddToBloomFilter 添加键到布隆过滤器
func (c *redisCache) AddToBloomFilter(ctx context.Context, key string) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg AddToBloomFilter"),
			)
		}
	}()

	c.bloomFilterMutex.Lock()
	defer c.bloomFilterMutex.Unlock()

	c.bloomFilter.Add([]byte(key))
	c.bloomFilterStats.ElementsAdded++
}

// BloomFilter 测试键是否可能在布隆过滤器中
func (c *redisCache) BloomFilter(ctx context.Context, key string) bool {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg TestBloomFilter"),
			)
		}

	}()

	c.bloomFilterMutex.RLock()
	defer c.bloomFilterMutex.RUnlock()

	return c.bloomFilter.Test([]byte(key))
}

// GetBloomFilterStats 获取布隆过滤器统计信息
func (c *redisCache) GetBloomFilterStats(ctx context.Context) BloomFilterStats {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg GetBloomFilterStats"),
			)
		}
	}()

	c.bloomFilterMutex.RLock()
	defer c.bloomFilterMutex.RUnlock()

	return *c.bloomFilterStats
}

// RebuildBloomFilter 重建布隆过滤器
func (c *redisCache) RebuildBloomFilter(ctx context.Context) (err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg RebuildBloomFilter"),
			)
			err = fmt.Errorf("panic in RebuildBloomFilter: %v", r)
		}
	}()

	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg RebuildBloomFilter"),
	}

	// 创建新的布隆过滤器
	newBloomFilter := bloom.NewWithEstimates(
		DefaultBloomFilterExpectedElements,
		DefaultBloomFilterFalsePositiveRate,
	)
	newStats := &BloomFilterStats{}

	// 使用SCAN迭代所有键
	var cursor uint64
	var count int64
	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, c.KeyPrefix+"*", 100).Result()
		if err != nil {
			fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
			c.log.Warn("Cache msg", fields...)
			return fmt.Errorf("c.client.Scan error: %v", err)
		}

		count += int64(len(keys))
		for _, key := range keys {
			// 移除前缀，只添加原始键到布隆过滤器
			originalKey := strings.TrimPrefix(key, c.KeyPrefix+":")
			newBloomFilter.Add([]byte(originalKey))
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	// 替换旧的布隆过滤器
	c.bloomFilterMutex.Lock()
	c.bloomFilter = newBloomFilter
	c.bloomFilterStats = newStats
	c.bloomFilterStats.ElementsAdded = count
	c.bloomFilterMutex.Unlock()

	fields = append(fields,
		zap.Int64("keys_processed", count),
		zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
	c.log.Info("Cache msg", fields...)

	return nil
}

// GetLoopLock acquires a distributed lock with the given key (blocking)
func (c *redisCache) GetLoopLock(ctx context.Context, key string, options ...redsync.Option) (mutex *redsync.Mutex, err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg GetLoopLock"),
			)
			err = fmt.Errorf("panic in GetLoopLock: %v", r)
		}

	}()

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

	// 创建新的互斥锁
	mutex = c.redsSync.NewMutex(lockKey, options...)

	// Lock 阻塞直到获取到锁或上下文被取消
	err = mutex.LockContext(ctx)
	if err != nil {
		logFields = append(logFields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", logFields...)
		return nil, err
	}

	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	if elapsed > 10 {
		logFields = append(logFields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
		c.log.Info("Cache msg", logFields...)
	}
	return mutex, nil
}

// GetLock acquires a distributed lock with the given key (non-blocking)
func (c *redisCache) GetLock(ctx context.Context, key string, options ...redsync.Option) (mutex *redsync.Mutex, err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg GetLock"),
			)
			err = fmt.Errorf("panic in GetLock: %v", r)
		}
	}()

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

	// 创建新的互斥锁
	mutex = c.redsSync.NewMutex(lockKey, options...)

	// TryLock 尝试获取锁而不阻塞
	err = mutex.TryLockContext(ctx)
	if err != nil {
		logFields = append(logFields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", logFields...)
		return nil, err
	}

	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	if elapsed > 10 {
		logFields = append(logFields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
		c.log.Info("Cache msg", logFields...)
	}
	return mutex, nil
}

// Set one value
func (c *redisCache) Set(ctx context.Context, key string, val interface{}, expireTime time.Duration) (err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg Set"),
			)
			err = fmt.Errorf("panic in Set: %v", r)
		}
	}()

	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Set"),
		zap.Any("sql", map[string]any{"key": key, "val": val, "expireTime": expireTime}),
	}
	// 添加到布隆过滤器
	c.AddToBloomFilter(ctx, key)

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
func (c *redisCache) Get(ctx context.Context, key string, val interface{}) (err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg Get"),
			)
			err = fmt.Errorf("panic in Get: %v", r)
		}
	}()

	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg Get"),
		zap.Any("sql", map[string]any{"key": key, "val": val}),
	}

	// 1. 先检查布隆过滤器
	if !c.BloomFilter(ctx, key) {
		c.bloomFilterMutex.Lock()
		c.bloomFilterStats.TrueNegatives++
		c.bloomFilterMutex.Unlock()

		fields = append(fields,
			zap.Bool("bloom_filter_miss", true),
			zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Info("Cache msg", fields...)
		return CacheNotFound
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
		if errors.Is(err, CacheNotFound) {
			c.bloomFilterMutex.Lock()
			c.bloomFilterStats.Misses++
			c.bloomFilterMutex.Unlock()
			// 缓存未命中，可能是布隆过滤器误判
			fields = append(fields,
				zap.Bool("bloom_filter_false_positive", true),
				zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
			c.log.Warn("Cache msg bloom filter false positive", fields...)

			c.bloomFilterMutex.Lock()
			c.bloomFilterStats.FalsePositives++
			c.bloomFilterMutex.Unlock()
			return err
		}
		fields = append(fields, zap.Error(err), zap.String("ms", fmt.Sprintf("%v", float64(time.Since(begin).Nanoseconds())/1e6)))
		c.log.Warn("Cache msg", fields...)
		return err
	}

	// 防止Unmarshal报错如果数据是空的
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

	c.bloomFilterMutex.Lock()
	c.bloomFilterStats.Hits++
	c.bloomFilterMutex.Unlock()

	elapsed := float64(time.Since(begin).Nanoseconds()) / 1e6
	if elapsed > 10 {
		fields = append(fields, zap.String("ms", fmt.Sprintf("%v", elapsed)))
		c.log.Info("Cache msg", fields...)
	}
	return nil
}

// MultiSet set multiple values
func (c *redisCache) MultiSet(ctx context.Context, valueMap map[string]interface{}, expireTime time.Duration) (err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg MultiSet"),
			)
			err = fmt.Errorf("panic in MultiSet: %v", r)
		}
	}()

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

	// 添加到布隆过滤器
	for key := range valueMap {
		c.AddToBloomFilter(ctx, key)
	}

	// the key-value is paired and has twice the capacity of a map
	paris := make([]interface{}, 0, 2*len(valueMap))
	for key, value := range valueMap {
		buf, err := encoding.Marshal(c.encoding, value)
		if err != nil {
			c.log.Error("encoding.Marshal error",
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg MultiSet"),
				zap.Error(err),
				zap.Any("value", value))
			continue
		}
		cacheKey, err := BuildCacheKey(c.KeyPrefix, key)
		if err != nil {
			c.log.Error("BuildCacheKey error",
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg MultiSet"),
				zap.Error(err),
				zap.String("key", key))
			continue
		}
		paris = append(paris, []byte(cacheKey))
		paris = append(paris, buf)
	}
	pipeline := c.client.Pipeline()
	err = pipeline.MSet(ctx, paris...).Err()
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
			c.log.Warn("redis expire is unsupported key type",
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg MultiSet"),
				zap.Any("type", reflect.TypeOf(paris[i])))
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
func (c *redisCache) MultiGet(ctx context.Context, keys []string, value interface{}) (err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg MultiGet"),
			)
			err = fmt.Errorf("panic in MultiGet: %v", r)
		}
	}()

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
		if errors.Is(err, CacheNotFound) {
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
			c.log.Error("unmarshal data error",
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg MultiGet"),
				zap.Error(err),
				zap.String("key", keys[i]),
				zap.String("cacheKey", cacheKeys[i]),
				zap.Any("type", reflect.TypeOf(value)))
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

// Del delete multiple values and update bloom filter
func (c *redisCache) Del(ctx context.Context, keys ...string) (err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg Del"),
			)
			err = fmt.Errorf("panic in Del: %v", r)
		}
	}()

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
	err = c.client.Del(ctx, cacheKeys...).Err()
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
func (c *redisCache) DelByPrefix(ctx context.Context, prefix string) (err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg DelByPrefix"),
			)
			err = fmt.Errorf("panic in DelByPrefix: %v", r)
		}
	}()

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
func (c *redisCache) SetCacheWithNotFound(ctx context.Context, key string) (err error) {
	// 添加panic处理机制
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			c.log.Error(
				fmt.Sprintf("panic recovered: %v\nstack: %s", r, string(debug.Stack())),
				requestIDField(ctx, "request_id"),
				zap.String("log_from", "Cache msg SetCacheWithNotFound"),
			)
			err = fmt.Errorf("panic in SetCacheWithNotFound: %v", r)
		}
	}()

	begin := time.Now()
	fields := []zap.Field{
		requestIDField(ctx, "request_id"),
		zap.String("log_from", "Cache msg SetCacheWithNotFound"),
	}

	// 添加到布隆过滤器（即使是空值也添加，防止缓存穿透）
	c.AddToBloomFilter(ctx, key)

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
