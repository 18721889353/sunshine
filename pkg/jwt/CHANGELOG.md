# 变更日志

本文件记录 `pkg/jwt` 的显著变更，遵循[语义化版本](https://semver.org/lang/zh-CN/)。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
变更类型分为 `破坏性变更`（BREAKING CHANGE）、`新增`（Added）、`变更`（Changed）、`修复`（Fixed）、`兼容性说明`。

本包尚无发布版本，以下记录未发布期间的累计变更；打第一个 tag（如 `v1.0.0`）时，将下方 `## [未发布]` 拆为
`## [未发布]`（保留后续新改动）与 `## [v1.0.0]`（归档本次全部变更）。

## [未发布]

### 破坏性变更（BREAKING CHANGE）

- **去全局实例化：删除包级全局 API，唯一入口改为 `jwt.New` 构造 `*Manager` 实例**（semver major）。
  原「一个进程一套全局密钥」的单例设计无法支持不同业务使用不同密钥；现在**多密钥 = 多 Manager**，
  各实例配置独立、互不覆盖。
  - **删除**：`Init(opts ...Option)`、包级函数 `GenerateToken` / `ParseToken` / `RefreshToken` /
    `GenerateCustomToken` / `ParseCustomToken` / `RefreshCustomToken`（共 7 个导出函数）
    及全局 `optStore`，不留兼容门面。
  - **新增**：`New(opts ...Option) (*Manager, error)`（唯一构造入口，构造校验缺密钥直接返回 error）、
    `(m *Manager) Reload(opts ...Option) error`（配置热更新整体生效，失败保留旧配置）、
    6 个业务方法（参数签名不变，仅接收者改为 `*Manager`）；
    配置存储为 `atomic.Pointer[options]`（原子读写、无需锁，热更新与请求线程并发安全），
    并发读写由 `TestReloadConcurrentWithParse`（8 对 goroutine × 200 次并发）守护。
  - **`ErrNotInitialized` 语义变化**：原「未调用 `Init`」→ 现「接收者为 nil（Manager 未注入）」；
    方法遇 nil receiver 返回该错误而非 panic（fail loud 不 crash，消费方无需重复判空）。
  - **`RequireSigningKey()` panic → error**：缺密钥时 `New` / `Reload` 返回
    `ErrSigningKeyNotConfigured`（原 `Init` 直接 panic）；启动方 panic 兜底，fail-fast 时机不变。
  - **仓内同步改造**：`internal/config` 新增 `InitJwt()` / `JwtManager()` 持有应用侧唯一默认实例，
    `reloadJwtConfig` 对同一实例 `Reload`（注入指针永不变，天然热更）；7 个示例 `initJWT`
    改调 `config.InitJwt()`；`pkg/gin/middleware`（`WithJwtManager`）、`pkg/grpc/interceptor`
    （`WithJwtManager`）、`pkg/gows`（`ParseTokenCtx` 尾部追加 `mgr` 参数）及 codegen 模板
    全部改为注入实例——**外部调用方必须同批修改，编译期即报错**。
  - 测试改名映射：`TestInitConcurrentWithParse` → `TestReloadConcurrentWithParse`、
    `TestRequireSigningKeyPanicsWhenMissing` → `TestRequireSigningKeyFailsWhenMissing`（panic 断言
    改 error 断言）、`TestInitWithoutOptionsResetsToNoKey` → `TestReloadWithoutOptionsResetsToNoKey`；
    新增 nil Manager 系列用例（6 个业务方法各一）、middleware `TestAuthWithJwtManagerAllowsValidToken` /
    `TestAuthWithoutJwtManagerRejects`、interceptor `TestJwtVerifyWithInjectedManager` /
    `TestJwtVerifyNilManagerRejects` 守护。
- **`GenerateToken` 签名由可变参数改为单参数**（semver major）：
  `GenerateToken(uid, name string, kvs ...map[string]any)` → `GenerateToken(uid, name string, fields map[string]any)`。
  原可变参数只取第一个 map、多余的静默忽略，易误导调用方以为多个 map 会被合并；
  现为单一 `fields`，签名与语义一致。**调用方需同步修改**：带一个 map 的调用无需变化，
  无字段调用需补 `nil` 第三参（编译期报错兜底）。
- **`GenerateToken` 不传字段时 `Fields` 由空 map 改为 nil**（行为变更）：
  与 `GenerateCustomToken(nil)` 的既有语义统一，消除「同一包内两条签发路径两种 nil 不变量」；
  nil map 读取安全（`claims.Fields[k]` 不 panic），`Get` 系列已处理 nil。
  由 `TestGenerateTokenDefaultFields` 的双路径一致性断言守护。

### 新增

- **解析侧校验选项 `WithExpectedIssuer` / `WithExpectedAudience`（多租户隔离）**：原实现只有写入端
  选项（`WithIssuer` / `WithAudience`），`ParseToken` 不校验 `iss` / `aud`——同密钥的多个服务之间
  token 可互用。现在配置期望值后解析/刷新会校验（空值跳过，兼容旧行为），不匹配返回底层库的
  `jwt.ErrTokenInvalidIssuer` / `jwt.ErrTokenInvalidAudience`（`errors.Is` 可判定）。
  由 `TestParseTokenIssuerValidation` / `TestParseTokenAudienceValidation` 守护。
- **`WithLeeway(d)` 时钟偏差容忍**：分布式节点对时误差会使 token 在过期边界随机被拒；
  配置后按 golang-jwt v5 的 leeway 语义放宽 `exp` / `nbf` 校验，默认 `0`（与原行为一致）。
  由 `TestParseTokenLeewayAllowsSlightlyExpired` 守护。
- **Fuzz 真实签名篡改种子**：翻转 payload 段中间一个字节（保留三段式结构），覆盖
  「验签拒绝 / 解码失败」路径；原末尾追加字符的种子只会触发 base64 解码失败，
  无法验证签名校验。
- **Linux CI 竞态取证入口 `.github/workflows/race.yml`**（pkg/jwt、pkg/tracer、pkg/logger 三包共用）：
  本机 Windows + MinGW 无法执行 `-race`（cgo 运行库报 `0xc0000139`），workflow 在 ubuntu 上跑
  `go test -race -short -count=1` 补齐证据；待首次推送后取证，通过后更新 README「竞态检测」声明。
- **导出错误变量 `ErrSigningKeyNotConfigured`**：签名密钥未配置时返回此错误，`errors.Is` 可判定；
  配合下方安全默认值变更使用。
- **`RequireSigningKey()` 选项**：传入后 `New` / `Reload` 强校验密钥非空，缺失或为空返回
  `ErrSigningKeyNotConfigured`（fail fast，启动方 panic 兜底），让「配置漏读密钥」在启动阶段
  暴露而非带病上线；不传则保持 fail-loud（首次签发/解析时返回 `ErrSigningKeyNotConfigured`）。
  由 `TestRequireSigningKeyFailsWhenMissing` / `TestRequireSigningKeyPassesWithKey` 守护。
- **导出错误变量 `ErrNotInitialized` / `ErrSignatureInvalid`**：原 `errInit` / `errSignature` 未导出，
  调用方只能字符串匹配错误文本；现在可用 `errors.Is` 判定「未初始化」与「兜底校验失败」两类错误。
  **非破坏性变更**：仅新增导出符号，原有返回路径不变。
- **`benchmark_test.go` 性能基线**：5 个基准（`BenchmarkGenerateToken` / `BenchmarkParseToken` /
  `BenchmarkGenerateCustomToken` / `BenchmarkParseCustomToken` / `BenchmarkRefreshToken`），
  纯 CPU 计算（HMAC 签名 + JSON 序列化），**不含网络 RTT**，定位是相对回归基线。
  实测读数随每轮验证更新，以 README「基准测试」的三轮读数表为准，本文件不保留会过时的快照。
- **`fuzz_test.go` 模糊测试**：3 个 target 守护不变量——`FuzzParseTokenNeverPanics` /
  `FuzzParseCustomTokenNeverPanics`（任意输入不 panic、失败必返回 nil claims、成功可重复解析）、
  `FuzzTokenRoundTripFields`（合法 UTF-8 的 uid/name 与字符串字段往返原样保留、数字字段无损还原）。
  种子语料随常规 `go test` 执行，挖掘需显式 `-fuzz`。种子阶段全绿，当前未做长时间定向挖掘。
- **`test_helpers_test.go` 共享测试工具**：`newTestManager`（按固定基线配置创建独立 Manager 实例，
  用例间配置互不共享、天然隔离）、`testSigningKey` 常量，供 jwt/option/benchmark/fuzz
  四个测试文件共用，避免同名 helper 重复定义。
- **README 按全仓模板重写 + 本 CHANGELOG 建立**：README 补齐架构概览、职责边界、三类使用场景
  （含完整可运行示例与内部行为）、结构体/Option/API 速查、错误处理表、测试与性能基线章节。

### 变更

- **【边界一致性】`GenerateCustomToken` 空 map 归一为 nil**：原实现 `if kv != nil` 对空 map
  仍分配 `len=0` 的非 nil map，与 `GenerateCustomToken(nil)` 的 nil 语义、以及 `GenerateToken`
  归一后的 nil 均不一致（同一「不传字段」意图出现两种结果）。现改为 `len(kv) > 0` 判断，
  两条签发路径的归一口径完全一致（nil 或空 map ⇒ 解析后 `Fields` 为 nil）；
  解析侧测试 helper `signTokenWithClaims` 的 `Fields` 同步由空 map 改为 nil，
  避免测试基建引入第二种不变量。由 `TestGenerateCustomTokenEmptyKVNormalizedToNil`
  （双路径一致性断言）守护。
- **【性能】解析侧算法白名单预计算**：原实现每次解析在 `buildParseOptions` 内现场构造
  `[]string{alg}`（解析热路径每次 1 次 slice 分配）。现由 `options.finalize()` 在
  `New` / `Reload` 的 `apply` 之后、`Store` 之前统一预计算 `validMethods`，
  白名单与生效配置同批原子提交；`buildParseOptions` 直接引用，并保留兜底
  （配置未经 finalize 时现场构造），未来新增构造路径遗漏 `finalize` 也不会静默失去
  算法白名单（保持 fail loud）。A/B 实测同一代码基线下回退版 63 → 预计算版 62 `allocs/op`。
  由 `TestReloadUpdatesValidMethods`（Reload 切换算法后白名单同步、新算法可解析、
  旧算法被拒绝）与既有 `TestParseTokenRejectsUnconfiguredMethod` 守护。
- **【文档】`HS256` / `HS384` / `HS512` 导出变量使用约定显式化**：三者为包级导出变量
  （沿袭 golang-jwt 上游设计），doc 注释与 README 明确「仅供读取与传参使用，
  库外代码禁止赋值」——重新赋值会波及同进程内所有引用方（全局可变状态）。
  改为函数属破坏性变更，本条仅做显式记录，API 保持不变。
- **【文档】`GetInt` / `GetUint64` 精度风险显式标注**：JSON 数字经 float64 中转只有 53 位尾数，
  绝对值超过 2^53 的整数（雪花 ID、Unix 纳秒时间戳）会静默丢失精度；README 与 doc 注释
  均标注该限制及「此类字段建议以字符串形式传递」的规避方式。
- **【文档】`WithExpectedAudience` 匹配语义精确化**：README 明确为「token 的 `aud` 声明与期望列表
  至少有一个交集时通过（由 golang-jwt/v5 的 `WithAudience` 定义）」，消除原「任一期望值」的
  主客体歧义；Option 表新增「签发侧 vs 解析侧」说明，解析侧三个选项的「适用 API」列
  改为具体函数名（对齐 tracer/logger 的 Option 表范式）。
- **【测试】`signTokenWithClaims` 去硬编码耦合**：签名算法与密钥改为从 `optStore` 读取，
  不再硬编码 HS256——若未来默认算法变更，helper 与解析侧自动对齐，
  用例失败不会指向错误的原因。
- **【防御】`WithAudience` 入参切片内部拷贝**：原实现直接存引用，配置生效后调用方修改原切片会
  改变后续所有签发的 `aud`（与 `GenerateToken` 对传入 map 浅拷贝的口径对齐）；空切片重置为 nil
  （不写入 `aud`）不变。由 `TestWithAudienceCopiesSlice` 守护。
- **【文档】`WithExpire` 非正值语义显式化**：0/负值有意接受（0 = 签发即过期，负值是测试构造
  「已过期 token」的标准手段），doc 注释与 README Option 表注明，消除「有意还是疏忽」的歧义。
- **【测试隔离切换到独立实例】**：旧全局单例时代各用例共享一个 Manager，「不得 t.Parallel()」
  曾以包级 `atomic.Bool` 占用标志强制串行；去全局实例化后 `newTestManager` 为每个用例创建
  独立 Manager、配置互不共享，串行约束与占用标志随之移除，`t.Parallel()` 可安全使用。
  同时 `testSigningKey` 改为 `TEST_ONLY_do_not_use_in_production_...` 前缀值，
  即使被误复制到配置文件也能一眼识别为测试密钥。
- **【边界】职责边界表补充「密钥轮换（kid 多密钥共存）」**：明确单一密钥配置、不支持 kid，
  轮换由调用方按部署策略灰度（与 token 黑名单同为显式非目标）。
- **【并发严格化】RefreshToken / RefreshCustomToken 单次 Load 配置**：原实现先调 `ParseToken` /
  `ParseCustomToken`（内部 Load 一次）再自行 Load 一次，两次 Load 之间若发生配置热更新，
  会出现「用旧配置校验、用新配置签发」的中间态（逻辑时序问题，非 data race）。
  现抽出共享内核 `parseTokenWith` / `parseCustomTokenWith`，校验与重新签发冻结在同一份
  配置快照上；跨调用语义（「按刷新时刻的配置重新签发」）不变。
  `TestReloadConcurrentWithParse` 同步扩展：请求线程由「签发 → 解析」扩为
  「签发 → 解析 → 刷新 + 自定义字段签发 → 自定义字段刷新」，
  两条刷新链路均纳入热更新并发覆盖（-race 取证用例）。
- **【文档】签发入口显式化并发约定**：`GenerateToken` / `GenerateCustomToken` 的 doc 与 README
  注明「传入 map 在函数返回前不得被并发读写」（Go map 非并发安全，内部会遍历浅拷贝；
  浅拷贝不递归，嵌套引用类型 value 由调用方保证），把原隐含的职责边界变为明示。
- **【安全，默认值变更】删除硬编码默认签名密钥 `zaq12wsxmko0`**：原实现将公开的示例密钥作为
  `defaultSigningKey`，调用方忘传 `WithSigningKey` 或配置漏读时会**静默用公开密钥签发 token**
  （任何拿到源码的人都能伪造）。现在默认不含密钥，未配置时签发/解析返回
  `ErrSigningKeyNotConfigured`（fail loud）。
  **行为破坏性变更（semver major）**：依赖默认密钥的调用方必须显式传 `WithSigningKey`；
  生产调用点（`internal/config`、`cmd/*/initial`）原本就传密钥，不受影响，
  测试调用点已随测试基建统一改为 `newTestManager` 注入密钥。
  由 `TestReloadWithoutOptionsResetsToNoKey` 守护。
- **【安全】解析时显式限定签名算法**：解析入口经 `buildParseOptions` 统一追加
  `jwt.WithValidMethods`（白名单为当前配置的算法，预计算方式见上「白名单预计算」条目），
  只信任当前配置的算法，收紧 token header 的算法攻击面
  （原实现依赖 keyFunc 返回的 key 隐式约束）。
  由 `TestParseTokenRejectsUnconfiguredMethod` 守护。
- **【安全】`WithSigningKey("")` 空串视为未设置**：忽略并保留当前值，避免配置中心返回空值时
  把已有密钥静默清空（与上述 fail-loud 检查叠加：从未设置过密钥时仍报错）。
  由 `TestWithSigningKeyEmptyKeepsOldValue` 守护。
- **`GenerateToken` / `GenerateCustomToken` 对传入 map 浅拷贝后再用**：原实现直接引用调用方的
  `kvs[0]` / `kv`，调用方在签发期间并发修改原 map 会构成 data race（与 `pkg/tracer` 早期
  `WithHeaders` 同模式）。现在签发前浅拷贝，签发后原 map 的修改不影响已签发 token。
  由 `TestGenerateTokenCopiesFields` / `TestGenerateCustomTokenCopiesKV` 守护。
- **错误消息中文化**：`ErrNotInitialized`（原 `not yet initialized jwt, usage 'jwt.Init()'`）与
  `ErrSignatureInvalid`（原 `signature failure`）改为中文描述。
  **对外可见变化**：错误文本变更，用 `strings.Contains` 匹配旧英文文本的代码需同步调整
  （推荐改用 `errors.Is` 判定）。
- **`nbf` 零值不再写入 token**：原行为在未设置 `WithNotBefore` 时也写入 `nbf`，值为 Go 零值时间戳
  `-62135596800`；现在仅显式设置 `WithNotBefore` 后才写入。
  **行为变更**：新签发 token 的 payload 少了无意义的 `nbf` 字段；旧 token 解析不受影响
  （历史 `nbf` 在过去，校验恒通过）。由 `TestGenerateTokenOmitsZeroNotBefore` 守护。
- **`GenerateCustomToken` 参数类型由 `map[string]interface{}` 改为 `KV`**：`KV` 是
  `map[string]any` 的类型别名，签名完全等价，调用方无需改动，仅文档表达更清晰。
- **测试函数重命名为全仓统一命名方案**：标识符 `Test<被测方法><场景>`（全英文驼峰）+ 中文 doc 注释 +
  中文子测试名，与 `pkg/nacoscli` 对齐；原 `Test_defaultOptions` 等旧命名、
  硬编码第三方 token、`fmt.Println` 调试输出、`time.Sleep(2s)` 等待过期的写法一并清理
  （过期用例改用负过期时长直接构造已过期 token，无需睡眠、无 flaky）。
- **注册声明构造收敛到 `buildRegisteredClaims`**：原先 4 个签发入口各自重复构造
  `jwt.RegisteredClaims`（30 行 × 4 处），收敛后字段更新只改一处；`RefreshToken` /
  `RefreshCustomToken` 顺带补齐了 `CustomRegisteredClaims.Subject` 的刷新（原实现遗留旧值）。
- **`GenerateToken` 清理恒等赋值**：`nameVal := ""; if len(name) > 0 { nameVal = name }`
  与直接使用 `name` 等价，删除冗余分支。

### 修复

- **【安全 P0】`GetUint64` 对负 float64 静默转换为巨大无符号值（数据损坏）**：
  原实现只防了内存对象中的负 `int`，未防 JSON 往返后的 `float64`——`Fields["balance"] = -1`
  经 `uint64(v)` 得到 `18446744073709551615` 且返回 `ok=true`（调用方读到错误值而非失败）。
  现对负 `float64` 同样返回 `false`，与 int 口径统一。
  由 `TestGetUint64RejectsNegative`（JSON 往返 / 内存 float64 / 内存 int 三路径）与
  `FuzzParseCustomTokenNeverPanics`（负值种子 + 不变量）守护。
- **`WithSigningMethod(nil)` 防护**：原实现直接写入 nil，签发时 `jwt.NewWithClaims(nil, ...)`
  会 panic → 传入 nil 时忽略该选项、保留先前值。由 `TestWithSigningMethodNilIgnored` 守护。

### 兼容性说明

- **密钥默认值变更的兼容影响**：生产调用点（`internal/config/reload_jwt.go`、`pkg/gin/middleware`、
  `cmd/*/initial/initApp.go`）均已传 `WithSigningKey`，无需改动；**唯一受影响的是不传密钥的调用方**，
  其行为从「静默用默认密钥」变为返回 `ErrSigningKeyNotConfigured`——这正是安全默认值变更的目的。
  其余 API 变化（去全局实例化、`GenerateToken` 单参数签名、`Fields` nil 语义）均为显式破坏性变更，
  见上方「破坏性变更」章节，调用方按其指引迁移。
- **`CustomRegisteredClaims.Subject` 的双层声明有意保留**：外层 `Subject any`（`sub`）
  遮蔽内嵌 `jwt.RegisteredClaims.Subject`（`string`），保证 `sub` 为数字的历史 token 仍可解析；
  读 `sub` 请用 `claims.Subject`。详见 README「核心结构体/类型说明」。
- **测试 helper 文件命名相对包级质量基线矩阵的实测修正**：本仓
  `.golangci.yml` 配置 `run.tests: false` + `unused`，实测非 `_test.go` 后缀的 helper 文件会被
  `unused` 规则报 `is unused`（测试文件不参与分析），且会把 `testing` 包链进生产构建，
  故落地为 `test_helpers_test.go`；「跨测试文件共享 helper 集中一处」的规则本意不变。
  基线（`.qoder/skills/package-quality-baseline`）的交付物矩阵与验收清单已同步改为 `test_helpers_test.go`，
  后续包不再需要此类修正。
- **本包无外部服务依赖**：按包级质量基线不设 `integration_test.go` / `.env`，
  `go test -tags=integration` 与普通 `go test` 等价。
