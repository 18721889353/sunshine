package gogroutine

import (
	"context"
	"fmt"
	"sync"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ============================================================================
// 池实例管理器 - 管理多个独立命名的池实例
// ============================================================================

// PoolManager 管理多个独立命名的池实例。
// 提供创建、获取、释放池实例的能力，支持生命周期管理。
type PoolManager struct {
	pools sync.Map // name -> Pool
}

// globalManager 全局池实例管理器。
var globalManager = &PoolManager{}

// ============================================================================
// 全局 API - 创建和管理池实例
// ============================================================================

// New 创建独立池实例（字节风格）。
// 参数：
//   - name: 池名称（唯一标识）
//   - capacity: 池容量
//   - opts: 可选配置
//
// 示例：
//
//	pool := gogroutine.New("my-pool", 100)
//	defer pool.Release()
//	pool.Go(func() { ... })
func New(name string, capacity int32, opts ...Option) Pool {
	pool, err := NewWithContext(context.Background(), name, capacity, opts...)
	if err != nil {
		panic(fmt.Sprintf("gogroutine: create pool %s failed: %v", name, err))
	}
	return pool
}

// NewWithContext 带 Context 创建池实例。
// Context 用于链路追踪和日志关联。
func NewWithContext(ctx context.Context, name string, capacity int32, opts ...Option) (Pool, error) {
	if name == "" {
		return nil, fmt.Errorf("gogroutine: pool name cannot be empty")
	}

	// 检查是否已存在同名池
	if _, loaded := globalManager.pools.Load(name); loaded {
		return nil, fmt.Errorf("gogroutine: pool %s already exists", name)
	}

	// 创建池实例
	pool, err := newInstance(name, int(capacity), opts...)
	if err != nil {
		return nil, err
	}

	// 注册到管理器
	globalManager.pools.Store(name, pool)

	logger.InfoWithCtx(ctx, "gogroutine: pool created",
		logger.String("name", name),
		logger.Int32("capacity", capacity),
	)

	return pool, nil
}

// Get 根据名称获取池实例。
// 不存在时返回 nil。
func Get(name string) Pool {
	if v, ok := globalManager.pools.Load(name); ok {
		if pool, ok := v.(Pool); ok {
			return pool
		}
	}
	return nil
}

// MustGet 根据名称获取池实例，不存在时 panic。
func MustGet(name string) Pool {
	pool := Get(name)
	if pool == nil {
		panic(fmt.Sprintf("gogroutine: pool %s not found", name))
	}
	return pool
}

// Delete 从管理器中删除池实例（不释放资源）。
func Delete(name string) {
	globalManager.pools.Delete(name)
}

// ReleasePool 释放指定池实例并从管理器中删除。
func ReleasePool(name string) {
	if v, ok := globalManager.pools.LoadAndDelete(name); ok {
		if pool, ok := v.(Pool); ok {
			pool.Release()
			logger.InfoWithCtx(context.Background(), "gogroutine: pool released",
				logger.String("name", name),
			)
		}
	}
}

// ReleaseAllPools 释放所有池实例。
func ReleaseAllPools() {
	globalManager.pools.Range(func(key, value any) bool {
		name, ok1 := key.(string)
		pool, ok2 := value.(Pool)
		if ok1 && ok2 {
			pool.Release()
			globalManager.pools.Delete(name)
			logger.InfoWithCtx(context.Background(), "gogroutine: pool released",
				logger.String("name", name),
			)
		}
		return true
	})
}

// ============================================================================
// 管理器查询 API
// ============================================================================

// PoolManagerStats 管理器统计信息。
type PoolManagerStats struct {
	TotalPools int             // 池总数
	Pools      []PoolStatsInfo // 各池统计
}

// Stats 返回所有池实例的统计信息。
func Stats() PoolManagerStats {
	var pools []PoolStatsInfo
	globalManager.pools.Range(func(_, value any) bool {
		if pool, ok := value.(*poolInstance); ok {
			pools = append(pools, pool.Stats())
		}
		return true
	})
	return PoolManagerStats{
		TotalPools: len(pools),
		Pools:      pools,
	}
}

// ListNames 返回所有池实例的名称列表。
func ListNames() []string {
	var names []string
	globalManager.pools.Range(func(key, _ any) bool {
		if name, ok := key.(string); ok {
			names = append(names, name)
		}
		return true
	})
	return names
}

// Count 返回池实例总数。
func Count() int {
	count := 0
	globalManager.pools.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
