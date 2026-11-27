package etcdcli

import (
	"context"
	"sync"
	"testing"
)

// TestStoreCancelIfAbsent 幂等注册：重复 key 不覆盖，且新建 cancel 被释放。
func TestStoreCancelIfAbsent(t *testing.T) {
	var m sync.Map
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	if !storeCancelIfAbsent(&m, "cli-a", cancel1) {
		t.Fatal("首次注册应成功")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	if storeCancelIfAbsent(&m, "cli-a", cancel2) {
		t.Error("重复注册应返回 false（不能覆盖已有 cancel）")
	}
	// 新建 ctx 应已被取消（避免泄漏）
	select {
	case <-ctx2.Done():
	default:
		t.Error("重复注册时新建的 ctx 应被取消")
	}
	// 原 ctx 不受影响
	select {
	case <-ctx1.Done():
		t.Error("已有监控的 ctx 不应被误取消")
	default:
	}

	// 不同 key 可并存
	_, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	if !storeCancelIfAbsent(&m, "cli-b", cancel3) {
		t.Error("不同 key 应可并存注册")
	}
}
