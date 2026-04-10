// Package goredis 是基于 github.com/go-redis/redis 封装的 Redis 客户端库
package goredis

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

// Client 是 Redis 客户端类型别名
type Client = redis.Client

const (
	// ErrRedisNotFound 表示在 Redis 中未找到指定的键
	ErrRedisNotFound = redis.Nil
	// DefaultRedisName 默认的 Redis 实例名称
	DefaultRedisName = "default"
)

// Init 连接到 Redis 服务器
// 支持的 DSN 格式:
// (1) 无密码无数据库: localhost:6379
// (2) 带密码和数据库: <user>:<pass>@localhost:6379/2
// (3) 完整 URL 格式: redis://default:123456@localhost:6379/0?max_retries=3
// 更多参数请参考 redis 源码中的 setupConnParams 函数
func Init(dsn string, opts ...Option) (*redis.Client, error) {
	// 初始化默认选项
	o := defaultOptions()
	o.apply(opts...)

	// 解析 Redis 连接选项
	opt, err := getRedisOpt(dsn, o)
	if err != nil {
		return nil, err
	}

	// 如果提供了单机选项则替换
	if o.singleOptions != nil {
		opt = o.singleOptions
	}

	// 创建 Redis 客户端
	rdb := redis.NewClient(opt)

	// 先启用 redisotel 追踪（它会在内部创建 Span）
	if o.tracerProvider != nil {
		err = redisotel.InstrumentTracing(rdb, redisotel.WithTracerProvider(o.tracerProvider))
		if err != nil {
			return nil, err
		}
	}

	// 再添加自定义 Hook：从 Context 提取 request_id 并设置到 Span 属性
	// 注意：必须在 InstrumentTracing 之后添加，这样 redisotel hook 先执行（创建 Span），我们的 Hook 后执行（设置属性）
	rdb.AddHook(&requestIDHook{})

	// 测试连接
	ctx, _ := context.WithTimeout(context.Background(), 15*time.Second) //nolint
	err = rdb.Ping(ctx).Err()

	return rdb, err
}

// InitSingle 连接到单机 Redis 实例
func InitSingle(addr string, password string, db int, opts ...Option) (*redis.Client, error) {
	// 初始化默认选项
	o := defaultOptions()
	o.apply(opts...)

	// 创建 Redis 连接选项
	opt := &redis.Options{
		Addr:         addr,         // Redis 地址
		Password:     password,     // Redis 密码
		DB:           db,           // 数据库编号
		DialTimeout:  o.dialTimeout,  // 连接超时时间
		ReadTimeout:  o.readTimeout,  // 读取超时时间
		WriteTimeout: o.writeTimeout, // 写入超时时间
		TLSConfig:    o.tlsConfig,    // TLS 配置
		PoolSize:          o.poolSize,        // 连接池大小
		MinIdleConns:      o.minIdleConns,    // 最小空闲连接数
		PoolTimeout:       o.poolTimeout,     // 连接池获取连接超时时间
		ConnMaxLifetime:   o.maxConnAge,      // 连接最大存活时间
		ConnMaxIdleTime:   o.idleTimeout,     // 连接最大空闲时间
	}

	// 如果未设置连接池大小，则使用默认值
	if opt.PoolSize == 0 {
		opt.PoolSize = 10
	}

	// 如果提供了单机选项则替换
	if o.singleOptions != nil {
		opt = o.singleOptions
	}

	// 创建 Redis 客户端
	rdb := redis.NewClient(opt)

	// 先启用 redisotel 追踪（它会在内部创建 Span）
	if o.tracerProvider != nil {
		err := redisotel.InstrumentTracing(rdb, redisotel.WithTracerProvider(o.tracerProvider))
		if err != nil {
			return nil, err
		}
	}

	// 再添加自定义 Hook：从 Context 提取 request_id 并设置到 Span 属性
	rdb.AddHook(&requestIDHook{})

	// 测试连接
	ctx, _ := context.WithTimeout(context.Background(), 15*time.Second) //nolint
	err := rdb.Ping(ctx).Err()

	return rdb, err
}

// InitSentinel 通过哨兵模式连接到 Redis，所有 Redis 实例使用相同的用户名和密码
func InitSentinel(masterName string, addrs []string, username string, password string, opts ...Option) (*redis.Client, error) {
	// 初始化默认选项
	o := defaultOptions()
	o.apply(opts...)

	// 创建 Redis 哨兵连接选项
	opt := &redis.FailoverOptions{
		MasterName:    masterName,     // 主节点名称
		SentinelAddrs: addrs,          // 哨兵地址列表
		Username:      username,       // 用户名
		Password:      password,       // 密码
		DialTimeout:   o.dialTimeout,  // 连接超时时间
		ReadTimeout:   o.readTimeout,  // 读取超时时间
		WriteTimeout:  o.writeTimeout, // 写入超时时间
		TLSConfig:     o.tlsConfig,    // TLS 配置
		PoolSize:          o.poolSize,        // 连接池大小
		MinIdleConns:      o.minIdleConns,    // 最小空闲连接数
		PoolTimeout:       o.poolTimeout,     // 连接池获取连接超时时间
		ConnMaxLifetime:   o.maxConnAge,      // 连接最大存活时间
		ConnMaxIdleTime:   o.idleTimeout,     // 连接最大空闲时间
	}

	// 如果未设置连接池大小，则使用默认值
	if opt.PoolSize == 0 {
		opt.PoolSize = 10
	}

	// 如果提供了哨兵选项则替换
	if o.sentinelOptions != nil {
		opt = o.sentinelOptions
	}

	// 创建 Redis 哨兵客户端
	rdb := redis.NewFailoverClient(opt)

	// 先启用 redisotel 追踪（它会在内部创建 Span）
	if o.tracerProvider != nil {
		err := redisotel.InstrumentTracing(rdb, redisotel.WithTracerProvider(o.tracerProvider))
		if err != nil {
			return nil, err
		}
	}

	// 再添加自定义 Hook：从 Context 提取 request_id 并设置到 Span 属性
	rdb.AddHook(&requestIDHook{})

	// 测试连接
	ctx, _ := context.WithTimeout(context.Background(), 15*time.Second) //nolint
	err := rdb.Ping(ctx).Err()

	return rdb, err
}

// InitCluster 通过集群模式连接到 Redis，所有 Redis 实例使用相同的用户名和密码
func InitCluster(addrs []string, username string, password string, opts ...Option) (*redis.ClusterClient, error) {
	// 初始化默认选项
	o := defaultOptions()
	o.apply(opts...)

	// 创建 Redis 集群连接选项
	opt := &redis.ClusterOptions{
		Addrs:        addrs,          // 集群节点地址列表
		Username:     username,       // 用户名
		Password:     password,       // 密码
		DialTimeout:  o.dialTimeout,  // 连接超时时间
		ReadTimeout:  o.readTimeout,  // 读取超时时间
		WriteTimeout: o.writeTimeout, // 写入超时时间
		TLSConfig:    o.tlsConfig,    // TLS 配置
		PoolSize:          o.poolSize,        // 连接池大小
		MinIdleConns:      o.minIdleConns,    // 最小空闲连接数
		PoolTimeout:       o.poolTimeout,     // 连接池获取连接超时时间
		ConnMaxLifetime:   o.maxConnAge,      // 连接最大存活时间
		ConnMaxIdleTime:   o.idleTimeout,     // 连接最大空闲时间
	}

	// 如果未设置连接池大小，则使用默认值
	if opt.PoolSize == 0 {
		opt.PoolSize = 10
	}

	// 如果提供了集群选项则替换
	if o.clusterOptions != nil {
		opt = o.clusterOptions
	}

	// 创建 Redis 集群客户端
	clusterRdb := redis.NewClusterClient(opt)

	// 先启用 redisotel 追踪（它会在内部创建 Span）
	if o.tracerProvider != nil {
		err := redisotel.InstrumentTracing(clusterRdb, redisotel.WithTracerProvider(o.tracerProvider))
		if err != nil {
			return nil, err
		}
	}

	// 再添加自定义 Hook：从 Context 提取 request_id 并设置到 Span 属性
	clusterRdb.AddHook(&requestIDHook{})

	// 测试连接，遍历所有主节点进行连接测试
	ctx, _ := context.WithTimeout(context.Background(), 15*time.Second) //nolint
	err := clusterRdb.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
		return client.Ping(ctx).Err()
	})

	return clusterRdb, err
}

// getRedisOpt 从 DSN 解析 Redis 连接选项
func getRedisOpt(dsn string, opts *options) (*redis.Options, error) {
	// 清理 DSN 字符串中的空格
	dsn = strings.ReplaceAll(dsn, " ", "")
	
	// 如果 DSN 长度大于 8 且末尾不包含 "/"，则默认使用数据库 0
	if len(dsn) > 8 {
		if !strings.Contains(dsn[len(dsn)-3:], "/") {
			dsn += "/0" // 默认使用 db 0
		}

		// 如果不是以 redis:// 或 rediss:// 开头，则添加 redis:// 前缀
		if dsn[:8] != "redis://" && dsn[:9] != "rediss://" {
			dsn = "redis://" + dsn
		}
	}

	// 解析 URL 格式的 DSN
	redisOpts, err := redis.ParseURL(dsn)
	if err != nil {
		return nil, err
	}

	// 应用自定义配置选项
	if opts.dialTimeout > 0 {
		redisOpts.DialTimeout = opts.dialTimeout
	}
	if opts.readTimeout > 0 {
		redisOpts.ReadTimeout = opts.readTimeout
	}
	if opts.writeTimeout > 0 {
		redisOpts.WriteTimeout = opts.writeTimeout
	}
	if opts.tlsConfig != nil {
		redisOpts.TLSConfig = opts.tlsConfig
	}
	if opts.poolSize > 0 {
		redisOpts.PoolSize = opts.poolSize
	}
	if opts.minIdleConns > 0 {
		redisOpts.MinIdleConns = opts.minIdleConns
	}
	if opts.poolTimeout > 0 {
		redisOpts.PoolTimeout = opts.poolTimeout
	}
	if opts.maxConnAge > 0 {
		redisOpts.ConnMaxLifetime = opts.maxConnAge
	}
	if opts.idleTimeout > 0 {
		redisOpts.ConnMaxIdleTime = opts.idleTimeout
	}
	return redisOpts, nil
}

// Close 关闭 Redis 客户端连接
func Close(rdb *redis.Client) error {
	// 如果客户端为空，则直接返回
	if rdb == nil {
		return nil
	}

	// 关闭连接
	err := rdb.Close()
	
	// 如果错误是连接已关闭，则返回错误
	if err != nil && errors.Is(err, redis.ErrClosed) {
		return err
	}

	return nil
}

// CloseCluster 关闭 Redis 集群客户端连接
func CloseCluster(clusterRdb *redis.ClusterClient) error {
	// 如果客户端为空，则直接返回
	if clusterRdb == nil {
		return nil
	}

	// 关闭连接
	err := clusterRdb.Close()
	
	// 如果错误是连接已关闭，则返回错误
	if err != nil && errors.Is(err, redis.ErrClosed) {
		return err
	}

	return nil
}