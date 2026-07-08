package gows

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
)

// testRabbitMQURL 从环境变量获取 RabbitMQ URL，默认值用于 CI/本地开发
func testRabbitMQURL() string {
	if url := os.Getenv("RABBITMQ_URL"); url != "" {
		return url
	}
	return "amqp://guest:guest@127.0.0.1:5672/"
}

// skipIfNoRabbitMQ 检查 RabbitMQ 是否可用
func skipIfNoRabbitMQ(t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := gorabbitmq.NewConnection(ctx, testRabbitMQURL())
	if err != nil {
		t.Skipf("RabbitMQ not available: %v", err)
	}
	conn.Close()
}

const defaultTestTimeout = 10 * time.Second

// ---------------------------------------------------------------------------
// RabbitMQBackend 结构测试（无需 RabbitMQ 服务器）
// ---------------------------------------------------------------------------

func TestNewRabbitMQBackend(t *testing.T) {
	b := NewRabbitMQBackend(testRabbitMQURL(), "test-exchange")
	if b == nil {
		t.Fatal("NewRabbitMQBackend returned nil")
	}
	if b.exchange != "test-exchange" {
		t.Errorf("exchange = %q, want %q", b.exchange, "test-exchange")
	}

	// 验证广播交换机名称
	if name := b.broadcastExchangeName(); name != "test-exchange:broadcast" {
		t.Errorf("broadcastExchangeName = %q, want %q", name, "test-exchange:broadcast")
	}
}

func TestNewRabbitMQBackendFromConn(t *testing.T) {
	b := NewRabbitMQBackendFromConn(nil, "test-exchange")
	if b == nil {
		t.Fatal("NewRabbitMQBackendFromConn returned nil")
	}
	if b.exchange != "test-exchange" {
		t.Errorf("exchange = %q, want %q", b.exchange, "test-exchange")
	}
}

func TestRabbitMQBackend_ExchangeTypes(t *testing.T) {
	b := NewRabbitMQBackend(testRabbitMQURL(), "test-exchange")

	direct := b.getDirectExchange()
	if direct.Type() != "direct" {
		t.Errorf("direct exchange type = %q, want 'direct'", direct.Type())
	}
	if direct.Name() != "test-exchange" {
		t.Errorf("direct exchange name = %q, want 'test-exchange'", direct.Name())
	}

	fanout := b.getFanoutExchange()
	if fanout.Type() != "fanout" {
		t.Errorf("fanout exchange type = %q, want 'fanout'", fanout.Type())
	}
	if fanout.Name() != "test-exchange:broadcast" {
		t.Errorf("fanout exchange name = %q, want 'test-exchange:broadcast'", fanout.Name())
	}

	// 验证惰性初始化：再次获取应返回相同对象
	if b.getDirectExchange() != direct {
		t.Error("getDirectExchange should return cached object")
	}
	if b.getFanoutExchange() != fanout {
		t.Error("getFanoutExchange should return cached object")
	}
}

func TestRabbitMQBackend_PublishWithUnknownType(t *testing.T) {
	b := NewRabbitMQBackend(testRabbitMQURL(), "test-exchange")
	ctx := context.Background()

	// 不存在的消息类型应报错
	err := b.Publish(ctx, &PubSubMessage{Type: "unknown_type"})
	if err == nil {
		t.Error("expected error for unknown message type")
	}
}

// ---------------------------------------------------------------------------
// RabbitMQ 集成测试（需要 RabbitMQ 服务器）
// ---------------------------------------------------------------------------

func TestRabbitMQBackend_Integration(t *testing.T) {
	skipIfNoRabbitMQ(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testExchange := "ws:test:" + randString(8)

	// 创建两个后端实例（模拟两个实例）
	A := NewRabbitMQBackend(testRabbitMQURL(), testExchange,
		gorabbitmq.WithHeartbeat(10),
		gorabbitmq.WithDialTimeout(3*time.Second),
	)
	B := NewRabbitMQBackend(testRabbitMQURL(), testExchange,
		gorabbitmq.WithHeartbeat(10),
		gorabbitmq.WithDialTimeout(3*time.Second),
	)
	defer A.Close()
	defer B.Close()

	// A 和 B 订阅广播
	msgChA, err := A.SubscribeBroadcast(ctx)
	if err != nil {
		t.Fatalf("A SubscribeBroadcast: %v", err)
	}
	msgChB, err := B.SubscribeBroadcast(ctx)
	if err != nil {
		t.Fatalf("B SubscribeBroadcast: %v", err)
	}

	// A 发布广播消息
	payload := []byte(`{"type":"test","msg":"hello"}`)
	if err := A.Publish(ctx, &PubSubMessage{
		InstanceID: "instance-A",
		Type:       MsgTypeBroadcast,
		Payload:    payload,
	}); err != nil {
		t.Fatalf("A Publish: %v", err)
	}

	// 确认 B 收到广播消息
	select {
	case msg := <-msgChB:
		if msg.Type != MsgTypeBroadcast {
			t.Errorf("got type %q, want %q", msg.Type, MsgTypeBroadcast)
		}
		if msg.InstanceID != "instance-A" {
			t.Errorf("got instanceID %q, want %q", msg.InstanceID, "instance-A")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("B did not receive broadcast message within 5s")
	}

	// 确认 A 也收到自己发布的广播（Fanout 特性）
	select {
	case msg := <-msgChA:
		if msg.Type != MsgTypeBroadcast {
			t.Errorf("A got type %q", msg.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("A did not receive its own broadcast message within 5s")
	}
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

// randString 生成随机字符串用于测试队列/交换机命名
func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[int(time.Now().UnixNano()%int64(len(letters)))]
	}
	return string(b)
}
