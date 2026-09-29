# Changelog

本文件记录 `pkg/tracer` 的显著变更。

## Unreleased

### 新增

- **`BatchConfig` 结构体收敛批次参数**：`InitWithOTLPBatch` 的 4 个扁平批次参数（maxQueueSize/maxExportBatchSize/batchTimeout/exportTimeout）收敛为值语义结构体 `BatchConfig`，零值字段表示使用 SDK 默认值；为什么：原 10 参数签名触发 revive `argument-limit`（上限 8），此前靠 `//nolint:revive` 压制，违反「禁止 nolint」核心原则，只能改签名根治。
- **`benchmark_test.go` 性能回归基线**：7 个基准（clampRate / 采样率读写 / 采样决策 / NewSpan / provider 替换），全部基于包内 mock、**不含网络 RTT**；实测读数（Windows / go1.25.0 / i7-10750H，`-benchtime=200ms`）见 README「性能基线与模糊测试」。
- **`fuzz_test.go` 模糊测试**：3 个 target 守护不变量——`FuzzClampRate`（输出恒在 [0,1] 且幂等）、`FuzzIsHTTPEndpoint`（不 panic + 前缀一致性）、`FuzzNewSpanNeverPanics`（任意标签类型降级不 panic）；种子语料随常规 `go test` 运行。
- **集成测试质量基线**：`TestMain` 强制 120s 总时长上限（`TRACER_TEST_TIMEOUT` 可上调）+ `loadDotEnv` 自动加载包内 `.env`（只补缺不覆盖）+ 端点 TCP 可达性前置探测（不可达 `Skipf` 带具体 error）+ 等待时长命名常量化（`dialProbeTimeout`/`spanRecordSimulateWait`/`exportFlushTimeout`）+ 用例拆分为 `TestIntegration_OTLPExportGRPC`（`InitWithOTLPBatch` + `authentication` 鉴权 metadata）与 `TestIntegration_OTLPExportHTTP`（`InitWithOTLP`，token 嵌 URL 路径）+ `errorCollector` 导出错误采集断言（0 错误 = 采集端应答接受上报；鉴权/网络类错误按部署差异 Skip，其余 Fail，判定表见 `exportErrClassify`）。
- **包内 `.env` 模板**：列出 `OTEL_EXPORTER_OTLP_ENDPOINT_GRPC`、`OTEL_EXPORTER_OTLP_ENDPOINT_HTTP`、`OTEL_EXPORTER_OTLP_HEADERS`（`k1=v1,k2=v2`，阿里云 gRPC 鉴权用 `authentication=<token>`）、`OTEL_EXPORTER_OTLP_TOKEN`、`TRACER_TEST_TIMEOUT`，根 `.gitignore` 的 `*.env` 已覆盖；仓库内不含真实凭据（本地 `.env` 可填真实凭据，不会入库）。
- **单元测试补齐**：`GetProvider` panic 契约、`SetSamplingRate`/`GetSamplingRate` 未初始化行为、`buildBatchOpts`/`resolveErrorHandler`、`NewResource` 默认值断言、`NewSpan` nil/未知类型标签等边界用例；全部测试函数命名统一并补中文 doc 注释。
- **`Provider()` 安全获取 API**：返回 `(*trace.TracerProvider, bool)`，未初始化/已 Close 返回 `(nil, false)` 不 panic；`GetProvider` 保留 panic 语义（编程错误快速失败）。为什么：基础库在运行期路径公开 panic 成本过高，库代码与运行期探测需要安全取法。
- **`WithProtocol` 显式协议选择 + HTTP 无路径自动补 `/v1/traces`**：新增 `Protocol` 类型（`ProtocolAuto`/`ProtocolHTTP`/`ProtocolGRPC`）；`isHTTPEndpoint` 判定放宽为「http/https scheme + host 非空」（此前要求 path 非空，导致 `http://host:4318` 被误判为 gRPC、把 gRPC 流量打到 HTTP 端口）。为什么：协议判定不应只靠 endpoint 字符串形态隐式决定，歧义场景必须可显式覆盖。
- **`WithPropagator` / `WithGlobalRegistration`**：自定义全局传播器；`WithGlobalRegistration(false)` 完全不触碰 OTel 全局状态（业务改用 `GetProvider().Tracer(name)`），适合多库共存场景。
- **`NewResource` 服务名兜底读 `OTEL_SERVICE_NAME`**：兜底顺序 `WithServiceName` 显式设置 > 环境变量 `OTEL_SERVICE_NAME` > 默认值（打警告日志）。为什么：生产漏配时静默上 `demo-service` 比启动失败更难排查，至少要能被环境变量纠正并留下警告。
- **高并发内存有界性压测与并行基准**：`TestNewSpanHighConcurrencyMemoryBounded`（burst 10 万 Span + sustained 20 协程持续 1 秒两阶段；burst 与 sustained 均断言「常驻 < 50MB」且「常驻 < 累计分配的 1/10」证明垃圾被回收、「累计分配 > 10MB」防空转，双次 `runtime.GC()` 收敛）；`TestInitSlowExporterMemoryBounded`（慢导出 + 队列收紧 256，背压断言取「导出数 < 打点数一半」，对极快/极慢机器均鲁棒）；`BenchmarkNewSpanParallel` 与 `BenchmarkNewSpanParallelChild`（ctx 带父 Span 覆盖 ParentBased 继承分支，均带 `b.ReportAllocs()`）。实测读数与内存上限估算公式见 README「高并发与内存行为」。为什么：基础库必须回答「高并发下内存是否暴涨」——结论是内存由 BSP 队列容量（默认 2048+512）有界，背压表现为丢弃 Span 而非堆积，且 burst 与 sustained 两口径均已取证。

### 变更

- **`Close`/`Init*` 并发契约明确化**：全局现场保存/恢复是包级单快照，无法配对「哪次 Close 对应哪次 Init」，三路交错下可能互相销毁对方的现场 → 契约定为「Close 与 Init* 应串行调用，不应并发；确需并发管理全局状态用 `WithGlobalRegistration(false)`」（`Close` 注释与 README「多库共存推荐做法」同步声明）。为什么：文档声明边界比引入 generation id 的复杂度更符合收益，且与 `SetSamplingRate`/`Init` 既有文档口径一致。
- **README「多库共存推荐做法」**：新增三条建议（优先 `WithGlobalRegistration(false)` 隔离 / `Init*` 与 `Close` 串行 / 传播器与 ErrorHandler「最后写入胜出」的共存策略），明确 `registerGlobal` 覆盖全局传播器的取舍与逃生舱用法。

- **`InitWithOTLPBatch` 签名变更（破坏性）**：`..., maxQueueSize, maxExportBatchSize int, batchTimeout, exportTimeout time.Duration, opts ...OTLPOption` → `..., batchCfg BatchConfig, opts ...OTLPOption`。迁移方式：

  ```go
  // 旧
  tracer.InitWithOTLPBatch(name, env, ver, rate, endpoint, 2048, 512, 5*time.Second, 30*time.Second, opts...)
  // 新
  tracer.InitWithOTLPBatch(name, env, ver, rate, endpoint, tracer.BatchConfig{
      MaxQueueSize: 2048, MaxExportBatchSize: 512,
      BatchTimeout: 5 * time.Second, ExportTimeout: 30 * time.Second,
  }, opts...)
  ```

  仓库内 7 个 `cmd/serverNameExample_*/initial/initApp.go` 调用点已同步更新；其余导出 API 签名不变。
- **测试命名统一为方案 A**：`TestXxx_场景` 下划线风格改为全英文驼峰 `TestXxx场景`（集成测试保留 `TestIntegration_` 规定前缀），每个测试函数补 `// TestXxx 验证……。` 中文 doc 注释；子测试 name 使用中文。
- **README 重写**：按包级质量基线补齐「职责边界」「BatchConfig/标签类型表」「超时说明」「GetProvider API 小节」「性能基线与模糊测试」章节，Option 表补齐「适用 API」列，示例代码全部可直接编译运行（补 `context` import、去掉重复 Init）。
- **`insecure` 默认值 `true` → `false`，并按 endpoint scheme 推断（破坏性）**：未显式调用 `WithInsecure` 时，`http://` 端点走明文、其余（含 `https://` 与无 scheme 的 gRPC `host:port`）走 TLS；显式 `WithInsecure` 优先于推断。为什么：旧默认值会在调用方忘记 `WithInsecure(false)` 时把 `https://` 链路静默降级为明文（且 SDK 的 `WithEndpointURL` 会按 scheme 强制覆盖 insecure，故 HTTP exporter 改为 `WithEndpoint`+`WithURLPath` 自主控制 TLS）。迁移：依赖明文 gRPC 的调用方需显式加 `WithInsecure(true)`；仓库内 7 个 cmd 调用点本就显式传 `WithInsecure(cfg.Otlp.Insecure)`，不受影响。
- **`InitWithOTLP*` 必填参数校验（破坏性）**：`appName`/`otlpEndpoint` 为空时返回 error（此前静默继续，可能产生空 `service.name` 或误落 exporter 默认端点 `localhost:4317`）。
- **`Init` fractions 缺省行为变更**：可变参数缺省时继承当前采样率（未初始化过为 1.0），不再固定 1.0。为什么：缓解「SetSamplingRate 热更新后被重新 Init 静默重置为全量采样」的语义竞态（与 `SetSamplingRate` 并发仍无先后保证，以最后完成者为准，见 `Init` 注释）。
- **`NewSpan` 标签合并为一次 `WithAttributes`**：4 标签基准 allocs/op 14 → 6（-57%）、B/op 704 → 536。
- **采样器迁移到 `math/rand/v2`**：顶层 `Float64` 无全局锁（per-P 状态），消除旧版 `math/rand` 全局 `lockedSource` 在高并发采样决策上的锁竞争。

### 修复

- **`clampRate` 漏出 NaN 导致采样率非法**：NaN 输入绕过 `rate <= 0` 判断，`GetSamplingRate()` 返回 NaN（不是合法 [0,1] 值），采样决策退化为静默全部丢弃 → 根因是 NaN 与任何数比较均为 false；修复为 `math.IsNaN(rate)` 显式归 0。
  发现途径：设计 `FuzzClampRate` 不变量「输出恒在 [0,1]」时推演特殊浮点输入，确认 NaN 会让该不变量必失败，遂在 fuzz 落地前先行修复。
- **注释/文档与实现不符（多处）**：`defaultErrorHandler` 注释写「输出到 stderr」实际走 `logger.WarnWithCtx`；README 写「Close 内部用 atomic.Bool 保证只执行一次」（实际是 `tpMu` 清空状态实现幂等）、「SetSamplingRate 用 atomic.Pointer 热替换采样器实例」（实际是 `atomic.Uint64` 原地更新同一实例）、「GetSamplingRate 未初始化返回 0」（实际返回 1.0）、「NewSpan 内部调用 SetTraceName」（实际只读 `getTraceName`）、「旧 Provider 异步关闭」（实际同步关闭）——全部对齐实现；集成测试环境变量名 `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 更正为代码实际读取的 `OTEL_EXPORTER_OTLP_ENDPOINT_HTTP`。
- **单元测试在包目录残留文件**：`TestNewFileExporter` 此前在包目录创建 `demo`/`traces.json` 后手动删除，失败路径会残留 → 改用 `t.TempDir()` 自动清理。
- **`//nolint:revive` 违规抑制**：删除 `InitWithOTLPBatch` 与 `GetProvider` 上的两处 `//nolint`（前者随签名改造根治；后者经 `golangci-lint run` 实测本不触规，属不必要抑制）。
- **Close 后 OTel 全局状态悬空（P0）**：`Close` 只清空包内 `tp/exp/sampler`，`otel.GetTracerProvider()` 仍指向已 Shutdown 的旧 provider，且 `registerGlobal` 覆盖的 propagator/ErrorHandler 永不恢复——多库共存时互相污染，Close 后并发 `NewSpan` 会拿到正在被关闭的 provider → 修复为「首次注册保存全局现场（provider/propagator/handler），`Close` 在锁内与 `tp` 清空同临界区恢复」，保证恢复先于旧 provider 关闭。
- **exporter 可能被重复 Shutdown（P0）**：SDK 中 `TracerProvider.Shutdown` 会经 `BatchSpanProcessor` 级联调用 `exporter.Shutdown`，替换/Close 又显式关闭——非幂等的自定义 exporter 会被关两次，复用同一 exporter 时还会被误关 → 修复为 `ownedExporter` 包装：级联 Shutdown 降级为 no-op，真正关闭由本包在「被替换/Close」时经 `sync.Once` 执行恰好一次（复用同一底层 exporter 时跳过，由最后持有者关闭）。
- **默认 `insecure=true` 的 TLS 降级风险（P0）**：详见「变更」小节（默认值翻转 + scheme 推断 + 不再使用 SDK 的 `WithEndpointURL`）。
- **`WithHeaders`/`WithAttributes` 保存调用方 map 引用（data race）**：调用方后续并发修改 map 会与 exporter 后台协程读取构成真实 data race（`-race` 必报）→ Option 内改为复制 map。
- **注释与实现不一致**：`WithErrorHandler`/`registerGlobal` 等注释写「默认 stderr 输出处理器」，实际 `defaultErrorHandler` 走 `logger.WarnWithCtx` 警告日志——统一修正。
- **`installProvider` 锁内创建 sampler/provider 移到锁外**：临界区只做指针替换与全局注册，减少 `SetSamplingRate` 等热更新路径的锁竞争。
- **`exportErrClassify` 关键词精确化**：裸 `strings.Contains(msg, "401")` 会误匹配端口号、trace id 中的数字，改为带语境正则 `(?i)(status|code|error)[^0-9]{0,12}40[13]\b`（纯文本无语境的 "403" 归为 Fail——宁可误报为缺陷也不静默放过）。
- **traceName 与 provider 替换的并发竞态**：`InitWithOTLP*` 的 `SetTraceName(appName)` 与 `Close` 的 `traceName.Store("unknown")` 原在 `tpMu` 锁外，并发时序下 Close 的重置会被后完成的锁外写入覆盖，导致「Close 完成后 traceName 仍为 appName」与契约矛盾 → 两者均移入 `tpMu` 临界区（`installProvider` 锁内按 name 参数设置、`Close` 锁内重置），与 tp/sampler/exp 原子一致。
- **`NewSpan` 全 nil 标签白分配**：`make([]attribute.KeyValue, 0, len(tags))` 无条件预分配，全 nil 标签 map 每次白分配一个底层数组 → 改为惰性分配（遇首个非 nil 标签才 make）：全 nil/空 map 零分配；标签全部有效的常见场景仍为 1 次底层数组分配（基准 6 allocs/op 无回退，实测验证）。
- **`NewResource` 默认服务名警告刷屏**：多处调用（测试、多组件初始化）时逐次告警 → `sync.Once` 限制仅输出一次（警告语义是「提示配置缺失」，首次出现已足够引起注意），告警文案标注「本警告仅输出一次」。

### 兼容性说明

- 与 `project-conventions` / `package-quality-baseline` 两个 skill 的分工对齐：代码写法与 README 规则见前者，测试交付物/集成测试方法论/证据标准见后者；本文档按 `doc-templates.md` 的 CHANGELOG 四小节结构编写。
- 环境变量命名保留 OTel 生态标准前缀（`OTEL_EXPORTER_OTLP_*`），未改用 `<SVC>_*` 前缀：该命名是 OpenTelemetry SDK 既定约定，改名会破坏与生态工具的兼容；测试总时长变量按基线命名为 `TRACER_TEST_TIMEOUT`。
- **semver 策略**：`InitWithOTLPBatch` 签名（`BatchConfig`）、`insecure` 默认值翻转、`InitWithOTLP*` 必填校验均属破坏性/行为破坏性变更，对外发布应升 major 版本；后续 `GetProvider`、`GetSamplingRate` 等若改签名同样走 major 或新增兼容 API。
- **实测取证**：集成测试已针对阿里云链路追踪真实执行（Windows / go1.25.0 本机，凭据脱敏）——gRPC 链路（`tracing-analysis-dc-bj.aliyuncs.com:8090` + `authentication` 头）与 HTTP 链路（token 嵌 URL 路径）均 **PASS**：各 3 个 Span、导出链路 0 错误、采集端应答接受上报。HTTP 链路早期一轮曾被服务端 403 拒绝（三组对照实验：带鉴权头 / 无关头 / 无头均 403，排除请求头干扰），后连续 4 次 PASS，判定为服务端临时状态。完整读数见 README「实测结果汇总」。
- **未验证声明**：「入库后可在阿里云控制台查询到」属服务端行为未取证（外部视角读数不在本包测试约定内）；`-race` 因本机 MinGW 工具链问题（`exit status 0xc0000139`）无法执行，未取证（多轮复测均如此），需在 Linux/CI 补采 `CGO_ENABLED=1 go test -race -count=10 ./pkg/tracer/`——**该证据未闭环前不应对外宣称本包「生产可用」**。
