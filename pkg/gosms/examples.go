package gosms

import (
	"context"
	"fmt"
	"time"
)

// ExampleSendTencentSMS 发送腾讯短信示例
func ExampleSendTencentSMS() {
	// 创建配置
	cfg := &Config{
		ProviderType: ProviderTypeTencentSMS,
		Region:       "ap-shanghai",
		AccessKeyID:  "your-secret-id",  // 从环境变量或配置中心获取
		SecretKey:    "your-secret-key", // 从环境变量或配置中心获取
		TencentAppID: "1400282665",      // 腾讯云短信应用ID
	}

	// 创建客户端
	client, err := NewSMSClient(cfg)
	if err != nil {
		fmt.Printf("创建客户端失败: %v\n", err)
		return
	}

	// 发送短信
	ctx := context.Background()
	req := &SendRequest{
		PhoneNumbers: []string{"+8613711112222"},
		TemplateID:   "1234567",      // 模板ID（需要在腾讯云控制台审核通过）
		SignName:     "江苏通卡数字科技有限公司", // 签名名称
		TemplateParams: map[string]string{
			"1": "123456", // 验证码
		},
	}

	result, err := client.SendSMS(ctx, req)
	if err != nil {
		fmt.Printf("发送失败: %v\n", err)
		return
	}

	fmt.Printf("发送结果: %+v\n", result)
}

// ExampleSendAliyunSMS 发送阿里短信示例
func ExampleSendAliyunSMS() {
	// 创建配置
	cfg := &Config{
		ProviderType: ProviderTypeAliyunSMS,
		Region:       "cn-hangzhou",
		AccessKeyID:  "your-access-key-id",     // 从环境变量或配置中心获取
		SecretKey:    "your-access-key-secret", // 从环境变量或配置中心获取
	}

	// 创建客户端
	client, err := NewSMSClient(cfg)
	if err != nil {
		fmt.Printf("创建客户端失败: %v\n", err)
		return
	}

	// 发送短信
	ctx := context.Background()
	req := &SendRequest{
		PhoneNumbers: []string{"+8613711112222"},
		TemplateID:   "SMS_123456789", // 模板ID（需要在阿里云控制台审核通过）
		SignName:     "江苏通卡数字科技有限公司",  // 签名名称
		TemplateParams: map[string]string{
			"code": "123456", // 验证码
		},
	}

	result, err := client.SendSMS(ctx, req)
	if err != nil {
		fmt.Printf("发送失败: %v\n", err)
		return
	}

	fmt.Printf("发送结果: %+v\n", result)
}

// ExampleQuerySMSStatus 查询短信状态示例
func ExampleQuerySMSStatus() {
	// 创建配置
	cfg := &Config{
		ProviderType: ProviderTypeTencentSMS,
		Region:       "ap-shanghai",
		AccessKeyID:  "your-secret-id",
		SecretKey:    "your-secret-key",
		TencentAppID: "1400282665",
	}

	// 创建客户端
	client, err := NewSMSClient(cfg)
	if err != nil {
		fmt.Printf("创建客户端失败: %v\n", err)
		return
	}

	// 查询状态
	ctx := context.Background()
	query := &SMSStatusQuery{
		PhoneNumber: "+8613711112222",
		FromDate:    time.Now().Add(-24 * time.Hour), // 查询最近24小时
		ToDate:      time.Now(),
		Offset:      0,
		Limit:       10,
	}

	result, err := client.GetSMSStatus(ctx, query)
	if err != nil {
		fmt.Printf("查询失败: %v\n", err)
		return
	}

	fmt.Printf("查询结果: %+v\n", result)
}

// ExampleBatchSendSMS 批量发送短信示例
func ExampleBatchSendSMS() {
	// 创建配置
	cfg := &Config{
		ProviderType: ProviderTypeTencentSMS,
		Region:       "ap-shanghai",
		AccessKeyID:  "your-secret-id",
		SecretKey:    "your-secret-key",
		TencentAppID: "1400282665",
	}

	// 创建客户端
	client, err := NewSMSClient(cfg)
	if err != nil {
		fmt.Printf("创建客户端失败: %v\n", err)
		return
	}

	// 准备批量请求
	ctx := context.Background()
	reqs := []*SendRequest{
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
		fmt.Printf("批量发送失败: %v\n", err)
		return
	}

	for i, result := range results {
		fmt.Printf("第%d条短信结果: %+v\n", i+1, result)
	}
}

// ExampleValidatePhone 验证手机号示例
func ExampleValidatePhone() {
	ctx := context.Background()

	// 验证国内手机号
	valid := ValidatePhoneNumber(ctx, "+8613711112222")
	fmt.Printf("手机号是否有效: %v\n", valid)

	// 格式化手机号
	formatted := FormatPhoneNumber("13711112222")
	fmt.Printf("格式化后: %s\n", formatted)
}
