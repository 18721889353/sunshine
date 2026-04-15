// Package logger 是封装在 https://github.com/uber-go/zap 中的日志库
//
// 支持终端打印和日志保存
// 支持自动日志文件切割
// 支持 JSON 格式和控制台日志格式输出
// 支持 Debug, Info, Warn, Error, Panic, Fatal，也支持类似 fmt.Printf 的日志打印，Debugf, Infof, Warnf, Errorf, Panicf, Fatalf
package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

var defaultLogger *zap.Logger
var defaultSugaredLogger *zap.SugaredLogger
var customHooks []CustomHook
var customHooksWithCtx []CustomHookWithCtx

func getLogger() *zap.Logger {
	checkNil()
	return defaultLogger.WithOptions(zap.AddCallerSkip(1))
}

func getSugaredLogger() *zap.SugaredLogger {
	checkNil()
	return defaultSugaredLogger.WithOptions(zap.AddCallerSkip(1))
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
	defaultSugaredLogger = defaultLogger.Sugar()

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

		if err := InitRouter(defaultLogger, o.routes, defaultRouteConfig); err != nil {
			WarnWithCtx(initCtx, "failed to init log router", Err(err))
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
		_ = zapLog.Sync()
	}

	return defaultLogger, err
}

func log2Terminal(levelName string, encoding string, isAsync bool, asyncBufferSize int, asyncFlushInterval time.Duration) (*zap.Logger, error) {
	js := fmt.Sprintf(`{
      		"level": "%s",
            "encoding": "%s",
      		"outputPaths": ["stdout"],
            "errorOutputPaths": ["stdout"]
		}`, levelName, encoding)

	var config zap.Config
	err := json.Unmarshal([]byte(js), &config)
	if err != nil {
		return nil, err
	}

	config.EncoderConfig = zap.NewProductionEncoderConfig()
	if encoding == formatConsole {
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder // 日志颜色
	} else {
		config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // 日志级别在日志文件中使用大写字母
	}
	config.EncoderConfig.EncodeTime = timeFormatter // 默认时间格式

	// 根据格式创建编码器
	encoder := zapcore.NewConsoleEncoder(config.EncoderConfig)
	if encoding == formatJSON {
		encoder = zapcore.NewJSONEncoder(config.EncoderConfig)
	}

	// 为标准输出创建写同步器
	writeSyncer := zapcore.Lock(zapcore.AddSync(os.Stdout))

	// 如果启用异步，则用缓冲写同步器包装写同步器
	if isAsync {
		bufferedSyncer := &zapcore.BufferedWriteSyncer{
			WS:            writeSyncer,
			Size:          asyncBufferSize,    // 使用配置的缓冲区大小
			FlushInterval: asyncFlushInterval, // 使用配置的刷新间隔
		}
		// 使用缓冲同步器创建核心
		core := zapcore.NewCore(encoder, bufferedSyncer, getLevelSize(levelName))

		// 如果有自定义钩子，则包装核心
		if len(customHooks) > 0 {
			core = &customHookCore{
				Core: core,
			}
		}

		return zap.New(core, zap.AddCaller()), nil
	}

	// 使用原始写同步器创建核心
	core := zapcore.NewCore(encoder, writeSyncer, getLevelSize(levelName))

	// 如果有自定义钩子，则包装核心
	if len(customHooks) > 0 {
		core = &customHookCore{
			Core: core,
		}
	}

	return zap.New(core, zap.AddCaller()), nil
}

func log2File(encoding string, levelName string, fo *fileOptions, isAsync bool, asyncBufferSize int, asyncFlushInterval time.Duration) *zap.Logger {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = timeFormatter                // 修改时间编码器
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // 日志级别在日志文件中使用大写字母
	var encoder zapcore.Encoder
	if encoding == formatConsole { // 控制台格式
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else { // JSON 格式
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}
	var ws zapcore.WriteSyncer
	if fo.isSaveDay {
		logWriter, err := rotatelogs.New(
			fo.filename+".%Y%m%d",                                        // 带日期格式的日志文件名
			rotatelogs.WithLinkName(fo.filename),                         // 符号链接名
			rotatelogs.WithMaxAge(time.Duration(fo.maxAge)*24*time.Hour), // 日志文件最大保留时间
			rotatelogs.WithRotationTime(24*time.Hour),                    // 每天轮转
		)
		if err != nil {
			panic(err)
		}
		ws = zapcore.AddSync(logWriter)
	} else {
		// lumberjack配置
		lumberjackLogger := &lumberjack.Logger{
			Filename:   fo.filename,      // 文件名
			MaxSize:    fo.maxSize,       // 最大文件大小（MB）
			MaxBackups: fo.maxBackups,    // 旧文件最大数量
			MaxAge:     fo.maxAge,        // 旧文档最大天数
			Compress:   fo.isCompression, // 是否压缩和归档旧文件
		}

		// 如果设置了使用本地时间，则设置lumberjack的LocalTime选项
		if fo.isLocalTime {
			lumberjackLogger.LocalTime = true
		}
		ws = zapcore.AddSync(lumberjackLogger)
	}
	if fo.noPrint {
		// 使用自定义的 NopWriteSyncer（禁止终端/文件输出）
		ws = nopWriteSyncer{}
	}

	// 如果启用异步，则用缓冲写同步器包装写同步器
	if isAsync {
		// 为异步操作创建缓冲写同步器
		ws = &zapcore.BufferedWriteSyncer{
			WS:            ws,
			Size:          asyncBufferSize,    // 使用配置的缓冲区大小
			FlushInterval: asyncFlushInterval, // 使用配置的刷新间隔
		}
	}

	core := zapcore.NewCore(encoder, ws, getLevelSize(levelName))

	// 如果有自定义钩子，则包装核心
	if len(customHooks) > 0 {
		core = &customHookCore{
			Core: core,
		}
	}

	// 添加函数调用信息日志到日志中
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

// Check 确定是否应该记录提供的条目
func (c *customHookCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// Write 将条目和字段写入底层写入器
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
	if defaultLogger == nil {
		// 如果 Logger 未初始化，自动初始化为终端输出（不写文件，使用 console 格式便于阅读）
		// 注意：这是兜底逻辑，正式的 Init() 调用会覆盖此配置
		_, err := Init(WithSave(false), WithFormat(formatConsole))
		if err != nil {
			panic(err)
		}
	}
}
