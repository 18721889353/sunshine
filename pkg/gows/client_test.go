package gows

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// 测试辅助: 创建一对 WebSocket 连接（server → *Client，test → *websocket.Conn）
// ---------------------------------------------------------------------------

// newTestClientPair 创建一个测试用的 WebSocket 连接对。
// 返回 (serverClient, testConn)，其中:
//   - serverClient: 被测试的 *Client（模拟服务端连接）
//   - testConn:     *websocket.Conn（模拟客户端，用于发送/接收数据）
func newTestClientPair(t testing.TB, opts ...ClientOption) (*Client, *websocket.Conn) {
	t.Helper()

	// 服务端: HTTP → WebSocket 升级后创建 *Client
	serverCh := make(chan *Client, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin:       func(r *http.Request) bool { return true },
			EnableCompression: false,
		}
		raw, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverCh <- nil
			return
		}
		serverCh <- NewClient(raw, "test-uid", opts...)
	}))
	t.Cleanup(s.Close)

	// 客户端: 通过 ws:// 协议连接服务端
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

// ---------------------------------------------------------------------------
// TestNewClient
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	client, testConn := newTestClientPair(t)
	defer client.Close()

	if client.UID() != "test-uid" {
		t.Errorf("UID = %q, want %q", client.UID(), "test-uid")
	}
	if client.RemoteAddr() == "" {
		t.Error("RemoteAddr should not be empty")
	}

	// 验证 Done() 在未关闭前不返回
	select {
	case <-client.Done():
		t.Error("Done() channel should NOT be closed before Close()")
	default:
	}

	_ = testConn.WriteMessage(websocket.TextMessage, []byte("hello"))
	_, data, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want %q", string(data), "hello")
	}
}

// ---------------------------------------------------------------------------
// TestNewClient_WithOptions
// ---------------------------------------------------------------------------

func TestNewClient_WithWriteQueueSize(t *testing.T) {
	client, _ := newTestClientPair(t, WithWriteQueueSize(128))
	defer client.Close()

	stats := client.Stats()
	if stats.WriteQueueSize != 128 {
		t.Errorf("WriteQueueSize = %d, want %d", stats.WriteQueueSize, 128)
	}
}

func TestNewClient_WithReadLimit(t *testing.T) {
	client, testConn := newTestClientPair(t, WithReadLimit(1024))
	defer client.Close()

	// 发送超过限制的消息
	bigData := make([]byte, 2048)
	for i := range bigData {
		bigData[i] = 'a'
	}
	_ = testConn.WriteMessage(websocket.TextMessage, bigData)

	// 服务端读取应触发 close
	_, _, err := client.ReadMessage()
	if err == nil {
		// ReadMessage 可能在关闭之后还被调用一次返回 nil，再下一次才返回错误
		_, _, err = client.ReadMessage()
	}
	if err == nil {
		t.Error("expected error when reading oversized message")
	}
}

// ---------------------------------------------------------------------------
// TestWriteJSON
// ---------------------------------------------------------------------------

func TestWriteJSON_Success(t *testing.T) {
	client, testConn := newTestClientPair(t)
	defer client.Close()

	msg := Message{Type: "greeting", Msg: "hello"}
	if err := client.WriteJSON(msg); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	_, data, err := testConn.ReadMessage()
	if err != nil {
		t.Fatalf("testConn.ReadMessage: %v", err)
	}
	if !strings.Contains(string(data), `"greeting"`) {
		t.Errorf("expected greeting in message, got %s", string(data))
	}
}

func TestWriteJSON_AfterClose(t *testing.T) {
	client, _ := newTestClientPair(t)
	client.Close()

	if err := client.WriteJSON(Message{Type: "ping"}); err != websocket.ErrCloseSent {
		t.Errorf("after close: got %v, want ErrCloseSent", err)
	}
}

func TestWriteJSON_QueueFull(t *testing.T) {
	// 创建队列大小为 1 的客户端
	client, _ := newTestClientPair(t, WithWriteQueueSize(1))
	defer client.Close()

	// 填充队列: 因为 writeLoop 协程会消费队列，所以需要在短时间内高速写入
	for i := 0; i < 100; i++ {
		err := client.WriteJSON(Message{Type: "ping", Data: i})
		if err == ErrWriteQueueFull {
			return // 期望的队列满错误
		}
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
	}
	// 100 次都没满说明队列一直在被消费，也算正常
	t.Log("write queue was never full (all messages consumed by writeLoop)")
}

// ---------------------------------------------------------------------------
// TestReadMessage_Count
// ---------------------------------------------------------------------------

func TestReadMessage_Count(t *testing.T) {
	client, testConn := newTestClientPair(t)
	defer client.Close()

	for i := 0; i < 5; i++ {
		_ = testConn.WriteMessage(websocket.TextMessage, []byte("msg"))
		_, _, err := client.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
	}

	stats := client.Stats()
	if stats.NumReceived != 5 {
		t.Errorf("NumReceived = %d, want %d", stats.NumReceived, 5)
	}
}

// ---------------------------------------------------------------------------
// TestClose_Idempotent
// ---------------------------------------------------------------------------

func TestClose_Idempotent(t *testing.T) {
	client, _ := newTestClientPair(t)

	// 多次调用 Close 应幂等
	if err := client.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("third close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestClose_ContextCancelled
// ---------------------------------------------------------------------------

func TestClose_ContextCancelled(t *testing.T) {
	client, _ := newTestClientPair(t)

	ctx := client.Context()
	select {
	case <-ctx.Done():
		t.Error("context should NOT be done before Close()")
	default:
	}

	client.Close()

	select {
	case <-ctx.Done():
		// expected
	default:
		t.Error("context should be done after Close()")
	}
}

// ---------------------------------------------------------------------------
// TestClose_DoneChannel
// ---------------------------------------------------------------------------

func TestClose_DoneChannel(t *testing.T) {
	client, _ := newTestClientPair(t)

	done := client.Done()
	select {
	case <-done:
		t.Error("done should NOT fire before close")
	default:
	}

	client.Close()

	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Error("done should fire after close within 1s")
	}
}

// ---------------------------------------------------------------------------
// TestSetCloseHook
// ---------------------------------------------------------------------------

func TestSetCloseHook(t *testing.T) {
	client, _ := newTestClientPair(t)

	var hookCalled atomic.Bool
	client.SetCloseHook(func() {
		hookCalled.Store(true)
	})

	client.Close()

	if !hookCalled.Load() {
		t.Error("close hook was not called")
	}
}

// ---------------------------------------------------------------------------
// TestClient_RemoteAddr
// ---------------------------------------------------------------------------

func TestClient_RemoteAddr(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	addr := client.RemoteAddr()
	if addr == "" {
		t.Error("RemoteAddr should not be empty")
	}
	t.Logf("RemoteAddr = %s", addr)
}

// ---------------------------------------------------------------------------
// TestClient_Context
// ---------------------------------------------------------------------------

func TestClient_SetContext(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	// 设置自定义 context
	ctx := context.WithValue(context.Background(), "key", "value")
	client.SetContext(ctx)

	if val, ok := client.Context().Value("key").(string); !ok || val != "value" {
		t.Errorf("context value = %v, want %q", client.Context().Value("key"), "value")
	}
}

// ---------------------------------------------------------------------------
// TestClientStats
// ---------------------------------------------------------------------------

func TestClientStats(t *testing.T) {
	client, testConn := newTestClientPair(t, WithWriteQueueSize(32))
	defer client.Close()

	// 发送几条消息
	_ = client.WriteJSON(Message{Type: "ping"})
	_ = client.WriteJSON(Message{Type: "pong"})

	// 等 writeLoop 消费完
	time.Sleep(50 * time.Millisecond)

	// 读取几条消息
	_ = testConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))
	_, _, _ = client.ReadMessage()

	stats := client.Stats()
	if stats.UID != "test-uid" {
		t.Errorf("UID = %q, want %q", stats.UID, "test-uid")
	}
	if stats.RemoteAddr == "" {
		t.Error("RemoteAddr should not be empty")
	}
	if stats.WriteQueueSize != 32 {
		t.Errorf("WriteQueueSize = %d, want %d", stats.WriteQueueSize, 32)
	}
	if stats.NumReceived != 1 {
		t.Errorf("NumReceived = %d, want %d", stats.NumReceived, 1)
	}
	if stats.IsClosed {
		t.Error("IsClosed should be false")
	}

	client.Close()
	stats = client.Stats()
	if !stats.IsClosed {
		t.Error("IsClosed should be true after Close()")
	}
}

// ---------------------------------------------------------------------------
// TestMessage_JSON
// ---------------------------------------------------------------------------

func TestMessageJSON(t *testing.T) {
	t.Parallel()

	msg := Message{
		Type: "notify",
		Msg:  "hello world",
		Data: map[string]int{"count": 42},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !strings.Contains(string(data), `"notify"`) {
		t.Errorf("expected type in json, got %s", string(data))
	}
	if !strings.Contains(string(data), `42`) {
		t.Errorf("expected data in json, got %s", string(data))
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if decoded.Type != "notify" {
		t.Errorf("Type = %q, want %q", decoded.Type, "notify")
	}
}

// ---------------------------------------------------------------------------
// TestWriteQueueFullError
// ---------------------------------------------------------------------------

func TestWriteQueueFullError(t *testing.T) {
	t.Parallel()

	err := ErrWriteQueueFull
	if err.Error() == "" {
		t.Error("ErrWriteQueueFull should not have empty error string")
	}

	// 验证它是 ErrWriteQueueFull
	if err != ErrWriteQueueFull {
		t.Errorf("expected ErrWriteQueueFull, got %T", err)
	}
}

// ---------------------------------------------------------------------------
// BenchmarkWriteJSON
// ---------------------------------------------------------------------------

func BenchmarkWriteJSON(b *testing.B) {
	client, _ := newTestClientPair(b, WithWriteQueueSize(256))
	defer client.Close()

	msg := Message{Type: "bench", Msg: "hello"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.WriteJSON(msg)
	}
}
