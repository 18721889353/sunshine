package database

import (
	"context"
	"sync"
	"time"

	"github.com/18721889353/sunshine/internal/config"

	"github.com/18721889353/sunshine/pkg/es"
	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	// esClient ES客户端实例
	esClient *es.Client
	// esOnce 确保ES客户端只初始化一次
	esOnce sync.Once
	// initCtx 初始化阶段使用的 context
	initCtx = context.Background()
)

// InitElasticsearch 初始化Elasticsearch客户端
func InitElasticsearch() *es.Client {
	// 安全地获取配置
	cfg := config.Get()
	if cfg == nil {
		panic("config is nil, please initialize config first")
	}
	esCfg := cfg.Elasticsearch

	// 创建ES配置，使用 esConfig 而不是 config 避免命名冲突
	esConfig := es.GetDefaultConfig()
	esConfig.Addresses = esCfg.Addresses
	esConfig.Username = esCfg.Username
	esConfig.Password = esCfg.Password
	esConfig.APIKey = esCfg.APIKey
	esConfig.Timeout = time.Duration(esCfg.Timeout) * time.Second

	// 连接池配置
	esConfig.MaxIdleConns = esCfg.MaxIdleConns
	esConfig.MaxIdleConnsPerHost = esCfg.MaxIdleConnsPerHost
	esConfig.MaxConnsPerHost = esCfg.MaxConnsPerHost
	esConfig.IdleConnTimeout = time.Duration(esCfg.IdleConnTimeout) * time.Second
	esConfig.TLSHandshakeTimeout = time.Duration(esCfg.TLSHandshakeTimeout) * time.Second
	esConfig.ExpectContinueTimeout = time.Duration(esCfg.ExpectContinueTimeout) * time.Second
	esConfig.ResponseHeaderTimeout = time.Duration(esCfg.ResponseHeaderTimeout) * time.Second
	esConfig.ConnectionTimeout = time.Duration(esCfg.ConnectionTimeout) * time.Second
	esConfig.ConnectionKeepAlive = time.Duration(esCfg.ConnectionKeepAlive) * time.Second

	// 重试配置
	esConfig.RetryOnStatus = esCfg.RetryOnStatus
	esConfig.MaxRetries = esCfg.MaxRetries
	esConfig.RetryBackoff = time.Duration(esCfg.RetryBackoff) * time.Millisecond

	// 验证配置
	if err := esConfig.Validate(); err != nil {
		panic("elasticsearch config validation failed: " + err.Error())
	}

	// 使用选项模式创建ES客户端
	client, err := es.NewClient(
		es.WithConfig(esConfig),
	)
	if err != nil {
		panic("failed to create elasticsearch client: " + err.Error())
	}

	// 检查连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		logger.WarnWithCtx(initCtx, "elasticsearch ping failed", logger.Err(err))
		// 根据配置决定是否panic
		if esCfg.EnablePingCheck {
			panic("elasticsearch connection failed: " + err.Error())
		}
	}

	// 检查集群健康状态
	if esCfg.EnableHealthCheck {
		health, err := client.HealthCheck(ctx)
		if err != nil {
			logger.WarnWithCtx(initCtx, "elasticsearch health check failed", logger.Err(err))
			if esCfg.EnablePingCheck {
				panic("elasticsearch health check failed: " + err.Error())
			}
		} else {
			logger.InfoWithCtx(initCtx, "elasticsearch cluster health status", logger.String("status", health))
		}
	}

	logger.InfoWithCtx(initCtx, "elasticsearch client initialized successfully")
	return client
}

// GetElasticsearch 获取ES客户端实例
func GetElasticsearch() *es.Client {
	if esClient == nil {
		esOnce.Do(func() {
			esClient = InitElasticsearch()
		})
	}
	return esClient
}

// CloseElasticsearch 关闭ES客户端（空实现，仅为了保持接口一致性）
func CloseElasticsearch() error {
	logger.InfoWithCtx(initCtx, "elasticsearch client closed")
	return nil
}
