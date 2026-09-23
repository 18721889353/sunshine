package logger

import (
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
)

var (
	defaultLevel    = "info"     // 日志级别：debug、info、warn、error，默认 info（生产环境推荐）
	defaultEncoding = formatJSON // 默认 json 格式，便于结构化解析
	defaultIsSave   = true       // false: 输出到终端，true: 输出到文件，默认 true（生产环境必须）

	defaultFilename      = "logs/app.log" // 日志文件名（支持相对路径）
	defaultMaxSize       = 100            // 最大文件大小（MB），避免频繁切割
	defaultMaxBackups    = 30             // 旧文件最大保留数量（约 3GB 空间）
	defaultMaxAge        = 7              // 旧文件最大保留天数（平衡存储与可追溯性）
	defaultIsCompression = true           // 是否压缩归档旧文件（节省 90% 存储）
	defaultIsLocalTime   = true           // 是否使用本地时间
	defaultSaveDay       = true           // 按天保存日志文件，便于日志管理
	defaultNoPrint       = false          // 禁止终端/文件输出（默认 false，开发环境可见）
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

	// 可访问字段数据的自定义钩子
	customHooks []CustomHook

	// 带 Context 支持的自定义钩子
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

// Option 日志配置选项函数类型
type Option func(*options)

// WithLevel 设置日志级别
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

// WithFormat 设置输出日志格式，console 或 json
func WithFormat(format string) Option {
	return func(o *options) {
		o.encoding = strings.ToLower(format)
		if o.encoding != formatJSON && o.encoding != formatConsole {
			o.encoding = defaultEncoding
		}
	}
}

// WithSave 设置是否保存日志到文件
func WithSave(isSave bool, opts ...FileOption) Option {
	return func(o *options) {
		o.isSave = isSave // 显式设置值，无论 true 或 false
		if isSave {
			fo := defaultFileOptions()
			fo.apply(opts...)
			o.fileConfig = fo
		}
	}
}

// WithHooks 设置日志钩子
func WithHooks(hooks ...func(zapcore.Entry) error) Option {
	return func(o *options) {
		o.hooks = hooks
	}
}

// WithCustomHooks 设置可访问字段数据的自定义钩子
func WithCustomHooks(hooks ...CustomHook) Option {
	return func(o *options) {
		o.customHooks = hooks
	}
}

// WithCustomHooksWithCtx 设置可访问 Context 和字段数据的自定义钩子
// 钩子可从 context 中提取 request_id、trace_id 等链路追踪信息
func WithCustomHooksWithCtx(hooks ...CustomHookWithCtx) Option {
	return func(o *options) {
		o.customHooksWithCtx = hooks
	}
}

// WithAsync 启用异步日志
func WithAsync(enabled bool) Option {
	return func(o *options) {
		o.isAsync = enabled
	}
}

// WithAsyncBufferSize 设置异步日志缓冲区大小（字节）
func WithAsyncBufferSize(size int) Option {
	return func(o *options) {
		o.asyncBufferSize = size
	}
}

// WithAsyncFlushInterval 设置异步日志刷新间隔
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

// FileOption 文件配置选项函数类型
type FileOption func(*fileOptions)

// WithFileName 设置日志文件名
func WithFileName(filename string) FileOption {
	return func(f *fileOptions) {
		if filename != "" {
			f.filename = filename
		}
	}
}

// WithFileMaxSize 设置日志文件最大大小（MB）
func WithFileMaxSize(maxSize int) FileOption {
	return func(f *fileOptions) {
		if maxSize > 0 {
			f.maxSize = maxSize
		}
	}
}

// WithFileMaxBackups 设置旧文件最大保留数量
func WithFileMaxBackups(maxBackups int) FileOption {
	return func(f *fileOptions) {
		if maxBackups > 0 {
			f.maxBackups = maxBackups
		}
	}
}

// WithFileMaxAge 设置旧文件最大保留天数
func WithFileMaxAge(maxAge int) FileOption {
	return func(f *fileOptions) {
		if maxAge > 0 {
			f.maxAge = maxAge
		}
	}
}

// WithFileIsCompression 设置是否压缩日志文件
func WithFileIsCompression(isCompression bool) FileOption {
	return func(f *fileOptions) {
		f.isCompression = isCompression
	}
}

// WithLocalTime 设置是否使用本地时间
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
