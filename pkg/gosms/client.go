// Package gosms 提供多平台短信发送功能
// 支持腾讯云SMS、阿里云SMS等多种发送方式
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
)

// ProviderType 短信服务提供商类型
type ProviderType string

const (
	// ProviderTypeTencentSMS 腾讯云短信服务
	ProviderTypeTencentSMS ProviderType = "tencent_sms"
	// ProviderTypeAliyunSMS 阿里云短信服务
	ProviderTypeAliyunSMS ProviderType = "aliyun_sms"
)

// SMSClient 短信客户端接口
type SMSClient interface {
	// SendSMS 发送短信
	SendSMS(ctx context.Context, req *SendRequest) (*SendResult, error)
	// SendBatchSMS 批量发送短信
	SendBatchSMS(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error)
	// GetSMSStatus 查询短信发送状态
	GetSMSStatus(ctx context.Context, query *SMSStatusQuery) (*SMSStatusResult, error)
	// GetProviderType 获取提供商类型
	GetProviderType() ProviderType
}

// SendRequest 发送请求
type SendRequest struct {
	PhoneNumbers   []string          // 手机号列表（国际格式，如 +8613711112222）
	TemplateID     string            // 模板ID
	TemplateParams map[string]string // 模板参数（键值对）
	SignName       string            // 签名名称
	Tags           map[string]string // 标签（用于追踪）
}

// SendResult 发送结果
type SendResult struct {
	MessageID   string                 // 消息ID
	PhoneNumber string                 // 手机号
	Status      string                 // 状态: success/failed
	Error       error                  // 错误信息
	Extra       map[string]interface{} // 额外信息（不同提供商返回的数据）
}

// SMSStatus 短信状态信息
type SMSStatus struct {
	MessageID     string                 // 短信消息ID
	PhoneNumber   string                 // 手机号
	Status        string                 // 发送状态: delivered, failed, pending, rejected
	StatusCode    int                    // 状态码
	StatusMessage string                 // 状态描述
	SendTime      time.Time              // 发送时间
	DeliverTime   time.Time              // 投递时间
	Extra         map[string]interface{} // 额外信息
}

// SMSStatusResult 短信状态查询结果
type SMSStatusResult struct {
	Status string                 // 查询状态: success, failed
	Data   []*SMSStatus           // 短信状态列表
	Error  error                  // 错误信息
	Extra  map[string]interface{} // 额外信息
}

// SMSStatusQuery 短信状态查询参数
type SMSStatusQuery struct {
	MessageID   string    // 短信消息ID（可选）
	PhoneNumber string    // 手机号（可选）
	FromDate    time.Time // 开始日期
	ToDate      time.Time // 结束日期
	Offset      uint64    // 偏移量
	Limit       uint64    // 拉取条数
}

// Config 基础配置
type Config struct {
	ProviderType ProviderType // 提供商类型
	Region       string       // 区域（云服务商需要）
	AccessKeyID  string       // Access Key ID / Secret ID
	SecretKey    string       // Secret Key
	// 腾讯云专用配置
	TencentAppID string // 腾讯云短信应用ID
	// 阿里云专用配置
	AliyunSignName string // 阿里云默认签名
}

// 短信状态常量
const (
	// StatusSuccess 短信发送成功状态
	StatusSuccess = "success"
	// StatusFailed 短信发送失败状态
	StatusFailed = "failed"
	// StatusPending 短信发送中状态
	StatusPending = "pending"
	// StatusDelivered 短信已送达状态
	StatusDelivered = "delivered"
)

// NewSMSClient 创建短信客户端
func NewSMSClient(cfg *Config) (SMSClient, error) {
	switch cfg.ProviderType {
	case ProviderTypeTencentSMS:
		return newTencentSMSClient(cfg)
	case ProviderTypeAliyunSMS:
		return newAliyunSMSClient(cfg)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", cfg.ProviderType)
	}
}

// ValidatePhoneNumber 验证手机号格式
func ValidatePhoneNumber(ctx context.Context, phoneNumber string) bool {
	// 链路追踪
	tracer := otel.Tracer("gosms")
	spanName := "sms.validate.phone"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "开始验证手机号",
		logger.String("phone", phoneNumber))

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("sms.phone_to_validate", phoneNumber),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	if len(phoneNumber) == 0 {
		span.SetAttributes(
			attribute.Bool("sms.is_valid", false),
		)
		span.SetStatus(codes.Ok, "validation completed")
		return false
	}

	// 简单验证：以+开头，后面跟数字
	if phoneNumber[0] != '+' {
		span.SetAttributes(
			attribute.Bool("sms.is_valid", false),
		)
		span.SetStatus(codes.Ok, "validation completed")
		return false
	}

	// 检查是否全部为数字（除了+号）
	for i := 1; i < len(phoneNumber); i++ {
		if phoneNumber[i] < '0' || phoneNumber[i] > '9' {
			span.SetAttributes(
				attribute.Bool("sms.is_valid", false),
			)
			span.SetStatus(codes.Ok, "validation completed")
			return false
		}
	}

	// 长度验证：最少7位，最多15位（不含+号）
	digitLen := len(phoneNumber) - 1
	if digitLen < 7 || digitLen > 15 {
		span.SetAttributes(
			attribute.Bool("sms.is_valid", false),
		)
		span.SetStatus(codes.Ok, "validation completed")
		return false
	}

	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Bool("sms.is_valid", true),
		attribute.Float64("sms.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "validation completed")

	logger.InfoWithCtx(ctx, "手机号验证通过",
		logger.String("phone", phoneNumber),
		logger.Float64("duration_ms", float64(duration.Milliseconds())))

	return true
}

// FormatPhoneNumber 格式化手机号为标准国际格式
func FormatPhoneNumber(phoneNumber string) string {
	// 如果已经有+号，直接返回
	if len(phoneNumber) > 0 && phoneNumber[0] == '+' {
		return phoneNumber
	}

	// 如果是国内手机号（11位），添加+86
	if len(phoneNumber) == 11 && phoneNumber[0] == '1' {
		return "+86" + phoneNumber
	}

	// 其他情况，假设已经是国际格式但没有+号
	return "+" + phoneNumber
}

// Validate 验证发送请求的合法性
func (r *SendRequest) Validate() error {
	if len(r.PhoneNumbers) == 0 {
		return fmt.Errorf("phone numbers are required")
	}

	if r.TemplateID == "" {
		return fmt.Errorf("template ID is required")
	}

	if r.SignName == "" {
		return fmt.Errorf("sign name is required")
	}

	// 验证手机号格式
	for _, phone := range r.PhoneNumbers {
		if !ValidatePhoneNumber(context.Background(), phone) {
			return fmt.Errorf("invalid phone number format: %s", phone)
		}
	}

	return nil
}

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("sms.request_id", reqID)
		}
	}
	return attribute.String("sms.request_id", "")
}

// getStringValue 安全获取字符串值（内部辅助函数）
func getStringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
