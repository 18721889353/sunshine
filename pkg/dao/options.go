// Package dao 提供泛型数据访问层基础组件
package dao

// QueryOptions 查询选项配置
type QueryOptions struct {
	ForceMaster bool // 是否强制使用主库
	Unscoped    bool // 是否忽略软删除
}

// QueryOption 查询选项函数类型
type QueryOption func(*QueryOptions)

// WithForceMaster 强制使用主库查询
func WithForceMaster() QueryOption {
	return func(o *QueryOptions) {
		o.ForceMaster = true
	}
}

// WithForceSlave 强制使用从库查询（默认即为从库，此选项主要用于显式声明意图）
func WithForceSlave() QueryOption {
	return func(o *QueryOptions) {
		o.ForceMaster = false
	}
}

// WithUnscoped 忽略软删除（不自动添加 deleted_at IS NULL）
func WithUnscoped() QueryOption {
	return func(o *QueryOptions) {
		o.Unscoped = true
	}
}

// ApplyOptions 应用选项配置（默认强制主库）
func ApplyOptions(opts ...QueryOption) *QueryOptions {
	o := &QueryOptions{
		ForceMaster: true,  // 默认主库
		Unscoped:    false, // 默认不忽略软删除
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
