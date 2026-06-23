package gows

import (
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// 测试辅助: 为 Dispatcher 测试创建轻量 Client
// ---------------------------------------------------------------------------

// newDispatcherTestClients 创建指定数量的测试用 Client（使用唯一的 testConn）。
// 返回的 clients 可通过各自的 Close() 清理。
func newDispatcherTestClients(t *testing.T, n int) []*Client {
	t.Helper()
	clients := make([]*Client, 0, n)
	for i := 0; i < n; i++ {
		client, _ := newTestClientPair(t)
		clients = append(clients, client)
	}
	return clients
}

// ---------------------------------------------------------------------------
// TestNewDispatcher
// ---------------------------------------------------------------------------

func TestNewDispatcher(t *testing.T) {
	d := NewDispatcher()
	if d == nil {
		t.Fatal("NewDispatcher() returned nil")
	}
	if d.Len() != 0 {
		t.Errorf("initial Len() = %d, want 0", d.Len())
	}
}

// ---------------------------------------------------------------------------
// TestDefaultDispatcher
// ---------------------------------------------------------------------------

func TestDefaultDispatcher(t *testing.T) {
	if DefaultDispatcher == nil {
		t.Fatal("DefaultDispatcher is nil")
	}
}

// ---------------------------------------------------------------------------
// TestRegisterAndUnregister
// ---------------------------------------------------------------------------

func TestRegisterAndUnregister(t *testing.T) {
	d := NewDispatcher()
	clients := newDispatcherTestClients(t, 3)

	// 注册
	for _, c := range clients {
		d.Register(c)
	}
	if d.Len() != 3 {
		t.Errorf("after register Len() = %d, want 3", d.Len())
	}

	// 注销
	d.Unregister(clients[0])
	if d.Len() != 2 {
		t.Errorf("after unregister Len() = %d, want 2", d.Len())
	}

	// 重复注销不应 panic
	d.Unregister(clients[0])
	if d.Len() != 2 {
		t.Errorf("after double unregister Len() = %d, want 2", d.Len())
	}

	// 清理
	for _, c := range clients {
		c.Close()
	}
}

// ---------------------------------------------------------------------------
// TestLen
// ---------------------------------------------------------------------------

func TestDispatcher_Len(t *testing.T) {
	d := NewDispatcher()
	clients := newDispatcherTestClients(t, 5)

	for _, c := range clients {
		d.Register(c)
	}
	if d.Len() != 5 {
		t.Errorf("Len() = %d, want 5", d.Len())
	}

	// 逐个注销并验证
	for i, c := range clients {
		d.Unregister(c)
		want := 4 - i
		if got := d.Len(); got != want {
			t.Errorf("after unregister %d: Len() = %d, want %d", i, got, want)
		}
	}

	for _, c := range clients {
		c.Close()
	}
}

// ---------------------------------------------------------------------------
// TestClients
// ---------------------------------------------------------------------------

func TestDispatcher_Clients(t *testing.T) {
	d := NewDispatcher()
	clients := newDispatcherTestClients(t, 3)
	for _, c := range clients {
		d.Register(c)
	}

	snapshot := d.Clients()
	if len(snapshot) != 3 {
		t.Errorf("Clients() len = %d, want 3", len(snapshot))
	}

	// 验证快照中的指针都在原始列表中
	m := make(map[*Client]bool)
	for _, c := range clients {
		m[c] = true
	}
	for _, c := range snapshot {
		if !m[c] {
			t.Error("Clients() returned unknown client")
		}
	}

	for _, c := range clients {
		c.Close()
	}
}

// ---------------------------------------------------------------------------
// TestBroadcast
// ---------------------------------------------------------------------------

func TestBroadcast(t *testing.T) {
	d := NewDispatcher()
	clients := newDispatcherTestClients(t, 3)
	for _, c := range clients {
		d.Register(c)
	}

	// Broadcast 发送消息，不应 panic
	d.Broadcast(Message{Type: "broadcast", Msg: "hello all"})

	for _, c := range clients {
		c.Close()
	}
}

// ---------------------------------------------------------------------------
// TestBroadcastFilter
// ---------------------------------------------------------------------------

func TestBroadcastFilter(t *testing.T) {
	d := NewDispatcher()
	clients := newDispatcherTestClients(t, 4)
	for _, c := range clients {
		d.Register(c)
	}

	d.BroadcastFilter(Message{Type: "notice"}, func(c *Client) bool {
		// 所有 test client 都是 "test-uid"，4 个都满足条件
		return c.UID() == "test-uid"
	})

	for _, c := range clients {
		c.Close()
	}
}

// ---------------------------------------------------------------------------
// TestSendToUID
// ---------------------------------------------------------------------------

func TestSendToUID(t *testing.T) {
	d := NewDispatcher()
	clients := newDispatcherTestClients(t, 5)
	for _, c := range clients {
		d.Register(c)
	}

	// 所有 client UID 都是 "test-uid"，应该发给所有 5 个
	d.SendToUID("test-uid", Message{Type: "personal", Msg: "hi"})

	// 发送给不存在的 UID，不应 panic
	d.SendToUID("nonexistent", Message{Type: "ghost"})

	for _, c := range clients {
		c.Close()
	}
}

// ---------------------------------------------------------------------------
// TestRange
// ---------------------------------------------------------------------------

func TestDispatcher_Range(t *testing.T) {
	d := NewDispatcher()
	clients := newDispatcherTestClients(t, 5)
	for _, c := range clients {
		d.Register(c)
	}

	var count atomic.Int32
	d.Range(func(c *Client) bool {
		count.Add(1)
		return true // 继续遍历
	})
	if n := int(count.Load()); n != 5 {
		t.Errorf("Range visited %d clients, want 5", n)
	}

	// 测试中途停止
	var partial atomic.Int32
	d.Range(func(c *Client) bool {
		partial.Add(1)
		return partial.Load() < 2 // 只访问前 2 个
	})
	if n := int(partial.Load()); n != 2 {
		t.Errorf("partial Range visited %d clients, want 2", n)
	}

	for _, c := range clients {
		c.Close()
	}
}

// ---------------------------------------------------------------------------
// TestStats
// ---------------------------------------------------------------------------

func TestDispatcher_Stats(t *testing.T) {
	d := NewDispatcher()
	clients := newDispatcherTestClients(t, 3)
	for _, c := range clients {
		d.Register(c)
	}

	stats := d.Stats()
	if stats.TotalConnections != 3 {
		t.Errorf("TotalConnections = %d, want %d", stats.TotalConnections, 3)
	}

	for _, c := range clients {
		d.Unregister(c)
		c.Close()
	}

	stats = d.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("after cleanup TotalConnections = %d, want 0", stats.TotalConnections)
	}
}

// ---------------------------------------------------------------------------
// TestDispatcher_GoroutineSafe
// ---------------------------------------------------------------------------

func TestDispatcher_GoroutineSafe(t *testing.T) {
	d := NewDispatcher()
	clients := newDispatcherTestClients(t, 10)
	for _, c := range clients {
		d.Register(c)
	}

	// 并发读写
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			d.Broadcast(Message{Type: "ping"})
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			d.Len()
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			d.Clients()
		}
		done <- struct{}{}
	}()

	for i := 0; i < 3; i++ {
		<-done
	}

	for _, c := range clients {
		c.Close()
	}
}
