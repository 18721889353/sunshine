# goemail

Go 语言多平台邮件发送库，以统一接口封装腾讯云 SES、阿里云 DM 与 SMTP 的发送、批量发送与状态查询。

## 架构概览

```
pkg/goemail/
├── client.go           # 公共类型、客户端工厂、校验/脱敏纯函数、批量并发骨架与错误聚合、request_id 注入
├── tencent_ses.go      # 腾讯云 SES 实现：模板发送、状态查询、状态码映射与 nil 安全解析
├── aliyun_dm.go        # 阿里云 DM 实现：单发/模板发送、基于 TagName 的发送记录查询
├── smtp.go             # SMTP 实现：TLS 语义、附件临时目录、自定义邮件头
├── *_test.go           # 单测/基准/模糊测试（与源文件一对一，另有 test_helpers_test.go）
├── integration_test.go # 真实云服务集成测试（-tags=integration）
├── benchmark_test.go   # 无网络 RTT 的性能回归基线
├── fuzz_test.go        # 校验/脱敏/失败路径/批量保序的不变量守护
├── env.example         # 集成测试环境变量模板（与 integration_test.go 读取集合一致）
├── README.md           # 本文件
└── CHANGELOG.md        # 变更记录（R1~R4 修复条目与守护测试、破坏性变更迁移方式；
                        #  R<轮次>-<序号> 为改造评审轮次编号，如 R4-1 = 第 4 轮评审第 1 项）
```

**核心设计原则**：

1. **入口统一 fail loud**：请求校验、地址格式校验全部在发起网络调用前完成；`cfg`/`req`/`query` 为 nil 时
   同样在入口直接返回中文错误（不 panic）；任何失败路径都返回非 nil 的
   `*SendResult`（`Status=StatusFailed` 且 `Error` 非空），不留下悬空 span，也不返回半截成功数据（`MessageID`/`Extra` 为空）。
2. **批量不中断、结果保序**：`SendBatchEmail` 复用同一并发骨架 `runBatchEmail`（并发上限默认 10，
   `Config.BatchConcurrency` 可覆盖），结果按输入下标回填（输出顺序恒等于请求顺序），单条失败不中断批量，
   全部跑完后返回带「第 N 条」序号的聚合 error；`ctx` 取消后未执行条目回填 failed、不再发起新任务。
3. **`err == nil` 不等于成功**：云业务失败的典型形态是 `err == nil` + `Status=failed`，判定统一走
   `SendResult.IsSuccess()`（nil-safe）。
4. **PII 与密钥边界明确**：邮箱仅在业务结构体（`SendRequest`/`EmailStatus`）保留原文；日志与 span 属性一律经
   `maskEmail` 脱敏；`Config` 打印经 `Config.String()` 掩码（`maskSecret` 首2尾2、`maskAccessKey` 首4尾4）；
   错误消息里的外部可控值走 `%q` 转义，既不含裸换行也不泄露整段原文。
5. **构造函数零副作用**：`new*Client` 只在本地解析默认值（SMTP 端口 465、腾讯 `ap-hongkong`、阿里 `cn-hangzhou`），
   不回写共享的 `*Config`——配置对象常被多方共享，回写会产生难以追踪的串扰。
6. **SDK 差异如实声明**：腾讯云 `SendEmail`/阿里云 `SingleSendMail` 只实现基础字段，`Cc`/`Bcc`/`ReplyTo`/
   `Headers`/`Attachments`/`Tags` 会被忽略（不报错、发送照常，但请求带了这些字段会打 Warn 告警，
   见下方字段支持矩阵）；需要这些能力只能走 SMTP。

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| 多平台邮件发送/批量/状态查询（统一接口） | 是 | 本包 |
| 腾讯/阿里模板的申请与审核、域名验证 | 否 | 云服务商控制台 |
| 最终投递保证（对方邮局是否收下） | 否 | 仅查询并回传云侧回执 |
| 退订链接、反垃圾内容审核 | 否 | 业务侧与云服务商 |
| 收件人列表、模板变量列的准备（阿里 BatchSendMail） | 否 | 阿里云控制台预先创建 |
| 余额、套餐、发送配额管理 | 否 | 云服务商控制台 |

复核命令（应无结果，本包不提供重试/队列/退订 API）：

```bash
grep -rn "func.*Retry\|func.*Queue\|func.*Unsubscribe" pkg/goemail/
```

### 字段支持矩阵（如实标注，勿按直觉假设）

`SendRequest` 是三家共用的结构体，但**只有 SMTP 实现了全部字段**：

| 字段 | SMTP | 腾讯云 SES | 阿里云 DM | 说明 |
|------|------|-----------|-----------|------|
| `From` / `To` / `Subject` | 支持 | 支持 | 支持 | 三家均校验地址格式 |
| `HTMLBody` / `TextBody` | 支持（HTML 为主、纯文本为 alternative） | 支持（两字段独立分设，均 base64 编码） | 支持（两字段独立分设） | 至少填一个 |
| `Cc` / `Bcc` | 支持 | **忽略** | **忽略** | 云 API 未传该字段，丢弃时打 Warn 告警 |
| `ReplyTo` | 支持 | **忽略** | **忽略** | 阿里写死 `ReplyToAddress=true`，不读本字段 |
| `Headers` | 支持 | **忽略** | **忽略** | 自定义邮件头仅 SMTP 生效 |
| `Attachments` | 支持 | **忽略** | **忽略** | 附件会写入独立临时目录并在发送后清理 |
| `Tags` | **忽略** | **忽略** | **忽略** | 当前三家实现均未消费该字段，不要用它做追踪 |
| 单次收件人数 | 无硬限制 | 无硬限制 | **≤ 100** | 超限直接返回校验错误 |

> **注意**：上表「忽略」的字段不会报错——发送照常成功，但该字段不生效。请求带上被忽略的字段时，
> 对应 provider 会打一条 `当前 provider 忽略部分字段` 的 Warn 日志（带 provider 与各字段计数）作为主动告警；
> 日志告警依赖调用方接入了 logger，**以上表为最终依据**。需要 `Cc`/`Bcc`/`ReplyTo`/附件时，请改用 SMTP provider。

## 使用场景选择

### 场景一：创建客户端并发送普通邮件（`NewEmailClient` + `SendEmail`）

**适用场景**：绝大多数业务——验证码、通知、订单消息的单封发送，平台按 `Config.ProviderType` 切换。

```go
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
	// 云 provider：填 AK/SK；SMTP provider：填服务器与授权码（密钥建议读环境变量）
	cfg := &goemail.Config{
		ProviderType: goemail.ProviderTypeTencentSES,
		Region:       "ap-guangzhou", // 空则本地默认 ap-hongkong（不回写 cfg）
		AccessKeyID:  os.Getenv("TENCENT_AK"),
		SecretKey:    os.Getenv("TENCENT_SK"),
	}

	client, err := goemail.NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := client.SendEmail(ctx, &goemail.SendRequest{
		From:     "noreply@example.com",
		To:       []string{"user@example.com"},
		Subject:  "欢迎注册",
		HTMLBody: "<h1>欢迎加入</h1><p>感谢您的注册！</p>",
		TextBody: "欢迎加入\n感谢您的注册！",
	})
	if err != nil {
		log.Printf("发送失败: %v", err)
		return
	}
	// err == nil 不等于成功，统一用 IsSuccess 判定
	if !result.IsSuccess() {
		log.Printf("云侧业务失败: status=%s error=%v", result.Status, result.Error)
		return
	}
	log.Printf("发送成功: message_id=%s", result.MessageID)
}
```

**内部行为**：

1. `NewEmailClient` 按 `ProviderType` 分派到 `newTencentSESClient`/`newAliyunDMClient`/`newSMTPClient`；
   出错时返回**真正的 nil 接口**（不是「非 nil 接口包 nil 指针」的 typed-nil），`if client != nil` 可直接判定。
2. `SendEmail` 先建 span（写入 provider/region/from 掩码/收件人数/request_id），再跑 `validateRequest`；
   校验不过立刻返回 `Status=StatusFailed` + 非 nil `Error`，**不产生任何网络调用**。
3. 校验通过才构造 SDK 请求并发起调用；云返回错误时返回的 `error` 是原始云错误的 `%w` 包装，
   同时 `result.Status=StatusFailed`。
4. 成功路径返回 `Status=StatusSuccess` 与 `MessageID`（阿里云为 `EnvId`，见 `Extra["env_id"]`）。

**注意**：常用 SMTP 服务器参数如下，`UseTLS` 按端口选择（465 置 `true`，25/587 置 `false`）：

| 服务商 | SMTPHost | 端口 | 凭据 |
|--------|----------|------|------|
| QQ 邮箱 | `smtp.qq.com` | 465 | 授权码（非登录密码） |
| 163 邮箱 | `smtp.163.com` | 465 | 授权码 |
| Gmail | `smtp.gmail.com` | 587 | 应用专用密码 |
| 企业微信邮箱 | `smtp.exmail.qq.com` | 465 | 账号密码 |

---

### 场景二：腾讯云模板邮件与验证码（`TencentSESClient.SendTemplateEmail`）

**适用场景**：验证码、审批通知等需要走云侧审核模板的场景；模板变量随请求以 JSON 传递。

```go
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
	cfg := &goemail.Config{
		ProviderType: goemail.ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  os.Getenv("TENCENT_AK"),
		SecretKey:    os.Getenv("TENCENT_SK"),
	}
	client, err := goemail.NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	// 模板方法在具体类型上，需先类型断言（断言失败即配置用错了 provider）
	tencentClient, ok := client.(*goemail.TencentSESClient)
	if !ok {
		log.Fatalf("当前配置不是 tencent_ses: %T", client)
	}

	email := "user@example.com"
	code := "123456" // 实际应随机生成并落库/落 Redis 带过期
	templateData := map[string]interface{}{
		"username": "用户",
		"code":     code,
		"expiry":   "10分钟",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := tencentClient.SendTemplateEmail(
		ctx,
		"noreply@example.com",        // 发件人（须为已验证域名）
		[]string{email},              // 收件人
		12345,                        // 模板 ID（腾讯云控制台创建）
		templateData,                 // 模板变量，序列化为 JSON 传给 TemplateData
		"验证码",                       // 主题
	)
	if err != nil {
		log.Fatalf("发送验证码失败: %v", err)
	}
	if !result.IsSuccess() {
		log.Fatalf("云侧业务失败: status=%s error=%v", result.Status, result.Error)
	}
	log.Printf("验证码已发送: message_id=%s request_id=%v",
		result.MessageID, result.Extra["request_id"])
}
```

**内部行为**：

1. 入口校验顺序：发件人非空且格式合法 → 收件人非空且全部合法 → 模板 ID 非 0 → 主题非空。
2. `templateData` 经 `json.Marshal` 塞进 `Template.TemplateData`；传 `nil` 时序列化为 `null`（无变量模板可用）。
3. 响应的 `MessageId`/`RequestId` 全部 nil 安全取值，任意字段缺失都只影响取值、不会 panic。

**注意**：模板必须先在腾讯云控制台创建并通过审核，发件域名也必须已验证，否则云侧返回
`模板ID无效或者不可用` / `无权限使用该模板` 等状态码（见「错误处理」）。

---

### 场景三：阿里云模板邮件（`AliyunDMClient.SendTemplateEmail`）

**适用场景**：阿里云 DM 的批量模板投递。**变量不在请求里**——需在控制台预先创建收件人列表并上传带列的 CSV/TXT。

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
	cfg := &goemail.Config{
		ProviderType: goemail.ProviderTypeAliyunDM,
		Region:       "cn-hangzhou", // 空则本地默认 cn-hangzhou（不回写 cfg）
		AccessKeyID:  os.Getenv("ALIYUN_AK"),
		SecretKey:    os.Getenv("ALIYUN_SK"),
	}
	client, err := goemail.NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	aliyunClient, ok := client.(*goemail.AliyunDMClient)
	if !ok {
		log.Fatalf("当前配置不是 aliyun_dm: %T", client)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// receiversName：预先创建并上传了收件人的「收件人列表名称」，不是内联地址列表
	// templateName：预先创建并通过审核的模板名称
	result, err := aliyunClient.SendTemplateEmail(
		ctx,
		"noreply@example.com", // 发信地址
		"my_receivers",        // 收件人列表名称
		"verification_code",   // 模板名称
	)
	if err != nil {
		log.Fatalf("发送模板邮件失败: %v", err)
	}
	if !result.IsSuccess() {
		log.Fatalf("云侧业务失败: status=%s error=%v", result.Status, result.Error)
	}
	log.Printf("模板邮件已提交: env_id=%v template=%v",
		result.Extra["env_id"], result.Extra["template_name"])
}
```

**内部行为**：

1. 入口校验：`from` 为空或格式非法、`receiversName`/`templateName` 为空即返回 `Status=StatusFailed` 的结果与中文错误
   （`from` 格式与 `SendEmail` 同口径本地拦截；后两个是云侧预建标识符，不做格式校验）。
2. 构造 `BatchSendMailRequest{AccountName, AddressType=1, TemplateName, ReceiversName}` 后调用云 API；
   **不携带模板变量、不携带 TagName**——变量来自收件人列表的列，标签需在控制台用 `CreateTag` 预先创建。
3. 成功时 `MessageID` 为空（该 API 不返回消息 ID），真正的标识在 `Extra["env_id"]`。

**不适用**：需要「按单个收件人实时组装变量」的场景——阿里 BatchSendMail 做不到，改用场景二（腾讯）或场景五（SMTP 自拼正文）。

---

### 场景四：批量发送（`SendBatchEmail`）

**适用场景**：一次提交多封不同内容的邮件（通知批、营销批）；三家 provider 共用同一并发骨架。

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
	client, err := goemail.NewEmailClient(&goemail.Config{
		ProviderType: goemail.ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  os.Getenv("TENCENT_AK"),
		SecretKey:    os.Getenv("TENCENT_SK"),
	})
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	reqs := []*goemail.SendRequest{
		{From: "noreply@example.com", To: []string{"user1@example.com"},
			Subject: "通知1", HTMLBody: "<p>内容1</p>"},
		{From: "noreply@example.com", To: []string{"user2@example.com"},
			Subject: "通知2", HTMLBody: "<p>内容2</p>"},
		{From: "noreply@example.com", To: []string{"user3@example.com"},
			Subject: "通知3", HTMLBody: "<p>内容3</p>"},
	}

	ctx := context.Background()
	results, err := client.SendBatchEmail(ctx, reqs)
	// 聚合 error 在全部条目跑完后才返回，results 始终是完整的逐条结果，两者可同时使用
	if err != nil {
		log.Printf("批量聚合错误（含逐条序号）: %v", err)
	}
	for i, result := range results {
		if result.IsSuccess() {
			log.Printf("第 %d 封成功: %s", i+1, result.MessageID)
		} else {
			log.Printf("第 %d 封失败: %v", i+1, result.Error)
		}
	}
}
```

**内部行为**：

1. 任务队列一次性灌满并关闭，worker 数取 `min(并发上限, len(reqs))`——上限由 `Config.BatchConcurrency` 解析，
   `0`/负值用默认 10，正值不封顶（按自家 provider 速率限制自定）。
2. `results` 按输入下标写入，输出顺序恒等于请求顺序——聚合里的「第 N 条」永远指向你提交的第 N 条。
3. 单条失败只记录 Warn 日志并回填 `failed` 结果，不中断其它条目。
4. `ctx` 取消后 worker **不再发起新任务**：未执行条目回填 `failed` +「批量任务因上下文取消未执行」，
   `results` 仍完整保序；已进入云调用的条目按各自返回收尾（首个取消打一条 Warn）。
5. 全部跑完后 `batchErrors` 用 `errors.Join` 聚合所有 `Status != success` 的条目；全成功返回 `nil`。

**注意**：批量的并发上限是**单批默认 10**（可配），不是全局限流；连续多批提交时瞬时并发仍可能叠加，云侧频率限制需自行在外层控制速率。

---

### 场景五：SMTP 附件与自定义邮件头（`SMTPClient.SendEmail`）

**适用场景**：需要抄送/密送/回复地址/附件/自定义邮件头的邮件——这些能力只有 SMTP 实现（见字段支持矩阵）。

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
	client, err := goemail.NewEmailClient(&goemail.Config{
		ProviderType: goemail.ProviderTypeSMTP,
		SMTPHost:     "smtp.qq.com",
		SMTPPort:     465,
		SMTPUsername: os.Getenv("SMTP_USERNAME"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		UseTLS:       true, // 465 置 true（隐式 SSL）；25/587 置 false（STARTTLS 自适应）
	})
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	report, err := os.ReadFile("report.pdf")
	if err != nil {
		log.Fatalf("读取附件失败: %v", err)
	}

	ctx := context.Background()
	result, err := client.SendEmail(ctx, &goemail.SendRequest{
		From:     "your_email@qq.com",
		To:       []string{"recipient@example.com"},
		Cc:       []string{"cc@example.com"},
		Bcc:      []string{"bcc@example.com"},
		ReplyTo:  []string{"support@example.com"},
		Subject:  "月度报告",
		HTMLBody: "<p>请查收附件中的月度报告</p>",
		TextBody: "请查收附件",
		Headers:  map[string]string{"X-Priority": "1"},
		Attachments: []*goemail.Attachment{
			{Filename: "report.pdf", Content: report, ContentType: "application/pdf"},
		},
	})
	if err != nil {
		log.Printf("发送失败: %v", err)
		return
	}
	if !result.IsSuccess() {
		log.Printf("发送失败: status=%s error=%v", result.Status, result.Error)
		return
	}
	log.Printf("发送成功: %s", result.MessageID)
}
```

**内部行为**：

1. 校验顺序：发件人 → 全部收件人 → 抄送 → 密送 → 主题 → 正文至少一个，错误消息均为中文且外部值用 `%q` 转义。
2. 附件内容写入**独立临时目录**（`os.MkdirTemp`），文件名取 `filepath.Base` 并拒绝 `.`/`..`（防路径穿越），
   `defer` 整体清理——并发发送同名附件互不覆盖，失败也不会残留用户附件内容。
3. 拨号器按 `UseTLS` 选择传输：`true` 强制隐式 SSL（465）；`false` 走 gomail 默认——465 仍隐式 SSL，
   25/587 用 STARTTLS 升级。端口为 0 时本地默认 465（**传给拨号器的也是默认值**，不会把 0 拨出去）。

**注意**：`ContentType` 当前仅作为结构体字段保留，gomail 按扩展名推断 MIME 类型。

---

### 场景六：错误判定与超时控制（`IsSuccess` + `context.WithTimeout`）

**适用场景**：所有场景的收尾姿势——统一判定结果、用 ctx 控制单次调用超时。

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/goemail"
)

func send(client goemail.EmailClient, req *goemail.SendRequest) {
	// 超时用时长常量：30 * time.Second（注意不是 30——30 是 30 纳秒）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.SendEmail(ctx, req)
	if err != nil {
		// 参数校验失败、网络失败、云返回错误码都会走到这里
		log.Printf("发送失败: %v", err)
		if result != nil && result.Status == goemail.StatusFailed {
			log.Printf("失败详情: %v", result.Error)
		}
		return
	}
	// 关键：err == nil 不等于成功——云业务失败是 err == nil + Status=failed
	if !result.IsSuccess() {
		log.Printf("业务失败: status=%s", result.Status)
		return
	}
	log.Printf("发送成功: %s", result.MessageID)
}
```

**内部行为**：

1. `err != nil`：入口校验失败 / 网络或 SDK 调用失败 / 云返回业务错误码——`result` 仍非 nil（`Status=failed`）。
2. `err == nil` 且 `result.IsSuccess()`：真正成功。
3. `err == nil` 但 `result.Status == StatusFailed`：云侧受理成功、业务失败（当前实现较少出现，仍需按统一姿势判定）。

**注意**：`context` 超时只能取消本包发起的调用；阿里云 DM SDK **没有 ctx 变体**，超时依赖 HTTP 层与 ctx 检查，
极端网络挂起时可能不立即返回——需要硬超时请在外层加超时保护（SMTP 拨号发送同样不直接响应 ctx，
不能作为超时兜底方案）。

---

### 场景七：查询发送状态（`GetEmailStatus` / `GetTrackList`）

**适用场景**：事后核对投递结果。三家能力差异很大，先看清楚再接：

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/goemail"
)

func query(client goemail.EmailClient, toAddress string) {
	ctx := context.Background()
	result, err := client.GetEmailStatus(ctx, &goemail.EmailStatusQuery{
		ToAddress: toAddress,           // 可选：按收件人过滤
		MessageID: "",                  // 可选：按消息 ID 过滤
		FromDate:  time.Now().Add(-24 * time.Hour),
		ToDate:    time.Now(),
		Offset:    0,                   // 必填（腾讯 API 要求显式偏移量）
		Limit:     10,
	})
	if err != nil {
		log.Printf("查询失败: %v", err)
		return
	}
	if result.Status != goemail.StatusSuccess {
		log.Printf("查询状态异常: %v", result.Error)
		return
	}
	for _, item := range result.Data {
		log.Printf("message=%s to=%s status=%s(%s) opened=%t",
			item.MessageID, item.ToAddress, item.Status, item.StatusMessage, item.UserOpened)
	}
	// 提示型查询会把说明放在 Extra["note"]（SMTP/阿里云）
	if note, ok := result.Extra["note"].(string); ok {
		log.Printf("提示: %s", note)
	}
}
```

**内部行为与能力差异**：

| provider | 行为 |
|----------|------|
| 腾讯云 SES | 真实查询 `GetSendEmailStatus`；`RequestDate` 由 `FromDate` 解析（零值取当天），**不突变入参**；返回 `Data` 列表 |
| 阿里云 DM | 无单封查询 API：返回 `Status=success` + `Extra["note"]` 指引改用 `GetTrackListByMailFromAndTagName` |
| 阿里云 `GetTrackList(accountName, tagName, start, end)` | 查询发送记录统计（`Extra` 里是 total/分页等统计量，不是逐封状态） |
| SMTP | 协议不支持：返回 `Status=success` + `Extra["note"]` 说明「SMTP协议不支持状态查询」 |

**状态值**：`accepted`（已受理）/ `pending`（排队或投递中）/ `delivered`（投递成功）/ `failed`（失败）/
`unknown`（状态码缺失或未映射）；用户行为字段 `UserOpened`/`UserClicked`/`UserUnsubscribed`/`UserComplained` 仅腾讯返回。

## 参数/结构体说明

### Config

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `ProviderType` | `ProviderType` | **是** | `tencent_ses` / `aliyun_dm` / `smtp`，其他值返回错误 |
| `Region` | `string` | 否 | 云区域；空则本地默认腾讯 `ap-hongkong`、阿里 `cn-hangzhou`（不回写本字段） |
| `AccessKeyID` | `string` | 云必填 | 腾讯 `SecretId` / 阿里 `AccessKeyId`；SMTP 忽略 |
| `SecretKey` | `string` | 云必填 | 腾讯 `SecretKey` / 阿里 `AccessKeySecret`；SMTP 忽略 |
| `SMTPHost` | `string` | smtp 必填 | 为空直接返回错误 |
| `SMTPPort` | `int` | 否 | `0` 时本地默认 `465`（默认值会真正传给拨号器，不回写本字段） |
| `SMTPUsername` | `string` | smtp 必填 | 一般为完整邮箱地址 |
| `SMTPPassword` | `string` | smtp 必填 | 邮箱授权码（非登录密码） |
| `UseTLS` | `bool` | 否 | `true` 强制隐式 SSL（465）；`false` 按端口自适应（465 SSL、25/587 STARTTLS） |
| `BatchConcurrency` | `int` | 否 | 批量发送并发上限；`0`/负值默认 `10`，正值不封顶（按 provider 速率限制自定） |

> `Config` 实现了值接收者的 `String()`：打印时 `AccessKeyID`/`SecretKey`/`SMTPPassword` 自动掩码。
> 但 `fmt` 的 `%#v` 与按字段反射的序列化（如 zap JSON）**不走 Stringer**，含密钥的 `Config` 不要交给这两条路径。

### SendRequest

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `From` | `string` | **是** | 发件人地址，格式必须通过校验 |
| `To` | `[]string` | **是** | 收件人列表，至少 1 个且全部合法；阿里云另限 ≤100 |
| `Cc` / `Bcc` | `[]string` | 否 | 仅 SMTP 生效，云 provider 忽略 |
| `Subject` | `string` | **是** | 邮件主题，不能为空 |
| `HTMLBody` | `string` | 二选一 | HTML 正文，与 `TextBody` 至少填一个 |
| `TextBody` | `string` | 二选一 | 纯文本正文 |
| `ReplyTo` | `[]string` | 否 | 仅 SMTP 生效，且格式非法时本地拦截（校验与 `Cc`/`Bcc` 同口径） |
| `Attachments` | `[]*Attachment` | 否 | 仅 SMTP 生效 |
| `Tags` | `map[string]string` | 否 | **当前三家均不消费**，勿用于追踪 |
| `Headers` | `map[string]string` | 否 | 仅 SMTP 生效的自定义邮件头 |

### Attachment

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `Filename` | `string` | **是** | 只取文件名主体（`filepath.Base`），`.`/`..`/空名直接报错 |
| `Content` | `[]byte` | **是** | 文件内容，写入独立临时目录后交 gomail 附加，发送后清理 |
| `ContentType` | `string` | 否 | 保留字段；gomail 按扩展名推断 MIME |

### SendResult

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `MessageID` | `string` | — | 腾讯为 `MessageId`；阿里为 `EnvId`（`Extra["env_id"]`）；阿里模板发送为空 |
| `Status` | `string` | — | `StatusSuccess` / `StatusFailed`；**判定成功请用 `IsSuccess()`** |
| `Error` | `error` | — | 失败原因；成功路径为 `nil` |
| `Extra` | `map[string]interface{}` | — | 云侧附加信息（`request_id`、`timestamp`、`note` 等） |

### EmailStatusQuery / EmailStatus / EmailStatusResult

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `EmailStatusQuery.MessageID` | `string` | 否 | 按消息 ID 过滤 |
| `EmailStatusQuery.ToAddress` | `string` | 否 | 按收件人过滤（日志中脱敏展示） |
| `EmailStatusQuery.FromDate` | `time.Time` | 否 | 查询起始；零值取当前时间，**不修改入参** |
| `EmailStatusQuery.ToDate` | `time.Time` | 否 | 查询结束（腾讯按 `FromDate` 单日语义查询） |
| `EmailStatusQuery.Offset` / `Limit` | `uint64` | 否 | 腾讯要求显式 `Offset`；`Limit=0` 时默认 10 |
| `EmailStatus.Status` | `string` | — | `accepted`/`pending`/`delivered`/`failed`/`unknown` |
| `EmailStatus.UserOpened` 等 | `bool` | — | 用户行为，仅腾讯返回 |
| `EmailStatusResult.Status` | `string` | — | 查询本身的成功/失败（与邮件状态不同） |
| `EmailStatusResult.Data` | `[]*EmailStatus` | — | 结果列表；提示型查询为 `nil`，说明在 `Extra["note"]` |

## API 速查

### NewEmailClient — 按配置创建客户端

```go
func NewEmailClient(cfg *Config) (EmailClient, error)
```

- 按 `cfg.ProviderType` 分派到三个具体客户端之一，返回 `EmailClient` 接口
- `ProviderType` 不支持、云密钥缺失、SMTP 服务器/账号缺失均返回中文错误
- **注意**：出错时返回的是**真正的 nil 接口**，`client != nil` 可靠；具体类型的方法（如 `SendTemplateEmail`）
  需先类型断言

### EmailClient.SendEmail — 发送单封邮件

```go
SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error)
```

- 先校验后联网：校验失败返回 `Status=StatusFailed` + 非 nil `Error`，无任何网络调用
- 失败时 `result` 恒非 nil；成功时 `Status=StatusSuccess`
- **注意**：`err == nil` 不代表成功，用 `result.IsSuccess()` 判定

### EmailClient.SendBatchEmail — 批量发送

```go
SendBatchEmail(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error)
```

- 三家共用 `runBatchEmail` 骨架：并发上限默认 10（`Config.BatchConcurrency` 可配）、结果保序、单条失败不中断
- 返回值 `(results, err)` 可同时使用：`results` 完整逐条，`err` 是跑完后聚合的 `errors.Join`
- **注意**：`len(results) == len(reqs)` 恒成立（含 ctx 取消回填）；聚合错误文本含「第 N 条」序号（输入顺序）

### EmailClient.GetEmailStatus — 查询发送状态

```go
GetEmailStatus(ctx context.Context, query *EmailStatusQuery) (*EmailStatusResult, error)
```

- 腾讯返回真实数据；阿里与 SMTP 返回 `Status=success` + `Extra["note"]` 提示（不报错）
- **注意**：查询前请先看「场景七」的能力差异表，避免把提示型返回误判成「查到 0 封」

### TencentSESClient.SendTemplateEmail — 腾讯模板发送

```go
func (c *TencentSESClient) SendTemplateEmail(ctx context.Context, from string, to []string,
    templateID uint64, templateData map[string]interface{}, subject string) (*SendResult, error)
```

- `templateData` 序列化为 JSON 传给云侧；`templateID` 为 0 直接拒绝
- `from`/`to` 地址格式本地校验，与 `SendEmail` 同口径（非法地址不发云 API）
- **注意**：方法在具体类型上，需 `client.(*goemail.TencentSESClient)` 断言

### AliyunDMClient.SendTemplateEmail — 阿里模板发送

```go
func (c *AliyunDMClient) SendTemplateEmail(ctx context.Context, from, receiversName, templateName string) (*SendResult, error)
```

- `receiversName` 是**预先创建的收件人列表名称**（不是地址列表）；`templateName` 是模板名称
- `from` 地址格式本地校验，与 `SendEmail` 同口径（非法地址不发云 API）
- 模板变量由收件人列表的列提供，请求不携带；标签需控制台预建，本方法不传 `TagName`
- **注意**：签名在本轮按官方 API 文档修正过（原实现把 JSON 塞进 `TagName`，语义错误），迁移方式见 CHANGELOG

### AliyunDMClient.GetTrackList — 阿里发送记录统计

```go
func (c *AliyunDMClient) GetTrackList(ctx context.Context, accountName, tagName string,
    startTime, endTime time.Time) (*EmailStatusResult, error)
```

- 返回的是统计量（`total`/分页），不是逐封状态
- **注意**：`tagName` 必须是控制台已创建的邮件标签

### ValidateEmail / ValidateEmails — 邮箱格式校验

```go
func ValidateEmail(ctx context.Context, email string) bool
func ValidateEmails(ctx context.Context, emails []string) []string
```

- 规则：非空、含 `@` 且不在首尾、本地部分与域名均非空、域名含点（纯函数核心为 `isValidEmail`）
- `ValidateEmails` 返回**不合法**的地址列表（合法的不返回）
- **注意**：两者每次调用都会写一条 Info 日志并创建 span（可观测路径），高频批量校验建议直接用返回值组织调用

### SendResult.IsSuccess — 统一成功判定

```go
func (r *SendResult) IsSuccess() bool
```

- `nil` receiver 返回 `false`，可直接对可能为 nil 的 result 调用
- **注意**：`err == nil ≠ 成功`；云业务失败是 `err == nil` + `Status=failed`

### Config.String — 密钥脱敏打印

```go
func (c Config) String() string
```

- `%v`/`%+v`/`%s`/`%q` 均输出掩码后的密钥（`maskSecret` 首2尾2、`maskAccessKey` 首4尾4）
- **注意**：`fmt` 的 `%#v` 与按字段反射的序列化不走 Stringer，含密钥结构体不要交给这两条路径

## 错误处理

| 场景 | 行为 |
|------|------|
| `NewEmailClient` 的 `ProviderType` 不支持 | 返回错误 `不支持的邮件服务商类型: %q`，客户端为 nil |
| 腾讯/阿里缺 `AccessKeyID` 或 `SecretKey` | 返回错误（消息含服务商名），客户端为 nil |
| SMTP 缺 `SMTPHost` / 账号 / 密码 | 返回错误「SMTP 服务器地址不能为空」「SMTP 用户名与密码不能为空」 |
| `SendEmail`/`SendTemplateEmail` 参数非法（地址格式、主题、正文为空、模板参数缺失、阿里 >100 收件人） | 返回 `error` + `Status=failed` 的 result，**不联网** |
| 云 API 调用失败（网络/鉴权/配额） | 返回云错误的 `%w` 包装 + `Status=failed` 的 result |
| 腾讯状态码映射（1006 频控、1007 黑名单、3007 模板无效…） | `Status=failed`，具体码在 `EmailStatus.StatusMessage` |
| 批量中单条失败 | 该条 `failed` + Warn 日志；全部跑完后返回带「第 N 条」序号的聚合 error |
| 附件名非法（`.`/`..`/空）或临时目录创建失败 | 返回 `error` + `Status=failed`，不发送 |
| 查询不支持的状态（SMTP/阿里） | **不报错**：`Status=success` + `Extra["note"]` 说明原因 |
| `ctx` 超时/取消 | 腾讯 SDK 支持 ctx 透传；阿里 SDK 无 ctx 变体，超时主要靠 HTTP 层（见场景六注意）；批量中取消则未执行条目回填 failed（不发起新任务） |

## OpenTelemetry 集成

- **Tracer**：`otel.Tracer("goemail")`（校验）与 `otel.Tracer("goemail.<provider>")`（发送/查询）
- **Span 命名**：`email.validate.single`、`email.validate.batch`、`smtp.send.email`、
  `tencent_ses.send.email`、`aliyun_dm.send.email`、`<provider>.send_template_email`、`<provider>.get_email_status`
- **request_id**：从 `ctx` 中的 `logger.ContextKeyRequestID` 取值写入 `email.request_id` 属性（`client.go` 统一注入），
  与全仓 utility 包同一口径
- **常用属性**：`email.provider`、`email.region`、`email.from`（**掩码**）、`email.to.count`、`email.subject`、
  `email.has_attachments`、`email.message_id`、`email.send.duration_ms`、`email.is_valid`
- **PII 约束**：span 属性与 Info 日志里的邮箱一律经 `maskEmail`（本地部分首2尾2按**字符**粒度、域名保留，
  中文等多字节地址不会被 byte 切断成乱码），
  业务结构体（`SendRequest`/`EmailStatus`）保留原文供调用方使用
- **埋点约定**：失败路径 `span.RecordError(err)` + `codes.Error`；成功在函数末尾置一次 `codes.Ok`
- **日志**：全部走 `logger.*WithCtx`（中文文案），request_id 由 logger 从同一 ctx 注入

## 集成测试

集成测试带 `//go:build integration` 构建标签，依赖真实云服务与真实 SMTP，通过环境变量配置。

**环境变量一览**（与 `integration_test.go` 实际读取的集合一一对应）：

| 变量 | 必填 | 用途 | 默认值 |
|------|------|------|--------|
| `GOEMAIL_SMTP_HOST` | SMTP 组必填 | SMTP 服务器地址 | 无 |
| `GOEMAIL_SMTP_PORT` | 否 | SMTP 端口 | `465` |
| `GOEMAIL_SMTP_USERNAME` | SMTP 组必填 | SMTP 用户名 | 无 |
| `GOEMAIL_SMTP_PASSWORD` | SMTP 组必填 | SMTP 密码/授权码 | 无 |
| `GOEMAIL_SMTP_USE_TLS` | 否 | `true` 强制隐式 SSL；未设置按端口自适应 | 未设置 = 自适应 |
| `GOEMAIL_TENCENT_SECRET_ID` | 腾讯组必填 | 腾讯云 SecretId | 无 |
| `GOEMAIL_TENCENT_SECRET_KEY` | 腾讯组必填 | 腾讯云 SecretKey | 无 |
| `GOEMAIL_TENCENT_REGION` | 否 | 腾讯云 SES 区域 | `ap-hongkong` |
| `GOEMAIL_TENCENT_TEMPLATE_ID` | 模板用例必填 | 腾讯云模板 ID | 无 |
| `GOEMAIL_TENCENT_TEMPLATE_DATA` | 否 | 模板变量 JSON 对象（如 `{"code":"123456"}`） | 无（不带变量） |
| `GOEMAIL_ALIYUN_ACCESS_KEY_ID` | 阿里组必填 | 阿里云 AccessKeyId | 无 |
| `GOEMAIL_ALIYUN_ACCESS_KEY_SECRET` | 阿里组必填 | 阿里云 AccessKeySecret | 无 |
| `GOEMAIL_ALIYUN_REGION` | 否 | 阿里云 DM 区域 | `cn-hangzhou` |
| `GOEMAIL_ALIYUN_RECEIVERS_NAME` | 阿里模板用例必填 | 预先创建的收件人列表名称 | 无 |
| `GOEMAIL_ALIYUN_TEMPLATE_NAME` | 阿里模板用例必填 | 模板名称 | 无 |
| `GOEMAIL_TEST_FROM` | 发送用例必填 | 测试发件人（须为已验证域名） | 无 |
| `GOEMAIL_TEST_TO` | 查询/发送用例必填 | 测试收件人 | 无 |
| `GOEMAIL_ALLOW_PAID_SEND` | 发送用例必需 | 设为 `1` 才执行真实发送（付费授权开关） | 未设置 = Skip |
| `GOEMAIL_TEST_TIMEOUT` | 否 | `TestMain` 总时长上限（Go duration） | `120s` |

\* 任一该组必需变量缺失时，对应用例自动 Skip。

**运行方式**：

```bash
cd pkg/goemail

# 方式一：显式传环境变量（CI 用法，优先级高于 .env）
GOEMAIL_TENCENT_SECRET_ID=xxx GOEMAIL_TENCENT_SECRET_KEY=yyy go test -tags=integration -v -count=1

# 方式二：包内 .env（推荐本地，无需任何插件）
# TestMain 里的 loadDotEnv 会自动读取包内 .env 并补齐环境变量，
# 语义是「只补缺、不覆盖已设置的变量」；根 .gitignore 的 *.env 已覆盖该文件，不会入库。
go test -tags=integration -v -count=1

# 只跑某一组（注意：多个用例名之间不要直接用 |，Git Bash 会把 | 当管道）
go test -tags=integration -count=1 -v -run 'TestIntegration_QueryTencent'

# 真实发送是付费操作，必须显式授权
GOEMAIL_ALLOW_PAID_SEND=1 go test -tags=integration -count=1 -v -run 'TestIntegration_SendTencent'

# 仅单元测试（无需任何环境变量，日常使用；注意当前已在 pkg/goemail 目录下）
go test -v .
```

**行为说明**：

- 必需环境变量缺失 → Skip；未授权 `GOEMAIL_ALLOW_PAID_SEND=1` → Skip（防 CI 误配凭据白花钱）；
  查询类免费用例不受付费开关限制；**断言不符（配了凭据仍失败）→ Fail 并保留云原始错误**
- `TestMain` 以看门狗限制总时长（默认 120s），防止外部 SDK goroutine 泄漏导致 `go test` 永远挂起
- 等待/超时均已抽为命名常量（`defaultIntegrationTimeout`、`integrationReadyTimeout`），
  出现超时问题优先调常量，不要改断言

**实测结果汇总**（本机 Windows，`go test -tags=integration -count=1`，多轮实测总耗时 0.02~0.10s）：

| 分组 | 结果 | 关键读数 |
|------|------|----------|
| TestIntegration_SendSMTP / SendBatchSMTP | SKIP | 本机未设置 `GOEMAIL_ALLOW_PAID_SEND=1` |
| TestIntegration_SendTencent / SendTencentTemplate | SKIP | 同上 |
| TestIntegration_SendAliyun / SendAliyunTemplate | SKIP | 同上 |
| TestIntegration_QueryTencent | SKIP | 本机未配置 `GOEMAIL_TEST_TO` |

> 诚实声明：本机未配置任何真实邮件凭据，**上述集成用例本轮未在真实云环境跑通（未实测）**；
> 代码仅通过 `gofmt`、`go vet -tags=integration`、Skip/Fail 边界行为的静态与本地验证。
> 修复过的两条真实 API 语义（阿里 `ReceiversName`/`TagName`、SMTP TLS 分支）同样**未实测**，
> 依据是官方 API 文档与 gomail 源码（证据写在对应源码注释里）。

## 性能基线与模糊测试

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 -short ./pkg/goemail/
```

> 诚实声明：本 README **不声称已经跑过 `-race`**——本机 Windows/MinGW 执行实测报 `exit status 0xc0000139`
> （与同仓 gosms/gozip 相同的环境限制），真实证据需在 Linux/CI 采集。

### 基准测试

```bash
go test ./pkg/goemail/ -bench . -benchtime=100000x -run '^$'
```

| 基准 | 场景 | ns/op | B/op | allocs/op | benchtime |
|------|------|-------|------|-----------|-----------|
| BenchmarkIsValidEmail | 邮箱格式校验（纯函数核心） | 14.29 | 0 | 0 | 100000x |
| BenchmarkMaskEmail | 邮箱脱敏（PII 掩码，rune 粒度） | 155.9 | 32 | 2 | 100000x |
| BenchmarkMaskSecret | 密钥脱敏 | 87.08 | 48 | 2 | 100000x |
| BenchmarkMaskAccessKey | 密钥标识脱敏 | 62.45 | 24 | 2 | 100000x |
| BenchmarkConfigString | Config Stringer 输出 | 943.3 | 496 | 15 | 100000x |
| BenchmarkParseEmailStatusList | 腾讯状态列表解析（nil 安全路径） | 1085 | 1568 | 14 | 100000x |
| BenchmarkBatchErrors | 批量失败聚合（errors.Join） | 999.1 | 456 | 13 | 100000x |
| BenchmarkRunBatchEmail | 批量并发骨架调度（10 条/批，含 ctx 感知退出） | 13065 | 2065 | 24 | 100000x |

**口径与解读：**

- 八条基准均为**无网络 RTT** 的纯函数与本地解析，量的是 goemail 自身的处理开销；实测环境：本机 Windows /
  11th Gen Intel i5-1135G7
- 上表为 `-benchtime=100000x` 正式基线的单轮读数；`ns/op` 受机器状态影响跨轮波动，
  **跨轮次对比以 allocs/op 为稳定指标**。本轮两处实现变化可从读数直接观察到：掩码函数改
  rune 粒度后（96.99 → 155.9 ns/op，字符转换开销换正确性，分配数不变）；批量骨架含 ctx
  感知退出检查后（5812 → 13065 ns/op，每条任务多一轮 select 判断）——均为预期内的语义增强成本
- 稳定指标（allocs/op）：校验 0 分配；三个脱敏函数 2 分配（`strings.Repeat`）；`Config.String` 15 分配；
  状态解析 14 分配（逐条结果 + `Extra` map）；聚合 13 分配（逐条 `fmt.Errorf` + `errors.Join`）；
  批量骨架 24 分配/批（worker goroutine + jobs channel + 结果切片）
- **不纳入基线**：`ValidateEmail`/`validateRequest` 每次调用都会写一条 Info 日志并创建 span（可观测路径），
  放进基准输出会被日志淹没——其纯函数核心 `isValidEmail` 已单列

### 模糊测试

```bash
go test ./pkg/goemail/ -fuzz FuzzIsValidEmail -fuzztime=10s -run '^$'
go test ./pkg/goemail/ -fuzz FuzzMaskEmail -fuzztime=10s -run '^$'
go test ./pkg/goemail/ -fuzz FuzzSendFailFast -fuzztime=10s -run '^$'
go test ./pkg/goemail/ -fuzz FuzzRunBatchOrdering -fuzztime=10s -run '^$'
```

| Target | 不变量 |
|--------|--------|
| FuzzIsValidEmail | 任意输入不 panic；校验通过 ⇒ 含 `@` 且不在首尾、本地/域名非空、域名含点 |
| FuzzMaskEmail | 任意输入不 panic；**按字符长度守恒**（rune 粒度，非 ASCII 不产生无效 UTF-8）；`@域名` 原样保留；本地部分按结构掩码（≤4 全 `*`，否则首2尾2 + 中间全 `*`） |
| FuzzSendFailFast | 校验失败 ⇒ 非 nil `error` + `Status=failed` 的非 nil result，**无半截 `MessageID`/`Extra`**；错误消息不含裸换行（外部值 `%q` 转义）；**nil req / nil query ⇒ fail fast 而非 panic**（三 provider） |
| FuzzRunBatchOrdering | 结果条数恒等于请求条数（**含 ctx 取消后的回填路径**）；输出顺序恒等于请求顺序；注入失败时聚合 error 含正确「第 N 条」序号，全成功为 nil |

**实测**（本机 Windows，`-fuzztime=10s`，最终代码状态）：FuzzIsValidEmail 183,521 execs PASS；
FuzzMaskEmail 176,274 execs PASS；FuzzSendFailFast 27,587 execs PASS；FuzzRunBatchOrdering 116,167 execs PASS。
种子语料随常规 `go test` 执行；execs 受本地语料库增长与机器状态影响跨轮波动，**判定标准是 PASS 本身**。
`testdata/fuzz/FuzzMaskEmail/` 保留了 1 条回归语料：退化输入掩码后恰好与原串相等（中段本就是 `*`），
用来守护断言不退化成「输出 ≠ 输入」这种会被巧合击穿的写法——**它是 fuzz 目标自身的断言缺陷，不是库缺陷**。

## 参考文档

- 变更记录：[CHANGELOG.md](CHANGELOG.md)（条目编号 `R<轮次>-<序号>` 对应改造评审轮次，如 R4-1 为第 4 轮第 1 项）
- [腾讯云 SES 官方文档](https://cloud.tencent.com/document/product/1317)
- [阿里云邮件推送 官方文档](https://help.aliyun.com/product/29412.html)
