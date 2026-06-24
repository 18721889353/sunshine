---
name: gows-package
description: Guides modification and extension of the gows WebSocket package, including OpenTelemetry tracing integration, request_id propagation, and code generation templates. Use when working with gows/, protoc-gen-go-gin templates, or WebSocket-related code.
---

# gows 包开发指南

## 包架构

```
pkg/gows/
├── upgrader.go    # HTTP → WebSocket 升级入口
├── client.go      # 客户端连接封装（Channel驱动写入模型）
├── conn.go        # Ping/Pong 协议级心跳保活
├── dispatcher.go  # 全局连接注册中心（广播/私信）
└── auth.go        # JWT token 解析
```

关键设计：
- **WriteJSON** 非阻塞写入（Channel + writeLoop 协程）
- **ReadMessage** 同步阻塞读取，配合 PongHandler 刷新 ReadDeadline
- **Close** 原子 CAS 保证幂等，关闭顺序：钩子 → cancel ctx → close closeCh → close writeCh → wg.Wait → conn.Close
- **forceCloseConn** 与 Close 分离，避免 writeLoop 自锁

## 链路追踪规范

所有公有方法需添加 OpenTelemetry span，规则如下：

```go
tracer := otel.Tracer("gows")
_, span := tracer.Start(ctx, "ws.<operation>", trace.WithSpanKind(trace.SpanKindInternal))
defer span.End()
span.SetAttributes(
    attribute.String("ws.uid", c.uid),
    attribute.String("ws.remote_addr", c.remoteAddr),
    requestIDAttr(ctx),
)
```

### Span 命名规范

| 操作 | Span 名称 | SpanKind | 位置 |
|------|-----------|----------|------|
| HTTP→WS 升级 | `ws.upgrade` | SpanKindServer | upgrader.go |
| 写入消息 | `ws.write` | SpanKindInternal | client.go |
| 读取消息 | `ws.read` | SpanKindInternal | client.go |
| 心跳保活 | `ws.heartbeat` | SpanKindInternal | conn.go |
| 广播消息 | `ws.broadcast` | SpanKindInternal | dispatcher.go |
| 条件广播 | `ws.broadcast_filter` | SpanKindInternal | dispatcher.go |
| 发送给指定用户 | `ws.send_to_uid` | SpanKindInternal | dispatcher.go |
| 解析JWT | `ws.parse_token` | SpanKindInternal | auth.go |

### request_id 传播

- 从 context 中通过 `logger.ContextKeyRequestID` 提取
- 使用 `requestIDAttr(ctx)` 辅助函数（定义在 client.go）
- 方法签名必须带 `ctx context.Context` 参数，命名加 `Ctx` 后缀
- 示例：`BroadcastCtx(ctx, v)`、`ParseTokenCtx(ctx, token)`

## 方法命名约定

所有需要 context 的公有方法使用 `Ctx` 后缀：

```go
// 正确
func (d *Dispatcher) BroadcastCtx(ctx context.Context, v any)
func (d *Dispatcher) SendToUIDCtx(ctx context.Context, uid string, v any)

// 禁止 - 不带 context 的旧方法已删除
func (d *Dispatcher) Broadcast(v any)  // ❌ 已删除
```

## 代码生成模板

`cmd/protoc-gen-go-gin/internal/generate/router/template.go` 中 WebSocket handler 模板结构：

```
1. ctx := c.Request.Context() + wrapCtxFn/WrapCtx  // 先注入 request_id
2. gows.ParseTokenCtx(ctx, token)                    // 解析 JWT（ctx 已带 request_id）
3. gows.Upgrade(c, ...)                              // 升级 WS
4. ctx = context.WithValue(ctx, WsConnKey, client)   // 添加连接
5. r.iLogic.{{.Name}}(ctx, req)                      // 调用业务逻辑
```

必须的 import：
```go
"encoding/json"  // WebSocket JSON 消息
"github.com/18721889353/sunshine/pkg/gows"  // WebSocket 包
```

## OpenTelemetry 导入

添加追踪时需导入：
```go
"go.opentelemetry.io/otel"
"go.opentelemetry.io/otel/attribute"
"go.opentelemetry.io/otel/codes"
"go.opentelemetry.io/otel/trace"
```
