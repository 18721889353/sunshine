package gows

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Dispatcher 核心功能测试（结构体、生命周期、查询）
// ---------------------------------------------------------------------------

func TestDemo_ViewConnections(t *testing.T) {
	d := NewDispatcher(nil)

	// 注册 3 个不同 UID 的客户端
	uidList := []string{"alice", "bob", "charlie"}
	clients := make([]*Client, 0, len(uidList))
	for _, uid := range uidList {
		c, _ := newTestClientWithUID(t, uid)
		if err := d.RegisterCtx(context.Background(), c); err != nil {
			t.Fatalf("register %s: %v", uid, err)
		}
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// 1. Len() — 获取在线连接总数
	if n := d.Len(); n != 3 {
		t.Fatalf("Len() = %d, want 3", n)
	}
	t.Logf("✅ 在线连接数: %d", d.Len())

	// 2. Clients() — 获取快照并遍历
	snapshot := d.Clients()
	t.Logf("✅ Clients() 快照大小: %d", len(snapshot))
	for _, c := range snapshot {
		t.Logf("   └─ UID=%s, RemoteAddr=%s, Alive=%v", c.UID(), c.RemoteAddr(), c.IsAlive())
	}

	// 3. Range() — 回调遍历
	t.Log("✅ Range() 遍历所有连接:")
	d.Range(func(c *Client) bool {
		t.Logf("   └─ [%s] addr=%s", c.UID(), c.RemoteAddr())
		return true
	})

	// 4. Stats() — 统计概览
	stats := d.Stats()
	t.Logf("✅ Dispatcher Stats: TotalConnections=%d, MaxConnections=%d, TotalRejected=%d",
		stats.TotalConnections, stats.MaxConnections, stats.TotalRejected)

	// 5. 按 UID 分组统计连接数
	uidCount := map[string]int{}
	d.Range(func(c *Client) bool {
		uidCount[c.UID()]++
		return true
	})
	t.Log("✅ 各用户连接分布:")
	for uid, count := range uidCount {
		t.Logf("   └─ %s: %d 个连接", uid, count)
	}
}

func TestDemo_UseCustomDispatcher(t *testing.T) {
	// 创建两个独立的 Dispatcher
	roomChat := NewDispatcher(nil)
	roomVoice := NewDispatcher(nil)

	// 用户 alice 同时加入了聊天室和语音室
	c1, _ := newTestClientWithUID(t, "alice")
	_ = roomChat.RegisterCtx(context.Background(), c1)
	_ = roomVoice.RegisterCtx(context.Background(), c1)
	defer c1.Close()

	// 用户 bob 只加入了聊天室
	c2, _ := newTestClientWithUID(t, "bob")
	_ = roomChat.RegisterCtx(context.Background(), c2)
	defer c2.Close()

	// 只在聊天室广播 — bob 能收到
	roomChat.BroadcastCtx(context.Background(), Message{Type: "chat", Msg: "大家好"})

	// 只在语音室广播 — bob 收不到
	roomVoice.BroadcastCtx(context.Background(), Message{Type: "voice", Msg: "语音消息"})

	time.Sleep(50 * time.Millisecond)
	t.Log("✅ 独立 Dispatcher 隔离:")
	t.Logf("   ├─ 聊天室: %d 人", roomChat.Len())
	t.Logf("   ├─ 语音室: %d 人", roomVoice.Len())
	t.Logf("   └─ (bob 只加入聊天室, 收不到语音室消息)")
}

func TestDemo_ConcurrentUsage(t *testing.T) {
	d := NewDispatcher(nil)
	var clients []*Client

	// 创建 5 个客户端
	for i := 0; i < 5; i++ {
		c, _ := newTestClientWithUID(t, fmt.Sprintf("user-%d", i))
		if err := d.RegisterCtx(context.Background(), c); err != nil {
			t.Fatalf("register: %v", err)
		}
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	var sendCount, viewCount atomic.Int64

	// 并发发送 goroutine × 3
	for i := 0; i < 3; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				d.SendToUIDCtx(context.Background(),
					fmt.Sprintf("user-%d", j%5),
					Message{Type: "concurrent", Msg: "test"},
				)
				sendCount.Add(1)
			}
		}()
	}

	// 并发查看 goroutine × 3
	for i := 0; i < 3; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				_ = d.Len()
				_ = d.Clients()
				_ = d.Stats()
				d.Range(func(c *Client) bool { return true })
				viewCount.Add(1)
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	t.Logf("✅ 并发安全测试:")
	t.Logf("   ├─ 并发发送: %d 次", sendCount.Load())
	t.Logf("   ├─ 并发查看: %d 次", viewCount.Load())
	t.Logf("   └─ 最终在线: %d", d.Len())
}
