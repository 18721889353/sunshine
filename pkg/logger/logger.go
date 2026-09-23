// Package logger 是封装在 https://github.com/uber-go/zap 中的日志库
//
// 支持终端打印和日志保存
// 支持自动日志文件切割
// 支持 JSON 格式和控制台日志格式输出
// 支持 Debug, Info, Warn, Error, Panic, Fatal，也支持类似 fmt.Printf 的日志打印，Debugf, Infof, Warnf, Errorf, Panicf, Fatalf
package logger

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	formatConsole = "console"
	formatJSON    = "json"

	levelDebug = "DEBUG"
	levelInfo  = "INFO"
	levelWarn  = "WARN"
	levelError = "ERROR"
)

var (
	defaultLogger       *zap.Logger
	defaultCallerLogger *zap.Logger // 预创建 skip=1 的 logger，避免每次调用分配
	customHooks         []CustomHook
	customHooksWithCtx  []CustomHookWithCtx
	initOnce            sync.Once // 保护 Init/checkNil 不被并发调用
)

// getDefaultLogger 返回预缓存的 skip=1 logger，避免每次日志调用都创建新实例
func getDefaultLogger() *zap.Logger {
	checkNil()
	return defaultCallerLogger
}

type nopWriteSyncer struct{}

func (nopWriteSyncer) Write(p []byte) (n int, err error) {
	return len(p), nil // 模拟写入成功，但不执行实际操作
}

func (nopWriteSyncer) Sync() error {
	return nil // 无需同步
}

// Init 初始化日志设置
// 在终端打印 debug 级别日志，示例: Init()
// 在终端打印 info 级别日志，示例: Init(WithLevel("info"))
// 在终端打印 json 格式，debug 级别日志，示例: Init(WithFormat("json"))
// 带钩子的日志，示例: Init(WithHooks(func(zapcore.Entry) error{return nil}))
// 输出日志到文件 out.log，使用默认切割日志相关参数，debug 级别日志，示例: Init(WithSave())
// 输出日志到指定文件，自定义设置日志文件切割参数，json 格式，debug 级别日志，示例:
// Init(
//
//	  WithFormat("json"),
//	  WithSave(true,
//
//			WithFileName("my.log"),
//			WithFileMaxSize(5),
//			WithFileMaxBackups(5),
//			WithFileMaxAge(10),
//			WithFileIsCompression(true),
//		))
func Init(opts ...Option) (*zap.Logger, error) {
	o := defaultOptions()
	o.apply(opts...)
	isSave := o.isSave
	levelName := o.level
	encoding := o.encoding
	isAsync := o.isAsync
	asyncBufferSize := o.asyncBufferSize
	asyncFlushInterval := o.asyncFlushInterval

	// 存储自定义钩子供自定义核心使用
	customHooks = o.customHooks
	customHooksWithCtx = o.customHooksWithCtx

	var err error
	var zapLog *zap.Logger
	if !isSave {
		zapLog, err = log2Terminal(levelName, encoding, isAsync, asyncBufferSize, asyncFlushInterval)
		if err != nil {
			panic(err)
		}
	} else {
		// 如果 isSave=true 但未配置 fileConfig，使用默认配置
		if o.fileConfig == nil {
			o.fileConfig = defaultFileOptions()
		}
		zapLog = log2File(encoding, levelName, o.fileConfig, isAsync, asyncBufferSize, asyncFlushInterval)
	}

	if len(o.hooks) > 0 {
		zapLog = zapLog.WithOptions(zap.Hooks(o.hooks...))
	}

	defaultLogger = zapLog
	defaultCallerLogger = defaultLogger.WithOptions(zap.AddCallerSkip(1))

	// 使用 context.Background() 因为此时还没有请求上下文
	initCtx := context.Background()

	// 自动初始化日志路由器 (如果配置了路由且开启了文件保存)
	if isSave && len(o.routes) > 0 {
		// 构建默认配置,供路由继承
		fileCfg := o.fileConfig
		if fileCfg == nil {
			fileCfg = defaultFileOptions()
		}
		defaultRouteConfig := &RouteConfig{
			MaxSize:       fileCfg.maxSize,
			MaxBackups:    fileCfg.maxBackups,
			MaxAge:        fileCfg.maxAge,
			IsCompression: fileCfg.isCompression,
			IsSaveDay:     fileCfg.isSaveDay,
			Format:        o.encoding,
			IsAsync:       o.isAsync,
		}

		if routerErr := InitRouter(defaultLogger, o.routes, defaultRouteConfig); routerErr != nil {
			WarnWithCtx(initCtx, "failed to init log router", Err(routerErr))
		} else {
			InfoWithCtx(initCtx, "[log router] was initialized", Int("routes_count", len(o.routes)))
		}
	} else if !isSave && len(o.routes) > 0 {
		// 如果未开启文件保存但配置了路由，记录一条提示，明确告知用户路由已忽略
		InfoWithCtx(initCtx, "[log router] skipped because 'isSave' is false")
	}

	// 强制同步一次，确保之前的 InfoWithCtx 写入完成
	// 注意：即使是终端输出，如果是异步模式也需要同步
	if isAsync {
		if syncErr := zapLog.Sync(); syncErr != nil {
			fmt.Printf("sync logger error: %v\n", syncErr)
		}
	}

	return defaultLogger, err
}

// buildEncoder 根据 encoding 类型构建 zapcore.Encoder
func buildEncoder(encoding string, encoderConfig zapcore.EncoderConfig) zapcore.Encoder {
	if encoding == formatConsole {
		return zapcore.NewConsoleEncoder(encoderConfig)
	}
	return zapcore.NewJSONEncoder(encoderConfig)
}

// buildWriteSyncer 根据文件配置构建 WriteSyncer
func buildWriteSyncer(fo *fileOptions) zapcore.WriteSyncer {
	var ws zapcore.WriteSyncer
	if fo.isSaveDay {
		logWriter, err := rotatelogs.New(
			fo.filename+".%Y%m%d",
			rotatelogs.WithLinkName(fo.filename),
			rotatelogs.WithMaxAge(time.Duration(fo.maxAge)*24*time.Hour),
			rotatelogs.WithRotationTime(24*time.Hour),
		)
		if err != nil {
			panic(err)
		}
		ws = zapcore.AddSync(logWriter)
	} else {
		lumberjackLogger := &lumberjack.Logger{
			Filename:   fo.filename,
			MaxSize:    fo.maxSize,
			MaxBackups: fo.maxBackups,
			MaxAge:     fo.maxAge,
			Compress:   fo.isCompression,
		}
		if fo.isLocalTime {
			lumberjackLogger.LocalTime = true
		}
		ws = zapcore.AddSync(lumberjackLogger)
	}
	if fo.noPrint {
		ws = nopWriteSyncer{}
	}
	return ws
}

// buildCore 组装最终的 zapcore.Core（含 async 包装 + 自定义 Hook）
func buildCore(encoder zapcore.Encoder, ws zapcore.WriteSyncer, level zapcore.Level, isAsync bool, asyncBufferSize int, asyncFlushInterval time.Duration) zapcore.Core {
	if isAsync {
		ws = &zapcore.BufferedWriteSyncer{
			WS:            ws,
			Size:          asyncBufferSize,
			FlushInterval: asyncFlushInterval,
		}
	}
	core := zapcore.NewCore(encoder, ws, level)
	if len(customHooks) > 0 {
		core = &customHookCore{Core: core}
	}
	return core
}

// log2Terminal 创建终端输出 logger
func log2Terminal(levelName string, encoding string, isAsync bool, asyncBufferSize int, asyncFlushInterval time.Duration) (*zap.Logger, error) {
	encoderConfig := zap.NewProductionEncoderConfig()
	if encoding == formatConsole {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	}
	encoderConfig.EncodeTime = timeFormatter

	encoder := buildEncoder(encoding, encoderConfig)
	ws := zapcore.Lock(zapcore.AddSync(os.Stdout))
	core := buildCore(encoder, ws, getLevelSize(levelName), isAsync, asyncBufferSize, asyncFlushInterval)
	return zap.New(core, zap.AddCaller()), nil
}

// log2File 创建文件输出 logger
func log2File(encoding string, levelName string, fo *fileOptions, isAsync bool, asyncBufferSize int, asyncFlushInterval time.Duration) *zap.Logger {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = timeFormatter
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	encoder := buildEncoder(encoding, encoderConfig)
	ws := buildWriteSyncer(fo)
	core := buildCore(encoder, ws, getLevelSize(levelName), isAsync, asyncBufferSize, asyncFlushInterval)
	return zap.New(core, zap.AddCaller())
}

// customHookCore 包装 zapcore.Core 并执行自定义钩子
type customHookCore struct {
	zapcore.Core
}

// With 向核心添加结构化上下文
func (c *customHookCore) With(fields []Field) zapcore.Core {
	return &customHookCore{
		Core: c.Core.With(fields),
	}
}

// Check 检查是否应该记录该日志条目
func (c *customHookCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// Write 将日志条目和字段写入底层输出
func (c *customHookCore) Write(ent zapcore.Entry, fields []Field) error {
	// 首先执行自定义钩子
	for _, hook := range customHooks {
		if err := hook(ent, fields); err != nil {
			return err
		}
	}

	// 然后写入底层核心
	return c.Core.Write(ent, fields)
}

// DEBUG(默认), INFO, WARN, ERROR
func getLevelSize(levelName string) zapcore.Level {
	levelName = strings.ToUpper(levelName)
	switch levelName {
	case levelDebug:
		return zapcore.DebugLevel
	case levelInfo:
		return zapcore.InfoLevel
	case levelWarn:
		return zapcore.WarnLevel
	case levelError:
		return zapcore.ErrorLevel
	}
	return zapcore.DebugLevel
}

func timeFormatter(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05.000000000"))
}

// GetWithSkip 获取 defaultLogger，设置跳过的调用者值，自定义显示的代码行数
func GetWithSkip(skip int) *zap.Logger {
	checkNil()
	return defaultLogger.WithOptions(zap.AddCallerSkip(skip))
}

// Get 获取日志记录器
func Get() *zap.Logger {
	checkNil()
	return defaultLogger
}

func checkNil() {
	initOnce.Do(func() {
		if defaultLogger == nil {
			// 如果 Logger 未初始化，自动初始化为终端输出（不写文件，使用 console 格式便于阅读）
			// 注意：这是兜底逻辑，正式的 Init() 调用会覆盖此配置
			_, err := Init(WithSave(false), WithFormat(formatConsole))
			if err != nil {
				panic(err)
			}
		}
	})
}
