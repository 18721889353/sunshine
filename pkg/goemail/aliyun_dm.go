package goemail

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	darabonba "github.com/alibabacloud-go/darabonba-openapi/client"
	dm "github.com/alibabacloud-go/dm-20151123/client"
	"github.com/alibabacloud-go/tea/tea"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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

	// 创建配置
	config := &darabonba.Config{
		AccessKeyId:     tea.String(cfg.AccessKeyID),
		AccessKeySecret: tea.String(cfg.SecretKey),
		Endpoint:        tea.String(fmt.Sprintf("dm.%s.aliyuncs.com", cfg.Region)),
		RegionId:        tea.String(cfg.Region),
	}

	// 创建客户端
	client, err := dm.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create aliyun DM client: %w", err)
	}

	return &AliyunDMClient{
		client: client,
		config: cfg,
	}, nil
}

// SendEmail 发送邮件
func (c *AliyunDMClient) SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.aliyun_dm")
	spanName := fmt.Sprintf("aliyun_dm.send.email")
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "aliyun_dm"),
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
	request := &dm.SingleSendMailRequest{
		AccountName:    tea.String(req.From),                  // 发件人地址
		AddressType:    tea.Int32(1),                          // 1为发信地址
		ReplyToAddress: tea.Bool(true),                        // 是否允许回复
		Subject:        tea.String(req.Subject),               // 邮件主题
		HtmlBody:       tea.String(req.HTMLBody),              // HTML正文
		TextBody:       tea.String(req.TextBody),              // 纯文本正文
		ToAddress:      tea.String(strings.Join(req.To, ",")), // 收件人列表（逗号分隔）
	}

	// 调用API
	response, err := c.client.SingleSendMail(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		duration := time.Since(startTime)
		span.SetAttributes(
			attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
		)
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("aliyun DM send email failed: %w", err),
		}, err
	}

	// 解析结果
	result := &SendResult{
		MessageID: tea.StringValue(response.Body.EnvId), // 使用EnvId作为MessageID
		Status:    "success",
		Extra: map[string]interface{}{
			"env_id":    tea.StringValue(response.Body.EnvId),
			"timestamp": time.Now().Unix(),
		},
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.String("email.message_id", tea.StringValue(response.Body.EnvId)),
		attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "email sent successfully")

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
func (c *AliyunDMClient) validateRequest(ctx context.Context, req *SendRequest) error {
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

	// 阿里云DM限制：单次最多100个收件人
	if len(req.To) > 100 {
		return fmt.Errorf("aliyun DM supports maximum 100 recipients per request")
	}

	return nil
}

// SendTemplateEmail 发送模板邮件（阿里云DM）
// templateName: 模板名称（在阿里云控制台创建）
// templateData: 模板变量数据，会被序列化为JSON字符串
func (c *AliyunDMClient) SendTemplateEmail(ctx context.Context, from string, to []string, templateName string, templateData map[string]interface{}) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.aliyun_dm")
	spanName := "aliyun_dm.send_template_email"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	logger.InfoWithCtx(ctx, "Start sending template email",
		logger.String("from", from),
		logger.Int("to_count", len(to)),
		logger.String("template_name", templateName))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "aliyun_dm"),
		attribute.String("email.from", from),
		attribute.Int("email.to.count", len(to)),
		attribute.String("email.template_name", templateName),
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

	if templateName == "" {
		err := fmt.Errorf("template name is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: "failed",
			Error:  err,
		}, err
	}

	// 构建批量发送请求（阿里云DM使用BatchSendMail发送模板）
	request := &dm.BatchSendMailRequest{
		AccountName:   tea.String(from),                  // 发件人地址
		AddressType:   tea.Int32(1),                      // 1为发信地址
		TemplateName:  tea.String(templateName),          // 模板名称
		ReceiversName: tea.String(strings.Join(to, ",")), // 收件人列表（逗号分隔）
	}

	// 如果有模板变量，需要添加到请求中
	// 注意：阿里云DM的模板变量通过TagName传递
	if templateData != nil && len(templateData) > 0 {
		// 将模板数据序列化为JSON，存入TagName或其他字段
		templateDataJSON, err := json.Marshal(templateData)
		if err != nil {
			return &SendResult{
				Status: "failed",
				Error:  fmt.Errorf("failed to marshal template data: %w", err),
			}, err
		}
		// TagName可以用于标记和追踪，也可以存储额外的模板数据
		request.TagName = tea.String(string(templateDataJSON))
	}

	// 调用API
	response, err := c.client.BatchSendMail(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		duration := time.Since(startTime)
		span.SetAttributes(
			attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
		)
		return &SendResult{
			Status: "failed",
			Error:  fmt.Errorf("aliyun DM send template email failed: %w", err),
		}, err
	}

	// 解析结果
	result := &SendResult{
		MessageID: "",
		Status:    "success",
		Extra: map[string]interface{}{
			"env_id":        tea.StringValue(response.Body.EnvId),
			"template_name": templateName,
			"timestamp":     time.Now().Unix(),
		},
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.String("email.env_id", tea.StringValue(response.Body.EnvId)),
		attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "email sent successfully")

	return result, nil
}
