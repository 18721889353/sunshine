package gows

import (
	"sync/atomic"

	prom "github.com/prometheus/client_golang/prometheus"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// gows 指标：连接数 + panic 恢复。
//
// 为什么必须有：WebSocket 是长连接，"心跳 panic 反复发生"是严重故障，
// 但进程不挂、日志淹没，没有指标发现不了。
// 之前 gows 完全没有指标，与 rabbitmq/kafka 覆盖不一致。
//
// 连接数告警注意：eqZero 规则（掉到 0 即告警）默认【未启用】——
// 服务启动初期无连接是正常状态，启用会误报。需要时通过 yml gaugeAbove
// 配 above 阈值，或用 DiagnosisHook 自定义判定。
var (
	descLocalClients = prom.NewDesc("gows_local_connections", "Number of local WebSocket connections.", nil, nil)
	descRemoteUIDs   = prom.NewDesc("gows_remote_uids", "Number of remote UIDs in distributed registry.", nil, nil)
)

// collector 引用当前 dispatcher 的连接计数器——用 atomic.Pointer 保护并发读写
// （RegisterStats 与 Gather 可能在不同 goroutine 同时发生）。
var activeStats atomic.Pointer[connectionStats]

type connectionStats struct {
	local  atomicInt32
	remote atomicInt32
}

// atomicInt32 抽象 atomic.Int32 的 Load（避免 import 循环/具体类型耦合）。
type atomicInt32 interface{ Load() int32 }

func init() {
	pkgmetrics.MustRegister(prom.CollectorFunc(collectGowsStats))
}

// RegisterStats 登记 dispatcher 的连接计数器。
// 用 Store（最新胜出）而非 CAS：dispatcher 重建时（测试多次 NewDispatcher、
// 或进程内重建）必须指向新实例，否则指标冻结在旧 dispatcher 的计数器上——
// 显示的连接数是已销毁实例的值，误导运维。
func RegisterStats(local, remote atomicInt32) {
	activeStats.Store(&connectionStats{local: local, remote: remote})
}

func collectGowsStats(ch chan<- prom.Metric) {
	st := activeStats.Load()
	if st == nil {
		return // 未启动 dispatcher：不 emit，避免创建假 0 样本
	}
	ch <- prom.MustNewConstMetric(descLocalClients, prom.GaugeValue, float64(st.local.Load()))
	ch <- prom.MustNewConstMetric(descRemoteUIDs, prom.GaugeValue, float64(st.remote.Load()))
}

// observeGowsPanic 记录 gows 内部 panic 恢复（复用公共 panic 指标）。
func observeGowsPanic(component string) {
	pkgmetrics.ObservePanic(component)
}
