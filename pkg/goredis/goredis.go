// Package goredis 是基于 github.com/go-redis/redis 封装的 Redis 客户端库
package goredis

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
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
	// initTimeout 初始化阶段的超时时间
	initTimeout = 15 * time.Second
)

// setupClient 为 Redis 客户端执行通用初始化：追踪挂载、Hook 注册、连接测试、Lua 探测
func setupClient(ctx context.Context, client redis.UniversalClient, o *options) error {
	// 挂载 OpenTelemetry 追踪（它会在内部创建 Span）
	if o.tracerProvider != nil {
		if err := redisotel.InstrumentTracing(client, redisotel.WithTracerProvider(o.tracerProvider)); err != nil {
			return err
		}
	}

	// 添加自定义 Hook：从 Context 提取 request_id 并设置到 Span 属性
	// 注意：必须在 InstrumentTracing 之后添加，这样 redisotel hook 先执行（创建 Span），我们的 Hook 后执行（设置属性）
	hook := &requestIDHook{extractor: o.requestIDExtractor}
	client.AddHook(hook)

	// 测试连接
	ctx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return err
	}

	// 探测 Lua 脚本通道是否可用
	probeLuaScriptChannel(ctx, client)

	return nil
}

// probeLuaScriptChannel 通过 EVAL "return 1" 探测 Lua 通道是否可用
// 注：redsync 的脚本加载由 go-redis 的 NOSCRIPT fallback 保证，不依赖本探测
func probeLuaScriptChannel(ctx context.Context, client redis.Cmdable) {
	if err := client.Eval(ctx, "return 1", nil).Err(); err != nil {
		log.Printf("[goredis] lua channel probe failed: %v", err)
	}
}

// ============================================================================
// Init 系列函数
//
// 选项应用优先级（所有 Init 函数统一）：
//
//  1. defaultOptions() 提供零值基底
//  2. o.apply(opts...) 执行所有 With* 选项
//     - WithXxxOptions 将传入 Options 的非零字段展开到 o，并置位 xxxSet（确保后续步骤均能识别）
//     - 其他 With* 直接设置字段并置位 xxxSet
//  3. baseBuilder / getRedisOpt 构造基础 Options
//     - Init 路径：DSN 解析值优先；xxxSet=true 的字段覆盖 DSN 值
//     - InitSingle/Sentinel/Cluster 路径：从 o 中读取所有字段
//  4. applyExplicitXxxOptions 将 xxxSet=true 的字段最终覆盖基础值
// ============================================================================

// Init 连接到 Redis 服务器
// 支持的 DSN 格式:
// (1) 无密码无数据库: localhost:6379
// (2) 带密码和数据库: <user>:<pass>@localhost:6379/2
// (3) 完整 URL 格式: redis://default:123456@localhost:6379/0?max_retries=3
// 更多参数请参考 redis 源码中的 setupConnParams 函数
func Init(dsn string, opts ...Option) (*redis.Client, error) {
	o := defaultOptions()
	o.apply(opts...)

	opt, err := getRedisOpt(dsn, o)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(opt)

	ctx := context.Background()
	if err := setupClient(ctx, rdb, o); err != nil {
		_ = rdb.Close()
		return nil, err
	}

	return rdb, nil
}

// InitSingle 连接到单机 Redis 实例
func InitSingle(addr string, password string, db int, opts ...Option) (*redis.Client, error) {
	o := defaultOptions()
	o.apply(opts...)

	opt := buildRedisBaseOptions(addr, password, db, o)
	applyExplicitRedisOptions(opt, o)

	rdb := redis.NewClient(opt)

	ctx := context.Background()
	if err := setupClient(ctx, rdb, o); err != nil {
		_ = rdb.Close()
		return nil, err
	}

	return rdb, nil
}

// InitSentinel 通过哨兵模式连接到 Redis，所有 Redis 实例使用相同的用户名和密码
func InitSentinel(masterName string, addrs []string, username string, password string, opts ...Option) (*redis.Client, error) {
	o := defaultOptions()
	o.apply(opts...)

	opt := buildFailoverBaseOptions(masterName, addrs, username, password, o)
	applyExplicitFailoverOptions(opt, o)

	rdb := redis.NewFailoverClient(opt)

	ctx := context.Background()
	if err := setupClient(ctx, rdb, o); err != nil {
		_ = rdb.Close()
		return nil, err
	}

	return rdb, nil
}

// InitCluster 通过集群模式连接到 Redis，所有 Redis 实例使用相同的用户名和密码
func InitCluster(addrs []string, username string, password string, opts ...Option) (*redis.ClusterClient, error) {
	o := defaultOptions()
	o.apply(opts...)

	opt := buildClusterBaseOptions(addrs, username, password, o)
	applyExplicitClusterOptions(opt, o)

	clusterRdb := redis.NewClusterClient(opt)

	// 集群模式：追踪挂载、Hook 注册
	if o.tracerProvider != nil {
		if err := redisotel.InstrumentTracing(clusterRdb, redisotel.WithTracerProvider(o.tracerProvider)); err != nil {
			return nil, err
		}
	}
	hook := &requestIDHook{extractor: o.requestIDExtractor}
	clusterRdb.AddHook(hook)

	// 测试连接，遍历所有分片（含主从）进行连接测试
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()
	err := clusterRdb.ForEachShard(ctx, func(ctx context.Context, client *redis.Client) error {
		return client.Ping(ctx).Err()
	})
	if err != nil {
		_ = clusterRdb.Close()
		return nil, err
	}

	// 探测 Lua 脚本通道（集群模式下只需在任一节点执行即可）
	probeLuaScriptChannel(ctx, clusterRdb)

	return clusterRdb, nil
}

// ============================================================================
// DSN 解析
// ============================================================================

// getRedisOpt 从 DSN 解析 Redis 连接选项
// 支持三种 DSN 格式:
//   - host:port（自动补 redis:// 前缀）
//   - :password@host:port/db（自动补 redis:// 前缀）
//   - redis://user:password@host:port/db?query（完整 URL）
//
// 选项应用优先级：DSN 解析值 → With* 显式覆盖
func getRedisOpt(dsn string, opts *options) (*redis.Options, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("goredis: dsn 不能为空")
	}

	// 1. 非完整 URL 格式（无 redis:// 或 rediss:// 前缀）→ 拼接前缀
	if !strings.HasPrefix(dsn, "redis://") && !strings.HasPrefix(dsn, "rediss://") {
		dsn = "redis://" + dsn
	}

	// 2. 用 net/url 解析，判断 Path 是否包含 /db
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("goredis: 解析 DSN 失败: %w", err)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/0"
	}

	// 3. 重新序列化为完整 URL，交给 redis.ParseURL
	dsn = u.String()
	redisOpts, err := redis.ParseURL(dsn)
	if err != nil {
		return nil, err
	}

	// 4. With* 显式设置的字段覆盖 DSN 解析值
	applyExplicitRedisOptions(redisOpts, opts)

	return redisOpts, nil
}

// ============================================================================
// Close
// ============================================================================

// Close 关闭 Redis 客户端连接（幂等：重复调用不会报错）
func Close(rdb *redis.Client) error {
	if rdb == nil {
		return nil
	}
	if err := rdb.Close(); err != nil && !errors.Is(err, redis.ErrClosed) {
		return err
	}
	return nil
}

// CloseCluster 关闭 Redis 集群客户端连接（幂等：重复调用不会报错）
func CloseCluster(clusterRdb *redis.ClusterClient) error {
	if clusterRdb == nil {
		return nil
	}
	if err := clusterRdb.Close(); err != nil && !errors.Is(err, redis.ErrClosed) {
		return err
	}
	return nil
}

// ============================================================================
// Base Builders：从参数构造基础 options（仅设置传入参数，其他为零值）
// ============================================================================

// buildRedisBaseOptions 从 addr/password/db 参数构造 redis.Options 基础结构
func buildRedisBaseOptions(addr, password string, db int, o *options) *redis.Options {
	return &redis.Options{
		Addr:            addr,
		Password:        password,
		DB:              db,
		PoolSize:        o.poolSize,
		MinIdleConns:    o.minIdleConns,
		PoolTimeout:     o.poolTimeout,
		ConnMaxLifetime: o.maxConnAge,
		ConnMaxIdleTime: o.idleTimeout,
		DialTimeout:     o.dialTimeout,
		ReadTimeout:     o.readTimeout,
		WriteTimeout:    o.writeTimeout,
		TLSConfig:       o.tlsConfig,
		MaxRetries:      o.maxRetries,
		MinRetryBackoff: o.minRetryBackoff,
		MaxRetryBackoff: o.maxRetryBackoff,
		Network:         o.network,
		Username:        o.username,
		ClientName:      o.clientName,
		Protocol:        o.protocol,
		OnConnect:       o.onConnect,
		Dialer:          o.dialer,
	}
}

// buildFailoverBaseOptions 从哨兵参数构造 redis.FailoverOptions 基础结构
func buildFailoverBaseOptions(masterName string, addrs []string, username, password string, o *options) *redis.FailoverOptions {
	return &redis.FailoverOptions{
		MasterName:                masterName,
		SentinelAddrs:             addrs,
		Username:                  username,
		Password:                  password,
		SentinelUsername:          o.sentinelUsername,
		SentinelPassword:          o.sentinelPassword,
		UseDisconnectedReplicas:  o.useDisconnectedReplicas,
		PoolSize:                  o.poolSize,
		MinIdleConns:              o.minIdleConns,
		PoolTimeout:               o.poolTimeout,
		ConnMaxLifetime:           o.maxConnAge,
		ConnMaxIdleTime:           o.idleTimeout,
		DialTimeout:               o.dialTimeout,
		ReadTimeout:               o.readTimeout,
		WriteTimeout:              o.writeTimeout,
		TLSConfig:                 o.tlsConfig,
		MaxRetries:                o.maxRetries,
		MinRetryBackoff:           o.minRetryBackoff,
		MaxRetryBackoff:           o.maxRetryBackoff,
		ClientName:                o.clientName,
		Protocol:                  o.protocol,
		OnConnect:                 o.onConnect,
		Dialer:                    o.dialer,
	}
}

// buildClusterBaseOptions 从集群参数构造 redis.ClusterOptions 基础结构
func buildClusterBaseOptions(addrs []string, username, password string, o *options) *redis.ClusterOptions {
	return &redis.ClusterOptions{
		Addrs:                    addrs,
		Username:                 username,
		Password:                 password,
		ReadOnly:                 o.readOnly,
		RouteByLatency:           o.routeByLatency,
		RouteRandomly:            o.routeRandomly,
		MaxRedirects:             o.maxRedirects,
		PoolSize:                 o.poolSize,
		MinIdleConns:             o.minIdleConns,
		PoolTimeout:              o.poolTimeout,
		ConnMaxLifetime:          o.maxConnAge,
		ConnMaxIdleTime:          o.idleTimeout,
		DialTimeout:              o.dialTimeout,
		ReadTimeout:              o.readTimeout,
		WriteTimeout:             o.writeTimeout,
		TLSConfig:                o.tlsConfig,
		MaxRetries:               o.maxRetries,
		MinRetryBackoff:          o.minRetryBackoff,
		MaxRetryBackoff:          o.maxRetryBackoff,
		ClientName:               o.clientName,
		Protocol:                 o.protocol,
		OnConnect:                o.onConnect,
		Dialer:                   o.dialer,
	}
}

// ============================================================================
// Apply Explicit：将 With* 显式设置的字段覆盖基础值
// 与 options 中的 xxxSet 标志一一对应，不会遗漏字段
// ============================================================================

// applyExplicitRedisOptions 将 With* 显式设置的字段应用到 redis.Options
func applyExplicitRedisOptions(redisOpts *redis.Options, opts *options) {
	if opts.poolSizeSet {
		redisOpts.PoolSize = opts.poolSize
	}
	if opts.minIdleSet {
		redisOpts.MinIdleConns = opts.minIdleConns
	}
	if opts.maxConnAgeSet {
		redisOpts.ConnMaxLifetime = opts.maxConnAge
	}
	if opts.poolTimeoutSet {
		redisOpts.PoolTimeout = opts.poolTimeout
	}
	if opts.idleTimeoutSet {
		redisOpts.ConnMaxIdleTime = opts.idleTimeout
	}
	if opts.dialTimeoutSet {
		redisOpts.DialTimeout = opts.dialTimeout
	}
	if opts.readTimeoutSet {
		redisOpts.ReadTimeout = opts.readTimeout
	}
	if opts.writeTimeoutSet {
		redisOpts.WriteTimeout = opts.writeTimeout
	}
	if opts.tlsConfig != nil {
		redisOpts.TLSConfig = opts.tlsConfig
	}
	// 重试与网络
	if opts.maxRetriesSet {
		redisOpts.MaxRetries = opts.maxRetries
	}
	if opts.minRetryBackoffSet {
		redisOpts.MinRetryBackoff = opts.minRetryBackoff
	}
	if opts.maxRetryBackoffSet {
		redisOpts.MaxRetryBackoff = opts.maxRetryBackoff
	}
	if opts.networkSet {
		redisOpts.Network = opts.network
	}
	if opts.usernameSet {
		redisOpts.Username = opts.username
	}
	// 客户端
	if opts.clientNameSet {
		redisOpts.ClientName = opts.clientName
	}
	if opts.protocolSet {
		redisOpts.Protocol = opts.protocol
	}
	if opts.onConnectSet {
		redisOpts.OnConnect = opts.onConnect
	}
	if opts.dialerSet {
		redisOpts.Dialer = opts.dialer
	}
}

// applyExplicitFailoverOptions 将 With* 显式设置的字段应用到 redis.FailoverOptions
func applyExplicitFailoverOptions(opt *redis.FailoverOptions, opts *options) {
	// 公共字段
	if opts.poolSizeSet {
		opt.PoolSize = opts.poolSize
	}
	if opts.minIdleSet {
		opt.MinIdleConns = opts.minIdleConns
	}
	if opts.maxConnAgeSet {
		opt.ConnMaxLifetime = opts.maxConnAge
	}
	if opts.poolTimeoutSet {
		opt.PoolTimeout = opts.poolTimeout
	}
	if opts.idleTimeoutSet {
		opt.ConnMaxIdleTime = opts.idleTimeout
	}
	if opts.dialTimeoutSet {
		opt.DialTimeout = opts.dialTimeout
	}
	if opts.readTimeoutSet {
		opt.ReadTimeout = opts.readTimeout
	}
	if opts.writeTimeoutSet {
		opt.WriteTimeout = opts.writeTimeout
	}
	if opts.tlsConfig != nil {
		opt.TLSConfig = opts.tlsConfig
	}
	// 重试与网络
	if opts.maxRetriesSet {
		opt.MaxRetries = opts.maxRetries
	}
	if opts.minRetryBackoffSet {
		opt.MinRetryBackoff = opts.minRetryBackoff
	}
	if opts.maxRetryBackoffSet {
		opt.MaxRetryBackoff = opts.maxRetryBackoff
	}
	// 哨兵专有
	if opts.sentinelUsername != "" {
		opt.SentinelUsername = opts.sentinelUsername
	}
	if opts.sentinelPassword != "" {
		opt.SentinelPassword = opts.sentinelPassword
	}
	if opts.useDisconnectedReplicas {
		opt.UseDisconnectedReplicas = true
	}
	// 客户端
	if opts.clientNameSet {
		opt.ClientName = opts.clientName
	}
	if opts.protocolSet {
		opt.Protocol = opts.protocol
	}
	if opts.onConnectSet {
		opt.OnConnect = opts.onConnect
	}
	if opts.dialerSet {
		opt.Dialer = opts.dialer
	}
}

// applyExplicitClusterOptions 将 With* 显式设置的字段应用到 redis.ClusterOptions
func applyExplicitClusterOptions(opt *redis.ClusterOptions, opts *options) {
	// 公共字段
	if opts.poolSizeSet {
		opt.PoolSize = opts.poolSize
	}
	if opts.minIdleSet {
		opt.MinIdleConns = opts.minIdleConns
	}
	if opts.maxConnAgeSet {
		opt.ConnMaxLifetime = opts.maxConnAge
	}
	if opts.poolTimeoutSet {
		opt.PoolTimeout = opts.poolTimeout
	}
	if opts.idleTimeoutSet {
		opt.ConnMaxIdleTime = opts.idleTimeout
	}
	if opts.dialTimeoutSet {
		opt.DialTimeout = opts.dialTimeout
	}
	if opts.readTimeoutSet {
		opt.ReadTimeout = opts.readTimeout
	}
	if opts.writeTimeoutSet {
		opt.WriteTimeout = opts.writeTimeout
	}
	if opts.tlsConfig != nil {
		opt.TLSConfig = opts.tlsConfig
	}
	// 重试与网络
	if opts.maxRetriesSet {
		opt.MaxRetries = opts.maxRetries
	}
	if opts.minRetryBackoffSet {
		opt.MinRetryBackoff = opts.minRetryBackoff
	}
	if opts.maxRetryBackoffSet {
		opt.MaxRetryBackoff = opts.maxRetryBackoff
	}
	// 集群专有
	if opts.readOnly {
		opt.ReadOnly = true
	}
	if opts.routeByLatency {
		opt.RouteByLatency = true
	}
	if opts.routeRandomly {
		opt.RouteRandomly = true
	}
	if opts.maxRedirects > 0 {
		opt.MaxRedirects = opts.maxRedirects
	}
	// 客户端
	if opts.clientNameSet {
		opt.ClientName = opts.clientName
	}
	if opts.protocolSet {
		opt.Protocol = opts.protocol
	}
	if opts.onConnectSet {
		opt.OnConnect = opts.onConnect
	}
	if opts.dialerSet {
		opt.Dialer = opts.dialer
	}
}
