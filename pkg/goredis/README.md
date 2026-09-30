# goredis

封装 [go-redis](https://github.com/redis/go-redis/v9) 库，提供单机/哨兵/集群三种连接模式、Lua 脚本通道探测、OpenTelemetry 追踪集成及 request_id 链路注入能力。

## 架构概览

```
goredis/
├── goredis.go          # 核心：Init/InitSingle/InitSentinel/InitCluster/Close/Shutdown、setupClient 统一初始化、Lua 通道探测
├── option.go           # Functional Options：WithPoolSize/WithTracing/WithMetrics/Without* 等
├── logging.go          # 包私有的全局 pkg/logger 日志钩子（仅供单测替换，不对外暴露）
├── requestid_hook.go   # Redis Hook：request_id 注入、逐命令 Pipeline 失败事件、eval/evalsha 分布式锁识别
├── goredis_test.go     # 单元测试：Init/Close/Shutdown 生命周期、nil Option、Lua 探测告警
├── option_test.go      # 单元测试：默认值、全部 With*/Without* 选项、三阶段合并语义
├── logging_test.go     # 单元测试：日志钩子捕获辅助、告警是否经由全局 logger 输出
├── requestid_hook_test.go  # 单元测试：Span 属性增强、request_id 注入、逐命令失败事件
├── integration_test.go # 集成测试（需 GOREDIS_TEST_DSN 环境变量）
├── dsn_test.go         # DSN 解析回归测试（query 参数、rediss:// TLS、特殊字符密码、merge 语义）
├── benchmark_test.go   # 性能基线：DSN 解析、Span 增强、Key 截断、命令端到端（见「性能基线与模糊测试」）
├── fuzz_test.go        # 模糊测试：DSN 解析不变量、路径补全幂等、Span 文本合法 UTF-8
├── CHANGELOG.md        # 版本变更日志（遵循 SemVer）
└── README.md
```

**核心设计原则：**

- **三阶段合并语义**：`DSN < WithXxxOptions 展开 < With* 显式设置`，所有字段通过 `xxxSet` 标志识别，零值（空串、false、nil、0）也可显式设置
- **Lua 脚本通道探测**：`Init` 时执行 `EVAL "return 1" nil` 确认 Lua 通道可用；redsync 的脚本加载由 go-redis 内置 NOSCRIPT fallback 保证，不依赖本探测
- **统一初始化路径**：`setupClient` 消除 8 个 Init 函数的重复代码（追踪挂载 → 指标挂载 → Hook 注册 → ping 策略测试 → Lua 探测）；单机/集群仅 ping 策略不同，挂载流程完全共享
- **日志统一走全局 `pkg/logger`**：库内不定义 `Logger` 接口、不提供 `WithLogger`，也不直接使用标准库 `log`/`slog`（见下方「为何取消日志依赖倒置」）
- **包内约定选型**：Option 采「`xxxSet` 显式标记」（与 `pkg/nacoscli` 的「最后赋值胜出」不同且均属合法选择）；
  测试命名已一次性收口为**方案 A**（全英文驼峰标识符 + 中文 doc + 中文子测试名，与全仓一致，见 skill
  [`package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md) 第二节）
- **配置错误快速失败**：`WithSingleOptions` 误传 `Addr/Password/DB` 等配置类错误在 `Init` 时返回 error，而非运行期刷屏
- **幂等关闭**：`Close`/`CloseCluster` 重复调用安全，第二次调用返回 nil；`Shutdown`/`ShutdownCluster` 等待飞行中命令归还后再关闭
- **request_id 自动注入**：`requestIDHook` 直接读取全项目约定的 `logger.ContextKeyRequestID`（gin/grpc middleware 写入的同一个 key），无需任何注入即可默认生效
- **evalsha 分布式锁识别**：自动识别 `lock:`、`/dlock/` 前缀，Span 名称显示锁标识便于排查；Pipeline 逐命令记录失败事件

### 为何取消日志依赖倒置

早期版本本包定义 `Logger` 接口 + `WithLogger`（默认实现走 `log/slog`）与 `RequestIDExtractor` + `WithRequestIDExtractor`，已全部删除：

- **mono-repo 前提**：`pkg/logger` 已是全仓日志底座（仓内 **140 个非测试 Go 文件**直接依赖，含 `pkg/es`、`pkg/goMq`、
  `pkg/kafka`、`pkg/cache`、`pkg/gin/middleware`、`internal/database` 等），包内再造一层接口并不能真正解耦，只多出一份 key-value 翻译逻辑
- **双出口风险**：默认实现走标准库 `slog`，日志不经过项目的 zap 管道，不带 ctx/request_id/trace_id，事实上形成生产日志盲区
- **死 API**：`WithLogger` / `WithRequestIDExtractor` 在生产代码中零调用点（仅单测使用），却抬高了 API 面积与认知成本
- **默认就对**：request_id 改读全项目约定的 context key 后，“忘注入 → Span 没 request_id”这类问题从源头消除

若将来确实需要切换底层日志实现，正确的收口点是 `pkg/logger` 自身，而不是每个基础设施包各造一套 `Logger` + `WithLogger`。

上述两项声明可自行复核（预期结果：前者 140，后者除 `pkg/gocron` 引用的 robfig 库自身 API 外无匹配）：

```bash
grep -rl "sunshine/pkg/logger" --include=*.go . | grep -v _test.go | wc -l
grep -rn "WithLogger\|WithRequestIDExtractor" --include=*.go .
```

可测试性由 `logging.go` 的包私有钩子 `logWarn` 提供；因为它是包级变量，`logging_test.go` 的 `captureWarn`
内置两项约束：收集器 `warnCollector` 带互斥锁，且用 `atomic.Bool` 拒绝同一测试内嵌套/重复捕获。

`pkg/nacoscli` 已按同一原则处理（可复核：[`pkg/nacoscli/logging.go`](../nacoscli/logging.go) 与
其 README「为何不依赖倒置日志」章节），两包的日志收口方式与测试侧收集器约束完全一致。

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
)
```

**内部行为**：`redisotel.InstrumentTracing` 挂载 Redis 追踪 Hook → `requestIDHook` 从 ctx 的 `logger.ContextKeyRequestID` 读取 request_id 并注入 `request_id` 属性（无需额外 Option）→ 命名规则：普通命令 `redis.<cmd>`，分布式锁 `redis.lock:<name>`

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

### 追踪与指标

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithTracing(tp)` | 启用 OpenTelemetry 追踪，参数为 `oteltrace.TracerProvider` **接口**（SDK 具体类型/自定义实现均可注入；传 `nil` 或 typed nil 不启用） | `nil`（不启用） |
| `WithMetrics()` | 启用 Redis 连接池/命令 OTel 指标（`redisotel.InstrumentMetrics`） | `false`（不启用） |
| `WithMeterProvider(mp)` | 指定指标上报的 `MeterProvider`（与 `WithTracing` 对称；需配合 `WithMetrics`，传 `nil` 回退全局） | `nil`（全局） |

> 日志与 request_id 不再提供 Option：日志固定走全局 `pkg/logger`，request_id 自动读 `logger.ContextKeyRequestID`。

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
func Shutdown(ctx context.Context, rdb *redis.Client) error
func ShutdownCluster(ctx context.Context, clusterRdb *redis.ClusterClient) error
```

- 轮询连接池状态（`PoolStats`），等待飞行中命令归还（`TotalConns == IdleConns`）后再关闭
- `ctx` 超时/取消时通过全局 `pkg/logger` 记录告警并强制关闭，不泄漏连接；若强制关闭也失败，返回 `errors.Join(等待错误, 关闭错误)` 双错误
- nil 安全：传入 nil 返回 nil

> 破坏性变更：早期签名为 `Shutdown(ctx, rdb, opts ...Option)`，可变参数仅用于接收 `WithLogger`，
> 日志改走全局 logger 后已无作用，因此删除。旧调用 `Shutdown(ctx, rdb, goredis.WithLogger(lg))` 升级后编译报错，
> 改为 `Shutdown(ctx, rdb)` 即可（告警会自动进入项目日志管道并带上 ctx 中的 request_id）。

---

## 错误处理

| 场景 | 行为 |
|------|------|
| `Init` DSN 为空或无效 | 返回解析错误，且错误消息**已脱敏**（密码段替换为 `***`，不回显明文；错误链保留供 `errors.Is/As` 判定） |
| `Init`/`InitSingle` 连接失败 | 返回 Ping 错误并自动关闭客户端 |
| `WithSingleOptions` 传入 `Addr/Password/DB` | `Init` 期快速失败，返回配置错误 |
| `WithSentinelOptions` 传入 `MasterName/SentinelAddrs`、`WithClusterOptions` 传入 `Addrs` | `Init` 期快速失败（这三个字段由 `InitSentinel`/`InitCluster` 参数传入，展开函数不读取，静默忽略是误导） |
| 越界/非法选项值（`WithPoolSize(-1)`、`WithProtocol(9)`、`WithNetwork("foo")`、`WithMaxRetries(-2)`、负 `MinIdleConns`/`MaxRedirects`） | `Init` 期快速失败，返回配置错误 |
| 传入 `nil` Option | 自动跳过，不 panic |
| `GET` 不存在的 key（`redis.Nil`） | 正常缓存 miss：错误仍透传调用方，但 **Span 状态保持 Unset、不记录 Error Event**（避免 miss 率打满 Redis Span 错误率） |
| `Close` 重复调用 | 返回 nil（幂等） |
| `Close(nil)` | 返回 nil |
| Lua 通道探测失败 | `logger.WarnWithCtx` 记录告警（全局 pkg/logger，带 ctx 自动关联 request_id），不中断初始化 |
| `Shutdown` 等待超时 | `logger.WarnWithCtx` 记录告警后强制关闭；若 Close 也失败，返回 `errors.Join` 合并后的双错误 |

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
| Unreleased | **删除 `Logger` 接口、`WithLogger`、`RequestIDExtractor`、`WithRequestIDExtractor`**：日志统一走全局 `pkg/logger`，request_id 自动读 `logger.ContextKeyRequestID` | 若有调用点，升级后**编译失败**；删除相应 Option 即可，request_id 注入默认生效（不再需要手工传提取函数） |
| Unreleased | `Shutdown`/`ShutdownCluster` 删除可变参数 `opts ...Option`（早期仅用于 `WithLogger`） | 旧调用 `Shutdown(ctx, rdb, goredis.WithLogger(lg))` 升级后**编译失败**，改为 `Shutdown(ctx, rdb)` |
| Unreleased | `WithSingleOptions` 传入 `Addr`/`Password`/`DB`：从「log 警告 + 静默忽略 + 正常初始化」改为「**Init 启动期返回 error，rdb 为 nil**」 | 此前复用含这三个字段的配置模板调用本函数的代码，升级后会**启动失败**（符合 SemVer MAJOR 语义）。请改为通过 `InitSingle`/`Init` 参数或 DSN 传入这三个字段 |
| Unreleased | `WithSentinelOptions` 误传 `MasterName`/`SentinelAddrs`、`WithClusterOptions` 误传 `Addrs`：从「静默忽略」改为「**Init 启动期返回 error**」；越界/非法选项值（负 `PoolSize`/`MinIdleConns`/`MaxRedirects`、`Protocol` 非 2/3、`Network` 非 tcp/unix、`MaxRetries < -1`）同理 | 此前依赖静默忽略或把非法值透传给 go-redis 的代码，升级后会**启动失败**。请改为通过 `InitSentinel`/`InitCluster` 参数传入连接地址，并修正越界值 |
| Unreleased | `WithTracing` 参数从 SDK 具体类型 `*sdk/trace.TracerProvider` 改为接口 `oteltrace.TracerProvider`；typed nil 自动归一化为「不启用」 | 现有传 SDK 具体类型的调用点**编译与行为均不变**（SDK 实现满足接口）；自定义 `TracerProvider` 实现从此可注入。极少数把 `WithTracing` 赋给旧函数签名变量的代码需同步改类型 |

详细变更记录见 [CHANGELOG.md](CHANGELOG.md)。

---

## 测试规范

测试函数命名统一为**方案 A**：`Test<被测方法><场景>`，**全英文驼峰标识符** + 中文 doc 注释 + 中文子测试名，全包同一策略：

```go
// ✅ 英文驼峰标识符 + 中文 doc + 中文子测试名
func TestGetRedisOptHostPortFormat(t *testing.T) {
	t.Run("自动补前缀与路径", func(t *testing.T) {})
}

// ✅ 标识符全英文；中文只出现在 doc 注释与子测试名/断言消息里
func TestEnhanceRedisSpanEvalshaDistributedLock(t *testing.T) {}

// ❌ 下划线 + 中文描述（已废止的方案 B，全包已重命名清零）
// TestGetRedisOpt_主机端口格式 —— 方案 B 形态，不再接受
```

约束：

- 结构体字段/变量名一律 ASCII（如 `name`/`apply`/`check`），中文仅用于 doc 注释、子测试名与断言消息
- 注释、日志、错误消息全部中文（见项目公约第十八章）
- 集成测试统一以 `TestIntegration_` 前缀命名
- **包内禁止混用两套风格**；新测试直接按方案 A 命名

> 跨包口径：全仓测试命名唯一权威是方案 A，定义见 skill
> [`package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md) 第二节。
> 本包存量方案 B 用例已一次性纯重命名收口（不改断言、不改输入输出），验收：
> `grep -rnP 'func (Test|Fuzz|Benchmark).*[\x{4e00}-\x{9fa5}]' pkg/goredis/` 无匹配。

---

## 性能基线与模糊测试

### 竞态检测

```bash
# 本包
CGO_ENABLED=1 go test -race -count=1 -short ./pkg/goredis/
# 全仓
CGO_ENABLED=1 go test -race -count=1 ./...
```

必须带 `CGO_ENABLED=1`（`-race` 依赖 cgo）。本包的并发相关面是
`requestIDHook`（多 goroutine 共用同一客户端）、`dlock` 分布式锁脚本路径与 `captureWarn` 全局钩子替换，
因此竞态检测必须一行命令可执行。

> 说明：`-race` 依赖 cgo 工具链。本机 Windows/MinGW 实测报 `exit status 0xc0000139`（gcc 运行时问题，
> 与本包代码无关），因此本机不采集 `-race` 读数。竞态取证已挂 CI：
> [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) 在 Linux 上执行
> `CGO_ENABLED=1 go test -race -count=1 -short ./pkg/goredis/`，同流水线还包含 `go vet`、
> Fuzz 种子、`golangci-lint` 与 `govulncheck` 门禁；**`-race` 结论以该 workflow 的运行结果为准**，
> 首次跑通后将在此回填具体结论（当前状态：已就位，尚未首跑）。
> 本机可验证的部分（`go test`/`vet`/Fuzz 种子/lint）均已本地跑绿。

### 基准测试（Benchmark）

```bash
# 全部基准（-run='^$' 用于跳过常规单测，只跑 Benchmark）
go test -run='^$' -bench=. -benchmem ./pkg/goredis/
# 需要更稳定的读数时（对比回归推荐：固定迭代次数，读数更可比）
go test -run='^$' -bench=. -benchmem -benchtime=1000x ./pkg/goredis/
# 只跑单个基准
go test -run='^$' -bench='BenchmarkEnhanceRedisSpan' -benchmem ./pkg/goredis/
```

下表为 Windows/amd64、i5-1135G7、`-benchtime=1000x` 的实测读数。**它的用途是「相对回归基线」——
判断某次改动是否让热路径变慢，不是生产绝对延迟**（生产延迟由网络与 Redis 决定）：

| 基准 | 场景 | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| `BenchmarkGetRedisOptDSNParse` | 仅地址 | 788.7 | 624 | 7 |
| `BenchmarkGetRedisOptDSNParse` | 带密码与库号 | 800.8 | 672 | 7 |
| `BenchmarkGetRedisOptDSNParse` | 完整 URL 带查询 | 1744 | 1056 | 11 |
| `BenchmarkEnsureDSNPathNormalize` | 需补全 | 53.5 | 24 | 1 |
| `BenchmarkEnsureDSNPathNormalize` | 已含路径（快路径） | 15.4 | 0 | 0 |
| `BenchmarkEnhanceRedisSpanTracingState` | **未开启追踪** | 18.2 | 0 | 0 |
| `BenchmarkEnhanceRedisSpanTracingState` | 开启追踪不导出 | 648.1 | 525 | 5 |
| `BenchmarkEnhanceRedisSpanTracingState` | 脚本命令走锁分支 | 696.6 | 580 | 6 |
| `BenchmarkSetRequestIDToRedisSpanRequestIDExtract` | ctx 带 request_id | 137.8 | 128 | 1 |
| `BenchmarkSetRequestIDToRedisSpanRequestIDExtract` | ctx 无 request_id | 5.8 | 0 | 0 |
| `BenchmarkTruncateKeyLengths` | 短 Key（快路径） | 33.5 | 0 | 0 |
| `BenchmarkTruncateKeyLengths` | 恰好上限 | 257.3 | 416 | 1 |
| `BenchmarkTruncateKeyLengths` | 超长截断 | 1703 | 2016 | 3 |
| `BenchmarkTruncateKeyLengths` | 中文超长截断 | 4040 | 2432 | 3 |
| `BenchmarkCommandEndToEndMiniredis` | 不含 request_id（SET+GET） | 122769 | 1285 | 39 |
| `BenchmarkCommandEndToEndMiniredis` | 含 request_id（SET+GET） | 108646 | 1286 | 39 |

**读数解读：**

- **未开启追踪 = 18.2ns / 0 分配**：没接 OTel 的部署里，本 Hook 的代价只有一次 `SpanFromContext` + `IsRecording`，
  这是「可观测性代码不该成为负担」的量化证据，也是最需要盯住不许劣化的一条
- **每条命令的 Span 增强 ≈ 0.65µs**，相对端到端 ~110µs 占比不足 1%；带 request_id 比不带的增量约 130ns（一次 `SetAttributes`）
- **`TruncateKey` 中文超长 4µs** 来自 `[]rune(s)` 全串物化。刻意**不做优化**：短 Key 已走零分配快路径（33.5ns），
  长 Key 成本相对端到端不足 4%，而这个函数刚被 Fuzz 守护过（见下），在此引入「提前计数再截断」的分支
  只会增加回归风险。若未来出现不可控的超长 Key（如把整个 JSON 当 key），再以此为改造依据
- `getRedisOpt` 在配置热更新重建客户端时执行，不在每条命令上，故 ~0.8µs 完全可接受

### 模糊测试（Fuzz）

```bash
# 种子语料随常规 go test 一起执行（无需额外命令，耗时可忽略）
go test -run='^Fuzz' ./pkg/goredis/
# 真正的挖掘：不进默认流程，按需本地或定时任务跑
go test -run='^$' -fuzz=FuzzSpanTruncateTextValidUTF8 -fuzztime=30s ./pkg/goredis/
```

各 target 守护的是「不变量」而非具体输出值——这正是 Fuzz 相对单测的增量价值：

| Target | 不变量 |
| --- | --- |
| `FuzzGetRedisOptArbitraryDSN` | 不 panic；**出错时必返回 nil Options**（调用方拿不到半成品去建连接）；成功时 `Addr` 非空；错误必带 `goredis:` 前缀 |
| `FuzzEnsureDSNPathIdempotent` | 归一化两次结果一致（配置热更新会反复归一化同一字符串）；含 `://` 时不丢协议头；只增不减 |
| `FuzzSpanTruncateTextValidUTF8` | 写入 Span 名称/属性的文本**必为合法 UTF-8** 且不超长度上限 |

**本包由 Fuzz 实际发现并修复的缺陷（值得留档）：**

`FuzzSpanTruncateTextValidUTF8` 的种子 `\xff\xfe invalid`、`a\x80b\x81c` 命中失败。根因是三处「只取截断分支」的疏漏：
`truncateRunes` 在「未超长」时原样 `return s`；`truncateKey` 未截断时 `return keyStr`；
`trimLockName`/`trimScriptSHA` 仅在 `truncated == true` 时采用返回值。于是 Redis key 里的非法 UTF-8 字节
会原样进入 `db.redis.key` 属性与 Span 名称——而 OTLP 的 protobuf string 字段要求合法 UTF-8，
这类 Span 会让后端拒绝整批上报或渲染成乱码。

修复方式（`requestid_hook.go`）：`truncateRunes` 在「未超长但非法」时用 `strings.ToValidUTF8` 归一化为 U+FFFD
（且不标记为截断，避免调用方误加省略号），三个调用方改为**始终采用其返回值**。
Redis key 常由业务方拼接外部输入而来，不能假设它一定合法 UTF-8——这条不变量此后由 Fuzz 长期守住。

> 失败语料会写入 `testdata/fuzz/<TargetName>/`，需人工确认后再提交或修正，不要直接删掉断言让它通过。

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
