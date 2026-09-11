package gogroutine

import (
	"context"
	"fmt"
	"sync"

	"github.com/panjf2000/ants/v2"

	"github.com/18721889353/sunshine/pkg/logger"
)

// Pool 协程池抽象接口，便于 mock 测试和多实例管理。
type Pool interface {
	// Submit 提交任务到池中执行
	Submit(task func()) error
	// Running 返回当前运行中的任务数
	Running() int
	// Waiting 返回等待队列中的任务数
	Waiting() int
	// Cap 返回池容量
	Cap() int
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

func (p *antsPool) Running() int {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	if pool == nil {
		return 0
	}
	return pool.Running()
}

func (p *antsPool) Waiting() int {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()
	if pool == nil {
		return 0
	}
	return pool.Waiting()
}

func (p *antsPool) Cap() int {
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
	return p.Running() >= p.Cap()
}

// ============================================================================
// 全局默认池管理
// ============================================================================

var (
	defaultPool     Pool
	defaultPoolOnce sync.Once
)

// getOrCreatePool 获取或创建全局默认协程池。
// 注意：此函数不使用 sync.Once，调用方（Init）需自行保证并发安全。
func getOrCreatePool(cfg *poolConfig) Pool {
	if defaultPool != nil {
		return defaultPool
	}
	p, err := newAntsPool(
		cfg.PoolSize,
		buildAntsOptions(cfg)...,
	)
	if err != nil {
		panic(fmt.Sprintf("gogroutine: init pool failed: %v", err))
	}
	defaultPool = p
	logger.InfoWithCtx(context.Background(), "gogroutine pool initialized",
		logger.Int("pool_size", cfg.PoolSize))
	return defaultPool
}

// buildAntsOptions 将 poolConfig 转换为 ants.Option 列表。
func buildAntsOptions(cfg *poolConfig) []ants.Option {
	opts := []ants.Option{
		ants.WithNonblocking(cfg.NonBlocking),
		ants.WithPanicHandler(func(r any) {
			panicCount.Add(1)
			m := getMetrics()
			if m != nil {
				m.IncPanic("unknown")
			}
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
