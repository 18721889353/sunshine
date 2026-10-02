# 变更日志

本文件记录 `pkg/goemail` 的显著变更，遵循[语义化版本](https://semver.org/lang/zh-CN/)。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
变更类型分为 `新增`（Added）、`变更`（Changed）、`移除`（Removed）、`修复`（Fixed）。

本包尚无发布版本，以下记录未发布期间的累计变更；打第一个 tag（如 `v1.0.0`）时，将下方 `## [未发布]` 拆为
`## [未发布]`（保留后续新改动）与 `## [v1.0.0] - YYYY-MM-DD`（归档本次全部变更）。

风险条目编号沿用改造评审的轮次体系 `R<轮次>-<序号>`，可按编号回溯「哪一轮评审提出」。

## [未发布]

### 新增

- **被忽略字段的主动 WARN 告警（R4-2）**：三 provider 的 `SendEmail` 在校验通过后，对本 provider 不消费的
  字段打 `当前 provider 忽略部分字段` Warn 日志（带 provider 名与各字段计数）——SMTP 提示 `Tags`；
  腾讯/阿里提示 `Cc`/`Bcc`/`ReplyTo`/`Attachments`/`Headers`/`Tags`。README 字段支持矩阵虽已标注「忽略」，
  但不看 README 的用户会误以为「已经抄送」——静默丢弃是生产事故级误判（终审 P1）。
  同轮为阿里 `SingleSendMailRequest.ReplyToAddress` 硬编码补充语义注释：阿里 API 的该字段是
  「是否允许回复」布尔开关，回复地址由控制台账户默认值决定，无法从 `req.ReplyTo` 注入。
  - 影响面：仅可观测性（多一条 Warn 日志）；发送行为与返回值零变化。
  - 守护：随各 provider 入口测试回归（告警不改变控制流，无独立断言）；README 矩阵与注意块同步更新。
- **`Config.BatchConcurrency` 批量并发上限配置（R5-2）**：`runBatchEmail` 的并发上限由包级硬编码 10
  改为按 `Config.BatchConcurrency` 解析——`0`/负值回落默认 10（存量零配置调用方行为不变），
  正值原样生效且不额外封顶（SMTP 服务器与云 API 速率限制差异大，由调用方按自家 provider 自定）。
  - 影响面：新增配置字段，零配置调用方行为与此前完全一致；`Config.String()` 输出同步带上该字段。
  - 守护：`TestResolveBatchConcurrency`（零值/负值/正值/大值四分支）、
    `TestRunBatchEmail`「自定义并发上限生效」子测试（30 条、上限 3，断言峰值 ≤ 3）。
- **测试体系（R3-1）**：原 `client_test.go` 单文件（330 行、子测试名英文、默认真实网络 + `t.Logf` 吞失败）
  拆为按被测文件对应的 `smtp_test.go` / `tencent_ses_test.go` / `aliyun_dm_test.go` / `client_test.go`
  与共享构造 `test_helpers_test.go`，命名统一为方案 A（英文驼峰标识符 + 中文 doc + 中文子测试名）。
  - `integration_test.go`（带 `//go:build integration`）：`TestMain` 看门狗（默认 120s，`GOEMAIL_TEST_TIMEOUT` 可调）、
    `loadDotEnv`（只补缺不覆盖，语义承自原 `test/env_loader.go`）、凭据缺失 Skip / 已配置仍失败 Fail /
    真实发送需 `GOEMAIL_ALLOW_PAID_SEND=1` 的三层边界，7 个 `TestIntegration_*` 用例覆盖三家 provider 的
    真实发送、批量、模板与状态查询入口。
  - `benchmark_test.go`：8 条无网络 RTT 的基准（校验/脱敏/解析/聚合/并发骨架），固定 benchtime 100000x，
    读数与解读见 README「性能基线与模糊测试」。
  - `fuzz_test.go`：4 个 target（`FuzzIsValidEmail` / `FuzzMaskEmail` / `FuzzSendFailFast` /
    `FuzzRunBatchOrdering`），守护不 panic、失败不留半截产物、错误消息不含裸换行、批量保序与聚合序号等不变量；
    种子随常规 `go test` 执行。`testdata/fuzz/FuzzMaskEmail/` 保留一条退化语料
    （`00*00@0`，掩码后恰等于原串），守护断言不退化成可被巧合击穿的「输出 ≠ 输入」写法。
- **`StatusSuccess` 常量与 `(*SendResult).IsSuccess()` 判定助手（R2-1）**：统一「`err == nil` 不等于成功」
  的判定入口——云业务失败的典型形态是 `err == nil` + `Status=failed`，只看 error 会静默漏判；
  `IsSuccess` 对 nil receiver 返回 false，可对可能为 nil 的 result 直接调用。README「API 速查」配套
  加了 `err == nil ≠ 成功` 警告。
- **批量发送并发骨架 `runBatchEmail` 与失败聚合 `batchErrors`（R2-2）**：三 provider 的 `SendBatchEmail`
  由各自手写的「for 循环 + 吞掉 error」改为共用骨架——worker 数 `min(batchConcurrency=10, len)`、
  results 按输入下标保序回填、单条失败不中断批量、全部跑完返回 `errors.Join` 聚合 error
  （带「第 N 条」输入序号，含 nil 结果与状态矛盾防御分支）。
- **PII 与密钥脱敏（R1-4 新增构件）**：`maskEmail`（本地部分首 2 尾 2、`@域名` 保留便于排查投递）、
  `maskSecret`（首 2 尾 2、≤4 全覆盖）、`maskAccessKey`（首 4 尾 4、≤8 全覆盖）三个纯函数，
  以及值接收者的 `Config.String()`——`%v/%+v/%s/%q` 等走 Stringer 的路径不再落密钥明文
  （`%#v` 与 zap 按字段反射不走 Stringer，已在 `String()` 注释中声明为禁用路径）。
- **README 按模板重写 + 本文件新建 + `env.example` 重写（R3-4）**：README 按 doc-templates 章节顺序
  重排（架构概览 / 职责边界 / 使用场景选择 / 参数结构体 / API 速查 / 错误处理 / OpenTelemetry /
  集成测试 / 性能基线与模糊测试），去全部 emoji，修 `install_deps.sh` 死链与 `HtmlBody`→`HTMLBody` 笔误，
  新增「字段支持矩阵」如实标注云 provider 忽略的字段，删除无实测依据的性能表；
  `env.example` 旧变量集（`TENCENT_AK` 等）替换为与 `integration_test.go` 实际 `os.Getenv` 一致的
  19 个 `GOEMAIL_*` 变量。

### 变更

- **`email.template_id` span 属性类型 `Int` → `Int64`（R7-7）**：原 `int(templateID)` 在 32 位平台
  对超过 `MaxInt32` 的模板 ID 会溢出为负数（只影响 span 属性、不影响发送）。改 `attribute.Int64`，
  64 位平台行为不变。
- **`SendRequest.Headers` 字段注释补充 `Reply-To` 覆盖提示（R7-8）**：Headers 在 `req.ReplyTo`
  之后写入，含 `Reply-To` 的自定义头会覆盖前者——语义选择而非 bug，注释固化避免误用；
  同轮修正 README 场景六「硬超时可改用腾讯/SMTP」的误导措辞（SMTP 拨号同样不直接响应 ctx）。
- **`runBatchEmail` ctx 感知的 worker 退出（R5-3）**：任务队列循环在每次取任务前检查 `ctx.Done()`，
  取消后不再发起新任务，未执行条目回填 `Status=failed` +「批量任务因上下文取消未执行」，
  `results` 仍完整保序（`len(results) == len(reqs)` 恒成立）；已进入发送的条目按各自返回收尾。
  原实现取消后剩余任务仍全部进入 worker（依赖 send 内部 ctx 快速失败），白跑一轮调度。
  - 影响面：**取消语义收紧**——此前取消后所有条目都会实际调用 send（即便快速失败），
    现在未发起条目直接回填；调用方拿到的 results 条数与顺序保证不变。
  - 守护：`TestRunBatchEmail`「ctx取消后停止发起新任务」（不断言精确次数——取消时刻与其他 worker
    的 select 存在竞态窗口，断言「未全量发出」这一确定性下界）；`FuzzRunBatchOrdering` 不变量 1 同步覆盖回填路径。
- **错误与日志中文化（R2-3）**：三份 `validateRequest` 及全部对外错误消息、Info/Warn 日志由英文改为中文
  （project-conventions 18.1/18.2），外部可控值（邮箱地址、附件名等）统一用 `%q` 转义，
  防止单行日志/单行 span 错误被换行注入拆断。**对外可见变化**：错误文本由
  `invalid from address: xxx` 变为 `发件人地址格式不合法: "xxx"` 等；按原文匹配错误文本的调用方需同步调整。
- **`AliyunDMClient.SendTemplateEmail` 签名修正（R2-5，破坏性）**：
  由 `(ctx, from, to []string, templateName, templateData)` 改为 `(ctx, from, receiversName, templateName)`。
  依据阿里云官方 API 文档：`BatchSendMail` 的 `ReceiversName` 是**预先创建的收件人列表名称**（控制台上传
  CSV/TXT，模板变量由列表的列提供），既不接受内联地址列表，也不接受把模板变量 JSON 塞进 `TagName`
  （`TagName` 是 `CreateTag` 创建的邮件标签名）。原实现把 `strings.Join(to, ",")` 当 `ReceiversName`、
  把 `templateData` 序列化后写 `TagName`，两条都会被云侧拒绝或静默丢变量。
  **迁移方式**：调用方改为传预先创建的收件人列表名称，模板变量改由收件人列表的列提供。
  本轮无凭据，未在真实云环境实测，仅静态检查与单测入口校验通过。
- **`ValidateEmail` 抽出包私有纯函数 `isValidEmail`**：导出函数只保留日志/span 外壳（统一写
  `email.is_valid` 与耗时属性），判定核心独立可测——fuzz 与基准直压纯函数，避免每次迭代写一条 Info 日志。
  对外行为不变。
- **`integration_test.go` 删除 `dirOf` 薄封装（R4-4，内部重构）**：改用标准库 `filepath.Dir` +
  `filepath.Join`。原注释「避免为一处调用引入 import 分组调整」的考量不成立——净增一行 import，
  手写路径分隔逻辑反而增加维护面（终审 P2）。仅影响测试文件，对外不可见。

### 移除

- **`test/` 目录整体删除（R3-4）**：`env_loader.go` 的 .env 加载语义并入 `integration_test.go` 的
  `loadDotEnv`（含候选路径、只补缺、去引号语义）；`send_and_query.go` 内嵌的真实个人信息
  （企业/个人邮箱、模板 ID、公司名）改为 `GOEMAIL_*` 环境变量驱动后删除。包外不再存在可被 `go run`
  的辅助源文件，运行真实发送的入口收敛到 `go test -tags=integration`。
- **`examples.go` 删除（R3-4）**：11 个 `Example*` 函数是生产源码中的示例代码，逐条核对后并入 README
  「使用场景选择」七个小节（其中 `context.WithTimeout(..., 30)` 的 30 **纳秒** bug 在 README 中修正为
  `30*time.Second`）。生产源码不放 Example 函数；README 中的示例是可复制运行的完整代码。

### 修复

- **腾讯云 `Simple` 正文缺 base64 编码（阻断定版，R6 全仓通读，R7-1）**：`SendEmail` 把正文原文
  直接填进 `Simple.Html`，而 SDK 字段注释明确要求「base64之后的Html代码」「base64之后的纯文本信息」
  （`ses@v1.3.86/models.go` 一手证据）——原文会被服务端当 base64 解码，轻则乱码重则直接报错，
  是腾讯云发送的主路径缺陷。抽为纯函数 `buildSimpleBody` 统一编码。
  - 影响面：所有调用 `TencentSESClient.SendEmail` 的请求；修复前主路径不可用于生产。
  - 守护：`TestBuildSimpleBody`（四分支：仅 HTML/仅文本/同传/全空，断言 base64 编码值与合法可解码）。
  - 未实测声明：修复依据为 SDK 字段注释与 API 文档，**真实云发送本轮仍无凭据未实测**。
- **腾讯云 `TextBody` 被塞进 `Html` 字段、`Text` 字段恒不设置（阻断定版，R7-2）**：原实现
  `bodyData := req.TextBody; if HTMLBody != "" { bodyData = HTMLBody }` 后只填 `Simple.Html`——
  只传 Text 时纯文本被当 HTML 渲染（换行丢失）；两者同传时 TextBody 被静默丢弃；SDK 语义中
  `Text` 是独立字段（Html 缺省时展示纯文本，同传时作为纯文本样式版本），原实现从未触达。
  与 R7-1 同段代码合并修复：`Html`/`Text` 各自独立填设（均 base64），至少填一个仍由 validateRequest 保证。
  - 影响面：腾讯云 `SendEmail` 的正文行为——**只传 TextBody 的请求从「被当 HTML 渲染」变为纯文本发送**；
    同传时 TextBody 不再丢失（随 Text 字段送达云端作为纯文本版本）。
  - 守护：`TestBuildSimpleBody` 四用例；README 矩阵「HTML 优先」表述同步修正。
- **阿里云三处 `response.Body` 未判 nil（阻断定版，跨 provider 一致性缺口，R7-3）**：`SendEmail` /
  `SendTemplateEmail` / `GetTrackList` 在 `err == nil` 后直接取 `response.Body.EnvId` 等字段——
  HTTP 200 但 body 空/畸形时 darabonba SDK 不填充 Body，解引用即 panic。与腾讯 R1-1（本文件记为
  P0 阻断级）完全同型，腾讯已修、阿里三处漏修。
  - 影响面：阿里云三条响应解析路径；修复前空响应直接打死进程，修复后返回中文错误 + failed 结果。
  - 守护：**阿里 SDK 为具体类型无注入点，单测无法构造空响应**——守护依赖代码评审与真实云集成测试
    （本轮无凭据未实测）；腾讯侧同型路径已有单测可参照。
- **腾讯云 `SendTemplateEmail` 缺 subject 非空校验（建议修，R7-4）**：入口校验了 from/格式/to/格式/
  templateID，唯独漏 subject——空主题直发云 API，与 R5-1（from 只判空）同型的残留缺口。
  - 影响面：腾讯模板发送的 subject 参数；修复前云端拦截浪费一次网络往返，修复后本地拦截。
  - 守护：`TestTencentSendTemplateEmailEntryValidation` 表新增「主题为空」用例（六分支）。
- **SMTP `validateRequest` 不校验 `ReplyTo`（建议修，R7-5）**：Cc/Bcc/ReplyTo 三个地址类字段中
  两个校验、一个不校验——非法 Reply-To 头被 gomail 直接设置，可能触发服务器拒收。
  - 影响面：`ProviderType=smtp` 且带 `ReplyTo` 的请求；修复前非法地址透传到协议层。
  - 守护：`TestSMTPValidateRequest` 表新增「回复地址格式非法」用例。
- **`maskEmail` 对非 ASCII 本地部分产生无效 UTF-8（建议修，R7-6）**：原实现按 byte 切片
  （`local[:2] + ...`），「用户@例子.中国」这类地址会在多字节字符中间切断，日志/span 出现乱码。
  改为 `[]rune` 按字符粒度掩码（合法输入的 ASCII 行为完全不变）。
  - 影响面：日志与 span 展示路径（PII 掩码值），不改变任何业务判定；byte 长度对非 ASCII 输入不再守恒
    是预期行为。
  - 守护：`TestMaskEmail` 新增四个中文用例（短中文整体掩码/长中文首2尾2/非法中文两档）；
    `FuzzMaskEmail` 长度守恒断言同步改为 rune 粒度（结构断言不变，仍防「输出 ≠ 输入」退化）。
- **`SendTemplateEmail` 入口校验与 `SendEmail` 不一致（P2，终审，R5-1）**：两份
  `SendTemplateEmail`（腾讯/阿里）对 `from` 只判空不校验格式，非法地址直发云 API、
  由云端 `InvalidEmailAddress` 才拦下——同一个 `from` 走 `SendEmail` 本地拦、走
  `SendTemplateEmail` 云端拦，口径分裂且浪费一次网络往返。
  - 影响面：腾讯/阿里 `SendTemplateEmail` 的 `from` 参数（腾讯另含 `to` 逐地址格式校验，
    与 `SendEmail` 同口径）；`receiversName`/`templateName` 是云侧预创建标识符，不做格式校验。
  - 守护：`TestTencentSendTemplateEmailEntryValidation` / `TestAliyunSendTemplateEmailEntryValidation`
    入口校验表新增「发件人格式非法」用例，断言本地拒绝且不联网。
- **构造与发送/查询入口的 nil 入参直接 panic（P0，终审，R4-1）**：`NewEmailClient(nil)` 解引用
  `cfg.ProviderType` 即 panic；三份 `SendEmail(nil)` 首行 `maskEmail(req.From)` 即 panic；三份
  `GetEmailStatus(nil)` 首行 `query.MessageID` 即 panic——与同仓 gosms R1-P0-1 完全同型，
  而同仓其他包的 `New*` 均有 nil 检查，唯本包三轮未收口（R1 收口了响应侧 nil 安全，入参侧此轮补齐）。
  尤其 `SendBatchEmail` 把切片元素直接交给 `SendEmail`，含 nil 元素时在 worker goroutine panic
  且无人 recover，直接打死进程。
  - 影响面：全部三 provider 的 `SendEmail`/`GetEmailStatus` 入口与 `NewEmailClient` 构造入口——
    修复前传 nil 即崩，修复后返回中文错误 + `StatusFailed` 结果，行为从 panic 变为正常错误返回。
  - 守护：`TestNewEmailClient`「nil配置返回错误」用例；`TestSMTPNilInputGuard` /
    `TestTencentNilInputGuard` / `TestAliyunNilInputGuard`（SendEmail/GetEmailStatus 各一个子测试，
    SMTP 另含「批量含nil元素不panic且聚合报错」）；`FuzzSendFailFast` 新增第 4 条不变量
    （三 provider 的 nil req / nil query 种子，仅测「置空必填字段」测不到该路径）。
- **SMTP `UseTLS=false` 分支的冗余 `TLSConfig` 设置（P1，终审，R4-3）**：原 `else` 分支设置
  `&tls.Config{InsecureSkipVerify: false}`——这是 Go 零值，与不设置完全无差别，却让代码看起来像
  「显式配置了 STARTTLS」，混淆意图（gomail 的 `startTLSConfig` 在 `TLSConfig` 为 nil 时会自行
  填充默认配置并补 ServerName）。删除 else 分支，注释固化 gomail 默认行为。
  - 影响面：行为零变化（等价清理）；`crypto/tls` import 随之移除。
  - 守护：`TestNewSMTPClient` 端口语义子测试；真实握手属集成测试范畴，本轮无凭据未实测。
- **腾讯云 SES 响应字段裸解引用导致进程 panic（P0 阻断级，R1-1）**：`SendEmail` / `SendTemplateEmail` /
  `GetEmailStatus` 对 `*response.Response.MessageId`、`*status.DeliverStatus`、`*status.SendStatus`、
  `int(*status.SendStatus)` 等逐字段裸解引用，云 API 在特定状态下（空响应、字段缺失）任一指针为 nil 即
  panic，直接打死调用方进程。新增 `derefString` / `derefInt64` 统一 nil 安全取值，空响应显式转错误，
  状态列表解析抽为 `parseEmailStatusList`（nil 列表/nil 元素/全 nil 字段均安全，字段缺失不填零值冒充）。
  - 影响面：腾讯云 SES 三条响应解析路径；SMTP 与阿里云不受影响（不消费腾讯响应结构）。
  - 守护：`TestDerefHelpers`、`TestParseEmailStatusList`（含「全nil字段不panic」回归用例）、
    `TestParseSendStatus` / `TestParseDeliverStatus`（nil 状态码映射）。
- **构造函数与查询方法回写共享状态（数据污染，R1-2）**：`newTencentSESClient` / `newAliyunDMClient` /
  `newSMTPClient` 在默认值缺失时直接改写入参 `cfg.Region` / `cfg.SMTPPort`（cfg 常被多方共享，
  构造一个客户端会改变其他持有方看到的配置）；`GetEmailStatus` 为满足「RequestDate 必填」直接突变入参
  `query.FromDate`（调用方下次查询会拿到被改过的日期）。改为本地解析默认值存入客户端私有字段
  （`region` / `port`），日期用 `resolveRequestDate` 返回值副本。
  - 影响面：所有共享同一 `*Config` 创建多个客户端的调用方，以及复用 `*EmailStatusQuery` 的查询调用方；
    单客户端单次查询的表象行为不变。
  - 守护：`TestNewTencentSESClient` / `TestNewAliyunDMClient` / `TestNewSMTPClient` 的「不回写共享配置」
    子测试、`TestResolveRequestDate`（入参值副本语义）。
- **SMTP 附件写入固定路径（路径穿越 / 并发互踩 / 内容残留，R1-3）**：原实现把附件按 `fmt.Sprintf("/tmp/%s",
  Filename)` 直接写盘——Windows 无 `/tmp`；`Filename` 含 `../` 时路径穿越可写任意位置；并发发送同名附件
  互相覆盖；且写完从不删除，用户附件内容永久残留在磁盘。改为 `os.MkdirTemp` 独立临时目录 +
  `filepath.Base` 净化文件名（拒绝空名与 `.`/`..`）+ `defer os.RemoveAll` 整体清理（失败打 Warn 日志）。
  - 影响面：仅 `ProviderType=smtp` 且请求带 `Attachments` 的发送路径；无附件与云 provider 不受影响。
  - 守护：`TestSMTPAttachmentSafety`（非法文件名拨号前拒绝、拨号失败后无 `goemail-attach-*` 目录残留，
    拨号目标 `127.0.0.1:1` 为本地不可达端口、无外网流量）。
- **邮箱与密钥明文进入日志、Span 与格式化输出（PII/密钥泄漏，R1-4）**：`ValidateEmail` / `ValidateEmails`
  与各 provider 的 Info 日志、span 属性原样写入完整邮箱（`email.from`、`to_address` 等），含密钥的
  `Config` 被 `%v` 打印即落明文。span/日志侧统一过 `maskEmail`，`Config` 增加值接收者 `String()` 过
  `maskSecret` / `maskAccessKey`，并补 `email.request_id` 属性（复用 `requestIDAttr`）。
  - 影响面：可观测性链路（span 属性、日志字段）与走 Stringer 的格式化输出；业务判定与网络请求的字段值
    不变（掩码只作用于展示路径）。注意 `Config` 字段本身仍是明文，不要序列化后对外发送。
  - 守护：`TestMaskEmail` / `TestMaskSecret` / `TestMaskAccessKey` / `TestConfigString`
    （四种格式化动词 + 指针接收）、`FuzzMaskEmail`（长度守恒、域名保留、结构化掩码断言）。
- **`err == nil` 被当作发送成功（判定语义，R2-1）**：原实现无统一成功状态，调用方只能判 `err`，
  而云业务失败返回 `err == nil` + `Status="failed"`，静默漏判。新增 `StatusSuccess` 常量统一三处
  `"success"` 字面量，`IsSuccess()` 作为唯一判定入口。
  - 影响面：所有按 `err != nil` 判定发送结果的调用方——**这是行为判定收紧**，原先漏判的失败现在会被发现。
  - 守护：`TestSendResultIsSuccess`（nil receiver / failed / success / 业务失败四分支）、
    集成测试 `assertSendSuccess` 统一走 `IsSuccess`。
- **批量发送静默吞掉单条错误（诊断失效，R2-2）**：原三份 `SendBatchEmail` 都是「for 循环 + `if err != nil
  { continue }`」，聚合 error 恒为 nil，调用方拿到全 nil error 时无法得知有几条失败、失败的是哪条；
  且串行逐条发送无并发吞吐。改为共用 `runBatchEmail`（见「新增」）。
  - 影响面：三家 provider 的 `SendBatchEmail` 全部路径；返回值从「恒 nil error」变为「有失败即聚合非 nil」，
    `results` 条数与顺序保证恒等于请求。
  - 守护：`TestBatchErrors`（7 种聚合与防御分支）、`TestRunBatchEmail`（保序 / 并发峰值 ≤ 上限 /
    确实并行）、`FuzzRunBatchOrdering`（任意批量大小的保序与序号）。
- **SMTP `UseTLS` 分支写反（连接失败，行为变更，R2-4）**：原实现 `UseTLS=true` 只设 `TLSConfig`、
  `else` 分支反而 `dialer.SSL = true`——语义完全颠倒：显式要求 TLS 的调用方实际走 STARTTLS，
  `UseTLS=false` 的调用方在 25/587 端口被强制隐式 SSL 导致握手必失败。现按 `net/smtp`/gomail 源码语义
  回正：`UseTLS=true` → 强制隐式 SSL（465 SMTPS）；`UseTLS=false` → 按 gomail 端口预设
  （465 仍隐式 SSL，25/587 走 STARTTLS 升级）。
  - 影响面：`ProviderType=smtp` 且 `UseTLS=false` 的调用方**行为变化**——原先 25/587 端口必握手失败，
    现在按端口自适应成功；`UseTLS=true` + 465 的主流配置行为不变。按端口选择：465 置 true，25/587 置 false。
  - 守护：`TestNewSMTPClient` 端口语义子测试 + 拨号参数注释固化；真实握手属集成测试范畴，
    本轮无凭据未实测。
- **`NewEmailClient` 出错返回 typed-nil 接口（R3-2）**：出错路径直接返回 `newSMTPClient(cfg)` 的
  `(*SMTPClient)(nil)`，接口本身非 nil——`client != nil` 恒成立，调用方判空失效后对 nil 接口调用方法即
  panic。新增 `asEmailClient` 辅助：出错一律返回真正 nil 接口。
  - 影响面：`NewEmailClient` 的全部错误分支（配置缺失、不支持的服务商）。
  - 守护：`TestNewEmailClient`「出错时客户端应为 nil」断言（typed-nil 时打印 `<nil>` 但接口非 nil，
    旧断言 `client != nil` 可捕获）。
- **`gomail.NewDialer` 收到端口 0（默认 465 失效，R3-3）**：构造函数把默认端口 465 解析进本地 `port`
  字段后，拨号器仍传 `c.config.SMTPPort`（未配置时为 0），端口默认值形同虚设、拨号必失败。改传解析后的
  `c.port`。由 R3 新增的 `TestNewSMTPClient` 端口语义子测试驱动发现。
  - 影响面：`SMTPPort` 未配置（为 0）的调用方——修复前该配置完全不可用，修复后按 465 拨号；
    显式配置了端口的调用方行为不变。
  - 守护：`TestNewSMTPClient`「端口为0时本地默认465且不回写共享配置」子测试。

### 兼容性说明

- **错误文本中文化**：全仓无 `pkg/goemail` 的实际调用方（仅 `internal/.../baseConsumer.go` 注释代码引用），
  无编译破坏面；错误字符串变更本身按「可能被 `strings.Contains` 匹配」的口径登记于「变更」条目，
  仓外调用方升级时按新文本对齐。
- **`AliyunDMClient.SendTemplateEmail` 为破坏性签名变更**，迁移方式见「变更」条目；同轮移除的
  `test/` 目录与 `examples.go` 影响面为包外直接引用它们的代码（仓内为零调用）。
- **未实测声明**：真实邮件发送（SMTP 拨号、腾讯/阿里云 API 调用）本轮**无凭据未实测**，集成测试在
  未配置凭据/未授权时全部 Skip（README「集成测试」记录了实测 Skip 汇总）；`-race` 本机执行报
  `exit status 0xc0000139` 未能完成，本包**不声称已跑过 `-race`**，真实证据需在具备 CGO 工具链的
  环境用 `CGO_ENABLED=1 go test -race -count=1 -short ./pkg/goemail/` 采集。
- 跨包规范边界与验收清单统一维护在 skill
  [`package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md)
  （交付物矩阵、测试命名、集成测试方法论、证据标准）与
  [`project-conventions`](../../.qoder/skills/project-conventions/SKILL.md)
  （代码写法、错误与日志中文、README 规则）。
