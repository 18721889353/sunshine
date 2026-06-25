验证# gows

Go 语言 WebSocket 服务端封装库，提供连接管理、消息读写、心跳保活和全局分发功能。

## ✨ 特性

- 🔒 **Channel 驱动写入模型**：非阻塞 WriteJSON，慢客户端自动丢弃消息，指数退避 + 随机 jitter
- 🛡️ **安全防护**：默认拒绝跨域、全局升级速率限制、单 IP 连接数限制
- 📡 **分布式 Dispatcher**：连接注册、广播、按 UID 发送、条件过滤、跨实例消息分发
- 🔗 **多种后端支持**：单机模式（无中间件）或 RabbitMQ 分布式模式（Producer 缓存池）
- 💓 **可配置心跳**：自定义间隔、写入超时
- 🔗 **链路追踪**：内置 OpenTelemetry，所有读写方法提供 Ctx 变体（`WriteJSONCtx`/`ReadMessageCtx`）
- 📊 **连接统计**：收发计数、队列状态、远程地址、健康检测（`IsAlive`）
- 🪝 **生命周期钩子**：升级前鉴权、升级后注入、关闭回调
- ⏱️ **主动读超时**：独立于心跳，防止 ReadMessage 永久阻塞
- 📏 **消息大小限制**：独立的读写大小限制（`readLimit`/`writeLimit`），防止恶意大消息

## 📦 安装

```bash
go get github.com/18721889353/sunshine/pkg/gows
```

### 依赖安装

```bash
go get github.com/gorilla/websocket
go get github.com/gin-gonic/gin
```

## 🚀 快速开始

### 1️⃣ 基础 WebSocket 升级

```go
package main

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "github.com/18721889353/sunshine/pkg/gows"
)

func main() {
    r := gin.Default()

    r.GET("/ws", func(c *gin.Context) {
        // 升级 HTTP 为 WebSocket 连接
        client, err := gows.Upgrade(c,
            gows.WithCheckOrigin(func(r *http.Request) bool {
                return true // 生产环境请限制可信域名
            }),
        )
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
            return
        }
        defer client.Close()

        // 读写循环（使用 Ctx 变体进行链路追踪）
        for {
            _, message, err := client.ReadMessageCtx(c.Request.Context())
            if err != nil {
                break
            }
            _ = client.WriteJSONCtx(c.Request.Context(), gows.Message{
                Type: "reply",
                Msg:  string(message),
            })
        }
    })

    _ = r.Run(":8080")
}
```

### 2️⃣ 启用心跳保活

```go
client, err := gows.Upgrade(c,
    gows.WithHeartbeat(), // 30s 自动心跳
)
```

### 3️⃣ 注册全局分发

```go
client, err := gows.Upgrade(c,
    gows.WithDispatcher(gows.DefaultDispatcher),
)
// 关闭时自动从 Dispatcher 注销
defer client.Close()
```

### 4️⃣ 完整示例

```go
package main

import (
    "log"
    "net/http"
    "github.com/gin-gonic/gin"
    "github.com/18721889353/sunshine/pkg/gows"
)

func main() {
    r := gin.Default()

    r.GET("/ws", func(c *gin.Context) {
        client, err := gows.Upgrade(c,
            gows.WithHeartbeat(),
            gows.WithDispatcher(gows.DefaultDispatcher),
        )
        if err != nil {
            log.Printf("upgrade failed: %v", err)
            return
        }
        defer client.Close()

        for {
            _, data, err := client.ReadMessageCtx(c.Request.Context())
            if err != nil {
                break
            }
            log.Printf("收到消息: %s, 来自: %s", string(data), client.RemoteAddr())
            _ = client.WriteJSONCtx(c.Request.Context(), gows.Message{
                Type: "ack",
                Msg:  "已收到: " + string(data),
            })
        }
    })

    // 在其他地方广播消息
    go func() {
        gows.DefaultDispatcher.BroadcastCtx(c.Request.Context(), gows.Message{
            Type: "notice",
            Msg:  "系统公告",
        })
    }()

    _ = r.Run(":8080")
}
```

## 📡 Dispatcher 用法

### 创建与注册

```go
// 使用默认全局实例
dispatcher := gows.DefaultDispatcher

// 或创建独立实例（单机模式）
dispatcher := gows.NewDispatcher(nil)

// 或创建分布式实例（需 RabbitMQ）
backend := gows.NewRabbitMQBackend("amqp://...", "ws:messages")
dispatcher := gows.NewDispatcher(backend)
dispatcher.Start(ctx)
```

### 发送消息

```go
// 广播给所有连接
dispatcher.BroadcastCtx(ctx, gows.Message{Type: "notice", Msg: "全员通知"})

// 按条件过滤广播
dispatcher.BroadcastFilterCtx(ctx, msg, func(c *gows.Client) bool {
    return c.UID() == "vip-user"
})

// 按 UID 发送
dispatcher.SendToUIDCtx(ctx, "user-123", gows.Message{Type: "personal", Msg: "你好"})
```

### 遍历与管理

```go
// 获取在线数量
count := dispatcher.Len()

// 遍历所有连接
dispatcher.Range(func(c *gows.Client) bool {
    log.Printf("在线: %s (%s)", c.UID(), c.RemoteAddr())
    return true // false 停止遍历
})

// 获取连接快照
clients := dispatcher.Clients()
for _, c := range clients {
    stats := c.Stats()
    log.Printf("UID=%s 已发=%d 已收=%d", stats.UID, stats.NumSent, stats.NumReceived)
}

// 统计信息
stats := dispatcher.Stats()
log.Printf("当前在线: %d", stats.TotalConnections)

// 分布式模式：启动后台接收
// dispatcher.Start(ctx)
// defer dispatcher.Stop()
```

## ⚙️ 高级配置

### 分布式部署

```go
// 使用 RabbitMQ 后端，跨实例消息分发
backend := gows.NewRabbitMQBackend(
    "amqp://user:pass@host:5672/",
    "ws:messages",
)
d := gows.NewDispatcher(backend, gows.WithMaxConnections(10000))
d.Start(ctx)
defer d.Stop()

// 注册连接后，即可跨实例发送
d.Register(client)
d.SendToUIDCtx(ctx, "user-123", msg)
d.BroadcastCtx(ctx, msg)

// 查看在线统计
log.Printf("在线: %d, 最大限制: %d", d.Len(), d.MaxConnections())
```

### Upgrader 选项

```go
client, err := gows.Upgrade(c,
    // 自定义跨域检查
    gows.WithCheckOrigin(func(r *http.Request) bool {
        return r.Header.Get("Origin") == "https://example.com"
    }),
    // 读写缓冲区大小
    gows.WithBufferSize(8192, 8192),
    // 子协议协商
    gows.WithSubprotocols("v2", "v1"),
    // 启用压缩（默认开启）
    gows.WithEnableCompression(true),
    // 设置用户标识
    gows.WithClientUID("user-123"),
    // 心跳
    gows.WithHeartbeat(),
    // 心跳高级配置
    gows.WithHeartbeatOptions(
        gows.WithHeartbeatInterval(15*time.Second),
        gows.WithPingMessage(func() any {
            return gows.Message{Type: "ping", Data: time.Now().Unix()}
        }),
    ),
    // 全局分发
    gows.WithDispatcher(gows.DefaultDispatcher),
    // 升级前钩子（鉴权）
    gows.WithBeforeUpgrade(func(c *gin.Context) error {
        token := c.Query("token")
        if token == "" {
            return fmt.Errorf("missing token")
        }
        return nil
    }),
    // 升级后钩子（链路追踪）
    gows.WithAfterUpgrade(func(c *gin.Context, client *gows.Client) {
        log.Printf("客户端 %s 已连接", client.RemoteAddr())
    }),
    // 升级失败回调
    gows.WithErrorHandler(func(c *gin.Context, err error) {
        c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
    }),
)
```

### Client 选项

```go
// Client 通过 NewClient 构造函数创建，ctx 用于链路追踪和生命周期管理
client := gows.NewClient(ctx, rawConn, "uid",
    // 写入队列大小（默认 64）
    gows.WithWriteQueueSize(128),
    // 读取队列大小（默认 64）
    gows.WithReadQueueSize(128),
)

// 单条消息大小限制通过 Upgrade 选项传播（Upgrade → Client）
// 见上方 Upgrader 选项中的 WithReadLimit / WithWriteLimit / WithReadTimeout / WithWriteTimeout
```

### Dispatcher 选项

```go
// 分布式模式：设置 WorkerPool 大小（默认 4）
d := gows.NewDispatcher(backend,
    gows.WithMaxConnections(10000),
    gows.WithWorkerPool(8), // 消息投递 worker 数
)
```

### RabbitMQ Backend 选项

```go
backend := gows.NewRabbitMQBackend("amqp://...", "ws:messages",
    // Producer 缓存池大小（默认 4）
    gows.WithProducerPoolSize(8),
)
```

### Client 统计信息

```go
stats := client.Stats()
fmt.Printf(`客户端状态:
  UID:            %s
  远程地址:       %s
  是否存活:       %v
  已发送:         %d
  已接收:         %d
  队列容量:       %d
  队列当前长度:   %d
  是否已关闭:     %v
  写入错误次数:   %d
  上次写入时间:   %s
  上次读取时间:   %s
`, stats.UID, stats.RemoteAddr, stats.IsAlive, stats.NumSent, stats.NumReceived,
    stats.WriteQueueSize, stats.WriteQueueLen, stats.IsClosed,
    stats.WriteErrCount, stats.LastWriteTime, stats.LastReadTime)

// Dispatcher 统计
dStats := dispatcher.Stats()
fmt.Printf(`分发器状态:
  总连接数:       %d
  本地连接数:     %d
  远程 UID 数:    %d
  最大连接限制:   %d
  被拒绝连接数:   %d
`, dStats.TotalConnections, dStats.LocalConnections,
    dStats.RemoteUIDs, dStats.MaxConnections, dStats.TotalRejected)

// 获取全局唯一 UID 列表
_ = dispatcher.ConnectedUIDs()
```

## 📋 API 参考

### 核心类型

```go
// 通用消息结构
type Message struct {
    Type string      `json:"type"`
    Msg  string      `json:"msg,omitempty"`
    Data any         `json:"data,omitempty"`
}
```

### Client 方法

| 方法 | 说明 |
|------|------|
| `WriteJSON(v any) error` | 异步非阻塞发送 JSON 消息（委托 `WriteJSONCtx(c.ctx, v)`） |
| `WriteJSONCtx(ctx, v) error` | 带链路追踪的 JSON 发送 |
| `WriteRaw(data []byte) error` | 直接写入预序列化的原始字节 |
| `WriteRawCtx(ctx, data) error` | 带链路追踪的原始数据发送 |
| `ReadMessage() (int, []byte, error)` | 阻塞读取消息（委托 `ReadMessageCtx(c.ctx)`） |
| `ReadMessageCtx(ctx) (int, []byte, error)` | 带链路追踪的消息读取 |
| `Close() error` | 优雅关闭（幂等） |
| `UID() string` | 获取用户标识 |
| `RemoteAddr() string` | 获取远程地址 |
| `Context() context.Context` | 获取关联上下文（Close 后自动取消） |
| `IsAlive() bool` | 检测连接是否健康存活 |
| `Done() <-chan struct{}` | 连接关闭信号 |
| `SetCloseHook(fn func())` | 设置关闭钩子 |
| `Stats() ClientStats` | 获取统计信息 |

### Dispatcher 方法

| 方法 | 说明 |
|------|------|
| `Register(client *Client) error` | 注册连接（委托 `RegisterCtx(context.Background(), client)`） |
| `RegisterCtx(ctx, client) error` | 带链路追踪的注册 |
| `Unregister(client *Client) error` | 注销连接 |
| `UnregisterCtx(ctx, client) error` | 带链路追踪的注销 |
| `BroadcastCtx(ctx, v)` | 广播消息 |
| `BroadcastFilterCtx(ctx, v, filter)` | 条件广播 |
| `SendToUIDCtx(ctx, uid, v)` | 按 UID 发送 |
| `SendToMultiUIDCtx(ctx, uids, v)` | 按多 UID 发送 |
| `Range(fn)` | 遍历连接 |
| `Len() int` | 在线连接数 |
| `LenLocal() int` | 本地在线连接数 |
| `Clients() []*Client` | 连接快照 |
| `ConnectedUIDs() []string` | 全局唯一 UID 列表 |
| `MaxConnections() int` | 最大连接数限制 |
| `Stats() DispatcherStats` | 统计信息 |
| `Start(ctx)` | 启动分布式后端（单机无需调用） |
| `Stop()` | 停止分布式后端 |

### Upgrade 选项

| 选项 | 说明 |
|------|------|
| `WithCheckOrigin(fn)` | 跨域检查（默认拒绝所有） |
| `WithBufferSize(read, write)` | 读写缓冲区大小 |
| `WithSubprotocols(protocols...)` | 子协议协商 |
| `WithEnableCompression(enable)` | 压缩开关 |
| `WithHeartbeat()` | 启用心跳（30s） |
| `WithHeartbeatOptions(opts...)` | 心跳高级配置 |
| `WithDispatcher(d)` | 注册到分发中心 |
| `WithClientUID(uid)` | 设置用户标识 |
| `WithErrorHandler(fn)` | 升级失败回调 |
| `WithBeforeUpgrade(fn)` | 升级前钩子 |
| `WithAfterUpgrade(fn)` | 升级后钩子 |
| `WithRateLimit(rps, burst)` | 全局升级速率限制 |
| `WithMaxConnPerIP(n)` | 单 IP 最大连接数 |
| `WithReadLimit(n)` | 单条消息读取大小限制 |
| `WithWriteLimit(n)` | 单条消息写入大小限制 |
| `WithReadTimeout(d)` | 读取超时（Upgrade → Client 传播） |
| `WithWriteTimeout(d)` | 写入超时（Upgrade → Client 传播） |

### Heartbeat 选项

| 选项 | 说明 |
|------|------|
| `WithHeartbeatInterval(d)` | 心跳间隔 |
| `WithPongTimeout(d)` | Pong 等待超时（默认 10s） |
| `WithPingWriteWait(d)` | Ping 控制帧写入超时（默认 5s） |
| `WithPingMessage(fn)` | 自定义 ping 消息（通过 HeartbeatOptions 传） |

## 💡 最佳实践

### 1. 优雅关闭

```go
client, err := gows.Upgrade(c, gows.WithDispatcher(dispatcher))
if err != nil {
    return
}
defer client.Close()

// client.Context() 会在 Close 时自动取消
client.Context().Done() // 可用于 select 监听
```

### 2. 并发写入安全

`WriteJSON` 是线程安全的，可以在多个 goroutine 中同时调用：

```go
// 业务协程
go func() {
    for {
        client.WriteJSON(Message{Type: "heartbeat"})
        time.Sleep(30 * time.Second)
    }
}()

// 推送协程
go func() {
    for msg := range msgCh {
        client.WriteJSON(msg)
    }
}()
```

### 3. Dispatcher 与业务整合

```go
type ChatRoom struct {
    dispatcher *gows.DistributedDispatcher
}

func (r *ChatRoom) Join(ctx context.Context, client *gows.Client) {
    if err := r.dispatcher.RegisterCtx(ctx, client); err != nil {
        log.Printf("注册失败: %v", err)
        return
    }
    client.SetCloseHook(func() {
        r.dispatcher.UnregisterCtx(ctx, client)
        r.dispatcher.BroadcastCtx(context.Background(), gows.Message{
            Type: "system",
            Msg:  client.UID() + " 离开了房间",
        })
    })
}
```

### 4. 错误处理

```go
result, err := client.WriteJSON(msg)
if err == gows.ErrWriteQueueFull {
    // 队列满，消息被丢弃，可降级处理
    log.Warn("消息丢弃: 客户端消费过慢")
} else if err == websocket.ErrCloseSent {
    // 连接已关闭
    return
}
```

## ⚠️ 注意事项

- **资源释放**：`Upgrade` 返回的 `*Client` 必须调用 `Close()`，建议使用 `defer`
- **跨域**：默认拒绝所有来源，必须使用 `WithCheckOrigin` 显式设置允许的来源
- **限流**：生产环境建议设置 `WithRateLimit` 和 `WithMaxConnPerIP` 防止连接风暴
- **链路追踪**：优先使用 Ctx 变体（`WriteJSONCtx`/`ReadMessageCtx`），传递请求上下文以保留 request_id
- **写队列满**：`WriteJSON` 在队列满时返回 `ErrWriteQueueFull` 而非阻塞
- **消息大小限制**：生产环境建议设置 `WithReadLimit()` 和 `WithWriteLimit()` 防止恶意大消息
- **ReadTimeout**：未启用心跳时建议设置 `WithReadTimeout()` 防止 ReadMessage 永久阻塞
