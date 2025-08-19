package es

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/elastic/go-elasticsearch/v7"
)

// Client ES客户端封装
type Client struct {
	*elasticsearch.Client
}

// NewClient 创建ES客户端，启用连接池
func NewClient(config Config) (*Client, error) {
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

	return &Client{client}, nil
}

// Ping 检查ES服务状态
func (c *Client) Ping() error {
	res, err := c.Client.Ping()
	if err != nil {
		return fmt.Errorf("failed to ping elasticsearch: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch ping returned error status: %s", res.String())
	}

	return nil
}

// Info 获取ES集群信息
func (c *Client) Info() (map[string]interface{}, error) {
	res, err := c.Client.Info()
	if err != nil {
		return nil, fmt.Errorf("failed to get elasticsearch info: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch info request returned error: %s", res.String())
	}

	var info map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode elasticsearch info: %w", err)
	}

	return info, nil
}

// HealthCheck 检查集群健康状态
func (c *Client) HealthCheck(ctx context.Context) (string, error) {
	res, err := c.Client.Cluster.Health(
		c.Client.Cluster.Health.WithContext(ctx),
	)
	if err != nil {
		return "", fmt.Errorf("failed to check cluster health: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return "", fmt.Errorf("cluster health check returned error: %s", res.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode health check result: %w", err)
	}

	status, ok := result["status"].(string)
	if !ok {
		return "", fmt.Errorf("unexpected health check response format")
	}

	return status, nil
}
