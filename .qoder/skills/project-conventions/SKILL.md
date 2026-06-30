---
name: project-conventions
description: Documents Sunshine framework's shared lint compliance rules, Go development conventions, error handling patterns, and lessons learned from real project fixes. Use when modifying any Go file, fixing lint errors, or reviewing code changes — this is the canonical reference for all project-wide coding standards.
---

# Sunshine 框架开发公约

## 核心原则

- **禁止 `//nolint`**：不允许用 `//nolint` 注释绕过 lint 检查，所有问题必须通过修复代码解决
- **禁止修改 `.golangci.yml`**：lint 配置是项目级统一标准，不为单点问题添加例外配置
- **`make ci-lint` 必须通过**：提交前运行 `make ci-lint`（`gofmt -s -w .` + `golangci-lint run ./...`）

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

## 十一、OpenTelemetry span 操作规范

`span.SetAttributes` 是直接调用，`_ = span.SetAttributes(...)` 会被 `check-blank` 标记：

```go
// ❌ 错误
_ = span.SetAttributes(attribute.String("k", "v"))

// ✅ 正确
span.SetAttributes(attribute.String("k", "v"))
```

## 十二、经验教训总结

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

## 十三、原子计数器优先使用 atomic.Int32/Int64 类型

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

## 十四、sync.Map 操作规范 — 计数漂移保护

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

## 十四、sync.Map 操作规范 — 计数漂移保护

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
    if loaded {  // 只有实际删除了才回滚
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
| `RegisterCtx` | `Store(client, struct{}{})` + `Add(1)`，回滚用 `Delete`+`Add(-1)`。新注册 client 不会被其他路径并发删除（`IsAlive()=true`），可不加 loaded 判断 |
| `UnregisterCtx` | 必须用 `LoadAndDelete` + `if loaded`，可能与其他清理路径并发 |
| `CleanupDeadConns` | Range 回调内必须用 `LoadAndDelete` + `if loaded { cleaned++ }`，再统一 `Add(-cleaned)` |
| 消息投递中的僵尸清理 | 已用 `LoadAndDelete` + `if loaded`，正确 |

### 规则

| 规则 | 说明 |
|------|------|
| **并发删除用 LoadAndDelete** | 可能被多条路径并发删除的 map，必须用 `LoadAndDelete` + `if loaded` 保护计数器 |
| **ctx 回滚一致** | 回滚操作的条件必须与删除操作一致 |
| **Range 中删除用 CAS** | `sync.Map.Range` 回调内删除当前 key，使用 `LoadAndDelete` 确保只看当前 key |
