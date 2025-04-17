package httpcli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-resty/resty/v2"
)

// Client 封装了resty客户端
type Client struct {
	cli *resty.Client
}

// Response 封装响应
type Response struct {
	resp *resty.Response
}

// Request 请求构建器
type Request struct {
	req *resty.Request
}

// ErrorResponse 错误响应结构
type ErrorResponse struct {
	StatusCode int
	Message    string
	Body       []byte
}

// Option 客户端配置选项
type Option func(*Client)

// New 创建新的HTTP客户端
func New(opts ...Option) *Client {
	client := resty.New()
	c := &Client{cli: client}

	// 默认配置
	c.cli.SetTimeout(30 * time.Second)
	c.cli.SetRetryCount(2)
	c.cli.SetRetryWaitTime(1 * time.Second)
	c.cli.SetRetryMaxWaitTime(3 * time.Second)
	c.cli.SetRedirectPolicy(resty.NoRedirectPolicy())
	c.cli.SetContentLength(true)

	// 应用自定义配置
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// ========== 客户端配置选项 ==========

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.cli.SetTimeout(timeout)
	}
}

// WithTransport 设置自定义 Transport
func WithTransport(transport *http.Transport) Option {
	return func(c *Client) {
		c.cli.SetTransport(transport)
	}
}

// WithRetry 设置重试策略
func WithRetry(count int, waitTime, maxWaitTime time.Duration) Option {
	return func(c *Client) {
		c.cli.SetRetryCount(count)
		c.cli.SetRetryWaitTime(waitTime)
		c.cli.SetRetryMaxWaitTime(maxWaitTime)
	}
}

// WithBaseURL 设置基础URL
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.cli.SetBaseURL(baseURL)
	}
}

// WithHeaders 设置公共请求头
func WithHeaders(headers map[string]string) Option {
	return func(c *Client) {
		c.cli.SetHeaders(headers)
	}
}

// WithDebug 启用调试模式
func WithDebug(enable bool) Option {
	return func(c *Client) {
		c.cli.SetDebug(enable)
	}
}

// WithTLSConfig 设置TLS配置
func WithTLSConfig(tlsConfig *tls.Config) Option {
	return func(c *Client) {
		c.cli.SetTLSClientConfig(tlsConfig)
	}
}

// WithRootCA 从文件加载根证书
func WithRootCA(caCertPath string) Option {
	return func(c *Client) {
		caCert, err := os.ReadFile(caCertPath)
		if err != nil {
			panic(fmt.Sprintf("failed to read CA cert: %v", err))
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			panic("failed to parse CA certificate")
		}

		tlsConfig := &tls.Config{
			RootCAs: caCertPool,
		}

		c.cli.SetTLSClientConfig(tlsConfig)
	}
}

// WithClientCert 加载客户端证书和私钥
func WithClientCert(certPath, keyPath string) Option {
	return func(c *Client) {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			panic(fmt.Sprintf("failed to load client cert: %v", err))
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}

		c.cli.SetTLSClientConfig(tlsConfig)
	}
}

// WithInsecureSkipVerify 跳过证书验证（仅用于测试环境）
func WithInsecureSkipVerify() Option {
	return func(c *Client) {
		c.cli.SetTLSClientConfig(&tls.Config{
			InsecureSkipVerify: true,
		})
	}
}

// WithProxy 设置代理
func WithProxy(proxyURL string) Option {
	return func(c *Client) {
		c.cli.SetProxy(proxyURL)
	}
}

// WithCookieJar 启用Cookie管理
func WithCookieJar() Option {
	return func(c *Client) {
		c.cli.SetCookieJar(http.DefaultClient.Jar)
	}
}

// ========== 请求构建方法 ==========

// Request 创建新请求
func (c *Client) Request(ctx context.Context) *Request {
	return &Request{
		req: c.cli.R().SetContext(ctx),
	}
}

// SetHeader 设置请求头
func (r *Request) SetHeader(key, value string) *Request {
	r.req.SetHeader(key, value)
	return r
}

// SetHeaders 批量设置请求头
func (r *Request) SetHeaders(headers map[string]string) *Request {
	r.req.SetHeaders(headers)
	return r
}

// SetQueryParam 设置查询参数
func (r *Request) SetQueryParam(key, value string) *Request {
	r.req.SetQueryParam(key, value)
	return r
}

// SetQueryParams 批量设置查询参数
func (r *Request) SetQueryParams(params map[string]string) *Request {
	r.req.SetQueryParams(params)
	return r
}

// SetBody 设置请求体
func (r *Request) SetBody(body interface{}) *Request {
	r.req.SetBody(body)
	return r
}

// SetResult 设置响应结果解析目标
func (r *Request) SetResult(result interface{}) *Request {
	r.req.SetResult(result)
	return r
}

// SetError 设置错误解析目标
func (r *Request) SetError(err interface{}) *Request {
	r.req.SetError(err)
	return r
}

// SetAuthToken 设置Bearer Token
func (r *Request) SetAuthToken(token string) *Request {
	r.req.SetAuthToken(token)
	return r
}

// ========== HTTP方法 ==========

// Get 发送GET请求
func (r *Request) Get(url string) (*Response, error) {
	return r.do("GET", url)
}

// Post 发送POST请求
func (r *Request) Post(url string) (*Response, error) {
	return r.do("POST", url)
}

// Put 发送PUT请求
func (r *Request) Put(url string) (*Response, error) {
	return r.do("PUT", url)
}

// Delete 发送DELETE请求
func (r *Request) Delete(url string) (*Response, error) {
	return r.do("DELETE", url)
}

// Patch 发送PATCH请求
func (r *Request) Patch(url string) (*Response, error) {
	return r.do("PATCH", url)
}

// do 执行请求
func (r *Request) do(method, url string) (*Response, error) {
	resp, err := r.req.Execute(method, url)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}

	if resp.IsError() {
		return &Response{resp: resp}, &ErrorResponse{
			StatusCode: resp.StatusCode(),
			Message:    resp.Status(),
			Body:       resp.Body(),
		}
	}

	return &Response{resp: resp}, nil
}

// ========== 响应处理方法 ==========

// StatusCode 获取状态码
func (r *Response) StatusCode() int {
	return r.resp.StatusCode()
}

// Body 获取原始响应体
func (r *Response) Body() []byte {
	return r.resp.Body()
}

// String 获取字符串格式响应体
func (r *Response) String() string {
	return r.resp.String()
}

// UnmarshalJSON 解析JSON响应体
func (r *Response) UnmarshalJSON(v interface{}) error {
	return json.Unmarshal(r.resp.Body(), v)
}

// IsSuccess 判断请求是否成功
func (r *Response) IsSuccess() bool {
	return r.resp.IsSuccess()
}

// IsError 判断请求是否失败
func (r *Response) IsError() bool {
	return r.resp.IsError()
}

// Headers 获取响应头
func (r *Response) Headers() http.Header {
	return r.resp.Header()
}

// Error 实现error接口
func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("http error: %d - %s", e.StatusCode, e.Message)
}

// Is 错误比较
func (e *ErrorResponse) Is(target error) bool {
	var err *ErrorResponse
	if errors.As(target, &err) {
		return e.StatusCode == err.StatusCode
	}
	return false
}
