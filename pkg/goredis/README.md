# goredis

封装 [go-redis](https://github.com/redis/go-redis/v9) 库，提供单机/哨兵/集群三种连接模式、Lua 脚本通道探测、OpenTelemetry 追踪集成及 request_id 链路注入能力。

## 架构概览

```
goredis/
├── goredis.go          # 核心：Init/InitSingle/InitSentinel/InitCluster/Close/Shutdown、setupClient 统一初始化、Lua 通道探测
├── option.go           # Functional Options：WithPoolSize/WithTracing/WithLogger/WithMetrics/Without* 等 + Logger 接口
├── requestid_hook.go   # Redis Hook：request_id 注入、逐命令 Pipeline 失败事件、eval/evalsha 分布式锁识别
├── goredis_test.go     # 单元测试：Init/Close/Shutdown 生命周期、nil Option、Logger 注入
├── option_test.go      # 单元测试：默认值、全部 With*/Without* 选项、三阶段合并语义
├── requestid_hook_test.go  # 单元测试：Span 属性增强、request_id 注入、逐命令失败事件
├── integration_test.go # 集成测试（需 GOREDIS_TEST_DSN 环境变量）
├── dsn_test.go         # DSN 解析回归测试（query 参数、rediss:// TLS、特殊字符密码、merge 语义）
├── CHANGELOG.md        # 版本变更日志（遵循 SemVer）
└── README.md
```

**核心设计原则：**

- **三阶段合并语义**：`DSN < WithXxxOptions 展开 < With* 显式设置`，所有字段通过 `xxxSet` 标志识别，零值（空串、false、nil、0）也可显式设置
- **Lua 脚本通道探测**：`Init` 时执行 `EVAL "return 1" nil` 确认 Lua 通道可用；redsync 的脚本加载由 go-redis 内置 NOSCRIPT fallback 保证，不依赖本探测
- **统一初始化路径**：`setupClient` 消除 8 个 Init 函数的重复代码（追踪挂载 → 指标挂载 → Hook 注册 → ping 策略测试 → Lua 探测）；单机/集群仅 ping 策略不同，挂载流程完全共享
- **日志依赖倒置**：库内不直接使用标准库 `log`，通过 `Logger` 接口（`WithLogger` 注入，默认 `log/slog`）接管告警
- **配置错误快速失败**：`WithSingleOptions` 误传 `Addr/Password/DB` 等配置类错误在 `Init` 时返回 error，而非运行期刷屏
- **幂等关闭**：`Close`/`CloseCluster` 重复调用安全，第二次调用返回 nil；`Shutdown`/`ShutdownCluster` 等待飞行中命令归还后再关闭
- **request_id 注入**：通过 `RequestIDExtractor` 函数类型解耦 goredis 与 logger 包，上层注入具体实现
- **evalsha 分布式锁识别**：自动识别 `lock:`、`/dlock/` 前缀，Span 名称显示锁标识便于排查；Pipeline 逐命令记录失败事件

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

1. `getRedisOpt` 解析 DSN（`ensureDSNPath` 补路径 + `redis.ParseURL`）→ 仅覆盖显式设置的 Option
2. 创建 `redis.NewClient` → 挂载追踪/指标/Hook → Ping 测试 → Lua 探测
3. DSN 无 `/db` 后缀时自动追加 `/0`（纯字符串拼接，不做 net/url round-trip，密码中的特殊字符不会被二次编码破坏）
4. 支持 `rediss://` 前缀，自动启用 TLS

**DSN 格式支持**：`host:port` | `:password@host:port/db` | `redis://user:password@host:port/db` | `rediss://...`（TLS）

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
| `WithTLSConfig(cfg)` | TLS 安全连接配置（传 `nil` 可显式清空 DSN 中的 TLS 配置） | `nil` |

### 初始化超时

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithInitTimeout(d)` | 初始化阶段连接测试（Ping）的超时时间（非正值保持默认） | `15s` |
| `WithProbeTimeout(d)` | Lua 脚本通道探测的超时时间（非正值保持默认） | `3s` |

### 重试与网络

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithMaxRetries(n)` | 最大重试次数（0=禁用，-1=默认） | `go-redis 默认值` |
| `WithMinRetryBackoff(d)` | 最小重试退避时间 | `go-redis 默认值` |
| `WithMaxRetryBackoff(d)` | 最大重试退避时间 | `go-redis 默认值` |
| `WithNetwork(n)` | 网络类型（tcp / unix） | `tcp` |
| `WithUsername(u)` | Redis ACL 用户名，对单机/哨兵/集群三种模式均生效（哨兵/集群的 `Init` 参数作为兜底，本选项优先） | `""` |
| `WithClientName(name)` | 客户端名称（CLIENT SETNAME） | `""` |
| `WithProtocol(v)` | RESP 协议版本（2 或 3） | `go-redis 默认值` |
| `WithOnConnect(fn)` | 连接建立时的回调函数 | `nil` |
| `WithDialer(fn)` | 自定义网络连接创建函数 | `nil` |

### 哨兵专用

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithSentinelUsername(u)` | 哨兵 ACL 用户名（传 `""` 可显式清空） | `""` |
| `WithSentinelPassword(p)` | 哨兵密码（传 `""` 可显式清空） | `""` |
| `WithUseDisconnectedReplicas()` | 启用哨兵断连副本路由 | `false` |
| `WithoutUseDisconnectedReplicas()` | 显式关闭哨兵断连副本路由（覆盖展开值） | `false` |

### 集群专用

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithReadOnly()` | 启用集群只读命令路由到副本节点 | `false` |
| `WithoutReadOnly()` | 显式关闭只读路由（覆盖展开值） | `false` |
| `WithRouteByLatency()` | 启用集群延迟路由 | `false` |
| `WithoutRouteByLatency()` | 显式关闭延迟路由（覆盖展开值） | `false` |
| `WithRouteRandomly()` | 启用集群随机路由 | `false` |
| `WithoutRouteRandomly()` | 显式关闭随机路由（覆盖展开值） | `false` |
| `WithMaxRedirects(n)` | 集群最大重定向次数（传 `0` 可显式设为 0） | `go-redis 默认值` |

### 追踪、指标与日志

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithTracing(tp)` | 启用 OpenTelemetry 追踪 | `nil`（不启用） |
| `WithRequestIDExtractor(fn)` | 自定义 request_id 提取函数 | `nil` |
| `WithMetrics()` | 启用 Redis 连接池/命令 OTel 指标（`redisotel.InstrumentMetrics`） | `false`（不启用） |
| `WithMeterProvider(mp)` | 指定指标上报的 `MeterProvider`（与 `WithTracing` 对称；需配合 `WithMetrics`，传 `nil` 回退全局） | `nil`（全局） |
| `WithLogger(lg)` | 注入日志实现（`Logger` 接口，含 `Warn`/`Error`） | `log/slog` 默认实现 |

### Options 结构体展开

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithSingleOptions(opt)` | 展开 `redis.Options` 非零字段到 options（With* 显式设置优先） | `nil` |
| `WithSentinelOptions(opt)` | 展开 `redis.FailoverOptions` 非零字段到 options | `nil` |
| `WithClusterOptions(opt)` | 展开 `redis.ClusterOptions` 非零字段到 options | `nil` |

**WithSingleOptions 支持的字段**：`PoolSize`、`MinIdleConns`、`PoolTimeout`、`ConnMaxLifetime`、`ConnMaxIdleTime`、`DialTimeout`、`ReadTimeout`、`WriteTimeout`、`TLSConfig`、`MaxRetries`、`MinRetryBackoff`、`MaxRetryBackoff`、`Network`、`Username`、`ClientName`、`Protocol`、`OnConnect`、`Dialer`。

> 注意：
> - `Addr`/`Password`/`DB` 请通过 `InitSingle`/`Init` 参数或 DSN 传入，`WithSingleOptions` 读取这三个字段会**启动期快速失败**（`Init` 直接返回 error）。
> - `MaxRetries=0` 通过 `WithSingleOptions` 传入时视为"未设置"，不会禁用重试。如需显式禁用重试，请使用 `WithMaxRetries(0)`。
> - `MinRetryBackoff`/`MaxRetryBackoff=0` 同理，如需显式设置请使用对应的 `With*` 函数。
> - 所有展开字段均通过 `xxxSet` 标志识别，可被后续 `With*`/`Without*` 显式覆盖（包括零值）。

---

## API 速查

### Init — DSN 初始化

```go
func Init(dsn string, opts ...Option) (*redis.Client, error)
func InitWithContext(ctx context.Context, dsn string, opts ...Option) (*redis.Client, error)
```

- 支持 `host:port`、`:password@host:port/db`、`redis://user:pass@host:port/db` 三种 DSN 格式，`rediss://` 启用 TLS
- 无 `/db` 时自动追加 `/0`
- 返回 error：连接失败、DSN 解析失败、配置类错误（如 `WithSingleOptions` 传入 `Addr`）
- `InitWithContext`：可被外部取消/超时控制初始化流程；`ctx` 为 nil 时降级为 `context.Background()`

---

### InitSingle — 单机初始化

```go
func InitSingle(addr string, password string, db int, opts ...Option) (*redis.Client, error)
func InitSingleWithContext(ctx context.Context, addr string, password string, db int, opts ...Option) (*redis.Client, error)
```

- 直接传入 `addr`/`password`/`db`，不经过 DSN 解析
- 适用于 Redis 5.0 及以下版本

---

### InitSentinel — 哨兵初始化

```go
func InitSentinel(masterName string, addrs []string, username string, password string, opts ...Option) (*redis.Client, error)
func InitSentinelWithContext(ctx context.Context, masterName string, addrs []string, username string, password string, opts ...Option) (*redis.Client, error)
```

- `masterName`：Sentinel 监控的主节点名称
- `addrs`：Sentinel 节点地址列表

---

### InitCluster — 集群初始化

```go
func InitCluster(addrs []string, username string, password string, opts ...Option) (*redis.ClusterClient, error)
func InitClusterWithContext(ctx context.Context, addrs []string, username string, password string, opts ...Option) (*redis.ClusterClient, error)
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

### Shutdown — 优雅关闭单机/哨兵客户端

```go
func Shutdown(ctx context.Context, rdb *redis.Client, opts ...Option) error
func ShutdownCluster(ctx context.Context, clusterRdb *redis.ClusterClient, opts ...Option) error
```

- 轮询连接池状态（`PoolStats`），等待飞行中命令归还（`TotalConns == IdleConns`）后再关闭
- `ctx` 超时/取消时记录告警并强制关闭，不泄漏连接；若强制关闭也失败，返回 `errors.Join(等待错误, 关闭错误)` 双错误
- 可选 `opts`：如 `Shutdown(ctx, rdb, goredis.WithLogger(lg))` 指定关闭告警的日志实现（默认 slog）
- nil 安全：传入 nil 返回 nil

---

## 错误处理

| 场景 | 行为 |
|------|------|
| `Init` DSN 为空或无效 | 返回解析错误 |
| `Init`/`InitSingle` 连接失败 | 返回 Ping 错误并自动关闭客户端 |
| `WithSingleOptions` 传入 `Addr/Password/DB` | `Init` 期快速失败，返回配置错误 |
| 传入 `nil` Option | 自动跳过，不 panic |
| `Close` 重复调用 | 返回 nil（幂等） |
| `Close(nil)` | 返回 nil |
| Lua 通道探测失败 | `Logger.Warn` 记录告警（默认 slog，可 `WithLogger` 注入），不中断初始化 |
| `Shutdown` 等待超时 | `Logger.Warn` 记录告警后强制关闭；若 Close 也失败，返回 `errors.Join` 合并后的双错误 |

---

## 版本兼容

本包遵循 [语义化版本](https://semver.org/lang/zh-CN/)（SemVer）：

- **MAJOR**：不兼容的 API 变更（导出函数签名删除/语义变更）
- **MINOR**：向下兼容的功能新增（新 Option、新函数）
- **PATCH**：向下兼容的问题修复

### 已知破坏性变更（升级必读）

| 版本 | 变更 | 升级影响 |
|------|------|----------|
| Unreleased | **删除导出函数 `WithEnableTrace()`**（及其 `enableTrace` 内部字段）：该函数从未产生实际效果（挂载逻辑只读 `tracerProvider`） | 若有调用点，升级后**编译失败**（导出符号不存在）；删除 `goredis.WithEnableTrace()` 并改用 `WithTracing(tp)`，未传 `tp` 的调用本就未启用追踪 |
| Unreleased | `WithSingleOptions` 传入 `Addr`/`Password`/`DB`：从「log 警告 + 静默忽略 + 正常初始化」改为「**Init 启动期返回 error，rdb 为 nil**」 | 此前复用含这三个字段的配置模板调用本函数的代码，升级后会**启动失败**（符合 SemVer MAJOR 语义）。请改为通过 `InitSingle`/`Init` 参数或 DSN 传入这三个字段 |

详细变更记录见 [CHANGELOG.md](CHANGELOG.md)。

---

## 测试规范

测试函数命名统一为**「被测标识符（ASCII）+ `_` + 中文描述」**，全包采用同一策略，禁止整体风格（全中文 vs 全英文）两套并存。描述段以中文为主，**允许保留技术专有名词的 ASCII 原文**（翻译反而失真）：

```go
// ✅ 被测函数名保留 ASCII，描述段用中文
func TestGetRedisOpt_主机端口格式(t *testing.T) {}
func TestWithPoolSize_设置与置位(t *testing.T) {}

// ✅ 描述段中的 Redis 命令名/字段名/库名作为专有名词保留 ASCII
func TestEnhanceRedisSpan_SET命令(t *testing.T) {}
func TestEnhanceRedisSpan_EVALSHA_分布式锁(t *testing.T) {}
func TestTruncateKey_中文截断不乱码(t *testing.T) {}

// ❌ 整段英文描述与 ❌ 纯中文标识符均不接受
func TestGetRedisOpt_HostPort(t *testing.T) {}
func TestGetRedisOpt_主机端口格式1(t *testing.T) {} // 禁止非描述性后缀
```

允许保留 ASCII 的专有名词范围：Redis 命令名（`SET`/`GET`/`EVAL`/`EVALSHA`）、协议/字段名（`Key`/`SHA`/`DSN`/`URL`/`PoolSize`/`NilOption`）、库与类型名（`Logger`/`MeterProvider`/`dlock`/`miniredis`）。

约束：

- 结构体字段/变量名一律 ASCII（如 `name`/`apply`/`check`），中文仅用于 case 描述字符串与断言消息
- 注释、日志、错误消息全部中文（见项目公约第十八章）
- 集成测试统一以 `TestIntegration_` 前缀 + 中文描述命名

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
