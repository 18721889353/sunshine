package es

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

// Client ES客户端封装
type Client struct {
	*elasticsearch.Client
}

// NewClient 创建ES客户端
func NewClient(config Config) (*Client, error) {
	cfg := elasticsearch.Config{
		Addresses: config.Addresses,
		Username:  config.Username,
		Password:  config.Password,
		APIKey:    config.APIKey,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			MaxIdleConnsPerHost:   10,
			ResponseHeaderTimeout: time.Second * 10,
		},
	}

	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{client}, nil
}

// Ping 检查ES服务状态
func (c *Client) Ping() error {
	_, err := c.Client.Ping()
	return err
}
