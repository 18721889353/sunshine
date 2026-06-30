---
name: gows-package
description: Guides modification and extension of the gows WebSocket package, including OpenTelemetry tracing integration, request_id propagation, and code generation templates. Use when working with gows/, protoc-gen-go-gin templates, or WebSocket-related code.
---

# 上下文传播与 Ctx 变体设计规范

## 核心模式：Ctx 变体方法

项目中涉及 I/O 操作、阻塞调用或创建 Span 的公共方法，均需提供带 `context.Context` 的 Ctx 变体，以满足链路追踪和超时控制需求。

### 模式实现

```go
// Ctx 变体：接受外部 ctx，创建 Span 进行链路追踪
func (s *Service) DoSomethingCtx(ctx context.Context, req *Request) (*Response, error) {
    ctx, span := tracer.Start(ctx, "svc.doSomething", trace.WithSpanKind(trace.SpanKindInternal))
    defer span.End()
    // ... 业务逻辑
}

// 无参版本：委托给 Ctx 变体，使用内部默认 ctx
func (s *Service) DoSomething(req *Request) (*Response, error) {
    return s.DoSomethingCtx(s.ctx, req)
}
```

### 规则

| 规则 | 说明 |
|------|------|
| **Ctx 变体优先** | 先实现带 Ctx 的完整版本 |
| **无参委托** | 原无参方法只做委托调用，避免重复实现 |
| **nil 降级** | Ctx 变体遇到 `nil` 应降级使用内部默认 ctx |
| **Span 创建** | Ctx 变体内部创建 `tracer.Start(ctx, ...)`，无参版本不重复创建 |

### 项目中的实际应用

| 包 | 示例 |
|----|------|
| `pkg/gows/readwrite.go` | `WriteJSONCtx` / `WriteRawCtx` / `ReadMessageCtx` |
| `pkg/kafka` | `ProduceCtx` / `ConsumeCtx` |
| `pkg/gohttp` | 涉及 request_id 传播的方法 |

---

## 文件结构参考 (`pkg/gows/`)

按职责单一原则拆分后的文件布局（注意命名对称性：主文件 `xxx.go` + Options `xxx_options.go`）：

| 文件 | 职责 | 行数 |
|------|------|------|
| ~~`doc.go`~~ | ~~包文档和示例（已删除，包注释非必需）~~ |
| `errors.go` | 错误类型 | ~10 |
| `message.go` | `Message` 通用消息结构体 | ~11 |
| `client_options.go` | `ClientOption`/`clientOptions`、`defaultClientOptions`+`apply`、`withClient*` 系列 | ~105 |
| `client.go` | `Client` 结构体、`NewClient`、`Close`/`forceCloseConn`、`UID`、`RemoteAddr`/`Context`/`Done`、`requestIDAttr` | ~180 |
| `health.go` | `healthState`、`IsAlive`、`ClientStats`/`Stats`、读写错误记录 | ~160 |
| `readwrite.go` | `readLoop`/`writeLoop`/`writeWithRetry`、`WriteJSON*`/`WriteRaw*`/`ReadMessage*` | ~300 |
| `heartbeat.go` | `SetupPongHandler`、`StartHeartbeat` | ~132 |
| `heartbeat_options.go` | `HeartbeatOption`/`heartbeatOptions`、`defaultHeartbeatOptions`+`apply`、`WithHeartbeatInterval`、`WithPongTimeout`、`WithPingWriteWait` | ~63 |
| `auth.go` | `ParseTokenCtx`、`extractUID` | ~88 |
| `upgrader.go` | `Upgrade` 函数、CORS/限流/IP 检查 | ~235 |
| `upgrade_options.go` | `upgradeOptions`/`UpgradeOption`、`defaultUpgradeOptions`+`apply`、所有 `With*` (UpgradeOption) | ~280 |
| `dispatcher.go` | `DistributedDispatcher` 结构体、`NewDispatcher`/`Start`/`Stop`、查询方法 | ~302 |
| `dispatcher_options.go` | `dispatcherOptions`/`DispatcherOption`、`defaultDispatcherOptions`+`apply`、`WithMaxConnections`、`WithWorkerPool`、`ErrMaxConnections` | ~60 |
| `dispatcher_lifecycle.go` | `RegisterCtx`/`UnregisterCtx`、`publishClientEvent`、`subscribeUID`/`unsubscribeUID` | ~180 |
| `dispatcher_receive.go` | `receiveLoop`/`dispatchMessage`/`deliverMessage`、`handleRemoteOnline`/`Offline`、`deliverBroadcast`/`deliverToUIDs` | ~235 |
| `dispatcher_send.go` | `SendToUIDCtx`/`SendToMultiUIDCtx`、`BroadcastCtx`/`BroadcastFilterCtx` | ~160 |
| `backend.go` | `Backend` 接口、`PubSubMessage` 结构体 | ~35 |
| `rabbitmq_backend.go` | `RabbitMQBackend` 实现（Publish/Receive/Close） | ~391 |
| `test_helpers.go` | 共享测试工具：`newTestClientPair`/`newTestClientWithUID`/`newTestServer`/`mockBackend` | ~200 |
| `integration_test.go` | 总测：全生命周期 + 分布式消息 + 并发 Stats 一致性 | ~306 |
| `rabbitmq_backend_integration_test.go` | RabbitMQ 集成测试（需 `RABBITMQ_INTEGRATION=true`） | ~148 |

**如何定位代码**：一个逻辑变更通常只涉及上述 1~2 个文件。例如修改 Upgrade 流程看 `upgrader.go` + `upgrade_options.go`，修改消息发送看 `dispatcher_send.go`。

**测试文件命名对称性**：每个源文件 `xxx.go` 对应一个 `xxx_test.go`（测试文件），Options 文件对应 `xxx_options_test.go`。共享测试工具统一放在 `test_helpers.go`。

---

## 模式一：构造函数接收 Context

### 问题场景

对象在初始化时就需要 context（用于生命周期管理和链路追踪），而不是创建后再通过 SetXxx 修补。

### 正确做法

```go
// ✅ 直接接收 ctx
func NewClient(ctx context.Context, conn *websocket.Conn, uid string, opts ...Option) *Client {
    clientCtx, cancel := context.WithCancel(ctx)
    return &Client{
        ctx:       clientCtx,
        ctxCancel: cancel,
    }
}
```

### 反模式

```go
// ❌ SetContext 修补方案
func NewClient(conn *websocket.Conn, uid string) *Client {
    ctx, cancel := context.WithCancel(context.Background())  // 浪费
    return &Client{ctx: ctx, ctxCancel: cancel}
}
func (c *Client) SetContext(ctx context.Context) {
    c.ctx = ctx  // c.ctxCancel 脱钩！指向旧的 background context
}
```

### 规则

| 规则 | 说明 |
|------|------|
| **无序的包文档可直接删除** | `doc.go` 中的包注释和示例不是必需的，删除不影响编译和运行 |
| **构造函数优先** | 构造函数第一个参数应为 `ctx context.Context` |
| **派生子 context** | 内部用 `WithCancel(ctx)` 派生，保证 `Close()` 时 `ctx.Done()` 生效但不影响调用方 ctx |
| **ctxCancel 一致性** | `c.ctx` 和 `c.ctxCancel` 必须始终成对创建，不可分开修改 |
| **禁止 SetContext** | 不要提供事后替换 ctx 的方法，应在构造函数中一次确定 |

---

## 模式二：Option 配置传播

### 问题场景

一个包内部有多个 Option 入口（如 `ClientOption` 和 `UpgradeOption`），相同的配置项需要在不同入口间传播，且入口函数名不能冲突。

### Options 统一模式：defaultXxx + apply

所有 Options 使用统一的 `defaultXxxOptions()` + `apply()` 模式（参考 auth.go）：

```go
// 1. Options 结构体（内部）
type clientOptions struct {
    writeQueueSize int
    readQueueSize  int
}

// 2. Option 函数类型
type ClientOption func(*clientOptions)

// 3. defaultXxxOptions() — 默认值集中管理
func defaultClientOptions() *clientOptions {
    return &clientOptions{
        writeQueueSize: 64,
        readQueueSize:  64,
    }
}

// 4. apply() — 应用选项
func (o *clientOptions) apply(opts ...ClientOption) {
    for _, opt := range opts {
        opt(o)
    }
}

// 5. 入口函数简洁
func NewClient(ctx context.Context, conn *websocket.Conn, uid string, opts ...ClientOption) *Client {
    o := defaultClientOptions()
    o.apply(opts...)
    // ... 使用 o.writeQueueSize / o.readQueueSize
}
```

### 同包同名函数冲突处理

```go
// client.go — 包内使用的 ClientOption
func withClientReadLimit(limit int64) ClientOption {  // 首字母小写，包内使用
    return func(o *clientOptions) { o.readLimit = limit }
}

// upgrade_options.go — 导出给外部使用的 UpgradeOption
func WithReadLimit(limit int64) UpgradeOption {  // 首字母大写，外部使用
    return func(o *upgradeOptions) { o.readLimit = limit }
}
```

### Upgrade → Client 配置统一传递

```go
// Upgrade() 中通过 buildClientOpts 将配置转为 ClientOption 列表，统一传入 NewClient
client := NewClient(ctx, rawConn, uid, buildClientOpts(o)...)

// buildClientOpts 将 upgradeOptions 中的 Client 字段批量转为 ClientOption
func buildClientOpts(o *upgradeOptions) []ClientOption {
    var opts []ClientOption
    if o.writeQueueSize > 0  { opts = append(opts, withWriteQueueSize(o.writeQueueSize)) }
    if o.readTimeout > 0     { opts = append(opts, withClientReadTimeout(o.readTimeout)) }
    if o.writeTimeout > 0    { opts = append(opts, withClientWriteTimeout(o.writeTimeout)) }
    if o.readLimit > 0       { opts = append(opts, withClientReadLimit(o.readLimit)) }
    if o.writeLimit > 0      { opts = append(opts, withClientWriteLimit(o.writeLimit)) }
    return opts
}
```

Benefits over direct field assignment:
- NewClient 创建 channel 时直接使用正确容量，解决物理容量与元数据不一致问题
- 配置传播链路清晰，所有 Client 配置统一经过 option 系统
- 新增配置只需升级 buildClientOpts，无需修改 Upgrade 主函数

### 规则

| 规则 | 说明 |
|------|------|
| **defaultXxx + apply 统一模式** | 所有 Options 必须包含 `defaultXxxOptions()`（默认值集中管理）和 `(o *xxxOptions) apply()`（应用选项）方法，入口函数使用 `o := defaultXxxOptions(); o.apply(opts...)` |
| **文件对称命名** | 主文件 `xxx.go` + Options 文件 `xxx_options.go`，成对出现 |
| **文件名精确反映职责** | 文件名必须精确匹配内容，例如 `conn.go` 只含心跳代码 → 重命名为 `heartbeat.go` |
| **文件三对称原则** | 同类职责拆多个文件时命名成对：`lifecycle` <-> `receive` <-> `send`、`register` 必须和 `unregister` 在同一文件 |
| **命名前缀** | 包内使用的 Option 函数以 `withClient*` / `withServer*` 为前缀（小写） |
| **导出函数** | 外部使用的 Option 函数以 `With*` 为前缀（大写），在不同文件中可重名 |
| **配置统一通过 ClientOption 传入** | Upgrade → NewClient 的配置传播统一走 option 系统，用 `buildClientOpts` 批量转换，不在主函数中逐个字段赋值 |
| **`> 0` 判断** | 零值表示"不设置"或"使用默认值"，option 内部判断 `> 0` 才生效 |
| **条件前置到调用处** | 涉及配置项的条件判断在调用处用 `if o.field` 模式判断，不在被调函数内部做 guard clause |

---

## 模式三：配置结构对称性

### 问题场景

多个配置结构体（`clientOptions`、`Client`、`upgradeOptions`）描述同一组配置时，字段缺失导致配置永远无法传播到目标对象。

### 检查清单

修改配置相关字段时，检查以下结构体是否对称：

```go
// 1. 入口 Option（如 clientOptions / upgradeOptions）
type upgradeOptions struct {
    readTimeout  time.Duration
    writeTimeout time.Duration
    readLimit    int64
    writeLimit   int64
}

// 2. 目标结构体（如 Client）
type Client struct {
    readTimeout  time.Duration
    writeTimeout time.Duration
    readLimit    int64
    writeLimit   int64
}

// 3. 配置传播段（Upgrade() / NewClient() 中）
if o.readLimit > 0 {
    client.readLimit = o.readLimit     // ← 两边都有才不会遗漏
}
```

### YAML → Go Config 全链路

```
configs/*.yml                   # YAML 定义（唯一数据源）
  ↓ make update-config
internal/config/*.go            # 自动生成 Go struct
  ↓ 代码生成模板
cmd/protoc-gen-xxx/template.go  # 模板中读取 config.Get().Xxx.Yyy
```

修改流程：**改 YAML → `make update-config` → 编译验证**，三步缺一不可。

### YAML 嵌套子分组设计

WebSocket 配置按功能分为 6 组，每组一个 YAML 子键，Go 结构体自动嵌套生成：

```yaml
# configs/serverNameExample.yml
websocket:
  cors:               # CORS & 连接基础
    cors, allowedOrigins, readBufferSize, writeBufferSize, enableCompression, subprotocols

  limit:              # 限流与安全防护
    enableRateLimit, rateLimitRps, rateLimitBurst, maxConnPerIP, readLimit, writeLimit

  timeout:            # 超时控制
    readTimeout, writeTimeout

  heartbeat:          # 心跳保活
    enableHeartbeat, heartbeatInterval, pongTimeout, pingWriteWait

  queue:              # 客户端队列
    writeQueueSize, readQueueSize

  distributed:        # 分布式分发
    enableDistributed, workerPool, maxConnections
```

模板中访问路径变为嵌套形式，在 `router/template.go` 中统一做路径映射：

```go
// YAML 路径 → gows Option（router/template.go）
// .Websocket.Limit.RateLimitRps     → gows.WithRateLimit(rps, burst)
// .Websocket.Timeout.ReadTimeout    → gows.WithReadTimeout(duration)
// .Websocket.Queue.WriteQueueSize   → gows.WithQueueSize(write, read)
// .Websocket.Distributed.WorkerPool → gows.WithWorkerPool(n)
```

### 新增配置时的完整检查清单

当需要新增一个 WebSocket 可配置项时，按顺序检查：

| # | 步骤 | 说明 |
|---|------|------|
| 1 | `configs/serverNameExample.yml` | 在对应分组下添加 YAML 字段（唯一数据源） |
| 2 | `make update-config` | 自动生成 Go 结构体和字段，无需手动改 `internal/config/*.go` |
| 3 | `gows/upgrade_options.go` | `upgradeOptions` 结构体添加字段 + `defaultUpgradeOptions()` 设默认值 |
| 4 | `gows/upgrader.go` | `Upgrade()` 中添加字段传播到 `Client`（使用 `> 0` 判断） |
| 5 | `gows/client.go` | `Client` 结构体添加字段（存储传播过来的值） |
| 6 | `cmd/protoc-gen-go-gin/.../template.go` | 模板中读取 `config.Get().Websocket.Group.Field` 并调用对应 `With*` |
| 7 | 编译验证 | `go build ./pkg/gows/...` + `./cmd/protoc-gen-go-gin/...` |

---

## 模式四：请求上下文（request_id）传播

### Span 中的 request_id

所有 I/O 操作的 Span 中必须携带 `request_id` attribute，用于跨服务追踪。

```go
func requestIDAttr(ctx context.Context) attribute.KeyValue {
    if ctx == nil {
        return attribute.String("request_id", "")
    }
    // 从 ctx 中提取 request_id
    if rid, ok := ctx.Value("request_id").(string); ok && rid != "" {
        return attribute.String("request_id", rid)
    }
    return attribute.String("request_id", "")
}
```

### 使用方式

```go
func (c *Client) WriteJSONCtx(ctx context.Context, v any) error {
    if ctx == nil {
        ctx = c.ctx
    }
    _, span := tracer.Start(ctx, "ws.write")
    defer span.End()
    span.SetAttributes(
        attribute.String("ws.uid", c.uid),
        requestIDAttr(ctx),
    )
    // ... 业务逻辑
}
```

---

## 模式五：测试文件组织

### 问题场景

测试文件混杂多个源文件的测试用例，或者共享测试工具散落在各个文件中，导致维护困难、重复代码。

### 干净的结构

```
xxx.go               # 源文件
xxx_test.go           # 对应测试

xxx_options.go        # Options 源文件
xxx_options_test.go   # 对应测试

test_helpers.go       # 共享测试工具（newXxxPair、mockXxx、skipXxx）
integration_test.go   # 总测：组合多个模块的集成场景
xxx_integration_test.go # 需外部依赖的集成测试（如 RabbitMQ）
```

### 案例：gows 包测试文件

| 源文件 | 测试文件 | 测试内容 |
|--------|---------|--------|
| `client.go` | `client_test.go` | Client 核心（NewClient/Close/Done/Hook） |
| `client_options.go` | `client_options_test.go` | Option 配置生效验证 |
| `health.go` | `health_test.go` | 健康状态/Stats/IsAlive |
| `readwrite.go` | `readwrite_test.go` | WriteJSON/ReadMessage/Benchmark |
| `errors.go` | `errors_test.go` | 错误类型判等 |
| `message.go` | `message_test.go` | Message JSON 序列化 |
| `dispatcher.go` | `dispatcher_test.go` | 分发器核心功能/并发安全 |
| `dispatcher_send.go` | `dispatcher_send_test.go` | 发送/广播/多设备 |
| `rabbitmq_backend.go` | `rabbitmq_backend_test.go` | 单元测试（mock 模拟） |
| | `rabbitmq_backend_integration_test.go` | 集成测试（需真实服务） |
| | `integration_test.go` | 总测：全生命周期链路 |
| `test_helpers.go` | 所有测试文件共享 | `newTestClientPair`/`mockBackend`/`skipNoRabbitMQ` |

### 规则

| 规则 | 说明 |
|------|------|
| **一对一映射** | 每个源文件 `xxx.go` 有且仅有一个测试文件 `xxx_test.go` |
| **命名对称** | Options 文件对应 `xxx_options_test.go`，集成测试后缀 `_integration_test.go` |
| **共享工具集中** | `test_helpers.go` 存放跨文件共享的工具函数、mock 实现、跳过条件 |
| **单元/集成分离** | 依赖外部服务的测试放 `_integration_test.go`，默认跳过（环境变量控制） |
| **总测覆盖链路** | `integration_test.go` 覆盖完整业务流程的多个模块组合场景 |
| **辅助方法首字母小写** | 测试辅助函数（`newTestPair`、`mockXxx`）首字母小写，限于包内使用 |
| **测试写完必须运行验证** | 新增或修改测试后，运行 `go test ./pkg/gows/... -count=1 -timeout 180s` 确认全部通过 |

### 反模式

```go
// ❌ 共享工具散落在各个文件中
// client_test.go 定义了 newTestClientPair
// dispatcher_demo_test.go 又定义了 newTestClientWithUID（重复）

// ✅ 统一放在 test_helpers.go
func newTestClientPair(t testing.TB, opts ...Option) (*Client, *websocket.Conn)
func newTestClientWithUID(t testing.TB, uid string, opts ...Option) (*Client, *websocket.Conn)
func newMockBackend(bufSize int) *mockBackend
```

```go
// ❌ 单元测试和集成测试混在一起
// rabbitmq_backend_test.go 既有构造函数测试又有真实 RabbitMQ 集成测试

// ✅ 分离
// rabbitmq_backend_test.go              — 构造函数、mock 测试
// rabbitmq_backend_integration_test.go   — 真实 RabbitMQ 测试（默认跳过）
```

---

## 模式六：无锁化并发控制

### 问题场景

高并发 WebSocket 场景下，Mutex/RWMutex 保护 map 或 time.Time 字段会成为性能瓶颈：

- `map[*Client]struct{} + sync.RWMutex`：读多写少的连接管理，RWMutex 在数百并发下仍有锁争用
- `time.Time + sync.RWMutex`：读写频繁的时间戳字段，每次操作都需加锁
- `map[string]chan struct{} + sync.Mutex`：UID 订阅表，高并发注册/注销时锁竞争

### 改造模式

#### 1. 连接集合 map + RWMutex → sync.Map + atomic.Int32

```go
// Before
mu      sync.RWMutex
clients map[*Client]struct{}

func (dd *DistributedDispatcher) Range(fn func(*Client) bool) {
    dd.mu.RLock()
    clients := make([]*Client, 0, len(dd.clients))
    for client := range dd.clients { clients = append(clients, client) }
    dd.mu.RUnlock()
    for _, client := range clients { if !fn(client) { break } }
}

// After
clients     sync.Map
clientCount atomic.Int32

func (dd *DistributedDispatcher) Range(fn func(*Client) bool) {
    var clients []*Client
    dd.clients.Range(func(key, _ any) bool {
        clients = append(clients, key.(*Client))
        return true
    })
    for _, client := range clients { if !fn(client) { break } }
}
```

替代清单：

| 原操作 | 替代 |
|--------|------|
| `dd.mu.RLock(); for c := range dd.clients {}; dd.mu.RUnlock()` | `dd.clients.Range(func(k, _ any) bool { ... })` |
| `dd.mu.Lock(); dd.clients[c] = struct{}{}; dd.mu.Unlock()` | `dd.clients.Store(c, struct{}{})` + `clientCount.Add(1)` |
| `dd.mu.Lock(); delete(dd.clients, c); dd.mu.Unlock()` | `dd.clients.Delete(c)` + `clientCount.Add(-1)` |
| `len(dd.clients)` | `clientCount.Load()` |

#### 2. 时间戳 + RWMutex → atomic.Int64 (UnixNano)

```go
// Before
type healthState struct {
    lastWriteTime time.Time
    lastReadTime  time.Time
    mu            sync.RWMutex
}

func (c *Client) markLastWrite() {
    c.health.mu.Lock()
    c.health.lastWriteTime = time.Now()
    c.health.mu.Unlock()
}

// After
type healthState struct {
    lastWriteTime atomic.Int64 // unix nano
    lastReadTime  atomic.Int64 // unix nano
}

func (c *Client) markLastWrite() {
    c.health.lastWriteTime.Store(time.Now().UnixNano())
}
```

#### 3. UID 订阅表 map[string]chan + Mutex → sync.Map

```go
// Before
uidSubs   map[string]chan struct{}
uidSubsMu sync.Mutex

// After
uidSubs sync.Map
```

### 关键陷阱：计数漂移保护

`sync.Map` 允许多路径并发操作同个 key。当 `UnregisterCtx`、`CleanupDeadConns`、delivery 清理三条路径可能并发删除同个 `*Client` 时，计数器必须受保护：

```go
// ❌ 错误：并发删除时无条件减一，计数漂移
dd.clients.Delete(client)
dd.clientCount.Add(-1)

// ✅ 正确：只有实际删除了才递减
if _, loaded := dd.clients.LoadAndDelete(client); loaded {
    dd.clientCount.Add(-1)
}
```

同样，ctx 回滚时也应条件执行：

```go
_, loaded := dd.clients.LoadAndDelete(client)
if loaded {
    dd.clientCount.Add(-1)
}
select {
case <-ctx.Done():
    if loaded {
        dd.clients.Store(client, struct{}{})
        dd.clientCount.Add(1)
    }
    return ctx.Err()
default:
}
```

### 规则

| 规则 | 说明 |
|------|------|
| **能不用锁就不用** | 优先用 `sync.Map`/`atomic.*`/chan 替代 Mutex/RWMutex |
| **LoadAndDelete 条件递减** | 并发删除同一 key 时，必须用 `LoadAndDelete` + `if loaded` 保护计数器 |
| **ctx 回滚也需条件** | 回滚时同样判断 `loaded`，避免无删除却回滚添加 |
| **RWMutex 保留场景** | 真正需要互斥语义的场景（如 RabbitMQ 连接初始化、消费者替换）仍用 Mutex |
| **soft limit 可接受** | `maxConns` 检查从精确 Mutex 变为两阶段 `Load`+`Store`，高并发下超限少量可接受 |
| **time.Time 用 UnixNano** | 原子化时间戳用 `atomic.Int64` 存纳秒，通过 `time.Unix(0, nano)`/`time.Now().UnixNano()` 转换 |

---

## 常见陷阱

| 陷阱 | 说明 | 解决 |
|------|------|------|
| **doc.go 包注释无用** | 包文档写了一大堆但没人看，还多了个文件要维护 | 可以安全删除，Go 不强制要求包注释，编译和工单检查均不报错 |
| **文件名与内容不匹配** | `conn.go` 实际只含心跳代码，留了个错误的文件名 | 文件重命名精确匹配职责：`conn.go` → `heartbeat.go`、`conn_options.go` → `heartbeat_options.go` |
| **非错误类型混入 errors.go** | `Message` 结构体、`requestIDAttr` 工具函数与错误类型放在一起 | 拆出 `message.go`，工具函数归入对应的主文件 |
| **基础属性查询放错文件** | `UID()` 放在 `health.go` 中，与健康监控无关 | `UID()` 归入 `client.go`，与 `RemoteAddr()`/`Context()` 等基础属性查询放一起 |
| **Options混入主文件** | Option 类型和 `With*` 函数与核心逻辑混在同一个 `.go` 文件中 | 提取到 `xxx_options.go`，与主文件 `xxx.go` 对称命名 |
| **缺少 defaultXxxOptions** | `NewClient`/`Upgrade` 入口中直接写 struct literal 初始化默认值，零值散落各处 | 添加 `defaultXxxOptions()` 函数集中管理，入口使用 `o := defaultXxxOptions(); o.apply(opts...)` |
| **Option 函数直接操作目标结构体** | `DispatcherOption func(*DistributedDispatcher)` 直接修改大结构体字段 | 引入独立的 `dispatcherOptions` 结构体，通过 `defaultDispatcherOptions` + `apply` 统一管理配置 |
| **同包函数名冲突** | 两个文件需要同名但不同类型的 Option 函数 | 包内使用 `withClient*` 小写前缀，导出使用 `With*` 大写 |
| **配置字段不对称** | 入口 Option 有字段但目标结构体没有，导致配置丢失 | 修改时对照检查清单逐个对齐 |
| **构造函数不接收 ctx** | 创建后通过 SetContext 修补，导致 ctxCancel 脱钩 | 构造函数第一个参数接收 `ctx`，内部用 `WithCancel` 派生 |
| **ctxCancel 不一致** | `c.ctx` 被替换但 `c.ctxCancel` 仍指向旧 context | `ctx` 和 `ctxCancel` 必须始终成对创建，禁止分开赋值 |
| **Ctx 变体重复实现** | 无参方法和 Ctx 变体各自实现一套逻辑 | 无参方法只做委托，Ctx 变体是唯一实现 |
| **request_id 遗漏** | Span 中不传 request_id，追踪链路断连 | 所有 Span 创建后立即调用 `requestIDAttr(ctx)` |
| **测试文件混杂** | 一个测试文件测试多个源文件，职责模糊 | 按源文件一对一首字母拆分：`xxx.go` → `xxx_test.go` |
| **共享测试工具重复** | `newTestClientPair` 等辅助函数在每个测试文件中重复定义 | 统一提取到 `test_helpers.go`，所有测试文件共享引用 |
| **单元/集成混在一起** | `rabbitmq_backend_test.go` 包含构造函数的单元测试和真实 RabbitMQ 集成测试 | 拆分：`xxx_test.go` 放单元测试，`xxx_integration_test.go` 放集成测试 |
| **缺少总测文件** | 只有单模块测试，没有组合全流程的集成测试 | 创建 `integration_test.go`，覆盖从 Upgrade 到关闭的完整生命周期 |
| **测试文件零对称** | 源文件拆分后但测试文件没跟着拆，仍然一个大文件 | 源文件和测试文件同步拆分，保持对称 |
| **close hook 闭包捕获自身** | `SetCloseHook(func() { if prev := c.onClose; prev != nil { prev() } })` 闭包中读取 `c.onClose` 在运行时返回自身，形成无限递归 | 在 `SetCloseHook` 调用前保存 `prevHook := c.onClose`，闭包中使用局部变量而非运行时读取实例字段 |
| **sync.Map 类型断言未处理 ok** | `counter := actual.(*atomic.Int32)` 被 errcheck 标记 | 用 `counter, ok := actual.(*atomic.Int32); if !ok { return }` 显式处理 |
| **全局变量用 mutex 保护** | 全局限流器用 `*rate.Limiter + sync.Mutex`，读写需临时变量 | 用 `atomic.Pointer[rate.Limiter]`，`Store`/`Load` 无锁 |
| **纯字符串错误定义自定义类型** | `ErrUpgradeRateLimited` 类型从未被 `errors.Is`/`errors.As` 判断 | 直接 `fmt.Errorf(...)` 即可 |
| **IP 计数器增量未条件判断** | `upgradePerIPIncrement` 无条件调用，即使 `maxConnPerIP=0` | 和 close 钩子一样放在 `if o.maxConnPerIP > 0` 内 |
| **gocognit 超限** | `Upgrade` 函数认知复杂度 47 > 40，大量内联的 if/for 块导致复杂度过高 | 提取子函数：安全检查/配置传播/资源注册各提取为一个命名函数，主函数只保留线性调用序列 |
| **context.Context 参数位置** | `revive` 报 `context-as-argument`，因为 `ctx context.Context` 不是函数的第一参数 | 将 `ctx` 移到函数签名的第一个位置，这是 Go 社区惯例 |
| **YAML 更新后未 re-gen** | YAML 新增字段后未运行 `make update-config` | 修改 YAML → `make update-config` → 编译验证 |
| **Span 在无参方法中创建** | 无参方法也创建 Span，导致重复 | 只在 Ctx 变体中创建 Span，无参方法委托时不重复创建 |
| **新增配置遗漏传播链路** | YAML 加了字段、Go struct 也生成了，但 `Upgrade()` 中没传播到 `Client` | 按检查清单走 7 步：YAML → make → upgradeOptions → Upgrade传播 → Client → 模板 → 编译 |
| **YAML 扁平不分组** | 所有配置项平铺在 `websocket:` 下，随着字段增多难以管理 | 按功能分为 6 组子键（CORS/Limit/Timeout/Heartbeat/Queue/Distributed），`make update-config` 自动生成嵌套结构体 |
| **配置访问路径写死** | 模板中硬编码 `config.Get().Websocket.RateLimitRps`，分组变动后散落各处难以修改 | 模板中的路径映射集中在 `upgradeOpts` 注释表中方便对照，分组调整时只改注释表和配置读取代码 |
| **Option 函数未设默认值保护** | `With*` 函数直接赋值，零值未被正确处理 | option 内部用 `> 0` 判断，零值表示"使用默认值"，不覆盖已有默认值
| **裸 int32/int64 + 包函数** | 使用 `atomic.LoadInt32(&c.closed)` 等包函数风格，需取地址且易误传副本 | 改为 `atomic.Int32`/`atomic.Int64` 类型字段 + 方法调用（`c.closed.Load()`），无需取地址，IDE 自动补全友好 |
| **计数器并发递减** | 多路径并发删除同个 client 时，无条件 `Delete` + `Add(-1)` 导致计数漂移 | 用 `LoadAndDelete` + `if loaded` 条件递减 |
