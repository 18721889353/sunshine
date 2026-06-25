package gows

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// 测试辅助: 使用 mockBackend 模拟跨实例消息传递
// ---------------------------------------------------------------------------

func TestDistributedDispatcher_SendToUIDCtx(t *testing.T) {
	backend := newMockBackend(64)

	// 实例 A 和 B 共用同一个 Backend，但各自管理本地连接
	instanceA := NewDispatcher(backend)
	instanceB := NewDispatcher(backend)

	ctx := context.Background()
	instanceA.Start(ctx)
	instanceB.Start(ctx)
	defer instanceA.Stop()
	defer instanceB.Stop()

	// 实例 A 上有 alice，实例 B 上有 bob
	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = instanceA.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = instanceB.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	// 实例 A 发消息给 bob — 应跨实例投递到 B
	instanceA.SendToUIDCtx(ctx, "bob", Message{Type: "greeting", Msg: "hello bob"})

	// 等待消息通过 Backend 传播
	time.Sleep(100 * time.Millisecond)

	// bob 应收到消息
	_, data, err := bobConn.ReadMessage()
	if err != nil {
		t.Fatalf("bob should receive message, got err: %v", err)
	}
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Msg != "hello bob" {
		t.Errorf("bob got msg=%q, want %q", msg.Msg, "hello bob")
	}
	t.Logf("✅ 跨实例: 实例A → Backend → 实例B, bob 收到: %s", string(data))

	// alice 不应收到消息
	aliceConn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	_, _, err = aliceConn.ReadMessage()
	if err == nil {
		t.Error("alice should NOT receive message for bob")
	} else {
		t.Logf("✅ alice 未收到消息（预期）: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestDistributedDispatcher_SendToMultiUIDCtx: 给多个 UID 发消息
// ---------------------------------------------------------------------------

func TestDistributedDispatcher_SendToMultiUIDCtx(t *testing.T) {
	backend := newMockBackend(64)

	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	// 在本地注册 alice 和 bob
	clients := map[string]*Client{}
	conns := map[string]*websocket.Conn{}
	for _, uid := range []string{"alice", "bob", "charlie"} {
		c, conn := newTestClientWithUID(t, uid)
		_ = dd.RegisterCtx(context.Background(), c)
		clients[uid] = c
		conns[uid] = conn
		defer c.Close()
	}

	// 只发给 alice 和 charlie
	dd.SendToMultiUIDCtx(ctx, []string{"alice", "charlie"}, Message{Type: "team", Msg: "team msg"})

	time.Sleep(100 * time.Millisecond)

	// alice 和 charlie 应收到
	for _, uid := range []string{"alice", "charlie"} {
		_, data, err := conns[uid].ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive: %v", uid, err)
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Msg != "team msg" {
			t.Errorf("%s got %q, want %q", uid, msg.Msg, "team msg")
		}
		t.Logf("✅ %s 收到消息: %s", uid, string(data))
	}

	// bob 不应收到
	conns["bob"].SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	_, _, err := conns["bob"].ReadMessage()
	if err == nil {
		t.Error("bob should NOT receive")
	} else {
		t.Logf("✅ bob 未收到（预期）")
	}
}

// ---------------------------------------------------------------------------
// TestDistributedDispatcher_BroadcastCtx: 跨实例广播
// ---------------------------------------------------------------------------

func TestDistributedDispatcher_BroadcastCtx(t *testing.T) {
	backend := newMockBackend(64)

	instanceA := NewDispatcher(backend)
	instanceB := NewDispatcher(backend)

	ctx := context.Background()
	instanceA.Start(ctx)
	instanceB.Start(ctx)
	defer instanceA.Stop()
	defer instanceB.Stop()

	// 实例 A 有 alice，实例 B 有 bob
	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = instanceA.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = instanceB.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	// 实例 A 广播 — 两个实例都应收到
	instanceA.BroadcastCtx(ctx, Message{Type: "announce", Msg: "系统通知"})

	time.Sleep(100 * time.Millisecond)

	// alice 和 bob 都应收到
	for name, conn := range map[string]*websocket.Conn{"alice": aliceConn, "bob": bobConn} {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive broadcast: %v", name, err)
		}
		t.Logf("✅ %s 收到广播: %s", name, string(data))
	}
}

// ---------------------------------------------------------------------------
// TestDistributedDispatcher_SelfPublishAndReceive: 自己发的自己能收到
// （在同一实例上有匹配的客户端时）
// ---------------------------------------------------------------------------

func TestDistributedDispatcher_SelfPublishAndReceive(t *testing.T) {
	backend := newMockBackend(64)

	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	// 本地注册 alice
	c, conn := newTestClientWithUID(t, "alice")
	_ = dd.RegisterCtx(context.Background(), c)
	defer c.Close()

	// 发给 alice（alice 在当前实例）
	dd.SendToUIDCtx(ctx, "alice", Message{Type: "self", Msg: "自己发的"})

	time.Sleep(100 * time.Millisecond)

	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("alice should receive self-published msg: %v", err)
	}
	t.Logf("✅ 自发布自接收: %s", string(data))
}

// ---------------------------------------------------------------------------
// TestDistributedDispatcher_Integration: 完整集成场景
// 模拟两个实例，多个用户，交叉发送
// ---------------------------------------------------------------------------

func TestDistributedDispatcher_Integration(t *testing.T) {
	backend := newMockBackend(64)

	// 两个实例
	instances := []*DistributedDispatcher{
		NewDispatcher(backend),
		NewDispatcher(backend),
	}

	ctx := context.Background()
	for _, dd := range instances {
		dd.Start(ctx)
		defer dd.Stop()
	}

	// 实例0: alice, bob   实例1: charlie, dave
	type userConn struct {
		client *Client
		conn   *websocket.Conn
		inst   int
	}
	users := map[string]*userConn{
		"alice":   {inst: 0},
		"bob":     {inst: 0},
		"charlie": {inst: 1},
		"dave":    {inst: 1},
	}
	for name, uc := range users {
		c, conn := newTestClientWithUID(t, name)
		uc.client = c
		uc.conn = conn
		_ = instances[uc.inst].RegisterCtx(context.Background(), c)
		defer c.Close()
	}

	// 实例0 给 charlie 发消息
	instances[0].SendToUIDCtx(ctx, "charlie", Message{Type: "p2p", Msg: "from instance 0"})

	// 实例1 广播
	instances[1].BroadcastCtx(ctx, Message{Type: "notice", Msg: "announcement"})

	time.Sleep(150 * time.Millisecond)

	// charlie 应收到两条（p2p + broadcast）
	received := make(map[string]int)
	for _, uc := range users {
		for {
			uc.conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
			_, data, err := uc.conn.ReadMessage()
			if err != nil {
				break
			}
			received[uc.client.UID()]++
			t.Logf("📬 %s 收到: %s", uc.client.UID(), string(data))
		}
	}

	// 验证
	for name, count := range received {
		t.Logf("   %s 共 %d 条", name, count)
	}
	if received["charlie"] < 2 {
		t.Errorf("charlie should receive >=2 messages, got %d", received["charlie"])
	}
	if received["dave"] < 1 {
		t.Errorf("dave should receive >=1 message, got %d", received["dave"])
	}
	if received["alice"] != 0 {
		t.Logf("alice 收到 %d 条（bob也应该一样）", received["alice"])
	}
}

// ---------------------------------------------------------------------------
// TestDistributedDispatcher_Concurrent: 并发安全
// ---------------------------------------------------------------------------

func TestDistributedDispatcher_Concurrent(t *testing.T) {
	backend := newMockBackend(64)

	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	// 注册 5 个客户端
	for i := 0; i < 5; i++ {
		c, _ := newTestClientWithUID(t, "user")
		_ = dd.RegisterCtx(context.Background(), c)
		defer c.Close()
	}

	var sendDone atomic.Int64

	// 并发发送
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 20; j++ {
				dd.BroadcastCtx(ctx, Message{Type: "concurrent", Msg: "test"})
				sendDone.Add(1)
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	t.Logf("✅ 并发发送完成: %d 次", sendDone.Load())
}

// ---------------------------------------------------------------------------
// TestDistributedDispatcher_ConnectedUIDs: 验证分布式 UID 全局视图
// ---------------------------------------------------------------------------

func TestDistributedDispatcher_ConnectedUIDs(t *testing.T) {
	backend := newMockBackend(64)

	instanceA := NewDispatcher(backend)
	instanceB := NewDispatcher(backend)

	ctx := context.Background()
	instanceA.Start(ctx)
	instanceB.Start(ctx)
	defer instanceA.Stop()
	defer instanceB.Stop()

	// 实例 A: alice(2 设备), bob(1 设备)
	for _, uid := range []string{"alice", "alice", "bob"} {
		c, _ := newTestClientWithUID(t, uid)
		_ = instanceA.RegisterCtx(context.Background(), c)
		defer c.Close()
	}

	// 实例 B: charlie(1 设备), dave(1 设备)
	for _, uid := range []string{"charlie", "dave"} {
		c, _ := newTestClientWithUID(t, uid)
		_ = instanceB.RegisterCtx(context.Background(), c)
		defer c.Close()
	}

	// 等待 client_online 消息同步
	time.Sleep(200 * time.Millisecond)

	// 两个实例看到的 ConnectedUIDs 应一致（4 个唯一 UID）
	for name, dd := range map[string]*DistributedDispatcher{"instanceA": instanceA, "instanceB": instanceB} {
		uids := dd.ConnectedUIDs()
		stats := dd.Stats()
		t.Logf("🔍 %s 全局视图:", name)
		t.Logf("   UIDs=%v", uids)
		t.Logf("   TotalConnections=%d LocalConnections=%d RemoteUIDs=%d",
			stats.TotalConnections, stats.LocalConnections, stats.RemoteUIDs)

		if len(uids) != 4 {
			t.Errorf("%s ConnectedUIDs should have 4 unique UIDs, got %d: %v", name, len(uids), uids)
		}
	}

	// Len() 应包含所有连接数
	t.Logf("   instanceA.Len()=%d (local=%d + remote=%d)",
		instanceA.Len(), instanceA.LenLocal(), instanceA.Stats().RemoteUIDs)
	t.Logf("   instanceB.Len()=%d (local=%d + remote=%d)",
		instanceB.Len(), instanceB.LenLocal(), instanceB.Stats().RemoteUIDs)
}
