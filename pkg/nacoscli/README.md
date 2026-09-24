# nacoscli

Nacos 配置中心客户端封装，提供配置拉取、实时监听与服务注册发现能力。

## 架构概览

```
nacoscli/
├── nacoscli.go       # 核心：Params 校验、Client 生命周期、GetConfig/NewConfigClient/NewNamingClient
├── listener.go       # ListenClient：长轮询注册、<-ctx.Done() 阻塞、CancelListenConfig 清理、panic 保护
├── watch.go          # WatchConfig：注册失败重试、重试计数持续累积、提前校验、goroutine 等待退出
├── option.go         # Option 函数：defaultOptions + apply 模式、优先级传播
├── nacoscli_test.go  # 单元测试 + 集成测试（需 NACOS_ADDR 环境变量）
└── README.md
```

**核心设计原则：**

- **Option 优先级**：`WithClientConfig`/`WithServerConfigs`（完整配置） > 单字段 `With*` 选项 > 默认值
- **Params 与 Option 的关系**：`Params` 定义"查什么"（Group/DataID/Format），`Option` 定义"怎么连"（地址/认证/超时），两者互补
- **OpenTelemetry 内置**：所有配置获取自动创建 Span，记录 `nacos.data_id`、`nacos.group`、`request_id` 等属性
- **panic 安全**：监听回调内置 recover，单次 panic 不会导致监听协程退出
- **valid() 无副作用**：返回归一化后的 Format，不修改原始 Params 结构体

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

**重试语义**：WatchConfig 只在两个时机重试：
- `NewListenClient` 创建失败（Nacos 地址不可达、认证失败等）
- `ListenConfig` 注册失败（SDK 内部状态异常、缓存目录不可写等）

一旦注册成功，连接的健康维护由 Nacos SDK 内部长轮询负责，WatchConfig 不再介入。

**重试行为配置**：

```go
stop, err := nacoscli.WatchConfig(ctx, params, handler,
    nacoscli.WithIPAddr("192.168.3.37"),
    nacoscli.WithPort(8848),
    // 最多重试 10 次后放弃（默认 0 = 无限重试）
    nacoscli.WithMaxRetries(10),
    // 注册失败后等待 5 秒再重试（默认 5s）
    nacoscli.WithCreateDelay(5*time.Second),
)
```

**重试计数规则**：

- `retries` 仅在 `NewListenClient` 创建失败或 `ListenConfig` 注册失败时递增
- 由于 `Start` 一旦成功返回（ctx 取消）就退出，不存在“运行一段时间后重试计数重置”的场景
- `retries >= maxRetries` 时记录 Error 日志并退出 goroutine
- `maxRetries = 0`（默认）时无限重试

> **生产环境提示**：默认无限重试 + 固定 5 秒延迟意味着 Nacos 长期不可达时会持续刷 Warn 日志。建议在生产环境显式设置 `WithMaxRetries`（如 10~20 次），避免日志膨胀。

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

### 场景四：服务注册与发现（NewNamingClient）

**适用场景**：微服务注册到 Nacos 或从 Nacos 发现其他服务实例。

```go
namingClient, err := nacoscli.NewNamingClient(
    "192.168.3.37",
    8848,
    "de7b176e-91cd-49a3-ac83-beb725979775",
    nacoscli.WithAuth("admin", "password"),
)
if err != nil {
    log.Fatal(err)
}

// 注册服务
success, err := namingClient.RegisterInstance(vo.RegisterInstanceParam{
    Ip:          "10.0.0.1",
    Port:        8080,
    ServiceName: "user-srv",
    Weight:      1,
    Enable:      true,
    Healthy:     true,
    Ephemeral:   true,
})
_ = success

// 发现服务
instances, err := namingClient.SelectInstances(vo.SelectInstancesParam{
    ServiceName: "user-srv",
    GroupName:   "DEFAULT_GROUP",
    Clusters:    []string{},
    Healthy:     true,
})
```

**注意**：`NewNamingClient` 返回 Nacos SDK 的 `INamingClient` 接口，**没有** `GetConfig`/`Close` 方法。它与 `NewConfigClient` 返回的 `*Client` 是完全不同的类型。

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

## Params 结构体

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `IPAddr` | `string` | 是* | Nacos 服务器地址 |
| `Port` | `int` | 是* | Nacos 服务器端口 |
| `Scheme` | `string` | 否 | 协议，默认空（SDK 默认 http） |
| `ContextPath` | `string` | 否 | 上下文路径 |
| `NamespaceID` | `string` | 否 | 命名空间 ID |
| `Group` | `string` | **是** | 配置分组，如 `dev`、`prod` |
| `DataID` | `string` | **是** | 配置文件 ID，如 `user-srv.yml` |
| `Format` | `string` | **是** | 配置类型：`json`、`yaml`（`yml` 会自动归一化）、`toml` |

> *`IPAddr`/`Port` 可通过 `WithIPAddr`/`WithPort` 或 `WithServerConfigs` 提供；若未提供 `WithServerConfigs`，二者均为必填（`Port == 0` 会报错）。

**参数校验规则**：`Group`、`DataID`、`Format` 为空时返回错误；`Format` 不在支持列表时返回错误。`valid()` 返回归一化后的 Format，**不修改**原始 Params 结构体。

---

## Option 列表

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithIPAddr(ip)` | Nacos 服务器地址 | `""` | 全部 |
| `WithPort(port)` | Nacos 服务器端口，负值被忽略，0 在未提供 `WithServerConfigs` 时报错 | `0` | 全部 |
| `WithScheme(scheme)` | 协议（http/grpc） | `""` | 全部 |
| `WithContextPath(path)` | 上下文路径 | `""` | 全部 |
| `WithNamespaceID(id)` | 命名空间 ID | `""` | 全部 |
| `WithTimeoutMs(ms)` | SDK 请求超时（毫秒），负值被忽略 | `5000` | 全部 |
| `WithGetTimeout(d)` | GetConfig 便捷函数整体超时 | `30s` | `GetConfig` |
| `WithAuth(user, pass)` | 认证用户名/密码 | `""` | 全部 |
| `WithClientConfig(cfg)` | 完整 SDK ClientConfig | `nil` | 全部 |
| `WithServerConfigs(cfgs)` | 完整 SDK ServerConfig 列表 | `nil` | 全部 |
| `WithMaxRetries(n)` | 最大重试次数，0=无限，负值被忽略 | `0` | `WatchConfig` |
| `WithCreateDelay(d)` | 创建或注册失败重试等待时间，负值被忽略 | `5s` | `WatchConfig` |

---

## API 速查

### GetConfig 便捷函数 — 一次性拉取

```go
func GetConfig(params *Params, opts ...Option) (format string, content []byte, err error)
```

- 内部创建客户端 -> 拉取 -> 关闭，整体超时时间通过 `WithGetTimeout` 配置（默认 30 秒）
- `params` 不能为 `nil`，`Group`/`DataID`/`Format` 必须有效
- 返回的 `format` 是归一化后的格式（`yml` -> `yaml`）
- 每次调用都创建/销毁客户端，高频场景建议使用 `NewConfigClient`

**超时说明**：
- `WithGetTimeout`：控制便捷函数 `GetConfig` 整体执行时间（创建+拉取+关闭），通过 `context.WithTimeout` 实现
- `WithTimeoutMs`：控制 Nacos SDK 单次 HTTP 请求超时（毫秒），默认 5000ms
- `Client.GetConfig(ctx, params)` 中的 `ctx`：仅用于调用前取消检查，无法中断 SDK 内部正在进行的网络请求

### Client.GetConfig — 复用客户端拉取

```go
func (c *Client) GetConfig(ctx context.Context, params *Params) (format string, content []byte, err error)
```

- 通过 `ctx` 控制超时与取消，调用方可精确控制每次请求的生命周期
- **注意**：`ctx` 仅用于调用前的取消检查（校验参数前的 `select ctx.Done()`）。
  由于 Nacos SDK 的 `GetConfig` 不接受 context，实际网络超时由 `WithTimeoutMs`（默认 5000ms）控制。
  若需要整体超时限制，建议通过 `WithGetTimeout`（默认 30s）配合 `context.WithTimeout` 实现调用级超时。
- 不自动关闭客户端，需调用方负责 `Client.Close()`

### NewConfigClient — 创建可复用的配置客户端

```go
func NewConfigClient(opts ...Option) (*Client, error)
```

- 返回 `*Client`，提供 `GetConfig(ctx, params)` 和 `Close()` 方法
- 用于需要多次读取不同配置文件的场景
- **注意**：`WithGetTimeout` 仅对便捷函数 `GetConfig` 生效，`NewConfigClient` 创建的 `Client.GetConfig(ctx, params)` 完全忽略 `getTimeout`，超时由调用方传入的 `ctx` 控制

### NewNamingClient — 创建命名客户端

```go
func NewNamingClient(nacosIPAddr string, nacosPort int, nacosNamespaceID string, opts ...Option) (naming_client.INamingClient, error)
```

- 返回 Nacos SDK 的 `INamingClient` 接口，用于服务注册/注销/发现
- **注意**：`INamingClient` 没有 `GetConfig`/`Close` 方法，与 `*Client` 完全不同

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
| `Params.Group/DataID/Format` 为空 | 返回对应校验错误 |
| `Params.Format` 不支持 | 返回 `fmt.Errorf("配置文件类型 'Format=%s' 不支持")` |
| `Port` 为 0 且未提供 `WithServerConfigs` | 返回 "Nacos 服务器端口 (Port 或 WithPort) 不能为空" |
| Nacos 服务器不可达 | 返回 SDK 错误（`从 Nacos 获取配置失败: ...`） |
| `WatchConfig` ctx 已取消 | 立即返回 `ctx.Err()`，不启动后台循环 |
| `WatchConfig` handler 为空 | 返回 `errors.New("配置变更回调函数不能为空")`，stop 非 nil |
| `WatchConfig` params 非法 | 立即返回 error，stop 非 nil，不启动后台循环 |
| `WatchConfig` 创建失败/注册失败 | 自动重试（受 `WithMaxRetries` 控制），retries 持续累积 |
| `WatchConfig` 超过最大重试 | 记录 Error 日志，goroutine 退出 |
| `WatchConfig` 所有错误路径 | 返回非 nil stop 函数，可安全 defer |
| `handler` 回调 panic | recover 捕获，记录 Warn 日志，监听继续 |

---

## OpenTelemetry 集成

所有通过 `Client.GetConfig` 的配置获取会自动创建 Span：

- **Span 名称**：`nacos.get_config`
- **Span 属性**：
  - `nacos.data_id` — 配置文件 ID
  - `nacos.group` — 配置分组
  - `nacos.config_length` — 配置内容长度
  - `nacoscli.request_id` — 关联的请求 ID（从 ctx 中提取）
- **错误处理**：失败时 `span.RecordError(err)` + `span.SetStatus(codes.Error, ...)`

---

## 集成测试

集成测试依赖真实 Nacos 服务，通过 `NACOS_ADDR` 环境变量配置地址：

```bash
# 运行集成测试（格式: host:port 或 host:port/namespaceID）
NACOS_ADDR=192.168.3.37:8848 go test ./pkg/nacoscli/ -v
NACOS_ADDR=192.168.3.37:8848/de7b176e-91cd-49a3-ac83-beb725979775 go test ./pkg/nacoscli/ -v

# 仅运行单元测试（默认行为）
go test ./pkg/nacoscli/ -v
```

未设置 `NACOS_ADDR` 时，所有需要 Nacos 服务的测试自动跳过，参数校验等单元测试不受影响。
