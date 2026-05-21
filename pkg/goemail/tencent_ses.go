package goemail

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	ses "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ses/v20201002"
)

// TencentSESClient 腾讯云SES客户端
type TencentSESClient struct {
	client *ses.Client
	config *Config
}

// newTencentSESClient 创建腾讯云SES客户端
func newTencentSESClient(cfg *Config) (*TencentSESClient, error) {
	if cfg.AccessKeyID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("tencent SES requires AccessKeyID and SecretKey")
	}

	if cfg.Region == "" {
		cfg.Region = "ap-guangzhou" // 默认广州区域
	}

	// 创建凭证
	credential := common.NewCredential(
		cfg.AccessKeyID,
		cfg.SecretKey,
	)

	// 创建客户端配置
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "ses.tencentcloudapi.com"

	// 创建SES客户端
	client, err := ses.NewClient(credential, cfg.Region, cpf)
	if err != nil {
		return nil, fmt.Errorf("failed to create tencent SES client: %w", err)
	}

	return &TencentSESClient{
		client: client,
		config: cfg,
	}, nil
}

// SendEmail 发送邮件
func (c *TencentSESClient) SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 验证参数
	if err := c.validateRequest(req); err != nil {
		return &SendResult{
			Status: "failed",
			Error:  err,
		}, err
	}

	// 构建请求
	request := ses.NewSendEmailRequest()

	// 设置发件人
	request.FromEmailAddress = common.StringPtr(req.From)

	// 设置收件人（逗号分隔）
	toAddresses := make([]*string, len(req.To))
	for i, to := range req.To {
		toAddresses[i] = common.StringPtr(to)
	}
	request.Destination = toAddresses

	// 设置主题
	request.Subject = common.StringPtr(req.Subject)

	// 设置正文（优先使用HTML）
	bodyData := req.TextBody
	if req.HtmlBody != "" {
		bodyData = req.HtmlBody
	}
	request.Simple = &ses.Simple{
		Html: common.StringPtr(bodyData),
	}

	// 调用API
	response, err := c.client.SendEmail(request)
	if err != nil {
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("tencent SES send email failed: %w", err),
		}, err
	}

	// 解析结果
	result := &SendResult{
		MessageID: *response.Response.MessageId,
		Status:    "success",
		Extra: map[string]interface{}{
			"request_id": *response.Response.RequestId,
			"timestamp":  time.Now().Unix(),
		},
	}

	return result, nil
}

// SendBatchEmail 批量发送邮件
func (c *TencentSESClient) SendBatchEmail(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
	results := make([]*SendResult, len(reqs))
	
	for i, req := range reqs {
		result, err := c.SendEmail(ctx, req)
		results[i] = result
		if err != nil {
			// 继续处理其他邮件，不中断
			continue
		}
	}

	return results, nil
}

// GetProviderType 获取提供商类型
func (c *TencentSESClient) GetProviderType() ProviderType {
	return ProviderTypeTencentSES
}

// validateRequest 验证请求参数
func (c *TencentSESClient) validateRequest(req *SendRequest) error {
	if req.From == "" {
		return fmt.Errorf("from address is required")
	}

	if !ValidateEmail(req.From) {
		return fmt.Errorf("invalid from address: %s", req.From)
	}

	if len(req.To) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	// 验证所有收件人
	invalidTo := ValidateEmails(req.To)
	if len(invalidTo) > 0 {
		return fmt.Errorf("invalid recipient addresses: %v", invalidTo)
	}

	if req.Subject == "" {
		return fmt.Errorf("subject is required")
	}

	if req.HtmlBody == "" && req.TextBody == "" {
		return fmt.Errorf("either htmlBody or textBody is required")
	}

	return nil
}

// SendTemplateEmail 发送模板邮件（参考PHP案例实现）
func (c *TencentSESClient) SendTemplateEmail(ctx context.Context, from string, to []string, templateID uint64, templateData map[string]interface{}, subject string) (*SendResult, error) {
	// 验证参数
	if from == "" {
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("from address is required"),
		}, fmt.Errorf("from address is required")
	}

	if len(to) == 0 {
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("at least one recipient is required"),
		}, fmt.Errorf("at least one recipient is required")
	}

	if templateID == 0 {
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("template ID is required"),
		}, fmt.Errorf("template ID is required")
	}

	// 构建请求
	request := ses.NewSendEmailRequest()

	// 设置发件人
	request.FromEmailAddress = common.StringPtr(from)

	// 设置收件人
	toAddresses := make([]*string, len(to))
	for i, addr := range to {
		toAddresses[i] = common.StringPtr(addr)
	}
	request.Destination = toAddresses

	// 设置主题
	request.Subject = common.StringPtr(subject)

	// 设置模板（参考PHP案例：Template包含TemplateID和TemplateData）
	templateDataJSON, err := json.Marshal(templateData)
	if err != nil {
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("failed to marshal template data: %w", err),
		}, err
	}

	request.Template = &ses.Template{
		TemplateID:   common.Uint64Ptr(templateID),
		TemplateData: common.StringPtr(string(templateDataJSON)),
	}

	// 调用API
	response, err := c.client.SendEmail(request)
	if err != nil {
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("tencent SES send template email failed: %w", err),
		}, err
	}

	// 解析结果
	result := &SendResult{
		MessageID: *response.Response.MessageId,
		Status:    "success",
		Extra: map[string]interface{}{
			"request_id":  *response.Response.RequestId,
			"template_id": templateID,
			"timestamp":   time.Now().Unix(),
		},
	}

	return result, nil
}
