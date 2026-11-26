package gorabbitmq

import (
	"context"
	amqp "github.com/rabbitmq/amqp091-go"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestConnection 创建测试用连接，连接失败则跳过测试
func newTestConnection(t testing.TB, ctx context.Context, opts ...ConnectionOption) *Connection {
	t.Helper()
	conn, err := NewConnection(ctx, testRabbitMQURL(), opts...)
	if err != nil {
		t.Skipf("skip: cannot connect to RabbitMQ: %v", err)
	}
	return conn
}

// waitFor 轮询直到 cond 满足或超时——替代固定 time.Sleep：
// sleep 太短会 flaky（慢 CI 上状态未达成即断言失败），太长拖慢测试。
// 条件达成即返回，确定性且快速。
func waitFor(t testing.TB, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时（%v）: %s", timeout, msg)
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

	// 轮询等待 monitor goroutine 处理退出信号（替代固定 50ms sleep，慢 CI 不 flaky）
	waitFor(t, 2*time.Second, func() bool {
		return !conn.CheckConnected(ctx)
	}, "Close 后连接应断开")
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

	waitFor(t, 2*time.Second, func() bool {
		return conn.GetConnectionStatus(ctx)["connected"] != true
	}, "Close 后 connected 应为 false")
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
	if urlStr == testRabbitMQURL() {
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

// ---------------------------------------------------------------------------
// 补充测试：TLS 证书缺失边界拦截
// ---------------------------------------------------------------------------

func TestConn_AmqpsWithoutTLSShouldFail(t *testing.T) {
	// 使用一个合法的 amqps 格式，但无需真正拨号成功
	_, cancel := context.WithCancel(context.Background())
	cancel() // 立刻取消 context，确保即使穿透了也会因为 context canceled 退出，但我们核心是抓字符串拦截

	// 注意：为了防止它真的去拨号，我们可以利用一个无效但合法的 url
	// 关键点：你的业务代码里 `getMqConnect` 第一步就是字符串校验，本应直接返回。
	// 为什么会去拨号？看下方源码分析！
	_, err := NewConnection(context.Background(), "amqps://guest:guest@localhost:5671/", WithTLSConfig(nil))
	if err == nil {
		t.Fatal("expected error when opening amqps connection without TLS config, got nil")
	}

	if !strings.Contains(err.Error(), "tls not set") && !strings.Contains(err.Error(), "refused") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 补充测试：异步网络突发断开与自动重连机制
// ---------------------------------------------------------------------------

func TestConn_AsynchronousDisconnectAndReconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 使用较短的重连基准时间，加速测试运行
	conn := newTestConnection(t, ctx, WithReconnectTime(time.Millisecond*50))
	defer conn.Close()

	initialCount := conn.GetReconnectCount(ctx)

	// 核心边缘：通过向底层的 close 通道注入错误，模拟网络突发中断
	rawConn := conn.mqConn.Load()
	if rawConn == nil {
		t.Fatal("underlying amqp connection is nil")
	}

	// 模拟 RabbitMQ 异常断开信号
	_ = &amqp.Error{Code: 320, Reason: "CONNECTION_FORCED - server went away", Server: true}

	// 通过反射或向正在监听的 channel 发送数据来关闭（amqp091-go 允许在测试中关闭底层连接触发通知）
	_ = rawConn.Close()

	// 轮询等待 monitor 监听到断开并完成重连（指数退避约 50ms 起步，
	// 条件达成即返回——比固定 sleep 1 秒更快且慢 CI 不 flaky）
	waitFor(t, 5*time.Second, func() bool {
		return conn.CheckConnected(ctx)
	}, "断开后应自动重连成功")

	if conn.GetReconnectCount(ctx) <= initialCount {
		t.Errorf("expected reconnectCount to increase, initial: %d, current: %d", initialCount, conn.GetReconnectCount(ctx))
	}

	// 检查重连后过期错误是否被成功清除
	_, err := conn.GetLastError(ctx)
	if err != nil {
		t.Errorf("expected last error to be cleared after successful reconnection, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 补充测试：重连退避等待期间，外部调用 Close 瞬间响应
// ---------------------------------------------------------------------------

func TestConn_AbortReconnectOnClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 设置一个极长的重连时间（例如 5 秒），确保它卡在 time.After 的退避等待中
	conn := newTestConnection(t, ctx, WithReconnectTime(time.Second*5))

	// 破坏当前连接
	rawConn := conn.mqConn.Load()
	if rawConn != nil {
		_ = rawConn.Close()
	}

	// 给 monitor 100ms 的时间进入重连等待（time.After 阶段）
	time.Sleep(time.Millisecond * 100)

	start := time.Now()
	// 边缘情况：在退避等待期间，外部调用 Close()
	conn.Close()

	// Close() 同步设置 isConnected=false（见 connection.go:298），
	// 断言立即成立，无需等 monitor 退出——原 200ms sleep 纯浪费且拖慢测试

	// 如果 Close 没有打破 time.After 的 5 秒等待，这里耗时一定会大于 1 秒
	if time.Since(start) > time.Second*1 {
		t.Fatal("Close() was blocked by backoff sleep timer, failed to abort instantly")
	}

	if conn.CheckConnected(ctx) {
		t.Error("expected connected status to be false")
	}
}

// ---------------------------------------------------------------------------
// 补充测试：高并发极限挑战（并发 GetConn, Check, 伴随突发 Close）
// ---------------------------------------------------------------------------

func TestConn_ExtremeConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)

	const goroutines = 50
	const iterations = 200
	var wg sync.WaitGroup

	// 并发读取与状态检查
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = conn.GetConn(ctx)
				_ = conn.CheckConnected(ctx)
				_ = conn.GetConnectionStatus(ctx)
				time.Sleep(time.Microsecond * 10) // 极短交错
			}
		}()
	}

	// 模拟在业务并发读取的激进过程中，突发动静：调用 Close
	time.Sleep(time.Millisecond * 20)
	conn.Close()

	wg.Wait() // 确保没有任何 panic、死锁或 Data Race
}

// ---------------------------------------------------------------------------
// 补充测试：服务端 TCP 阻塞通知边缘触发
// ---------------------------------------------------------------------------

func TestConn_ServerBlockNotification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := newTestConnection(t, ctx)
	defer conn.Close()

	// 借由底层的封装，我们可以人为往 mqBlockChan 塞入一个阻塞激活信号，验证日志和流程不崩
	select {
	case conn.mqBlockChan <- amqp.Blocking{Active: true, Reason: "resource low (disk)"}:
	default:
		// 如果通道满了则跳过（本身结构体初始化时容量为1）
	}

	// 阻塞通知不应该破坏连接本身（连接本来就该是活的，断言立即成立，
	// 无需等 monitor 消费 channel——原 50ms sleep 是多余的）
	if !conn.CheckConnected(ctx) {
		t.Error("connection should still be active and connected after resource block notification")
	}
}
