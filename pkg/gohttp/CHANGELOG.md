# 变更日志

本文件记录 `pkg/gohttp` 的显著变更，遵循[语义化版本](https://semver.org/lang/zh-CN/)。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
变更类型分为 `新增`（Added）、`变更`（Changed）、`废弃`（Deprecated）、`移除`（Removed）、`修复`（Fixed）。

本包尚无发布版本，以下记录未发布期间的累计变更；打第一个 tag（如 `v1.0.0`）时，将下方 `## [未发布]` 拆为
`## [未发布]`（保留后续新改动）与 `## [v1.0.0] - YYYY-MM-DD`（归档本次全部变更）。

## [未发布]

### 变更

- **`WithBaseURL("")` 由构造期报错改为合法输入（行为放宽）**：此前空串触发 `ErrEmptyBaseURL`，
  但「不设基础 URL、每次请求传完整地址」是正常用法（与 `WithProxy("")` = 不用代理的空串语义一致），
  调用方写 `gohttp.WithBaseURL("")` 会在 `New` 直接失败。现改为：空串等价于不设置基础 URL，
  非空仍校验可解析、协议 http/https、主机名非空。
  **影响面**：所有传空串的调用方（如 `internal/mq/rabbitmq/consumers/baseConsumer.go` 注释示例）
  从「构造失败」变为「构造成功且请求必须传完整地址」；`ErrEmptyBaseURL` 不再被返回，
  已标记 `Deprecated` 并保留导出以兼容既有 `errors.Is` 判断。
  守护：`TestValidateBaseURL`（空串用例改为通过）、`TestNewEmptyBaseURLAllowed`（构造 + 完整地址请求）。

- **`New` 签名改为 `New(opts ...Option) (*Client, error)`（破坏性）**：配置非法（负超时、重试区间颠倒、
  熔断阈值非正、`nil` transport、非法代理协议、证书文件缺失等）在**构造期返回错误**，不再被 resty 静默接受后
  在运行期产生不可预期行为。对齐 `pkg/nacoscli` `NewConfigClient`、`pkg/goredis` `Init` 的快速失败模式。
  **影响面**：全部调用方；**迁移方式**：改双返回值并在错误时中止，参考三个收口后的调用点——
  `pkg/gin/handlerfunc/common_test.go`（4 处，构造后 `assert.NoError`）、
  `pkg/sdk/tk/core/tk_http_client.go`（单例内构造失败记 `logger.Error` 后 `panic`，启动期暴露配置错误）、
  `internal/mq/rabbitmq/consumers/baseConsumer.go`（注释示例同步为双返回值写法）。
- **`Option` 类型由 `func(*Client)` 收敛为 `func(*options)`（暂存结构）**：选项只写暂存，
  `New` 内 `validate` 统一校验后再应用，是上一条「构造期快速失败」的实现前提；
  **Option 赋值语义保持「最后赋值胜出」**，与 `pkg/nacoscli` 约定一致。非破坏性（`Option` 仍是函数类型）。
- **W3C 传播改为标准 `propagation.HeaderCarrier` 双向路径**：先从调用方已设置的请求头 `Extract`
  （支持上游预设的 `traceparent`），Span 建立后 `Inject` 写回，格式由全局 `otel.GetTextMapPropagator()` 决定。
- **`USAGE_EXAMPLES.go` 移除，改为 `example_test.go`**：独立 `.go` 文件会把全部示例代码编译进生产二进制，
  `example_test.go` 只在 `go test` 时存在，且 `// Output:` 断言同时充当文档正确性的回归防线
  （示例与实现漂移会直接让测试失败）。**破坏性**：若有外部代码 import 了 `USAGE_EXAMPLES.go` 的符号需删除引用（仓内无此引用）。
- **测试命名统一为方案 A**（`Test<被测方法><场景>` 全英文驼峰标识符 + 中文 doc 注释），
  与 `nacoscli`/`goredis` 之外的全仓规范对齐；存量 `pkg/goredis` 方案 B 待收口清单见 skill
  [`package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md) 第二节。
- **README 全量重写**：删除无实测依据的「性能对比 v1.0/v2.0/v3.0」表（平均响应时间/P99/连接复用率等
  数字均无读数来源）与 emoji 堆砌式结构，改为场景驱动 + 职责边界 + Option 表 + API 速查 + 集成测试
  + 性能基线的模板结构；所有示例改为双返回值 `New` 并带错误处理，删除 `New(...)` 单返回值旧写法。

### 新增

- **交付物矩阵全量补齐**（对齐 `package-quality-baseline` skill）：
  - 源文件一对一测试：`option_test.go` / `tracing_test.go` / `security_test.go`，与既有 `gohttp_test.go`
    共同覆盖 4 个源文件；共享 helper 集中 `test_helpers_test.go`（带 `_test.go` 后缀，避免 `unused` 误报）。
  - `benchmark_test.go`：12 组纯本包基准（校验/脱敏/体积估算/熔断判定/request_id 属性/选项应用/构造），
    不含网络 RTT，定位是回归基线；正式读数（i7-10750H，总耗时 16.431s）见 README「性能基线与模糊测试」。
  - `fuzz_test.go`：3 个 target（`FuzzRedactURL`/`FuzzValidateURL`/`FuzzBodySize`），守护脱敏参数对消失、
    校验通过必达「http/https + 非空主机名」、错误文本无裸换行（防日志注入）、体积估算不误判四条不变量。
  - `integration_test.go`（`//go:build integration`）+ `.env`：`TestMain` 超时保护（`GOHTTP_TEST_TIMEOUT` 默认 120s）、
    `loadDotEnv` 只补缺不覆盖、TCP 探测不可达 `Skipf` 带具体 error、断言不符 `Fatalf`。
    实测（Windows 本机 Git Bash，1 轮）：`GET https://httpbin.org/get → 200, 耗时 1.0347902s`，
    `.env` 自动加载 3 个变量，TLS 用例因未配置 `GOHTTP_TEST_TLS_URL` 按设计 SKIP，总耗时 1.322s。
  - `CHANGELOG.md`（本文件）与 README 场景化重写。
- **`WithTracerProvider(tp)`**：注入自定义 `oteltrace.TracerProvider`（接口类型，SDK 与自定义实现均可）；
  `nil`/typed nil 回退全局 provider。此前只能用全局 `otel.Tracer("gohttp")`，单测无法捕获 Span
  （全局默认 noop 产无效 SpanContext），追踪行为在包内不可验证。
- **URL 脱敏 `redactURL`**：transport 错误消息与 Span 的 `http.url` 属性统一过滤敏感 query 参数
  （`token=***`，普通参数原样保留），敏感值不再明文进入日志与 APM。

### 修复

- **熔断器配置项是空壳，配了也没有任何保护（阻断级/可用性）**：原实现 `circuitBreakerThreshold` 全文件仅两处
  赋值（默认 5、`WithCircuitBreaker` 写入），**无任何读取判定**；`ErrCircuitBreakerOpen` 定义后零引用——
  调用方以为有熔断保护，实际每个请求都直连已故障的下游。
  → 实装三态状态机：连续 `threshold` 次失败 → `open` 拒绝一切请求 → 冷却 10s → `halfOpen` 仅放行 1 个探测 →
  成功回 `closed`、失败重回 `open`；计数口径（一次 `do` 计一次、本地拒绝不计、4xx 记成功、
  transport 错误与 5xx 记失败）写进 `gohttp.go` 注释作为单一事实源。
  - 影响面：所有启用 `WithCircuitBreaker` 的调用方（此前全部受影响）；未启用者无变化。
  - 守护：`TestClientBreakerRejectsRequestAfterFailure`（open 后本地拒绝，下游 `hits` 不再增长）、
    `TestCircuitBreakerConcurrentProbeSingleFlight`（32 并发仅 1 个探测通过）、
    `TestCircuitBreakerHalfOpenProbeLifecycle`（半开探测生命周期）。
- **请求体上限从未生效，是直接返回 nil 的死代码（资源耗尽）**：原实现 `WithRequestSizeLimit(_ int64)`
  **参数名就是 `_`**，注册的中间件对任意请求 `return nil`（注释写着「Resty 本身不直接支持」）。
  中途曾改为读 `req.RawRequest.ContentLength`，但 resty 的用户中间件先于内置 `createHTTPRequest` 执行，
  拦截点上 `RawRequest` 恒为 `nil`，同样从未生效——先判为「读法问题」，测试打不通才暴露
  「**拦截时机上 RawRequest 根本不存在**」这一根因。
  → 改为读 `req.Body` 类型分派估算（`[]byte`/`string`/`bytes.Buffer`/`bytes.Reader`/`strings.Reader`
  长度已知；流式 `io.Reader` 不可预知则不拦截），超限返回包装 `ErrRequestTooLarge` 的错误且请求不发出。
  - 影响面：所有启用 `WithRequestSizeLimit` 的调用方（此前上限形同虚设）；未启用者无变化。
  - 守护：`TestClientSizeLimitRejectsBody`（超限请求被拒，下游 `hits==0`）、
    `TestBodySizeKnownTypes`、`FuzzBodySize`（估算不误判）。
- **SSRF 防护可被域名绕过（安全）**：原实现只在 `host` 能被 `net.ParseIP` 解析时才拦截，
  域名形式（DNS 记录指向 `127.0.0.1`/`169.254.169.254`）直接放行。
  → 双层防护：IP 字面量拨号时直接拦截；域名先完成解析、**逐个校验解析结果**，全部公网才
  **直拨已校验的 IP**（消除「解析→拨号」之间的 DNS 重绑定窗口）。
  - 影响面：所有启用 `WithSSRFProtection` 的外呼路径；`ValidateURL` 的字面量校验行为不变（两者互补）。
  - 守护：`TestApplySSRFProtectionDialsValidatedIP`（fake resolver 注入，断言直拨校验后的 IP）、
    `TestApplySSRFProtectionBlocksDomain`（域名指向内网被拦）、`TestValidateURL`、`FuzzValidateURL`。
- **transport 失败请求的 Span 永远不导出，「最需要观测的失败反而零导出」（观测盲区）**：
  原实现的 `End` 依赖 resty `OnAfterResponse`，而 resty 在 transport 错误路径直接返回、不执行该回调。
  → Span 生命周期移入 `do` 流程统一收尾（成功/HTTP 错误/transport 失败/重试各 attempt 均 `End` 并记状态）。
  - 影响面：全部请求路径的追踪数据；此前连接失败、DNS 失败等场景在 APM 中完全缺失。
  - 守护：`TestSpanExportedOnTransportFailure`（连接拒绝仍导出恰好 1 个 Error Span）、
    `TestTransportFailureMarksEveryAttemptError`、`TestSpanStatusSequenceOn502Retry`（每 attempt 一个 Span）。
- **`http.scheme` 硬编码 `https`，http 明文服务也上报 https（数据质量）**：
  `urlScheme` 改为从完整 URL 实际提取（解析失败回空串）；同时 `http.url` 曾在用户中间件时序上
  拿到未拼 BaseURL 的相对路径，新增 `resolveRequestURL` 拼接 BaseURL 与查询参数后再写属性。
  - 影响面：APM 中 scheme/url 两个属性的准确性；只影响展示与告警规则，不影响请求本身。
  - 守护：`TestSpanSchemeAttribute`（http 服务断言 `scheme=http` 且 url 为完整地址）。
- **自签 CA 覆盖系统信任链，启用后访问公网证书全报错（可用性）**：原实现直接给 `RootCAs` 赋值；
  → 与系统根证书池**合并**。守护：`TestNewClientCustomTLSCertificates`。
- **证书加载失败静默继续（阻断级）**：原实现打 stderr 后带着坏配置继续运行，错误到首次请求才以难懂的
  TLS 握手失败暴露；→ 文件缺失/证书与私钥不匹配时 `New` 直接返回错误。守护：`TestNewCertificateErrorsReturnError`。
- **`recover` 防护形同虚设（健壮性）**：原实现把 `recover` 放在 `Request()` 中，其函数体内几乎不会 panic；
  → 移到 `do` 层，任何执行路径的 panic 转为错误返回。守护：`TestDoPanicRecovered`。
- **`WithTransport(nil)` 静默忽略（健壮性）**：nil transport 被静默丢弃、调用方误以为已生效；
  → 构造期返回错误。守护：`TestValidateTransportNil`。

### 兼容性说明

- **破坏性变更集中在 `New` 签名**（双返回值）与 `USAGE_EXAMPLES.go` 移除，迁移方式见「变更」第一条；
  其余 Option 名称、`Request`/`Response`/`ErrorResponse` 对外形态均未变。
- 本包与 `pkg/nacoscli`、`pkg/goredis` 统一三项约定：**构造期快速失败**（`New`/`Init` 内 `validate`）、
  **Option 最后赋值胜出**、**测试命名方案 A**。跨包规范的统一边界与验收清单维护在 skill
  [`package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md)（交付物矩阵、测试命名、
  集成测试方法论、证据标准）与 `project-conventions`（代码写法与 README 规则）。
- **未取证声明**：本机（Windows/MinGW）`go test -race` 报 `0xc0000139`，本轮**未在 `-race` 模式下验证**，
  真实证据需在 Linux/CI 采集；单测（53 个函数 + 3 Example）、集成测试、基准、Fuzz 种子均已本机实测通过，
  读数分别见 README「集成测试」「性能基线与模糊测试」。
