package gogroutine

import "time"

// ============================================================================
// 协程池配置 - 遵循 defaultXxxOptions + apply 模式
// ============================================================================

const (
	// DefaultPoolSize 默认并发限制
	DefaultPoolSize = 1000
	// MinPoolSize 最小并发限制
	MinPoolSize = 10
	// MaxPoolSize 最大并发限制
	MaxPoolSize = 10000
)

// poolConfig 协程池内部配置（小写，包内使用）。
type poolConfig struct {
	PoolSize      int
	NonBlocking   bool
	PreAlloc      bool
	DisablePurge  bool
	PurgeInterval time.Duration
}

// defaultPoolConfig 返回默认的池配置。
func defaultPoolConfig() *poolConfig {
	return &poolConfig{
		PoolSize:      DefaultPoolSize,
		NonBlocking:   false,
		PreAlloc:      false,
		DisablePurge:  false,
		PurgeInterval: 1 * time.Second,
	}
}

// ============================================================================
// Option 配置选项函数 - 导出给外部使用
// ============================================================================

// Option 配置选项函数类型。
type Option func(*poolConfig)

// apply 批量应用配置选项。
func (c *poolConfig) apply(opts ...Option) {
	for _, opt := range opts {
		opt(c)
	}
}

// normalize 校验并修正配置参数，确保在合法范围内。
func (c *poolConfig) normalize() {
	if c.PoolSize < MinPoolSize {
		c.PoolSize = MinPoolSize
	}
	if c.PoolSize > MaxPoolSize {
		c.PoolSize = MaxPoolSize
	}
}

// WithPoolSize 设置协程池大小（会被限制在 [MinPoolSize, MaxPoolSize] 范围内）。
func WithPoolSize(size int) Option {
	return func(c *poolConfig) {
		c.PoolSize = size
	}
}

// WithNonBlocking 设置非阻塞模式。
// 非阻塞模式下，Submit 在池满时立即返回错误，而非阻塞等待。
func WithNonBlocking(nonBlocking bool) Option {
	return func(c *poolConfig) {
		c.NonBlocking = nonBlocking
	}
}

// WithPreAlloc 设置预分配内存。
// 预分配可减少运行时内存分配次数，适合高并发场景。
func WithPreAlloc(preAlloc bool) Option {
	return func(c *poolConfig) {
		c.PreAlloc = preAlloc
	}
}

// WithDisablePurge 禁用自动清理。
// 禁用后空闲 goroutine 不会被回收，适合突发流量场景。
func WithDisablePurge(disable bool) Option {
	return func(c *poolConfig) {
		c.DisablePurge = disable
	}
}
