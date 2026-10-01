# gogroutine

基于 [ants](https://github.com/panjf2000/ants) 协程池的生产级 goroutine 管理：统一的提交/执行管线（panic 兜底、OpenTelemetry Span、指标采集）、全局池与多实例池双轨、池满降级不丢任务。

## 架构概览

```
pkg/gogroutine/
├── groutine.go        # 全局池 API：Init/Go*/GoBatch*/生命周期/优雅关闭钩子
├── executor.go        # 执行器：ctx 归一、跳过判定、observeTask 统一执行管线（Span+指标+panic 兜底）
├── instance.go        # poolInstance（Pool 接口实现）：提交/降级/统计/实例级 panic 处理
├── pool.go            # 全局池的创建与并发安全访问（globalPoolMu + Once）
├── pool_manager.go    # 多实例管理器：New/Get/Delete/ReleasePool/Stats（sync.Map）
├── metrics.go         # Metrics 接口与全局指标管理器（可运行时替换）
├── options.go         # 配置与 Option（defaultPoolConfig + apply 模式）
└── *_test.go          # 与源文件一对一的测试 + benchmark/fuzz/test_helpers
```

**核心设计原则**：

- **提交与执行分离**：`submitTask` 只决定「受理还是跳过」（nil task / ctx 已取消），执行由 `observeTask` 管线统一负责——跳过信号（bool）让批量调用方能自行归还 WaitGroup 计数。
- **单执行管线**：全局池与实例池共用 `observeTask`（Span、`Metrics` 接口、计数、panic 兜底），差异（计数归属、实例级 panic 处理）经 `taskHooks` 注入——两类池观测行为一致（评审 R2-P1-3，原双实现下实例池不建 Span、不触达 `SetMetrics`）。
- **池满不丢任务**：`Submit` 返回错误（池满/已关闭）时降级为原生 goroutine 执行，同时记 WARN 日志与 Fallback 计数；`WithNonBlocking` 只改变提交方是否阻塞，任务最终仍会执行。
- **nil/取消 ctx 显式归一**：所有导出 API 对 `ctx == nil` 宽容（归一为 `context.Background()`），对已取消 ctx 短路跳过——这是 API 参数 nil 校验规范的统一落点。
- **共享字段永不裸访问**：`globalPool` 由 `globalPoolMu` 保护、`poolInstance` 的 `pool/capacity/panicHandler` 由实例 `mu` 保护，读取一律走快照函数（`currentGlobalPool`/`currentPool`），消除并发竞争。
- **观测零侵入**：otel TracerProvider 未配置时是 noop，Span 无导出开销；`Metrics` 接口可注入 Prometheus 实现，未注入时仅有包内原子计数。

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| goroutine 池化提交、panic 兜底、池满降级 | 是 | 本包 |
| 任务级 OpenTelemetry Span 与 request_id 透传 | 是 | 本包（Span 属性 `gogroutine.*`） |
| 任务业务编排、重试策略 | 否 | 调用方业务代码 |
| 指标存储与查询（Prometheus 拉取） | 否 | 调用方通过 `SetMetrics` 注入实现 |
| 日志落盘/路由 | 否 | `pkg/logger` |
| 请求级 trace 上下文创建 | 否 | `pkg/tracer` |

复核命令：`grep -rn "prometheus.NewRegistry\|zap.New" pkg/gogroutine/`（应无结果——本包不引入存储与日志实现）。

## 使用场景选择

### 场景一：异步任务不阻塞主流程（`Go` / `GoWithName`）

**适用场景**：HTTP handler / MQ 消费者里「发邮件、写缓存、清过期数据」这类 fire-and-forget 任务。

```go
func RegisterHandler(c *gin.Context) {
	user, err := svc.CreateUser(c.Request.Context(), &req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 异步操作：不阻塞响应，ctx 取消时任务自动跳过
	ctx := c.Request.Context()
	gogroutine.GoWithName(ctx, "sendWelcomeEmail", func() {
		mail.SendWelcome(user.Email)
	})
	gogroutine.GoWithName(ctx, "initPoints", func() {
		points.Init(user.ID, 100)
	})

	c.JSON(200, gin.H{"user_id": user.ID})
}
```

**内部行为**：

1. `task == nil` → 静默不执行；
2. `ctx == nil` → 归一为 `context.Background()`；
3. `ctx` 已取消 → 打 WARN `skip submit, context cancelled` 后跳过，不入队；
4. 提交全局池（首次调用时用默认配置惰性 `Init`），池满则降级原生 goroutine（WARN + Fallback++）；
5. 执行期：建 Span → `IncRunning` → 任务本体 → recover/成功计数 → `DecRunning` + 耗时观测。

**注意**：`ctx` 取消只保证「不开始」，不中断已运行的任务；需要中断请在任务内自己 `select ctx.Done()`。

---

### 场景二：带超时控制的任务（`GoWithTimeout`）

**适用场景**：下载、外部调用等需要硬超时兜底的任务。

```go
gogroutine.GoWithTimeout(ctx, "downloadReport", 30*time.Second, func(tctx context.Context) {
	select {
	case <-tctx.Done():
		logger.WarnWithCtx(tctx, "download cancelled or timed out")
	case data := <-downloaded:
		store.Save(data)
	}
})
```

**内部行为**：任务收到的 `tctx` 基于传入 ctx 派生 `context.WithTimeout`，超时后自动取消并由 `defer cancel()` 回收；提交跳过规则与场景一相同。

**注意**：超时只是**信号**，不抢占 goroutine——任务不监听 `tctx` 时超时不会让它停下。

---

### 场景三：批量并发并聚合结果（`GoBatch` / `GoBatchWithName` / `GoBatchWithResult`）

**适用场景**：扇出查询（dashboard 多数据源）、分片同步等「并发跑一批、等全部结束」。

```go
func DashboardHandler(c *gin.Context) {
	tasks := []func() (any, error){
		func() (any, error) { return svc.GetUserProfile(c.Request.Context(), userID) },
		func() (any, error) { return svc.GetUserOrders(c.Request.Context(), userID) },
		func() (any, error) { return svc.GetUserPoints(c.Request.Context(), userID) },
	}

	results, err := gogroutine.GoBatchWithResult(c.Request.Context(), "dashboard", tasks)
	if err != nil {
		logger.WarnWithCtx(c.Request.Context(), "dashboard batch failed", logger.Err(err))
	}

	c.JSON(200, gin.H{
		"profile": results[0],
		"orders":  results[1],
		"points":  results[2],
	})
}
```

**内部行为**：

1. 空切片直接返回（`GoBatchWithResult` 返回 `(nil, nil)`）；
2. 每个任务独立提交：受理则闭包内 `wg.Done()`，跳过则**当场** `wg.Done()`——因此 ctx 已取消时函数立即返回，不会死锁；
3. `GoBatchWithResult` 中被跳过的位置记 `errs[i] = ctx.Err()`（不留零值假成功）；
4. 结束后 `errors.Join` 聚合全部错误：全成功返回 `nil`，任一失败返回可 `errors.Is` 逐个命中的聚合错误。

**注意**：聚合错误不是第一个错误——需要 fail-fast 语义请自己在任务里短路；任务 panic 不影响其他任务（单任务 recover + Panic++）。

---

### 场景四：独立命名池做资源隔离（`New` / `Get` + `Pool` 接口）

**适用场景**：关键业务（订单、支付）不能被旁路大批量任务挤占——给它独立容量。

```go
// 启动时创建（同名重复创建会 panic，适合包级变量）
var orderPool = gogroutine.New("order-processor", 100)

func HandleOrder(ctx context.Context, order *Order) {
	orderPool.CtxGo(ctx, func() {
		svc.ProcessOrder(ctx, order)
	})
}

// 关闭时释放（幂等；释放后 CtxGo 降级为原生 goroutine）
func Shutdown() {
	gogroutine.ReleasePool("order-processor")
}
```

**内部行为**：实例注册进全局 `sync.Map` 管理器（`Stats()`/`ListNames()`/`Count()` 可查）；`Release` 把底层 ants 池置 nil 并停止接收新任务，此后 `CtxGo` 降级为原生 goroutine、`Submit` 返回 `ants.ErrPoolClosed`。

**注意**：实例池与全局池互不影响；需要拿到创建错误（而非 panic）用 `NewWithContext`。实例池任务同样走 `observeTask` 统一管线：建 Span（名 `gogroutine.task.{池名}`）、触达 `SetMetrics` 注入的采集器；panic/降级计数记入实例 `Stats()`（不写全局 `PoolStats`）。容量只认 `capacity` 参数——传 `WithPoolSize` 会打 WARN 但不生效（R2-P1-2）。

---

### 场景五：优雅关闭（`WithGracefulShutdown` / `ReleaseAndWait*`）

**适用场景**：进程退出前让在跑的任务收尾（K8s 终止、Ctrl+C）。

```go
func main() {
	gogroutine.Init(
		gogroutine.WithPoolSize(500),
		gogroutine.WithGracefulShutdown(true),
		gogroutine.WithGracefulShutdownTimeout(10*time.Second),
	)
	gogroutine.RegisterGracefulShutdownHook(func() {
		mq.Close() // 按注册顺序执行，panic 被 recover 不影响后续 hook
	})

	// ... 服务运行 ...

	// 手动路径（未挂信号时）：停收新任务 → 每 10ms 轮询运行中任务数 → 超时返回并 WARN
	gogroutine.ReleaseAndWaitWithTimeout(10 * time.Second)
}
```

**内部行为**：收到 SIGINT/SIGTERM → 执行 shutdown hooks（`sync.Once` 只跑一次）→ `ReleaseAndWaitWithTimeout`（`Release` 停收 → 轮询 `releasePollInterval=10ms` → deadline 到则 WARN `ReleaseAndWait timeout` 并返回）。

**注意**：信号监听只在**显式调用 `Init` 且启用 `WithGracefulShutdown`** 时启动；不调 `Init` 时任务提交走惰性初始化，不挂信号。K8s 的 `terminationGracePeriodSeconds` 建议比关闭超时大 5 秒。

---

### 场景六：指标注入（`SetMetrics` / `PoolStats` / `Stats`）

**适用场景**：把任务数/panic/降级暴露到 Prometheus 或测试断言。

```go
type promMetrics struct{ /* 实现 gogroutine.Metrics 五个方法，转 Prometheus counter/gauge */ }

func setupMetrics() {
	gogroutine.SetMetrics(&promMetrics{}) // 可运行时替换，无需重建池
}

func report() {
	s := gogroutine.PoolStats() // 全局池
	logger.Info("pool stats",
		logger.Int("running", s.Running),
		logger.Int64("panic", s.Panic),
		logger.Int64("fallback", s.Fallback))

	all := gogroutine.Stats() // 所有实例池
	for _, p := range all.Pools {
		logger.Info("instance", logger.String("name", p.Name), logger.Int("cap", p.Cap))
	}
}
```

**内部行为**：未注入 `Metrics` 时只有包内原子计数（`PoolStats`/`Stats` 仍可用）；注入后每次提交/完成/panic/降级回调对应方法。

**注意**：`SetMetrics(nil)` 恢复为「仅包内计数」；指标回调发生在任务热路径上，实现里不要再做重 IO。

---

## 参数/结构体说明

### StatsInfo（全局池统计，`PoolStats()` 返回）

| 字段 | 类型 | 说明 |
|------|------|------|
| `Running` | `int` | 当前运行中任务数 |
| `Waiting` | `int` | 等待队列长度 |
| `Cap` | `int` | 池容量 |
| `Success` | `int64` | 累计成功任务数 |
| `Panic` | `int64` | 累计 panic 次数 |
| `Fallback` | `int64` | 累计池满降级次数 |

### PoolStatsInfo（实例池统计，`Stats()` / `Pool.Stats()` 返回）

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 池名称 |
| `Running` / `Waiting` / `Cap` | `int` | 运行中 / 等待队列 / 容量 |
| `Submit` / `Success` / `Panic` | `int64` | 累计提交 / 成功 / panic |
| `Created` | `time.Time` | 创建时间 |

### Metrics（指标接口，`SetMetrics` 注入）

| 方法 | 触发时机 |
|------|----------|
| `IncRunning(name)` / `DecRunning(name)` | 任务开始 / 结束（含 panic 路径） |
| `ObserveTaskDuration(name, d)` | 任务结束时（含 panic 路径） |
| `IncPanic(name)` | 任务 panic 被 recover 后 |
| `IncFallback(name)` | 池满降级为原生 goroutine 时 |

### Pool（池抽象接口）

`Name` / `SetCap` / `Go` / `CtxGo` / `SetPanicHandler` / `Submit` / `GetRunningNum` / `GetWaitingNum` / `GetCap` / `Release` / `IsFull` / `Stats`——全局池与实例池共用该接口，便于 mock 测试。

其中 `Submit` 是**底层裸提交**：池已关闭/已 Release 时直接返回 `ants.ErrPoolClosed`，**不降级**（R2-P1-5）。需要「池满不丢任务」的降级语义请用 `CtxGo`/`Go`——两者是有意的分层，不是遗漏。

## Option 列表

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithPoolSize(size)` | 全局池大小，自动裁剪到 `[MinPoolSize, MaxPoolSize]` = `[10, 10000]`；**在 `New*` 中不生效**（容量由 `capacity` 参数决定，传入会打 WARN） | `1000` | 仅 `Init` |
| `WithNonBlocking(b)` | 池满时 `Submit` 立即返回错误（不阻塞提交方）；本包仍会降级执行 | `false` | `Init` / `New*` |
| `WithPreAlloc(b)` | 预分配 worker 内存，减少高并发期 GC | `true` | `Init` / `New*` |
| `WithDisablePurge(b)` | 禁止空闲 worker 回收，避免冷启动延迟 | `true` | `Init` / `New*` |
| `WithGracefulShutdown(b)` | 挂 SIGINT/SIGTERM 监听，退出时自动收尾 | `true`（仅调用 `Init` 时生效） | `Init` |
| `WithGracefulShutdownTimeout(d)` | 优雅关闭等待上限 | `30s` | `Init` |

## API 速查

### Init — 初始化全局池（可选）

```go
func Init(opts ...Option)
```

- `sync.Once` 保证并发安全，**仅首次调用生效**；
- **警示：`Init` 必须先于任何 `Go*` 调用**——若某包 `init()` 或后台任务先触发了惰性初始化，池已按默认配置建成，此后 `Init` 的 Option 全部失效（仅打 WARN `pool already initialized, Init options ignored`）：你以为池大小 5000、实际 1000（R2-P1-1）；
- 不调用则首次提交时按默认配置惰性创建；
- **注意**：只有显式调用它（且未禁用 `WithGracefulShutdown`）才会挂退出信号监听。

### Go / GoWithName — 提交 fire-and-forget 任务

```go
func Go(ctx context.Context, task func())
func GoWithName(ctx context.Context, name string, task func())
```

- `Go` 等价于 `GoWithName(ctx, "", task)`（匿名任务在 Span/指标中标记 `anonymous`）；
- nil task 静默跳过；nil ctx 归一 Background；ctx 已取消则跳过并打 WARN；
- **注意**：池满自动降级原生 goroutine，**任务永远不会因为池满而丢失**。

### GoWithTimeout — 提交带超时的任务

```go
func GoWithTimeout(ctx context.Context, name string, timeout time.Duration, task func(ctx context.Context))
```

- 任务参数是派生出的 `WithTimeout` ctx，超时自动取消；
- **注意**：超时是信号不是抢占，任务须自己监听 `ctx.Done()`。

### GoBatch / GoBatchWithName — 批量执行并等待

```go
func GoBatch(ctx context.Context, tasks []func())
func GoBatchWithName(ctx context.Context, name string, tasks []func())
```

- 全部任务完成后才返回；任务名 `{name}_{index}` 便于监控按前缀聚合；
- ctx 已取消时任务全部跳过、函数**立即返回（不会死锁）**；nil task 跳过不执行（R2-P1-4）。

### GoBatchWithResult — 批量执行并收集结果（泛型）

```go
func GoBatchWithResult[T any](ctx context.Context, name string, tasks []func() (T, error)) ([]T, error)
```

- 返回按索引对齐的结果切片；错误经 `errors.Join` 聚合，可用 `errors.Is` 逐个命中；
- ctx 已取消时被跳过的位置记 `ctx.Err()`；**nil task 记 `gogroutine: nil task`**——两者结果位均为零值，不会出现「err==nil 但任务没跑」的静默失败（R2-P1-4）。

### New / NewWithContext — 创建实例池

```go
func New(name string, capacity int32, opts ...Option) Pool
func NewWithContext(ctx context.Context, name string, capacity int32, opts ...Option) (Pool, error)
```

- `name` 为空或已存在时：`NewWithContext` 返回 error，`New` **panic**（适合包级初始化的快速失败）；
- `ctx` 仅用于创建日志的链路关联；
- **注意**：同名池必须先 `ReleasePool`/`Delete` 才能重建；容量只由 `capacity` 参数决定，`WithPoolSize` 在 `New*` 中**不生效**（传入会打 WARN，R2-P1-2）。

### Get / MustGet — 按名称取实例池

```go
func Get(name string) Pool      // 不存在返回 nil
func MustGet(name string) Pool  // 不存在 panic
```

### Delete / ReleasePool / ReleaseAllPools — 实例池清理

```go
func Delete(name string)        // 仅从管理器移除，不释放底层资源
func ReleasePool(name string)   // 释放并移除（推荐）
func ReleaseAllPools()          // 释放全部实例池
```

- **注意**：`Delete` 会留下仍在运行的底层池——只用于「已持有引用、要改名重建」等少数场景。

### Count / ListNames / Stats — 实例池查询

```go
func Count() int
func ListNames() []string
func Stats() PoolManagerStats
```

- 基于 `sync.Map` 遍历，与创建/释放并发安全；遍历期间的瞬时状态不保证快照一致性。

### PoolStats — 全局池统计

```go
func PoolStats() StatsInfo
```

- 首次调用会惰性初始化全局池；计数为进程内累计值，不跨进程聚合。

### SetMetrics — 注入/替换指标采集器

```go
func SetMetrics(m Metrics)
```

- 可运行时替换（测试或动态切换）；传 `nil` 恢复为仅包内计数。

### Release — 非阻塞释放全局池

```go
func Release()
```

- 立即停止接收新任务并释放资源；**不等待**运行中任务、**不执行** shutdown hooks。

### ReleaseAndWait / ReleaseAndWaitWithTimeout — 释放并等待收尾

```go
func ReleaseAndWait()                                     // 默认 30s
func ReleaseAndWaitWithTimeout(timeout time.Duration)
```

- `Release` 后每 `releasePollInterval`（10ms）轮询运行中任务数，归零即返回；
- 超时打 WARN `ReleaseAndWait timeout` 后**返回**（不阻塞退出），剩余任务被 ants 丢弃。

### RegisterGracefulShutdownHook / IsGracefulShutdownEnabled — 退出回调

```go
func RegisterGracefulShutdownHook(hook func())
func IsGracefulShutdownEnabled() bool
```

- hooks 按注册顺序执行、`sync.Once` 只跑一次；单个 hook panic 被 recover（WARN），不影响后续 hook；
- 触发点：信号到达（`WithGracefulShutdown` 启用时）或手动调用内部执行入口。

## 错误处理

| 场景 | 行为 |
|------|------|
| 任务 panic | 最内层 `recover`：WARN `gogroutine: task panic recovered` + 实例/全局 `Panic++` + Span 记录错误；进程不崩，后续任务不受影响 |
| 池满 / 池已关闭 | 降级为原生 goroutine 执行：WARN + `Fallback++`，**任务不丢** |
| `ctx == nil` | 归一为 `context.Background()`，正常执行（不 panic、不跳过） |
| `ctx` 已取消 | 跳过入队 + WARN `skip submit`；`GoBatch*` 立即返回不阻塞 |
| `task == nil` | `Go*` 静默跳过；`GoBatch*` 跳过不执行（`GoBatchWithResult` 记 `gogroutine: nil task`，R2-P1-4）；`Submit` 方法返回 `gogroutine: nil task` |
| `GoBatchWithResult` 有失败 | 返回 `errors.Join` 聚合错误；全成功返回 `nil` |
| `New("")` / 重名 | `New` panic；`NewWithContext` 返回 error |
| `MustGet` 不存在 | panic |
| 实例池已 `Release` 后 `CtxGo` | 降级为原生 goroutine；`Submit` 是裸提交不降级，返回 `ants.ErrPoolClosed`（R2-P1-5） |
| `ReleaseAndWaitWithTimeout` 超时 | WARN 后返回，不挂起进程 |

## 集成测试

**本包不设 `integration_test.go`**：按交付物矩阵，集成测试仅用于依赖真实外部服务的包；gogroutine 的全部行为（池化、降级、panic、关闭）都在包内完成，不产生网络/数据库/中间件依赖，所有用例由包内单元测试覆盖，无需 `-tags=integration`。

## 性能基线与模糊测试

### 单元测试

```bash
go test ./pkg/gogroutine/ -count=1
```

- 实测（2026-10-01，Windows 22H2 本机，i7-10750H 12 核，`-count=2` 两轮）：两轮合计 3.304s，**176 个测试全绿**（本轮 +8 守护测试，覆盖 R2-P1-1~6 收口）；
- 测试命名遵循方案 A：标识符 `Test<被测方法><场景>` 全英文驼峰，doc 注释中文描述，子测试名中文；
- 共享 helper 集中在 `test_helpers_test.go`；等待治理：正向判定用 channel 信号（`waitDone`）或轮询 + deadline（`waitUntil`），负向断言用命名窗口 `negativeAssertionWindow`，时序让路/任务模拟用 `taskStartPad`/`simulatedTaskWork`/`overlapPad`——判定路径无裸字面量 `sleep`，各常量注释均含「为何固定等待 / 取值依据 / 为何不 flaky」三要素。

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 ./pkg/gogroutine/
```

> **诚实声明**：本 README 不声称已经跑过 `-race`——本机 Windows/MinGW 环境执行 `-race` 报 `0xc0000139`（cgo 工具链问题，见 skill 第八节），无法采集有效读数。真实证据需在 Linux/CI 环境采集（仓库已有 `.github/workflows/race.yml`，待首跑取证）。本轮并发修复（`globalPoolMu`、实例 `mu` 快照）的正确性依据是「锁序分析 + 并发守护断言（不 panic、可见性）」，非 race 报告。

### 基准测试

```bash
go test -bench=. -benchmem -run='^$' ./pkg/gogroutine/
```

实测（2026-10-01，Windows 22H2 本机，i7-10750H 12 核，默认 `-benchtime=1s`，2 轮各约 12.2s，下表取第 2 轮，两轮 `allocs/op` 完全一致）：

| 基准 | 场景 | ns/op | B/op | allocs/op |
|------|------|-------|------|-----------|
| `BenchmarkSubmitTaskHotPath` | 全局池提交热路径 | 847.6 | 557 | 10 |
| `BenchmarkSubmitTaskCancelledCtx` | ctx 已取消短路跳过 | 122.9 | 128 | 1 |
| `BenchmarkExecuteTask` | 执行器（Span+指标+panic 兜底） | 561.4 | 504 | 9 |
| `BenchmarkPoolInstanceCtxGo` | 实例池提交（统一观测管线） | 844.3 | 566 | 10 |
| `BenchmarkPoolConfigApply` | 默认配置 + Option 归并 | 37.98 | 32 | 1 |
| `BenchmarkNewInstance` | 实例池完整构造（ants.NewPool+hooks） | 5852 | 17707 | 30 |
| `BenchmarkCollectBatchErrors` | 批量错误聚合（errors.Join） | 907.8 | 1496 | 6 |

**口径与解读（很重要，否则数字会被误读）：**

- 全部用例是**纯本包路径**，不含网络 RTT；测的是「改动是否让热路径变慢」的回归基线，不是绝对吞吐。
- 基准阶段日志已静音到 error 级别（`silenceBenchLogs`）：本包仅输出 INFO/WARN，静音后读数不含日志 IO；未静音时 `SubmitTaskCancelledCtx` 的读数行会被 WARN 日志打散无法采集。
- 波动看 **allocs/op**（稳定）而非 ns/op（受机器负载影响）；本轮相对上轮的 allocs 变化全部可归因：`PoolInstanceCtxGo` 1→10（R2-P1-3 实例池接入统一观测管线的预期代价，现与全局热路径同成本）、`SubmitTaskHotPath` 11→10（同轮去除双层包装）、`PoolConfigApply` B 48→32（R2-P1-6 删 `PurgeInterval` 字段）、`NewInstance` 25→30（hooks 闭包构造）。
- ns/op 为单机读数、无误差带（本轮采 2 轮，`allocs/op` 逐项一致）；跨轮比较时建议自行重测 2~3 轮，allocs/op 变化 ≥1 即视为回归信号。
- 采集环境如实标注：本轮两轮均经 Qoder 终端执行，输出夹杂 `sync logger error: sync /dev/stdout: The handle is invalid`（logger 在该 pty 句柄下 sync 失败），属终端噪音，不影响 benchmark 数值。

### 模糊测试

```bash
# 种子语料随常规 go test 执行（实测 go test -run='^Fuzz' 0.037s 全过）
go test -run='^Fuzz' ./pkg/gogroutine/

# 按需本地挖掘（不进常规流程）
go test -fuzz=FuzzClampPoolSize -fuzztime=30s ./pkg/gogroutine/
```

| Target | 守护的不变量 |
|--------|--------------|
| `FuzzClampPoolSize` | 容量裁剪结果恒在 `[10, 10000]` 且幂等（越界会把 ants 池打到非法容量） |
| `FuzzNormalizeCtx` | 归一永不返回 nil、nil→Background、`shouldSkipSubmit` 与 `ctx.Err()` 判定一致（不出现「已取消仍入队」或「未取消被误杀」） |
| `FuzzCollectBatchErrors` | 全 nil 聚合为 `nil`；存在错误时结果非 nil 且 `errors.Is` 逐个命中（不丢错） |

失败语料会写入 `testdata/fuzz/<TargetName>/`，确认后提交或修正。
