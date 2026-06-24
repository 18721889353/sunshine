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

// TencentSMSClient 腾讯云短信客户端
type TencentSMSClient struct {
	config *Config
	client *tencentSms.Client
}

// newTencentSMSClient 创建腾讯云短信客户端
func newTencentSMSClient(cfg *Config) (*TencentSMSClient, error) {
	if cfg.AccessKeyID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("tencent sms: access key id and secret key are required")
	}
	if cfg.TencentAppID == "" {
		return nil, fmt.Errorf("tencent sms: app id is required")
	}
	if cfg.Region == "" {
		cfg.Region = "ap-shanghai" // 默认上海区域
	}

	// 创建认证对象
	cred := tencentCommon.NewCredential(cfg.AccessKeyID, cfg.SecretKey)

	// 创建客户端配置
	cpf := tencentProfile.NewClientProfile()
	cpf.HttpProfile.ReqMethod = "POST"
	cpf.HttpProfile.ReqTimeout = 15 // 15秒超时
	cpf.HttpProfile.Endpoint = "sms.tencentcloudapi.com"
	cpf.SignMethod = "TC3-HMAC-SHA256"

	// 创建SMS客户端
	client, err := tencentSms.NewClient(cred, cfg.Region, cpf)
	if err != nil {
		return nil, fmt.Errorf("create tencent sms client failed: %w", err)
	}

	return &TencentSMSClient{
		config: cfg,
		client: client,
	}, nil
}

// SendSMS 发送短信
func (c *TencentSMSClient) SendSMS(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gosms")
	spanName := "sms.tencent.send"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "开始发送腾讯短信",
		logger.String("phone", req.PhoneNumbers[0]),
		logger.String("template_id", req.TemplateID))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("sms.provider", string(ProviderTypeTencentSMS)),
		attribute.String("sms.template_id", req.TemplateID),
		attribute.Int("sms.phone_count", len(req.PhoneNumbers)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 验证手机号
	for _, phone := range req.PhoneNumbers {
		if !ValidatePhoneNumber(ctx, phone) {
			err := fmt.Errorf("invalid phone number: %s", phone)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return &SendResult{
				Status: StatusFailed,
				Error:  err,
			}, err
		}
	}

	// 创建请求
	request := tencentSms.NewSendSmsRequest()

	// 设置短信应用ID
	request.SmsSdkAppId = tencentCommon.StringPtr(c.config.TencentAppID)

	// 设置签名
	if req.SignName != "" {
		request.SignName = tencentCommon.StringPtr(req.SignName)
	} else {
		// 如果没有指定签名，使用配置中的默认签名（如果有的话）
		request.SignName = tencentCommon.StringPtr("")
	}

	// 设置手机号列表
	phoneNumbers := make([]*string, 0, len(req.PhoneNumbers))
	for _, phone := range req.PhoneNumbers {
		formattedPhone := FormatPhoneNumber(phone)
		phoneNumbers = append(phoneNumbers, tencentCommon.StringPtr(formattedPhone))
	}
	request.PhoneNumberSet = phoneNumbers

	// 设置模板ID
	request.TemplateId = tencentCommon.StringPtr(req.TemplateID)

	// 设置模板参数
	if len(req.TemplateParams) > 0 {
		templateParams := make([]*string, 0, len(req.TemplateParams))
		for _, value := range req.TemplateParams {
			templateParams = append(templateParams, tencentCommon.StringPtr(value))
		}
		request.TemplateParamSet = templateParams
	}

	// 发送短信
	response, err := c.client.SendSms(request)
	if err != nil {
		logger.ErrorWithCtx(ctx, "腾讯短信发送失败",
			logger.Err(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SendResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	// 解析响应
	result := &SendResult{
		Extra: make(map[string]interface{}),
	}

	if response.Response != nil && response.Response.SendStatusSet != nil {
		status := response.Response.SendStatusSet[0]

		if status.SerialNo != nil {
			result.MessageID = *status.SerialNo
		}
		if status.PhoneNumber != nil {
			result.PhoneNumber = *status.PhoneNumber
		}

		// 检查发送状态
		if status.Code != nil && *status.Code == "Ok" {
			result.Status = StatusSuccess
			logger.InfoWithCtx(ctx, "腾讯短信发送成功",
				logger.String("message_id", result.MessageID),
				logger.String("phone", result.PhoneNumber))
		} else {
			result.Status = StatusFailed
			var code, message string
			if status.Code != nil {
				code = *status.Code
			}
			if status.Message != nil {
				message = *status.Message
			}
			errMsg := fmt.Sprintf("send failed: code=%s, message=%s", code, message)
			result.Error = fmt.Errorf("%s", errMsg)
			logger.ErrorWithCtx(ctx, "腾讯短信发送失败",
				logger.String("code", code),
				logger.String("message", message))
			span.RecordError(result.Error)
			span.SetStatus(codes.Error, errMsg)
		}

		// 保存额外信息
		if status.Code != nil {
			result.Extra["Code"] = *status.Code
		}
		if status.Message != nil {
			result.Extra["Message"] = *status.Message
		}
		if status.SerialNo != nil {
			result.Extra["SerialNo"] = *status.SerialNo
		}
		if status.Fee != nil {
			result.Extra["Fee"] = *status.Fee
		}
	}

	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.String("sms.message_id", result.MessageID),
		attribute.String("sms.status", result.Status),
		attribute.Float64("sms.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "sms sent")

	return result, nil
}

// SendBatchSMS 批量发送短信
func (c *TencentSMSClient) SendBatchSMS(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
	results := make([]*SendResult, 0, len(reqs))

	for _, req := range reqs {
		result, err := c.SendSMS(ctx, req)
		if err != nil {
			logger.WarnWithCtx(ctx, "批量发送中单个短信失败",
				logger.String("phone", req.PhoneNumbers[0]),
				logger.Err(err))
		}
		results = append(results, result)
	}

	return results, nil
}

// GetSMSStatus 查询短信发送状态
func (c *TencentSMSClient) GetSMSStatus(ctx context.Context, query *SMSStatusQuery) (*SMSStatusResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gosms")
	spanName := "sms.tencent.query_status"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "开始查询腾讯短信状态",
		logger.String("phone", query.PhoneNumber))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("sms.provider", string(ProviderTypeTencentSMS)),
		attribute.String("sms.query_phone", query.PhoneNumber),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	// 创建请求
	request := tencentSms.NewPullSmsSendStatusByPhoneNumberRequest()

	// 设置手机号
	phoneNumber := FormatPhoneNumber(query.PhoneNumber)
	request.PhoneNumber = tencentCommon.StringPtr(phoneNumber)

	// 设置发送时间（Unix时间戳）
	request.BeginTime = tencentCommon.Uint64Ptr(uint64(query.FromDate.Unix()))

	// 设置偏移量和限制
	request.Offset = tencentCommon.Uint64Ptr(query.Offset)
	request.Limit = tencentCommon.Uint64Ptr(query.Limit)

	// 设置短信应用ID
	request.SmsSdkAppId = tencentCommon.StringPtr(c.config.TencentAppID)

	// 查询状态
	response, err := c.client.PullSmsSendStatusByPhoneNumber(request)
	if err != nil {
		logger.ErrorWithCtx(ctx, "查询腾讯短信状态失败",
			logger.Err(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &SMSStatusResult{
			Status: StatusFailed,
			Error:  err,
		}, err
	}

	// 解析响应
	result := &SMSStatusResult{
		Status: StatusSuccess,
		Data:   make([]*SMSStatus, 0),
		Extra:  make(map[string]interface{}),
	}

	if response.Response != nil && response.Response.PullSmsSendStatusSet != nil {
		for _, status := range response.Response.PullSmsSendStatusSet {
			smsStatus := &SMSStatus{
				MessageID:   getStringValue(status.SerialNo),
				PhoneNumber: getStringValue(status.PhoneNumber),
				Extra:       make(map[string]interface{}),
			}

			// 解析状态
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

			result.Data = append(result.Data, smsStatus)
		}
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

// GetProviderType 获取提供商类型
func (c *TencentSMSClient) GetProviderType() ProviderType {
	return ProviderTypeTencentSMS
}
