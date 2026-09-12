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
	// DefaultGracefulShutdownTimeout 默认优雅关闭超时时间
	DefaultGracefulShutdownTimeout = 30 * time.Second
)

// poolConfig 协程池内部配置（小写，包内使用）。
type poolConfig struct {
	PoolSize                int           // 协程池大小
	NonBlocking             bool          // 是否启用非阻塞模式
	PreAlloc                bool          // 是否预分配内存
	DisablePurge            bool          // 是否禁用自动清理
	PurgeInterval           time.Duration // 自动清理间隔
	GracefulShutdown        bool          // 是否启用优雅关闭（信号退出自动释放）
	GracefulShutdownTimeout time.Duration // 优雅关闭超时时间
}

// defaultPoolConfig 返回默认的池配置。
// 生产环境推荐配置：预分配 + 禁用清理 + 优雅关闭
func defaultPoolConfig() *poolConfig {
	return &poolConfig{
		PoolSize:                DefaultPoolSize,                // 协程池大小
		NonBlocking:             false,                          // 阻塞模式（池满时降级，不丢失任务）
		PreAlloc:                true,                           // 预分配内存（减少 GC，生产环境推荐）
		DisablePurge:            true,                           // 禁用自动清理（保留 worker，避免冷启动延迟）
		PurgeInterval:           1 * time.Second,                // 自动清理间隔（DisablePurge=true 时无效）
		GracefulShutdown:        true,                           // 启用优雅关闭（程序退出自动释放池）
		GracefulShutdownTimeout: DefaultGracefulShutdownTimeout, // 优雅关闭超时时间（默认 30 秒）
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

// WithPoolSize 设置协程池大小（自动限制在 [MinPoolSize, MaxPoolSize] 范围内）。
func WithPoolSize(size int) Option {
	return func(c *poolConfig) {
		c.PoolSize = clampPoolSize(size)
	}
}

// clampPoolSize 将池大小限制在合法范围内。
func clampPoolSize(size int) int {
	if size < MinPoolSize {
		return MinPoolSize
	}
	if size > MaxPoolSize {
		return MaxPoolSize
	}
	return size
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

// WithGracefulShutdown 启用优雅关闭。
// 启用后，程序收到 SIGINT/SIGTERM 信号时自动释放协程池。
// 注意：此选项会启动信号监听 goroutine，仅在 main goroutine 中调用一次 Init 即可。
func WithGracefulShutdown(enable bool) Option {
	return func(c *poolConfig) {
		c.GracefulShutdown = enable
	}
}

// WithGracefulShutdownTimeout 设置优雅关闭超时时间。
// 仅在启用 WithGracefulShutdown 时生效，默认 30 秒。
func WithGracefulShutdownTimeout(timeout time.Duration) Option {
	return func(c *poolConfig) {
		c.GracefulShutdownTimeout = timeout
	}
}
