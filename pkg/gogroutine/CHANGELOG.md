# Changelog

本文件记录 `pkg/gogroutine` 的显著变更。

## Unreleased

### 新增

- **一对一测试文件矩阵补齐**：`instance_test.go` 新建（覆盖 `instance.go` 的 poolInstance 提交/降级/统计/生命周期，含 4 个并发守护断言）、`pool_test.go` 重写（覆盖 `pool.go` 的全局池创建与并发初始化）、共享 helper 收敛到 `test_helpers_test.go`（`taskWaitTimeout`/`waitDone`/`resetForTest`/`resetMetrics`/`mockMetrics`）。源文件与测试文件现在严格一对一，helper 不再散落在业务测试文件中重复定义。
- **`benchmark_test.go`：7 个无 RTT 回归基线**——提交热路径、已取消短路、执行器、实例池提交、配置归并、实例构造、批量错误聚合，全部 `b.ReportAllocs()`，基准阶段日志静音（`silenceBenchLogs`，本包仅 INFO/WARN，提到 error 级即全过滤）避免日志 IO 污染读数与打散输出行。实测读数汇总表与口径解读见 [README「基准测试」](README.md#基准测试)（数值不在两处重复维护）。
- **`fuzz_test.go`：3 个不变量守护**——`FuzzClampPoolSize`（容量裁剪恒在 `[10,10000]` 且幂等）、`FuzzNormalizeCtx`（归一不返回 nil、跳过判定与 `Err()` 一致）、`FuzzCollectBatchErrors`（全 nil 聚合为 nil、`errors.Is` 不丢错）；种子语料随常规 `go test` 执行，实测 `go test -run='^Fuzz'` 0.037s 全过。
- **测试命名方案 A 全量收口**：168 个测试函数统一为 `Test<被测方法><场景>` 全英文驼峰标识符（doc 注释中文、子测试名中文），清零 `TestXxx_YYY` 下划线风格与注释中的旧名引用，与 `goredis`/`nacoscli` 等已收口包一致。
- **评审 R2 收口的 8 个守护测试（168→176）**：`TestInitAfterLazyInitIgnoresOptions`（R2-P1-1）、`TestNewWithPoolSizeDoesNotOverrideCapacity`（R2-P1-2）、`TestPoolInstanceTaskEmitsMetrics`/`TestPoolInstancePanicCountsScopedToInstance`/`TestPoolInstanceFallbackEmitsMetrics`/`TestPoolInstanceTaskCreatesSpan`（R2-P1-3）、`TestGoBatchWithResultNilTask`/`TestGoBatchWithNameNilTaskSkipped`（R2-P1-4）。Span 导出断言用测试内自带的 `memorySpanExporter`——otel `tracertest` 子模块不在本模块依赖图中（import 报 `no required module provides package`），`sdktrace` 包本身可用，故自带实现。

### 变更

- **README 按 package-quality-baseline 模板重写**：新增架构概览（文件树 + 核心设计原则）、职责边界、场景驱动的使用选择（6 个场景块，含内部行为与注意）、结构体/Option 表、逐函数 API 速查、错误处理表、「集成测试不适用」说明、性能基线与模糊测试章节（含 `-race` 诚实声明）；原「生产案例」内容并入对应场景块保留。
- **`resetForTest` 对 `globalPool` 的重置改为持 `globalPoolMu`**：与 `pool.go` 新引入的锁保持「受锁字段永不裸访问」一致性（调用方为串行 Test/Benchmark，属防御性对齐而非解除已知竞争）。
- **测试等待治理：判定路径的裸 `time.Sleep` 清零**：正向判定（计数/panic 指标/监控回调）由「固定 sleep 后单次读数」改为 `waitUntil` 轮询 + deadline（12 处）；负向断言（验证「不发生」）抽成命名常量 `negativeAssertionWindow`（4 处，含三段注释：为何只能固定等待 / 取值依据 / 为何不 flaky）；时序让路与任务模拟抽成 `taskStartPad`/`simulatedTaskWork`/`overlapPad`；基于 happens-before 可直读的冗余 sleep 直接删除（`wg.Wait` 后 `DecRunning` 未落地、`close(done)` 晚于 panic handler 等顺序陷阱改用轮询）。副产品：单测总耗时从 2.5s 级降至每轮 ~1.64s（`-count=2` 实测合计 3.272s）。
- **panic 日志 message 统一（R2-P1-3 副作用）**：全局池原 `goroutine panic recovered`、实例池原 `gogroutine: task panic recovered`，现统一为 `gogroutine: task panic recovered`（字段 name/panic/duration/stack 不变）——若运维按 message 关键字过滤日志，需同步调整旧关键字。
- **实例降级路径补齐 `Metrics.IncFallback` 回调**：原降级只有日志与实例原子计数，不触达注入的采集器；现与全局池一致。
- **基准读数重采（R2 改造后，2 轮）**：`PoolInstanceCtxGo` 48→566 B、1→10 allocs（实例池接入统一观测管线的预期代价）、`SubmitTaskHotPath` 11→10 allocs（去除双层包装）、`PoolConfigApply` 48→32 B（删字段）、`NewInstance` 25→30 allocs（hooks 闭包）；数值与口径统一见 [README「基准测试」](README.md#基准测试)，不在两处重复。

### 修复

- **GoBatch* 传入已取消 ctx 永久死锁（阻断级，R1-1）**：`shouldSkipSubmit` 跳过任务时，携带 `wg.Done()` 的包装闭包从未执行，`wg.Wait()` 永久阻塞——调用方协程泄漏、批量接口挂死。`submitTask` 改为返回受理信号（bool），跳过时当场归还 WaitGroup 计数；`GoBatchWithResult` 对被跳过位置记 `errs[i] = ctx.Err()`，不再静默留零值。先判为「已取消即快速返回的预期行为」——原守护用例 `TestBatchWithCancelledCtx` 两分支均无断言且注释把死锁写成预期，掩盖了真实缺陷，本轮以真断言用例替换。
  - 影响面：`GoBatch`/`GoBatchWithName`/`GoBatchWithResult` 在 ctx 已取消（或超时）时的全部调用；单任务 `Go*` 路径原本就只跳过不阻塞，不受影响。
  - 守护：`TestGoBatchCancelledCtxReturns`、`TestGoBatchWithResultCancelledCtx`（批量立即返回 + 错误位为 `context.Canceled`）、`TestGoBatchWithResultMixedSuccessAndError`。
- **nil ctx 提交直接 panic（可用性，R1-2）**：`Go`/`GoWithName`/`GoWithTimeout`/`GoBatch*`/`poolInstance.CtxGo` 在 nil 接口上调 `ctx.Done()` 或 `context.WithTimeout(nil, ...)` 立即 panic。统一入口 `normalizeCtx` 把 nil 归一为 `context.Background()`，`shouldSkipSubmit` 补 nil 防御——对齐本仓「API 参数 nil 校验规范」与 `gohttp.Request(nil)` 的回落模式。
  - 影响面：所有接受 `ctx` 的导出 API 的 nil 输入（此前 panic，现在等价 Background 正常执行）；非 nil ctx 的判定逻辑无任何变化。
  - 守护：`TestGoNilCtxFallsBackToBackground`、`TestGoWithNameNilCtxFallsBackToBackground`、`TestGoWithTimeoutNilCtxFallsBackToBackground`、`TestGoBatchNilCtxFallsBackToBackground`、`TestPoolInstanceNilCtxFallsBackToBackground`，以及 `FuzzNormalizeCtx` 的随机输入冲击。
- **池字段并发数据竞争（数据竞争，R1-3）**：`poolInstance.pool/capacity` 无锁读写（`Release` 置 nil 与 `CtxGo`/`SetCap`/`GetCap` 并发即 race）、全局 `globalPool` 裸读与首次 `Init` 写并发即 race。修复：实例侧由 `mu` 统一保护并引入 `currentPool()` 读快照（`Release` 写锁内置 nil），全局侧引入 `globalPoolMu` + `currentGlobalPool()` 快照，`SetCap`/`GetCap`/`Release` 全部入锁。
  - 影响面：并发调用 `Release`/`SetCap` 与任务提交、或并发 `Init` 与池读取的多 goroutine 进程；串行使用路径行为不变。
  - 守护：`TestCurrentGlobalPoolConcurrentWithInit`、`TestPoolInstanceConcurrentReleaseAndSubmit`、`TestPoolInstanceSetCapDuringExecution`、`TestConcurrentSubmitAndRelease`。**本轮未跑 `-race` 取证**（Windows/MinGW 报 `0xc0000139`），正确性依据是锁序分析 + 并发守护断言（不 panic、可见性），真实 race 报告需 Linux/CI 采集——详见 README「竞态检测」的诚实声明。
- **R2-P1-1：`Init` 迟到调用 Option 被静默忽略**：先 `Go*`（含包 `init()` 触发的惰性初始化）后 `Init` 时 `sync.Once` 已消费，Option 无任何提示地失效（用户以为池大小 5000、实际 1000）。`Init` 自第二次调用起打 WARN `pool already initialized, Init options ignored`（含被忽略 Option 数），doc 与 README 增加「必须先于任何 `Go*` 调用」醒目警示；不改执行结果（最小改动优先，与 `sync.Once` 语义保持一致）。守护：`TestInitAfterLazyInitIgnoresOptions`。
- **R2-P1-2：`New*` 中 `WithPoolSize` 被 `capacity` 参数静默覆盖**：`newInstance` 新增 `poolSizeSet` 标记，Option 里显式传过 `WithPoolSize` 即打 WARN（容量仍以 `capacity` 为准，不改优先级——改优先级是行为变更，本轮只做显式提示）；Option 表与 `New*` doc 标注「仅对 `Init` 生效」。守护：`TestNewWithPoolSizeDoesNotOverrideCapacity`。
- **R2-P1-3：全局池与实例池 `executeTask` 双实现不一致（可观测性缺口）**：实例池原不建 Span、不触达 `SetMetrics` 注入的采集器、duration 只算不用、panic 不写全局计数——用户用 `New("order-pool", 100)` 创建的池完全脱离 OpenTelemetry。修复：抽出 `observeTask` 统一执行管线（Span → IncRunning → 计数 → defer 清理 → defer recover → 任务体），两池差异经 `taskHooks` 注入（计数归属、实例级 panicHandler）；同时发现全局池原为双层包装（`submitTask` → `Submit` → `executeTask` 再包一层），引入 `rawSubmitter.submitRaw` 绕过外层避免双重埋点。关键实现约束：running 计数器在任务开始时取一次、结束复用同一份——全局池计数器经 atomic 重绑定，双次 Load 会让重绑定窗口内的在途任务把 -1 记到新池，新池 Running 变负导致 `ReleaseAndWait` 永远等不到（该缺陷在全量跑测试时暴露、隔离跑必过，根因已写入 `taskCounter` 字段注释）。作用域守护：实例 panic 仍只记实例计数，不污染全局 `PoolStats` 口径。守护：`TestPoolInstanceTaskEmitsMetrics`、`TestPoolInstancePanicCountsScopedToInstance`、`TestPoolInstanceFallbackEmitsMetrics`、`TestPoolInstanceTaskCreatesSpan`。
- **R2-P1-4：`GoBatchWithResult` 对 nil task 静默假成功**：`tasks[i] == nil` 时包装闭包非 nil 被正常受理，执行期 `tasks[i]()` panic 被 recover，结果位零值且聚合 err==nil——调用方以为全部成功。现循环头检查 nil 并记 `gogroutine: nil task`（`GoBatch`/`GoBatchWithName` 同步跳过），失败在 `errors.Join` 里可被 `errors.Is` 命中。守护：`TestGoBatchWithResultNilTask`、`TestGoBatchWithNameNilTaskSkipped`。
- **R2-P1-5：`Submit` 与 `CtxGo` 降级语义不一致缺设计意图文档**：行为本身是既有设计（README 错误表已记录），本轮在 `Pool` 接口注释与 `Submit` doc 明确「裸提交不降级」的定位与分层理由：需要降级语义用 `CtxGo`/`Go`。纯文档收口，无代码变更。
- **R2-P1-6：`poolConfig.PurgeInterval` 死配置**：字段声明并赋默认值但无任何消费方（也无对应 Option），属「改了不生效」的陷阱；删除字段与默认值，`BenchmarkPoolConfigApply` B/op 48→32 即其体积。

### 兼容性说明

- 导出 API 签名零变更；行为变化仅发生在原缺陷路径（死锁→立即返回、panic→归一执行、race→加锁），正常路径调用方无需改动。
- 测试命名与全仓方案 A 对齐（`goredis`/`nacoscli` 已收口），今后本包新增用例一律沿用；规范权威位置为 `.qoder/skills/package-quality-baseline/SKILL.md` 第二节。
- 基准与模糊测试的运行命令、读数口径统一维护在 README，本文档不重复具体数值（单一事实单一来源）。
- R2 收口不改任何导出 API 签名；新增两条 WARN（`Init` 迟到、`New*` 传 `WithPoolSize`）只影响日志，不改执行结果。两处行为变化需关注：① panic 日志 message 统一为 `gogroutine: task panic recovered`（按旧 message 过滤的告警规则需同步）；② 实例池任务现创建 Span 并触达 `Metrics` 接口——若实例池任务量大，Prometheus/追踪后端的序列数与 Span 量会相应增加（这正是 R2-P1-3 要补齐的能力，代价见 README 基准表 `PoolInstanceCtxGo` 1→10 allocs）。
