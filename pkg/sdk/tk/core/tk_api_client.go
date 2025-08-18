package core

import (
	"context"
	"fmt"

	"github.com/18721889353/sunshine/pkg/sdk/tk/errors"
	"github.com/18721889353/sunshine/pkg/sdk/tk/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type TkApiClient struct{}

func NewTkApiClient() *TkApiClient {
	return &TkApiClient{}
}

var DefaultTkApiClient *TkApiClient = NewTkApiClient()

func (client *TkApiClient) Request(request TkApiRequest, accessToken string) (string, error) {
	if request.GetConfig() == nil {
		return "", errors.NewTkError(errors.ConfigIsNull)
	}

	appSecret := request.GetConfig().AppSecret
	if len(appSecret) == 0 {
		return "", errors.NewTkErrorWithMessage(errors.ParamError, "appSecret为空")
	}
	paramJson := request.GetParamObject()
	urlPath := request.GetUrlPath()
	paramJsonString := utils.Marshal(paramJson, appSecret)
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

	httpRequest := &TkHttpRequest{
		Url:     fmt.Sprintf("%s%s", request.GetConfig().OpenRequestUrl, urlPath),
		Headers: httpHeaderMap,
		Body:    paramJsonString,
	}

	httpResponse, err := GetHttpClient().Post(httpRequest)

	if err != nil {
		return "", err
	}
	return httpResponse.Body, nil
}

func (client *TkApiClient) RequestWithContext(ctx context.Context, request TkApiRequest, accessToken string) (string, error) {
	// 创建链路追踪 span
	// 使用请求的 URL 路径作为 span 名称，便于区分不同接口
	spanName := fmt.Sprintf("APIRequest:%s", request.GetUrlPath())
	ctx, span := otel.Tracer("tk-api-client").Start(ctx, spanName)
	defer span.End()

	if request.GetConfig() == nil {
		err := errors.NewTkError(errors.ConfigIsNull)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	appSecret := request.GetConfig().AppSecret
	if len(appSecret) == 0 {
		err := errors.NewTkErrorWithMessage(errors.ParamError, "appSecret为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	// 添加请求信息到 span
	span.SetAttributes(
		attribute.String("api.url_path", request.GetUrlPath()),
	)

	paramJson := request.GetParamObject()
	urlPath := request.GetUrlPath()
	paramJsonString := utils.Marshal(paramJson, appSecret)
	// 记录请求参数到 span 中
	span.SetAttributes(
		attribute.String("request.body", paramJsonString),
	)

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

	httpRequest := &TkHttpRequest{
		Url:     fmt.Sprintf("%s%s", request.GetConfig().OpenRequestUrl, urlPath),
		Headers: httpHeaderMap,
		Body:    paramJsonString,
	}

	// 更新 span 中的 URL 信息
	span.SetAttributes(
		attribute.String("http.url", httpRequest.Url),
		attribute.String("http.method", "POST"),
	)

	httpResponse, err := GetHttpClient().PostWithContext(ctx, httpRequest)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	span.SetAttributes(attribute.String("response.body", httpResponse.Body))

	return httpResponse.Body, nil
}
