# gosms

Go 语言多平台短信发送库，以统一接口封装腾讯云 SMS 与阿里云 SMS 的发送、批量发送与状态查询。

## 架构概览

```
pkg/gosms/
├── client.go           # 公共类型、客户端工厂、校验/归一化/脱敏纯函数、request_id 注入
├── tencent_sms.go      # 腾讯云实现：ctx 透传、位置参数顺序构建、响应解析
├── aliyun_sms.go       # 阿里云实现：HTTP 超时兜底、单号限制、分页换算、响应解析
├── examples.go         # 各场景可运行示例（Example* 函数）
├── *_test.go           # 单测/基准/模糊测试（与源文件一对一，另有 integration_test.go）
├── integration_test.go # 真实云服务集成测试（-tags=integration）
├── README.md           # 本文件
├── CHANGELOG.md        # 变更记录（R1-C1/C2/C3 破坏性变更迁移方式、R1~R3 修复条目与守护测试）
└── test/               # 手动真实发送验证程序（go run .，读取 test/.env）
```

**核心设计原则**：

1. **入口统一 fail loud**：nil 入参、请求校验、查询归一化全部在发起网络调用前完成；任何失败路径都返回非 nil 的 `*SendResult`/`*SMSStatusResult`（`Status=failed`），不留下悬空 span。
2. **模板参数必须有序**：`[]TemplateParam` 而非 `map`——腾讯云模板是位置参数（%1%、%2%），Go map 随机遍历序会导致占位符内容错位；阿里云为命名参数，顺序无关。
3. **埋点单一收口**：失败统一走 `failSpan`（RecordError + `codes.Error`）；成功才在函数末尾设置一次 `codes.Ok`，杜绝「先记 Error 再被末尾无条件 Ok 覆盖」的埋点反转。
4. **PII/密钥边界明确**：手机号仅在业务结构体（`SendResult`/`SMSStatus`）保留原文；日志与 span 属性一律经 `maskPhone` 脱敏；`Config` 打印经 `maskSecret`（首2尾2）与 `maskAccessKey`（首4尾4）掩码；错误消息保留原文便于调用方定位自己的入参。
5. **SDK 差异如实声明**：腾讯云 SDK 存在 `WithContext` 变体，调用方取消/超时可透传；阿里云 dysmsapi v4.1.3 **无任何 ctx 变体**，只能在 HTTP 层设超时兜底（详见「限制与声明」）。

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| 多平台短信发送/批量/状态查询（统一接口） | 是 | 本包 |
| 短信模板、签名的申请与审核 | 否 | 云服务商控制台 |
| 最终投递保证（运营商送达） | 否 | 仅查询并回传运营商回执 |
| 阿里云单次多号码提交 | 否 | 云 API 单次仅支持 1 个号码，多号码走 `SendBatchSMS` 逐条发送 |
| 号码合规性、黑名单、区域限制校验 | 否 | 本包仅做 E.164 格式校验（+ 开头、7~15 位数字） |
| 余额、套餐、计费管理 | 否 | 云服务商控制台 |

## 使用场景选择

### 场景一：发送腾讯云短信（`SendSMS`）

**适用场景**：单次提交一条或多条号码（腾讯云原生支持多号）。

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/18721889353/sunshine/pkg/gosms"
)

func main() {
	cfg := &gosms.Config{
		ProviderType:   gosms.ProviderTypeTencentSMS,
		Region:         "ap-shanghai",
		AccessKeyID:    "your-secret-id",     // 从环境变量或配置中心获取
		SecretKey:      "your-secret-key",    // 从环境变量或配置中心获取
		TencentAppID:   "1400282665",         // 腾讯云短信应用ID
		TencentSignName: "江苏通卡数字科技有限公司", // 可选：默认签名（req.SignName 为空时兜底）
	}

	client, err := gosms.NewSMSClient(cfg)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	ctx := context.Background()
	req := &gosms.SendRequest{
		PhoneNumbers: []string{"+8613711112222"},
		TemplateID:   "1234567",                    // 需控制台审核通过
		SignName:     "江苏通卡数字科技有限公司",
		TemplateParams: []gosms.TemplateParam{      // 顺序 = 模板 %1%、%2%…
			{Key: "1", Value: "123456"},
		},
	}

	result, err := client.SendSMS(ctx, req)
	if err != nil {
		// 传输层失败（网络/鉴权/入参校验）：err 非 nil
		log.Printf("发送失败: %v", err)
		return
	}
	if result.Status == gosms.StatusFailed {
		// 业务失败（云返回错误码）：err == nil，靠 result.Status（或 result.IsSuccess()）判断
		log.Printf("业务失败: %v", result.Error)
		return
	}
	fmt.Printf("发送成功: message_id=%s\n", result.MessageID)
}
```

**内部行为**：

1. span 启动（`sms.tencent.send`）→ 入口校验（nil / `Validate()`，含 Config 默认签名兜底）→ 失败立即 `failSpan` 并返回，**不发起网络调用**。
2. Debug 级开始日志（手机号脱敏）→ 构建请求：模板参数按**切片顺序**填入 `TemplateParamSet`。
3. `SendSmsWithContext(ctx, ...)` 发送——调用方的取消与超时生效。
4. 响应解析：`SendStatusSet` 为空返回错误（不 panic）；`Code != "Ok"` 记业务失败（span 记 Error，返回 `err == nil` + `Status=failed`）。
5. **一次提交多个号码时**：逐号码状态（各自的 SerialNo/Code）全部保留在 `result.MultipleResults`，旧版只取第一个、其余静默丢弃；任一号码失败则整体 `Status=failed`（聚合错误含失败比例）。
6. 成功才设置 `sms.message_id`/`sms.duration_ms` 属性与 `codes.Ok`，输出 INFO 日志。

**注意**：`SignName` 为空时自动回退 `Config.TencentSignName`，两者都为空时报「签名不能为空」；多号码场景以 `result.MultipleResults` 为准获取每个号码的状态。

---

### 场景二：发送阿里云短信（`SendSMS`）

**适用场景**：单次提交**一个**号码（阿里云 API 限制）。

```go
cfg := &gosms.Config{
	ProviderType:   gosms.ProviderTypeAliyunSMS,
	Region:         "cn-hangzhou",
	AccessKeyID:    "your-access-key-id",
	SecretKey:      "your-access-key-secret",
	AliyunSignName: "江苏通卡数字科技有限公司", // 可选：默认签名（req.SignName 为空时兜底）
}

client, err := gosms.NewSMSClient(cfg)
if err != nil {
	log.Fatalf("创建客户端失败: %v", err)
}

result, err := client.SendSMS(context.Background(), &gosms.SendRequest{
	PhoneNumbers: []string{"+8613711112222"}, // 仅允许 1 个号码
	TemplateID:   "SMS_123456789",
	SignName:     "江苏通卡数字科技有限公司",
	TemplateParams: []gosms.TemplateParam{ // 命名参数，转为 JSON 对象，顺序无关
		{Key: "code", Value: "123456"},
	},
})
```

**内部行为**：与场景一一致，差异点：

1. 传入 **多于 1 个号码** 时直接返回错误并指引使用 `SendBatchSMS`（旧版本静默只发第一个号码，已修复）。
2. 阿里云 SDK 无 ctx 变体，请求超时由内置 `RuntimeOptions` 兜底（连接 5s + 读 10s = 15s 总预算，对齐腾讯云 `ReqTimeout=15`）；**调用方 ctx 取消不会中断已发起的 HTTP 请求**（见「限制与声明」）。

**注意**：模板参数序列化失败、响应体为空均按失败路径返回（非 nil result + span Error）。

---

### 场景三：批量发送（`SendBatchSMS`）

**适用场景**：逐条发送多个请求（阿里云多号码的唯一正确路径；腾讯云也可用）。

```go
reqs := []*gosms.SendRequest{
	{
		PhoneNumbers:   []string{"+8613711112222"},
		TemplateID:     "1234567",
		SignName:       "江苏通卡数字科技有限公司",
		TemplateParams: []gosms.TemplateParam{{Key: "1", Value: "123456"}},
	},
	{
		PhoneNumbers:   []string{"+8613711112223"},
		TemplateID:     "1234567",
		SignName:       "江苏通卡数字科技有限公司",
		TemplateParams: []gosms.TemplateParam{{Key: "1", Value: "654321"}},
	},
}

results, err := client.SendBatchSMS(ctx, reqs)
if err != nil {
	// 聚合错误：含失败条目序号（第 N 条: ...）；单条失败不中断，results 仍完整
	log.Printf("批量存在失败: %v", err)
}
for i, r := range results {
	if !r.IsSuccess() {
		log.Printf("第 %d 条失败: %v", i+1, r.Error)
	}
}
```

**内部行为**：以**至多 10 条并发**（可经 `Config.BatchConcurrency` 覆盖，0/负数回落默认）逐条调用 `SendSMS`（超出上限的条目排队，不会一批打满云侧速率限制）；**输出顺序恒等于 `reqs` 输入顺序**（与串行语义等价，`results[i]` 对应 `reqs[i]`）；单条失败只记 Warn 日志（含序号）并产出失败 result，**不中断批量**；全部条目跑完后聚合失败为外层 `error`（`errors.Join`，含条目序号），全成功为 nil——`results` 无论成败都包含完整的逐条结果，两者可同时使用；**ctx 取消后未执行条目不再发起，逐条回填 failed + `ctx.Err()`**（与 goemail `runBatchEmail` 语义对称）。

**注意**：`reqs` 中的 nil 元素或非法请求会产出失败 result（不 panic），并计入外层聚合 error。

---

### 场景四：查询发送状态（`GetSMSStatus`）

**适用场景**：回查发送记录与运营商回执。

```go
result, err := client.GetSMSStatus(ctx, &gosms.SMSStatusQuery{
	PhoneNumber: "+8613711112222",           // 必填
	MessageID:   "",                          // 可选：发送返回的 BizId/SerialNo
	FromDate:    time.Now().Add(-24 * time.Hour), // 零值 → ToDate 前 24 小时
	ToDate:      time.Now(),                  // 零值 → 当前时间
	Offset:      0,
	Limit:       10,                          // 0 → 默认 10；腾讯截到 100、阿里截到 50
})
if err != nil {
	log.Printf("查询失败: %v", err)
	return
}
for _, s := range result.Data {
	fmt.Printf("手机号 %s 状态 %s 送达时间 %v\n", s.PhoneNumber, s.Status, s.DeliverTime)
}
```

**内部行为**：

1. `normalizeStatusQuery` 归一化（nil/缺号/日期倒置 → 错误；零值日期回退；Limit 默认 10）→ 各云上限截断（腾讯 100 / 阿里 50），全部在发网络前完成。
2. 阿里页码按 `Offset/Limit + 1` 换算（旧版写死 `Offset/10+1`，Limit≠10 时页码错误，已修复）。
3. 阿里响应的 `Code != "OK"` 按错误返回（旧版吞掉后返回「0 条记录 + 成功」，已修复）。
4. 查询到 0 条是**合法结果**（`Data` 为空切片、`err == nil`）。

**注意**：腾讯云仅支持回溯 7 天、阿里云 30 天——超限由云 API 报错并透传（不静默截断查询范围）。

---

### 场景五：手机号工具

```go
// 格式校验（带 span 与 request_id 属性；手机号在日志/span 中脱敏）
if !gosms.ValidatePhoneNumber(ctx, "+8613711112222") {
	log.Println("号码格式非法")
}

// 格式化：国内 11 位自动补 +86，已有 + 前缀恒等
phone := gosms.FormatPhoneNumber("13711112222") // +8613711112222
```

## 参数/结构体说明

### Config

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| ProviderType | ProviderType | 是 | `tencent_sms` / `aliyun_sms`；其他值报「不支持的短信服务商类型」 |
| Region | string | 否 | 腾讯默认 `ap-shanghai`，阿里默认 `cn-hangzhou` |
| AccessKeyID | string | 是 | 腾讯 SecretId / 阿里 AccessKeyId；`String()` 打印时按首4尾4 掩码 |
| SecretKey | string | 是 | 对应 SecretKey。**禁止直接打印 Config**——已实现 `String()`，`%v/%+v/%s` 输出时 SecretKey（首2尾2）与 AccessKeyID（首4尾4）自动脱敏（仍建议不主动输出） |
| TencentAppID | string | 腾讯必填 | 腾讯云短信应用 ID |
| TencentSignName | string | 否 | 腾讯默认签名：`SendRequest.SignName` 为空时兜底 |
| AliyunSignName | string | 否 | 阿里默认签名：`SendRequest.SignName` 为空时兜底 |
| BatchConcurrency | int | 否 | 批量发送并发上限（`SendBatchSMS` 用）；0/负数回落默认 10，正值不封顶 |

### SendRequest

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| PhoneNumbers | []string | 是 | E.164 国际格式（`+` 开头 + 7~15 位数字）；阿里云至多 1 个 |
| TemplateID | string | 是 | 控制台审核通过的模板 ID |
| TemplateParams | []TemplateParam | 否 | **有序**；腾讯=位置参数（切片序对应 %1%），阿里=命名参数 |
| SignName | string | 条件必填 | 为空时回退 Config 对应云的默认签名；兜底后仍为空报错 |
| Tags | map[string]string | 否 | 预留字段，当前未使用 |

### TemplateParam

| 字段 | 腾讯云语义 | 阿里云语义 |
|------|-----------|-----------|
| Key | 仅代码自文档（发送不使用） | JSON 键（对应模板参数名） |
| Value | 按**切片顺序**填入 %1%、%2%… | JSON 值 |

### SMSStatusQuery

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| PhoneNumber | string | 是 | 国际格式；为空报「查询手机号不能为空」 |
| MessageID | string | 否 | 腾讯 SerialNo / 阿里 BizId |
| FromDate / ToDate | time.Time | 否 | 零值回退：ToDate=当前、FromDate=ToDate 前 24h；倒置报错 |
| Offset | uint64 | 否 | 分页偏移；阿里页码 = Offset/Limit + 1 |
| Limit | uint64 | 否 | 0 → 10；腾讯 ≤100、阿里 ≤50 自动截断 |

### SendResult

| 字段 | 类型 | 说明 |
|------|------|------|
| MessageID | string | 腾讯 SerialNo / 阿里 BizId；多号码时为**首个**号码的 ID |
| PhoneNumber | string | 手机号原文（业务数据不脱敏）；多号码时为首个号码 |
| Status | string | `success` / `failed`——**唯一权威判定字段** |
| Error | error | 失败原因；传输层/入参失败时非空，业务失败时也非空 |
| Extra | map[string]interface{} | 各云返回的附加字段（Code/Message/SerialNo/Fee 等） |
| MultipleResults | []*SendResult | 仅腾讯云一次提交多号码时非 nil，按请求顺序逐号码结果；单号码/阿里云恒为 nil。**子结果（本字段中的元素）的本字段恒为 nil**——只展开一层，无递归嵌套 |

判定助手：`result.IsSuccess()`（nil receiver 返回 false，已涵盖「业务失败 `err == nil`」陷阱）。

## API 速查

### NewSMSClient — 按配置创建客户端

```go
func NewSMSClient(cfg *Config) (SMSClient, error)
```

- `cfg == nil` 报「短信配置不能为空」（不 panic）
- 未知 ProviderType 报错；创建过程不发起网络调用
- **出错时返回真 nil 接口**：缺密钥等构造失败路径下 `err != nil` 时 `client` 必为 nil，
  按 `client != nil` 判定安全（typed-nil 防护，与 goemail `NewEmailClient` 同型）

### SMSClient.SendSMS — 发送短信

```go
SendSMS(ctx context.Context, req *SendRequest) (*SendResult, error)
```

- **注意（API 语义）：`err == nil` 不等于发送成功**——云业务失败返回 `err == nil` + `result.Status == failed`，
  请统一以 `result.IsSuccess()`（或 `result.Status == StatusSuccess`）做最终判定，勿只看 error
- 入口校验失败：`err != nil` 且 `result.Status == failed`，**不发网络请求**
- 腾讯多号码：逐号码状态见 `result.MultipleResults`，任一失败则整体 `failed`
- 腾讯透传 ctx；阿里 ctx 不透传（超时兜底）

### SMSClient.SendBatchSMS — 批量发送

```go
SendBatchSMS(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error)
```

- 至多 **10 条并发**（可经 `Config.BatchConcurrency` 覆盖）逐条调用、单条失败**不中断**（`results` 恒为完整逐条结果），**输出顺序恒等于输入顺序**；全部跑完后外层 error 聚合失败（`errors.Join`，含条目序号），全成功为 nil——勿再假设「外层 error 恒为 nil」；**ctx 取消后未执行条目回填 failed + `ctx.Err()`，不再发起新任务**

### SMSClient.GetSMSStatus — 查询发送状态

```go
GetSMSStatus(ctx context.Context, query *SMSStatusQuery) (*SMSStatusResult, error)
```

- 归一化在发网络前完成；0 条记录是合法成功结果
- **空响应是错误不是 0 条**：腾讯 `response`/`response.Response` 为 nil、阿里 `resp.Body` 为 nil
  均返回中文错误 + `StatusFailed`（不伪装成功）——两云语义对称

### ValidatePhoneNumber / FormatPhoneNumber / SendRequest.Validate / SendResult.IsSuccess

```go
func ValidatePhoneNumber(ctx context.Context, phoneNumber string) bool   // 带 span 的格式校验
func FormatPhoneNumber(phoneNumber string) string                        // 国际前缀补全
func (r *SendRequest) Validate() error                                   // 纯校验，中文错误消息
func (r *SendResult) IsSuccess() bool                                    // 结果判定（nil-safe）
func (c Config) String() string                                          // 打印时 SecretKey（首2尾2）与 AccessKeyID（首4尾4）自动脱敏
```

- `Validate()` 无 ctx、不打日志不建 span（内部为纯函数）；由各 provider 入口在签名兜底后调用
- `ValidatePhoneNumber` 结果与 `Validate()` 内部判定一致（同一纯函数）

## 错误处理

| 场景 | 行为 |
|------|------|
| `NewSMSClient(nil)` / 未知 ProviderType | 返回中文错误，不 panic |
| `SendSMS(nil)` / 缺手机号/缺模板/缺签名 | 返回错误 + `Status=failed` 的非 nil result，不发网络 |
| 手机号格式非法（无 + / 含字母 / 位数越界） | 「手机号格式错误（需+开头的国际格式）: 号码」 |
| 阿里云传入多个号码 | 「阿里云单次仅支持一个号码（当前 N 个），多号码请使用 SendBatchSMS」 |
| 腾讯响应 `SendStatusSet` 为空 | 「腾讯短信响应中无发送状态记录」（旧版越界 panic，已修复） |
| 阿里响应体为空 / 模板参数序列化失败 | 统一失败路径（failSpan + 非 nil result） |
| 阿里查询 `Code != OK` | 返回错误（旧版吞成「0 条 + 成功」，已修复） |
| `GetSMSStatus(nil)` / 缺手机号 / 日期倒置 | 归一化阶段返回中文错误，不发网络 |
| 云业务错误码（如 `isv.*` / `ErrMessage`） | `err == nil` + `result.Status=failed` + `result.Error` 非空——用 `IsSuccess()` 统一判定 |
| 腾讯一次提交多号码任一失败 | 主结果 `Status=failed` + 聚合错误（含失败比例），逐号码状态在 `MultipleResults` |
| `SendBatchSMS` 存在失败条目 | 外层 error 为聚合错误（`第 N 条: ...`），`results` 仍为完整逐条结果 |
| 出现 `Status=success` 但 `Error` 非空的矛盾结果 | 防御性计入聚合错误（`第 N 条状态矛盾...`），不静默吞掉 Error |
| 打印 Config（`%v`/`%+v`/`%s`） | `String()` 自动脱敏 SecretKey（首2尾2）与 AccessKeyID（首4尾4），不泄露明文 |
| 网络/SDK 错误 | `err != nil` + `Status=failed` result |

## 限制与声明

1. **阿里云 SDK 不支持 context 透传**（已读源码核实）：dysmsapi v4.1.3 的 `SendSmsWithOptions`/`QuerySendDetailsWithOptions` 均无 ctx 变体（`client/client.go` 中 grep `WithContext` 零结果），tea-utils v2 `RuntimeOptions` 也无 `SetContext`。缓解：内置 `RuntimeOptions` 设置连接 5s + 读 10s 超时（毫秒，`tea@v1.4.0/dara/core.go:326` 将两者相加作为 `http.Client.Timeout`）。**调用方 ctx 取消不会中断已发出的阿里云 HTTP 请求**，超时兜底参数本机未在真实云环境验证生效（未实测声明）。
2. **腾讯云查询回溯上限 7 天**、**阿里云 30 天**（含查询参数 PageSize 上限 50）：超限由云 API 拒绝并透传原始错误，本包不静默修改查询范围。
3. **`Tags` 字段为预留**：结构体保留但当前无任何使用点。
4. **并发安全**：客户端创建后可并发调用（底层 SDK 客户端无请求级可变状态；`RuntimeOptions` 每次请求新建）。
5. **本机未验证项**：真实云发送/查询（付费操作需显式授权）、`-race`（见下节）。

## 前提条件

**腾讯云 SMS**：

1. 控制台创建短信应用，获取 AppID 与 SecretId/Key
2. 申请并通过签名、模板审核
3. 账户余额充足

**阿里云 SMS**：

1. 开通短信服务，获取 AccessKey ID/Secret
2. 申请并通过签名、模板审核
3. 账户余额充足

## 集成测试

集成测试带 `//go:build integration` 构建标签，依赖真实云服务，通过环境变量配置。

**环境变量一览**（与 `integration_test.go` 实际读取的变量一一对应）：

| 变量 | 必填 | 用途 | 默认值 |
|------|------|------|--------|
| `GOSMS_TENCENT_SECRET_ID` | 腾讯组必填 | 腾讯云 SecretId | 无 |
| `GOSMS_TENCENT_SECRET_KEY` | 腾讯组必填 | 腾讯云 SecretKey | 无 |
| `GOSMS_TENCENT_APP_ID` | 腾讯组必填 | 短信应用 ID | 无 |
| `GOSMS_TENCENT_SIGN_NAME` | 腾讯发送必填 | 签名 | 无 |
| `GOSMS_TENCENT_TEMPLATE_ID` | 腾讯发送必填 | 模板 ID | 无 |
| `GOSMS_ALIYUN_ACCESS_KEY_ID` | 阿里组必填 | 阿里云 AccessKeyId | 无 |
| `GOSMS_ALIYUN_ACCESS_KEY_SECRET` | 阿里组必填 | 阿里云 AccessKeySecret | 无 |
| `GOSMS_ALIYUN_SIGN_NAME` | 阿里发送必填 | 签名 | 无 |
| `GOSMS_ALIYUN_TEMPLATE_ID` | 阿里发送必填 | 模板 ID | 无 |
| `GOSMS_TEST_PHONE` | 是 | 测试手机号（国际格式） | 无 |
| `GOSMS_TEMPLATE_PARAM_KEY` | 否 | 模板参数名 | `1` |
| `GOSMS_TEMPLATE_PARAM_VALUE` | 否 | 模板参数值；不设置则不带参数发送 | 无 |
| `GOSMS_ALLOW_PAID_SEND` | 发送用例必需 | 设为 `1` 才执行真实发送（付费授权开关） | 未设置 = Skip |
| `GOSMS_TEST_TIMEOUT` | 否 | `TestMain` 总时长上限（Go duration） | `120s` |

\* 任一该组必需变量缺失时，对应用例自动 Skip。

**运行方式**：

```bash
cd pkg/gosms

# 方式一：显式传环境变量（CI 用法，优先级高于 .env）
GOSMS_TENCENT_SECRET_ID=xxx go test -tags=integration -v -count=1

# 方式二：包内 .env（推荐本地，无需任何插件）
# TestMain 里的 loadDotEnv 会自动读取包内 .env 并补齐环境变量，
# 语义是「只补缺、不覆盖已设置的变量」；根 .gitignore 的 *.env 已覆盖该文件，不会入库。
go test -tags=integration -v -count=1

# 只跑某一组（注意：多个用例名之间不要直接用 |，Git Bash 会把 | 当管道）
go test -tags=integration -count=1 -v -run 'TestIntegration_QueryTencent'

# 仅单元测试（无需任何环境变量，日常使用）
go test ./pkg/gosms/ -v
```

**行为说明**：

- 必需环境变量缺失 → Skip；未授权 `GOSMS_ALLOW_PAID_SEND=1` → Skip（防 CI 误配凭据白花钱）；断言不符（如配了凭据仍失败）→ **Fail** 并保留云原始错误
- `TestMain` 以看门狗限制总时长（默认 120s），防止外部 SDK goroutine 泄漏导致 `go test` 永远挂起
- 等待/超时均已抽为命名常量（`defaultIntegrationTimeout`、`integrationReadyTimeout`），出现超时问题优先调常量，不要改断言

**实测结果汇总**（本机 Windows，`go test -tags=integration -count=1`）：

| 分组 | 结果 | 关键读数 |
|------|------|----------|
| TestIntegration_SendTencent / QueryTencent | SKIP | 本机未配置 `GOSMS_TENCENT_*` 凭据 |
| TestIntegration_SendAliyun / QueryAliyun | SKIP | 本机未配置 `GOSMS_ALIYUN_*` 凭据 |

> 本机未在真实云环境跑通集成用例（未实测声明）；凭据配置方式见 `test/README.md`（手动发送验证程序）。

## 性能基线与模糊测试

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 ./pkg/gosms/
```

> 诚实声明：本 README 不声称已经跑过 `-race`——本机 Windows/MinGW 环境执行报 `0xc0000139`（同仓 gozip 已验证的环境限制），真实证据需在 Linux/CI 采集。

### 基准测试

```bash
go test ./pkg/gosms/ -bench . -benchtime=100000x -run '^$'
```

| 基准 | 场景 | ns/op | B/op | allocs/op | benchtime |
|------|------|-------|------|-----------|-----------|
| BenchmarkValidatePhoneNumber | E.164 格式校验（纯函数） | 8.039 | 0 | 0 | 100000x |
| BenchmarkFormatPhoneNumber | 国际前缀补全（纯函数） | 21.39 | 0 | 0 | 100000x |
| BenchmarkMaskPhone | 手机号脱敏（纯函数） | 70.43 | 24 | 2 | 100000x |
| BenchmarkParseSendSmsStatus | 腾讯发送响应解析（多号码路径） | 990.4 | 1848 | 19 | 100000x |
| BenchmarkParsePullSendStatus | 腾讯下发状态列表解析 | 652.7 | 1464 | 13 | 100000x |
| BenchmarkParseSendDetailItems | 阿里发送明细 DTO 解析 | 748.6 | 552 | 7 | 100000x |
| BenchmarkBatchErrors | 批量失败聚合（errors.Join） | 913.1 | 456 | 13 | 100000x |

**口径与解读：**

- 七条基准均为无网络 RTT 的纯函数（入口校验/脱敏 + 响应解析/批量聚合两条热路径），量的是 gosms 自身的处理开销；实测环境：本机 Windows / 11th Gen Intel i5-1135G7
- 上表为 `-benchtime=100000x` 正式基线的单轮读数（与上方命令一致）。`ns/op` 受机器状态影响跨轮波动，**跨轮次对比以 allocs/op 与 B/op 为稳定指标**（本轮 7 条读数两者与历史完全一致）；10x 快速回归波动更大（同机曾读到 40~290 ns）
- 稳定指标（allocs/op）：校验与格式化 0 分配，脱敏 2 分配（`strings.Repeat` 产生）；解析与聚合类基准的分配来自逐条构造结果/错误对象（parse* 每条 1 个结果 + Extra map，batchErrors 每条 1 个包装错误）

### 模糊测试

```bash
go test ./pkg/gosms/ -fuzz FuzzValidatePhoneNumber -fuzztime=10s -run '^$'
go test ./pkg/gosms/ -fuzz FuzzFormatPhoneNumber -fuzztime=10s -run '^$'
```

| Target | 不变量 |
|--------|--------|
| FuzzValidatePhoneNumber | 校验不 panic；校验通过 ⇒ 以 `+` 开头且格式化为恒等 |
| FuzzFormatPhoneNumber | 格式化不 panic；输出恒以 `+` 开头且幂等（`Format(Format(x)) == Format(x)`） |

**实测**（本机 Windows，`-fuzztime=10s`，最终代码状态）：FuzzValidatePhoneNumber 197,848 execs PASS；FuzzFormatPhoneNumber 3,580 execs PASS。种子语料随常规 `go test` 执行。execs 受本地语料库增长与机器状态影响跨轮次波动（同口径另测得 95,832 / 2,173、109,100 / 2,174、112,098 / 2,252，早期轮次 92,797 / 96,894），判定标准是 PASS 本身而非 execs 高低。

## 许可证

MIT License
