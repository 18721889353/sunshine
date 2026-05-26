package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/gosms"
)

func main() {
	fmt.Println("=== 腾讯云SMS 真实短信发送与状态查询测试 ===")
	fmt.Println()

	// 从.env文件或环境变量加载配置
	secretID, secretKey, appID, signName, templateID, phoneNumber, err := LoadConfig()
	if err != nil {
		log.Printf("⚠️  加载配置文件失败: %v\n", err)
	}

	// 验证必要配置
	if secretID == "" || secretKey == "" || appID == "" {
		panic("未设置腾讯云的SecretID、SecretKey或AppID")
	}

	if signName == "" || templateID == "" {
		panic("未设置短信签名或模板ID")
	}

	fmt.Println("✅ 已从配置文件或环境变量加载配置")
	fmt.Println()

	// 使用配置创建客户端
	cfg := &gosms.Config{
		ProviderType: gosms.ProviderTypeTencentSMS,
		Region:       "ap-guangzhou", // 广州区域（腾讯云短信推荐使用广州地域）
		AccessKeyID:  secretID,
		SecretKey:    secretKey,
		TencentAppID: appID,
	}

	fmt.Println("🔧 配置信息:")
	fmt.Printf("   Provider: %s\n", cfg.ProviderType)
	fmt.Printf("   Region: %s\n", cfg.Region)
	fmt.Printf("   AppID: %s\n", cfg.TencentAppID)
	fmt.Printf("   SecretID: %s...\n", cfg.AccessKeyID[:10])
	fmt.Printf("   签名: %s\n", signName)
	fmt.Printf("   模板ID: %s\n", templateID)
	fmt.Printf("   测试手机号: %s\n", phoneNumber)
	fmt.Println()

	client, err := gosms.NewSMSClient(cfg)
	if err != nil {
		log.Printf("❌ 创建客户端失败: %v\n", err)
		return
	}

	fmt.Println("✅ 客户端创建成功")
	fmt.Println()

	// 步骤1: 发送短信
	fmt.Println("📤 步骤1: 发送验证码短信...")
	fmt.Printf("   手机号: %s\n", phoneNumber)
	fmt.Printf("   签名: %s\n", signName)
	fmt.Printf("   模板ID: %s\n", templateID)
	fmt.Println()

	// 生成随机验证码（6位数字）
	verifyCode := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	fmt.Printf("🔢 生成的验证码: %s\n", verifyCode)
	fmt.Println()

	ctx := context.Background()

	// 准备发送请求
	req := &gosms.SendRequest{
		PhoneNumbers: []string{phoneNumber},
		TemplateID:   templateID,
		SignName:     signName,
		TemplateParams: map[string]string{
			"1": verifyCode, // 腾讯云模板参数使用数字索引，如 "1", "2"
		},
	}

	result, err := client.SendSMS(ctx, req)
	if err != nil {
		log.Printf("❌ 发送短信失败: %v\n", err)
		log.Printf("💡 提示: 请检查以下几点:\n")
		log.Printf("   1. SecretID 和 SecretKey 是否正确\n")
		log.Printf("   2. AppID 是否正确\n")
		log.Printf("   3. 签名 '%s' 是否已在腾讯云SMS中审核通过\n", signName)
		log.Printf("   4. 模板ID '%s' 是否存在且可用\n", templateID)
		log.Printf("   5. API密钥是否有SMS服务权限\n")
		log.Printf("   6. 手机号格式是否正确（需要国际格式，如 +8613711112222）\n")
		return
	}

	fmt.Printf("✅ 短信发送成功!\n")
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
		if serialNo, ok := result.Extra["SerialNo"]; ok {
			fmt.Printf("   序列号: %v\n", serialNo)
		}
	}
	fmt.Println()

	// 等待几秒让短信系统处理
	fmt.Println("⏳ 等待短信系统处理...")
	time.Sleep(5 * time.Second)

	// 步骤2: 轮询查询短信发送状态
	fmt.Println("🔍 步骤2: 轮询查询短信发送状态...")
	fmt.Println("   将每5秒查询一次，最多查询6次（30秒）")
	fmt.Println()

	// 轮询参数配置
	maxRetries := 6                  // 最大重试次数
	retryInterval := 5 * time.Second // 重试间隔

	startQueryTime := time.Now()
	queryCount := 0
	smsDelivered := false
	var lastStatusResult *gosms.SMSStatusResult

	// 轮询查询
	for queryCount < maxRetries {
		queryCount++
		elapsed := time.Since(startQueryTime)

		fmt.Printf("🔄 第 %d/%d 次查询 (已耗时: %.1f秒)...\n", queryCount, maxRetries, elapsed.Seconds())

		// 构建查询请求
		query := &gosms.SMSStatusQuery{
			MessageID:   result.MessageID,
			PhoneNumber: phoneNumber,
			FromDate:    time.Now().Add(-24 * time.Hour), // 查询最近24小时
			ToDate:      time.Now(),
			Offset:      0,
			Limit:       10,
		}

		// 查询状态
		statusResult, err := client.GetSMSStatus(ctx, query)
		if err != nil {
			fmt.Printf("   ❌ 查询失败: %v\n", err)
			fmt.Printf("    继续重试...\n\n")
			time.Sleep(retryInterval)
			continue
		}

		lastStatusResult = statusResult
		fmt.Printf("   ✅ 查询成功 (状态: %s)\n", statusResult.Status)

		// 检查是否有短信状态记录
		if len(statusResult.Data) == 0 {
			fmt.Printf("    未找到状态记录，短信可能还在处理中...\n\n")
			time.Sleep(retryInterval)
			continue
		}

		// 显示短信状态
		for i, status := range statusResult.Data {
			fmt.Printf("   📱 短信[%d]:\n", i+1)
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

			// 检查是否已送达
			if status.Status == gosms.StatusDelivered || status.Status == gosms.StatusSuccess {
				smsDelivered = true
				fmt.Printf("      ✅ 短信已成功送达！\n")
			} else if status.Status == gosms.StatusFailed {
				fmt.Printf("      ❌ 短信发送失败\n")
			} else {
				fmt.Printf("      ⏳ 短信仍在处理中...\n")
			}
		}

		// 如果短信已送达或失败，停止轮询
		if smsDelivered {
			fmt.Printf("\n🎉 短信已成功送达，停止轮询！\n")
			break
		}

		// 如果还有重试次数，等待后继续
		if queryCount < maxRetries {
			fmt.Printf("   ⏳ 等待 %v 后继续查询...\n\n", retryInterval)
			time.Sleep(retryInterval)
		}
	}

	// 轮询结束，输出总结
	fmt.Println()
	fmt.Println("=== 轮询查询总结 ===")
	fmt.Printf("🔢 查询次数: %d 次\n", queryCount)
	fmt.Printf("⏱️  总耗时: %.1f 秒\n", time.Since(startQueryTime).Seconds())
	fmt.Printf(" 消息ID: %s\n", result.MessageID)

	if smsDelivered {
		fmt.Println("✅ 最终状态: 短信已成功送达")
	} else if lastStatusResult != nil && len(lastStatusResult.Data) > 0 {
		lastStatus := lastStatusResult.Data[0]
		fmt.Printf(" 最终状态: %s (%s)\n", lastStatus.Status, lastStatus.StatusMessage)
		fmt.Println("💡 提示: 短信可能仍在投递中，请稍后再次查询")
	} else {
		fmt.Println("⚠️  最终状态: 未找到短信状态记录")
		fmt.Println("💡 提示: 可能需要更长时间才能查询到状态")
	}

	fmt.Println()
	fmt.Println(" 注意事项:")
	fmt.Println("   1. 请确认手机是否收到验证码短信")
	fmt.Println("   2. 如果未收到，请检查腾讯云SMS控制台的状态")
	fmt.Println("   3. 测试完成后请及时清理测试数据")
	fmt.Println("   4. 短信状态可能有延迟，建议稍后再次查询")
}
