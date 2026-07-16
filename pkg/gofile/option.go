package gofile

const (
	prefix  = "prefix"
	suffix  = "suffix"
	contain = "contain"
)

var (
	defaultFilterType = ""
)

type options struct {
	filter string
	name   string

	noAbsolutePath bool
}

// defaultOptions 返回默认的选项配置，默认不启用文件名过滤
//
// 返回值：
//
//	*options - 默认选项实例
func defaultOptions() *options {
	return &options{
		filter: defaultFilterType,
	}
}

// Option 定义文件选项的函数类型，用于配置文件操作的行为
//
// 支持 WithPrefix、WithSuffix、WithContain 进行文件名过滤，
// 以及 WithNoAbsolutePath 禁用绝对路径转换。
type Option func(*options)

// apply 将一组选项应用到当前选项实例
//
// 参数：
//
//	opts - 要应用的选项列表
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithSuffix 设置文件名后缀匹配过滤
//
// 参数：
//
//	name - 要匹配的文件名后缀
//
// 返回值：
//
//	Option - 选项函数
func WithSuffix(name string) Option {
	return func(o *options) {
		o.filter = suffix
		o.name = name
	}
}

// WithPrefix 设置文件名前缀匹配过滤
//
// 参数：
//
//	name - 要匹配的文件名前缀
//
// 返回值：
//
//	Option - 选项函数
func WithPrefix(name string) Option {
	return func(o *options) {
		o.filter = prefix
		o.name = name
	}
}

// WithContain 设置文件名包含匹配过滤
//
// 参数：
//
//	name - 要匹配的字符串
//
// 返回值：
//
//	Option - 选项函数
func WithContain(name string) Option {
	return func(o *options) {
		o.filter = contain
		o.name = name
	}
}

// WithNoAbsolutePath 禁用路径转换为绝对路径，保留原始路径格式
//
// 返回值：
//
//	Option - 选项函数
func WithNoAbsolutePath() Option {
	return func(o *options) {
		o.noAbsolutePath = true
	}
}
