package gosms

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v4/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
)

const (
	// 阿里云单次请求超时预算（毫秒）。
	// tea@v1.4.0/dara/core.go:326 将 ConnectTimeout+ReadTimeout 相加作为 http.Client.Timeout：
	// 5000+10000=15s 总超时，对齐腾讯云 ReqTimeout=15。
	// 背景：dysmsapi v4.1.3 的 SendSmsWithOptions/QuerySendDetailsWithOptions 均不接受 context
	//（client/client.go 中无 WithContext 变体，grep 零结果），调用方的取消/超时无法透传，
	// 只能在 HTTP 层设超时兜底。
	aliyunConnectTimeoutMs = 5000
	aliyunReadTimeoutMs    = 10000
	// aliyunMaxPageSize 阿里云 QuerySendDetails 的 PageSize 上限 50（README 参数说明既有结论）。
	aliyunMaxPageSize uint64 = 50
)

// newAliyunRuntime 构造带超时的运行时选项。
// RuntimeOptions 含指针字段、无并发安全保证，每次调用新建。
func newAliyunRuntime() *util.RuntimeOptions {
	runtime := &util.RuntimeOptions{}
	runtime.SetConnectTimeout(aliyunConnectTimeoutMs)
	runtime.SetReadTimeout(aliyunReadTimeoutMs)
	return runtime
}

// AliyunSMSClient 阿里云短信客户端
type AliyunSMSClient struct {
	config *Config
	client *dysmsapi.Client
}

// newAliyunSMSClient 创建阿里云短信客户端
func newAliyunSMSClient(cfg *Config) (*AliyunSMSClient, error) {
	if cfg.AccessKeyID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("阿里云短信: AccessKeyID 与 SecretKey 不能为空")
	}

	// 客户端持有 Config 副本：不修改调用方结构体，且调用方后续修改 *Config
	// 不会影响已创建客户端的默认签名等行为（与腾讯侧一致的副本语义）
	effectiveCfg := *cfg

	// 创建配置
	config := &openapi.Config{
		AccessKeyId:     tea.String(effectiveCfg.AccessKeyID),
		AccessKeySecret: tea.String(effectiveCfg.SecretKey),
		Endpoint:        tea.String("dysmsapi.aliyuncs.com"),
	}

	if effectiveCfg.Region != "" {
		config.RegionId = tea.String(effectiveCfg.Region)
	} else {
		config.RegionId = tea.String("cn-hangzhou") // 默认杭州区域
	}

	// 创建客户端
	client, err := dysmsapi.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("创建阿里云短信客户端失败: %w", err)
	}

	return &AliyunSMSClient{
		config: &effectiveCfg,
		client: client,
	}, nil
}

// SendSMS 发送短信（阿里云单次仅支持一个号码，多号码请使用 SendBatchSMS）。
func (c *AliyunSMSClient) SendSMS(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gosms")
	ctx, span := tracer.Start(ctx, "sms.aliyun.send", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// 入口校验：非法入参在发起任何网络调用前拦截（fail loud）
	if req == nil {
		err := fmt.Errorf("发送请求不能为空")
		return failedResult(err), failSpan(span, err)
	}
	// 签名兜底到 Config 默认值（用副本，不修改调用方结构体）后统一校验
	effective := withSignFallback(req, c.config.AliyunSignName)
	if err := effective.Validate(); err != nil {
		return failedResult(err), failSpan(span, err)
	}
	// 阿里云 SendSms 单次只支持一个号码；旧实现取 PhoneNumbers[0] 静默丢弃其余号码，
	// 多号码必须走 SendBatchSMS 逐条发送
	if len(effective.PhoneNumbers) > 1 {
		err := fmt.Errorf("阿里云单次仅支持一个号码（当前 %d 个），多号码请使用 SendBatchSMS", len(effective.PhoneNumbers))
		return failedResult(err), failSpan(span, err)
	}

	logger.DebugWithCtx(ctx, "开始发送阿里短信",
		logger.String("phone", maskPhone(effective.PhoneNumbers[0])),
		logger.String("template_id", effective.TemplateID))

	// 设置追踪属性（手机号属 PII，span 属性不记录明文）
	span.SetAttributes(
		attribute.String("sms.provider", string(ProviderTypeAliyunSMS)),
		attribute.String("sms.template_id", effective.TemplateID),
		attribute.Int("sms.phone_count", len(effective.PhoneNumbers)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 创建请求（入口校验已保证列表非空且至多一个号码）
	sendReq := &dysmsapi.SendSmsRequest{
		PhoneNumbers: tea.String(effective.PhoneNumbers[0]),
		SignName:     tea.String(effective.SignName),
		TemplateCode: tea.String(effective.TemplateID),
	}

	// 设置模板参数（阿里云为命名参数，转 JSON 对象，顺序无关）
	if len(effective.TemplateParams) > 0 {
		params := make(map[string]string, len(effective.TemplateParams))
		for _, p := range effective.TemplateParams {
			params[p.Key] = p.Value
		}
		templateParamJSON, err := json.Marshal(params)
		if err != nil {
			// 旧实现此处直接 return nil，span 未记错误且与其它失败路径返回结构不一致
			err = fmt.Errorf("序列化模板参数失败: %w", err)
			return failedResult(err), failSpan(span, err)
		}
		sendReq.TemplateParam = tea.String(string(templateParamJSON))
	}

	// 发送短信
	resp, err := c.client.SendSmsWithOptions(sendReq, newAliyunRuntime())
	if err != nil {
		logger.ErrorWithCtx(ctx, "阿里短信发送失败",
			logger.Err(err))
		return failedResult(err), failSpan(span, err)
	}

	// 解析响应：结构异常返回 error（failSpan）；业务失败只置 result，err 保持 nil
	result, parseErr := parseSendSmsResult(resp)
	if parseErr != nil {
		logger.ErrorWithCtx(ctx, "阿里短信响应解析失败",
			logger.Err(parseErr))
		return failedResult(parseErr), failSpan(span, parseErr)
	}
	result.PhoneNumber = effective.PhoneNumbers[0]

	if result.Status == StatusFailed {
		// 业务失败（Code != OK）：span 记为 Error，且不再被末尾的成功埋点覆盖
		//（旧实现末尾无条件 SetStatus(codes.Ok)，失败链路在 tracing 中显示为成功）。
		// 返回语义保持不变：err == nil，调用方以 result.Status 判断业务失败。
		logger.ErrorWithCtx(ctx, "阿里短信发送失败",
			logger.Err(result.Error))
		span.RecordError(result.Error)
		span.SetStatus(codes.Error, result.Error.Error())
		return result, nil
	}

	duration := time.Since(startTime)
	logger.InfoWithCtx(ctx, "阿里短信发送成功",
		logger.String("message_id", result.MessageID),
		logger.String("phone", maskPhone(result.PhoneNumber)))

	span.SetAttributes(
		attribute.String("sms.message_id", result.MessageID),
		attribute.String("sms.status", result.Status),
		attribute.Float64("sms.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "sms sent")

	return result, nil
}

// parseSendSmsResult 解析阿里云发送响应（纯函数，便于单测构造响应守护分支）。
//   - 结构异常（响应/响应体为空）→ 返回 error，由调用方 failSpan + failedResult；
//   - 业务失败（Body.Code != "OK"）→ 不返回 error，而是 result.Status=failed + result.Error 非空，
//     保持既有语义「业务失败 err==nil，调用方以 result.Status 判断」。
func parseSendSmsResult(resp *dysmsapi.SendSmsResponse) (*SendResult, error) {
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("阿里短信响应体为空")
	}

	result := &SendResult{
		MessageID: tea.StringValue(resp.Body.BizId),
		Extra: map[string]interface{}{
			"Code":    tea.StringValue(resp.Body.Code),
			"Message": tea.StringValue(resp.Body.Message),
			"BizId":   tea.StringValue(resp.Body.BizId),
		},
	}

	if tea.StringValue(resp.Body.Code) == "OK" {
		result.Status = StatusSuccess
		return result, nil
	}

	result.Status = StatusFailed
	result.Error = fmt.Errorf("发送失败: code=%s, message=%s",
		tea.StringValue(resp.Body.Code),
		tea.StringValue(resp.Body.Message))
	return result, nil
}

// SendBatchSMS 批量发送短信（并发调度，单条失败不中断批量）。
// 并发上限与保序由 runBatchSMS 统一保证（与腾讯云对称），输出顺序恒等于请求顺序。
// 外层 error 为聚合结果：任一条目失败（传输层 err 或业务 Status=failed）时非 nil
// （errors.Join 拼接带序号），全成功为 nil；无论成败 results 都包含完整的逐条结果。
func (c *AliyunSMSClient) SendBatchSMS(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
	return runBatchSMS(ctx, reqs, c.config.BatchConcurrency, c.SendSMS)
}

// GetSMSStatus 查询短信发送状态
func (c *AliyunSMSClient) GetSMSStatus(ctx context.Context, query *SMSStatusQuery) (*SMSStatusResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gosms")
	ctx, span := tracer.Start(ctx, "sms.aliyun.query_status", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// 入口归一化：nil / 缺手机号 / 日期倒置 / PageSize 超上限截断，在发起网络调用前完成
	q, err := normalizeAliyunStatusQuery(query)
	if err != nil {
		return failedStatusResult(err), failSpan(span, err)
	}

	logger.DebugWithCtx(ctx, "开始查询阿里短信状态",
		logger.String("phone", maskPhone(q.PhoneNumber)))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("sms.provider", string(ProviderTypeAliyunSMS)),
		attribute.String("sms.query_phone", maskPhone(q.PhoneNumber)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 页码换算由 aliyunCurrentPage 完成（纯函数，单测守护）
	request := &dysmsapi.QuerySendDetailsRequest{
		PhoneNumber: tea.String(q.PhoneNumber),
		SendDate:    tea.String(q.FromDate.Format("20060102")), // 格式：yyyyMMdd
		CurrentPage: tea.Int64(aliyunCurrentPage(q.Offset, q.Limit)),
		PageSize:    tea.Int64(int64(q.Limit)),
	}

	// 如果有 BizId（消息ID），也传入
	if q.MessageID != "" {
		request.BizId = tea.String(q.MessageID)
	}

	// 查询状态
	resp, err := c.client.QuerySendDetailsWithOptions(request, newAliyunRuntime())
	if err != nil {
		logger.ErrorWithCtx(ctx, "查询阿里短信状态失败",
			logger.Err(err))
		return failedStatusResult(err), failSpan(span, err)
	}

	// 解析响应：结构异常/查询失败返回 error；「查询到 0 条」是合法结果不是错误
	data, extra, parseErr := parseQuerySendDetails(resp)
	if parseErr != nil {
		logger.ErrorWithCtx(ctx, "阿里短信查询响应解析失败",
			logger.Err(parseErr))
		return failedStatusResult(parseErr), failSpan(span, parseErr)
	}

	result := &SMSStatusResult{
		Status: StatusSuccess,
		Data:   data,
		Extra:  extra,
	}

	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("sms.status_count", len(result.Data)),
		attribute.Float64("sms.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "status queried")

	logger.InfoWithCtx(ctx, "查询阿里短信状态成功",
		logger.Int("count", len(result.Data)),
		logger.Float64("duration_ms", float64(duration.Milliseconds())))

	return result, nil
}

// normalizeAliyunStatusQuery 归一化查询参数并按阿里上限截断 Limit（纯函数，便于单测）。
// 归一化保证 Limit ≥1，页码换算无除零风险。
func normalizeAliyunStatusQuery(query *SMSStatusQuery) (SMSStatusQuery, error) {
	q, err := normalizeStatusQuery(query)
	if err != nil {
		return SMSStatusQuery{}, err
	}
	if q.Limit > aliyunMaxPageSize {
		q.Limit = aliyunMaxPageSize
	}
	return q, nil
}

// aliyunCurrentPage 由偏移量与每页条数换算阿里云页码（从 1 开始）。
// 旧实现写死 Offset/10+1，Limit≠10 时页码算错
// （如 Limit=20、Offset=20 应为第 2 页，旧实现算出第 3 页）。
func aliyunCurrentPage(offset, limit uint64) int64 {
	if limit == 0 {
		return 1 // 防御：normalizeAliyunStatusQuery 已保证 ≥1，此处仅避免除零 panic
	}
	return int64(offset/limit) + 1
}

// parseQuerySendDetails 解析阿里云查询响应（纯函数）：
//   - 结构异常（响应/响应体为空）或业务失败（Body.Code != "OK"）→ 返回 error，
//     由调用方 failSpan + failedStatusResult。旧实现不检查 Body.Code，
//     查询失败（如手机号不合法）会被当作「0 条记录」静默吞掉；
//   - 正常响应返回明细列表与 Extra（「查询到 0 条」是合法结果）。
func parseQuerySendDetails(resp *dysmsapi.QuerySendDetailsResponse) ([]*SMSStatus, map[string]interface{}, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil, fmt.Errorf("阿里短信查询响应体为空")
	}
	if tea.StringValue(resp.Body.Code) != "OK" {
		return nil, nil, fmt.Errorf("查询失败: code=%s, message=%s",
			tea.StringValue(resp.Body.Code),
			tea.StringValue(resp.Body.Message))
	}

	data := make([]*SMSStatus, 0)
	if resp.Body.SmsSendDetailDTOs != nil {
		data = parseSendDetailItems(resp.Body.SmsSendDetailDTOs.SmsSendDetailDTO)
	}

	extra := make(map[string]interface{})
	if resp.Body.TotalCount != nil {
		extra["TotalCount"] = *resp.Body.TotalCount
	}
	return data, extra, nil
}

// parseSendDetailItems 将阿里云发送明细 DTO 映射为统一状态列表（纯函数，
// 单测可直接构造 DTO 守护状态映射；details 为空返回空列表）。
func parseSendDetailItems(details []*dysmsapi.QuerySendDetailsResponseBodySmsSendDetailDTOsSmsSendDetailDTO) []*SMSStatus {
	data := make([]*SMSStatus, 0, len(details))
	for _, detail := range details {
		if detail == nil {
			continue
		}
		smsStatus := &SMSStatus{
			MessageID:   getStringValue(detail.OutId),
			PhoneNumber: getStringValue(detail.PhoneNum),
			Extra:       make(map[string]interface{}),
		}

		// 解析发送状态
		// 1: 等待回执, 2: 发送失败, 3: 发送成功
		if detail.SendStatus != nil {
			switch *detail.SendStatus {
			case 3:
				smsStatus.Status = StatusDelivered
			case 2:
				smsStatus.Status = StatusFailed
			default:
				smsStatus.Status = StatusPending
			}
		} else {
			// SendStatus 字段缺失（边缘返回）时按「处理中」兜底，与腾讯侧
			// parsePullSendStatus 的 default 分支一致——不给零值空串，
			// 避免调用方 switch status 落 default 分支误报（R6 全仓复核 P1）
			smsStatus.Status = StatusPending
		}

		// 状态码和消息
		if detail.ErrCode != nil {
			errCode := *detail.ErrCode
			if errCode == "DELIVERED" {
				smsStatus.StatusCode = 0
				smsStatus.StatusMessage = "送达成功"
			} else {
				smsStatus.StatusCode = 1
				smsStatus.StatusMessage = errCode
			}
		}

		// 解析时间
		if detail.ReceiveDate != nil {
			if receiveTime, err := time.Parse("2006-01-02 15:04:05", *detail.ReceiveDate); err == nil {
				smsStatus.DeliverTime = receiveTime
			}
		}

		// 保存额外信息
		if detail.Content != nil {
			smsStatus.Extra["Content"] = *detail.Content
		}
		if detail.TemplateCode != nil {
			smsStatus.Extra["TemplateCode"] = *detail.TemplateCode
		}
		if detail.SendDate != nil {
			smsStatus.Extra["SendDate"] = *detail.SendDate
		}

		data = append(data, smsStatus)
	}
	return data
}

// GetProviderType 获取提供商类型
func (c *AliyunSMSClient) GetProviderType() ProviderType {
	return ProviderTypeAliyunSMS
}
