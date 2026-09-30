# nacoscli

Nacos 配置中心客户端封装，提供配置拉取与实时监听能力，并为服务注册发现提供统一的 SDK 命名客户端工厂。

## 架构概览

```
nacoscli/
├── nacoscli.go         # 核心：Params 校验、Client 生命周期、GetConfig/NewConfigClient
├── naming.go           # NewNamingClient：SDK 命名客户端工厂（仅负责创建，注册发现业务归 pkg/servicerd）
├── listener.go         # ListenClient：长轮询注册、<-ctx.Done() 阻塞、CancelListenConfig 清理、panic 保护
├── watch.go            # WatchConfig：注册失败重试、重试计数持续累积、提前校验、goroutine 等待退出
├── option.go           # Option 函数：defaultOptions + apply 模式、优先级传播
├── logging.go          # 包私有的全局 pkg/logger 日志钩子（仅供单测替换，不对外暴露）
├── *_test.go           # 单元测试（option/nacoscli/listener/watch/naming/logging）
├── benchmark_test.go   # 性能基线：GetConfig / valid / apply / buildConfigs / requestIDAttr
├── fuzz_test.go        # 模糊测试：Params 校验、GetConfig、回调透传、Option 零值防护
├── integration_test.go # 集成测试（-tags=integration，需环境变量，见「集成测试」章节）
├── CHANGELOG.md        # 版本变更日志（遵循 SemVer，含已删除注入点的迁移说明）
└── README.md
```

**核心设计原则：**

- **Option 优先级**：`WithClientConfig`/`WithServerConfigs`（完整配置） > 单字段 `With*` 选项 > 默认值
- **Option 赋值语义**：「最后赋值胜出」，不区分「设置」与「未设置」（与 goredis 的 `xxxSet` 显式标记不同），详见「[Option 列表](#option-列表)」
- **包内约定选型**：Option 采「最后赋值胜出」（与 `pkg/goredis` 的 `xxxSet` 显式标记不同，两者均属合法选择）；
  测试命名则遵循**全仓统一规范**（方案 A），依据见 skill
  [`package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md)
- **Params 与 Option 的关系**：`Params` 定义"查什么"（Group/DataID/Format），`Option` 定义"怎么连"（地址/认证/超时），两者互补
- **OpenTelemetry 内置**：`Client.GetConfig` 自动创建 Span，记录 `nacos.data_id`、`nacos.group`、`request_id` 等属性
- **日志统一走全局 `pkg/logger`**：不定义 `Logger` 接口、不提供 `WithLogger`，日志调用点直调
  `logger.Debug/Info/Warn/ErrorWithCtx(ctx, msg, logger.Field...)`（见下方「为何不依赖倒置日志」）；
  request_id 直接读全项目约定的 `logger.ContextKeyRequestID`，也不提供注入点
- **panic 安全**：监听回调内置 recover，单次 panic 不会导致监听协程退出；`apply` 对 nil Option 防御
- **零值防护**：`WithCreateDelay`/`WithTimeoutMs`/`WithGetTimeout` 拒绝 0（0 分别导致忙循环/SDK 超时未定义/立即超时）
- **valid() 无副作用**：返回归一化后的 Format，不修改原始 Params 结构体

### 为何不依赖倒置日志

早期版本本包定义 `Logger` 接口（`WithLogger` 注入）与 `RequestIDExtractor`（`WithRequestIDExtractor` 注入），已全部删除：

- **mono-repo 前提**：`pkg/logger` 已是全仓日志底座——仓内 **140 个非测试 Go 文件**直接依赖它，
  包括 `pkg/es`、`pkg/goMq`、`pkg/kafka`、`pkg/cache`、`pkg/etcdcli`、`pkg/gin/middleware`、
  `internal/database`、`internal/server` 等基础设施与业务层，包内再造一层接口无法真正解耦，
  只多出一份 `key,value` 到 `logger.Field` 的翻译逻辑，并在翻译中丢失字段类型
- **死 API**：两个注入点在全仓生产代码中零调用点（仅本包单测使用）
- **默认就对**：request_id 固定读 `logger.ContextKeyRequestID` 后，“忘注入 → Span 没 request_id”从源头消除

上述两项声明可自行复核（预期结果：前者 140，后者除 `pkg/gocron` 引用的 robfig 库自身 API 外无匹配）：

```bash
# 直接依赖 pkg/logger 的非测试文件数
grep -rl "sunshine/pkg/logger" --include=*.go . | grep -v _test.go | wc -l
# 已删除的注入点是否仍有调用点
grep -rn "WithLogger\|WithRequestIDExtractor" --include=*.go .
```

日志输出的可测试性由 `logging.go` 的包私有钩子变量（`logDebug`/`logInfo`/`logWarn`/`logError`）提供，
仅供单测替换，不对外暴露注入能力（`logging_test.go` 的 `captureLogs` 为唯一使用者）。
因为钩子是包级变量，`captureLogs` 内置两项约束：收集器 `logCollector` 带互斥锁（写入方可能是
Nacos SDK 后台回调 goroutine），且用 `atomic.Bool` 拒绝同一测试内嵌套/重复捕获。

`pkg/goredis` 已按同一原则处理（可复核：[`pkg/goredis/logging.go`](../goredis/logging.go) 与
[`pkg/goredis/CHANGELOG.md`](../goredis/CHANGELOG.md) 的「移除」章节均已列出删除的 `Logger`/`WithLogger`/
`RequestIDExtractor`/`WithRequestIDExtractor`），两包的测试侧收集器同样采用「带锁 + 拒绝嵌套」。

---

## 职责边界

本包**只负责「读取配置」与「监听配置变更」**，刻意不封装其余 Nacos 能力：

| 能力 | 是否在本包 | 应使用的入口 |
|------|-----------|-------------|
| 读取配置（`GetConfig`） | 是 | 本包 |
| 监听配置变更（`WatchConfig` / `NewListenClient`） | 是 | 本包 |
| 创建命名客户端（`NewNamingClient`） | 是（仅工厂） | 本包 |
| 服务注册 / 注销 / 发现 / watcher | **否** | `pkg/servicerd` |
| **配置发布 / 删除 / 搜索**（`PublishConfig` / `DeleteConfig` / `SearchConfig`） | **否** | 直接使用 `nacos-sdk-go` 的 `IConfigClient`，或在上层按需封装 |

配置的发布/删除不在本包范围的原因：它们是**写操作**，涉及幂等性、并发写冲突、审计与权限，
属于业务/运维平台职责而非基础设施读取层职责；一旦在本包封装，会诱导「读路径与写路径共用一个客户端」的
误用（读客户端参数不含写所需的 `Content`/`Md5`/权限语义）。可复核：

```bash
# PublishConfig/DeleteConfig/SearchConfig 在仓内只出现在测试与 SDK 直连，本包生产代码零封装
grep -rn "PublishConfig\|DeleteConfig\|SearchConfig" pkg/nacoscli/ --include=*.go
```

> `integration_test.go` 需要发布配置来准备测试数据时，直接用 SDK 的 `configClient.PublishConfig(...)`，
> 并在注释中显式标明「nacoscli 包未封装 PublishConfig」，而不是为测试便利在本包加一个只有测试调用的方法。

---

## 使用场景选择

### 场景一：一次性拉取配置（GetConfig 便捷函数）

**适用场景**：应用启动时从 Nacos 读取配置，读完即关闭连接。无需监听变更。

```go
format, data, err := nacoscli.GetConfig(&nacoscli.Params{
    IPAddr:      "192.168.3.37",
    Port:        8848,
    NamespaceID: "de7b176e-91cd-49a3-ac83-beb725979775",
    Group:       "dev",
    DataID:      "user-srv.yml",
    Format:      "yaml",
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("format: %s, data length: %d\n", format, len(data))
```

**内部行为**：创建客户端 -> 拉取配置 -> 关闭客户端，默认 30 秒超时（可通过 `WithGetTimeout` 调整）。

**不适用**：需要持续监听配置变更的场景（应使用 `WatchConfig`）。

---

### 场景二：监听配置变更（WatchConfig）

**适用场景**：应用运行期间需要实时感知 Nacos 配置变更（如动态调整日志级别、限流规则等）。

```go
params := &nacoscli.Params{
    NamespaceID: "de7b176e-91cd-49a3-ac83-beb725979775",
    Group:       "dev",
    DataID:      "user-srv.yml",
    Format:      "yaml",
}

handler := func(namespace, group, dataID, data string) {
    // data 是最新的完整配置内容，需自行解析
    var newCfg Config
    if err := yaml.Unmarshal([]byte(data), &newCfg); err != nil {
        log.Printf("解析配置失败: %v", err)
        return
    }
    applyConfig(&newCfg)
}

// 返回 stop 函数和错误，stop() 会等待 goroutine 完全退出
stop, err := nacoscli.WatchConfig(context.Background(), params, handler,
    nacoscli.WithIPAddr("192.168.3.37"),
    nacoscli.WithPort(8848),
    nacoscli.WithAuth("admin", "password"),
)
if err != nil {
    log.Fatal(err)
}
defer stop()
```

**内部行为**：

1. 提前校验 params 和 handler，非法参数立即返回 error（不进入后台循环）
2. 创建 `ListenClient` 并注册长轮询（Nacos SDK 的 `ListenConfig`，非阻塞）
3. `Start()` 返回 nil 表示 ctx 正常取消，返回 error 表示 ListenConfig 注册失败
4. 配置变更时调用 `handler`，内置 recover 保护
5. 注册失败时自动重试，retries 持续累积直到达到 maxRetries
6. 注册成功后，连接维护由 SDK 内部长轮询负责，WatchConfig 不再介入
7. 调用 `stop()` 时，cancel ctx -> CancelListenConfig -> CloseClient -> WaitGroup 等待退出

**重试语义**：WatchConfig 只在注册动作失败时重试，注册成功后连接维护完全由 Nacos SDK 内部长轮询负责。
完整的重试时机、计数规则与生产建议见下方独立章节「[重试语义](#重试语义)」。

**监听生效的三个前提（均在真实 Nacos 实测过，任一条不满足则「注册成功但永远收不到回调」）**：

1. **gRPC 端口可达且与 SDK 的推导值一致**。本包 `buildConfigs` 不设置 `ServerConfig.GrpcPort`，SDK 会按
   「HTTP 端口 + 1000」推导；K8s NodePort 部署下 gRPC 往往是**独立映射端口**（实测 HTTP 32564 / gRPC 32566，
   推导值 33564 不存在），此时必须走 `WithServerConfigs` 显式指定 `GrpcPort`（见场景五），
   否则报 `client not connected, current status:STARTING`。
2. **服务端开启鉴权时必须传 `WithAuth`**。实测匿名读配置返回 `Code: 401, Message: User not found!`，
   匿名服务注册则返回 `false`。
3. **注册后需要预热窗口**：`ListenConfig` 是非阻塞注册，且 SDK 未暴露「已生效」信号。实测送达延迟如下
   （公网实例，`go test -tags=integration`，多轮读数）：

   | 场景 | 送达延迟 |
   |------|----------|
   | 建立监听后的**第一次**变更 | 2.264s / 2.267s / 2.27s / 2.283s（4 轮） |
   | 随后的变更 | 210ms / 233ms / 269ms / 275ms |
   | **空闲 15s 后**的变更 | 220ms / 221ms（证明长连接不会因空闲失效，监听也不是一次性消耗） |

   验证入口：`TestIntegration_WatchConfigExternalChange`——它的修改方走 **Nacos Open API**
   （即控制台点「发布」、运维脚本、CI 同一个入口），监听方全程只读不改，
   因此它证明的是真实运维场景：**别人改了配置，本进程能感知**。

---

### 场景三：复用配置客户端多次获取配置（NewConfigClient）

**适用场景**：同一服务需要频繁读取多个 Nacos 配置文件，复用底层连接避免反复创建/销毁。

```go
// 创建可复用的配置客户端
client, err := nacoscli.NewConfigClient(
    nacoscli.WithIPAddr("192.168.3.37"),
    nacoscli.WithPort(8848),
    nacoscli.WithNamespaceID("de7b176e-91cd-49a3-ac83-beb725979775"),
    nacoscli.WithAuth("admin", "password"),
    nacoscli.WithTimeoutMs(3000),
)
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// 读取多个配置（复用同一连接）
params1 := &nacoscli.Params{Group: "dev", DataID: "app.yml", Format: "yaml"}
params2 := &nacoscli.Params{Group: "dev", DataID: "db.yml", Format: "yaml"}

format1, data1, err := client.GetConfig(context.Background(), params1)
format2, data2, err := client.GetConfig(context.Background(), params2)
_ = format1
_ = format2
_ = data1
_ = data2
```

**注意**：
- `NewConfigClient` 返回 `*Client`（配置客户端），提供 `GetConfig(ctx, params)` 和 `Close()` 方法
- 便捷函数 `GetConfig` 每次调用都会创建/销毁客户端，高频场景应使用 `NewConfigClient`
- `client.GetConfig` 支持通过 `context.Context` 精确控制超时与取消

---

### 场景四：为服务注册发现准备命名客户端（NewNamingClient）

**适用场景**：需要把服务注册到 Nacos 或从 Nacos 发现服务实例。**本包只负责创建 SDK 命名客户端**，
注册/注销/发现的业务语义（防误删校验、指数退避重注册、watcher、追踪埋点）由 `pkg/servicerd` 实现。

```go
// 1. 用本包工厂按统一连接参数创建 SDK 命名客户端
namingClient, err := nacoscli.NewNamingClient(
    "192.168.3.37",
    8848,
    "de7b176e-91cd-49a3-ac83-beb725979775",
    nacoscli.WithAuth("admin", "password"),
)
if err != nil {
    log.Fatal(err)
}
defer namingClient.CloseClient()

// 2. 注入 servicerd 的服务注册表，由它完成注册/发现/重注册与追踪
iRegistry := nacos.New(namingClient, cfg.NacosInfo.NacosRegistry.BuildRegistryOptions()...)
```

**职责边界**：
- `NewNamingClient` 返回 `naming_client.INamingClient`（**工厂定位**）：它与配置客户端共用同一套
  地址/命名空间/认证/超时参数构建，避免每个调用点重复拼装 SDK 配置
- **本包不封装注册发现业务**：曾提供的 `RegisterInstance`/`SelectInstances`/`Subscribe` 等包装方法已移除，
  因为它们与 `pkg/servicerd` 的 `Registry`/`Discovery` 职责重复，且生产调用点只用到「创建客户端」这一件事
- 服务注册发现的埋点（Span 名称 `nacos.register` 等、`nacos.service_name` 属性）在 `servicerd/registry/nacos` 中实现
- 生命周期由调用方负责，用完调用 SDK 的 `CloseClient()`

---

### 场景五：完整 SDK 配置（高级）

**适用场景**：需要精细控制 Nacos SDK 行为（如自定义 LogDir/CacheDir、设置 GrpcPort 等）。

```go
clientConfig := &constant.ClientConfig{
    NamespaceId:         "de7b176e-91cd-49a3-ac83-beb725979775",
    TimeoutMs:           5000,
    NotLoadCacheAtStart: true,
    LogDir:              "/var/log/nacos",
    CacheDir:            "/var/cache/nacos",
    Username:            "admin",
    Password:            "password",
}

serverConfigs := []constant.ServerConfig{
    {
        IpAddr:   "192.168.3.37",
        Port:     8848,
        GrpcPort: 9848,  // gRPC 端口，通常 = HTTP 端口 + 1000
        Scheme:   "http",
    },
}

format, data, err := nacoscli.GetConfig(params,
    nacoscli.WithClientConfig(clientConfig),
    nacoscli.WithServerConfigs(serverConfigs),
)
```

**覆盖规则**：设置 `WithClientConfig` 后，`WithNamespaceID`、`WithTimeoutMs`、`WithAuth` 等单字段选项被忽略。设置 `WithServerConfigs` 后，`WithIPAddr`、`WithPort`、`WithScheme`、`WithContextPath` 被忽略。

---

## 重试语义

`nacoscli.go` 包注释中提到的「重试语义」即本章节，它是理解 WatchConfig API 设计的关键。

**重试时机**：WatchConfig 只在两个时机重试：
- `NewListenClient` 创建失败（Nacos 地址不可达、认证失败等）
- `ListenConfig` 注册失败（SDK 内部状态异常、缓存目录不可写等）

一旦注册成功，连接的健康维护由 Nacos SDK 内部长轮询负责，WatchConfig 不再介入。
因此本包不存在「断线重连」相关配置，retries 也只在注册失败时递增（不存在运行期断连事件）。

**重试行为配置**：

```go
stop, err := nacoscli.WatchConfig(ctx, params, handler,
    nacoscli.WithIPAddr("192.168.3.37"),
    nacoscli.WithPort(8848),
    // 连续失败累计 10 次后放弃（默认 0 = 无限重试）
    nacoscli.WithMaxRetries(10),
    // 注册失败后等待 5 秒再重试（默认 5s，必须为正数）
    nacoscli.WithCreateDelay(5*time.Second),
)
```

**重试计数规则**：

- `retries` 仅在 `NewListenClient` 创建失败或 `ListenConfig` 注册失败时递增
- `WithMaxRetries(n)` 的语义是「总失败次数上限」：第 n 次失败时记录 Error 日志并退出 goroutine，
  即 `WithMaxRetries(3)` 共尝试 3 次（而非“3 次重试 = 4 次尝试”）
- 由于 `Start` 一旦成功返回（ctx 取消）就退出，不存在“运行一段时间后重试计数重置”的场景
- `maxRetries = 0`（默认）时无限重试；负值被忽略，保留先前设置

> **生产环境提示**：默认无限重试 + 固定 5 秒延迟意味着 Nacos 长期不可达时会持续刷 Warn 日志。建议在生产环境显式设置 `WithMaxRetries`（如 10~20 次），避免日志膨胀。

---

## Params 结构体

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `IPAddr` | `string` | 是* | Nacos 服务器地址 |
| `Port` | `int` | 是* | Nacos 服务器端口 |
| `Scheme` | `string` | 否 | 协议，合法取值 `http`、`https`、`grpc`；默认空（表示未设置，由 SDK 按默认 `http` 处理） |
| `ContextPath` | `string` | 否 | 上下文路径 |
| `NamespaceID` | `string` | 否 | 命名空间 ID |
| `Group` | `string` | **是** | 配置分组，如 `dev`、`prod` |
| `DataID` | `string` | **是** | 配置文件 ID，如 `user-srv.yml` |
| `Format` | `string` | **是** | 配置类型：`json`、`yaml`（`yml` 会自动归一化）、`toml` |

> *`IPAddr`/`Port` 可通过 `WithIPAddr`/`WithPort` 或 `WithServerConfigs` 提供；若未提供 `WithServerConfigs`，二者均为必填（`Port == 0` 会报错）。

**参数校验规则**：`Group`、`DataID`、`Format` 为空时返回错误；`Format` 不在支持列表时返回错误。`valid()` 返回归一化后的 Format，**不修改**原始 Params 结构体。

> `Scheme` 不经过 `valid()` 校验，而是在 `WithScheme` 内做白名单过滤：**白名单外的取值（如 `h2`、`HTTP` 之外的乱写）被静默忽略**，
> 最终回落到 SDK 默认 `http`。选择「忽略而非报错」是因为它经 `WithScheme(params.Scheme)` 进入选项链，
> 与 `WithTimeoutMs` 等既有防御风格保持一致；代价是写错的协议名不会立即报错（详见「Option 列表」的说明与理由）。

---

## Option 列表

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithIPAddr(ip)` | Nacos 服务器地址 | `""` | 全部 |
| `WithPort(port)` | Nacos 服务器端口，负值被忽略，0 在未提供 `WithServerConfigs` 时报错 | `0` | 全部 |
| `WithScheme(scheme)` | 协议，白名单 `http`/`https`/`grpc`；大小写与首尾空格不敏感（`" HTTPS "` 归一化为 `https`），空串与白名单外取值被忽略 | `""`（SDK 默认 http） | 全部 |
| `WithContextPath(path)` | 上下文路径 | `""` | 全部 |
| `WithNamespaceID(id)` | 命名空间 ID | `""` | 全部 |
| `WithTimeoutMs(ms)` | SDK 请求超时（毫秒），必须为正数，0/负值被忽略 | `5000` | 全部 |
| `WithGetTimeout(d)` | GetConfig 便捷函数整体超时，必须为正数，0/负值被忽略 | `30s` | `GetConfig` |
| `WithAuth(user, pass)` | 认证用户名/密码 | `""` | 全部 |
| `WithClientConfig(cfg)` | 完整 SDK ClientConfig | `nil` | 全部 |
| `WithServerConfigs(cfgs)` | 完整 SDK ServerConfig 列表 | `nil` | 全部 |
| `WithMaxRetries(n)` | 总失败次数上限（第 n 次失败后放弃），0=无限，负值被忽略 | `0` | `WatchConfig` |
| `WithCreateDelay(d)` | 创建或注册失败重试等待时间，必须为正数（0 会导致忙循环），0/负值被忽略 | `5s` | `WatchConfig` |

**赋值语义（最后赋值胜出）**：选项按传入顺序逐个应用到同一个 `options` 结构体，因此
**后传入的 `With*` 会覆盖先传入的同字段值**，包括零值与空字串：

- `WithIPAddr("")` 会清空已设置的地址；`WithPort(0)` 会写入 0（到校验阶段才报错）
- 有值域防护的选项除外：`WithTimeoutMs`/`WithGetTimeout`/`WithMaxRetries`/`WithCreateDelay` 拒绝 `<= 0`，`WithPort` 拒绝负值，
  `WithScheme` 只接受白名单 `http`/`https`/`grpc`（空串同样被忽略）
- **`WithScheme("")` 不能用来清空协议**：因为 `Params.Scheme` 为空是常态（仓内配置大多不填该字段），
  若按「最后赋值胜出」写入空串，会把调用方显式设置的协议抹掉；此处语义是「未设置」而非「清空」。
  确需回落 SDK 默认时，通过 `WithClientConfig`/`WithServerConfigs` 显式提供完整配置
- 与 goredis 的差异：goredis 用 `xxxSet` 标记区分「显式设置」与「未设置」，因此 `WithDB(0)`、`WithPassword("")` 能语义正确地覆盖默认值；
  nacoscli 不引入标记位，因为连接参数（地址/端口/命名空间/超时）本身不存在合法的零值场景，且默认值恰好是 Go 零值，无需区分

---

## API 速查

### GetConfig 便捷函数 — 一次性拉取

```go
func GetConfig(params *Params, opts ...Option) (string, []byte, error)
```

- 内部创建客户端 -> 拉取 -> 关闭，整体超时时间通过 `WithGetTimeout` 配置（默认 30 秒）
- `params` 不能为 `nil`，`Group`/`DataID`/`Format` 必须有效
- **参数非法时在创建客户端之前返回**：这一层提前校验与 `client.GetConfig` 内部校验重复一次，是**有意的防御**
  （不为注定失败的请求初始化 SDK 客户端——SDK 会创建缓存目录与后台 goroutine）；`valid()` 无副作用且仅 ~9ns，重复不产生行为差异
- 返回的 `format` 是归一化后的格式（`yml` -> `yaml`）
- 每次调用都创建/销毁客户端，高频场景建议使用 `NewConfigClient`

**超时说明**：
- `WithGetTimeout`：控制便捷函数 `GetConfig` 整体执行时间（创建+拉取+关闭），通过 `context.WithTimeout` 实现，默认 30s
- `WithTimeoutMs`：控制 Nacos SDK 单次 HTTP 请求超时（毫秒），默认 5000ms
- `Client.GetConfig(ctx, params)` 中的 `ctx`：真正生效（内部 goroutine + select），取消/超时立即返回 `ctx.Err()`

### Client.GetConfig — 复用客户端拉取

```go
func (c *Client) GetConfig(ctx context.Context, params *Params) (string, []byte, error)
```

- 通过 `ctx` 控制超时与取消，调用方可精确控制每次请求的生命周期
- **返回值不用具名返回值**：函数体内多处 `err := ...` 局部变量与返回值同名，具名返回值虽可编译
  （同一块内 `:=` 是复用而非遮蔽），但容易让阅读者误判「赋值给返回值」与「新建局部变量」的边界，因此采用匿名返回值
- **实现方式**：Nacos SDK 的 `GetConfig` 不接受 context，内部将 SDK 调用放入独立 goroutine，
  再通过 `select` 等待 ctx 或结果：ctx 取消/超时时立即返回 `ctx.Err()`，不会被阻塞的 SDK 调用拖住
- **trade-off**：超时返回后，后台 goroutine 仍会运行至 SDK 自身超时（`WithTimeoutMs`，默认 5000ms）才退出，
  期间无法被取消（SDK 不支持）；结果写入带缓冲 channel，不会阻塞或泄漏 goroutine
- 不自动关闭客户端，需调用方负责 `Client.Close()`

### NewConfigClient — 创建可复用的配置客户端

```go
func NewConfigClient(opts ...Option) (*Client, error)
```

- 返回 `*Client`，提供 `GetConfig(ctx, params)` 和 `Close()` 方法
- 用于需要多次读取不同配置文件的场景
- **注意**：`WithGetTimeout` 仅对便捷函数 `GetConfig` 生效，`NewConfigClient` 创建的 `Client.GetConfig(ctx, params)` 完全忽略 `getTimeout`，超时由调用方传入的 `ctx` 控制

### NewNamingClient — 创建 SDK 命名客户端（工厂）

```go
func NewNamingClient(nacosIPAddr string, nacosPort int, nacosNamespaceID string, opts ...Option) (naming_client.INamingClient, error)
```

- 返回 SDK 的 `naming_client.INamingClient`，可直接传入 `pkg/servicerd/registry/nacos.New(...)`，无需再取底层实例
- 位置参数与 `internal/config.NacosRegistryConf.NewNacosInstance(...)` 对齐（地址/端口/命名空间），便于配置层直接展开传入
- `WithServerConfigs` 优先于 `nacosIPAddr`/`nacosPort`；`WithClientConfig` 优先于 `nacosNamespaceID` 与单字段选项
- 本包不再提供注册/发现包装方法（已移除），相关能力统一由 `pkg/servicerd` 承担
- **适用边界**：返回 SDK 接口类型意味着调用方与 `nacos-sdk-go` 的 API 稳定性耦合，仅在本仓 mono-repo 内成立
  （`nacoscli` 与 `servicerd` 同仓，SDK 升级一起改）；若作为独立库对外发布，应改回封装类型

### WatchConfig — 监听配置变更

```go
func WatchConfig(ctx context.Context, params *Params, handler ChangeHandler, opts ...Option) (context.CancelFunc, error)
```

- 启动后台 goroutine 监听，返回 `stop` 函数和 `error`
- `stop()` 调用后 cancel ctx 并等待 goroutine 完全退出（`sync.WaitGroup`）
- `error` 在 ctx 已取消、params 校验失败或 handler 为空时立即返回（不启动后台循环）
- **所有路径均返回非 nil 的 `stop` 函数**，可安全 `defer stop()`，不会因错误路径 panic
- `handler` 签名：`func(namespace, group, dataID, data string)`，不能为空
- 内部使用 `ListenConfig` 注册回调（SDK 内部后台长轮询），`<-ctx.Done()` 阻塞等待
- 无论 `Start` 成功与否，都会调用 `CancelListenConfig` + `CloseClient` 清理资源
- 停止时调用 `CancelListenConfig` 取消注册 + `CloseClient` 释放连接

### NewListenClient — 底层监听客户端

```go
func NewListenClient(params *Params, handler ChangeHandler, opts ...Option) (*ListenClient, error)
```

- `WatchConfig` 内部使用，一般不直接调用
- `Start(ctx)` 返回 `error`：nil 表示 ctx 正常取消，error 表示 ListenConfig 注册失败
- `Stop()` 调用 `CancelListenConfig` 取消注册
- **注意：ListenClient 非并发安全**，不要在多个 goroutine 中同时调用 Start/Stop/Close

### ErrNilParams — 导出错误变量

```go
var ErrNilParams = errors.New("Params 不能为空")
```

- 可用于 `errors.Is(err, nacoscli.ErrNilParams)` 断言
- `GetConfig(nil)` 和 `Client.GetConfig(ctx, nil)` 均返回此错误

---

## 错误处理

| 场景 | 行为 |
|------|------|
| `GetConfig(nil)` / `Client.GetConfig(ctx, nil)` | 返回 `ErrNilParams` 错误 |
| 传入 nil Option | 跳过该 Option，不 panic（动态拼接选项安全） |
| `Client.GetConfig` ctx 取消/超时 | 立即返回 `ctx.Err()`（后台 SDK 调用最多滞留至 `WithTimeoutMs` 超时后自行退出） |
| `Params.Group/DataID/Format` 为空 | 返回对应校验错误 |
| `Params.Format` 不支持 | 返回 `fmt.Errorf("配置文件类型 'Format=%q' 不支持", p.Format)`——用 `%q` 而非 `%s`，`Format` 中的换行/控制字符会被转义，避免一行日志被拆成多行（日志注入） |
| `Port` 为 0 且未提供 `WithServerConfigs` | 返回 "Nacos 服务器端口 (Port 或 WithPort) 不能为空" |
| Nacos 服务器不可达 | 返回 SDK 错误（`从 Nacos 获取配置失败: ...`） |
| `WatchConfig` ctx 已取消 | 立即返回 `ctx.Err()`，不启动后台循环 |
| `WatchConfig` handler 为空 | 返回 `errors.New("配置变更回调函数不能为空")`，stop 非 nil |
| `WatchConfig` params 非法 | 立即返回 error，stop 非 nil，不启动后台循环 |
| `WatchConfig` 创建失败/注册失败 | 自动重试（受 `WithMaxRetries` 控制），retries 持续累积 |
| `WatchConfig` 超过最大重试 | 记录 Error 日志，goroutine 退出 |
| `WatchConfig` 所有错误路径 | 返回非 nil stop 函数，可安全 defer |
| `handler` 回调 panic | recover 捕获，记录 Warn 日志，监听继续 |
| `WatchConfig` 后台 goroutine panic（SDK 内部 `NewListenClient`/`Start`/`Stop` 报错） | recover 捕获并记录 Error 日志，本轮监听终止但**不打死进程**；`wg.Done` 已 defer，`stop()` 仍正常返回 |
| `Client.GetConfig` 后台 goroutine panic | recover 后作为 error 返回；recover 值是 `error` 时用 `%w` 包装（保留错误链，调用方可 `errors.Is`/`As`），非 error 时退回 `%v` |

---

## OpenTelemetry 集成

### 配置获取（Client.GetConfig）

所有通过 `Client.GetConfig` 的配置获取会自动创建 Span：

- **Span 名称**：`nacos.get_config`
- **Span 属性**：
  - `nacos.data_id` — 配置文件 ID
  - `nacos.group` — 配置分组
  - `nacos.config_length` — 配置内容长度
  - `nacoscli.request_id` — 关联的请求 ID（从 ctx 的 `logger.ContextKeyRequestID` 提取）
- **属性文本先经 `safeAttrText` 归一化**：`DataID`/`Group` 是外部可控输入（来自配置文件/配置中心），
  含非法 UTF-8 字节时 OTLP（protobuf string 要求合法 UTF-8）会**整批丢弃该 Span**，因此写入前用
  `strings.ToValidUTF8` 替换；合法输入走 `utf8.ValidString` 快路径，**不发生拷贝**（实测 6.3ns / 0 分配）
- **错误处理**：失败时 `span.RecordError(err)` + `span.SetStatus(codes.Error, ...)`
- **request_id 的 key 是全项目约定**：由 gin/grpc middleware 写入、`pkg/logger` 定义，本包不提供自定义提取函数
  的注入点；若上游使用了不同的 key，应在 middleware 层统一归一，而不是在每个基础设施包重复约定

### 服务注册与发现

`NewNamingClient` 仅创建 SDK 客户端，**不产生 Span**；注册/发现的追踪埋点由 `pkg/servicerd/registry/nacos` 实现
（Span：`nacos.register`、`nacos.deregister`、`nacos.watcher`，属性：`registry.type`、`nacos.service_name`）。

---

## 集成测试

集成测试带 `//go:build integration` 构建标签，依赖真实 Nacos 服务，通过环境变量配置。

**环境变量一览**（与 `integration_test.go` 实际读取的变量一一对应）：

| 变量 | 必填 | 用途 | 默认值 |
|------|------|------|--------|
| `NACOS_IP_ADDR` | 是* | Nacos 服务器地址，未设置时全部集成测试自动 Skip | 无 |
| `NACOS_PORT` | 是* | Nacos 服务器端口 | 无 |
| `NACOS_NAMESPACE_ID` | 否 | 命名空间 ID | `""` |
| `NACOS_GROUP` | 否 | 配置分组 | `dev` |
| `NACOS_DATA_ID` | 否 | 配置文件 ID | `serverNameExample.yml` |
| `NACOS_USERNAME` | 否 | 认证用户名，未设置时认证类测试 Skip | 无 |
| `NACOS_PASSWORD` | 否 | 认证密码 | 无 |
| `NACOS_GRPC_PORT` | 否 | gRPC 端口（通常 = HTTP 端口 + 1000），未设置时服务注册/发现类测试 Skip | 无 |
| `NACOS_CONTEXT_PATH` | 否 | Nacos 控制台上下文路径，Open API 用例（发布配置 / 查实例）拼 URL 用 | `""` |
| `NACOS_TEST_TIMEOUT` | 否 | `TestMain` 限制的集成测试总时长上限（Go duration，如 `180s`、`5m`） | `120s` |

\* `NACOS_IP_ADDR` 与 `NACOS_PORT` 必须同时设置，依赖 Nacos 服务的测试才会执行。

**运行方式**：

```bash
cd pkg/nacoscli

# 方式一：直接传入环境变量
NACOS_IP_ADDR=192.168.3.37 \
NACOS_PORT=8848 \
NACOS_NAMESPACE_ID=de7b176e-91cd-49a3-ac83-beb725979775 \
NACOS_GROUP=dev \
NACOS_DATA_ID=user-srv.yml \
NACOS_USERNAME=admin \
NACOS_PASSWORD=password \
NACOS_GRPC_PORT=9848 \
NACOS_TEST_TIMEOUT=300s \
go test -tags=integration -v -count=1

# 方式二：包内 .env（推荐，无需任何插件）
# `TestMain` 里的 `loadDotEnv` 会自动读取 pkg/nacoscli/.env 并补齐环境变量，
# 语义是「只补缺、不覆盖已设置的变量」，因此命令行上显式导出的变量优先于 .env。
# .env 只需填 NACOS_* 那几行；根目录 .gitignore 的 `*.env` 已覆盖该文件，不会入库，仍请勿提交真实凭据。
go test -tags=integration -v -count=1

# 只跑外部变更监听这一组（回答「别人改了配置本进程能否感知」）
go test -tags=integration -count=1 -v -run 'Config.*Change'

# 仅运行单元测试（无需任何环境变量，推荐日常使用）
go test ./pkg/nacoscli/ -v
```

> `-run` 的多个用例名之间**不要直接用 `|`**：Git Bash 会把 `|` 当管道，导致后半截被当命令执行。
> 用 `Config.*Change` 这类无竖线的正则，或拆成多次执行。

**行为说明**：
- 未设置必需变量时，依赖 Nacos 的测试自动 Skip，参数校验等单元测试不受影响
- **监听类用例的固定预热已抽为常量**（`listenRegistrationWarmup=3s` / `watchRegistrationWarmup=2s` /
  `retryCompletionWait=5s`）而不是散落的 `time.Sleep` 魔法数字。保留固定等待而非改纯轮询，是因为
  SDK 未暴露「注册已生效」的可观测信号；而**结果判定始终靠已有的「轮询 + deadline」循环**，
  预热不足只会让回调晚到、不会造成假失败。出现 flaky 时优先调这三个常量，不要改断言
- `GetConfig` 相关测试区分「加密配置无法解密（预期，记录后通过）」与「基础设施故障（测试失败）」，
  不会再把连接失败/认证失败静默归类为加密配置
- `TestMain` 限制集成测试总时长（默认 120 秒，可用 `NACOS_TEST_TIMEOUT` 上调），防止 Nacos SDK gRPC goroutine 泄漏导致 go test 永远挂起

**实测结果汇总**（公网 Nacos 实例，`go test -tags=integration -count=1`，总耗时 ~80s）：

| 分组 | 结果 | 关键读数 |
|------|------|----------|
| 配置读取 / 加密配置 | PASS（鉴权相关的 4 个按缺失凭据 SKIP） | 匿名读配置返回 `Code: 401, Message: User not found!` |
| SDK 发布 → SDK 监听 | PASS | `TestIntegration_ListenConfigPublishChange`、`TestIntegration_WatchConfigDetectsChange` |
| **外部 Open API 修改 → SDK 监听** | **PASS** | 首次 2.264~2.283s，后续 210~275ms，**空闲 15s 后 220/221ms**，回调总数恰为 3 |
| 服务注册 / 发现 | PASS | 新注册实例并非立即可见，须轮询（`pollHealthyInstances`），否则 SDK 直接返回 error `instance list is empty!` |

**服务发现侧记录的两个环境限制（属部署层面，不是本包缺陷）**：

1. **`HealthyOnly` 的语义陷阱**（SDK v2.3.5，已定位到源码 `naming_client.go` 的 `selectInstances`）：
   过滤条件是 `host.Healthy == param.HealthyOnly`，因此 `HealthyOnly` 缺省（false）时它**只返回不健康实例**，
   与 Nacos HTTP API 的 `healthyOnly=false`（返回全部）含义相反。查询必须显式传 `HealthyOnly: true`。
   生产侧不受影响：`pkg/servicerd/registry/nacos/registry.go` 三处调用均显式传了 `HealthyOnly: true`。
2. **临时实例在服务端不聚合**：连续注册 3 个临时实例（`10.0.0.10:8080` / `11:8081` / `12:8082`）后，
   SDK 侧与服务端 Open API 侧**都只看到 1 个**，且不同时刻看到的那 1 个还会变。
   因此 `TestIntegration_NamingClientListInstances` 只断言「已注册实例能被列举到（≥1）」，
   把「看到几个」降级为日志记录（同时打印 SDK 缓存数与服务端快照），而不是留一条永不满足的断言。

---

## 测试命名规范

本包测试采用「**英文函数名 + 中文注释**」（全仓统一标准，权威定义见
[skill `package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md)）：

- **函数标识符**：`Test<被测方法><场景>` 全英文驼峰，如 `TestWatchConfigCancelledCtx`、`TestBuildConfigsDefaultValues`、`TestSafeCallHandlerPanicRecover`
- **doc 注释**：第一行以函数名开头 + 中文描述测试意图，如 `// TestWatchConfigNilHandler 验证 WatchConfig 对 nil handler 的处理。`
- **子测试名称**：表驱动用例的 `name` 字段使用中文（如 `"恰好达上限"`），因为它只出现在 `go test -v` 输出中，不影响 godoc 与符号索引

为什么不把中文写进函数名（如 `TestInit_连接各种情况`）：

1. `go doc` / IDE 的符号索引、`-run` 过滤、grep 定位都依赖标识符，中文混排后 `go test -run TestGetConfig` 类过滤不再直观
2. 英文函数名与被测方法名一一对应，重命名/重构时工具链可跟踪；中文部分不参与编译，无法反映真实 API 名称
3. 测试意图的可读性由注释和子测试名承担已足够，无需重复到标识符里

> 命名验证：`go test ./pkg/nacoscli/ -run 'TestWatchConfig'` 能精确匹配该 API 的全部用例。
>
> 跨包口径：本命名风格（全英文函数名 + 中文注释 + 中文子测试名，即方案 A）现为**全仓唯一规范**，
> 权威定义与维护均位于 skill [`package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md)
> 第二节；本小节只作为包内快速说明，不另行定义规则。
> 存量方案 B 用例（`pkg/goredis`）已一次性纯重命名收口，全仓已无方案 B 残留。

---

## 性能基线与模糊测试

### 竞态检测

```bash
# 本包
CGO_ENABLED=1 go test -race -count=1 -short ./pkg/nacoscli/
# 全仓
CGO_ENABLED=1 go test -race -count=1 ./...
```

必须带 `CGO_ENABLED=1`（`-race` 依赖 cgo）。本包的并发面是实质性的——
`GetConfig` 内部 goroutine + `select`、`WatchConfig` 的重试计数、`ListenClient` 回调 goroutine、
`logCollector` 的锁与 `capturing` 标记，因此竞态检测必须一行命令可执行。

> 诚实声明：当前开发机（Windows/MinGW）执行 `-race` 会报 `exit status 0xc0000139`（gcc 运行时问题，与本包代码无关），
> 因此本 README **不声称已经跑过 `-race`**，只给出已验证过的命令形式；真实的 `-race` 证据需在 Linux/CI 上采集。

### 基准测试（Benchmark）

```bash
# 全部基准（-run='^$' 用于跳过常规单测，只跑 Benchmark）
go test -run='^$' -bench=. -benchmem ./pkg/nacoscli/
# 需要更稳定的读数时（对比回归推荐：固定迭代次数，读数更可比）
go test -run='^$' -bench=. -benchmem -benchtime=1000x ./pkg/nacoscli/
# 只跑单个基准
go test -run='^$' -bench='BenchmarkValid' -benchmem ./pkg/nacoscli/
```

下表为 Windows/amd64、i5-1135G7 的实测读数（区分 `-benchtime` 已标注）。**它是「相对回归基线」，不是生产延迟**：

| 基准 | 场景 | ns/op | B/op | allocs/op | benchtime |
| --- | --- | --- | --- | --- | --- |
| `ClientGetConfig` | 128B 配置 | 2970 | 769 | 11 | 1000x |
| `ClientGetConfig` | 1KB 配置 | 2934 | 1665 | 11 | 1000x |
| `ClientGetConfig` | 64KB 配置 | 16469 | 66183 | 11 | 1000x |
| `ClientGetConfigWithRequestID` | 1KB + 带 request_id 的 ctx | 2161 | 1728 | 12 | 1000x |
| `Valid` | `yaml` | 9.8 | 0 | 0 | 20000x |
| `Valid` | `yml` 归一化 | 8.3 | 0 | 0 | 20000x |
| `Valid` | JSON 大小写混合 | 77.1 | 8 | 1 | 20000x |
| `Valid` | 不支持的类型（错误路径） | 312~488（波动 ±40%） | 80 | 3 | 20000x |
| `ApplyOptions` | 含 `nil` Option 的选项链 | 107.0 | 176 | 1 | 1000x |
| `BuildConfigs` | 预设完整配置 | 3.6 | 0 | 0 | 1000x |
| `BuildConfigs` | 按单字段组装 | 2930 | 1856 | 8 | 1000x |
| `RequestIDAttr` | ctx 命中 | 26.5 | **0** | **0** | 20000x×4 |
| `RequestIDAttr` | ctx 未命中 | 12.8~14.7 | 0 | 0 | 20000x×4 |
| `SafeAttrText` | 合法 ASCII | 6.3 | 0 | 0 | 20000x |
| `SafeAttrText` | 合法中文 | 21.7 | 0 | 0 | 20000x |
| `SafeAttrText` | 含非法字节（走替换） | 62.7 | 16 | 1 | 20000x |

**口径与解读（很重要，否则数字会被误读）：**

- `ClientGetConfig` 基于包内的 `mockConfigClient`，**不含任何网络 RTT**，它量的是「本包自己的开销」：
  Span 创建/属性/结束 + 参数组装 + content 处理。真实的 `GetConfig` 延迟由 Nacos SDK 长轮询/RPC 决定（毫秒级）
- **分配数不随配置大小增长（恒 11 次）**，只有字节量线性变（769B → 66KB）：说明没有按内容长度做额外
  中间结构；64KB 的 16.5µs 主要是内存拷贝，吞吐 ≈ 4.0GB/s，可接受
- **`RequestIDAttr` 由指针返回改为值返回是一次已证实的优化**：命中路径 69.6ns/64B/1alloc → **26.5ns/0B/0alloc**
  （四次重测 26.2~26.6ns，读数极稳定）。代价在未命中路径：4.6ns → 12.8ns（返回零值结构体需多拷几个字），
  但生产上 request_id 几乎总是存在（全项目 middleware 约定），**命中才是代表场景**，且 8ns 相对整请求 2900ns 占 0.3%
- **`safeAttrText` 证明「防 UTF-8 污染」不是免费但几乎免费**：合法 ASCII 6.3ns（只跑 `utf8.ValidString`）、
  合法中文 21.7ns（逐 rune 解码，**无拷贝**）、只有真现非法字节才付 62.7ns/1 分配——而那是应该报警的异常输入
- **`valid()` 快路径 ~9ns / 0 分配**，错误路径 312~488ns / 3 分配。错误贵 30~50 倍是合理的（要拼错误消息），
  而且它只在请求入口跑一次，不在热循环里；同时印证了「valid 无副作用」（归一化不写回 Params）
- **错误路径的读数不可用于微基准对比**：同一命令四次重测得到 312/418/486/488ns（±40%），而分配恒为 3 次。
  因此本轮把 `Format` 的 `%s` 改 `%q` 带来的额外开销**落在噪声区间内，不能声称准确增量**；
  需要区分优劣时请看 `allocs/op`（稳定）而不是 `ns/op`（本机波动大）
- **`buildConfigs` 的 800 倍差距（3.6ns vs 2930ns）** 是本包最值得记录的结论：预设完整 `ClientConfig`
  几乎零成本，而逐个 `With*` 单字段组装会构造 SDK 配置对象。**高频路径请传 `WithClientConfig`**（详见「Option 列表」优先级说明）
- 带 request_id 的整请求开销：旧基线比不带多 ~130ns（2 个额外分配），本轮值返回后降到 1 个分配（13 vs 12 allocs），
  属于可观测性的固定成本

### 模糊测试（Fuzz）

```bash
# 种子语料随常规 go test 一起执行（无需额外命令，耗时可忽略）
go test -run='^Fuzz' ./pkg/nacoscli/
# 真正的挖掘：不进默认流程，按需本地或定时任务跑（六个 target 逐个挖掘）
go test -run='^$' -fuzz=FuzzParamsValid          -fuzztime=30s ./pkg/nacoscli/
go test -run='^$' -fuzz=FuzzClientGetConfig      -fuzztime=30s ./pkg/nacoscli/
go test -run='^$' -fuzz=FuzzBuildOnChange        -fuzztime=30s ./pkg/nacoscli/
go test -run='^$' -fuzz=FuzzOptionZeroGuard      -fuzztime=30s ./pkg/nacoscli/
go test -run='^$' -fuzz=FuzzSafeAttrText         -fuzztime=30s ./pkg/nacoscli/
go test -run='^$' -fuzz=FuzzWithSchemeWhitelist  -fuzztime=30s ./pkg/nacoscli/
```

各 target 守护的是「不变量」而非具体输出值：

| Target | 不变量 |
| --- | --- |
| `FuzzParamsValid` | 不 panic；校验失败时返回的 format **必为空**（不给半成品）且**错误消息不含裸换行/回车**（防日志注入）；成功时必为 `json`/`yaml`/`toml` 之一且小写；**始终不修改入参 `Params.Format`**（印证「valid 无副作用」） |
| `FuzzClientGetConfig` | 任意 Group/DataID/Format 组合不 panic；出错时 **format 与 content 同时为空**（避免调用方误用半截配置）；成功时内容与 SDK 返回值逐字一致 |
| `FuzzBuildOnChange` | ctx 存活⇒回调**恰好执行一次**且四个参数原样透传；ctx 已取消⇒`calls == 0`（丢弃变更，绝不触达业务 handler） |
| `FuzzOptionZeroGuard` | 任意负值/零值/INT64_MIN 输入后，`timeoutMs`/`createDelay`/`getTimeout` **恒为正**（防止立即超时与 `time.After(0)` 忙循环），`port`/`maxRetries` 非负 |
| `FuzzSafeAttrText` | 输出**必为合法 UTF-8**（否则 OTLP 整批丢 Span）；合法输入**必须原样返回不得被改写**；非法输入必须被替换 |
| `FuzzWithSchemeWhitelist` | 任意输入后 `scheme` **只能**是 `""`/`http`/`https`/`grpc` 之一，且恒为小写、不含首尾空格（白名单外取值不写入，保留先前值） |

> `FuzzBuildOnChange` 与 `FuzzClientGetConfig` 的 mock 闭包内**不触碰 `testing.T`**（跨 goroutine 使用是 data race），
> 只依据入参派生返回值，断言全部在主测试 goroutine 里做——这是带并发回调的库写 Fuzz 时必须遵守的约束。
> `discardLogHooks` 会在挖掘期间屏蔽包内日志钩子（不然测试时间会耗在日志 I/O 上并填满临时目录），退出时恢复。
> 失败语料会写入 `testdata/fuzz/<TargetName>/`，需人工确认后再提交或修正。

本包历轮 Fuzz 挖掘未复现生产缺陷；但**代码审查**发现了一处与 `pkg/goredis` 同类的隐患——
`Client.GetConfig` 把外部可控的 `params.DataID`/`params.Group` 原样写入 Span 属性，
含非法 UTF-8 时会让 OTLP 整批丢弃该 Span（`pkg/goredis` 的同一 bug 当时是由 Fuzz 发现的，见
[`../goredis/README.md`](../goredis/README.md) 「性能基线与模糊测试」章节）。
本轮修复（`safeAttrText`）并用 `FuzzSafeAttrText` 守护这条不变量，避免后续回退。

> 这个对比值得记下来：**同一类 bug 在一个包里被 Fuzz 挖出、在另一个包里靠人工审阅发现**。
> 发现方式本身不重要，关键是把它当生产 bug 登记并写下可守护的不变量——两个包现在都有 `Fuzz*AttrText` 同类 target。
