package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/sdk/tk/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var clientMap sync.Map

type TkHttpClient struct {
	httpClient *http.Client
}
type TkHttpRequest struct {
	Url     string
	Params  map[string]string
	Headers map[string]string
	Body    string
}

type TkHttpResponse struct {
	Body string
}

func (client *TkHttpClient) Post(httpRequest *TkHttpRequest) (*TkHttpResponse, error) {
	u, err := url.Parse(httpRequest.Url)
	if err != nil {
		return nil, errors.NewTkErrorWithMessage(errors.HttpError, err.Error())
	}
	if len(httpRequest.Params) > 0 {
		query := u.Query()
		for k, v := range httpRequest.Params {
			query.Add(k, v)
		}
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequest("POST", u.String(), bytes.NewBufferString(httpRequest.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "Content-Type: application/json")
	if len(httpRequest.Headers) > 0 {
		for k, v := range httpRequest.Headers {
			req.Header.Set(k, v)
		}
	}
	httpResp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, errors.NewTkErrorWithMessage(errors.HttpError, err.Error())
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, errors.NewTkErrorWithMessage(errors.HttpError, fmt.Sprintf("http code = %d", httpResp.StatusCode))
	}
	bs, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, errors.NewTkErrorWithMessage(errors.HttpError, err.Error())
	}

	return &TkHttpResponse{Body: string(bs)}, nil
}

func (client *TkHttpClient) PostWithContext(ctx context.Context, httpRequest *TkHttpRequest) (*TkHttpResponse, error) {
	// 创建链路追踪 span
	spanName := fmt.Sprintf("HttpPost:%s", httpRequest.Url)
	ctx, span := otel.Tracer("tk-http-client").Start(ctx, spanName)
	defer span.End()

	// 添加请求信息到 span
	span.SetAttributes(
		attribute.String("http.url", httpRequest.Url),
		attribute.String("http.method", "POST"),
		attribute.String("http.request.body", httpRequest.Body),
	)

	u, err := url.Parse(httpRequest.Url)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, errors.NewTkErrorWithMessage(errors.HttpError, err.Error())
	}
	if len(httpRequest.Params) > 0 {
		query := u.Query()
		for k, v := range httpRequest.Params {
			query.Add(k, v)
		}
		u.RawQuery = query.Encode()
	}

	// 更新 span 中的 URL 信息
	span.SetAttributes(attribute.String("http.url", u.String()))

	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewBufferString(httpRequest.Body))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if len(httpRequest.Headers) > 0 {
		for k, v := range httpRequest.Headers {
			req.Header.Set(k, v)
		}
	}

	httpResp, err := client.httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, errors.NewTkErrorWithMessage(errors.HttpError, err.Error())
	}
	defer func() {
		if httpResp != nil && httpResp.Body != nil {
			httpResp.Body.Close()
		}
	}()

	// 记录响应状态码
	span.SetAttributes(attribute.Int("http.status_code", httpResp.StatusCode))

	if httpResp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("http code = %d", httpResp.StatusCode)
		span.SetStatus(codes.Error, errMsg)
		return nil, errors.NewTkErrorWithMessage(errors.HttpError, errMsg)
	}

	bs, err := io.ReadAll(httpResp.Body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, errors.NewTkErrorWithMessage(errors.HttpError, err.Error())
	}

	span.SetAttributes(attribute.Int("http.response.size", len(bs)))

	return &TkHttpResponse{Body: string(bs)}, nil
}

func GetHttpClient() *TkHttpClient {
	// 使用 LoadOrStore 确保并发安全初始化
	client, loaded := clientMap.LoadOrStore(GetTkConfig().HttpReadTimeout, nil)
	if !loaded || client == nil {
		// 获取配置
		config := GetTkConfig()
		newClient := &TkHttpClient{
			httpClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: config.InsecureSkipVerify,
						MinVersion:         tls.VersionTLS12,
						RootCAs:            nil,
						ClientAuth:         tls.NoClientCert,
					},
					DisableKeepAlives:     config.DisableKeepAlives,
					MaxIdleConns:          config.MaxIdleCons,
					MaxIdleConnsPerHost:   config.MaxIdleConsPerHost,
					IdleConnTimeout:       config.IdleConnTimeout,
					ResponseHeaderTimeout: config.ResponseHeaderTimeout,
					DialContext: (&net.Dialer{
						Timeout:   config.DialTimeout,
						KeepAlive: config.DialKeepAlive,
					}).DialContext,
				},
				Timeout: time.Duration(config.HttpReadTimeout) * time.Millisecond,
			},
		}
		clientMap.Store(GetTkConfig().HttpReadTimeout, newClient)
		return newClient
	}
	return client.(*TkHttpClient)
}
