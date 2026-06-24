// Package goemail 提供多平台邮件发送功能
// 支持腾讯云SES、阿里云DM、SMTP等多种发送方式
package goemail

import (
	"context"
	"fmt"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	// GetEmailStatus 查询邮件发送状态
	GetEmailStatus(ctx context.Context, query *EmailStatusQuery) (*EmailStatusResult, error)
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
	HTMLBody    string            // HTML正文
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

// EmailStatus 邮件状态信息
type EmailStatus struct {
	MessageID        string                 // 邮件消息ID
	ToAddress        string                 // 收件人地址
	FromAddress      string                 // 发件人地址
	Status           string                 // 发送状态: delivered, failed, pending, rejected
	StatusCode       int                    // 状态码
	StatusMessage    string                 // 状态描述
	RequestTime      time.Time              // 请求时间
	DeliverTime      time.Time              // 投递时间
	UserOpened       bool                   // 是否已打开
	UserClicked      bool                   // 是否已点击
	UserUnsubscribed bool                   // 是否退订
	UserComplained   bool                   // 是否投诉
	Extra            map[string]interface{} // 额外信息
}

// EmailStatusResult 邮件状态查询结果
type EmailStatusResult struct {
	Status string                 // 查询状态: success, failed
	Data   []*EmailStatus         // 邮件状态列表
	Error  error                  // 错误信息
	Extra  map[string]interface{} // 额外信息
}

// EmailStatusQuery 邮件状态查询参数
type EmailStatusQuery struct {
	MessageID string    // 邮件消息ID（可选）
	ToAddress string    // 收件人地址（可选）
	FromDate  time.Time // 开始日期
	ToDate    time.Time // 结束日期
	Offset    uint64    // 偏移量
	Limit     uint64    // 拉取条数
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

// 邮件状态常量
const (
	// StatusFailed 邮件发送失败状态
	StatusFailed = "failed"
)

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

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("email.request_id", reqID)
		}
	}
	return attribute.String("email.request_id", "")
}

// ValidateEmail 验证邮箱地址格式
func ValidateEmail(ctx context.Context, email string) bool {
	// 链路追踪
	tracer := otel.Tracer("goemail")
	spanName := "email.validate.single"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "Start validating email",
		logger.String("email", email))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.to_validate", email),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	if len(email) == 0 {
		span.SetAttributes(
			attribute.Bool("email.is_valid", false),
		)
		span.SetStatus(codes.Ok, "validation completed")
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
		span.SetAttributes(
			attribute.Bool("email.is_valid", false),
		)
		span.SetStatus(codes.Ok, "validation completed")
		return false
	}

	localPart := email[:atIndex]
	domainPart := email[atIndex+1:]

	if len(localPart) == 0 || len(domainPart) == 0 {
		span.SetAttributes(
			attribute.Bool("email.is_valid", false),
		)
		span.SetStatus(codes.Ok, "validation completed")
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

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Bool("email.is_valid", hasDot),
		attribute.Float64("email.validate.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "validation completed")

	return hasDot
}

// ValidateEmails 批量验证邮箱地址
func ValidateEmails(ctx context.Context, emails []string) []string {
	// 链路追踪
	tracer := otel.Tracer("goemail")
	spanName := "email.validate.batch"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "Start validating emails batch",
		logger.Int("count", len(emails)))

	// 设置追踪属性
	span.SetAttributes(
		attribute.Int("email.count", len(emails)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	var invalid []string
	for _, email := range emails {
		if !ValidateEmail(ctx, email) {
			invalid = append(invalid, email)
		}
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("email.invalid_count", len(invalid)),
		attribute.Float64("email.validate.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "validation completed")

	return invalid
}
