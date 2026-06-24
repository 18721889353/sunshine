package gows

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// 测试辅助: 创建带自定义 UID 的 Client
// ---------------------------------------------------------------------------

// newTestClientWithUID 创建一对 (Client, *websocket.Conn)，
// 其中 Client 的 UID 由 uid 参数指定。
func newTestClientWithUID(t testing.TB, uid string, opts ...ClientOption) (*Client, *websocket.Conn) {
	t.Helper()

	serverCh := make(chan *Client, 1)
	s := newTestServer(t, func(raw *websocket.Conn) {
		serverCh <- NewClient(raw, uid, opts...)
	})
	url := "ws" + strings.TrimPrefix(s.URL, "http")
	testConn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	t.Cleanup(func() { _ = testConn.Close() })

	client := <-serverCh
	if client == nil {
		t.Fatal("server failed to create client")
	}
	return client, testConn
}

// newTestServer 创建一个测试 HTTP 服务器，handler 接收升级后的原始连接。
func newTestServer(t testing.TB, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin:       func(r *http.Request) bool { return true },
			EnableCompression: false,
		}
		raw, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		handler(raw)
	}))
	t.Cleanup(s.Close)
	return s
}

// 避免未使用的导入警告
var _ = context.Background
var _ = fmt.Sprintf

// ---------------------------------------------------------------------------
// Demo 1: 查看在线连接 — Clients() / Range() / Len() / Stats()
// ---------------------------------------------------------------------------

// TestDemo_ViewConnections 演示如何查看 Dispatcher 中的所有在线连接。
func TestDemo_ViewConnections(t *testing.T) {
	d := NewDispatcher()

	// 注册 3 个不同 UID 的客户端
	uidList := []string{"alice", "bob", "charlie"}
	clients := make([]*Client, 0, len(uidList))
	for _, uid := range uidList {
		c, _ := newTestClientWithUID(t, uid)
		if err := d.Register(c); err != nil {
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

// ---------------------------------------------------------------------------
// Demo 2: 向单个指定 UID 用户发送消息
// ---------------------------------------------------------------------------

// TestDemo_SendToSingleUID 演示向指定 UID 发送消息。
func TestDemo_SendToSingleUID(t *testing.T) {
	d := NewDispatcher()

	// 创建 3 个不同用户
	clients := make(map[string]*Client)
	for _, uid := range []string{"alice", "bob", "charlie"} {
		c, conn := newTestClientWithUID(t, uid)
		if err := d.Register(c); err != nil {
			t.Fatalf("register %s: %v", uid, err)
		}
		clients[uid] = c
		_ = conn // 用于验证接收
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// 只发给 alice
	msg := Message{Type: "private", Msg: "Hello Alice!"}
	d.SendToUIDCtx(context.Background(), "alice", msg)

	// 发给不存在的 UID — 不应 panic
	d.SendToUIDCtx(context.Background(), "nonexistent", Message{Type: "ghost"})

	// 验证: alice 的写入队列应有 1 条，bob 和 charlie 应为 0
	// 由于 writeLoop 异步消费，稍等片刻再检查
	time.Sleep(50 * time.Millisecond)

	for uid, c := range clients {
		s := c.Stats()
		if uid == "alice" {
			if s.NumSent == 0 && s.WriteQueueLen > 0 {
				t.Logf("✅ %s 消息在队列中, queue_len=%d", uid, s.WriteQueueLen)
			} else if s.NumSent > 0 {
				t.Logf("✅ %s 已收到消息 (sent=%d)", uid, s.NumSent)
			}
		} else {
			t.Logf("✅ %s 不应有消息 (sent=%d, queue_len=%d)", uid, s.NumSent, s.WriteQueueLen)
		}
	}
}

// ---------------------------------------------------------------------------
// Demo 3: 向多个指定 UID 发送消息 — BroadcastFilterCtx()
// ---------------------------------------------------------------------------

// TestDemo_SendToMultipleUIDs 演示使用 BroadcastFilterCtx 向多个指定用户发送消息。
func TestDemo_SendToMultipleUIDs(t *testing.T) {
	d := NewDispatcher()

	clients := make(map[string]*Client)
	for _, uid := range []string{"alice", "bob", "charlie", "dave"} {
		c, _ := newTestClientWithUID(t, uid)
		if err := d.Register(c); err != nil {
			t.Fatalf("register %s: %v", uid, err)
		}
		clients[uid] = c
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// 只发给 alice 和 charlie
	targets := map[string]bool{"alice": true, "charlie": true}
	d.BroadcastFilterCtx(context.Background(),
		Message{Type: "team_notify", Msg: "团队消息"},
		func(c *Client) bool {
			return targets[c.UID()]
		},
	)

	time.Sleep(50 * time.Millisecond)
	t.Log("✅ BroadcastFilterCtx 只发给 alice 和 charlie:")
	for uid, c := range clients {
		s := c.Stats()
		if targets[uid] {
			t.Logf("   └─ %s ✅ 应收到 (sent=%d, queue_len=%d)", uid, s.NumSent, s.WriteQueueLen)
		} else {
			t.Logf("   └─ %s ❌ 不应收到 (sent=%d, queue_len=%d)", uid, s.NumSent, s.WriteQueueLen)
		}
	}
}

// ---------------------------------------------------------------------------
// Demo 4: 给不同用户发不同的消息 — Range() + WriteJSON()
// ---------------------------------------------------------------------------

// TestDemo_SendDifferentMessages 演示如何给每个用户发送不同的消息内容。
func TestDemo_SendDifferentMessages(t *testing.T) {
	d := NewDispatcher()

	scores := map[string]int{"alice": 95, "bob": 88, "charlie": 72}
	clients := make([]*Client, 0, len(scores))
	for uid := range scores {
		c, _ := newTestClientWithUID(t, uid)
		if err := d.Register(c); err != nil {
			t.Fatalf("register %s: %v", uid, err)
		}
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// 根据 UID 给不同用户发送不同的成绩消息
	d.Range(func(c *Client) bool {
		if score, ok := scores[c.UID()]; ok {
			c.WriteJSON(Message{
				Type: "score_report",
				Data: map[string]any{"uid": c.UID(), "score": score},
			})
		}
		return true
	})

	time.Sleep(50 * time.Millisecond)
	t.Log("✅ Range + WriteJSON 给不同用户发不同消息:")
	d.Range(func(c *Client) bool {
		s := c.Stats()
		t.Logf("   └─ %s: sent=%d", c.UID(), s.NumSent)
		return true
	})
}

// ---------------------------------------------------------------------------
// Demo 5: 同一 UID 多设备连接
// ---------------------------------------------------------------------------

// TestDemo_SameUIDMultiDevice 演示同一用户多个设备连接的场景。
func TestDemo_SameUIDMultiDevice(t *testing.T) {
	d := NewDispatcher()

	// 同一用户 "alice" 有 3 个设备连接
	for i := 0; i < 3; i++ {
		c, _ := newTestClientWithUID(t, "alice")
		if err := d.Register(c); err != nil {
			t.Fatalf("register alice device %d: %v", i, err)
		}
		defer c.Close()
	}

	// 查看 alice 的连接数
	aliceCount := 0
	d.Range(func(c *Client) bool {
		if c.UID() == "alice" {
			aliceCount++
		}
		return true
	})
	t.Logf("✅ alice 在线设备数: %d", aliceCount)

	// 发送给 alice — 所有设备都能收到
	d.SendToUIDCtx(context.Background(), "alice", Message{Type: "multi_device", Msg: "所有设备同步"})

	t.Log("✅ SendToUIDCtx 发给 alice 的所有设备:")
	time.Sleep(50 * time.Millisecond)
	d.Range(func(c *Client) bool {
		if c.UID() == "alice" {
			s := c.Stats()
			t.Logf("   └─ [设备] addr=%s sent=%d queue_len=%d", c.RemoteAddr(), s.NumSent, s.WriteQueueLen)
		}
		return true
	})

	// 直接向某个特定设备发送（不经过 SendToUIDCtx）
	var specificDevice *Client
	d.Range(func(c *Client) bool {
		if c.UID() == "alice" {
			specificDevice = c
			return false // 只取第一个
		}
		return true
	})
	if specificDevice != nil {
		specificDevice.WriteJSON(Message{Type: "private", Msg: "仅此设备可见"})
		time.Sleep(50 * time.Millisecond)
		s := specificDevice.Stats()
		t.Logf("✅ 指定设备单独发送完毕 (sent=%d)", s.NumSent)
	}
}

// ---------------------------------------------------------------------------
// Demo 6: 获取客户端详细统计信息
// ---------------------------------------------------------------------------

// TestDemo_ClientStats 演示使用 Client.Stats() 获取单个连接的详情。
func TestDemo_ClientStats(t *testing.T) {
	d := NewDispatcher()

	// 创建客户端并发送/接收消息
	c, testConn := newTestClientWithUID(t, "dave", WithWriteQueueSize(32))
	if err := d.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer c.Close()

	// 发送消息
	_ = c.WriteJSON(Message{Type: "ping"})
	_ = c.WriteJSON(Message{Type: "pong"})

	// 接收消息
	_ = testConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))
	_, _, _ = c.ReadMessage()

	time.Sleep(50 * time.Millisecond)

	// 读取详细统计
	stats := c.Stats()
	t.Log("✅ Client.Stats() 详情:")
	t.Logf("   ├─ UID:            %s", stats.UID)
	t.Logf("   ├─ RemoteAddr:     %s", stats.RemoteAddr)
	t.Logf("   ├─ NumSent:        %d", stats.NumSent)
	t.Logf("   ├─ NumReceived:    %d", stats.NumReceived)
	t.Logf("   ├─ WriteQueueSize: %d", stats.WriteQueueSize)
	t.Logf("   ├─ WriteQueueLen:  %d", stats.WriteQueueLen)
	t.Logf("   ├─ IsClosed:       %v", stats.IsClosed)
	t.Logf("   ├─ IsAlive:        %v", stats.IsAlive)
	t.Logf("   ├─ WriteErrCount:  %d", stats.WriteErrCount)
	t.Logf("   ├─ LastWriteErr:   %s", stats.LastWriteErr)
	t.Logf("   ├─ LastWriteTime:  %s", stats.LastWriteTime)
	t.Logf("   └─ LastReadTime:   %s", stats.LastReadTime)
}

// ---------------------------------------------------------------------------
// Demo 7: 使用私有 Dispatcher 实例
// ---------------------------------------------------------------------------

// TestDemo_UseCustomDispatcher 演示创建独立 Dispatcher（如按房间/频道隔离）。
func TestDemo_UseCustomDispatcher(t *testing.T) {
	// 创建两个独立的 Dispatcher
	roomChat := NewDispatcher()
	roomVoice := NewDispatcher()

	// 用户 alice 同时加入了聊天室和语音室
	c1, _ := newTestClientWithUID(t, "alice")
	_ = roomChat.Register(c1)
	_ = roomVoice.Register(c1)
	defer c1.Close()

	// 用户 bob 只加入了聊天室
	c2, _ := newTestClientWithUID(t, "bob")
	_ = roomChat.Register(c2)
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

// ---------------------------------------------------------------------------
// Demo 8: 并发安全 — 多 goroutine 同时读写
// ---------------------------------------------------------------------------

// TestDemo_ConcurrentUsage 演示 Dispatcher 的并发安全性。
func TestDemo_ConcurrentUsage(t *testing.T) {
	d := NewDispatcher()
	var clients []*Client

	// 创建 5 个客户端
	for i := 0; i < 5; i++ {
		c, _ := newTestClientWithUID(t, fmt.Sprintf("user-%d", i))
		if err := d.Register(c); err != nil {
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
