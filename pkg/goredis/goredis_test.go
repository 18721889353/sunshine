package goredis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInit 测试 Init 函数连接 Redis 的各种情况
func TestInit(t *testing.T) {
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

// TestInitWithPassword 验证带密码的 DSN 连接
func TestInitWithPassword(t *testing.T) {
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

// TestInitSingle 测试 InitSingle 函数连接单机 Redis
func TestInitSingle(t *testing.T) {
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

// TestInitSentinel 验证 Sentinel 模式在 miniredis 下的错误处理
func TestInitSentinel(t *testing.T) {
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

// TestInitCluster 测试 InitCluster 函数
func TestInitCluster(t *testing.T) {
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

// TestCloseIdempotent 验证 Close 幂等性
func TestCloseIdempotent(t *testing.T) {
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

// TestCloseClusterIdempotent 验证 CloseCluster 幂等性
func TestCloseClusterIdempotent(t *testing.T) {
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
