# Logger 日志包

基于 [uber-go/zap](https://github.com/uber-go/zap) 封装的高性能结构化日志库，支持异步写入、文件切割、日志路由、SLS 上报、链路追踪集成。

## 功能特性

| 特性 | 说明 |
|------|------|
| 结构化日志 | JSON / Console 两种格式，字段类型安全 |
| Context 传递 | 自动提取 request_id、trace_id、span_id、caller_func |
| 异步写入 | BufferedWriteSyncer，默认 8MB 缓冲区，性能提升 155% |
| 文件切割 | lumberjack / rotatelogs，支持按大小或按天切割 |
| 日志路由 | 按模块/级别路由到不同文件（如 order.log、payment.log） |
| SLS 上报 | 阿里云 SLS 异步上报，支持 Warn 级别过滤 |
| 优雅关闭 | Closer 接口 + Shutdown 统一清理 |
| 运行时指标 | Prometheus Collector，暴露丢弃数/活跃 logger 数 |
| 并发安全 | sync.Once + atomic + RWMutex，全局单例 |

## 快速开始

### 基础用法

```go
import "github.com/18721889353/sunshine/pkg/logger"

// 初始化（终端输出，debug 级别）
logger.Init()

// 带 context 的日志（自动注入 request_id、trace_id）
ctx = context.WithValue(ctx, logger.ContextKeyForRequestID(), "req-001")
logger.InfoWithCtx(ctx, "用户登录", logger.String("user_id", "123"))
```

### 生产配置

```go
logger.Init(
    logger.WithLevel("info"),
    logger.WithFormat("json"),
    logger.WithAsync(true),
    logger.WithSave(true,
        logger.WithFileName("logs/app.log"),
        logger.WithFileMaxSize(100),
        logger.WithFileMaxBackups(30),
        logger.WithFileMaxAge(7),
        logger.WithFileIsCompression(true),
        logger.WithSaveDay(true),
    ),
)
```

### 日志路由（按模块分文件）

```go
logger.Init(
    logger.WithSave(true, ...),
    logger.WithRoutes([]*logger.RouteConfig{
        {
            Module:   "order",
            Filename: "logs/order/order.log",
            MaxSize:  200,
            IsAsync:  true,
        },
        {
            Module:   "payment",
            Filename: "logs/payment/payment.log",
            MaxSize:  50,
            IsAsync:  true,
        },
    }),
)

// 使用：自动路由到 order.log
logger.ModuleInfoWithCtx(ctx, "order", "订单创建", logger.String("id", "001"))
```

### SLS 上报

```go
hook, err := logger.NewSLSHook(&logger.SLSConfig{
    Endpoint:        "cn-shanghai.log.aliyuncs.com",
    AccessKeyID:     "your-key",
    AccessKeySecret: "your-secret",
    ProjectName:     "your-project",
    LogStoreName:    "your-logstore",
})
if err != nil {
    panic(err)
}
defer hook.Close()

// 注册 Hook（只上报 Warn 及以上）
logger.Init(
    logger.WithCustomHooksWithCtx(func(ctx context.Context, entry zapcore.Entry, fields []logger.Field) error {
        if entry.Level < zapcore.WarnLevel {
            return nil // Info/Debug 不上报 SLS
        }
        return hook.Hook(ctx, entry, fields)
    }),
)
```

### 优雅关闭

```go
// 注册 SLS Hook 到 logger（Shutdown 时自动关闭）
logger.RegisterCloser(hook)

// 应用退出时
defer logger.Shutdown(context.Background())
```

### Prometheus 指标

```go
// 注册后 /metrics 端点自动包含 logger 指标
logger.RegisterPrometheus()
```

暴露的指标：

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `logger_dropped_entries_total` | Counter | 异步缓冲区满时丢弃的日志条数 |
| `logger_router_active_loggers` | Gauge | 路由器中活跃的 logger 数量 |

## API 一览

### 日志方法

| 方法 | 说明 |
|------|------|
| `DebugWithCtx(ctx, msg, fields...)` | 调试级别 |
| `InfoWithCtx(ctx, msg, fields...)` | 信息级别 |
| `WarnWithCtx(ctx, msg, fields...)` | 警告级别 |
| `ErrorWithCtx(ctx, msg, fields...)` | 错误级别 |
| `PanicWithCtx(ctx, msg, fields...)` | Panic 级别 |
| `FatalWithCtx(ctx, msg, fields...)` | Fatal 级别 |
| `ModuleInfoWithCtx(ctx, module, msg, fields...)` | 按模块记录 |
| `ModuleErrorWithCtx(ctx, module, msg, fields...)` | 按模块记录 |

### 字段函数

| 函数 | 说明 |
|------|------|
| `String(key, val)` | 字符串 |
| `Int(key, val)` | 整数 |
| `Int64(key, val)` | 64 位整数 |
| `Float64(key, val)` | 浮点数 |
| `Bool(key, val)` | 布尔 |
| `Err(err)` | 错误（nil 时自动跳过） |
| `Duration(key, val)` | 时间间隔 |
| `Any(key, val)` | 任意类型 |

### 配置函数

| 函数 | 说明 |
|------|------|
| `WithLevel(level)` | 日志级别：debug/info/warn/error |
| `WithFormat(format)` | 输出格式：json/console |
| `WithSave(isSave, opts...)` | 是否保存到文件 |
| `WithAsync(enabled)` | 是否异步写入 |
| `WithAsyncBufferSize(size)` | 异步缓冲区大小（字节） |
| `WithAsyncFlushInterval(d)` | 异步刷新间隔 |
| `WithRoutes(routes)` | 日志路由配置 |
| `WithCustomHooksWithCtx(hooks...)` | 带 Context 的自定义钩子 |

### 文件配置

| 函数 | 说明 |
|------|------|
| `WithFileName(name)` | 文件名 |
| `WithFileMaxSize(mb)` | 最大文件大小（MB） |
| `WithFileMaxBackups(n)` | 最大备份数 |
| `WithFileMaxAge(days)` | 最大保留天数 |
| `WithFileIsCompression(on)` | 是否压缩 |
| `WithSaveDay(on)` | 是否按天保存 |
| `WithNoPrint(on)` | 禁止终端输出 |

### 工具函数

| 函数 | 说明 |
|------|------|
| `IsDebugEnabled()` | 检查 DEBUG 是否开启 |
| `IsInfoEnabled()` | 检查 INFO 是否开启 |
| `GetMetrics()` | 获取运行时指标快照 |
| `RecordDrop()` | 记录一次日志丢弃 |
| `RegisterPrometheus()` | 注册 Prometheus 指标 |
| `RegisterCloser(c)` | 注册 Shutdown 时关闭的资源 |
| `Shutdown(ctx)` | 优雅关闭日志系统 |
| `Sync()` | 刷新缓冲区 |
| `Get()` | 获取底层 zap.Logger |

## Context 字段自动提取

| 字段 | 来源 | 说明 |
|------|------|------|
| `request_id` | `ContextKeyRequestID` | 请求唯一标识 |
| `trace_id` | OpenTelemetry | 链路追踪 ID |
| `span_id` | OpenTelemetry | 当前 span ID |
| `caller_func` | `WithCallerFunc()` | 调用者方法名 |

## 并发安全

```
所有 goroutine 调用路径:

  InfoWithCtx(ctx, "msg")
    → getDefaultLogger()
      → checkNil()           ← initOnce.Do 保证只执行一次
      → return defaultCallerLogger  ← 全局唯一实例
```

- `sync.Once` 保护初始化
- `atomic.Int32/Int64` 保护计数器
- `sync.RWMutex` 保护路由器 map
- 全程 2 个 logger 实例，不随请求增长

## 内存模型

```
启动时: Init() → 创建全局实例（固定）
   ↓
运行期: 每次日志调用 → 临时 []Field → GC 回收（恒定）
         SLS Producer 缓冲 → 有界（512MB 上限）
         路由器 map → 固定大小
   ↓
关闭时: Shutdown() → flush 所有缓冲 → 释放资源
```

内存不会持续上涨，每次调用产生的临时对象在 GC 正常回收范围内。

## Benchmark 参考

| 场景 | ns/op | allocs/op |
|------|-------|-----------|
| InfoWithCtx (异步) | ~2500 | 5 |
| 高并发 (100 goroutine) | ~3000 | 5 |
| 路由查找 | ~800 | 2 |

## 目录结构

```
pkg/logger/
├── logger.go           # 核心初始化、单例管理、build 辅助
├── method.go           # 日志方法、Shutdown、Closer、Metrics
├── metrics.go          # Prometheus Collector 集成
├── option.go           # 配置选项（Functional Options 模式）
├── type.go             # Field 类型封装
├── router.go           # 日志路由、Context 字段提取
├── sls_hook.go         # 阿里云 SLS Hook 实现
├── grpcLogger.go       # gRPC 日志桥接
├── benchmark_test.go   # 性能基准测试
├── method_ctx_test.go  # 功能测试（含 table-driven）
└── logs/               # 日志输出目录
    ├── order/
    └── payment/
```
