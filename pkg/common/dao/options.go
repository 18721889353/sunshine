// Package dao 提供泛型数据访问层基础组件
package dao

// QueryOptions 定义了本次查询的“配置单”
// ForceMaster: true 表示强制走主库，false 表示走默认路由（通常是从库）
// Unscoped:    true 表示忽略软删除（查出已删数据），false 表示只查未删的
type QueryOptions struct {
	ForceMaster bool
	Unscoped    bool
}

// QueryOption 是一个函数类型，它接收配置单指针，可以在里面修改任意字段
// 它的作用是：把“怎么改”这个动作封装成一个变量，方便传递和组合
type QueryOption func(*QueryOptions)

// WithForceMaster 返回一个“把手”：把配置单的 ForceMaster 设置为 true
func WithForceMaster() QueryOption {
	return func(o *QueryOptions) {
		o.ForceMaster = true
	}
}

// WithForceSlave 返回一个“把手”：把配置单的 ForceMaster 设置为 false
// 注意：这里只是关闭“强制主库”，实际走主还是从由 GORM 的 dbresolver 决定
func WithForceSlave() QueryOption {
	return func(o *QueryOptions) {
		o.ForceMaster = false
	}
}

// WithUnscoped 返回一个“把手”：把配置单的 Unscoped 设置为 true
// 启用后，GORM 的查询将不会自动添加 deleted_at IS NULL 条件
func WithUnscoped() QueryOption {
	return func(o *QueryOptions) {
		o.Unscoped = true
	}
}

// DefaultQueryOptions 返回默认的查询配置
// 默认：走主库（确保刚写入的数据能读到），不查已删数据
func DefaultQueryOptions() *QueryOptions {
	return &QueryOptions{
		ForceMaster: true,
		Unscoped:    false,
	}
}

// ApplyOptions 是“配置车间”：先造一份默认配置单，然后用传进来的所有把手（opts）逐一修改它
// 参数 opts ...QueryOption 是可变参数，调用方可以传 0 个或多个选项函数
// 返回值是一个最终定稿的配置单指针
func ApplyOptions(opts ...QueryOption) *QueryOptions {
	o := DefaultQueryOptions() // 直接调用，避免硬编码
	// 遍历所有选项函数，依次执行，每次传入配置单指针供修改
	for _, opt := range opts {
		opt(o)
	}
	return o
}
