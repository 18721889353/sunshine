package gosms

import (
	"context"
	"testing"
)

// TestNewSMSClient 测试创建客户端
func TestNewSMSClient(t *testing.T) {
	t.Parallel()
	// 测试创建腾讯云客户端
	cfg := &Config{
		ProviderType: ProviderTypeTencentSMS,
		Region:       "ap-shanghai",
		AccessKeyID:  "test-key-id",
		SecretKey:    "test-secret-key",
		TencentAppID: "1400000000",
	}

	client, err := NewSMSClient(cfg)
	if err != nil {
		t.Fatalf("创建腾讯云客户端失败: %v", err)
	}

	if client == nil {
		t.Fatal("客户端不应为 nil")
	}

	if client.GetProviderType() != ProviderTypeTencentSMS {
		t.Errorf("期望提供商类型为 %s, 实际为 %s", ProviderTypeTencentSMS, client.GetProviderType())
	}
}

// TestFormatPhoneNumber 测试手机号格式化
func TestFormatPhoneNumber(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "国内手机号不带+86",
			input:    "13711112222",
			expected: "+8613711112222",
		},
		{
			name:     "国内手机号已带+86",
			input:    "+8613711112222",
			expected: "+8613711112222",
		},
		{
			name:     "国际手机号",
			input:    "+1234567890",
			expected: "+1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatPhoneNumber(tt.input)
			if result != tt.expected {
				t.Errorf("期望 %s, 实际得到 %s", tt.expected, result)
			}
		})
	}
}

// TestValidatePhoneNumber 测试手机号验证
func TestValidatePhoneNumber(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name     string
		phone    string
		expected bool
	}{
		{
			name:     "有效的国内手机号",
			phone:    "+8613711112222",
			expected: true,
		},
		{
			name:     "无效的手机号（太短）",
			phone:    "+86137",
			expected: false,
		},
		{
			name:     "无效的手机号（无国家码）",
			phone:    "13711112222",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidatePhoneNumber(ctx, tt.phone)
			if result != tt.expected {
				t.Errorf("手机号 %s: 期望 %v, 实际得到 %v", tt.phone, tt.expected, result)
			}
		})
	}
}

// TestSendRequestValidation 测试发送请求验证
func TestSendRequestValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		req     *SendRequest
		wantErr bool
	}{
		{
			name: "有效请求",
			req: &SendRequest{
				PhoneNumbers: []string{"+8613711112222"},
				TemplateID:   "123456",
				SignName:     "测试签名",
			},
			wantErr: false,
		},
		{
			name: "缺少手机号",
			req: &SendRequest{
				PhoneNumbers: []string{},
				TemplateID:   "123456",
				SignName:     "测试签名",
			},
			wantErr: true,
		},
		{
			name: "缺少模板ID",
			req: &SendRequest{
				PhoneNumbers: []string{"+8613711112222"},
				TemplateID:   "",
				SignName:     "测试签名",
			},
			wantErr: true,
		},
		{
			name: "缺少签名",
			req: &SendRequest{
				PhoneNumbers: []string{"+8613711112222"},
				TemplateID:   "123456",
				SignName:     "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
