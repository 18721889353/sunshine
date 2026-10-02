package goemail

import (
	"context"
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
	region string // 解析后的区域（不回写共享 cfg，避免构造函数产生副作用）
}

// newAliyunDMClient 创建阿里云DM客户端
func newAliyunDMClient(cfg *Config) (*AliyunDMClient, error) {
	if cfg.AccessKeyID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("阿里云 DM 需要 AccessKeyID 与 AccessKeySecret")
	}

	// 区域只在本地解析，不回写入参 cfg：cfg 可能被多个客户端/调用方共享，
	// 构造函数修改共享状态会产生难以追踪的副作用。
	region := cfg.Region
	if region == "" {
		region = "cn-hangzhou" // 默认杭州区域
	}

	// 创建配置
	config := &darabonba.Config{
		AccessKeyId:     tea.String(cfg.AccessKeyID),
		AccessKeySecret: tea.String(cfg.SecretKey),
		Endpoint:        tea.String(fmt.Sprintf("dm.%s.aliyuncs.com", region)),
		RegionId:        tea.String(region),
	}

	// 创建客户端
	client, err := dm.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("创建阿里云 DM 客户端失败: %w", err)
	}

	return &AliyunDMClient{
		client: client,
		config: cfg,
		region: region,
	}, nil
}

// SendEmail 发送邮件
func (c *AliyunDMClient) SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.aliyun_dm")
	ctx, span := tracer.Start(ctx, "aliyun_dm.send.email", trace.WithSpanKind(trace.SpanKindClient))
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
		attribute.String("email.provider", "aliyun_dm"),
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

	// 被忽略字段主动告警：阿里云 SingleSendMail 只消费 From/To/Subject/正文，
	// 其余字段被静默丢弃会造成「以为抄送了实际没抄送」的误判（见 README 字段支持矩阵）
	if len(req.Cc) > 0 || len(req.Bcc) > 0 || len(req.ReplyTo) > 0 ||
		len(req.Attachments) > 0 || len(req.Headers) > 0 || len(req.Tags) > 0 {
		logger.WarnWithCtx(ctx, "当前 provider 忽略部分字段",
			logger.String("provider", "aliyun_dm"),
			logger.Int("cc_ignored", len(req.Cc)),
			logger.Int("bcc_ignored", len(req.Bcc)),
			logger.Int("reply_to_ignored", len(req.ReplyTo)),
			logger.Int("attachments_ignored", len(req.Attachments)),
			logger.Int("headers_ignored", len(req.Headers)),
			logger.Int("tags_ignored", len(req.Tags)))
	}

	// 构建请求
	request := &dm.SingleSendMailRequest{
		AccountName: tea.String(req.From), // 发件人地址
		AddressType: tea.Int32(1),         // 1为发信地址
		// 阿里云 API 语义约束：ReplyToAddress 是「是否允许回复」的布尔开关，
		// 真正的回复地址由控制台账户默认值决定，无法通过 req.ReplyTo 注入——
		// 这与 README 字段支持矩阵中「ReplyTo 阿里云忽略」的标注一致
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
			requestIDAttr(ctx),
		)
		return &SendResult{
			Status: StatusFailed,
			Error:  fmt.Errorf("阿里云 DM 发送邮件失败: %w", err),
		}, err
	}

	// 解析结果（响应体可能为 nil——HTTP 200 但 body 空/畸形时 darabonba 不填充，
	// 直接取 response.Body.EnvId 会 panic；与腾讯 R1-1 同型的响应侧防护，终审 R7-2）
	if response == nil || response.Body == nil {
		err := fmt.Errorf("阿里云 DM 返回空响应")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}
	result := &SendResult{
		MessageID: tea.StringValue(response.Body.EnvId), // 使用EnvId作为MessageID
		Status:    StatusSuccess,
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
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "email sent successfully")

	return result, nil
}

// SendBatchEmail 批量发送邮件（并发骨架见 runBatchEmail）：单条失败不中断批量，
// results 保序回填，全部跑完后返回 batchErrors 聚合 error。
func (c *AliyunDMClient) SendBatchEmail(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
	return runBatchEmail(ctx, reqs, c.config.BatchConcurrency, c.SendEmail)
}

// GetProviderType 获取提供商类型
func (c *AliyunDMClient) GetProviderType() ProviderType {
	return ProviderTypeAliyunDM
}

// GetEmailStatus 查询邮件发送状态
// 注：阿里云DM没有直接提供查询单封邮件状态的API
// 这里实现一个基于TagName追踪的查询方法
func (c *AliyunDMClient) GetEmailStatus(ctx context.Context, query *EmailStatusQuery) (*EmailStatusResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.aliyun_dm")
	spanName := "aliyun_dm.get_email_status"
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
		attribute.String("email.provider", "aliyun_dm"),
		attribute.String("email.query.message_id", query.MessageID),
		attribute.String("email.query.to_address", maskEmail(query.ToAddress)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 阿里云DM没有直接查询邮件状态的API
	// 建议使用以下方式：
	// 1. 使用GetTrackListByMailFromAndTagName查询发送记录
	// 2. 通过TagName追踪邮件
	// 3. 通过阿里云控制台查看统计报表

	// 这里返回一个提示信息
	span.SetAttributes(
		attribute.String("email.status.note", "aliyun_dm_does_not_support_direct_status_query"),
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
			"note":       "阿里云DM暂不支持直接查询单封邮件状态，请使用GetTrackListByMailFromAndTagName查询发送记录",
			"message_id": query.MessageID,
			"timestamp":  time.Now().Unix(),
		},
	}, nil
}

// GetTrackList 查询发送记录（基于AccountName和TagName）
// 注：这是阿里云DM提供的主要查询方式，但返回的是统计数据而非单封邮件状态
func (c *AliyunDMClient) GetTrackList(ctx context.Context, accountName, tagName string, startTime, endTime time.Time) (*EmailStatusResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.aliyun_dm")
	spanName := "aliyun_dm.get_track_list"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	logger.InfoWithCtx(ctx, "开始查询发送记录",
		logger.String("account_name", accountName),
		logger.String("tag_name", tagName))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "aliyun_dm"),
		attribute.String("email.account_name", accountName),
		attribute.String("email.tag_name", tagName),
		requestIDAttr(ctx),
	)

	queryStartTime := time.Now()

	// 构建请求
	request := &dm.GetTrackListByMailFromAndTagNameRequest{}
	request.SetAccountName(accountName)
	request.SetTagName(tagName)
	request.SetStartTime(startTime.Format("2006-01-02 15:04:05"))
	request.SetEndTime(endTime.Format("2006-01-02 15:04:05"))

	// 调用API
	response, err := c.client.GetTrackListByMailFromAndTagName(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		duration := time.Since(queryStartTime)
		span.SetAttributes(
			attribute.Float64("email.query.duration_ms", float64(duration.Milliseconds())),
			requestIDAttr(ctx),
		)
		return &EmailStatusResult{
			Status: StatusFailed,
			Error:  fmt.Errorf("阿里云 DM 查询发送记录失败: %w", err),
		}, err
	}

	// 解析结果（响应体可能为 nil，先判再取——与腾讯 R1-1 同型的响应侧防护，终审 R7-2）
	if response == nil || response.Body == nil {
		err := fmt.Errorf("阿里云 DM 返回空响应")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &EmailStatusResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	// 设置成功的追踪属性
	duration := time.Since(queryStartTime)
	span.SetAttributes(
		attribute.Float64("email.query.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "track list queried successfully")

	// 阿里云DM的TrackList返回的是统计数据，不是单封邮件状态
	// 返回一个提示信息
	return &EmailStatusResult{
		Status: StatusSuccess,
		Data:   nil,
		Extra: map[string]interface{}{
			"note":       "阿里云DM的TrackList API返回的是统计数据，建议使用控制台查看详细发送记录",
			"total":      tea.Int32Value(response.Body.Total),
			"page_no":    tea.Int32Value(response.Body.PageNo),
			"page_size":  tea.Int32Value(response.Body.PageSize),
			"request_id": tea.StringValue(response.Body.RequestId),
			"timestamp":  time.Now().Unix(),
		},
	}, nil
}

// validateRequest 验证请求参数
func (c *AliyunDMClient) validateRequest(ctx context.Context, req *SendRequest) error {
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

	// 阿里云DM限制：单次最多100个收件人
	if len(req.To) > 100 {
		return fmt.Errorf("单次请求最多支持 100 个收件人")
	}

	return nil
}

// SendTemplateEmail 发送模板邮件（阿里云DM BatchSendMail）。
// receiversName: 预先在控制台创建并上传了收件人的收件人列表名称（API 不接受内联地址列表）；
// templateName: 预先创建并通过审核的模板名称。
// 注：BatchSendMail 请求不携带模板变量——模板变量由收件人列表的列提供（上传的 CSV/TXT 中的 {变量} 列）；
// TagName 是预先创建的邮件标签名称（CreateTag），不能存 JSON。
// 语义按阿里云官方 API 文档修正；本轮无凭据未实测，仅静态检查通过。
func (c *AliyunDMClient) SendTemplateEmail(ctx context.Context, from, receiversName, templateName string) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("goemail.aliyun_dm")
	spanName := "aliyun_dm.send_template_email"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	logger.InfoWithCtx(ctx, "开始发送模板邮件",
		logger.String("from", from),
		logger.String("receivers_name", receiversName),
		logger.String("template_name", templateName))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("email.provider", "aliyun_dm"),
		attribute.String("email.from", maskEmail(from)),
		attribute.String("email.receivers_name", receiversName),
		attribute.String("email.template_name", templateName),
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
	// 由云端拦截，同一个 from 走两个入口行为不同（终审 P2-1）；
	// receiversName/templateName 是云侧预创建的标识符，不做格式校验
	if !ValidateEmail(ctx, from) {
		err := fmt.Errorf("发件人地址格式不合法: %q", from)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	if receiversName == "" {
		err := fmt.Errorf("收件人列表名称不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	if templateName == "" {
		err := fmt.Errorf("模板名称不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	// 构建批量发送请求（阿里云DM使用BatchSendMail发送模板）
	request := &dm.BatchSendMailRequest{
		AccountName:   tea.String(from),          // 发信地址
		AddressType:   tea.Int32(1),              // 1为发信地址
		TemplateName:  tea.String(templateName),  // 模板名称
		ReceiversName: tea.String(receiversName), // 预先创建的收件人列表名称（非地址列表）
	}

	// 调用API
	response, err := c.client.BatchSendMail(request)
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
			Error:  fmt.Errorf("阿里云 DM 发送模板邮件失败: %w", err),
		}, err
	}

	// 解析结果（响应体可能为 nil，先判再取——与 SendEmail 同型防护，终审 R7-2）
	if response == nil || response.Body == nil {
		err := fmt.Errorf("阿里云 DM 返回空响应")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}
	result := &SendResult{
		MessageID: "",
		Status:    StatusSuccess,
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
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "email sent successfully")

	return result, nil
}
