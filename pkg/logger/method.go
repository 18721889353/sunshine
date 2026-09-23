package logger

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap/zapcore"
)

// ==================== 运行时指标 ====================

// Metrics 日志系统运行时指标快照
// 可对接 Prometheus / 自定义监控采集
type Metrics struct {
	// DroppedEntries 异步缓冲区满时丢弃的日志条数
	DroppedEntries int64
	// RouterLoggerCount 路由器中活跃的 logger 数量
	RouterLoggerCount int
}

var droppedEntries int64 // 原子计数器：缓冲区满导致的丢弃

// RecordDrop 记录一次日志丢弃（由自定义 WriteSyncer wrapper 调用）
// 用法: 实现自定义 zapcore.WriteSyncer，在 Write 返回 error 时调用 logger.RecordDrop()
func RecordDrop() {
	atomic.AddInt64(&droppedEntries, 1)
}

// GetMetrics 获取日志系统运行时指标快照
// 用于 Prometheus exporter 或 /metrics 端点暴露
func GetMetrics() Metrics {
	m := Metrics{
		DroppedEntries: atomic.LoadInt64(&droppedEntries),
	}
	if globalRouter != nil {
		globalRouter.mu.RLock()
		m.RouterLoggerCount = len(globalRouter.loggers)
		globalRouter.mu.RUnlock()
	}
	return m
}

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

// Sync 刷新所有缓冲的日志条目，应用退出前应调用此方法确保日志完整写入
func Sync() error {
	// sugaredLogger 与 defaultLogger 共享底层 writer，只需 Sync 一次
	if defaultLogger == nil {
		return nil
	}
	if syncErr := defaultLogger.Sync(); syncErr != nil && !strings.Contains(syncErr.Error(), "stdout") {
		return syncErr
	}
	return nil
}

// Closer 可关闭资源的接口，用于 Shutdown 时统一清理
type Closer interface {
	Close() error
}

var (
	closerMu sync.Mutex
	closers  []Closer
)

// RegisterCloser 注册一个需要在 Shutdown 时关闭的资源
// 典型用法: logger.RegisterCloser(slsHook)
func RegisterCloser(c Closer) {
	closerMu.Lock()
	closers = append(closers, c)
	closerMu.Unlock()
}

// Shutdown 优雅关闭日志系统，确保所有缓冲数据写入完成
// 关闭顺序: 已注册的 Closer → 路由器 → 默认 logger
// 推荐在应用 main 函数的 defer 中调用:
//
//	defer logger.Shutdown(context.Background())
func Shutdown(_ context.Context) error {
	var firstErr error

	// 1. 关闭所有已注册的资源（如 SLS Hook）
	closerMu.Lock()
	registeredClosers := make([]Closer, len(closers))
	copy(registeredClosers, closers)
	closers = nil
	closerMu.Unlock()

	for _, c := range registeredClosers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// 2. 同步路由器中的所有 logger
	if err := RouterSync(); err != nil && firstErr == nil {
		firstErr = err
	}

	// 3. 同步默认 logger
	if err := Sync(); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

// logWithCtx 提取 context 字段并执行自定义 Hook，返回最终字段列表
func logWithCtx(ctx context.Context, level zapcore.Level, msg string, fields ...Field) []Field {
	ctxFields := extractContextFields(ctx)
	allFields := append(ctxFields, fields...)
	if len(customHooksWithCtx) > 0 {
		// Hook 错误静默丢弃：日志系统的错误不应阻塞或拖慢业务逻辑
		executeHooksIgnoreError(ctx, level, msg, allFields)
	}
	return allFields
}

// executeHooksIgnoreError 执行自定义 Hook，忽略错误
// 日志系统的内部错误不应影响主日志写入流程
func executeHooksIgnoreError(ctx context.Context, level zapcore.Level, msg string, fields []Field) {
	if err := ExecuteCustomHooksWithCtx(ctx, level, msg, fields...); err != nil {
		return // Hook 错误不阻塞业务，静默丢弃
	}
}

// ==================== 强制使用 Context 的日志方法 ====================

// DebugWithCtx 调试级别日志（必须提供 context）
// 自动从 context 中提取 request_id、trace_id 等链路追踪信息
func DebugWithCtx(ctx context.Context, msg string, fields ...Field) {
	getDefaultLogger().Debug(msg, logWithCtx(ctx, zapcore.DebugLevel, msg, fields...)...)
}

// InfoWithCtx 信息级别日志（必须提供 context）
func InfoWithCtx(ctx context.Context, msg string, fields ...Field) {
	getDefaultLogger().Info(msg, logWithCtx(ctx, zapcore.InfoLevel, msg, fields...)...)
}

// WarnWithCtx 警告级别日志（必须提供 context）
func WarnWithCtx(ctx context.Context, msg string, fields ...Field) {
	getDefaultLogger().Warn(msg, logWithCtx(ctx, zapcore.WarnLevel, msg, fields...)...)
}

// ErrorWithCtx 错误级别日志（必须提供 context）
func ErrorWithCtx(ctx context.Context, msg string, fields ...Field) {
	getDefaultLogger().Error(msg, logWithCtx(ctx, zapcore.ErrorLevel, msg, fields...)...)
}

// PanicWithCtx panic 级别日志（必须提供 context）
func PanicWithCtx(ctx context.Context, msg string, fields ...Field) {
	getDefaultLogger().Panic(msg, logWithCtx(ctx, zapcore.PanicLevel, msg, fields...)...)
}

// FatalWithCtx fatal 级别日志（必须提供 context）
func FatalWithCtx(ctx context.Context, msg string, fields ...Field) {
	getDefaultLogger().Fatal(msg, logWithCtx(ctx, zapcore.FatalLevel, msg, fields...)...)
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
