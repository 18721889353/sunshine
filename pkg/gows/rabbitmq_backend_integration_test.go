package gows

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// skipNoRabbitMQ / getRabbitMQURL 定义在 test_helpers.go

// ---------------------------------------------------------------------------
// 集成测试: 需真实 RabbitMQ 服务（通过 RABBITMQ_INTEGRATION=true 环境变量启用）
// ---------------------------------------------------------------------------

func TestRabbitMQBackend_PublishAndReceive(t *testing.T) {
	url := skipNoRabbitMQ(t)

	exchange := "ws:inttest:" + strings.ReplaceAll(t.Name(), "/", "_")
	backendA := NewRabbitMQBackend(url, exchange)
	backendB := NewRabbitMQBackend(url, exchange)

	ctx := context.Background()

	msgChB, err := backendB.Receive(ctx)
	if err != nil {
		t.Fatalf("backendB.Receive: %v", err)
	}
	defer backendB.Close()

	testPayload := []byte(`{"type":"chat","msg":"跨实例消息"}`)
	err = backendA.Publish(ctx, &PubSubMessage{
		Type:    "send_to_uid",
		UIDs:    []string{"bob"},
		Payload: testPayload,
	})
	if err != nil {
		t.Fatalf("backendA.Publish: %v", err)
	}
	defer backendA.Close()

	select {
	case msg := <-msgChB:
		if msg.Type != "send_to_uid" {
			t.Errorf("Type = %q, want %q", msg.Type, "send_to_uid")
		}
		if len(msg.UIDs) != 1 || msg.UIDs[0] != "bob" {
			t.Errorf("UIDs = %v, want [bob]", msg.UIDs)
		}
		if string(msg.Payload) != string(testPayload) {
			t.Errorf("Payload = %q, want %q", string(msg.Payload), string(testPayload))
		}
		t.Logf("✅ 实例 B 收到来自 A 的消息: Type=%s UIDs=%v", msg.Type, msg.UIDs)

	case <-time.After(3 * time.Second):
		t.Fatal("等待 RabbitMQ 消息超时")
	}
}

func TestRabbitMQBackend_WithDispatcher(t *testing.T) {
	skipNoRabbitMQ(t)
	url := getRabbitMQURL()

	exchange := "ws:inttest:" + strings.ReplaceAll(t.Name(), "/", "_")
	backendA := NewRabbitMQBackend(url, exchange)
	backendB := NewRabbitMQBackend(url, exchange)

	instanceA := NewDispatcher(backendA)
	instanceB := NewDispatcher(backendB)

	ctx := context.Background()
	instanceA.Start(ctx)
	instanceB.Start(ctx)
	defer instanceA.Stop()
	defer instanceB.Stop()

	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = instanceA.Register(alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = instanceB.Register(bob)
	defer bob.Close()

	instanceA.SendToUIDCtx(ctx, "bob", Message{Type: "greeting", Msg: "hello from instanceA"})

	time.Sleep(300 * time.Millisecond)
	_, data, err := bobConn.ReadMessage()
	if err != nil {
		t.Fatalf("bob should receive: %v", err)
	}
	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Msg != "hello from instanceA" {
		t.Errorf("bob got %q, want %q", msg.Msg, "hello from instanceA")
	}
	t.Logf("✅ [集成] 跨实例投递成功: bob 收到来自 instanceA 的消息")

	instanceB.BroadcastCtx(ctx, Message{Type: "notice", Msg: "系统通知"})

	time.Sleep(200 * time.Millisecond)

	for name, conn := range map[string]*websocket.Conn{"alice": aliceConn, "bob": bobConn} {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive broadcast: %v", name, err)
		}
		t.Logf("✅ [集成] %s 收到广播: %s", name, string(data))
	}
}

func TestRabbitMQBackend_ConcurrentPublish(t *testing.T) {
	skipNoRabbitMQ(t)
	url := getRabbitMQURL()

	exchange := "ws:inttest:" + strings.ReplaceAll(t.Name(), "/", "_")
	backend := NewRabbitMQBackend(url, exchange)

	ctx := context.Background()
	defer backend.Close()

	var sendCnt atomic.Int64
	var errCnt atomic.Int64

	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 20; j++ {
				err := backend.Publish(ctx, &PubSubMessage{
					Type:    "broadcast",
					Payload: []byte(`{"msg":"concurrent"}`),
				})
				if err != nil {
					errCnt.Add(1)
				}
				sendCnt.Add(1)
			}
		}(i)
	}

	time.Sleep(2 * time.Second)
	t.Logf("✅ [集成] 并发发送完成: 总 %d 次, 失败 %d 次", sendCnt.Load(), errCnt.Load())
	if errCnt.Load() > 0 {
		t.Errorf("并发发送失败 %d 次", errCnt.Load())
	}
}
