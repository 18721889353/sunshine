## tracer

Tracer library wrapped in [go.opentelemetry.io/otel](https://github.com/open-telemetry/opentelemetry-go).

支持 **OTLP gRPC** 分布式链路追踪。

<br>

## Example of use

### OTLP gRPC

初始化 trace，使用 OTLP gRPC 协议上报：

```go
import "github.com/18721889353/sunshine/pkg/tracer"

func initTrace() {
    // 推荐：使用 OTLP gRPC 协议（如阿里云 SLS链路追踪）
    tracer.InitWithOTLP(
        "your-service-name",           // 服务名称
        "dev",                          // 运行环境
        "v1.0.0",                       // 版本号
        1.0,                            // 采样率（1.0=全量采样）
        "cn-shanghai.tracing-api.aliyuncs.com:80", // OTLP collector 端点
        tracer.WithInsecure(true),      // 是否禁用 TLS
        tracer.WithHeaders(map[string]string{      // 额外 gRPC 头部（如认证 token）
            "Authorization": "Bearer xxx",
        }),
        tracer.WithTimeout(10*time.Second), // 导出超时
    )
}
```

如需自定义 BatchSpanProcessor 参数（队列大小、批次大小、超时等）：

```go
    tracer.InitWithOTLPBatch(
        "your-service-name", "dev", "v1.0.0",
        1.0,                    // 采样率
        "endpoint:port",        // OTLP collector 端点
        2048,                   // MaxQueueSize（0=默认值）
        512,                    // MaxExportBatchSize（0=默认值）
        5*time.Second,          // BatchTimeout（0=默认值）
        30*time.Second,         // ExportTimeout（0=默认值）
        tracer.WithInsecure(true),
    )
```

### Console / File 输出

```go
    // 输出到终端
    exporter := tracer.NewConsoleExporter()

    // 输出到文件
    exporter, f, err := tracer.NewFileExporter("trace.json")
    defer f.Close()
```

### 手动初始化 Resource

```go
    resource := tracer.NewResource(
        tracer.WithServiceName("your-service-name"),
        tracer.WithEnvironment("dev"),
        tracer.WithServiceVersion("v1.0.0"),
    )
    tracer.Init(exporter, resource) // 采样率默认 1.0
    // tracer.Init(exporter, resource, 0.5) // 采样率 50%
```

<br>

## Create a Span

```go
    _, span := otel.Tracer(serviceName).Start(
        ctx,
        spanName,
        trace.WithAttributes(attribute.String("foo", "bar")), // 自定义属性
    )
    defer span.End()
```

使用 `tracer.NewSpan` 简化创建：

```go
    tags := map[string]interface{}{"foo": "bar"}
    _, span := tracer.NewSpan(ctx, "spanName", tags)
    defer span.End()
```

<br>

## API Reference

| 函数 | 说明 |
|------|------|
| `InitWithOTLP(appName, env, version, samplingRate, endpoint, ...OTLPOption)` | OTLP gRPC 初始化 |
| `InitWithOTLPBatch(appName, env, version, samplingRate, endpoint, maxQueueSize, maxExportBatchSize, batchTimeout, exportTimeout, ...OTLPOption)` | OTLP gRPC 初始化（自定义 Batch 参数） |
| `Init(exporter, resource, fractions...)` | 通用初始化 |
| `Close(ctx)` | 关闭 tracer，确保 Span 全部上报 |
| `NewOTLPExporter(...OTLPOption)` | 创建 OTLP gRPC exporter |
| `NewConsoleExporter()` | 创建 Console exporter |
| `NewFileExporter(filename)` | 创建 File exporter |
| `NewResource(...ResourceOption)` | 创建 Resource |
| `NewSpan(ctx, spanName, tags)` | 创建 Span |

### OTLP Options

| Option | 说明 |
|--------|------|
| `WithEndpoint(endpoint)` | OTLP collector 端点，格式 `host:port` |
| `WithInsecure(insecure)` | 是否禁用 TLS |
| `WithHeaders(headers)` | 额外 gRPC 头部 `map[string]string` |
| `WithTimeout(timeout)` | 导出超时时间 |

<br>

documents https://opentelemetry.io/docs/instrumentation/go/

support OpenTelemetry in other libraries https://opentelemetry.io/registry/?language=go&component=instrumentation
