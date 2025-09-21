// Package logger is log library encapsulated in https://github.com/uber-go/zap
//
// Support for terminal printing and log saving.
// Support for automatic log file cutting.
// Support for json format and console log format output.
// Supports Debug, Info, Warn, Error, Panic, Fatal, also supports fmt.Printf-like log printing, Debugf, Infof, Warnf, Errorf, Panicf, Fatalf.
package logger

import (
	"encoding/json"
	"fmt"
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

// Init initial log settings
// print the debug level log in the terminal, example: Init()
// print the info level log in the terminal, example: Init(WithLevel("info"))
// print the json format, debug level log in the terminal, example: Init(WithFormat("json"))
// log with hooks, example: Init(WithHooks(func(zapcore.Entry) error{return nil}))
// output the log to the file out.log, using the default cut log-related parameters, debug-level log, example: Init(WithSave())
// output the log to the specified file, custom set the log file cut log parameters, json format, debug level log, example:
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

	// Store custom hooks for use in our custom core
	customHooks = o.customHooks

	var err error
	var zapLog *zap.Logger
	var str string
	if !isSave {
		zapLog, err = log2Terminal(levelName, encoding)
		if err != nil {
			panic(err)
		}
		str = fmt.Sprintf("initialize logger finish, config is output to 'terminal', format=%s, level=%s", encoding, levelName)
	} else {
		zapLog = log2File(encoding, levelName, o.fileConfig)
		str = fmt.Sprintf("initialize logger finish, config is output to 'file', format=%s, level=%s, file=%s", encoding, levelName, o.fileConfig.filename)
	}

	if len(o.hooks) > 0 {
		zapLog = zapLog.WithOptions(zap.Hooks(o.hooks...))
	}

	defaultLogger = zapLog
	defaultSugaredLogger = defaultLogger.Sugar()
	Info(str)

	return defaultLogger, err
}

func log2Terminal(levelName string, encoding string) (*zap.Logger, error) {
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
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder // logging color
	} else {
		config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // logging levels in the log file using upper case letters
	}
	config.EncoderConfig.EncodeTime = timeFormatter // default time format

	// If we have custom hooks, we need to create a custom core
	if len(customHooks) > 0 {
		// Create the base core from the config
		baseCore, err := config.Build()
		if err != nil {
			return nil, err
		}

		// Get the underlying core to wrap it
		core := baseCore.Core()

		// Wrap the core with our custom hook support
		wrappedCore := &customHookCore{
			Core: core,
		}

		return zap.New(wrappedCore, zap.AddCaller()), nil
	}

	return config.Build()
}

func log2File(encoding string, levelName string, fo *fileOptions) *zap.Logger {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = timeFormatter                // modify Time Encoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // logging levels in the log file using upper case letters
	var encoder zapcore.Encoder
	if encoding == formatConsole { // console format
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else { // json format
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}
	var ws zapcore.WriteSyncer
	if fo.isSaveDay {
		logWriter, err := rotatelogs.New(
			fo.filename+".%Y%m%d",                                        // Log file name with date format
			rotatelogs.WithLinkName(fo.filename),                         // Symlink name
			rotatelogs.WithMaxAge(time.Duration(fo.maxAge)*24*time.Hour), // Maximum age of log files
			rotatelogs.WithRotationTime(24*time.Hour),                    // Rotate daily
		)
		if err != nil {
			panic(err)
		}
		ws = zapcore.AddSync(logWriter)
	} else {
		// lumberjack配置
		lumberjackLogger := &lumberjack.Logger{
			Filename:   fo.filename,      // file name
			MaxSize:    fo.maxSize,       // maximum file size (MB)
			MaxBackups: fo.maxBackups,    // maximum number of old files
			MaxAge:     fo.maxAge,        // maximum number of days for old documents
			Compress:   fo.isCompression, // whether to compress and archive old files
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

	core := zapcore.NewCore(encoder, ws, getLevelSize(levelName))

	// If we have custom hooks, wrap the core
	if len(customHooks) > 0 {
		core = &customHookCore{
			Core: core,
		}
	}

	// add the function call information log to the log.
	return zap.New(core, zap.AddCaller())
}

// customHookCore wraps a zapcore.Core and executes custom hooks
type customHookCore struct {
	zapcore.Core
}

// With adds structured context to the Core.
func (c *customHookCore) With(fields []Field) zapcore.Core {
	return &customHookCore{
		Core: c.Core.With(fields),
	}
}

// Check determines whether the supplied Entry should be logged.
func (c *customHookCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// Write writes the entry and fields to the underlying writer.
func (c *customHookCore) Write(ent zapcore.Entry, fields []Field) error {
	// Execute custom hooks first
	for _, hook := range customHooks {
		if err := hook(ent, fields); err != nil {
			return err
		}
	}

	// Then write to the underlying core
	return c.Core.Write(ent, fields)
}

// DEBUG(default), INFO, WARN, ERROR
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
	enc.AppendString(t.Format("2006-01-02 15:04:05.000000"))
}

// GetWithSkip get defaultLogger, set the skipped caller value, customize the number of lines of code displayed
func GetWithSkip(skip int) *zap.Logger {
	checkNil()
	return defaultLogger.WithOptions(zap.AddCallerSkip(skip))
}

// Get logger
func Get() *zap.Logger {
	checkNil()
	return defaultLogger
}

func checkNil() {
	if defaultLogger == nil {
		_, err := Init() // default output to console
		if err != nil {
			panic(err)
		}
	}
}
