package etcdcli

import (
	"context"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/connectivity"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// etcdConnected etcd 连接状态（1=Ready，0=其他状态）。
//
// 为什么必须有：etcd_token_refresh_total 只覆盖"鉴权操作失败"，
// 独立的连通性探针能区分"操作层面问题"与"连接层面问题"，
// 且无 RPC 开销（本地读 grpc 连接状态）。
var etcdConnected = pkgmetrics.Gauge("etcd_connected", "etcd connectivity (1=Ready, 0=other states).", "endpoint")

// connMonitorCancels 存储每个客户端的连接监控取消函数
// （与 refreshCancels 分开——同一 map 存两个 cancel 会互相覆盖）。
var connMonitorCancels sync.Map // map[*clientv3.Client]context.CancelFunc

// storeCancelIfAbsent 幂等注册 cancel：已存在则取消新建的并返回 false。
//
// 为什么需要：连接监控/token 刷新都是后台 goroutine，若重复启动用 Store 覆盖
// cancel，旧 goroutine 的取消句柄就永久丢失（泄漏）。抽成通用函数便于单测。
func storeCancelIfAbsent(m *sync.Map, key any, cancel context.CancelFunc) bool {
	_, loaded := m.LoadOrStore(key, cancel)
	if loaded {
		cancel() // 新建 ctx 无 goroutine 使用，直接释放
		return false
	}
	return true
}

// ObserveEtcdConnected 记录 etcd 连通性（grpc 连接状态，单实例默认 label）。
func ObserveEtcdConnected(ready bool) {
	ObserveEtcdConnectedAs("default", ready)
}

// ObserveEtcdConnectedAs 记录指定 endpoint 的连通性（多客户端实例场景）。
func ObserveEtcdConnectedAs(endpoint string, ready bool) {
	if endpoint == "" {
		endpoint = "default"
	}
	v := 0.0
	if ready {
		v = 1.0
	}
	etcdConnected.WithLabelValues(endpoint).Set(v)
}

// StartConnMonitor 定期读取 etcd grpc 连接状态（本地状态，无 RPC 开销）。
// 与 startTokenRefresh 同模式：内部创建 cancel，StopConnMonitor 释放。
func StartConnMonitor(cli *clientv3.Client, interval time.Duration) {
	if cli == nil || interval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	// 幂等：同一 cli 重复 Start 不覆盖已有 cancel（否则旧 goroutine 永远无法停止）。
	if !storeCancelIfAbsent(&connMonitorCancels, cli, cancel) {
		return
	}

	// endpoints 在启动时取一次（多客户端实例用不同 label，避免互相覆盖）
	endpoint := "default"
	if eps := cli.Endpoints(); len(eps) > 0 {
		endpoint = eps[0]
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn := cli.ActiveConnection()
				if conn == nil {
					ObserveEtcdConnectedAs(endpoint, false)
					continue
				}
				// Ready = 连接已建立且可发送 RPC；其他状态视为不可用
				ObserveEtcdConnectedAs(endpoint, conn.GetState() == connectivity.Ready)
			}
		}
	}()
}

// StopConnMonitor 停止指定客户端的连接监控。
func StopConnMonitor(cli *clientv3.Client) {
	if cli == nil {
		return
	}
	if cancel, ok := connMonitorCancels.LoadAndDelete(cli); ok {
		if fn, ok := cancel.(context.CancelFunc); ok {
			fn()
		}
	}
}
