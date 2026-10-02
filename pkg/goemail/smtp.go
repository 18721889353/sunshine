package goemail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"gopkg.in/gomail.v2"
)

// SMTPClient SMTP协议客户端
type SMTPClient struct {
	config *Config
	port   int // 解析后的端口（不回写共享 cfg，避免构造函数产生副作用）
}

// newSMTPClient 创建SMTP客户端
func newSMTPClient(cfg *Config) (*SMTPClient, error) {
	if cfg.SMTPHost == "" {
		return nil, fmt.Errorf("SMTP 服务器地址不能为空")
	}

	// 端口只在本地解析，不回写入参 cfg（cfg 可能被多方共享）
	port := cfg.SMTPPort
	if port == 0 {
		port = 465 // 默认SSL端口
	}

	if cfg.SMTPUsername == "" || cfg.SMTPPassword == "" {
		return nil, fmt.Errorf("SMTP 用户名与密码不能为空")
	}

	return &SMTPClient{
		config: cfg,
		port:   port,
	}, nil
}

// SendEmail 发送邮件
func (c *SMTPClient) SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.smtp")
	ctx, span := tracer.Start(ctx, "smtp.send.email", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	// nil 入参前置校验：后续所有字段访问（req.From 等）都假设 req 非空，
	// 而 SendBatchEmail 会把切片元素直接交给本方法，nil 元素会在 worker goroutine 里 panic
	if req == nil {
		err := fmt.Errorf("发送请求不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{Status: StatusFailed, Error: err}, err
	}

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "smtp"),
		attribute.String("email.host", c.config.SMTPHost),
		attribute.Int("email.port", c.port),
		attribute.String("email.from", maskEmail(req.From)),
		attribute.Int("email.to.count", len(req.To)),
		attribute.Int("email.cc.count", len(req.Cc)),
		attribute.Int("email.bcc.count", len(req.Bcc)),
		attribute.String("email.subject", req.Subject),
		attribute.Bool("email.has_attachments", len(req.Attachments) > 0),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 验证参数
	if err := c.validateRequest(ctx, req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	// 被忽略字段主动告警：README 字段支持矩阵标注了 SMTP 不消费 Tags，
	// 但用户不看 README 就会误以为标签生效——静默丢弃会造成「以为打了标签实际没打」的误判
	if len(req.Tags) > 0 {
		logger.WarnWithCtx(ctx, "当前 provider 忽略部分字段",
			logger.String("provider", "smtp"),
			logger.Int("tags_ignored", len(req.Tags)))
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
	if req.HTMLBody != "" {
		m.SetBody("text/html; charset=UTF-8", req.HTMLBody)
		if req.TextBody != "" {
			m.AddAlternative("text/plain; charset=UTF-8", req.TextBody)
		}
	} else {
		m.SetBody("text/plain; charset=UTF-8", req.TextBody)
	}

	// 添加附件：写入独立临时目录并整体清理。
	// 不再在 /tmp 下直接拼接文件名：Windows 无 /tmp；Filename 含路径分隔符时会造成
	// 路径穿越；并发发送同名附件会互相覆盖；且原实现写完从不删除，残留用户附件内容。
	if len(req.Attachments) > 0 {
		attachDir, dirErr := os.MkdirTemp("", "goemail-attach-*")
		if dirErr != nil {
			attachErr := fmt.Errorf("创建附件临时目录失败: %w", dirErr)
			span.RecordError(attachErr)
			span.SetStatus(codes.Error, attachErr.Error())
			return &SendResult{
				Status: StatusFailed,
				Error:  attachErr,
			}, attachErr
		}
		defer func() {
			if rmErr := os.RemoveAll(attachDir); rmErr != nil {
				logger.WarnWithCtx(ctx, "清理附件临时目录失败",
					logger.String("dir", attachDir), logger.Err(rmErr))
			}
		}()

		for _, attachment := range req.Attachments {
			// 只取文件名主体：拒绝空名与 . / ..，净化路径分隔符，防路径穿越
			safeName := filepath.Base(strings.TrimSpace(attachment.Filename))
			if safeName == "" || safeName == "." || safeName == ".." {
				attachErr := fmt.Errorf("附件文件名不合法: %q", attachment.Filename)
				span.RecordError(attachErr)
				span.SetStatus(codes.Error, attachErr.Error())
				return &SendResult{
					Status: StatusFailed,
					Error:  attachErr,
				}, attachErr
			}
			tmpFile := filepath.Join(attachDir, safeName)
			if err := os.WriteFile(tmpFile, attachment.Content, 0600); err != nil {
				attachErr := fmt.Errorf("写入附件临时文件失败: %w", err)
				span.RecordError(attachErr)
				span.SetStatus(codes.Error, attachErr.Error())
				return &SendResult{
					Status: StatusFailed,
					Error:  attachErr,
				}, attachErr
			}
			m.Attach(tmpFile)
		}
	}

	// 创建拨号器（用解析后的 c.port：cfg.SMTPPort 为 0 时默认 465 只存在于本地副本，
	// 若传 cfg.SMTPPort 会把 0 直接交给 gomail，端口默认值形同虚设、拨号必失败）
	dialer := gomail.NewDialer(
		c.config.SMTPHost,
		c.port,
		c.config.SMTPUsername,
		c.config.SMTPPassword,
	)

	// TLS 配置（本包语义）：
	//   UseTLS=true  → 强制隐式 SSL/TLS（dialer.SSL，对应 465 端口的 SMTPS）
	//   UseTLS=false → 不强制，按 gomail 默认：465 端口仍隐式 SSL（NewDialer 按端口预设），
	//                  25/587 端口走 STARTTLS 升级（服务器声明支持时）；
	//                  不设 dialer.TLSConfig——gomail 的 startTLSConfig 会在 nil 时自行填充
	//                  默认 tls.Config，显式设置零值配置与不设置无差别，只会混淆代码意图。
	// 原实现两分支反了（UseTLS=false 反而强制 SSL=true，25/587 端口必握手失败），
	// 已回正并登记 CHANGELOG；调用方按端口选择：465 置 true，25/587 置 false。
	if c.config.UseTLS {
		dialer.SSL = true
	}

	// 发送邮件
	err := dialer.DialAndSend(m)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		duration := time.Since(startTime)
		span.SetAttributes(
			attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
			requestIDAttr(ctx),
		)
		return &SendResult{
			Status: StatusFailed,
			Error:  fmt.Errorf("SMTP 发送邮件失败: %w", err),
		}, err
	}

	// 生成消息ID（SMTP不返回，自己生成）
	messageID := fmt.Sprintf("%d@%s", time.Now().UnixNano(), c.config.SMTPHost)

	result := &SendResult{
		MessageID: messageID,
		Status:    StatusSuccess,
		Extra: map[string]interface{}{
			"host":      c.config.SMTPHost,
			"port":      c.port,
			"timestamp": time.Now().Unix(),
		},
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.String("email.message_id", messageID),
		attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "email sent successfully")

	return result, nil
}

// SendBatchEmail 批量发送邮件（并发骨架见 runBatchEmail）：单条失败不中断批量，
// results 保序回填，全部跑完后返回 batchErrors 聚合 error。
func (c *SMTPClient) SendBatchEmail(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
	return runBatchEmail(ctx, reqs, c.config.BatchConcurrency, c.SendEmail)
}

// GetProviderType 获取提供商类型
func (c *SMTPClient) GetProviderType() ProviderType {
	return ProviderTypeSMTP
}

// GetEmailStatus 查询邮件发送状态
// 注：SMTP协议不支持查询邮件发送状态
// 邮件发送成功后即认为已送达SMTP服务器，后续投递由邮件服务器处理
func (c *SMTPClient) GetEmailStatus(ctx context.Context, query *EmailStatusQuery) (*EmailStatusResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.smtp")
	spanName := "smtp.get_email_status"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	// nil 入参前置校验：后续 query.MessageID 等字段访问假设 query 非空
	if query == nil {
		err := fmt.Errorf("查询参数不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &EmailStatusResult{Status: StatusFailed, Error: err}, err
	}

	logger.InfoWithCtx(ctx, "开始查询邮件发送状态",
		logger.String("message_id", query.MessageID))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "smtp"),
		attribute.String("email.query.message_id", query.MessageID),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// SMTP协议不支持查询邮件状态
	span.SetAttributes(
		attribute.String("email.status.note", "smtp_does_not_support_status_query"),
	)

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Float64("email.query.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "query completed with notice")

	return &EmailStatusResult{
		Status: StatusSuccess,
		Data:   nil,
		Extra: map[string]interface{}{
			"note":       "SMTP协议不支持查询邮件发送状态，邮件发送成功后即认为已送达SMTP服务器",
			"message_id": query.MessageID,
			"timestamp":  time.Now().Unix(),
		},
	}, nil
}

// validateRequest 验证请求参数
func (c *SMTPClient) validateRequest(ctx context.Context, req *SendRequest) error {
	if req.From == "" {
		return fmt.Errorf("发件人地址不能为空")
	}

	if !ValidateEmail(ctx, req.From) {
		return fmt.Errorf("发件人地址格式不合法: %q", req.From)
	}

	if len(req.To) == 0 {
		return fmt.Errorf("至少需要一个收件人地址")
	}

	// 验证所有收件人
	invalidTo := ValidateEmails(ctx, req.To)
	if len(invalidTo) > 0 {
		return fmt.Errorf("收件人地址格式不合法: %q", invalidTo)
	}

	// 验证抄送
	if len(req.Cc) > 0 {
		invalidCc := ValidateEmails(ctx, req.Cc)
		if len(invalidCc) > 0 {
			return fmt.Errorf("抄送地址格式不合法: %q", invalidCc)
		}
	}

	// 验证密送
	if len(req.Bcc) > 0 {
		invalidBcc := ValidateEmails(ctx, req.Bcc)
		if len(invalidBcc) > 0 {
			return fmt.Errorf("密送地址格式不合法: %q", invalidBcc)
		}
	}

	// 验证回复地址：Cc/Bcc/ReplyTo 三个地址类字段同口径校验，
	// 非法 Reply-To 头会被 gomail 直接设置、可能触发服务器拒收（终审 R7-4）
	if len(req.ReplyTo) > 0 {
		invalidReplyTo := ValidateEmails(ctx, req.ReplyTo)
		if len(invalidReplyTo) > 0 {
			return fmt.Errorf("回复地址格式不合法: %q", invalidReplyTo)
		}
	}

	if req.Subject == "" {
		return fmt.Errorf("邮件主题不能为空")
	}

	if req.HTMLBody == "" && req.TextBody == "" {
		return fmt.Errorf("HTMLBody 与 TextBody 至少填一个")
	}

	return nil
}
