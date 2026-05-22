# GoEmail - 多平台邮件发送库

## 📦 概述

企业级多平台邮件发送库，支持**腾讯云SES**、**阿里云DM**和**SMTP**协议。

### 核心特性

- ✅ **多平台支持**: 腾讯云SES、阿里云DM、SMTP
- ✅ **统一接口**: 一套API适配所有平台
- ✅ **模板邮件**: 支持腾讯云模板发送（验证码、通知等）
- ✅ **批量发送**: 高效批量邮件处理
- ✅ **附件支持**: 多附件上传
- ✅ **标签追踪**: 邮件统计分析
- ✅ **状态查询**: 查询邮件发送状态和投递结果 ⭐
- ✅ **Context支持**: 超时控制和取消

---

## 🚀 快速开始

### 安装

```bash
cd D:/go/src/sunshine/pkg/goemail
bash install_deps.sh
```

### 配置环境变量

复制环境变量模板并填写实际值：

```bash
cp env.example .env
# 编辑 .env 文件，填写实际的AK/SK或SMTP账号
```

**环境变量说明：**

| 变量名 | 说明 | 适用平台 |
|--------|------|----------|
| `TENCENT_AK` | 腾讯云SecretId | tencent_ses |
| `TENCENT_SK` | 腾讯云SecretKey | tencent_ses |
| `ALIYUN_AK` | 阿里云AccessKey ID | aliyun_dm |
| `ALIYUN_SK` | 阿里云AccessKey Secret | aliyun_dm |
| `SMTP_USERNAME` | SMTP邮箱账号 | smtp |
| `SMTP_PASSWORD` | SMTP授权码（非密码） | smtp |
| `EMAIL_AK` | 通用AK | 所有平台 |
| `EMAIL_SK` | 通用SK | 所有平台 |

### 方式1：从配置文件初始化（推荐）⭐

**步骤1：配置YAML文件** (`configs/serverNameExample.yml`)

```yaml
# 邮件服务配置（支持腾讯云SES/阿里云DM/SMTP）
email:
  providerType: "tencent_ses"     # tencent_ses | aliyun_dm | smtp
  region: "ap-guangzhou"          # 区域
  accessKeyID: "YOUR_AK"          # AK/SK（建议从环境变量读取）
  secretKey: "YOUR_SK"
  # SMTP专用（providerType=smtp时使用）
  smtp:
    host: "smtp.qq.com"
    port: 465
    username: "your_email@qq.com"
    password: "your_auth_code"
```

**步骤2：运行配置生成命令**

```bash
make update-config
```

这会自动生成Go结构体到 `internal/config/serverNameExample.go`

**步骤3：在代码中使用**

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    
    "github.com/18721889353/sunshine/internal/config"
    "github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
    // 1. 初始化配置（项目启动时调用一次）
    if err := config.Init("configs/serverNameExample.yml"); err != nil {
        log.Fatal(err)
    }
    
    // 2. 获取email配置
    cfg := config.Get()
    emailCfg := cfg.Email
    
    // 3. 创建邮件客户端配置
    emailClientCfg := &goemail.Config{
        ProviderType: goemail.ProviderType(emailCfg.ProviderType),
        Region:       emailCfg.Region,
        AccessKeyID:  os.Getenv("EMAIL_AK"), // 从环境变量读取AK/SK
        SecretKey:    os.Getenv("EMAIL_SK"),
    }
    
    // SMTP专用配置
    if emailCfg.ProviderType == "smtp" {
        emailClientCfg.SMTPHost = emailCfg.SMTP.Host
        emailClientCfg.SMTPPort = emailCfg.SMTP.Port
        emailClientCfg.SMTPUsername = emailCfg.SMTP.Username
        emailClientCfg.SMTPPassword = emailCfg.SMTP.Password
        emailClientCfg.UseTLS = true
    }
    
    // 4. 创建客户端
    client, err := goemail.NewEmailClient(emailClientCfg)
    if err != nil {
        log.Fatal(err)
    }
    
    // 5. 发送邮件
    req := &goemail.SendRequest{
        From:     "sender@example.com",
        To:       []string{"user@example.com"},
        Subject:  "欢迎注册",
        HtmlBody: "<h1>欢迎加入</h1><p>感谢注册！</p>",
    }
    
    ctx := context.Background()
    result, err := client.SendEmail(ctx, req)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("发送成功！Message ID: %s\n", result.MessageID)
}
```

### 方式2：直接创建配置（快速测试）

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    
    "github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
    // 1. 创建客户端
    cfg := &goemail.Config{
        ProviderType: goemail.ProviderTypeTencentSES,
        Region:       "ap-guangzhou",
        AccessKeyID:  os.Getenv("TENCENT_AK"), // 从环境变量读取
        SecretKey:    os.Getenv("TENCENT_SK"),
    }
    
    client, err := goemail.NewEmailClient(cfg)
    if err != nil {
        log.Fatal(err)
    }
    
    // 2. 发送邮件
    req := &goemail.SendRequest{
        From:     "sender@example.com",
        To:       []string{"user@example.com"},
        Subject:  "欢迎注册",
        HtmlBody: "<h1>欢迎加入</h1><p>感谢注册！</p>",
    }
    
    ctx := context.Background()
    result, err := client.SendEmail(ctx, req)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("发送成功！Message ID: %s\n", result.MessageID)
}
```

---

## 📊 邮件状态查询 ⭐

### 功能概述

发送邮件后，您可以查询邮件的发送状态、投递状态以及用户行为（打开、点击、退订、投诉等）。

**支持的平台：**
- ✅ **腾讯云SES**: 完整支持单封邮件状态查询
- ⚠️ **阿里云DM**: 仅支持基于TagName的发送记录统计查询
- ❌ **SMTP**: 不支持（协议限制）

### 案例1：腾讯云SES - 查询邮件发送状态

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
    // 创建客户端
    cfg := &goemail.Config{
        ProviderType: goemail.ProviderTypeTencentSES,
        Region:       "ap-guangzhou",
        AccessKeyID:  os.Getenv("TENCENT_AK"),
        SecretKey:    os.Getenv("TENCENT_SK"),
    }
    
    client, err := goemail.NewEmailClient(cfg)
    if err != nil {
        log.Fatal(err)
    }
    
    ctx := context.Background()
    
    // 步骤1: 发送邮件
    sendReq := &goemail.SendRequest{
        From:     "sender@example.com",
        To:       []string{"user@example.com"},
        Subject:  "测试邮件",
        HTMLBody: "<h1>这是一封测试邮件</h1>",
    }
    
    result, err := client.SendEmail(ctx, sendReq)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("✅ 邮件发送成功! MessageID: %s\n", result.MessageID)
    
    // 步骤2: 等待邮件处理（可选）
    time.Sleep(2 * time.Second)
    
    // 步骤3: 查询邮件状态
    statusResult, err := client.GetEmailStatus(ctx, &goemail.EmailStatusQuery{
        MessageID: result.MessageID,
        FromDate:  time.Now(),
        Limit:     10,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // 步骤4: 显示状态信息
    for i, status := range statusResult.Data {
        fmt.Printf("📧 邮件 %d:\n", i+1)
        fmt.Printf("   MessageID: %s\n", status.MessageID)
        fmt.Printf("   收件人: %s\n", status.ToAddress)
        fmt.Printf("   状态: %s\n", status.Status)
        fmt.Printf("   状态码: %d\n", status.StatusCode)
        fmt.Printf("   描述: %s\n", status.StatusMessage)
        fmt.Printf("   请求时间: %s\n", 
            status.RequestTime.Format("2006-01-02 15:04:05"))
        if !status.DeliverTime.IsZero() {
            fmt.Printf("   投递时间: %s\n", 
                status.DeliverTime.Format("2006-01-02 15:04:05"))
        }
        fmt.Printf("   已打开: %v\n", status.UserOpened)
        fmt.Printf("   已点击: %v\n", status.UserClicked)
        fmt.Printf("   已退订: %v\n", status.UserUnsubscribed)
        fmt.Printf("   已投诉: %v\n", status.UserComplained)
    }
}
```

### 案例2：按时间范围查询

```go
// 查询最近1小时的邮件状态
now := time.Now()
oneHourAgo := now.Add(-1 * time.Hour)

statusResult, err := client.GetEmailStatus(ctx, &goemail.EmailStatusQuery{
    FromDate: oneHourAgo,
    ToDate:   now,
    Offset:   0,
    Limit:    100,
})
if err != nil {
    log.Fatal(err)
}

fmt.Printf("查询到 %d 条邮件记录\n", len(statusResult.Data))
```

### 案例3：按收件人查询

```go
// 查询特定收件人的邮件状态
statusResult, err := client.GetEmailStatus(ctx, &goemail.EmailStatusQuery{
    ToAddress: "user@example.com",
    FromDate:  time.Now().Add(-24 * time.Hour),
    Limit:     50,
})
if err != nil {
    log.Fatal(err)
}

for _, status := range statusResult.Data {
    fmt.Printf("收件人: %s, 状态: %s\n", status.ToAddress, status.Status)
}
```

### 案例4：阿里云DM - 查询发送记录

```go
// 注意：阿里云DM不支持直接查询单封邮件状态
// 需要使用GetTrackList方法查询基于TagName的发送记录

aliyunClient := client.(*goemail.AliyunDMClient)

trackResult, err := aliyunClient.GetTrackList(
    ctx,
    "sender@example.com",           // 发件人
    "verification_code",             // 标签名（发送邮件时设置）
    time.Now().Add(-24*time.Hour),   // 开始时间
    time.Now(),                      // 结束时间
)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("查询到 %d 条发送记录\n", trackResult.Extra["total"])
```

**发送邮件时设置TagName：**

```go
// 阿里云DM发送邮件时，通过Tags设置TagName用于后续追踪
sendReq := &goemail.SendRequest{
    From:     "sender@example.com",
    To:       []string{"user@example.com"},
    Subject:  "验证码",
    HTMLBody: "<p>您的验证码是: 123456</p>",
    Tags: map[string]string{
        "TagName": "verification_code", // 设置标签名
    },
}

result, err := client.SendEmail(ctx, sendReq)
```

### 状态说明

**腾讯云SES状态码：**

| 状态 | 说明 |
|------|------|
| `accepted` | 处理成功，进入发送队列 |
| `delivered` | 邮件递送成功 |
| `failed` | 发送失败 |
| `rejected` | 被收件方拒收 |
| `delayed` | 延迟递送 |
| `pending` | 等待投递 |

**用户行为追踪：**

| 字段 | 说明 |
|------|------|
| `UserOpened` | 用户是否打开了邮件 |
| `UserClicked` | 用户是否点击了邮件中的链接 |
| `UserUnsubscribed` | 用户是否退订 |
| `UserComplained` | 用户是否投诉为垃圾邮件 |

---

## 💡 详细使用案例

### 案例1：发送验证码（模板邮件）⭐

**场景**: 用户注册、登录时发送验证码

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"
    
    "github.com/18721889353/sunshine/pkg/goemail"
)

func SendVerificationCode(email string, code string) error {
    // 1. 初始化客户端
    cfg := &goemail.Config{
        ProviderType: goemail.ProviderTypeTencentSES,
        Region:       "ap-guangzhou",
        AccessKeyID:  os.Getenv("TENCENT_AK"),
        SecretKey:    os.Getenv("TENCENT_SK"),
    }
    
    client, err := goemail.NewEmailClient(cfg)
    if err != nil {
        return fmt.Errorf("init client failed: %w", err)
    }
    
    // 2. 转换为TencentSESClient以使用模板功能
    tencentClient, ok := client.(*goemail.TencentSESClient)
    if !ok {
        return fmt.Errorf("not a Tencent SES client")
    }
    
    // 3. 准备模板数据
    // 假设在腾讯云控制台创建了模板：
    // 内容：尊敬的{{username}}，您的验证码是{{code}}，有效期{{expiry}}
    templateData := map[string]interface{}{
        "username": "用户",
        "code":     code,
        "expiry":   "10分钟",
    }
    
    // 4. 发送模板邮件
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    result, err := tencentClient.SendTemplateEmail(
        ctx,
        "noreply@example.com",           // 发件人
        []string{email},                  // 收件人
        12345,                            // 模板ID（从腾讯云控制台获取）
        templateData,                     // 模板变量
        "验证码",                          // 邮件主题
    )
    
    if err != nil {
        return fmt.Errorf("send failed: %w", err)
    }
    
    log.Printf("验证码已发送至 %s, MessageID: %s", email, result.MessageID)
    return nil
}

func main() {
    // 生成随机验证码
    code := "123456" // 实际应使用随机生成
    
    err := SendVerificationCode("user@example.com", code)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println("验证码发送成功！")
}
```

**模板管理步骤:**
1. 登录 [腾讯云SES控制台](https://console.cloud.tencent.com/ses)
2. 进入「模板管理」→ 创建模板
3. 填写模板内容（使用`{{变量名}}`语法）
4. 审核通过后获得模板ID

---

### 案例1.5：阿里云DM模板邮件

**场景**: 使用阿里云DM发送模板邮件

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    
    "github.com/18721889353/sunshine/pkg/goemail"
)

func SendAliyunTemplateEmail(email string, code string) error {
    // 1. 初始化客户端
    cfg := &goemail.Config{
        ProviderType: goemail.ProviderTypeAliyunDM,
        Region:       "cn-hangzhou",
        AccessKeyID:  os.Getenv("ALIYUN_AK"),
        SecretKey:    os.Getenv("ALIYUN_SK"),
    }
    
    client, err := goemail.NewEmailClient(cfg)
    if err != nil {
        return fmt.Errorf("init client failed: %w", err)
    }
    
    // 2. 转换为AliyunDMClient以使用模板功能
    aliyunClient, ok := client.(*goemail.AliyunDMClient)
    if !ok {
        return fmt.Errorf("not an Aliyun DM client")
    }
    
    // 3. 准备模板数据
    // 假设在阿里云控制台创建了模板：
    // 内容：尊敬的${userName}，您的验证码是${code}，有效期${expiry}
    templateData := map[string]interface{}{
        "userName": "用户",
        "code":     code,
        "expiry":   "10分钟",
    }
    
    // 4. 发送模板邮件
    ctx := context.Background()
    result, err := aliyunClient.SendTemplateEmail(
        ctx,
        "noreply@example.com",           // 发件人
        []string{email},                  // 收件人
        "verification_code",              // 模板名称（从阿里云控制台获取）
        templateData,                     // 模板变量
    )
    
    if err != nil {
        return fmt.Errorf("send failed: %w", err)
    }
    
    log.Printf("验证码已发送至 %s, EnvID: %v", email, result.Extra["env_id"])
    return nil
}

func main() {
    // 生成随机验证码
    code := "123456" // 实际应使用随机生成
    
    err := SendAliyunTemplateEmail("user@example.com", code)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println("验证码发送成功！")
}
```

**阿里云DM模板管理步骤:**
1. 登录 [阿里云DM控制台](https://dm.console.aliyun.com)
2. 进入「模板管理」→ 创建模板
3. 填写模板内容（使用`${变量名}`语法）
4. 审核通过后获得模板名称

---

### 案例2：订单通知（带标签追踪）

**场景**: 电商订单发货通知

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    
    "github.com/18721889353/sunshine/pkg/goemail"
)

type Order struct {
    OrderNo        string
    UserName       string
    UserEmail      string
    ProductName    string
    Amount         float64
    ExpressCompany string
    TrackingNo     string
}

func SendOrderNotification(order *Order) error {
    cfg := &goemail.Config{
        ProviderType: goemail.ProviderTypeTencentSES,
        Region:       "ap-guangzhou",
        AccessKeyID:  os.Getenv("TENCENT_AK"),
        SecretKey:    os.Getenv("TENCENT_SK"),
    }
    
    client, err := goemail.NewEmailClient(cfg)
    if err != nil {
        return err
    }
    
    req := &goemail.SendRequest{
        From:     "order@example.com",
        To:       []string{order.UserEmail},
        Subject:  fmt.Sprintf("订单 #%s 已发货", order.OrderNo),
        HtmlBody: fmt.Sprintf(`
            <div style="padding: 20px; font-family: Arial;">
                <h2 style="color: #333;">订单发货通知</h2>
                <p>尊敬的 %s：</p>
                <p>您的订单 <strong>%s</strong> 已发货。</p>
                <div style="background: #f5f5f5; padding: 15px; margin: 10px 0;">
                    <p><strong>商品：</strong>%s</p>
                    <p><strong>金额：</strong>¥%.2f</p>
                    <p><strong>快递：</strong>%s</p>
                    <p><strong>单号：</strong>%s</p>
                </div>
                <p style="color: #999; font-size: 12px;">如有疑问，请联系客服</p>
            </div>
        `, order.UserName, order.OrderNo, order.ProductName, 
           order.Amount, order.ExpressCompany, order.TrackingNo),
        Tags: map[string]string{
            "order_no": order.OrderNo,
            "type":     "order_notification",
            "user_id":  "12345",
        },
    }
    
    ctx := context.Background()
    result, err := client.SendEmail(ctx, req)
    if err != nil {
        return err
    }
    
    log.Printf("订单通知已发送: %s", result.MessageID)
    return nil
}

func main() {
    order := &Order{
        OrderNo:        "ORD20240521001",
        UserName:       "张三",
        UserEmail:      "zhangsan@example.com",
        ProductName:    "iPhone 15 Pro",
        Amount:         8999.00,
        ExpressCompany: "顺丰速运",
        TrackingNo:     "SF1234567890",
    }
    
    if err := SendOrderNotification(order); err != nil {
        log.Fatal(err)
    }
    
    fmt.Println("订单通知发送成功！")
}
```

---

### 案例3：批量发送营销邮件

**场景**: 向多个用户发送促销信息

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    
    "github.com/18721889353/sunshine/pkg/goemail"
)

type User struct {
    ID    int
    Name  string
    Email string
}

func SendMarketingEmail(users []User) error {
    cfg := &goemail.Config{
        ProviderType: goemail.ProviderTypeTencentSES,
        Region:       "ap-guangzhou",
        AccessKeyID:  os.Getenv("TENCENT_AK"),
        SecretKey:    os.Getenv("TENCENT_SK"),
    }
    
    client, err := goemail.NewEmailClient(cfg)
    if err != nil {
        return err
    }
    
    // 构建批量请求
    var reqs []*goemail.SendRequest
    for _, user := range users {
        reqs = append(reqs, &goemail.SendRequest{
            From:     "marketing@example.com",
            To:       []string{user.Email},
            Subject:  "限时优惠 - 全场8折",
            HtmlBody: fmt.Sprintf(`
                <div style="padding: 20px;">
                    <h1 style="color: #ff6b6b;">🎉 限时优惠</h1>
                    <p>尊敬的 %s：</p>
                    <p>感谢您的支持！现在全场商品<strong>8折优惠</strong>。</p>
                    <p style="color: #ff6b6b; font-size: 18px;">活动时间：即日起至本月底</p>
                    <a href="https://example.com/sale" 
                       style="display: inline-block; padding: 10px 20px; 
                              background: #ff6b6b; color: white; 
                              text-decoration: none; border-radius: 5px;">
                        立即抢购
                    </a>
                    <hr style="margin: 20px 0;">
                    <p style="font-size: 12px; color: #999;">
                        不想再收到此类邮件？<a href="https://example.com/unsubscribe?uid=%d">点击退订</a>
                    </p>
                </div>
            `, user.Name, user.ID),
            Tags: map[string]string{
                "campaign": "sale_2024_q2",
                "user_id":  fmt.Sprintf("%d", user.ID),
                "type":     "marketing",
            },
        })
    }
    
    // 批量发送
    ctx := context.Background()
    results, err := client.SendBatchEmail(ctx, reqs)
    if err != nil {
        return err
    }
    
    // 统计结果
    successCount := 0
    failCount := 0
    for i, result := range results {
        if result.Status == "success" {
            successCount++
            log.Printf("用户 %d (%s) 发送成功", i+1, users[i].Email)
        } else {
            failCount++
            log.Printf("用户 %d (%s) 发送失败: %v", i+1, users[i].Email, result.Error)
        }
    }
    
    log.Printf("批量发送完成: 成功 %d, 失败 %d, 总计 %d", 
        successCount, failCount, len(users))
    
    return nil
}

func main() {
    users := []User{
        {ID: 1, Name: "张三", Email: "zhangsan@example.com"},
        {ID: 2, Name: "李四", Email: "lisi@example.com"},
        {ID: 3, Name: "王五", Email: "wangwu@example.com"},
    }
    
    if err := SendMarketingEmail(users); err != nil {
        log.Fatal(err)
    }
    
    fmt.Println("营销邮件批量发送完成！")
}
```

---

### 案例4：SMTP发送（QQ邮箱示例）

**场景**: 小规模内部通知、测试环境

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
    // QQ邮箱配置
    cfg := &goemail.Config{
        ProviderType: goemail.ProviderTypeSMTP,
        SMTPHost:     "smtp.qq.com",
        SMTPPort:     465,
        SMTPUsername: "your_email@qq.com",
        SMTPPassword: "your_auth_code", // 授权码，不是密码
        UseTLS:       false,
    }
    
    client, err := goemail.NewEmailClient(cfg)
    if err != nil {
        log.Fatal(err)
    }
    
    req := &goemail.SendRequest{
        From:     "your_email@qq.com",
        To:       []string{"recipient@example.com"},
        Cc:       []string{"cc@example.com"},      // 抄送
        Bcc:      []string{"bcc@example.com"},     // 密送
        Subject:  "测试邮件",
        HtmlBody: "<h1>Hello</h1><p>这是一封测试邮件</p>",
        TextBody: "Hello\n这是一封测试邮件",
        Headers: map[string]string{
            "X-Priority": "1",
            "X-Mailer":   "GoEmail",
        },
    }
    
    ctx := context.Background()
    result, err := client.SendEmail(ctx, req)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("发送成功！Message ID: %s\n", result.MessageID)
}
```

**获取QQ邮箱授权码:**
1. 登录QQ邮箱网页版
2. 设置 → 账户
3. 开启POP3/SMTP服务
4. 生成授权码

---

### 案例5：带附件发送

**场景**: 发送报表、文档

```go
package main

import (
    "context"
    "io/ioutil"
    "log"
    
    "github.com/18721889353/sunshine/pkg/goemail"
)

func SendReport(email string, reportPath string) error {
    cfg := &goemail.Config{
        ProviderType: goemail.ProviderTypeSMTP,
        SMTPHost:     "smtp.qq.com",
        SMTPPort:     465,
        SMTPUsername: "your_email@qq.com",
        SMTPPassword: "your_password",
    }
    
    client, err := goemail.NewEmailClient(cfg)
    if err != nil {
        return err
    }
    
    // 读取附件
    pdfContent, err := ioutil.ReadFile(reportPath)
    if err != nil {
        return err
    }
    
    req := &goemail.SendRequest{
        From:     "your_email@qq.com",
        To:       []string{email},
        Subject:  "月度报告",
        HtmlBody: "<p>请查收附件中的月度报告</p>",
        Attachments: []*goemail.Attachment{
            {
                Filename:    "monthly_report.pdf",
                Content:     pdfContent,
                ContentType: "application/pdf",
            },
        },
    }
    
    ctx := context.Background()
    _, err = client.SendEmail(ctx, req)
    return err
}

func main() {
    err := SendReport("boss@example.com", "report.pdf")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("报告发送成功！")
}
```

---

### 案例6：错误处理与重试

**场景**: 生产环境可靠的邮件发送

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"
    
    "github.com/18721889353/sunshine/pkg/goemail"
)

// 带重试的发送函数
func sendWithRetry(client goemail.EmailClient, req *goemail.SendRequest, maxRetries int) (*goemail.SendResult, error) {
    var lastErr error
    
    for i := 0; i < maxRetries; i++ {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        result, err := client.SendEmail(ctx, req)
        cancel()
        
        if err == nil && result.Status == "success" {
            return result, nil
        }
        
        lastErr = err
        log.Printf("第 %d/%d 次重试失败: %v", i+1, maxRetries, err)
        
        // 指数退避：1s, 2s, 4s, 8s...
        if i < maxRetries-1 {
            waitTime := time.Duration(1<<uint(i)) * time.Second
            log.Printf("等待 %v 后重试...", waitTime)
            time.Sleep(waitTime)
        }
    }
    
    return nil, fmt.Errorf("重试 %d 次后仍失败: %w", maxRetries, lastErr)
}

func main() {
    cfg := &goemail.Config{
        ProviderType: goemail.ProviderTypeTencentSES,
        Region:       "ap-guangzhou",
        AccessKeyID:  os.Getenv("TENCENT_AK"),
        SecretKey:    os.Getenv("TENCENT_SK"),
    }
    
    client, err := goemail.NewEmailClient(cfg)
    if err != nil {
        log.Fatal(err)
    }
    
    req := &goemail.SendRequest{
        From:     "sender@example.com",
        To:       []string{"user@example.com"},
        Subject:  "重要通知",
        HtmlBody: "<p>这是一封重要邮件</p>",
    }
    
    // 最多重试3次
    result, err := sendWithRetry(client, req, 3)
    if err != nil {
        log.Fatalf("发送失败: %v", err)
    }
    
    log.Printf("发送成功！MessageID: %s", result.MessageID)
}
```

---

## 📊 平台对比

| 特性 | 腾讯云SES | 阿里云DM | SMTP |
|------|-----------|----------|------|
| **适用场景** | 生产环境 | 生产环境 | 测试/小规模 |
| **发送速度** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| **到达率** | 高 | 高 | 中 |
| **成本** | 按量付费 | 按量付费 | 免费 |
| **模板支持** | ✅ (TemplateID) | ✅ (TemplateName) | ❌ |
| **批量发送** | ✅ | ✅ | ✅ |
| **标签追踪** | ✅ | ✅ | ❌ |

**推荐选择:**
- 🎯 **生产环境**: 腾讯云SES（优先）或阿里云DM
- 🧪 **开发测试**: SMTP（QQ/163邮箱）
- 📨 **验证码/通知**: 腾讯云SES模板邮件（使用TemplateID）
- 📢 **营销邮件**: 阿里云DM模板邮件（使用TemplateName）

---

## 🔧 常用SMTP配置

```go
// QQ邮箱
cfg := &goemail.Config{
    ProviderType: goemail.ProviderTypeSMTP,
    SMTPHost:     "smtp.qq.com",
    SMTPPort:     465,
    SMTPUsername: "xxx@qq.com",
    SMTPPassword: "授权码",
}

// 163邮箱
cfg := &goemail.Config{
    ProviderType: goemail.ProviderTypeSMTP,
    SMTPHost:     "smtp.163.com",
    SMTPPort:     465,
    SMTPUsername: "xxx@163.com",
    SMTPPassword: "授权码",
}

// Gmail
cfg := &goemail.Config{
    ProviderType: goemail.ProviderTypeSMTP,
    SMTPHost:     "smtp.gmail.com",
    SMTPPort:     587,
    SMTPUsername: "xxx@gmail.com",
    SMTPPassword: "应用专用密码",
    UseTLS:       true,
}

// 企业邮箱
cfg := &goemail.Config{
    ProviderType: goemail.ProviderTypeSMTP,
    SMTPHost:     "smtp.exmail.qq.com",
    SMTPPort:     465,
    SMTPUsername: "user@company.com",
    SMTPPassword: "password",
}
```

---

## 📋 API参考

### 创建客户端

```go
client, err := goemail.NewEmailClient(&goemail.Config{
    ProviderType: goemail.ProviderTypeTencentSES,
    Region:       "ap-guangzhou",
    AccessKeyID:  "YOUR_AK",
    SecretKey:    "YOUR_SK",
})
```

### 发送邮件

```go
result, err := client.SendEmail(ctx, &goemail.SendRequest{
    From:     "sender@example.com",
    To:       []string{"user@example.com"},
    Subject:  "主题",
    HtmlBody: "<p>HTML内容</p>",
    TextBody: "纯文本内容",
})
```

### 模板邮件（腾讯云SES）

```go
tencentClient := client.(*goemail.TencentSESClient)
result, err := tencentClient.SendTemplateEmail(
    ctx,
    "sender@example.com",
    []string{"user@example.com"},
    12345,                    // 模板ID
    map[string]interface{}{   // 模板变量
        "username": "张三",
        "code": "123456",
    },
    "验证码",
)
```

### 模板邮件（阿里云DM）

```go
aliyunClient := client.(*goemail.AliyunDMClient)
result, err := aliyunClient.SendTemplateEmail(
    ctx,
    "sender@example.com",
    []string{"user@example.com"},
    "verification_code",      // 模板名称
    map[string]interface{}{   // 模板变量
        "userName": "张三",
        "code": "123456",
    },
)
```

---

## ⚠️ 注意事项

1. **密钥安全**: 使用环境变量存储AK/SK，不要硬编码
2. **频率限制**: 控制发送速率，避免触发限流
3. **退订机制**: 营销邮件必须提供退订链接
4. **超时控制**: 始终使用Context设置超时
5. **错误重试**: 实现重试机制提高成功率
6. **监控告警**: 记录发送日志，监控失败率

---

## 🧪 测试

```bash
# 运行测试
cd D:/go/src/sunshine/pkg/goemail
go test -v

# 跳过需要真实配置的测试
go test -v -short
```

---

## 📈 性能指标

| 平台 | 平均延迟 | QPS | 并发能力 |
|------|---------|-----|---------|
| 腾讯云SES | ~200ms | 100+ | 高 |
| 阿里云DM | ~250ms | 80+ | 高 |
| SMTP | ~500ms | 10-20 | 低 |

---

## 📝 更新日志

- **v2.3.0** (2026-05-22): 新增邮件状态查询功能 ⭐
  - ✅ 腾讯云SES完整支持单封邮件状态查询
  - ✅ 阿里云DM支持基于TagName的发送记录统计查询
  - ✅ 统一接口设计（GetEmailStatus）
  - ✅ 支持查询发送状态、投递状态、用户行为（打开/点击/退订/投诉）
  - ✅ 完整的OpenTelemetry链路追踪集成
  
- **v2.2.0** (2026-05-21): 新增阿里云DM模板邮件支持
  - ✅ 阿里云DM客户端完整实现（修复SDK初始化）
  - ✅ 阿里云DM模板邮件发送功能（使用TemplateName）
  - ✅ 完善文档和示例（腾讯云SES + 阿里云DM双平台）
  
- **v2.1.0** (2026-05-21): 新增模板邮件支持
  - ✅ 腾讯云SES模板邮件完整实现
  - ✅ 参考PHP官方案例优化API设计
  - ✅ 完善文档和示例
  
- **v2.0.0** (2026-05-21): 重构为多平台架构
  - 支持腾讯云SES、阿里云DM、SMTP
  - 统一接口设计
  - 批量发送支持

---

**相关文档**: 
- [腾讯云SES官方文档](https://cloud.tencent.com/document/product/1317)
- [阿里云DM官方文档](https://help.aliyun.com/product/29412.html)
