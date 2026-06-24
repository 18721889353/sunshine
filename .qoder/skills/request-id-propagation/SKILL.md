---
name: request-id-propagation
description: Add OpenTelemetry request_id propagation to Sunshine framework utility packages (gozip, goexcel, goupload, goemail, gosms, gohttp). Use when adding or fixing request_id attributes in span.SetAttributes, or when integrating OpenTelemetry tracing with request_id across utility packages.
---

# Request ID Propagation for Sunshine Utility Packages

## Overview

在 Sunshine 框架的 utility 包中，`request_id` 存储在 context 中，需要通过 `logger.ContextKeyRequestID` 提取并注入到 OpenTelemetry span 属性中。每个包有自己的 `requestIDAttr` 辅助函数和特定的 attribute key 命名。

## 工作模式

### 1. 添加 `requestIDAttr` 辅助函数

在每个包的公共文件（如 `client.go`）中添加以下函数：

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

| 包 | 文件 | Attribute Key |
|---|---|---|
| `gozip` | `zip.go` | `gozip.request_id` |
| `goexcel` | `excel.go` | `excel.request_id` |
| `goupload` | `uploader.go` / `local_uploader.go` | `upload.request_id` |
| `goemail` | `client.go` / `tencent_ses.go` / `aliyun_dm.go` / `smtp.go` | `email.request_id` |
| `gosms` | `client.go` / `tencent_sms.go` / `aliyun_sms.go` | `sms.request_id` |
| `gohttp` | `gohttp.go` | `http.request_id` |

### 3. 需要修改的代码模式

#### a. `span.SetAttributes(...)` — 追加 `requestIDAttr(ctx)`

在已有的 `span.SetAttributes` 调用的最后一个属性后面追加：

```go
span.SetAttributes(
    attribute.String("...", "..."),
    // ... existing attributes ...
    requestIDAttr(ctx),  // <-- 追加
)
```

在 `trace.WithAttributes(...)` 中同理：

```go
trace.WithAttributes(
    attribute.String("...", "..."),
    // ... existing attributes ...
    requestIDAttr(ctx),  // <-- 追加
)
```

#### b. `logger` 导入

如果包中还没有导入 `logger`，需要在 import block 中添加：

```go
import (
    // ...
    "github.com/18721889353/sunshine/pkg/logger"
    // ...
)
```

注意：`gohttp/gohttp.go` 没有 `logger` 导入，需要额外添加。

### 4. 排查步骤

1. **定位公共文件**：找到包中定义公共类型的文件（通常为 `client.go` 或主文件），在该文件中添加 `requestIDAttr` 辅助函数
2. **搜索所有 `span.SetAttributes`**：检查包内所有文件，确认每个 `span.SetAttributes` 是否都包含了 `requestIDAttr(ctx)`
3. **搜索 `trace.WithAttributes`**：检查使用 `trace.WithAttributes` 创建 span 时的属性，也需添加 `requestIDAttr(ctx)`
4. **检查 logger context**：确认 `logger.InfoWithCtx(ctx, ...)` 等调用使用的是 `ctx` 而非 `context.Background()`
5. **检查编译**：修改后确保 `go build ./pkg/{name}/...` 无错误

### 5. 注意事项

- `gohttp` 的 span 创建在 `setupMiddlewares()` 的 `OnBeforeRequest` 回调中，使用 `trace.WithAttributes` 方式。注意这里的 `ctx` 是 `req.Context()` 而非 `extractedCtx`，request_id 在原始 `ctx` 中
- 同一包内多个文件共享 `requestIDAttr` 函数，只需在公共文件中定义一次
- attribute key 使用 `{pkg}.request_id` 格式，便于在 tracing 系统中按包名过滤

> 关于项目公共 lint 规则和开发公约，请参见 [project-conventions](../project-conventions/SKILL.md)

## Examples

### gozip 修改示例

在 `zip.go` 中添加 `requestIDAttr`，然后在 4 处 `span.SetAttributes` 追加 `requestIDAttr(ctx)`，同时修复 3 处 `logger.Info(context.Background(), ...)` → `logger.InfoWithCtx(ctx, ...)`。

### gosms 修改示例

在 `client.go` 中添加 `requestIDAttr` 和 `ValidatePhoneNumber` 的 request_id 注入，然后在 `tencent_sms.go` 和 `aliyun_sms.go` 中各 4 处 span 属性追加 `requestIDAttr(ctx)`。
