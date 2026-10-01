# gocron

封装 robfig/cron/v3 的定时任务调度包：注册、删除、暂停/恢复定时任务，支持秒级/分钟级粒度与单次任务自动注销。

## 架构概览

```
pkg/gocron/
├── cron.go                # Scheduler 工厂与实例方法、Task/Option、时间表达式、日志适配
├── cron_test.go           # 单元测试（方案 A 命名，轮询 + deadline 判定）
├── test_helpers_test.go   # 跨测试文件共享的等待常量与断言 helper
├── benchmark_test.go      # 注册-删除往返 / 查询 / 日志转换的回归基线
├── fuzz_test.go           # parseKVs 与粒度归一化的不变量守护
├── README.md              # 本文件
└── CHANGELOG.md           # 行为变更记录
```

**核心设计原则**：

- **纯工厂 + 实例方法**：包级无任何可变全局状态，`New(opts...)` 返回独立 `Scheduler`，
  多实例互不干扰；生命周期归调用方（`internal/server` 持有单例，`internal/config` 注入引用供热更新用）。
- **单一互斥锁**：实例内 `c/cronOpts/paused/taskFuncs/nameID` 全部由 `mu` 串行化，
  任务回调内注销任务（单次任务自动删除）走同一把锁，与调度操作互不持锁等待，不会死锁。
- **taskFuncs 是任务注册表的唯一事实来源**：`IsRunningTask`/`GetRunningTasks` 与 `Resume` 重建
  都以它为准，因此暂停期间注册的任务同样可见，删除的任务不会被 `Resume` 复活。
- **日志回调走 sync.Map**：`EntryID → 任务名` 映射由 cron 日志回调读取（不经过 `mu`），
  故用 `sync.Map`；`Resume`/`Stop` 时整体重建，避免新实例 EntryID 从 1 重计数导致同号错配。

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| 定时任务注册/删除/暂停/恢复 | 是 | 本包 |
| 任务业务逻辑的注册表（`RegisterTask`/`GetTasks`） | 否 | `internal/cron` |
| 调度器实例的装配与生命周期（何时创建/停止） | 否 | `internal/server`（cronServer） |
| 定时任务开关热更新的接线 | 否 | `internal/config`（SetCronScheduler） |
| 分布式任务锁 | 否 | 业务侧自行基于 Redis 实现 |

本包无外部服务依赖（纯进程内调度），因此**没有集成测试**，`go test` 默认跑的即是全量测试。

## 使用场景选择

### 场景一：注册并运行定时任务（`New` + `Run`）

**适用场景**：应用启动时批量注册周期任务。

```go
s := gocron.New(gocron.WithOnlyPrintError(true))

if err := s.Run(
    &gocron.Task{
        Name:     "report",
        TimeSpec: gocron.EveryHour(4),
        Fn:       generateReport,
    },
    &gocron.Task{
        Name:     "cleanup",
        TimeSpec: "0 15,45 9-12 * * *", // 秒级 6 字段
        Fn:       cleanupCache,
    },
); err != nil {
    // 错误已聚合："任务 'x' 已存在 || 运行任务 'y' 失败: ..."
    return err
}
```

**内部行为**：

1. 逐个校验任务（nil / 空名 / 空函数 / 重名 / 非法表达式），单个失败不中断其余注册；
2. 校验通过才挂载到调度器并登记注册表，挂载失败不残留脏条目；
3. `IsRunOnce` 任务在构造阶段包装为「执行后自动注销」，不修改入参 `Fn`。

**注意**：实例由调用方持有并在退出时调用 `s.Stop()`（不排空）或 `s.Shutdown(ctx)`（排空在途任务）；
同一任务名在同一实例内不允许重复注册。

---

### 场景二：配置热更新暂停/恢复（`Pause` / `Resume`）

**适用场景**：`app.openCron` 开关变更时动态停启全部任务，无需重启进程。

```go
s.Pause()  // 停止调度循环，任务注册表原样保留（幂等）
s.Resume() // 按注册表整体重建调度器并重新注册全部任务
```

**内部行为**：

1. `Pause` 只停调度循环，`taskFuncs` 不动，期间 `Run` 注册的任务照常登记；
2. `Resume` 创建新 cron 实例（v3 的 Stop 不可逆），先清空 EntryID 映射再按注册表重挂，
   单个任务恢复失败记警告日志并**聚合成 error 返回**（与 `Run` 的 `" \|\| "` 聚合先例一致）；
3. 暂停期间 `DeleteTask` 的任务**不会**在 `Resume` 后复活。

**注意**：仓内由 `config.SetCronScheduler` 注入实例后，热更新回调直接调 `Resume`/`Pause`，
并检查 `Resume` 返回值（部分任务重挂失败记 error 日志）；未注入时热更新跳过并输出警告日志。

---

### 场景三：单次任务（`Task.IsRunOnce`）

**适用场景**：只执行一次的延时任务（如启动后延迟初始化）。

```go
err := s.Run(&gocron.Task{
    Name:      "delayed-init",
    TimeSpec:  gocron.EveryMinute(5),
    IsRunOnce: true,
    Fn:        doInit,
})
```

**内部行为**：执行体先跑原函数，随后自动 `DeleteTask` 注销自身，保证只执行一次。

---

### 场景四：优雅退出（`Stop` / `Shutdown` + 重建）

**适用场景**：服务关闭时选择是否等待在途任务跑完。

```go
// 路径 A：立即停止（不等待在途任务）
s.Stop()

// 路径 B：优雅关闭——等待在途任务全部结束（或 ctx 超时）再清空状态
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
if err := s.Shutdown(ctx); err != nil {
    // 超时后状态仍已清空（调度已停，残留任务不会继续执行），由调用方决定是否强退
    log.Warnf("任务未在期限内跑完：%v", err)
}

s2 := gocron.New(...) // 实例不可复用，如需再调度请重新 New
```

**选择依据**：`Stop` 立即返回、**不等待在途任务**——进程退出时正在执行的任务（如发通知、写 DB）
可能被截断；需要任务完整跑完再退出时用 `Shutdown(ctx)`。`Shutdown` 等待期间不持锁，
任务回调内的 `DeleteTask` 等操作照常进行，不会死锁。

**并发约束**：`Shutdown` 执行期间置关闭标记，**拒绝并发的 `Run`/`Resume`**（返回「正在关闭」错误）——
否则并发 `Resume` 重建新实例会让 `Shutdown` 等待错对象、新实例在途任务不被排空；
`Run` 则会把任务挂上即将被清空的实例造成静默丢失。调用方在关机序列中应先停配置热更新，再调 `Shutdown`。

**注意**：两者均幂等；`Stop` 后的实例上 `Run` 返回「已停止」错误，`Pause`/`Resume`/`Shutdown` 是安全空操作。

## 参数/结构体说明

### Task

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `TimeSpec` | `string` | 是 | 时间表达式。秒级 6 字段（`0 15,45 9-12 * * *`）或分钟级 5 字段，也可用 `Every*` 生成 `@every` 形式 |
| `Name` | `string` | 是 | 任务名，实例内唯一，用于删除与日志还原 |
| `Fn` | `func()` | 是 | 任务函数；包内 panic 由 `cron.Recover` 捕获并记日志，不会打挂进程 |
| `IsRunOnce` | `bool` | 否 | `true` 时执行一次后自动注销；不修改 `Fn` 本身 |

### Scheduler

由 `New` 创建，字段不导出。所有方法并发安全；`Stop` 后除 `Stop`/`Shutdown` 自身外的方法要么返回错误要么是空操作。
**零值不可用**：必须经 `New` 创建（零值的内部注册表为 nil，调 `Run` 会 panic）。

## Option 列表

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithGranularity(granularity)` | 调度粒度：`SecondType`=6 字段表达式，`MinuteType`=5 字段；`>= MinuteType` 归一化为分钟级，其余归秒级 | `SecondType` | `New` |
| `WithOnlyPrintError(enable)` | 只输出 error 级 cron 日志，吞掉 info（如 added/removed/wake） | `false` | `New` |

## API 速查

### New — 创建并启动调度器实例

```go
func New(opts ...Option) *Scheduler
```

- 纯工厂：无包级状态、无「重复初始化」错误，可并行创建多个实例
- **不返回 error 是有意设计**：当前选项均为纯值（粒度、日志开关），无 IO、无外部依赖、无构造期失败路径；
  若未来引入需要校验的选项（如分布式锁依赖），签名将改为 `(*Scheduler, error)`
- **注意**：返回即已启动（空转），生命周期归调用方，用完调 `Stop` 或 `Shutdown`

### Run — 批量注册任务

```go
func (s *Scheduler) Run(tasks ...*Task) error
```

- 单个失败不中断其余任务，错误用 `" || "` 聚合
- **注意**：已 `Stop` 的实例返回「cron 调度器已停止」；暂停期间注册的任务在 `Resume` 后生效

### DeleteTask — 删除任务（幂等）

```go
func (s *Scheduler) DeleteTask(name string)
```

- 先删注册表再摘条目，保证后续 `Resume` 不复活
- **注意**：对不存在的任务是空操作，无返回值

### Pause / Resume — 暂停与恢复

```go
func (s *Scheduler) Pause()
func (s *Scheduler) Resume() error
```

- `Pause` 停调度循环保留注册表；`Resume` 重建实例并按注册表整体重挂
- **注意**：`Resume` 仅在「已暂停」时生效，其余情况空操作并返回 nil；
  单任务重挂失败会聚合成 error 返回（`" \|\| "` 分隔，含任务名与原因），**必须检查返回值**，
  否则会静默丢任务（看似恢复成功、实际部分任务未执行）

### Stop — 停止并清空状态（幂等）

```go
func (s *Scheduler) Stop()
```

- 清空任务注册表与 EntryID 映射，实例不可复用
- **注意**：不等待执行中的任务结束——**进程退出时正在执行的任务可能被截断**；
  需要任务跑完再退出时改用 `Shutdown(ctx)`

### Shutdown — 优雅关闭（排空在途任务）

```go
func (s *Scheduler) Shutdown(ctx context.Context) error
```

- 停止调度循环后等待在途任务全部结束（信号由 robfig/cron 的 `Stop()` 返回 ctx 提供），
  再清空状态；等待期间不持锁，任务回调内的管理操作不会死锁
- **注意**：`ctx` 超时后仍会清空状态并返回包裹 `ctx.Err()` 的错误，由调用方决定是否强退；
  对已 `Stop` 的实例幂等返回 nil；执行期间并发的 `Run`/`Resume` 返回「正在关闭」错误

### IsPaused / IsRunningTask / GetRunningTasks / Stats — 状态查询

```go
func (s *Scheduler) IsPaused() bool
func (s *Scheduler) IsRunningTask(name string) bool
func (s *Scheduler) GetRunningTasks() []string
func (s *Scheduler) Stats() SchedulerStats
```

- 查询基于任务注册表（含暂停期间注册的任务），`GetRunningTasks` 按字典序排序且非 nil
- `Stats()` 返回 `SchedulerStats{Registered, Paused, Stopped, ResumeFailures}` 快照：
  `Registered` 取 `len(map)` 为 O(1) **无切片分配**，适合高频观测（`GetRunningTasks` 每次分配切片，
  不宜高频，仅需数量时用 `Stats().Registered`）；`ResumeFailures` 是进程级累计计数
  （一次含失败的 `Resume` 计 1），`Stop` 不重置；`Stopped` 语义是 `s.c == nil`，
  含零值/从未 `New` 的情况（零值不可用，见上）

### EverySecond / EveryMinute / EveryHour / Everyday — 时间表达式便捷函数

```go
func EverySecond(size int) string  // "@every 5s"
func EveryMinute(size int) string  // "@every 2m"
func EveryHour(size int) string    // "@every 3h"
func Everyday(size int) string     // "@every 24h"（@every 不支持天单位，按 24h 换算）
```

`Everyday(N)` 的语义是固定 `24h×N` 间隔（从注册时刻起计时），**不是**「每天同一时刻」，不受时区/DST 影响。

## 错误处理

| 场景 | 行为 |
|------|------|
| `Run`：nil 任务 / 空任务名 | 返回对应中文错误 |
| `Run`：`Fn` 为 nil / 任务名重复 / 表达式非法 | 返回中文错误，批量时 `" \|\| "` 聚合 |
| `Run`：实例已 `Stop` | 返回「cron 调度器已停止」 |
| `Run` / `Resume`：`Shutdown` 执行期间调用 | 返回「cron 调度器正在关闭」（拒绝并发重建/注册） |
| 任务执行中 panic | `cron.Recover` 捕获并记 error 日志，调度循环继续 |
| `Resume` 时单个任务重挂失败 | 记 warning 日志并**聚合成 error 返回**（按名字典序），其余任务照常恢复 |
| `Shutdown` 等待在途任务超时 | 清空状态并返回包裹 `context.DeadlineExceeded` 的错误 |
| `DeleteTask` / `Pause` / `Stop` / `Shutdown` 对无效状态调用 | 空操作，不报错不 panic |

## 性能基线与模糊测试

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 ./pkg/gocron/
```

> 诚实声明：本 README 不声称已经跑过 `-race`。本机（Windows/MinGW）执行报 `exit status 0xc0000139`
> （gcc 运行时问题，与本包代码无关），真实 `-race` 证据需在 Linux/CI 采集。
> `TestConcurrentOperations` 是对应主用例（6 goroutine × 20 轮，覆盖注册/删除/查询/暂停恢复全路径）。

### 基准测试

```bash
go test ./pkg/gocron/ -run='^$' -bench=. -benchtime=200000x -count=3
```

**口径固定**：必须带 `-benchtime=200000x`。本包基准是 ns 级纯内存操作，默认 `-benchtime=1s`
下采样次数不可控、读数波动大；固定迭代次数才能得到可比较的回归基线（下列读数即按此口径采集）。

实测（Go 1.25.0 windows/amd64，本机，`-benchtime=200000x` × 3 轮）：

| 基准 | 场景 | ns/op（3 轮） | 口径 |
|------|------|---------------|------|
| `BenchmarkSchedulerRunDelete` | 注册-删除往返 | 3761 / 3792 / 3777 | 进程内，无网络 RTT；`WithOnlyPrintError(true)` 剔除日志开销，只反映锁与注册表成本 |
| `BenchmarkSchedulerGetRunningTasks` | 64 任务持锁查询+排序 | 4066 / 4338 / 4193 | 同上，读数量级而非绝对值 |
| `BenchmarkParseKVs` | 日志键值转换 + EntryID 还原 | 166.0 / 160.6 / 167.0 | 纯内存转换，无日志输出 |

**口径与解读**：三轮读数波动 < 8%，可作回归基线；ns/op 对机器敏感，跨机器比较时看相对变化。

### 模糊测试

| Target | 不变量 |
|--------|--------|
| `FuzzParseKVs` | 任意输入不 panic；奇数参数对返回 nil；输出字段数 ≤ 输入对数的一半；偶数路径**每次必命中** EntryID → 任务名还原分支（固定追加 entry 对 + 守护断言，不依赖随机生成 "entry" 键） |
| `FuzzNormalizeGranularity` | 任意整数粒度经 `WithGranularity` 后必为 `SecondType` 或 `MinuteType` |

种子语料随常规 `go test` 执行；挖掘需显式：

```bash
go test ./pkg/gocron/ -fuzz=FuzzParseKVs -fuzztime=30s
```
