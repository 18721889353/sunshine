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
		cfg.Region = "ap-hongkong" // 默认香港区域
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

// GetEmailStatus 查询邮件发送状态
func (c *TencentSESClient) GetEmailStatus(ctx context.Context, query *EmailStatusQuery) (*EmailStatusResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.tencent_ses")
	spanName := "tencent_ses.get_email_status"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	logger.InfoWithCtx(ctx, "Start querying email status",
		logger.String("message_id", query.MessageID),
		logger.String("to_address", query.ToAddress))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "tencent_ses"),
		attribute.String("email.query.message_id", query.MessageID),
		attribute.String("email.query.to_address", query.ToAddress),
	)

	startTime := time.Now()

	// 构建请求
	request := ses.NewGetSendEmailStatusRequest()

	// 设置查询日期（必须）
	if query.FromDate.IsZero() {
		query.FromDate = time.Now()
	}
	request.RequestDate = common.StringPtr(query.FromDate.Format("2006-01-02"))

	// 设置可选参数
	if query.MessageID != "" {
		request.MessageId = common.StringPtr(query.MessageID)
	}
	if query.ToAddress != "" {
		request.ToEmailAddress = common.StringPtr(query.ToAddress)
	}
	// Offset 必须设置，默认为0
	request.Offset = common.Uint64Ptr(query.Offset)
	if query.Limit > 0 {
		request.Limit = common.Uint64Ptr(query.Limit)
	} else {
		request.Limit = common.Uint64Ptr(10) // 默认10条
	}

	// 调用API
	response, err := c.client.GetSendEmailStatus(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		duration := time.Since(startTime)
		span.SetAttributes(
			attribute.Float64("email.query.duration_ms", float64(duration.Milliseconds())),
		)
		return &EmailStatusResult{
			Status: "failed",
			Error:  fmt.Errorf("tencent SES get email status failed: %w", err),
		}, err
	}

	// 解析结果
	var emailStatuses []*EmailStatus
	if response.Response.EmailStatusList != nil {
		for _, status := range response.Response.EmailStatusList {
			emailStatus := &EmailStatus{
				MessageID:   *status.MessageId,
				ToAddress:   *status.ToEmailAddress,
				FromAddress: *status.FromEmailAddress,
				StatusCode:  int(*status.SendStatus),
			}

			// 转换发送状态
			emailStatus.Status, emailStatus.StatusMessage = c.parseSendStatus(status.SendStatus)

			// 转换投递状态
			if status.DeliverStatus != nil {
				deliverStatus, deliverMsg := c.parseDeliverStatus(status.DeliverStatus)
				if emailStatus.Status == "pending" {
					emailStatus.Status = deliverStatus
					emailStatus.StatusMessage = deliverMsg
				}
			}

			// 时间戳转换
			if status.RequestTime != nil {
				emailStatus.RequestTime = time.Unix(*status.RequestTime, 0)
			}
			if status.DeliverTime != nil {
				emailStatus.DeliverTime = time.Unix(*status.DeliverTime, 0)
			}

			// 用户行为
			if status.UserOpened != nil {
				emailStatus.UserOpened = *status.UserOpened
			}
			if status.UserClicked != nil {
				emailStatus.UserClicked = *status.UserClicked
			}
			if status.UserUnsubscribed != nil {
				emailStatus.UserUnsubscribed = *status.UserUnsubscribed
			}
			if status.UserComplained != nil {
				emailStatus.UserComplained = *status.UserComplained
			}

			// 额外信息
			emailStatus.Extra = map[string]interface{}{
				"send_status":     *status.SendStatus,
				"deliver_status":  *status.DeliverStatus,
				"deliver_message": *status.DeliverMessage,
			}

			emailStatuses = append(emailStatuses, emailStatus)
		}
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("email.status.count", len(emailStatuses)),
		attribute.Float64("email.query.duration_ms", float64(duration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "email status queried successfully")

	return &EmailStatusResult{
		Status: "success",
		Data:   emailStatuses,
		Extra: map[string]interface{}{
			"request_id":  *response.Response.RequestId,
			"total_count": len(emailStatuses),
			"timestamp":   time.Now().Unix(),
		},
	}, nil
}

// parseSendStatus 解析腾讯云服务端处理状态
func (c *TencentSESClient) parseSendStatus(status *int64) (statusStr string, message string) {
	if status == nil {
		return "unknown", "状态未知"
	}

	switch *status {
	case 0:
		return "accepted", "处理成功"
	case 1001, 1002, 1003, 1005, 1009:
		return "failed", "内部系统异常"
	case 1004:
		return "failed", "发信超时"
	case 1006:
		return "failed", "触发频率控制"
	case 1007:
		return "failed", "邮件地址在黑名单中"
	case 1008:
		return "failed", "域名被收件人拒收"
	case 1010:
		return "failed", "超出了每日发送限制"
	case 1011:
		return "failed", "无发送自定义内容权限，必须使用模板"
	case 1013:
		return "failed", "域名被收件人取消订阅"
	case 2001:
		return "failed", "找不到相关记录"
	case 3007:
		return "failed", "模板ID无效或者不可用"
	case 3008:
		return "failed", "被收信域名临时封禁"
	case 3009:
		return "failed", "无权限使用该模板"
	case 3010:
		return "failed", "TemplateData字段格式不正确"
	case 3014:
		return "failed", "发件域名没有经过认证，无法发送"
	case 3020:
		return "failed", "收件方邮箱类型在黑名单"
	case 3024:
		return "failed", "邮箱地址格式预检查失败"
	case 3030:
		return "failed", "退信率过高，临时限制发送"
	case 3033:
		return "failed", "余额不足，账号欠费等"
	default:
		return "unknown", fmt.Sprintf("未知状态码: %d", *status)
	}
}

// parseDeliverStatus 解析收件方处理状态
func (c *TencentSESClient) parseDeliverStatus(status *int64) (statusStr string, message string) {
	if status == nil {
		return "pending", "等待投递"
	}

	switch *status {
	case 0:
		return "pending", "请求成功被腾讯云接受，进入发送队列"
	case 1:
		return "delivered", "邮件递送成功"
	case 2:
		return "failed", "邮件因某种原因被丢弃"
	case 3:
		return "rejected", "收件方ESP拒信"
	case 8:
		return "delayed", "邮件被ESP因某些原因延迟递送"
	default:
		return "unknown", fmt.Sprintf("未知投递状态码: %d", *status)
	}
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
