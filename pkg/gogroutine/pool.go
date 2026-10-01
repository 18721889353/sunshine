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

	// Submit 提交任务到池中执行（底层提交语义：不降级——池满/已释放返回 error，
	// 交由调用方处理；需要「池满自动降级为原生 goroutine」时用 CtxGo，评审 R2-P1-5）
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
	// globalPool 全局默认协程池实例（读写均经 globalPoolMu，见 currentGlobalPool）
	globalPool Pool
	// globalPoolOnce 确保全局池只初始化一次
	globalPoolOnce sync.Once
	// globalPoolMu 保护 globalPool 变量本身的读写。
	// 初始化仍在 once 内串行，但 Release/ReleaseAndWait 等读取方不经过 once，
	// 若无锁直接读会与并发首次 Init 的写构成数据竞争（原实现缺陷）。
	globalPoolMu sync.RWMutex
)

// currentGlobalPool 返回全局池快照（未初始化时为 nil）。
// 所有非初始化路径的 globalPool 读取都走本函数，与 getOrCreateGlobalPool 的写互斥。
func currentGlobalPool() Pool {
	globalPoolMu.RLock()
	p := globalPool
	globalPoolMu.RUnlock()
	return p
}

// ============================================================================
// 全局池辅助函数
// ============================================================================

// getOrCreateGlobalPool 获取或创建全局默认协程池。
// 注意：此函数不使用 sync.Once，调用方（Init）需自行保证并发安全。
func getOrCreateGlobalPool(cfg *poolConfig) (Pool, error) {
	if p := currentGlobalPool(); p != nil {
		return p, nil
	}

	// 创建新的全局池实例（使用 "_global" 作为名称）
	p, err := newInstance("_global", cfg.PoolSize, poolConfigToOptions(cfg)...)
	if err != nil {
		return nil, fmt.Errorf("gogroutine: create global pool: %w", err)
	}
	globalPoolMu.Lock()
	// 绑定全局池计数器：globalTaskHooks 经 atomic 读取它维护 running，
	// 保证 PoolStats().Running 与 ReleaseAndWait 轮询口径不因管线统一而改变（评审 R2-P1-3）
	globalPoolMetrics.Store(p.metrics)
	globalPool = p
	globalPoolMu.Unlock()
	logger.InfoWithCtx(context.Background(), "gogroutine global pool initialized",
		logger.Int("pool_size", cfg.PoolSize))
	return p, nil
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
