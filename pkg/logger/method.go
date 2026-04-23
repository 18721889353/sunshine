package logger

import (
	"context"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
)

// WithCallerFunc 将调用者方法名注入到 context 中
// 使用示例：
//
//	ctx = logger.WithCallerFunc(ctx, "UserService.GetUser")
//	db.WithContext(ctx).First(&user, id)
func WithCallerFunc(ctx context.Context, callerFunc string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ContextKeyCallerFunc, callerFunc)
}

// Sync flushing any buffered log entries, applications should take care to call Sync before exiting.
func Sync() error {
	// 如果默认 logger 是输出到终端 (stdout)，则跳过 Sync，避免在关闭时产生文件 I/O
	// defaultLogger != nil check is intentional

	_ = getSugaredLogger().Sync()
	err := getDefaultLogger().Sync()
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
		_ = ExecuteCustomHooksWithCtx(ctx, zapcore.DebugLevel, msg, allFields...)
	}

	getDefaultLogger().Debug(msg, allFields...)
}

// InfoWithCtx 信息级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func InfoWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)

	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		_ = ExecuteCustomHooksWithCtx(ctx, zapcore.InfoLevel, msg, allFields...)
	}

	getDefaultLogger().Info(msg, allFields...)
}

// WarnWithCtx 警告级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func WarnWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)

	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		_ = ExecuteCustomHooksWithCtx(ctx, zapcore.WarnLevel, msg, allFields...)
	}

	getDefaultLogger().Warn(msg, allFields...)
}

// ErrorWithCtx 错误级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func ErrorWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)

	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		_ = ExecuteCustomHooksWithCtx(ctx, zapcore.ErrorLevel, msg, allFields...)
	}

	getDefaultLogger().Error(msg, allFields...)
}

// PanicWithCtx panic 级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func PanicWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)

	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		_ = ExecuteCustomHooksWithCtx(ctx, zapcore.PanicLevel, msg, allFields...)
	}

	getDefaultLogger().Panic(msg, allFields...)
}

// FatalWithCtx fatal 级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
// 自动执行 customHooksWithCtx（如 SLS 日志上报）
func FatalWithCtx(ctx context.Context, msg string, fields ...Field) {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)

	// 执行自定义 Hook（如 SLS 上报）
	if len(customHooksWithCtx) > 0 {
		_ = ExecuteCustomHooksWithCtx(ctx, zapcore.FatalLevel, msg, allFields...)
	}

	getDefaultLogger().Fatal(msg, allFields...)
}

// IsDebugEnabled 检查是否启用了 DEBUG 级别日志
// 用于性能敏感场景，避免不必要的字符串拼接或对象创建
func IsDebugEnabled() bool {
	return getDefaultLogger().Core().Enabled(zapcore.DebugLevel)
}

// IsInfoEnabled 检查是否启用了 INFO 级别日志
func IsInfoEnabled() bool {
	return getDefaultLogger().Core().Enabled(zapcore.InfoLevel)
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
//
//	ctx := context.WithValue(context.Background(), logger.ContextKeyForRequestID(), "12345")
//	logger.ExecuteCustomHooksWithCtx(ctx, zapcore.InfoLevel, "user login", logger.String("user_id", "123"))
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
			PC:      pc,
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
