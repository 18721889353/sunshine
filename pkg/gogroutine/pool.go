package gogroutine

import (
	"context"
	"fmt"
	"sync"

	"github.com/panjf2000/ants/v2"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ============================================================================
// 全局默认池管理
// ============================================================================

var (
	defaultPool     Pool      // 默认协程池实例
	defaultPoolOnce sync.Once // 用于确保默认池只初始化一次
)

// Pool 协程池抽象接口，便于 mock 测试和多实例管理。
type Pool interface {
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
}

// antsPool 基于 ants 的 Pool 实现，封装线程安全的池访问。
type antsPool struct {
	pool *ants.Pool
	mu   sync.RWMutex
}

// newAntsPool 创建一个基于 ants 的协程池实例。
func newAntsPool(size int, opts ...ants.Option) (*antsPool, error) {
	p, err := ants.NewPool(size, opts...)
	if err != nil {
		return nil, fmt.Errorf("gogroutine: create ants pool: %w", err)
	}
	return &antsPool{pool: p}, nil
}

func (p *antsPool) Submit(task func()) error {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	if pool == nil {
		return ants.ErrPoolClosed
	}
	return pool.Submit(task)
}

func (p *antsPool) GetRunningNum() int {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	if pool == nil {
		return 0
	}
	return pool.Running()
}

func (p *antsPool) GetWaitingNum() int {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	if pool == nil {
		return 0
	}
	return pool.Waiting()
}

func (p *antsPool) GetCap() int {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	if pool == nil {
		return 0
	}
	return pool.Cap()
}

func (p *antsPool) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pool != nil {
		p.pool.Release()
		p.pool = nil
	}
}

func (p *antsPool) IsFull() bool {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	if pool == nil {
		return false
	}
	return pool.Running() >= pool.Cap()
}

// getOrCreatePool 获取或创建全局默认协程池。
// 注意：此函数不使用 sync.Once，调用方（Init）需自行保证并发安全。
func getOrCreatePool(cfg *poolConfig) (Pool, error) {
	if defaultPool != nil {
		return defaultPool, nil
	}

	// 创建新的协程池实例
	p, err := newAntsPool(
		cfg.PoolSize,
		buildAntsOptions(cfg)...,
	)
	if err != nil {
		return nil, fmt.Errorf("gogroutine: create pool: %w", err)
	}
	defaultPool = p
	logger.InfoWithCtx(context.Background(), "gogroutine pool initialized",
		logger.Int("pool_size", cfg.PoolSize))
	return defaultPool, nil
}

// buildAntsOptions 将 poolConfig 转换为 ants.Option 列表。
func buildAntsOptions(cfg *poolConfig) []ants.Option {
	// 创建 ants.Option 列表
	opts := []ants.Option{
		// 非阻塞模式
		ants.WithNonblocking(cfg.NonBlocking),
		// 异常处理（备份：executor.go 已完整处理 panic 恢复）
		ants.WithPanicHandler(func(r any) {
			logger.ErrorWithCtx(context.Background(), "goroutine panic recovered in pool handler",
				logger.Any("panic", r))
		}),
	}
	if cfg.PreAlloc {
		opts = append(opts, ants.WithPreAlloc(true))
	}
	if cfg.DisablePurge {
		opts = append(opts, ants.WithDisablePurge(true))
	}
	return opts
}
