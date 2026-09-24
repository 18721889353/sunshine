package goredis

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/sdk/trace"
)

// RequestIDExtractor 从 Context 中提取 request_id 的函数签名
// 用于解耦 goredis 与 logger 包的依赖，上层注入具体提取逻辑
type RequestIDExtractor func(ctx context.Context) string

// Option 函数选项模式，用于设置 Redis 配置选项
type Option func(*options)

// options Redis 配置选项结构体
type options struct {
	// 连接与超时
	dialTimeout     time.Duration
	dialTimeoutSet  bool
	readTimeout     time.Duration
	readTimeoutSet  bool
	writeTimeout    time.Duration
	writeTimeoutSet bool
	tlsConfig       *tls.Config

	// 连接池
	poolSize       int
	poolSizeSet    bool
	minIdleConns   int
	minIdleSet     bool
	maxConnAge     time.Duration
	maxConnAgeSet  bool
	poolTimeout    time.Duration
	poolTimeoutSet bool
	idleTimeout    time.Duration
	idleTimeoutSet bool

	// 重试与网络
	maxRetries         int
	maxRetriesSet      bool
	minRetryBackoff    time.Duration
	minRetryBackoffSet bool
	maxRetryBackoff    time.Duration
	maxRetryBackoffSet bool
	network            string
	networkSet         bool
	username           string
	usernameSet        bool

	// 客户端
	clientName   string
	clientNameSet bool
	protocol     int
	protocolSet  bool
	onConnect    func(ctx context.Context, cn *redis.Conn) error
	onConnectSet bool
	dialer       func(ctx context.Context, network, addr string) (net.Conn, error)
	dialerSet    bool

	// 哨兵专用
	sentinelUsername        string
	sentinelPassword        string
	useDisconnectedReplicas bool

	// 集群专用
	readOnly       bool
	routeByLatency bool
	routeRandomly  bool
	maxRedirects   int

	// 追踪
	enableTrace    bool // Deprecated: 使用 WithTracing 替代
	tracerProvider *trace.TracerProvider

	// request_id 提取器
	requestIDExtractor RequestIDExtractor
}

// apply 应用配置选项
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultOptions 返回默认配置选项（零值 = 不覆盖 DSN 解析结果）
func defaultOptions() *options {
	return &options{
		enableTrace: false, // 是否启用追踪，默认关闭
	}
}

// WithPoolSize 设置 Redis 连接池大小
func WithPoolSize(size int) Option {
	return func(o *options) {
		o.poolSize = size
		o.poolSizeSet = true
	}
}

// WithMinIdleConns 设置最小空闲连接数
func WithMinIdleConns(minIdle int) Option {
	return func(o *options) {
		o.minIdleConns = minIdle
		o.minIdleSet = true
	}
}

// WithMaxConnAge 设置连接最大存活时间
func WithMaxConnAge(age time.Duration) Option {
	return func(o *options) {
		o.maxConnAge = age
		o.maxConnAgeSet = true
	}
}

// WithPoolTimeout 设置从连接池获取连接的超时时间
func WithPoolTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.poolTimeout = timeout
		o.poolTimeoutSet = true
	}
}

// WithIdleTimeout 设置连接最大空闲时间
func WithIdleTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.idleTimeout = timeout
		o.idleTimeoutSet = true
	}
}

// WithEnableTrace 启用追踪功能（已废弃，请使用 WithTracing 替代）
//
// Deprecated: 请使用 WithTracing 传入 TracerProvider 替代
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
		o.dialTimeoutSet = true
	}
}

// WithReadTimeout 设置读取超时时间
func WithReadTimeout(t time.Duration) Option {
	return func(o *options) {
		o.readTimeout = t
		o.readTimeoutSet = true
	}
}

// WithWriteTimeout 设置写入超时时间
func WithWriteTimeout(t time.Duration) Option {
	return func(o *options) {
		o.writeTimeout = t
		o.writeTimeoutSet = true
	}
}

// WithTLSConfig 设置 TLS 配置
func WithTLSConfig(c *tls.Config) Option {
	return func(o *options) {
		o.tlsConfig = c
	}
}

// WithMaxRetries 设置最大重试次数（0 = 禁用重试，-1 = 默认重试次数）
func WithMaxRetries(n int) Option {
	return func(o *options) {
		o.maxRetries = n
		o.maxRetriesSet = true
	}
}

// WithMinRetryBackoff 设置最小重试退避时间
func WithMinRetryBackoff(d time.Duration) Option {
	return func(o *options) {
		o.minRetryBackoff = d
		o.minRetryBackoffSet = true
	}
}

// WithMaxRetryBackoff 设置最大重试退避时间
func WithMaxRetryBackoff(d time.Duration) Option {
	return func(o *options) {
		o.maxRetryBackoff = d
		o.maxRetryBackoffSet = true
	}
}

// WithNetwork 设置网络类型（tcp / unix）
func WithNetwork(n string) Option {
	return func(o *options) {
		o.network = n
		o.networkSet = true
	}
}

// WithUsername 设置 Redis ACL 用户名
func WithUsername(u string) Option {
	return func(o *options) {
		o.username = u
		o.usernameSet = true
	}
}

// WithClientName 设置客户端名称（CLIENT SETNAME）
func WithClientName(name string) Option {
	return func(o *options) {
		o.clientName = name
		o.clientNameSet = true
	}
}

// WithProtocol 设置 RESP 协议版本（2 或 3）
func WithProtocol(v int) Option {
	return func(o *options) {
		o.protocol = v
		o.protocolSet = true
	}
}

// WithOnConnect 设置连接建立时的回调函数
func WithOnConnect(fn func(ctx context.Context, cn *redis.Conn) error) Option {
	return func(o *options) {
		o.onConnect = fn
		o.onConnectSet = true
	}
}

// WithDialer 设置自定义网络连接创建函数
func WithDialer(fn func(ctx context.Context, network, addr string) (net.Conn, error)) Option {
	return func(o *options) {
		o.dialer = fn
		o.dialerSet = true
	}
}

// WithSentinelUsername 设置哨兵 ACL 用户名
func WithSentinelUsername(u string) Option {
	return func(o *options) {
		o.sentinelUsername = u
	}
}

// WithSentinelPassword 设置哨兵密码
func WithSentinelPassword(p string) Option {
	return func(o *options) {
		o.sentinelPassword = p
	}
}

// WithUseDisconnectedReplicas 启用哨兵断连副本路由
func WithUseDisconnectedReplicas() Option {
	return func(o *options) {
		o.useDisconnectedReplicas = true
	}
}

// WithReadOnly 启用集群只读命令路由到副本节点
func WithReadOnly() Option {
	return func(o *options) {
		o.readOnly = true
	}
}

// WithRouteByLatency 启用集群延迟路由（自动启用 ReadOnly）
func WithRouteByLatency() Option {
	return func(o *options) {
		o.routeByLatency = true
	}
}

// WithRouteRandomly 启用集群随机路由（自动启用 ReadOnly）
func WithRouteRandomly() Option {
	return func(o *options) {
		o.routeRandomly = true
	}
}

// WithMaxRedirects 设置集群最大重定向次数
func WithMaxRedirects(n int) Option {
	return func(o *options) {
		o.maxRedirects = n
	}
}

// WithSingleOptions 设置单机 Redis 选项（展开到 options 中，With* 显式设置的字段优先）
//
// 支持的字段:
//	连接池: PoolSize, MinIdleConns, PoolTimeout, ConnMaxLifetime, ConnMaxIdleTime
//	超时:   DialTimeout, ReadTimeout, WriteTimeout, TLSConfig
//	重试:   MaxRetries, MinRetryBackoff, MaxRetryBackoff
//	网络:   Network, Username, ClientName, Protocol, OnConnect, Dialer
//
// 注意:
//   - Addr/Password/DB 请通过 InitSingle/Init 参数或 DSN 传入，本函数不读取这三个字段。
//   - MaxRetries=0 表示“未设置”，不会禁用重试。如需显式禁用重试，请使用 WithMaxRetries(0)。
//   - MinRetryBackoff/MaxRetryBackoff=0 同理，如需显式设置请使用对应的 With* 函数。
func WithSingleOptions(opt *redis.Options) Option {
	return func(o *options) {
		if opt == nil {
			return
		}
		// 检测 Addr/Password/DB 非零时警告（本函数不读取这三个字段）
		if opt.Addr != "" || opt.Password != "" || opt.DB != 0 {
			log.Printf("[goredis] WithSingleOptions 不读取 Addr/Password/DB 字段，请通过 InitSingle/Init 参数或 DSN 传入")
		}
		expandCommonOptions(o, commonOpts{
			PoolSize:        opt.PoolSize,
			MinIdleConns:    opt.MinIdleConns,
			ConnMaxLifetime: opt.ConnMaxLifetime,
			PoolTimeout:     opt.PoolTimeout,
			ConnMaxIdleTime: opt.ConnMaxIdleTime,
			DialTimeout:     opt.DialTimeout,
			ReadTimeout:     opt.ReadTimeout,
			WriteTimeout:    opt.WriteTimeout,
			TLSConfig:       opt.TLSConfig,
		})
		expandSingleOptions(o, opt)
	}
}

// WithSentinelOptions 设置 Redis 哨兵选项（展开到 options 中，With* 显式设置的字段优先）
func WithSentinelOptions(opt *redis.FailoverOptions) Option {
	return func(o *options) {
		if opt == nil {
			return
		}
		expandCommonOptions(o, commonOpts{
			PoolSize:        opt.PoolSize,
			MinIdleConns:    opt.MinIdleConns,
			ConnMaxLifetime: opt.ConnMaxLifetime,
			PoolTimeout:     opt.PoolTimeout,
			ConnMaxIdleTime: opt.ConnMaxIdleTime,
			DialTimeout:     opt.DialTimeout,
			ReadTimeout:     opt.ReadTimeout,
			WriteTimeout:    opt.WriteTimeout,
			TLSConfig:       opt.TLSConfig,
		})
		expandSentinelOptions(o, opt)
	}
}

// WithClusterOptions 设置 Redis 集群选项（展开到 options 中，With* 显式设置的字段优先）
func WithClusterOptions(opt *redis.ClusterOptions) Option {
	return func(o *options) {
		if opt == nil {
			return
		}
		expandCommonOptions(o, commonOpts{
			PoolSize:        opt.PoolSize,
			MinIdleConns:    opt.MinIdleConns,
			ConnMaxLifetime: opt.ConnMaxLifetime,
			PoolTimeout:     opt.PoolTimeout,
			ConnMaxIdleTime: opt.ConnMaxIdleTime,
			DialTimeout:     opt.DialTimeout,
			ReadTimeout:     opt.ReadTimeout,
			WriteTimeout:    opt.WriteTimeout,
			TLSConfig:       opt.TLSConfig,
		})
		expandClusterOptions(o, opt)
	}
}

// WithRequestIDExtractor 设置 request_id 提取器
// 用于从 Context 中提取 request_id 并自动附加到 Redis Span 属性
// 示例: WithRequestIDExtractor(func(ctx context.Context) string { return ctx.Value("request_id").(string) })
func WithRequestIDExtractor(fn RequestIDExtractor) Option {
	return func(o *options) {
		o.requestIDExtractor = fn
	}
}

// ============================================================================
// 内部展开函数
// ============================================================================

// setIntIfUnset 仅在 flag=false 且 v>0 时写入值并置位 flag
func setIntIfUnset(v int, val *int, flag *bool) {
	if !*flag && v > 0 {
		*val = v
		*flag = true
	}
}

// setDurIfUnset 仅在 flag=false 且 v>0 时写入值并置位 flag
func setDurIfUnset(v time.Duration, val *time.Duration, flag *bool) {
	if !*flag && v > 0 {
		*val = v
		*flag = true
	}
}

// setStrIfEmpty 仅在 val 为空且 v 非空时写入
func setStrIfEmpty(v string, val *string) {
	if *val == "" && v != "" {
		*val = v
	}
}

// expandCommonOptions 将公共字段（连接池 + 超时 + TLS）展开到 options 中
// 仅在用户未通过 With* 显式设置时（xxxSet=false）才写入，并同步置位 xxxSet
type commonOpts struct {
	PoolSize        int
	MinIdleConns    int
	ConnMaxLifetime time.Duration
	PoolTimeout     time.Duration
	ConnMaxIdleTime time.Duration
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	TLSConfig       *tls.Config
}

func expandCommonOptions(o *options, src commonOpts) {
	setIntIfUnset(src.PoolSize, &o.poolSize, &o.poolSizeSet)
	setIntIfUnset(src.MinIdleConns, &o.minIdleConns, &o.minIdleSet)
	setDurIfUnset(src.ConnMaxLifetime, &o.maxConnAge, &o.maxConnAgeSet)
	setDurIfUnset(src.PoolTimeout, &o.poolTimeout, &o.poolTimeoutSet)
	setDurIfUnset(src.ConnMaxIdleTime, &o.idleTimeout, &o.idleTimeoutSet)
	setDurIfUnset(src.DialTimeout, &o.dialTimeout, &o.dialTimeoutSet)
	setDurIfUnset(src.ReadTimeout, &o.readTimeout, &o.readTimeoutSet)
	setDurIfUnset(src.WriteTimeout, &o.writeTimeout, &o.writeTimeoutSet)
	if o.tlsConfig == nil && src.TLSConfig != nil {
		o.tlsConfig = src.TLSConfig
	}
}

// retryClientOpts 重试与客户端公共字段（供 expandRetryClientOptions 使用）
type retryClientOpts struct {
	MaxRetries      int
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration
	Network         string
	Username        string
	ClientName      string
	Protocol        int
	OnConnect       func(ctx context.Context, cn *redis.Conn) error
	Dialer          func(ctx context.Context, network, addr string) (net.Conn, error)
}

// expandRetryClientOptions 展开重试与客户端公共字段（供三种模式共用）
func expandRetryClientOptions(o *options, src retryClientOpts) {
	// 重试（MaxRetries 支持 0=禁用和 -1=默认，不能用 >0 判断）
	if !o.maxRetriesSet && src.MaxRetries != 0 {
		o.maxRetries = src.MaxRetries
		o.maxRetriesSet = true
	}
	setDurIfUnset(src.MinRetryBackoff, &o.minRetryBackoff, &o.minRetryBackoffSet)
	setDurIfUnset(src.MaxRetryBackoff, &o.maxRetryBackoff, &o.maxRetryBackoffSet)
	// 网络/连接
	if !o.networkSet && src.Network != "" {
		o.network = src.Network
		o.networkSet = true
	}
	if !o.usernameSet && src.Username != "" {
		o.username = src.Username
		o.usernameSet = true
	}
	if !o.clientNameSet && src.ClientName != "" {
		o.clientName = src.ClientName
		o.clientNameSet = true
	}
	if !o.protocolSet && src.Protocol > 0 {
		o.protocol = src.Protocol
		o.protocolSet = true
	}
	if !o.onConnectSet && src.OnConnect != nil {
		o.onConnect = src.OnConnect
		o.onConnectSet = true
	}
	if !o.dialerSet && src.Dialer != nil {
		o.dialer = src.Dialer
		o.dialerSet = true
	}
}

// expandSingleOptions 展开 redis.Options 专有字段
func expandSingleOptions(o *options, opt *redis.Options) {
	expandRetryClientOptions(o, retryClientOpts{
		MaxRetries: opt.MaxRetries, MinRetryBackoff: opt.MinRetryBackoff, MaxRetryBackoff: opt.MaxRetryBackoff,
		Network: opt.Network, Username: opt.Username, ClientName: opt.ClientName,
		Protocol: opt.Protocol, OnConnect: opt.OnConnect, Dialer: opt.Dialer,
	})
}

// expandSentinelOptions 展开 redis.FailoverOptions 专有字段
func expandSentinelOptions(o *options, opt *redis.FailoverOptions) {
	expandRetryClientOptions(o, retryClientOpts{
		MaxRetries: opt.MaxRetries, MinRetryBackoff: opt.MinRetryBackoff, MaxRetryBackoff: opt.MaxRetryBackoff,
		Username: opt.Username, ClientName: opt.ClientName,
		Protocol: opt.Protocol, OnConnect: opt.OnConnect, Dialer: opt.Dialer,
	})
	setStrIfEmpty(opt.SentinelUsername, &o.sentinelUsername)
	setStrIfEmpty(opt.SentinelPassword, &o.sentinelPassword)
	if !o.useDisconnectedReplicas && opt.UseDisconnectedReplicas {
		o.useDisconnectedReplicas = true
	}
}

// expandClusterOptions 展开 redis.ClusterOptions 专有字段
func expandClusterOptions(o *options, opt *redis.ClusterOptions) {
	expandRetryClientOptions(o, retryClientOpts{
		MaxRetries: opt.MaxRetries, MinRetryBackoff: opt.MinRetryBackoff, MaxRetryBackoff: opt.MaxRetryBackoff,
		Username: opt.Username, ClientName: opt.ClientName,
		Protocol: opt.Protocol, OnConnect: opt.OnConnect, Dialer: opt.Dialer,
	})
	if !o.readOnly && opt.ReadOnly {
		o.readOnly = true
	}
	if !o.routeByLatency && opt.RouteByLatency {
		o.routeByLatency = true
	}
	if !o.routeRandomly && opt.RouteRandomly {
		o.routeRandomly = true
	}
	if o.maxRedirects == 0 && opt.MaxRedirects > 0 {
		o.maxRedirects = opt.MaxRedirects
	}
}
