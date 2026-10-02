package goemail

import "testing"

// 本文件为包内单测共享的构造与断言 helper（带 _test.go 后缀，不进产物）。
// 三个 new*Client 均只构建客户端对象、不发起任何网络调用，可在单测中直接创建。

// smtpTestConfig 返回 SMTP 测试配置。
func smtpTestConfig() *Config {
	return &Config{
		ProviderType: ProviderTypeSMTP,
		SMTPHost:     "smtp.example.com",
		SMTPPort:     465,
		SMTPUsername: "test@example.com",
		SMTPPassword: "test-password",
		UseTLS:       true,
	}
}

// mustSMTPClient 创建 SMTP 测试客户端（失败即 Fatal）。
func mustSMTPClient(t *testing.T) *SMTPClient {
	t.Helper()
	client, err := newSMTPClient(smtpTestConfig())
	if err != nil {
		t.Fatalf("创建 SMTP 测试客户端失败: %v", err)
	}
	return client
}

// tencentTestConfig 返回腾讯云 SES 测试配置。
func tencentTestConfig() *Config {
	return &Config{
		ProviderType: ProviderTypeTencentSES,
		Region:       "ap-guangzhou",
		AccessKeyID:  "test-access-key-id",
		SecretKey:    "test-secret-key",
	}
}

// mustTencentClient 创建腾讯云 SES 测试客户端（失败即 Fatal）。
func mustTencentClient(t *testing.T) *TencentSESClient {
	t.Helper()
	client, err := newTencentSESClient(tencentTestConfig())
	if err != nil {
		t.Fatalf("创建腾讯云 SES 测试客户端失败: %v", err)
	}
	return client
}

// aliyunTestConfig 返回阿里云 DM 测试配置。
func aliyunTestConfig() *Config {
	return &Config{
		ProviderType: ProviderTypeAliyunDM,
		Region:       "cn-hangzhou",
		AccessKeyID:  "test-access-key-id",
		SecretKey:    "test-access-key-secret",
	}
}

// mustAliyunClient 创建阿里云 DM 测试客户端（失败即 Fatal）。
func mustAliyunClient(t *testing.T) *AliyunDMClient {
	t.Helper()
	client, err := newAliyunDMClient(aliyunTestConfig())
	if err != nil {
		t.Fatalf("创建阿里云 DM 测试客户端失败: %v", err)
	}
	return client
}

// validSMTPRequest 返回一封可通过 validateRequest 的最小合法请求。
func validSMTPRequest() *SendRequest {
	return &SendRequest{
		From:     "sender@example.com",
		To:       []string{"receiver@example.com"},
		Subject:  "测试主题",
		HTMLBody: "<p>测试正文</p>",
	}
}

// assertNilInputSendGuard 断言 nil 入参防护的统一返回形态（终审 P0 回归守护）：
// error 非空、failed 结果非 nil、不带半截成功侧字段。三 provider 的
// SendEmail(nil) 共用，保证三家行为完全对称。
func assertNilInputSendGuard(t *testing.T, where string, result *SendResult, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: nil 入参必返回 error（且不得 panic）", where)
	}
	if result == nil {
		t.Fatalf("%s: 失败路径必返回非 nil result（batchErrors 依赖）", where)
	}
	if result.Status != StatusFailed {
		t.Errorf("%s: 状态应为 %s, 实际 %s", where, StatusFailed, result.Status)
	}
	if result.Error == nil {
		t.Errorf("%s: result.Error 应携带失败原因", where)
	}
	if result.MessageID != "" || result.Extra != nil {
		t.Errorf("%s: nil 入参失败不得返回半截成功侧字段: %+v", where, result)
	}
}
