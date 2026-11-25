package metrics_test

import (
	"testing"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"

	// import 所有组件包，触发其 var 初始化（定义即注册）。
	// 新增组件包接入指标后必须在此补 import + 测试条目，否则漏注册无人报警。
	_ "github.com/18721889353/sunshine/pkg/cache"
	_ "github.com/18721889353/sunshine/pkg/dlock"
	_ "github.com/18721889353/sunshine/pkg/es"
	_ "github.com/18721889353/sunshine/pkg/etcdcli"
	ginmetrics "github.com/18721889353/sunshine/pkg/gin/middleware/metrics"
	_ "github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	_ "github.com/18721889353/sunshine/pkg/gocron"
	_ "github.com/18721889353/sunshine/pkg/gohttp"
	_ "github.com/18721889353/sunshine/pkg/goredis"
	_ "github.com/18721889353/sunshine/pkg/logger"
	_ "github.com/18721889353/sunshine/pkg/nacoscli"
	_ "github.com/18721889353/sunshine/pkg/sgorm/glog"
	_ "github.com/18721889353/sunshine/pkg/sgorm/query"
)

// TestAllComponentsRegisteredInFast 验证各组件指标经"定义即注册"后
// 确实进了注册中心。这是重构正确性的端到端守护：漏注册 = 指标静默缺失。
//
// 用 HasMetric 精确查询（无副作用）；不用 Gather（Vec 无样本不 emit），
// 也不用"同名冲突 panic"反证（有副作用且脆弱）。
func TestAllComponentsRegisteredInFast(t *testing.T) {
	for _, tc := range []struct{ pkg, name string }{
		{"cache", "cache_hit_total"},
		{"cache", "cache_operation_duration_seconds"},
		{"dlock", "lock_acquire_duration_seconds"},
		{"es", "es_operation_duration_seconds"},
		{"etcdcli", "etcd_token_refresh_total"},
		{"gocron", "cron_task_duration_seconds"},
		{"gohttp", "http_client_duration_seconds"},
		{"gorabbitmq", "rabbitmq_publish_duration_seconds"},
		{"gorabbitmq", "rabbitmq_consume_duration_seconds"},
		{"goredis", "redis_operation_duration_seconds"},
		{"nacoscli", "nacos_config_callback_total"},
		{"nacoscli", "nacos_config_fetch_duration_seconds"},
		{"sgorm/glog", "db_query_duration_seconds"},
		{"sgorm/query", "query_build_duration_seconds"},
		{"logger", "logger_dropped_entries_total"},
		{"logger", "logger_router_active_loggers"},
		// gin HTTP 中间件（触发其 var 初始化）
		{"gin/middleware", ginmetrics.MetricNameUptime},
		{"gin/middleware", ginmetrics.MetricNameRequestCount},
		{"gin/middleware", ginmetrics.MetricNameRequestDuration},
		{"gin/middleware", ginmetrics.MetricNameRequestSize},
		{"gin/middleware", ginmetrics.MetricNameResponseSize},
		// 报告器自监控（监控的监控，漏注册 = 报告器故障无人发现）
		{"metrics reporter", "metrics_reporter_push_total"},
		{"metrics reporter", "metrics_reporter_last_push_timestamp_seconds"},
	} {
		if !pkgmetrics.Default().HasMetric(tc.name) {
			t.Errorf("[%s] 指标 %s 未注册（定义即注册漏了？）", tc.pkg, tc.name)
		}
	}
}

// TestCollectorCount_Dedup 幂等重复注册不重复计入 CollectorCount
func TestCollectorCount_Dedup(t *testing.T) {
	before := pkgmetrics.Default().CollectorCount()
	// 重复注册一个已存在的 collector（幂等）
	pkgmetrics.Counter("cache_hit_total", "Cache read hits by operation.", []string{"op"}...)
	after := pkgmetrics.Default().CollectorCount()
	if after != before {
		t.Errorf("幂等重复注册不应增加计数: before=%d after=%d", before, after)
	}
}
