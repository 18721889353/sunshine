package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/gosms"
)

func main() {
	fmt.Println("=== 腾讯云SMS 真实短信发送与状态查询测试 ===")
	fmt.Println()

	// 加载配置
	cfg, err := LoadConfig()
	if err != nil {
		log.Printf(" 加载配置文件失败: %v\n", err)
	}

	// 验证配置
	if err := validateConfig(cfg); err != nil {
		panic(err)
	}

	fmt.Println(" 已从配置文件或环境变量加载配置")
	fmt.Println()

	// 创建客户端
	client := createSMSClient(cfg)

	// 执行测试
	ctx := context.Background()
	result := sendSMS(ctx, client, cfg)
	pollSMSStatus(ctx, client, result, cfg.PhoneNumber)

	fmt.Println()
	fmt.Println(" 注意事项:")
	fmt.Println("   1. 请确认手机是否收到验证码短信")
	fmt.Println("   2. 如果未收到，请检查腾讯云SMS控制台的状态")
	fmt.Println("   3. 测试完成后请及时清理测试数据")
	fmt.Println("   4. 短信状态可能有延迟，建议稍后再次查询")
}

// validateConfig 验证配置
func validateConfig(cfg *Config) error {
	if cfg.SecretID == "" || cfg.SecretKey == "" || cfg.AppID == "" {
		return fmt.Errorf("未设置腾讯云的SecretID、SecretKey或AppID")
	}
	if cfg.SignName == "" || cfg.TemplateID == "" {
		return fmt.Errorf("未设置短信签名或模板ID")
	}
	return nil
}

// createSMSClient 创建短信客户端
func createSMSClient(cfg *Config) gosms.SMSClient {
	smsCfg := &gosms.Config{
		ProviderType: gosms.ProviderTypeTencentSMS,
		Region:       "ap-guangzhou",
		AccessKeyID:  cfg.SecretID,
		SecretKey:    cfg.SecretKey,
		TencentAppID: cfg.AppID,
	}

	fmt.Println(" 配置信息:")
	fmt.Printf("   Provider: %s\n", smsCfg.ProviderType)
	fmt.Printf("   Region: %s\n", smsCfg.Region)
	fmt.Printf("   AppID: %s\n", smsCfg.TencentAppID)
	fmt.Printf("   SecretID: %s...\n", smsCfg.AccessKeyID[:10])
	fmt.Printf("   签名: %s\n", cfg.SignName)
	fmt.Printf("   模板ID: %s\n", cfg.TemplateID)
	fmt.Printf("   测试手机号: %s\n", cfg.PhoneNumber)
	fmt.Println()

	client, err := gosms.NewSMSClient(smsCfg)
	if err != nil {
		log.Printf(" 创建客户端失败: %v\n", err)
		panic("创建客户端失败")
	}

	fmt.Println(" 客户端创建成功")
	fmt.Println()
	return client
}

// sendSMS 发送短信
func sendSMS(ctx context.Context, client gosms.SMSClient, cfg *Config) *gosms.SendResult {
	fmt.Println(" 步骤1: 发送验证码短信...")
	fmt.Printf("   手机号: %s\n", cfg.PhoneNumber)
	fmt.Printf("   签名: %s\n", cfg.SignName)
	fmt.Printf("   模板ID: %s\n", cfg.TemplateID)
	fmt.Println()

	// 生成验证码
	verifyCode := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	fmt.Printf(" 生成的验证码: %s\n", verifyCode)
	fmt.Println()

	// 准备请求
	req := &gosms.SendRequest{
		PhoneNumbers: []string{cfg.PhoneNumber},
		TemplateID:   cfg.TemplateID,
		SignName:     cfg.SignName,
		TemplateParams: map[string]string{
			"1": verifyCode,
			"2": time.Now().Format(time.DateTime),
		},
	}

	// 发送短信
	result, err := client.SendSMS(ctx, req)
	if err != nil {
		log.Printf(" 发送短信失败: %v\n", err)
		log.Printf(" 提示: 请检查以下几点:\n")
		log.Printf("   1. SecretID 和 SecretKey 是否正确\n")
		log.Printf("   2. AppID 是否正确\n")
		log.Printf("   3. 签名 '%s' 是否已在腾讯云SMS中审核通过\n", cfg.SignName)
		log.Printf("   4. 模板ID '%s' 是否存在且可用\n", cfg.TemplateID)
		log.Printf("   5. API密钥是否有SMS服务权限\n")
		log.Printf("   6. 手机号格式是否正确（需要国际格式，如 +8613711112222）\n")
		panic("发送短信失败")
	}

	fmt.Printf(" 短信发送成功!\n")
	fmt.Printf("   消息ID: %s\n", result.MessageID)
	fmt.Printf("   手机号: %s\n", result.PhoneNumber)
	fmt.Printf("   状态: %s\n", result.Status)
	if result.Extra != nil {
		if code, ok := result.Extra["Code"]; ok {
			fmt.Printf("   返回码: %v\n", code)
		}
		if message, ok := result.Extra["Message"]; ok {
			fmt.Printf("   返回消息: %v\n", message)
		}
	}
	marshal, err := json.Marshal(result)
	if err != nil {
		log.Printf(" 序列化结果失败: %v\n", err)
	}
	fmt.Printf("   完整结果: %s\n", string(marshal))
	fmt.Println()

	return result
}

// pollSMSStatus 轮询查询短信状态
func pollSMSStatus(ctx context.Context, client gosms.SMSClient, result *gosms.SendResult, phoneNumber string) {
	fmt.Println(" 步骤2: 轮询查询短信发送状态...")
	fmt.Println("   将每5秒查询一次，最多查询6次（30秒）")
	fmt.Println()
	time.Sleep(time.Second * 5)

	maxRetries := 6
	retryInterval := 5 * time.Second
	startQueryTime := time.Now()
	queryCount := 0
	smsDelivered := false
	var lastStatusResult *gosms.SMSStatusResult

	// 轮询查询
	for queryCount < maxRetries {
		queryCount++
		elapsed := time.Since(startQueryTime)

		fmt.Printf(" 第 %d/%d 次查询 (已耗时: %.1f秒)...\n", queryCount, maxRetries, elapsed.Seconds())

		// 查询状态
		statusResult := queryStatus(ctx, client, result.MessageID, phoneNumber)
		if statusResult == nil {
			time.Sleep(retryInterval)
			continue
		}
		marshal, err := json.Marshal(statusResult)
		if err != nil {
			log.Printf(" 序列化结果失败: %v\n", err)
		}
		fmt.Printf("   完整结果: %s\n", string(marshal))

		lastStatusResult = statusResult
		fmt.Printf("   查询成功 (状态: %s)\n", statusResult.Status)

		// 检查状态
		if len(statusResult.Data) == 0 {
			fmt.Printf("    未找到状态记录，短信可能还在处理中...\n\n")
			time.Sleep(retryInterval)
			continue
		}

		// 显示状态
		displayStatus(statusResult.Data)

		// 检查是否已送达
		if checkDeliveryStatus(statusResult.Data) {
			smsDelivered = true
			fmt.Printf("\n 短信已成功送达，停止轮询！\n")
			break
		}

		// 等待后继续
		if queryCount < maxRetries {
			fmt.Printf("    等待 %v 后继续查询...\n\n", retryInterval)
			time.Sleep(retryInterval)
		}
	}

	// 输出总结
	printQuerySummary(queryCount, startQueryTime, result.MessageID, smsDelivered, lastStatusResult)
}

// queryStatus 查询短信状态
func queryStatus(ctx context.Context, client gosms.SMSClient, messageID, phoneNumber string) *gosms.SMSStatusResult {
	query := &gosms.SMSStatusQuery{
		MessageID:   messageID,
		PhoneNumber: phoneNumber,
		FromDate:    time.Now().Add(-24 * time.Hour),
		ToDate:      time.Now(),
		Offset:      0,
		Limit:       10,
	}

	statusResult, err := client.GetSMSStatus(ctx, query)
	if err != nil {
		fmt.Printf("    查询失败: %v\n", err)
		fmt.Printf("    继续重试...\n\n")
		return nil
	}

	return statusResult
}

// displayStatus 显示短信状态
func displayStatus(data []*gosms.SMSStatus) {
	for i, status := range data {
		fmt.Printf("   短信[%d]:\n", i+1)
		fmt.Printf("      消息ID: %s\n", status.MessageID)
		fmt.Printf("      手机号: %s\n", status.PhoneNumber)
		fmt.Printf("      状态: %s (%s)\n", status.Status, status.StatusMessage)
		fmt.Printf("      状态码: %d\n", status.StatusCode)

		if !status.DeliverTime.IsZero() {
			fmt.Printf("      送达时间: %s\n", status.DeliverTime.Format("2006-01-02 15:04:05"))
		}

		if status.Extra != nil {
			if reportStatus, ok := status.Extra["ReportStatus"]; ok {
				fmt.Printf("      回执状态: %v\n", reportStatus)
			}
			if description, ok := status.Extra["Description"]; ok {
				fmt.Printf("      描述: %v\n", description)
			}
		}
	}
}

// checkDeliveryStatus 检查投递状态
func checkDeliveryStatus(data []*gosms.SMSStatus) bool {
	for _, status := range data {
		if status.Status == gosms.StatusDelivered || status.Status == gosms.StatusSuccess {
			fmt.Printf("      短信已成功送达！\n")
			return true
		} else if status.Status == gosms.StatusFailed {
			fmt.Printf("      短信发送失败\n")
		} else {
			fmt.Printf("      短信仍在处理中...\n")
		}
	}
	return false
}

// printQuerySummary 打印查询总结
func printQuerySummary(queryCount int, startTime time.Time, messageID string, delivered bool, lastResult *gosms.SMSStatusResult) {
	fmt.Println()
	fmt.Println("=== 轮询查询总结 ===")
	fmt.Printf(" 查询次数: %d 次\n", queryCount)
	fmt.Printf(" 总耗时: %.1f 秒\n", time.Since(startTime).Seconds())
	fmt.Printf(" 消息ID: %s\n", messageID)

	if delivered {
		fmt.Println(" 最终状态: 短信已成功送达")
	} else if lastResult != nil && len(lastResult.Data) > 0 {
		lastStatus := lastResult.Data[0]
		fmt.Printf(" 最终状态: %s (%s)\n", lastStatus.Status, lastStatus.StatusMessage)
		fmt.Println(" 提示: 短信可能仍在投递中，请稍后再次查询")
	} else {
		fmt.Println(" 最终状态: 未找到短信状态记录")
		fmt.Println(" 提示: 可能需要更长时间才能查询到状态")
	}
}
