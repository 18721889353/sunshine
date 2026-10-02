package gosms

import (
	"context"
	"fmt"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	tencentCommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tencentProfile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tencentSms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

// tencentMaxPageSize 腾讯云 PullSmsSendStatusByPhoneNumber 的 Limit 上限 100
// （models.go:1259 注释「拉取最大条数，最多 100」，超出会被 API 拒绝，按上限截断）。
// 查询日期回溯上限（7 天）不在此归一：由腾讯 API 校验并报错，透传原始错误比静默
// 截断查询范围更可诊断（README 已记录该限制）。
const tencentMaxPageSize uint64 = 100

// TencentSMSClient 腾讯云短信客户端
type TencentSMSClient struct {
	config *Config
	client *tencentSms.Client
}

// newTencentSMSClient 创建腾讯云短信客户端
func newTencentSMSClient(cfg *Config) (*TencentSMSClient, error) {
	if cfg.AccessKeyID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("腾讯云短信: SecretId 与 SecretKey 不能为空")
	}
	if cfg.TencentAppID == "" {
		return nil, fmt.Errorf("腾讯云短信: TencentAppID 不能为空")
	}
	// 默认区域写入副本，不修改调用方结构体（与 withSignFallback 的副本语义对齐；
	// 旧实现直接 cfg.Region = "ap-shanghai"，复用同一 *Config 的调用方会被意外写入默认值）
	region := cfg.Region
	if region == "" {
		region = "ap-shanghai" // 默认上海区域
	}
	effectiveCfg := *cfg
	effectiveCfg.Region = region

	// 创建认证对象
	cred := tencentCommon.NewCredential(effectiveCfg.AccessKeyID, effectiveCfg.SecretKey)

	// 创建客户端配置
	cpf := tencentProfile.NewClientProfile()
	cpf.HttpProfile.ReqMethod = "POST"
	cpf.HttpProfile.ReqTimeout = 15 // 15秒超时
	cpf.HttpProfile.Endpoint = "sms.tencentcloudapi.com"
	cpf.SignMethod = "TC3-HMAC-SHA256"

	// 创建SMS客户端
	client, err := tencentSms.NewClient(cred, region, cpf)
	if err != nil {
		return nil, fmt.Errorf("创建腾讯云短信客户端失败: %w", err)
	}

	return &TencentSMSClient{
		config: &effectiveCfg,
		client: client,
	}, nil
}

// SendSMS 发送短信（腾讯云原生支持多号码一次提交）。
func (c *TencentSMSClient) SendSMS(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gosms")
	ctx, span := tracer.Start(ctx, "sms.tencent.send", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// 入口校验：非法入参在发起任何网络调用前拦截（fail loud）
	if req == nil {
		err := fmt.Errorf("发送请求不能为空")
		return failedResult(err), failSpan(span, err)
	}
	// 签名兜底到 Config 默认值（用副本，不修改调用方结构体）后统一校验
	effective := withSignFallback(req, c.config.TencentSignName)
	if err := effective.Validate(); err != nil {
		return failedResult(err), failSpan(span, err)
	}

	logger.DebugWithCtx(ctx, "开始发送腾讯短信",
		logger.String("phone", maskPhone(effective.PhoneNumbers[0])),
		logger.String("template_id", effective.TemplateID))

	// 设置追踪属性（手机号属 PII，span 属性不记录明文）
	span.SetAttributes(
		attribute.String("sms.provider", string(ProviderTypeTencentSMS)),
		attribute.String("sms.template_id", effective.TemplateID),
		attribute.Int("sms.phone_count", len(effective.PhoneNumbers)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 创建请求
	request := tencentSms.NewSendSmsRequest()

	// 设置短信应用ID
	request.SmsSdkAppId = tencentCommon.StringPtr(c.config.TencentAppID)

	// 设置签名（兜底后必非空，Validate 已校验；旧实现传空串并注释「使用默认签名」是误导）
	request.SignName = tencentCommon.StringPtr(effective.SignName)

	// 设置手机号列表
	phoneNumbers := make([]*string, 0, len(effective.PhoneNumbers))
	for _, phone := range effective.PhoneNumbers {
		formattedPhone := FormatPhoneNumber(phone)
		phoneNumbers = append(phoneNumbers, tencentCommon.StringPtr(formattedPhone))
	}
	request.PhoneNumberSet = phoneNumbers

	// 设置模板ID
	request.TemplateId = tencentCommon.StringPtr(effective.TemplateID)

	// 设置模板参数：腾讯云为位置参数，严格按切片顺序填入（构建逻辑抽为纯函数由单测守护）
	if len(effective.TemplateParams) > 0 {
		request.TemplateParamSet = buildTemplateParamSet(effective.TemplateParams)
	}

	// 发送短信：WithContext 透传调用方的取消与超时（SDK 存在该变体，client.go:1719）
	response, err := c.client.SendSmsWithContext(ctx, request)
	if err != nil {
		logger.ErrorWithCtx(ctx, "腾讯短信发送失败",
			logger.Err(err))
		return failedResult(err), failSpan(span, err)
	}

	// 解析响应：结构异常返回 error（failSpan）；业务失败只置 result，err 保持 nil
	result, parseErr := parseSendSmsStatus(response)
	if parseErr != nil {
		logger.ErrorWithCtx(ctx, "腾讯短信响应解析失败",
			logger.Err(parseErr))
		return failedResult(parseErr), failSpan(span, parseErr)
	}

	if result.Status == StatusFailed {
		// 业务失败（Code != Ok）：span 记为 Error，且不再被末尾的成功埋点覆盖
		//（旧实现末尾无条件 SetStatus(codes.Ok)，失败链路在 tracing 中显示为成功）。
		// 返回语义保持不变：err == nil，调用方以 result.Status 判断业务失败。
		logger.ErrorWithCtx(ctx, "腾讯短信发送失败",
			logger.Err(result.Error))
		span.RecordError(result.Error)
		span.SetStatus(codes.Error, result.Error.Error())
		return result, nil
	}

	duration := time.Since(startTime)
	logger.InfoWithCtx(ctx, "腾讯短信发送成功",
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

// buildTemplateParamSet 按切片顺序生成腾讯云位置参数列表（纯函数）。
// 旧实现遍历 map[string]string，Go 随机遍历序导致模板 %1%/%2% 占位符错位
// （验证码等短信内容错乱），改用有序切片后由单测守护顺序。
func buildTemplateParamSet(params []TemplateParam) []*string {
	values := make([]*string, 0, len(params))
	for _, p := range params {
		values = append(values, tencentCommon.StringPtr(p.Value))
	}
	return values
}

// normalizeTencentStatusQuery 归一化查询参数并按腾讯上限截断 Limit（纯函数，便于单测）。
func normalizeTencentStatusQuery(query *SMSStatusQuery) (SMSStatusQuery, error) {
	q, err := normalizeStatusQuery(query)
	if err != nil {
		return SMSStatusQuery{}, err
	}
	if q.Limit > tencentMaxPageSize {
		q.Limit = tencentMaxPageSize
	}
	return q, nil
}

// parseSendSmsStatus 解析腾讯云发送响应（纯函数，便于单测构造响应守护分支）：
//   - 结构异常（响应为空 / SendStatusSet 为空）→ 返回 error，由调用方 failSpan + failedResult。
//     旧实现只判 SendStatusSet != nil 未判长度，空切片时 [0] 下标越界 panic；
//   - 多号码（len > 1）→ 逐号码结果填入 result.MultipleResults（旧实现只取 [0]，
//     其余号码的 SerialNo/状态被静默丢弃）；主结果取首个号码，任一号码失败则整体 failed；
//   - 业务失败（Code != "Ok"）→ 不返回 error，而是 result.Status=failed + result.Error 非空，
//     保持既有语义「业务失败 err==nil，调用方以 result.Status 判断」。
func parseSendSmsStatus(resp *tencentSms.SendSmsResponse) (*SendResult, error) {
	if resp == nil || resp.Response == nil || len(resp.Response.SendStatusSet) == 0 {
		return nil, fmt.Errorf("腾讯短信响应中无发送状态记录")
	}

	// 逐号码解析（跳过 nil 元素，守护旧实现对 nil 元素直接取字段的潜在 panic）
	items := make([]*SendResult, 0, len(resp.Response.SendStatusSet))
	for _, status := range resp.Response.SendStatusSet {
		if status == nil {
			continue
		}
		items = append(items, parseSendStatusItem(status))
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("腾讯短信响应中无发送状态记录")
	}

	// 单号码：逐号码结果即主结果，MultipleResults 保持 nil（不改变既有单号码语义）
	if len(items) == 1 {
		return items[0], nil
	}

	// 多号码：主结果独立构造（Extra 拷贝避免与子结果共享 map），逐号码状态入 MultipleResults
	first := items[0]
	extra := make(map[string]interface{}, len(first.Extra))
	for k, v := range first.Extra {
		extra[k] = v
	}
	result := &SendResult{
		MessageID:       first.MessageID,
		PhoneNumber:     first.PhoneNumber,
		Extra:           extra,
		MultipleResults: items,
	}

	failedCount := 0
	var firstFailed *SendResult
	for _, item := range items {
		if item.Status == StatusFailed {
			failedCount++
			if firstFailed == nil {
				firstFailed = item
			}
		}
	}
	if failedCount > 0 {
		result.Status = StatusFailed
		result.Error = fmt.Errorf("多号码发送部分失败: %d/%d 条失败，首个失败号码 %s: %v",
			failedCount, len(items), firstFailed.PhoneNumber, firstFailed.Error)
		return result, nil
	}
	result.Status = StatusSuccess
	return result, nil
}

// parseSendStatusItem 解析单个发送状态条目（纯函数，多号码场景复用）。
func parseSendStatusItem(status *tencentSms.SendStatus) *SendResult {
	result := &SendResult{
		MessageID:   getStringValue(status.SerialNo),
		PhoneNumber: getStringValue(status.PhoneNumber),
		Extra: map[string]interface{}{
			"Code":     getStringValue(status.Code),
			"Message":  getStringValue(status.Message),
			"SerialNo": getStringValue(status.SerialNo),
		},
	}
	if status.Fee != nil {
		result.Extra["Fee"] = *status.Fee
	}

	if getStringValue(status.Code) == "Ok" {
		result.Status = StatusSuccess
		return result
	}

	result.Status = StatusFailed
	result.Error = fmt.Errorf("发送失败: code=%s, message=%s",
		getStringValue(status.Code),
		getStringValue(status.Message))
	return result
}

// SendBatchSMS 批量发送短信（并发调度，单条失败不中断批量）。
// 并发上限与保序由 runBatchSMS 统一保证（与阿里云对称），输出顺序恒等于请求顺序。
// 外层 error 为聚合结果：任一条目失败（传输层 err 或业务 Status=failed）时非 nil
// （errors.Join 拼接带序号），全成功为 nil；无论成败 results 都包含完整的逐条结果。
func (c *TencentSMSClient) SendBatchSMS(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
	return runBatchSMS(ctx, reqs, c.config.BatchConcurrency, c.SendSMS)
}

// GetSMSStatus 查询短信发送状态
func (c *TencentSMSClient) GetSMSStatus(ctx context.Context, query *SMSStatusQuery) (*SMSStatusResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gosms")
	ctx, span := tracer.Start(ctx, "sms.tencent.query_status", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// 入口归一化：nil / 缺手机号 / 日期倒置 / Limit 超上限截断，在发起网络调用前完成
	q, err := normalizeTencentStatusQuery(query)
	if err != nil {
		return failedStatusResult(err), failSpan(span, err)
	}

	logger.DebugWithCtx(ctx, "开始查询腾讯短信状态",
		logger.String("phone", maskPhone(q.PhoneNumber)))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("sms.provider", string(ProviderTypeTencentSMS)),
		attribute.String("sms.query_phone", maskPhone(q.PhoneNumber)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 创建请求
	request := tencentSms.NewPullSmsSendStatusByPhoneNumberRequest()

	// 设置手机号
	phoneNumber := FormatPhoneNumber(q.PhoneNumber)
	request.PhoneNumber = tencentCommon.StringPtr(phoneNumber)

	// 设置发送时间（Unix时间戳；腾讯限制最大回溯 7 天，超限由 API 报错并透传）
	request.BeginTime = tencentCommon.Uint64Ptr(uint64(q.FromDate.Unix()))

	// 设置偏移量和限制
	request.Offset = tencentCommon.Uint64Ptr(q.Offset)
	request.Limit = tencentCommon.Uint64Ptr(q.Limit)

	// 设置短信应用ID
	request.SmsSdkAppId = tencentCommon.StringPtr(c.config.TencentAppID)

	// 查询状态：WithContext 透传调用方的取消与超时（SDK 存在该变体，client.go:1367）
	response, err := c.client.PullSmsSendStatusByPhoneNumberWithContext(ctx, request)
	if err != nil {
		logger.ErrorWithCtx(ctx, "查询腾讯短信状态失败",
			logger.Err(err))
		return failedStatusResult(err), failSpan(span, err)
	}

	// 解析响应；腾讯云的业务错误经 SDK 以 error 返回，
	// 但 err==nil 时 response 本身或其 Response 字段为 nil 属于结构异常：
	// 静默返回 0 条会让调用方无法区分「真查到 0 条」与「服务端结构异常」，
	// 且原实现未先判 response 自身，SDK 返回 (nil, nil) 时直接 panic；
	// 与阿里侧 resp.Body==nil 返回 error 的语义对称（R6 全仓复核 P1）
	if response == nil || response.Response == nil {
		err := fmt.Errorf("腾讯短信查询返回空响应")
		logger.ErrorWithCtx(ctx, "查询腾讯短信状态返回空响应", logger.Err(err))
		return failedStatusResult(err), failSpan(span, err)
	}
	data := parsePullSendStatus(response.Response.PullSmsSendStatusSet)

	result := &SMSStatusResult{
		Status: StatusSuccess,
		Data:   data,
		Extra:  make(map[string]interface{}),
	}

	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("sms.status_count", len(result.Data)),
		attribute.Float64("sms.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "status queried")

	logger.InfoWithCtx(ctx, "查询腾讯短信状态成功",
		logger.Int("count", len(result.Data)),
		logger.Float64("duration_ms", float64(duration.Milliseconds())))

	return result, nil
}

// parsePullSendStatus 将腾讯云下发状态列表映射为统一状态列表（纯函数，
// 单测可直接构造 DTO 守护状态映射；set 为空返回空列表——「查询到 0 条」是合法结果）。
func parsePullSendStatus(set []*tencentSms.PullSmsSendStatus) []*SMSStatus {
	data := make([]*SMSStatus, 0, len(set))
	for _, status := range set {
		if status == nil {
			continue
		}
		smsStatus := &SMSStatus{
			MessageID:   getStringValue(status.SerialNo),
			PhoneNumber: getStringValue(status.PhoneNumber),
			Extra:       make(map[string]interface{}),
		}

		// 解析状态：SUCCESS 下发成功、FAIL 下发失败、未回执（ReportStatus 为空）视为处理中
		reportStatus := getStringValue(status.ReportStatus)
		switch reportStatus {
		case "SUCCESS":
			smsStatus.Status = StatusDelivered
		case "FAIL":
			smsStatus.Status = StatusFailed
		default:
			smsStatus.Status = StatusPending
		}

		smsStatus.StatusMessage = getStringValue(status.Description)

		// 解析时间（UserReceiveTime 是 uint64 Unix 时间戳）
		if status.UserReceiveTime != nil && *status.UserReceiveTime > 0 {
			smsStatus.DeliverTime = time.Unix(int64(*status.UserReceiveTime), 0)
		}

		// 保存额外信息
		smsStatus.Extra["ReportStatus"] = reportStatus
		smsStatus.Extra["Description"] = getStringValue(status.Description)
		smsStatus.Extra["CountryCode"] = getStringValue(status.CountryCode)

		data = append(data, smsStatus)
	}
	return data
}

// GetProviderType 获取提供商类型
func (c *TencentSMSClient) GetProviderType() ProviderType {
	return ProviderTypeTencentSMS
}
