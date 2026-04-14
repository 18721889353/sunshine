package logger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/natefinch/lumberjack"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LogRouter 日志路由器 - 支持按模块/级别动态路由到不同文件
type LogRouter struct {
	mu            sync.RWMutex
	loggers       map[string]*zap.Logger
	configs       map[string]*RouteConfig
	defaultLogger *zap.Logger
}

// RouteConfig 路由配置
type RouteConfig struct {
	Module        string
	Filename      string // 支持完整路径或相对路径 (如: "/var/log/order.log" 或 "order.log")
	Level         string
	MaxSize       int
	MaxBackups    int
	MaxAge        int
	IsCompression bool
	IsSaveDay     bool
	Format        string
	IsAsync       bool
}

var (
	globalRouter *LogRouter
	routerOnce   sync.Once
)

// InitRouter 初始化全局日志路由器
// defaultConfig: 默认配置,路由配置中未指定的字段将继承此配置
func InitRouter(defaultLog *zap.Logger, routes []*RouteConfig, defaultConfig *RouteConfig) error {
	var initErr error
	routerOnce.Do(func() {
		router := &LogRouter{
			loggers:       make(map[string]*zap.Logger),
			configs:       make(map[string]*RouteConfig),
			defaultLogger: defaultLog,
		}

		for _, route := range routes {
			// 合并默认配置
			mergedConfig := mergeRouteConfig(route, defaultConfig)
			if err := router.RegisterRoute(mergedConfig); err != nil {
				initErr = fmt.Errorf("failed to register route for module %s: %w", route.Module, err)
				return
			}
		}

		globalRouter = router
	})

	return initErr
}

// RegisterRoute 注册一个日志路由
func (r *LogRouter) RegisterRoute(config *RouteConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := r.getRouteKey(config)

	if oldLogger, exists := r.loggers[key]; exists {
		_ = oldLogger.Sync()
	}

	logger, err := r.createLogger(config)
	if err != nil {
		return err
	}

	r.loggers[key] = logger
	r.configs[key] = config

	return nil
}

// GetLogger 根据模块名获取对应的 logger
func GetLogger(module string) *zap.Logger {
	if globalRouter == nil {
		return Get()
	}

	logger := globalRouter.getLoggerByModule(module)
	if logger != nil {
		return logger
	}

	return Get()
}

// GetLoggerByLevel 根据日志级别获取对应的 logger
func GetLoggerByLevel(level string) *zap.Logger {
	if globalRouter == nil {
		return Get()
	}

	logger := globalRouter.getLoggerByLevel(level)
	if logger != nil {
		return logger
	}

	return Get()
}

// getLoggerByModule 内部方法:根据模块获取 logger
func (r *LogRouter) getLoggerByModule(module string) *zap.Logger {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if logger, exists := r.loggers[module]; exists {
		return logger
	}

	for key, logger := range r.loggers {
		if strings.HasPrefix(module, key+".") {
			return logger
		}
	}

	return nil
}

// getLoggerByLevel 内部方法:根据级别获取 logger
func (r *LogRouter) getLoggerByLevel(level string) *zap.Logger {
	r.mu.RLock()
	defer r.mu.RUnlock()

	levelKey := "level_" + strings.ToUpper(level)
	if logger, exists := r.loggers[levelKey]; exists {
		return logger
	}

	return nil
}

// getRouteKey 生成路由键
func (r *LogRouter) getRouteKey(config *RouteConfig) string {
	if config.Module != "" {
		return config.Module
	}
	if config.Level != "" {
		return "level_" + strings.ToUpper(config.Level)
	}
	return "default"
}

// mergeRouteConfig 合并路由配置,未指定的字段继承默认配置
func mergeRouteConfig(route, defaultConfig *RouteConfig) *RouteConfig {
	if defaultConfig == nil {
		return route
	}

	merged := &RouteConfig{
		Module:   route.Module,
		Filename: route.Filename,
		Level:    route.Level,
	}

	// 数值类型:如果路由配置为0,则使用默认配置
	if route.MaxSize == 0 && defaultConfig.MaxSize > 0 {
		merged.MaxSize = defaultConfig.MaxSize
	} else {
		merged.MaxSize = route.MaxSize
	}

	if route.MaxBackups == 0 && defaultConfig.MaxBackups > 0 {
		merged.MaxBackups = defaultConfig.MaxBackups
	} else {
		merged.MaxBackups = route.MaxBackups
	}

	if route.MaxAge == 0 && defaultConfig.MaxAge > 0 {
		merged.MaxAge = defaultConfig.MaxAge
	} else {
		merged.MaxAge = route.MaxAge
	}

	// 布尔值和字符串:路由配置优先 (因为无法区分"未设置"和"显式设置为false/空")
	// 如果需要继承默认值,YAML 中不要写该字段即可
	merged.IsCompression = route.IsCompression
	merged.IsSaveDay = route.IsSaveDay
	merged.IsAsync = route.IsAsync

	// 字符串:如果路由配置为空,则使用默认配置
	if route.Format == "" && defaultConfig.Format != "" {
		merged.Format = defaultConfig.Format
	} else {
		merged.Format = route.Format
	}

	return merged
}

// createLogger 根据配置创建 zap logger
func (r *LogRouter) createLogger(config *RouteConfig) (*zap.Logger, error) {
	logFilePath := buildLogFilePath(config.Filename)

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = timeFormatter
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	var encoder zapcore.Encoder
	if strings.ToLower(config.Format) == formatConsole {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	var ws zapcore.WriteSyncer
	if config.IsSaveDay {
		logWriter, err := rotatelogs.New(
			logFilePath+".%Y%m%d",
			rotatelogs.WithLinkName(logFilePath),
			rotatelogs.WithMaxAge(time.Duration(config.MaxAge)*24*time.Hour),
			rotatelogs.WithRotationTime(24*time.Hour),
		)
		if err != nil {
			return nil, err
		}
		ws = zapcore.AddSync(logWriter)
	} else {
		lumberjackLogger := &lumberjack.Logger{
			Filename:   logFilePath,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.IsCompression,
		}
		ws = zapcore.AddSync(lumberjackLogger)
	}

	level := getLevelSize(config.Level)
	core := zapcore.NewCore(encoder, ws, level)

	return zap.New(core, zap.AddCaller()), nil
}

// Close 关闭所有 logger
func (r *LogRouter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for key, logger := range r.loggers {
		if err := logger.Sync(); err != nil {
			lastErr = fmt.Errorf("failed to sync logger %s: %w", key, err)
		}
	}

	return lastErr
}

// RouterSync 同步全局路由器的所有 logger + 默认 logger
func RouterSync() error {
	var lastErr error

	// 1. 同步路由器中的所有 logger (如果已初始化)
	if globalRouter != nil {
		if err := globalRouter.Close(); err != nil {
			lastErr = err
		}
	}

	// 2. 同步默认的全局 logger
	if err := Sync(); err != nil {
		if lastErr != nil {
			// 如果之前已有错误，返回组合错误
			return fmt.Errorf("router sync error: %w, default logger sync error: %v", lastErr, err)
		}
		return err
	}

	return lastErr
}

// buildLogFilePath 构建日志文件路径并自动创建目录
// 支持绝对路径和相对路径
func buildLogFilePath(filename string) string {
	if filename == "" {
		return "out.log"
	}

	// 如果是绝对路径,直接返回并创建目录
	if filepath.IsAbs(filename) {
		dir := filepath.Dir(filename)
		ensureDirExists(dir)
		return filename
	}

	// 如果是相对路径,也需要创建目录
	if strings.Contains(filename, string(filepath.Separator)) {
		dir := filepath.Dir(filename)
		ensureDirExists(dir)
	}

	return filename
}

// ensureDirExists 确保目录存在
func ensureDirExists(dir string) {
	if dir == "" || dir == "." {
		return
	}

	if _, err := os.Stat(dir); err == nil {
		return
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(fmt.Sprintf("failed to create log directory '%s': %v", dir, err))
	}
}

// extractContextFields 从 context 中提取链路追踪字段
func extractContextFields(ctx context.Context) []Field {
	if ctx == nil {
		return nil
	}

	var fields []Field

	// 提取 request_id (从 context value)
	if reqID := getRequestIDFromCtx(ctx); reqID != "" {
		fields = append(fields, String("request_id", reqID))
	}

	// 提取 trace_id (OpenTelemetry)
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		// TraceID: 全局唯一的追踪标识
		if traceID := spanCtx.TraceID().String(); traceID != "00000000000000000000000000000000" {
			fields = append(fields, String("trace_id", traceID))
		}
	}

	return fields
}

// getRequestIDFromCtx 从 context 中获取 request_id
// 支持 gin middleware 和 gRPC interceptor 的 context key
func getRequestIDFromCtx(ctx context.Context) string {
	// 尝试常见的 request_id context keys（支持 string 和 contextKey 类型）
	commonKeys := []string{"request_id", "RequestID", "x-request-id", "X-Request-Id"}

	for _, key := range commonKeys {
		// 先尝试用 contextKey 类型
		if v := ctx.Value(contextKey(key)); v != nil {
			if reqID, ok := v.(string); ok && reqID != "" {
				return reqID
			}
		}
		// 再尝试用 string 类型（兼容 context.WithValue(ctx, "request_id", value) 的写法）
		if v := ctx.Value(key); v != nil {
			if reqID, ok := v.(string); ok && reqID != "" {
				return reqID
			}
		}
	}

	return ""
}

// contextKey context key 类型
type contextKey string

// getTraceIDFromCtx 从 context 中获取 trace_id
func getTraceIDFromCtx(ctx context.Context) string {
	// OpenTelemetry trace ID 通常存储在特定的 context key 中
	// 这里可以根据实际使用的 tracing 框架调整
	return ""
}
