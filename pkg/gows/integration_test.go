package gows

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// 总测: WebSocket 核心链路 — 从 Upgrade 到消息收发到关闭
// 模拟真实业务场景：HTTP 升级 → Client 创建 → 读写消息 → 健康状态 → 关闭
// ---------------------------------------------------------------------------

func TestIntegration_WebSocketFullLifecycle(t *testing.T) {
	// 1. 启动 HTTP 服务，通过 Upgrade 创建 Client（含 heart + dispatcher）
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

		// 标记心跳已启动
		close(heartStarted)

		// 持续读取直至关闭
		for {
			_, err := client.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	s := httptest.NewServer(r)
	defer s.Close()

	// 2. 客户端连接
	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Origin": {"http://trusted.com"},
	})
	if err != nil {
		t.Fatalf("WebSocket Dial 失败: %v", err)
	}
	defer conn.Close()

	// 3. 等待心跳启动
	select {
	case <-heartStarted:
	case <-time.After(time.Second):
		t.Fatal("心跳未在 1s 内启动")
	}

	// 4. 验证 dispatcher 有连接
	time.Sleep(100 * time.Millisecond)
	if n := d.Len(); n != 1 {
		t.Errorf("Dispatcher.Len() = %d, want 1", n)
	}

	// 5. 遍历所有在线连接
	var client *Client
	d.Range(func(c *Client) bool {
		client = c
		return false
	})
	if client == nil {
		t.Fatal("Dispatcher 中应有 Client")
	}

	// 6. 验证 Client 基础属性
	if client.UID() == "" {
		t.Error("UID 不应为空")
	}
	if client.RemoteAddr() == "" {
		t.Error("RemoteAddr 不应为空")
	}
	if !client.IsAlive() {
		t.Error("新连接应处于存活状态")
	}

	// 7. 客户端向服务端发送消息
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping","msg":"hello"}`)); err != nil {
		t.Fatalf("发送消息失败: %v", err)
	}

	// 8. 通过 Dispatcher 向客户端发送消息
	msg := Message{Type: "notify", Msg: "server push"}
	d.SendToUIDCtx(context.Background(), client.UID(), msg)

	time.Sleep(50 * time.Millisecond)

	// 9. 验证 Stats
	stats := client.Stats()
	t.Logf("📊 Client Stats:")
	t.Logf("   UID=%s, RemoteAddr=%s", stats.UID, stats.RemoteAddr)
	t.Logf("   NumSent=%d, NumReceived=%d, WriteQueueLen=%d", stats.NumSent, stats.NumReceived, stats.WriteQueueLen)
	t.Logf("   IsAlive=%v, IsClosed=%v", stats.IsAlive, stats.IsClosed)
	t.Logf("   LastWriteTime=%s, LastReadTime=%s", stats.LastWriteTime, stats.LastReadTime)

	if stats.UID != client.UID() {
		t.Errorf("Stats.UID = %q, want %q", stats.UID, client.UID())
	}

	// 10. 验证 Dispatcher Stats
	dStats := d.Stats()
	t.Logf("📊 Dispatcher Stats:")
	t.Logf("   TotalConnections=%d, LocalConnections=%d", dStats.TotalConnections, dStats.LocalConnections)
	if dStats.TotalConnections != 1 {
		t.Errorf("TotalConnections = %d, want 1", dStats.TotalConnections)
	}

	// 11. 关闭连接
	client.Close()

	// 12. 验证关闭后状态
	if client.IsAlive() {
		t.Error("Close 后 IsAlive 应为 false")
	}
	select {
	case <-client.Done():
		// 正确：关闭后 Done 应触发
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

// ---------------------------------------------------------------------------
// 总测: Dispatcher + 跨实例分布式消息
// 模拟两个实例、多个用户、交叉发送的完整场景
// ---------------------------------------------------------------------------

func TestIntegration_DistributedMessaging(t *testing.T) {
	backend := newMockBackend(64)

	instanceA := NewDispatcher(backend)
	instanceB := NewDispatcher(backend)

	ctx := context.Background()
	instanceA.Start(ctx)
	instanceB.Start(ctx)
	defer instanceA.Stop()
	defer instanceB.Stop()

	// 实例 A: alice, bob
	alice, aliceConn := newTestClientWithUID(t, "alice")
	_ = instanceA.Register(alice)
	defer alice.Close()

	bob, bobConn := newTestClientWithUID(t, "bob")
	_ = instanceA.Register(bob)
	defer bob.Close()

	// 实例 B: charlie, dave
	charlie, charlieConn := newTestClientWithUID(t, "charlie")
	_ = instanceB.Register(charlie)
	defer charlie.Close()

	dave, daveConn := newTestClientWithUID(t, "dave")
	_ = instanceB.Register(dave)
	defer dave.Close()

	time.Sleep(150 * time.Millisecond)

	// 全局 UID 视图应一致
	uidsA := instanceA.ConnectedUIDs()
	uidsB := instanceB.ConnectedUIDs()
	if len(uidsA) != 4 || len(uidsB) != 4 {
		t.Errorf("ConnectedUIDs: A=%d, B=%d, want both=4", len(uidsA), len(uidsB))
	}

	// 实例 A → charlie：跨实例发送
	instanceA.SendToUIDCtx(ctx, "charlie", Message{Type: "p2p", Msg: "from A to charlie"})

	// 实例 B → alice：跨实例发送
	instanceB.SendToUIDCtx(ctx, "alice", Message{Type: "p2p", Msg: "from B to alice"})

	// 实例 B 广播
	instanceB.BroadcastCtx(ctx, Message{Type: "notice", Msg: "global notice"})

	time.Sleep(200 * time.Millisecond)

	// 验证各用户收到的消息数
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

	// alice: p2p from B (1) + broadcast (1) = 2
	if received["alice"] < 2 {
		t.Errorf("alice 应收到 >=2 条, 实际 %d", received["alice"])
	}
	// charlie: p2p from A (1) + broadcast (1) = 2
	if received["charlie"] < 2 {
		t.Errorf("charlie 应收到 >=2 条, 实际 %d", received["charlie"])
	}
	// bob: broadcast (1) = 1
	if received["bob"] < 1 {
		t.Errorf("bob 应收到 >=1 条, 实际 %d", received["bob"])
	}
	// dave: broadcast (1) = 1
	if received["dave"] < 1 {
		t.Errorf("dave 应收到 >=1 条, 实际 %d", received["dave"])
	}

	t.Log("✅ 分布式消息集成测试通过")
}

// ---------------------------------------------------------------------------
// 总测: 全栈 — Dispatcher + 并发读写 + Stats 一致性
// ---------------------------------------------------------------------------

func TestIntegration_ConcurrentSendAndStats(t *testing.T) {
	d := NewDispatcher(nil)

	// 创建多个客户端
	var clients []*Client
	for i := 0; i < 3; i++ {
		c, _ := newTestClientWithUID(t, "user")
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

	// 并发发送
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				d.BroadcastCtx(context.Background(), Message{Type: "concurrent", Msg: "test"})
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)

	// 验证 Stats 一致性
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

	// Dispatcher 统计
	dStats := d.Stats()
	t.Logf("📊 Dispatcher Stats: TotalConnections=%d", dStats.TotalConnections)
	if int(dStats.TotalConnections) != len(clients) {
		t.Errorf("TotalConnections=%d, want %d", dStats.TotalConnections, len(clients))
	}

	_, _ = json.Marshal(dStats)
	t.Log("✅ 并发发送 + Stats 一致性测试通过")
}
