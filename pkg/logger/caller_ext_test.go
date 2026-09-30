package logger_test

import (
	"context"
	"strings"
	"testing"

	"github.com/18721889353/sunshine/pkg/logger"
	"go.uber.org/zap/zapcore"
)

// TestCustomHookWithCtxCaller 验证自定义 ctx 钩子拿到的 caller 是用户调用点本身，
// 而不是 logger 内部帧（回归 caller 栈深度错误）。
// 本文件位于外部测试包 logger_test，模拟真实用户从 logger 包之外调用，
// callerOutsideLogger 应把首个非 logger 包帧（即本测试函数）作为 caller。
func TestCustomHookWithCtxCaller(t *testing.T) {
	var captured zapcore.EntryCaller
	hook := func(_ context.Context, entry zapcore.Entry, _ []logger.Field) error {
		captured = entry.Caller
		return nil
	}
	_, err := logger.Init(
		logger.WithLevel("debug"),
		logger.WithFormat("console"),
		logger.WithSave(false),
		logger.WithCustomHooksWithCtx(hook),
	)
	if err != nil {
		t.Fatalf("init logger failed: %v", err)
	}

	// 路径一：经公开 *WithCtx 方法触发钩子，caller 应指向本测试文件
	logger.InfoWithCtx(context.Background(), "via api") // 期望 caller 定位到此行
	if !captured.Defined || !strings.Contains(captured.File, "caller_ext_test.go") {
		t.Errorf("via *WithCtx: caller 期望指向本测试文件, 实际 defined=%v file=%q", captured.Defined, captured.File)
	}

	// 路径二：直接调用 ExecuteCustomHooksWithCtx，caller 同样应指向本测试文件
	captured = zapcore.EntryCaller{}
	if execErr := logger.ExecuteCustomHooksWithCtx(context.Background(), zapcore.InfoLevel, "direct"); execErr != nil {
		t.Fatalf("execute hooks failed: %v", execErr)
	}
	if !captured.Defined || !strings.Contains(captured.File, "caller_ext_test.go") {
		t.Errorf("direct call: caller 期望指向本测试文件, 实际 defined=%v file=%q", captured.Defined, captured.File)
	}
}
