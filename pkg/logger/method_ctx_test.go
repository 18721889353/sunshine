package logger

import (
	"context"
	"fmt"
	"os"
	"testing"

	"go.uber.org/zap/zapcore"
)

// TestInfoWithCtx 测试 InfoWithCtx 方法
func TestInfoWithCtx(t *testing.T) {
	Init(WithLevel("debug"))

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-001")
	ctx = context.WithValue(ctx, "trace_id", "test-trace-001")

	// 应该自动包含 request_id 和 trace_id
	InfoWithCtx(ctx, "测试信息日志",
		String("user_id", "user-123"),
		Int("age", 25),
	)

	t.Log("InfoWithCtx test passed")
}

// TestErrorWithCtx 测试 ErrorWithCtx 方法
func TestErrorWithCtx(t *testing.T) {
	Init(WithLevel("debug"))

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-002")

	ErrorWithCtx(ctx, "测试错误日志",
		Err(fmt.Errorf("模拟错误")),
		String("operation", "create_order"),
	)

	t.Log("ErrorWithCtx test passed")
}

// TestWarnWithCtx 测试 WarnWithCtx 方法
func TestWarnWithCtx(t *testing.T) {
	Init(WithLevel("debug"))

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-003")

	WarnWithCtx(ctx, "测试警告日志",
		String("warning_type", "rate_limit"),
		Int("current_rate", 100),
	)

	t.Log("WarnWithCtx test passed")
}

// TestDebugWithCtx 测试 DebugWithCtx 方法
func TestDebugWithCtx(t *testing.T) {
	Init(WithLevel("debug"))

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-004")

	DebugWithCtx(ctx, "测试调试日志",
		String("step", "validation"),
		Bool("passed", true),
	)

	t.Log("DebugWithCtx test passed")
}

// TestModuleLogWithCtxIntegration 测试模块化日志与 Context 集成
func TestModuleLogWithCtxIntegration(t *testing.T) {
	Init(WithLevel("debug"))

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-005")
	ctx = context.WithValue(ctx, "trace_id", "test-trace-005")

	// 测试不同模块的日志
	ModuleInfoWithCtx(ctx, "order", "订单创建", String("order_id", "ORD-001"))
	ModuleErrorWithCtx(ctx, "payment", "支付失败", Err(fmt.Errorf("支付超时")))
	ModuleWarnWithCtx(ctx, "inventory", "库存不足", Int("remaining", 0))
	ModuleDebugWithCtx(ctx, "notification", "发送通知", String("type", "email"))

	t.Log("ModuleLogWithCtx integration test passed")
}

// TestIsDebugEnabled 测试日志级别检查
func TestIsDebugEnabled(t *testing.T) {
	// 测试 DEBUG 级别
	Init(WithLevel("debug"))
	if !IsDebugEnabled() {
		t.Error("Expected DEBUG level to be enabled")
	}
	if !IsInfoEnabled() {
		t.Error("Expected INFO level to be enabled when DEBUG is set")
	}

	// 测试 INFO 级别
	Init(WithLevel("info"))
	if IsDebugEnabled() {
		t.Error("Expected DEBUG level to be disabled when INFO is set")
	}
	if !IsInfoEnabled() {
		t.Error("Expected INFO level to be enabled")
	}

	t.Log("IsDebugEnabled test passed")
}

// TestContextExtraction 测试 Context 字段提取
func TestContextExtraction(t *testing.T) {
	Init(WithLevel("debug"))

	// 测试完整的 context
	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "req-123")
	ctx = context.WithValue(ctx, "trace_id", "trace-456")

	InfoWithCtx(ctx, "完整 context 测试")

	// 测试只有 request_id
	ctx2 := context.WithValue(context.Background(), ContextKeyForRequestID(), "req-789")
	InfoWithCtx(ctx2, "只有 request_id")

	// 测试空 context
	ctx3 := context.Background()
	InfoWithCtx(ctx3, "空 context")

	t.Log("Context extraction test passed")
}

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
	t.Log("")
	t.Log("═══════════════════════════════════════════════════════════")
	t.Log("如何查看 SLS 日志：")
	t.Log("═══════════════════════════════════════════════════════════")
	t.Logf("1. 登录阿里云 SLS 控制台: https://sls.console.aliyun.com")
	t.Logf("2. 选择 Project: %s", slsConfig.ProjectName)
	t.Logf("3. 选择 LogStore: %s", slsConfig.LogStoreName)
	t.Logf("4. 查询条件: __topic__: test")
	t.Logf("5. 时间范围: 选择'最近 15 分钟'或自定义时间")
	t.Logf("6. 应该能看到 3 条日志 (INFO, ERROR, WARN)")
	t.Log("═══════════════════════════════════════════════════════════")
	t.Log("")
	t.Log("本地日志文件：")
	t.Log("  - test-sls.log (默认日志)")
	t.Log("  - logs/order/order.log (订单模块)")
	t.Log("  - logs/payment/payment.log (支付模块)")
	// 注意：defer slsHook.Close() 会自动等待日志发送完成（最多30秒）
}

// TestTerminalLoggingOnly 测试日志只输出到终端，不保存到文件
func TestTerminalLoggingOnly(t *testing.T) {
	// 初始化 Logger：isSave=false
	_, err := Init(
		WithLevel("debug"),
		WithFormat("console"), // 使用 console 格式便于肉眼观察
		WithSave(false),       // ✅ 关键：设置为 false，不保存文件
	)
	if err != nil {
		t.Fatalf("Failed to init logger: %v", err)
	}

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-terminal-001")

	// 记录日志（应该只出现在终端/控制台，不会生成任何 .log 文件）
	InfoWithCtx(ctx, "终端日志测试 - 这条日志不应该保存到文件")
	ErrorWithCtx(ctx, "终端错误测试", Err(fmt.Errorf("模拟错误")))

	// 验证：检查当前目录下是否意外生成了日志文件
	if _, err := os.Stat("logs/app.log"); err == nil {
		t.Error("Unexpected log file 'logs/app.log' was created when isSave=false")
	}

	t.Log("Terminal-only logging test passed (no files should be generated)")
}

// TestLocalFileLogging 测试本地文件日志保存
func TestLocalFileLogging(t *testing.T) {
	// 初始化 Logger，只保存到本地文件
	_, err := Init(
		WithLevel("debug"),
		WithFormat("json"),
		WithSave(true,
			WithFileName("test-local.log"),
			WithFileMaxSize(10),          // 10MB
			WithFileMaxBackups(5),        // 保留5个备份
			WithFileMaxAge(7),            // 保留7天
			WithFileIsCompression(false), // 不压缩
		),
	)
	if err != nil {
		t.Fatalf("Failed to init logger: %v", err)
	}

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-local-001")
	ctx = context.WithValue(ctx, "trace_id", "test-trace-local-001")

	// 记录各种类型的日志
	InfoWithCtx(ctx, "本地文件测试 - 用户登录",
		String("username", "testuser"),
		String("ip", "192.168.1.100"),
		Int("port", 8080),
	)

	ErrorWithCtx(ctx, "本地文件测试 - 数据库错误",
		Err(fmt.Errorf("connection timeout")),
		String("database", "mysql"),
		String("host", "localhost:3306"),
	)

	ModuleInfoWithCtx(ctx, "order", "订单创建成功",
		String("order_id", "ORD-2024-001"),
		Int("amount", 9999),
		String("currency", "CNY"),
	)

	t.Log("Local file logging test completed (check file: test-local.log)")
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

// ==================== Table-driven 测试 ====================

// TestLogWithCtxByLevel table-driven: 不同日志级别测试
func TestLogWithCtxByLevel(t *testing.T) {
	Init(WithLevel("debug"))

	tests := []struct {
		name  string
		logFn func(ctx context.Context, msg string, fields ...Field)
		msg   string
	}{
		{"Debug", DebugWithCtx, "debug level test"},
		{"Info", InfoWithCtx, "info level test"},
		{"Warn", WarnWithCtx, "warn level test"},
		{"Error", ErrorWithCtx, "error level test"},
	}

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "table-req-001")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.logFn(ctx, tt.msg,
				String("user_id", "user-table"),
				Int("count", 1),
			)
		})
	}
}

// TestModuleLogByLevel table-driven: 不同模块 + 级别测试
func TestModuleLogByLevel(t *testing.T) {
	Init(WithLevel("debug"))

	tests := []struct {
		name   string
		logFn  func(ctx context.Context, module, msg string, fields ...Field)
		module string
		msg    string
	}{
		{"ModuleInfo", ModuleInfoWithCtx, "order", "order created"},
		{"ModuleError", ModuleErrorWithCtx, "payment", "payment failed"},
		{"ModuleWarn", ModuleWarnWithCtx, "inventory", "stock low"},
		{"ModuleDebug", ModuleDebugWithCtx, "notification", "send email"},
	}

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "table-module-001")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.logFn(ctx, tt.module, tt.msg,
				String("module", tt.module),
			)
		})
	}
}

// TestContextExtractionTable table-driven: Context 字段提取测试
func TestContextExtractionTable(t *testing.T) {
	Init(WithLevel("debug"))

	tests := []struct {
		name      string
		ctx       context.Context
		wantReqID bool
	}{
		{"nil context", nil, false},
		{"empty context", context.Background(), false},
		{"with request_id", context.WithValue(context.Background(), ContextKeyForRequestID(), "req-123"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 验证不 panic
			InfoWithCtx(tt.ctx, "context extraction test")
		})
	}
}

// TestMetrics 测试运行时指标暴露
func TestMetrics(t *testing.T) {
	Init(WithLevel("debug"))

	m := GetMetrics()
	if m.DroppedEntries < 0 {
		t.Errorf("DroppedEntries should be >= 0, got %d", m.DroppedEntries)
	}

	// 验证不 panic
	t.Logf("Metrics: dropped=%d, router_loggers=%d", m.DroppedEntries, m.RouterLoggerCount)
}

// TestErrField 测试 Err() 函数处理 nil 和非 nil
func TestErrField(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantNil bool
	}{
		{"nil error", nil, true},
		{"non-nil error", fmt.Errorf("test error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Err(tt.err)
			if tt.wantNil && f.Type != zapcore.SkipType {
				t.Errorf("expected SkipType for nil error, got %v", f.Type)
			}
			if !tt.wantNil && f.Type == zapcore.SkipType {
				t.Errorf("expected non-SkipType for non-nil error")
			}
		})
	}
}

// TestLevelFilter 测试日志级别过滤
func TestLevelFilter(t *testing.T) {
	Init(WithLevel("warn"))

	if IsDebugEnabled() {
		t.Error("DEBUG should be disabled when level is WARN")
	}
	if IsInfoEnabled() {
		t.Error("INFO should be disabled when level is WARN")
	}

	Init(WithLevel("debug"))

	if !IsDebugEnabled() {
		t.Error("DEBUG should be enabled when level is DEBUG")
	}
	if !IsInfoEnabled() {
		t.Error("INFO should be enabled when level is DEBUG")
	}
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
