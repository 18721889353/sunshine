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

## 九、OpenTelemetry span 操作规范

`span.SetAttributes` 是直接调用，`_ = span.SetAttributes(...)` 会被 `check-blank` 标记：

```go
// ❌ 错误
_ = span.SetAttributes(attribute.String("k", "v"))

// ✅ 正确
span.SetAttributes(attribute.String("k", "v"))
```

## 十、经验教训总结

### 10.1 `SetupPongHandler` 中 SetReadDeadline 错误处理

`SetupPongHandler` 中如果 `SetReadDeadline` 失败，说明连接已关闭，应及早返回：

```go
func SetupPongHandler(conn *websocket.Conn, interval, pongTimeout time.Duration) {
    readDeadline := interval + 2*pongTimeout
    if err := conn.SetReadDeadline(time.Now().Add(readDeadline)); err != nil {
        return // 连接已关闭，无需继续
    }
    conn.SetPongHandler(func(string) error {
        if err := conn.SetReadDeadline(time.Now().Add(readDeadline)); err != nil {
            return nil // 连接可能已关闭，忽略
        }
        return nil
    })
}
```

### 10.2 日志上下文传递

`logger.Info(...)` 和 `logger.Warn(...)` 不带 context，无法输出 request_id。必须用带 `Ctx` 的版本：

```go
// ❌ 错误：丢失 request_id
logger.Warn("write failed", logger.Err(err))

// ✅ 正确：保留 request_id
logger.WarnWithCtx(ctx, "write failed", logger.Err(err))
```

### 10.3 广播类函数中 WriteJSON 错误

批量发送场景（Broadcast/SendToUID）中，单个客户端写入失败不应中断其他客户端：

```go
// ✅ 错误只记录日志，继续处理其他客户端
if err := client.WriteJSON(v); err != nil {
    logger.WarnWithCtx(ctx, "write to client failed",
        logger.String("uid", client.uid),
        logger.Err(err),
    )
}
```

### 10.4 升级失败时清理资源

WebSocket 升级失败（如 Dispatcher 注册满）时，需关闭连接并记录日志：

```go
if err := o.dispatcher.Register(client); err != nil {
    if closeErr := client.Close(); closeErr != nil {
        logger.WarnWithCtx(c.Request.Context(), "close after register failed",
            logger.Err(closeErr),
        )
    }
    span.SetStatus(codes.Error, err.Error())
    return nil, fmt.Errorf("register: %w", err)
}
```
