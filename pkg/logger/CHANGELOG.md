# Changelog

`pkg/logger` 的版本变更日志，遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [未发布]

### 修复（Fixes）

- **SLS 浮点字段序列化错误（数据损坏）**：`sls_hook.go` 原先用 `float64(field.Integer)` 读取
  `Float64`/`Float32` 类型字段，但 zap 实际是用 `math.Float64bits`/`math.Float32bits` 把浮点数的
  **位模式**存入 `Integer`，导致上报到阿里云 SLS 的所有 `Float64(...)`/`Float32(...)` 字段值都是错误数字。
  现改用 `math.Float64frombits`/`math.Float32frombits` 按位还原。
  - 影响面：仅改变 **SLS 侧的字段取值**（从乱码变为正确值），本地 zap 输出、API 签名与函数输入输出均不变。
  - 守护：新增 `TestSlsFieldValue`（含 Float64/Float32 正例与「反证历史错误写法」）与 `FuzzSlsFieldValue`。

- **带 ctx 的自定义钩子不受日志级别过滤（成本 + 语义不一致）**：`logWithCtx` 原先无条件执行
  `customHooksWithCtx`，即使该日志级别在本地根本不会输出。典型后果——`WithLevel("info")` 下调用
  `DebugWithCtx` 仍会触发 SLS 上报与字段提取分配，既浪费网络/CPU，也与 `customHooks`（走 zap core、
  受级别过滤）行为相悖，还违背 README「SLS 只上报 Warn 及以上」的描述。现当 `Core().Enabled(level)` 为
  false 时直接返回原始字段。
  - 影响面：级别**启用**时行为完全不变；级别**关闭**时不再向钩子透传该条日志（与 zap core 的 `customHooks`
    对齐）。`Panic`/`Fatal` 恒高于任何 minLevel，不受影响。

- **`grpcLogger.V` 详细级别语义写反（生产刷屏）**：`V` 原实现 `return l.verbosity <= level`，在
  `verbosity=0`（`ReplaceGRPCLoggerV2` 的固定初值）下 `V(1)/V(2)...` 全部返回 true，导致 gRPC 内部
  大量 `grpclog.V(n>0)` 详细日志以 Info 输出。改为 `l.verbosity >= level`，对齐 grpclog/glog 约定
  （verbosity 为「允许的最大详细级别」），`verbosity=0` 时仅放行 `V(0)`。
  - 守护：新增 `grpcLogger_test.go::TestGRPCLoggerVerbosity`。

- **`SLSHook.Close()` 自锁死锁（阻断级）**：`printCloseStats` 原在持有 `h.errorMu.RLock()` 期间调用
  `WarnWithCtx`，而该日志会再入本 hook → `SendLog`（producer 已关闭 → 失败）→ `RecordFailure` 去 `Lock`
  **同一把** `errorMu`，同协程读写自锁，使 `TestSLSIntegration` 永久挂起（曾表现为 `go test` 10 分钟超时）。
  现改为「快照错误字段 → 先 `RUnlock` → 再记日志」，并在 `Hook` 入口对 `StateStopping/StateStopped`
  直接放行，杜绝关闭期再入发送。

### 健壮性（Hardening）

- **`SLSHook.Close()` 二次关闭 panic 风险**：当健康检查把状态置为 `StateFailed` 后，状态机幂等判断会放行，
  并发/重复 `Close()` 可能对 `healthCheckStop` channel 二次 `close` 触发
  `panic: close of closed channel`。新增 `closeOnce sync.Once` 兜底。

- **`Init` 与并发日志的数据竞争（改原子读写）**：`defaultLogger`/`defaultCallerLogger`/`customHooks`/
  `customHooksWithCtx` 四个包级变量由裸指针/切片改为 `atomic.Pointer`。`getDefaultLogger`/`Get`/`Sync`/
  `logWithCtx`/`ExecuteCustomHooksWithCtx` 读侧改 `Load()`，`Init` 写侧改 `Store()`；`customHookCore` 在
  构建时持有钩子快照，`Write` 不再读包级切片。消除「运行期重复 Init 与并发日志」在内存模型层面的数据竞争。

- **初始化路径 `panic` 改为 `error` 传播**：`Init`（终端/文件构建失败）、`buildWriteSyncer`（rotatelogs
  失败）、`ensureDirExists`/`buildLogFilePath`（目录创建失败）不再 `panic`，统一以 `error` 逐级返回，
  使 `Init` 的 `(*zap.Logger, error)` 错误分支真正可达，调用方可集中处理「日志系统初始化失败」。

- **`verifyProducerState` 假验证 + 污染生产 LogStore**：`SendLog` 是异步的，返回 nil 仅代表入队而非发送成功，
  旧「发一条 `__topic__:health-check` 测试日志」既是假验证又会向 LogStore 写污染数据。改为仅校验 producer
  句柄与状态是否就绪，运行期健康仍由 `performHealthCheck` 基于失败率判定。

- **`BenchmarkHighConcurrency` 假基准**：原用 `b.N / concurrency` 手动起 goroutine，首次运行 `b.N=1<100`
  时 `perGoroutine=0`，所有 goroutine 不记任何日志却仍报告 ns/op。改用 `b.RunParallel`，由框架决定并发与迭代。

- **测试清理死代码**：`cleanupBenchmarkFiles` 去掉未使用的 `*testing.B` 形参（`TestMain` 曾传空 `&testing.B{}`）；
  删除声明即未使用的包级 `syncWaitGroup` 与随之无用的 `sync` 导入。

- **`extractContextFields` 预分配**：`var fields []Field` 改为 `make([]Field, 0, 4)`，覆盖 request_id /
  caller_func / trace_id / span_id 上限，省去热点日志路径多次 append 扩容重分配（`nil` ctx 仍返回 nil）。

### 新增（Added）

- 从 `Hook` 中抽出纯函数 `slsFieldValue(Field) string`，使字段序列化可脱离网络 Producer 单独单元/模糊测试。
- 新增测试矩阵（补齐包质量基线要求的纯函数 + fuzz 覆盖）：
  - `sls_hook_test.go`：`validateSLSConfig` / `isValidEndpoint` / `slsFieldValue` 表驱动 +
    `FuzzValidateSLSConfig` / `FuzzSlsFieldValue`。
  - `router_test.go`：`mergeRouteConfig` / `getRouteKey` / `buildLogFilePath` / `extractContextFields`。
  - `option_test.go`：`WithLevel` / `WithFormat` / `apply(nil)` / `WithSave` / 文件选项值域防护 / `getLevelSize`。

### 说明（Docs）

- `Init` 中同步失败时的 `fmt.Printf` 补充注释，明确其为**启动自举例外**（logger 尚未就绪，不能回调自身记日志），
  不违反「全仓禁止 `fmt.Printf` 输出业务日志」的统一出口规范。

### 第二版评审收口（Round 2）

- **`ExecuteCustomHooksWithCtx` 的 caller 栈深度错误（语义 bug）**：原用固定 `runtime.Caller(2)` 取调用者，
  但「直接调用 `ExecuteCustomHooksWithCtx`」与「经 `*WithCtx → logWithCtx → executeHooksIgnoreError` 调用」
  两条路径栈深不同，`Caller(2)` 两条都取不到用户代码（拿到的是 logger 内部帧），使上报到 SLS 的 `caller` 字段恒错。
  改为 `callerOutsideLogger()`：用 `runtime.Callers` + `CallersFrames`（会展开内联帧）遍历到**第一个不属于
  logger 包的帧**作为用户调用点，对两条路径都稳定正确。包边界前缀带末尾点号（`.../pkg/logger.`），
  避免误匹配 `.../logger_test`、`.../loggerutil` 等同前缀兄弟包。
  - 守护：新增外部测试包 `caller_ext_test.go::TestCustomHookWithCtxCaller`（覆盖两条路径均定位到用户文件）。

- **`Init()` 默认行为文档与代码矛盾**：`defaultIsSave=true`（默认写 `logs/app.log`、JSON、info），但 `Init` 文档注释
  与 README 均称「终端输出、debug 级别」。以代码为准修正注释与 README，并说明终端输出需显式 `Init(WithSave(false))`。

- **`SLSConfig.Timeout` / `SendTimeout` 为未接线死配置**：`aliyun-log-go-sdk` 的 `ProducerConfig` 未暴露对应超时项，
  二者被赋默认值、被 `validateSLSConfig` 校验却从未传给 producer，令调用方误以为 `Timeout: 30` 生效。
  现明确标注为**预留字段**（仅取值校验、不影响发送），保留字段以避免破坏公开 API。

- **`trace_id` 误导测试**：多个测试用 `context.WithValue(ctx, "trace_id", ...)` 字符串 key 并注释「自动包含 trace_id」，
  但 `extractContextFields` 只从 OTel `SpanContext` 提取，字符串 key 从不生效。移除误导行，新增
  `TestExtractContextFieldsTraceID`：用真实 `trace.SpanContext` 验证 `trace_id/span_id` 提取，并反证字符串 key 不被提取。

- **基准测试混入 IO**：`BenchmarkInfoWithCtx`/`BenchmarkErrorWithCtx`/`BenchmarkSyncVsAsync`/`BenchmarkContextExtraction`
  等纯 CPU 基准原以 `WithSave(true)` 写真实文件，磁盘 IO 污染 ns/op。改配 `WithNoPrint(true)`（丢弃输出）隔离 IO；
  含网络 IO 的 `BenchmarkSLSHook`/`BenchmarkMixedScenarios` 显式标注并保留无凭据自动 Skip。

- **`InitRouter` 二次调用静默 no-op**：受 `sync.Once` 保护，初始化后再次携带 `routes` 调用不会重配路由却静默返回 nil，
  令调用者误以为已重配。新增 `routerInited` 原子标志，重复携带路由时返回明确错误（增量注册请用 `RegisterRoute`）。

- **测试工具函数集中**：`getEnv` 由 `method_ctx_test.go` 移至 `testutil_test.go`，供 `method_ctx_test.go` 与
  `benchmark_test.go` 复用，避免任一文件加构建标签或拆分后跨文件编译失败。

- **README Benchmark 参考表**：历史表既无测量环境又含假基准遗留数据，删除并改为「以目标环境实测为准」。

### 第三版评审收口（Round 3）

- **测试去 `//nolint:staticcheck`**：`TestExtractContextFieldsTraceID` 的反例原用裸 string key + `//nolint:staticcheck` 规避 SA1029。
  改用函数内局部类型 `type traceIDTestKey string` 作 key，既保留「非 SpanContext 来源不被提取」的测试意图，又不触发 lint（与仓内
  `jwtAuth.go` 等用类型化 key 的惯例一致）。注：本仓并无「全局禁 nolint」硬规则（仓内现有 ~25 处带理由的 `//nolint`），此处属主动选用更干净写法。

- **SLS 网络测试收敛到 `//go:build integration`**：`TestSLSIntegration`/`TestMixedLogging` 会用假凭据创建真实 Producer（网络 IO、
  失败噪音、`Close` 最多阻塞 30s），违反「单测不做网络 IO」。移至 `sls_integration_test.go`（带 `//go:build integration`）；
  默认 `go test ./pkg/logger/` 不编译，包单测耗时从 ~4.25s 降至 ~0.04s。集成跑法：`go test -tags integration ./pkg/logger/`。

- **基准 IO 口径统一**：`BenchmarkModuleLog`/`BenchmarkConcurrentLogging`/`BenchmarkRouteLookup`/`BenchmarkHighConcurrency`/
  `BenchmarkMixedScenarios` 的默认 logger 补 `WithNoPrint(true)`（丢盘）；因 `RouteConfig` 无丢弃开关，路由 logger 仍写文件，
  故在各自 doc 注释显式标注「含路由文件写入 IO、非纯 CPU 口径」（保持函数名不变以免破坏 CI bench 基线）。

- **`TestMain` 清理改为条件触发**：仅当 `-test.bench=` 非空（基准模式）时才调 `cleanupBenchmarkFiles`，避免 `go test -run TestXxx`
  误删开发者在 `pkg/logger/` 下手放的同名文件。

- **`loggerPkgPath` 硬编码注释**：补充「模块路径 fork/重命名时需同步更新」的风险说明（当前仓库路径稳定，属可接受工程取舍）。

- **`InitRouter` 幂等语义注释**：明确「已初始化后重复调用且 `len(routes)==0`」为幂等 no-op、返回 nil，与「携带路由重复调用报错」区分。

### 第四版评审收口（Round 4）

- **恢复丢失的 SLS 集成测试（回归修复）**：Round 3 记录称 `TestSLSIntegration`/`TestMixedLogging` 已移至
  `sls_integration_test.go`，但核盘发现该文件**从未落盘**——两个网络测试已从 `method_ctx_test.go` 删除却无承接文件，
  集成覆盖实际丢失。现从 git HEAD 忠实恢复 `sls_integration_test.go`（带 `//go:build integration`、**不含 TestMain**）。
  验证：`go vet -tags integration ./pkg/logger/` 编译通过，全包仅 `main_test.go` 一个 `TestMain`，二者不冲突
  （回应评审 P2-4 对 TestMain 冲突的担忧——冲突不存在，根因是文件缺失）。

- **`TestMain` 迁出 benchmark_test.go（评审 P2-1）**：`TestMain`/`isBenchmarkRun`/`cleanupBenchmarkFiles` 移至独立的
  `main_test.go`，专注包级 setup/teardown。避免 benchmark_test.go 未来加构建标签或被拆分时连带 TestMain 丢失，
  也让「同包唯一 TestMain」归属清晰。功能与清理逻辑（基准模式条件触发）不变。

- **评审 P2-2 / P2-3 主动不改**：`cleanupBenchmarkFiles` 改前缀扫描（P2-2）与 `isBenchmarkRun` 边界（P2-3）评审均标注
  「非 bug / 不必现在做」；且 `filepath.Glob` 会改变删除范围（行为变更风险），遵循「无收益不改动」原则维持现状。

## 已知限制

- 本机（Windows/MinGW）执行 `go test -race` 报 `exit status 0xc0000139`（gcc 运行时问题，与本包代码无关），
  `-race` 证据需在 Linux/CI 上采集。
- `Init()` 仍约定为**启动期单次调用**：`defaultLogger`/`defaultCallerLogger`/`customHooks*` 已改原子读写，
  消除了「运行期重复 Init 与并发日志」在内存模型层面的数据竞争；但 `Init` 内多次 `Store` 并非单一原子事务，
  理论上可观察到「钩子已更新而默认 logger 尚未发布」的中间态，故仍不建议在流量期反复 `Init`。
- `globalRouter` 仍为非原子包级变量，仅在启动期 `InitRouter` 的 `sync.Once` 内写入一次；`GetMetrics`
  （Prometheus scrape 回调）读取它与启动期写入之间存在理论竞争，因实际只写一次且早于暴露 metrics，风险可接受。
