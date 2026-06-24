---
name: gows-package
description: Guides modification and extension of the gows WebSocket package, including OpenTelemetry tracing integration, request_id propagation, and code generation templates. Use when working with gows/, protoc-gen-go-gin templates, or WebSocket-related code.
---

# gows 包开发指南

## 包架构

```
pkg/gows/
├── upgrader.go                 # HTTP → WebSocket 升级入口（CORS/限流/单IP限制）
├── client.go                   # 客户端连接封装（Channel驱动写入模型 + 读超时）
├── conn.go                     # Ping/Pong 协议级心跳保活
├── dispatcher_distributed.go   # 全局连接注册中心 + 分布式消息分发（WorkerPool）
├── backend.go                  # Backend 抽象接口（单机/分布式模式）
├── rabbitmq_backend.go         # RabbitMQ Fanout 后端实现（Producer缓存池）
├── auth.go                     # JWT token 解析
└── upgrader_test.go            # 完整的安全/限流/CORS集成测试
```

## 核心架构原则

### 1. 安全防御（P0）

| 防御层 | 默认行为 | 配置方式 | 检查点 |
|--------|---------|---------|--------|
| **CORS** | 拒绝所有来源 | `WithCheckOrigin(fn)` | gorilla.Upgrade 之前 |
| **全局速率** | 不限制 | `WithRateLimit(rps, burst)` | gorilla.Upgrade 之前 |
| **单IP限制** | 不限制 | `WithMaxConnPerIP(n)` | gorilla.Upgrade 之前 + 成功后递增 |
| **连接上限** | 不限制 | `WithMaxConnections(n)` | Dispatcher.Register |

**关键经验**: CORS 检查、速率检查、IP 限制都在 `gorilla.Upgrade()` 之前执行，防止恶意请求到达协议层。单 IP 计数器在升级成功后递增，连接关闭时通过 Close 钩子链递减。

### 2. 连接生命周期

```
Upgrade → BeforeHook → CORS/限流/IP检查 → gorilla.Upgrade → IP计数+1 →
  → NewClient(writeLoop启动) → 设置CloseHook(IP清理) → AfterHook →
  → Heartbeat启动 → Dispatcher.Register → 返回Client

Close → CloseHook链(IP计数-1 → Dispatcher注销) → cancel ctx →
  → close writeCh → wg.Wait(writeLoop退出) → forceCloseConn
```

**关键经验**:
- `Close()` 使用 CAS + closeOnce 保证幂等
- `forceCloseConn()` 与 `Close()` 分离，避免 writeLoop 在 close writeCh 时自锁
- Close 钩子链要求: 先入后出（LIFO），后注册的钩子在链首执行

### 3. 分布式架构

```
实例A (alice)          Backend (RabbitMQ)         实例B (bob)
  │                                                     │
  ├─ SendToUIDCtx("bob")                                 │
  │   └─ Publish ───────────────────────────────────► ├─ receiveLoop
  │                                                      └─ dispatchMessage
  │                                                           ├─ workerLoop(0) → client.WriteJSON
  │                                                           └─ workerLoop(1) → client.WriteJSON
```

**Backend 接口**: `Backend` 接口抽象了消息中间件，当前实现:
- `RabbitMQBackend`: Fanout 交换机 + Producer 缓存池（默认 4 个 Producer）
- 可扩展: Redis Pub/Sub、Kafka 等

**关键经验**:
- 自发布消息跳过: `receiveLoop` 中通过 `InstanceID` 比对，跳过本实例发布的消息
- WorkerPool 背压保护: 默认 4 个 worker 协程 + 缓冲队列（workerNum×2），满时降级串行处理
- `dispatchMessage`: 先尝试异步投递到 workerCh，失败则同步串行（永不阻塞 receiveLoop）

### 4. 并发安全模型

| 同步机制 | 使用场景 |
|---------|---------|
| `chan` | writeCh（数据写入）、closeCh（关闭通知）、workerCh（背压分发）、producerPool（RabbitMQ Channel 复用） |
| `sync.Mutex` | clients map 读写、RabbitMQ 连接/消费者状态、全局限流器初始化 |
| `sync/atomic` | started/closed 状态、maxConns、workerNum、计数统计 |
| `sync.Once` | closeOnce（关闭幂等）、poolOnce（Producer 池惰性初始化） |
| `sync.WaitGroup` | writeLoop、receiveLoop、workerLoop、consumer goroutine 等待退出 |

## 安全策略

### CORS 默认拒绝

```go
var defaultCheckOrigin = func(_ *http.Request) bool { return false }
```

- 默认拒绝所有来源，必须显式调用 `WithCheckOrigin`
- 首次使用默认策略时打印安全警告
- `checkOriginSet` 标记区分默认值与用户显式设置（避免指针比较陷阱）

### 限流配置

```go
// 全局升级速率：每秒最多 100 次握手，突发最多 20
ws.Upgrade(c,
    ws.WithCheckOrigin(allowedOrigin),
    ws.WithRateLimit(100, 20),
    ws.WithMaxConnPerIP(10),
)
```

**警告**: `WithRateLimit` 操作的是**全局限流器**，每次调用都会重置。\
应在**初始化时调用一次**，不要在请求处理中重复调用。

## 连接管理经验

### 写入模型

- `WriteJSON` 将消息序列化后写入 `writeCh`（阻塞写缓冲区），永不阻塞调用方
- `writeLoop` 后台协程消费 writeCh，失败时指数退避重试（100ms→200ms→400ms）
- 退避公式: `baseDelay × 2^(attempt-1) + rand(0, baseDelay)`（jitter 防惊群）
- 写满时返回 `ErrWriteQueueFull`，由业务层决定是否丢弃

### 读取超时

- `WithReadTimeout(duration)`: 独立于心跳的主动读超时
- 在 `ReadMessage` 每次读取前设置 `SetReadDeadline`
- **建议**: 启用心跳时可不设置（心跳已管理 ReadDeadline），未启用心跳时必须设置防止永久阻塞

### 连接关闭

```go
// Close 关闭顺序:
// 1. CloseHook链(IP计数器递减 → Dispatcher注销)
// 2. 取消 ctx
// 3. 关闭 closeCh（通知 writeLoop 等协程）
// 4. 关闭 writeCh
// 5. wg.Wait（等待 writeLoop 退出）
// 6. SetReadDeadline + forceCloseConn
```

## 可观测性

### Span 命名规范

| 操作 | Span 名称 | SpanKind | 位置 |
|------|-----------|----------|------|
| HTTP→WS 升级 | `ws.upgrade` | SpanKindServer | upgrader.go |
| 写入消息 | `ws.write` | SpanKindInternal | client.go |
| 读取消息 | `ws.read` | SpanKindInternal | client.go |
| 心跳保活 | `ws.heartbeat` | SpanKindInternal | conn.go |
| 广播消息 | `ws.broadcast` | SpanKindInternal | dispatcher_distributed.go |
| 条件广播 | `ws.broadcast_filter` | SpanKindInternal | dispatcher_distributed.go |
| 发送给指定用户 | `ws.send_to_uid` | SpanKindInternal | dispatcher_distributed.go |
| 解析JWT | `ws.parse_token` | SpanKindInternal | auth.go |
| 分布式接收 | `ws.distributed.receive` | SpanKindInternal | dispatcher_distributed.go |

### 结构化日志

所有关键操作记录结构化日志（`logger.InfoWithCtx` / `logger.WarnWithCtx`）：
- 连接建立/断开: uid, remote_addr, dispatcher_conn_count
- 心跳异常: uid, error
- 写入失败: uid, attempt, error, write_queue_len
- 连接被拒绝: uid, remote_addr, reason(CORS/限流/满连接)
- 分布式消息投递失败: uid, target_instance_id

## 测试注意事项

### Upgrade 测试必须使用真实 WebSocket 拨号

`gorilla.Upgrade()` 需要 HTTP 请求携带 WebSocket 头（Connection: Upgrade, Upgrade: websocket, Sec-WebSocket-Key, Sec-WebSocket-Version）。`httptest.NewRequest()` 不会自动添加这些头。

**正确做法**:
```go
s := httptest.NewServer(r)
defer s.Close()
url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
conn, _, err := websocket.DefaultDialer.Dial(url, nil)
```

**例外**: CORS 拒绝、BeforeHook 中止等发生在 gorilla.Upgrade 之前的检查，可以直接用 httptest.NewRecorder 测试。

### 全局限流器测试

`WithRateLimit` 设置的是**全局共享**的 `rate.Limiter`，测试时:
1. 提前设置全局 limiter（不通过 WithRateLimit）
2. 使用匿名 UpgradeOption 仅开启 enableRateLimit 标志
3. 测试结束后恢复全局状态

```go
upgradeLimiterMu.Lock()
upgradeLimiter = rate.NewLimiter(1, 1) // 1 rps, burst=1
upgradeLimiterMu.Unlock()
defer func() {
    upgradeLimiterMu.Lock()
    upgradeLimiter = nil
    upgradeLimiterMu.Unlock()
}()

// handler 中:
Upgrade(c, WithCheckOrigin(fn), func(o *upgradeOptions) { o.enableRateLimit = true })
```

### 单 IP 限制测试

需要保持连接打开以累积 IP 计数，通过 channel 阻塞 handler：
```go
blockCh := make(chan struct{})
r.GET("/ws", func(c *gin.Context) {
    Upgrade(c, ..., WithMaxConnPerIP(2))
    <-blockCh  // 保持连接打开
})

// 先连 2 个成功 → 第 3 个应被拒绝 → 关闭 blockCh 释放所有连接
```

## 模板代码生成经验（protoc-gen-go-gin）

### 配置驱动全链路

```
configs/serverNameExample.yml              # YAML 定义
  ↓ make update-config
internal/config/serverNameExample.go        # 自动生成 Go struct（字段名与 YAML 严格对应）
  ↓ protoc-gen-go-gin 模板渲染
cmd/protoc-gen-go-gin/internal/generate/router/
  ├── main.go       # 传递 moduleName 给 saveGinRouterFiles
  ├── gen.go        # importPkg 结构体新增 ModuleName 字段
  └── template.go   # 使用 {{.ModuleName}} 构造 import 路径
  ↓ protoc 生成
api/.../xxx_router.pb.go                    # 生成的代码从 config.Get().Websocket.* 读取配置
```

### ModuleName 传递链路

```go
// main.go
func saveGinRouterFiles(f *protogen.File, moduleName string) error {
    ginRouterFileContent := router.GenerateFiles(f, moduleName)

// gen.go - importPkg 结构体
type importPkg struct {
    PackageName  string
    PackagePaths string
    HasWebSocket bool
    ModuleName   string   // ← 用于 import "<module>/internal/config"
}

// template.go - import 模板
{{if $.HasWebSocket}}
    "net/http"
    "time"
    "{{.ModuleName}}/internal/config"
    "github.com/18721889353/sunshine/pkg/gows"
{{end}}{{$.PackagePaths}}
```

### 模板中 WebSocket Upgrade 标准写法

生成的 `_router.pb.go` 中 WebSocket handler 的 Upgrade 调用：

```go
client, err := gows.Upgrade(c,
    gows.WithCheckOrigin(func(r *http.Request) bool {
        return config.Get().Websocket.Cors // 从配置读取 CORS 开关
    }),
    gows.WithHeartbeatOptions(
        gows.WithHeartbeatInterval(time.Duration(config.Get().Websocket.HeartbeatInterval)*time.Second),
    ),
    gows.WithClientUID(uid),
    gows.WithDispatcher(gows.DefaultDispatcher),
    gows.WithBufferSize(config.Get().Websocket.ReadBufferSize, config.Get().Websocket.WriteBufferSize),
    gows.WithRateLimit(config.Get().Websocket.RateLimitRps, config.Get().Websocket.RateLimitBurst),
    gows.WithMaxConnPerIP(config.Get().Websocket.MaxConnPerIP),
    gows.WithReadTimeout(time.Duration(config.Get().Websocket.ReadTimeout)*time.Second),
)
```

### YAML → Config 字段映射表

```yaml
websocket:
  cors: false              # Websocket.Cors
  readTimeout: 60          # Websocket.ReadTimeout（秒）
  rateLimitRps: 0          # Websocket.RateLimitRps
  rateLimitBurst: 0        # Websocket.RateLimitBurst
  maxConnPerIP: 0          # Websocket.MaxConnPerIP
  heartbeatInterval: 30    # Websocket.HeartbeatInterval（秒）
  readBufferSize: 0        # Websocket.ReadBufferSize（0=4096）
  writeBufferSize: 0       # Websocket.WriteBufferSize（0=4096）
```

所有值为 0 时 option 内部判断 `\u003e 0` 才生效，开箱即用无限制。

**关键经验**:
- `config.Get().Websocket.Cors` 是 bool 类型（非函数），模板中直接 `return config.Get().Websocket.Cors`
- `WithHeartbeatInterval` 接收 `time.Duration`，需 `time.Duration(val)*time.Second` 转换
- `WithBufferSize(read, write)` 接收两个 int 参数，0 表示使用 gorilla/websocket 默认值 4096
- `WithReadTimeout` 接收 `time.Duration`，0 时 option 内部不设置 ReadDeadline，由心跳管理
- 修改 YAML 后必须运行 `make update-config` 重新生成结构体

## 常见陷阱

| 陷阱 | 说明 | 解决 |
|------|------|------|
| CORS 默认全放行 | `defaultCheckOrigin` 原为 `return true` | 改为 `return false` + warn once |
| 限流器被重置 | `WithRateLimit` 每次调用创建新 limiter | 只初始化一次，handler 中只设 enable 标志 |
| 自发布消息误过滤 | InstanceID 跳过导致本地消息丢失 | 竞态由锁内 publish 解决，receiveLoop 不过滤 |
| writeLoop 自锁 | `Close()` 关闭 writeCh 后 writeLoop 写入 panic | 分离 `forceCloseConn` 跳过 writeCh |
| 心跳后 ReadDeadline 覆盖 | PongHandler 设置 ReadDeadline 可能被外部覆盖 | 心跳和 ReadTimeout 各管各的 |
| Producer 泄漏 | 每次 Publish 新建 AMQP Channel | Producer 缓存池（acquire/release 模式） |
| 模板 CORS 字段大小写 | 模板中用 `cors` 而非 `Cors` | Go 导出字段大写，必须 `config.Get().Websocket.Cors` |
| `make update-config` 缺失字段 | YAML 新增字段后未运行命令 | 修改 YAML → `make update-config` → 编译验证三步缺一不可 |
| 限流器因 `WithRateLimit` 测试中被重置 | 测试中调用 `WithRateLimit` 覆盖全局限流器 | 直接操作 `upgradeLimiter` + `upgradeLimiterMu` 而非调用 `WithRateLimit` |
| WebSocket import 遗漏 | 模板中 `HasWebSocket=false` 时不导入 net/http/config/time/gows | 检查 `gen.go` 中 `hasWebSocket` 扫描逻辑是否正确 |
| `WithReadTimeout` 类型冲突 | `client.go` 中 `WithReadTimeout` 返回 `ClientOption`，但 `Upgrade()` 接受 `UpgradeOption` | `client.go` 中重命名为 `withClientReadTimeout`（内部函数），`upgrader.go` 中新增 `WithReadTimeout` 返回 `UpgradeOption`，`Upgrade` 内部自动传播到 `client.readTimeout` |
