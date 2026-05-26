# gosms

Go 语言多平台短信发送库，支持腾讯云SMS、阿里云SMS等服务提供商。

## ✨ 特性

- 🔒 **多平台支持**：腾讯云SMS、阿里云SMS
- 🧵 **线程安全**：支持并发使用
- 🎯 **统一接口**：不同提供商使用相同的API
- 📝 **链路追踪**：集成 OpenTelemetry
- ✅ **完善文档**：丰富的示例和文档

<br>

## 📦 安装

```bash
go get github.com/18721889353/sunshine/pkg/gosms
```

### 依赖安装

**腾讯云SMS：**
```bash
go get github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111
```

**阿里云SMS：**
```bash
go get github.com/alibabacloud-go/dysmsapi-20170525/v4/client
go get github.com/alibabacloud-go/darabonba-openapi/v2/client
go get github.com/alibabacloud-go/tea-utils/v2/service
go get github.com/alibabacloud-go/tea/tea
```

<br>

## 🚀 快速开始

### 1️⃣ 发送腾讯短信

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/18721889353/sunshine/pkg/gosms"
)

func main() {
    // 创建配置
    cfg := &gosms.Config{
        ProviderType: gosms.ProviderTypeTencentSMS,
        Region:       "ap-shanghai",
        AccessKeyID:  "your-secret-id",     // 从环境变量获取
        SecretKey:    "your-secret-key",    // 从环境变量获取
        TencentAppID: "1400282665",         // 腾讯云短信应用ID
    }

    // 创建客户端
    client, err := gosms.NewSMSClient(cfg)
    if err != nil {
        log.Fatalf("创建客户端失败: %v", err)
    }

    // 发送短信
    ctx := context.Background()
    req := &gosms.SendRequest{
        PhoneNumbers: []string{"+8613711112222"},
        TemplateID:   "1234567",                    // 模板ID
        SignName:     "江苏通卡数字科技有限公司",      // 签名名称
        TemplateParams: map[string]string{
            "1": "123456", // 验证码
        },
    }

    result, err := client.SendSMS(ctx, req)
    if err != nil {
        log.Printf("发送失败: %v", err)
        return
    }

    fmt.Printf("发送结果: %+v\n", result)
}
```

### 2️⃣ 发送阿里短信

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/18721889353/sunshine/pkg/gosms"
)

func main() {
    // 创建配置
    cfg := &gosms.Config{
        ProviderType: gosms.ProviderTypeAliyunSMS,
        Region:       "cn-hangzhou",
        AccessKeyID:  "your-access-key-id",     // 从环境变量获取
        SecretKey:    "your-access-key-secret",  // 从环境变量获取
    }

    // 创建客户端
    client, err := gosms.NewSMSClient(cfg)
    if err != nil {
        log.Fatalf("创建客户端失败: %v", err)
    }

    // 发送短信
    ctx := context.Background()
    req := &gosms.SendRequest{
        PhoneNumbers: []string{"+8613711112222"},
        TemplateID:   "SMS_123456789",              // 模板ID
        SignName:     "江苏通卡数字科技有限公司",      // 签名名称
        TemplateParams: map[string]string{
            "code": "123456", // 验证码
        },
    }

    result, err := client.SendSMS(ctx, req)
    if err != nil {
        log.Printf("发送失败: %v", err)
        return
    }

    fmt.Printf("发送结果: %+v\n", result)
}
```

### 3️⃣ 批量发送短信

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/18721889353/sunshine/pkg/gosms"
)

func main() {
    // 创建配置
    cfg := &gosms.Config{
        ProviderType: gosms.ProviderTypeTencentSMS,
        Region:       "ap-shanghai",
        AccessKeyID:  "your-secret-id",
        SecretKey:    "your-secret-key",
        TencentAppID: "1400282665",
    }

    // 创建客户端
    client, err := gosms.NewSMSClient(cfg)
    if err != nil {
        log.Fatalf("创建客户端失败: %v", err)
    }

    // 准备批量请求
    ctx := context.Background()
    reqs := []*gosms.SendRequest{
        {
            PhoneNumbers: []string{"+8613711112222"},
            TemplateID:   "1234567",
            SignName:     "江苏通卡数字科技有限公司",
            TemplateParams: map[string]string{
                "1": "123456",
            },
        },
        {
            PhoneNumbers: []string{"+8613711112223"},
            TemplateID:   "1234567",
            SignName:     "江苏通卡数字科技有限公司",
            TemplateParams: map[string]string{
                "1": "654321",
            },
        },
    }

    // 批量发送
    results, err := client.SendBatchSMS(ctx, reqs)
    if err != nil {
        log.Printf("批量发送失败: %v", err)
        return
    }

    for i, result := range results {
        fmt.Printf("第%d条短信结果: %+v\n", i+1, result)
    }
}
```

### 4️⃣ 查询短信状态

**腾讯云SMS：**

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    "github.com/18721889353/sunshine/pkg/gosms"
)

func main() {
    // 创建配置
    cfg := &gosms.Config{
        ProviderType: gosms.ProviderTypeTencentSMS,
        Region:       "ap-shanghai",
        AccessKeyID:  "your-secret-id",
        SecretKey:    "your-secret-key",
        TencentAppID: "1400282665",
    }

    // 创建客户端
    client, err := gosms.NewSMSClient(cfg)
    if err != nil {
        log.Fatalf("创建客户端失败: %v", err)
    }

    // 查询状态
    ctx := context.Background()
    query := &gosms.SMSStatusQuery{
        PhoneNumber: "+8613711112222",
        FromDate:    time.Now().Add(-24 * time.Hour), // 查询最近24小时
        ToDate:      time.Now(),
        Offset:      0,
        Limit:       10,
    }

    result, err := client.GetSMSStatus(ctx, query)
    if err != nil {
        log.Printf("查询失败: %v", err)
        return
    }

    fmt.Printf("查询结果: %+v\n", result)
}
```

**阿里云SMS：**

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    "github.com/18721889353/sunshine/pkg/gosms"
)

func main() {
    // 创建配置
    cfg := &gosms.Config{
        ProviderType: gosms.ProviderTypeAliyunSMS,
        Region:       "cn-hangzhou",
        AccessKeyID:  "your-access-key-id",
        SecretKey:    "your-access-key-secret",
    }

    // 创建客户端
    client, err := gosms.NewSMSClient(cfg)
    if err != nil {
        log.Fatalf("创建客户端失败: %v", err)
    }

    // 查询状态
    ctx := context.Background()
    query := &gosms.SMSStatusQuery{
        PhoneNumber: "+8613711112222",
        FromDate:    time.Now().Add(-1 * time.Hour), // 查询最近1小时
        ToDate:      time.Now(),
        Offset:      0,  // 偏移量（从0开始）
        Limit:       10, // 每页数量（1-50）
    }

    result, err := client.GetSMSStatus(ctx, query)
    if err != nil {
        log.Printf("查询失败: %v", err)
        return
    }

    // 遍历查询结果
    for _, status := range result.Data {
        fmt.Printf("手机号: %s, 状态: %s, 送达时间: %s\n",
            status.PhoneNumber,
            status.Status,
            status.DeliverTime.Format("2006-01-02 15:04:05"))
    }
}
```

> **注意**：阿里云SMS查询限制
> - 只能查询最近30天内的短信
> - `SendDate` 参数格式为 `yyyyMMdd`（如：20240101）
> - `PageSize` 范围为 1-50
> - `CurrentPage` 从 1 开始

### 5️⃣ 验证手机号

```go
package main

import (
    "context"
    "fmt"
    "github.com/18721889353/sunshine/pkg/gosms"
)

func main() {
    ctx := context.Background()

    // 验证国内手机号
    valid := gosms.ValidatePhoneNumber(ctx, "+8613711112222")
    fmt.Printf("手机号是否有效: %v\n", valid) // true

    // 格式化手机号
    formatted := gosms.FormatPhoneNumber("13711112222")
    fmt.Printf("格式化后: %s\n", formatted) // +8613711112222
}
```

<br>

## 📋 API 参考

### 核心接口

```go
type SMSClient interface {
    // SendSMS 发送短信
    SendSMS(ctx context.Context, req *SendRequest) (*SendResult, error)
    
    // SendBatchSMS 批量发送短信
    SendBatchSMS(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error)
    
    // GetSMSStatus 查询短信发送状态
    GetSMSStatus(ctx context.Context, query *SMSStatusQuery) (*SMSStatusResult, error)
    
    // GetProviderType 获取提供商类型
    GetProviderType() ProviderType
}
```

### 配置结构

```go
type Config struct {
    ProviderType ProviderType // 提供商类型
    Region       string       // 区域
    AccessKeyID  string       // Access Key ID / Secret ID
    SecretKey    string       // Secret Key
    TencentAppID string       // 腾讯云短信应用ID（腾讯云专用）
    AliyunSignName string     // 阿里云默认签名（阿里云专用）
}
```

### 发送请求

```go
type SendRequest struct {
    PhoneNumbers   []string            // 手机号列表（国际格式）
    TemplateID     string              // 模板ID
    TemplateParams map[string]string   // 模板参数
    SignName       string              // 签名名称
    Tags           map[string]string   // 标签
}
```

### 发送结果

```go
type SendResult struct {
    MessageID   string                 // 消息ID
    PhoneNumber string                 // 手机号
    Status      string                 // 状态: success/failed
    Error       error                  // 错误信息
    Extra       map[string]interface{} // 额外信息
}
```

<br>

## 💡 最佳实践

### 1. 使用环境变量存储密钥

```go
cfg := &gosms.Config{
    ProviderType: gosms.ProviderTypeTencentSMS,
    AccessKeyID:  os.Getenv("TENCENT_SECRET_ID"),
    SecretKey:    os.Getenv("TENCENT_SECRET_KEY"),
    TencentAppID: os.Getenv("TENCENT_SMS_APP_ID"),
}
```

### 2. 手机号格式化

```go
// 自动添加国际区号
phone := gosms.FormatPhoneNumber("13711112222") // +8613711112222
```

### 3. 错误处理

```go
result, err := client.SendSMS(ctx, req)
if err != nil {
    // 记录错误日志
    logger.Error("短信发送失败", logger.Err(err))
    return err
}

if result.Status == gosms.StatusFailed {
    // 处理业务失败
    logger.Warn("短信发送业务失败", 
        logger.String("message_id", result.MessageID),
        logger.Any("extra", result.Extra))
}
```

### 4. 批量发送优化

```go
// 分批发送，避免单次过多
batchSize := 100
for i := 0; i < len(phoneNumbers); i += batchSize {
    end := i + batchSize
    if end > len(phoneNumbers) {
        end = len(phoneNumbers)
    }
    
    batch := phoneNumbers[i:end]
    // 发送当前批次
    // ...
}
```

<br>

## ⚠️ 注意事项

### 腾讯云SMS前置条件

使用腾讯云SMS前必须完成：
1. ✅ 在腾讯云控制台创建短信应用
2. ✅ 申请并审核通过短信签名
3. ✅ 创建并审核通过短信模板
4. ✅ 获取正确的 SecretId 和 SecretKey
5. ✅ 确保账户余额充足

### 阿里云SMS前置条件

使用阿里云SMS前必须完成：
1. ✅ 开通阿里云短信服务
2. ✅ 申请并审核通过短信签名
3. ✅ 创建并审核通过短信模板
4. ✅ 获取正确的 AccessKey ID 和 Secret
5. ✅ 确保账户余额充足

### 手机号格式要求

- 必须使用国际格式：`+国家码手机号`
- 中国手机号示例：`+8613711112222`
- 可使用 `FormatPhoneNumber()` 函数自动格式化

<br>

## 🔄 与 PHP 版本对比

| 功能 | PHP版本 | Go版本 |
|------|---------|--------|
| 发送短信 | ✅ | ✅ |
| 批量发送 | ❌ | ✅ |
| 状态查询 | ✅ | ✅ |
| 手机号验证 | ❌ | ✅ |
| 链路追踪 | ❌ | ✅ |
| 多平台支持 | 仅腾讯 | 腾讯+阿里 |
| 统一接口 | ❌ | ✅ |

<br>

## 📄 许可证

MIT License
