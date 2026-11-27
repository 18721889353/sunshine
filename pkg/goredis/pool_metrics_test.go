package goredis

import (
	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
	"testing"

	"github.com/redis/go-redis/v9"
)

// fakePoolClient 实现 UniversalPoolStats，无需真实 Redis。
type fakePoolClient struct {
	stats *redis.PoolStats
}

func (f *fakePoolClient) PoolStats() *redis.PoolStats { return f.stats }

// TestRegisterPoolStats_Collect 验证连接池指标 emit + 登记幂等。
func TestRegisterPoolStats_Collect(t *testing.T) {
	fake := &fakePoolClient{stats: &redis.PoolStats{
		TotalConns: 12, IdleConns: 5, WaitCount: 3,
		WaitDurationNs: 2e9, Timeouts: 1,
	}}
	poolMu.Lock()
	orig := poolRegister
	poolRegister = map[string]UniversalPoolStats{}
	poolMu.Unlock()
	t.Cleanup(func() {
		poolMu.Lock()
		poolRegister = orig
		poolMu.Unlock()
	})

	RegisterPoolStats("test-pool", fake)
	RegisterPoolStats("test-pool", &fakePoolClient{}) // 幂等：不覆盖

	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	got := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			switch f.GetName() {
			case "redis_pool_total_conns":
				got["total"] = m.GetGauge().GetValue()
			case "redis_pool_wait_count_total":
				got["wait"] = m.GetCounter().GetValue()
			}
		}
	}
	if got["total"] != 12 {
		t.Errorf("total_conns 应 12（首次登记生效）, got %v", got["total"])
	}
	if got["wait"] != 3 {
		t.Errorf("wait_count 应 3, got %v", got["wait"])
	}
}

// TestCollectRedisPoolStats_Labels 等待类指标必须带 result=error（进诊断）。
func TestCollectRedisPoolStats_Labels(t *testing.T) {
	fake := &fakePoolClient{stats: &redis.PoolStats{WaitCount: 1}}
	poolMu.Lock()
	orig := poolRegister
	poolRegister = map[string]UniversalPoolStats{"lbl": fake}
	poolMu.Unlock()
	t.Cleanup(func() {
		poolMu.Lock()
		poolRegister = orig
		poolMu.Unlock()
	})

	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "redis_pool_wait_count_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			hasResult := false
			for _, l := range m.GetLabel() {
				if l.GetName() == "result" && l.GetValue() == "error" {
					hasResult = true
				}
			}
			if !hasResult {
				t.Error("redis_pool_wait_count_total 缺少 result=error label")
			}
		}
	}
}
