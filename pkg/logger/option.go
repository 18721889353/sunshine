package logger

import (
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
)

var (
	defaultLevel    = "debug" // output log levels debug, info, warn, error, default is debug
	defaultEncoding = formatConsole
	defaultIsSave   = false // false:output to terminal, true:output to file, default is false

	defaultFilename      = "out.log" // file name
	defaultMaxSize       = 10        // maximum file size (MB)
	defaultMaxBackups    = 100       // maximum number of old files
	defaultMaxAge        = 30        // maximum number of days for old documents
	defaultIsCompression = false     // whether to compress and archive old files
	defaultIsLocalTime   = true      // whether to use local time
	defaultSaveDay       = false
	defaultNoPrint       = false //禁止终端/文件输出）
)

// customHookWrapper wraps CustomHook to implement zap's Hook interface
type customHookWrapper struct {
	hook CustomHook
}

// Execute executes the custom hook with level, message and fields
func (w customHookWrapper) Execute(entry zapcore.Entry, fields []Field) error {
	return w.hook(entry, fields)
}

type options struct {
	level    string
	encoding string
	isSave   bool
	isAsync  bool // 是否启用异步日志

	// 异步日志相关配置
	asyncBufferSize      int           // 异步缓冲区大小（字节）
	asyncFlushInterval   time.Duration // 异步刷新间隔

	fileConfig *fileOptions

	hooks []func(zapcore.Entry) error

	// Custom hooks that can access fields data
	customHooks []CustomHook
}

func defaultOptions() *options {
	return &options{
		level:    defaultLevel,
		encoding: defaultEncoding,
		isSave:   defaultIsSave,
		isAsync:  false, // 默认不启用异步日志
		asyncBufferSize:    512 * 1024,      // 512KB 默认缓冲区大小
		asyncFlushInterval: 30 * time.Second, // 30秒默认刷新间隔
	}
}

func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// Option set the logger options.
type Option func(*options)

// WithLevel setting the log level
func WithLevel(levelName string) Option {
	return func(o *options) {
		levelName = strings.ToUpper(levelName)
		switch levelName {
		case levelDebug, levelInfo, levelWarn, levelError:
			o.level = levelName
		default:
			o.level = levelDebug
		}
	}
}

// WithFormat set the output log format, console or json
func WithFormat(format string) Option {
	return func(o *options) {
		o.encoding = strings.ToLower(format)
		if o.encoding != formatJSON && o.encoding != formatConsole {
			o.encoding = defaultEncoding
		}
	}
}

// WithSave save log to file
func WithSave(isSave bool, opts ...FileOption) Option {
	return func(o *options) {
		if isSave {
			o.isSave = true
			fo := defaultFileOptions()
			fo.apply(opts...)
			o.fileConfig = fo
		}
	}
}

// WithHooks set the log hooks
func WithHooks(hooks ...func(zapcore.Entry) error) Option {
	return func(o *options) {
		o.hooks = hooks
	}
}

// WithCustomHooks sets custom hooks that can access fields data
func WithCustomHooks(hooks ...CustomHook) Option {
	return func(o *options) {
		o.customHooks = hooks
	}
}

// WithAsync enables asynchronous logging
func WithAsync(enabled bool) Option {
	return func(o *options) {
		o.isAsync = enabled
	}
}

// WithAsyncBufferSize sets the buffer size for asynchronous logging (in bytes)
func WithAsyncBufferSize(size int) Option {
	return func(o *options) {
		o.asyncBufferSize = size
	}
}

// WithAsyncFlushInterval sets the flush interval for asynchronous logging
func WithAsyncFlushInterval(interval time.Duration) Option {
	return func(o *options) {
		o.asyncFlushInterval = interval
	}
}

// ------------------------------------------------------------------------------------------

type fileOptions struct {
	filename      string
	maxSize       int
	maxBackups    int
	maxAge        int
	isCompression bool
	isLocalTime   bool
	isSaveDay     bool
	noPrint       bool
}

func defaultFileOptions() *fileOptions {
	return &fileOptions{
		filename:      defaultFilename,
		maxSize:       defaultMaxSize,
		maxBackups:    defaultMaxBackups,
		maxAge:        defaultMaxAge,
		isCompression: defaultIsCompression,
		isLocalTime:   defaultIsLocalTime,
		isSaveDay:     defaultSaveDay,
		noPrint:       defaultNoPrint,
	}
}

func (o *fileOptions) apply(opts ...FileOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// FileOption set the file options.
type FileOption func(*fileOptions)

// WithFileName set log filename
func WithFileName(filename string) FileOption {
	return func(f *fileOptions) {
		if filename != "" {
			f.filename = filename
		}
	}
}

// WithFileMaxSize set maximum file size (MB)
func WithFileMaxSize(maxSize int) FileOption {
	return func(f *fileOptions) {
		if f.maxSize > 0 {
			f.maxSize = maxSize
		}
	}
}

// WithFileMaxBackups set maximum number of old files
func WithFileMaxBackups(maxBackups int) FileOption {
	return func(f *fileOptions) {
		if f.maxBackups > 0 {
			f.maxBackups = maxBackups
		}
	}
}

// WithFileMaxAge set maximum number of days for old documents
func WithFileMaxAge(maxAge int) FileOption {
	return func(f *fileOptions) {
		if f.maxAge > 0 {
			f.maxAge = maxAge
		}
	}
}

// WithFileIsCompression set whether to compress log files
func WithFileIsCompression(isCompression bool) FileOption {
	return func(f *fileOptions) {
		f.isCompression = isCompression
	}
}

// WithLocalTime set whether to use local time
func WithLocalTime(isLocalTime bool) FileOption {
	return func(f *fileOptions) {
		f.isLocalTime = isLocalTime
	}
}

func WithSaveDay(isSaveDay bool) FileOption {
	return func(f *fileOptions) {
		f.isSaveDay = isSaveDay
	}
}

func WithNoPrint(noPrint bool) FileOption {
	return func(f *fileOptions) {
		f.noPrint = noPrint
	}
}