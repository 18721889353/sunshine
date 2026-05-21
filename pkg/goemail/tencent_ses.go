package goemail

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	ses "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ses/v20201002"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	// 链路追踪
	tracer := otel.Tracer("goemail.tencent_ses")
	spanName := fmt.Sprintf("tencent_ses.send.email")
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "tencent_ses"),
		attribute.String("email.region", c.config.Region),
		attribute.String("email.from", req.From),
		attribute.Int("email.to.count", len(req.To)),
		attribute.String("email.subject", req.Subject),
		attribute.Bool("email.has_attachments", len(req.Attachments) > 0),
	)

	startTime := time.Now()

	// 验证参数
	if err := c.validateRequest(ctx, req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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
	if req.HTMLBody != "" {
		bodyData = req.HTMLBody
	}
	request.Simple = &ses.Simple{
		Html: common.StringPtr(bodyData),
	}

	// 调用API
	response, err := c.client.SendEmail(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		duration := time.Since(startTime)
		span.SetAttributes(
			attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
		)
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

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.String("email.message_id", *response.Response.MessageId),
		attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "email sent successfully")

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
func (c *TencentSESClient) validateRequest(ctx context.Context, req *SendRequest) error {
	if req.From == "" {
		return fmt.Errorf("from address is required")
	}

	if !ValidateEmail(ctx, req.From) {
		return fmt.Errorf("invalid from address: %s", req.From)
	}

	if len(req.To) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	// 验证所有收件人
	invalidTo := ValidateEmails(ctx, req.To)
	if len(invalidTo) > 0 {
		return fmt.Errorf("invalid recipient addresses: %v", invalidTo)
	}

	if req.Subject == "" {
		return fmt.Errorf("subject is required")
	}

	if req.HTMLBody == "" && req.TextBody == "" {
		return fmt.Errorf("either htmlBody or textBody is required")
	}

	return nil
}

// SendTemplateEmail 发送模板邮件（参考PHP案例实现）
func (c *TencentSESClient) SendTemplateEmail(ctx context.Context, from string, to []string, templateID uint64, templateData map[string]interface{}, subject string) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.tencent_ses")
	spanName := "tencent_ses.send_template_email"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	logger.InfoWithCtx(ctx, "Start sending template email",
		logger.String("from", from),
		logger.Int("to_count", len(to)),
		logger.Uint64("template_id", templateID),
		logger.String("subject", subject))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "tencent_ses"),
		attribute.String("email.from", from),
		attribute.Int("email.to.count", len(to)),
		attribute.Int("email.template_id", int(templateID)),
		attribute.String("email.subject", subject),
	)

	startTime := time.Now()

	// 验证参数
	if from == "" {
		err := fmt.Errorf("from address is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: "failed",
			Error:  err,
		}, err
	}

	if len(to) == 0 {
		err := fmt.Errorf("at least one recipient is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: "failed",
			Error:  err,
		}, err
	}

	if templateID == 0 {
		err := fmt.Errorf("template ID is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: "failed",
			Error:  err,
		}, err
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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		duration := time.Since(startTime)
		span.SetAttributes(
			attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
		)
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

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.String("email.message_id", *response.Response.MessageId),
		attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "email sent successfully")

	return result, nil
}
