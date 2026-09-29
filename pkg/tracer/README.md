# tracer

封装 [opentelemetry.io/otel](https://github.com/open-telemetry/opentelemetry-go) 链路追踪库，解决「服务初始化 TracerProvider、动态调采样率、把 Span 上报到 OTLP Collector」的问题，并提供 map 标签式简化 Span 创建。

## 架构概览

```
pkg/tracer/
├── tracerProvider.go    # 核心：Init/InitWithOTLP/InitWithOTLPBatch/Close、BatchConfig、installProvider 锁内原子替换、registerGlobal
├── otlp_options.go      # OTLP Option：WithEndpoint/WithInsecure/WithHeaders/WithTimeout/WithErrorHandler/WithPropagator/WithGlobalRegistration/WithProtocol、协议与 TLS 自动推断
├── resource.go          # Resource 构建：WithServiceName/WithServiceVersion/WithEnvironment/WithAttributes
├── dynamic_sampler.go   # 动态采样器：atomic.Uint64 热更新采样率、clampRate 钳位、ShouldSample 概率采样
├── span.go              # Span 创建：NewSpan（map 标签）、SetTraceName/getTraceName（进程级追踪名）
├── console.go           # Console/File exporter：本地调试输出（NewConsoleExporter/NewFileExporter）
├── *_test.go            # 单元测试（源文件一对一映射）
├── benchmark_test.go    # 性能回归基线（mock 口径，不含网络 RTT）
├── fuzz_test.go         # 模糊测试（守护 clampRate/isHTTPEndpoint/NewSpan 不变量）
├── integration_test.go  # 集成测试（//go:build integration，需真实 OTLP 端点）
└── README.md
```

**核心设计原则：**

- **全局状态一致性**：`tp` 赋值与 `otel.SetTracerProvider()` 在同一 `tpMu` 临界区内完成，杜绝并发 Init 导致全局 provider 指向已关闭的旧 provider
- **全局状态治理**：首次注册时保存 OTel 全局现场（provider/propagator/handler），`Close` 时恢复——不悬空已关闭实例、不永久覆盖其他库的全局配置；`WithGlobalRegistration(false)` 可完全不触碰全局状态
- **ParentBased 采样**：分布式链路始终继承父级采样决策，用 `trace.ParentBased(dynamicSampler)` 包装动态采样器
- **动态热更新**：采样率以 `atomic.Uint64`（Float64bits 编码）存储在采样器实例内，`SetSamplingRate` 原地更新、无需重建 TracerProvider
- **Exporter 生命周期**：Init 接管 exporter 生命周期；SDK 级联关闭被 `ownedExporter` 拦截，底层 exporter 恰好关闭一次，复用同一 exporter 不会被误关
- **安全默认**：TLS 默认启用，仅 `http://` 端点（或显式 `WithInsecure(true)`）走明文，杜绝 https 链路被静默降级
- **库代码安全**：所有公开初始化函数返回 error 不 panic（唯一例外：`GetProvider` 在未初始化时 panic，属编程错误快速失败；需要安全获取用 `Provider()`）
- **OTLP 自动协议选择**：endpoint 以 `http://`/`https://` 开头 → HTTP 协议（无路径自动补 `/v1/traces`）；`host:port` 格式 → gRPC 协议；歧义场景用 `WithProtocol` 显式指定

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| TracerProvider 初始化 / 关闭、采样率热更新 | 是 | 本包 |
| OTLP exporter 创建（HTTP/gRPC 自动选择） | 是 | 本包 |
| Span 打点辅助（map 标签 → OTel attributes） | 是 | 本包 |
| 业务埋点、HTTP/RPC 中间件自动插桩 | 否 | `pkg/gin`、`pkg/grpc`、`pkg/gows` 等调用方 |
| 日志输出（request_id/trace_id 字段） | 否 | `pkg/logger` |
| 日志/指标导出（logs、metrics 信号） | 否 | 暂无归属，本包只做 traces |

---

## 使用场景选择

### 场景一：OTLP 初始化（InitWithOTLP）

**适用场景**：生产/测试环境把 Span 通过 OTLP 协议上报到 Collector（阿里云链路追踪、Jaeger、otel-collector 等），不需要调批次参数。

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/tracer"
)

func main() {
	// endpoint 为 HTTP URL（http:// 或 https:// 开头）时走 OTLP/HTTP 协议，无路径自动补 /v1/traces；
	// endpoint 为 "host:port"（如 "tracing-analysis-dc-bj.aliyuncs.com:8090"）时走 OTLP/gRPC 协议。
	// TLS 默认启用：http:// 端点自动走明文，其余走 TLS；明文 gRPC 需显式 WithInsecure(true)。
	if err := tracer.InitWithOTLP(
		"your-service-name",
		"dev",
		"v1.0.0",
		1.0,
		"https://tracing-analysis-dc-sh.aliyuncs.com/xxx/api/otlp/traces",
		tracer.WithHeaders(map[string]string{
			"Authorization": "Bearer xxx",
		}),
		tracer.WithTimeout(10*time.Second),
	); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := tracer.Close(context.Background()); err != nil {
			log.Printf("关闭 tracer 失败: %v", err)
		}
	}()

	// ... 业务代码，使用 tracer.NewSpan 或 otel.Tracer 打点
}
```

**内部行为**：

1. 校验 `appName`/`otlpEndpoint` 非空（为空直接返回 error，防止空 service.name 或误落默认端点）
2. 根据 endpoint 格式创建 OTLP exporter（HTTP 或 gRPC，异步建连，创建阶段不发起网络请求）
3. 构建 `TracerProvider`：`BatchSpanProcessor` + `Resource` + `ParentBased(DynamicSampler)`
4. 在 `tpMu` 锁内原子替换旧 `TracerProvider`，并注册全局 OTel Provider / W3C 传播器 / ErrorHandler（首次注册前保存全局现场，`Close` 时恢复）
5. 锁外同步关闭旧 `TracerProvider` 与被替换的旧 exporter（复用同一 exporter 时跳过）

**不适用**：需要自定义 exporter（Console/File）的场景（用 `Init`）；需要精细控制批次参数的场景（用 `InitWithOTLPBatch`）。

**注意**：`InitWithOTLP` 会设置进程级 `traceName`（等于 `appName`），影响后续 `NewSpan` 创建的 Span 归属名。

---

### 场景二：自定义 Batch 参数（InitWithOTLPBatch + BatchConfig）

**适用场景**：高吞吐场景调大队列/批次、低延迟场景缩短凑批超时，需要精细控制 `BatchSpanProcessor`。

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/tracer"
)

func main() {
	if err := tracer.InitWithOTLPBatch(
		"your-service-name", "dev", "v1.0.0",
		1.0,
		"tracing-analysis-dc-bj.aliyuncs.com:8090",
		tracer.BatchConfig{
			MaxQueueSize:       2048,        // 0 = SDK 默认值
			MaxExportBatchSize: 512,         // 0 = SDK 默认值
			BatchTimeout:       5 * time.Second,  // 0 = SDK 默认值
			ExportTimeout:      30 * time.Second, // 0 = SDK 默认值
		},
		tracer.WithInsecure(true), // 明文 gRPC 需显式声明；http:// 端点无需此项（按 scheme 推断明文）
	); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := tracer.Close(context.Background()); err != nil {
			log.Printf("关闭 tracer 失败: %v", err)
		}
	}()
}
```

**内部行为**：与场景一相同，额外把 `BatchConfig` 中大于零的字段转换为 `BatchSpanProcessorOption`；零值字段不生成 Option，由 SDK 使用默认值。`BatchConfig{}` 整体等效于场景一的默认批次行为。

**注意**：`BatchConfig` 是 `InitWithOTLPBatch` 的独立位置参数，不是 `OTLPOption`，对 `InitWithOTLP` / `NewOTLPExporter` 不生效。

---

### 场景三：Console / File 输出（Init + NewConsoleExporter / NewFileExporter）

**适用场景**：本地开发调试，Span 输出到终端或 JSON 文件，无需部署 Collector。

```go
package main

import (
	"context"
	"log"

	"github.com/18721889353/sunshine/pkg/tracer"
)

func main() {
	// 输出到终端
	exporter, err := tracer.NewConsoleExporter()
	if err != nil {
		log.Fatal(err)
	}

	// 或输出到文件（调用方负责关闭文件句柄）
	// exporter, f, err := tracer.NewFileExporter("trace.json")
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer f.Close()

	if err := tracer.Init(exporter, tracer.NewResource()); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := tracer.Close(context.Background()); err != nil {
			log.Printf("关闭 tracer 失败: %v", err)
		}
	}()
}
```

**内部行为**：创建 Console/File exporter 后经 `Init` 传入，与 OTLP 场景共用同一套 TracerProvider 管理逻辑（含旧 provider/exporter 的替换关闭）。

---

### 场景四：手动构建 Resource（NewResource）

**适用场景**：需要自定义服务属性（如多实例部署时用 `instance.id` 区分 Pod）。

```go
package main

import (
	"context"
	"log"

	"github.com/18721889353/sunshine/pkg/tracer"
)

func main() {
	res := tracer.NewResource(
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
	// Init 第三个可变参数为采样率，缺省时继承当前采样率（未初始化过为 1.0 全量采样）；0.5 表示 50% 采样
	if err := tracer.Init(exporter, res, 0.5); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := tracer.Close(context.Background()); err != nil {
			log.Printf("关闭 tracer 失败: %v", err)
		}
	}()
}
```

**内部行为**：`NewResource` 仅构建 `*resource.Resource` 对象（与 `resource.Default()` 合并），不涉及 TracerProvider；`Init` 时才安装 provider。服务名兜底顺序：`WithServiceName` 显式设置 > 环境变量 `OTEL_SERVICE_NAME` > 默认值 `demo-service`（打警告日志，防止生产漏配静默上错误服务名；警告仅输出一次，防多处调用刷屏）；其余字段默认 `v0.0.0` / `dev`。

---

### 场景五：创建 Span（NewSpan / SetTraceName）

**适用场景**：业务代码里用 map 写标签，不想逐个 `attribute.KeyValue` 拼 OTel API。

```go
package main

import (
	"context"
	"fmt"

	"github.com/18721889353/sunshine/pkg/tracer"
)

func doWork(ctx context.Context) error {
	tags := map[string]interface{}{
		"user.id": 1001,
		"ok":      true,
		"detail":  "下单成功",
	}
	ctx, span := tracer.NewSpan(ctx, "order.create", tags)
	defer span.End()

	// 后续调用继续传递 ctx，链路自动串联下游 Span
	return charge(ctx)
}

func charge(ctx context.Context) error {
	fmt.Println("扣款逻辑，ctx 已携带上游 Span 上下文:", ctx != nil)
	return nil
}

func main() {
	tracer.SetTraceName("order-service") // 进程级追踪名，影响 NewSpan 的 Tracer 归属
	if err := doWork(context.Background()); err != nil {
		fmt.Println("失败:", err)
	}
}
```

**内部行为**：

1. `NewSpan` 读取进程级 `traceName`（`SetTraceName` 设置的值，未设置时为 `"unknown"`）作为 Tracer 名
2. 把 map 中每个标签按值类型转换为 OTel attribute（bool/string/int/int64/float64 及其切片），`nil` 值跳过
3. 不支持的类型（struct/chan/map 等）降级为 `fmt.Sprintf("%+v")` 字符串存储，不 panic

**不适用**：多 service 共存于同一进程的场景，请直接用原生 `otel.Tracer(name).Start(ctx, name, opts...)`（`traceName` 是进程级全局状态）。

---

## 参数/结构体说明

### BatchConfig（`InitWithOTLPBatch` 批次参数）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `MaxQueueSize` | `int` | 否 | Span 队列上限，超过后新 Span 被丢弃；`0` 使用 SDK 默认值 |
| `MaxExportBatchSize` | `int` | 否 | 单次导出的 Span 数量上限；`0` 使用 SDK 默认值 |
| `BatchTimeout` | `time.Duration` | 否 | 凑批超时，到点即导出当前批次；`0` 使用 SDK 默认值 |
| `ExportTimeout` | `time.Duration` | 否 | 单次导出超时；`0` 使用 SDK 默认值 |

> 全部字段都是「零值 = 不覆盖 SDK 默认值」，因此 `BatchConfig{}` 是合法输入。

### NewSpan 标签类型支持表

| 标签值类型 | 转换结果 |
|------|------|
| `bool` / `string` / `int` / `int64` / `float64` | 对应 `attribute.Bool/String/Int/Int64/Float64` |
| `[]string` / `[]int` / `[]int64` / `[]float64` | 对应 `attribute.*Slice` |
| `nil` | 跳过不设置 |
| 其他任意类型 | `fmt.Sprintf("%+v")` 字符串降级 |

## Option 列表

### OTLPOption

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithEndpoint(endpoint)` | OTLP collector 端点。gRPC: `host:port`；HTTP: 完整 URL（无路径自动补 `/v1/traces`） | `"localhost:4317"` | `InitWithOTLP`、`InitWithOTLPBatch`、`NewOTLPExporter` |
| `WithInsecure(insecure)` | `true` 禁用 TLS（明文），`false` 启用 TLS；显式设置后不再按 scheme 推断 | 按 endpoint scheme 推断：`http://` → 明文，其余 → TLS | `InitWithOTLP`、`InitWithOTLPBatch`、`NewOTLPExporter` |
| `WithHeaders(headers)` | 额外请求头部 `map[string]string`（如认证 token）；传入 map 会被复制，后续修改原 map 不影响配置 | 空 | `InitWithOTLP`、`InitWithOTLPBatch`、`NewOTLPExporter` |
| `WithTimeout(timeout)` | 单次导出超时 | `10s` | `InitWithOTLP`、`InitWithOTLPBatch`、`NewOTLPExporter` |
| `WithErrorHandler(h)` | 自定义 OTel ErrorHandler；未设时注册默认警告日志处理器；注册期最后写入胜出，`Close` 恢复 | `nil` | `InitWithOTLP`、`InitWithOTLPBatch` |
| `WithPropagator(p)` | 自定义全局 TextMapPropagator；未设时注册 W3C 复合（TraceContext+Baggage）；`Close` 恢复 | `nil` | `InitWithOTLP`、`InitWithOTLPBatch` |
| `WithGlobalRegistration(enabled)` | `false` 完全不触碰 OTel 全局状态（业务需改用 `GetProvider().Tracer(name)`） | `true` | `InitWithOTLP`、`InitWithOTLPBatch` |
| `WithProtocol(p)` | 显式指定传输协议（`ProtocolHTTP`/`ProtocolGRPC`），覆盖 endpoint 形态自动判定 | `ProtocolAuto` | `InitWithOTLP`、`InitWithOTLPBatch`、`NewOTLPExporter` |

### ResourceOption

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithServiceName(name)` | 服务名称（写入 `service.name`） | 环境变量 `OTEL_SERVICE_NAME`，未设置时 `"demo-service"`（打警告日志，仅输出一次） | `NewResource` |
| `WithServiceVersion(version)` | 服务版本（写入 `service.version`） | `"v0.0.0"` | `NewResource` |
| `WithEnvironment(env)` | 运行环境（写入 `env`，如 dev/staging/prod） | `"dev"` | `NewResource` |
| `WithAttributes(attrs)` | 自定义属性 `map[string]string`；传入 map 会被复制 | 空 | `NewResource` |

---

## API 速查

### InitWithOTLP — OTLP 初始化（推荐）

```go
func InitWithOTLP(appName, appEnv, appVersion string, samplingRate float64, otlpEndpoint string, opts ...OTLPOption) error
```

- 创建 OTLP exporter → 构建 TracerProvider → 注册全局 Provider，一步完成
- `samplingRate` 范围 `0~1`，超出自动钳位（NaN 视为 `0`）；`0` 全丢弃、`1` 全量采样
- `otlpEndpoint` 以 `http://`/`https://` 开头走 HTTP 协议（无路径自动补 `/v1/traces`），`host:port` 走 gRPC 协议；歧义场景用 `WithProtocol` 显式指定
- 返回 error：`appName`/`otlpEndpoint` 为空、或 exporter 创建失败时返回错误（失败时自动关闭已创建的 exporter）
- **注意**：与 `Init` 的区别是 exporter 由本函数创建并接管；与 `InitWithOTLPBatch` 的区别是批次参数不可定制

---

### InitWithOTLPBatch — OTLP 初始化（自定义 Batch 参数）

```go
func InitWithOTLPBatch(appName, appEnv, appVersion string, samplingRate float64, otlpEndpoint string, batchCfg BatchConfig, opts ...OTLPOption) error
```

- 与 `InitWithOTLP` 相同，额外把 `batchCfg` 中大于零的字段转为 `BatchSpanProcessorOption`
- `batchCfg` 为值语义位置参数（非 Option）；零值字段使用 SDK 默认值
- **注意**：签名已由 4 个扁平参数改为 `BatchConfig` 结构体（见 CHANGELOG 兼容性说明）

---

### Init — 通用初始化（自定义 exporter）

```go
func Init(exporter trace.SpanExporter, res *resource.Resource, fractions ...float64) error
```

- 传入自定义 exporter（Console/File/自实现），与 OTLP 场景共用同一套 TracerProvider 管理
- `res` 为 nil 时自动 `NewResource()` 用默认值
- `fractions` 可变参数取第一个元素作采样率；缺省时继承当前采样率（未初始化过为 `1.0`），避免热更新后的采样率被重新 Init 静默重置
- 返回 error：仅 `exporter` 为 nil 时返回错误
- **注意**：`Init` 会接管 `exporter` 生命周期——被替换或 `Close` 时由本包负责 Shutdown；复用同一实例不会被误关

---

### GetProvider — 获取全局 TracerProvider（panic 语义）

```go
func GetProvider() *trace.TracerProvider
```

- 返回全局 `*trace.TracerProvider`，用于 `tracer.GetProvider().Tracer(name)` 或给 `goredis.WithTracing` 等组件注入
- **注意**：未初始化或已 `Close` 时 panic（编程错误快速失败，勿在运行期容错路径调用）；运行期安全获取用 `Provider()`

---

### Provider — 安全获取全局 TracerProvider

```go
func Provider() (*trace.TracerProvider, bool)
```

- 未初始化或已 `Close` 时返回 `(nil, false)`，不 panic；适合库代码与运行期探测
- **注意**：需要「拿不到就快速失败」语义时用 `GetProvider`

---

### Close — 优雅关闭

```go
func Close(ctx context.Context) error
```

- 恢复 OTel 全局现场（provider/propagator/handler）→ 刷新待导出 Span → 关闭 TracerProvider → 关闭当前 exporter（恰好一次）→ 重置 `traceName` 为 `"unknown"`
- `ctx` 控制 `TracerProvider.Shutdown` 的总超时；exporter 关闭使用独立 5 秒超时（`defaultShutdownTimeout`），避免 exporter 卡死长期阻塞调用方
- 幂等且并发安全：内部经 `tpMu` 清空全局状态，重复调用返回 nil
- **注意**：`Close` 后 `otel.GetTracerProvider()` 回到注册前的现场（通常是 noop provider），不会悬空在已关闭实例上；`WithGlobalRegistration(false)` 时本就不触碰全局状态，无需恢复

---

### NewOTLPExporter — 仅创建 OTLP exporter

```go
func NewOTLPExporter(opts ...OTLPOption) (*otlptrace.Exporter, error)
```

- 只创建 exporter 不初始化 TracerProvider，生命周期由调用方管理
- **注意**：与 `InitWithOTLP` 的区别——需要自行 `Shutdown`；适用于把同一 exporter 复用到多套 provider 的高级场景

---

### NewConsoleExporter — 创建 Console exporter

```go
func NewConsoleExporter() (trace.SpanExporter, error)
```

- Span 美化输出到终端，用于本地调试；配合 `Init` 使用

---

### NewFileExporter — 创建 File exporter

```go
func NewFileExporter(filename string) (trace.SpanExporter, *os.File, error)
```

- Span 输出到 JSON 文件，返回 exporter 与文件句柄；`filename` 为空时默认 `traces.json`
- **注意**：调用方负责 `file.Close()`；创建失败时内部已关闭文件，不会泄漏

---

### NewResource — 构建 Resource

```go
func NewResource(opts ...ResourceOption) *resource.Resource
```

- 构建 OTel Resource 并与 `resource.Default()` 合并；未设字段用默认值
- 仅构建对象，不涉及 TracerProvider

---

### NewSpan — 创建 Span（map 标签简化版）

```go
func NewSpan(ctx context.Context, spanName string, tags map[string]interface{}) (context.Context, trace.Span)
```

- 返回携带 Span 的新 ctx 与 Span 本体；结束必须调用 `span.End()`
- 标签按值类型转换（见「NewSpan 标签类型支持表」），不支持的类型降级为字符串
- **注意**：Tracer 名取自进程级 `traceName`（`SetTraceName` 设置），多 service 场景请用原生 `otel.Tracer(name)`

---

### SetTraceName — 设置进程级追踪名称

```go
func SetTraceName(name string)
```

- 影响后续所有 `NewSpan` 的 Tracer 归属名；空字符串保持原值不变
- `Close` 后自动重置为 `"unknown"`
- `Init*`/`Close` 内部的 traceName 更新在 `tpMu` 临界区内与 provider 替换原子一致（并发 Init/Close 后 traceName 与 provider 状态不会矛盾）；公开 `SetTraceName` 是独立原子操作，与并发 Init/Close 无先后保证
- **注意**：进程级全局状态，多 service 共存场景不要使用

---

### SetSamplingRate — 动态修改采样率

```go
func SetSamplingRate(rate float64)
```

- 运行期热更新采样率（`0~1`），立即对新创建的 Span 生效，无需重启
- 越界值自动钳位：`≤0`（含 NaN）→ `0`，`≥1` → `1`
- 未初始化时为安全 no-op

---

### GetSamplingRate — 获取当前采样率

```go
func GetSamplingRate() float64
```

- 返回当前采样率（atomic 读取）
- **注意**：未初始化时返回 `1.0`（与 `Init` 缺省采样率一致），不是 `0`

---

### 超时说明

本包涉及多种超时/时间参数，各自生效范围不同：

| 参数 | 控制对象 | 生效位置 |
|------|----------|----------|
| `WithTimeout(d)` | OTLP exporter 单次导出超时 | `NewOTLPExporter` 创建时生效 |
| `BatchConfig.BatchTimeout` | 凑批超时（到点导出） | `InitWithOTLPBatch` |
| `BatchConfig.ExportTimeout` | 单次导出超时 | `InitWithOTLPBatch` |
| `Close(ctx)` 的 `ctx` | `TracerProvider.Shutdown` 刷新总超时 | 调用 `Close` 时 |
| `defaultShutdownTimeout`（5s，内部常量） | exporter / 旧 provider Shutdown 的兜底超时 | `Close` 与替换旧 exporter 时 |

---

## 错误处理

| 场景 | 行为 |
|------|------|
| `Init(nil, res)` | 返回 `error("tracer: exporter 不能为 nil")` |
| `InitWithOTLP*` 传空 `appName` / `otlpEndpoint` | 返回 `error("tracer: appName 不能为空")` / `error("tracer: otlpEndpoint 不能为空")` |
| `Init` / `InitWithOTLP*` 传入 nil Resource | 自动补全默认 Resource，不报错 |
| `NewOTLPExporter` endpoint 不可达 | 创建阶段不报错（连接异步建立）；导出失败经 ErrorHandler 走警告日志 |
| `Close` 时 exporter Shutdown 失败/超时 | 以警告日志记录，不影响 `Close` 主流程返回值 |
| 并发调用 `Init` | 安全：锁内原子替换 provider，旧 provider 在锁外同步关闭 |
| 重复 `Init` | 新 exporter 替换旧的，旧 exporter 自动 Shutdown（复用同一实例除外） |
| 重复 / 并发 `Close` | 幂等：第二次调用返回 nil |
| `GetProvider` 未初始化 | panic（编程错误，见 API 速查） |

---

## OpenTelemetry 集成

- `Init*` 会把 TracerProvider 注册为全局默认（`otel.SetTracerProvider`），业务侧直接用 `otel.Tracer(name)` 即可
- 注册 W3C 复合传播器（`TraceContext` + `Baggage`），跨服务调用自动携带 trace 上下文；可用 `WithPropagator` 替换
- 采样器为 `ParentBased(DynamicRatioBasedSampler)`：根 Span 按采样率决策，子 Span 继承父级决策
- ErrorHandler 可通过 `WithErrorHandler` 自定义（如对接 `pkg/logger`），未设置时注册默认警告日志处理器
- **全局治理**：首次注册前保存 OTel 全局现场（provider/propagator/handler），`Close` 时恢复——不悬空已关闭实例、不永久覆盖其他库（如 B3/Jaeger 传播器）的全局配置。传播器与 ErrorHandler 注册期为「最后写入胜出」（OTel 默认 ErrorHandler 的委托机制不允许安全的链式组合，详见 `registerGlobal` 注释）；多库共存或自行管理全局状态时用 `WithGlobalRegistration(false)` 完全不触碰全局
- Span 创建后导出由 `BatchSpanProcessor` 异步批量完成，`Close` 时强制刷新

**多库共存推荐做法**（进程内还有其他库使用 OTel 全局状态时）：

1. **优先 `WithGlobalRegistration(false)`**：本包完全不触碰 OTel 全局，业务改用 `Provider().Tracer(name)`；其他库的全局注册不受影响——这是最干净的隔离方式
2. 无法避开全局注册时：**`Init*` 与 `Close` 串行调用，不应并发**——全局现场保存/恢复是包级单快照，无法配对「哪次 Close 对应哪次 Init」，并发交错（Init/Close/Init 三路）可能互相销毁对方的现场（契约见 `Close` 注释）
3. 传播器 / ErrorHandler 注册期「最后写入胜出」：本包会覆盖已注册的全局传播器（如 B3/Jaeger），`Close` 时恢复现场。需共存保留他人配置时，用 `WithPropagator`/`WithErrorHandler` 显式传入要保留的实现，或走第 1 条完全不注册

---

## 集成测试

集成测试带 `//go:build integration` 构建标签，依赖真实 OTLP Collector，通过环境变量配置。

**环境变量一览**（与 `integration_test.go` 实际读取的变量一一对应）：

| 变量 | 必填 | 用途 | 默认值 |
|------|------|------|--------|
| `OTEL_EXPORTER_OTLP_ENDPOINT_GRPC` | 二选一 | gRPC 端点（`host:port`，勿带 scheme）；未设置时 gRPC 用例自动 Skip | 无 |
| `OTEL_EXPORTER_OTLP_ENDPOINT_HTTP` | 二选一 | HTTP 端点（完整 URL 含路径，token 通常嵌在路径中）；未设置时 HTTP 用例自动 Skip | 无 |
| `OTEL_EXPORTER_OTLP_HEADERS` | 否 | 额外鉴权头部，格式 `k1=v1,k2=v2`（阿里云 gRPC 用 `authentication=<token>`） | 无 |
| `OTEL_EXPORTER_OTLP_TOKEN` | 否 | Bearer token 简写，等效 `Authorization=Bearer <token>`（`HEADERS` 已给 `Authorization` 时不覆盖） | 无 |
| `TRACER_TEST_TIMEOUT` | 否 | `TestMain` 总时长上限（Go duration） | `120s` |

**运行方式**：

```bash
cd pkg/tracer

# 方式一：显式传环境变量（CI 用法，优先级高于 .env），gRPC 示例：
OTEL_EXPORTER_OTLP_ENDPOINT_GRPC=tracing-analysis-dc-bj.aliyuncs.com:8090 \
  OTEL_EXPORTER_OTLP_HEADERS=authentication=<token> go test -tags=integration -v -count=1

# 方式二：包内 .env（推荐本地，无需任何插件）
# TestMain 里的 loadDotEnv 会自动读取包内 .env 并补齐环境变量，
# 语义是「只补缺、不覆盖已设置的变量」；根 .gitignore 的 *.env 已覆盖该文件，不会入库。
go test -tags=integration -v -count=1

# 只跑集成用例（TestIntegration_OTLPExport 前缀同时命中 GRPC / HTTP 两个用例；
# 注意：多个用例名之间不要直接用 |，Git Bash 会把 | 当管道）
go test -tags=integration -count=1 -v -run 'TestIntegration_OTLPExport'

# 仅单元测试（无需任何环境变量，日常使用）
go test ./pkg/tracer/ -v
```

**行为说明**：

- 未设置端点变量 → 对应用例自动 Skip；端点 TCP 不可达 → `Skipf` 并打印具体 error（区分「端口没映射」「IP 写错」「防火墙丢包」）；`Init`/`Close` 失败 → `Fail`
- 用例通过 `errorCollector`（自定义 `otel.ErrorHandler`）采集导出错误并在 `Close` 后断言：**0 错误 = 采集端应答接受上报（含鉴权通过）**；鉴权类与网络类错误按部署/环境差异 `Skipf`，其余错误必须 `Fail`（关键词判定表及「为何某些词不能加」见 `exportErrClassify` 注释）
- 等待时长已抽为命名常量（`dialProbeTimeout`、`spanRecordSimulateWait`、`exportFlushTimeout`）而非散落的 `time.Sleep`；出现 flaky 时**优先调这些常量，不要改断言**
- `TestMain` 限制总时长（`TRACER_TEST_TIMEOUT` 可上调），防止外部 SDK goroutine 泄漏导致 `go test` 永远挂起

**实测结果汇总**（真实 OTLP Collector = 阿里云链路追踪，Windows 10 / go1.25.0 本机，`go test -tags=integration -v -count=1`，凭据已脱敏）：

| 分组 | 结果 | 关键读数 |
|------|------|----------|
| 单元测试（全部用例） | PASS | 总耗时 ~0.2s（`go test ./pkg/tracer/ -count=1`）；覆盖率 91.7%（`go test -cover`，质量门槛 85%） |
| `TestIntegration_OTLPExportGRPC`（`tracing-analysis-dc-bj.aliyuncs.com:8090` + `authentication` 头） | **PASS** | init=1.7ms；3 个 Span 创建=151ms（含 3×50ms 模拟业务耗时）；Close 强制刷新导出=159ms；**导出链路 0 错误，采集端应答接受上报** |
| `TestIntegration_OTLPExportHTTP`（`http://tracing-analysis-dc-sh.aliyuncs.com/adapt_***@***/api/otlp/traces`） | **PASS** | 3 个 Span 创建=152ms；Close 强制刷新导出=43ms；**导出链路 0 错误，采集端应答接受上报**。注：早期一轮曾被服务端 403 拒绝（三组对照：带鉴权头/无关头/无头均 403，排除请求头干扰），后连续 4 次 PASS，判定为服务端临时状态；`exportErrClassify` 仍保留鉴权类错误按部署差异 Skip 的兜底 |
| fuzz 种子语料 + 挖掘冒烟（3 个 target） | PASS | 种子随常规 `go test`；`FuzzClampRate` 冒烟 5s / 113419 execs 通过 |

> **验证范围限制**：用例通过证明的是**客户端侧导出链路**（OTLP 上报被采集端应答接受、鉴权通过）；「入库后可在控制台查询到」属服务端行为，需在阿里云控制台人工确认（外部视角读数不在本包测试约定内）。
> **未验证声明**：`-race` 仍未取证（见「竞态检测」一节）。

---

## 性能基线与模糊测试

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=10 ./pkg/tracer/
```

> **诚实声明**：本机（Windows 10 / MinGW）实测 `CGO_ENABLED=1 go test -race` 启动即失败，输出 `exit status 0xc0000139`（工具链问题，非被测代码问题，多轮复测均如此；本机亦无 WSL/Docker/clang 可迂回），因此**本 README 不声称已经跑过 `-race`**。
> 真实竞态证据需在 Linux/CI 上执行上述命令采集（`-count=10` 多轮调度变化，单次跑过不等于没 race）；**该证据未闭环前，不应对外宣称本包「生产可用」**。
> 并发正确性目前由 `TestInitRace`、`TestInitWithOTLPRace`、`TestInstallProviderRegisterGlobalAtomic`、`TestDynamicSamplerConcurrentAccess`、`TestNewSpanHighConcurrencyMemoryBounded` 等并发用例在非 race 模式下覆盖。

### 基准测试

环境：Windows 10 / go1.25.0 / Intel Core i7-10750H @ 2.60GHz（-cpu=12），命令
`go test -bench=. -benchmem -run='^$' -benchtime=200ms ./pkg/tracer/`：

| 基准 | 场景 | ns/op | B/op | allocs/op |
|------|------|------|------|-----------|
| `BenchmarkClampRate` | 采样率钳位 | 0.24 | 0 | 0 |
| `BenchmarkGetSamplingRate` | 采样率读取（atomic） | 0.29 | 0 | 0 |
| `BenchmarkSetSamplingRate` | 采样率热更新（atomic） | 5.77 | 0 | 0 |
| `BenchmarkDynamicSamplerShouldSample` | 采样决策（比例分支，math/rand/v2 无全局锁） | 28.54 | 0 | 0 |
| `BenchmarkNewSpanNoTags` | 创建 Span（无标签） | 200.6 | 240 | 3 |
| `BenchmarkNewSpanWithTags` | 创建 Span（4 个混合类型标签，合并 attrs 后） | 422.9 | 536 | 6 |
| `BenchmarkNewSpanParallel` | 高并发打点（b.RunParallel，完整热路径：采样决策+录制+入队） | 518.2 | 1752 | 8 |
| `BenchmarkNewSpanParallelChild` | 高并发子 Span（ctx 带父 Span，覆盖 ParentBased 继承分支） | 526.7 | 1752 | 8 |
| `BenchmarkInitReplaceProvider` | TracerProvider 原子替换（含关闭旧 provider） | 73478 | 52831 | 109 |

**口径与解读（很重要，否则数字会被误读）：**

- 全部基于包内 mock / 内存操作，**不含任何网络 RTT**，量的是本包开销，不代表生产上报耗时（上报是 BatchSpanProcessor 异步批量完成）
- `allocs/op` 与 `B/op` 是稳定指标，回归对比看这两个；`ns/op` 在本机有波动（benchtime 仅 200ms，未做多轮方差标定）
- 运行口径：`GOGC=100`（默认）、`-cpu=12`（默认全核）、`benchtime=200ms`（快速回归口径；RunParallel 基准要更稳读数建议单独 `-benchtime=2s`）
- `BenchmarkInitReplaceProvider` 含旧 provider 的 Shutdown 开销，属「替换路径」而非热路径
- `NewSpan` 标签合并为一次 `WithAttributes` 传入后，4 标签场景 allocs/op 由 14 降至 6（-57%）、B/op 由 704 降至 536（旧读数为优化前基线，见 CHANGELOG）
- `BenchmarkNewSpanParallel` / `BenchmarkNewSpanParallelChild` 口径略高于串行基准（含采样决策 + Span 录制 + BSP 入队/丢弃完整路径），并行下 allocs/op 与串行同量级（8 vs 6），无锁竞争放大；Child 变体以 ctx 携带父 Span，覆盖 ParentBased「继承父级决策」分支。两基准口径限定于注册全局模式——`WithGlobalRegistration(false)` 时业务直接用 `Provider().Tracer(name)` 不经 `NewSpan`，无对应基准

### 高并发与内存行为

**结论：高并发打点不会导致内存暴涨。内存有界，上限由 BatchSpanProcessor 队列容量决定，与打点速率无关。**

**为什么有界（逐层拆解）：**

| 层 | 分配/存储 | 并发行为 |
|------|----------|----------|
| `NewSpan` 标签转换 | 每次 3~6 allocs（约 240~536B），Span End 后即成纯垃圾 | 无共享锁（每次独立分配） |
| 采样热路径（`ShouldSample`/`GetSamplingRate`/`SetSamplingRate`） | **0 allocs**（atomic.Uint64，`math/rand/v2` 无全局锁） | 无锁竞争 |
| `BatchSpanProcessor` 队列 | 固定容量 `MaxQueueSize`（默认 2048，`BatchConfig` 可调）+ 批次缓冲 `MaxExportBatchSize`（默认 512） | **非阻塞入队，队列满丢弃新 Span**（背压表现为丢弃，不是堆积） |
| OTLP exporter | 仅持有当前导出批次 | 导出串行异步，不随打点量增长 |

内存上限估算：`(MaxQueueSize + MaxExportBatchSize) × 单 Span 常驻大小`，默认配置约 `2560 × ~1KB ≈ 2~3MB` 级别；打点速率再高，超出部分被丢弃而不是排队。

**实测读数**（Windows 10 / go1.25.0 / i7-10750H / GOGC=100，`go test -run 'MemoryBounded' -v ./pkg/tracer/`；压测含 burst 与 sustained 两阶段，同时覆盖「瞬时猛打」与「持续打点稳态」两个口径）：

| 压测场景 | 结果 | 读数 |
|---------|------|------|
| burst：100 协程 × 1000 次带标签 `NewSpan`（共 10 万 Span，全量采样） | PASS | 累计分配 167MB（分配量真实口径，GC 不影响该计数器）；打点结束瞬间残余待回收垃圾 13.4MB；**GC 后常驻堆增量仅 181KB**（远小于累计分配的 1/10，证明「垃圾被回收」而非「分配少」），无线性累积 |
| sustained：20 协程持续打点 1 秒（稳态） | PASS | 累计分配 3305MB（稳态分配吞吐约 3.3GB/s，全部为可回收垃圾）；**GC 后常驻堆增量 329KB**（回收比约 1/10000）——「GC 边打边收」的稳态成立，覆盖「打点速率再高也不会堆积」结论的稳态口径 |
| 导出端持续缓慢（队列收紧 256 / 批次 64 / 单次导出阻塞 20ms，100 协程 × 500 = 5 万 Span） | PASS | **GC 后常驻堆增量仅 137KB**（上限由队列容量决定）；实际导出 0 Span（其余全被背压丢弃），证明丢弃而非堆积 |
| `BenchmarkNewSpanParallel`（12 线程并行打点） | PASS | 518.2 ns/op、8 allocs/op——与串行同量级，无锁竞争放大 |

**真正会导致内存暴涨的三种调用方错误（本包无法防御，文档明示）：**

1. **Span 创建后不调用 `span.End()`**：未 End 的 Span 连同其 attributes 常驻内存且不入队导出——这是最常见的泄漏，务必 `defer span.End()`
2. **导出持续失败时的错误风暴**：ErrorHandler 对每条导出错误打日志，若采集端长时间不可用，日志 IO/内存压力在 `pkg/logger` 侧（可 `WithErrorHandler` 自定义降噪/采样）
3. **把大对象塞进 Span 标签**：单 Span 常驻大小直接乘以队列容量；大 payload 应存日志/对象存储，Span 只放索引

复现命令：

```bash
go test -run 'MemoryBounded' -v ./pkg/tracer/          # 两个内存有界性压测用例
go test -bench=BenchmarkNewSpanParallel -benchmem -run='^$' ./pkg/tracer/
```

### 模糊测试

种子语料随常规 `go test` 运行；挖掘深层输入：

```bash
go test -fuzz=FuzzClampRate -fuzztime=30s ./pkg/tracer/
```

| Target | 不变量 |
|--------|--------|
| `FuzzClampRate` | 输出恒在 `[0,1]`（含 NaN/±Inf）；幂等 `clampRate(clampRate(x)) == clampRate(x)` |
| `FuzzIsHTTPEndpoint` | 任意输入不 panic；判定为 HTTP 时输入必以 `http://` 或 `https://` 开头 |
| `FuzzNewSpanNeverPanics` | 任意标签键值/类型组合创建 Span 不 panic（不支持类型降级字符串） |

---

## 参考文档

- [OpenTelemetry Go SDK 文档](https://opentelemetry.io/docs/instrumentation/go/)
- [OpenTelemetry Go 集成库](https://opentelemetry.io/registry/?language=go&component=instrumentation)
