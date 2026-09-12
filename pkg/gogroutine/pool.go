package gogroutine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ============================================================================
// Pool 接口定义 - 参考字节跳动 gopool 设计
// ============================================================================

// Pool 协程池抽象接口，便于 mock 测试和多实例管理。
// 包含字节 gopool 标准接口和扩展接口。
type Pool interface {
	// ---- 字节 gopool 标准接口 ----

	// Name 返回池名称（唯一标识）
	Name() string
	// SetCap 设置池容量（动态调整）
	SetCap(capacity int32)
	// Go 提交任务到池中执行（无 Context）
	Go(f func())
	// CtxGo 提交带 Context 的任务到池中执行
	CtxGo(ctx context.Context, f func())
	// SetPanicHandler 设置自定义 panic 处理器
	SetPanicHandler(f func(ctx context.Context, r interface{}))

	// ---- 扩展接口（保持兼容） ----

	// Submit 提交任务到池中执行
	Submit(task func()) error
	// GetRunningNum 返回当前运行中的任务数
	GetRunningNum() int
	// GetWaitingNum 返回等待队列中的任务数
	GetWaitingNum() int
	// GetCap 返回池容量
	GetCap() int
	// Release 释放池资源
	Release()
	// IsFull 检查池是否已满
	IsFull() bool
	// Stats 返回池统计信息
	Stats() PoolStatsInfo
}

// PoolStatsInfo 池统计信息。
type PoolStatsInfo struct {
	Name    string    // 池名称
	Running int       // 当前运行中任务数
	Waiting int       // 等待队列中的任务数
	Cap     int       // 池容量
	Submit  int64     // 累计提交任务数
	Success int64     // 累计成功任务数
	Panic   int64     // 累计 panic 次数
	Created time.Time // 创建时间
}

// ============================================================================
// 全局默认池管理
// ============================================================================

var (
	// globalPool 全局默认协程池实例
	globalPool Pool
	// globalPoolOnce 确保全局池只初始化一次
	globalPoolOnce sync.Once
)

// ============================================================================
// 全局池辅助函数
// ============================================================================

// getOrCreateGlobalPool 获取或创建全局默认协程池。
// 注意：此函数不使用 sync.Once，调用方（Init）需自行保证并发安全。
func getOrCreateGlobalPool(cfg *poolConfig) (Pool, error) {
	if globalPool != nil {
		return globalPool, nil
	}

	// 创建新的全局池实例（使用 "_global" 作为名称）
	p, err := newInstance("_global", cfg.PoolSize, poolConfigToOptions(cfg)...)
	if err != nil {
		return nil, fmt.Errorf("gogroutine: create global pool: %w", err)
	}
	globalPool = p
	logger.InfoWithCtx(context.Background(), "gogroutine global pool initialized",
		logger.Int("pool_size", cfg.PoolSize))
	return globalPool, nil
}

// poolConfigToOptions 将 poolConfig 转换为 Option 列表。
func poolConfigToOptions(cfg *poolConfig) []Option {
	var opts []Option
	if cfg.NonBlocking {
		opts = append(opts, WithNonBlocking(true))
	}
	if cfg.PreAlloc {
		opts = append(opts, WithPreAlloc(true))
	}
	if cfg.DisablePurge {
		opts = append(opts, WithDisablePurge(true))
	}
	return opts
}
