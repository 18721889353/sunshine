package gows

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// 测试辅助: 创建一对 WebSocket 连接（server → *Client，test → *websocket.Conn）
// ---------------------------------------------------------------------------

// newTestClientPair 创建一个测试用的 WebSocket 连接对。
func newTestClientPair(t testing.TB, opts ...ClientOption) (*Client, *websocket.Conn) {
	t.Helper()

	serverCh := make(chan *Client, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin:       func(_ *http.Request) bool { return true },
			EnableCompression: false,
		}
		raw, err := upgrader.Upgrade(w, r, nil)
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

// newTestClientWithUID 创建一对 (Client, *websocket.Conn)，UID 由参数指定。
func newTestClientWithUID(t testing.TB, uid string, opts ...ClientOption) (*Client, *websocket.Conn) {
	t.Helper()

	serverCh := make(chan *Client, 1)
	s := newTestServer(t, func(raw *websocket.Conn) {
		serverCh <- NewClient(context.Background(), raw, uid, opts...)
	})
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

// newTestServer 创建一个测试 HTTP WebSocket 服务器。
func newTestServer(t testing.TB, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin:       func(_ *http.Request) bool { return true },
			EnableCompression: false,
		}
		raw, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		handler(raw)
	}))
	t.Cleanup(s.Close)
	return s
}

// ---------------------------------------------------------------------------
// mockBackend: 模拟 Backend 实现，支持多订阅者扇出
// ---------------------------------------------------------------------------

type mockBackend struct {
	mu          sync.Mutex
	publishCh   chan *PubSubMessage
	subscribers []chan *PubSubMessage
	closed      atomic.Bool
	publishCnt  atomic.Int64
}

func newMockBackend(bufSize int) *mockBackend {
	return &mockBackend{
		publishCh: make(chan *PubSubMessage, bufSize),
	}
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

func (m *mockBackend) Receive(_ context.Context) (<-chan *PubSubMessage, error) {
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

// ---------------------------------------------------------------------------
// RabbitMQ 集成测试辅助
// ---------------------------------------------------------------------------

func getRabbitMQURL() string {
	if url := os.Getenv("RABBITMQ_URL"); url != "" {
		return url
	}
	return "amqp://sunjianguo:jianguo123@43.143.78.234:5672/"
}

func skipNoRabbitMQ(t *testing.T) string {
	t.Helper()

	if os.Getenv("RABBITMQ_INTEGRATION") != "true" {
		t.Skip("RABBITMQ_INTEGRATION!=true, 跳过集成测试")
	}

	url := getRabbitMQURL()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	b := NewRabbitMQBackend(url, "ws:inttest:"+t.Name())
	if err := b.Publish(ctx, &PubSubMessage{Type: "probe"}); err != nil {
		t.Skipf("RabbitMQ 不可用 (%v), 跳过集成测试", err)
	}
	_ = b.Close()
	return url
}
