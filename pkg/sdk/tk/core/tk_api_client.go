package core

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sdk/tk/tkerrors"
	"github.com/18721889353/sunshine/pkg/sdk/tk/utils"
)

// TkAPIClient 途刻API客户端
type TkAPIClient struct {
	tracer trace.Tracer // OpenTelemetry tracer for reuse
}

// NewTkAPIClient 创建新的API客户端实例
func NewTkAPIClient() *TkAPIClient {
	return &TkAPIClient{
		tracer: otel.Tracer("tk-api-client"), // 初始化 tracer
	}
}

// DefaultTkAPIClient 默认API客户端实例
var DefaultTkAPIClient = NewTkAPIClient()

// RequestWithContext 带上下文发送API请求（推荐使用）
func (client *TkAPIClient) RequestWithContext(ctx context.Context, request TkAPIRequest, accessToken string) (string, error) {
	urlPath := request.GetURLPath()
	spanName := fmt.Sprintf("TK.API.%s", urlPath)
	ctx, span := client.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	startTime := time.Now()

	// 提取 Context 中的 RequestID（大厂标准：关联业务日志和 Trace）
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			span.SetAttributes(attribute.String(string(logger.ContextKeyForRequestID()), reqIDStr))
		}
	}

	// 验证配置
	if request.GetConfig() == nil {
		err := tkerrors.NewTkError(tkerrors.ConfigIsNull)
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", "configuration-error"),
				attribute.String("error.context", "config-validation"),
			),
		)
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "[tk api client] config is null",
			logger.String("url_path", urlPath))
		return "", err
	}

	appSecret := request.GetConfig().AppSecret
	if len(appSecret) == 0 {
		err := tkerrors.NewTkErrorWithMessage(tkerrors.ParamError, "appSecret为空")
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", "parameter-error"),
				attribute.String("error.context", "appsecret-validation"),
			),
		)
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "[tk api client] appSecret is empty",
			logger.String("url_path", urlPath))
		return "", err
	}

	// 设置 OpenTelemetry 标准属性
	span.SetAttributes(
		attribute.String("http.url_path", urlPath),
		attribute.String("http.method", "POST"),
		attribute.String("messaging.system", "tk-api"),
		attribute.String("messaging.operation", "request"),
	)

	// 准备请求参数
	paramJSON := request.GetParamObject()
	if GetTkConfig().SignFunc == nil {
		GetTkConfig().SignFunc = utils.Sign
	}
	paramJSONString := utils.Marshal(paramJSON, appSecret, GetTkConfig().SignFunc)

	// 构建 HTTP Headers
	httpHeaderMap := map[string]string{
		"from":     "sdk",
		"sdk-type": "golang",
	}
	if accessToken != "" {
		httpHeaderMap["Authorization"] = fmt.Sprintf("Bearer %s", accessToken)
	}
	if request.GetConfig() != nil {
		for k, v := range request.GetConfig().Headers {
			httpHeaderMap[k] = v
		}
	}

	// 注入 Trace Context 到 HTTP Headers（大厂标准做法）
	// 使用 OpenTelemetry Propagator 自动注入标准 W3C Trace Context
	headersMap := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headersMap))

	// 将自定义 headers 合并到 Trace headers
	for k, v := range httpHeaderMap {
		headersMap[k] = v
	}

	fullURL := fmt.Sprintf("%s%s", request.GetConfig().OpenRequestURL, urlPath)

	// 记录请求信息到 span
	span.SetAttributes(
		attribute.String("http.url", fullURL),
		attribute.String("http.request.body", paramJSONString),
		attribute.Int("http.request.headers_count", len(headersMap)),
	)

	span.AddEvent("preparing API request",
		trace.WithAttributes(
			attribute.String("url", fullURL),
			attribute.String("url_path", urlPath),
			attribute.Int("body_size", len(paramJSONString)),
			attribute.Int("headers_count", len(headersMap)),
		))

	httpRequest := &TkHTTPRequest{
		URL:     fullURL,
		Headers: headersMap, // 携带 Trace 信息
		Body:    paramJSONString,
	}

	// 发起 HTTP 请求
	httpResponse, err := GetHTTPClient().PostWithContext(ctx, httpRequest)

	duration := time.Since(startTime)
	span.SetAttributes(attribute.Float64("http.request.duration_ms", float64(duration.Milliseconds())))

	if err != nil {
		errorMsg := fmt.Sprintf("API request failed: %v | url=%s | url_path=%s | duration_ms=%.2f",
			err, fullURL, urlPath, float64(duration.Milliseconds()))
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error.type", fmt.Sprintf("%T", err)),
				attribute.String("error.context", "http-request-failed"),
				attribute.String("error.url", fullURL),
				attribute.String("error.url_path", urlPath),
			),
		)
		span.SetStatus(codes.Error, errorMsg)
		span.AddEvent("API request failed",
			trace.WithAttributes(
				attribute.String("error.message", err.Error()),
				attribute.String("url", fullURL),
				attribute.String("url_path", urlPath),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			))
		logger.WarnWithCtx(ctx, "[tk api client] API request failed",
			logger.Err(err),
			logger.String("url", fullURL),
			logger.String("url_path", urlPath),
			logger.Float64("duration_ms", float64(duration.Milliseconds())))
		return "", err
	}

	// 记录响应信息
	span.SetAttributes(
		attribute.String("http.response.body", httpResponse.Body),
		attribute.Int("http.response.body.size", len(httpResponse.Body)),
	)

	span.AddEvent("API request completed successfully",
		trace.WithAttributes(
			attribute.String("url", fullURL),
			attribute.String("url_path", urlPath),
			attribute.Int("response_size", len(httpResponse.Body)),
			attribute.Float64("duration_ms", float64(duration.Milliseconds())),
		))

	logger.DebugWithCtx(ctx, "[tk api client] API request success",
		logger.String("url", fullURL),
		logger.String("url_path", urlPath),
		logger.Int("response_size", len(httpResponse.Body)),
		logger.Float64("duration_ms", float64(duration.Milliseconds())))

	return httpResponse.Body, nil
}
