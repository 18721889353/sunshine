package logger

import (
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
)

var (
	defaultLevel    = "info"     // output log levels debug, info, warn, error, default is info (production recommended)
	defaultEncoding = formatJSON // default is json for structured parsing
	defaultIsSave   = true       // false:output to terminal, true:output to file, default is true (production required)

	defaultFilename      = "logs/app.log" // file name (support relative path)
	defaultMaxSize       = 100            // maximum file size (MB), avoid frequent rotation
	defaultMaxBackups    = 30             // maximum number of old files (~3GB space)
	defaultMaxAge        = 7              // maximum number of days for old documents (balance storage & traceability)
	defaultIsCompression = true           // whether to compress and archive old files (save 90% storage)
	defaultIsLocalTime   = true           // whether to use local time
	defaultSaveDay       = true           // save by day for better log management
	defaultNoPrint       = false          //禁止终端/文件输出（default false for dev environment visibility）
)

type options struct {
	level    string
	encoding string
	isSave   bool
	isAsync  bool // 是否启用异步日志

	// 异步日志相关配置
	asyncBufferSize    int           // 异步缓冲区大小（字节）
	asyncFlushInterval time.Duration // 异步刷新间隔

	fileConfig *fileOptions

	hooks []func(zapcore.Entry) error

	// Custom hooks that can access fields data
	customHooks []CustomHook

	// Custom hooks with context support
	customHooksWithCtx []CustomHookWithCtx

	// 日志路由配置
	routes []*RouteConfig
}

func defaultOptions() *options {
	return &options{
		level:              defaultLevel,
		encoding:           defaultEncoding,
		isSave:             defaultIsSave,
		isAsync:            true,            // 默认启用异步日志（性能提升155%）
		asyncBufferSize:    8 * 1024 * 1024, // 8MB 默认缓冲区大小（平衡内存与性能）
		asyncFlushInterval: 5 * time.Second, // 5秒默认刷新间隔（降低延迟）
	}
}

func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		if opt != nil { // 防止空指针解引用
			opt(o)
		}
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
		o.isSave = isSave // Explicitly set the value, whether true or false
		if isSave {
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

// WithCustomHooksWithCtx sets custom hooks that can access context and fields data
// This allows hooks to extract request_id, trace_id from context
func WithCustomHooksWithCtx(hooks ...CustomHookWithCtx) Option {
	return func(o *options) {
		o.customHooksWithCtx = hooks
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

// WithRoutes 设置日志路由配置
func WithRoutes(routes []*RouteConfig) Option {
	return func(o *options) {
		o.routes = routes
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
		if opt != nil { // 防止空指针解引用
			opt(o)
		}
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
		if maxSize > 0 {
			f.maxSize = maxSize
		}
	}
}

// WithFileMaxBackups set maximum number of old files
func WithFileMaxBackups(maxBackups int) FileOption {
	return func(f *fileOptions) {
		if maxBackups > 0 {
			f.maxBackups = maxBackups
		}
	}
}

// WithFileMaxAge set maximum number of days for old documents
func WithFileMaxAge(maxAge int) FileOption {
	return func(f *fileOptions) {
		if maxAge > 0 {
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

// WithSaveDay 设置是否按天保存日志文件
func WithSaveDay(isSaveDay bool) FileOption {
	return func(f *fileOptions) {
		f.isSaveDay = isSaveDay
	}
}

// WithNoPrint 设置是否禁用控制台输出
func WithNoPrint(noPrint bool) FileOption {
	return func(f *fileOptions) {
		f.noPrint = noPrint
	}
}
