# nacoscli

Nacos 配置中心客户端封装，提供配置拉取、实时监听与服务注册发现能力。

## 架构概览

```
nacoscli/
├── nacoscli.go     # 核心：Params 校验、Client 生命周期、GetConfig/NewClient 公共 API
├── listener.go     # ListenClient：长轮询监听、panic 保护、优雅关闭
├── watch.go        # WatchConfig：自动重连包装、重试计数、延迟可配置
├── option.go       # Option 函数：defaultOptions + apply 模式、优先级传播
└── nacoscli_test.go
```

**核心设计原则：**

- **Option 优先级**：`WithClientConfig`/`WithServerConfigs`（完整配置） > 单字段 `With*` 选项 > 默认值
- **Params 与 Option 的关系**：`Params` 定义"查什么"（Group/DataID/Format），`Option` 定义"怎么连"（地址/认证/超时），两者互补
- **OpenTelemetry 内置**：所有配置获取自动创建 Span，记录 `nacos.data_id`、`nacos.group`、`request_id` 等属性
- **panic 安全**：监听回调内置 recover，单次 panic 不会导致监听协程退出

---

## 使用场景选择

### 场景一：一次性拉取配置（GetConfig）

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

**内部行为**：创建客户端 -> 拉取配置 -> 关闭客户端，固定 30 秒超时。

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

// 返回 cancel 函数，进程退出时调用以优雅停止
cancel := nacoscli.WatchConfig(context.Background(), params, handler,
    nacoscli.WithIPAddr("192.168.3.37"),
    nacoscli.WithPort(8848),
    nacoscli.WithAuth("admin", "password"),
)

defer cancel()
```

**内部行为**：

1. 创建 `ListenClient` 并启动长轮询（Nacos SDK 的 `ListenConfig`）
2. 配置变更时调用 `handler`，内置 recover 保护
3. 连接断开后自动重连（默认等待 3 秒）
4. 创建失败后自动重试（默认等待 5 秒）
5. 调用 `cancel()` 或 ctx 取消时，优雅停止并清理资源

**重试行为配置**：

```go
cancel := nacoscli.WatchConfig(ctx, params, handler,
    nacoscli.WithIPAddr("192.168.3.37"),
    nacoscli.WithPort(8848),
    // 最多重试 10 次后放弃（默认 0 = 无限重试）
    nacoscli.WithMaxRetries(10),
    // 创建失败后等待 5 秒再重试（默认 5s）
    nacoscli.WithCreateDelay(5 * time.Second),
    // 连接断开后等待 3 秒再重连（默认 3s）
    nacoscli.WithReconnectDelay(3 * time.Second),
)
```

**重试计数规则**：

- 每次创建监听器失败：`retries++`
- 每次连接断开：`retries++`
- `retries >= maxRetries` 时记录 Error 日志并退出 goroutine
- `maxRetries = 0`（默认）时无限重试

---

### 场景三：复用客户端多次获取配置

**适用场景**：同一服务需要频繁读取多个 Nacos 配置文件，复用底层连接避免反复创建/销毁。

```go
// 创建客户端
client, err := nacoscli.NewClient(
    "192.168.3.37", 8848, "de7b176e-91cd-49a3-ac83-beb725979775",
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
```

**注意**：`GetConfig`（便捷函数）每次调用都会创建/销毁客户端，高频场景应使用 `NewClient` + `client.GetConfig`。

---

### 场景四：服务注册与发现

**适用场景**：微服务注册到 Nacos 或从 Nacos 发现其他服务实例。

```go
namingClient, err := nacoscli.NewClient(
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

// 发现服务
instances, err := namingClient.SelectInstances(vo.SelectInstancesParam{
    ServiceName: "user-srv",
    GroupName:   "DEFAULT_GROUP",
    Clusters:    []string{},
    Healthy:     true,
})
```

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

> *`IPAddr`/`Port` 在 `Params` 中为可选，可通过 `WithIPAddr`/`WithPort` 或 `WithServerConfigs` 提供。

**参数校验规则**：`Group`、`DataID`、`Format` 为空时返回错误；`Format` 不在支持列表时返回错误。

---

## Option 列表

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithIPAddr(ip)` | Nacos 服务器地址 | `""` | 全部 |
| `WithPort(port)` | Nacos 服务器端口 | `0` | 全部 |
| `WithScheme(scheme)` | 协议（http/grpc） | `""` | 全部 |
| `WithContextPath(path)` | 上下文路径 | `""` | 全部 |
| `WithNamespaceID(id)` | 命名空间 ID | `""` | 全部 |
| `WithTimeoutMs(ms)` | 请求超时（毫秒） | `5000` | 全部 |
| `WithAuth(user, pass)` | 认证用户名/密码 | `""` | 全部 |
| `WithClientConfig(cfg)` | 完整 SDK ClientConfig | `nil` | 全部 |
| `WithServerConfigs(cfgs)` | 完整 SDK ServerConfig 列表 | `nil` | 全部 |
| `WithMaxRetries(n)` | 最大重试次数，0=无限 | `0` | `WatchConfig` |
| `WithCreateDelay(d)` | 创建失败重试等待时间 | `5s` | `WatchConfig` |
| `WithReconnectDelay(d)` | 连接断开重连等待时间 | `3s` | `WatchConfig` |

---

## API 速查

### GetConfig — 一次性拉取

```go
func GetConfig(params *Params, opts ...Option) (format string, content []byte, err error)
```

- 内部创建客户端 -> 拉取 -> 关闭，固定 30 秒超时
- `params` 不能为 `nil`
- 返回的 `format` 是归一化后的格式（`yml` -> `yaml`）
- 返回的 `content` 是原始配置文本的 `[]byte`

### NewClient — 创建命名客户端

```go
func NewClient(nacosIPAddr string, nacosPort int, nacosNamespaceID string, opts ...Option) (naming_client.INamingClient, error)
```

- 返回 Nacos SDK 的 `INamingClient` 接口，用于服务注册/注销/发现
- 前三个参数为快捷参数，被 `WithServerConfigs`/`WithClientConfig` 覆盖

### WatchConfig — 监听配置变更

```go
func WatchConfig(ctx context.Context, params *Params, handler ChangeHandler, opts ...Option) context.CancelFunc
```

- 启动后台 goroutine 监听，返回 `CancelFunc` 用于优雅停止
- `handler` 签名：`func(namespace, group, dataID, data string)`
- `data` 参数为 Nacos 推送的最新完整配置内容
- 内置 recover 保护，handler panic 不会导致监听退出

### NewListenClient — 底层监听客户端

```go
func NewListenClient(params *Params, handler ChangeHandler, opts ...Option) (*ListenClient, error)
```

- `WatchConfig` 内部使用，一般不直接调用
- `handler` 不能为空，`params` 不能为空

---

## 错误处理

| 场景 | 行为 |
|------|------|
| `GetConfig(nil)` | 返回 `errors.New("Params 不能为空")` |
| `Params.Group/DataID/Format` 为空 | 返回对应校验错误 |
| `Format` 不支持 | 返回 `fmt.Errorf("配置文件类型 'Format=%s' 不支持")` |
| Nacos 服务器不可达 | 返回 SDK 错误（`从 Nacos 获取配置失败: ...`） |
| `WatchConfig` 创建失败 | 自动重试（受 `WithMaxRetries` 控制），记录 Warn 日志 |
| `WatchConfig` 连接断开 | 自动重连（受 `WithReconnectDelay` 控制），记录 Warn 日志 |
| `WatchConfig` 超过最大重试 | 记录 Error 日志，goroutine 退出 |
| `handler` 回调 panic | recover 捕获，记录 Warn 日志，监听继续 |

---

## OpenTelemetry 集成

所有通过 `Client.getConfig` 的配置获取会自动创建 Span：

- **Span 名称**：`nacos.get_config`
- **Span 属性**：
  - `nacos.data_id` — 配置文件 ID
  - `nacos.group` — 配置分组
  - `nacos.config_length` — 配置内容长度
  - `nacoscli.request_id` — 关联的请求 ID（从 ctx 中提取）
- **错误处理**：失败时 `span.RecordError(err)` + `span.SetStatus(codes.Error, ...)`

---

## 集成测试

集成测试依赖真实 Nacos 服务，通过 `NACOS_ADDR` 环境变量控制：

```bash
# 运行集成测试
NACOS_ADDR=192.168.3.37 go test ./pkg/nacoscli/ -v

# 仅运行单元测试（默认行为）
go test ./pkg/nacoscli/ -v
```

未设置 `NACOS_ADDR` 时，所有需要 Nacos 服务的测试自动跳过，参数校验等单元测试不受影响。
