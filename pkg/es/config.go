// config.go
// Package es 提供 Elasticsearch 客户端封装。
package es

import (
	"fmt"
	"time"
)

// Config Elasticsearch配置
type Config struct {
	Addresses []string      `json:"addresses"`
	Username  string        `json:"username"`
	Password  string        `json:"password"`
	APIKey    string        `json:"api_key"`
	Timeout   time.Duration `json:"timeout"`

	// 连接池配置
	MaxIdleConns          int           `json:"max_idle_conns"`
	MaxIdleConnsPerHost   int           `json:"max_idle_conns_per_host"`
	MaxConnsPerHost       int           `json:"max_conns_per_host"`
	IdleConnTimeout       time.Duration `json:"idle_conn_timeout"`
	TLSHandshakeTimeout   time.Duration `json:"tls_handshake_timeout"`
	ExpectContinueTimeout time.Duration `json:"expect_continue_timeout"`
	ResponseHeaderTimeout time.Duration `json:"response_header_timeout"`
	ConnectionTimeout     time.Duration `json:"connection_timeout"`
	ConnectionKeepAlive   time.Duration `json:"connection_keep_alive"`

	// 重试配置
	RetryOnStatus []int         `json:"retry_on_status"`
	MaxRetries    int           `json:"max_retries"`
	RetryBackoff  time.Duration `json:"retry_backoff"` // 基础重试间隔
}

// Validate 验证配置
func (c Config) Validate() error {
	if len(c.Addresses) == 0 {
		return fmt.Errorf("elasticsearch addresses cannot be empty")
	}

	if c.Username != "" && c.Password == "" {
		return fmt.Errorf("password must be provided when username is set")
	}

	return nil
}

// GetDefaultConfig 返回默认配置
func GetDefaultConfig() Config {
	return Config{
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   5,
		MaxConnsPerHost:       20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ConnectionTimeout:     30 * time.Second,
		ConnectionKeepAlive:   30 * time.Second,
		Timeout:               30 * time.Second,
		RetryOnStatus:         []int{502, 503, 504, 429},
		MaxRetries:            3,
		RetryBackoff:          100 * time.Millisecond,
	}
}
