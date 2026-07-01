---
name: gows-package
description: Guides modification and extension of the gows WebSocket package, including OpenTelemetry tracing integration, request_id propagation, and code generation templates. Use when working with gows/, protoc-gen-go-gin templates, or WebSocket-related code.
---

# gows 包开发指南

本文档记录 gows WebSocket 包特有的设计决策、文件结构和实现细节。
**通用 Go 开发规范请参考** `project-conventions` SKILL.md。

## 文件结构参考 (`pkg/gows/`)

按职责单一原则拆分后的文件布局（注意命名对称性：主文件 `xxx.go` + Options `xxx_options.go`）：

| 文件 | 职责 | 行数 |
|------|------|------|
| ~~`doc.go`~~ | ~~包文档和示例（已删除，包注释非必需）~~ |
| `errors.go` | 错误类型 | ~10 |
| `message.go` | `Message` 通用消息结构体 | ~11 |
| `client_options.go` | `ClientOption`/`clientConfig`、`defaultClientConfig`+`applyClientOptions`（含 `client_config.go` 合并内容） | ~40 |
| `client.go` | `Client` 结构体（嵌入 `clientConfig`）、`NewClient`/`newClientWithConfig`、`Close`/`closeWsConn`、`UID`、`RemoteAddr`/`Context`/`Done`（返回 `clientCtx.Done()`）、`requestIDAttr` | ~183 |
| `health.go` | `healthState`、`IsAlive`、`ClientStats`/`Stats`、读写错误记录 | ~145 |
| `client_readwrite.go` | `msgFromWsToCh`/`msgFromChToWs`（含 drain）/`writeWithRetry`、`checkWriteLimit` | ~172 |
| `client_local.go` | `WriteJSONCtx`/`WriteRawCtx`/`ReadMessageCtx`（含 `<-clientCtx.Done()` 兜底） | ~145 |
| `heartbeat.go` | `SetupPongHandler`、`StartHeartbeat` | ~132 |
| `heartbeat_options.go` | `HeartbeatOption`/`heartbeatOptions`、`defaultHeartbeatOptions`+`apply`、`WithHeartbeatInterval`、`WithPongTimeout`、`WithPingWriteWait` | ~62 |
| `auth.go` | `ParseTokenCtx`、`extractUID` | ~86 |
| `upgrader.go` | `Upgrade` 函数、CORS/限流/IP 检查、`buildClientOpts`（直接返回嵌入 `clientConfig`） | ~230 |
| `upgrade_options.go` | `upgradeOptions`（嵌入 `clientConfig`）/`UpgradeOption`、`defaultUpgradeOptions`+`apply`、所有 `With*` (UpgradeOption) | ~276 |
| `dispatcher.go` | `DistributedDispatcher` 结构体（含 `uidIndex`/`remoteUIDCount`）、`NewDispatcher`/`Start`/`Stop`、查询方法 | ~250 |
| `dispatcher_options.go` | `dispatcherOptions`/`DispatcherOption`、`defaultDispatcherOptions`+`apply`、`WithMaxConnections`、`WithWorkerPool`、`ErrMaxConnections` | ~53 |
| `dispatcher_lifecycle.go` | `RegisterCtx`/`UnregisterCtx`、`publishClientEvent`、`subscribeUID`/`unsubscribeUID` | ~202 |
| `dispatcher_receive.go` | `receiveLoop`/`dispatchMessage`/`deliverMessage`、`handleRemoteOnline`/`Offline`（`*atomic.Int32` 简化 CAS）、`deliverBroadcast`/`deliverToUIDs`（Range 内联写入） | ~252 |
| `dispatcher_send.go` | `SendToUIDCtx`/`SendToMultiUIDCtx`、`BroadcastCtx`/`BroadcastFilterCtx` | ~204 |
| `client_distributed.go` | `SendToUIDCtx`/`SendToMultiUIDCtx`/`BroadcastCtx`/`BroadcastReliableCtx`（Client 代理 Dispatcher） | ~60 |
| `backend.go` | `Backend` 接口、`PubSubMessage` 结构体 | ~56 |
| `rabbitmq_backend.go` | `RabbitMQBackend` 实现（Publish/Receive/Close） | ~489 |
| `test_helpers.go` | 共享测试工具：`newTestClientPair`/`newTestClientWithUID`/`newTestServer`/`mockBackend` | ~200 |
| `integration_test.go` | 总测：全生命周期 + 分布式消息 + 并发 Stats 一致性 | ~306 |
| `rabbitmq_backend_integration_test.go` | RabbitMQ 集成测试（需 `RABBITMQ_INTEGRATION=true`） | ~148 |

**如何定位代码**：一个逻辑变更通常只涉及上述 1~2 个文件。例如修改 Upgrade 流程看 `upgrader.go` + `upgrade_options.go`，修改消息发送看 `dispatcher_send.go`。

**测试文件命名对称性**：每个源文件 `xxx.go` 对应一个 `xxx_test.go`（测试文件），Options 文件对应 `xxx_options_test.go`。共享测试工具统一放在 `test_helpers.go`。

---

## 模式一：构造函数接收 Context

通用规则请参考 **project-conventions → 十四 → 构造函数接收 Context**。
gows 的实践：`NewClient(ctx, conn, uid, opts...)` 使用 `context.WithoutCancel` 阻断上游 Gin 超时传递，`clientCtx.Done()` 仅在显式 `Close()` 时触发。

---

## 模式二：Option 配置传播

通用模式请参考 **project-conventions → 十五、Option 配置传播模式**。

gows 包内具体实现：
- `clientConfig`（client_options.go）—— 嵌入 `upgradeOptions` 和 `Client`，消除三处字段冗余
- `upgradeOptions`（upgrade_options.go）—— 嵌入 `clientConfig`，Upgrade 入口配置直接传入 Client
- `dispatcherOptions`（dispatcher_options.go）—— 分发器配置
- `heartbeatOptions`（heartbeat_options.go）—— 心跳配置

### gows 特有：共享配置嵌入（clientConfig）

重构前，`upgradeOptions`、`clientOptions`、`Client` 三个结构体重复声明 7 个相同字段，
通过 `buildClientOpts` + 7 个 `withClient*` 辅助函数逐字段打包再解包。

重构后，提取 `clientConfig` 共享结构体，嵌入到 `upgradeOptions` 和 `Client`：

```go
// client_options.go — 共享配置
 type clientConfig struct {
     writeChSize  int
     readChSize   int
     readTimeout  time.Duration
     writeTimeout time.Duration
     readLimit    int64
     writeLimit   int64
     dispatcher   *DistributedDispatcher
 }

// upgrade_options.go — 嵌入配置，UpgradeOption 配置直接传递
 type upgradeOptions struct {
     clientConfig           // ← 嵌入，字段自动提升
     enableHeart bool
     heartbeatOpts []HeartbeatOption
     // ... 其他 Upgrade 独有字段
 }

// client.go — Client 同样嵌入，构造函数一行赋值
 type Client struct {
     clientConfig           // ← 嵌入，替代 7 个独立字段
     wsConn  *websocket.Conn
     uid     string
     writeCh chan []byte
     readCh  chan []byte
     // ...
 }

// upgrader.go — buildClientOpts 直接从嵌入配置返回
 func buildClientOpts(o *upgradeOptions) *clientConfig {
     return &o.clientConfig
 }

// client.go — 构造函数接收 *clientConfig，嵌入赋值
 func newClientWithConfig(ctx context.Context, conn *websocket.Conn, uid string, cfg *clientConfig) *Client {
     c := &Client{
         clientConfig: *cfg,  // ← 一行替代 6 行手动拷贝
         wsConn:  conn,
         uid:     uid,
         writeCh: make(chan []byte, cfg.writeChSize),
         readCh:  make(chan []byte, cfg.readChSize),
     }
     // ...
 }
```

Benefits:
- 消除 7 个 `withClient*` 辅助函数（文件从 ~105 行减至 ~40 行）
- `buildClientOpts` 从 25 行减至 4 行
- Client 结构体字段数减少 ~30%
- 新增配置只需改 `clientConfig` 一处，嵌入后自动传播

---

## 模式三：Embed 驱动的配置对称性

### 优化后方案

通过 `clientConfig` 嵌入 `upgradeOptions` 和 `Client`，**自动保证字段对称**，无需手动对齐：

```go
// client_options.go — 唯一配置定义
 type clientConfig struct {
     readTimeout  time.Duration
     writeTimeout time.Duration
     readLimit    int64
     writeLimit   int64
 }

// upgrade_options.go — 嵌入 upgradeOptions，自动拥有所有 clientConfig 字段
 type upgradeOptions struct {
     clientConfig
     // Upgrade 独有字段...
 }

// client.go — 嵌入 Client，同样自动拥有
 type Client struct {
     clientConfig
     // Client 独有字段...
 }

// upgrader.go — 传播只需一行
 func buildClientOpts(o *upgradeOptions) *clientConfig {
     return &o.clientConfig
 }

// client.go — 构造函数也只需一行
 func newClientWithConfig(ctx context.Context, conn *websocket.Conn, uid string, cfg *clientConfig) *Client {
     c := &Client{
         clientConfig: *cfg,
         // ...
     }
 }
```

**优势**：新增配置只需改 `clientConfig` 一处，嵌入后自动传播到所有结构体，
从"人工检查 3 个地方是否对称"变成"编译器保证对称"。

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

```
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

``go
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
| 3 | `gows/client_options.go` | `clientConfig` 结构体添加字段 + `defaultClientConfig()` 设默认值 |
| 4 | `gows/upgrade_options.go` | 如果新增的是 Upgrade 独有字段，在 `upgradeOptions` 添加；如果属于通用客户端配置，嵌入 `clientConfig` 自动获得 |
| 5 | `cmd/protoc-gen-go-gin/.../template.go` | 模板中读取 `config.Get().Websocket.Group.Field` 并调用对应 `With*` |
| 6 | 编译验证 | `go build ./pkg/gows/...` + `./cmd/protoc-gen-go-gin/...` |

**注意**：嵌入 `clientConfig` 后，Client 结构体和构造函数无需手动修改，
新增的通用配置字段自动传播到位，检查清单从 7 步缩短为 6 步。

---

## 模式四：请求上下文（request_id）传播

参考专用 SKILL：**request-id-propagation**。
gows 中的实现位于 `client.go` 的 `requestIDAttr` 辅助函数。

---

## 模式五：测试文件组织

通用规则请参考 **project-conventions → 十六、测试文件组织**。

### gows 包测试文件映射

| 源文件 | 测试文件 | 测试内容 |
|--------|---------|--------|
| `client.go` | `client_test.go` | Client 核心（NewClient/Close/Done/Hook） |
| `client_options.go` | `client_options_test.go` | Option 配置生效验证 |
| `health.go` | `health_test.go` | 健康状态/Stats/IsAlive |
| `client_readwrite.go` | `client_readwrite_test.go` | msgFromWsToCh/msgFromChToWs/writeWithRetry 写入重试 |
| `client_local.go` | `client_local_test.go` | WriteJSONCtx/WriteRawCtx/ReadMessageCtx 本地读写 |
| `errors.go` | `errors_test.go` | 错误类型判等 |
| `message.go` | `message_test.go` | Message JSON 序列化 |
| `dispatcher.go` | `dispatcher_test.go` | 分发器核心功能/并发安全 |
| `dispatcher_send.go` | `dispatcher_send_test.go` | 发送/广播/多设备 |
| `rabbitmq_backend.go` | `rabbitmq_backend_test.go` | 单元测试（mock 模拟） |
| | `rabbitmq_backend_integration_test.go` | 集成测试（需真实服务） |
| | `integration_test.go` | 总测：全生命周期链路 |
| `test_helpers.go` | 所有测试文件共享 | `newTestClientPair`/`mockBackend`/`skipNoRabbitMQ` |

---

## 模式六：无锁化并发控制

通用规则请参考 **project-conventions → 十二（atomic.Int）和 十三（sync.Map）**。
以下为 gows 包中具体的无锁化改造模式。

### 问题场景

高并发 WebSocket 场景下，Mutex/RWMutex 保护 map 或 time.Time 字段会成为性能瓶颈：

- `map[*Client]struct{} + sync.RWMutex`：读多写少的连接管理，RWMutex 在数百并发下仍有锁争用
- `time.Time + sync.RWMutex`：读写频繁的时间戳字段，每次操作都需加锁
- `map[string]chan struct{} + sync.Mutex`：UID 订阅表，高并发注册/注销时锁竞争

### 改造模式

#### 1. 连接集合 map + RWMutex → sync.Map + atomic.Int32

``go
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
        if c, ok := key.(*Client); ok {
            clients = append(clients, c)
        }
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

``go
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

``go
// Before
uidSubs   map[string]chan struct{}
uidSubsMu sync.Mutex

// After
uidSubs sync.Map
```

### 关键陷阱：计数漂移保护

`sync.Map` 允许多路径并发操作同个 key。当 `UnregisterCtx`、`CleanupDeadConns`、delivery 清理三条路径可能并发删除同个 `*Client` 时，计数器必须受保护：

``go
// ❌ 错误：并发删除时无条件减一，计数漂移
dd.clients.Delete(client)
dd.clientCount.Add(-1)

// ✅ 正确：只有实际删除了才递减
if _, loaded := dd.clients.LoadAndDelete(client); loaded {
    dd.clientCount.Add(-1)
}
```

同样，ctx 回滚时也应条件执行：

``go
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
| **doc.go 包注释无用** | 包文档写了一大堆但没人看，还多了个文件要维护 | 可以安全删除，Go 不强制要求包注释 |
| **文件名与内容不匹配** | `conn.go` 实际只含心跳代码，留了个错误的文件名 | 文件重命名精确匹配职责：`conn.go` → `heartbeat.go` |
| **非错误类型混入 errors.go** | `Message` 结构体、`requestIDAttr` 工具函数与错误类型放在一起 | 拆出 `message.go`，工具函数归入对应的主文件 |
| **基础属性查询放错文件** | `UID()` 放在 `health.go` 中，与健康监控无关 | `UID()` 归入 `client.go`，与 `RemoteAddr()`/`Context()` 等放一起 |
| **Option 函数直接操作目标结构体** | `DispatcherOption func(*DistributedDispatcher)` 直接修改大结构体字段 | 引入独立的 `dispatcherOptions` 结构体，通过 `defaultDispatcherOptions` + `apply` 管理 |
| **配置字段不对称** | 入口 Option 有字段但 Client 结构体没有，导致配置丢失 | 提取 `clientConfig` 嵌入两方，编译器保证对称，消除人工对齐 |
| **IP 计数器增量未条件判断** | `upgradePerIPIncrement` 无条件调用，即使 `maxConnPerIP=0` | 和 close 钩子一样放在 `if o.maxConnPerIP > 0` 内 |
| **新增配置遗漏传播链路** | YAML 加了字段但 `Upgrade()` 中没传播到 `Client` | 提取 `clientConfig` 嵌入后，新增通用配置只需改 `clientConfig` 一处，自动传播到 `Client`，不走 `Upgrade()` 传播；检查清单从 7 步减至 6 步 |
| **YAML 扁平不分组** | 所有配置项平铺在 `websocket:` 下难以管理 | 按功能分为 6 组子键（CORS/Limit/Timeout/Heartbeat/Queue/Distributed） |
| **配置访问路径写死** | 模板中硬编码 `config.Get().Websocket.RateLimitRps` | 模板中的路径映射集中在 `upgradeOpts` 注释表中，分组调整时只改注释表 |
| **close hook 闭包捕获自身** | `SetCloseHook` 闭包读取 `c.onClose` 在运行时返回自身 | `SetCloseHook` 前保存 `prevHook := c.onClose`，闭包使用局部变量 |
| **sync.Map 类型断言必须 comma-ok** | `key.(*Client)` / `value.(chan struct{})` 被 errcheck 标记 | 参考 **project-conventions 一→类型断言规范** |
| **裸 int32/int64 + 包函数** | `atomic.LoadInt32(&c.closed)` 需取地址且易误传副本 | 改为 `atomic.Int32`/`atomic.Int64` 类型 + 方法调用 |
| **Option 中间层冗余** | `upgradeOptions`→`clientOptions`→`Client` 三段结构体重复 7 个字段，每段都需 `with*` 辅助函数逐字段拷贝 | 提取 `clientConfig` 嵌入到 `upgradeOptions` 和 `Client`，删除 `clientOptions` 和 7 个 `with*` 函数；`buildClientOpts` 从 25 行降为 4 行；`client_options.go` 从 ~105 行减至 ~40 行 |
| **deliverToUIDs nil span** | `deliverToUIDs` 从 `subscribeUID` 调用时传入 `nil`，直接 `span.SetStatus` 会 panic | 调用 `span` 前加 `if span != nil` 保护 |
| **计数器并发递减** | 多路径并发删除同个 client，无条件 `Delete` + `Add(-1)` 导致计数漂移 | 用 `LoadAndDelete` + `if loaded` 条件递减，参考 **project-conventions 十三** |
