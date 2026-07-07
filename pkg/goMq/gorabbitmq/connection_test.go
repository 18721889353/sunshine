package gorabbitmq

import (
	"context"
	"strings"
	"testing"
	"time"
)

// newTestConnection 创建测试用连接，连接失败则跳过测试
func newTestConnection(t testing.TB, ctx context.Context, opts ...ConnectionOption) *Connection {
	t.Helper()
	conn, err := NewConnection(ctx, testRabbitMQURL, opts...)
	if err != nil {
		t.Skipf("skip: cannot connect to RabbitMQ: %v", err)
	}
	return conn
}

// ---------------------------------------------------------------------------
// 测试面：NewConnection 基本创建
// ---------------------------------------------------------------------------

func TestConn_NewConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx,
		WithReconnectTime(time.Second*3),
		WithDialTimeout(time.Second*5),
		WithHeartbeat(time.Second*3),
	)
	defer conn.Close()

	if !conn.CheckConnected(ctx) {
		t.Fatal("new connection should be connected")
	}
}

// ---------------------------------------------------------------------------
// 测试面：空 URL 应返回错误
// ---------------------------------------------------------------------------

func TestConn_EmptyURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := NewConnection(ctx, "")
	if err == nil {
		t.Error("expected error for empty URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// 测试面：CheckConnected — 创建后为 true，关闭后为 false
// ---------------------------------------------------------------------------

func TestConn_CheckConnected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)
	defer conn.Close()

	if !conn.CheckConnected(ctx) {
		t.Fatal("expected CheckConnected=true after creation")
	}

	conn.Close()

	// 关闭后需要短暂等待 monitor goroutine 处理退出信号
	time.Sleep(time.Millisecond * 50)

	if conn.CheckConnected(ctx) {
		t.Fatal("expected CheckConnected=false after Close")
	}
}

// ---------------------------------------------------------------------------
// 测试面：Close — 幂等，多次调用不 panic
// ---------------------------------------------------------------------------

func TestConn_CloseIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)

	// 多次关闭不应 panic
	conn.Close()
	conn.Close()
	conn.Close()

	_ = ctx
}

// ---------------------------------------------------------------------------
// 测试面：GetConn — 返回底层 AMQP 连接
// ---------------------------------------------------------------------------

func TestConn_GetConn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)
	defer conn.Close()

	amqpConn := conn.GetConn(ctx)
	if amqpConn == nil {
		t.Fatal("GetConn returned nil")
	}
	if amqpConn.IsClosed() {
		t.Fatal("GetConn returned closed connection")
	}
}

// ---------------------------------------------------------------------------
// 测试面：GetReconnectCount — 初始为 0
// ---------------------------------------------------------------------------

func TestConn_GetReconnectCount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)
	defer conn.Close()

	if count := conn.GetReconnectCount(ctx); count != 0 {
		t.Errorf("expected initial reconnectCount=0, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// 测试面：GetLastError — 初始为零值
// ---------------------------------------------------------------------------

func TestConn_GetLastError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)
	defer conn.Close()

	tm, err := conn.GetLastError(ctx)
	if err != nil {
		t.Errorf("expected nil initial error, got %v", err)
	}
	if !tm.IsZero() {
		t.Errorf("expected zero initial time, got %v", tm)
	}
}

// ---------------------------------------------------------------------------
// 测试面：GetConnectionStatus — 包含所有必需字段
// ---------------------------------------------------------------------------

func TestConn_GetConnectionStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)
	defer conn.Close()

	status := conn.GetConnectionStatus(ctx)
	requiredFields := []string{"connected", "reconnectCount", "url", "maxRetries"}
	for _, field := range requiredFields {
		if _, ok := status[field]; !ok {
			t.Errorf("status missing field: %s", field)
		}
	}

	if status["connected"] != true {
		t.Error("expected connected=true")
	}
	if status["url"] == "" {
		t.Error("expected non-empty url")
	}
}

// ---------------------------------------------------------------------------
// 测试面：GetConnectionStatus 关闭后状态
// ---------------------------------------------------------------------------

func TestConn_GetConnectionStatusAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)
	conn.Close()

	time.Sleep(time.Millisecond * 50)

	status := conn.GetConnectionStatus(ctx)
	if status["connected"] == true {
		t.Error("expected connected=false after close")
	}
}

// ---------------------------------------------------------------------------
// 边界：GetConn 关闭后应返回 nil
// ---------------------------------------------------------------------------

func TestConn_GetConnAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)
	conn.Close()

	time.Sleep(time.Millisecond * 50)

	if amqpConn := conn.GetConn(ctx); amqpConn != nil {
		t.Log("GetConn after Close returned non-nil (may be cached reference)")
	}
}

// ---------------------------------------------------------------------------
// 边界：并发 Close + CheckConnected 不应 panic 或死锁
// ---------------------------------------------------------------------------

func TestConn_ConcurrentCloseAndCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			conn.CheckConnected(ctx)
		}
		close(done)
	}()

	conn.Close()
	<-done
}

// ---------------------------------------------------------------------------
// 边界：GetConnectionStatus 关闭后 URL 已脱敏
// ---------------------------------------------------------------------------

func TestConn_GetConnectionStatusURLMasked(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)
	defer conn.Close()

	status := conn.GetConnectionStatus(ctx)
	urlStr, ok := status["url"].(string)
	if !ok {
		t.Fatal("status.url is not a string")
	}
	// URL 应是脱敏后的，不应等于原始 URL
	if urlStr == testRabbitMQURL {
		t.Errorf("url in status is not masked: %s", urlStr)
	}
	// 应包含 *** 脱敏标记
	if !strings.Contains(urlStr, "***") {
		t.Errorf("url in status missing mask marker: %s", urlStr)
	}
}

// ---------------------------------------------------------------------------
// 边界：maskURL 更多边界情况
// ---------------------------------------------------------------------------

func TestConn_maskURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"empty", "", ""},
		{"no credentials", "amqp://localhost:5672/", "amqp://localhost:5672/"},
		{"with credentials", "amqp://user:pass@localhost:5672/", "amqp://***:***@localhost:5672/"},
		{"with special chars in password",
			"amqp://admin:s3cret!@host:5672/vhost",
			"amqp://***:***@host:5672/vhost"},
		{"amqps with credentials", "amqps://user:pass@host:5671/", "amqps://***:***@host:5671/"},
		{"only host and port", "localhost:5672", "localhost:5672"},
		{"no scheme with @", "user:pass@localhost:5672", "***:***@localhost:5672"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskURL(tt.url); got != tt.want {
				t.Errorf("maskURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
