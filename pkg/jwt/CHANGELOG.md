# 变更日志

本文件记录 `pkg/jwt` 的显著变更，遵循[语义化版本](https://semver.org/lang/zh-CN/)。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
变更类型分为 `新增`（Added）、`变更`（Changed）、`修复`（Fixed）、`兼容性说明`。

本包尚无发布版本，以下记录未发布期间的累计变更；打第一个 tag（如 `v1.0.0`）时，将下方 `## [未发布]` 拆为
`## [未发布]`（保留后续新改动）与 `## [v1.0.0]`（归档本次全部变更）。

## [未发布]

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
- **`RequireSigningKey()` 选项**：传入后 `Init` 强校验密钥非空，缺失或为空直接 panic（fail fast），
  让「配置漏读密钥」在启动阶段暴露而非带病上线；不传则保持 fail-loud（首次签发/解析时返回
  `ErrSigningKeyNotConfigured`）。由 `TestRequireSigningKeyPanicsWhenMissing` /
  `TestRequireSigningKeyPassesWithKey` 守护。
- **导出错误变量 `ErrNotInitialized` / `ErrSignatureInvalid`**：原 `errInit` / `errSignature` 未导出，
  调用方只能字符串匹配错误文本；现在可用 `errors.Is` 判定「未初始化」与「兜底校验失败」两类错误。
  **非破坏性变更**：仅新增导出符号，原有返回路径不变。
- **`benchmark_test.go` 性能基线**：5 个基准（`BenchmarkGenerateToken` / `BenchmarkParseToken` /
  `BenchmarkGenerateCustomToken` / `BenchmarkParseCustomToken` / `BenchmarkRefreshToken`），
  纯 CPU 计算（HMAC 签名 + JSON 序列化），**不含网络 RTT**，定位是相对回归基线。
  实测读数（Windows、i7-10750H @ 2.60GHz、`-benchtime=1s`、两轮）：签发 4.4~4.6µs/2811B/37allocs，
  解析 7.0µs/3504B/60allocs，刷新 10.8~11.0µs/5502B/86allocs，`allocs/op` 两轮稳定。
  注：该读数为建立基线时的实测；后续安全加固（map 浅拷贝 + `WithValidMethods`）使各基准
  `allocs/op` 增加 1~2，最新读数与口径见 README「基准测试」。
- **`fuzz_test.go` 模糊测试**：3 个 target 守护不变量——`FuzzParseTokenNeverPanics` /
  `FuzzParseCustomTokenNeverPanics`（任意输入不 panic、失败必返回 nil claims、成功可重复解析）、
  `FuzzTokenRoundTripFields`（合法 UTF-8 的 uid/name 与字符串字段往返原样保留、数字字段无损还原）。
  种子语料随常规 `go test` 执行，挖掘需显式 `-fuzz`。种子阶段全绿，本轮未做长时间定向挖掘。
- **`test_helpers_test.go` 共享测试工具**：`initTestJWT`（固定测试配置 + 用例间还原全局配置）、
  `testSigningKey` 常量，供 jwt/option/benchmark/fuzz 四个测试文件共用，避免同名 helper 重复定义。
- **README 按全仓模板重写 + 本 CHANGELOG 建立**：README 补齐架构概览、职责边界、三类使用场景
  （含完整可运行示例与内部行为）、结构体/Option/API 速查、错误处理表、测试与性能基线章节。

### 变更

- **【文档】`WithExpectedAudience` 匹配语义精确化**：README 明确为「token 的 `aud` 声明与期望列表
  至少有一个交集时通过（由 golang-jwt/v5 的 `WithAudience` 定义）」，消除原「任一期望值」的
  主客体歧义；Option 表新增「签发侧 vs 解析侧」说明，解析侧三个选项的「适用 API」列
  改为具体函数名（对齐 tracer/logger 的 Option 表范式）。
- **【测试】`signTokenWithClaims` 去硬编码耦合**：签名算法与密钥改为从 `optStore` 读取，
  不再硬编码 HS256——若未来默认算法变更，helper 与解析侧自动对齐，
  用例失败不会指向错误的原因；`initTestJWT` 注释补明占用标志复位依赖
  Go testing 的「Cleanup 必然执行」保证（覆盖 Fatal / panic 路径）。
- **【防御】`WithAudience` 入参切片内部拷贝**：原实现直接存引用，`Init` 后调用方修改原切片会
  改变后续所有签发的 `aud`（与 `GenerateToken` 对传入 map 浅拷贝的口径对齐）；空切片重置为 nil
  （不写入 `aud`）不变。由 `TestWithAudienceCopiesSlice` 守护。
- **【文档】`WithExpire` 非正值语义显式化**：0/负值有意接受（0 = 签发即过期，负值是测试构造
  「已过期 token」的标准手段），doc 注释与 README Option 表注明，消除「有意还是疏忽」的歧义。
- **【测试机制化】`initTestJWT` 拦截并发进入**：原「各用例不得 t.Parallel()」只有注释约定；
  现用包级 `atomic.Bool` 占用标志，检测到并发进入直接 `tb.Fatal`——约束从注释升级为运行时机制。
  同时 `testSigningKey` 改为 `TEST_ONLY_do_not_use_in_production_...` 前缀值，
  即使被误复制到配置文件也能一眼识别为测试密钥。
- **【边界】职责边界表补充「密钥轮换（kid 多密钥共存）」**：明确单一密钥配置、不支持 kid，
  轮换由调用方按部署策略灰度（与 token 黑名单同为显式非目标）。
- **【并发严格化】RefreshToken / RefreshCustomToken 单次 Load 配置**：原实现先调 `ParseToken` /
  `ParseCustomToken`（内部 Load 一次）再自行 Load 一次，两次 Load 之间若发生配置热更新，
  会出现「用旧配置校验、用新配置签发」的中间态（逻辑时序问题，非 data race）。
  现抽出共享内核 `parseTokenWith` / `parseCustomTokenWith`，校验与重新签发冻结在同一份
  配置快照上；跨调用语义（「按刷新时刻的配置重新签发」）不变。
  `TestInitConcurrentWithParse` 同步扩展：请求线程由「签发 → 解析」扩为
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
  仓内 `pkg/gows/gows_test.go` 的 3 处裸 `jwt.Init()` 已同步补密钥，
  生产调用点（`internal/config`、`cmd/*/initial`）原本就传密钥，不受影响。
  由 `TestInitWithoutOptionsResetsToNoKey` 守护。
- **【安全】解析时显式限定签名算法**：两处 `ParseWithClaims` 增加
  `jwt.WithValidMethods([]string{o.signingMethod.Alg()})`，只信任当前配置的算法，
  收紧 token header 的算法攻击面（原实现依赖 keyFunc 返回的 key 隐式约束）。
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

- **全局配置数据竞争**：`Init` 写全局裸指针 `opt`、`GenerateToken`/`ParseToken` 等读同一指针，
  配置热更新（`internal/config` 的 `reloadJwtConfig`）与请求线程解析并发时构成 data race
  （`-race` 下应报告）→ 改为 `atomic.Pointer[options]` 原子读写，读写之间无需锁。
  由 `TestInitConcurrentWithParse`（8 对 goroutine × 200 次并发）守护。
  真实竞态证据需在 Linux/CI 采集：本机（Windows + MinGW）`-race` 因 cgo 运行库问题报
  `exit status 0xc0000139`，**本轮未在带 `-race` 的环境下验证**，仅常规 `go test` 通过。
- **`WithSigningMethod(nil)` 防护**：原实现直接写入 nil，签发时 `jwt.NewWithClaims(nil, ...)`
  会 panic → 传入 nil 时忽略该选项、保留先前值。由 `TestWithSigningMethodNilIgnored` 守护。

### 兼容性说明

- **对外 API 签名全部保持兼容**：`Init`、`GenerateToken`、`ParseToken`、`RefreshToken`、
  `GenerateCustomToken`、`ParseCustomToken`、`RefreshCustomToken`、`Claims`、`CustomClaims`、
  `KV`、`HS256/HS384/HS512` 均未变化（新增 `RequireSigningKey` / `ErrSigningKeyNotConfigured` 不影响旧调用）；
  生产调用点（`internal/config/reload_jwt.go`、`pkg/gin/middleware`、`cmd/*/initial/initApp.go`）
  均已传 `WithSigningKey`，无需改动；**唯一受影响的是不传密钥的调用方**
  （仓内仅 `pkg/gows/gows_test.go` 的测试，已同步修复），其行为从「静默用默认密钥」
  变为返回 `ErrSigningKeyNotConfigured`——这正是安全默认值变更的目的。
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
