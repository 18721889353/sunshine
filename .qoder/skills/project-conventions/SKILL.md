---
name: project-conventions
description: Documents Sunshine framework's shared lint compliance rules, Go development conventions, error handling patterns, Chinese comment/log standards, and lessons learned from real project fixes. Use when modifying any Go file, fixing lint errors, or reviewing code changes — this is the canonical reference for all project-wide coding standards.
---

# Sunshine 框架开发公约

## 核心原则

- **禁止 `//nolint`**：不允许用 `//nolint` 注释绕过 lint 检查，所有问题必须通过修复代码解决
- **禁止修改 `.golangci.yml`**：lint 配置是项目级统一标准，不为单点问题添加例外配置
- **`make ci-lint` 必须通过**：提交前运行 `make ci-lint`（`gofmt -s -w .` + `golangci-lint run ./...`）

## 目录

| # | 内容 | 说明 |
|---|------|------|
| 一 | errcheck — 错误必须显式处理 | check-blank + check-type-assertions + 类型断言规范 + 清理场景 + shadow 陷阱 |
| 二 | revive empty-block — 禁止空 if 块 | |
| 三 | revive redefines-builtin-id — 禁止覆盖内置标识符 | |
| 四 | revive unused-parameter — 未使用的参数用 `_` | |
| 五 | revive var-naming — Go 命名约定 | |
| 六 | revive package-comments — 包注释格式 | |
| 七 | goimports — 本地包必须单独分组 | |
| 八 | revive identical-branches — 禁止相同分支 | |
| 九 | revive context-as-argument — context.Context 必须是第一参数 | |
| 十 | gocognit — 认知复杂度超限 | 提取子函数 + 条件前置 |
| 十一 | 经验教训总结 | 闭包递归 / gocognit 修复 / 日志 ctx / 批量不中断 / 条件前置 / 资源清理 / atomic.Pointer / 字符串错误 / 优雅关闭 |
| 十二 | 原子计数器优先使用 atomic.Int32/Int64 | 指针替代 CAS 循环 |
| 十三 | sync.Map 操作规范 — 计数漂移保护 | Range 内联 I/O 去中间切片 |
| 十四 | Ctx 变体设计规范 | Ctx 变体 + 构造函数接收 Context |
| 十五 | Option 配置传播模式 | defaultXxx+apply + 冲突处理 |
| 十六 | 测试文件组织 | 一对一映射 + mock 共享 + 单元/集成分离 + 集成测试规范 |
| 十七 | goroutine panic recover 策略 | 终止型退出清理 + 循环型继续执行 |
| 十八 | 中文注释与日志规范 | 包注释内容要求 / 日志中文 / 错误中文 / 详细注释风格 |
| 十九 | README 文档编写规范 | 场景驱动 / 架构概览 / 结构体表格 / Option 适用 API / API 速查独立小节 / 分隔线 / 反模式 |

## 一、errcheck — 错误必须显式处理

`.golangci.yml` 开启了 `check-blank: true`（标记 `_ = func()`）和 `check-type-assertions: true`（标记类型断言）。以下写法都会被报错：

### 禁止写法

```go
// ❌ 空白赋值被 check-blank 标记
_ = conn.SetReadDeadline(t)

// ❌ 类型断言被 check-type-assertions 标记
s, _ := v.(string)

// ❌ 禁止使用 nolint
_ = client.Close() //nolint:errcheck
```

### 正确写法

```go
// ✅ if err 处理，body 放有意义的操作
if err := conn.SetReadDeadline(t); err != nil {
    return // 连接已关闭，及早返回
}

// ✅ 类型断言检查 ok 值
if s, ok := v.(string); ok {
    lastWriteErrStr = s
}

// ✅ 记录日志
if closeErr := client.Close(); closeErr != nil {
    logger.WarnWithCtx(ctx, "close failed", logger.Err(closeErr))
}
```

### 类型断言规范

errcheck 开启了 `check-type-assertions: true`，**所有**类型断言都必须使用 comma-ok 双值形式，包括 `sync.Map.Range` 回调、`interface{}` 字段取值等场景。

```go
// ❌ 错误：单值类型断言被 errcheck 标记
s := v.(string)
close(value.(chan struct{}))
if key.(*Client).uid == uid { }

// ✅ 正确：双值 comma-ok
if s, ok := v.(string); ok {
    lastWriteErrStr = s
}
if ch, ok := value.(chan struct{}); ok {
    close(ch)
}
if c, ok := key.(*Client); ok && c.uid == uid {
    found = true
}
```

#### sync.Map.Range 回调中的类型断言

`sync.Map.Range` 回调接收 `key, value any`，类型断言时必须检查 `ok`，不可识别的 key 应跳过（`return true` 继续遍历）：

```go
// ❌ 错误：无条件断言，panic 风险 + errcheck 标记
dd.clients.Range(func(key, _ any) bool {
    clients = append(clients, key.(*Client))
    return true
})

// ✅ 正确：双值接收，不可识别的 key 跳过
var clients []*Client
dd.clients.Range(func(key, _ any) bool {
    if c, ok := key.(*Client); ok {
        clients = append(clients, c)
    }
    return true
})
```

需要提前返回的场景：

```go
// before: client := key.(*Client)
// after:
client, ok := key.(*Client)
if !ok {
    return true // 跳过不可识别的 key
}
```

### 纯清理场景的规范处理

纯清理场景（如缓存池满时关闭多余 Producer、关闭失败连接后的二次清理），Close 错误的无法恢复，但 errcheck 不允许忽略。必须用 `if err` 记录日志，不得使用 `//nolint:errcheck`：

```go
// ❌ 错误：禁止使用 nolint 绕过
default:
    _ = p.Close() //nolint:errcheck

// ❌ 错误：空白赋值仍被 check-blank 标记
default:
    _ = p.Close()

// ✅ 正确：用 if err 记录日志
default:
    if err := p.Close(); err != nil {
        logger.WarnWithCtx(ctx, "close producer failed", logger.Err(err))
    }
```

**无 ctx 场景**：传入 `context.Background()`

```go
func (b *RabbitMQBackend) putProducer(p *gorabbitmq.Producer) {
    select {
    case b.producerPool <- p:
    default:
        if err := p.Close(); err != nil {
            logger.WarnWithCtx(context.Background(), "close producer failed", logger.Err(err))
        }
    }
}
```

### 变量 Shadow 陷阱

在已有 `err` 的作用域内，`if err :=` 会触发 `govet shadow`：

```go
// ❌ 错误：err 与外层冲突
if err := register(client); err != nil {
    if err := client.Close(); err != nil {}  // shadow!
    span.SetStatus(codes.Error, err.Error()) // 这里的 err 是 close 的 error，不是 register 的
}

// ✅ 正确：用不同变量名避免 shadow
if err := register(client); err != nil {
    if closeErr := client.Close(); closeErr != nil {
        logger.WarnWithCtx(ctx, "...", logger.Err(closeErr))
    }
    span.SetStatus(codes.Error, err.Error()) // err 仍指向 register 的错误
}
```

## 二、revive empty-block — 禁止空 if 块

```go
// ❌ 错误：空注释不能绕过
if err := fn(); err != nil {
    // 空注释
}

// ✅ 正确：加日志或实际处理
if err := fn(); err != nil {
    logger.WarnWithCtx(ctx, "failed", logger.Err(err))
}
```

## 三、revive redefines-builtin-id — 禁止覆盖内置标识符

Go 1.21+ 新增了 `max`、`min`、`clear` 内置函数，不能用作参数名或变量名：

```go
// ❌ 错误
func WithMaxConnections(max int) {}

// ✅ 正确
func WithMaxConnections(n int) {}
// 或 limit, count, maxConns 等
```

## 四、revive unused-parameter — 未使用的参数用 `_`

```go
// ❌ 错误
var fn = func(r *http.Request) bool { return true }

// ✅ 正确
var fn = func(_ *http.Request) bool { return true }
```

## 五、revive var-naming — Go 命名约定

| 错误写法 | 正确写法 |
|----------|----------|
| `clientUid` | `clientUID` |
| `parseUrl` | `parseURL` |
| `userId` | `userID` |
| `httpAddr` | `HTTPAddr` |

## 六、revive package-comments — 包注释格式

必须 `// Package <包名> ...`，与 `package <包名>` 严格一致：

```go
// ❌ 错误
// Package ws 提供...
package gows

// ✅ 正确
// Package gows 提供...
package gows
```

### 包注释内容要求

包注释必须使用中文，描述包的**实际功能和职责**，不能只是泛泛的"提供 XXX 功能"。每个包只在**主文件**中写一份详细的包级注释，其余文件不重复。

```go
// ❌ 错误：过于笼统，看不出包的具体能力
// Package nacoscli 提供 Nacos 配置中心客户端。
package nacoscli

// ❌ 错误：多个文件重复写包注释
// nacoscli.go — // Package nacoscli 提供...
// listener.go — // Package nacoscli 提供...  ← 重复

// ✅ 正确：详细描述实际功能，只在主文件写一次
// Package nacoscli 封装 Nacos 配置中心的客户端操作，提供配置获取、实时监听和服务注册能力。
//
// 核心功能：
//   - 配置获取：GetConfig / Client.getConfig 从 Nacos 拉取配置，支持 context 超时控制。
//   - 实时监听：ListenClient 基于长轮询监听配置变更；WatchConfig 封装自动重连。
//   - 服务注册与发现：NewClient 创建命名客户端，用于服务注册/注销/发现。
//   - 选项模式：通过 Option 函数灵活配置，优先级：完整配置 > 单字段选项 > 默认值。
package nacoscli
```

| 规则 | 说明 |
|------|------|
| **中文描述** | 包注释必须使用中文 |
| **描述实际功能** | 列出核心 API 和职责，不能只是"提供 XXX 客户端" |
| **单文件声明** | 每个包只在主文件写包级注释，其他文件不重复 |
| **主文件选择** | 选包的核心入口文件（如 `xxx.go`），不选辅助文件 |

## 七、goimports — 本地包必须单独分组

`.golangci.yml` 配置了 `local-prefixes: github.com/18721889353/sunshine`：

```go
import (
    "fmt"
    "time"

    "github.com/go-resty/resty/v2"
    "go.opentelemetry.io/otel"

    "github.com/18721889353/sunshine/pkg/logger"  // 本地包独立分组
)
```

## 八、revive identical-branches — 禁止相同分支

```go
// ❌ 错误：if 和 else 完全一致
if len(x) > 0 {
    return fn(a, b)
} else {
    return fn(a, b)
}

// ✅ 正确：去掉 if-else
return fn(a, b)
```

## 九、revive context-as-argument — context.Context 必须是第一参数

`context.Context` 必须是函数的第一个参数，这是 Go 社区惯例。放在后面不会触发编译器错误，但 `revive` 会报错：

```go
// ❌ 错误：ctx 不是第一参数
func upgradeCORSCheck(o *upgradeOptions, c *gin.Context, span trace.Span, ctx context.Context) error {}

// ✅ 正确：ctx 是第一参数
func upgradeCORSCheck(ctx context.Context, o *upgradeOptions, c *gin.Context, span trace.Span) error {}
```

## 十、gocognit — 认知复杂度超限

`.golangci.yml` 配置 `gocognit` 阈值 40。超过此值说明函数包含过多嵌套（if/for/switch 嵌套层次深）或过多布尔运算符。

### 解决办法：提取子函数 + 条件前置

把函数中独立的功能块提取为命名函数，并在调用处用`if o.field`模式前置条件判断，可有效降低主函数的认知复杂度：

```go
// ❌ 错误：函数认知复杂度 47，超过 40
func Upgrade(...) (*Client, error) {
    // 30 行 CORS 检查内联
    // 15 行限流检查内联
    // 25 行 IP 检查内联
    // 40 行配置传播（6 个 if 块）
    // 50 行分发注册+清理钩子
}

// ✅ 正确：提取子函数 + 条件前置后复杂度降至 < 40
func Upgrade(...) (*Client, error) {
    // CORS 简化内联（无需再提取）
    if !o.checkOriginSet { ... return }
    if !o.checkOrigin(c.Request) { ... return }

    if o.enableRateLimit {                          // 条件前置
        if limiter := upgradeLimiter.Load(); limiter != nil {
            if !limiter.Allow() { return nil, err }  // 限流逻辑简单，保持内联
        }
    }
    if o.maxConnPerIP > 0 {                         // 条件前置
        if err := upgradePerIPCheck(clientIP, o.maxConnPerIP, span); err != nil { return nil, err }
    }
    client := NewClient(ctx, rawConn, o.clientUID, buildClientOpts(o)...)
    if o.enableDistributed && o.dispatcher != nil { // 条件前置
        if err := upgradeRegisterDispatcher(ctx, client, o, c); err != nil { return nil, err }
    }
}
```

### 提取收益参考表

| 提取前 | 提取后 | 复杂度降幅 |
|--------|--------|----------|
| CORS 检查 30 行内联 | 简化内联 6 行（无需提取） | ~2 点 |
| 限流检查 ~10 行内联 | 保持内联（逻辑简单，无需提取） | ~0 点 |
| IP 检查 25 行内联 | `upgradePerIPCheck` 函数调用 | ~4 点 |
| 6 个 Option 传播 + 字段赋值 | `buildClientOpts` + ClientOption 传入 | ~6 点 |
| 分发注册+清理钩子 50 行 | `upgradeRegisterDispatcher` 函数调用 | ~6 点 |

提取 2~3 个子函数通常可将复杂度从 47 降至 < 40。

### 条件前置原则

调用处用`if o.field`模式判断是否执行该子函数，**不在被调函数内部做冗余判断**。子函数假设"调用者已经保证需要执行我"，内部不做`if !o.enableXxx { return nil }`类的提前返回。

Benefits:
- 调用处清晰展示配置生效条件，一目了然
- 子函数职责单一，不隐含"可能跳过"的逻辑分支
- 条件变化时只改主函数调用处，不影响子函数
- 避免内外两层 guard 的冗余嵌套

## 十一、经验教训总结

### 12.1 close hook 闭包避免递归

`SetCloseHook` 的闭包中如果运行时读取 `c.onClose` 字段，会读到闭包自身（`SetCloseHook` 设置的新值），形成无限递归导致栈溢出：

```go
// ❌ 错误：运行时读取 c.onClose，返回的是刚设置的新钩子自身
client.SetCloseHook(func() {
    if prevHook := client.onClose; prevHook != nil {  // → 闭包自身
        prevHook()  // 递归调用！
    }
})

// ✅ 正确：在 SetCloseHook 调用前保存旧钩子
prevHook := client.onClose
client.SetCloseHook(func() {
    if prevHook != nil {  // 指向旧钩子，安全
        prevHook()
    }
})
```

**规则**：闭包需要引用对象的前值（如钩子链、计数器）时，必须在 `SetXxx`/`Register` 之前保存到局部变量，闭包中使用局部变量而非运行时读取实例字段。

### 12.2 gocognit 超限的修复模式

当 `gocognit` 报认知复杂度超限时，不要尝试在函数内部简化（inline if 改 switch 等小技巧效果有限），最佳方案是提取子函数：

1. 找到函数中逻辑独立的代码块（配置传播、安全检查、资源注册等）
2. 每个块提取为一个命名函数，参数显式传递依赖
3. 主函数变为子函数调用的线性序列
4. 每个提取操作通常降低 2~6 点认知复杂度

```go
// 提取前：6 个属性传播的 if 块散落在主函数中
if o.readTimeout > 0 { client.readTimeout = o.readTimeout }
if o.writeTimeout > 0 { client.writeTimeout = o.writeTimeout }
if o.readLimit > 0 { client.readLimit = o.readLimit }
// ...

// 提取后：一行调用，降低 ~6 点
buildClientOpts(o)  // buildClientOpts 在 NewClient 参数中调用
```

### 12.3 日志上下文传递

`logger.Info(...)` 和 `logger.Warn(...)` 不带 context，无法输出 request_id。必须用带 `Ctx` 的版本：

```go
// ❌ 错误：丢失 request_id
logger.Warn("write failed", logger.Err(err))

// ✅ 正确：保留 request_id
logger.WarnWithCtx(ctx, "write failed", logger.Err(err))
```

### 12.4 批量操作中单点失败不中断整体

批量处理场景（遍历列表、广播消息等）中，单个元素的处理失败不应中断其他元素：

```go
// ✅ 错误只记录日志，继续处理剩余元素
for _, item := range items {
    if err := process(item); err != nil {
        logger.WarnWithCtx(ctx, "process item failed",
            logger.String("item", item.ID()),
            logger.Err(err),
        )
        continue // 继续处理下一个
    }
}
```

### 12.5 条件判断前置到调用处

代码中涉及配置项的条件判断，统一采用`if o.field > 0` / `if o.field`模式在**调用处**判断是否执行，不在被调函数内部做冗余判断。

```go
// ❌ 错误：被调函数内部做 guard clause
func upgradePerIPCheck(o *upgradeOptions, clientIP string, span trace.Span) error {
    if o.maxConnPerIP <= 0 {
        return nil  // 调用处已确保不会进入此函数，此处多余
    }
    // ... 实际检查逻辑
}

// ✅ 正确：调用处判断，被调函数只管执行
if o.maxConnPerIP > 0 {
    if err := upgradePerIPCheck(clientIP, o.maxConnPerIP, span); err != nil {
        return nil, err
    }
}
func upgradePerIPCheck(clientIP string, maxConnPerIP int32, span trace.Span) error {
    // 调用者已保证 maxConnPerIP > 0，直接检查
    // ...
}
```

收益：
1. **调用处即文档** — 扫一眼主函数就看清所有前置条件，无需翻看子函数实现
2. **子函数纯净** — 不隐含"可能跳过"的逻辑分支，职责单一
3. **条件变化影响最小** — 只改主函数调用处，所有子函数免修改
4. **避免重复 guard** — 调用处和被调函数不会出现两层嵌套判断

### 12.6 初始化/注册失败时清理已分配资源

创建资源后如果后续步骤失败，必须先清理已分配的资源再返回错误：

```go
client := createClient()
if err := register(client); err != nil {
    if closeErr := client.Close(); closeErr != nil {
        logger.WarnWithCtx(ctx, "close after register failed",
            logger.Err(closeErr),
        )
    }
    return nil, fmt.Errorf("register: %w", err)
}
```

### 12.7 全局变量优先用 atomic.Pointer

全局单例指针（如限流器）优先使用 `atomic.Pointer[T]`，而非 `*T + sync.Mutex`：

```go
// ❌ 错误：mutex 保护读写，需要临时变量
var (
    limiter   *rate.Limiter
    limiterMu sync.Mutex
)

func Upgrade() {
    limiterMu.Lock()
    l := limiter
    limiterMu.Unlock()
    if l != nil && !l.Allow() { ... }
}

// ✅ 正确：atomic.Pointer，无需锁和局部变量
var limiter atomic.Pointer[rate.Limiter]

func WithRateLimit(rps, burst int) {
    limiter.Store(rate.NewLimiter(...))
}

func Upgrade() {
    if l := limiter.Load(); l != nil {
        if !l.Allow() { ... }
    }
}
```

### 12.8 纯字符串错误用 fmt.Errorf，不定义自定义类型

如果错误类型从未被 `errors.Is`/`errors.As` 判断过，只是作为字符串返回给调用方，直接用 `fmt.Errorf`：

```go
// ❌ 错误：自定义类型 + Error() 方法，从未被判断过
type ErrUpgradeRateLimited struct { limit rate.Limit }
func (e *ErrUpgradeRateLimited) Error() string {
    return fmt.Sprintf("rate limited: %.2f rps", e.limit)
}

// ✅ 正确：直接 fmt.Errorf
return fmt.Errorf("ws upgrade rate limited: %.2f rps", limiter.Limit())
```

判断标准：全局搜索 `errors.Is` / `errors.As` + 类型名，没有任何使用即可删除。

### 12.9 优雅关闭：WithoutCancel + drain 保证零丢失

长连接关闭时需保证已入队数据不丢失，核心三原则：

1. **`context.WithoutCancel` 阻断上游超时** — 长连接独立于请求生命周期，上游 Gin ctx 超时不级联取消
2. **永不 close 多生产者 channel** — 用 `ctx.Done()` 做统一退出信号，根除 send-on-closed-channel panic
3. **关闭信号后先 drain 再退出** — 收到 ctx.Done() 后排空队列中剩余数据，确保不丢失

```go
// 构造函数中隔离上游
clientCtx := context.WithCancel(context.WithoutCancel(ctx))

// 消费协程：关闭信号后 drain
case <-clientCtx.Done():
    for len(ch) > 0 {
        _ = process(<-ch) // best-effort 排空
    }
    return

// 关闭时序：cancel → wait write (drain) → close TCP → wait read
func (c *Client) Close() error {
    if c.closed.CompareAndSwap(false, true) {
        c.clientCtxCancel()
        c.writeWg.Wait()              // drain + 退出
        closeErr := c.wsConn.Close()  // unblock ReadMessage
        c.readWg.Wait()
        return closeErr
    }
    return nil
}
```

**适用场景**：WebSocket 连接、消息队列消费者、任何需保证关闭时数据不丢失的并发写入系统。

## 十二、原子计数器优先使用 atomic.Int32/Int64 类型

涉及并发写入的整型计数场景（跨 goroutine `Add`/`CAS`/`Store`+`Load`），优先使用标准库 `atomic.Int32` / `atomic.Int64` 结构体类型，而非裸 `int32`/`int64` + 包函数：

```go
// ❌ 旧风格：裸类型 + 包函数，需取地址 &，易错
var count int32
atomic.AddInt32(&count, 1)
n := atomic.LoadInt32(&count)

// ✅ 新风格：atomic.Int64 类型 + 方法调用，无需取地址
var count atomic.Int64
count.Add(1)
n := count.Load()
```

### 判断标准

| 需要改 | 不改 |
|--------|------|
| 多处并发写入（`Add`/`CAS`/`Store`）的字段 | 构造函数写一次、后续只读的字段（如 `readLimit`） |
| 跨 goroutine 读写竞争的热点 | 仅在创建 goroutine 内使用的局部变量 |

### 迁移示例

```go
// Before
type Client struct {
    closed      int32   // 原子关闭标记
    numSent     int64   // 原子计数
    numReceived int64   // 原子计数
}

func (c *Client) Close() error {
    if atomic.CompareAndSwapInt32(&c.closed, 0, 1) { ... }
}

// After
type Client struct {
    closed      atomic.Int32  // 原子关闭标记
    numSent     atomic.Int64  // 原子计数
    numReceived atomic.Int64  // 原子计数
}

func (c *Client) Close() error {
    if c.closed.CompareAndSwap(0, 1) { ... }
}
```

### 注意事项

- 结构体字面量中不能直接赋值 `maxConns: o.maxConns`（`atomic.Int32` 是结构体类型），需在构造后调用 `.Store()`
- 不再需要 `sync/atomic` 导入的情况：文件中所有 `atomic.*` 包函数调用被替换为方法调用后，可删除导入

### `*atomic.Int32` 指针在 sync.Map 中的应用

当 sync.Map 需要存储并递增计数器时，存储 `*atomic.Int32` 指针替代裸 `int32` CAS 循环：

```go
// ❌ 错误：MAP 级 CAS 循环，复杂且易错
for {
    val, _ := m.LoadOrStore(key, int32(0))
    count := val.(int32)
    if m.CompareAndSwap(key, count, count+1) {
        break
    }
}

// ✅ 正确：*atomic.Int32 指针，Add(1) 一行完成
actual, _ := m.LoadOrStore(key, &atomic.Int32{})
counter, ok := actual.(*atomic.Int32)
if !ok { continue }  // comma-ok 类型断言
counter.Add(1)
```

递减时配合 `LoadAndDelete` 安全移除 key：

```go
counter := val.(*atomic.Int32)
if counter.Add(-1) <= 0 {
    if _, loaded := m.LoadAndDelete(key); loaded {
        totalCount.Add(-1)  // 只有实际移除了才递减总量
    }
}
```

**收益**：省去 CAS 循环，代码更简洁可读。适用于分布式连接管理、在线状态追踪等 sync.Map + 计数器组合场景。

## 十三、sync.Map 操作规范 — 计数漂移保护

### 问题场景

`sync.Map` 允许多路径并发操作同一 key（如 `UnregisterCtx` / `CleanupDeadConns` / 消息投递中的僵尸清理同时删除同个 `*Client`）。无条件计数器递减会导致计数漂移。

### 正确模式

```go
// ❌ 错误：无条件递减，并发删除时计数漂移
dd.clients.Delete(client)
dd.clientCount.Add(-1)

// ✅ 正确：只有实际删除了才递减
if _, loaded := dd.clients.LoadAndDelete(client); loaded {
    dd.clientCount.Add(-1)
}
```

ctx 回滚时同样需条件判断：

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

### 应用场景

| 操作 | 注意 |
|------|------|
| `RegisterCtx` | `Store(client, struct{}{})` + `Add(1)`，新注册 client 不会与其他路径并发，可不加 loaded 判断 |
| `UnregisterCtx` | 必须用 `LoadAndDelete` + `if loaded`，可能与其他清理路径并发 |
| `CleanupDeadConns` | Range 回调内必须用 `LoadAndDelete` + `if loaded { cleaned++ }`，再统一 `Add(-cleaned)` |
| 消息投递中的僵尸清理 | 已用 `LoadAndDelete` + `if loaded`，正确 |

### 规则

| 规则 | 说明 |
|------|------|
| **并发删除用 LoadAndDelete** | 可能被多条路径并发删除的 map，必须用 `LoadAndDelete` + `if loaded` 保护计数器 |
| **ctx 回滚一致** | 回滚操作的条件必须与删除操作一致 |
| **Range 中删除用 CAS** | `sync.Map.Range` 回调内删除当前 key，使用 `LoadAndDelete` 确保操作原子性 |

### Range 回调中直接执行业务逻辑

`sync.Map.Range` **不持有内部锁**，回调中可以直接执行网络写入等 I/O 操作，无需预拷贝全量切片：

```go
// ❌ 错误：先拷贝全量切片再遍历（额外分配 + 两步循环）
var clients []*Client
m.Range(func(key, _ any) bool {
    if c, ok := key.(*Client); ok {
        clients = append(clients, c)
    }
    return true
})
for _, c := range clients {
    c.Write(data)
}

// ✅ 正确：Range 回调中直接写入，零中间分配
m.Range(func(key, _ any) bool {
    c, ok := key.(*Client)
    if !ok { return true }
    if !c.IsAlive() {
        dead = append(dead, c)  // 异常元素延迟删除
        return true
    }
    c.Write(data)
    return true
})
// 遍历结束后统一清理
for _, c := range dead {
    m.LoadAndDelete(c)
}
```

**注意**：回调内部需先做条件过滤（如 IsAlive 检查），将无效元素暂存后延迟删除，避免在 Range 回调内部修改 map 影响遍历。

## 十四、Ctx 变体设计规范

项目中涉及 I/O 操作、阻塞调用或创建 Span 的公共方法，均需提供带 `context.Context` 的 Ctx 变体，以满足链路追踪和超时控制需求。

### 通用模式

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
| **Ctx 变体优先** | 先实现带 Ctx 的完整版本，无参版本只做委托 |
| **nil 降级** | Ctx 变体遇到 `nil` 应降级使用内部默认 ctx |
| **Span 创建** | Ctx 变体内部创建 `tracer.Start(ctx, ...)`，无参版本不重复创建 |

### 构造函数接收 Context

对象在初始化时就需要 context（用于生命周期管理和链路追踪），而不是创建后再通过 SetXxx 修补：

```go
// ✅ 正确：构造函数直接接收 ctx
func NewClient(ctx context.Context, conn *websocket.Conn, uid string, opts ...Option) *Client {
    clientCtx, cancel := context.WithCancel(ctx)
    return &Client{ctx: clientCtx, ctxCancel: cancel}
}

// ❌ 反模式：SetContext 修补方案
func NewClient(conn *websocket.Conn, uid string) *Client {
    ctx, cancel := context.WithCancel(context.Background())
    return &Client{ctx: ctx, ctxCancel: cancel}
}
func (c *Client) SetContext(ctx context.Context) {
    c.ctx = ctx  // ctxCancel 脱钩！指向旧的 context
}
```

**规则**：构造函数第一个参数应为 `ctx context.Context`，内部用 `WithCancel(ctx)` 派生子 context。`ctx` 和 `ctxCancel` 必须始终成对创建，禁止 SetContext。

**上游超时隔离**：长连接场景下，构造函数需用 `context.WithoutCancel` 阻断上游超时传递，避免请求级 ctx 取消导致长连接误退出：

```go
// ✅ 长连接：隔离上游超时，ctx.Done() 仅在显式 Close 时触发
clientCtx := context.WithCancel(context.WithoutCancel(ginCtx))

// ❌ 短期操作：直接派生即可
rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
```

## 十五、Option 配置传播模式

### defaultXxx + apply 统一模式

所有 Options 结构体使用统一的 `defaultXxxOptions()` + `apply()` 模式：

```go
// 1. Options 结构体（内部）
type clientOptions struct {
    writeChSize int
    readChSize  int
}

// 2. Option 函数类型
type ClientOption func(*clientOptions)

// 3. defaultXxxOptions() — 默认值集中管理
func defaultClientOptions() *clientOptions {
    return &clientOptions{
        writeChSize: 1024,
        readChSize:  1024,
    }
}

// 4. apply() — 应用选项
func (o *clientOptions) apply(opts ...ClientOption) {
    for _, opt := range opts {
        opt(o)
    }
}

// 5. 入口函数简洁
func NewClient(ctx context.Context, opts ...ClientOption) *Client {
    o := defaultClientOptions()
    o.apply(opts...)
}
```

### 同包同名函数冲突处理

当同一包内多个类型的 Options（如 `ClientOption` 和 `UpgradeOption`）需要相同的 `With*` 函数名时，包内使用小写前缀、导出使用大写：

```go
// 包内使用的小写 Option 函数
func withClientReadLimit(limit int64) ClientOption {
    return func(o *clientOptions) { o.readLimit = limit }
}

// 导出给外部使用的 UpgradeOption
func WithReadLimit(limit int64) UpgradeOption {
    return func(o *upgradeOptions) { o.readLimit = limit }
}
```

### 规则

| 规则 | 说明 |
|------|------|
| **defaultXxx + apply** | 所有 Options 必须包含 `defaultXxxOptions()`（默认值集中管理）和 `(o *xxxOptions) apply()` 方法 |
| **文件对称命名** | 主文件 `xxx.go` + Options 文件 `xxx_options.go`，成对出现 |
| **命名前缀** | 包内使用的 Option 函数以 `withClient*`/`withServer*` 为前缀（小写）；导出函数以 `With*` 为前缀（大写） |
| **`> 0` 判断** | 零值表示"不设置"或"使用默认值"，option 内部判断 `> 0` 才生效 |
| **条件前置到调用处** | 涉及配置项的条件判断在调用处用 `if o.field` 模式判断，不在被调函数内部做 guard clause |

## 十六、测试文件组织

### 核心原则：一对一映射

每个源文件 `xxx.go` 必须有且仅有一个对应的测试文件 `xxx_test.go`。一个包有多个源文件时，测试文件必须拆分到各自对应的文件中，禁止所有测试堆在一个文件里。

| 源文件 | 测试文件 | 说明 |
|--------|----------|------|
| `nacoscli.go` | `nacoscli_test.go` | 包主入口函数测试 |
| `option.go` | `option_test.go` | 所有 Option 函数测试 |
| `listener.go` | `listener_test.go` | ListenClient 及相关方法测试 |
| `watch.go` | `watch_test.go` | WatchConfig 及重试逻辑测试 |
| `integration_test.go` | `integration_test.go` | 集成测试（`//go:build integration`） |

```go
// ❌ 错误：所有测试堆在一个文件
// nacoscli_test.go（800+ 行）
func TestGetConfig(...) {}
func TestListenClientStart(...) {}
func TestOptionPriority(...) {}
func TestWatchConfigRetry(...) {}

// ✅ 正确：每个源文件对应独立测试文件
// nacoscli_test.go    — GetConfig、NewConfigClient、buildConfigs
// listener_test.go    — Start、Stop、buildOnChange、safeCallHandler
// option_test.go      — WithXxx 函数、负数边界、优先级覆盖
// watch_test.go       — exceededMaxRetries、WatchConfig 错误路径、stop 行为
```

### mock 共享规则

同一包内多个测试文件共享的 mock 结构体（如 `mockConfigClient`），统一放在包主测试文件（`xxx_test.go`）中定义。其他测试文件通过同包访问直接使用，不需要重复定义。

```go
// nacoscli_test.go — 定义 mock（所有测试文件共享）
type mockConfigClient struct {
    getConfigFn    func(param vo.ConfigParam) (string, error)
    listenConfigFn func(params vo.ConfigParam) error
    closeCalled    bool
}

// listener_test.go — 直接使用同包的 mockConfigClient
func TestListenClientStart(t *testing.T) {
    mock := &mockConfigClient{}
    listener := &ListenClient{configClient: mock, ...}
    // ...
}
```

| 规则 | 说明 |
|------|------|
| **mock 定义位置** | 包主测试文件（`xxx_test.go`）中定义，其他文件直接引用 |
| **禁止重复定义** | 同包内不同测试文件不定义相同名称的 mock 结构体 |
| **mock 字段追踪** | mock 需记录调用次数（`called bool`）、传入参数（`lastParam`）、返回值函数 |

### 标准结构

```
xxx.go               # 源文件
xxx_test.go           # 对应测试 + 共享 mock 定义

xxx_options.go        # Options 源文件
xxx_options_test.go   # 对应测试

listener.go           # 功能模块源文件
listener_test.go      # 对应测试

watch.go              # 功能模块源文件
watch_test.go         # 对应测试

test_helpers.go       # 共享测试工具（newXxxPair、skipXxx）
integration_test.go   # 集成测试（//go:build integration）
```

### 单元测试规范

| 规则 | 说明 |
|------|------|
| **一对一映射** | 每个源文件 `xxx.go` 有且仅有一个测试文件 `xxx_test.go` |
| **覆盖全部导出函数** | 每个导出函数至少一个正向测试 + 一个错误/边界测试 |
| **未导出函数通过导出函数覆盖** | `buildOnChange`、`safeCallHandler` 等通过 `Start` 间接测试，或直接测试（同包可访问） |
| **mock 隔离外部依赖** | 依赖外部服务的调用通过 mock 替代，不发起真实网络请求 |
| **辅助方法首字母小写** | 测试辅助函数（`newTestPair`、`mockXxx`）首字母小写，限于包内使用 |
| **并发安全** | 涉及 goroutine 的测试用 `go func()` + channel 或 `sync.WaitGroup` 等待结果 |

### 集成测试规范

集成测试依赖真实外部服务（Nacos、RabbitMQ、Redis 等），必须通过 build tag 隔离，不影响普通 `go test`。

#### 文件与 build tag

```go
// integration_test.go 第一行
//go:build integration

package mypackage
```

运行方式：
```bash
# 普通单元测试（不包含集成测试）
go test -v ./pkg/xxx/

# 包含集成测试
go test -tags=integration -v ./pkg/xxx/
```

#### 环境变量与跳过机制

集成测试必须通过环境变量控制，未设置时自动 `t.Skip`：

```go
// requireNacos 从环境变量读取 Nacos 配置，未设置时跳过。
func requireNacos(t *testing.T) (host string, port int, ...) {
    t.Helper()
    host = os.Getenv("NACOS_IP_ADDR")
    portStr := os.Getenv("NACOS_PORT")
    if host == "" || portStr == "" {
        t.Skip("NACOS_IP_ADDR 或 NACOS_PORT 未设置，跳过集成测试")
    }
    // ...
}

func TestIntegration_GetConfig(t *testing.T) {
    host, port, ... := requireNacos(t)  // 未设置环境变量则自动 skip
    // ...
}
```

#### TestMain 超时保护

依赖外部服务的集成测试包必须实现 `TestMain`，设置总时长上限，防止 SDK goroutine 泄漏导致 `go test` 永远挂起：

```go
func TestMain(m *testing.M) {
    ch := make(chan int, 1)
    go func() {
        ch <- m.Run()
    }()
    select {
    case code := <-ch:
        os.Exit(code)
    case <-time.After(120 * time.Second):
        fmt.Fprintln(os.Stderr, "FATAL: 集成测试超过 120 秒超时")
        os.Exit(1)
    }
}
```

#### 命名规范

| 测试函数名 | 说明 |
|------------|------|
| `TestIntegration_GetConfig` | 集成测试统一前缀 `TestIntegration_` |
| `TestIntegration_PublishAndGetConfig` | CRUD 闭环场景 |
| `TestIntegration_ListenConfigPublishChange` | 监听+发布联动 |
| `TestIntegration_NamingClientRegisterAndDiscover` | 服务注册发现 |

#### 跳过策略

| 场景 | 策略 | 示例 |
|------|------|------|
| 环境变量未设置 | `t.Skip("xxx 未设置")` | `requireNacos` |
| 外部服务不可达 | `t.Skipf("gRPC 不可达: %v", err)` | TCP 检测失败 |
| SDK 操作返回 false | `t.Skipf("RegisterInstance 返回 false，gRPC 未就绪")` | gRPC STARTING 状态 |
| 加密配置无密钥 | `t.Logf(...)` 记录但不 Fatal | Nacos ENC(...) 配置 |

```go
// ✅ 正确：RegisterInstance 失败时优雅跳过
success, err := namingClient.RegisterInstance(...)
if err != nil {
    t.Skipf("RegisterInstance 失败: %v", err)
}
if !success {
    t.Skipf("RegisterInstance 返回 false，gRPC 服务可能未就绪")
}
```

#### 辅助函数规范

| 辅助函数 | 说明 |
|----------|------|
| `requireNacos(t)` | 读取 Nacos 连接环境变量，返回 host/port/group/dataID |
| `requireAuth(t)` | 读取认证环境变量，未设置时 skip |
| `requireGRPCPort(t)` | 读取 gRPC 端口环境变量 |
| `isGRPCReachable(host, port)` | TCP 检测 gRPC 端口可达性（3 秒超时） |
| `testConfigKey()` | 生成唯一临时配置 ID（纳秒时间戳） |
| `publishConfig(...)` | 直接通过 SDK 发布配置（测试辅助，返回 cleanup 函数） |

#### 集成测试覆盖目标

每个集成测试包至少覆盖以下场景：

| 场景类型 | 示例 |
|----------|------|
| **基本 CRUD** | GetConfig、PublishConfig + GetConfig 闭环 |
| **参数变体** | 不同超时、不同认证方式、不同 ServerConfigs |
| **监听与变更** | ListenConfig 启动 + PublishConfig 触发回调 |
| **重试与失败** | WatchConfig maxRetries、不可达地址重试 |
| **服务注册发现** | NamingClient 注册 + SelectInstances 发现 |
| **context 控制** | ctx 取消中断 GetConfig、优雅关闭 |

### 反模式

```go
// ❌ 共享工具散落在各个文件中
// client_test.go 定义了 newTestClientPair
// dispatcher_test.go 又定义了 newTestClientPair（重复）

// ✅ 统一放在 test_helpers.go
func newTestClientPair(t testing.TB, opts ...Option) (*Client, *websocket.Conn)
func newMockBackend(bufSize int) *mockBackend
```

```go
// ❌ 所有测试堆在一个文件（listener、watch、option 测试混在 nacoscli_test.go）

// ✅ 每个源文件对应独立测试文件
// nacoscli_test.go    — 包主入口函数测试 + 共享 mock
// listener_test.go    — ListenClient 测试
// option_test.go      — Option 函数测试
// watch_test.go       — WatchConfig 测试
// integration_test.go — 集成测试（//go:build integration）
```

```go
// ❌ 单元测试和集成测试混在一起
// rabbitmq_backend_test.go 既有构造函数测试又有真实 RabbitMQ 集成测试

// ✅ 分离
// rabbitmq_backend_test.go              — 单元测试（mock 模拟）
// rabbitmq_backend_integration_test.go   — 集成测试（需真实服务，默认跳过）
```

```go
// ❌ 集成测试没有超时保护，SDK goroutine 泄漏导致 go test 永远挂起

// ✅ TestMain 设置 120 秒超时
func TestMain(m *testing.M) { ... }
```

```go
// ❌ RegisterInstance 返回 false 时直接 require.True 导致测试失败
success, _ := namingClient.RegisterInstance(...)
require.True(t, success)  // gRPC STARTING 时永远 false → 测试失败

// ✅ 返回 false 时优雅跳过
if !success {
    t.Skipf("RegisterInstance 返回 false，gRPC 服务可能未就绪")
}
```

### 端到端测试（代码生成类）

代码生成类命令（如 `generate/` 下各子命令）的测试策略：

| 原则 | 说明 |
|------|------|
| **只写端到端测试** | 测试必须执行完整的代码生成流程，生成真实文件到 `testdata/` 目录供查看 |
| **不写 mock 测试** | 不对内部方法单独写单元测试 |
| **真实数据用例** | 使用贴近生产环境的真实配置数据 |
| **输出到 testdata** | 生成的文件复制到 `testdata/<test-name>/` 目录 |
| **预清理** | 每次运行前清理旧的临时目录和 testdata 目录 |
| **验证内容** | 验证生成文件包含预期结构体和字段名 |

## 十七、goroutine 必须添加 panic recover（两种策略）

任何项目中启动 goroutine 必须添加 `defer recover()`，防止 panic 导致整个进程崩溃。根据 goroutine 的生命周期类型，采用不同恢复策略：

### 策略一：终止型 — recover 后退出，确保清理

适用于有明确生命周期的 goroutine（读写循环、心跳协程等），recover 后必须执行 `WaitGroup.Done()` 等清理操作，防止父协程永久阻塞：

```go
func (c *Client) msgFromWsToCh() {
    defer func() {
        if r := recover(); r != nil {
            c.recordReadErr(fmt.Errorf("panic: %v", r))
            logger.WarnWithCtx(c.clientCtx, "ws msgFromWsToCh panic recovered",
                logger.String("uid", c.uid),
                logger.Any("panic", r),
            )
        }
        close(c.readCh)
        c.readWg.Done()
    }()
    // ... 循环体
}
```

### 策略二：循环型 — recover 后继续执行

适用于持续运行的消息消费、WorkerPool 等循环 goroutine。recover 后**必须继续循环**，防止单条坏消息导致整个后台协程宕机：

```go
// workerLoop：每任务独立 recover，单次 panic 不影响下一个任务
func (dd *DistributedDispatcher) workerLoop() {
    defer dd.workerWg.Done()
    for task := range dd.workerCh {
        func() {  // 闭包隔离，panic 不影响外层循环
            defer func() {
                if r := recover(); r != nil {
                    logger.WarnWithCtx(context.Background(), "worker panic recovered",
                        logger.Any("panic", r),
                    )
                }
            }()
            task()
        }()
    }
}

// receiveOnce：提取为独立方法，通过 named return 控制循环
func (dd *DistributedDispatcher) receiveOnce(ctx context.Context, msgCh <-chan *PubSubMessage) (done bool) {
    defer func() {
        if r := recover(); r != nil {
            logger.WarnWithCtx(ctx, "receiveOnce panic recovered",
                logger.Any("panic", r),
            )
            done = false // 返回 false 让上层继续循环
        }
    }()
    // select...
}
```

### 注意事项

| 要点 | 说明 |
|------|------|
| **recover 必须在 defer 中** | 只有 defer 中的 recover 才能捕获 panic，其他位置无效 |
| **defer 必须在 goroutine 入口** | 确保任何路径进入 goroutine 后都能被保护 |
| **日志记录 panic 上下文** | 包含 uid/remoteAddr 等标识信息，方便问题追踪 |
| **不使用 `logger.Warn`** | 带 `Ctx` 版本 `logger.WarnWithCtx` 才能输出 request_id |
| **循环型 recover 包裹在闭包内** | 闭包隔离使单次 panic 不影响外层 for 循环的继续执行 |

### 判断标准

| goroutine 类型 | 示例 | recover 策略 |
|----------------|------|-------------|
| 读写/心跳循环 | `msgFromWsToCh`、`StartHeartbeat` | 终止型：recover → 清理 → 退出 |
| 消息消费循环 | `receiveLoop`、worker 池、订阅协程 | 循环型：recover → 继续循环 |
| 一次性任务 | `go func()` 执行单个异步操作 | 终止型：recover → 记录日志 |

## 十八、中文注释与日志规范

项目所有可读文本（注释、日志、错误消息）统一使用中文，确保团队协作和运维排障时无障碍阅读。

### 18.1 日志消息必须使用中文

所有 `logger.*WithCtx`、`logger.Get().Warn/Info/Error` 调用中的日志消息字符串必须使用中文。标签前缀（如 `[config reload]`、`[nacos watch]`）保留英文。

```go
// ❌ 错误：英文日志消息
logger.WarnWithCtx(ctx, "[nacos watch] connection lost, retrying in 3s")
logger.InfoWithCtx(ctx, "[config reload] database pool config updated",
    logger.Int("maxIdleConns", newMysql.MaxIdleConns))

// ✅ 正确：中文日志消息，标签前缀保留英文
logger.WarnWithCtx(ctx, "[nacos watch] 连接断开，3秒后重试")
logger.InfoWithCtx(ctx, "[config reload] 数据库连接池配置已更新",
    logger.Int("maxIdleConns", newMysql.MaxIdleConns))
```

### 18.2 错误消息必须使用中文

`fmt.Errorf`、`errors.New` 返回的错误消息字符串必须使用中文，方便排障定位。

```go
// ❌ 错误：英文错误消息
return nil, errors.New("field 'Group' cannot be empty")
return nil, fmt.Errorf("failed to get config from Nacos: %w", err)

// ✅ 正确：中文错误消息
return nil, errors.New("字段 'Group' 不能为空")
return nil, fmt.Errorf("从 Nacos 获取配置失败: %w", err)
```

### 18.3 详细中文注释风格

对于需要清晰表达业务逻辑的方法，采用详细中文注释风格，让阅读者一眼看懂每个步骤的目的和注意事项。

```go
// setGroupPath 为指定的路由分组添加一组中间件处理函数。
// 如果多次调用同一 groupPath，中间件会以追加方式累积（通常用于不同模块叠加功能）。
// 注意：本函数不负责去重，也不处理中间件顺序冲突，调用方需自行保证逻辑正确性。
func (c *middlewareConfig) setGroupPath(groupPath string, handlers ...gin.HandlerFunc) {
    // 1. 空路径或空处理程序直接返回，避免无效存储
    if groupPath == "" || len(handlers) == 0 {
        return
    }
    // 2. 规范化路径：
    //    - 使用 path.Clean 去除多余的斜杠和相对路径（如 /api/../v1 -> /v1）
    //    - 确保以 / 开头，否则补全
    cleaned := path.Clean(groupPath)
    // ...
}
```

| 要素 | 说明 |
|------|------|
| **函数级注释** | 方法名开头 + 一句话功能 + 业务场景 + 注意事项（`注意：`） |
| **步骤编号** | 方法体内用 `// 1.` `// 2.` 等编号，清晰表达执行流程 |
| **why 注释** | 不仅写“做什么”，还写“为什么这么做” |
| **边界说明** | 各分支、异常情况、设计意图都在注释中说明 |

### 适用场景

| 方法类型 | 推荐注释风格 | 示例 |
|----------|-------------|------|
| 业务逻辑类 | 详细中文注释 + 步骤编号 + why 说明 | `setGroupPath`、`reloadDatabasePool` |
| 简单工具方法 | 一行函数级注释即可 | `FormatDataID` |
| 接口/构造函数 | 函数级注释 + 参数/返回值说明 | `NewClient`、`WatchConfig` |


## 十九、README 文档编写规范

README 是包的门面文档，必须让使用者在 30 秒内找到自己需要的用法。所有 `pkg/` 下的包必须有 README.md。

**参考模板**：`pkg/nacoscli/README.md`（覆盖所有规范要点）。

### 核心原则：场景驱动

README 以**使用场景**组织，不以 API 列表组织。每个场景包含：适用/不适用说明 + 完整代码示例 + 内部行为描述。

### README 标准结构

1. `# 包名` + 一句话功能描述
2. `## 架构概览`（复杂包必写）：文件目录树 + 核心设计原则 bullet 列表
3. `## 使用场景选择`：每个场景用 `### 场景N：标题` + `**适用场景**` + 完整代码 + `**内部行为**`（编号步骤）+ 可选 `**不适用**`/`**注意**`，场景之间用 `---` 分隔
4. `## 核心结构体/类型说明`：表格（字段/类型/必填/说明）
5. `## Option 列表`：表格（Option/说明/默认值/适用 API）
6. `## API 速查`：每个导出函数独立小节（签名 + 要点列表 + 注意事项）
7. `## 错误处理`：表格（场景/行为）
8. `## OpenTelemetry 集成`（如有）
9. `## 集成测试`：环境变量 + 运行命令
10. `## 参考文档`（可选）

### 架构概览规范

文件数 ≥ 4 的包必须写架构概览，包含：

1. **文件目录树**：列出每个文件的职责（一行注释），帮助读者快速定位代码
2. **核心设计原则**：3~6 条 bullet，描述包的关键设计决策和约束

### 使用场景规范

每个场景必须包含以下元素（按顺序）：

| 元素 | 必选 | 说明 |
|------|------|------|
| `### 场景N：标题（关键 API）` | 是 | 标题点明使用场景和核心 API |
| `**适用场景**：` | 是 | 一句话说明什么情况下用这个场景 |
| 完整代码示例 | 是 | 必须是可直接复制运行的代码（含 package/main/import） |
| `**内部行为**：` | 是 | 编号步骤说明内部执行流程 |
| `**不适用**：` | 否 | 说明什么情况下不应使用此场景 |
| `**注意**：` | 否 | 额外注意事项（如全局状态、性能影响等） |
| `---` 分隔线 | 是 | 每个场景之间必须用 `---` 分隔 |

### 结构体/类型说明规范

用表格列出所有导出结构体的字段，必须包含必填列：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `Group` | `string` | **是** | 配置分组 |
| `NamespaceID` | `string` | 否 | 命名空间 ID |

必填字段说明和特殊语义用 `>` 引用块补充。

### Option 列表规范

表格必须包含 `适用 API` 列，标明每个 Option 可用于哪些函数：

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithTimeoutMs(ms)` | 请求超时（毫秒） | `5000` | 全部 |
| `WithMaxRetries(n)` | 最大重试次数 | `0` | `WatchConfig` |

### API 速查规范

每个导出函数**独立小节**，不合并为一个表格。每个小节包含：

1. 函数签名（代码块）
2. 要点列表（`-` 开头），说明行为、参数语义、注意事项
3. 特殊说明用 **注意** 加粗标记

### 错误处理规范

用表格列出所有可能的错误场景，让读者一眼看清错误边界：

| 场景 | 行为 |
|------|------|
| `GetConfig(nil)` | 返回 `ErrNilParams` 错误 |
| Nacos 服务器不可达 | 返回 SDK 错误 |

### 代码示例规范

代码示例必须干净、可运行，禁止出现空白赋值或 `_ = xxx`：

```go
// ❌ 错误：代码示例中出现空白赋值
format1, data1, err := client.GetConfig(context.Background(), params1)
format2, data2, err := client.GetConfig(context.Background(), params2)
_ = format1
_ = format2
_ = data1
_ = data2

// ✅ 正确：示例只展示核心用法，忽略返回值用 err 变量
format1, data1, err := client.GetConfig(context.Background(), params1)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("format: %s, data length: %d\n", format1, len(data1))
```

### 易混淆 API 区分

当包内存在功能相近但类型/用途不同的 API 时，必须在 README 中用 `**注意**` 块明确区分：

```go
// ❌ 错误：读者不知道 NewConfigClient 和 GetConfig 的区别

// ✅ 正确：在 API 速查中明确说明
// ### NewConfigClient — 创建可复用的配置客户端
//
// - 返回 `*Client`，提供 `GetConfig(ctx, params)` 和 `Close()` 方法
// - 用于需要多次读取不同配置文件的场景
// - **注意**：`WithGetTimeout` 仅对便捷函数 `GetConfig` 生效，`NewConfigClient` 创建的
//   `Client.GetConfig(ctx, params)` 完全忽略 `getTimeout`，超时由调用方传入的 `ctx` 控制
```

| 场景 | 处理方式 |
|------|----------|
| 两个 API 返回不同类型 | 用 `**注意**` 说明类型差异和使用场景 |
| 一个 API 有隐含限制 | 用 `**注意**` 标注限制条件 |
| Option 对不同 API 效果不同 | 用 `**注意**` 说明各 API 的生效范围 |

### 超时说明独立小节

当包涉及多种超时机制（SDK 超时、整体超时、context 超时）时，必须在 API 速查中单独列出超时说明：

```markdown
**超时说明**：
- `WithGetTimeout`：控制便捷函数 `GetConfig` 整体执行时间（创建+拉取+关闭），通过 `context.WithTimeout` 实现
- `WithTimeoutMs`：控制 Nacos SDK 单次 HTTP 请求超时（毫秒），默认 5000ms
- `Client.GetConfig(ctx, params)` 中的 `ctx`：仅用于调用前取消检查，无法中断 SDK 内部正在进行的网络请求
```

### 引用块用于提示和警告

生产环境注意事项、重要限制、安全提示用 Markdown `>` 引用块：

```markdown
> **生产环境提示**：默认无限重试 + 固定 5 秒延迟意味着 Nacos 长期不可达时会持续刷 Warn 日志。
> 建议在生产环境显式设置 `WithMaxRetries`（如 10~20 次），避免日志膨胀。
```

### 参数校验说明

涉及 `valid()` 校验或格式归一化的包，必须在结构体说明后单独列出校验规则：

```markdown
**参数校验规则**：`Group`、`DataID`、`Format` 为空时返回错误；`Format` 不在支持列表时返回错误。
`valid()` 返回归一化后的 Format，**不修改**原始 Params 结构体。
```

### 重试语义说明

涉及重试机制的 API，必须在场景说明中明确重试时机、计数规则和停止条件：

```markdown
**重试语义**：WatchConfig 只在两个时机重试：
- `NewListenClient` 创建失败（Nacos 地址不可达、认证失败等）
- `ListenConfig` 注册失败（SDK 内部状态异常、缓存目录不可写等）

一旦注册成功，连接的健康维护由 Nacos SDK 内部长轮询负责，WatchConfig 不再介入。

**重试计数规则**：
- `retries` 仅在创建失败或注册失败时递增
- `retries >= maxRetries` 时记录 Error 日志并退出 goroutine
- `maxRetries = 0`（默认）时无限重试
```

### 规则

| 规则 | 说明 |
|------|------|
| **场景优先** | 按使用场景组织，不按函数名列表 |
| **代码示例完整** | 每个示例必须是可直接复制运行的完整代码（含 package/main/import） |
| **代码示例干净** | 禁止 `_ = xxx` 空白赋值，示例展示正确用法 |
| **内部行为** | 每个场景必须说明内部行为（编号步骤），不能只列签名 |
| **表格化** | Option 列表、结构体字段、错误场景用表格呈现 |
| **中文撰写** | 所有说明文字使用中文，代码标识符保留英文 |
| **分隔线** | 每个场景之间、主要章节之间用 `---` 分隔 |
| **架构概览** | 文件数 ≥ 4 的包必须写架构概览（目录树 + 设计原则） |
| **API 速查** | 每个导出函数独立小节（签名 + 要点列表），不合并为一个表格 |
| **Option 适用 API** | Option 表格必须有 `适用 API` 列 |
| **不重复代码注释** | README 不复制源码注释内容，补充使用视角说明 |
| **集成测试说明** | 依赖外部服务的包必须说明环境变量和运行命令 |
| **易混淆 API 区分** | 功能相近但类型/用途不同的 API 用 `**注意**` 块明确区分 |
| **超时说明** | 涉及多种超时机制时单独列出超时说明小节 |
| **引用块提示** | 生产环境提示、重要限制用 `>` 引用块 |
| **参数校验** | 涉及 valid() 校验的包单独列出校验规则 |
| **重试语义** | 涉及重试的 API 明确重试时机、计数规则和停止条件 |

### 反模式

- 只列函数签名，没有使用说明
- 代码示例不完整（缺少 package/import）
- 代码示例中出现 `_ = xxx` 空白赋值
- 没有场景分隔线，所有场景粘在一起难以区分
- Option 表格缺少 `适用 API` 列
- API 速查合并为一个大表格，无法放详细说明
- 没有说明适用场景，读者不知道该用哪个
- 两个功能相近的 API 没有区分说明，读者不知道选哪个
- 涉及多种超时但没有统一说明，读者容易混淆
- 有重试机制但没有说明重试时机和停止条件
