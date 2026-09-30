---
name: logger-package
description: Guides work on the global pkg/logger base (zap foundation, WithCtx methods, Option config, file routing, SLS hook, graceful shutdown) and how infrastructure packages (nacoscli/goredis/es/etcdcli/...) consume it without building their own Logger abstraction. Use when editing pkg/logger, adding log call sites or request_id propagation across packages, or reviewing whether a package should inject a Logger.
---

# pkg/logger 日志底座开发指南

`pkg/logger` 是全仓**唯一日志出口**（zap 底座，仓内约 140 个非测试 Go 文件直接依赖）。
本文档记录该底座的设计契约、消费方收口范式与已知陷阱。
**通用 Go 规范见 `project-conventions`；包交付文件矩阵与测试命名见 `package-quality-baseline`。**

---

## 一、核心契约（不可违背）

| 契约 | 说明 |
|------|------|
| **唯一出口** | 业务/基础包一律 `logger.Debug/Info/Warn/ErrorWithCtx(ctx, msg, fields...)`，禁止直接用 `zap` / 标准库 `log` / `log/slog` / `fmt.Printf` |
| **强类型字段** | 字段用 `logger.String/Int/Int64/Float64/Bool/Err/Any/Duration`，不用裸 `zap.Any` 拼字符串 |
| **request_id 直读** | request_id 一律来自 `logger.ContextKeyRequestID`，由 gin/grpc middleware 注入，**底座与消费包都不提供注入点** |
| **Init 单次** | `Init()` 以 `atomic.Pointer` 发布 `defaultLogger`/`defaultCallerLogger`/`customHooks*`，读写均原子、不再构成 data race；但仍约定**启动期调用一次**（多次 Store 非单一原子事务，流量期反复 Init 可见中间态） |
| **自举例外** | `Init` 内同步失败用 `fmt.Printf` 降级——此刻 logger 尚未就绪，回调自身会递归触发 `checkNil/Init`，属有意豁免 |

---

## 二、消费方收口范式（参考 nacoscli / goredis）

基础设施包**不定义 `Logger` 接口、不提供 `WithLogger` / `RequestIDExtractor`**。日志调用点直调 `logger.*WithCtx`，
可测试性由**包私有钩子变量**提供。见 [`pkg/nacoscli/logging.go`](../../../pkg/nacoscli/logging.go)、
[`pkg/goredis/logging.go`](../../../pkg/goredis/logging.go)：

```go
// logging.go — 包私有，仅用于单元测试替换以捕获日志，不对外暴露
var (
	logDebug = logger.DebugWithCtx
	logInfo  = logger.InfoWithCtx
	logWarn  = logger.WarnWithCtx
	logError = logger.ErrorWithCtx
)
```

替换这些变量等价于改全局状态，单测必须遵守（由 `logging_test.go` 的 captureLogs 强制）：
- 生产代码视为只读，不得提供 `SetLogger` 类运行期替入入口；
- 替换期间禁止 `t.Parallel()`；禁止同一测试内嵌套/重复捕获；
- 写入方可能是后台 goroutine（如 SDK 回调），收集器必须带互斥锁。

**为何不依赖倒置**：mono-repo 内再造一层接口无法真正解耦，只会多出一份 `key,value → logger.Field` 的翻译并丢字段类型；
若将来要换底层实现，正确抽象点是 `pkg/logger` 自身（统一收口）。
**失效边界**：若某包要拆成独立模块对外发布、或需同进程多套日志实现，则改回封装/注入形态并登记破坏性变更。

---

## 三、文件结构参考（`pkg/logger/`）

| 文件 | 职责 |
|------|------|
| `logger.go` | `Init`（原子发布、遇错返回 error 不 panic）、`checkNil`（sync.Once 兜底）、`log2Terminal/log2File`、`buildEncoder/buildWriteSyncer/buildCore`、`customHookCore` |
| `method.go` | `Debug/Info/Warn/Error/Panic/FatalWithCtx`、`Module*WithCtx`、`logWithCtx`（级别门控）、`Sync/Shutdown/RegisterCloser`、`GetMetrics/RecordDrop` |
| `option.go` | `Option`/`FileOption`（Functional Options，`defaultOptions`+`apply`，nil 防御） |
| `type.go` | `Field = zapcore.Field` 及各强类型构造器、`CustomHook`/`CustomHookWithCtx` |
| `router.go` | `LogRouter` 按模块/级别路由、`mergeRouteConfig`、`extractContextFields`、`ContextKeyRequestID`/`ContextKeyCallerFunc` |
| `sls_hook.go` | 阿里云 SLS `SLSHook`（状态机 CAS、健康检查、`slsFieldValue` 字段序列化） |
| `metrics.go` | Prometheus Collector（`logger_dropped_entries_total`/`logger_router_active_loggers`） |
| `grpcLogger.go` | `ReplaceGRPCLoggerV2` 桥接 gRPC 日志到 zap |

一个逻辑变更通常只涉及 1~2 个文件。每个源文件 `xxx.go` 对应 `xxx_test.go`。

---

## 四、两条钩子路径与「级别门控」（关键陷阱）

底座有两条自定义钩子路径，**都必须受日志级别过滤**：

1. `customHooks`（`CustomHook`，无 ctx）——经 `customHookCore.Write` 执行，由 zap core 天然按级别过滤。
2. `customHooksWithCtx`（`CustomHookWithCtx`，带 ctx）——经 `logWithCtx` 执行。

历史陷阱：`logWithCtx` 曾**无条件**跑钩子，导致 `WithLevel("info")` 下 `DebugWithCtx` 仍触发 SLS 上报与
字段分配，与路径 1 行为不一致、也违背 README「只上报 Warn 及以上」。修复见 `method.go`：

```go
func logWithCtx(ctx context.Context, level zapcore.Level, msg string, fields ...Field) []Field {
	if !getDefaultLogger().Core().Enabled(level) {
		return fields // 级别未启用：不提取 context 字段、不跑钩子，交由 zap 丢弃
	}
	// ... 提取字段 + executeHooksIgnoreError
}
```

> `Panic`/`Fatal` 恒高于任何 minLevel，`Enabled` 恒为 true，不会被跳过。修改此处务必保持两条路径对称。

---

## 五、SLS 字段序列化陷阱（zap 浮点位编码）

`zap.Float64/Float32` **不是**把数值直接存进 `Field.Integer`，而是先 `math.Float64bits`/`math.Float32bits`
存**位模式**。序列化回字符串必须按位还原（`sls_hook.go` 的 `slsFieldValue`）：

```go
case zapcore.Float64Type:
	return strconv.FormatFloat(math.Float64frombits(uint64(field.Integer)), 'f', -1, 64)
case zapcore.Float32Type:
	return strconv.FormatFloat(float64(math.Float32frombits(uint32(field.Integer))), 'f', -1, 32) // FormatFloat 需 float64
```

❌ 直接 `float64(field.Integer)` 会得到完全错误的数字（历史数据损坏 Bug，已由 `TestSlsFieldValue` + `FuzzSlsFieldValue` 锁死）。
新增字段类型分支时，同步补 `slsFieldValue` 的用例。

---

## 六、生命周期、并发与关闭

- 关闭顺序（`Shutdown`）：已注册 `Closer`（如 SLS Hook）→ 路由器 `RouterSync` → 默认 `Sync`。
- **包级状态原子化**：`defaultLogger`/`defaultCallerLogger`/`customHooks`/`customHooksWithCtx` 均为
  `atomic.Pointer`——`Init` 只 `Store`、读侧（`getDefaultLogger`/`Get`/`Sync`/`logWithCtx`/
  `ExecuteCustomHooksWithCtx`）只 `Load`；`customHookCore` 构建时持有钩子快照，`Write` 不读包级切片。
  因此「运行期重复 `Init` 与并发日志」不再是内存模型层面的 data race。
- **初始化不 panic、返回 error**：`Init`/`buildWriteSyncer`/`ensureDirExists`/`buildLogFilePath` 遇错一律
  `return err` 逐级上抛（rotatelogs 失败、目录创建失败等），使 `Init` 的 `(*zap.Logger, error)` 错误分支可达。
- `SLSHook` 用状态机 CAS（Running/Starting→Stopping）做幂等关闭；`healthCheckStop` 的 `close` 由
  `closeOnce` 兜底，防 `StateFailed` 态下并发/重复 `Close` 触发 `close of closed channel`。
- **`grpcLogger.V` 方向**：grpclog/glog 语义是 `V(level) = verbosity >= level`（verbosity 为允许的最大详细级）。
  历史写反为 `verbosity <= level`，`verbosity=0` 时 `V(1)/V(2)...` 全 true → gRPC 内部详细日志刷屏（回归见 `grpcLogger_test.go`）。
- `droppedEntries`/SLS 计数用 `atomic`；路由器 map 用 `RWMutex`；运行期只维持固定 logger 实例。
- **钩子里的 caller 定位（真实语义 Bug）**：`ExecuteCustomHooksWithCtx` 绝不能用固定 `runtime.Caller(2)` 取调用者——
  「直接调用」与「经 `*WithCtx → logWithCtx → executeHooksIgnoreError`」两条路径栈深不同，固定 skip 两条都取错（拿到 logger 内部帧），
  使上报 SLS 的 `caller` 字段恒错。正确姿势用 `callerOutsideLogger()`：`runtime.Callers` + `CallersFrames`（会展开内联帧）
  遍历到**第一个不属于 logger 包的帧**；包边界前缀必须带末尾点号 `.../pkg/logger.` 以免误匹配 `.../logger_test`、`.../loggerutil`。
  回归见外部测试包 `caller_ext_test.go`（in-package 测试因函数名同样带 logger 前缀会被跳过，无法验证，故必须用外部包）。
- **`InitRouter` 不可重入**：受 `sync.Once` 保护，初始化后再次携带 `routes` 不会重配路由。用 `routerInited atomic.Bool` 区分首次/后续，
  重复携带路由时**返回明确 error**（而非静默 no-op）；增量注册走 `RegisterRoute`。

> **关闭期再入死锁（真实 Bug）**：`SLSHook.printCloseStats` 绝不能在**持有 `errorMu.RLock()` 期间**调用
> `WarnWithCtx`/`InfoWithCtx`——这类日志会再入本 hook 的 `Hook`→`RecordFailure` 去 `Lock` 同一把 `errorMu`，
> 同协程读写自锁（曾令 `TestSLSIntegration` 永久挂起）。正确姿势：先快照错误字段、`RUnlock`，再记日志；
> 并在 `Hook` 入口对 `StateStopping/StateStopped` 直接 `return nil`，从根上杜绝关闭期再入发送。
> 泛化教训：**任何日志钩子里都不得持锁回调自身日志管道**。

---

## 七、测试与交付基线

- **命名（方案 A）**：英文函数名 + 中文 doc 注释 + 中文子测试名，权威定义见 `package-quality-baseline` 第二节。
- **纯函数优先**：`validateSLSConfig`/`isValidEndpoint`/`slsFieldValue`/`mergeRouteConfig`/`getRouteKey`/
  `extractContextFields`/Option 校验分支都无网络/全局状态依赖，表驱动 + comma-ok 断言，不 `t.Parallel()`（涉及全局变量替换时）。
- **Fuzz 守不变量**：`FuzzValidateSLSConfig`（不 panic、通过校验则边界必满足）、`FuzzSlsFieldValue`（数字类型结果可被 strconv 解析回、不含裸换行）。
- **集成测试**：含网络/外部依赖的用例（如 SLS `TestSLSIntegration`/`TestMixedLogging` 会创真实 Producer）**必须**放 `//go:build integration`
  标签文件（`sls_integration_test.go`），保证默认 `go test` 为纯本地（CPU/内存）测试；跑集成：`go test -tags integration ./pkg/logger/`。
- **测试不用 `//nolint` 绕 lint**：向 `context.WithValue` 传自定义值时用类型化 key（`type xKey string`/`struct{}`）而非裸 string +
  `//nolint:staticcheck`（SA1029）；根治优于抑制，与仓内 `jwtAuth.go` 等类型化 key 惯例一致。
- **`TestMain` 清理要按模式条件触发**：只删 benchmark 产物时用 `isBenchmarkRun(os.Args)`（判 `-test.bench=` 非空）门控，
  避免 `go test -run TestXxx`（非基准）误删包目录下手工放置的同名文件。
- **基准不手动管并发**：并发类 benchmark 用 `b.RunParallel`（每 P 局部计数器），禁用 `b.N / concurrency`
  起 goroutine 的写法——`b.N<concurrency` 时会得到 `perGoroutine=0` 的**假基准**。
- **纯 CPU 基准不写磁盘**：`BenchmarkInfoWithCtx` 等测量编码/缓冲开销的基准用 `WithSave(true, WithNoPrint(true))`——
  `buildWriteSyncer` 命中 `noPrint` 会返回 `nopWriteSyncer{}` 丢弃输出，从而把磁盘 IO 从 ns/op 中隔离出去；
  含文件/网络 IO 的基准（路由写入、`BenchmarkSLSHook`/`BenchmarkMixedScenarios`）须显式标注且无凭据时 `Skip`。
- **`Init()` 默认写文件**：`defaultIsSave=true`（JSON/info、`logs/app.log`），终端输出需显式 `Init(WithSave(false), WithFormat("console"))`；
  写文档/示例时以代码默认值为准，勿沿用旧「终端/debug」描述。
- **交付文件**：新增/修改须同步 `CHANGELOG.md`（记 Fixes/Added/破坏性变更）与 README 对应小节。
- 本机（Windows/MinGW）`go test -race` 报 `0xc0000139`（gcc 运行时问题，与本包无关），`-race` 证据在 Linux/CI 采集。

---

## 八、改动检查清单

| # | 步骤 |
|---|------|
| 1 | 判定变更属于哪条路径（Init/配置、WithCtx 方法+钩子门控、路由、SLS、关闭、消费包收口） |
| 2 | 若动钩子/级别相关：确认 `customHooks` 与 `customHooksWithCtx` 两条路径过滤语义一致 |
| 3 | 若动字段序列化：核对 zap 位编码陷阱，补 `slsFieldValue` 用例 |
| 4 | 消费包只调 `logger.*WithCtx`，**不得**新增 `Logger` 接口/`WithLogger`（见第二节失效边界） |
| 5 | `gofmt -l -local github.com/18721889353/sunshine` + `go build ./pkg/logger/...` + `go test ./pkg/logger/`（-race 见第七节） |
| 6 | 同步 `CHANGELOG.md` 与 README 小节 |
