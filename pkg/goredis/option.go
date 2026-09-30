package goredis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"reflect"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// 日志与 request_id 统一走全局 pkg/logger（见 logging.go 与 requestid_hook.go），
// 本包不再提供 Logger 接口 / WithLogger / RequestIDExtractor / WithRequestIDExtractor：
//   - sunshine 是 mono-repo，pkg/logger 已是全仓日志底座，包内再造接口无法真正解耦；
//   - 上述注入点在生产代码中零调用，属于死 API，且默认实现走标准库 slog 会绕过项目日志管道；
//   - 如需替换底层日志实现，正确的收口点是 pkg/logger 自身，而不是每个基础设施包各造一套。

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
	tlsConfigSet    bool

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
	clientName    string
	clientNameSet bool
	protocol      int
	protocolSet   bool
	onConnect     func(ctx context.Context, cn *redis.Conn) error
	onConnectSet  bool
	dialer        func(ctx context.Context, network, addr string) (net.Conn, error)
	dialerSet     bool

	// 哨兵专用
	sentinelUsername           string
	sentinelUsernameSet        bool
	sentinelPassword           string
	sentinelPasswordSet        bool
	useDisconnectedReplicas    bool
	useDisconnectedReplicasSet bool

	// 集群专用
	readOnly          bool
	readOnlySet       bool
	routeByLatency    bool
	routeByLatencySet bool
	routeRandomly     bool
	routeRandomlySet  bool
	maxRedirects      int
	maxRedirectsSet   bool

	// 可观测性
	enableMetrics bool                 // 是否启用 OpenTelemetry 指标
	meterProvider metric.MeterProvider // 自定义指标 Provider，nil 时使用全局 MeterProvider

	// 初始化超时（默认 initTimeout/probeTimeout 常量，可由 WithInitTimeout/WithProbeTimeout 覆盖）
	initTimeout  time.Duration // 连接测试超时
	probeTimeout time.Duration // Lua 通道探测超时

	// 配置误用标记：WithXxxOptions 读取到本包不处理的字段时置位，由 Init 阶段快速失败
	singleOptionsMisused   bool
	sentinelOptionsMisused bool
	clusterOptionsMisused  bool

	// 追踪（接口类型，自定义 TracerProvider 实现与 SDK 实现均可注入，与 meterProvider 对称）
	tracerProvider oteltrace.TracerProvider
}

// apply 应用配置选项（nil Option 防御：跳过以避免 Init(dsn, nil) 等调用 panic）
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
}

// validate 校验选项组合的合法性
// 配置类错误在 Init 启动阶段快速失败，而不是运行期静默忽略
// 覆盖三类问题：展开函数不读取的字段误用、越界数值、非法枚举值
func (o *options) validate() error {
	if o.singleOptionsMisused {
		return errors.New("goredis: WithSingleOptions 不读取 Addr/Password/DB 字段，请通过 InitSingle/Init 参数或 DSN 传入")
	}
	if o.sentinelOptionsMisused {
		return errors.New("goredis: WithSentinelOptions 不读取 MasterName/SentinelAddrs 字段，请通过 InitSentinel 参数传入")
	}
	if o.clusterOptionsMisused {
		return errors.New("goredis: WithClusterOptions 不读取 Addrs 字段，请通过 InitCluster 参数传入")
	}
	if o.poolSizeSet && o.poolSize < 0 {
		return fmt.Errorf("goredis: WithPoolSize 不可为负数，当前值 %d", o.poolSize)
	}
	if o.minIdleSet && o.minIdleConns < 0 {
		return fmt.Errorf("goredis: WithMinIdleConns 不可为负数，当前值 %d", o.minIdleConns)
	}
	if o.protocolSet && o.protocol != 2 && o.protocol != 3 {
		return fmt.Errorf("goredis: WithProtocol 仅支持 2（RESP2）或 3（RESP3），当前值 %d", o.protocol)
	}
	if o.networkSet && o.network != "tcp" && o.network != "unix" {
		return fmt.Errorf("goredis: WithNetwork 仅支持 tcp 或 unix，当前值 %q", o.network)
	}
	if o.maxRetriesSet && o.maxRetries < -1 {
		return fmt.Errorf("goredis: WithMaxRetries 最小为 -1（默认重试次数），当前值 %d", o.maxRetries)
	}
	if o.maxRedirectsSet && o.maxRedirects < 0 {
		return fmt.Errorf("goredis: WithMaxRedirects 不可为负数，当前值 %d", o.maxRedirects)
	}
	return nil
}

// defaultOptions 返回默认配置选项（零值 = 不覆盖 DSN 解析结果）
func defaultOptions() *options {
	return &options{
		initTimeout:  initTimeout,  // 连接测试超时，默认 15s
		probeTimeout: probeTimeout, // Lua 探测超时，默认 3s
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

// WithTracing 设置 Redis 追踪提供者，适用于 redis v9 版本
// 参数为 oteltrace.TracerProvider 接口（与 WithMeterProvider(metric.MeterProvider) 对称），
// SDK 实现（sdk/trace.TracerProvider）与自定义实现均可注入；传入 nil 或 typed nil 时不启用追踪
func WithTracing(tp oteltrace.TracerProvider) Option {
	return func(o *options) {
		// 接口值的 != nil 判断拦不住 typed nil（如 (*sdktrace.TracerProvider)(nil) 包进接口），
		// 透传给 redisotel 会在后续调用中 panic，这里统一归一化为未设置
		if isNilTracerProvider(tp) {
			o.tracerProvider = nil
			return
		}
		o.tracerProvider = tp
	}
}

// isNilTracerProvider 判断接口值是否为 nil 或 typed nil
func isNilTracerProvider(tp oteltrace.TracerProvider) bool {
	if tp == nil {
		return true
	}
	rv := reflect.ValueOf(tp)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return rv.IsNil()
	default:
		return false
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
// 传入 nil 表示显式清空（覆盖 DSN/展开值中的 TLS 配置）
func WithTLSConfig(c *tls.Config) Option {
	return func(o *options) {
		o.tlsConfig = c
		o.tlsConfigSet = true
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

// WithSentinelUsername 设置哨兵 ACL 用户名（传入空串表示显式清空）
func WithSentinelUsername(u string) Option {
	return func(o *options) {
		o.sentinelUsername = u
		o.sentinelUsernameSet = true
	}
}

// WithSentinelPassword 设置哨兵密码（传入空串表示显式清空）
func WithSentinelPassword(p string) Option {
	return func(o *options) {
		o.sentinelPassword = p
		o.sentinelPasswordSet = true
	}
}

// WithUseDisconnectedReplicas 启用哨兵断连副本路由
func WithUseDisconnectedReplicas() Option {
	return func(o *options) {
		o.useDisconnectedReplicas = true
		o.useDisconnectedReplicasSet = true
	}
}

// WithoutUseDisconnectedReplicas 显式关闭哨兵断连副本路由
// 用于覆盖 WithSentinelOptions 展开的 UseDisconnectedReplicas=true
func WithoutUseDisconnectedReplicas() Option {
	return func(o *options) {
		o.useDisconnectedReplicas = false
		o.useDisconnectedReplicasSet = true
	}
}

// WithReadOnly 启用集群只读命令路由到副本节点
func WithReadOnly() Option {
	return func(o *options) {
		o.readOnly = true
		o.readOnlySet = true
	}
}

// WithoutReadOnly 显式关闭集群只读路由
// 用于覆盖 WithClusterOptions 展开的 ReadOnly=true（选项系统的对称关闭能力）
func WithoutReadOnly() Option {
	return func(o *options) {
		o.readOnly = false
		o.readOnlySet = true
	}
}

// WithRouteByLatency 启用集群延迟路由（自动启用 ReadOnly）
func WithRouteByLatency() Option {
	return func(o *options) {
		o.routeByLatency = true
		o.routeByLatencySet = true
	}
}

// WithoutRouteByLatency 显式关闭集群延迟路由
func WithoutRouteByLatency() Option {
	return func(o *options) {
		o.routeByLatency = false
		o.routeByLatencySet = true
	}
}

// WithRouteRandomly 启用集群随机路由（自动启用 ReadOnly）
func WithRouteRandomly() Option {
	return func(o *options) {
		o.routeRandomly = true
		o.routeRandomlySet = true
	}
}

// WithoutRouteRandomly 显式关闭集群随机路由
func WithoutRouteRandomly() Option {
	return func(o *options) {
		o.routeRandomly = false
		o.routeRandomlySet = true
	}
}

// WithMaxRedirects 设置集群最大重定向次数（0 也视为显式设置）
func WithMaxRedirects(n int) Option {
	return func(o *options) {
		o.maxRedirects = n
		o.maxRedirectsSet = true
	}
}

// WithSingleOptions 设置单机 Redis 选项（展开到 options 中，With* 显式设置的字段优先）
//
// 支持的字段:
//
//	连接池: PoolSize, MinIdleConns, PoolTimeout, ConnMaxLifetime, ConnMaxIdleTime
//	超时:   DialTimeout, ReadTimeout, WriteTimeout, TLSConfig
//	重试:   MaxRetries, MinRetryBackoff, MaxRetryBackoff
//	网络:   Network, Username, ClientName, Protocol, OnConnect, Dialer
//
// 注意:
//   - Addr/Password/DB 请通过 InitSingle/Init 参数或 DSN 传入，本函数不读取这三个字段。
//   - MaxRetries=0 表示“未设置”，不会禁用重试。如需显式禁用重试，请使用 WithMaxRetries(0)。
//   - MinRetryBackoff/MaxRetryBackoff=0 同理，如需显式设置请使用对应的 With* 函数。
//
// 破坏性变更（Breaking Change）：
//   - 早期版本对 Addr/Password/DB 的处理是 log.Printf 警告后静默忽略并正常初始化，
//     现在改为 Init 启动期直接返回 error（配置类错误快速失败）。
//   - 升级影响：若此前传入了这三个字段且初始化“能跑”，升级后会启动失败，
//     请改为通过 InitSingle/Init 参数或 DSN 传入。详见 README「版本兼容」章节与 CHANGELOG。
func WithSingleOptions(opt *redis.Options) Option {
	return func(o *options) {
		if opt == nil {
			return
		}
		// 检测 Addr/Password/DB 非零：本函数不读取这三个字段
		// 标记误用，由 validate() 在 Init 启动阶段快速失败（而非运行期静默忽略）
		if opt.Addr != "" || opt.Password != "" || opt.DB != 0 {
			o.singleOptionsMisused = true
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
		// 检测 MasterName/SentinelAddrs 非零：这两个字段由 InitSentinel 参数提供，本函数不读取
		// 标记误用，由 validate() 在 Init 启动阶段快速失败（而非运行期静默忽略）
		if opt.MasterName != "" || len(opt.SentinelAddrs) > 0 {
			o.sentinelOptionsMisused = true
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
		// 检测 Addrs 非零：节点地址由 InitCluster 参数提供，本函数不读取
		// 标记误用，由 validate() 在 Init 启动阶段快速失败（而非运行期静默忽略）
		if len(opt.Addrs) > 0 {
			o.clusterOptionsMisused = true
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

// WithMetrics 启用 OpenTelemetry 指标上报（redisotel.InstrumentMetrics）
// 覆盖命令延迟、连接池水位（db_client_connections_*）等指标
// 默认使用全局 MeterProvider；如需为 Redis 客户端指定独立的指标 Provider
// （多租户隔离/单测验证），请配合 WithMeterProvider 传入
func WithMetrics() Option {
	return func(o *options) {
		o.enableMetrics = true
	}
}

// WithMeterProvider 指定 Redis 指标上报使用的 MeterProvider
// 需配合 WithMetrics 使用；传入 nil 时回退到全局 MeterProvider（otel.GetMeterProvider）
// 与 WithTracing(TracerProvider) 对称，用于多租户/按服务隔离指标的观测场景
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(o *options) {
		o.meterProvider = mp
	}
}

// WithInitTimeout 覆盖初始化阶段连接测试的超时时间（默认 15s）
// 仅接受正数，非正值保持默认
func WithInitTimeout(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.initTimeout = d
		}
	}
}

// WithProbeTimeout 覆盖 Lua 脚本通道探测的超时时间（默认 3s）
// 仅接受正数，非正值保持默认；探测失败仅告警不中断初始化，超时过长会拖慢初始化
func WithProbeTimeout(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.probeTimeout = d
		}
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
	if !o.tlsConfigSet && src.TLSConfig != nil {
		o.tlsConfig = src.TLSConfig
		o.tlsConfigSet = true
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
	// 哨兵专有：仅在用户未通过 With* 显式设置时（xxxSet=false）才写入，并同步置位
	if !o.sentinelUsernameSet && opt.SentinelUsername != "" {
		o.sentinelUsername = opt.SentinelUsername
		o.sentinelUsernameSet = true
	}
	if !o.sentinelPasswordSet && opt.SentinelPassword != "" {
		o.sentinelPassword = opt.SentinelPassword
		o.sentinelPasswordSet = true
	}
	if !o.useDisconnectedReplicasSet && opt.UseDisconnectedReplicas {
		o.useDisconnectedReplicas = true
		o.useDisconnectedReplicasSet = true
	}
}

// expandClusterOptions 展开 redis.ClusterOptions 专有字段
func expandClusterOptions(o *options, opt *redis.ClusterOptions) {
	expandRetryClientOptions(o, retryClientOpts{
		MaxRetries: opt.MaxRetries, MinRetryBackoff: opt.MinRetryBackoff, MaxRetryBackoff: opt.MaxRetryBackoff,
		Username: opt.Username, ClientName: opt.ClientName,
		Protocol: opt.Protocol, OnConnect: opt.OnConnect, Dialer: opt.Dialer,
	})
	// 集群专有：仅在用户未通过 With* 显式设置时（xxxSet=false）才写入，并同步置位
	if !o.readOnlySet && opt.ReadOnly {
		o.readOnly = true
		o.readOnlySet = true
	}
	if !o.routeByLatencySet && opt.RouteByLatency {
		o.routeByLatency = true
		o.routeByLatencySet = true
	}
	if !o.routeRandomlySet && opt.RouteRandomly {
		o.routeRandomly = true
		o.routeRandomlySet = true
	}
	if !o.maxRedirectsSet && opt.MaxRedirects > 0 {
		o.maxRedirects = opt.MaxRedirects
		o.maxRedirectsSet = true
	}
}
