package logger

import (
	"context"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
)

// Sync flushing any buffered log entries, applications should take care to call Sync before exiting.
func Sync() error {
	// 如果默认 logger 是输出到终端 (stdout)，则跳过 Sync，避免在关闭时产生文件 I/O
	if defaultLogger != nil {
		// zap 的 stdout 路径通常包含 "stdout" 或 "/dev/stdout"
		// 我们通过检查是否开启了 isSave 来判断，但这里无法直接获取 options
		// 简单的做法是：如果 Sync 报错且与 stdout 有关，则忽略
	}
	
	_ = getSugaredLogger().Sync()
	err := getLogger().Sync()
	if err != nil && !strings.Contains(err.Error(), "/dev/stdout") && !strings.Contains(err.Error(), "stdout") {
		return err
	}
	return nil
}

// ==================== 强制使用 Context 的日志方法 ====================

// DebugWithCtx 调试级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func DebugWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)
	
	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		ExecuteCustomHooksWithCtx(ctx, zapcore.DebugLevel, msg, allFields...)
	}
	
	getLogger().Debug(msg, allFields...)
}

// InfoWithCtx 信息级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func InfoWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)
	
	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		ExecuteCustomHooksWithCtx(ctx, zapcore.InfoLevel, msg, allFields...)
	}
	
	getLogger().Info(msg, allFields...)
}

// WarnWithCtx 警告级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func WarnWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)
	
	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		ExecuteCustomHooksWithCtx(ctx, zapcore.WarnLevel, msg, allFields...)
	}
	
	getLogger().Warn(msg, allFields...)
}

// ErrorWithCtx 错误级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func ErrorWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)
	
	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		ExecuteCustomHooksWithCtx(ctx, zapcore.ErrorLevel, msg, allFields...)
	}
	
	getLogger().Error(msg, allFields...)
}

// PanicWithCtx panic 级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func PanicWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)
	
	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		ExecuteCustomHooksWithCtx(ctx, zapcore.PanicLevel, msg, allFields...)
	}
	
	getLogger().Panic(msg, allFields...)
}

// FatalWithCtx fatal 级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func FatalWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)
	
	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		ExecuteCustomHooksWithCtx(ctx, zapcore.FatalLevel, msg, allFields...)
	}
	
	getLogger().Fatal(msg, allFields...)
}

// IsDebugEnabled 检查是否启用了 DEBUG 级别日志
// 用于性能敏感场景，避免不必要的字符串拼接或对象创建
func IsDebugEnabled() bool {
	return getLogger().Core().Enabled(zapcore.DebugLevel)
}

// IsInfoEnabled 检查是否启用了 INFO 级别日志
func IsInfoEnabled() bool {
	return getLogger().Core().Enabled(zapcore.InfoLevel)
}

// ModuleLogWithCtx 按模块记录日志(带Context) - 自动提取 request_id/trace_id
// 用法: logger.ModuleErrorWithCtx(ctx, "order", "订单创建失败", logger.Err(err))
func ModuleLogWithCtx(ctx context.Context, module string, levelFunc func(string, ...Field), msg string, fields ...Field) {
	// 从 context 中提取链路字段
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)

	levelFunc(msg, allFields...)
}

// ModuleErrorWithCtx 按模块记录错误日志(带Context)
func ModuleErrorWithCtx(ctx context.Context, module string, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	GetLogger(module).Error(msg, append(ctxFields, fields...)...)
}

// ModuleWarnWithCtx 按模块记录警告日志(带Context)
func ModuleWarnWithCtx(ctx context.Context, module string, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	GetLogger(module).Warn(msg, append(ctxFields, fields...)...)
}

// ModuleInfoWithCtx 按模块记录信息日志(带Context)
func ModuleInfoWithCtx(ctx context.Context, module string, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	GetLogger(module).Info(msg, append(ctxFields, fields...)...)
}

// ModuleDebugWithCtx 按模块记录调试日志(带Context)
func ModuleDebugWithCtx(ctx context.Context, module string, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	GetLogger(module).Debug(msg, append(ctxFields, fields...)...)
}

// ExecuteCustomHooksWithCtx 手动执行带 Context 的自定义钩子
// 用法: 在业务代码中调用此函数来执行需要访问 context 的钩子
// 示例:
//   ctx := context.WithValue(context.Background(), logger.ContextKeyForRequestID(), "12345")
//   logger.ExecuteCustomHooksWithCtx(ctx, zapcore.InfoLevel, "user login", logger.String("user_id", "123"))
func ExecuteCustomHooksWithCtx(ctx context.Context, level zapcore.Level, msg string, fields ...Field) error {
	if len(customHooksWithCtx) == 0 {
		return nil
	}

	// 获取调用者信息（跳过 ExecuteCustomHooksWithCtx 和 *WithCtx 两层）
	pc, file, line, ok := runtime.Caller(2)
	caller := zapcore.NewEntryCaller(0, "", 0, false)
	if ok {
		// 简化文件路径，只保留最后两层目录
		if idx := strings.LastIndex(file, "/"); idx != -1 {
			file = file[idx+1:]
		}
		caller = zapcore.EntryCaller{
			Defined: true,
			PC:      uintptr(pc),
			File:    file,
			Line:    line,
		}
	}

	entry := zapcore.Entry{
		Level:      level,
		Time:       time.Now(),
		LoggerName: "",
		Message:    msg,
		Caller:     caller,
		Stack:      "",
	}

	for _, hook := range customHooksWithCtx {
		if err := hook(ctx, entry, fields); err != nil {
			return err
		}
	}
	return nil
}
