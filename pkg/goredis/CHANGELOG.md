# 变更日志

本文件记录 `pkg/goredis` 的显著变更，遵循[语义化版本](https://semver.org/lang/zh-CN/)。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
变更类型分为 `新增`（Added）、`变更`（Changed）、`废弃`（Deprecated）、`移除`（Removed）、`修复`（Fixed）。

## [未发布]

### 移除

- `Logger` 接口、`WithLogger` 选项、包内 `defaultLogger` / `resolveLogger`：库内日志统一改走全仓底座
  `pkg/logger`（`logger.WarnWithCtx`，zap 实现、ctx 感知、自带 request_id 关联）。
  原因：默认实现走标准库 `log/slog`，不进入项目日志管道，形成生产日志盲区；且 `WithLogger`
  在全仓生产代码中零调用点（仅单测使用），属于抬高 API 面积的死代码。
  **破坏性变更**：调用 `goredis.WithLogger(...)` 的代码升级后编译失败，直接删除该选项即可。
- `RequestIDExtractor` 类型与 `WithRequestIDExtractor` 选项：`requestIDHook` 改为直接读取全项目约定的
  `logger.ContextKeyRequestID`（与 gin/grpc middleware 写入的 key 一致）。
  **行为变更（修复性质）**：此前未注入提取器时 Redis Span **完全不带** `request_id`；现在默认生效。
  **破坏性变更**：调用 `goredis.WithRequestIDExtractor(...)` 的代码升级后编译失败，直接删除该选项即可。
- `Shutdown` / `ShutdownCluster` 的可变参数 `opts ...Option`：该参数早期仅用于接收 `WithLogger`，
  日志收口后已无任何作用。
  **破坏性变更**：`Shutdown(ctx, rdb, goredis.WithLogger(lg))` 需改为 `Shutdown(ctx, rdb)`。

### 变更

- Lua 脚本通道探测失败、初始化失败后关闭客户端出错、连接池等待超时三处告警，签名由
  `Warn(msg, key, value...)` 改为 `logger.WarnWithCtx(ctx, msg, logger.Err(err))`，
  字段以强类型 `logger.Field` 上报，不再丢失原始类型。
- `probeLuaScriptChannel` / `closeAfterInitFail` / `waitPoolDrained` 的内部签名改为显式接收 `ctx`，
  移除 `Logger` 参数（均为包内私有函数，不影响调用方）。

### 新增

- `logging.go`：包私有的日志钩子变量（`logWarn`），仅用于单元测试替换以捕获日志，不对外暴露注入能力。
- `logging_test.go`：日志钩子捕获辅助 `captureWarn`，以及「告警是否经由全局 logger 输出」的行为断言。
- `warnCollector`（测试辅助）：收集告警时内部用 `sync.Mutex` 保护写入与读取（`msgs()` 返回快照副本），
  不再依赖“调用方记住钩子可能由后台 goroutine 触发”这一隐含约束。
- `captureWarn` 增加重复调用拦截（`atomic.Bool`）：同一测试内嵌套或二次捕获直接 `t.Fatal`，
  避免把上一个钩子当作「默认值」保存导致还原后断言永久读到替换实现。
- `TestLogWarnHook_替换与还原`：验证钩子默认指向 `logger.WarnWithCtx`、可被替换捕获、测试结束后自动还原。
- `TestWarnCollector_并发安全`：两个 goroutine 并发写读，验证不丢消息（为 `-race` 环境提供回归基线）。
- `benchmark_test.go`：性能基线（`BenchmarkGetRedisOpt_DSN解析`、`BenchmarkEnsureDSNPath_补全路径`、
  `BenchmarkEnhanceRedisSpan_追踪状态`、`BenchmarkSetRequestIDToRedisSpan_提取开销`、`BenchmarkTruncateKey_长短Key`、
  `Benchmark命令端到端_miniredis`）。定位是「相对回归基线」；其中**未开启追踪时的 18.2ns / 0 分配**是
  「可观测性代码不该成为负担」的量化证据；端到端用例基于 miniredis，含回环 RTT，只能比较相对变化。
  读数与解读见 README「性能基线与模糊测试」。
- `fuzz_test.go`：模糊测试 3 个 target（`FuzzGetRedisOpt_任意DSN` / `FuzzEnsureDSNPath_幂等` /
  `FuzzSpan截断文本_合法UTF8`），守护「不 panic 且不返回半成品 Options」、「路径补全幂等且不丢协议头」、
  「写入 Span 的文本必为合法 UTF-8 且不超长度上限」。种子语料随常规 `go test` 执行，
  挖掘需显式 `go test -run='^$' -fuzz=<Target> -fuzztime=30s ./pkg/goredis/`。
- 竞态/基准/挖掘均使用原生 `go test` 命令（**不新增 Makefile target**，保持项目根构建入口不变）：
  `CGO_ENABLED=1 go test -race -count=1 -short ./pkg/goredis/`、`go test -run='^$' -bench=. -benchmem ./pkg/goredis/`。

### 修复

- **非法 UTF-8 会原样写入 Redis Span 的名称与属性**（`requestid_hook.go`）：`truncateRunes` 在「未超长」分支
  原样返回入参，`truncateKey` 未截断时直接返回原始 key，`trimLockName` / `trimScriptSHA` 仅在
  `truncated == true` 时采用返回值；Redis key 常由业务方拼接外部输入而来，一旦含非法字节就会进入
  `db.redis.key` 属性与 Span 名称——而 OTLP 的 protobuf string 字段要求合法 UTF-8，这类 Span 会被后端
  拒绝整批上报或渲染成乱码。现改为：`truncateRunes` 在「未超长但非法」时用 `strings.ToValidUTF8`
  归一化为 U+FFFD（且不标记为截断，避免调用方误加省略号），三个调用方始终采用其返回值。
  **行为变更**：仅影响含非法 UTF-8 的输入（由「原样透传」变为「替换为 U+FFFD」）；合法 UTF-8 的
  `db.redis.key` / 锁名 / 脚本摘要输出完全不变，截断长度与省略号规则也不变。
  **发现方式**：`FuzzSpan截断文本_合法UTF8` 的种子语料 `\xff\xfe invalid`、`a\x80b\x81c`（非人工构造用例）。

### 兼容性说明

本包与 `pkg/nacoscli` 的日志方案已对齐：**基础设施包不再各自定义 `Logger` 接口**，
统一依赖 `pkg/logger`。若将来需要替换底层日志实现，收口点是 `pkg/logger` 自身。
跨包规范的统一边界与验收清单现统一维护在 skill
[`package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md)（交付物矩阵、测试命名、
集成测试方法论、证据标准）与 `project-conventions`（代码写法与 README 规则）。

测试命名口径变更：全仓已统一为方案 A（全英文标识符 + 中文 doc 注释），**本包现存用例为方案 B，
已登记为存量待收口项**（仅需重命名，不改断言逻辑与输入输出，尚未执行）；本包新增测试按方案 A 命名。

## 历史破坏性变更（升级必读）

以下变更在本 CHANGELOG 建立前已合入主干，记录于此以便升级排查：

| 变更 | 升级影响 |
|------|----------|
| 删除导出函数 `WithEnableTrace()` 及内部字段 `enableTrace`（该函数从未产生实际效果，挂载逻辑只读 `tracerProvider`） | 有调用点则编译失败；改用 `WithTracing(tp)` |
| `WithSingleOptions` 传入 `Addr`/`Password`/`DB` 由「log 警告 + 静默忽略 + 正常初始化」改为「Init 启动期返回 error，rdb 为 nil」 | 此前复用含这三个字段的配置模板的代码会启动失败；改由 `InitSingle`/`Init` 参数或 DSN 传入 |
