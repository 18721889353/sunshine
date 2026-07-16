---
name: project-conventions
description: Documents Sunshine framework's shared lint compliance rules, Go development conventions, error handling patterns, and lessons learned from real project fixes. Use when modifying any Go file, fixing lint errors, or reviewing code changes — this is the canonical reference for all project-wide coding standards.
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
| 十六 | 测试文件组织 | 标准结构 + 规则 + 反模式 + 端到端测试策略 |
| 十七 | goroutine panic recover 策略 | 终止型退出清理 + 循环型继续执行 |

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

### 标准结构

```
xxx.go               # 源文件
xxx_test.go           # 对应测试

xxx_options.go        # Options 源文件
xxx_options_test.go   # 对应测试

test_helpers.go       # 共享测试工具（newXxxPair、mockXxx、skipXxx）
integration_test.go   # 总测：组合多个模块的集成场景
xxx_integration_test.go # 需外部依赖的集成测试
```

### 规则

| 规则 | 说明 |
|------|------|
| **一对一映射** | 每个源文件 `xxx.go` 有且仅有一个测试文件 `xxx_test.go` |
| **命名对称** | Options 文件对应 `xxx_options_test.go`，集成测试后缀 `_integration_test.go` |
| **共享工具集中** | `test_helpers.go` 存放跨文件共享的工具函数、mock 实现、跳过条件 |
| **单元/集成分离** | 依赖外部服务的测试放 `_integration_test.go`，默认跳过（环境变量控制） |
| **总测覆盖链路** | `integration_test.go` 覆盖完整业务流程的多个模块组合场景 |
| **辅助方法首字母小写** | 测试辅助函数（`newTestPair`、`mockXxx`）首字母小写，限于包内使用 |

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
// ❌ 单元测试和集成测试混在一起
// rabbitmq_backend_test.go 既有构造函数测试又有真实 RabbitMQ 集成测试

// ✅ 分离
// rabbitmq_backend_test.go              — 单元测试（mock 模拟）
// rabbitmq_backend_integration_test.go   — 集成测试（需真实服务，默认跳过）

### 测试策略：仅保留端到端测试

代码生成类命令（如 `generate/` 下各子命令）的测试策略：

| 原则 | 说明 |
|------|------|
| **只写端到端测试** | 测试必须执行完整的代码生成流程（`generateCode()` / `convertToGoFile()`），生成真实文件到 `testdata/` 目录供查看，不写单方法测试 |
| **不写 mock 测试** | 不对 `addFields`、`getYAMLFile`、`saveFile` 等内部方法单独写单元测试或 mock 测试 |
| **真实数据用例** | 使用贴近生产环境的真实配置数据（多层 YAML 结构、真实表名字段等），不造假数据 |
| **输出到 testdata** | 生成的文件复制到 `testdata/<test-name>/` 目录下，与 `cache_test.go` 的 `testdata/cache-gen/` 模式一致 |
| **预清理** | 每次运行前清理旧的临时目录和 testdata 目录，避免残留干扰 |
| **验证内容** | 验证生成文件包含预期结构体和字段名，验证占位符已被替换，验证标记代码已被清理 |

```go
// ✅ 正确：端到端测试，生成文件到 testdata
func TestXxx_GenerateToTestdata(t *testing.T) {
    tmpOut := filepath.Join(os.TempDir(), "sunshine-test", "xxx-gen")
    os.RemoveAll(tmpOut)
    testDataDir := filepath.Join("testdata", "xxx-gen")
    os.RemoveAll(testDataDir)

    // 执行完整生成流程
    outPath, err := gen.generateCode()

    // 复制到 testdata 供查看
    targetFile := filepath.Join(testDataDir, relPath)
    copyFile(t, generatedFile, targetFile)

    // 验证内容
    content := string(data)
    if !strings.Contains(content, "expectedType") { t.Error(...) }
}
```

**判断标准**：测试需要生成可查看的实物文件（`.go`/`.proto` 等）到 `testdata/` 目录并做内容验证，才是端到端测试。仅调内部方法断言返回值不是端到端测试。

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
```