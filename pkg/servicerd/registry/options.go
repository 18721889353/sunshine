package registry

// Option 定义服务实例的选项类型。
type Option func(*options)

// options 定义服务实例的选项结构体。
type options struct {
	// version 服务实例的版本号。
	version string
	// metadata 服务实例的元数据，键值对形式。
	metadata map[string]string
}

// defaultOptions 返回一个默认的 options 实例。
func defaultOptions() *options {
	return &options{}
}

// apply 应用传入的选项列表到 options 实例。
func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithVersion 设置服务实例的版本号。
func WithVersion(version string) Option {
	return func(o *options) {
		o.version = version
	}
}

// WithMetadata 设置服务实例的元数据。
func WithMetadata(metadata map[string]string) Option {
	return func(o *options) {
		o.metadata = metadata
	}
}
