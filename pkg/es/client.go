package es

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v7"
)

// Client ES客户端封装
type Client struct {
	*elasticsearch.Client
	// 添加缓冲池以提高并发性能
	bufferPool sync.Pool
}

// ESOptions ES客户端选项配置
type ESOptions func(*esOptions)

// esOptions ES客户端配置选项
type esOptions struct {
	config Config
}

// defaultESOptions 默认ES客户端选项
func defaultESOptions() *esOptions {
	return &esOptions{
		config: GetDefaultConfig(),
	}
}

// WithConfig 设置ES配置
func WithConfig(config Config) ESOptions {
	return func(o *esOptions) {
		o.config = config
	}
}

// apply 应用选项
func (o *esOptions) apply(opts ...ESOptions) {
	for _, opt := range opts {
		opt(o)
	}
}

// NewClient 创建ES客户端，启用连接池
func NewClient(opts ...ESOptions) (*Client, error) {
	o := defaultESOptions()
	o.apply(opts...)

	config := o.config

	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid elasticsearch config: %w", err)
	}

	// 创建自定义的Transport以配置连接池
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		// 连接池配置 - 从config中读取
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.MaxConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ExpectContinueTimeout: config.ExpectContinueTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		DialContext: (&net.Dialer{
			Timeout:   config.ConnectionTimeout,
			KeepAlive: config.ConnectionKeepAlive,
		}).DialContext,
	}

	cfg := elasticsearch.Config{
		Addresses: config.Addresses,
		Username:  config.Username,
		Password:  config.Password,
		APIKey:    config.APIKey,
		Transport: transport,
		// 重试配置 - 从config中读取
		RetryOnStatus: config.RetryOnStatus,
		RetryBackoff: func(i int) time.Duration {
			// 指数退避策略，基于配置的基础间隔
			return config.RetryBackoff + time.Duration(i)*config.RetryBackoff
		},
		MaxRetries: config.MaxRetries,
	}

	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	esClient := &Client{client, sync.Pool{}}
	// 初始化缓冲池
	esClient.bufferPool.New = func() interface{} {
		return new(bytes.Buffer)
	}

	return esClient, nil
}

// Search 返回搜索操作实例
func (c *Client) Search() SearchService {
	return c.NewSearch()
}

// Ping 检查ES服务状态
func (c *Client) Ping(ctx context.Context) error {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "ping")
	defer endSpan(nil)

	res, err := c.Client.Ping(
		c.Client.Ping.WithContext(ctx),
	)
	if err != nil {
		endSpan(err)
		return fmt.Errorf("failed to ping elasticsearch: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("elasticsearch ping returned error status: %s", res.String())
		endSpan(err)
		return err
	}

	return nil
}

// Info 获取ES集群信息
func (c *Client) Info(ctx context.Context) (map[string]interface{}, error) {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "info")
	defer endSpan(nil)

	res, err := c.Client.Info(
		c.Client.Info.WithContext(ctx),
	)
	if err != nil {
		endSpan(err)
		return nil, fmt.Errorf("failed to get elasticsearch info: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("elasticsearch info request returned error: %s", res.String())
		endSpan(err)
		return nil, err
	}

	var info map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		endSpan(err)
		return nil, fmt.Errorf("failed to decode elasticsearch info: %w", err)
	}

	return info, nil
}

// HealthCheck 检查集群健康状态
func (c *Client) HealthCheck(ctx context.Context) (string, error) {
	// 添加追踪支持
	ctx, endSpan := c.withSpan(ctx, "health_check")
	defer endSpan(nil)

	res, err := c.Client.Cluster.Health(
		c.Client.Cluster.Health.WithContext(ctx),
	)
	if err != nil {
		endSpan(err)
		return "", fmt.Errorf("failed to check cluster health: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.IsError() {
		err = fmt.Errorf("cluster health check returned error: %s", res.String())
		endSpan(err)
		return "", err
	}

	var result map[string]interface{}
	if clientErr := json.NewDecoder(res.Body).Decode(&result); clientErr != nil {
		endSpan(clientErr)
		return "", fmt.Errorf("failed to decode health check result: %w", clientErr)
	}

	status, ok := result["status"].(string)
	if !ok {
		err = fmt.Errorf("unexpected health check response format")
		endSpan(err)
		return "", err
	}

	return status, nil
}

// getBuffer 从池中获取缓冲区
func (c *Client) getBuffer() *bytes.Buffer {
	buf := c.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// putBuffer 将缓冲区放回池中
func (c *Client) putBuffer(buf *bytes.Buffer) {
	c.bufferPool.Put(buf)
}
