// Package goemail 提供多平台邮件发送功能
// 支持腾讯云SES、阿里云DM、SMTP等多种发送方式
package goemail

import (
	"context"
	"fmt"
)

// ProviderType 邮件服务提供商类型
type ProviderType string

const (
	// ProviderTypeTencentSES 腾讯云简单邮件服务
	ProviderTypeTencentSES ProviderType = "tencent_ses"
	// ProviderTypeAliyunDM 阿里云邮件推送
	ProviderTypeAliyunDM ProviderType = "aliyun_dm"
	// ProviderTypeSMTP SMTP协议
	ProviderTypeSMTP ProviderType = "smtp"
)

// EmailClient 邮件客户端接口
type EmailClient interface {
	// SendEmail 发送邮件
	SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error)
	// SendBatchEmail 批量发送邮件
	SendBatchEmail(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error)
	// GetProviderType 获取提供商类型
	GetProviderType() ProviderType
}

// SendRequest 发送请求
type SendRequest struct {
	From        string            // 发件人地址
	To          []string          // 收件人列表
	Cc          []string          // 抄送列表
	Bcc         []string          // 密送列表
	Subject     string            // 邮件主题
	HtmlBody    string            // HTML正文
	TextBody    string            // 纯文本正文
	ReplyTo     []string          // 回复地址
	Attachments []*Attachment     // 附件列表
	Tags        map[string]string // 标签（用于追踪）
	Headers     map[string]string // 自定义邮件头
}

// Attachment 附件
type Attachment struct {
	Filename    string // 文件名
	Content     []byte // 文件内容
	ContentType string // MIME类型
}

// SendResult 发送结果
type SendResult struct {
	MessageID string                 // 消息ID
	Status    string                 // 状态: success/failed
	Error     error                  // 错误信息
	Extra     map[string]interface{} // 额外信息（不同提供商返回的数据）
}

// Config 基础配置
type Config struct {
	ProviderType ProviderType // 提供商类型
	Region       string       // 区域（云服务商需要）
	AccessKeyID  string       // Access Key ID
	SecretKey    string       // Secret Key
	// SMTP专用配置
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	UseTLS       bool
}

// NewEmailClient 创建邮件客户端
func NewEmailClient(cfg *Config) (EmailClient, error) {
	switch cfg.ProviderType {
	case ProviderTypeTencentSES:
		return newTencentSESClient(cfg)
	case ProviderTypeAliyunDM:
		return newAliyunDMClient(cfg)
	case ProviderTypeSMTP:
		return newSMTPClient(cfg)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", cfg.ProviderType)
	}
}

// ValidateEmail 验证邮箱地址格式
func ValidateEmail(email string) bool {
	if len(email) == 0 {
		return false
	}
	
	atIndex := -1
	for i, c := range email {
		if c == '@' {
			atIndex = i
			break
		}
	}
	
	if atIndex <= 0 || atIndex >= len(email)-1 {
		return false
	}
	
	localPart := email[:atIndex]
	domainPart := email[atIndex+1:]
	
	if len(localPart) == 0 || len(domainPart) == 0 {
		return false
	}
	
	// 检查域名是否包含点
	hasDot := false
	for _, c := range domainPart {
		if c == '.' {
			hasDot = true
			break
		}
	}
	
	return hasDot
}

// ValidateEmails 批量验证邮箱地址
func ValidateEmails(emails []string) []string {
	var invalid []string
	for _, email := range emails {
		if !ValidateEmail(email) {
			invalid = append(invalid, email)
		}
	}
	return invalid
}
