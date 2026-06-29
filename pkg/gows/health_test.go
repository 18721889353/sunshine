package gows

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// TestClientStats
// ---------------------------------------------------------------------------

func TestClientStats(t *testing.T) {
	client, testConn := newTestClientPair(t)
	defer client.Close()

	// 发送几条消息
	_ = client.WriteJSONCtx(context.Background(), Message{Type: "ping"})
	_ = client.WriteJSONCtx(context.Background(), Message{Type: "pong"})

	// 等 writeLoop 消费完
	time.Sleep(50 * time.Millisecond)

	// 读取几条消息
	_ = testConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))
	_, _ = client.ReadMessageCtx(context.Background())

	stats := client.Stats()
	if stats.UID != "test-uid" {
		t.Errorf("UID = %q, want %q", stats.UID, "test-uid")
	}
	if stats.RemoteAddr == "" {
		t.Error("RemoteAddr should not be empty")
	}
	if stats.WriteQueueSize != 64 {
		t.Errorf("WriteQueueSize = %d, want %d", stats.WriteQueueSize, 64)
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
// IsAlive 测试
// ---------------------------------------------------------------------------

func TestClient_IsAlive_NewClient(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	if !client.IsAlive() {
		t.Error("new client should be alive")
	}
}

func TestClient_IsAlive_AfterClose(t *testing.T) {
	client, _ := newTestClientPair(t)
	client.Close()

	if client.IsAlive() {
		t.Error("client should NOT be alive after Close()")
	}
}

// ---------------------------------------------------------------------------
// Stats 详细字段测试
// ---------------------------------------------------------------------------

func TestClient_Stats_NewFields(t *testing.T) {
	client, _ := newTestClientPair(t)
	defer client.Close()

	// 执行一次写入，等待 writeLoop 异步消费后触发时间记录
	_ = client.WriteJSONCtx(context.Background(), Message{Type: "ping"})
	time.Sleep(50 * time.Millisecond)

	stats := client.Stats()

	if !stats.IsAlive {
		t.Error("Stats.IsAlive should be true for active client")
	}
	if stats.WriteErrCount != 0 {
		t.Errorf("Stats.WriteErrCount = %d, want 0", stats.WriteErrCount)
	}
	if stats.LastWriteTime == "" {
		t.Error("Stats.LastWriteTime should not be empty after write")
	}

	client.Close()
	stats = client.Stats()
	if stats.IsAlive {
		t.Error("Stats.IsAlive should be false after Close()")
	}
	if !stats.IsClosed {
		t.Error("Stats.IsClosed should be true after Close()")
	}
}

// ---------------------------------------------------------------------------
// Demo: ClientStats 使用示例
// ---------------------------------------------------------------------------

func TestDemo_ClientStats(t *testing.T) {
	d := NewDispatcher(nil)

	// 创建客户端并发送/接收消息
	c, testConn := newTestClientWithUID(t, "dave")
	if err := d.RegisterCtx(context.Background(), c); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer c.Close()

	// 发送消息
	_ = c.WriteJSONCtx(context.Background(), Message{Type: "ping"})
	_ = c.WriteJSONCtx(context.Background(), Message{Type: "pong"})

	// 接收消息
	_ = testConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))
	_, _ = c.ReadMessageCtx(context.Background())

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

	jsonMsg := Message{Type: "notify", Msg: "hello", Data: map[string]int{"count": 42}}
	data, err := json.Marshal(jsonMsg)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	t.Logf("   └─ Message JSON:   %s", string(data))
}
