package goredis

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

// TestDefaultPoolSize 验证默认连接池大小为零值（不覆盖 DSN 解析结果）
func TestDefaultPoolSize(t *testing.T) {
	o := defaultOptions()
	assert.Equal(t, 0, o.poolSize, "默认连接池大小应为零值")
}

// TestDefaultOptions 验证默认选项值
func TestDefaultOptions(t *testing.T) {
	o := defaultOptions()
	assert.False(t, o.enableTrace, "默认不启用追踪")
	assert.Nil(t, o.tracerProvider, "默认追踪提供者为 nil")
	assert.Nil(t, o.requestIDExtractor, "默认提取器为 nil")
	assert.False(t, o.poolSizeSet, "默认 poolSizeSet 应为 false")
	assert.False(t, o.maxRetriesSet, "默认 maxRetriesSet 应为 false")
}

// TestWithPoolSize 验证 WithPoolSize 选项
func TestWithPoolSize(t *testing.T) {
	o := defaultOptions()
	assert.False(t, o.poolSizeSet, "默认 poolSizeSet 应为 false")
	WithPoolSize(25)(o)
	assert.Equal(t, 25, o.poolSize)
	assert.True(t, o.poolSizeSet, "设置后 poolSizeSet 应为 true")
}

// TestWithMinIdleConns 验证 WithMinIdleConns 选项
func TestWithMinIdleConns(t *testing.T) {
	o := defaultOptions()
	WithMinIdleConns(8)(o)
	assert.Equal(t, 8, o.minIdleConns)
}

// TestWithMaxConnAge 验证 WithMaxConnAge 选项
func TestWithMaxConnAge(t *testing.T) {
	o := defaultOptions()
	WithMaxConnAge(30 * time.Minute)(o)
	assert.Equal(t, 30*time.Minute, o.maxConnAge)
}

// TestWithPoolTimeout 验证 WithPoolTimeout 选项
func TestWithPoolTimeout(t *testing.T) {
	o := defaultOptions()
	WithPoolTimeout(5 * time.Second)(o)
	assert.Equal(t, 5*time.Second, o.poolTimeout)
}

// TestWithIdleTimeout 验证 WithIdleTimeout 选项
func TestWithIdleTimeout(t *testing.T) {
	o := defaultOptions()
	WithIdleTimeout(10 * time.Minute)(o)
	assert.Equal(t, 10*time.Minute, o.idleTimeout)
}

// TestWithDialTimeout 验证 WithDialTimeout 选项
func TestWithDialTimeout(t *testing.T) {
	o := defaultOptions()
	assert.False(t, o.dialTimeoutSet, "默认 dialTimeoutSet 应为 false")
	WithDialTimeout(3 * time.Second)(o)
	assert.Equal(t, 3*time.Second, o.dialTimeout)
	assert.True(t, o.dialTimeoutSet, "设置后 dialTimeoutSet 应为 true")
}

// TestWithReadTimeout 验证 WithReadTimeout 选项
func TestWithReadTimeout(t *testing.T) {
	o := defaultOptions()
	WithReadTimeout(2 * time.Second)(o)
	assert.Equal(t, 2*time.Second, o.readTimeout)
}

// TestWithWriteTimeout 验证 WithWriteTimeout 选项
func TestWithWriteTimeout(t *testing.T) {
	o := defaultOptions()
	WithWriteTimeout(4 * time.Second)(o)
	assert.Equal(t, 4*time.Second, o.writeTimeout)
}

// TestWithTLSConfig 验证 WithTLSConfig 选项
func TestWithTLSConfig(t *testing.T) {
	o := defaultOptions()
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	WithTLSConfig(cfg)(o)
	assert.Equal(t, cfg, o.tlsConfig)
}

// TestWithRequestIDExtractor 验证 WithRequestIDExtractor 选项注入
func TestWithRequestIDExtractor(t *testing.T) {
	o := defaultOptions()
	assert.Nil(t, o.requestIDExtractor)

	called := false
	extractor := func(_ context.Context) string {
		called = true
		return "test-id"
	}
	WithRequestIDExtractor(extractor)(o)

	assert.NotNil(t, o.requestIDExtractor)
	assert.Equal(t, "test-id", o.requestIDExtractor(context.Background()))
	assert.True(t, called)
}

// TestApplyOrder 验证 apply 按顺序应用选项，后覆盖先
func TestApplyOrder(t *testing.T) {
	o := defaultOptions()
	// 先设 PoolSize=20，再设 PoolSize=50，最终应为 50
	WithPoolSize(20)(o)
	WithPoolSize(50)(o)
	assert.Equal(t, 50, o.poolSize)
}

// TestWithSingleOptions 验证 WithSingleOptions 设置 + 展开效果
func TestWithSingleOptions(t *testing.T) {
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

// TestWithSingleOptions_不覆盖已设置字段 展开不应覆盖用户已设置的字段
func TestWithSingleOptions_不覆盖已设置字段(t *testing.T) {
	o := defaultOptions()
	WithPoolSize(50)(o)
	WithSingleOptions(&redis.Options{PoolSize: 5})(o)
	assert.Equal(t, 50, o.poolSize, "WithPoolSize 应优先")
	assert.True(t, o.poolSizeSet, "poolSizeSet 应保持 true")
}

// TestWithSingleOptions_nil nil 安全
func TestWithSingleOptions_nil(t *testing.T) {
	o := defaultOptions()
	WithSingleOptions(nil)(o)
	assert.False(t, o.poolSizeSet, "nil 展开不应改变任何 Set 标志")
}

// TestWithSentinelOptions 验证 WithSentinelOptions 展开效果
func TestWithSentinelOptions(t *testing.T) {
	o := defaultOptions()

	opt := &redis.FailoverOptions{MasterName: "mymaster", SentinelUsername: "sentinel-user"}
	WithSentinelOptions(opt)(o)
	assert.Equal(t, "sentinel-user", o.sentinelUsername, "SentinelUsername 应被展开")
	assert.True(t, o.poolSizeSet == false, "未设置 PoolSize 不应置位")
}

// TestWithClusterOptions 验证 WithClusterOptions 展开效果
func TestWithClusterOptions(t *testing.T) {
	o := defaultOptions()

	opt := &redis.ClusterOptions{Addrs: []string{"127.0.0.1:7000"}, ReadOnly: true, MaxRedirects: 5}
	WithClusterOptions(opt)(o)
	assert.True(t, o.readOnly, "ReadOnly 应被展开")
	assert.Equal(t, 5, o.maxRedirects, "MaxRedirects 应被展开")
}

// TestIndependentOptions 表驱动测试：验证 16 个独立 Option 函数的字段设置与置位
func TestIndependentOptions(t *testing.T) {
	onConnectFn := func(_ context.Context, _ *redis.Conn) error { return nil }
	dialerFn := func(_ context.Context, _, _ string) (net.Conn, error) { return nil, nil }

	cases := []struct {
		名称  string
		应用 Option
		检查 func(t *testing.T, o *options)
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
		}},
		{"WithSentinelPassword", WithSentinelPassword("sent-pass"), func(t *testing.T, o *options) {
			assert.Equal(t, "sent-pass", o.sentinelPassword)
		}},
		{"WithUseDisconnectedReplicas", WithUseDisconnectedReplicas(), func(t *testing.T, o *options) {
			assert.True(t, o.useDisconnectedReplicas)
		}},
		{"WithReadOnly", WithReadOnly(), func(t *testing.T, o *options) {
			assert.True(t, o.readOnly)
		}},
		{"WithRouteByLatency", WithRouteByLatency(), func(t *testing.T, o *options) {
			assert.True(t, o.routeByLatency)
		}},
		{"WithRouteRandomly", WithRouteRandomly(), func(t *testing.T, o *options) {
			assert.True(t, o.routeRandomly)
		}},
		{"WithMaxRedirects", WithMaxRedirects(8), func(t *testing.T, o *options) {
			assert.Equal(t, 8, o.maxRedirects)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.名称, func(t *testing.T) {
			o := defaultOptions()
			tc.应用(o)
			tc.检查(t, o)
		})
	}
}

// TestIndependentOptions_优先级 独立 Option 优先于 WithSingleOptions 展开
func TestIndependentOptions_优先级(t *testing.T) {
	o := defaultOptions()
	WithSingleOptions(&redis.Options{Username: "from-single"})(o)
	WithUsername("from-explicit")(o)
	assert.Equal(t, "from-explicit", o.username, "WithUsername 应优先于 WithSingleOptions")
	assert.True(t, o.usernameSet)
}
