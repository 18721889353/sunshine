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

// 初始化（默认行为：保存日志到 logs/app.log，JSON 格式，info 级别，异步写入）
logger.Init()

// 终端输出（开发环境常用）：显式关闭文件保存
logger.Init(logger.WithSave(false), logger.WithFormat("console"))

// 带 context 的日志（request_id 自动注入；trace_id/span_id 需 ctx 携带真实 OTel SpanContext）
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

> **就绪校验不写测试日志**：`NewSLSHook` 启动时的 `verifyProducerState` 只校验 producer 句柄/状态，
> 不会向 LogStore 写入 `health-check` 测试日志（历史上会污染生产数据，且因 `SendLog` 异步属假验证，已移除）。
>
> **级别门控说明**：带 ctx 的钩子（`customHooksWithCtx`）现与 zap core 上的 `customHooks` 行为一致，
> 仅当该日志级别被 `Core().Enabled(level)` 启用时才会执行——即 `WithLevel("info")` 下 `DebugWithCtx` 不会触发 SLS 上报，
> 也不会产生字段提取分配。上述 `entry.Level < zapcore.WarnLevel` 的包裹仍是「只上报 Warn 及以上」所需的最小过滤。

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
      → checkNil()                          ← initOnce.Do 保证只执行一次
      → return defaultCallerLoggerPtr.Load()  ← 原子读取全局唯一实例
```

- `sync.Once` 保护兜底初始化
- `atomic.Pointer` 发布默认 logger 与钩子快照：`Init` 写侧 `Store`、日志读侧 `Load`，
  使「运行期重复 `Init` 与并发日志」不再构成内存模型层面的数据竞争
- `atomic.Int32/Int64` 保护计数器
- `sync.RWMutex` 保护路由器 map
- 全程 2 个 logger 实例，不随请求增长

> **`Init` 启动期单次调用约定**：虽然各包级变量已原子化，但 `Init` 内多次 `Store` 并非单一原子事务，
> 理论上可观察到「钩子已更新而默认 logger 尚未发布」的中间态。请在服务启动、开始处理流量之前完成 `Init`，
> 不要在流量期反复调用。

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

> 不提供固定参考值：旧表既无测量机器/Go 版本/benchtime，历史 `BenchmarkHighConcurrency` 还存在 `b.N/concurrency` 假基准（现已改 `b.RunParallel`）。
> 真实数据请以目标环境实测为准：`go test -bench=. -benchtime=1s -count=5`。
> 纯 CPU 基准已使用丢弃输出（`WithNoPrint(true)`）隔离磁盘 IO；含文件/网络 IO 的基准（`BenchmarkSLSHook` 等）在未配置 SLS 凭据时会自动 Skip。
>
> **集成测试**：`TestSLSIntegration`/`TestMixedLogging` 会创建真实 SLS Producer（网络 IO），已收敛到 `//go:build integration`；
> 默认 `go test ./pkg/logger/` 为纯本地（CPU/内存）测试。跑集成用例：`go test -tags integration ./pkg/logger/`。

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
├── main_test.go        # 包级测试入口 TestMain（基准模式条件清理）+ isBenchmarkRun/cleanupBenchmarkFiles
├── benchmark_test.go   # 性能基准测试（纯 CPU 基准用 WithNoPrint 丢盘；路由写入基准含文件 IO 已在注释标注）
├── method_ctx_test.go  # 功能测试（含 table-driven）
├── caller_ext_test.go  # 外部测试包 logger_test：验证钩子 caller 定位到用户调用点（两条路径）
├── sls_integration_test.go # `//go:build integration`：TestSLSIntegration/TestMixedLogging（创真实 Producer、含网络 IO）
├── testutil_test.go    # 测试公共工具（getEnv）
├── grpcLogger_test.go  # grpcLogger.V 语义回归
├── sls_hook_test.go    # SLS 纯函数单测 + Fuzz（validateSLSConfig/slsFieldValue 等）
├── router_test.go      # 路由/Context 纯函数单测（mergeRouteConfig/extractContextFields 等）
├── option_test.go      # Option 校验分支单测（WithLevel/WithFormat/值域防护）
├── CHANGELOG.md        # 版本变更日志（含 SLS 浮点序列化 Bug、级别门控、caller 定位的修复记录）
└── logs/               # 日志输出目录
    ├── order/
    └── payment/
```
