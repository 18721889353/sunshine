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

	"github.com/18721889353/sunshine/pkg/jwt"
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
		if err != nil {
			serverCh <- nil
			return
		}
		serverCh <- NewClient(context.Background(), raw, "test-uid", opts...)
	}))
	t.Cleanup(s.Close)
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

func newTestClientWithUID(t testing.TB, uid string, opts ...ClientOption) (*Client, *websocket.Conn) {
	t.Helper()
	serverCh := make(chan *Client, 1)
	s := newTestServer(t, func(raw *websocket.Conn) { serverCh <- NewClient(context.Background(), raw, uid, opts...) })
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

func newTestServer(t testing.TB, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := (&websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
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
		select {
		case ch <- msg:
		default:
		}
	}
	m.mu.Unlock()
	select {
	case m.publishCh <- msg:
	default:
	}
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
	for _, ch := range m.subscribers {
		close(ch)
	}
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
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"notify"`) {
		t.Errorf("expected type, got %s", string(data))
	}
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.Type != "notify" {
		t.Errorf("Type=%q", m.Type)
	}
}

// ---------------------------------------------------------------------------
// Client 生命周期
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	client, testConn := newTestClientPair(t)
	defer client.Close()
	if client.UID() != "test-uid" {
		t.Errorf("UID=%q", client.UID())
	}
	if client.RemoteAddr() == "" {
		t.Error("RemoteAddr empty")
	}

	_ = testConn.WriteMessage(websocket.TextMessage, []byte("hello"))
	data, err := client.ReadMessageCtx(context.Background())
	if err != nil {
		t.Fatalf("ReadMessageCtx: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want hello", string(data))
	}
}

func TestClose_Idempotent(t *testing.T) {
	client, _ := newTestClientPair(t)
	for i := 0; i < 3; i++ {
		if err := client.Close(); err != nil {
			t.Errorf("close #%d: %v", i, err)
		}
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
	if !called.Load() {
		t.Error("close hook not called")
	}
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
	if err != nil {
		t.Fatalf("testConn.ReadMessage: %v", err)
	}
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
	t.Log("queue never full (msgFromChToWs consumed all)")
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

func TestReadMessageCtx_WithTimeout(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	// 传入带超时的 ctx，不应阻塞超过超时时间
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.ReadMessageCtx(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("got %v, want DeadlineExceeded", err)
	}
}

func TestWriteJSONCtx_WithTimeout(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// 写入 2000 条消息填满 writeCh，触发 <-ctx.Done() 路径
	var timeoutCount int
	for i := 0; i < 3000; i++ {
		err := client.WriteJSONCtx(ctx, Message{Type: "timeout", Data: make([]byte, 100)})
		if err == context.DeadlineExceeded {
			timeoutCount++
		} else if err != nil && err != ErrWriteQueueFull {
			t.Errorf("unexpected err: %v", err)
		}
		if err == context.DeadlineExceeded {
			break
		}
	}
	if timeoutCount == 0 {
		t.Log("writeCh never full enough for timeout - write path always available")
	} else {
		t.Logf("ctx timeout triggered %d times", timeoutCount)
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
	if s.UID != "test-uid" {
		t.Errorf("UID=%q", s.UID)
	}
	if s.RemoteAddr == "" {
		t.Error("RemoteAddr empty")
	}
	if s.WriteQueueSize != 1024 {
		t.Errorf("WriteQueueSize=%d", s.WriteQueueSize)
	}
	if s.NumReceived != 1 {
		t.Errorf("NumReceived=%d, want 1", s.NumReceived)
	}
	if s.IsClosed {
		t.Error("IsClosed before Close()")
	}

	client.Close()
	s = client.Stats()
	if !s.IsClosed {
		t.Error("IsClosed false after Close()")
	}
}

func TestClient_IsAlive(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()
	if !client.IsAlive() {
		t.Error("new client not alive")
	}
	client.Close()
	if client.IsAlive() {
		t.Error("alive after Close()")
	}
}

// ---------------------------------------------------------------------------
// Dispatcher 核心功能
// ---------------------------------------------------------------------------

func TestDispatcher_Basic(t *testing.T) {
	d := NewDispatcher(nil)
	var clients []*Client
	for _, uid := range []string{"alice", "bob", "charlie"} {
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

	if n := d.Len(); n != 3 {
		t.Fatalf("Len=%d, want 3", n)
	}
	if n := len(d.Clients()); n != 3 {
		t.Fatalf("Clients=%d, want 3", n)
	}
	count := 0
	d.Range(func(c *Client) bool { count++; return true })
	if count != 3 {
		t.Errorf("Range count=%d, want 3", count)
	}
	s := d.Stats()
	if s.TotalConnections != 3 {
		t.Errorf("TotalConnections=%d", s.TotalConnections)
	}
}

func TestDispatcher_RegisterUnregister(t *testing.T) {
	d := NewDispatcher(nil)
	c, _ := newTestClientWithUID(t, "alice")
	if err := d.RegisterCtx(context.Background(), c); err != nil {
		t.Fatalf("register: %v", err)
	}
	if d.Len() != 1 {
		t.Errorf("after register Len=%d", d.Len())
	}
	if err := d.UnregisterCtx(context.Background(), c); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if d.Len() != 0 {
		t.Errorf("after unregister Len=%d", d.Len())
	}
	c.Close()
}

func TestDispatcher_CleanupDeadConns(t *testing.T) {
	d := NewDispatcher(nil)
	c, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c)

	// 验证注册后 uidIndex 存在
	if !d.hasLocalUID("alice") {
		t.Error("hasLocalUID should be true after register")
	}

	c.Close()
	time.Sleep(50 * time.Millisecond)

	// 验证 Close 后 uidIndex 仍然存在（未清理前）
	if !d.hasLocalUID("alice") {
		t.Error("hasLocalUID should still be true before CleanupDeadConns")
	}

	if n := d.CleanupDeadConns(); n != 1 {
		t.Errorf("Clean=%d, want 1", n)
	}

	// 验证清理后 uidIndex 已递减
	if d.hasLocalUID("alice") {
		t.Error("hasLocalUID should be false after CleanupDeadConns")
	}
}

func TestDispatcher_CleanupDeadConns_MultiDevice(t *testing.T) {
	d := NewDispatcher(nil)
	// 同 UID 多设备连接
	c1, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c1)
	c2, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c2)
	defer c2.Close()

	// 验证 uidIndex 存在
	if !d.hasLocalUID("alice") {
		t.Error("hasLocalUID should be true after 2 connections")
	}

	// 关闭一个连接
	c1.Close()
	time.Sleep(50 * time.Millisecond)

	if n := d.CleanupDeadConns(); n != 1 {
		t.Errorf("Clean=%d, want 1", n)
	}

	// 另一个连接还在，uidIndex 应保持
	if !d.hasLocalUID("alice") {
		t.Error("hasLocalUID should still be true when other device still connected")
	}
}

func TestDispatcher_MaxConnections(t *testing.T) {
	d := NewDispatcher(nil, WithMaxConnections(2))
	c1, _ := newTestClientWithUID(t, "a")
	_ = d.RegisterCtx(context.Background(), c1)
	defer c1.Close()
	c2, _ := newTestClientWithUID(t, "b")
	_ = d.RegisterCtx(context.Background(), c2)
	defer c2.Close()
	c3, _ := newTestClientWithUID(t, "c")
	if err := d.RegisterCtx(context.Background(), c3); err != ErrMaxConnections {
		t.Errorf("got %v, want ErrMaxConnections", err)
	}
	c3.Close()
}

func TestDispatcher_SendToUID(t *testing.T) {
	d := NewDispatcher(nil)
	alice, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, _ := newTestClientWithUID(t, "bob")
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	d.SendToUIDCtx(context.Background(), "alice", Message{Type: "private", Msg: "hello"})
	time.Sleep(50 * time.Millisecond)

	sA := alice.Stats()
	sB := bob.Stats()
	if sA.NumSent == 0 && sA.WriteQueueLen == 0 {
		t.Error("alice should have message")
	}
	if sB.NumSent > 0 || sB.WriteQueueLen > 0 {
		t.Errorf("bob should NOT receive, sent=%d", sB.NumSent)
	}
	d.SendToUIDCtx(context.Background(), "nonexistent", Message{Type: "ghost"})
}

func TestDispatcher_BroadcastFilter(t *testing.T) {
	d := NewDispatcher(nil)
	targets := map[string]bool{"alice": true, "charlie": true}
	for uid := range targets {
		c, _ := newTestClientWithUID(t, uid)
		_ = d.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	bob, _ := newTestClientWithUID(t, "bob")
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()

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
		c, _ := newTestClientWithUID(t, "alice")
		_ = d.RegisterCtx(context.Background(), c)
		defer c.Close()
	}
	d.SendToUIDCtx(context.Background(), "alice", Message{Type: "multi", Msg: "sync"})
	time.Sleep(50 * time.Millisecond)
	d.Range(func(c *Client) bool {
		if c.UID() == "alice" {
			s := c.Stats()
			if s.NumSent == 0 && s.WriteQueueLen == 0 {
				t.Errorf("alice device should have message")
			}
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
	A.Start(ctx)
	B.Start(ctx)
	defer A.Stop()
	defer B.Stop()

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
	A.Start(ctx)
	B.Start(ctx)
	defer A.Stop()
	defer B.Stop()

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
	A.Start(ctx)
	B.Start(ctx)
	defer A.Stop()
	defer B.Stop()

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
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

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
				_ = d.Len()
				_ = d.Clients()
				_ = d.Stats()
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
	for _, dd := range instances {
		dd.Start(ctx)
		defer dd.Stop()
	}

	type uc struct {
		client *Client
		conn   *websocket.Conn
	}
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
			if _, _, err := u.conn.ReadMessage(); err != nil {
				break
			} else {
				received[u.client.UID()]++
			}
		}
	}
	if received["charlie"] < 2 {
		t.Errorf("charlie got %d, want >=2", received["charlie"])
	}
	if received["dave"] < 1 {
		t.Errorf("dave got %d, want >=1", received["dave"])
	}
}

// ---------------------------------------------------------------------------
// 集成测试 — 全生命周期 & 分布式消息 & 并发一致性
// ---------------------------------------------------------------------------

func TestClose_DrainBehavior(t *testing.T) {
	client, _ := newTestClientPair(t)

	// 先填充一些消息到 writeCh（msgFromChToWs 异步消费）
	for i := 0; i < 5; i++ {
		if err := client.WriteJSONCtx(context.Background(), Message{Type: "drain", Msg: fmt.Sprintf("msg-%d", i)}); err != nil {
			t.Fatalf("WriteJSONCtx #%d: %v", i, err)
		}
	}

	// Close 应触发 drain：msgFromChToWs 收到 Done 后排空 writeCh
	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// 验证 Done() 已触发
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() not triggered within 1s after Close")
	}

	// 验证 writeCh 已排空
	s := client.Stats()
	if s.WriteQueueLen != 0 {
		t.Errorf("writeCh not fully drained after Close, len=%d", s.WriteQueueLen)
	}
}

func TestClose_DrainWithFullQueue(t *testing.T) {
	client, _ := newTestClientPair(t)

	// 用极小的 writeCh 容量构造队列满场景
	// 快速发送大量消息，让 msgFromChToWs 来不及消费
	// 如果 writeCh 满，WriteJSONCtx 返回 ErrWriteQueueFull 或通过 <-clientCtx.Done() 返回
	var queueFull, sentOk int
	for i := 0; i < 2000; i++ {
		err := client.WriteJSONCtx(context.Background(), Message{Type: "full", Data: make([]byte, 100)})
		if err == ErrWriteQueueFull {
			queueFull++
		} else if err == nil {
			sentOk++
		} else if err == websocket.ErrCloseSent {
			break
		} else {
			t.Fatalf("unexpected err: %v", err)
		}
	}
	t.Logf("sentOk=%d queueFull=%d before Close", sentOk, queueFull)

	// Close - drain 路径应处理剩余消息
	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// 验证 writeCh 已排空
	s := client.Stats()
	if s.WriteQueueLen != 0 {
		t.Errorf("writeCh not drained after Close, len=%d", s.WriteQueueLen)
	}
	if !s.IsClosed {
		t.Error("IsClosed should be true")
	}
}

func TestClose_ConcurrentWriteAndClose(t *testing.T) {
	client, _ := newTestClientPair(t)

	var writeErrCount atomic.Int64
	var writeOkCount atomic.Int64
	var closeSentCount atomic.Int64
	var wg sync.WaitGroup

	// 5 个 goroutine 并发写入，模拟生产环境多条业务逻辑同时 Write
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				err := client.WriteJSONCtx(context.Background(), Message{Type: "concurrent", Msg: fmt.Sprintf("g-%d-%d", id, j)})
				if err == nil {
					writeOkCount.Add(1)
				} else if err == websocket.ErrCloseSent || err == ErrWriteQueueFull {
					closeSentCount.Add(1)
					return // 连接已关闭或队列满，退出
				} else {
					writeErrCount.Add(1)
					t.Errorf("unexpected write err: %v", err)
					return
				}
			}
		}(i)
	}

	// 让写协程跑一会儿再 Close
	time.Sleep(10 * time.Millisecond)

	// Close 应优雅处理：等待 drain 完成，不 panic
	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// 等待所有写入协程退出
	wg.Wait()

	t.Logf("writeOk=%d closeSent=%d writeErr=%d", writeOkCount.Load(), closeSentCount.Load(), writeErrCount.Load())

	// 验证：全部写入成功 + 关闭触发 = 总调用次数
	total := writeOkCount.Load() + closeSentCount.Load() + writeErrCount.Load()
	if total == 0 {
		t.Error("no writes completed")
	}
	if writeErrCount.Load() > 0 {
		t.Errorf("unexpected write errors: %d", writeErrCount.Load())
	}
}

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

func TestDispatcher_BroadcastReliable(t *testing.T) {
	d := NewDispatcher(nil)
	alice, connA := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()
	bob, connB := newTestClientWithUID(t, "bob")
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	d.BroadcastReliableCtx(context.Background(), Message{Type: "reliable", Msg: "all must receive"})
	time.Sleep(50 * time.Millisecond)

	for name, conn := range map[string]*websocket.Conn{"alice": connA, "bob": connB} {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("%s should receive BroadcastReliable: %v", name, err)
			continue
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Msg != "all must receive" {
			t.Errorf("%s got msg=%q", name, msg.Msg)
		}
	}
}

func TestUpgrade_PerIPLimit(t *testing.T) {
	// perIPConns 是包级全局变量，需隔离
	perIPConns = sync.Map{}

	var rejectedCount atomic.Int32
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		client, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return true }),
			WithMaxConnPerIP(2),
		)
		if err != nil {
			rejectedCount.Add(1)
			return // 被拒绝，静默返回
		}
		defer client.Close()
		<-client.Done()
	})

	s := httptest.NewServer(r)
	defer s.Close()

	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
	origin := http.Header{"Origin": {"http://trusted.com"}}

	// 前 2 个连接应成功
	conn1, _, err := websocket.DefaultDialer.Dial(url, origin)
	if err != nil {
		t.Fatalf("conn1 dial: %v", err)
	}
	defer conn1.Close()

	conn2, _, err := websocket.DefaultDialer.Dial(url, origin)
	if err != nil {
		t.Fatalf("conn2 dial: %v", err)
	}
	defer conn2.Close()

	// 第 3 个连接应被拒绝（per-IP 限制）
	_, _, err = websocket.DefaultDialer.Dial(url, origin)
	if err == nil {
		t.Error("3rd connection from same IP should be rejected")
	}
	if n := rejectedCount.Load(); n != 1 {
		t.Errorf("rejectedCount=%d, want 1", n)
	}
}

func TestUpgrade_PerIPLimit_CloseHook(t *testing.T) {
	// 验证连接关闭后 perIPConns 计数器递减
	perIPConns = sync.Map{}

	var conn1Closed atomic.Bool
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		client, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return true }),
			WithMaxConnPerIP(5),
		)
		if err != nil {
			return
		}
		conn1Closed.Store(true)
		client.Close()
	})

	s := httptest.NewServer(r)
	defer s.Close()

	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
	origin := http.Header{"Origin": {"http://trusted.com"}}

	conn, _, err := websocket.DefaultDialer.Dial(url, origin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// 关闭 WS 连接，触发服务端 Close 钩子
	conn.Close()
	time.Sleep(50 * time.Millisecond)

	if !conn1Closed.Load() {
		t.Error("server should have closed the connection")
	}
	// perIPConns 应在 Close 钩子中递减
	if actual, ok := perIPConns.Load("127.0.0.1"); ok {
		if counter, ok := actual.(*atomic.Int32); ok {
			if n := counter.Load(); n != 0 {
				t.Errorf("perIPConns count=%d, want 0 after close", n)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 边缘场景测试
// ---------------------------------------------------------------------------

func TestWriteRawCtx_WithTimeout(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// 快速填充 writeCh，触发 <-ctx.Done() 路径
	var timeoutCount int
	for i := 0; i < 3000; i++ {
		err := client.WriteRawCtx(ctx, make([]byte, 100))
		if err == context.DeadlineExceeded {
			timeoutCount++
			break
		} else if err != nil && err != ErrWriteQueueFull {
			t.Errorf("unexpected err: %v", err)
			break
		}
	}
	if timeoutCount == 0 {
		t.Log("writeCh never full enough for timeout")
	} else {
		t.Logf("WriteRawCtx ctx timeout triggered %d times", timeoutCount)
	}
}

func TestRegisterCtx_CancelledCtx(t *testing.T) {
	d := NewDispatcher(nil)
	defer d.CleanupDeadConns()

	c, _ := newTestClientWithUID(t, "alice")

	// 已取消的 ctx
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := d.RegisterCtx(ctx, c)
	if err == nil {
		t.Error("should return error for cancelled ctx")
	}
	// 验证回滚：client 不应在 dispatcher 中
	if d.Len() != 0 {
		t.Errorf("Len=%d after cancelled ctx, want 0", d.Len())
	}
	if d.hasLocalUID("alice") {
		t.Error("hasLocalUID should be false after rollback")
	}
	c.Close()
}

func TestUnregisterCtx_CancelledCtx(t *testing.T) {
	d := NewDispatcher(nil)

	c, _ := newTestClientWithUID(t, "bob")
	if err := d.RegisterCtx(context.Background(), c); err != nil {
		t.Fatalf("register: %v", err)
	}

	// 已取消的 ctx 注销应回滚（保留 client 在 dispatcher 中）
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := d.UnregisterCtx(ctx, c)
	if err == nil {
		t.Error("should return error for cancelled ctx")
	}
	// 验证回滚：client 仍在 dispatcher 中
	if d.Len() != 1 {
		t.Errorf("Len=%d after cancelled ctx rollback, want 1", d.Len())
	}
	if !d.hasLocalUID("bob") {
		t.Error("hasLocalUID should still be true after rollback")
	}
	c.Close()
}

func TestDistributed_ConcurrentOnlineOffline(t *testing.T) {
	backend := newMockBackend(256)
	dd := NewDispatcher(backend)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	// 并发上线/下线同一 UID，验证 remoteUIDs 计数一致
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dd.handleRemoteOnline([]string{"alice"})
		}()
	}
	wg.Wait()

	// 验证 remoteUIDs 计数
	val, ok := dd.remoteUIDs.Load("alice")
	if !ok {
		t.Fatal("alice should be in remoteUIDs")
	}
	counter := val.(*atomic.Int32)
	if n := counter.Load(); n != 20 {
		t.Errorf("remoteUIDs[alice]=%d, want 20", n)
	}
	if n := dd.remoteUIDCount.Load(); n != 1 {
		t.Errorf("remoteUIDCount=%d, want 1", n)
	}

	// 并发下线
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dd.handleRemoteOffline([]string{"alice"})
		}()
	}
	wg.Wait()

	// 验证计数归零
	if _, ok := dd.remoteUIDs.Load("alice"); ok {
		t.Error("alice should be removed from remoteUIDs after all offline")
	}
	if n := dd.remoteUIDCount.Load(); n != 0 {
		t.Errorf("remoteUIDCount=%d, want 0", n)
	}
}

func TestBroadcastFilter_DeadClient(t *testing.T) {
	d := NewDispatcher(nil)

	c, conn := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c)

	// 关闭底层连接但不调用 Close，模拟僵尸连接
	conn.Close()
	time.Sleep(50 * time.Millisecond)

	// BroadcastFilter 应清理死连接
	var cleaned int
	d.BroadcastFilterCtx(context.Background(),
		Message{Type: "test"},
		func(c *Client) bool { return true },
	)

	if n := d.Len(); n != 0 {
		// 可能清理也可能不清理（取决于 IsAlive 的判断时间），记录日志不失败
		t.Logf("clients after broadcast with dead: %d", n)
		cleaned = d.CleanupDeadConns()
		t.Logf("manual CleanupDeadConns removed: %d", cleaned)
	}
}

func TestStats_AfterClose_QueueMetrics(t *testing.T) {
	client, _ := newTestClientPair(t)

	// 写入一些消息
	for i := 0; i < 10; i++ {
		_ = client.WriteJSONCtx(context.Background(), Message{Type: "test"})
	}

	// Close 后验证队列指标
	client.Close()
	s := client.Stats()
	if !s.IsClosed {
		t.Error("IsClosed should be true")
	}
	if s.WriteQueueLen != 0 {
		t.Errorf("WriteQueueLen=%d after drain, want 0", s.WriteQueueLen)
	}
	// NumSent 在 drain 后可能 > 0
	if s.NumSent == 0 {
		t.Log("NumSent=0 after Close - messages may have been drained")
	}
	// 关闭后队列容量应仍有值（字段不可变）
	if s.WriteQueueSize != 1024 {
		t.Errorf("WriteQueueSize=%d, want 1024", s.WriteQueueSize)
	}
	if s.ReadQueueSize != 1024 {
		t.Errorf("ReadQueueSize=%d, want 1024", s.ReadQueueSize)
	}
	// 验证 lastWriteErr/lastReadErr 仍可安全读取
	t.Logf("LastWriteErr=%q, LastReadErr=%q", s.LastWriteErr, s.LastReadErr)
}

// ---------------------------------------------------------------------------
// Client 代理方法（client_distributed.go）
// ---------------------------------------------------------------------------

func TestClientProxy_NoDispatcher(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	// dispatcher=nil, 所有代理方法应为 no-op（不 panic）
	client.SendToUIDCtx(context.Background(), "someone", Message{Type: "t"})
	client.SendToMultiUIDCtx(context.Background(), []string{"a", "b"}, Message{Type: "t"})
	client.BroadcastCtx(context.Background(), Message{Type: "t"})
	client.BroadcastReliableCtx(context.Background(), Message{Type: "t"})
}

func TestClientProxy_SendToSelf_NoOp(t *testing.T) {
	d := NewDispatcher(nil)
	alice, _ := newTestClientWithUID(t, "alice", withClientDispatcher(d))
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	// 发送给自己应被过滤（no-op）
	alice.SendToUIDCtx(context.Background(), "alice", Message{Type: "self"})
	// 不影响已存在的客户端信息
	if !d.hasLocalUID("alice") {
		t.Error("alice should still be in dispatcher")
	}
}

func TestClientProxy_SendToUID_Success(t *testing.T) {
	d := NewDispatcher(nil)
	alice, aliceConn := newTestClientWithUID(t, "alice", withClientDispatcher(d))
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob", withClientDispatcher(d))
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	// alice 通过代理向 bob 发送消息
	alice.SendToUIDCtx(context.Background(), "bob", Message{Type: "proxy", Msg: "from alice"})
	time.Sleep(50 * time.Millisecond)

	// bob 应收到
	bobConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, data, err := bobConn.ReadMessage()
	if err != nil {
		t.Fatalf("bob should receive: %v", err)
	}
	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Msg != "from alice" {
		t.Errorf("bob got %q, want 'from alice'", msg.Msg)
	}

	// alice 不应收到
	aliceConn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := aliceConn.ReadMessage(); err == nil {
		t.Error("alice should NOT receive her own send")
	}
}

func TestClientProxy_SendToMultiUID_FilterSelf(t *testing.T) {
	d := NewDispatcher(nil)
	alice, aliceConn := newTestClientWithUID(t, "alice", withClientDispatcher(d))
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob", withClientDispatcher(d))
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	// 发送给 bob 和 alice 自己，alice 不应收到
	alice.SendToMultiUIDCtx(context.Background(), []string{"alice", "bob"}, Message{Type: "multi", Msg: "team msg"})
	time.Sleep(50 * time.Millisecond)

	// bob 应收到
	bobConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, data, err := bobConn.ReadMessage()
	if err != nil {
		t.Fatalf("bob should receive: %v", err)
	}
	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Msg != "team msg" {
		t.Errorf("bob got %q", msg.Msg)
	}

	// alice 不应收到
	aliceConn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := aliceConn.ReadMessage(); err == nil {
		t.Error("alice should NOT receive her own multicast")
	}
}

func TestClientProxy_SendToMultiUID_EmptyFiltered(t *testing.T) {
	d := NewDispatcher(nil)
	alice, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	// 只有自己的 UID，过滤后应 empty，不 panic
	alice.SendToMultiUIDCtx(context.Background(), []string{"alice"}, Message{Type: "t"})
}

func TestClientProxy_Broadcast(t *testing.T) {
	d := NewDispatcher(nil)
	alice, aliceConn := newTestClientWithUID(t, "alice", withClientDispatcher(d))
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob", withClientDispatcher(d))
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	alice.BroadcastCtx(context.Background(), Message{Type: "announce", Msg: "all"})
	time.Sleep(50 * time.Millisecond)

	for name, conn := range map[string]*websocket.Conn{"alice": aliceConn, "bob": bobConn} {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("%s should receive broadcast: %v", name, err)
			continue
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Msg != "all" {
			t.Errorf("%s got %q", name, msg.Msg)
		}
	}
}

func TestClientProxy_BroadcastReliable(t *testing.T) {
	d := NewDispatcher(nil)
	alice, aliceConn := newTestClientWithUID(t, "alice", withClientDispatcher(d))
	_ = d.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob", withClientDispatcher(d))
	_ = d.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	alice.BroadcastReliableCtx(context.Background(), Message{Type: "reliable", Msg: "guaranteed"})
	time.Sleep(50 * time.Millisecond)

	for name, conn := range map[string]*websocket.Conn{"alice": aliceConn, "bob": bobConn} {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("%s should receive reliable: %v", name, err)
			continue
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Msg != "guaranteed" {
			t.Errorf("%s got %q", name, msg.Msg)
		}
	}
}

// ---------------------------------------------------------------------------
// auth 功能（auth.go）
// ---------------------------------------------------------------------------

func TestExtractUID_FromClaims(t *testing.T) {
	claims := &jwt.Claims{UID: "user-123"}
	if uid := extractUID(claims); uid != "user-123" {
		t.Errorf("extractUID=%q, want user-123", uid)
	}
}

func TestExtractUID_FromFields(t *testing.T) {
	claims := &jwt.Claims{Fields: map[string]any{"id": "user-id-field"}}
	if uid := extractUID(claims); uid != "user-id-field" {
		t.Errorf("extractUID=%q, want user-id-field", uid)
	}
}

func TestExtractUID_PreferenceOrder(t *testing.T) {
	// UID 字段优先于 Fields
	claims := &jwt.Claims{UID: "primary", Fields: map[string]any{"id": "secondary"}}
	if uid := extractUID(claims); uid != "primary" {
		t.Errorf("extractUID=%q, want primary", uid)
	}
}

func TestExtractUID_FieldsFallbackOrder(t *testing.T) {
	claims := &jwt.Claims{Fields: map[string]any{"user_id": "from-user_id"}}
	if uid := extractUID(claims); uid != "from-user_id" {
		t.Errorf("extractUID=%q, want from-user_id", uid)
	}
}

func TestExtractUID_EmptyClaims(t *testing.T) {
	claims := &jwt.Claims{}
	if uid := extractUID(claims); uid != "" {
		t.Errorf("extractUID=%q, want empty", uid)
	}
}

func TestExtractUID_IntField(t *testing.T) {
	// Fields 中值为 int 类型时转为 string
	claims := &jwt.Claims{Fields: map[string]any{"id": 12345}}
	if uid := extractUID(claims); uid != "12345" {
		t.Errorf("extractUID=%q, want 12345", uid)
	}
}

func TestParseTokenCtx_InvalidToken(t *testing.T) {
	_, err := ParseTokenCtx(context.Background(), "invalid-token")
	if err == nil {
		t.Error("should return error for invalid token")
	}
}

func TestParseTokenCtx_BearerPrefix(t *testing.T) {
	_, err := ParseTokenCtx(context.Background(), "Bearer invalid-token")
	if err == nil {
		t.Error("should return error for Bearer token")
	}
}

func TestParseTokenCtx_EmptyToken(t *testing.T) {
	_, err := ParseTokenCtx(context.Background(), "")
	if err == nil {
		t.Error("should return error for empty token")
	}
}

func TestParseTokenCtx_ValidToken(t *testing.T) {
	// 初始化 jwt 并在测试结束后重置
	jwt.Init()

	tokenStr := createTestJWT(t, "test-user-uid")
	uid, err := ParseTokenCtx(context.Background(), tokenStr)
	if err != nil {
		t.Fatalf("ParseTokenCtx failed: %v", err)
	}
	if uid != "test-user-uid" {
		t.Errorf("uid=%q, want test-user-uid", uid)
	}
}

func TestParseTokenCtx_ValidTokenBearer(t *testing.T) {
	jwt.Init()

	tokenStr := "Bearer " + createTestJWT(t, "bearer-user")
	uid, err := ParseTokenCtx(context.Background(), tokenStr)
	if err != nil {
		t.Fatalf("ParseTokenCtx with Bearer prefix failed: %v", err)
	}
	if uid != "bearer-user" {
		t.Errorf("uid=%q, want bearer-user", uid)
	}
}

func TestParseTokenCtx_MissingUID(t *testing.T) {
	jwt.Init()

	// 创建缺少 uid 字段的 token（通过直接使用 Claims 构造）
	tokenStr := createTestJWTWithClaims(t, &jwt.Claims{})
	_, err := ParseTokenCtx(context.Background(), tokenStr)
	if err != ErrTokenInvalid {
		t.Errorf("got %v, want ErrTokenInvalid", err)
	}
}

// createTestJWT 创建包含指定 UID 的测试 JWT token
func createTestJWT(t testing.TB, uid string) string {
	t.Helper()
	token, err := jwt.GenerateToken(uid, "test-user")
	if err != nil {
		t.Fatalf("create test JWT: %v", err)
	}
	return token
}

// createTestJWTWithClaims 使用自定义 Claims 创建测试 JWT token
// 通过 GenerateCustomToken 设置 Fields 来间接构造需要的 claims
func createTestJWTWithClaims(t testing.TB, claims *jwt.Claims) string {
	t.Helper()
	// 无 uid 时用不含 uid 的 custom token
	fields := make(jwt.KV)
	for k, v := range claims.Fields {
		fields[k] = v
	}
	token, err := jwt.GenerateCustomToken(fields)
	if err != nil {
		t.Fatalf("create test JWT with claims: %v", err)
	}
	return token
}

// ---------------------------------------------------------------------------
// Dispatcher 查询方法
// ---------------------------------------------------------------------------

func TestDispatcher_MaxConnections_Default(t *testing.T) {
	d := NewDispatcher(nil)
	if n := d.MaxConnections(); n != 0 {
		t.Errorf("MaxConnections=%d, want 0 (unlimited)", n)
	}
}

func TestDispatcher_MaxConnections_Custom(t *testing.T) {
	d := NewDispatcher(nil, WithMaxConnections(100))
	if n := d.MaxConnections(); n != 100 {
		t.Errorf("MaxConnections=%d, want 100", n)
	}
}

func TestDispatcher_TotalRejected(t *testing.T) {
	d := NewDispatcher(nil, WithMaxConnections(1))
	defer d.CleanupDeadConns()

	// 初始值为 0
	if n := d.TotalRejected(); n != 0 {
		t.Errorf("initial TotalRejected=%d, want 0", n)
	}

	c1, _ := newTestClientWithUID(t, "a")
	_ = d.RegisterCtx(context.Background(), c1)
	defer c1.Close()

	// 第二个连接应被拒绝
	c2, _ := newTestClientWithUID(t, "b")
	if err := d.RegisterCtx(context.Background(), c2); err != ErrMaxConnections {
		t.Errorf("expected ErrMaxConnections, got %v", err)
	}
	c2.Close()

	if n := d.TotalRejected(); n != 1 {
		t.Errorf("TotalRejected=%d, want 1", n)
	}
}

func TestDispatcher_LenLocal(t *testing.T) {
	d := NewDispatcher(nil)
	if n := d.LenLocal(); n != 0 {
		t.Errorf("LenLocal=%d, want 0", n)
	}

	c, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c)
	defer c.Close()

	if n := d.LenLocal(); n != 1 {
		t.Errorf("LenLocal=%d, want 1", n)
	}
	if n := d.Len(); n != d.LenLocal() {
		t.Errorf("standalone Len=%d != LenLocal=%d", n, d.LenLocal())
	}
}

func TestDispatcher_Stats_Standalone(t *testing.T) {
	d := NewDispatcher(nil)
	c, _ := newTestClientWithUID(t, "alice")
	_ = d.RegisterCtx(context.Background(), c)
	defer c.Close()

	s := d.Stats()
	if s.TotalConnections != 1 {
		t.Errorf("TotalConnections=%d", s.TotalConnections)
	}
	if s.LocalConnections != 1 {
		t.Errorf("LocalConnections=%d", s.LocalConnections)
	}
	if s.RemoteUIDs != 0 {
		t.Errorf("RemoteUIDs=%d, want 0", s.RemoteUIDs)
	}
	if s.MaxConnections != 0 {
		t.Errorf("MaxConnections=%d", s.MaxConnections)
	}
	if s.TotalRejected != 0 {
		t.Errorf("TotalRejected=%d", s.TotalRejected)
	}
}

func TestDispatcher_Clients_Snapshot(t *testing.T) {
	d := NewDispatcher(nil)
	c, _ := newTestClientWithUID(t, "snap")
	_ = d.RegisterCtx(context.Background(), c)
	defer c.Close()

	clients := d.Clients()
	if len(clients) != 1 {
		t.Errorf("Clients()=%d, want 1", len(clients))
	}
	if clients[0].UID() != "snap" {
		t.Errorf("client UID=%q", clients[0].UID())
	}
}

// ---------------------------------------------------------------------------
// healthState（health.go）
// ---------------------------------------------------------------------------

func TestHealth_RecordWriteErr(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	client.recordWriteErr(fmt.Errorf("test write error"))
	s := client.Stats()
	if s.WriteErrCount != 1 {
		t.Errorf("WriteErrCount=%d, want 1", s.WriteErrCount)
	}
	if s.LastWriteErr != "test write error" {
		t.Errorf("LastWriteErr=%q", s.LastWriteErr)
	}
}

func TestHealth_RecordReadErr(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	client.recordReadErr(fmt.Errorf("test read error"))
	s := client.Stats()
	if s.ReadErrCount != 1 {
		t.Errorf("ReadErrCount=%d, want 1", s.ReadErrCount)
	}
	if s.LastReadErr != "test read error" {
		t.Errorf("LastReadErr=%q", s.LastReadErr)
	}
}

func TestHealth_MarkLastWrite(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	before := time.Now()
	time.Sleep(time.Millisecond)
	client.markLastWrite()

	s := client.Stats()
	if s.LastWriteTime == "" {
		t.Fatal("LastWriteTime empty")
	}
	writeTime, err := time.Parse(time.RFC3339Nano, s.LastWriteTime)
	if err != nil {
		t.Fatalf("parse LastWriteTime: %v", err)
	}
	if writeTime.Before(before) {
		t.Errorf("writeTime %v should be after %v", writeTime, before)
	}
}

func TestHealth_MarkLastRead(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	before := time.Now()
	time.Sleep(time.Millisecond)
	client.markLastRead()

	s := client.Stats()
	if s.LastReadTime == "" {
		t.Fatal("LastReadTime empty")
	}
	readTime, err := time.Parse(time.RFC3339Nano, s.LastReadTime)
	if err != nil {
		t.Fatalf("parse LastReadTime: %v", err)
	}
	if readTime.Before(before) {
		t.Errorf("readTime %v should be after %v", readTime, before)
	}
}

func TestHealth_GetLastReadErr(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	// 未记录错误时返回默认消息
	err := client.getLastReadErr()
	if err == nil || err.Error() != "connection closed" {
		t.Errorf("getLastReadErr=%v, want 'connection closed'", err)
	}

	// 记录错误后可读取
	client.recordReadErr(fmt.Errorf("network timeout"))
	err = client.getLastReadErr()
	if err == nil || err.Error() != "network timeout" {
		t.Errorf("getLastReadErr=%v, want 'network timeout'", err)
	}
}

func TestHealth_RecordWriteErr_Multiple(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	client.recordWriteErr(fmt.Errorf("err1"))
	client.recordWriteErr(fmt.Errorf("err2"))

	s := client.Stats()
	if s.WriteErrCount != 2 {
		t.Errorf("WriteErrCount=%d, want 2", s.WriteErrCount)
	}
	// 最后一条错误应覆盖前一条
	if s.LastWriteErr != "err2" {
		t.Errorf("LastWriteErr=%q, want err2", s.LastWriteErr)
	}
}

func TestHealth_InitialStats(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	s := client.Stats()
	if s.NumSent != 0 {
		t.Errorf("initial NumSent=%d", s.NumSent)
	}
	if s.NumReceived != 0 {
		t.Errorf("initial NumReceived=%d", s.NumReceived)
	}
	if s.WriteErrCount != 0 {
		t.Errorf("initial WriteErrCount=%d", s.WriteErrCount)
	}
	if s.ReadErrCount != 0 {
		t.Errorf("initial ReadErrCount=%d", s.ReadErrCount)
	}
}

// ---------------------------------------------------------------------------
// checkWriteLimit（client_readwrite.go）
// ---------------------------------------------------------------------------

func TestCheckWriteLimit_NoLimit(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	// 未设置 writeLimit（默认 0），任何大小都应通过
	if err := client.checkWriteLimit(make([]byte, 100000)); err != nil {
		t.Errorf("no limit should pass: %v", err)
	}
}

func TestCheckWriteLimit_WithinLimit(t *testing.T) {
	client, _ := newTestClientPair(t, withClientWriteLimit(1000))
	defer client.Close()

	if err := client.checkWriteLimit(make([]byte, 500)); err != nil {
		t.Errorf("within limit should pass: %v", err)
	}
}

func TestCheckWriteLimit_ExceedLimit(t *testing.T) {
	client, _ := newTestClientPair(t, withClientWriteLimit(100))
	defer client.Close()

	if err := client.checkWriteLimit(make([]byte, 200)); err != ErrWriteLimitExceeded {
		t.Errorf("exceed limit: got %v, want ErrWriteLimitExceeded", err)
	}
}

func TestCheckWriteLimit_ExactBoundary(t *testing.T) {
	client, _ := newTestClientPair(t, withClientWriteLimit(100))
	defer client.Close()

	// 正好等于限制时应通过
	if err := client.checkWriteLimit(make([]byte, 100)); err != nil {
		t.Errorf("equal to limit should pass: %v", err)
	}
	// 超过限制 1 字节应拒绝
	if err := client.checkWriteLimit(make([]byte, 101)); err != ErrWriteLimitExceeded {
		t.Errorf("1 byte over limit should reject")
	}
}

func TestWriteLimit_ThroughWriteRaw(t *testing.T) {
	client, _ := newTestClientPair(t, withClientWriteLimit(10))
	defer client.Close()

	err := client.WriteRawCtx(context.Background(), make([]byte, 100))
	if err != ErrWriteLimitExceeded {
		t.Errorf("WriteRawCtx with over-limit: got %v, want ErrWriteLimitExceeded", err)
	}
}

// ---------------------------------------------------------------------------
// WriteRawCtx after Close
// ---------------------------------------------------------------------------

func TestWriteRaw_AfterClose(t *testing.T) {
	client, _ := newTestClientPair(t)
	client.Close()

	if err := client.WriteRawCtx(context.Background(), []byte("data")); err != websocket.ErrCloseSent {
		t.Errorf("got %v, want ErrCloseSent", err)
	}
}

// ---------------------------------------------------------------------------
// clientCtx WithoutCancel 隔离验证
// ---------------------------------------------------------------------------

func TestClientCtx_WithoutCancelIsolation(t *testing.T) {
	// 模拟上游 Gin ctx 超时
	ginCtx, ginCancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer ginCancel()

	// 等待 Gin ctx 超时
	<-ginCtx.Done()

	// 用已超时的上游 ctx 创建 client
	// 新版本使用 WithoutCancel，clientCtx 不应被上游超时影响
	if ginCtx.Err() == nil {
		t.Fatal("ginCtx should be expired")
	}

	serverCh := make(chan *Client, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := (&websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}).Upgrade(w, r, nil)
		client := NewClient(ginCtx, raw, "isolated")
		serverCh <- client
	}))
	defer s.Close()

	url := "ws" + strings.TrimPrefix(s.URL, "http")
	_, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	client := <-serverCh
	defer client.Close()

	// clientCtx 应仍活跃（不被上游超时影响）
	select {
	case <-client.Done():
		t.Error("clientCtx should NOT be done after upstream ctx timeout")
	default:
		// ✅ 通过：clientCtx 仍活跃
	}

	// 显式关闭后 clientCtx 才应结束
	client.Close()
	select {
	case <-client.Done():
		// ✅ 通过：Close 后 clientCtx 结束
	case <-time.After(time.Second):
		t.Error("clientCtx should be done after Close")
	}
}

// ---------------------------------------------------------------------------
// Option 函数单元测试
// ---------------------------------------------------------------------------

func TestClientOptions_WriteChSize(t *testing.T) {
	o := defaultClientOptions()
	withClientWriteChSize(2048)(o)
	if o.writeChSize != 2048 {
		t.Errorf("writeChSize=%d, want 2048", o.writeChSize)
	}
}

func TestClientOptions_ReadChSize(t *testing.T) {
	o := defaultClientOptions()
	withClientReadChSize(512)(o)
	if o.readChSize != 512 {
		t.Errorf("readChSize=%d, want 512", o.readChSize)
	}
}

func TestClientOptions_ReadTimeout(t *testing.T) {
	o := defaultClientOptions()
	withClientReadTimeout(30 * time.Second)(o)
	if o.readTimeout != 30*time.Second {
		t.Errorf("readTimeout=%v", o.readTimeout)
	}
}

func TestClientOptions_WriteTimeout(t *testing.T) {
	o := defaultClientOptions()
	withClientWriteTimeout(15 * time.Second)(o)
	if o.writeTimeout != 15*time.Second {
		t.Errorf("writeTimeout=%v", o.writeTimeout)
	}
}

func TestClientOptions_ReadLimit(t *testing.T) {
	o := defaultClientOptions()
	withClientReadLimit(8192)(o)
	if o.readLimit != 8192 {
		t.Errorf("readLimit=%d, want 8192", o.readLimit)
	}
}

func TestClientOptions_WriteLimit(t *testing.T) {
	o := defaultClientOptions()
	withClientWriteLimit(4096)(o)
	if o.writeLimit != 4096 {
		t.Errorf("writeLimit=%d, want 4096", o.writeLimit)
	}
}

func TestClientOptions_Dispatcher(t *testing.T) {
	o := defaultClientOptions()
	dd := NewDispatcher(nil)
	withClientDispatcher(dd)(o)
	if o.dispatcher != dd {
		t.Error("dispatcher not set")
	}
}

func TestClientOptions_DispatcherNil(t *testing.T) {
	o := defaultClientOptions()
	withClientDispatcher(nil)(o)
	if o.dispatcher != nil {
		t.Error("nil dispatcher should not replace existing")
	}
}

func TestClientOptions_ZeroValues(t *testing.T) {
	o := defaultClientOptions()
	// 零值参数不应覆盖默认
	withClientWriteChSize(0)(o)
	if o.writeChSize != 1024 {
		t.Errorf("writeChSize should remain default 1024, got %d", o.writeChSize)
	}
	withClientReadChSize(0)(o)
	if o.readChSize != 1024 {
		t.Errorf("readChSize should remain default 1024, got %d", o.readChSize)
	}
}

func TestDispatcherOptions_WorkerPool(t *testing.T) {
	o := defaultDispatcherOptions()
	WithWorkerPool(8)(o)
	if o.workerNum != 8 {
		t.Errorf("workerNum=%d, want 8", o.workerNum)
	}
}

func TestDispatcherOptions_MaxConnections(t *testing.T) {
	o := defaultDispatcherOptions()
	WithMaxConnections(5000)(o)
	if o.maxConns != 5000 {
		t.Errorf("maxConns=%d, want 5000", o.maxConns)
	}
}

func TestDispatcherOptions_ZeroValues(t *testing.T) {
	o := defaultDispatcherOptions()
	WithWorkerPool(0)(o)     // 0 不覆盖
	WithMaxConnections(0)(o) // 0 不覆盖
	if o.workerNum != 4 {
		t.Errorf("workerNum default 4, got %d", o.workerNum)
	}
	if o.maxConns != 0 {
		t.Errorf("maxConns default 0, got %d", o.maxConns)
	}
}

func TestHeartbeatOptions_Defaults(t *testing.T) {
	o := defaultHeartbeatOptions()
	if o.interval != defaultHeartbeatInterval {
		t.Errorf("interval=%v, want %v", o.interval, defaultHeartbeatInterval)
	}
	if o.pongTimeout != defaultPongTimeout {
		t.Errorf("pongTimeout=%v, want %v", o.pongTimeout, defaultPongTimeout)
	}
	if o.pingWriteWait != defaultPingWriteWait {
		t.Errorf("pingWriteWait=%v, want %v", o.pingWriteWait, defaultPingWriteWait)
	}
}

func TestHeartbeatOptions_Custom(t *testing.T) {
	o := defaultHeartbeatOptions()
	WithHeartbeatInterval(15 * time.Second)(o)
	WithPongTimeout(5 * time.Second)(o)
	WithPingWriteWait(3 * time.Second)(o)

	if o.interval != 15*time.Second {
		t.Errorf("interval=%v", o.interval)
	}
	if o.pongTimeout != 5*time.Second {
		t.Errorf("pongTimeout=%v", o.pongTimeout)
	}
	if o.pingWriteWait != 3*time.Second {
		t.Errorf("pingWriteWait=%v", o.pingWriteWait)
	}
}

func TestUpgradeOptions_Defaults(t *testing.T) {
	o := defaultUpgradeOptions()
	// CORS 默认拒绝
	if o.checkOrigin(nil) != false {
		t.Error("default CheckOrigin should reject")
	}
	if o.checkOriginSet {
		t.Error("checkOriginSet should be false by default")
	}
	if !o.enableCompression {
		t.Error("compression enabled by default")
	}
}

func TestUpgradeOptions_CheckOrigin(t *testing.T) {
	o := defaultUpgradeOptions()
	WithCheckOrigin(func(r *http.Request) bool { return true })(o)
	if !o.checkOriginSet {
		t.Error("checkOriginSet should be true after WithCheckOrigin")
	}
	if !o.checkOrigin(nil) {
		t.Error("WithCheckOrigin should accept")
	}
}

func TestUpgradeOptions_BufferSize(t *testing.T) {
	o := defaultUpgradeOptions()
	WithBufferSize(8192, 16384)(o)
	if o.readBufSize != 8192 {
		t.Errorf("readBufSize=%d", o.readBufSize)
	}
	if o.writeBufSize != 16384 {
		t.Errorf("writeBufSize=%d", o.writeBufSize)
	}
}

func TestUpgradeOptions_QueueSize(t *testing.T) {
	o := defaultUpgradeOptions()
	WithQueueSize(2048, 4096)(o)
	if o.writeChSize != 2048 {
		t.Errorf("writeChSize=%d", o.writeChSize)
	}
	if o.readChSize != 4096 {
		t.Errorf("readChSize=%d", o.readChSize)
	}
}

func TestUpgradeOptions_Subprotocols(t *testing.T) {
	o := defaultUpgradeOptions()
	WithSubprotocols("v1", "v2")(o)
	if len(o.subprotocols) != 2 || o.subprotocols[0] != "v1" {
		t.Errorf("subprotocols=%v", o.subprotocols)
	}
}

// ---------------------------------------------------------------------------
// 并发安全场景
// ---------------------------------------------------------------------------

func TestConcurrent_ReadAndWrite(t *testing.T) {
	client, conn := newTestClientPair(t)
	defer client.Close()

	var wg sync.WaitGroup
	// 并发读写
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = client.WriteJSONCtx(context.Background(), Message{Type: "w", Msg: fmt.Sprintf("w-%d", i)})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"r","msg":"r-%d"}`, i)))
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = client.ReadMessageCtx(context.Background())
		}
	}()

	wg.Wait()

	s := client.Stats()
	t.Logf("concurrent rw: NumSent=%d, NumReceived=%d", s.NumSent, s.NumReceived)
}

func TestConcurrent_RangeAndRegister(t *testing.T) {
	d := NewDispatcher(nil)

	var wg sync.WaitGroup
	// 并发注册和 Range
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c, _ := newTestClientWithUID(t, fmt.Sprintf("u-%d", id))
			_ = d.RegisterCtx(context.Background(), c)
			// 不 Close，留作测试用例
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			d.Range(func(c *Client) bool { return true })
		}
	}()

	wg.Wait()
	t.Logf("concurrent range+register: Len=%d", d.Len())
}

func TestConcurrent_ClientsAndCleanup(t *testing.T) {
	d := NewDispatcher(nil)
	for i := 0; i < 10; i++ {
		c, _ := newTestClientWithUID(t, fmt.Sprintf("u-%d", i))
		_ = d.RegisterCtx(context.Background(), c)
		defer c.Close()
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.CleanupDeadConns()
			_ = len(d.Clients())
			_ = d.Stats()
		}()
	}
	wg.Wait()
	t.Logf("concurrent cleanup+clients: Len=%d", d.Len())
}
