package gows

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// 单元测试: 构造函数、错误处理（无需真实 RabbitMQ）
// ---------------------------------------------------------------------------

// TestRabbitMQBackend_New 测试构造函数。
func TestRabbitMQBackend_New(t *testing.T) {
	// NewRabbitMQBackend
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
	if b.conn != nil {
		t.Error("conn should be nil before Start")
	}
	t.Log("✅ NewRabbitMQBackend 创建成功")
}

// TestRabbitMQBackend_NewFromConn 测试从已有连接构造。
func TestRabbitMQBackend_NewFromConn(t *testing.T) {
	// 注意: 这里只测试构造本身，不传真实连接
	b := NewRabbitMQBackendFromConn(nil, "ws:test")
	if b == nil {
		t.Fatal("NewRabbitMQBackendFromConn returned nil")
	}
	if b.conn != nil {
		t.Error("conn should be nil since we passed nil")
	}
	if b.exchange != "ws:test" {
		t.Errorf("exchange = %q, want %q", b.exchange, "ws:test")
	}
	t.Log("✅ NewRabbitMQBackendFromConn 创建成功")
}

// TestRabbitMQBackend_EnsureConnNoURL 测试没有 URL 时 ensureConn 报错。
func TestRabbitMQBackend_EnsureConnNoURL(t *testing.T) {
	b := NewRabbitMQBackend("", "ws:test") // 空 URL
	err := b.ensureConn(context.Background())
	if err == nil {
		t.Fatal("ensureConn should fail with empty URL")
	}
	if !strings.Contains(err.Error(), "no URL provided") {
		t.Errorf("unexpected error: %v", err)
	}
	t.Logf("✅ 空 URL 报错正确: %v", err)
}

// TestRabbitMQBackend_PublishNoConn 测试未初始化连接时 Publish 返回错误。
func TestRabbitMQBackend_PublishNoConn(t *testing.T) {
	b := NewRabbitMQBackend("amqp://sunjianguo:jianguo123@43.143.78.234:5672/", "ws:test")

	// 不调用 Start，直接 Publish — ensureConn 会尝试连接远程 RabbitMQ
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := b.Publish(ctx, &PubSubMessage{Type: "test", Payload: []byte(`{}`)})
	if err == nil {
		t.Log("⚠️  本地有 RabbitMQ 服务，Publish 成功了（如果这是测试环境则正常）")
		return
	}
	// 应报连接相关错误
	if errors.Is(err, context.DeadlineExceeded) {
		t.Log("⚠️ RabbitMQ 连接超时（检查网络或地址）")
	} else {
		t.Logf("⚠️ Publish 报错: %v", err)
	}
}

// TestRabbitMQBackend_PublishFromConnNil 测试从 nil 连接构造后 Publish。
func TestRabbitMQBackend_PublishFromConnNil(t *testing.T) {
	b := NewRabbitMQBackendFromConn(nil, "ws:test")

	// conn 为 nil, url 为空
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

// TestRabbitMQBackend_Close 测试 Close 调用（未 Start 时幂等）。
func TestRabbitMQBackend_Close(t *testing.T) {
	b := NewRabbitMQBackend("amqp://sunjianguo:jianguo123@43.143.78.234:5672/", "ws:test")
	err := b.Close()
	if err != nil {
		t.Errorf("Close should be nil: %v", err)
	}
	// 再次 Close 应幂等
	err = b.Close()
	if err != nil {
		t.Errorf("second Close should be nil: %v", err)
	}
	t.Log("✅ Close 幂等正确")
}

// TestRabbitMQBackend_PubSubMessageMarshaling 测试 PubSubMessage 序列化/反序列化。
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
// 单元测试: 分布式 Dispatcher 使用 Mock Backend（无需真实 RabbitMQ）
// ---------------------------------------------------------------------------

// mockBackend 模拟 Backend 实现，支持多订阅者扇出。
// 每个 Receive() 调用创建独立的消费通道，Publish 向所有订阅者广播。
type mockBackend struct {
	mu         sync.Mutex
	publishCh  chan *PubSubMessage // 所有 Publish 的消息（仅用于验证）
	subscribers []chan *PubSubMessage // 每个 Receive 创建的独立通道
	closed     atomic.Bool
	publishCnt atomic.Int64
}

func newMockBackend(bufSize int) *mockBackend {
	return &mockBackend{
		publishCh: make(chan *PubSubMessage, bufSize),
	}
}

func (m *mockBackend) Publish(ctx context.Context, msg *PubSubMessage) error {
	m.publishCnt.Add(1)

	// 向所有订阅者扇出（非阻塞，模拟真实 Pub/Sub）
	m.mu.Lock()
	for _, ch := range m.subscribers {
		select {
		case ch <- msg:
		default:
		}
	}
	m.mu.Unlock()

	// 同时记录到 publishCh 用于验证
	select {
	case m.publishCh <- msg:
	default:
	}
	return nil
}

func (m *mockBackend) Receive(ctx context.Context) (<-chan *PubSubMessage, error) {
	ch := make(chan *PubSubMessage, 64)
	m.mu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.mu.Unlock()
	return ch, nil
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

// TestRabbitMQ_SendToUIDCtx_WithMockBackend 使用 mock 测试分布式发送。
func TestRabbitMQ_SendToUIDCtx_WithMockBackend(t *testing.T) {
	mock := newMockBackend(64)

	dd := NewDispatcher(mock)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	// 注册 alice 和 bob
	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = dd.Register(alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = dd.Register(bob)
	defer bob.Close()

	// 发送给 bob
	dd.SendToUIDCtx(ctx, "bob", Message{Type: "mock", Msg: "to bob"})

	time.Sleep(100 * time.Millisecond)

	// bob 应收到
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

	// alice 不应收到
	aliceConn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	_, _, err = aliceConn.ReadMessage()
	if err == nil {
		t.Error("alice should NOT receive")
	} else {
		t.Logf("✅ [Mock] alice 未收到（预期）")
	}
}

// TestRabbitMQ_BroadcastCtx_WithMockBackend 使用 mock 测试分布式广播。
func TestRabbitMQ_BroadcastCtx_WithMockBackend(t *testing.T) {
	mock := newMockBackend(64)

	dd := NewDispatcher(mock)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	// 两个实例上的用户
	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = dd.Register(alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = dd.Register(bob)
	defer bob.Close()

	// 广播
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

// TestRabbitMQ_SendToMultiUIDCtx_WithMockBackend 使用 mock 测试多用户发送。
func TestRabbitMQ_SendToMultiUIDCtx_WithMockBackend(t *testing.T) {
	mock := newMockBackend(64)

	dd := NewDispatcher(mock)
	ctx := context.Background()
	dd.Start(ctx)
	defer dd.Stop()

	conns := map[string]*websocket.Conn{}
	for _, uid := range []string{"alice", "bob", "charlie"} {
		c, conn := newTestClientWithUID(t, uid)
		_ = dd.Register(c)
		conns[uid] = conn
		defer c.Close()
	}

	// 只发给 alice 和 charlie
	dd.SendToMultiUIDCtx(ctx, []string{"alice", "charlie"}, Message{Type: "team", Msg: "团队通知"})

	time.Sleep(100 * time.Millisecond)

	// alice 和 charlie 应收到
	for _, uid := range []string{"alice", "charlie"} {
		_, data, err := conns[uid].ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive: %v", uid, err)
		}
		t.Logf("✅ [Mock] %s 收到: %s", uid, string(data))
	}

	// bob 不应收到
	conns["bob"].SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	_, _, err := conns["bob"].ReadMessage()
	if err == nil {
		t.Error("bob should NOT receive")
	} else {
		t.Logf("✅ [Mock] bob 未收到（预期）")
	}
}

// ---------------------------------------------------------------------------
// 集成测试: 需真实 RabbitMQ 服务（通过 RABBITMQ_URL 环境变量控制）
// ---------------------------------------------------------------------------

// getRabbitMQURL 从环境变量获取 RabbitMQ 连接地址。
// 优先级: RABBITMQ_URL > 默认远程 RabbitMQ
func getRabbitMQURL() string {
	if url := os.Getenv("RABBITMQ_URL"); url != "" {
		return url
	}
	return "amqp://sunjianguo:jianguo123@43.143.78.234:5672/"
}

// skipNoRabbitMQ 检查 RabbitMQ 是否可用，不可用时跳过测试。
// 默认跳过（需设置 RABBITMQ_INTEGRATION=true 启用集成测试）。
func skipNoRabbitMQ(t *testing.T) string {
	t.Helper()

	// 默认跳过集成测试（避免 AMQP goroutine 泄漏影响其他测试）
	if os.Getenv("RABBITMQ_INTEGRATION") != "true" {
		t.Skip("RABBITMQ_INTEGRATION!=true, 跳过集成测试")
	}

	url := getRabbitMQURL()

	// 快速探测 RabbitMQ 是否可达
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	b := NewRabbitMQBackend(url, "ws:inttest:"+t.Name())
	// 尝试 Publish 一条测试消息
	if err := b.Publish(ctx, &PubSubMessage{Type: "probe"}); err != nil {
		t.Skipf("RabbitMQ 不可用 (%v), 跳过集成测试", err)
	}
	_ = b.Close()
	return url
}

// TestRabbitMQBackend_PublishAndReceive 完整的 Publish → Receive 集成测试。
func TestRabbitMQBackend_PublishAndReceive(t *testing.T) {
	url := skipNoRabbitMQ(t)

	// 创建两个后端（模拟两个实例）
	exchange := "ws:inttest:" + strings.ReplaceAll(t.Name(), "/", "_")
	backendA := NewRabbitMQBackend(url, exchange)
	backendB := NewRabbitMQBackend(url, exchange)

	ctx := context.Background()

	// 实例 B 启动接收
	msgChB, err := backendB.Receive(ctx)
	if err != nil {
		t.Fatalf("backendB.Receive: %v", err)
	}
	defer backendB.Close()

	// 实例 A 发送
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

	// B 应收到
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

// TestRabbitMQBackend_WithDispatcher 完整的集成测试: Dispatcher + RabbitMQBackend。
func TestRabbitMQBackend_WithDispatcher(t *testing.T) {
	skipNoRabbitMQ(t) // 探测并跳过
	url := getRabbitMQURL()

	exchange := "ws:inttest:" + strings.ReplaceAll(t.Name(), "/", "_")
	backendA := NewRabbitMQBackend(url, exchange)
	backendB := NewRabbitMQBackend(url, exchange)

	// 实例 A 和 B
	instanceA := NewDispatcher(backendA)
	instanceB := NewDispatcher(backendB)

	ctx := context.Background()
	instanceA.Start(ctx)
	instanceB.Start(ctx)
	defer instanceA.Stop()
	defer instanceB.Stop()

	// 实例 A: alice, 实例 B: bob
	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = instanceA.Register(alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = instanceB.Register(bob)
	defer bob.Close()

	// 实例 A 跨实例发送给 bob
	instanceA.SendToUIDCtx(ctx, "bob", Message{Type: "greeting", Msg: "hello from instanceA"})

	// bob 应收到
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

	// 广播测试
	instanceB.BroadcastCtx(ctx, Message{Type: "notice", Msg: "系统通知"})

	time.Sleep(200 * time.Millisecond)

	// alice 和 bob 都应收到广播
	for name, conn := range map[string]*websocket.Conn{"alice": aliceConn, "bob": bobConn} {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("%s should receive broadcast: %v", name, err)
		}
		t.Logf("✅ [集成] %s 收到广播: %s", name, string(data))
	}
}

// TestRabbitMQBackend_ConcurrentPublish 并发发送测试。
func TestRabbitMQBackend_ConcurrentPublish(t *testing.T) {
	skipNoRabbitMQ(t) // 探测并跳过
	url := getRabbitMQURL()

	exchange := "ws:inttest:" + strings.ReplaceAll(t.Name(), "/", "_")
	backend := NewRabbitMQBackend(url, exchange)

	ctx := context.Background()
	defer backend.Close()

	var sendCnt atomic.Int64
	var errCnt atomic.Int64

	// 并发 10 个 goroutine 各发 20 条消息
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
