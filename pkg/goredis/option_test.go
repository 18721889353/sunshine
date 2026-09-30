package goredis

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestDefaultPoolSizeZeroValue 验证默认连接池大小为零值（不覆盖 DSN 解析结果）
func TestDefaultPoolSizeZeroValue(t *testing.T) {
	o := defaultOptions()
	assert.Equal(t, 0, o.poolSize, "默认连接池大小应为零值")
}

// TestDefaultOptionsDefaults 验证默认选项值
func TestDefaultOptionsDefaults(t *testing.T) {
	o := defaultOptions()
	assert.Nil(t, o.tracerProvider, "默认追踪提供者为 nil")
	assert.False(t, o.poolSizeSet, "默认 poolSizeSet 应为 false")
	assert.False(t, o.maxRetriesSet, "默认 maxRetriesSet 应为 false")
}

// TestWithPoolSizeSetsValueAndFlag 验证 WithPoolSize 选项
func TestWithPoolSizeSetsValueAndFlag(t *testing.T) {
	o := defaultOptions()
	assert.False(t, o.poolSizeSet, "默认 poolSizeSet 应为 false")
	WithPoolSize(25)(o)
	assert.Equal(t, 25, o.poolSize)
	assert.True(t, o.poolSizeSet, "设置后 poolSizeSet 应为 true")
}

// TestWithMinIdleConnsSetsValue 验证 WithMinIdleConns 选项
func TestWithMinIdleConnsSetsValue(t *testing.T) {
	o := defaultOptions()
	WithMinIdleConns(8)(o)
	assert.Equal(t, 8, o.minIdleConns)
}

// TestWithMaxConnAgeSetsValue 验证 WithMaxConnAge 选项
func TestWithMaxConnAgeSetsValue(t *testing.T) {
	o := defaultOptions()
	WithMaxConnAge(30 * time.Minute)(o)
	assert.Equal(t, 30*time.Minute, o.maxConnAge)
}

// TestWithPoolTimeoutSetsValue 验证 WithPoolTimeout 选项
func TestWithPoolTimeoutSetsValue(t *testing.T) {
	o := defaultOptions()
	WithPoolTimeout(5 * time.Second)(o)
	assert.Equal(t, 5*time.Second, o.poolTimeout)
}

// TestWithIdleTimeoutSetsValue 验证 WithIdleTimeout 选项
func TestWithIdleTimeoutSetsValue(t *testing.T) {
	o := defaultOptions()
	WithIdleTimeout(10 * time.Minute)(o)
	assert.Equal(t, 10*time.Minute, o.idleTimeout)
}

// TestWithDialTimeoutSetsValueAndFlag 验证 WithDialTimeout 选项
func TestWithDialTimeoutSetsValueAndFlag(t *testing.T) {
	o := defaultOptions()
	assert.False(t, o.dialTimeoutSet, "默认 dialTimeoutSet 应为 false")
	WithDialTimeout(3 * time.Second)(o)
	assert.Equal(t, 3*time.Second, o.dialTimeout)
	assert.True(t, o.dialTimeoutSet, "设置后 dialTimeoutSet 应为 true")
}

// TestWithReadTimeoutSetsValue 验证 WithReadTimeout 选项
func TestWithReadTimeoutSetsValue(t *testing.T) {
	o := defaultOptions()
	WithReadTimeout(2 * time.Second)(o)
	assert.Equal(t, 2*time.Second, o.readTimeout)
}

// TestWithWriteTimeoutSetsValue 验证 WithWriteTimeout 选项
func TestWithWriteTimeoutSetsValue(t *testing.T) {
	o := defaultOptions()
	WithWriteTimeout(4 * time.Second)(o)
	assert.Equal(t, 4*time.Second, o.writeTimeout)
}

// TestWithTLSConfigSetsValue 验证 WithTLSConfig 选项
func TestWithTLSConfigSetsValue(t *testing.T) {
	o := defaultOptions()
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	WithTLSConfig(cfg)(o)
	assert.Equal(t, cfg, o.tlsConfig)
}

// TestApplyOrderLaterWins 验证 apply 按顺序应用选项，后覆盖先
func TestApplyOrderLaterWins(t *testing.T) {
	o := defaultOptions()
	// 先设 PoolSize=20，再设 PoolSize=50，最终应为 50
	WithPoolSize(20)(o)
	WithPoolSize(50)(o)
	assert.Equal(t, 50, o.poolSize)
}

// TestWithSingleOptionsExpansion 验证 WithSingleOptions 设置 + 展开效果
func TestWithSingleOptionsExpansion(t *testing.T) {
	o := defaultOptions()

	opt := &redis.Options{Addr: "127.0.0.1:6379", PoolSize: 5, MinIdleConns: 2, MaxRetries: 3}
	WithSingleOptions(opt)(o)
	// P1-3: 断言展开效果
	assert.Equal(t, 5, o.poolSize, "PoolSize 应被展开")
	assert.True(t, o.poolSizeSet, "展开后 poolSizeSet 应为 true")
	assert.Equal(t, 2, o.minIdleConns, "MinIdleConns 应被展开")
	assert.True(t, o.minIdleSet, "展开后 minIdleSet 应为 true")
	assert.False(t, o.maxConnAgeSet, "ConnMaxLifetime 为零不应置位")
	assert.Equal(t, 3, o.maxRetries, "MaxRetries 应被展开")
	assert.True(t, o.maxRetriesSet, "展开后 maxRetriesSet 应为 true")
}

// TestWithSingleOptionsKeepsExplicitFields 展开不应覆盖用户已设置的字段
func TestWithSingleOptionsKeepsExplicitFields(t *testing.T) {
	o := defaultOptions()
	WithPoolSize(50)(o)
	WithSingleOptions(&redis.Options{PoolSize: 5})(o)
	assert.Equal(t, 50, o.poolSize, "WithPoolSize 应优先")
	assert.True(t, o.poolSizeSet, "poolSizeSet 应保持 true")
}

// TestWithSingleOptionsNilSafe nil 安全
func TestWithSingleOptionsNilSafe(t *testing.T) {
	o := defaultOptions()
	WithSingleOptions(nil)(o)
	assert.False(t, o.poolSizeSet, "nil 展开不应改变任何 Set 标志")
}

// TestWithSentinelOptionsExpansion 验证 WithSentinelOptions 展开效果
func TestWithSentinelOptionsExpansion(t *testing.T) {
	o := defaultOptions()

	opt := &redis.FailoverOptions{MasterName: "mymaster", SentinelUsername: "sentinel-user"}
	WithSentinelOptions(opt)(o)
	assert.Equal(t, "sentinel-user", o.sentinelUsername, "SentinelUsername 应被展开")
	assert.True(t, o.poolSizeSet == false, "未设置 PoolSize 不应置位")
}

// TestWithClusterOptionsExpansion 验证 WithClusterOptions 展开效果
func TestWithClusterOptionsExpansion(t *testing.T) {
	o := defaultOptions()

	opt := &redis.ClusterOptions{Addrs: []string{"127.0.0.1:7000"}, ReadOnly: true, MaxRedirects: 5}
	WithClusterOptions(opt)(o)
	assert.True(t, o.readOnly, "ReadOnly 应被展开")
	assert.Equal(t, 5, o.maxRedirects, "MaxRedirects 应被展开")
}

// TestIndependentOptionsFieldSetFlags 表驱动测试：验证 16 个独立 Option 函数的字段设置与置位
func TestIndependentOptionsFieldSetFlags(t *testing.T) {
	onConnectFn := func(_ context.Context, _ *redis.Conn) error { return nil }
	dialerFn := func(_ context.Context, _, _ string) (net.Conn, error) { return nil, nil }

	cases := []struct {
		name  string
		apply Option
		check func(t *testing.T, o *options)
	}{
		{"WithMaxRetries 正常值", WithMaxRetries(5), func(t *testing.T, o *options) {
			assert.Equal(t, 5, o.maxRetries)
			assert.True(t, o.maxRetriesSet)
		}},
		{"WithMaxRetries 禁用重试", WithMaxRetries(0), func(t *testing.T, o *options) {
			assert.Equal(t, 0, o.maxRetries, "0 是有效值（禁用重试）")
			assert.True(t, o.maxRetriesSet, "0 也应置位")
		}},
		{"WithMaxRetries 默认重试", WithMaxRetries(-1), func(t *testing.T, o *options) {
			assert.Equal(t, -1, o.maxRetries, "-1 表示使用默认重试次数")
			assert.True(t, o.maxRetriesSet)
		}},
		{"WithMinRetryBackoff", WithMinRetryBackoff(100 * time.Millisecond), func(t *testing.T, o *options) {
			assert.Equal(t, 100*time.Millisecond, o.minRetryBackoff)
			assert.True(t, o.minRetryBackoffSet)
		}},
		{"WithMaxRetryBackoff", WithMaxRetryBackoff(2 * time.Second), func(t *testing.T, o *options) {
			assert.Equal(t, 2*time.Second, o.maxRetryBackoff)
			assert.True(t, o.maxRetryBackoffSet)
		}},
		{"WithNetwork", WithNetwork("tcp"), func(t *testing.T, o *options) {
			assert.Equal(t, "tcp", o.network)
			assert.True(t, o.networkSet)
		}},
		{"WithUsername", WithUsername("myuser"), func(t *testing.T, o *options) {
			assert.Equal(t, "myuser", o.username)
			assert.True(t, o.usernameSet)
		}},
		{"WithClientName", WithClientName("my-app"), func(t *testing.T, o *options) {
			assert.Equal(t, "my-app", o.clientName)
			assert.True(t, o.clientNameSet)
		}},
		{"WithProtocol", WithProtocol(3), func(t *testing.T, o *options) {
			assert.Equal(t, 3, o.protocol)
			assert.True(t, o.protocolSet)
		}},
		{"WithOnConnect", WithOnConnect(onConnectFn), func(t *testing.T, o *options) {
			assert.NotNil(t, o.onConnect)
			assert.True(t, o.onConnectSet)
		}},
		{"WithDialer", WithDialer(dialerFn), func(t *testing.T, o *options) {
			assert.NotNil(t, o.dialer)
			assert.True(t, o.dialerSet)
		}},
		{"WithSentinelUsername", WithSentinelUsername("sent-user"), func(t *testing.T, o *options) {
			assert.Equal(t, "sent-user", o.sentinelUsername)
			assert.True(t, o.sentinelUsernameSet, "设置后 sentinelUsernameSet 应为 true")
		}},
		{"WithSentinelPassword", WithSentinelPassword("sent-pass"), func(t *testing.T, o *options) {
			assert.Equal(t, "sent-pass", o.sentinelPassword)
			assert.True(t, o.sentinelPasswordSet, "设置后 sentinelPasswordSet 应为 true")
		}},
		{"WithUseDisconnectedReplicas", WithUseDisconnectedReplicas(), func(t *testing.T, o *options) {
			assert.True(t, o.useDisconnectedReplicas)
			assert.True(t, o.useDisconnectedReplicasSet, "设置后 useDisconnectedReplicasSet 应为 true")
		}},
		{"WithReadOnly", WithReadOnly(), func(t *testing.T, o *options) {
			assert.True(t, o.readOnly)
			assert.True(t, o.readOnlySet, "设置后 readOnlySet 应为 true")
		}},
		{"WithRouteByLatency", WithRouteByLatency(), func(t *testing.T, o *options) {
			assert.True(t, o.routeByLatency)
			assert.True(t, o.routeByLatencySet, "设置后 routeByLatencySet 应为 true")
		}},
		{"WithRouteRandomly", WithRouteRandomly(), func(t *testing.T, o *options) {
			assert.True(t, o.routeRandomly)
			assert.True(t, o.routeRandomlySet, "设置后 routeRandomlySet 应为 true")
		}},
		{"WithMaxRedirects", WithMaxRedirects(8), func(t *testing.T, o *options) {
			assert.Equal(t, 8, o.maxRedirects)
			assert.True(t, o.maxRedirectsSet, "设置后 maxRedirectsSet 应为 true")
		}},
		{"WithMaxRedirects 显式设 0", WithMaxRedirects(0), func(t *testing.T, o *options) {
			assert.Equal(t, 0, o.maxRedirects, "0 是有效值（显式关闭重定向）")
			assert.True(t, o.maxRedirectsSet, "0 也应置位")
		}},
		{"WithoutReadOnly 显式关闭", WithoutReadOnly(), func(t *testing.T, o *options) {
			assert.False(t, o.readOnly)
			assert.True(t, o.readOnlySet, "关闭操作也应置位")
		}},
		{"WithoutRouteByLatency 显式关闭", WithoutRouteByLatency(), func(t *testing.T, o *options) {
			assert.False(t, o.routeByLatency)
			assert.True(t, o.routeByLatencySet, "关闭操作也应置位")
		}},
		{"WithoutRouteRandomly 显式关闭", WithoutRouteRandomly(), func(t *testing.T, o *options) {
			assert.False(t, o.routeRandomly)
			assert.True(t, o.routeRandomlySet, "关闭操作也应置位")
		}},
		{"WithoutUseDisconnectedReplicas 显式关闭", WithoutUseDisconnectedReplicas(), func(t *testing.T, o *options) {
			assert.False(t, o.useDisconnectedReplicas)
			assert.True(t, o.useDisconnectedReplicasSet, "关闭操作也应置位")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := defaultOptions()
			tc.apply(o)
			tc.check(t, o)
		})
	}
}

// TestIndependentOptionsPriority 独立 Option 优先于 WithSingleOptions 展开
func TestIndependentOptionsPriority(t *testing.T) {
	o := defaultOptions()
	WithSingleOptions(&redis.Options{Username: "from-single"})(o)
	WithUsername("from-explicit")(o)
	assert.Equal(t, "from-explicit", o.username, "WithUsername 应优先于 WithSingleOptions")
	assert.True(t, o.usernameSet)
}

// ============================================================================
// 三阶段合并语义：xxxSet 标志完整性与显式清空/关闭
// ============================================================================

// TestWithTLSConfigExplicitClear 传 nil 应置位并清空 TLS 配置
func TestWithTLSConfigExplicitClear(t *testing.T) {
	o := defaultOptions()
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	WithTLSConfig(cfg)(o)
	assert.True(t, o.tlsConfigSet)

	WithTLSConfig(nil)(o)
	assert.Nil(t, o.tlsConfig, "传 nil 应显式清空")
	assert.True(t, o.tlsConfigSet, "清空操作也应置位")
}

// TestWithSentinelCredentialsExplicitClear 传空串应置位并清空哨兵凭据
func TestWithSentinelCredentialsExplicitClear(t *testing.T) {
	o := defaultOptions()
	WithSentinelUsername("user")(o)
	WithSentinelPassword("pass")(o)

	WithSentinelUsername("")(o)
	WithSentinelPassword("")(o)
	assert.Empty(t, o.sentinelUsername, "空串应显式清空")
	assert.Empty(t, o.sentinelPassword, "空串应显式清空")
	assert.True(t, o.sentinelUsernameSet)
	assert.True(t, o.sentinelPasswordSet)
}

// TestClusterOptionsWithoutReadOnlyOverridesExpanded
// 三阶段语义端到端：展开值 → Without* 显式关闭 → applyExplicit 最终生效
func TestClusterOptionsWithoutReadOnlyOverridesExpanded(t *testing.T) {
	o := defaultOptions()
	WithClusterOptions(&redis.ClusterOptions{ReadOnly: true, RouteByLatency: true, MaxRedirects: 5})(o)
	assert.True(t, o.readOnly, "展开值应生效")

	WithoutReadOnly()(o)
	assert.False(t, o.readOnly, "WithoutReadOnly 应覆盖展开值")
	assert.True(t, o.readOnlySet)

	opt := buildClusterBaseOptions([]string{"127.0.0.1:7000"}, "", "", o)
	applyExplicitClusterOptions(opt, o)
	assert.False(t, opt.ReadOnly, "WithoutReadOnly 应最终生效")
	assert.True(t, opt.RouteByLatency, "未关闭的展开值应保留")
	assert.Equal(t, 5, opt.MaxRedirects, "展开的 MaxRedirects 应保留")
}

// TestSentinelOptionsExplicitClearCredentials 空串应覆盖展开的哨兵用户名/密码
func TestSentinelOptionsExplicitClearCredentials(t *testing.T) {
	o := defaultOptions()
	WithSentinelOptions(&redis.FailoverOptions{SentinelUsername: "u", SentinelPassword: "p", UseDisconnectedReplicas: true})(o)
	assert.Equal(t, "u", o.sentinelUsername)

	WithSentinelUsername("")(o)
	WithSentinelPassword("")(o)
	WithoutUseDisconnectedReplicas()(o)

	opt := buildFailoverBaseOptions("mymaster", []string{"127.0.0.1:26380"}, "", "", o)
	applyExplicitFailoverOptions(opt, o)
	assert.Empty(t, opt.SentinelUsername, "空串应显式清空展开值")
	assert.Empty(t, opt.SentinelPassword, "空串应显式清空展开值")
	assert.False(t, opt.UseDisconnectedReplicas, "Without 应显式关闭展开值")
}

// TestApplyNilOptionSkipsWithoutPanic nil Option 跳过而不 panic
func TestApplyNilOptionSkipsWithoutPanic(t *testing.T) {
	o := defaultOptions()
	WithPoolSize(10)(o)
	o.apply(nil, WithPoolSize(20), nil)
	assert.Equal(t, 20, o.poolSize)

	// 多个连续 nil Option 应全部跳过且不 panic
	assert.NotPanics(t, func() {
		o.apply(nil, nil, nil)
	}, "多个 nil Option 应全部跳过且不 panic")
}

// ============================================================================
// validate 快速失败与 Logger/Metrics
// ============================================================================

// TestValidateSingleOptionsMisuse 非零 Addr/Password/DB 应快速失败
func TestValidateSingleOptionsMisuse(t *testing.T) {
	o := defaultOptions()
	WithSingleOptions(&redis.Options{Addr: "127.0.0.1:6379"})(o)
	assert.Error(t, o.validate(), "误用 Addr 应校验失败")

	o2 := defaultOptions()
	WithSingleOptions(&redis.Options{PoolSize: 5})(o2)
	assert.NoError(t, o2.validate(), "仅连接池字段不应误报")
}

// TestWithMetricsToggle 验证指标开关默认关闭、显式开启
func TestWithMetricsToggle(t *testing.T) {
	o := defaultOptions()
	assert.False(t, o.enableMetrics, "默认不启用指标")
	WithMetrics()(o)
	assert.True(t, o.enableMetrics, "WithMetrics 应开启指标")
}

// TestWithMeterProviderInjectAndFallback 验证自定义 MeterProvider 注入与回退（与 WithTracing 对称）
func TestWithMeterProviderInjectAndFallback(t *testing.T) {
	o := defaultOptions()
	assert.Nil(t, o.meterProvider, "默认 meterProvider 为 nil（使用全局 MeterProvider）")

	WithMeterProvider(otel.GetMeterProvider())(o)
	assert.NotNil(t, o.meterProvider, "应记录注入的 MeterProvider")

	WithMeterProvider(nil)(o)
	assert.Nil(t, o.meterProvider, "传 nil 应回退到全局 MeterProvider")
}

// TestWithInitTimeoutConfigurable 验证初始化/探测超时默认值与显式覆盖
func TestWithInitTimeoutConfigurable(t *testing.T) {
	o := defaultOptions()
	assert.Equal(t, initTimeout, o.initTimeout, "默认应为 initTimeout 常量")
	assert.Equal(t, probeTimeout, o.probeTimeout, "默认应为 probeTimeout 常量")

	WithInitTimeout(5 * time.Second)(o)
	WithProbeTimeout(1 * time.Second)(o)
	assert.Equal(t, 5*time.Second, o.initTimeout, "WithInitTimeout 应覆盖默认值")
	assert.Equal(t, time.Second, o.probeTimeout, "WithProbeTimeout 应覆盖默认值")

	WithInitTimeout(0)(o)
	WithProbeTimeout(-1)(o)
	assert.Equal(t, 5*time.Second, o.initTimeout, "非正值应保持已设置的值")
	assert.Equal(t, time.Second, o.probeTimeout, "非正值应保持已设置的值")
}

// ============================================================================
// P1 回归：WithUsername 对哨兵/集群生效（此前静默失效）
// ============================================================================

// TestApplyExplicitFailoverOptionsUsernameOverrides WithUsername 应覆盖 InitSentinel 的 username 参数
func TestApplyExplicitFailoverOptionsUsernameOverrides(t *testing.T) {
	o := defaultOptions()
	WithUsername("opt-user")(o)

	opt := buildFailoverBaseOptions("mymaster", []string{"127.0.0.1:26380"}, "param-user", "p", o)
	applyExplicitFailoverOptions(opt, o)
	assert.Equal(t, "opt-user", opt.Username, "WithUsername 应覆盖函数参数")
}

// TestApplyExplicitFailoverOptionsUsernameFallback 未显式设置时保留 InitSentinel 参数
func TestApplyExplicitFailoverOptionsUsernameFallback(t *testing.T) {
	o := defaultOptions()

	opt := buildFailoverBaseOptions("mymaster", []string{"127.0.0.1:26380"}, "param-user", "p", o)
	applyExplicitFailoverOptions(opt, o)
	assert.Equal(t, "param-user", opt.Username, "未显式设置时函数参数应兜底")
}

// TestApplyExplicitClusterOptionsUsernameOverrides WithUsername 应覆盖 InitCluster 的 username 参数
func TestApplyExplicitClusterOptionsUsernameOverrides(t *testing.T) {
	o := defaultOptions()
	WithUsername("opt-user")(o)

	opt := buildClusterBaseOptions([]string{"127.0.0.1:7000"}, "param-user", "p", o)
	applyExplicitClusterOptions(opt, o)
	assert.Equal(t, "opt-user", opt.Username, "WithUsername 应覆盖函数参数")
}

// TestApplyExplicitClusterOptionsSingleOptionsUsername 展开的 Username 对集群同样生效
func TestApplyExplicitClusterOptionsSingleOptionsUsername(t *testing.T) {
	o := defaultOptions()
	WithSingleOptions(&redis.Options{Username: "expanded-user"})(o)

	opt := buildClusterBaseOptions([]string{"127.0.0.1:7000"}, "param-user", "p", o)
	applyExplicitClusterOptions(opt, o)
	assert.Equal(t, "expanded-user", opt.Username, "WithSingleOptions 展开的 Username 应对集群生效")
}

// ============================================================================
// P0-6 回归：validate 范围/枚举校验与哨兵/集群误用检测
// ============================================================================

// TestValidateRangeAndEnumChecks 表驱动验证越界数值与非法枚举在启动阶段快速失败
func TestValidateRangeAndEnumChecks(t *testing.T) {
	cases := []struct {
		name  string
		apply Option
		want  string
	}{
		{"PoolSize 负数", WithPoolSize(-1), "WithPoolSize 不可为负数"},
		{"MinIdleConns 负数", WithMinIdleConns(-1), "WithMinIdleConns 不可为负数"},
		{"Protocol 非法", WithProtocol(9), "WithProtocol 仅支持"},
		{"Network 非法", WithNetwork("foo"), "WithNetwork 仅支持"},
		{"MaxRetries 小于 -1", WithMaxRetries(-2), "WithMaxRetries 最小为 -1"},
		{"MaxRedirects 负数", WithMaxRedirects(-1), "WithMaxRedirects 不可为负数"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := defaultOptions()
			c.apply(o)
			err := o.validate()
			assert.Error(t, err, "越界/非法值应在启动阶段报错，而非运行期静默忽略")
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

// TestValidateAcceptsBoundaryValues 验证合法边界值不被误报
func TestValidateAcceptsBoundaryValues(t *testing.T) {
	o := defaultOptions()
	WithPoolSize(0)(o)
	WithMinIdleConns(0)(o)
	WithProtocol(2)(o)
	WithNetwork("tcp")(o)
	WithMaxRetries(-1)(o) // -1 = 使用默认重试次数，合法
	WithMaxRedirects(0)(o)
	assert.NoError(t, o.validate(), "合法边界值不应误报")

	o2 := defaultOptions()
	WithProtocol(3)(o2)     // RESP3 合法
	WithNetwork("unix")(o2) // unix 合法
	assert.NoError(t, o2.validate(), "RESP3 与 unix 应为合法枚举值")
}

// TestValidateSentinelOptionsMisuse 验证 WithSentinelOptions 误传 MasterName/SentinelAddrs 快速失败
func TestValidateSentinelOptionsMisuse(t *testing.T) {
	o := defaultOptions()
	WithSentinelOptions(&redis.FailoverOptions{MasterName: "mymaster"})(o)
	assert.Error(t, o.validate(), "MasterName 被静默忽略是误导，应启动失败")

	o2 := defaultOptions()
	WithSentinelOptions(&redis.FailoverOptions{SentinelAddrs: []string{"127.0.0.1:26380"}})(o2)
	assert.Error(t, o2.validate(), "SentinelAddrs 被静默忽略是误导，应启动失败")

	o3 := defaultOptions()
	WithSentinelOptions(&redis.FailoverOptions{PoolSize: 5})(o3)
	assert.NoError(t, o3.validate(), "仅传可展开字段不应误报")
}

// TestValidateClusterOptionsMisuse 验证 WithClusterOptions 误传 Addrs 快速失败
func TestValidateClusterOptionsMisuse(t *testing.T) {
	o := defaultOptions()
	WithClusterOptions(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:7000"}})(o)
	assert.Error(t, o.validate(), "Addrs 被静默忽略是误导，应启动失败")

	o2 := defaultOptions()
	WithClusterOptions(&redis.ClusterOptions{ReadOnly: true})(o2)
	assert.NoError(t, o2.validate(), "仅传可展开字段不应误报")
}

// ============================================================================
// P0-5 回归：WithTracing 面向接口而非 SDK 具体类型
// ============================================================================

// TestWithTracingInterfaceAndTypedNil 验证 SDK 具体类型可赋给接口字段，
// 且 typed nil（(*sdktrace.TracerProvider)(nil) 包进接口）被归一化为未设置
func TestWithTracingInterfaceAndTypedNil(t *testing.T) {
	o := defaultOptions()
	assert.Nil(t, o.tracerProvider, "默认 tracerProvider 为 nil（不启用追踪）")

	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	WithTracing(tp)(o)
	assert.Same(t, tp, o.tracerProvider, "SDK 具体类型应可赋给 oteltrace.TracerProvider 接口字段")

	// typed nil：接口值 != nil 成立，但底层指针为 nil，透传给 redisotel 会 panic
	var typedNil *sdktrace.TracerProvider
	WithTracing(typedNil)(o)
	assert.Nil(t, o.tracerProvider, "typed nil 应归一化为未设置")

	WithTracing(tp)(o)
	WithTracing(nil)(o)
	assert.Nil(t, o.tracerProvider, "显式 nil 应回退到未设置")
}
