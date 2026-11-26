package config

import (
	"testing"

	"go.uber.org/atomic"
)

// TestReloadManager_RegisterNil nil 回调必须被忽略——
// 否则 Reload 时 nil 调用被 recover 静默吞掉（回调永久失效且运维无感）。
func TestReloadManager_RegisterNil(t *testing.T) {
	r := &reloadManagerImpl{}
	defer r.Reset()

	r.Register(nil)
	if n := r.ReloadCount(); n != 0 {
		t.Errorf("nil 回调不应注册, got %d", n)
	}

	// 正常回调仍可注册并执行
	called := atomic.NewInt32(0)
	r.Register(func(_, _ *Config) { called.Inc() })
	r.Register(nil) // 夹在正常回调之间
	if n := r.ReloadCount(); n != 1 {
		t.Errorf("应只注册 1 个有效回调, got %d", n)
	}
	r.Reload(&Config{}, &Config{})
	if got := called.Load(); got != 1 {
		t.Errorf("有效回调应执行 1 次, got %d", got)
	}
}

// TestReload_NilCallbackNotSwallowed 端到端模拟一次真实配置变更：
// 先尝试注册 nil 回调（模拟误传），再走完整 Reload 流程（审计日志→回调链→Set 全局配置），
// 验证 nil 回调不会进入回调链被 recover 静默吞掉，正常回调仍执行、全局配置已更新。
func TestReload_NilCallbackNotSwallowed(t *testing.T) {
	// 保存/恢复全局状态，避免污染其他测试
	// 测试环境未 Init 全局配置，先 Set 一个初始值
	initial := &Config{}
	initial.App.Name = "initial-app"
	Set(initial)
	oldCfg := Get()
	t.Cleanup(func() { Set(oldCfg) })

	resetReloadManager := func() {
		reloadManager.mu.Lock()
		reloadManager.callbacks = nil
		reloadManager.mu.Unlock()
	}
	resetReloadManager()
	t.Cleanup(resetReloadManager)

	// 1. 模拟误传 nil 回调（真实场景下可能来自 nil 函数变量）
	var fn ReloadFunc // nil
	RegisterReload(fn)
	if n := ReloadCount(); n != 0 {
		t.Fatalf("nil 回调不应进入回调链, got %d", n)
	}

	// 2. 注册一个正常回调，记录是否执行
	called := atomic.NewInt32(0)
	RegisterReload(func(old, new *Config) {
		if old == nil || new == nil {
			t.Error("回调应收到 old/new 配置")
		}
		called.Inc()
	})

	// 3. 构造新配置（模拟 Nacos 推送的新内容）
	newCfg := *Get()
	newCfg.App.Name = "reload-test-app"
	newCfg.App.Version = "v2"

	// 4. 触发真实 Reload（审计日志 + 回调链 + Set）
	Reload(&newCfg)

	// 5. 验证：回调执行 1 次（nil 未入链，否则会被 recover 吞掉但 count 仍为 1）
	if got := called.Load(); got != 1 {
		t.Errorf("有效回调应执行 1 次, got %d", got)
	}
	if n := ReloadCount(); n != 1 {
		t.Errorf("回调链应只有 1 个有效回调, got %d", n)
	}
	// 全局配置已更新（Reload 最后一步 Set）
	if got := Get().App.Version; got != "v2" {
		t.Errorf("全局配置应更新为 v2, got %q", got)
	}
}

// TestReload_NilInChainRecoveredAndContinues 直接把 nil 塞进回调链（绕过 Register 校验，
// 模拟历史遗留/内部误操作），验证 Reload 时 nil 调用被 recover 隔离、
// 且**不影响后续回调执行**（不会被吞掉整条链）。
func TestReload_NilInChainRecoveredAndContinues(t *testing.T) {
	r := &reloadManagerImpl{}
	defer r.Reset()

	afterNil := atomic.NewInt32(0)
	beforeNil := atomic.NewInt32(0)

	// 正常 → nil → 正常：nil 夹在中间
	r.Register(func(_, _ *Config) { beforeNil.Inc() })
	r.mu.Lock()
	r.callbacks = append(r.callbacks, nil) // 绕过 Register 的 nil 校验，直接塞入
	r.mu.Unlock()
	r.Register(func(_, _ *Config) { afterNil.Inc() })

	if n := r.ReloadCount(); n != 3 {
		t.Fatalf("回调链应有 3 项（含 nil）, got %d", n)
	}

	// Reload：nil panic 应被 recover，前后两个正常回调都执行
	r.Reload(&Config{}, &Config{})

	if got := beforeNil.Load(); got != 1 {
		t.Errorf("nil 之前的回调应执行 1 次, got %d", got)
	}
	if got := afterNil.Load(); got != 1 {
		t.Errorf("nil 之后的回调应执行 1 次（不被 nil 吞掉）, got %d", got)
	}
}

// TestRegisterNil_DoesNotOverwriteExisting nil 注册不应覆盖/清空已有回调列表。
func TestRegisterNil_DoesNotOverwriteExisting(t *testing.T) {
	r := &reloadManagerImpl{}
	defer r.Reset()

	called := atomic.NewInt32(0)
	r.Register(func(_, _ *Config) { called.Inc() })
	before := r.ReloadCount()

	r.Register(nil) // 不应覆盖已有回调

	if got := r.ReloadCount(); got != before {
		t.Errorf("Register(nil) 不应改变回调数量: before=%d after=%d", before, got)
	}
	r.Reload(&Config{}, &Config{})
	if got := called.Load(); got != 1 {
		t.Errorf("已有回调应仍可执行 1 次, got %d", got)
	}
}
