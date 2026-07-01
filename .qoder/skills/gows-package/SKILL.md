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
| `client_options.go` | `ClientOption`/`clientOptions`、`defaultClientOptions`+`apply`、`withClient*` 系列 | ~105 |
| `client.go` | `Client` 结构体、`NewClient`、`Close`/`closeWsConn`、`UID`、`RemoteAddr`/`Context`/`Done`（返回 `clientCtx.Done()`）、`requestIDAttr` | ~170 |
| `health.go` | `healthState`、`IsAlive`、`ClientStats`/`Stats`、读写错误记录 | ~160 |
| `client_readwrite.go` | `msgFromWsToCh`/`msgFromChToWs`（含 drain）/`writeWithRetry`、`checkWriteLimit` | ~160 |
| `client_local.go` | `WriteJSONCtx`/`WriteRawCtx`/`ReadMessageCtx`（含 `<-clientCtx.Done()` 兜底） | ~150 |
| `heartbeat.go` | `SetupPongHandler`、`StartHeartbeat` | ~132 |
| `heartbeat_options.go` | `HeartbeatOption`/`heartbeatOptions`、`defaultHeartbeatOptions`+`apply`、`WithHeartbeatInterval`、`WithPongTimeout`、`WithPingWriteWait` | ~63 |
| `auth.go` | `ParseTokenCtx`、`extractUID` | ~88 |
| `upgrader.go` | `Upgrade` 函数、CORS/限流/IP 检查 | ~235 |
| `upgrade_options.go` | `upgradeOptions`/`UpgradeOption`、`defaultUpgradeOptions`+`apply`、所有 `With*` (UpgradeOption) | ~280 |
| `dispatcher.go` | `DistributedDispatcher` 结构体（含 `uidIndex`/`remoteUIDCount`）、`NewDispatcher`/`Start`/`Stop`、查询方法 | ~250 |
| `dispatcher_options.go` | `dispatcherOptions`/`DispatcherOption`、`defaultDispatcherOptions`+`apply`、`WithMaxConnections`、`WithWorkerPool`、`ErrMaxConnections` | ~60 |
| `dispatcher_lifecycle.go` | `RegisterCtx`/`UnregisterCtx`、`publishClientEvent`、`subscribeUID`/`unsubscribeUID` | ~180 |
| `dispatcher_receive.go` | `receiveLoop`/`dispatchMessage`/`deliverMessage`、`handleRemoteOnline`/`Offline`（`*atomic.Int32` 简化 CAS）、`deliverBroadcast`/`deliverToUIDs`（Range 内联写入） | ~230 |
| `dispatcher_send.go` | `SendToUIDCtx`/`SendToMultiUIDCtx`、`BroadcastCtx`/`BroadcastFilterCtx` | ~160 |
| `client_distributed.go` | `SendToUIDCtx`/`SendToMultiUIDCtx`/`BroadcastCtx`/`BroadcastReliableCtx`（Client 代理 Dispatcher） | ~61 |
| `backend.go` | `Backend` 接口、`PubSubMessage` 结构体 | ~35 |
| `rabbitmq_backend.go` | `RabbitMQBackend` 实现（Publish/Receive/Close） | ~495 |
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
- `clientOptions`（client_options.go）—— 客户端级别配置
- `upgradeOptions`（upgrade_options.go）—— Upgrade 入口配置
- `dispatcherOptions`（dispatcher_options.go）—— 分发器配置
- `heartbeatOptions`（heartbeat_options.go）—— 心跳配置

### gows 特有：Upgrade → Client 配置统一传递

``go
// Upgrade() 中通过 buildClientOpts 将配置转为 ClientOption 列表，统一传入 NewClient
client := NewClient(ctx, rawConn, uid, buildClientOpts(o)...)

// buildClientOpts 将 upgradeOptions 中的 Client 字段批量转为 ClientOption
func buildClientOpts(o *upgradeOptions) []ClientOption {
    var opts []ClientOption
    if o.writeChSize > 0  { opts = append(opts, withWriteChSize(o.writeChSize)) }
    if o.readTimeout > 0  { opts = append(opts, withClientReadTimeout(o.readTimeout)) }
    if o.writeTimeout > 0 { opts = append(opts, withClientWriteTimeout(o.writeTimeout)) }
    if o.readLimit > 0    { opts = append(opts, withClientReadLimit(o.readLimit)) }
    if o.writeLimit > 0   { opts = append(opts, withClientWriteLimit(o.writeLimit)) }
    return opts
}
```

Benefits over direct field assignment:
- NewClient 创建 channel 时直接使用正确容量
- 配置传播链路清晰，所有 Client 配置统一经过 option 系统
- 新增配置只需升级 buildClientOpts，无需修改 Upgrade 主函数

---

## 模式三：配置结构对称性

### 问题场景

多个配置结构体（`clientOptions`、`Client`、`upgradeOptions`）描述同一组配置时，字段缺失导致配置永远无法传播到目标对象。

### 检查清单

修改配置相关字段时，检查以下结构体是否对称：

``go
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
| 3 | `gows/upgrade_options.go` | `upgradeOptions` 结构体添加字段 + `defaultUpgradeOptions()` 设默认值 |
| 4 | `gows/upgrader.go` | `Upgrade()` 中添加字段传播到 `Client`（使用 `> 0` 判断） |
| 5 | `gows/client.go` | `Client` 结构体添加字段（存储传播过来的值） |
| 6 | `cmd/protoc-gen-go-gin/.../template.go` | 模板中读取 `config.Get().Websocket.Group.Field` 并调用对应 `With*` |
| 7 | 编译验证 | `go build ./pkg/gows/...` + `./cmd/protoc-gen-go-gin/...` |

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
| **配置字段不对称** | 入口 Option 有字段但 Client 结构体没有，导致配置丢失 | 修改时对照检查清单逐个对齐 |
| **IP 计数器增量未条件判断** | `upgradePerIPIncrement` 无条件调用，即使 `maxConnPerIP=0` | 和 close 钩子一样放在 `if o.maxConnPerIP > 0` 内 |
| **新增配置遗漏传播链路** | YAML 加了字段但 `Upgrade()` 中没传播到 `Client` | 按检查清单走 7 步：YAML → make → option → Upgrade → Client → 模板 → 编译 |
| **YAML 扁平不分组** | 所有配置项平铺在 `websocket:` 下难以管理 | 按功能分为 6 组子键（CORS/Limit/Timeout/Heartbeat/Queue/Distributed） |
| **配置访问路径写死** | 模板中硬编码 `config.Get().Websocket.RateLimitRps` | 模板中的路径映射集中在 `upgradeOpts` 注释表中，分组调整时只改注释表 |
| **close hook 闭包捕获自身** | `SetCloseHook` 闭包读取 `c.onClose` 在运行时返回自身 | `SetCloseHook` 前保存 `prevHook := c.onClose`，闭包使用局部变量 |
| **sync.Map 类型断言必须 comma-ok** | `key.(*Client)` / `value.(chan struct{})` 被 errcheck 标记 | 参考 **project-conventions 一→类型断言规范** |
| **裸 int32/int64 + 包函数** | `atomic.LoadInt32(&c.closed)` 需取地址且易误传副本 | 改为 `atomic.Int32`/`atomic.Int64` 类型 + 方法调用 |
| **deliverToUIDs nil span** | `deliverToUIDs` 从 `subscribeUID` 调用时传入 `nil`，直接 `span.SetStatus` 会 panic | 调用 `span` 前加 `if span != nil` 保护 |
| **计数器并发递减** | 多路径并发删除同个 client，无条件 `Delete` + `Add(-1)` 导致计数漂移 | 用 `LoadAndDelete` + `if loaded` 条件递减，参考 **project-conventions 十三** |
