package database

import (
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/goredis"
	"github.com/18721889353/sunshine/pkg/tracer"

	"github.com/18721889353/sunshine/internal/config"
)

var (
	// ErrCacheNotFound 未命中缓存
	ErrCacheNotFound = goredis.ErrRedisNotFound
)

var (
	redisCli     *goredis.Client
	redisCliOnce sync.Once

	cacheType     *CacheType
	cacheTypeOnce sync.Once
)

// CacheType 缓存类型
type CacheType struct {
	CType string          // 缓存类型 memory 或 redis
	Rdb   *goredis.Client // 如果 CType=redis，则 Rdb 不能为空
}

// InitCache 初始化缓存
func InitCache(cType string) {
	cacheType = &CacheType{
		CType: cType,
	}

	if cType == "redis" {
		cacheType.Rdb = GetRedisCli()
	}
}

// GetCacheType 获取缓存类型
func GetCacheType() *CacheType {
	if cacheType == nil {
		cacheTypeOnce.Do(func() {
			InitCache(config.Get().App.CacheType)
		})
	}

	return cacheType
}

// InitRedis 连接 Redis
func InitRedis() {
	redisCfg := config.Get().Redis
	opts := []goredis.Option{
		goredis.WithDialTimeout(time.Duration(redisCfg.DialTimeout) * time.Second),   // 设置连接超时时间
		goredis.WithReadTimeout(time.Duration(redisCfg.ReadTimeout) * time.Second),   // 设置读取超时时间
		goredis.WithWriteTimeout(time.Duration(redisCfg.WriteTimeout) * time.Second), // 设置写入超时时间
		goredis.WithPoolSize(redisCfg.PoolSize),                                      // 设置连接池大小
		goredis.WithMinIdleConns(redisCfg.MinIdleConns),                              // 设置最小空闲连接数
		goredis.WithMaxConnAge(time.Duration(redisCfg.MaxConnAge) * time.Second),     // 设置连接最大存活时间
		goredis.WithPoolTimeout(time.Duration(redisCfg.PoolTimeout) * time.Second),   // 设置连接池超时时间
		goredis.WithIdleTimeout(time.Duration(redisCfg.IdleTimeout) * time.Second),   // 设置连接最大空闲时间
	}
	if config.Get().App.EnableTrace {
		opts = append(opts, goredis.WithTracing(tracer.GetProvider())) // 启用追踪
	}

	var err error
	redisCli, err = goredis.Init(redisCfg.Dsn, opts...)
	if err != nil {
		panic("goredis.Init error: " + err.Error())
	}
}

// GetRedisCli 获取 Redis 客户端
func GetRedisCli() *goredis.Client {
	if redisCli == nil {
		redisCliOnce.Do(func() {
			InitRedis()
		})
	}

	return redisCli
}

// CloseRedis 关闭 Redis 连接
func CloseRedis() error {
	return goredis.Close(redisCli)
}
