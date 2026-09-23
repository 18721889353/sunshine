// Package config 提供配置初始化和全局配置访问。
package config

import (
	"context"
	"sync"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ReloadFunc 配置热更新回调函数类型。
// 接收旧配置和新配置，由调用方决定如何处理差异。
type ReloadFunc func(oldCfg, newCfg *Config)

// reloadManager 全局配置重载管理器，管理所有注册的热更新回调。
var reloadManager = &reloadManagerImpl{
	callbacks: make([]ReloadFunc, 0),
}

// RegisterReload 注册配置热更新回调。
// 回调在 Nacos 配置变更时按注册顺序依次执行，单个回调 panic 不影响其他回调。
//
// 参数:
//   - fn: 热更新回调函数。
func RegisterReload(fn ReloadFunc) {
	reloadManager.register(fn)
}

// Reload 执行所有注册的配置热更新回调。
// 由 Nacos 监听触发，先替换全局配置，再依次执行回调。
// 每个回调都有 recover 保护，单个失败不影响其他回调。
//
// 参数:
//   - newCfg: 从 Nacos 解析出的新配置。
func Reload(newCfg *Config) {
	reloadManager.reload(newCfg)
}

// reloadManagerImpl 配置重载管理器实现。
type reloadManagerImpl struct {
	callbacks []ReloadFunc
	mu        sync.RWMutex
}

// register 注册热更新回调。
func (m *reloadManagerImpl) register(fn ReloadFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, fn)
}

// reload 执行所有热更新回调。
// 流程：1) 保存旧配置 2) 替换全局配置 3) 遍历执行回调（recover 保护）。
func (m *reloadManagerImpl) reload(newCfg *Config) {
	ctx := context.Background()

	// 1. 获取当前配置作为旧配置
	oldCfg := Get()

	// 2. 替换全局配置（原子操作，通过 Set 实现）
	Set(newCfg)

	// 3. 获取回调列表快照（读锁）
	m.mu.RLock()
	callbacks := make([]ReloadFunc, len(m.callbacks))
	copy(callbacks, m.callbacks)
	m.mu.RUnlock()

	// 4. 逐个执行回调，recover 保护
	for i, fn := range callbacks {
		safeExecuteReload(ctx, i, fn, oldCfg, newCfg)
	}

	logger.InfoWithCtx(ctx, "[config reload] all callbacks executed",
		logger.Int("callbackCount", len(callbacks)),
	)
}

// safeExecuteReload 安全执行单个 reload 回调，recover 保护防止 panic 影响其他回调。
func safeExecuteReload(ctx context.Context, index int, fn ReloadFunc, oldCfg, newCfg *Config) {
	defer func() {
		if r := recover(); r != nil {
			logger.WarnWithCtx(ctx, "[config reload] callback panic",
				logger.Int("callbackIndex", index),
				logger.Any("panic", r),
			)
		}
	}()

	// 执行回调
	fn(oldCfg, newCfg)
}

// ReloadCount 返回已注册的 reload 回调数量（用于测试和调试）。
func ReloadCount() int {
	reloadManager.mu.RLock()
	defer reloadManager.mu.RUnlock()
	return len(reloadManager.callbacks)
}

// ResetReloadCallbacks 清空所有已注册的 reload 回调（仅用于测试）。
func ResetReloadCallbacks() {
	reloadManager.mu.Lock()
	defer reloadManager.mu.Unlock()
	reloadManager.callbacks = make([]ReloadFunc, 0)
}
