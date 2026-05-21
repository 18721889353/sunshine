package goemail

import (
	"context"
	"testing"
)

// TestValidateEmail 测试邮箱验证
func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  bool
	}{
		{"valid email", "test@example.com", true},
		{"valid qq email", "123456@qq.com", true},
		{"valid 163 email", "user@163.com", true},
		{"invalid no @", "testexample.com", false},
		{"invalid no domain", "test@", false},
		{"invalid empty", "", false},
		{"invalid no dot in domain", "test@example", false},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateEmail(ctx, tt.email); got != tt.want {
				t.Errorf("ValidateEmail(%q) = %v, want %v", tt.email, got, tt.want)
			}
		})
	}
}

// TestValidateEmails 测试批量邮箱验证
func TestValidateEmails(t *testing.T) {
	emails := []string{
		"valid@example.com",
		"invalid",
		"another@test.com",
		"@bad.com",
	}

	ctx := context.Background()
	invalid := ValidateEmails(ctx, emails)
	if len(invalid) != 2 {
		t.Errorf("Expected 2 invalid emails, got %d: %v", len(invalid), invalid)
	}
}

// TestNewEmailClient 测试客户端创建
func TestNewEmailClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "smtp client",
			cfg: &Config{
				ProviderType: ProviderTypeSMTP,
				SMTPHost:     "smtp.qq.com",
				SMTPPort:     465,
				SMTPUsername: "test@qq.com",
				SMTPPassword: "password",
			},
			wantErr: false,
		},
		{
			name: "smtp missing host",
			cfg: &Config{
				ProviderType: ProviderTypeSMTP,
				SMTPUsername: "test@qq.com",
				SMTPPassword: "password",
			},
			wantErr: true,
		},
		{
			name: "unsupported provider",
			cfg: &Config{
				ProviderType: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEmailClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEmailClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSMTPClient_SendEmail 测试SMTP发送（需要真实配置）
func TestSMTPClient_SendEmail(t *testing.T) {
	// 跳过需要真实配置的测试
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	cfg := &Config{
		ProviderType: ProviderTypeSMTP,
		SMTPHost:     "smtp.qq.com",
		SMTPPort:     465,
		SMTPUsername: "your_email@qq.com", // 替换为真实邮箱
		SMTPPassword: "your_password",     // 替换为授权码
		UseTLS:       false,
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req := &SendRequest{
		From:     "your_email@qq.com",
		To:       []string{"recipient@example.com"},
		Subject:  "Test Email",
		HTMLBody: "<h1>Hello</h1><p>This is a test email</p>",
		TextBody: "Hello\nThis is a test email",
	}

	ctx := context.Background()
	result, err := client.SendEmail(ctx, req)
	if err != nil {
		t.Logf("Send email failed (expected if credentials are not configured): %v", err)
		return
	}

	if result.Status != "success" {
		t.Errorf("Expected success status, got %s", result.Status)
	}

	t.Logf("Message ID: %s", result.MessageID)
}

// TestTencentSESClient_SendEmail 测试腾讯云SES（需要真实配置）
func TestTencentSESClient_SendEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	cfg := &Config{
		ProviderType: ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  "your_access_key", // 替换为真实AK
		SecretKey:    "your_secret_key", // 替换为真实SK
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req := &SendRequest{
		From:     "sender@example.com",
		To:       []string{"recipient@example.com"},
		Subject:  "Test from Tencent SES",
		HTMLBody: "<h1>Hello from Tencent SES</h1>",
		Tags: map[string]string{
			"env":  "test",
			"type": "notification",
		},
	}

	ctx := context.Background()
	result, err := client.SendEmail(ctx, req)
	if err != nil {
		t.Logf("Send email failed (expected if credentials are not configured): %v", err)
		return
	}

	if result.Status != "success" {
		t.Errorf("Expected success status, got %s", result.Status)
	}

	t.Logf("Message ID: %s", result.MessageID)
	t.Logf("Extra: %+v", result.Extra)
}

// TestAliyunDMClient_SendEmail 测试阿里云DM（需要真实配置）
func TestAliyunDMClient_SendEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	cfg := &Config{
		ProviderType: ProviderTypeAliyunDM,
		Region:       "cn-hangzhou",
		AccessKeyID:  "your_access_key", // 替换为真实AK
		SecretKey:    "your_secret_key", // 替换为真实SK
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req := &SendRequest{
		From:     "sender@example.com",
		To:       []string{"recipient@example.com"},
		Subject:  "Test from Aliyun DM",
		HTMLBody: "<h1>Hello from Aliyun DM</h1>",
		Tags: map[string]string{
			"campaign": "test",
		},
	}

	ctx := context.Background()
	result, err := client.SendEmail(ctx, req)
	if err != nil {
		t.Logf("Send email failed (expected if credentials are not configured): %v", err)
		return
	}

	if result.Status != "success" {
		t.Errorf("Expected success status, got %s", result.Status)
	}

	t.Logf("Extra: %+v", result.Extra)
}

// TestSendBatchEmail 测试批量发送
func TestSendBatchEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	cfg := &Config{
		ProviderType: ProviderTypeSMTP,
		SMTPHost:     "smtp.qq.com",
		SMTPPort:     465,
		SMTPUsername: "test@qq.com",
		SMTPPassword: "password",
	}

	client, err := NewEmailClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	reqs := []*SendRequest{
		{
			From:     "test@qq.com",
			To:       []string{"user1@example.com"},
			Subject:  "Batch Email 1",
			HTMLBody: "<p>Email 1</p>",
		},
		{
			From:     "test@qq.com",
			To:       []string{"user2@example.com"},
			Subject:  "Batch Email 2",
			HTMLBody: "<p>Email 2</p>",
		},
	}

	ctx := context.Background()
	results, err := client.SendBatchEmail(ctx, reqs)
	if err != nil {
		t.Logf("Batch send failed (expected if credentials are not configured): %v", err)
		return
	}

	if len(results) != len(reqs) {
		t.Errorf("Expected %d results, got %d", len(reqs), len(results))
	}

	for i, result := range results {
		t.Logf("Result %d: Status=%s, MessageID=%s", i, result.Status, result.MessageID)
	}
}

// BenchmarkSMTPSendEmail SMTP发送性能基准测试
func BenchmarkSMTPSendEmail(b *testing.B) {
	cfg := &Config{
		ProviderType: ProviderTypeSMTP,
		SMTPHost:     "smtp.qq.com",
		SMTPPort:     465,
		SMTPUsername: "test@qq.com",
		SMTPPassword: "password",
	}

	client, _ := NewEmailClient(cfg)
	req := &SendRequest{
		From:     "test@qq.com",
		To:       []string{"recipient@example.com"},
		Subject:  "Benchmark Email",
		HTMLBody: "<p>Benchmark test</p>",
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.SendEmail(ctx, req)
	}
}

// ExampleNewEmailClient 创建邮件客户端示例
func ExampleNewEmailClient() {
	// 腾讯云SES
	tencentCfg := &Config{
		ProviderType: ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  "your_ak",
		SecretKey:    "your_sk",
	}
	_, _ = NewEmailClient(tencentCfg)

	// 阿里云DM
	aliyunCfg := &Config{
		ProviderType: ProviderTypeAliyunDM,
		Region:       "cn-hangzhou",
		AccessKeyID:  "your_ak",
		SecretKey:    "your_sk",
	}
	_, _ = NewEmailClient(aliyunCfg)

	// SMTP
	smtpCfg := &Config{
		ProviderType: ProviderTypeSMTP,
		SMTPHost:     "smtp.qq.com",
		SMTPPort:     465,
		SMTPUsername: "user@qq.com",
		SMTPPassword: "password",
	}
	_, _ = NewEmailClient(smtpCfg)
}
