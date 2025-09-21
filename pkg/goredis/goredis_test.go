package goredis

import (
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

// TestInit 测试 Init 函数连接 Redis 的各种情况
func TestInit(t *testing.T) {
	// 启动一个模拟的 Redis 服务器用于测试
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	// 定义测试用例参数结构
	type args struct {
		redisURL string
	}
	
	// 定义测试用例
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
			name:    "有密码无数据库",
			args:    args{"root:123456@" + addr},
			wantErr: false,
		},
		{
			name:    "无密码有数据库",
			args:    args{addr + "/5"},
			wantErr: false,
		},
		{
			name:    "有密码有数据库",
			args:    args{fmt.Sprintf("root:123456@%s/5", addr)},
			wantErr: false,
		},
		{
			name:    "带 Redis 前缀",
			args:    args{fmt.Sprintf("redis://root:123456@%s/5", addr)},
			wantErr: false,
		},
	}
	
	// 遍历执行测试用例
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 使用各种配置选项初始化 Redis 客户端
			rdb, err := Init(tt.args.redisURL,
				WithDialTimeout(time.Second),       // 设置连接超时时间
				WithReadTimeout(time.Second),       // 设置读取超时时间
				WithWriteTimeout(time.Second),      // 设置写入超时时间
				WithPoolSize(20),                   // 设置连接池大小
				WithMinIdleConns(5),                // 设置最小空闲连接数
				WithMaxConnAge(time.Hour),          // 设置连接最大存活时间
				WithPoolTimeout(time.Second),       // 设置连接池超时时间
				WithIdleTimeout(time.Hour),         // 设置连接最大空闲时间
				WithEnableTrace(),                  // 启用追踪
				WithTracing(nil),                   // 设置追踪提供者（nil 表示不设置）
				WithTLSConfig(nil),                 // 设置 TLS 配置（nil 表示不设置）
			)
			
			// 检查错误是否符合预期
			if (err != nil) != tt.wantErr {
				t.Logf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			// 测试结束后关闭连接
			defer Close(rdb)
			// 断言客户端不为空
			assert.NotNil(t, rdb)
		})
	}
}

// TestInitSingle 测试 InitSingle 函数连接单机 Redis
func TestInitSingle(t *testing.T) {
	// 启动一个模拟的 Redis 服务器用于测试
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	// 使用各种配置选项初始化单机 Redis 客户端
	rdb, err := InitSingle(addr, "", 0,
		WithDialTimeout(time.Second),           // 设置连接超时时间
		WithReadTimeout(time.Second),           // 设置读取超时时间
		WithWriteTimeout(time.Second),          // 设置写入超时时间
		WithPoolSize(20),                       // 设置连接池大小
		WithMinIdleConns(5),                    // 设置最小空闲连接数
		WithMaxConnAge(time.Hour),              // 设置连接最大存活时间
		WithPoolTimeout(time.Second),           // 设置连接池超时时间
		WithIdleTimeout(time.Hour),             // 设置连接最大空闲时间
		WithTracing(nil),                       // 设置追踪提供者（nil 表示不设置）
		WithTLSConfig(nil),                     // 设置 TLS 配置（nil 表示不设置）
		WithSingleOptions(nil),                 // 设置单机选项（nil 表示不设置）
	)
	
	// 断言没有错误且客户端不为空
	assert.Nil(t, err)
	assert.NotNil(t, rdb)
}

// TestInitSentinel 测试 InitSentinel 函数连接哨兵模式 Redis
func TestInitSentinel(t *testing.T) {
	// 启动一个模拟的 Redis 服务器用于测试
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	// 使用各种配置选项初始化哨兵模式 Redis 客户端
	rdb, err := InitSentinel("mymaster", []string{addr}, "", "",
		WithDialTimeout(time.Second),           // 设置连接超时时间
		WithReadTimeout(time.Second),           // 设置读取超时时间
		WithWriteTimeout(time.Second),          // 设置写入超时时间
		WithPoolSize(20),                       // 设置连接池大小
		WithMinIdleConns(5),                    // 设置最小空闲连接数
		WithMaxConnAge(time.Hour),              // 设置连接最大存活时间
		WithPoolTimeout(time.Second),           // 设置连接池超时时间
		WithIdleTimeout(time.Hour),             // 设置连接最大空闲时间
		WithTracing(nil),                       // 设置追踪提供者（nil 表示不设置）
		WithTLSConfig(nil),                     // 设置 TLS 配置（nil 表示不设置）
		WithSentinelOptions(nil),               // 设置哨兵选项（nil 表示不设置）
	)
	
	// 记录错误日志并断言客户端不为空
	t.Log(err)
	assert.NotNil(t, rdb)
}

// TestInitCluster 测试 InitCluster 函数连接集群模式 Redis
func TestInitCluster(t *testing.T) {
	// 启动一个模拟的 Redis 服务器用于测试
	redisServer, _ := miniredis.Run()
	defer redisServer.Close()
	addr := redisServer.Addr()

	// 使用各种配置选项初始化集群模式 Redis 客户端
	clusterRdb, err := InitCluster([]string{addr}, "", "",
		WithDialTimeout(time.Second*15),        // 设置连接超时时间
		WithReadTimeout(time.Second),           // 设置读取超时时间
		WithWriteTimeout(time.Second),          // 设置写入超时时间
		WithPoolSize(20),                       // 设置连接池大小
		WithMinIdleConns(5),                    // 设置最小空闲连接数
		WithMaxConnAge(time.Hour),              // 设置连接最大存活时间
		WithPoolTimeout(time.Second),           // 设置连接池超时时间
		WithIdleTimeout(time.Hour),             // 设置连接最大空闲时间
		WithTracing(nil),                       // 设置追踪提供者（nil 表示不设置）
		WithTLSConfig(nil),                     // 设置 TLS 配置（nil 表示不设置）
		WithClusterOptions(nil),                // 设置集群选项（nil 表示不设置）
	)
	
	// 测试结束后关闭集群连接
	defer CloseCluster(clusterRdb)
	// 断言没有错误且客户端不为空
	assert.Nil(t, err)
	assert.NotNil(t, clusterRdb)
}