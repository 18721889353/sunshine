package cache

import (
	"errors"
	"fmt"
	"testing"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestCacheMetricRegistered 指标进 Fast Registry（巡检可达）
func TestCacheMetricRegistered(t *testing.T) {
	observeCache("Get", "ok", time.Millisecond)
	observeCacheRead("Get", nil)           // hit
	observeCacheRead("Get", CacheNotFound) // miss（counter 无样本不返回，需触发）
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	need := []string{"cache_operation_duration_seconds", "cache_hit_total", "cache_miss_total"}
	got := map[string]bool{}
	for _, f := range fams {
		got[f.GetName()] = true
	}
	for _, n := range need {
		if !got[n] {
			t.Errorf("%s 应进 Fast Registry", n)
		}
	}
}

// TestCacheGetResult 统一口径：nil/miss→ok，真错误→error（hit/miss 走独立 counter）
func TestCacheGetResult(t *testing.T) {
	if r := cacheGetResult(nil); r != "ok" {
		t.Errorf("nil(命中) 应为 ok, got %s", r)
	}
	if r := cacheGetResult(CacheNotFound); r != "ok" {
		t.Errorf("redis.Nil(未命中) 应为 ok, got %s", r)
	}
	if r := cacheGetResult(ErrPlaceholder); r != "ok" {
		t.Errorf("placeholder(未命中) 应为 ok, got %s", r)
	}
	if r := cacheGetResult(errFake); r != "error" {
		t.Errorf("真错误应为 error, got %s", r)
	}
}

// TestObserveCacheRead hit/miss 计数口径：真错误不计入 hit/miss
func TestObserveCacheRead(t *testing.T) {
	hitBefore := readCounter(t, "cache_hit_total", "op=Get")
	missBefore := readCounter(t, "cache_miss_total", "op=Get")

	observeCacheRead("Get", nil)            // hit
	observeCacheRead("Get", CacheNotFound)  // miss
	observeCacheRead("Get", errFake)        // 真错误：不计 hit/miss
	observeCacheRead("Get", ErrPlaceholder) // miss

	hitAfter := readCounter(t, "cache_hit_total", "op=Get")
	missAfter := readCounter(t, "cache_miss_total", "op=Get")
	if hitAfter-hitBefore != 1 {
		t.Errorf("hit 应 +1, got %.0f", hitAfter-hitBefore)
	}
	if missAfter-missBefore != 2 {
		t.Errorf("miss 应 +2（Nil+Placeholder），真错误不计, got %.0f", missAfter-missBefore)
	}
}

// readCounter 从 Fast Registry 读 counter 当前值（labelKey 精确匹配）。
func readCounter(t *testing.T, metric, labelKey string) float64 {
	t.Helper()
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	for _, f := range fams {
		if f.GetName() != metric {
			continue
		}
		for _, m := range f.Metric {
			if m.Counter == nil {
				continue
			}
			key := ""
			for _, l := range m.Label {
				if key != "" {
					key += ","
				}
				key += l.GetName() + "=" + l.GetValue()
			}
			if key == labelKey {
				return m.Counter.GetValue()
			}
		}
	}
	return 0
}

var errFake = errors.New("fake")

// TestErrorChain_RedisWrapping Redis 错误必须用 %w/errors.Join 包装——
// %v 会断链，调用方 errors.Is(err, redis.Nil/CacheNotFound) 失效，
// 命中率与静默故障判定就会误判。
func TestErrorChain_RedisWrapping(t *testing.T) {
	sentinel := errors.New("sentinel")

	// 单错误 %w
	wrapped := fmt.Errorf("BuildCacheKey error: %w, key=%s", sentinel, "k")
	if !errors.Is(wrapped, sentinel) {
		t.Error("单错误应 errors.Is 穿透")
	}

	// 多错误 errors.Join
	child1 := errors.New("child1")
	child2 := CacheNotFound
	joined := fmt.Errorf("partial failures: %w", errors.Join(child1, child2))
	if !errors.Is(joined, child1) || !errors.Is(joined, child2) {
		t.Error("errors.Join 后应能匹配每个子错误")
	}
}
