# pkg/metrics — 统一指标采集 / 差分 / 诊断 / 企业微信推送

sunshine 框架的指标中枢。业务侧只做**注册**，框架负责采集、窗口差分、异常诊断、报告渲染与推送，全链路单一口径。

```
业务包定义即注册 ──▶ 统一 Registry（Default + Fast）
                          │
        ┌─────────────────┼──────────────────┐
        ▼                 ▼                  ▼
   /metrics 端点      5s 快速巡检        60s 周期报告
  （含 go_* 运行时）  （仅自定义指标，      （完整报告）
                      不触发 runtime STW）
                          │
                    诊断（阈值可配）
                          │
                    企业微信 webhook 推送
```

---

## 1. 快速开始（业务侧接入）

### 方式一：定义即注册（推荐）

在组件包新建 `metrics.go`，用 `pkgmetrics` 便捷 API 创建——**创建即注册**，无需调用任何 init/register：

```go
package mycomponent

import (
    "time"

    pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// 指标在 var 定义时自动完成双注册（Default + Fast）
var opDuration = pkgmetrics.HistogramDefault(
    "my_operation_duration_seconds",
    "My operation latency by result.",
    "result", // label 名列表
)

// 埋点：统一 result 口径（见 §3）
func observeOp(err error, cost time.Duration) {
    result := "ok"
    if err != nil {
        result = "error"
    }
    opDuration.WithLabelValues(result).Observe(cost.Seconds())
}
```

可用的便捷 API（均创建即注册到统一中心）：

| API | 指标类型 | 适用场景 |
|-----|---------|---------|
| `HistogramDefault(name, help, labels...)` | Histogram（默认桶） | 耗时（秒），最常用 |
| `Histogram(name, help, buckets, labels...)` | Histogram（自定义桶） | 需要自定义分位数精度 |
| `Counter(name, help, labels...)` | Counter | 次数、累计量 |
| `Gauge(name, help, labels...)` | Gauge | 当前值（连接数、活跃数） |
| `Summary(name, help, labels...)` | Summary | 无需分位数桶的分布 |

### 方式二：手动注册（自定义 Collector）

```go
pkgmetrics.MustRegister(myCollector)                 // 双注册（Default + Fast）
pkgmetrics.MustRegisterDefaultOnly(goStyleCollector) // 仅 Default（见 §7 为何 runtime 指标要隔离）
```

> **幂等**：重复注册同一 collector 或同名指标静默忽略，不 panic。
> **但同名不同定义会 panic**（help/labels 不一致）——这是真实配置错误，故意不吞。

### 接入集成测试守护

新增组件包的指标后，在 `pkg/metrics/integration_registration_test.go` 补两行（import + 测试条目），否则漏注册无人报警：

```go
_ "github.com/18721889353/sunshine/pkg/mycomponent"
// ...
{"mycomponent", "my_operation_duration_seconds"},
```

---

## 2. 启动指标报告器

### 框架应用（自动接入）

框架自带的示例模板（`cmd/serverNameExample_*`）**已自动接入**：`initApp` 调 `config.StartMetricsReporter()`、`close` 调 `config.StopMetricsReporter()`。只需在 yml 配置：

```yaml
metrics:
  enabled: true                # 指标体系总开关（原 app.enableMetrics 已合并到此）
                               # false = 入口埋点/路由不装配 + 报告器不启动（零外发）
  webhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"  # 留空不启动
  intervalSec: 60
  errRateWarn: 0.05      # 0 = 用默认
  errRateCritical: 0.20
  p99Warn: 1.0
  p99Critical: 5.0
  minSamples: 10
```

- **开关语义**：`enabled` 是唯一总开关，启动时装配、**无热更新**——false 时 gin 中间件/gRPC 拦截器/`/metrics` 路由都不装配，报告器也不启动
- **定制诊断扩展点**：`metrics.WithDiagnosisHook(func(s *MetricSnapshot, th DiagnosisThresholds) *Diagnosis)` 注入业务诊断
  （如网关的 502 下游异常、三段耗时归因）；返回 nil 回落框架通用诊断，hook panic 自动保护回落。
  `Diagnosis.Evidence` 字段承载支撑信号（如 `"502=180, 摘除=2"`），只展示不参与判定。
  平台给能力（报告链/推送/防抖/巡检），业务给规则——业务定制诊断无需自建报告器
- 配置结构由 `make update-config` 从 yml 生成（**改配置先改 yml 再跑生成**，不要手写结构体字段）

### 直接调用（非框架应用）

```go
pkgmetrics.StartReporter(cfg.MetricsWebhookURL, cfg.MetricsIntervalSec)

// 需要按业务调阈值时（如支付接口 P99 1s 即严重）：
pkgmetrics.StartReporter(webhookURL, 60,
    pkgmetrics.WithDiagnosisThresholds(pkgmetrics.DiagnosisThresholds{
        P99Warn:     0.5,
        P99Critical: 1.0,
        // 未设置的字段回落默认值
    }),
)
```

- **webhook 留空** → 不启动，打印 WARN 留痕（yml 不入库，静默返回会让运维误以为在跑）
- **interval ≤ 0** → 默认 60s；**< 10s** → 钳到 10s（企微限频 20 条/分钟）
- **幂等**：重复调用只生效一次
- 停止：`pkgmetrics.StopReporter()`（等待在途推送结束）

### 双节奏

| 节奏 | 间隔 | 行为 |
|------|------|------|
| 周期报告 | 配置值（默认 60s） | 完整报告，含运行时指标 |
| 快速巡检 | 5s | 诊断结论**变化**且进入异常时即时补推（防抖 30s），异常秒级发现 |

关键不变量：`prev`/`lastCode`/`lastPushAt` **只在推送成功后推进**——失败保留现场，下个巡检点按同一窗口重试，否则一次推送失败会吞掉异常告警。

---

## 3. 指标口径约定（全仓统一）

**这是本包最重要的约定，所有埋点必须遵守：**

### result label：只有 `ok` / `error`

```go
result := "ok"
if err != nil {
    result = "error"
}
```

例外归一化（采集层自动处理，埋点方无需关心）：

| 来源 | 原始 label | 归一化后 |
|------|-----------|---------|
| 全仓业务指标 | `result=ok/error` | 原样 |
| gRPC provider | `grpc_code=OK` | `result=ok` |
| gRPC provider | `grpc_code=Internal` 等 | `result=error` |

### 语义边界（什么算 error）

| 场景 | 记法 | 理由 |
|------|------|------|
| Redis GET miss（`redis.Nil`） | `ok` | 正常结果，生产 miss 率高，计 error 会打满错误率 |
| 分布式锁竞争失败（TryLock） | `ok` | 正常竞争结果 |
| 分布式锁阻塞获取失败（Lock） | `error` | 阻塞锁拿不到 = 故障，必须可见 |
| 定时任务 panic | `error` | 静默故障 |
| 配置回调 panic | `error` | 配置已变但业务没生效 |
| HTTP 4xx/5xx / transport 失败 | `error` | status 是协议事实，另存 `status` label |

### 单位

- 耗时指标：**秒**（`*_duration_seconds`），后缀必须是 `_duration_seconds`
- 累计计数：**`_total`** 后缀的 Counter
- 字节数：`*_bytes`，绝不混入耗时集合（否则会被渲染成毫秒、按错误率误诊）

### 高基数防护

- gin 的 `path` label 必须用路由模板（`/users/:id`），不能用原始路径——否则每个 ID 一条 series 撑爆 Prometheus
- label 值不含逗号/等号（`labelsKey` 用 `k=v,k=v` 拼接做差分）

---

## 4. 组件指标清单

### 接入层（HTTP/gRPC）

| 指标 | 类型 | labels | 来源 |
|------|------|--------|------|
| `http_request_duration_seconds` | Histogram | status, path, method, result | gin 中间件 |
| `http_request_count_total` | Counter | 同上 | gin 中间件 |
| `http_request_size_bytes` / `http_response_size_bytes` | Summary | 同上 | gin 中间件 |
| `uptime_total` | Counter | — | gin 中间件（每分钟 +1） |
| `http_client_duration_seconds` | Histogram | method, status, result | gohttp 出站 |
| `grpc_server_handling_seconds` | Histogram | grpc_type, grpc_service, grpc_method | gRPC server |
| `grpc_server_handled_total` | Counter | 同上 + grpc_code | gRPC server |
| `grpc_client_handling_seconds` | Histogram | grpc_type, grpc_service, grpc_method | gRPC client（WithClientHandlingTimeHistogram，与 server 对称） |
| `grpc_client_handled_total` | Counter | 同上 + grpc_code | gRPC client |
| `grpc_server_active_connections` | Gauge | — | gRPC server（CustomListener 计数） |
| `grpc_client_active_connections` | Gauge | — | gRPC client（stats.ConnBegin/End 计数，与 server 对称） |

### 数据层

| 指标 | 类型 | labels |
|------|------|--------|
| `redis_operation_duration_seconds` | Histogram | op, result |
| `cache_operation_duration_seconds` | Histogram | op, result |
| `cache_hit_total` / `cache_miss_total` | Counter | op（命中率独立计数，不进 result） |
| `db_query_duration_seconds` | Histogram | op, result |
| `es_operation_duration_seconds` | Histogram | op, result |
| `query_build_duration_seconds` | Histogram | result |

### 消息 / 任务

| 指标 | 类型 | labels |
|------|------|--------|
| `rabbitmq_publish_duration_seconds` | Histogram | exchange, result |
| `rabbitmq_consume_duration_seconds` | Histogram | queue, result |
| `cron_task_duration_seconds` | Histogram | task, result |

### 基础设施

| 指标 | 类型 | labels |
|------|------|--------|
| `lock_acquire_duration_seconds` | Histogram | backend, result |
| `etcd_token_refresh_total` | Counter | result |
| `nacos_config_fetch_duration_seconds` | Histogram | result |
| `nacos_config_callback_total` | Counter | result |

### 运行时

| 指标 | 类型 | 说明 |
|------|------|------|
| `logger_dropped_entries_total` | Counter | 异步缓冲区满丢弃条数（自定义 Collector） |
| `logger_router_active_loggers` | Gauge | 活跃 logger 数 |
| `go_*` | — | GoCollector，仅进 Default（见 §7） |
| `metrics_reporter_push_total` | Counter | 报告器自监控（见 §8） |
| `metrics_reporter_last_push_timestamp_seconds` | Gauge | 报告器自监控 |

---

## 5. 诊断引擎

窗口增量自动诊断，**无需指标名白名单**——遍历所有组件指标：

```go
diag := pkgmetrics.Diagnose(snapshot)                    // 默认阈值
diag := pkgmetrics.DiagnoseWithThresholds(snapshot, th)  // 自定义阈值
```

### 默认阈值（`DefaultDiagnosisThresholds()`）

| 字段 | 默认 | 含义 |
|------|------|------|
| `ErrRateWarn` | 0.05 | 错误率警告 5% |
| `ErrRateCritical` | 0.20 | 错误率严重 20% |
| `P99Warn` | 1.0s | P99 警告 |
| `P99Critical` | 5.0s | P99 严重 |
| `MinSamples` | 10 | 样本 < 10 不诊断（防偶发单次失败刷告警） |

零值字段自动回落默认值（`normalize`），只覆盖需要的字段即可。

### Counter 类错误

`etcd_token_refresh` / `nacos_config_callback` 等低频静默故障指标是 Counter，不走 P99：

> **窗口内出现失败样本即 warn** —— 此类操作低频，偶发错误就值得关注。

### 诊断结论结构

```go
type Diagnosis struct {
    Level   DiagnosisLevel // OK / Warn / Critical
    Code    string         // 稳定异常标识（不含计数），如 "redis:err-rate>5%"
    Desc    string         // 最严重项描述
    Details []string       // 全部异常（多组件同时异常时不吞）
}
```

`Code` 只取**最严重的一项**，供快速巡检判断"结论是否变化"（code 变了才即时推送，防刷屏）；`Details` 展示全景，不参与变化检测。

---

## 6. HTTP 暴露端点

**全仓只从这里拿 handler，不要直接用 `promhttp`：**

```go
pkgmetrics.Handler()     // 全部指标（含 go_* 运行时）——常规 /metrics 用这个
pkgmetrics.FastHandler() // 仅自定义指标（无 go_memstats）——内部诊断/巡检端点
```

注册到路由：

```go
mux.Handle("/metrics", pkgmetrics.Handler())
```

---

## 7. 双 Registry 设计（为什么有 Default + Fast）

| Registry | 内容 | 用途 |
|----------|------|------|
| **Default** | 全部指标 + `go_*` 运行时 | `/metrics` 端点拉取 |
| **Fast** | 仅自定义指标 | 5s 快速巡检 |

**为什么隔离**：读 `go_memstats_*` 会触发 `runtime.ReadMemStats` 的 **STW 停顿**。巡检每 5s 一次，读运行时指标会周期性卡住业务。所以 Fast Registry 只收自定义指标。

推论：**runtime 类指标必须用 `MustRegisterDefaultOnly`**（只进 Default），不能进 Fast。

---

## 8. 报告器自监控

报告器是"监控的监控"——它自身故障时若无指标，运维分不清"群里没消息"是业务正常还是报告器挂了：

| 指标 | 监控表达式 | 含义 |
|------|-----------|------|
| `metrics_reporter_push_total{result}` | `rate(..._total{result="error"}[5m]) > 0` | 失败率突增 = webhook 故障/被限频 |
| `metrics_reporter_last_push_timestamp_seconds` | `time() - ... > 180` | 报告器停摆（或从未成功推送） |

---

## 9. 架构边界（为什么有些东西不进本包）

| 关注点 | 归属 | 理由 |
|--------|------|------|
| promhttp 暴露入口 | **本包** `Handler()` | 通用 HTTP 暴露能力，任何指标都用，统一后加认证/限流只改一处 |
| gRPC provider（`go-grpc-middleware/providers/prometheus`） | `pkg/grpc/metrics` | 它是"指标 + interceptor 行为"复合体，属 gRPC 适配层专属依赖；搬进本包会让底层通用包反向依赖 gRPC 语义 |
| gRPC 指标口径 | **本包**采集层归一化 | `grpc_code` → `result`，让 grpc 指标接入统一诊断/渲染（见 §3） |

**注册与暴露路径已全仓统一**：所有包走 `pkgmetrics.MustRegister*` + `pkgmetrics.Handler()`，无独立 registry、无散落的 promhttp。

---

## 10. 常见问题

**Q：Vec 无样本时 Gather 采不到，测试怎么验证注册？**
A：用 `Default().HasMetric(name)` 精确查询（无副作用）。不要用 Gather（无样本不 emit，会假阳性），也不要用"同名冲突 panic"反证（有副作用）。

**Q：为什么我的指标没出现在报告里？**
A：按序排查：
1. 是否注册进 Fast？（`HasMetric` 查）
2. 是否有样本？（Vec 无样本不 emit）
3. 指标名 base 是否在 `componentSpecs`？不在会走 `renderUnrecognized` 兜底（有展示但无中文标签/分组）
4. 耗时指标后缀必须是 `_duration_seconds`，Counter 必须 `_total`，否则 `metricBase` 提取不出 base

**Q：重复注册会 panic 吗？**
A：同 collector / 同名同定义 → 静默忽略（幂等）。同名**不同** help/labels → panic（真实配置错误，两包各定义一版必须暴露）。

**Q：为什么报告里显示"近600秒"而不是"近10分钟"？**
A：窗口 = 配置间隔 + 采集/推送耗时，几乎不可能精确整除。恰为整分钟才显示分钟（见 `FormatIntervalText`），这是正常现象。

**Q：从旧版 `app.enableMetrics` 迁移要注意什么？**
A：旧字段已**物理合并**到 `metrics.enabled`，迁移清单（三处都要改，漏了会被 YAML **静默忽略**为 false）：
1. 业务项目 yml：`app.enableMetrics` → `metrics.enabled`
2. Nacos 配置中心的配置项（dataID 对应 yml）
3. K8s ConfigMap 里的配置
同时旧版的"指标热更新"能力**已移除**（`metrics.SetMetricsEnabled` 等接口已删除），开关只在启动时读取。

---

## 附：目录结构

| 文件 | 职责 |
|------|------|
| `registry.go` | 统一注册中心（双 Registry、便捷 API、HasMetric） |
| `handler.go` | promhttp 单一暴露入口 |
| `collect.go` | Gather → 结构化快照（含 grpc_code 归一化） |
| `delta.go` | 窗口差分（累计→增量，重置兜底） |
| `diagnose.go` | 诊断引擎（阈值可配、Counter 错误） |
| `render.go` | 报告渲染（组件分组、兜底展示） |
| `report.go` | 报告器（双节奏、状态推进、自监控） |
| `webhook.go` | 企微推送（errcode 判定、脱敏、截断） |
| `format.go` | 格式化（时长/窗口/字节） |

包级设计说明与核心不变量见 `collect.go` 的 package 注释。
