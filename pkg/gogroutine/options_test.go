package gogroutine

import (
	"testing"
	"time"
)

// ============================================================================
// 测试 defaultPoolConfig - 默认配置值
// ============================================================================

func TestDefaultPoolConfig(t *testing.T) {
	cfg := defaultPoolConfig()

	if cfg.PoolSize != DefaultPoolSize {
		t.Errorf("PoolSize = %d, want %d", cfg.PoolSize, DefaultPoolSize)
	}
	if cfg.NonBlocking {
		t.Error("NonBlocking should be false by default")
	}
	if cfg.PreAlloc {
		t.Error("PreAlloc should be false by default")
	}
	if cfg.DisablePurge {
		t.Error("DisablePurge should be false by default")
	}
	if cfg.PurgeInterval != 1*time.Second {
		t.Errorf("PurgeInterval = %v, want 1s", cfg.PurgeInterval)
	}
}

// ============================================================================
// 测试 normalize - 配置参数校验和修正
// ============================================================================

func TestNormalize_PoolSizeBelowMin(t *testing.T) {
	cfg := &poolConfig{PoolSize: 1}
	cfg.normalize()

	if cfg.PoolSize != MinPoolSize {
		t.Errorf("PoolSize = %d, want %d (should clamp to min)", cfg.PoolSize, MinPoolSize)
	}
}

func TestNormalize_PoolSizeAboveMax(t *testing.T) {
	cfg := &poolConfig{PoolSize: 99999}
	cfg.normalize()

	if cfg.PoolSize != MaxPoolSize {
		t.Errorf("PoolSize = %d, want %d (should clamp to max)", cfg.PoolSize, MaxPoolSize)
	}
}

func TestNormalize_PoolSizeValid(t *testing.T) {
	cfg := &poolConfig{PoolSize: 500}
	cfg.normalize()

	if cfg.PoolSize != 500 {
		t.Errorf("PoolSize = %d, want 500 (should remain unchanged)", cfg.PoolSize)
	}
}

func TestNormalize_PoolSizeAtBoundary(t *testing.T) {
	// 测试下边界
	cfg := &poolConfig{PoolSize: MinPoolSize}
	cfg.normalize()
	if cfg.PoolSize != MinPoolSize {
		t.Errorf("PoolSize at min boundary = %d, want %d", cfg.PoolSize, MinPoolSize)
	}

	// 测试上边界
	cfg2 := &poolConfig{PoolSize: MaxPoolSize}
	cfg2.normalize()
	if cfg2.PoolSize != MaxPoolSize {
		t.Errorf("PoolSize at max boundary = %d, want %d", cfg2.PoolSize, MaxPoolSize)
	}
}

// ============================================================================
// 测试 apply - Option 批量应用
// ============================================================================

func TestApply_NoOptions(t *testing.T) {
	cfg := defaultPoolConfig()
	cfg.apply()

	if cfg.PoolSize != DefaultPoolSize {
		t.Errorf("PoolSize should remain default, got %d", cfg.PoolSize)
	}
}

func TestApply_MultipleOptions(t *testing.T) {
	cfg := defaultPoolConfig()
	cfg.apply(
		WithPoolSize(200),
		WithNonBlocking(true),
		WithPreAlloc(true),
		WithDisablePurge(true),
	)

	if cfg.PoolSize != 200 {
		t.Errorf("PoolSize = %d, want 200", cfg.PoolSize)
	}
	if !cfg.NonBlocking {
		t.Error("NonBlocking should be true")
	}
	if !cfg.PreAlloc {
		t.Error("PreAlloc should be true")
	}
	if !cfg.DisablePurge {
		t.Error("DisablePurge should be true")
	}
}

func TestApply_LastOptionWins(t *testing.T) {
	cfg := defaultPoolConfig()
	cfg.apply(
		WithPoolSize(100),
		WithPoolSize(300),
	)

	if cfg.PoolSize != 300 {
		t.Errorf("PoolSize = %d, want 300 (last option should win)", cfg.PoolSize)
	}
}

// ============================================================================
// 测试 With* 选项函数
// ============================================================================

func TestWithPoolSize(t *testing.T) {
	cfg := defaultPoolConfig()
	WithPoolSize(500)(cfg)

	if cfg.PoolSize != 500 {
		t.Errorf("PoolSize = %d, want 500", cfg.PoolSize)
	}
}

func TestWithNonBlocking(t *testing.T) {
	cfg := defaultPoolConfig()
	WithNonBlocking(true)(cfg)

	if !cfg.NonBlocking {
		t.Error("NonBlocking should be true")
	}
}

func TestWithPreAlloc(t *testing.T) {
	cfg := defaultPoolConfig()
	WithPreAlloc(true)(cfg)

	if !cfg.PreAlloc {
		t.Error("PreAlloc should be true")
	}
}

func TestWithDisablePurge(t *testing.T) {
	cfg := defaultPoolConfig()
	WithDisablePurge(true)(cfg)

	if !cfg.DisablePurge {
		t.Error("DisablePurge should be true")
	}
}

// ============================================================================
// 测试 DefaultConfig 兼容性导出
// ============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.PoolSize != DefaultPoolSize {
		t.Errorf("PoolSize = %d, want %d", cfg.PoolSize, DefaultPoolSize)
	}
	if cfg.NonBlocking {
		t.Error("NonBlocking should be false")
	}
	if cfg.PreAlloc {
		t.Error("PreAlloc should be false")
	}
	if cfg.DisablePurge {
		t.Error("DisablePurge should be false")
	}
	if cfg.PurgeInterval != 1*time.Second {
		t.Errorf("PurgeInterval = %v, want 1s", cfg.PurgeInterval)
	}
}
