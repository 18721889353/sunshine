package database

import (
	"context"
	"sync"
	"sync/atomic"
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
	// initCtx 初始化阶段使用的 context（仅用于日志，不可取消）
	initCtx = context.Background()
	// esMonitorCancel 停止 ES 健康监控 goroutine（优雅停机用）。
	// 用 atomic.Pointer 避免 Init/Close 并发读写 data race。
	esMonitorCancel atomic.Pointer[context.CancelFunc]
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

	// 连接健康监控：定期 Ping 记录连通性（30s 一次）。
	// 用独立可取消 ctx（不能用 initCtx=Background，否则优雅停机时 monitor 无法停止）。
	monitorCtx, cancel := context.WithCancel(context.Background())
	esMonitorCancel.Store(&cancel)
	es.StartHealthMonitor(monitorCtx, client, 30*time.Second)

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

// CloseElasticsearch 停止 ES 健康监控。
// ES 客户端本身是轻量封装（无独立连接资源需释放），但监控 goroutine
// 必须在优雅停机时停止——否则进程退出前仍会 Ping 并写日志。
func CloseElasticsearch() error {
	if p := esMonitorCancel.Load(); p != nil {
		(*p)()
		esMonitorCancel.Store(nil)
	}
	logger.InfoWithCtx(initCtx, "elasticsearch client closed")
	return nil
}
