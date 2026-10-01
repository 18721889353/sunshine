package gogroutine

import (
	"sync"
	"testing"
	"time"
)

// 本文件对应 pool.go（Pool 接口定义 + 全局默认池管理）的一对一测试映射。

// TestCurrentGlobalPoolNilBeforeInit 验证未初始化时全局池快照为 nil。
func TestCurrentGlobalPoolNilBeforeInit(t *testing.T) {
	resetForTest()
	defer resetForTest()

	if p := currentGlobalPool(); p != nil {
		t.Errorf("未初始化时 currentGlobalPool() = %v, want nil", p)
	}
}

// TestGetOrCreateGlobalPoolIdempotent 验证重复创建返回同一实例（不重复建池）。
func TestGetOrCreateGlobalPoolIdempotent(t *testing.T) {
	resetForTest()
	defer resetForTest()

	first, err := getOrCreateGlobalPool(defaultPoolConfig())
	if err != nil {
		t.Fatalf("getOrCreateGlobalPool failed: %v", err)
	}
	second, err := getOrCreateGlobalPool(defaultPoolConfig())
	if err != nil {
		t.Fatalf("second getOrCreateGlobalPool failed: %v", err)
	}
	if first != second {
		t.Error("重复调用应返回同一实例，而不是新建池")
	}
	if got := first.GetCap(); got != DefaultPoolSize {
		t.Errorf("GetCap() = %d, want %d", got, DefaultPoolSize)
	}
}

// TestPoolConfigToOptionsConversion 验证 poolConfig→Option 转换只带生效项：
// 关闭的开关不产出 Option（避免覆盖 newInstance 里的默认值判断）。
func TestPoolConfigToOptionsConversion(t *testing.T) {
	t.Run("全部关闭时不产出 Option", func(t *testing.T) {
		cfg := &poolConfig{}
		if opts := poolConfigToOptions(cfg); len(opts) != 0 {
			t.Errorf("len(opts) = %d, want 0", len(opts))
		}
	})

	t.Run("开启项逐一产出", func(t *testing.T) {
		cfg := &poolConfig{NonBlocking: true, PreAlloc: true, DisablePurge: true}
		if opts := poolConfigToOptions(cfg); len(opts) != 3 {
			t.Errorf("len(opts) = %d, want 3", len(opts))
		}
	})
}

// TestCurrentGlobalPoolConcurrentWithInit 并发「首次初始化写」与「快照读」守护。
// 原实现 Release/ReleaseAndWait 直接裸读 globalPool，与并发首次 Init 的写构成数据竞争；
// 现读写统一经 globalPoolMu。本机 Windows/MinGW 无法执行 -race 取证（见 README 诚实声明），
// 该用例的作用是保证并发路径不 panic、且初始化完成后读取方可见。
func TestCurrentGlobalPoolConcurrentWithInit(t *testing.T) {
	resetForTest()
	defer resetForTest()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() { // 初始化方：once 内写 globalPool
		defer wg.Done()
		initAndGetPool()
	}()
	go func() { // 读取方：不经 once 的裸读路径
		defer wg.Done()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			_ = currentGlobalPool()
		}
	}()

	wg.Wait()

	if p := currentGlobalPool(); p == nil {
		t.Fatal("初始化完成后 currentGlobalPool() 不应为 nil")
	}
}
