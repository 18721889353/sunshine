package gosms

import "testing"

// tencentTestConfig 返回腾讯云测试配置。
// newTencentSMSClient 只构建 SDK 客户端对象，不发起任何网络调用，可在单测中直接创建。
func tencentTestConfig() *Config {
	return &Config{
		ProviderType:    ProviderTypeTencentSMS,
		Region:          "ap-shanghai",
		AccessKeyID:     "test-secret-id",
		SecretKey:       "test-secret-key",
		TencentAppID:    "1400000000",
		TencentSignName: "测试签名",
	}
}

// mustTencentClient 创建腾讯云客户端供单测使用（失败即 Fatal）。
func mustTencentClient(t *testing.T) *TencentSMSClient {
	t.Helper()
	client, err := newTencentSMSClient(tencentTestConfig())
	if err != nil {
		t.Fatalf("创建腾讯云测试客户端失败: %v", err)
	}
	return client
}

// aliyunTestConfig 返回阿里云测试配置（同样不发起网络调用）。
func aliyunTestConfig() *Config {
	return &Config{
		ProviderType:   ProviderTypeAliyunSMS,
		Region:         "cn-hangzhou",
		AccessKeyID:    "test-access-key-id",
		SecretKey:      "test-access-key-secret",
		AliyunSignName: "测试签名",
	}
}

// mustAliyunClient 创建阿里云客户端供单测使用（失败即 Fatal）。
func mustAliyunClient(t *testing.T) *AliyunSMSClient {
	t.Helper()
	client, err := newAliyunSMSClient(aliyunTestConfig())
	if err != nil {
		t.Fatalf("创建阿里云测试客户端失败: %v", err)
	}
	return client
}

// validSendRequest 返回一个可通过 Validate 的合法单号码请求。
func validSendRequest() *SendRequest {
	return &SendRequest{
		PhoneNumbers: []string{"+8613711112222"},
		TemplateID:   "1234567",
		SignName:     "测试签名",
		TemplateParams: []TemplateParam{
			{Key: "1", Value: "123456"},
		},
	}
}
