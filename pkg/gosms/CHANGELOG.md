# Changelog

本文件记录 `pkg/gosms` 的显著变更。

## Unreleased

### 第 1 版评审收口（Round 1）

#### 新增

- **`TemplateParam` 模板参数类型**：`SendRequest.TemplateParams` 由 `map[string]string` 改为
  `[]TemplateParam{Key, Value}` 切片——腾讯云模板参数是位置语义（%1%/%2%），map 随机遍历序无法保证
  参数顺序（见修复 R1-P0-5），必须用显式顺序的切片表达；阿里云为命名参数，按 Key/Value 转 JSON 不受影响。
  本类型同时是破坏性变更 R1-C1 的迁移目标
- **`Config.TencentSignName` 字段**：与既有 `AliyunSignName` 对称，供 `req.SignName` 为空时兜底；
  两个字段本轮同时启用（见变更「签名兜底」）
- **`Config.AliyunSignName` 由死字段转为实际生效**：原定义后无任何读取点，现于 `withSignFallback`
  中作为 `req.SignName` 为空时的回退值（仍为空则由 `Validate()` 拦截）
- **七个测试交付物**（按 `package-quality-baseline` 交付矩阵，此前仅有一份 `client_test.go`）：
  - `client_test.go`：`NewSMSClient`（nil 配置/未知类型/双 provider 创建）、`SendRequest.Validate` 全分支
    中文消息、`ValidatePhoneNumber` 与纯函数一致性、`FormatPhoneNumber`、`maskPhone` 边界、
    `withSignFallback`、`normalizeStatusQuery`、`requestIDAttr`、`failSpan`（`tracetest.SpanRecorder`
    断言 `codes.Error` + exception 事件）、`failedResult`/`failedStatusResult`
  - `tencent_sms_test.go`：创建校验、入口校验表（非法请求不发网络即返回）、
    `buildTemplateParamSet` 顺序守护、`parseSendSmsStatus`（空集合不 panic）、`parsePullSendStatus`、
    normalize clamp、`SendBatchSMS`（原 `req.PhoneNumbers[0]` panic 点守护）、`GetSMSStatus` 入口
  - `aliyun_sms_test.go`：创建校验、`newAliyunRuntime` 超时断言、入口校验、多号码拒绝（R1-C2 守护）、
    `parseSendSmsResult`、`parseQuerySendDetails`（Code≠OK 返回错误守护，见 R1-P0-8）、
    `parseSendDetailItems` 状态映射与时间解析、`aliyunCurrentPage`（R1-P0-7 守护）、
    normalize clamp、`SendBatchSMS`、`GetSMSStatus` 入口
  - `benchmark_test.go`：`BenchmarkValidatePhoneNumber` / `BenchmarkFormatPhoneNumber` /
    `BenchmarkMaskPhone` 三条纯函数基线（无网络 RTT），读数已录入 README 性能基线表
  - `fuzz_test.go`：`FuzzValidatePhoneNumber`（不 panic、校验通过 ⇒ `+` 开头且格式化恒等）、
    `FuzzFormatPhoneNumber`（不 panic、输出恒 `+` 开头且幂等），种子语料随常规 `go test` 执行
  - `integration_test.go`（`//go:build integration`）：手写 `loadDotEnv`（只补缺不覆盖）、
    `TestMain` 120s 看门狗、四个真实云用例（发送需 `GOSMS_ALLOW_PAID_SEND=1` 付费授权，查询免费）、
    缺凭据 Skip / 凭据齐全仍失败 Fail 的边界
  - `test_helpers_test.go`：跨文件共享 helper（配置构造与必成客户端）
- **`README.md`（文档）**：按 `doc-templates` 骨架重写——架构概览、职责边界、五个使用场景、
  四张参数表、API 速查、错误处理表、集成测试（环境变量表与 `integration_test.go` 实际 getenv 一致）、
  性能基线与模糊测试（含 `-race` 未验证的诚实声明）
- **`CHANGELOG.md`（文档）**：本文件

#### 变更

- **破坏性：`SendRequest.TemplateParams` 由 `map[string]string` 改为 `[]TemplateParam`（R1-C1）**：
  腾讯 `TemplateParamSet` 是位置参数，原实现遍历 map 随机序填入，多参数短信必然错位（见 R1-P0-5）。
  - 迁移方式：`map[string]string{"1": "123456"}` → `[]gosms.TemplateParam{{Key: "1", Value: "123456"}}`；
    阿里云侧行为不变（命名参数转 JSON）
  - 影响面：全部 `SendSMS`/`SendBatchSMS` 调用方。仓内 grep 确认活跃调用方为 0
    （`internal/mq/rabbitmq/consumers/baseConsumer.go` 中 gosms 引用均被注释），仅 `examples.go` 与
    `test/send_and_query.go` 同步更新
- **破坏性：阿里云 `SendSMS` 多号码由静默只发第一个改为返回明确错误（R1-C2）**：原实现取
  `PhoneNumbers[0]` 直接发送，其余号码不发也不告警（见 R1-P0-6）。迁移方式：多号码场景改用
  `SendBatchSMS`（内部逐个发送，路径不变）。影响面：传多号码的阿里云单发调用方（此前结果不可信）
- **破坏性：错误消息英→中（R1-C3）**：`phone numbers are required` 等改为中文，对齐
  `project-conventions` 中文错误规范。仓内无字符串匹配调用方，风险仅外部日志告警规则
- **腾讯云调用改用 `WithContext` 变体**：`SendSmsWithContext` / `PullSmsSendStatusByPhoneNumberWithContext`
  透传调用方 `ctx`——此前不透传，调用方取消/超时不生效（见 R1-P1-11）。SDK v1.3.93 源码确认两个变体存在
- **阿里云请求增加 15s 超时兜底**：`RuntimeOptions.SetConnectTimeout(5000)` + `SetReadTimeout(10000)`，
  tea 将两者相加作为 `http.Client.Timeout`，对齐腾讯 `ReqTimeout=15`。阿里 dysmsapi v4.1.3 源码无任何
  ctx 变体（无 `WithContext`、`RuntimeOptions` 无 `SetContext`），ctx 无法透传，只能以超时兜底
- **状态查询归一化与条数截断**：`GetSMSStatus` 入口经 `normalizeStatusQuery`（nil/缺号报错、零值日期
  回退 `ToDate=now`、`FromDate=ToDate-24h`、日期倒置报错、`Limit=0` 默认 10）；`Limit` 按云上限截断——
  腾讯 100（`PullSmsSendStatusByPhoneNumber` 上限）、阿里 50（`QuerySendDetails` PageSize 上限）
- **签名兜底启用**：`req.SignName` 为空时回退 `Config.TencentSignName`/`Config.AliyunSignName`；
  删除腾讯侧「传空串即用默认签名」的误导注释（该注释与实际传参不符）
- **开始类日志由 INFO 降为 Debug**：「开始发送/开始查询/开始验证手机号」位于每次调用必经的热路径，
  批量场景刷 INFO 造成噪音（对齐 gozip P2-5 处理）；成功/失败日志仍为 Info/Error
- **手机号 PII 脱敏**：日志与 span 属性中的手机号统一经 `maskPhone`（保留首 3 尾 4，≤7 位全 `*`）；
  `SendResult`/`SMSStatus` 等业务结构体与错误消息保留原文（调用方需要完整号码定位自己的入参）
- **可测性重构（无行为变更）**：响应解析与页码/模板参数组装抽为纯函数——`parseSendSmsResult`、
  `parseQuerySendDetails`、`parseSendSmsStatus`、`parsePullSendStatus`、`aliyunCurrentPage`、
  `buildTemplateParamSet`、`withSignFallback`、`newAliyunRuntime`，单测直接构造响应对象守护分支

#### 修复

- **空 `PhoneNumbers` / nil req / nil query 导致 panic（崩溃级，R1-P0-1）**：开始日志在验证之前取
  `req.PhoneNumbers[0]`，空列表越界、nil 解引用 → 入口校验链前置（`req == nil` → `Validate()` →
  归一化），全部失败路径经 `failSpan` 返回，取 `[0]` 前已保证非空
  - 影响面：所有发送/查询调用方的非法入参路径；合法入参行为不变
  - 守护：`TestAliyunSendSMSEntryValidation`、`TestTencentSendSMSEntryValidation`（5 类非法入参
    不发网络即返回）、`TestTencentGetSMSStatusEntryValidation`、`TestAliyunGetSMSStatusEntryValidation`、
    `TestSendRequestValidate`
- **span 状态被覆盖，失败被记为成功（可观测性级，R1-P0-2）**：业务失败已
  `SetStatus(codes.Error)`，函数末尾仍无条件 `SetStatus(codes.Ok, "sms sent")` 盖回成功，
  下游按 span 状态聚合的告警全部漏报 → 成功状态块只在成功路径执行；所有失败路径统一经
  `failSpan`（`RecordError` + `SetStatus(codes.Error)`）收口
  - 影响面：全部发送/查询路径的 Tracing 语义；SDK 返回值与业务结果不变
  - 守护：`TestFailSpan`（`tracetest.SpanRecorder` 断言 Error 状态与 exception 事件）、
    `TestFailedResult`、各入口校验测试
- **`json.Marshal` 失败分支 span 悬空且返回结构不一致（可观测性级，R1-P0-3）**：原分支
  `return nil, err`——不 RecordError、不 SetStatus、不返回 `SendResult`，与「失败也返回非 nil result」
  的既有语义冲突 → 改走 `failSpan` + `&SendResult{Status: StatusFailed, Error: ...}`
  - 影响面：`SendSMS` 的模板参数序列化分支——内部 map 由 string Key/Value 组装，正常输入下
    序列化不会失败，属防御分支；修复保证该分支与其它失败路径的 span 语义、返回结构一致
  - 守护：`TestParseSendSmsResult` 覆盖失败返回结构；`TestFailSpan` 覆盖 span 语义
- **腾讯 `SendStatusSet[0]` 越界 panic（崩溃级，R1-P0-4）**：响应检查只判 `!= nil` 未判 `len > 0`，
  空切片取 `[0]` 越界 → `parseSendSmsStatus` 先判长度，空集合视为业务失败
  （`StatusFailed` + 「响应中无发送状态记录」+ `failSpan`），不再 panic
  - 影响面：腾讯发送响应为空集合的路径（账号异常/模版拒绝等边缘返回）
  - 守护：`TestParseSendSmsStatus`「空集合返回失败不 panic」子测试
- **腾讯模板参数顺序错乱，多参数短信内容报废（数据错乱级，R1-P0-5）**：Go map 随机遍历序填入
  位置参数 `TemplateParamSet`，%1%/%2% 错位——验证码类短信直接发出错误内容 → `buildTemplateParamSet`
  按 `[]TemplateParam` 切片顺序取 Value，顺序显式可断言（Key 在腾讯侧仅自文档，注释写明）
  - 影响面：全部使用两个及以上模板参数的腾讯云发送；单参数不受影响。根治依赖破坏性变更 R1-C1
  - 守护：`TestBuildTemplateParamSet`（切片序 == `TemplateParamSet` 序）
- **阿里云多手机号静默丢弃（数据丢失级，R1-P0-6）**：`SendSMS` 只发 `PhoneNumbers[0]`，
  其余号码既不发送也无告警，调用方以为全发成功 → 返回明确错误指引改用 `SendBatchSMS`
  （行为变更见 R1-C2）
  - 影响面：阿里云单发接口传多号码的调用方——从「假成功」变为「报错」，fail loud
  - 守护：`TestAliyunSendSMSMultiPhoneRejected`
- **阿里云分页页码写死除数 10（查询错误级，R1-P0-7）**：`CurrentPage = Offset/10 + 1`，
  `Limit ≠ 10` 时页码错误，翻页读到重复/漏读数据 → `aliyunCurrentPage(offset, limit)` 改为
  `Offset/Limit + 1`（入口已归一 `Limit ≥ 1`，除零不可能）
  - 影响面：`Limit ≠ 10` 的所有分页查询；`Limit = 10` 行为不变
  - 守护：`TestAliyunCurrentPage`（含 `Offset=20, Limit=20 → 第 2 页` 的回归子测试）
- **阿里云查询失败被当作「0 条 + 成功」（静默吞错级，R1-P0-8）**：`parseQuerySendDetails` 原实现
  不检查响应 `Body.Code`，云侧返回非 `OK`（如 AccessId 失效、签名过期）时 `Items` 为空，
  调用方拿到 `StatusSuccess` + 空列表，排查方向被误导 → 先判 `Body.Code != "OK"`，
  提取 `Body.Message` 返回错误并经 `failSpan` 记录
  - 影响面：全部阿里云 `GetSMSStatus` 的失败路径；成功路径解析逻辑不变
  - 守护：`TestParseQuerySendDetails`「Code≠OK 返回错误」子测试
- **`SendRequest.Validate()` 是死代码，非法请求直达 SDK（校验缺失级，R1-P1-9）**：原仅测试调用，
  两个 provider 入口都不校验 → 两 provider 入口接入 `Validate()`；校验分层重构为纯函数
  `isValidPhoneNumber`（无 span/无日志）+ 导出版 `ValidatePhoneNumber(ctx, phone)`（保留 span 与
  `sms.request_id`），`Validate()` 内部委托纯函数，消除原 `context.Background()` 丢链路上下文的用法
  - 影响面：全部发送/查询入口的非法入参（报错前置，不产生网络调用）
  - 守护：`TestSendRequestValidate`（8 子测试，断言中文错误消息）、两云入口校验测试
- **`NewSMSClient(nil)` panic（崩溃级，R1-P1-10）**：构造函数不检查 nil 配置，解引用 panic →
  返回中文错误「配置不能为空」
  - 影响面：误传 nil 的调用方
  - 守护：`TestNewSMSClient`「nil 配置返回错误」子测试
- **腾讯云不透传 ctx，调用方取消/超时不生效（正确性风险级，R1-P1-11）**：原调用
  `SendSms(request)` / `PullSmsSendStatusByPhoneNumber(request)` 无 ctx 变体 → 改用
  SDK v1.3.93 的 `SendSmsWithContext` / `PullSmsSendStatusByPhoneNumberWithContext`
  - 影响面：全部腾讯云发送/查询调用方；`ctx` 超时与取消从不生效变为生效
  - 守护：编译期签名绑定 + 各入口校验测试（网络路径本机不测，见兼容性说明）
- **签名死字段与误导注释（文档/配置失效级，R1-P1-12）**：`Config.AliyunSignName` 定义后无任何
  读取点；腾讯注释写「使用配置中的默认签名」实际传空串 → 两云签名统一走 `withSignFallback`
  （`req.SignName` 为空回退 `Config.*SignName`），新增对称字段 `Config.TencentSignName`，
  删除不实注释
  - 影响面：依赖配置默认签名的调用方（从「死配置」变为生效）；显式传 `req.SignName` 的行为不变
  - 守护：`TestWithSignFallback`（回退/显式优先/双空路径）
- **`SMSStatusQuery` 无归一化，四种非法输入直达云 API（校验缺失级，R1-P1-13）**：nil query panic、
  `Limit=0`（阿里 `PageSize=0` 直接 API 报错）、`ToDate/FromDate` 零值、`FromDate > ToDate`
  → `normalizeStatusQuery` 统一拦截/回退（见变更「状态查询归一化」）
  - 影响面：全部状态查询调用方；合法查询行为不变
  - 守护：`TestNormalizeStatusQuery`（6 子测试）、`TestNormalizeTencentStatusQuery`、
    `TestNormalizeAliyunStatusQuery`
- **手机号 PII 明文入日志与 span（安全级，R1-P2-14）**：`sms.phone_to_validate`、
  `logger.String("phone", ...)` 直接记完整号码 → 引入 `maskPhone` 脱敏（边界：仅日志与 span 属性，
  业务结构体与错误消息保留原文）
  - 影响面：全部日志与 Tracing 输出；`SendResult`/`SMSStatus` 等业务返回值不变
  - 守护：`TestMaskPhone`（≤7 位全 `*`、国际号码首 3 尾 4 等 5 个边界）
- **手机号验证重复 span 样板与 INFO 噪音（可维护性级，R1-P2-15）**：`ValidatePhoneNumber`
  失败分支 4 处重复「RecordError + SetStatus + 返回」样板 → 重写为单出口（一次属性设置 +
  `failSpan` 收口）；验证日志降为 Debug，批量循环不再每号一条 INFO
  - 影响面：仅日志与 Span 记录代码，无行为变更
  - 守护：`TestValidatePhoneNumber`（含与纯函数一致性断言）、`TestRequestIDAttr`
    （`sms.request_id` 键保持不变）

#### 兼容性说明

- **三项破坏性变更（R1-C1/C2/C3）**：`TemplateParams` 类型、阿里多号报错、错误消息中文化——
  迁移方式均写在「变更」对应条目下。仓内活跃调用方为 0（grep 核实），受影响代码仅
  `examples.go` 与 `test/send_and_query.go`，已同步更新
- **阿里云 ctx 无法透传**：dysmsapi v4.1.3 源码无任何 ctx 变体、`RuntimeOptions` 无 `SetContext`
  （已核实），本轮以 15s 超时兜底替代，README「限制与声明」已登记该限制；腾讯云侧 ctx 完整透传
- **腾讯查询 `FromDate` 7 天上限不做 clamp**：超限由云 API 报错透传——静默截断查询范围会让
  调用方拿到比预期更少的数据且更难诊断；仅 `Limit` 按云上限截断
- **`requestIDAttr` 的 attribute key 维持 `sms.request_id`**，与 `request-id-propagation` skill
  第五节登记的 gosms 行一致，本轮未改动
- **实测边界（诚实声明）**：单测 31 项、bench 三读数、fuzz 两目标读数已录入 README（口径一致）；
  集成测试本机 4 用例全 Skip（无 `GOSMS_*` 凭据，付费发送另需 `GOSMS_ALLOW_PAID_SEND=1` 授权），
  真实云路径本轮未实测；`-race` 因本机 MinGW `0xc0000139` 未执行，证据需 Linux/CI
- **`SendRequest.Tags` 字段**：仍未使用，属预留字段（导出 API 保留），README 已标注
- 测试命名遵循 `package-quality-baseline` 方案 A（英文驼峰标识符 + 中文 doc + 中文子测试名）；
  未修改 `.golangci.yml`，未新增根 Makefile target，未动 `internal/` 下被注释的调用方代码

### 第 2 版评审收口（Round 2）

首轮质量评估（8.9 分）提出 5 项 P1 收口 + 文档修正，本轮全部落地。

#### 新增

- **`SendResult.MultipleResults` 逐号码子结果字段（R2-P1-1）**：腾讯云一次提交多号码时，按请求顺序
  保留每个号码的 MessageID/Status/Error；单号码与阿里云恒为 nil（零值兼容，非破坏性新增字段）
- **`SendResult.IsSuccess()` 判定助手（R2-P1-3）**：nil-safe，涵盖「业务失败 err == nil」陷阱，
  为调用方提供单一权威判定入口
- **`Config.String()` 与 `maskSecret`（R2-P1-5）**：`%v/%+v/%s` 打印 Config 时 SecretKey 自动脱敏
 （首2尾2，≤4 位整体掩码），与手机号 maskPhone 同一安全边界
- **`batchErrors` 聚合纯函数（R2-P1-4）**：收集批量结果中的全部失败（`errors.Join`，带条目序号），
  纯函数便于单测
- **守护测试**：`TestParseSendSmsStatus` 新增多号码三子测试（全成功/部分失败/Extra 隔离）、
  `TestSendResultIsSuccess`、`TestConfigString`、`TestMaskSecret`、`TestBatchErrors`（6 子）、
  两云构造函数副本语义子测试、两云 `SendBatchSMS` 聚合断言

#### 变更

- **腾讯多号码状态聚合（行为变更，R2-P1-1）**：主结果取首个号码（单值字段语义不变），任一号码
  失败则整体 `Status=failed` + 聚合错误（含 `N/M` 失败比例）；此前只取 `SendStatusSet[0]`，
  第 2..N 个号码的状态静默丢弃。影响面：多号码提交的腾讯调用方（单号码行为完全不变）
- **两云构造函数持有 Config 副本（行为变更，R2-P1-2）**：`newTencentSMSClient` 不再写
  `cfg.Region`（旧实现直接修改调用方结构体，与 withSignFallback 的副本约定冲突）；两云客户端
  统一持副本——调用方创建后继续修改 *Config 不再影响已创建客户端。影响面：复用同一 *Config
  的调用方（此前会被意外写入默认 Region，现在不会）
- **`SendBatchSMS` 外层 error 由恒 nil 改为聚合结果（行为变更，R2-P1-4）**：任一条目失败
 （传输层 err 或业务 Status=failed）时外层 error 非 nil（`第 N 条: ...`），全成功为 nil；
  「单条失败不中断批量」不变，results 始终为完整逐条结果。影响面：只看外层 error 的调用方
  会新看见失败（此前全部失败也返回 nil，属静默吞错）；仓内活跃调用方为 0
- **文档**：README 场景一示例 `result.Extra` 改为 `result.Error`（R2-P2-3，Extra 中无错误信息）、
  API 速查新增「err == nil 不等于成功」醒目警告、新增 SendResult 参数表、错误处理表补 3 行

#### 修复

- **腾讯多号码只返回第一个号码的状态（功能缺失级，R2-P1-1）**：`parseSendSmsStatus` 只取
  `SendStatusSet[0]`，多号码提交时第 2..N 个号码的 SerialNo 与状态被静默丢弃（R1-P0-4 只修了
  空切片越界，未修多号码丢失）→ 逐号码解析入 `MultipleResults` + 整体状态聚合 + nil 元素跳过
  （旧实现对 nil 元素直接取字段也有潜在 panic）
  - 影响面：一次提交多个号码的腾讯云调用方；单号码与阿里云不受影响
  - 守护：`TestParseSendSmsStatus/多号码全部成功返回逐号码状态`、`.../多号码任一失败整体failed且保留各号码状态`
 （含 Extra 拷贝隔离断言）、`.../单条成功`（MultipleResults 为 nil 断言）
- **`newTencentSMSClient` 修改调用方的 `cfg.Region`（副作用级，R2-P1-2）**：默认区域直接写回调用方
  结构体，复用同一 *Config 的调用方被意外写入；与 withSignFallback「用副本不修改调用方」约定冲突
  → 区域默认值落到副本，两云客户端统一持有 Config 副本
  - 影响面：复用 *Config 创建多个客户端或创建后读取 cfg 的调用方
  - 守护：`TestNewTencentSMSClient/不修改调用方Config且后续修改互不影响`、
    `TestNewAliyunSMSClient/持有Config副本且不修改调用方`
- **`err == nil` 即成功的 API 陷阱（API 语义级，R2-P1-3）**：云业务失败返回 `err == nil`，
  调用方漏检 `result.Status` 会把失败当成功 → 新增 `IsSuccess()` 助手 + README API 速查
  醒目警告 + 场景一示例注释标明两级判定。不选「业务失败也返回 error」（破坏性且违背
  R1 既有语义）
  - 影响面：全部 SendSMS 调用方的判定写法；返回值语义不变
  - 守护：`TestSendResultIsSuccess`（nil receiver / failed / 业务失败三态）
- **`SendBatchSMS` 外层 error 恒为 nil，全部失败也伪装成功（错误吞没级，R2-P1-4）**：
  凭据失效/网络全断时调用方只看外层 error 永远以为成功 → `batchErrors` 聚合全部失败返回；
  仍保持「单条失败不中断批量」
  - 影响面：全部批量发送调用方（行为变更详见「变更」）
  - 守护：`TestBatchErrors`（6 子：空/全成功/业务失败/无 Error 信息/多失败/nil 防御）、
    `TestTencentSendBatchSMS`、`TestAliyunSendBatchSMS` 的聚合断言
- **`Config.SecretKey` 打印明文泄露（安全级，R2-P1-5）**：无 Stringer，`%+v` 直接输出密钥明文，
  与手机号脱敏的安全边界不一致 → 实现 `Config.String()`（值接收，*Config 同样生效）
  + `maskSecret` 纯函数；不选自定义 SecretString 类型（破坏导出 API 签名）
  - 影响面：所有格式化输出 Config 的场景；字段本身仍为明文（doc 已声明勿序列化外发）
  - 守护：`TestConfigString`（%v/%+v/%s 三格式均不含明文 + 指针接收）、`TestMaskSecret`（4 边界）
- **README 场景一示例业务失败打印 `result.Extra`（文档级，R2-P2-3）**：Extra 中只有
  Code/Message/SerialNo，不含错误信息，误导排查方向 → 改为 `result.Error`
  - 影响面：仅文档示例，无代码行为变更

#### 兼容性说明

- `SendResult.MultipleResults` 与 `IsSuccess()` 均为新增（零值 nil / 无签名变化），非破坏性；
  破坏性在两处**行为变更**：`SendBatchSMS` 外层 error（恒 nil → 聚合失败）、客户端持 Config 副本
 （创建后修改 *Config 不再生效）——仓内活跃调用方为 0，迁移方式：批量失败判断改为
  `if err != nil`（或逐条查 `IsSuccess()`），配置在创建客户端前定稿
- 腾讯多号码聚合的 `Status` 判定变化：此前只看首个号码（首成功+后失败 = 假成功），现在任一失败即
  failed——这是修复目标，多号码场景结果更严格
- Round 1 的「业务失败 err==nil」语义**保持不变**（R2-P1-3 用助手而非破坏性 error 化解决）
- 实测边界沿用 Round 1 声明：本机未跑 `-race`（MinGW 限制）与真实云发送；本轮新增测试均为
  离线单测，随 `go test ./pkg/gosms/` 全量执行

### 第 3 版评审收口（Round 3）

终审（9.4 分，五项 P1 全清）后收口 3 项 P2 + 2 项 P3。

#### 新增

- **`maskAccessKey` 密钥标识脱敏（R3-P2-2）**：保留首 4 尾 4、≤8 位整体掩码——AccessKeyID 与
  SecretKey 组合使用，安全审计通常要求两者都不以明文入日志；保留首 4 尾 4 仍可区分「用的哪把密钥」
- **`runBatchSMS` 并发调度骨架（R3-P2-3）**：worker 数取 `min(batchConcurrency=10, len(reqs))`，
  任务队列消费、results 按输入下标回填、收尾经 `batchErrors` 聚合；两云 `SendBatchSMS` 统一复用，
  保证并发语义对称（可测性：send 回调注入，单测不发网络即可守护上限与保序）
- **四条新基准（R3-P3-1）**：`BenchmarkParseSendSmsStatus`（多号码路径）、`BenchmarkParsePullSendStatus`、
  `BenchmarkParseSendDetailItems`、`BenchmarkBatchErrors`——每次请求必经的解析/聚合热路径纳入基线，
  读数已录入 README 性能基线表（共 7 条）
- **守护测试**：`TestMaskAccessKey`（4 边界）、`TestRunBatchSMS`（3 子：空列表 / 保序且按输入序聚合 /
  并发峰值不超上限且确实并行）、`TestBatchErrors` 新增「状态矛盾」子测试、`TestConfigString` 增
  AccessKeyID 掩码断言

#### 变更

- **`SendBatchSMS` 由串行改为至多 10 并发（行为变更，R3-P2-3）**：单条失败不中断、输出顺序恒等于
  输入顺序、外层聚合 error 语义均不变；变化的是执行方式——完成次序不再按输入序（results 按下标
  回填保证对齐）、高吞吐批量不再受 1000 次串行 RTT 累加制约。影响面：批量发送调用方（仓内活跃
  调用方为 0）；调用方传入的 `[]*SendRequest` 在批量执行期间保持只读（既有约定）
- **`Config.String()` 同时脱敏 AccessKeyID（R3-P2-2）**：打印输出中 SecretKey 首 2 尾 2、
  AccessKeyID 首 4 尾 4。影响面：仅格式化输出，字段仍为明文
- **bench 基线由 3 条扩至 7 条并更新读数（R3-P3-1）**：README 性能基线表与多轮区间同步刷新

#### 修复

- **`MultipleResults` 子结果恒为 nil 未文档化（文档级，R3-P2-1）**：用户会困惑
  `MultipleResults[0].MultipleResults` 为何是 nil——实为正确设计（只展开一层，避免无限递归）但缺说明
  → 字段 doc 注释与 README SendResult 表均补「子结果的本字段恒为 nil，无递归嵌套」
  - 影响面：仅文档；行为不变（多号码解析逻辑本轮未动）
  - 守护：`TestParseSendSmsStatus/单条成功`（MultipleResults 为 nil 断言）+ 文档一致性核查
- **`batchErrors` 对 success+Error 矛盾组合静默吞错（防御级，R3-P3-2）**：`Status==success` 分支
  原直接跳过，理论上不应存在（SendSMS 成功路径保证 Error==nil）但防御性应暴露而非吞掉
  → 命中时计入聚合错误（`第 N 条状态矛盾（success 但 Error 非空）: ...`）
  - 影响面：正常路径零影响（矛盾态才会触发）；全成功仍返回 nil
  - 守护：`TestBatchErrors/状态矛盾success但Error非空` 子测试
- **`Config.String()` 保留 AccessKeyID 明文（安全级，R3-P2-2）**：仅掩码 SecretKey，
  打印仍含完整密钥标识——与 SecretKey 组合使用，单泄露也可被安全审计判为缺口
  → 接入 `maskAccessKey`（三格式 + 指针形态均断言无明文）
  - 影响面：所有格式化输出 Config 的场景
  - 守护：`TestConfigString`（AccessKeyID 明文不存在 + 掩码存在）、`TestMaskAccessKey`（4 边界）

#### 兼容性说明

- 本轮无破坏性 API 变更：`maskAccessKey` 为内部纯函数（未导出），`runBatchSMS` 为内部骨架；
  唯一对外可感的变化是 `SendBatchSMS` 的执行方式（串行 → 至多 10 并发）与 `String()` 打印内容
- 批量并发的诊断语义不变：`results[i]` 恒对应 `reqs[i]`、聚合错误的「第 N 条」恒指输入序；
  变的是日志产生顺序与各条完成时刻（并发固有）
- 实测边界沿用前两轮声明：本轮 37 项单测、7 条 bench 读数、fuzz 两目标 10s 读数已录入 README；
  `-race`（MinGW 限制）与真实云路径仍待 Linux/CI 采集——并发改动后建议在 Linux/CI 补跑
  `-race` 以覆盖 `runBatchSMS` 的并发写入路径

### 第 4 版发布前复核（R6 全仓复核收口）

R6 全仓复核（8.5 分）识别 1 项 P0 + 1 项 P1 + 2 项 P2，核心视角是**跨包对称性**——
goemail R3-2/R5-3/R5-2 的三项修复 gosms 未同步，本轮全部对齐；另含 R6 报告指出的
腾讯/阿里查询语义不对称（P1）与本轮顺手补的腾讯空响应 panic 隐患。

#### 新增

- **`asSMSClient` 构造辅助（R6-P0）**：把具体构造函数的多返回值转成接口返回，
  失败时丢弃 typed-nil、只返回 err——与 goemail `asEmailClient` 完全同型
- **`Config.BatchConcurrency` 字段与 `resolveBatchConcurrency`（R6-P2）**：批量并发上限
  由硬编码 const 改为可配（0/负数回落默认 10，正值不封顶）；`Config.String()` 同步输出该字段
- **守护测试**：`TestNewSMSClient` 新增「腾讯云/阿里云缺密钥时客户端必须为 nil」两条
  （typed-nil 泄漏守护，走的是旧实现真正会触发的路径）；`TestRunBatchSMS` 新增「自定义并发
  上限生效」「上下文取消后全部条目标记失败且零发送」「中途取消后不再发起新任务」三个子测试；
  新增 `TestResolveBatchConcurrency`（4 分支）；`TestParseSendDetailItems/发送状态映射`
  新增 `SendStatus==nil` 兜底断言（R6 指出原用例只断言长度不断言第 4 条状态）

#### 修复

- **`NewSMSClient` 返回 typed-nil 接口（P0 阻断发布，R6-P0）**：`return newTencentSMSClient(cfg)`
  把 `(*TencentSMSClient)(nil)` 装箱进接口——缺密钥等失败路径下 `err != nil` 但 `client != nil`
  恒成立，调用方按非空判定（常见写法）后续方法调用即 panic。与 goemail `NewEmailClient` R3-2
  完全同型，goemail 已修 gosms 未同步。旧测试只覆盖 nil 配置与未知 provider（均走 default
  直接 return nil，绕过 typed-nil），真正触发的缺密钥路径无测试
  - 影响面：全部失败路径的 `NewSMSClient` 调用方；修复后 `err != nil` 时接口必为真 nil
  - 守护：`TestNewSMSClient` 两条缺密钥子测试（断言 client == nil）
- **阿里 `parseSendDetailItems` 空状态字符串（P1，R6-P1）**：`SendStatus == nil` 时
  `smsStatus.Status` 保持零值空串（不属于任何合法状态），调用方 `switch status` 落 default
  分支误报；腾讯侧 `parsePullSendStatus` 的 default 分支兑底为 `StatusPending`，两云不对称
  - 影响面：阿里查询边缘返回缺字段时的状态值；修复后统一兜底为 `pending`
  - 守护：`TestParseSendDetailItems/发送状态映射` 新增 `got[3]` 断言
- **腾讯 `GetSMSStatus` 空响应静默返回成功（P1，R6 报告遗漏的同段隐患）**：原代码除
  `response.Response == nil` 按「查到 0 条」返回成功外，`response` 本身为 nil 时
  `response.Response` 解引用直接 panic（SDK 返回 `(nil, nil)` 即触发）。改为返回中文错误 +
  `failedStatusResult`，与阿里侧 `resp.Body == nil` 返回 error 的语义对称
  - 影响面：腾讯查询路径；修复前结构异常伪装成功（或 panic），修复后 fail loud
  - 守护：**腾讯 SDK 具体类型无注入点，单测无法构造空响应**——依赖代码评审与真实云集成测试
    （与 goemail R7-3 阿里同型问题同口径）
- **`runBatchSMS` 缺 ctx 感知退出（P2，R6-P2）**：循环不检查 ctx，取消后剩余条目仍逐个
  送进云 SDK；与 goemail `runBatchEmail` R5-3 修复对齐——取消后未执行条目回填
  failed + ctx.Err()，首个取消打一条 Warn（`sync.Once`），results 仍完整保序
  - 影响面：带取消/超时 ctx 的批量调用方；修复后取消即止，不再空跑调度
  - 守护：`TestRunBatchSMS` 两条取消子测试（预取消零发送 / 中途取消未全量发出）

#### 兼容性说明

- `runBatchSMS` 为内部骨架（未导出），签名新增 `concurrency` 参数不破坏外部 API；
  `Config.BatchConcurrency` 新增字段零值即回落默认 10，存量配置行为不变；`Config.String()`
  打印新增该字段（格式化输出变更）
- **未实测声明**：本轮无真实云凭据，腾讯空响应 fail loud 路径**未实测**（单测无注入点）；
  `-race` 仍受 MinGW `exit status 0xc0000139` 限制未跑，并发改动后建议 Linux/CI 补跑
