---
name: request-id-propagation
description: Add OpenTelemetry request_id propagation to Sunshine framework utility packages (gozip, goexcel, goupload, goemail, gosms, gohttp). Use when adding or fixing request_id attributes in span.SetAttributes, or when integrating OpenTelemetry tracing with request_id across utility packages.
---

# request_id 上下文传播规范

## 概述

request_id 是 Sunshine 框架中跨服务、跨层级的链路追踪标识。它在 HTTP/gRPC 入口处生成或提取，通过 `context.Context` 逐层传递到各个 utility 包（如文件处理、邮件、短信、上传等），最终写入 OpenTelemetry span attribute，实现端到端追踪。

### 传播路径

```
HTTP/gRPC 入口 ─── ctx ───> Service 层 ─── ctx ───> utility 包（gozip/goemail/...）
                              │                         │
                              ▼                         ▼
                     OTel Span 属性                 OTel Span 属性
                   (request_id 注入)             (request_id 注入)
```

---

## 核心实现模式

### 1. `requestIDAttr` 辅助函数

每个涉及 Span 操作的包（或同一包内共享的公共文件）中，定义如下辅助函数：

```go
// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
    if ctx != nil {
        if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
            return attribute.String("{pkg}.request_id", reqID)
        }
    }
    return attribute.String("{pkg}.request_id", "")
}
```

### 2. Attribute Key 命名规则

| 模式 | 格式 | 示例 |
|------|------|------|
| 标准 | `{包简称}.request_id` | `zip.request_id`、`email.request_id` |
| 入口型 | `{协议}.request_id` | `http.request_id`、`rpc.request_id` |
| 多实现包 | `{领域}.request_id` | `upload.request_id`（含 local/oss 实现） |
| 无包名场景 | `request_id` | 仅在明确无歧义时使用 |

**规则**：使用小写 + 点号分隔，避免与 Go 导出字段命名冲突，便于 tracing 系统中按前缀过滤。

### 3. 注入到 Span 的方式

#### 方式 A：`span.SetAttributes` 追加

```go
span.SetAttributes(
    attribute.String("pkg.some_attr", "value"),
    // ... 其他属性 ...
    requestIDAttr(ctx),  // 始终追加在最后
)
```

#### 方式 B：`trace.WithAttributes` 创建时注入

```go
ctx, span := tracer.Start(ctx, "pkg.operation",
    trace.WithAttributes(
        attribute.String("pkg.some_attr", "value"),
        // ... 其他属性 ...
        requestIDAttr(ctx),
    ),
)
defer span.End()
```

### 4. 日志上下文传递

request_id 同时也通过 `logger.WarnWithCtx` / `logger.InfoWithCtx` 写入日志：

```go
// ✅ 正确：带 ctx 的日志会输出 request_id
logger.WarnWithCtx(ctx, "operation failed", logger.Err(err))

// ❌ 错误：不带 ctx 丢失 request_id
logger.Warn("operation failed", logger.Err(err))

// ❌ 错误：不能传 context.Background()，丢失链路上下文
logger.InfoWithCtx(context.Background(), "message")
```

---

## 排查与实施步骤

当需要为某个包添加 request_id 传播时，按以下顺序执行：

### Step 1：定位公共文件

找到包中定义公共类型的文件（通常为 `client.go` 或主文件），在该文件中添加 `requestIDAttr` 辅助函数。

### Step 2：检查依赖

确认包是否已导入 `logger`，如缺失则添加：

```go
import (
    // ... 其他导入 ...
    "github.com/18721889353/sunshine/pkg/logger"
)
```

### Step 3：搜索所有 Span 创建点

| 搜索目标 | 说明 |
|----------|------|
| `span.SetAttributes` | 已有 span 创建后的属性设置，追加 `requestIDAttr(ctx)` |
| `trace.WithAttributes` | 创建 span 时的属性参数，追加 `requestIDAttr(ctx)` |
| `tracer.Start` | 确认每个 span 都有对应的 attribute 注入点 |

### Step 4：修复日志调用

| 搜索目标 | 修复方式 |
|----------|----------|
| `logger.Info(ctx` 无参数 | 已经是 info 的正确用法 |
| `logger.Info\("` 字符串参数开头 | 改为 `InfoWithCtx` |
| `logger.Warn\("` 字符串参数开头 | 改为 `WarnWithCtx` |
| `context.Background()` 传入日志 | 替换为正确的业务 ctx |
| `logger.InfoWithCtx(context.Background()` | 替换为正确的业务 ctx |

### Step 5：验证编译

```bash
go build ./pkg/{name}/...
```

---

## 常见陷阱

| 陷阱 | 说明 | 解决 |
|------|------|------|
| **ctx 来源错误** | span 中使用 `context.Background()` 而非请求 ctx | 确认 ctx 来自请求链路，而非新创建 |
| **attribute key 不统一** | 各包 key 格式不一致，无法统一过滤 | 遵循 `{pkg}.request_id` 格式 |
| **requestIDAttr 重复定义** | 同一包多个文件各自定义了同名函数 | 只在一个公共文件中定义一次 |
| **logger 未用 Ctx 版本** | 日志丢失 request_id | 全局搜索 `logger.Info(` / `logger.Warn(` 替换为 Ctx 版本 |
| **span.SetAttributes 被 `_ =` 包裹** | lint 规则 `check-blank` 标记 | 直接调用，不加 `_ =` |

> 项目公共 lint 规则和开发公约，请参见 [project-conventions](../project-conventions/SKILL.md)

---

## 示例参考

以下包已按此规范完成改造，可作为实施参考：

| 包 | 辅助函数位置 | attribute key |
|---|---|---|
| `pkg/gozip` | `zip.go` | `zip.request_id` |
| `pkg/goexcel` | `excel.go` | `excel.request_id` |
| `pkg/goupload` | `uploader.go` | `upload.request_id` |
| `pkg/goemail` | `client.go` | `email.request_id` |
| `pkg/gosms` | `client.go` | `sms.request_id` |
| `pkg/gohttp` | `gohttp.go` | `http.request_id` |
