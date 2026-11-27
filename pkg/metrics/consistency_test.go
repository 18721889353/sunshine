package metrics

import (
	"os"
	"strings"
	"testing"
)

func TestComponentSpecs_NewMetrics(t *testing.T) {
	names := []string{
		"panic_recovered_total", "request_timeout_total", "request_retry_total",
		"kafka_publish_duration_seconds", "kafka_consume_duration_seconds",
		"db_pool_open_connections", "db_pool_in_use_connections",
		"db_pool_wait_count_total", "db_pool_wait_duration_seconds_total",
		"gows_local_connections", "gows_remote_uids",
	}
	for _, name := range names {
		if _, ok := componentSpecs[metricBase(name)]; !ok {
			t.Errorf("指标 %s 的 base=%s 未在 componentSpecs 登记", name, metricBase(name))
		}
	}
}

func TestConsistency_MetricsInComponentSpecs(t *testing.T) {
	fams, err := Default().GatherDefault()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, f := range fams {
		base := metricBase(f.GetName())
		if len(base) >= 3 && base[:3] == "go_" {
			continue // 运行时指标走 runtime 分组，不需登记
		}
		if len(base) >= 8 && base[:8] == "process_" || base == "promhttp" {
			continue // 进程级/HTTP 库指标，与 go_* 同类
		}
		if strings.Contains(base, "test") {
			continue // 其他测试注册的指标（测试污染 GatherDefault，非生产指标）
		}
		if _, ok := componentSpecs[base]; !ok {
			t.Errorf("指标 %s 的 base=%s 未在 componentSpecs 登记", f.GetName(), base)
		}
	}
}

func TestConsistency_MetricsInREADME(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("读取 README: %v", err)
	}
	content := string(readme)
	fams, err := Default().GatherDefault()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, f := range fams {
		name := f.GetName()
		base := metricBase(name)
		if len(base) >= 3 && base[:3] == "go_" || len(base) >= 8 && base[:8] == "process_" {
			continue // 运行时/进程级指标，README 用 go_* 泛指
		}
		if strings.Contains(base, "test") {
			continue // 测试注册的指标
		}
		// 指标名或其 base 应出现在 README
		if !strings.Contains(content, name) && !strings.Contains(content, base) {
			t.Errorf("指标 %s 未在 README 中声明", name)
		}
	}
}
