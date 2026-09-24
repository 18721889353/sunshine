# goredis

封装 [go-redis](https://github.com/redis/go-redis/v9) 库，提供单机/哨兵/集群三种连接模式、Lua 脚本通道探测、OpenTelemetry 追踪集成及 request_id 链路注入能力。

## 架构概览

```
goredis/
├── goredis.go          # 核心：Init/InitSingle/InitSentinel/InitCluster/Close、setupClient 统一初始化、Lua 通道探测
├── option.go           # Functional Options：WithPoolSize/WithDialTimeout/WithTracing/WithRequestIDExtractor 等
├── requestid_hook.go   # Redis Hook：从 Context 提取 request_id 并设置到 Span 属性，evalsha 分布式锁识别
├── goredis_test.go     # 单元测试：Init/Close 幂等性、DSN 解析
├── option_test.go      # 单元测试：默认值、全部 With* 选项
├── requestid_hook_test.go  # 单元测试：Span 属性增强、request_id 注入
├── integration_test.go # 集成测试（需 GOREDIS_TEST_DSN 环境变量）
├── dsn_test.go         # DSN 解析回归测试（query 参数、merge 语义）
└── README.md
```

**核心设计原则：**

- **Lua 脚本通道探测**：`Init` 时执行 `EVAL "return 1" nil` 确认 Lua 通道可用；redsync 的脚本加载由 go-redis 内置 NOSCRIPT fallback 保证，不依赖本探测
- **统一初始化路径**：`setupClient` 消除 4 个 Init 函数的重复代码（追踪挂载 → Hook 注册 → Ping 测试 → Lua 探测）
- **幂等关闭**：`Close`/`CloseCluster` 重复调用安全，第二次调用返回 nil
- **request_id 注入**：通过 `RequestIDExtractor` 函数类型解耦 goredis 与 logger 包，上层注入具体实现
- **evalsha 分布式锁识别**：自动识别 `lock:`、`/dlock/` 前缀，Span 名称显示锁标识便于排查

---

## 使用场景选择

### 场景一：DSN 初始化（推荐）

**适用场景**：生产环境，通过 DSN 字符串连接 Redis，支持所有认证格式。

```go
package main

import (
	"log"

	"github.com/18721889353/sunshine/pkg/goredis"
)

func main() {
	// 无密码
	rdb, err := goredis.Init("127.0.0.1:6379")
	if err != nil {
		log.Fatal(err)
	}
	defer goredis.Close(rdb)

	// 有密码（Redis 6.0+）
	rdb, err = goredis.Init(":123456@127.0.0.1:6379/0")
	if err != nil {
		log.Fatal(err)
	}

	// 完整 URL 格式
	rdb, err = goredis.Init("redis://default:123456@127.0.0.1:6379/0")
	if err != nil {
		log.Fatal(err)
	}
}
```

**内部行为**：

1. `getRedisOpt` 解析 DSN（`net/url` + `redis.ParseURL`）→ 仅覆盖显式设置的 Option
2. 创建 `redis.NewClient` → 挂载追踪/Hook → Ping 测试 → Lua 探测
3. DSN 无 `/db` 后缀时自动追加 `/0`

**DSN 格式支持**：`host:port` | `:password@host:port/db` | `redis://user:password@host:port/db`

---

### 场景二：单机模式（InitSingle）

**适用场景**：需要直接传入 `addr`/`password`/`db` 参数，不经过 DSN 解析。

```go
rdb, err := goredis.InitSingle("127.0.0.1:6379", "123456", 0,
	goredis.WithDialTimeout(5 * time.Second),
	goredis.WithPoolSize(20),
)
if err != nil {
	log.Fatal(err)
}
defer goredis.Close(rdb)
```

---

### 场景三：哨兵模式

**适用场景**：高可用部署，通过 Sentinel 自动发现主节点。

```go
addrs := []string{"127.0.0.1:26380", "127.0.0.1:26381", "127.0.0.1:26382"}
rdb, err := goredis.InitSentinel("mymaster", addrs, "", "123456",
	goredis.WithDialTimeout(5 * time.Second),
	goredis.WithSentinelOptions(&redis.FailoverOptions{
		MaxRetries: 3,
	}),
)
if err != nil {
	log.Fatal(err)
}
defer goredis.Close(rdb)
```

---

### 场景四：集群模式

**适用场景**：分片部署，数据自动分布在多个节点。

```go
addrs := []string{"127.0.0.1:7000", "127.0.0.1:7001", "127.0.0.1:7002"}
clusterRdb, err := goredis.InitCluster(addrs, "", "123456",
	goredis.WithDialTimeout(5 * time.Second),
	goredis.WithClusterOptions(&redis.ClusterOptions{
		MaxRetries: 3,
	}),
)
if err != nil {
	log.Fatal(err)
}
defer goredis.CloseCluster(clusterRdb)
```

---

### 场景五：启用 OpenTelemetry 追踪

**适用场景**：需要将 Redis 操作纳入分布式链路追踪。

```go
rdb, err := goredis.Init("redis://:123456@127.0.0.1:6379/0",
	goredis.WithTracing(tracerProvider),
	goredis.WithRequestIDExtractor(func(ctx context.Context) string {
		return logger.GetRequestID(ctx) // 从 Context 提取 request_id
	}),
)
```

**内部行为**：`redisotel.InstrumentTracing` 挂载 Redis 追踪 Hook → `requestIDHook` 注入 `request_id` 属性 → 命名规则：普通命令 `redis.<cmd>`，分布式锁 `redis.lock:<name>`

---

## Option 列表

### 连接池与超时

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithPoolSize(n)` | 连接池最大连接数 | `go-redis 默认值` |
| `WithMinIdleConns(n)` | 最小空闲连接数 | `0` |
| `WithMaxConnAge(d)` | 连接最大存活时间 | `0`（不限） |
| `WithPoolTimeout(d)` | 从连接池获取连接的超时时间 | `0`（不限） |
| `WithIdleTimeout(d)` | 空闲连接最大存活时间 | `0`（不限） |
| `WithDialTimeout(d)` | 连接拨号超时时间 | `0`（不限） |
| `WithReadTimeout(d)` | 读取操作超时时间 | `0`（不限） |
| `WithWriteTimeout(d)` | 写入操作超时时间 | `0`（不限） |
| `WithTLSConfig(cfg)` | TLS 安全连接配置 | `nil` |

### 重试与网络

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithMaxRetries(n)` | 最大重试次数（0=禁用，-1=默认） | `go-redis 默认值` |
| `WithMinRetryBackoff(d)` | 最小重试退避时间 | `go-redis 默认值` |
| `WithMaxRetryBackoff(d)` | 最大重试退避时间 | `go-redis 默认值` |
| `WithNetwork(n)` | 网络类型（tcp / unix） | `tcp` |
| `WithUsername(u)` | Redis ACL 用户名 | `""` |
| `WithClientName(name)` | 客户端名称（CLIENT SETNAME） | `""` |
| `WithProtocol(v)` | RESP 协议版本（2 或 3） | `go-redis 默认值` |
| `WithOnConnect(fn)` | 连接建立时的回调函数 | `nil` |
| `WithDialer(fn)` | 自定义网络连接创建函数 | `nil` |

### 哨兵专用

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithSentinelUsername(u)` | 哨兵 ACL 用户名 | `""` |
| `WithSentinelPassword(p)` | 哨兵密码 | `""` |
| `WithUseDisconnectedReplicas()` | 启用哨兵断连副本路由 | `false` |

### 集群专用

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithReadOnly()` | 启用集群只读命令路由到副本节点 | `false` |
| `WithRouteByLatency()` | 启用集群延迟路由 | `false` |
| `WithRouteRandomly()` | 启用集群随机路由 | `false` |
| `WithMaxRedirects(n)` | 集群最大重定向次数 | `go-redis 默认值` |

### 追踪与链路

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithTracing(tp)` | 启用 OpenTelemetry 追踪 | `nil`（不启用） |
| `WithRequestIDExtractor(fn)` | 自定义 request_id 提取函数 | `nil` |

### Options 结构体展开

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithSingleOptions(opt)` | 展开 `redis.Options` 非零字段到 options（With* 显式设置优先） | `nil` |
| `WithSentinelOptions(opt)` | 展开 `redis.FailoverOptions` 非零字段到 options | `nil` |
| `WithClusterOptions(opt)` | 展开 `redis.ClusterOptions` 非零字段到 options | `nil` |

**WithSingleOptions 支持的字段**：`PoolSize`、`MinIdleConns`、`PoolTimeout`、`ConnMaxLifetime`、`ConnMaxIdleTime`、`DialTimeout`、`ReadTimeout`、`WriteTimeout`、`TLSConfig`、`MaxRetries`、`MinRetryBackoff`、`MaxRetryBackoff`、`Network`、`Username`、`ClientName`、`Protocol`、`OnConnect`、`Dialer`。

> 注意：
> - `Addr`/`Password`/`DB` 请通过 `InitSingle`/`Init` 参数或 DSN 传入，`WithSingleOptions` 不读取这三个字段。
> - `MaxRetries=0` 通过 `WithSingleOptions` 传入时视为"未设置"，不会禁用重试。如需显式禁用重试，请使用 `WithMaxRetries(0)`。
> - `MinRetryBackoff`/`MaxRetryBackoff=0` 同理，如需显式设置请使用对应的 `With*` 函数。

---

## API 速查

### Init — DSN 初始化

```go
func Init(dsn string, opts ...Option) (*redis.Client, error)
```

- 支持 `host:port`、`:password@host:port/db`、`redis://user:pass@host:port/db` 三种 DSN 格式
- 无 `/db` 时自动追加 `/0`
- 返回 error：连接失败、DSN 解析失败

---

### InitSingle — 单机初始化

```go
func InitSingle(addr string, password string, db int, opts ...Option) (*redis.Client, error)
```

- 直接传入 `addr`/`password`/`db`，不经过 DSN 解析
- 适用于 Redis 5.0 及以下版本

---

### InitSentinel — 哨兵初始化

```go
func InitSentinel(masterName string, addrs []string, username string, password string, opts ...Option) (*redis.Client, error)
```

- `masterName`：Sentinel 监控的主节点名称
- `addrs`：Sentinel 节点地址列表

---

### InitCluster — 集群初始化

```go
func InitCluster(addrs []string, username string, password string, opts ...Option) (*redis.ClusterClient, error)
```

- `addrs`：集群节点地址列表（至少一个即可）
- 内部遍历所有主节点执行 Ping 测试

---

### Close — 关闭单机/哨兵客户端

```go
func Close(rdb *redis.Client) error
```

- 幂等：重复调用安全
- nil 安全：传入 nil 不 panic

---

### CloseCluster — 关闭集群客户端

```go
func CloseCluster(clusterRdb *redis.ClusterClient) error
```

- 幂等：重复调用安全
- nil 安全：传入 nil 不 panic

---

## 错误处理

| 场景 | 行为 |
|------|------|
| `Init` DSN 为空或无效 | 返回解析错误 |
| `Init`/`InitSingle` 连接失败 | 返回 Ping 错误并自动关闭客户端 |
| `Close` 重复调用 | 返回 nil（幂等） |
| `Close(nil)` | 返回 nil |
| Lua 通道探测失败 | `log.Printf` 记录错误，不中断初始化 |

---

## 集成测试

集成测试需要真实 Redis 服务器，通过 `GOREDIS_TEST_DSN` 环境变量控制：

```bash
# 配置环境变量
export GOREDIS_TEST_DSN="redis://:password@host:port/db"

# 运行集成测试
cd pkg/goredis
go test -tags=integration -v -run TestIntegration -count=1
```

---

## 参考文档

- [go-redis v9 官方文档](https://redis.uptrace.dev/zh/guide/go-redis.html)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/instrumentation/go/)
- [redsync 分布式锁](https://github.com/go-redsync/redsync)
