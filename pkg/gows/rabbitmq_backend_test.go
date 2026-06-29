package gows

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// mockBackend / newMockBackend 定义在 test_helpers.go

// ---------------------------------------------------------------------------
// 单元测试: 构造函数、错误处理（无需真实 RabbitMQ）
// ---------------------------------------------------------------------------

func TestRabbitMQBackend_New(t *testing.T) {
	b := NewRabbitMQBackend("amqp://sunjianguo:jianguo123@43.143.78.234:5672/", "ws:test")
	if b == nil {
		t.Fatal("NewRabbitMQBackend returned nil")
	}
	if b.url != "amqp://sunjianguo:jianguo123@43.143.78.234:5672/" {
		t.Errorf("url = %q, want %q", b.url, "amqp://sunjianguo:jianguo123@43.143.78.234:5672/")
	}
	if b.exchange != "ws:test" {
		t.Errorf("exchange = %q, want %q", b.exchange, "ws:test")
	}
	if b.conn.Load() != nil {
		t.Error("conn should be nil before Start")
	}
	t.Log("✅ NewRabbitMQBackend 创建成功")
}

func TestRabbitMQBackend_NewFromConn(t *testing.T) {
	b := NewRabbitMQBackendFromConn(nil, "ws:test")
	if b == nil {
		t.Fatal("NewRabbitMQBackendFromConn returned nil")
	}
	if b.conn.Load() != nil {
		t.Error("conn should be nil since we passed nil")
	}
	if b.exchange != "ws:test" {
		t.Errorf("exchange = %q, want %q", b.exchange, "ws:test")
	}
	t.Log("✅ NewRabbitMQBackendFromConn 创建成功")
}

func TestRabbitMQBackend_EnsureConnNoURL(t *testing.T) {
	b := NewRabbitMQBackend("", "ws:test")
	err := b.ensureConn(context.Background())
	if err == nil {
		t.Fatal("ensureConn should fail with empty URL")
	}
	if !strings.Contains(err.Error(), "no URL provided") {
		t.Errorf("unexpected error: %v", err)
	}
	t.Logf("✅ 空 URL 报错正确: %v", err)
}

func TestRabbitMQBackend_PublishNoConn(t *testing.T) {
	b := NewRabbitMQBackend("amqp://sunjianguo:jianguo123@43.143.78.234:5672/", "ws:test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := b.Publish(ctx, &PubSubMessage{Type: "test", Payload: []byte(`{}`)})
	if err == nil {
		t.Log("⚠️  本地有 RabbitMQ 服务，Publish 成功了（如果这是测试环境则正常）")
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Log("⚠️ RabbitMQ 连接超时（检查网络或地址）")
	} else {
		t.Logf("⚠️ Publish 报错: %v", err)
	}
}

func TestRabbitMQBackend_PublishFromConnNil(t *testing.T) {
	b := NewRabbitMQBackendFromConn(nil, "ws:test")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := b.Publish(ctx, &PubSubMessage{Type: "test"})
	if err == nil {
		t.Fatal("Publish should fail with nil conn and no url")
	}
	if !strings.Contains(err.Error(), "no URL provided") {
		t.Errorf("unexpected error: %v", err)
	}
	t.Logf("✅ nil conn + 空 URL 报错正确: %v", err)
}

func TestRabbitMQBackend_Close(t *testing.T) {
	b := NewRabbitMQBackend("amqp://sunjianguo:jianguo123@43.143.78.234:5672/", "ws:test")
	err := b.Close()
	if err != nil {
		t.Errorf("Close should be nil: %v", err)
	}
	err = b.Close()
	if err != nil {
		t.Errorf("second Close should be nil: %v", err)
	}
	t.Log("✅ Close 幂等正确")
}

func TestRabbitMQBackend_PubSubMessageMarshaling(t *testing.T) {
	original := &PubSubMessage{
		Type:    "send_to_uid",
		UIDs:    []string{"alice", "bob"},
		Payload: json.RawMessage(`{"type":"chat","msg":"hello"}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded PubSubMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, original.Type)
	}
	if len(decoded.UIDs) != 2 || decoded.UIDs[0] != "alice" {
		t.Errorf("UIDs = %v, want %v", decoded.UIDs, original.UIDs)
	}
	t.Logf("✅ PubSubMessage 序列化/反序列化正确: %s", string(data))
}

// ---------------------------------------------------------------------------
// 使用 Mock Backend 的 Dispatcher 测试（无需真实 RabbitMQ）
// ---------------------------------------------------------------------------

func TestRabbitMQ_SendToUIDCtx_WithMockBackend(t *testing.T) {
	mock := newMockBackend(64)

	dd := NewDispatcher(mock)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = dd.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = dd.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	dd.SendToUIDCtx(ctx, "bob", Message{Type: "mock", Msg: "to bob"})

	time.Sleep(100 * time.Millisecond)

	_, data, err := bobConn.ReadMessage()
	if err != nil {
		t.Fatalf("bob should receive: %v", err)
	}
	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Msg != "to bob" {
		t.Errorf("bob got %q, want %q", msg.Msg, "to bob")
	}
	t.Logf("✅ [Mock] bob 收到: %s", string(data))

	aliceConn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	_, _, err = aliceConn.ReadMessage()
	if err == nil {
		t.Error("alice should NOT receive")
	} else {
		t.Logf("✅ [Mock] alice 未收到（预期）")
	}
}

func TestRabbitMQ_BroadcastCtx_WithMockBackend(t *testing.T) {
	mock := newMockBackend(64)

	dd := NewDispatcher(mock)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = dd.RegisterCtx(context.Background(), alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = dd.RegisterCtx(context.Background(), bob)
	defer bob.Close()

	dd.BroadcastCtx(ctx, Message{Type: "broadcast", Msg: "大家注意"})

	time.Sleep(100 * time.Millisecond)

	for name, conn := range map[string]*websocket.Conn{"alice": aliceConn, "bob": bobConn} {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive broadcast: %v", name, err)
		}
		t.Logf("✅ [Mock] %s 收到广播: %s", name, string(data))
	}
}

func TestRabbitMQ_SendToMultiUIDCtx_WithMockBackend(t *testing.T) {
	mock := newMockBackend(64)

	dd := NewDispatcher(mock)
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

	dd.SendToMultiUIDCtx(ctx, []string{"alice", "charlie"}, Message{Type: "team", Msg: "团队通知"})

	time.Sleep(100 * time.Millisecond)

	for _, uid := range []string{"alice", "charlie"} {
		_, data, err := conns[uid].ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive: %v", uid, err)
		}
		t.Logf("✅ [Mock] %s 收到: %s", uid, string(data))
	}

	conns["bob"].SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	_, _, err := conns["bob"].ReadMessage()
	if err == nil {
		t.Error("bob should NOT receive")
	} else {
		t.Logf("✅ [Mock] bob 未收到（预期）")
	}
}
