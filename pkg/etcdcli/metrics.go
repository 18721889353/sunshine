package etcdcli

import (
	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// etcdTokenRefresh 记录 etcd token 刷新结果（counter，周期性操作）。
//
// 为什么必须有：token 刷新失败只有 Warn 日志，且失败后 SDK仍用旧 token——
// 旧 token 过期后所有注册/发现请求会静默失败，无指标发现不了。
var etcdTokenRefresh = pkgmetrics.Counter("etcd_token_refresh_total", "etcd auth token refresh attempts by result.", []string{"result"}...)

// observeEtcdTokenRefreshResult 记录一次 token 刷新结果。
// 一次刷新（Authenticate + Update SDK）只计一次，两者任一失败即 error。
func observeEtcdTokenRefreshResult(result string) {
	etcdTokenRefresh.WithLabelValues(result).Inc()
}
