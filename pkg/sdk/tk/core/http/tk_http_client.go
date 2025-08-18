package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/18721889353/sunshine/pkg/sdk/tk/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
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

func GetHttpClient(timeout int64) *TkHttpClient {
	// 使用 LoadOrStore 确保并发安全初始化
	client, loaded := clientMap.LoadOrStore(timeout, nil)
	if !loaded || client == nil {
		newClient := &TkHttpClient{
			httpClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: false, // 启用证书验证（生产环境建议开启）
						MinVersion:         tls.VersionTLS12,
						// 增加以下配置
						RootCAs:    nil, // 指定CA证书池
						ClientAuth: tls.NoClientCert,
					},
					DisableKeepAlives:     false,
					MaxIdleConns:          1000,             // 合理限制空闲连接数
					MaxIdleConnsPerHost:   1000,             // 每个 Host 的最大空闲连接数
					IdleConnTimeout:       30 * time.Second, // 缩短空闲连接超时时间
					ResponseHeaderTimeout: 5 * time.Second,  // 添加响应头超时限制
					DialContext: (&net.Dialer{
						Timeout:   10 * time.Second, // 缩短连接超时时间
						KeepAlive: 30 * time.Second,
					}).DialContext,
				},
				Timeout: time.Duration(timeout) * time.Millisecond,
			},
		}
		clientMap.Store(timeout, newClient)
		return newClient
	}
	return client.(*TkHttpClient)
}
