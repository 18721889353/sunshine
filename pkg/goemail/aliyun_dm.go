package goemail

import (
	"context"
	"fmt"
	"strings"
	"time"

	dm "github.com/alibabacloud-go/dm-20151123/client"
	"github.com/alibabacloud-go/tea/tea"
)

// AliyunDMClient 阿里云邮件推送客户端
type AliyunDMClient struct {
	client *dm.Client
	config *Config
}

// newAliyunDMClient 创建阿里云DM客户端
func newAliyunDMClient(cfg *Config) (*AliyunDMClient, error) {
	if cfg.AccessKeyID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("aliyun DM requires AccessKeyID and SecretKey")
	}

	if cfg.Region == "" {
		cfg.Region = "cn-hangzhou" // 默认杭州区域
	}

	// TODO: 使用正确的阿里云DM SDK初始化方式
	// 当前SDK版本API有变化，需要查阅最新文档
	return nil, fmt.Errorf("aliyun DM client initialization pending SDK update")
}

// SendEmail 发送邮件
func (c *AliyunDMClient) SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 验证参数
	if err := c.validateRequest(req); err != nil {
		return &SendResult{
			Status: "failed",
			Error:  err,
		}, err
	}

	// 构建请求
	request := &dm.SingleSendMailRequest{
		AccountName:    tea.String(req.From),                     // 发件人地址
		AddressType:    tea.Int32(1),                             // 1为发信地址
		ReplyToAddress: tea.Bool(true),                           // 是否允许回复
		Subject:        tea.String(req.Subject),                  // 邮件主题
		HtmlBody:       tea.String(req.HtmlBody),                 // HTML正文
		TextBody:       tea.String(req.TextBody),                 // 纯文本正文
		ToAddress:      tea.String(strings.Join(req.To, ",")),   // 收件人列表（逗号分隔）
	}

	// 调用API
	response, err := c.client.SingleSendMail(request)
	if err != nil {
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("aliyun DM send email failed: %w", err),
		}, err
	}

	// 解析结果
	result := &SendResult{
		MessageID: "", // 阿里云DM不返回MessageID
		Status:    "success",
		Extra: map[string]interface{}{
			"env_id":    tea.StringValue(response.Body.EnvId),
			"timestamp": time.Now().Unix(),
		},
	}

	return result, nil
}

// SendBatchEmail 批量发送邮件
func (c *AliyunDMClient) SendBatchEmail(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
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
func (c *AliyunDMClient) GetProviderType() ProviderType {
	return ProviderTypeAliyunDM
}

// validateRequest 验证请求参数
func (c *AliyunDMClient) validateRequest(req *SendRequest) error {
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

	// 阿里云DM限制：单次最多100个收件人
	if len(req.To) > 100 {
		return fmt.Errorf("aliyun DM supports maximum 100 recipients per request")
	}

	return nil
}
