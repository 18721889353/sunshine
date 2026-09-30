// Package goredis 是基于 github.com/go-redis/redis 封装的 Redis 客户端库
package goredis

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/18721889353/sunshine/pkg/logger"
)

// Client 是 Redis 客户端类型别名
type Client = redis.Client

const (
	// ErrRedisNotFound 表示在 Redis 中未找到指定的键
	ErrRedisNotFound = redis.Nil
	// DefaultRedisName 默认的 Redis 实例名称
	// 注：本包内不直接引用，保留为导出符号供上层业务作为默认实例名使用（删除会破坏兼容）
	DefaultRedisName = "default"
	// initTimeout 初始化阶段（连接测试）的超时时间
	initTimeout = 15 * time.Second
	// probeTimeout Lua 通道探测的独立超时时间
	// 不复用连接测试的 pingCtx：若 ForEachShard 等已消耗大部分 initTimeout，
	// 探测会因剩余时间不足而误报告警（噪音日志）
	probeTimeout = 3 * time.Second
)

// pingFunc 连接测试策略：单机直接 Ping，集群遍历所有分片 Ping
// 抽象为策略注入 setupClient，保证单机/集群共享同一套挂载与探测流程（统一初始化路径）
type pingFunc func(ctx context.Context, client redis.UniversalClient) error

// pingSingle 单机连接测试策略：直接对客户端执行 Ping
func pingSingle(ctx context.Context, client redis.UniversalClient) error {
	return client.Ping(ctx).Err()
}

// setupClient 为 Redis 客户端执行通用初始化：追踪/指标挂载、Hook 注册、连接测试（ping 策略）、Lua 探测
func setupClient(ctx context.Context, client redis.UniversalClient, o *options, ping pingFunc) error {
	// 挂载 OpenTelemetry 追踪（它会在内部创建 Span）
	if o.tracerProvider != nil {
		if err := redisotel.InstrumentTracing(client, redisotel.WithTracerProvider(o.tracerProvider)); err != nil {
			return err
		}
	}

	// 挂载 OpenTelemetry 指标（命令延迟、连接池水位等，需 WithMetrics 开启）
	// WithMeterProvider 指定了独立 Provider 时优先使用，否则回退到全局 MeterProvider
	if o.enableMetrics {
		metricOpts := make([]redisotel.MetricsOption, 0, 1)
		if o.meterProvider != nil {
			metricOpts = append(metricOpts, redisotel.WithMeterProvider(o.meterProvider))
		}
		if err := redisotel.InstrumentMetrics(client, metricOpts...); err != nil {
			return err
		}
	}

	// 添加自定义 Hook：从 Context 提取 request_id（全项目约定的 logger.ContextKeyRequestID）并设置到 Span 属性
	// 注意：必须在 InstrumentTracing 之后添加，这样 redisotel hook 先执行（创建 Span），我们的 Hook 后执行（设置属性）
	client.AddHook(&requestIDHook{})

	// 测试连接（由调用方注入 ping 策略：单机直接 Ping，集群 ForEachShard 遍历分片）
	// 超时可用 WithInitTimeout 覆盖（默认 initTimeout 常量）
	pingCtx, cancel := context.WithTimeout(ctx, o.initTimeout)
	defer cancel()
	if err := ping(pingCtx, client); err != nil {
		return err
	}

	// 探测 Lua 脚本通道是否可用（独立短超时，避免复用已消耗的 pingCtx 产生误报告警；可用 WithProbeTimeout 覆盖）
	probeCtx, probeCancel := context.WithTimeout(ctx, o.probeTimeout)
	defer probeCancel()
	probeLuaScriptChannel(probeCtx, client)

	return nil
}

// probeLuaScriptChannel 通过 EVAL "return 1" 探测 Lua 通道是否可用
// 注：redsync 的脚本加载由 go-redis 的 NOSCRIPT fallback 保证，不依赖本探测
// 探测失败仅告警（走全局 pkg/logger），不中断初始化（启动健壮性原则）
func probeLuaScriptChannel(ctx context.Context, client redis.Cmdable) {
	if err := client.Eval(ctx, "return 1", nil).Err(); err != nil {
		logWarn(ctx, "goredis: Lua 脚本通道探测失败，不影响初始化", logger.Err(err))
	}
}

// defaultContext 降级 nil Context 为 context.Background()（Ctx 变体的 nil 防御）
func defaultContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// closeAfterInitFail 初始化失败后清理已创建的客户端
// 关闭出错仅告警，不掩盖初始化的原始错误
func closeAfterInitFail(ctx context.Context, closer interface{ Close() error }) {
	if err := closer.Close(); err != nil {
		logWarn(ctx, "goredis: 初始化失败后关闭客户端出错", logger.Err(err))
	}
}

// ============================================================================
// Init 系列函数
//
// 选项应用优先级（所有 Init 函数统一）：
//
//  1. defaultOptions() 提供零值基底
//  2. o.apply(opts...) 执行所有 With* 选项（nil Option 跳过）
//     - WithXxxOptions 将传入 Options 的非零字段展开到 o，并置位 xxxSet（确保后续步骤均能识别）
//     - 其他 With* 直接设置字段并置位 xxxSet（零值也置位，支持显式清空）
//  3. o.validate() 校验选项组合，配置类错误在此快速失败
//  4. baseBuilder / getRedisOpt 构造基础 Options
//     - Init 路径：DSN 解析值优先；xxxSet=true 的字段覆盖 DSN 值
//     - InitSingle/Sentinel/Cluster 路径：从 o 中读取所有字段
//  5. applyExplicitXxxOptions 将 xxxSet=true 的字段最终覆盖基础值
//
// 无 Context 的变体统一使用 context.Background()；
// 需要控制初始化超时/取消请使用 InitWithContext 等 Ctx 变体。
// ============================================================================

// Init 连接到 Redis 服务器（内部使用 context.Background()，需控制超时/取消请用 InitWithContext）
// 支持的 DSN 格式:
// (1) 无密码无数据库: localhost:6379
// (2) 带密码和数据库: <user>:<pass>@localhost:6379/2
// (3) 完整 URL 格式: redis://default:123456@localhost:6379/0?max_retries=3
// 更多参数请参考 redis 源码中的 setupConnParams 函数
func Init(dsn string, opts ...Option) (*redis.Client, error) {
	return InitWithContext(context.Background(), dsn, opts...)
}

// InitWithContext 连接到 Redis 服务器，支持外部 Context 控制初始化的超时与取消
func InitWithContext(ctx context.Context, dsn string, opts ...Option) (*redis.Client, error) {
	ctx = defaultContext(ctx)
	o := defaultOptions()
	o.apply(opts...)
	if err := o.validate(); err != nil {
		return nil, err
	}

	opt, err := getRedisOpt(dsn, o)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(opt)
	if err := setupClient(ctx, rdb, o, pingSingle); err != nil {
		closeAfterInitFail(ctx, rdb)
		return nil, err
	}

	return rdb, nil
}

// InitSingle 连接到单机 Redis 实例（内部使用 context.Background()，需控制超时/取消请用 InitSingleWithContext）
func InitSingle(addr string, password string, db int, opts ...Option) (*redis.Client, error) {
	return InitSingleWithContext(context.Background(), addr, password, db, opts...)
}

// InitSingleWithContext 连接到单机 Redis 实例，支持外部 Context 控制初始化的超时与取消
func InitSingleWithContext(ctx context.Context, addr string, password string, db int, opts ...Option) (*redis.Client, error) {
	ctx = defaultContext(ctx)
	o := defaultOptions()
	o.apply(opts...)
	if err := o.validate(); err != nil {
		return nil, err
	}

	opt := buildRedisBaseOptions(addr, password, db, o)
	applyExplicitRedisOptions(opt, o)

	rdb := redis.NewClient(opt)
	if err := setupClient(ctx, rdb, o, pingSingle); err != nil {
		closeAfterInitFail(ctx, rdb)
		return nil, err
	}

	return rdb, nil
}

// InitSentinel 通过哨兵模式连接到 Redis，所有 Redis 实例使用相同的用户名和密码
// （内部使用 context.Background()，需控制超时/取消请用 InitSentinelWithContext）
func InitSentinel(masterName string, addrs []string, username string, password string, opts ...Option) (*redis.Client, error) {
	return InitSentinelWithContext(context.Background(), masterName, addrs, username, password, opts...)
}

// InitSentinelWithContext 通过哨兵模式连接到 Redis，支持外部 Context 控制初始化的超时与取消
func InitSentinelWithContext(ctx context.Context, masterName string, addrs []string, username string, password string, opts ...Option) (*redis.Client, error) {
	ctx = defaultContext(ctx)
	o := defaultOptions()
	o.apply(opts...)
	if err := o.validate(); err != nil {
		return nil, err
	}

	opt := buildFailoverBaseOptions(masterName, addrs, username, password, o)
	applyExplicitFailoverOptions(opt, o)

	rdb := redis.NewFailoverClient(opt)
	if err := setupClient(ctx, rdb, o, pingSingle); err != nil {
		closeAfterInitFail(ctx, rdb)
		return nil, err
	}

	return rdb, nil
}

// InitCluster 通过集群模式连接到 Redis，所有 Redis 实例使用相同的用户名和密码
// （内部使用 context.Background()，需控制超时/取消请用 InitClusterWithContext）
func InitCluster(addrs []string, username string, password string, opts ...Option) (*redis.ClusterClient, error) {
	return InitClusterWithContext(context.Background(), addrs, username, password, opts...)
}

// InitClusterWithContext 通过集群模式连接到 Redis，支持外部 Context 控制初始化的超时与取消
func InitClusterWithContext(ctx context.Context, addrs []string, username string, password string, opts ...Option) (*redis.ClusterClient, error) {
	ctx = defaultContext(ctx)
	o := defaultOptions()
	o.apply(opts...)
	if err := o.validate(); err != nil {
		return nil, err
	}

	opt := buildClusterBaseOptions(addrs, username, password, o)
	applyExplicitClusterOptions(opt, o)

	clusterRdb := redis.NewClusterClient(opt)

	// 集群连接测试策略：遍历所有分片（含主从）进行连接测试
	// 挂载/Hook/探测流程与其他 Init 完全共享 setupClient，避免两套逻辑漂移
	clusterPing := func(pingCtx context.Context, _ redis.UniversalClient) error {
		return clusterRdb.ForEachShard(pingCtx, func(shardCtx context.Context, client *redis.Client) error {
			return client.Ping(shardCtx).Err()
		})
	}
	if err := setupClient(ctx, clusterRdb, o, clusterPing); err != nil {
		closeAfterInitFail(ctx, clusterRdb)
		return nil, err
	}

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

	// 2. 缺少 /db 路径时补 /0（仅做最小字符串拼接，避免 net/url 重新编码破坏密码中的特殊字符）
	dsn = ensureDSNPath(dsn)

	// 3. 交给 redis.ParseURL 解析
	redisOpts, err := redis.ParseURL(dsn)
	if err != nil {
		// net/url 的错误文本会回显原始 URL（含密码），直接 %w 会让密码进入日志与错误链，
		// 这里用脱敏后的文本作为 Error()，同时保留原始错误供 errors.Is/As 判定
		return nil, &redactedDSNError{
			err: err,
			msg: fmt.Sprintf("goredis: 解析 DSN 失败: %s", redactDSN(err.Error())),
		}
	}

	// 4. With* 显式设置的字段覆盖 DSN 解析值
	applyExplicitRedisOptions(redisOpts, opts)

	return redisOpts, nil
}

// ensureDSNPath 在 DSN 缺少 /db 路径时补全为 /0
// 仅对缺失场景做最小拼接，不做 net/url 的 parse → String 往返，
// 避免用户密码中的特殊字符（如 %2B、+ 等）被重新编码后改变语义
func ensureDSNPath(dsn string) string {
	schemeIdx := strings.Index(dsn, "://")
	if schemeIdx < 0 {
		return dsn
	}

	// 分离 query 部分（? 之后不参与路径判断）
	authority := dsn[schemeIdx+3:]
	query := ""
	if qIdx := strings.Index(authority, "?"); qIdx >= 0 {
		authority, query = authority[:qIdx], authority[qIdx:]
	}

	// authority 中第一个 / 即 path 起点（userinfo 不允许出现未转义的 /）
	pathIdx := strings.Index(authority, "/")
	prefix := dsn[:schemeIdx+3]
	switch {
	case pathIdx < 0:
		// 形如 redis://host:6379 → redis://host:6379/0
		return prefix + authority + "/0" + query
	case pathIdx == len(authority)-1:
		// 形如 redis://host:6379/ → redis://host:6379/0
		return prefix + authority + "0" + query
	default:
		// 已有 /db 路径，保持原样
		return dsn
	}
}

// dsnPasswordRe 匹配 URL userinfo 中的密码段：scheme://user:password@host
// 密码在 URL 中必须以百分号编码（不允许空白与 @），因此用 [^\s@] 精确圈定，
// 避免跨 token 误伤错误消息里的其他内容
var dsnPasswordRe = regexp.MustCompile(`(://[^\s/@]*:)[^\s@]*(@)`)

// redactDSN 把错误文本中的 DSN 密码替换为 ***
// redis.ParseURL 底层是 net/url，解析失败时错误文本会回显完整 URL（含密码）
func redactDSN(msg string) string {
	return dsnPasswordRe.ReplaceAllString(msg, "${1}***${2}")
}

// redactedDSNError 包装 DSN 解析错误：对外只暴露脱敏后的文本，同时保留原始错误链
// 仅脱敏不拆链的原因：errors.Is/As 仍需能识别底层 url 错误；而 Unwrap 出来的原始错误
// 只会被程序化遍历，所有面向日志的出口（Error()/errors.Join）都走本类型的脱敏文本
type redactedDSNError struct {
	err error
	msg string
}

// Error 返回脱敏后的错误消息（不含密码明文）
func (e *redactedDSNError) Error() string { return e.msg }

// Unwrap 保留原始错误链，供 errors.Is/As 判定
func (e *redactedDSNError) Unwrap() error { return e.err }

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

// shutdownPollInterval 关闭前轮询连接池状态的间隔
// 取值是精度与开销的权衡：过小则高频 shutdown（滚动发布/K8s 频繁重启）场景下
// PoolStats 查询（互斥锁级别）累计开销可观，过大则池清空后最多多等一个间隔；
// 50ms 下 Shutdown 的等待尾延迟可忽略（通常 < 1 个轮询周期）
const shutdownPollInterval = 50 * time.Millisecond

// Shutdown 优雅关闭 Redis 客户端
// 先轮询等待连接池中无命令占用连接（尽力等待飞行中的命令完成），再执行幂等关闭；
// 等待期间 ctx 取消/超时会立即转入关闭流程（避免连接泄漏）并返回 ctx 的错误
// 关闭阶段的告警统一走全局 pkg/logger（带 ctx，自动关联 request_id）
func Shutdown(ctx context.Context, rdb *redis.Client) error {
	if rdb == nil {
		return nil
	}
	ctx = defaultContext(ctx)

	if err := waitPoolDrained(ctx, rdb.PoolStats); err != nil {
		// 等待超时/取消后仍需强制关闭；Close 也失败时用 errors.Join 保留双错误，
		// 避免丢失“因 ctx 错误进入强制关闭”这一关键上下文
		if closeErr := Close(rdb); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	return Close(rdb)
}

// ShutdownCluster 优雅关闭 Redis 集群客户端（语义同 Shutdown）
func ShutdownCluster(ctx context.Context, clusterRdb *redis.ClusterClient) error {
	if clusterRdb == nil {
		return nil
	}
	ctx = defaultContext(ctx)

	if err := waitPoolDrained(ctx, clusterRdb.PoolStats); err != nil {
		// 同 Shutdown：Close 失败时保留“等待失败 + 关闭失败”双错误
		if closeErr := CloseCluster(clusterRdb); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	return CloseCluster(clusterRdb)
}

// waitPoolDrained 轮询等待连接池中所有连接归还
// 判定条件：TotalConns == IdleConns，即无连接被命令占用（尽力而为的飞行中命令等待）
func waitPoolDrained(ctx context.Context, statsFn func() *redis.PoolStats) error {
	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()

	for {
		stats := statsFn()
		if stats.TotalConns == stats.IdleConns {
			return nil
		}
		select {
		case <-ctx.Done():
			logWarn(ctx, "goredis: 等待连接池归还超时，将强制关闭客户端", logger.Err(ctx.Err()))
			return ctx.Err()
		case <-ticker.C:
		}
	}
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
		MasterName:              masterName,
		SentinelAddrs:           addrs,
		Username:                username,
		Password:                password,
		SentinelUsername:        o.sentinelUsername,
		SentinelPassword:        o.sentinelPassword,
		UseDisconnectedReplicas: o.useDisconnectedReplicas,
		PoolSize:                o.poolSize,
		MinIdleConns:            o.minIdleConns,
		PoolTimeout:             o.poolTimeout,
		ConnMaxLifetime:         o.maxConnAge,
		ConnMaxIdleTime:         o.idleTimeout,
		DialTimeout:             o.dialTimeout,
		ReadTimeout:             o.readTimeout,
		WriteTimeout:            o.writeTimeout,
		TLSConfig:               o.tlsConfig,
		MaxRetries:              o.maxRetries,
		MinRetryBackoff:         o.minRetryBackoff,
		MaxRetryBackoff:         o.maxRetryBackoff,
		ClientName:              o.clientName,
		Protocol:                o.protocol,
		OnConnect:               o.onConnect,
		Dialer:                  o.dialer,
	}
}

// buildClusterBaseOptions 从集群参数构造 redis.ClusterOptions 基础结构
func buildClusterBaseOptions(addrs []string, username, password string, o *options) *redis.ClusterOptions {
	return &redis.ClusterOptions{
		Addrs:           addrs,
		Username:        username,
		Password:        password,
		ReadOnly:        o.readOnly,
		RouteByLatency:  o.routeByLatency,
		RouteRandomly:   o.routeRandomly,
		MaxRedirects:    o.maxRedirects,
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
		ClientName:      o.clientName,
		Protocol:        o.protocol,
		OnConnect:       o.onConnect,
		Dialer:          o.dialer,
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
	if opts.tlsConfigSet {
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
	if opts.tlsConfigSet {
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
	// WithUsername / WithSingleOptions 展开的 Username 对哨兵模式生效
	//（buildFailoverBaseOptions 中的函数参数 username 作为兜底，显式选项优先）
	if opts.usernameSet {
		opt.Username = opts.username
	}
	// 哨兵专有：xxxSet=true 时覆盖（零值也生效，支持显式清空）
	if opts.sentinelUsernameSet {
		opt.SentinelUsername = opts.sentinelUsername
	}
	if opts.sentinelPasswordSet {
		opt.SentinelPassword = opts.sentinelPassword
	}
	if opts.useDisconnectedReplicasSet {
		opt.UseDisconnectedReplicas = opts.useDisconnectedReplicas
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
	if opts.tlsConfigSet {
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
	// WithUsername / WithSingleOptions 展开的 Username 对集群模式生效
	//（buildClusterBaseOptions 中的函数参数 username 作为兜底，显式选项优先）
	if opts.usernameSet {
		opt.Username = opts.username
	}
	// 集群专有：xxxSet=true 时覆盖（零值也生效，支持显式关闭）
	if opts.readOnlySet {
		opt.ReadOnly = opts.readOnly
	}
	if opts.routeByLatencySet {
		opt.RouteByLatency = opts.routeByLatency
	}
	if opts.routeRandomlySet {
		opt.RouteRandomly = opts.routeRandomly
	}
	if opts.maxRedirectsSet {
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
