package goemail

import (
	"context"
	"encoding/base64"
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
	region string // 解析后的区域（不回写共享 cfg，避免构造函数产生副作用）
}

// derefString / derefInt64：SDK 响应字段的 nil 安全取值。
// 云 API 响应中的标量字段多为指针，服务端在特定状态下可能不返回，直接解引用会 panic。
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// derefInt64 同 derefString，用于状态码等数值字段。
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// newTencentSESClient 创建腾讯云SES客户端
func newTencentSESClient(cfg *Config) (*TencentSESClient, error) {
	if cfg.AccessKeyID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("腾讯云 SES 需要 AccessKeyID 与 SecretKey")
	}

	// 区域只在本地解析，不回写入参 cfg：cfg 可能被多个客户端/调用方共享，
	// 构造函数修改共享状态会产生难以追踪的副作用。
	region := cfg.Region
	if region == "" {
		region = "ap-hongkong" // 默认香港区域
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
	client, err := ses.NewClient(credential, region, cpf)
	if err != nil {
		return nil, fmt.Errorf("创建腾讯云 SES 客户端失败: %w", err)
	}

	return &TencentSESClient{
		client: client,
		config: cfg,
		region: region,
	}, nil
}

// SendEmail 发送邮件
func (c *TencentSESClient) SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.tencent_ses")
	ctx, span := tracer.Start(ctx, "tencent_ses.send.email", trace.WithSpanKind(trace.SpanKindClient))
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
		attribute.String("email.provider", "tencent_ses"),
		attribute.String("email.region", c.region),
		attribute.String("email.from", maskEmail(req.From)),
		attribute.Int("email.to.count", len(req.To)),
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

	// 被忽略字段主动告警：腾讯云 SES SendEmail 只消费 From/To/Subject/正文，
	// 其余字段被静默丢弃会造成「以为抄送了实际没抄送」的误判（见 README 字段支持矩阵）
	if len(req.Cc) > 0 || len(req.Bcc) > 0 || len(req.ReplyTo) > 0 ||
		len(req.Attachments) > 0 || len(req.Headers) > 0 || len(req.Tags) > 0 {
		logger.WarnWithCtx(ctx, "当前 provider 忽略部分字段",
			logger.String("provider", "tencent_ses"),
			logger.Int("cc_ignored", len(req.Cc)),
			logger.Int("bcc_ignored", len(req.Bcc)),
			logger.Int("reply_to_ignored", len(req.ReplyTo)),
			logger.Int("attachments_ignored", len(req.Attachments)),
			logger.Int("headers_ignored", len(req.Headers)),
			logger.Int("tags_ignored", len(req.Tags)))
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

	// 设置正文：Simple.Html / Simple.Text 按腾讯云 API 要求必须传 base64 编码后的内容
	//（SDK 字段注释「base64之后的Html代码/纯文本信息」），且两字段语义独立、
	// 不是「HTML 优先」二选一——Html 缺省时邮件展示 Text 纯文本，两者都传时 Text
	// 是 Html 的纯文本样式版本（终审 R7-1/R7-2：原实现传原文且 TextBody 只能塞进 Html）
	request.Simple = buildSimpleBody(req)

	// 调用API
	response, err := c.client.SendEmail(request)
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
			Error:  fmt.Errorf("腾讯云 SES 发送邮件失败: %w", err),
		}, err
	}

	// 解析结果（响应字段可能为 nil，统一 nil 安全取值防 panic）
	if response == nil || response.Response == nil {
		err := fmt.Errorf("腾讯云 SES 返回空响应")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}
	result := &SendResult{
		MessageID: derefString(response.Response.MessageId),
		Status:    StatusSuccess,
		Extra: map[string]interface{}{
			"request_id": derefString(response.Response.RequestId),
			"timestamp":  time.Now().Unix(),
		},
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.String("email.message_id", result.MessageID),
		attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "email sent successfully")

	return result, nil
}

// buildSimpleBody 构造腾讯云 SES 的 Simple 正文（纯函数，便于单测直压）：
//   - Html / Text 必须是 base64 编码后的内容（SDK 字段注释：「base64之后的Html代码」
//     「base64之后的纯文本信息」）——传原文会被服务端当 base64 解码，轻则乱码重则报错；
//   - 两字段语义独立：只传 Text 时邮件展示纯文本；只传 Html 时无纯文本样式版本；
//     都传时 Text 是 Html 的纯文本样式版本。至少填一个由 validateRequest 保证。
func buildSimpleBody(req *SendRequest) *ses.Simple {
	simple := &ses.Simple{}
	if req.HTMLBody != "" {
		simple.Html = common.StringPtr(base64.StdEncoding.EncodeToString([]byte(req.HTMLBody)))
	}
	if req.TextBody != "" {
		simple.Text = common.StringPtr(base64.StdEncoding.EncodeToString([]byte(req.TextBody)))
	}
	return simple
}

// SendBatchEmail 批量发送邮件（并发骨架见 runBatchEmail）：单条失败不中断批量，
// results 保序回填，全部跑完后返回 batchErrors 聚合 error。
func (c *TencentSESClient) SendBatchEmail(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
	return runBatchEmail(ctx, reqs, c.config.BatchConcurrency, c.SendEmail)
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

	// nil 入参前置校验：后续 query.MessageID 等字段访问假设 query 非空
	if query == nil {
		err := fmt.Errorf("查询参数不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &EmailStatusResult{Status: StatusFailed, Error: err}, err
	}

	logger.InfoWithCtx(ctx, "开始查询邮件发送状态",
		logger.String("message_id", query.MessageID),
		logger.String("to_address", maskEmail(query.ToAddress)))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "tencent_ses"),
		attribute.String("email.query.message_id", query.MessageID),
		attribute.String("email.query.to_address", maskEmail(query.ToAddress)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 构建请求
	request := ses.NewGetSendEmailStatusRequest()

	// 设置查询日期（必须）；用局部变量，不突变入参 query
	requestDate := resolveRequestDate(query.FromDate)
	request.RequestDate = common.StringPtr(requestDate.Format("2006-01-02"))

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
			requestIDAttr(ctx),
		)
		return &EmailStatusResult{
			Status: StatusFailed,
			Error:  fmt.Errorf("腾讯云 SES 查询发送状态失败: %w", err),
		}, err
	}

	// 解析结果（响应体可能为 nil，先判再取）
	if response == nil || response.Response == nil {
		err := fmt.Errorf("腾讯云 SES 返回空响应")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &EmailStatusResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}
	emailStatuses := c.parseEmailStatusList(response.Response.EmailStatusList)

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("email.status.count", len(emailStatuses)),
		attribute.Float64("email.query.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "email status queried successfully")

	return &EmailStatusResult{
		Status: StatusSuccess,
		Data:   emailStatuses,
		Extra: map[string]interface{}{
			"request_id":  derefString(response.Response.RequestId),
			"total_count": len(emailStatuses),
			"timestamp":   time.Now().Unix(),
		},
	}, nil
}

// resolveRequestDate 解析查询起始日期：零值时取当前时间。
// 纯函数（入参为值）——旧实现直接改写 query.FromDate 突变调用方对象，已改为局部取值。
func resolveRequestDate(fromDate time.Time) time.Time {
	if fromDate.IsZero() {
		return time.Now()
	}
	return fromDate
}

// parseEmailStatusList 把腾讯云响应中的状态列表转为内部模型。
// 所有指针字段统一 nil 安全取值：服务端在特定状态下可能不返回其中任意一个。
func (c *TencentSESClient) parseEmailStatusList(list []*ses.SendEmailStatus) []*EmailStatus {
	var emailStatuses []*EmailStatus
	for _, status := range list {
		if status == nil {
			continue
		}
		emailStatus := &EmailStatus{
			MessageID:   derefString(status.MessageId),
			ToAddress:   derefString(status.ToEmailAddress),
			FromAddress: derefString(status.FromEmailAddress),
			StatusCode:  int(derefInt64(status.SendStatus)),
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

		// 额外信息（逐字段 nil 判断后再取值，避免任意一个字段缺失即 panic）
		extra := map[string]interface{}{
			"send_status": derefInt64(status.SendStatus),
		}
		if status.DeliverStatus != nil {
			extra["deliver_status"] = *status.DeliverStatus
		}
		if status.DeliverMessage != nil {
			extra["deliver_message"] = *status.DeliverMessage
		}
		emailStatus.Extra = extra

		emailStatuses = append(emailStatuses, emailStatus)
	}
	return emailStatuses
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
		return StatusFailed, "内部系统异常"
	case 1004:
		return StatusFailed, "发信超时"
	case 1006:
		return StatusFailed, "触发频率控制"
	case 1007:
		return StatusFailed, "邮件地址在黑名单中"
	case 1008:
		return StatusFailed, "域名被收件人拒收"
	case 1010:
		return StatusFailed, "超出了每日发送限制"
	case 1011:
		return StatusFailed, "无发送自定义内容权限，必须使用模板"
	case 1013:
		return StatusFailed, "域名被收件人取消订阅"
	case 2001:
		return StatusFailed, "找不到相关记录"
	case 3007:
		return StatusFailed, "模板ID无效或者不可用"
	case 3008:
		return StatusFailed, "被收信域名临时封禁"
	case 3009:
		return StatusFailed, "无权限使用该模板"
	case 3010:
		return StatusFailed, "TemplateData字段格式不正确"
	case 3014:
		return StatusFailed, "发件域名没有经过认证，无法发送"
	case 3020:
		return StatusFailed, "收件方邮箱类型在黑名单"
	case 3024:
		return StatusFailed, "邮箱地址格式预检查失败"
	case 3030:
		return StatusFailed, "退信率过高，临时限制发送"
	case 3033:
		return StatusFailed, "余额不足，账号欠费等"
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
		return StatusFailed, "邮件因某种原因被丢弃"
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

	if req.Subject == "" {
		return fmt.Errorf("邮件主题不能为空")
	}

	if req.HTMLBody == "" && req.TextBody == "" {
		return fmt.Errorf("HTMLBody 与 TextBody 至少填一个")
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

	logger.InfoWithCtx(ctx, "开始发送模板邮件",
		logger.String("from", from),
		logger.Int("to_count", len(to)),
		logger.Uint64("template_id", templateID),
		logger.String("subject", subject))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "tencent_ses"),
		attribute.String("email.from", maskEmail(from)),
		attribute.Int("email.to.count", len(to)),
		attribute.Int64("email.template_id", int64(templateID)),
		attribute.String("email.subject", subject),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 验证参数
	if from == "" {
		err := fmt.Errorf("发件人地址不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	// 地址格式本地拦截，与 SendEmail 口径一致——否则非法地址直发云 API、
	// 由云端 InvalidEmailAddress 拦，同一个 from 走两个入口行为不同（终审 P2-1）
	if !ValidateEmail(ctx, from) {
		err := fmt.Errorf("发件人地址格式不合法: %q", from)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	if len(to) == 0 {
		err := fmt.Errorf("至少需要一个收件人地址")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	if invalidTo := ValidateEmails(ctx, to); len(invalidTo) > 0 {
		err := fmt.Errorf("收件人地址格式不合法: %q", invalidTo)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	if templateID == 0 {
		err := fmt.Errorf("模板 ID 不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	// 主题本地拦截，与 SendEmail 的 validateRequest 口径一致——
	// 否则空主题直发云 API，同一个 subject 走两个入口行为不同（终审 R7-3）
	if subject == "" {
		err := fmt.Errorf("邮件主题不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
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
		marshalErr := fmt.Errorf("序列化模板数据失败: %w", err)
		span.RecordError(marshalErr)
		span.SetStatus(codes.Error, marshalErr.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  marshalErr,
		}, marshalErr
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
			requestIDAttr(ctx),
		)
		return &SendResult{
			Status: StatusFailed,
			Error:  fmt.Errorf("腾讯云 SES 发送模板邮件失败: %w", err),
		}, err
	}

	// 解析结果（响应字段可能为 nil，统一 nil 安全取值防 panic）
	if response == nil || response.Response == nil {
		err := fmt.Errorf("腾讯云 SES 返回空响应")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}
	result := &SendResult{
		MessageID: derefString(response.Response.MessageId),
		Status:    StatusSuccess,
		Extra: map[string]interface{}{
			"request_id":  derefString(response.Response.RequestId),
			"template_id": templateID,
			"timestamp":   time.Now().Unix(),
		},
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.String("email.message_id", result.MessageID),
		attribute.Float64("email.send.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "email sent successfully")

	return result, nil
}
