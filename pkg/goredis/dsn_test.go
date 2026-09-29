package goredis

import (
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// getRedisOpt DSN 解析回归测试
// ============================================================================

// TestGetRedisOpt_空DSN 验证空 DSN 返回错误
func TestGetRedisOpt_空DSN(t *testing.T) {
	_, err := getRedisOpt("", defaultOptions())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dsn 不能为空")
}

// TestGetRedisOpt_主机端口格式 验证 host:port 格式自动补 redis:// 和 /0
func TestGetRedisOpt_主机端口格式(t *testing.T) {
	o := defaultOptions()
	opt, err := getRedisOpt("127.0.0.1:6379", o)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:6379", opt.Addr)
	assert.Equal(t, 0, opt.DB)
}

// TestGetRedisOpt_密码格式 验证 :password@host:port/db 格式
func TestGetRedisOpt_密码格式(t *testing.T) {
	o := defaultOptions()
	opt, err := getRedisOpt(":123456@127.0.0.1:6379/5", o)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:6379", opt.Addr)
	assert.Equal(t, "123456", opt.Password)
	assert.Equal(t, 5, opt.DB)
}

// TestGetRedisOpt_完整URL格式 验证完整 redis:// URL 格式
func TestGetRedisOpt_完整URL格式(t *testing.T) {
	o := defaultOptions()
	opt, err := getRedisOpt("redis://default:123456@127.0.0.1:6379/0", o)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:6379", opt.Addr)
	assert.Equal(t, "123456", opt.Password)
	assert.Equal(t, 0, opt.DB)
}

// TestGetRedisOpt_DSN查询参数保留 验证 DSN query 参数不被 Option 覆盖
// P0-A 回归测试：DSN 中的 dial_timeout/read_timeout 应被 ParseURL 解析并保留
func TestGetRedisOpt_DSN查询参数保留(t *testing.T) {
	o := defaultOptions() // 未设置任何 xxxSet 标志
	opt, err := getRedisOpt("redis://:123@127.0.0.1:6379/0?dial_timeout=10s&read_timeout=3s&write_timeout=5s", o)
	require.NoError(t, err)
	// DSN query 参数应被保留（因为 xxxSet 全为 false，不会覆盖）
	assert.Equal(t, 10*time.Second, opt.DialTimeout, "dial_timeout 应来自 DSN")
	assert.Equal(t, 3*time.Second, opt.ReadTimeout, "read_timeout 应来自 DSN")
	assert.Equal(t, 5*time.Second, opt.WriteTimeout, "write_timeout 应来自 DSN")
}

// TestGetRedisOpt_选项覆盖DSN参数 验证显式 Option 覆盖 DSN query 参数
// P0-A 回归测试：WithDialTimeout(1s) 应覆盖 DSN 的 dial_timeout=10s
func TestGetRedisOpt_选项覆盖DSN参数(t *testing.T) {
	o := defaultOptions()
	WithDialTimeout(1 * time.Second)(o) // 显式设置
	opt, err := getRedisOpt("redis://:123@127.0.0.1:6379/0?dial_timeout=10s", o)
	require.NoError(t, err)
	assert.Equal(t, 1*time.Second, opt.DialTimeout, "WithDialTimeout 应覆盖 DSN 参数")
	// read_timeout 未在 DSN 与 Option 中出现，应为 go-redis 的默认零值
	assert.Equal(t, time.Duration(0), opt.ReadTimeout, "未指定的字段应保持 go-redis 默认值")
}

// TestGetRedisOpt_DSN无路径补零 自动补 /0
func TestGetRedisOpt_DSN无路径补零(t *testing.T) {
	o := defaultOptions()
	opt, err := getRedisOpt("redis://:123@127.0.0.1:6379", o)
	require.NoError(t, err)
	assert.Equal(t, 0, opt.DB, "无路径时应默认 DB=0")
}

// TestGetRedisOpt_DSN带查询无路径补零 自动补 /0，query 参数保留
// P0-B 回归测试：redis://host:6379?max_retries=7 → /0?max_retries=7
// 注：特意使用 7 这类非默认值，避免 go-redis 默认值恒真导致断言失效
func TestGetRedisOpt_DSN带查询无路径补零(t *testing.T) {
	o := defaultOptions()
	opt, err := getRedisOpt("redis://:123@127.0.0.1:6379?max_retries=7", o)
	require.NoError(t, err)
	assert.Equal(t, 0, opt.DB, "无路径时应默认 DB=0")
	// max_retries 由 ParseURL 解析（未配置时 ParseURL 返回 0，非 go-redis 运行时默认值）
	assert.Equal(t, 7, opt.MaxRetries, "max_retries 应从 query 解析")
}

// TestGetRedisOpt_PoolSize未设置不覆盖 默认不覆盖 DSN
// D-4 回归测试：未调用 WithPoolSize 时，ParseURL 的 pool_size 应保留
func TestGetRedisOpt_PoolSize未设置不覆盖(t *testing.T) {
	o := defaultOptions() // poolSizeSet = false
	opt, err := getRedisOpt("redis://:123@127.0.0.1:6379/0", o)
	require.NoError(t, err)
	// go-redis ParseURL 返回的 PoolSize 为 0（运行时由 GOMAXPROCS 决定）
	// 关键：xxxSet 为 false 时不应被错误覆盖为某个固定值
	assert.Equal(t, 0, opt.PoolSize, "未设置 WithPoolSize 时 PoolSize 保持 ParseURL 返回值")
}

// TestGetRedisOpt_PoolSize已设置覆盖 验证 WithPoolSize 覆盖 DSN
func TestGetRedisOpt_PoolSize已设置覆盖(t *testing.T) {
	o := defaultOptions()
	WithPoolSize(50)(o) // 显式设置
	opt, err := getRedisOpt("redis://:123@127.0.0.1:6379/0", o)
	require.NoError(t, err)
	assert.Equal(t, 50, opt.PoolSize, "WithPoolSize(50) 应覆盖默认值")
}

// ============================================================================
// Init 端到端 DSN query 参数测试
// ============================================================================

// TestInit_DSN查询参数端到端 验证 Init 完整链路中 DSN query 参数生效
func TestInit_DSN查询参数端到端(t *testing.T) {
	srv, _ := miniredis.Run()
	defer srv.Close()

	dsn := fmt.Sprintf("redis://%s/0?dial_timeout=10s&read_timeout=3s", srv.Addr())
	rdb, err := Init(dsn)
	require.NoError(t, err)
	defer Close(rdb)

	opts := rdb.Options()
	assert.Equal(t, 10*time.Second, opts.DialTimeout, "DSN dial_timeout 应生效")
	assert.Equal(t, 3*time.Second, opts.ReadTimeout, "DSN read_timeout 应生效")
}

// TestInit_WithPoolSize_覆盖SingleOptions 验证三阶段合并语义
// 优先级: DSN < singleOptions < With* 显式设置
func TestInit_WithPoolSize_覆盖SingleOptions(t *testing.T) {
	srv, _ := miniredis.Run()
	defer srv.Close()

	dsn := fmt.Sprintf("redis://%s/0", srv.Addr())
	rdb, err := Init(dsn,
		WithSingleOptions(&redis.Options{PoolSize: 5}),
		WithPoolSize(50), // 后设置，应覆盖 singleOptions.PoolSize
	)
	require.NoError(t, err)
	defer Close(rdb)

	assert.Equal(t, 50, rdb.Options().PoolSize, "WithPoolSize(50) 应覆盖 singleOptions.PoolSize=5")
}

// TestInit_反序WithSingleOptions_仍由WithPoolSize优先 验证无论选项顺序如何，With* 始终优先于 singleOptions
func TestInit_反序WithSingleOptions_仍由WithPoolSize优先(t *testing.T) {
	srv, _ := miniredis.Run()
	defer srv.Close()

	dsn := fmt.Sprintf("redis://%s/0", srv.Addr())
	rdb, err := Init(dsn,
		WithPoolSize(50), // 先设置
		WithSingleOptions(&redis.Options{PoolSize: 5}), // 后设置
	)
	require.NoError(t, err)
	defer Close(rdb)

	assert.Equal(t, 50, rdb.Options().PoolSize, "With* 始终优先于 singleOptions")
}

// TestInitSingle_WithSingleOptions_优先级 验证 InitSingle 中 With* 优先于 singleOptions
// P0-NEW-A 回归测试：InitSingle 与 Init 保持一致的选项优先级
func TestInitSingle_WithSingleOptions_优先级(t *testing.T) {
	srv, _ := miniredis.Run()
	defer srv.Close()

	rdb, err := InitSingle(srv.Addr(), "", 0,
		WithPoolSize(50),
		WithSingleOptions(&redis.Options{PoolSize: 5}),
	)
	require.NoError(t, err)
	defer Close(rdb)

	assert.Equal(t, 50, rdb.Options().PoolSize, "WithPoolSize(50) 应覆盖 singleOptions.PoolSize=5")
}

// TestInit_DialTimeout_优先级 验证 WithDialTimeout 覆盖 singleOptions.DialTimeout
// P0-NEW-B 回归测试：DialTimeout/ReadTimeout/WriteTimeout/TLSConfig 均应被 With* 覆盖
func TestInit_DialTimeout_优先级(t *testing.T) {
	srv, _ := miniredis.Run()
	defer srv.Close()

	dsn := fmt.Sprintf("redis://%s/0", srv.Addr())
	rdb, err := Init(dsn,
		WithDialTimeout(1*time.Second),
		WithReadTimeout(2*time.Second),
		WithWriteTimeout(3*time.Second),
		WithSingleOptions(&redis.Options{
			DialTimeout:  10 * time.Second,
			ReadTimeout:  20 * time.Second,
			WriteTimeout: 30 * time.Second,
		}),
	)
	require.NoError(t, err)
	defer Close(rdb)

	opts := rdb.Options()
	assert.Equal(t, 1*time.Second, opts.DialTimeout, "WithDialTimeout 应覆盖 singleOptions")
	assert.Equal(t, 2*time.Second, opts.ReadTimeout, "WithReadTimeout 应覆盖 singleOptions")
	assert.Equal(t, 3*time.Second, opts.WriteTimeout, "WithWriteTimeout 应覆盖 singleOptions")
}

// TestInit_仅WithSingleOptions_展开值生效
// P0-LEFTOVER 回归测试：单独使用 WithSingleOptions 时展开值在 Init 路径下生效
func TestInit_仅WithSingleOptions_展开值生效(t *testing.T) {
	srv, _ := miniredis.Run()
	defer srv.Close()

	dsn := fmt.Sprintf("redis://%s/0", srv.Addr())
	rdb, err := Init(dsn,
		WithSingleOptions(&redis.Options{
			PoolSize:     100,
			MinIdleConns: 10,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
		}),
	)
	require.NoError(t, err)
	defer Close(rdb)

	opts := rdb.Options()
	assert.Equal(t, 100, opts.PoolSize, "WithSingleOptions.PoolSize 应生效")
	assert.Equal(t, 10, opts.MinIdleConns, "WithSingleOptions.MinIdleConns 应生效")
	assert.Equal(t, 5*time.Second, opts.DialTimeout, "WithSingleOptions.DialTimeout 应生效")
	assert.Equal(t, 3*time.Second, opts.ReadTimeout, "WithSingleOptions.ReadTimeout 应生效")
	assert.Equal(t, 3*time.Second, opts.WriteTimeout, "WithSingleOptions.WriteTimeout 应生效")
}

// TestInitSingle_仅WithSingleOptions_展开值生效 展开值在 InitSingle 路径下生效
func TestInitSingle_仅WithSingleOptions_展开值生效(t *testing.T) {
	srv, _ := miniredis.Run()
	defer srv.Close()

	rdb, err := InitSingle(srv.Addr(), "", 0,
		WithSingleOptions(&redis.Options{
			PoolSize:     100,
			MinIdleConns: 10,
			DialTimeout:  5 * time.Second,
		}),
	)
	require.NoError(t, err)
	defer Close(rdb)

	opts := rdb.Options()
	assert.Equal(t, 100, opts.PoolSize, "WithSingleOptions.PoolSize 应生效")
	assert.Equal(t, 10, opts.MinIdleConns, "WithSingleOptions.MinIdleConns 应生效")
	assert.Equal(t, 5*time.Second, opts.DialTimeout, "WithSingleOptions.DialTimeout 应生效")
}

// TestInitCluster_WithClusterOptions_扩展字段端到端验证
// P1-3 回归测试：InitCluster 中 WithClusterOptions 的扩展字段端到端生效
// 注：ReadOnly/RouteByLatency/RouteRandomly 会触发 READONLY 等命令，miniredis 不支持，
//
//	通过 expandClusterOptions 单元测试覆盖
func TestInitCluster_WithClusterOptions_扩展字段端到端验证(t *testing.T) {
	srv, _ := miniredis.Run()
	defer srv.Close()

	rdb, err := InitCluster([]string{srv.Addr()}, "", "",
		WithClusterOptions(&redis.ClusterOptions{
			MaxRedirects: 8,
			PoolSize:     50,
			MaxRetries:   3,
		}),
	)
	require.NoError(t, err)
	defer CloseCluster(rdb)

	opts := rdb.Options()
	assert.Equal(t, 8, opts.MaxRedirects, "MaxRedirects 应端到端生效")
	assert.Equal(t, 50, opts.PoolSize, "PoolSize 应端到端生效")
	assert.Equal(t, 3, opts.MaxRetries, "MaxRetries 应端到端生效")
}

// TestInitSentinel_Miniredis不支持报错 验证 miniredis 不支持哨兵时 InitSentinel 正确报错
// 注：SentinelUsername/SentinelPassword 等哨兵专有字段的展开逻辑通过 TestWithSentinelOptions 单元测试覆盖
func TestInitSentinel_Miniredis不支持报错(t *testing.T) {
	srv, _ := miniredis.Run()
	defer srv.Close()

	_, err := InitSentinel("mymaster", []string{srv.Addr()}, "", "",
		WithSentinelOptions(&redis.FailoverOptions{
			PoolSize:   50,
			MaxRetries: 3,
		}),
	)
	// miniredis 不支持哨兵协议，InitSentinel 应报错
	assert.Error(t, err, "miniredis 不支持哨兵，InitSentinel 应报错")
}

// BenchmarkGetRedisOpt 基准测试 DSN 解析性能
// 注：getRedisOpt 不会修改传入的 o（applyExplicitRedisOptions 仅在 xxxSet=true 时覆盖到 opt），
// 因此循环外复用同一 o 是安全的；若未来实现变化需在循环内重建 o
func BenchmarkGetRedisOpt(b *testing.B) {
	dsn := "redis://:123@127.0.0.1:6379/0?dial_timeout=10s"
	o := defaultOptions()
	WithPoolSize(50)(o)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = getRedisOpt(dsn, o)
	}
}

// BenchmarkEnsureDSNPath 基准测试 ensureDSNPath 纯字符串拼接性能
// 覆盖三种形态：无路径（拼接）、空路径（补 0）、已有路径（原样返回）
func BenchmarkEnsureDSNPath(b *testing.B) {
	cases := []string{
		"redis://127.0.0.1:6379",                      // 无路径 → 拼接 /0
		"redis://127.0.0.1:6379/",                     // 空路径 → 补 0
		"redis://:123@127.0.0.1:6379/5?max_retries=7", // 已有路径 → 原样
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, dsn := range cases {
			_ = ensureDSNPath(dsn)
		}
	}
}

// ============================================================================
// P1 修复回归测试：rediss:// TLS 路径与特殊字符 DSN 保护
// ============================================================================

// TestGetRedisOpt_RedissURL启用TLS 验证 rediss:// 前缀自动启用 TLS 配置
// P1-5 回归测试：此前 TLS 分支零覆盖
func TestGetRedisOpt_RedissURL启用TLS(t *testing.T) {
	o := defaultOptions()
	opt, err := getRedisOpt("rediss://:pass@127.0.0.1:6380/0", o)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:6380", opt.Addr)
	assert.NotNil(t, opt.TLSConfig, "rediss:// 应自动启用 TLSConfig")
}

// TestGetRedisOpt_RedissURL_无路径 验证 rediss:// 缺失路径时自动补 /0 且 TLS 仍生效
func TestGetRedisOpt_RedissURL_无路径(t *testing.T) {
	o := defaultOptions()
	opt, err := getRedisOpt("rediss://127.0.0.1:6380", o)
	require.NoError(t, err)
	assert.Equal(t, 0, opt.DB, "无路径时应默认 DB=0")
	assert.NotNil(t, opt.TLSConfig, "rediss:// 应自动启用 TLSConfig")
}

// TestGetRedisOpt_特殊字符密码不被破坏 验证 ensureDSNPath 不做 net/url round-trip
// P1-6 回归测试：URL 编码的密码（%40/%2B）经补路径后不被二次编码或解码破坏
func TestGetRedisOpt_特殊字符密码不被破坏(t *testing.T) {
	cases := []struct {
		name             string
		dsn              string
		expectedPassword string
	}{
		{"有路径保持原样", "redis://:p%40ss%2Bw@127.0.0.1:6379/0", "p@ss+w"},
		{"无路径补 /0", "redis://:p%40ss%2Bw@127.0.0.1:6379", "p@ss+w"},
		{"无路径带 query", "redis://:p%40ss%2Bw@127.0.0.1:6379?max_retries=7", "p@ss+w"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			o := defaultOptions()
			opt, err := getRedisOpt(tt.dsn, o)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedPassword, opt.Password, "密码应保持 URL 解码后的正确值")
		})
	}
}
