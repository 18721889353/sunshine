//go:build integration

package tracer

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TestOTLPIntegration HTTP 上报集成测试（从环境变量读取配置）。
// 运行方式：
//
//	cd pkg/tracer
//	go test -tags=integration -v -run TestOTLPIntegration -count=1
//
// 需要先加载 .env（IDE 中配置 EnvFile，或手动 export）。
func TestOTLPIntegration(t *testing.T) {
	t.Cleanup(func() { _ = Close(context.Background()) })

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT_HTTP")
	token := os.Getenv("OTEL_EXPORTER_OTLP_TOKEN")
	if endpoint == "" {
		t.Skip("OTEL_EXPORTER_OTLP_ENDPOINT_HTTP 未设置，跳过 HTTP 集成测试")
	}

	t.Log("=== HTTP OTLP 集成测试 ===")
	t.Logf("endpoint: %s", endpoint)

	opts := []OTLPOption{
		WithInsecure(true),
	}
	if token != "" {
		opts = append(opts, WithHeaders(map[string]string{
			"Authorization": "Bearer " + token,
		}))
	}

	err := InitWithOTLP(
		"tracer-test",
		"test",
		"v0.0.1-test",
		1.0,
		endpoint,
		opts...,
	)
	if err != nil {
		t.Fatalf("InitWithOTLP 失败: %v", err)
	}
	t.Log("InitWithOTLP 成功")

	// 创建几个测试 Span
	tr := GetProvider().Tracer("tracer-test")
	for i := 0; i < 3; i++ {
		_, span := tr.Start(context.Background(), "test-span",
			oteltrace.WithAttributes(attribute.String("test.index", fmt.Sprintf("%d", i))),
		)
		span.SetAttributes(attribute.String("test.env", "integration"))
		time.Sleep(50 * time.Millisecond)
		span.End()
	}
	t.Log("已创建 3 个测试 Span")

	// 关闭 provider，确保 Span 全部导出
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := Close(ctx); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	// t.Cleanup 中的 Close 为防御性二次关闭（幂等），确保即使 Fatalf 未触发也能清理
	t.Log("Close 成功，Span 已上报")
}
