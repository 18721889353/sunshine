package gows

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func init() { gin.SetMode(gin.TestMode) }

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

func newTestClientPair(t testing.TB, opts ...ClientOption) (*Client, *websocket.Conn) {
	t.Helper()
	serverCh := make(chan *Client, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := (&websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil { serverCh <- nil; return }
		serverCh <- NewClient(context.Background(), raw, "test-uid", opts...)
	}))
	t.Cleanup(s.Close)
	url := "ws" + strings.TrimPrefix(s.URL, "http")
	testConn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil { t.Fatalf("dial server: %v", err) }
	t.Cleanup(func() { _ = testConn.Close() })
	client := <-serverCh
	if client == nil { t.Fatal("server failed to create client") }
	return client, testConn
}

func newTestClientWithUID(t testing.TB, uid string, opts ...ClientOption) (*Client, *websocket.Conn) {
	t.Helper()
	serverCh := make(chan *Client, 1)
	s := newTestServer(t, func(raw *websocket.Conn) { serverCh <- NewClient(context.Background(), raw, uid, opts...) })
	url := "ws" + strings.TrimPrefix(s.URL, "http")
	testConn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil { t.Fatalf("dial server: %v", err) }
	t.Cleanup(func() { _ = testConn.Close() })
	client := <-serverCh
	if client == nil { t.Fatal("server failed to create client") }
	return client, testConn
}

func newTestServer(t testing.TB, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := (&websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil { return }
		handler(raw)
	}))
	t.Cleanup(s.Close)
	return s
}

// mockBackend 模拟 Backend 实现，支持多订阅者扇出
type mockBackend struct {
	mu          sync.Mutex
	publishCh   chan *PubSubMessage
	subscribers []chan *PubSubMessage
	closed      atomic.Bool
	publishCnt  atomic.Int64
}

func newMockBackend(bufSize int) *mockBackend {
	return &mockBackend{publishCh: make(chan *PubSubMessage, bufSize)}
}

func (m *mockBackend) Publish(_ context.Context, msg *PubSubMessage) error {
	m.publishCnt.Add(1)
	m.mu.Lock()
	for _, ch := range m.subscribers {
		select { case ch <- msg: default: }
	}
	m.mu.Unlock()
	select { case m.publishCh <- msg: default: }
	return nil
}

func (m *mockBackend) ReceiveBroadcast(_ context.Context) (<-chan *PubSubMessage, error) {
	ch := make(chan *PubSubMessage, 64)
	m.mu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.mu.Unlock()
	return ch, nil
}

func (m *mockBackend) Subscribe(_ context.Context, uid string) (<-chan *PubSubMessage, error) {
	ch := make(chan *PubSubMessage, 64)
	m.mu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.mu.Unlock()
	return ch, nil
}

func (m *mockBackend) Unsubscribe(_ context.Context, uid string) error {
	return nil
}

func (m *mockBackend) Close() error {
	m.closed.Store(true)
	m.mu.Lock()
	for _, ch := range m.subscribers { close(ch) }
	m.subscribers = nil
	m.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Message 序列化
// ---------------------------------------------------------------------------

func TestMessageJSON(t *testing.T) {
	t.Parallel()
	msg := Message{Type: "notify", Msg: "hello", Data: map[string]int{"n": 42}}
	data, err := json.Marshal(msg)
	if err != nil { t.Fatalf("Marshal: %v", err) }
	if !strings.Contains(string(data), `"notify"`) {
		t.Errorf("expected type, got %s", string(data))
	}
	var m Message
	if err := json.Unmarshal(data, &m); err != nil { t.Fatalf("Unmarshal: %v", err) }
	if m.Type != "notify" { t.Errorf("Type=%q", m.Type) }
}

// ---------------------------------------------------------------------------
// Client 生命周期
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	client, testConn := newTestClientPair(t)
	defer client.Close()
	if client.UID() != "test-uid" { t.Errorf("UID=%q", client.UID()) }
	if client.RemoteAddr() == "" { t.Error("RemoteAddr empty") }

	_ = testConn.WriteMessage(websocket.TextMessage, []byte("hello"))
	data, err := client.ReadMessageCtx(context.Background())
	if err != nil { t.Fatalf("ReadMessageCtx: %v", err) }
	if string(data) != "hello" { t.Errorf("got %q, want hello", string(data)) }
}

func TestClose_Idempotent(t *testing.T) {
	client, _ := newTestClientPair(t)
	for i := 0; i < 3; i++ {
		if err := client.Close(); err != nil { t.Errorf("close #%d: %v", i, err) }
	}
}

func TestClose_ContextCancelled(t *testing.T) {
	client, _ := newTestClientPair(t)
	ctx := client.Context()
	client.Close()
	select {
	case <-ctx.Done():
	default:
		t.Error("context should be done after Close()")
	}
}

func TestSetCloseHook(t *testing.T) {
	client, _ := newTestClientPair(t)
	var called atomic.Bool
	client.SetCloseHook(func() { called.Store(true) })
	client.Close()
	if !called.Load() { t.Error("close hook not called") }
}

// ---------------------------------------------------------------------------
// 读写功能
// ---------------------------------------------------------------------------

func TestWriteJSON_Success(t *testing.T) {
	client, c := newTestClientPair(t)
	defer client.Close()

	if err := client.WriteJSONCtx(context.Background(), Message{Type: "greeting", Msg: "hi"}); err != nil {
		t.Fatalf("WriteJSONCtx: %v", err)
	}
	_, data, err := c.ReadMessage()
	if err != nil { t.Fatalf("testConn.ReadMessage: %v", err) }
	if !strings.Contains(string(data), `"greeting"`) {
		t.Errorf("expected greeting, got %s", string(data))
	}
}

func TestWriteJSON_QueueFull(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	for i := 0; i < 100; i++ {
		if err := client.WriteJSONCtx(context.Background(), Message{Type: "ping", Data: i}); err == ErrWriteQueueFull {
			return
		} else if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	}
	t.Log("queue never full (writeLoop consumed all)")
}

func TestWriteJSON_AfterClose(t *testing.T) {
	client, _ := newTestClientPair(t)
	client.Close()
	if err := client.WriteJSONCtx(context.Background(), Message{Type: "ping"}); err != websocket.ErrCloseSent {
		t.Errorf("got %v, want ErrCloseSent", err)
	}
}

func TestReadMessage_Count(t *testing.T) {
	client, c := newTestClientPair(t)
	defer client.Close()
	for i := 0; i < 5; i++ {
		_ = c.WriteMessage(websocket.TextMessage, []byte("msg"))
		if _, err := client.ReadMessageCtx(context.Background()); err != nil {
			t.Fatalf("ReadMessageCtx: %v", err)
		}
	}
	if s := client.Stats(); s.NumReceived != 5 {
		t.Errorf("NumReceived=%d, want 5", s.NumReceived)
	}
}

func BenchmarkWriteJSON(b *testing.B) {
	client, _ := newTestClientPair(b)
	defer client.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.WriteJSONCtx(context.Background(), Message{Type: "bench", Msg: "hello"})
	}
}

// ---------------------------------------------------------------------------
// 健康状态
// ---------------------------------------------------------------------------

func TestClientStats(t *testing.T) {
	client, c := newTestClientPair(t)
	defer client.Close()

	_ = client.WriteJSONCtx(context.Background(), Message{Type: "ping"})
	_ = client.WriteJSONCtx(context.Background(), Message{Type: "pong"})
	time.Sleep(50 * time.Millisecond)
	_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))
	_, _ = client.ReadMessageCtx(context.Background())

	s := client.Stats()
	if s.UID != "test-uid" { t.Errorf("UID=%q", s.UID) }
	if s.RemoteAddr == "" { t.Error("RemoteAddr empty") }
	if s.WriteQueueSize != 1024 { t.Errorf("WriteQueueSize=%d", s.WriteQueueSize) }
	if s.NumReceived != 1 { t.Errorf("NumReceived=%d, want 1", s.NumReceived) }
	if s.IsClosed { t.Error("IsClosed before Close()") }

	client.Close()
	s = client.Stats()
	if !s.IsClosed { t.Error("IsClosed false after Close()") }
}

func TestClient_IsAlive(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	if !client.IsAlive() { t.Error("new client not alive") }
	client.Close()
	if client.IsAlive() { t.Error("alive after Close()") }
}

// ---------------------------------------------------------------------------
// Dispatcher 核心功能
// ---------------------------------------------------------------------------

func TestDispatcher_Basic(t *testing.T) {
	d := NewDispatcher(nil)
	var clients []*Client
	for _, uid := range []string{"alice", "bob", "charlie"} {
		c, _ := newTestClientWithUID(t, uid)
		if err := d.RegisterCtx(context.Background(), c); err != nil { t.Fatalf("register %s: %v", uid, err) }
		clients = append(clients, c)
	}
	defer func() { for _, c := range clients { c.Close() } }()

	if n := d.Len(); n != 3 { t.Fatalf("Len=%d, want 3", n) }
	if n := len(d.Clients()); n != 3 { t.Fatalf("Clients=%d, want 3", n) }
	count := 0
	d.Range(func(c *Client) bool { count++; return true })
	if count != 3 { t.Errorf("Range count=%d, want 3", count) }
	s := d.Stats()
	if s.TotalConnections != 3 { t.Errorf("TotalConnections=%d", s.TotalConnections) }
}

func TestDispatcher_RegisterUnregister(t *testing.T) {
	d := NewDispatcher(nil)
	c, _ := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), c); err != nil { t.Fatalf("register: %v", err) }
	if d.Len() != 1 { t.Errorf("after register Len=%d", d.Len()) }
	if err := d.UnregisterCtx(context.Background(), c); err != nil { t.Fatalf("unregister: %v", err) }
	if d.Len() != 0 { t.Errorf("after unregister Len=%d", d.Len()) }
	c.Close()
}

func TestDispatcher_CleanupDeadConns(t *testing.T) {
	d := NewDispatcher(nil)
	c, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c)
	c.Close()
	time.Sleep(50 * time.Millisecond)
	if n := d.CleanupDeadConns(); n != 1 { t.Errorf("Clean=%d, want 1", n) }
}

func TestDispatcher_MaxConnections(t *testing.T) {
	d := NewDispatcher(nil, WithMaxConnections(2))
	c1, _ := newTestClientWithUID(t, "a"); _ = d.RegisterCtx(context.Background(), c1); defer c1.Close()
	c2, _ := newTestClientWithUID(t, "b"); _ = d.RegisterCtx(context.Background(), c2); defer c2.Close()
	c3, _ := newTestClientWithUID(t, "c")
	if err := d.RegisterCtx(context.Background(), c3); err != ErrMaxConnections {
		t.Errorf("got %v, want ErrMaxConnections", err)
	}
	c3.Close()
}

func TestDispatcher_SendToUID(t *testing.T) {
	d := NewDispatcher(nil)
	alice, _ := newTestClientWithUID(t, "alice"); _ = d.RegisterCtx(context.Background(), alice); defer alice.Close()
	bob, _ := newTestClientWithUID(t, "bob"); _ = d.RegisterCtx(context.Background(), bob); defer bob.Close()

	d.SendToUIDCtx(context.Background(), "alice", Message{Type: "private", Msg: "hello"})
	time.Sleep(50 * time.Millisecond)

	sA := alice.Stats(); sB := bob.Stats()
	if sA.NumSent == 0 && sA.WriteQueueLen == 0 { t.Error("alice should have message") }
	if sB.NumSent > 0 || sB.WriteQueueLen > 0 { t.Errorf("bob should NOT receive, sent=%d", sB.NumSent) }
	d.SendToUIDCtx(context.Background(), "nonexistent", Message{Type: "ghost"})
}

func TestDispatcher_BroadcastFilter(t *testing.T) {
	d := NewDispatcher(nil)
	targets := map[string]bool{"alice": true, "charlie": true}
	for uid := range targets { c, _ := newTestClientWithUID(t, uid); _ = d.RegisterCtx(context.Background(), c); defer c.Close() }
	bob, _ := newTestClientWithUID(t, "bob"); _ = d.RegisterCtx(context.Background(), bob); defer bob.Close()

	d.BroadcastFilterCtx(context.Background(),
		Message{Type: "team", Msg: "msg"},
		func(c *Client) bool { return targets[c.UID()] },
	)
	time.Sleep(50 * time.Millisecond)
	if s := bob.Stats(); s.NumSent > 0 || s.WriteQueueLen > 0 {
		t.Errorf("bob should NOT receive filtered")
	}
}

func TestDispatcher_MultiDevice(t *testing.T) {
	d := NewDispatcher(nil)
	for i := 0; i < 3; i++ {
		c, _ := newTestClientWithUID(t, "alice"); _ = d.RegisterCtx(context.Background(), c); defer c.Close()
	}
	d.SendToUIDCtx(context.Background(), "alice", Message{Type: "multi", Msg: "sync"})
	time.Sleep(50 * time.Millisecond)
	d.Range(func(c *Client) bool {
		if c.UID() == "alice" {
			s := c.Stats()
			if s.NumSent == 0 && s.WriteQueueLen == 0 { t.Errorf("alice device should have message") }
		}
		return true
	})
}

// ---------------------------------------------------------------------------
// 分布式 Dispatcher 测试（mockBackend）
// ---------------------------------------------------------------------------

func TestDistributed_SendToUID(t *testing.T) {
	backend := newMockBackend(64)
	A := NewDispatcher(backend)
	B := NewDispatcher(backend)
	ctx := context.Background()
	A.Start(ctx); B.Start(ctx)
	defer A.Stop(); defer B.Stop()

	alice, ac := newTestClientWithUID(t, "alice")
	_ = A.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, bc := newTestClientWithUID(t, "bob")
	_ = B.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	A.SendToUIDCtx(ctx, "bob", Message{Type: "greeting", Msg: "hello bob"})
	time.Sleep(100 * time.Millisecond)

	_, data, err := bc.ReadMessage()
	if err != nil {
		t.Fatalf("bob should receive: %v", err)
	}
	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Msg != "hello bob" {
		t.Errorf("bob got %q, want %q", msg.Msg, "hello bob")
	}

	ac.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := ac.ReadMessage(); err == nil {
		t.Error("alice should NOT receive")
	}
}

func TestDistributed_SendToMultiUID(t *testing.T) {
	backend := newMockBackend(64)
	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	conns := map[string]*websocket.Conn{}
	for _, uid := range []string{"alice", "bob", "charlie"} {
		c, conn := newTestClientWithUID(t, uid)
		_ = dd.RegisterCtx(context.Background(), c)
		conns[uid] = conn
		defer c.Close()
	}

	dd.SendToMultiUIDCtx(ctx, []string{"alice", "charlie"}, Message{Type: "team", Msg: "team msg"})
	time.Sleep(100 * time.Millisecond)

	for _, uid := range []string{"alice", "charlie"} {
		_, data, err := conns[uid].ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive: %v", uid, err)
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Msg != "team msg" {
			t.Errorf("%s got %q", uid, msg.Msg)
		}
	}
	conns["bob"].SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := conns["bob"].ReadMessage(); err == nil {
		t.Error("bob should NOT receive")
	}
}

func TestDistributed_Broadcast(t *testing.T) {
	backend := newMockBackend(64)
	A := NewDispatcher(backend)
	B := NewDispatcher(backend)
	ctx := context.Background()
	A.Start(ctx); B.Start(ctx)
	defer A.Stop(); defer B.Stop()

	alice, ac := newTestClientWithUID(t, "alice")
	_ = A.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, bc := newTestClientWithUID(t, "bob")
	_ = B.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	A.BroadcastCtx(ctx, Message{Type: "announce", Msg: "通知"})
	time.Sleep(100 * time.Millisecond)
	for name, conn := range map[string]*websocket.Conn{"alice": ac, "bob": bc} {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive: %v", name, err)
		}
		t.Logf("%s 收到: %s", name, string(data))
	}
}

func TestDistributed_SelfPublish(t *testing.T) {
	backend := newMockBackend(64)
	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	c, conn := newTestClientWithUID(t, "alice")
	_ = dd.RegisterCtx(context.Background(), c)
	defer c.Close()

	dd.SendToUIDCtx(ctx, "alice", Message{Type: "self", Msg: "自己发的"})
	time.Sleep(100 * time.Millisecond)
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("self-publish: %v", err)
	}
}

func TestDistributed_ConnectedUIDs(t *testing.T) {
	backend := newMockBackend(64)
	A := NewDispatcher(backend)
	B := NewDispatcher(backend)
	ctx := context.Background()
	A.Start(ctx); B.Start(ctx)
	defer A.Stop(); defer B.Stop()

	for _, uid := range []string{"alice", "alice", "bob"} {
		c, _ := newTestClientWithUID(t, uid)
		_ = A.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	for _, uid := range []string{"charlie", "dave"} {
		c, _ := newTestClientWithUID(t, uid)
		_ = B.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	time.Sleep(200 * time.Millisecond)

	for name, dd := range map[string]*DistributedDispatcher{"A": A, "B": B} {
		if uids := dd.ConnectedUIDs(); len(uids) != 4 {
			t.Errorf("%s ConnectedUIDs=%d, want 4: %v", name, len(uids), uids)
		}
	}
}

func TestDistributed_Concurrent(t *testing.T) {
	backend := newMockBackend(64)
	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	for i := 0; i < 5; i++ {
		c, _ := newTestClientWithUID(t, "user")
		_ = dd.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	var done atomic.Int64
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 20; j++ {
				dd.BroadcastCtx(ctx, Message{Type: "t", Msg: "test"})
				done.Add(1)
			}
		}()
	}
	time.Sleep(300 * time.Millisecond)
	t.Logf("concurrent send: %d", done.Load())
}

func TestDistributed_ConcurrentView(t *testing.T) {
	d := NewDispatcher(nil)
	var clients []*Client
	for i := 0; i < 5; i++ {
		c, _ := newTestClientWithUID(t, fmt.Sprintf("u-%d", i))
		if err := d.RegisterCtx(context.Background(), c); err != nil {
			t.Fatalf("register: %v", err)
		}
		clients = append(clients, c)
	}
	defer func() { for _, c := range clients { c.Close() } }()

	var send, view atomic.Int64
	for i := 0; i < 3; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				d.SendToUIDCtx(context.Background(), fmt.Sprintf("u-%d", j%5), Message{Type: "t"})
				send.Add(1)
			}
		}()
		go func() {
			for j := 0; j < 50; j++ {
				_ = d.Len(); _ = d.Clients(); _ = d.Stats()
				d.Range(func(c *Client) bool { return true })
				view.Add(1)
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	t.Logf("concurrent: send=%d view=%d online=%d", send.Load(), view.Load(), d.Len())
}

func TestDistributed_Integration(t *testing.T) {
	backend := newMockBackend(64)
	instances := []*DistributedDispatcher{NewDispatcher(backend), NewDispatcher(backend)}
	ctx := context.Background()
	for _, dd := range instances { dd.Start(ctx); defer dd.Stop() }

	type uc struct { client *Client; conn *websocket.Conn }
	users := map[string]*uc{}
	for i, name := range []string{"alice", "bob", "charlie", "dave"} {
		c, conn := newTestClientWithUID(t, name)
		users[name] = &uc{c, conn}
		_ = instances[i/2].RegisterCtx(context.Background(), c)
		defer c.Close()
	}

	instances[0].SendToUIDCtx(ctx, "charlie", Message{Type: "p2p", Msg: "from 0"})
	instances[1].BroadcastCtx(ctx, Message{Type: "notice", Msg: "ann"})
	time.Sleep(150 * time.Millisecond)

	received := map[string]int{}
	for _, u := range users {
		for {
			u.conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
			if _, _, err := u.conn.ReadMessage(); err != nil { break } else { received[u.client.UID()]++ }
		}
	}
	if received["charlie"] < 2 { t.Errorf("charlie got %d, want >=2", received["charlie"]) }
	if received["dave"] < 1 { t.Errorf("dave got %d, want >=1", received["dave"]) }
}

// ---------------------------------------------------------------------------
// 集成测试 — 全生命周期 & 分布式消息 & 并发一致性
// ---------------------------------------------------------------------------

func TestIntegration_WebSocketFullLifecycle(t *testing.T) {
	d := NewDispatcher(nil)
	heartStarted := make(chan struct{})

	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		client, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return true }),
			WithClientUID("test-user"),
			WithHeartbeat(),
			WithEnableDistributed(true),
			WithDispatcher(d),
		)
		if err != nil {
			t.Errorf("Upgrade failed: %v", err)
			return
		}

		close(heartStarted)

		for {
			_, err := client.ReadMessageCtx(context.Background())
			if err != nil {
				return
			}
		}
	})

	s := httptest.NewServer(r)
	defer s.Close()

	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Origin": {"http://trusted.com"},
	})
	if err != nil {
		t.Fatalf("WebSocket Dial 失败: %v", err)
	}
	defer conn.Close()

	select {
	case <-heartStarted:
	case <-time.After(time.Second):
		t.Fatal("心跳未在 1s 内启动")
	}

	time.Sleep(100 * time.Millisecond)
	if n := d.Len(); n != 1 {
		t.Errorf("Dispatcher.Len() = %d, want 1", n)
	}

	var client *Client
	d.Range(func(c *Client) bool {
		client = c
		return false
	})
	if client == nil {
		t.Fatal("Dispatcher 中应有 Client")
	}

	if client.UID() == "" {
		t.Error("UID 不应为空")
	}
	if client.RemoteAddr() == "" {
		t.Error("RemoteAddr 不应为空")
	}
	if !client.IsAlive() {
		t.Error("新连接应处于存活状态")
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping","msg":"hello"}`)); err != nil {
		t.Fatalf("发送消息失败: %v", err)
	}

	msg := Message{Type: "notify", Msg: "server push"}
	d.SendToUIDCtx(context.Background(), client.UID(), msg)

	time.Sleep(50 * time.Millisecond)

	stats := client.Stats()
	t.Logf("📊 Client Stats:")
	t.Logf("   UID=%s, RemoteAddr=%s", stats.UID, stats.RemoteAddr)
	t.Logf("   NumSent=%d, NumReceived=%d, WriteQueueLen=%d", stats.NumSent, stats.NumReceived, stats.WriteQueueLen)
	t.Logf("   IsAlive=%v, IsClosed=%v", stats.IsAlive, stats.IsClosed)
	t.Logf("   LastWriteTime=%s, LastReadTime=%s", stats.LastWriteTime, stats.LastReadTime)

	if stats.UID != client.UID() {
		t.Errorf("Stats.UID = %q, want %q", stats.UID, client.UID())
	}

	dStats := d.Stats()
	t.Logf("📊 Dispatcher Stats:")
	t.Logf("   TotalConnections=%d, LocalConnections=%d", dStats.TotalConnections, dStats.LocalConnections)
	if dStats.TotalConnections != 1 {
		t.Errorf("TotalConnections = %d, want 1", dStats.TotalConnections)
	}

	client.Close()

	if client.IsAlive() {
		t.Error("Close 后 IsAlive 应为 false")
	}
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Error("Close 后 Done 应在 1s 内触发")
	}
	if client.Context().Err() == nil {
		t.Error("Close 后 Context 应返回错误")
	}

	time.Sleep(100 * time.Millisecond)
	if n := d.Len(); n != 0 {
		t.Errorf("Close 后 Dispatcher.Len() = %d, want 0", n)
	}

	t.Log("✅ WebSocket 全生命周期集成测试通过")
}

func TestIntegration_DistributedMessaging(t *testing.T) {
	backend := newMockBackend(64)

	instanceA := NewDispatcher(backend)
	instanceB := NewDispatcher(backend)

	ctx := context.Background()
	instanceA.Start(ctx)
	instanceB.Start(ctx)
	defer instanceA.Stop()
	defer instanceB.Stop()

	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = instanceA.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = instanceA.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	charlie, charlieConn := newTestClientWithUID(t, "charlie")
	_ = instanceB.RegisterCtx(context.Background(), charlie)
	defer charlie.Close()

	dave, daveConn := newTestClientWithUID(t, "dave")
	_ = instanceB.RegisterCtx(context.Background(), dave)
	defer dave.Close()

	time.Sleep(150 * time.Millisecond)

	uidsA := instanceA.ConnectedUIDs()
	uidsB := instanceB.ConnectedUIDs()
	if len(uidsA) != 4 || len(uidsB) != 4 {
		t.Errorf("ConnectedUIDs: A=%d, B=%d, want both=4", len(uidsA), len(uidsB))
	}

	instanceA.SendToUIDCtx(ctx, "charlie", Message{Type: "p2p", Msg: "from A to charlie"})
	instanceB.SendToUIDCtx(ctx, "alice", Message{Type: "p2p", Msg: "from B to alice"})
	instanceB.BroadcastCtx(ctx, Message{Type: "notice", Msg: "global notice"})

	time.Sleep(200 * time.Millisecond)

	received := map[string]int{}
	for name, conn := range map[string]*websocket.Conn{
		"alice": aliceConn, "bob": bobConn,
		"charlie": charlieConn, "dave": daveConn,
	} {
		for {
			conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}
			received[name]++
			t.Logf("📬 %s 收到: %s", name, string(data))
		}
	}

	t.Log("📊 分布式消息统计:")
	for name, count := range received {
		t.Logf("   %s: %d 条", name, count)
	}

	if received["alice"] < 2 {
		t.Errorf("alice 应收到 >=2 条, 实际 %d", received["alice"])
	}
	if received["charlie"] < 2 {
		t.Errorf("charlie 应收到 >=2 条, 实际 %d", received["charlie"])
	}
	if received["bob"] < 1 {
		t.Errorf("bob 应收到 >=1 条, 实际 %d", received["bob"])
	}
	if received["dave"] < 1 {
		t.Errorf("dave 应收到 >=1 条, 实际 %d", received["dave"])
	}

	t.Log("✅ 分布式消息集成测试通过")
}

func TestIntegration_ConcurrentSendAndStats(t *testing.T) {
	d := NewDispatcher(nil)

	var clients []*Client
	for i := 0; i < 3; i++ {
		c, _ := newTestClientWithUID(t, "user")
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

	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				d.BroadcastCtx(context.Background(), Message{Type: "concurrent", Msg: "test"})
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)

	statsList := make([]ClientStats, 0, len(clients))
	d.Range(func(c *Client) bool {
		statsList = append(statsList, c.Stats())
		return true
	})

	t.Log("📊 并发后各连接 Stats:")
	for i, s := range statsList {
		t.Logf("   [%d] Sent=%d, QueueLen=%d, QueueSize=%d, Alive=%v",
			i, s.NumSent, s.WriteQueueLen, s.WriteQueueSize, s.IsAlive)
	}

	dStats := d.Stats()
	t.Logf("📊 Dispatcher Stats: TotalConnections=%d", dStats.TotalConnections)
	if int(dStats.TotalConnections) != len(clients) {
		t.Errorf("TotalConnections=%d, want %d", dStats.TotalConnections, len(clients))
	}

	_, _ = json.Marshal(dStats)
	t.Log("✅ 并发发送 + Stats 一致性测试通过")
}
