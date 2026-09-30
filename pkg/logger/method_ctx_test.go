package logger

import (
	"context"
	"fmt"
	"os"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zapcore"
)

// TestInfoWithCtx 测试 InfoWithCtx 方法
func TestInfoWithCtx(t *testing.T) {
	Init(WithLevel("debug"))

	ctx := context.WithValue(context.Background(), ContextKeyForRequestID(), "test-req-001")

	// request_id 会被自动提取；trace_id 需通过 OTel SpanContext 注入（见 TestExtractContextFieldsTraceID）
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

	InfoWithCtx(ctx, "完整 context 测试")

	// 测试只有 request_id
	ctx2 := context.WithValue(context.Background(), ContextKeyForRequestID(), "req-789")
	InfoWithCtx(ctx2, "只有 request_id")

	// 测试空 context
	ctx3 := context.Background()
	InfoWithCtx(ctx3, "空 context")

	t.Log("Context extraction test passed")
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

// TestExtractContextFieldsTraceID 验证 trace_id/span_id 只有当 ctx 携带真实 OTel SpanContext 时才会被提取，
// 非 SpanContext 来源（自定义类型 key）不会被提取——修正此前测试对 trace_id 的误导性写法
func TestExtractContextFieldsTraceID(t *testing.T) {
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	got := map[string]bool{}
	for _, f := range extractContextFields(ctx) {
		got[f.Key] = true
	}
	if !got["trace_id"] || !got["span_id"] {
		t.Errorf("期望提取到 trace_id/span_id, 实际字段: %+v", extractContextFields(ctx))
	}

	// 反例：用自定义类型 key（而非 OTel SpanContext）存 trace_id 时不会被提取。
	// 用类型化 key 而非裸 string，避免触发 staticcheck SA1029（unhandled key type）
	type traceIDTestKey string
	ctx2 := context.WithValue(context.Background(), traceIDTestKey("trace_id"), "should-be-ignored")
	for _, f := range extractContextFields(ctx2) {
		if f.Key == "trace_id" {
			t.Errorf("自定义类型 key 存储的 trace_id 不应被提取")
		}
	}
}
