// Package logger is log library encapsulated in https://github.com/uber-go/zap
//
// Support for terminal printing and log saving.
// Support for automatic log file cutting.
// Support for json format and console log format output.
// Supports Debug, Info, Warn, Error, Panic, Fatal, also supports fmt.Printf-like log printing, Debugf, Infof, Warnf, Errorf, Panicf, Fatalf.
package logger

import (
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

var syncOnce sync.Once
var defaultLogger *zap.Logger
var defaultSugaredLogger *zap.SugaredLogger

func getLogger() *zap.Logger {
	checkNil()
	return defaultLogger.WithOptions(zap.AddCallerSkip(1))
}

func getSugaredLogger() *zap.SugaredLogger {
	checkNil()
	return defaultSugaredLogger.WithOptions(zap.AddCallerSkip(1))
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

	if strings.ToUpper(levelName) == "DEBUG" {
		// 启动定时刷新 goroutine
		startLogSyncTicker()
	}
	return defaultLogger, err
}

// 定义定时刷新函数
func startLogSyncTicker() {
	syncOnce.Do(func() {
		ticker := time.NewTicker(1 * time.Second)
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					_ = Sync()
				}
			}
		}()
	})
}

//	func log2Terminal(levelName string, encoding string) (*zap.Logger, error) {
//		js := fmt.Sprintf(`{
//	     		"level": "%s",
//	           "encoding": "%s",
//	     		"outputPaths": ["stdout"],
//	           "errorOutputPaths": ["stdout"]
//			}`, levelName, encoding)
//
//		var config zap.Config
//		err := json.Unmarshal([]byte(js), &config)
//		if err != nil {
//			return nil, err
//		}
//
//		config.EncoderConfig = zap.NewProductionEncoderConfig()
//		if encoding == formatConsole {
//			config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder // logging color
//		} else {
//			config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // logging levels in the log file using upper case letters
//		}
//		config.EncoderConfig.EncodeTime = timeFormatter // default time format
//		return config.Build()
//	}
type nopWriteSyncer struct{}

func (nopWriteSyncer) Write(p []byte) (n int, err error) {
	return len(p), nil // 模拟写入成功，但不执行实际操作
}

func (nopWriteSyncer) Sync() error {
	return nil // 无需同步
}
func log2Terminal(levelName string, encoding string) (*zap.Logger, error) {
	// 直接构建 EncoderConfig（避免 JSON 解析）
	encoderConfig := zap.NewProductionEncoderConfig()
	if encoding == formatConsole {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	}
	encoderConfig.EncodeTime = timeFormatter
	var encoder zapcore.Encoder
	if encoding == formatConsole { // console format
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else { // json format
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}
	var ws zapcore.WriteSyncer
	if strings.ToUpper(levelName) == "DEBUG" {
		//创建终端 WriteSyncer 并启用缓冲
		ws = zapcore.Lock(os.Stdout) // 锁定标准输出
		ws = &zapcore.BufferedWriteSyncer{
			WS:   ws,
			Size: 1024 * 1024, // 缓冲区大小：1024KB
		}
	} else {
		// 使用自定义的 NopWriteSyncer（禁止终端/文件输出）
		ws = nopWriteSyncer{}
	}

	// 构建 Core 并禁用调用栈追踪（AddCaller）
	core := zapcore.NewCore(
		encoder,
		ws,
		getLevelSize(levelName),
	)
	return zap.New(core, zap.AddCaller()), nil
}

func log2File(encoding string, levelName string, fo *fileOptions) *zap.Logger {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = timeFormatter                // zapcore.ISO8601TimeEncoder   // modify Time Encoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // logging levels in the log file using upper case letters
	var encoder zapcore.Encoder
	if encoding == formatConsole { // console format
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else { // json format
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}
	var ws zapcore.WriteSyncer
	if strings.ToUpper(levelName) == "DEBUG" {
		if fo.isSaveDay {
			logWriter, err := rotatelogs.New(
				fo.filename+".%Y%m%d",                // Log file name with date format
				rotatelogs.WithLinkName(fo.filename), // Symlink name
				// WithMaxAge和WithRotationCount二者只能设置一个，
				// WithMaxAge设置文件清理前的最长保存时间，
				// WithRotationCount设置文件清理前最多保存的个数。
				rotatelogs.WithMaxAge(time.Duration(fo.maxAge)*24*time.Hour), // Maximum age of log files
				rotatelogs.WithRotationTime(24*time.Hour),                    //WithRotationTime设置日志分割的时间，这里设置为一小时分割一次
			)
			if err != nil {
				panic(err)
			}
			ws = zapcore.AddSync(logWriter)
		} else {
			ws = zapcore.AddSync(&lumberjack.Logger{
				Filename:   fo.filename,      // file name
				MaxSize:    fo.maxSize,       // maximum file size (MB)
				MaxBackups: fo.maxBackups,    // maximum number of old files
				MaxAge:     fo.maxAge,        // maximum number of days for old documents
				Compress:   fo.isCompression, // whether to compress and archive old files
			})
		}

		// 添加缓冲层
		ws = &zapcore.BufferedWriteSyncer{
			WS:   ws,
			Size: 1024 * 1024,
		}
	} else {
		// 使用自定义的 NopWriteSyncer（禁止终端/文件输出）
		ws = nopWriteSyncer{}
	}

	core := zapcore.NewCore(encoder, ws, getLevelSize(levelName))

	// add the function call information log to the log.
	return zap.New(core, zap.AddCaller())
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
	enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
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
