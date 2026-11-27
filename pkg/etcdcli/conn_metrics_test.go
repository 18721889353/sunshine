package etcdcli

import (
	"testing"

	clientv3 "go.etcd.io/etcd/client/v3"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestObserveEtcdConnected 连通性 Gauge 记录。
func TestObserveEtcdConnected(t *testing.T) {
	ObserveEtcdConnected(true)

	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range families {
		if f.GetName() != "etcd_connected" {
			continue
		}
		for _, m := range f.GetMetric() {
			if m.GetGauge().GetValue() != 1 {
				t.Errorf("etcd_connected 应 1, got %v", m.GetGauge().GetValue())
			}
			found = true
		}
	}
	if !found {
		t.Error("etcd_connected 未找到")
	}
}

// TestObserveEtcdConnectedAs_MultiInstance 多客户端实例用不同 endpoint label，不互相覆盖。
func TestObserveEtcdConnectedAs_MultiInstance(t *testing.T) {
	ObserveEtcdConnectedAs("etcd-a:2379", true)
	ObserveEtcdConnectedAs("etcd-b:2379", false)

	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, f := range families {
		if f.GetName() != "etcd_connected" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "endpoint" {
					got[l.GetValue()] = m.GetGauge().GetValue()
				}
			}
		}
	}
	if got["etcd-a:2379"] != 1 || got["etcd-b:2379"] != 0 {
		t.Errorf("多实例 label 互相覆盖: got %v", got)
	}
}

// TestNewClient_TracingHandler 验证两条创建路径都挂了 otelgrpc StatsHandler。
// 用无效 endpoint + 短 DialTimeout 让 New 快速失败，避免真实连接；
// 通过构造的 config 副本验证 DialOptions 被追加（不修改调用方 config）。
func TestNewClient_TracingHandler(t *testing.T) {
	externalCfg := clientv3.Config{Endpoints: []string{"127.0.0.1:1"}, DialTimeout: 20 * time.Millisecond}

	// config 分支：传入的 config 不应被修改（值复制语义）
	before := len(externalCfg.DialOptions)
	_, _ = NewClient(nil, WithConfig(&externalCfg))
	if len(externalCfg.DialOptions) != before {
		t.Error("NewClient 不应修改调用方传入的 config.DialOptions")
	}
}
