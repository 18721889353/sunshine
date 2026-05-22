package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/goemail"
)

func main() {
	fmt.Println("=== 腾讯云SES 真实邮件发送与状态查询测试 ===")
	fmt.Println()

	// 从.env文件或环境变量加载配置
	accessKeyID, secretKey, err := LoadConfig()
	if err != nil {
		log.Printf("⚠️  加载配置文件失败: %v\n", err)
	}

	// 如果未设置，使用硬编码配置（仅用于测试）
	if accessKeyID == "" || secretKey == "" {
		panic("未设置腾讯云SES的AccessKeyID和SecretKey")
	}

	fmt.Println("✅ 已从配置文件或环境变量加载配置")

	// 使用配置创建客户端
	cfg := &goemail.Config{
		ProviderType: goemail.ProviderTypeTencentSES,
		Region:       "ap-hongkong", // 广州区域（腾讯云SES主要区域）
		AccessKeyID:  accessKeyID,
		SecretKey:    secretKey,
	}

	fmt.Println("🔧 配置信息:")
	fmt.Printf("   Provider: %s\n", cfg.ProviderType)
	fmt.Printf("   Region: %s\n", cfg.Region)
	fmt.Printf("   AccessKeyID: %s...\n", cfg.AccessKeyID[:10])
	fmt.Println()

	client, err := goemail.NewEmailClient(cfg)
	if err != nil {
		log.Printf("❌ 创建客户端失败: %v\n", err)
		return
	}

	fmt.Println("✅ 客户端创建成功")
	fmt.Println()

	// 步骤1: 使用模板发送邮件
	fmt.Println("📤 步骤1: 使用模板发送邮件...")
	fmt.Println("   发件人: 江苏通卡数字科技有限公司 <card@info.tongkask.com>")
	fmt.Println("   收件人: 418406417@qq.com")
	fmt.Println("   模板ID: 15532")
	fmt.Println("   主题: 提卡通")
	fmt.Println()

	// 准备模板数据（参考实际业务场景）
	now := time.Now()
	templateData := map[string]interface{}{
		"orderSn":     "TK" + now.Format("20060102150405"), // 订单号
		"dealerName":  "测试经销商",                             // 经销商名称
		"downloadUrl": "https://example.com/download/card", // 下载链接（注意：模板中使用驼峰命名）
		"payTime":     now.Format("2006-01-02 15:04:05"),   // 支付时间
		"tip":         "感谢您的购买，请及时下载卡片",                    // 提示信息
	}

	// 步骤1: 使用模板发送邮件
	ctx := context.Background()

	// 由于 SendTemplateEmail 是腾讯云SES特有的方法，我们需要类型断言
	tencentClient, ok := client.(*goemail.TencentSESClient)
	if !ok {
		log.Printf("❌ 客户端类型转换失败\n")
		return
	}

	fmt.Println("⚠️  注意: 腾讯云SES需要使用已验证的域名和审核通过的模板")
	fmt.Println()

	result, err := tencentClient.SendTemplateEmail(
		ctx,
		"江苏通卡数字科技有限公司 <card@info.tongkask.com>", // 发件人
		[]string{"418406417@qq.com"},            // 收件人
		15532,                                   // 模板ID
		templateData,                            // 模板数据
		"提卡通知",                                  // 主题
	)

	if err != nil {
		log.Printf("❌ 发送邮件失败: %v\n", err)
		log.Printf("💡 提示: 请检查以下几点:\n")
		log.Printf("   1. AccessKeyID 和 SecretKey 是否正确\n")
		log.Printf("   2. 发件域名 card@info.tongkask.com 是否已在腾讯云SES中验证\n")
		log.Printf("   3. 模板ID 15532 是否存在且可用\n")
		log.Printf("   4. API密钥是否有SES服务权限\n")
		return
	}

	fmt.Printf("✅ 邮件发送成功!\n")
	fmt.Printf("   消息ID: %s\n", result.MessageID)
	fmt.Printf("   状态: %s\n", result.Status)
	if result.Extra != nil {
		if reqID, ok := result.Extra["request_id"]; ok {
			fmt.Printf("   请求ID: %v\n", reqID)
		}
	}
	fmt.Println()

	// 等待几秒让邮件系统处理
	fmt.Println("⏳ 等待邮件系统处理...")
	time.Sleep(5 * time.Second)

	// 步骤2: 查询邮件发送状态
	fmt.Println("🔍 步骤2: 查询邮件发送状态...")
	query := &goemail.EmailStatusQuery{
		MessageID: result.MessageID,
		ToDate:    time.Now(),
		FromDate:  time.Now().Add(-24 * time.Hour), // 查询最近24小时
		Offset:    0,                               // 必须设置偏移量
		Limit:     10,
	}

	statusResult, err := client.GetEmailStatus(ctx, query)
	if err != nil {
		log.Printf("❌ 查询邮件状态失败: %v\n", err)
		return
	}

	fmt.Printf("✅ 状态查询成功!\n")
	fmt.Printf("   查询状态: %s\n", statusResult.Status)
	if len(statusResult.Data) > 0 {
		for i, status := range statusResult.Data {
			fmt.Printf("   邮件[%d]:\n", i+1)
			fmt.Printf("     消息ID: %s\n", status.MessageID)
			fmt.Printf("     收件人: %s\n", status.ToAddress)
			fmt.Printf("     发件人: %s\n", status.FromAddress)
			fmt.Printf("     状态: %s (%s)\n", status.Status, status.StatusMessage)
			fmt.Printf("     状态码: %d\n", status.StatusCode)
			if !status.RequestTime.IsZero() {
				fmt.Printf("     请求时间: %s\n", status.RequestTime.Format("2006-01-02 15:04:05"))
			}
			if !status.DeliverTime.IsZero() {
				fmt.Printf("     投递时间: %s\n", status.DeliverTime.Format("2006-01-02 15:04:05"))
			}
			fmt.Printf("     已打开: %t\n", status.UserOpened)
			fmt.Printf("     已点击: %t\n", status.UserClicked)
		}
	} else {
		fmt.Println("   未找到相关邮件状态记录")
	}

	fmt.Println()
	fmt.Println("=== 测试完成 ===")
}
