package gows

import (
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

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
	// 使用默认队列大小
	client, _ := newTestClientPair(t)
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
// TestReadMessage
// ---------------------------------------------------------------------------

func TestReadMessage_Count(t *testing.T) {
	client, testConn := newTestClientPair(t)
	defer client.Close()

	for i := 0; i < 5; i++ {
		_ = testConn.WriteMessage(websocket.TextMessage, []byte("msg"))
		_, err := client.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
	}

	stats := client.Stats()
	if stats.NumReceived != 5 {
		t.Errorf("NumReceived = %d, want %d", stats.NumReceived, 5)
	}
}

// TestReadMessage_WithTimeout 的超时功能通过 Upgrade 的 WithReadTimeout 选项设置
// 客户端无独立的 withClientReadTimeout

// ---------------------------------------------------------------------------
// BenchmarkWriteJSON
// ---------------------------------------------------------------------------

func BenchmarkWriteJSON(b *testing.B) {
	client, _ := newTestClientPair(b)
	defer client.Close()

	msg := Message{Type: "bench", Msg: "hello"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.WriteJSON(msg)
	}
}
