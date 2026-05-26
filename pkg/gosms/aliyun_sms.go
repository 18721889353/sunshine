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

// AliyunSMSClient 阿里云短信客户端
type AliyunSMSClient struct {
	config *Config
	client *dysmsapi.Client
}

// newAliyunSMSClient 创建阿里云短信客户端
func newAliyunSMSClient(cfg *Config) (*AliyunSMSClient, error) {
	if cfg.AccessKeyID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("aliyun sms: access key id and secret key are required")
	}

	// 创建配置
	config := &openapi.Config{
		AccessKeyId:     tea.String(cfg.AccessKeyID),
		AccessKeySecret: tea.String(cfg.SecretKey),
		Endpoint:        tea.String("dysmsapi.aliyuncs.com"),
	}

	if cfg.Region != "" {
		config.RegionId = tea.String(cfg.Region)
	} else {
		config.RegionId = tea.String("cn-hangzhou") // 默认杭州区域
	}

	// 创建客户端
	client, err := dysmsapi.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("create aliyun sms client failed: %w", err)
	}

	return &AliyunSMSClient{
		config: cfg,
		client: client,
	}, nil
}

// SendSMS 发送短信
func (c *AliyunSMSClient) SendSMS(ctx context.Context, req *SendRequest) (*SendResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gosms")
	spanName := "sms.aliyun.send"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "开始发送阿里短信",
		logger.String("phone", req.PhoneNumbers[0]),
		logger.String("template_id", req.TemplateID))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("sms.provider", string(ProviderTypeAliyunSMS)),
		attribute.String("sms.template_id", req.TemplateID),
		attribute.Int("sms.phone_count", len(req.PhoneNumbers)),
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
	sendReq := &dysmsapi.SendSmsRequest{
		PhoneNumbers: tea.String(req.PhoneNumbers[0]), // 阿里云单次只支持一个号码
		SignName:     tea.String(req.SignName),
		TemplateCode: tea.String(req.TemplateID),
	}

	// 设置模板参数
	if len(req.TemplateParams) > 0 {
		templateParamJSON, err := json.Marshal(req.TemplateParams)
		if err != nil {
			return nil, fmt.Errorf("marshal template params: %w", err)
		}
		sendReq.TemplateParam = tea.String(string(templateParamJSON))
	}

	// 发送短信
	runtime := &util.RuntimeOptions{}
	resp, err := c.client.SendSmsWithOptions(sendReq, runtime)
	if err != nil {
		logger.ErrorWithCtx(ctx, "阿里短信发送失败",
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

	if resp.Body != nil {
		result.MessageID = tea.StringValue(resp.Body.BizId)
		result.PhoneNumber = req.PhoneNumbers[0]

		// 检查发送状态
		if tea.StringValue(resp.Body.Code) == "OK" {
			result.Status = StatusSuccess
			logger.InfoWithCtx(ctx, "阿里短信发送成功",
				logger.String("message_id", result.MessageID),
				logger.String("phone", result.PhoneNumber))
		} else {
			result.Status = StatusFailed
			errMsg := fmt.Sprintf("send failed: code=%s, message=%s",
				tea.StringValue(resp.Body.Code),
				tea.StringValue(resp.Body.Message))
			result.Error = fmt.Errorf("%s", errMsg)
			logger.ErrorWithCtx(ctx, "阿里短信发送失败",
				logger.String("code", tea.StringValue(resp.Body.Code)),
				logger.String("message", tea.StringValue(resp.Body.Message)))
			span.RecordError(result.Error)
			span.SetStatus(codes.Error, errMsg)
		}

		// 保存额外信息
		result.Extra["Code"] = tea.StringValue(resp.Body.Code)
		result.Extra["Message"] = tea.StringValue(resp.Body.Message)
		result.Extra["BizId"] = tea.StringValue(resp.Body.BizId)
	}

	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.String("sms.message_id", result.MessageID),
		attribute.String("sms.status", result.Status),
		attribute.Float64("sms.duration_ms", float64(duration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "sms sent")

	return result, nil
}

// SendBatchSMS 批量发送短信
func (c *AliyunSMSClient) SendBatchSMS(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error) {
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
func (c *AliyunSMSClient) GetSMSStatus(ctx context.Context, query *SMSStatusQuery) (*SMSStatusResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gosms")
	spanName := "sms.aliyun.query_status"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "开始查询阿里短信状态",
		logger.String("phone", query.PhoneNumber))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("sms.provider", string(ProviderTypeAliyunSMS)),
		attribute.String("sms.query_phone", query.PhoneNumber),
	)

	startTime := time.Now()

	// 创建请求
	request := &dysmsapi.QuerySendDetailsRequest{
		PhoneNumber: tea.String(query.PhoneNumber),
		SendDate:    tea.String(query.FromDate.Format("20060102")), // 格式：yyyyMMdd
		CurrentPage: tea.Int64(int64(query.Offset/10 + 1)),         // 页码从1开始
		PageSize:    tea.Int64(int64(query.Limit)),
	}

	// 如果有 BizId（消息ID），也传入
	if query.MessageID != "" {
		request.BizId = tea.String(query.MessageID)
	}

	// 查询状态
	runtime := &util.RuntimeOptions{}
	resp, err := c.client.QuerySendDetailsWithOptions(request, runtime)
	if err != nil {
		logger.ErrorWithCtx(ctx, "查询阿里短信状态失败",
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

	if resp.Body != nil && resp.Body.SmsSendDetailDTOs != nil && len(resp.Body.SmsSendDetailDTOs.SmsSendDetailDTO) > 0 {
		for _, detail := range resp.Body.SmsSendDetailDTOs.SmsSendDetailDTO {
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

			result.Data = append(result.Data, smsStatus)
		}
	}

	// 保存总数
	if resp.Body.TotalCount != nil {
		result.Extra["TotalCount"] = *resp.Body.TotalCount
	}

	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("sms.status_count", len(result.Data)),
		attribute.Float64("sms.duration_ms", float64(duration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "status queried")

	logger.InfoWithCtx(ctx, "查询阿里短信状态成功",
		logger.Int("count", len(result.Data)),
		logger.Float64("duration_ms", float64(duration.Milliseconds())))

	return result, nil
}

// GetProviderType 获取提供商类型
func (c *AliyunSMSClient) GetProviderType() ProviderType {
	return ProviderTypeAliyunSMS
}
