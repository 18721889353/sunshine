package goredis

import (
	"crypto/tls"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Option 函数选项模式，用于设置 Redis 配置选项
type Option func(*options)

// options Redis 配置选项结构体
type options struct {
	dialTimeout  time.Duration // 连接超时时间
	readTimeout  time.Duration // 读取超时时间
	writeTimeout time.Duration // 写入超时时间
	tlsConfig    *tls.Config   // TLS 配置

	// 连接池配置
	poolSize     int           // 连接池大小
	minIdleConns int           // 最小空闲连接数
	maxConnAge   time.Duration // 连接最大存活时间
	poolTimeout  time.Duration // 从连接池获取连接的超时时间
	idleTimeout  time.Duration // 连接最大空闲时间

	// 注意: 此字段仅用于 Init 和 InitSingle，其他参数将被忽略
	singleOptions *redis.Options

	// 注意: 此字段仅用于 InitSentinel，其他参数将被忽略
	sentinelOptions *redis.FailoverOptions

	// 注意: 此字段仅用于 InitCluster，其他参数将被忽略
	clusterOptions *redis.ClusterOptions

	// 已废弃: 使用 tp 替代
	enableTrace    bool
	tracerProvider *trace.TracerProvider // 追踪提供者
}

// apply 应用配置选项
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultOptions 返回默认配置选项
func defaultOptions() *options {
	return &options{
		enableTrace: false, // 是否启用追踪，默认关闭
	}
}

// WithPoolSize 设置 Redis 连接池大小
func WithPoolSize(size int) Option {
	return func(o *options) {
		o.poolSize = size
		if o.singleOptions != nil {
			o.singleOptions.PoolSize = size
		}
		if o.sentinelOptions != nil {
			o.sentinelOptions.PoolSize = size
		}
		if o.clusterOptions != nil {
			o.clusterOptions.PoolSize = size
		}
	}
}

// WithMinIdleConns 设置最小空闲连接数
func WithMinIdleConns(minIdle int) Option {
	return func(o *options) {
		o.minIdleConns = minIdle
		if o.singleOptions != nil {
			o.singleOptions.MinIdleConns = minIdle
		}
		if o.sentinelOptions != nil {
			o.sentinelOptions.MinIdleConns = minIdle
		}
		if o.clusterOptions != nil {
			o.clusterOptions.MinIdleConns = minIdle
		}
	}
}

// WithMaxConnAge 设置连接最大存活时间
func WithMaxConnAge(age time.Duration) Option {
	return func(o *options) {
		o.maxConnAge = age
		if o.singleOptions != nil {
			o.singleOptions.ConnMaxLifetime = age
		}
		if o.sentinelOptions != nil {
			o.sentinelOptions.ConnMaxLifetime = age
		}
		if o.clusterOptions != nil {
			o.clusterOptions.ConnMaxLifetime = age
		}
	}
}

// WithPoolTimeout 设置从连接池获取连接的超时时间
func WithPoolTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.poolTimeout = timeout
		if o.singleOptions != nil {
			o.singleOptions.PoolTimeout = timeout
		}
		if o.sentinelOptions != nil {
			o.sentinelOptions.PoolTimeout = timeout
		}
		if o.clusterOptions != nil {
			o.clusterOptions.PoolTimeout = timeout
		}
	}
}

// WithIdleTimeout 设置连接最大空闲时间
func WithIdleTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.idleTimeout = timeout
		if o.singleOptions != nil {
			o.singleOptions.ConnMaxIdleTime = timeout
		}
		if o.sentinelOptions != nil {
			o.sentinelOptions.ConnMaxIdleTime = timeout
		}
		if o.clusterOptions != nil {
			o.clusterOptions.ConnMaxIdleTime = timeout
		}
	}
}

// WithEnableTrace 使用追踪功能，适用于 redis v8 版本
// 已废弃: 请使用 WithEnableTracer 替代
func WithEnableTrace() Option {
	return func(o *options) {
		o.enableTrace = true
	}
}

// WithTracing 设置 Redis 追踪提供者，适用于 redis v9 版本
func WithTracing(tp *trace.TracerProvider) Option {
	return func(o *options) {
		o.tracerProvider = tp
	}
}

// WithDialTimeout 设置连接超时时间
func WithDialTimeout(t time.Duration) Option {
	return func(o *options) {
		o.dialTimeout = t
	}
}

// WithReadTimeout 设置读取超时时间
func WithReadTimeout(t time.Duration) Option {
	return func(o *options) {
		o.readTimeout = t
	}
}

// WithWriteTimeout 设置写入超时时间
func WithWriteTimeout(t time.Duration) Option {
	return func(o *options) {
		o.writeTimeout = t
	}
}

// WithTLSConfig 设置 TLS 配置
func WithTLSConfig(c *tls.Config) Option {
	return func(o *options) {
		o.tlsConfig = c
	}
}

// WithSingleOptions 设置单机 Redis 选项
func WithSingleOptions(opt *redis.Options) Option {
	return func(o *options) {
		o.singleOptions = opt
	}
}

// WithSentinelOptions 设置 Redis 哨兵选项
func WithSentinelOptions(opt *redis.FailoverOptions) Option {
	return func(o *options) {
		o.sentinelOptions = opt
	}
}

// WithClusterOptions 设置 Redis 集群选项
func WithClusterOptions(opt *redis.ClusterOptions) Option {
	return func(o *options) {
		o.clusterOptions = opt
	}
}
