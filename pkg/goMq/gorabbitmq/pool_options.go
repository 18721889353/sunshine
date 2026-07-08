package gorabbitmq

import (
	"time"
)

// PoolOption 连接池配置选项函数类型
type PoolOption func(*poolOptions)

// poolOptions 连接池配置选项
type poolOptions struct {
	initialCap        int                // 初始连接数
	maxCap            int                // 最大连接数
	maxIdle           time.Duration      // 连接最大空闲时间
	connOpts          []ConnectionOption // 连接选项
	healthCheckPeriod time.Duration      // 健康检查周期
	enableTrace       bool               // 是否启用 Trace
}

// apply 应用连接池配置选项
func (o *poolOptions) apply(opts ...PoolOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultPoolOptions 默认连接池配置选项
func defaultPoolOptions() *poolOptions {
	return &poolOptions{
		initialCap:        5,
		maxCap:            30,
		maxIdle:           time.Minute * 10,
		connOpts:          []ConnectionOption{},
		healthCheckPeriod: time.Minute,
		enableTrace:       true,
	}
}

// WithInitialCap 设置初始连接数
func WithInitialCap(initialCap int) PoolOption {
	return func(o *poolOptions) {
		if initialCap > 0 {
			o.initialCap = initialCap
		}
	}
}

// WithMaxCap 设置最大连接数
func WithMaxCap(maxCap int) PoolOption {
	return func(o *poolOptions) {
		if maxCap > 0 {
			o.maxCap = maxCap
		}
	}
}

// WithMaxIdle 设置连接最大空闲时间
func WithMaxIdle(d time.Duration) PoolOption {
	return func(o *poolOptions) {
		if d > 0 {
			o.maxIdle = d
		}
	}
}

// WithConnOptions 设置连接选项
func WithConnOptions(connOpts ...ConnectionOption) PoolOption {
	return func(o *poolOptions) {
		o.connOpts = connOpts
	}
}

// WithAntsPoolSize 设置ants协程池大小（已弃用，无操作）
func WithAntsPoolSize(antsCap int) PoolOption {
	return func(_ *poolOptions) {
		_ = antsCap
	}
}

// WithHealthCheckPeriod 设置健康检查周期
func WithHealthCheckPeriod(d time.Duration) PoolOption {
	return func(o *poolOptions) {
		if d > 0 {
			o.healthCheckPeriod = d
		}
	}
}

// WithTraceEnabled 启用或禁用 Trace
func WithTraceEnabled(enabled bool) PoolOption {
	return func(o *poolOptions) {
		o.enableTrace = enabled
	}
}
