package cache

import (
	"errors"
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
	observeCacheRead("Get", nil)            // hit
	observeCacheRead("Get", CacheNotFound)  // miss
	observeCacheRead("Get", errFake)        // 真错误：不计 hit/miss
	observeCacheRead("Get", ErrPlaceholder) // miss
}

var errFake = errors.New("fake")
