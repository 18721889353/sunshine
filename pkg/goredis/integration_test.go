//go:build integration

package goredis

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireRealRedis 从环境变量读取真实 Redis DSN，未设置时跳过
func requireRealRedis(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("GOREDIS_TEST_DSN")
	if dsn == "" {
		t.Skip("GOREDIS_TEST_DSN 未设置，跳过集成测试")
	}
	return dsn
}

// TestIntegration_Init 真实 Redis 连接测试
// 运行方式：
//
//	cd pkg/goredis
//	GOREDIS_TEST_DSN="redis://:password@host:port/0" go test -tags=integration -v -run TestIntegration -count=1
func TestIntegration_Init(t *testing.T) {
	dsn := requireRealRedis(t)

	rdb, err := Init(dsn,
		WithDialTimeout(5*time.Second),
		WithReadTimeout(3*time.Second),
		WithWriteTimeout(3*time.Second),
		WithPoolSize(10),
	)
	require.NoError(t, err, "Init 连接真实 Redis 失败")
	require.NotNil(t, rdb)
	defer Close(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Ping 连通性
	assert.NoError(t, rdb.Ping(ctx).Err(), "Ping 失败")

	// 2. SET / GET 读写验证
	testKey := fmt.Sprintf("goredis:test:%d", time.Now().UnixNano())
	testVal := "integration-test-value"

	err = rdb.Set(ctx, testKey, testVal, 30*time.Second).Err()
	require.NoError(t, err, "SET 失败")

	got, err := rdb.Get(ctx, testKey).Result()
	require.NoError(t, err, "GET 失败")
	assert.Equal(t, testVal, got, "GET 返回值不匹配")

	// 3. DEL 清理
	err = rdb.Del(ctx, testKey).Err()
	require.NoError(t, err, "DEL 失败")

	_, err = rdb.Get(ctx, testKey).Result()
	assert.ErrorIs(t, err, ErrRedisNotFound, "DEL 后 GET 应返回 nil")

	// 4. Lua 脚本执行（验证预热效果）
	result, err := rdb.Eval(ctx, "return 42", nil).Result()
	require.NoError(t, err, "EVAL Lua 脚本失败")
	assert.Equal(t, int64(42), result, "Lua 脚本返回值不匹配")

	// 5. EVALSHA 验证（redsync 依赖此能力）
	shaCmd := rdb.ScriptLoad(ctx, "return 'hello'")
	require.NoError(t, shaCmd.Err(), "SCRIPT LOAD 失败")
	shaStr := shaCmd.Val()

	shaResult, err := rdb.EvalSha(ctx, shaStr, nil).Result()
	require.NoError(t, err, "EVALSHA 失败")
	assert.Equal(t, "hello", shaResult, "EVALSHA 返回值不匹配")

	t.Log("真实 Redis 集成测试全部通过")
}

// TestIntegration_InitSingle 真实 Redis 单机连接测试
func TestIntegration_InitSingle(t *testing.T) {
	dsn := requireRealRedis(t)

	u, err := url.Parse(dsn)
	require.NoError(t, err, "解析 DSN 失败")

	host := u.Host // 含端口，如 43.143.78.234:6379
	password := ""
	if u.User != nil {
		password, _ = u.User.Password()
	}

	rdb, err := InitSingle(host, password, 0,
		WithDialTimeout(5*time.Second),
		WithReadTimeout(3*time.Second),
		WithWriteTimeout(3*time.Second),
		WithPoolSize(10),
	)
	require.NoError(t, err, "InitSingle 连接真实 Redis 失败")
	require.NotNil(t, rdb)
	defer Close(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assert.NoError(t, rdb.Ping(ctx).Err(), "Ping 失败")
	t.Log("InitSingle 真实 Redis 集成测试通过")
}

// TestIntegration_LuaProbe 验证 Lua 脚本通道探测
func TestIntegration_LuaProbe(t *testing.T) {
	dsn := requireRealRedis(t)

	rdb, err := Init(dsn, WithPoolSize(5))
	require.NoError(t, err)
	defer Close(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 连续执行多次 EVALSHA（模拟 redsync 场景）
	// Lua 通道探测已在 Init 时完成，此处验证 EVALSHA 正常工作
	script := "redis.call('set', KEYS[1], ARGV[1])\nreturn redis.call('get', KEYS[1])"
	shaCmd := rdb.ScriptLoad(ctx, script)
	require.NoError(t, shaCmd.Err(), "SCRIPT LOAD 失败")
	shaStr := shaCmd.Val()

	for i := 0; i < 5; i++ {
		testKey := fmt.Sprintf("goredis:lua:preheat:%d", i)
		result, err := rdb.EvalSha(ctx, shaStr, []string{testKey}, "value").Result()
		require.NoError(t, err, "第 %d 次 EVALSHA 失败", i)
		assert.Equal(t, "value", result, "第 %d 次 EVALSHA 返回值不匹配", i)

		// 清理
		_ = rdb.Del(ctx, testKey)
	}

	t.Log("Lua 脚本通道探测验证通过")
}

// TestIntegration_DSNQueryParams 验证 DSN query 参数在集成环境中生效
func TestIntegration_DSNQueryParams(t *testing.T) {
	dsn := requireRealRedis(t)

	// 确保 DSN 中不带 query 参数时不 panic
	rdb, err := Init(dsn,
		WithDialTimeout(5*time.Second),
		WithReadTimeout(3*time.Second),
		WithWriteTimeout(3*time.Second),
	)
	require.NoError(t, err)
	defer Close(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	assert.NoError(t, rdb.Ping(ctx).Err())

	// 验证 DSN query 参数能正确传递（模拟带 query 的 DSN）
	parts := strings.SplitN(dsn, "?", 2)
	dsnWithQuery := parts[0] + "?max_retries=3"
	rdb2, err := Init(dsnWithQuery,
		WithDialTimeout(5*time.Second),
	)
	require.NoError(t, err)
	defer Close(rdb2)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	assert.NoError(t, rdb2.Ping(ctx2).Err(), "带 query 参数的 DSN 连接失败")
}
