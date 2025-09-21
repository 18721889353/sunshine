## goredis

`goredis` 是对 [go-redis](https://github.com/redis/go-redis) 库的封装。

<br>

### 使用示例

#### 单机 Redis

```go
// 方式1：Redis 6.0 或更高版本
redisCli, err := goredis.Init("default:123456@127.0.0.1:6379") // 可以设置超时、TLS、追踪等参数，如 goredis.Withxxx()
if err != nil {
    panic("goredis.Init error: " + err.Error())
}

// 方式2：Redis 5.0 或更低版本
redisCli, err := goredis.InitSingle("127.0.0.1:6379", "123456", 0) // 可以设置超时、TLS、追踪等参数，如 goredis.Withxxx()
```

<br>

#### 哨兵模式

```go
addrs := []string{"127.0.0.1:6380", "127.0.0.1:6381", "127.0.0.1:6382", "127.0.0.1:26380", "127.0.0.1:26381", "127.0.0.1:26382"}
rdbCli, err := goredis.InitSentinel("mymaster", addrs, "", "123456") // 可以设置超时、TLS、追踪等参数，如 goredis.Withxxx()
```

<br>

#### 集群模式

```go
addrs := []string{"127.0.0.1:6380", "127.0.0.1:6381", "127.0.0.1:6382", "127.0.0.1:6383", "127.0.0.1:6384", "127.0.0.1:6385"}
clusterRdb, err := goredis.InitCluster(addrs, "", "123456") // 可以设置超时、TLS、追踪等参数，如 goredis.Withxxx()
```

<br>

### 配置选项

以下配置选项可用：

- `WithDialTimeout`：设置 Redis 连接的拨号超时时间
- `WithReadTimeout`：设置 Redis 操作的读取超时时间
- `WithWriteTimeout`：设置 Redis 操作的写入超时时间
- `WithPoolSize`：设置 socket 连接的最大数量
- `WithMinIdleConns`：设置空闲连接的最小数量
- `WithMaxConnAge`：设置连接可重用的最大时间
- `WithPoolTimeout`：设置在连接池中等待连接的时间
- `WithIdleTimeout`：设置客户端关闭空闲连接的时间
- `WithIdleCheckFrequency`：设置空闲连接检查器的检查频率
- `WithTLSConfig`：为安全连接设置 TLS 配置
- `WithTracing`：启用 OpenTelemetry 追踪
- `WithSingleOptions`：为单机 Redis 实例设置自定义选项
- `WithSentinelOptions`：为 Redis 哨兵设置自定义选项
- `WithClusterOptions`：为 Redis 集群设置自定义选项

<br>

官方文档 https://redis.uptrace.dev/zh/guide/go-redis.html