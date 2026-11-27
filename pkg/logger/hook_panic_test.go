package logger

import (
	"context"
	"fmt"
	"testing"

	"go.uber.org/zap/zapcore"
)

// TestExecuteHooksIgnoreError_Panic hook panic 必须被兜住，不崩业务 goroutine。
func TestExecuteHooksIgnoreError_Panic(t *testing.T) {
	original := customHooksWithCtxPtr.Load()
	t.Cleanup(func() { customHooksWithCtxPtr.Store(original) })

	panicking := []CustomHookWithCtx{
		func(context.Context, zapcore.Entry, []Field) error { panic("hook boom") },
	}
	customHooksWithCtxPtr.Store(&panicking)

	// 不应 panic（本用例若 panic 则测试失败）
	executeHooksIgnoreError(context.Background(), zapcore.WarnLevel, "msg", nil)
}

// TestExecuteHooksIgnoreError_Error hook 返回 error 不影响主流程。
func TestExecuteHooksIgnoreError_Error(t *testing.T) {
	original := customHooksWithCtxPtr.Load()
	t.Cleanup(func() { customHooksWithCtxPtr.Store(original) })

	withErr := []CustomHookWithCtx{
		func(context.Context, zapcore.Entry, []Field) error { return fmt.Errorf("hook err") },
	}
	customHooksWithCtxPtr.Store(&withErr)
	executeHooksIgnoreError(context.Background(), zapcore.WarnLevel, "msg", nil)
}
