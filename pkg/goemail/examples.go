package goemail

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

// Example_FromConfig 从配置文件初始化（推荐方式）
func Example_FromConfig() {
	// 1. 从项目配置中读取email配置
	// 假设你已经通过 config.Init() 初始化了配置
	// import "github.com/18721889353/sunshine/internal/config"
	
	// cfg := config.Get()
	// emailCfg := cfg.Email
	
	// 这里模拟配置数据
	emailCfg := struct {
		ProviderType string
		Region       string
		AccessKeyID  string
		SecretKey    string
		SMTP         struct {
			Host     string
			Port     int
			Username string
			Password string
		}
	}{
		ProviderType: "tencent_ses",
		Region:       "ap-guangzhou",
		AccessKeyID:  os.Getenv("EMAIL_AK"), // 从环境变量读取
		SecretKey:    os.Getenv("EMAIL_SK"),
		SMTP: struct {
			Host     string
			Port     int
			Username string
			Password string
		}{
			Host:     "smtp.qq.com",
			Port:     465,
			Username: "your_email@qq.com",
			Password: "your_auth_code",
		},
	}
	
	// 2. 创建邮件客户端配置
	cfg := &Config{
		ProviderType: ProviderType(emailCfg.ProviderType),
		Region:       emailCfg.Region,
		AccessKeyID:  emailCfg.AccessKeyID,
		SecretKey:    emailCfg.SecretKey,
	}
	
	// SMTP专用配置
	if emailCfg.ProviderType == "smtp" {
		cfg.SMTPHost = emailCfg.SMTP.Host
		cfg.SMTPPort = emailCfg.SMTP.Port
		cfg.SMTPUsername = emailCfg.SMTP.Username
		cfg.SMTPPassword = emailCfg.SMTP.Password
		cfg.UseTLS = true
	}
	
	// 3. 创建客户端
	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	
	// 4. 发送邮件
	req := &SendRequest{
		From:     "sender@example.com",
		To:       []string{"recipient@example.com"},
		Subject:  "测试邮件",
		HtmlBody: "<h1>Hello</h1><p>从配置初始化的邮件客户端</p>",
	}
	
	ctx := context.Background()
	result, err := client.SendEmail(ctx, req)
	if err != nil {
		log.Printf("Send failed: %v", err)
		return
	}
	
	fmt.Printf("Message ID: %s\n", result.MessageID)
	fmt.Printf("Status: %s\n", result.Status)
}

// Example_TencentSES 腾讯云SES使用示例
func Example_TencentSES() {
	// 从环境变量读取AK/SK（推荐）
	cfg := &Config{
		ProviderType: ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  os.Getenv("TENCENT_AK"),
		SecretKey:    os.Getenv("TENCENT_SK"),
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 2. 构建发送请求
	req := &SendRequest{
		From:     "sender@example.com",
		To:       []string{"recipient1@example.com", "recipient2@example.com"},
		Cc:       []string{"cc@example.com"},
		Subject:  "欢迎注册",
		HtmlBody: "<h1>欢迎加入</h1><p>感谢您的注册！</p>",
		TextBody: "欢迎加入\n感谢您的注册！",
		ReplyTo:  []string{"support@example.com"},
		Tags: map[string]string{
			"env":      "production",
			"type":     "welcome",
			"campaign": "2024-q1",
		},
	}

	// 3. 发送邮件
	ctx := context.Background()
	result, err := client.SendEmail(ctx, req)
	if err != nil {
		log.Printf("Send failed: %v", err)
		return
	}

	fmt.Printf("Message ID: %s\n", result.MessageID)
	fmt.Printf("Status: %s\n", result.Status)
}

// Example_AliyunDM 阿里云DM使用示例
func Example_AliyunDM() {
	// 从环境变量读取AK/SK
	cfg := &Config{
		ProviderType: ProviderTypeAliyunDM,
		Region:       "cn-hangzhou",
		AccessKeyID:  os.Getenv("ALIYUN_AK"),
		SecretKey:    os.Getenv("ALIYUN_SK"),
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 2. 构建发送请求
	req := &SendRequest{
		From:     "sender@example.com",
		To:       []string{"recipient@example.com"},
		Subject:  "订单通知",
		HtmlBody: "<h1>订单已发货</h1><p>您的订单已发货，请注意查收。</p>",
		Tags: map[string]string{
			"order_id": "123456",
			"type":     "notification",
		},
	}

	// 3. 发送邮件
	ctx := context.Background()
	result, err := client.SendEmail(ctx, req)
	if err != nil {
		log.Printf("Send failed: %v", err)
		return
	}

	fmt.Printf("Status: %s\n", result.Status)
	fmt.Printf("Env ID: %v\n", result.Extra["env_id"])
}

// Example_AliyunDM_Template 阿里云DM模板邮件示例
func Example_AliyunDM_Template() {
	// 1. 创建客户端
	cfg := &Config{
		ProviderType: ProviderTypeAliyunDM,
		Region:       "cn-hangzhou",
		AccessKeyID:  os.Getenv("ALIYUN_AK"),
		SecretKey:    os.Getenv("ALIYUN_SK"),
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 2. 转换为AliyunDMClient以使用模板功能
	aliyunClient, ok := client.(*AliyunDMClient)
	if !ok {
		log.Fatal("Client is not AliyunDMClient")
	}

	// 3. 准备模板数据
	// 假设在阿里云控制台创建了模板，模板内容类似：
	// 尊敬的${userName}，您的验证码是${code}，有效期${expiry}
	templateData := map[string]interface{}{
		"userName": "张三",
		"code":     "123456",
		"expiry":   "10分钟",
	}

	// 4. 发送模板邮件
	ctx := context.Background()
	result, err := aliyunClient.SendTemplateEmail(
		ctx,
		"noreply@example.com",           // 发件人
		[]string{"recipient@example.com"}, // 收件人
		"verification_code",              // 模板名称（在阿里云控制台创建）
		templateData,                     // 模板变量
	)
	if err != nil {
		log.Printf("Send template email failed: %v", err)
		return
	}

	fmt.Printf("Template email sent successfully!\n")
	fmt.Printf("Env ID: %v\n", result.Extra["env_id"])
	fmt.Printf("Template Name: %v\n", result.Extra["template_name"])
}

// Example_SMTP SMTP使用示例
func Example_SMTP() {
	// 从环境变量读取SMTP配置
	cfg := &Config{
		ProviderType: ProviderTypeSMTP,
		SMTPHost:     "smtp.qq.com",
		SMTPPort:     465,
		SMTPUsername: os.Getenv("SMTP_USERNAME"), // your_email@qq.com
		SMTPPassword: os.Getenv("SMTP_PASSWORD"), // QQ邮箱授权码
		UseTLS:       false,
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 2. 构建发送请求
	req := &SendRequest{
		From:     "your_email@qq.com",
		To:       []string{"recipient@example.com"},
		Cc:       []string{"cc@example.com"},
		Bcc:      []string{"bcc@example.com"},
		Subject:  "测试邮件",
		HtmlBody: "<h1>Hello</h1><p>这是一封测试邮件</p>",
		TextBody: "Hello\n这是一封测试邮件",
		Headers: map[string]string{
			"X-Priority": "1",
			"X-Mailer":   "GoEmail",
		},
	}

	// 3. 发送邮件
	ctx := context.Background()
	result, err := client.SendEmail(ctx, req)
	if err != nil {
		log.Printf("Send failed: %v", err)
		return
	}

	fmt.Printf("Message ID: %s\n", result.MessageID)
	fmt.Printf("Status: %s\n", result.Status)
}

// Example_BatchSend 批量发送示例
func Example_BatchSend() {
	cfg := &Config{
		ProviderType: ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  os.Getenv("TENCENT_AK"),
		SecretKey:    os.Getenv("TENCENT_SK"),
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 构建多个发送请求
	reqs := []*SendRequest{
		{
			From:     "sender@example.com",
			To:       []string{"user1@example.com"},
			Subject:  "通知1",
			HtmlBody: "<p>内容1</p>",
		},
		{
			From:     "sender@example.com",
			To:       []string{"user2@example.com"},
			Subject:  "通知2",
			HtmlBody: "<p>内容2</p>",
		},
		{
			From:     "sender@example.com",
			To:       []string{"user3@example.com"},
			Subject:  "通知3",
			HtmlBody: "<p>内容3</p>",
		},
	}

	// 批量发送
	ctx := context.Background()
	results, err := client.SendBatchEmail(ctx, reqs)
	if err != nil {
		log.Printf("Batch send failed: %v", err)
		return
	}

	// 处理结果
	for i, result := range results {
		if result.Status == "success" {
			fmt.Printf("Email %d sent successfully: %s\n", i+1, result.MessageID)
		} else {
			fmt.Printf("Email %d failed: %v\n", i+1, result.Error)
		}
	}
}

// Example_WithAttachment 带附件发送示例
func Example_WithAttachment() {
	cfg := &Config{
		ProviderType: ProviderTypeSMTP,
		SMTPHost:     "smtp.qq.com",
		SMTPPort:     465,
		SMTPUsername: os.Getenv("SMTP_USERNAME"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 读取附件内容（实际使用时从文件读取）
	attachmentContent := []byte("This is attachment content")

	req := &SendRequest{
		From:     "your_email@qq.com",
		To:       []string{"recipient@example.com"},
		Subject:  "带附件的邮件",
		HtmlBody: "<h1>请查收附件</h1>",
		Attachments: []*Attachment{
			{
				Filename:    "report.pdf",
				Content:     attachmentContent,
				ContentType: "application/pdf",
			},
			{
				Filename:    "data.csv",
				Content:     []byte("name,age\nJohn,30"),
				ContentType: "text/csv",
			},
		},
	}

	ctx := context.Background()
	result, err := client.SendEmail(ctx, req)
	if err != nil {
		log.Printf("Send failed: %v", err)
		return
	}

	fmt.Printf("Sent with attachments: %s\n", result.MessageID)
}

// Example_TemplateEmail 模板邮件示例（腾讯云SES）
func Example_TemplateEmail() {
	cfg := &Config{
		ProviderType: ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  os.Getenv("TENCENT_AK"),
		SecretKey:    os.Getenv("TENCENT_SK"),
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// 转换为TencentSESClient以使用模板功能
	tencentClient, ok := client.(*TencentSESClient)
	if !ok {
		log.Fatal("Client is not TencentSESClient")
	}

	// 模板数据（对应模板中的变量）
	// 假设模板中有 {{username}}、{{code}}、{{expiry}} 等变量
	templateData := map[string]interface{}{
		"username": "张三",
		"code":     "123456",
		"expiry":   "10分钟",
	}

	ctx := context.Background()
	result, err := tencentClient.SendTemplateEmail(
		ctx,
		"sender@example.com",           // 发件人
		[]string{"recipient@example.com"}, // 收件人
		12345,                          // 模板ID（在腾讯云控制台创建）
		templateData,                   // 模板数据
		"验证码",                        // 邮件主题
	)
	if err != nil {
		log.Printf("Send template email failed: %v", err)
		return
	}

	fmt.Printf("Template email sent successfully!\n")
	fmt.Printf("Message ID: %s\n", result.MessageID)
	fmt.Printf("Template ID: %v\n", result.Extra["template_id"])
}

// Example_ErrorHandling 错误处理示例
func Example_ErrorHandling() {
	cfg := &Config{
		ProviderType: ProviderTypeSMTP,
		SMTPHost:     "smtp.qq.com",
		SMTPPort:     465,
		SMTPUsername: os.Getenv("SMTP_USERNAME"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Printf("Create client error: %v", err)
		return
	}

	req := &SendRequest{
		From:     "test@qq.com",
		To:       []string{"invalid-email"}, // 无效的邮箱地址
		Subject:  "Test",
		HtmlBody: "<p>Test</p>",
	}

	ctx := context.Background()
	result, err := client.SendEmail(ctx, req)
	
	// 检查错误
	if err != nil {
		log.Printf("Send error: %v", err)
		
		// 可以根据错误类型进行不同处理
		if result != nil && result.Status == "failed" {
			log.Printf("Result status: %s", result.Status)
			log.Printf("Result error: %v", result.Error)
		}
		return
	}

	fmt.Println("Email sent successfully")
}

// Example_ContextTimeout 超时控制示例
func Example_ContextTimeout() {
	cfg := &Config{
		ProviderType: ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  os.Getenv("TENCENT_AK"),
		SecretKey:    os.Getenv("TENCENT_SK"),
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	req := &SendRequest{
		From:     "sender@example.com",
		To:       []string{"recipient@example.com"},
		Subject:  "Test",
		HtmlBody: "<p>Test</p>",
	}

	// 创建带超时的context
	ctx, cancel := context.WithTimeout(context.Background(), 30)
	defer cancel()

	result, err := client.SendEmail(ctx, req)
	if err != nil {
		log.Printf("Send failed or timeout: %v", err)
		return
	}

	fmt.Printf("Sent: %s\n", result.MessageID)
}

// Example_SendVerificationCode 发送验证码（完整实战案例）
func Example_SendVerificationCode() {
	// 1. 初始化客户端（建议全局单例）
	cfg := &Config{
		ProviderType: ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  os.Getenv("TENCENT_AK"),
		SecretKey:    os.Getenv("TENCENT_SK"),
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		log.Printf("Init client error: %v", err)
		return
	}

	tencentClient, ok := client.(*TencentSESClient)
	if !ok {
		log.Fatal("Not a Tencent SES client")
	}

	// 2. 模拟业务逻辑：生成验证码
	email := "user@example.com"
	code := "123456" // 实际应随机生成

	// 3. 准备模板数据
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
		"noreply@example.com",
		[]string{email},
		12345, // 模板ID，从腾讯云控制台获取
		templateData,
		"验证码",
	)

	if err != nil {
		log.Printf("Send verification code failed: %v", err)
		// 可以记录日志或重试
		return
	}

	log.Printf("Verification code sent to %s, MessageID: %s", email, result.MessageID)

	// 5. 将验证码存入Redis，设置过期时间
	// redis.Set(ctx, fmt.Sprintf("verify_code:%s", email), code, 10*time.Minute)
}
