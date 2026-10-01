# Changelog

本文件记录 `pkg/gocron` 的显著变更。

## Unreleased

### 新增

- **工厂化实例 API（`New(opts...) *Scheduler`）**：调度器改为纯工厂创建 + 实例方法
  （`Run`/`DeleteTask`/`Pause`/`Resume`/`Stop`/`Shutdown`/`IsPaused`/`IsRunningTask`/`GetRunningTasks`），
  包级不再保留任何可变全局状态；对齐 `nacoscli.NewNamingClient` 的「工厂定位 + 调用方持有生命周期」范式。
  应用侧持有单例：`internal/server.NewCronServer` 内创建实例并暴露 `GetCronScheduler()`，
  `internal/config.SetCronScheduler` 注入引用供 `openCron` 热更新使用（镜像 `SetHTTPServerGetter` 先例）。
- **`Shutdown(ctx) error` 优雅关闭**：停止调度循环后等待在途任务全部结束（信号来自
  robfig/cron `Stop()` 返回的 ctx，v3.0.1 `jobWaiter.Wait` 后 cancel）或 `ctx` 超时，再清空状态；
  等待期间不持锁，任务回调内的管理操作不会死锁。`Stop` 保持原语义（不排空、立即返回）。
  - 影响面：进程退出路径由调用方选择 `Stop`（截断在途任务）或 `Shutdown`（完整跑完）；
    超时后状态仍清空并返回包裹 `ctx.Err()` 的错误，由调用方决定是否强退。
  - 守护：`TestShutdownWaitsInflightTasks`、`TestShutdownTimeoutStillStops`、`TestShutdownAfterStopIdempotent`。
- **`Stats() SchedulerStats` 状态快照**：返回 `Registered`（`len(map)`，O(1) 无切片分配，
  适合高频观测）/ `Paused` / `Stopped` / `ResumeFailures`（进程级累计，一次含失败的 `Resume`
  计 1，`Stop` 不重置，`atomic.Int64` 不占主锁）。
  - 影响面：纯新增 API，不改变现有行为；回应运维对「已注册任务数/恢复失败次数」的持续观测诉求。
  - 守护：`TestStatsReflectsState`（初始/注册/暂停/失败累计/停止清空五个阶段 + 计数不重置）。
- **交付物补齐（质量基线）**：新增 `test_helpers_test.go`（共享等待常量与断言 helper）、
  `benchmark_test.go`（注册-删除往返 / 查询 / 日志转换回归基线）、`fuzz_test.go`
  （`parseKVs` 四条不变量：不 panic / 奇数对返回 nil / 输出 ≤ 输入对数一半 /
  EntryID → 任务名还原必命中；粒度归一化二值不变量）、本 `CHANGELOG.md`。
  测试命名统一方案 A（全英文标识符 + 中文 doc 注释），结果判定一律「轮询 + deadline」。

### 变更

- **破坏性变更：包级 API 全部移除，无兼容层**（应「彻底改造、不考虑兼容」的既定决策）：
  `Init`/`Run`/`DeleteTask`/`Pause`/`Resume`/`Stop`/`IsPaused`/`IsRunningTask`/`GetRunningTasks`
  从包级函数变为 `Scheduler` 方法。
  - 影响面：仓内全部调用点已迁移——`internal/server/cron.go`（构造/Run/Stop/新增 getter）、
    `internal/config/reload_infra.go`（Pause/Resume 改走注入实例）、
    6 个 `cmd/serverNameExample_*/initial/createService.go`（OpenCron 分支新增
    `config.SetCronScheduler(server.GetCronScheduler())` 接线）；`go build ./...` 通过，
    全仓无旧 API 残留。
  - 迁移方式：`gocron.Init(...)` + `gocron.Run(...)` → `s := gocron.New(...)` + `s.Run(...)`；
    热更新侧由包级调用改为持有实例调用。
- **`Init` 的「重复初始化返回错误」语义随实例化消除**：多实例天然隔离，无需再以错误阻止覆盖。
- **`Resume` 签名变更：`Resume()` → `Resume() error`**：恢复时单个任务重挂失败原仅记 warning
  日志，调用方无法感知「恢复后哪些任务实际没起来」，存在静默丢任务风险。→ 失败聚合为 error
  返回（与 `Run` 的 `" \|\| "` 聚合先例一致），成功路径（含未暂停/已停止的空操作）返回 nil。
  - 影响面：`internal/config/reload_infra.go` 已改为检查返回值，部分任务重挂失败记 error 日志；
    其余调用点同步迁移。
  - 守护：`TestResumeAggregatesErrors`（白盒注入非法 spec）、`TestRunAfterStopReturnsError`
    与 `TestResumeNoopWhenNotPaused`（空操作返回 nil）。
- **设计决策文档化**：`New` 不返回 error（选项均为纯值、无构造期失败路径，未来引入需校验的
  选项时改为双返回值）与 `Scheduler` 零值不可用（必须经 `New` 创建）均写入包注释与 README。
- **`Shutdown` 执行期间拒绝并发 `Run`/`Resume`（并发边缘竞态收口）**：原实现在 Shutdown
  等待在途任务期间不设任何标记，并发 `Resume` 会重建新实例（`s.c` 被替换），导致 Shutdown
  等待的是旧实例、新实例的在途任务不被排空；并发 `Run` 则把任务挂上即将被清空的旧实例，
  注册成功却静默丢失。→ 新增 `shuttingDown` 标记（`mu` 保护），Shutdown 全程持有、
  返回前解除，`Run`/`Resume` 检查到标记即返回「cron 调度器正在关闭」。
  - 影响面：仅 Shutdown 与 Run/Resume 并发的边缘窗口（关机信号与配置热更新同帧碰撞，
    命中概率极低）；正常运行路径行为不变。
  - 守护：`TestShutdownRejectsConcurrentRunResume`（等待窗口内轮询断言两者被拒、结束后标记解除）。
- **`Resume` 重挂顺序由随机改为名字典序（确定性）**：原 `range s.taskFuncs` 遍历顺序随机，
  EntryID 分配与聚合错误顺序不可预测。→ 改为 `slices.Sorted(maps.Keys(...))` 字典序重挂。
  - 影响面：EntryID 仅用于日志还原、不参与业务逻辑，无功能变化；聚合错误顺序可预测。
  - 守护：`TestResumeAggregatesErrors`（反字典序注入 bad-z/bad-a，断言 bad-a 先聚合）。

### 修复

- **任务注册表生命周期缺陷（Resume 复活已删除任务）**：`DeleteTask` 只摘调度条目、
  不删 `taskFuncs` 注册表，`Pause`→`DeleteTask`→`Resume` 后任务被按注册表重新挂载复活。
  → 改为先删注册表再摘条目，任何后续 `Resume` 都不会复活。
  - 影响面：所有走「暂停期间删除任务」路径的调用方；正常运行期删除不受影响（原逻辑恰好未触发）。
  - 守护：`TestResumeKeepsDeletedTaskGone`。
- **Resume 后 EntryID 同号错配（日志任务名错乱风险）**：robfig/cron v3 新实例的
  `nextID` 从 1 重新计数（源码 `cron.go:161`），旧映射残留会让同号 EntryID 还原成错误任务名。
  → `Resume`/`Stop` 时整体清空并按注册表重建双向映射。
  - 守护：`TestPauseResumeCycle`（含日志映射重建路径）。
- **Run 失败残留脏条目**：原实现先登记注册表后挂载，挂载失败（非法表达式）留下无效条目，
  `Resume` 时被重挂并记警告。→ 调整为挂载成功才登记。
  - 守护：`TestRunValidationErrors`、`TestRunAggregatesErrors`。
- **单次任务修改入参 `Fn` 的副作用**：原 `IsRunOnce` 包装直接改写 `task.Fn` 字段，
  调用方结构体被污染。→ 包装在局部闭包完成，不修改入参。
  - 守护：`TestRunOnceAutoDelete`。
- **Stop 状态残留**：原 `Stop` 不清 `cronOpts`/部分映射，跨生命周期复用产生串扰。
  → `Stop` 清空全部状态且幂等（重复 `Stop`、`Stop` 后 `Pause`/`Resume` 均为安全空操作）。
  - 守护：`TestStopResetsState`、`TestRunAfterStopReturnsError`。
- **单测中断言取错 zapcore 字段存储位（测试可信度缺陷）**：`zapcore.Field` 对 string
  走 `String` 字段而非 `Interface`，直接读 `Interface` 得到 nil，曾让
  「已映射 entry 还原为任务名」用例误判。→ 新增 `fieldValue` helper 按 `FieldType` 取值。
  - 守护：`TestSchedulerParseKVs/字符串值存于String字段`。
- **FuzzParseKVs 的 entry 分支永不命中（测试有效性缺陷）**：`buildKVs` 生成的字符串键
  只能是单字符，而 `parseKVs` 的关键分支要求 `key == "entry"`（5 字符），
  「EntryID → 任务名还原」这条核心不变量表面被 fuzz 守护、实际从未被随机输入命中。
  → 偶数路径固定追加已映射的 entry 对并加守护断言（找不到还原任务名即 fail），
  随机路径也支持生成 `"entry"` 键（`%7==0`）作为额外探索；奇数路径分支保持独立断言。
  - 影响面：仅测试可信度，产品代码无缺陷。
  - 守护：`FuzzParseKVs` 守护断言本身（常规 `go test` 跑种子即验证）+ 10s 挖掘实测 15.6 万次执行 PASS。

### 兼容性说明

- 与 `nacoscli` 工厂形态对齐：纯工厂、调用方持有生命周期、应用侧装配注入；
  `Task`/`Option`/`Every*`/`SecondType`/`MinuteType` 等纯类型与函数保持不变，
  `internal/cron`（任务注册表）无需改动。
- 签名变更迁移：`s.Resume()` → `if err := s.Resume(); err != nil { ... }`；
  需要排空的退出路径 `s.Stop()` → `s.Shutdown(ctx)`（`Stop` 语义不变，不迁移亦可）。
- 测试与文档基线遵循 skill `package-quality-baseline`：方案 A 命名、轮询判定、
  等待常量三要素注释、README/CHANGELOG 模板骨架。
