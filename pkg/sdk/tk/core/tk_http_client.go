package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/gohttp"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sdk/tk/tkerrors"
)

var (
	// 全局单例客户端（推荐方式）
	globalClient *gohttp.Client
	httpOnce     sync.Once
)

// TkHTTPClient 途刻HTTP客户端（基于 gohttp 封装）
// 注意：gohttp 已内置 OpenTelemetry 追踪，此处不再重复创建 Span
type TkHTTPClient struct {
	client *gohttp.Client
}

// TkHTTPRequest HTTP请求结构
type TkHTTPRequest struct {
	URL     string
	Params  map[string]string
	Headers map[string]string
	Body    string
}

// TkHTTPResponse HTTP响应结构
type TkHTTPResponse struct {
	Body string
}

// PostWithContext 带上下文发送POST请求（推荐使用）
// 注意：底层 gohttp 已内置完整的 OpenTelemetry 追踪，包括：
// - 自动创建 HTTP Client Span
// - 自动注入 W3C Trace Context
// - 自动记录请求/响应指标
// 因此此处不再重复创建 Span，避免追踪冗余
func (client *TkHTTPClient) PostWithContext(ctx context.Context, httpRequest *TkHTTPRequest) (*TkHTTPResponse, error) {
	// 构建请求
	req := client.client.Request(ctx).
		SetHeader("Content-Type", "application/json")

	// 添加自定义 Headers
	if len(httpRequest.Headers) > 0 {
		req.SetHeaders(httpRequest.Headers)
	}

	// 添加查询参数
	if len(httpRequest.Params) > 0 {
		req.SetQueryParams(httpRequest.Params)
	}

	// 设置请求体
	if httpRequest.Body != "" {
		req.SetBody(bytes.NewBufferString(httpRequest.Body))
	}

	// 发起 POST 请求（gohttp 会自动处理追踪）
	resp, err := req.Post(httpRequest.URL)
	if err != nil {
		// 判断是否为业务错误
		var httpErr *gohttp.ErrorResponse
		if errors.As(err, &httpErr) {
			logger.WarnWithCtx(ctx, "[tk http client] HTTP request failed",
				logger.String("url", httpRequest.URL),
				logger.Int("status_code", httpErr.StatusCode),
				logger.Err(httpErr))
			return nil, tkerrors.NewTkErrorWithMessage(tkerrors.HTTPError,
				fmt.Sprintf("http code = %d, message: %s", httpErr.StatusCode, httpErr.Message))
		}
		logger.ErrorWithCtx(ctx, "[tk http client] HTTP transport error",
			logger.String("url", httpRequest.URL),
			logger.Err(err))
		return nil, tkerrors.NewTkErrorWithMessage(tkerrors.HTTPError, err.Error())
	}

	// 检查响应状态码
	if resp.StatusCode() != http.StatusOK {
		errMsg := fmt.Sprintf("http code = %d", resp.StatusCode())
		logger.WarnWithCtx(ctx, "[tk http client] HTTP request non-200 status",
			logger.String("url", httpRequest.URL),
			logger.Int("status_code", resp.StatusCode()),
			logger.String("body", resp.String()))
		return nil, tkerrors.NewTkErrorWithMessage(tkerrors.HTTPError, errMsg)
	}

	return &TkHTTPResponse{Body: resp.String()}, nil
}

// GetHTTPClient 获取HTTP客户端实例（单例模式，推荐使用）
func GetHTTPClient() *TkHTTPClient {
	httpOnce.Do(func() {
		config := GetTkConfig()

		// 构建 gohttp 客户端配置选项
		opts := []gohttp.Option{
			// 超时配置
			gohttp.WithTimeout(time.Duration(config.HTTPReadTimeout) * time.Millisecond),

			// 重试配置：智能退避重试（仅对网络故障和特定状态码重试）
			gohttp.WithRetry(3, 1*time.Second, 5*time.Second),
		}

		// TLS 配置：根据配置决定是否跳过证书验证
		if config.InsecureSkipVerify {
			opts = append(opts, gohttp.WithInsecureSkipVerify())
			logger.WarnWithCtx(context.Background(), "TkHTTPClient: InsecureSkipVerify is enabled (not recommended for production)")
		}

		// SSRF 防护：防止访问内网地址（生产环境建议启用）
		// opts = append(opts, gohttp.WithSSRFProtection())

		// 使用 gohttp 创建企业级 HTTP 客户端
		globalClient = gohttp.New(opts...)

		logger.InfoWithCtx(context.Background(), "TkHTTPClient initialized with gohttp enterprise client",
			logger.Int64("timeout", config.HTTPReadTimeout),
			logger.Bool("insecure_skip_verify", config.InsecureSkipVerify))
	})

	return &TkHTTPClient{
		client: globalClient,
	}
}

// Close 优雅关闭客户端（在应用退出时调用）
func Close() {
	if globalClient != nil {
		globalClient.Close()
		logger.InfoWithCtx(context.Background(), "TkHTTPClient closed gracefully")
	}
}
