# tracer

封装 [opentelemetry.io/otel](https://github.com/open-telemetry/opentelemetry-go) 链路追踪库，提供 TracerProvider 初始化、动态采样率控制、OTLP 导出及 Span 创建等能力。

## 架构概览

```
tracer/
├── tracerProvider.go    # 核心：Init/InitWithOTLP/InitWithOTLPBatch/Close、installProvider 锁内原子替换、registerGlobal
├── otlp_options.go      # OTLP Option 函数：WithEndpoint/WithInsecure/WithHeaders/WithTimeout/WithErrorHandler
├── resource.go          # Resource 构建：WithServiceName/WithEnvironment/WithServiceVersion/WithAttributes
├── dynamic_sampler.go   # 动态采样器：atomic 指针热更新、clampRate 范围钳位、ShouldSample 概率采样
├── console.go           # Console/File exporter：本地调试输出
├── integration_test.go  # 集成测试（需 OTEL_EXPORTER_OTLP_TRACES_ENDPOINT 环境变量）
└── README.md
```

**核心设计原则：**

- **全局状态一致性**：`tp` 与 `otel.SetTracerProvider()` 在同一 `tpMu` 临界区内完成，杜绝并发 Init 导致全局 provider 指向已关闭旧 provider
- **ParentBased 采样**：分布式链路始终继承父级采样决策，通过 `trace.ParentBased(newSampler)` 包装动态采样器
- **动态热更新**：`SetSamplingRate` 通过 `atomic.Pointer` 替换采样器实例，无需重启，立即生效
- **Exporter 生命周期**：复用同一 exporter 不会被误关；不同 exporter 被替换时旧的自动 Shutdown
- **库代码安全**：所有公开函数返回 error，不 panic
- **OTLP 自动协议选择**：endpoint 以 `http://` 或 `https://` 开头 → HTTP 协议；其他格式（`host:port`）→ gRPC 协议

---

## 使用场景选择

### 场景一：OTLP 初始化（推荐）

**适用场景**：生产环境，Span 通过 OTLP 协议上报到 Collector（如阿里云链路追踪、Jaeger、Zipkin）。

```go
package main

import (
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/tracer"
)

func main() {
	// gRPC 协议（host:port 格式）
	if err := tracer.InitWithOTLP(
		"your-service-name",
		"dev",
		"v1.0.0",
		1.0,
		"cn-shanghai.tracing-api.aliyuncs.com:80",
		tracer.WithInsecure(true),
		tracer.WithHeaders(map[string]string{
			"Authorization": "Bearer xxx",
		}),
		tracer.WithTimeout(10*time.Second),
	); err != nil {
		log.Fatal(err)
	}
	defer tracer.Close(context.Background())

	// HTTP 协议（以 http:// 或 https:// 开头，包含路径）
	if err := tracer.InitWithOTLP(
		"your-service-name", "dev", "v1.0.0",
		1.0,
		"http://tracing-analysis-dc-sh.aliyuncs.com/xxx/api/otlp/traces",
		tracer.WithInsecure(true),
	); err != nil {
		log.Fatal(err)
	}
}
```

**内部行为**：

1. 创建 OTLP exporter（根据 endpoint 格式自动选择 HTTP/gRPC 传输）
2. 构建 `TracerProvider`：`BatchSpanProcessor` + `Resource` + `ParentBased(DynamicSampler)`
3. 锁内原子替换旧 `TracerProvider` 并注册为全局 OTel Provider
4. 旧 Provider 异步关闭（不阻塞当前调用）

**不适用**：需要自定义 exporter 或精细控制 Batch 参数的场景（应使用 `Init` 或 `InitWithOTLPBatch`）。

---

### 场景二：自定义 Batch 参数

**适用场景**：需要精细控制导出批次大小和频率（如高吞吐场景调大队列、低延迟场景缩短批次超时）。

```go
package main

import (
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/tracer"
)

func main() {
	if err := tracer.InitWithOTLPBatch(
		"your-service-name", "dev", "v1.0.0",
		1.0,
		"endpoint:port",
		2048,           // MaxQueueSize（0=SDK 默认值）
		512,            // MaxExportBatchSize（0=SDK 默认值）
		5*time.Second,  // BatchTimeout（0=SDK 默认值）
		30*time.Second, // ExportTimeout（0=SDK 默认值）
		tracer.WithInsecure(true),
	); err != nil {
		log.Fatal(err)
	}
	defer tracer.Close(context.Background())
}
```

**内部行为**：与场景一相同，但使用 `BatchSpanProcessorOption` 覆盖默认的队列大小、批次大小、超时等参数。传 0 表示使用 SDK 默认值。

---

### 场景三：Console / File 输出

**适用场景**：本地开发调试，Span 输出到终端或文件，无需 Collector。

```go
package main

import (
	"log"
	"context"

	"github.com/18721889353/sunshine/pkg/tracer"
)

func main() {
	// 输出到终端
	exporter, err := tracer.NewConsoleExporter()
	if err != nil {
		log.Fatal(err)
	}

	// 或输出到文件
	// exporter, f, err := tracer.NewFileExporter("trace.json")
	// defer f.Close()

	if err := tracer.Init(exporter, tracer.NewResource()); err != nil {
		log.Fatal(err)
	}
	defer tracer.Close(context.Background())
}
```

**内部行为**：创建 Console/File exporter 后，通过 `Init` 传入，与 OTLP 场景共用同一套 TracerProvider 管理逻辑。

---

### 场景四：手动创建 Resource

**适用场景**：需要自定义服务属性（如多实例部署时区分实例 ID）。

```go
package main

import (
	"log"
	"context"

	"github.com/18721889353/sunshine/pkg/tracer"
)

func main() {
	resource := tracer.NewResource(
		tracer.WithServiceName("your-service-name"),
		tracer.WithEnvironment("dev"),
		tracer.WithServiceVersion("v1.0.0"),
		tracer.WithAttributes(map[string]string{
			"instance.id": "pod-001",
		}),
	)
	exporter, err := tracer.NewConsoleExporter()
	if err != nil {
		log.Fatal(err)
	}
	if err := tracer.Init(exporter, resource); err != nil {
		log.Fatal(err)
	}
	// 自定义采样率：tracer.Init(exporter, resource, 0.5) // 50% 采样
	defer tracer.Close(context.Background())
}
```

**内部行为**：`NewResource` 仅构建 Resource 对象，不涉及 TracerProvider。采样率通过 `Init` 的可变参数 `fractions` 传入，默认 1.0（全量采样）。

---

## 创建 Span

### 原生 OTel API

```go
_, span := otel.Tracer(serviceName).Start(
    ctx,
    spanName,
    trace.WithAttributes(attribute.String("foo", "bar")),
)
defer span.End()
```

### tracer.NewSpan 简化创建

```go
tags := map[string]interface{}{"foo": "bar"}
_, span := tracer.NewSpan(ctx, "spanName", tags)
defer span.End()
```

**注意**：`NewSpan` 内部调用 `SetTraceName`，会修改进程级 traceName 全局状态。多 service 场景建议使用原生 `otel.Tracer(name)` 替代。

---

## ResourceOption 列表

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithServiceName(name)` | 服务名称 | `"demo-service"` |
| `WithServiceVersion(version)` | 服务版本 | `"v0.0.0"` |
| `WithEnvironment(env)` | 运行环境（dev/staging/prod） | `"dev"` |
| `WithAttributes(attrs)` | 自定义属性 `map[string]string` | 空 |

---

## OTLP Option 列表

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithEndpoint(endpoint)` | OTLP collector 端点。gRPC: `host:port`；HTTP: `http://host/path` | `"localhost:4317"` | `InitWithOTLP`、`InitWithOTLPBatch`、`NewOTLPExporter` |
| `WithInsecure(insecure)` | `true` 禁用 TLS，`false` 启用 TLS | `true` | `InitWithOTLP`、`InitWithOTLPBatch`、`NewOTLPExporter` |
| `WithHeaders(headers)` | 额外请求头部 `map[string]string`（如认证 token） | 空 | `InitWithOTLP`、`InitWithOTLPBatch`、`NewOTLPExporter` |
| `WithTimeout(timeout)` | 导出超时时间 | `10s` | `InitWithOTLP`、`InitWithOTLPBatch`、`NewOTLPExporter` |
| `WithErrorHandler(h)` | 自定义 OTel ErrorHandler，未设则用默认 stderr 输出 | `nil` | `InitWithOTLP`、`InitWithOTLPBatch` |

---

## API 速查

### InitWithOTLP — OTLP 初始化（推荐）

```go
func InitWithOTLP(appName, appEnv, appVersion string, samplingRate float64, otlpEndpoint string, opts ...OTLPOption) error
```

- 创建 OTLP exporter → 构建 TracerProvider → 注册全局 Provider，一步完成
- `samplingRate` 范围 `0~1`，超出自动钳位；`0` 表示全丢弃，`1` 表示全量采样
- `otlpEndpoint` 以 `http://`/`https://` 开头走 HTTP 协议，其他格式走 gRPC 协议
- 返回 error：exporter 创建失败时返回错误并自动清理已创建资源

---

### InitWithOTLPBatch — OTLP 初始化（自定义 Batch 参数）

```go
func InitWithOTLPBatch(appName, appEnv, appVersion string, samplingRate float64, otlpEndpoint string, maxQueueSize, maxExportBatchSize int, batchTimeout, exportTimeout time.Duration, opts ...OTLPOption) error
```

- 与 `InitWithOTLP` 相同，但额外支持自定义 `BatchSpanProcessor` 参数
- `maxQueueSize`、`maxExportBatchSize`、`batchTimeout`、`exportTimeout` 传 `0` 使用 SDK 默认值
- 适用于高吞吐（调大队列）或低延迟（缩短批次超时）场景

---

### Init — 通用初始化（自定义 exporter）

```go
func Init(exporter trace.SpanExporter, res *resource.Resource, fractions ...float64) error
```

- 传入自定义 exporter（Console/File/自实现），与 OTLP 场景共用同一套 TracerProvider 管理
- `res` 为 nil 时自动调用 `NewResource()` 使用默认值
- `fractions` 可变参数，取第一个有效值作为采样率，默认 `1.0`
- 返回 error：`exporter` 为 nil 时返回错误

---

### Close — 优雅关闭

```go
func Close(ctx context.Context) error
```

- 刷新待导出 Span → 关闭 BatchSpanProcessor → 关闭 exporter → 重置 traceName
- `ctx` 控制 `TracerProvider.Shutdown` 的总超时
- exporter 关闭使用独立 5 秒超时（避免 exporter 卡死阻塞调用方）
- 幂等：重复调用安全，第二次调用返回 nil
- 并发安全：内部使用 `atomic.Bool` 保证只执行一次

---

### NewOTLPExporter — 创建 OTLP exporter

```go
func NewOTLPExporter(opts ...OTLPOption) (*otlptrace.Exporter, error)
```

- 仅创建 exporter，不初始化 TracerProvider
- 需要调用方自行管理 exporter 生命周期
- 适用于需要精细控制 exporter 创建和销毁时机的场景

---

### NewConsoleExporter — 创建 Console exporter

```go
func NewConsoleExporter() (trace.SpanExporter, error)
```

- 输出到终端，用于本地调试
- 配合 `Init` 使用

---

### NewFileExporter — 创建 File exporter

```go
func NewFileExporter(filename string) (trace.SpanExporter, *os.File, error)
```

- 输出到 JSON 文件，返回 exporter 和文件句柄
- 调用方需负责 `file.Close()`
- 配合 `Init` 使用

---

### NewResource — 创建 Resource

```go
func NewResource(opts ...ResourceOption) *resource.Resource
```

- 构建 OTel Resource 对象，未设字段使用默认值
- 仅构建 Resource，不涉及 TracerProvider

---

### NewSpan — 创建 Span（简化）

```go
func NewSpan(ctx context.Context, spanName string, tags map[string]interface{}) (context.Context, trace.Span)
```

- 简化 Span 创建，自动关联当前 traceName 并设置标签
- 内部调用 `SetTraceName`，会修改进程级全局状态
- 多 service 场景建议使用原生 `otel.Tracer(name).Start(ctx, spanName)` 替代

---

### SetTraceName — 设置服务追踪名称

```go
func SetTraceName(name string)
```

- 设置进程级 traceName，影响后续所有 `NewSpan` 创建的 Span 所属服务名称
- `Close` 后自动重置为 `"unknown"`
- 多 service 场景建议使用 `otel.Tracer(name)` 替代

---

### SetSamplingRate — 动态修改采样率

```go
func SetSamplingRate(rate float64)
```

- 动态修改采样率（`0~1`），通过 `atomic.Pointer` 热替换采样器实例
- 立即生效，无需重启 TracerProvider
- 范围外值自动钳位：`≤0` → `0`，`≥1` → `1`

---

### GetSamplingRate — 获取当前采样率

```go
func GetSamplingRate() float64
```

- 返回当前采样率，通过 `atomic.Pointer.Load` 读取
- 未初始化时返回 `0`

---

## 错误处理

| 场景 | 行为 |
|------|------|
| `Init`/`InitWithOTLP` 传入 nil exporter | 返回 `error("tracer: exporter 不能为 nil")` |
| `NewOTLPExporter` endpoint 不可达 | 返回 error（连接异步建立，通常不阻塞） |
| `Close` 时 exporter Shutdown 超时 | 5 秒后强制放弃，返回 Shutdown error |
| 并发调用 `Init` | 安全：锁内原子替换 TracerProvider，旧 provider 异步关闭 |
| 重复调用 `Init` | 新 exporter 替换旧的，旧 exporter 被自动 Shutdown（复用同一 exporter 除外） |

---

## 集成测试

集成测试需要真实 OTLP endpoint，通过环境变量控制：

```bash
# 设置 OTLP 端点（参考 .env 文件）
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT="http://..."

# 运行集成测试
go test -tags=integration -v ./pkg/tracer/...
```

---

## 参考文档

- [OpenTelemetry Go SDK 文档](https://opentelemetry.io/docs/instrumentation/go/)
- [OpenTelemetry Go 集成库](https://opentelemetry.io/registry/?language=go&component=instrumentation)
