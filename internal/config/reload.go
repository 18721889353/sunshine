package config

import (
	"sync"

	"go.uber.org/zap"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ReloadFunc 配置热更新回调函数类型。
// oldCfg 是变更前的配置，newCfg 是变更后的配置。
type ReloadFunc func(oldCfg, newCfg *Config)

// reloadManagerImpl 热更新管理器。
// 持有所有注册的回调函数，reload 时按注册顺序依次执行。
// 每个回调执行后会 recover，单个回调失败不影响后续回调执行。
type reloadManagerImpl struct {
	callbacks []ReloadFunc
	mu        sync.RWMutex
}

// reloadManager 全局热更新管理器实例。
var reloadManager = &reloadManagerImpl{}

// Register 注册配置热更新回调。
// 回调将在 reload 时按注册顺序执行。可多次注册，不会自动去重。
func (r *reloadManagerImpl) Register(fn ReloadFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbacks = append(r.callbacks, fn)
}

// Reload 执行所有已注册的热更新回调。
// 按注册顺序依次执行，单个回调 panic 不影响后续回调。
// 先拷贝回调列表再释放锁执行，避免回调中调用 Register/Reset 导致死锁。
func (r *reloadManagerImpl) Reload(oldCfg, newCfg *Config) {
	r.mu.RLock()
	callbacks := make([]ReloadFunc, len(r.callbacks))
	copy(callbacks, r.callbacks)
	r.mu.RUnlock()

	for i, cb := range callbacks {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Get().Warn("reload callback panic",
						zap.Int("index", i),
						zap.Any("panic", rec),
					)
				}
			}()
			cb(oldCfg, newCfg)
		}()
	}
}

// ReloadCount 返回已注册的热更新回调数量。
func (r *reloadManagerImpl) ReloadCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.callbacks)
}

// Reset 重置热更新回调列表（仅用于测试）。
func (r *reloadManagerImpl) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbacks = nil
}

// RegisterReload 注册配置热更新回调。
func RegisterReload(fn ReloadFunc) {
	reloadManager.Register(fn)
}

// Reload 执行配置热更新，先执行回调链，再更新全局配置。
func Reload(newCfg *Config) {
	oldCfg := Get()
	reloadManager.Reload(oldCfg, newCfg)
	Set(newCfg)
}

// ReloadCount 返回已注册的热更新回调数量。
func ReloadCount() int {
	return reloadManager.ReloadCount()
}

// ResetReloadCallbacks 重置热更新回调列表（仅用于测试）。
func ResetReloadCallbacks() {
	reloadManager.Reset()
}

// RegisterBuiltinReloads 注册所有内置的配置热更新回调。
// 按功能域分组注册，每组回调在对应功能域的配置变更时触发。
func RegisterBuiltinReloads() {
	// Sentinel 限流/熔断规则
	RegisterReload(reloadSentinelFlowRules)
	RegisterReload(reloadSentinelBreakerRules)

	// 安全认证（JWT + 签名）
	RegisterReload(reloadJwtConfig)
	RegisterReload(reloadSignConfig)

	// 应用开关
	RegisterReload(reloadAppFlags)

	// 数据库
	RegisterReload(reloadDatabasePool)
	RegisterReload(reloadGormLogger)

	// 基础设施（链路追踪 + 定时任务 + HTTP超时）
	RegisterReload(reloadTracingConfig)
	RegisterReload(reloadOpenCron)
	RegisterReload(reloadHTTPTimeout)
}
