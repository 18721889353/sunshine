package goemail

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"time"

	"gopkg.in/gomail.v2"
)

// SMTPClient SMTP协议客户端
type SMTPClient struct {
	config *Config
}

// newSMTPClient 创建SMTP客户端
func newSMTPClient(cfg *Config) (*SMTPClient, error) {
	if cfg.SMTPHost == "" {
		return nil, fmt.Errorf("smtp host is required")
	}

	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 465 // 默认SSL端口
	}

	if cfg.SMTPUsername == "" || cfg.SMTPPassword == "" {
		return nil, fmt.Errorf("smtp username and password are required")
	}

	return &SMTPClient{
		config: cfg,
	}, nil
}

// SendEmail 发送邮件
func (c *SMTPClient) SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 验证参数
	if err := c.validateRequest(req); err != nil {
		return &SendResult{
			Status: "failed",
			Error:  err,
		}, err
	}

	// 创建邮件消息
	m := gomail.NewMessage()

	// 设置发件人
	m.SetHeader("From", req.From)

	// 设置收件人
	m.SetHeader("To", req.To...)

	// 设置抄送
	if len(req.Cc) > 0 {
		m.SetHeader("Cc", req.Cc...)
	}

	// 设置密送
	if len(req.Bcc) > 0 {
		m.SetHeader("Bcc", req.Bcc...)
	}

	// 设置主题
	m.SetHeader("Subject", req.Subject)

	// 设置回复地址
	if len(req.ReplyTo) > 0 {
		m.SetHeader("Reply-To", req.ReplyTo...)
	}

	// 设置自定义邮件头
	for key, value := range req.Headers {
		m.SetHeader(key, value)
	}

	// 设置正文（优先使用HTML）
	if req.HtmlBody != "" {
		m.SetBody("text/html; charset=UTF-8", req.HtmlBody)
		if req.TextBody != "" {
			m.AddAlternative("text/plain; charset=UTF-8", req.TextBody)
		}
	} else {
		m.SetBody("text/plain; charset=UTF-8", req.TextBody)
	}

	// 添加附件
	for _, attachment := range req.Attachments {
		// 创建临时文件
		tmpFile := fmt.Sprintf("/tmp/%s", attachment.Filename)
		if err := ioutil.WriteFile(tmpFile, attachment.Content, 0644); err != nil {
			return &SendResult{
				Status: "failed",
				Error:  fmt.Errorf("failed to write attachment: %w", err),
			}, err
		}
		m.Attach(tmpFile)
	}

	// 创建拨号器
	dialer := gomail.NewDialer(
		c.config.SMTPHost,
		c.config.SMTPPort,
		c.config.SMTPUsername,
		c.config.SMTPPassword,
	)

	// 配置TLS
	if c.config.UseTLS {
		dialer.TLSConfig = &tls.Config{
			InsecureSkipVerify: false,
		}
	} else {
		dialer.SSL = true
	}

	// 发送邮件
	err := dialer.DialAndSend(m)
	if err != nil {
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("smtp send email failed: %w", err),
		}, err
	}

	// 生成消息ID（SMTP不返回，自己生成）
	messageID := fmt.Sprintf("%d@%s", time.Now().UnixNano(), c.config.SMTPHost)

	result := &SendResult{
		MessageID: messageID,
		Status:    "success",
		Extra: map[string]interface{}{
			"host":      c.config.SMTPHost,
			"port":      c.config.SMTPPort,
			"timestamp": time.Now().Unix(),
		},
	}

	return result, nil
}

// SendBatchEmail 批量发送邮件
func (c *SMTPClient) SendBatchEmail(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
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
func (c *SMTPClient) GetProviderType() ProviderType {
	return ProviderTypeSMTP
}

// validateRequest 验证请求参数
func (c *SMTPClient) validateRequest(req *SendRequest) error {
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

	// 验证抄送
	if len(req.Cc) > 0 {
		invalidCc := ValidateEmails(req.Cc)
		if len(invalidCc) > 0 {
			return fmt.Errorf("invalid cc addresses: %v", invalidCc)
		}
	}

	// 验证密送
	if len(req.Bcc) > 0 {
		invalidBcc := ValidateEmails(req.Bcc)
		if len(invalidBcc) > 0 {
			return fmt.Errorf("invalid bcc addresses: %v", invalidBcc)
		}
	}

	if req.Subject == "" {
		return fmt.Errorf("subject is required")
	}

	if req.HtmlBody == "" && req.TextBody == "" {
		return fmt.Errorf("either htmlBody or textBody is required")
	}

	return nil
}
