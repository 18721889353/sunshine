//go:build integration

// 本文件带 integration 构建标签：TestSLSIntegration / TestMixedLogging 会创建真实 SLS Producer
// 并向配置的 LogStore 发送日志（网络 IO、失败噪音、Close 最多阻塞 30 秒），不属于默认单元测试范畴。
// 运行方式：go test -tags integration ./pkg/logger/ -run 'TestSLSIntegration|TestMixedLogging'
// 默认 `go test ./pkg/logger/` 不编译本文件，保证纯 CPU/内存测试无网络副作用。
// 注意：本文件不含 TestMain（同包唯一 TestMain 在 main_test.go），加 -tags integration 编译时不会与入口冲突。
package logger

import (
	"context"
	"fmt"
	"testing"
)

// TestSLSIntegration 测试 SLS 日志上报集成
func TestSLSIntegration(t *testing.T) {
	// SLS 配置（从环境变量或配置文件读取）
	slsConfig := &SLSConfig{
		Endpoint:        getEnv("SLS_ENDPOINT", "cn-shanghai.log.aliyuncs.com"),
		AccessKeyID:     getEnv("SLS_ACCESS_KEY_ID", "REMOVED_SECRET"),
		AccessKeySecret: getEnv("SLS_ACCESS_KEY_SECRET", "REMOVED_SECRET"),
		ProjectName:     getEnv("SLS_PROJECT", "sunshine123"),
		LogStoreName:    getEnv("SLS_LOGSTORE", "sunshine"),
		Topic:           "test",
		Source:          "logger-test",
		MaxRetries:      3,
		Timeout:         30,
	}

	// 创建 SLS Hook
	slsHook, err := NewSLSHook(slsConfig)
	if err != nil {
		t.Logf("Failed to create SLS hook: %v", err)
		t.Logf("Please check:")
		t.Logf("  1. AccessKey ID/Secret is correct")
		t.Logf("  2. Project '%s' exists in region '%s'", slsConfig.ProjectName, slsConfig.Endpoint)
		t.Logf("  3. LogStore '%s' exists in the project", slsConfig.LogStoreName)
		t.Logf("  4. Network connection to SLS endpoint")
		t.Skip("Skipping SLS integration test")
		return
	}
	t.Logf("✓ SLS Hook created successfully")
	t.Logf("  Endpoint: %s", slsConfig.Endpoint)
	t.Logf("  Project: %s", slsConfig.ProjectName)
	t.Logf("  LogStore: %s", slsConfig.LogStoreName)
	defer slsHook.Close()

	// 初始化 Logger，同时启用本地文件保存和 SLS 上报
	_, err = Init(
		WithLevel("debug"),
		WithFormat("json"),
		WithAsync(true), // 启用异步写入，提升高并发性能
		WithSave(true,
			WithFileName("test-sls.log"),
			WithFileMaxSize(10),   // 10MB
			WithFileMaxBackups(3), // 保留3个备份
			WithFileMaxAge(7),     // 保留7天
		),
		WithCustomHooksWithCtx(slsHook.Hook),
		WithRoutes([]*RouteConfig{
			{
				Module:   "order",
				Filename: "logs/order/order.log",
				MaxSize:  50,
				MaxAge:   30,
				Format:   "json",
				IsAsync:  true,
			},
			{
				Module:   "payment",
				Filename: "logs/payment/payment.log",
				MaxSize:  20,
				MaxAge:   15,
				Format:   "json",
				IsAsync:  true,
			},
		}),
	)
	if err != nil {
		t.Fatalf("Failed to init logger: %v", err)
	}

	// 创建带追踪信息的 context
	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-sls-001")

	// 记录不同级别的日志（会同时保存到本地文件和 SLS）
	InfoWithCtx(ctx, "SLS 集成测试 - 信息日志",
		String("user_id", "user-001"),
		String("action", "login"),
	)

	ErrorWithCtx(ctx, "SLS 集成测试 - 错误日志",
		Err(fmt.Errorf("模拟错误")),
		String("operation", "create_order"),
		Int("order_id", 12345),
	)

	// 测试路由日志 - 订单模块
	ModuleInfoWithCtx(ctx, "order", "订单创建成功",
		String("order_id", "ORD-2024-001"),
		Int("amount", 9999),
	)

	// 测试路由日志 - 支付模块
	ModuleErrorWithCtx(ctx, "payment", "支付失败",
		Err(fmt.Errorf("支付超时")),
		String("order_id", "ORD-2024-001"),
	)

	t.Log("SLS integration test completed")
	// 注意：defer slsHook.Close() 会自动等待日志发送完成（最多30秒）
}

// TestMixedLogging 测试混合模式：本地文件 + SLS
func TestMixedLogging(t *testing.T) {
	// SLS 配置
	slsConfig := &SLSConfig{
		Endpoint:        getEnv("SLS_ENDPOINT", "cn-shanghai.log.aliyuncs.com"),
		AccessKeyID:     getEnv("SLS_ACCESS_KEY_ID", "REMOVED_SECRET"),
		AccessKeySecret: getEnv("SLS_ACCESS_KEY_SECRET", "REMOVED_SECRET"),
		ProjectName:     getEnv("SLS_PROJECT", "sunshine123"),
		LogStoreName:    getEnv("SLS_LOGSTORE", "sunshine"),
		Topic:           "mixed-test",
		Source:          "mixed-logger",
		MaxRetries:      3,
		Timeout:         30,
	}

	// 尝试创建 SLS Hook（失败时不影响本地日志）
	slsHook, err := NewSLSHook(slsConfig)
	hasSLS := err == nil
	if !hasSLS {
		t.Logf("SLS hook creation failed (will use local-only mode): %v", err)
	} else {
		defer slsHook.Close()
	}

	// 初始化 Logger
	opts := []Option{
		WithLevel("debug"),
		WithFormat("json"),
		WithSave(true,
			WithFileName("test-mixed.log"),
			WithFileMaxSize(10),
			WithFileMaxBackups(3),
			WithFileMaxAge(7),
		),
	}

	// 如果 SLS Hook 创建成功，添加到选项中
	if hasSLS {
		opts = append(opts, WithCustomHooksWithCtx(slsHook.Hook))
	}

	_, err = Init(opts...)
	if err != nil {
		t.Fatalf("Failed to init logger: %v", err)
	}

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-mixed-001")

	// 记录日志（会保存到本地，如果 SLS 可用也会上报）
	InfoWithCtx(ctx, "混合模式测试",
		String("mode", "local+sls"),
		Bool("sls_enabled", hasSLS),
	)

	t.Logf("Mixed logging test completed (local file: test-mixed.log, SLS: %v)", hasSLS)
}
