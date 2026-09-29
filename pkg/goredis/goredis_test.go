package goredis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInit_连接各种情况 测试 Init 函数连接 Redis 的各种情况
func TestInit_连接各种情况(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	type args struct {
		redisURL string
	}

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name:    "无密码无数据库",
			args:    args{addr},
			wantErr: false,
		},
		{
			name:    "无密码有数据库",
			args:    args{addr + "/5"},
			wantErr: false,
		},
		{
			name:    "空 DSN",
			args:    args{""},
			wantErr: true,
		},
		{
			name:    "无效 DSN 格式",
			args:    args{"redis://"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdb, err := Init(tt.args.redisURL,
				WithDialTimeout(time.Second),
				WithReadTimeout(time.Second),
				WithWriteTimeout(time.Second),
				WithPoolSize(20),
				WithMinIdleConns(5),
				WithMaxConnAge(time.Hour),
				WithPoolTimeout(time.Second),
				WithIdleTimeout(time.Hour),
				WithTLSConfig(nil),
			)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, rdb)
			defer Close(rdb)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			assert.NoError(t, rdb.Ping(ctx).Err())
		})
	}
}

// TestInitWithPassword_带密码 验证带密码的 DSN 连接
func TestInitWithPassword_带密码(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()
	redisServer.RequireAuth("123456")

	passwordTests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{"有密码无数据库", ":123456@" + addr, false},
		{"有密码有数据库", fmt.Sprintf(":123456@%s/5", addr), false},
		{"带 Redis 前缀", fmt.Sprintf("redis://:123456@%s/5", addr), false},
		{"密码错误", ":wrong@" + addr, true},
	}

	for _, tt := range passwordTests {
		t.Run(tt.name, func(t *testing.T) {
			rdb, err := Init(tt.dsn, WithPoolSize(5))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, rdb)
			defer Close(rdb)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			assert.NoError(t, rdb.Ping(ctx).Err())
		})
	}
}

// TestInitSingle_单机连接 测试 InitSingle 函数连接单机 Redis
func TestInitSingle_单机连接(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	rdb, err := InitSingle(addr, "", 0,
		WithDialTimeout(time.Second),
		WithReadTimeout(time.Second),
		WithWriteTimeout(time.Second),
		WithPoolSize(20),
		WithMinIdleConns(5),
		WithMaxConnAge(time.Hour),
		WithPoolTimeout(time.Second),
		WithIdleTimeout(time.Hour),
		WithTLSConfig(nil),
		WithSingleOptions(nil),
	)

	require.NoError(t, err)
	require.NotNil(t, rdb)
	defer Close(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.NoError(t, rdb.Ping(ctx).Err())
}

// TestInitSentinel_哨兵模式 验证 Sentinel 模式在 miniredis 下的错误处理
func TestInitSentinel_哨兵模式(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	rdb, err := InitSentinel("mymaster", []string{addr}, "", "",
		WithDialTimeout(time.Second),
		WithReadTimeout(time.Second),
		WithPoolSize(20),
		WithSentinelOptions(nil),
	)

	// miniredis 不支持 Sentinel 协议，预期连接失败
	assert.Error(t, err, "miniredis 不支持 Sentinel 协议，预期连接失败")
	if rdb != nil {
		_ = rdb.Close()
	}
}

// TestInitCluster_集群连接 测试 InitCluster 函数
func TestInitCluster_集群连接(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	clusterRdb, err := InitCluster([]string{addr}, "", "",
		WithDialTimeout(time.Second*15),
		WithReadTimeout(time.Second),
		WithPoolSize(20),
		WithClusterOptions(nil),
	)

	if err != nil {
		t.Skipf("miniredis 集群模式不完全支持，跳过: %v", err)
		return
	}

	defer CloseCluster(clusterRdb)
	require.NotNil(t, clusterRdb)
}

// TestClose_幂等 验证 Close 幂等性
func TestClose_幂等(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	rdb, err := Init(addr, WithPoolSize(5))
	require.NoError(t, err)
	require.NotNil(t, rdb)

	assert.NoError(t, Close(rdb))
	assert.NoError(t, Close(rdb))
	assert.NoError(t, Close(nil))
}

// TestCloseCluster_幂等 验证 CloseCluster 幂等性
func TestCloseCluster_幂等(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	clusterRdb, err := InitCluster([]string{addr}, "", "",
		WithDialTimeout(time.Second*5),
		WithClusterOptions(nil),
	)
	if err != nil {
		t.Skipf("miniredis 集群模式不完全支持，跳过: %v", err)
		return
	}

	assert.NoError(t, CloseCluster(clusterRdb))
	assert.NoError(t, CloseCluster(clusterRdb))
	assert.NoError(t, CloseCluster(nil))
}

// ============================================================================
// 防御性与快速失败测试
// ============================================================================

// TestInit_NilOption防御 验证 nil Option 防御：跳过而不 panic
func TestInit_NilOption防御(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()

	rdb, err := Init(redisServer.Addr(), nil)
	require.NoError(t, err)
	defer Close(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.NoError(t, rdb.Ping(ctx).Err())
}

// TestInit_WithSingleOptions误用Addr_快速失败
// 配置类错误在 Init 启动阶段返回错误，而非 log 后静默忽略
func TestInit_WithSingleOptions误用Addr_快速失败(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()

	rdb, err := Init(redisServer.Addr(),
		WithSingleOptions(&redis.Options{Addr: "ignored:6379", Password: "p", DB: 1}),
	)
	require.Error(t, err, "误用 Addr/Password/DB 应快速失败")
	assert.Nil(t, rdb)
	assert.Contains(t, err.Error(), "Addr/Password/DB")
}

// TestInitWithContext_已取消Context 验证 Ctx 变体感知外部取消
func TestInitWithContext_已取消Context(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := InitWithContext(ctx, redisServer.Addr())
	require.Error(t, err, "已取消的 ctx 应导致初始化失败")
}

// TestProbeLuaScriptChannel_失败仅告警不中断
// 探测失败仅通过全局 pkg/logger 告警不中断；关闭后的客户端触发失败路径验证告警被接收
func TestProbeLuaScriptChannel_失败仅告警不中断(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()

	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})

	warns := captureWarn(t)
	// 正常探测（miniredis 支持 EVAL）不产生告警
	probeLuaScriptChannel(context.Background(), rdb)
	assert.Empty(t, warns.msgs(), "正常探测不应告警")

	require.NoError(t, rdb.Close())
	// 客户端已关闭，探测失败应走全局 logger 告警而非标准库 slog / log.Printf
	probeLuaScriptChannel(context.Background(), rdb)
	require.Len(t, warns.msgs(), 1, "探测失败应告警一次")
}

// ============================================================================
// Shutdown 优雅关闭测试
// ============================================================================

// TestShutdown_正常与幂等 验证等待归还后关闭、重复调用与 nil 安全
func TestShutdown_正常与幂等(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()

	rdb, err := Init(redisServer.Addr())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, Shutdown(ctx, rdb))
	// 已关闭后重复 Shutdown 仍幂等
	require.NoError(t, Shutdown(ctx, rdb))
	// nil 安全
	require.NoError(t, Shutdown(ctx, nil))
}

// TestShutdown_已取消Context 仍强制关闭（不泄漏连接）并返回 ctx 错误
func TestShutdown_已取消Context(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()

	rdb, err := Init(redisServer.Addr())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 已取消时池内无飞行命令，可能直接归还成功；两种结果都必须完成关闭
	shutdownErr := Shutdown(ctx, rdb)
	if shutdownErr != nil {
		assert.ErrorIs(t, shutdownErr, context.Canceled)
	}
	// 关闭后再次操作应报错，验证连接确实已释放
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	assert.Error(t, rdb.Ping(ctx2).Err(), "Shutdown 后客户端应已关闭")
}

// TestShutdownCluster_正常与幂等 验证集群版优雅关闭（miniredis 不支持时跳过）
func TestShutdownCluster_正常与幂等(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()

	clusterRdb, err := InitCluster([]string{redisServer.Addr()}, "", "",
		WithDialTimeout(time.Second*5),
		WithClusterOptions(nil),
	)
	if err != nil {
		t.Skipf("miniredis 集群模式不完全支持，跳过: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, ShutdownCluster(ctx, clusterRdb))
	require.NoError(t, ShutdownCluster(ctx, clusterRdb))
	require.NoError(t, ShutdownCluster(ctx, nil))
}

// TestShutdown_正常路径无告警 验证无等待超时的正常关闭不产生 Warn 日志
func TestShutdown_正常路径无告警(t *testing.T) {
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()

	rdb, err := Init(redisServer.Addr())
	require.NoError(t, err)

	warns := captureWarn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, Shutdown(ctx, rdb))
	// 无等待告警时不应有日志输出
	assert.Empty(t, warns.msgs(), "正常关闭不应产生 Warn 日志")
}
