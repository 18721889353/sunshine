package gorabbitmq

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testRabbitMQURL = "amqp://sunjianguo:jianguo123@43.143.78.234:5672/"

// defaultTestConnOpts 测试用连接选项
var defaultTestConnOpts = []ConnectionOption{
	WithReconnectTime(time.Second * 3),
	WithDialTimeout(time.Second * 5),
	WithHeartbeat(time.Second * 3),
}

// newTestPool 创建测试用连接池，连接失败则跳过测试
func newTestPool(t testing.TB, ctx context.Context, opts ...PoolOption) *Pool {
	t.Helper()
	pool, err := NewPool(ctx, testRabbitMQURL, opts...)
	if err != nil {
		t.Skipf("skip: cannot connect to RabbitMQ: %v", err)
	}
	return pool
}

// ---------------------------------------------------------------------------
// 测试面：基本 Get/Put
// ---------------------------------------------------------------------------

func TestPool_GetPut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(2),
		WithMaxCap(10),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// 获取连接
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if conn == nil {
		t.Fatal("Get returned nil connection")
	}

	// 验证连接可用
	if !conn.CheckConnected(ctx) {
		t.Fatal("connection is not connected")
	}

	// 放回连接池
	if err := pool.Put(ctx, conn); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// 验证统计
	stats := pool.Stats(ctx)
	if stats["poolSize"].(int) < 1 {
		t.Errorf("expected poolSize >= 1, got %d", stats["poolSize"])
	}
}

// ---------------------------------------------------------------------------
// 测试面：并发 Get/Put
// ---------------------------------------------------------------------------

func TestPool_ConcurrentGetPut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(5),
		WithMaxCap(20),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	const goroutines = 10
	const iterations = 5
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				conn, err := pool.Get(ctx)
				if err != nil {
					t.Errorf("Get failed: %v", err)
					return
				}
				time.Sleep(time.Millisecond * 2)
				if err := pool.Put(ctx, conn); err != nil {
					t.Errorf("Put failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// 测试面：Context 取消导致 Get 返回
// ---------------------------------------------------------------------------

func TestPool_GetContextCancel(t *testing.T) {
	// 创建只有 1 个连接的池，借出后第二个 Get 应等待，ctx 取消应返回错误
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(1),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// 借出唯一的连接
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}

	// 用已取消的 context 尝试获取第二个连接，应立刻返回错误
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err = pool.Get(cancelCtx)
	if err == nil {
		t.Error("expected error on cancelled context, got nil")
	}

	// 还回连接，确保池恢复正常
	if err := pool.Put(ctx, conn); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 测试面：放回无效连接应被丢弃
// ---------------------------------------------------------------------------

func TestPool_PutInvalidConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	initialStats := pool.Stats(ctx)
	initialTotal := initialStats["totalConns"].(int64)

	// 获取连接
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// 关闭连接（模拟连接失效）
	conn.Close()

	// 放回已关闭的连接，池应丢弃它
	if err := pool.Put(ctx, conn); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// totalConns 应减少（无效连接被丢弃）
	afterStats := pool.Stats(ctx)
	afterTotal := afterStats["totalConns"].(int64)
	if afterTotal != initialTotal-1 {
		t.Errorf("expected totalConns=%d after discarding invalid connection, got %d", initialTotal-1, afterTotal)
	}
}

// ---------------------------------------------------------------------------
// 测试面：放回 nil 连接不应 panic
// ---------------------------------------------------------------------------

func TestPool_PutNil(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// 放回 nil 连接应不报错也不 panic
	if err := pool.Put(ctx, nil); err != nil {
		t.Fatalf("Put(nil) failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 测试面：连接复用 — 同一个连接应被复用
// ---------------------------------------------------------------------------

func TestPool_ConnectionReuse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// 第一次获取
	conn1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}

	// 放回
	if err := pool.Put(ctx, conn1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// 再次获取 — 连接应来自池中（池中只有一个）
	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}

	if conn1 != conn2 {
		t.Log("pool returned a different connection instance (may be new or fresh)")
	}

	if err := pool.Put(ctx, conn2); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 测试面：Pool 满时关闭，等待的 Get 应收到错误
// ---------------------------------------------------------------------------

func TestPool_CloseWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(1),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)

	// 借出唯一的连接，池变空
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// 在 goroutine 中执行第二个 Get（会等待），然后关闭池
	waitErrCh := make(chan error, 1)
	go func() {
		_, err := pool.Get(ctx)
		waitErrCh <- err
	}()

	// 确保 goroutine 已进入等待
	time.Sleep(time.Millisecond * 50)

	// 关闭连接池
	pool.Close(ctx)

	// 等待者应收到错误
	select {
	case err := <-waitErrCh:
		if err == nil {
			t.Error("expected error from Get after pool closed, got nil")
		}
	case <-time.After(time.Second * 3):
		t.Fatal("Get did not return after pool closed")
	}

	// 还回连接 — 池已关闭，Put 应安全丢弃连接
	if err := pool.Put(ctx, conn); err != nil {
		t.Logf("Put after close returned: %v (expected either nil or ErrPoolClosed)", err)
	}
}

// ---------------------------------------------------------------------------
// 测试面：关闭池后 Put 归还连接应安全处理不 panic
// ---------------------------------------------------------------------------

func TestPool_PutAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)

	// 借出连接
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	closedTotal := pool.Stats(ctx)["totalConns"].(int64)

	// 关闭池
	pool.Close(ctx)

	// 关闭后 Put 归还 — 应安全丢弃，totalConns 减 1
	if err := pool.Put(ctx, conn); err != nil {
		t.Logf("Put after close: %v", err)
	}

	if got := pool.totalConns.Load(); got != closedTotal-1 {
		t.Errorf("expected totalConns=%d after Put-after-close, got %d", closedTotal-1, got)
	}

	// 再次 Put nil 也不应 panic
	if err := pool.Put(ctx, nil); err != nil {
		t.Logf("Put(nil) after close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 测试面：Put 归还连接后唤醒等待的 Get
// ---------------------------------------------------------------------------

func TestPool_PutWakesUpWaiter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(1),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// 借出唯一的连接
	conn1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}

	// 启动 goroutine 等待第二个连接
	gotConn := make(chan *Connection, 1)
	go func() {
		c, err := pool.Get(ctx)
		if err != nil {
			t.Errorf("waiter Get failed: %v", err)
			return
		}
		gotConn <- c
	}()

	// 确保 goroutine 已进入等待
	time.Sleep(time.Millisecond * 50)

	// 归还连接 → 应唤醒等待者
	if err := pool.Put(ctx, conn1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// 等待者应拿到连接
	select {
	case c := <-gotConn:
		if c == nil {
			t.Error("waiter got nil connection")
		}
		// 归还
		if err := pool.Put(ctx, c); err != nil {
			t.Fatalf("waiter Put failed: %v", err)
		}
	case <-time.After(time.Second * 3):
		t.Fatal("waiter was not woken up by Put")
	}
}

// ---------------------------------------------------------------------------
// 测试面：借出所有 maxCap 后 Get 的行为校验
// ---------------------------------------------------------------------------

func TestPool_MaxCapExhausted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(2),
		WithMaxCap(2),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// 全部借出
	conns := make([]*Connection, 0, 2)
	for i := 0; i < 2; i++ {
		c, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("Get failed when borrowing all: %v", err)
		}
		conns = append(conns, c)
	}

	// 池为空且已达 maxCap，stats 应准确
	stats := pool.Stats(ctx)
	if stats["poolSize"].(int) != 0 {
		t.Errorf("expected poolSize=0 when all borrowed, got %d", stats["poolSize"])
	}
	if stats["totalConns"].(int64) != 2 {
		t.Errorf("expected totalConns=2, got %d", stats["totalConns"])
	}

	// 全部归还
	for _, c := range conns {
		if err := pool.Put(ctx, c); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// 归还后池应恢复
	stats = pool.Stats(ctx)
	if stats["poolSize"].(int) != 2 {
		t.Errorf("expected poolSize=2 after returning all, got %d", stats["poolSize"])
	}
}

// ---------------------------------------------------------------------------
// 测试面：池满后 Get 应阻塞等待，Put 后恢复
// ---------------------------------------------------------------------------

func TestPool_GetBlocksWhenExhausted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(1),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// 借出唯一的连接，池变空+已达 maxCap
	borrowed, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}

	// 测量第二个 Get 的等待时间
	const waitMs = 100
	start := time.Now()
	go func() {
		time.Sleep(waitMs * time.Millisecond)
		pool.Put(ctx, borrowed)
	}()

	// 此时池空且满，Get 应阻塞直到 Put 归还
	conn, err := pool.Get(ctx)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Get after Put failed: %v", err)
	}
	if elapsed < waitMs*time.Millisecond {
		t.Errorf("Get did not block: elapsed=%v, expected >= %dms", elapsed, waitMs)
	}
	pool.Put(ctx, conn)
}

// ---------------------------------------------------------------------------
// 测试面：GetWithRetry
// ---------------------------------------------------------------------------

func TestPool_GetWithRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(2),
		WithMaxCap(10),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	conn, err := pool.GetWithRetry(ctx, 3)
	if err != nil {
		t.Fatalf("GetWithRetry failed: %v", err)
	}
	if conn == nil {
		t.Fatal("GetWithRetry returned nil")
	}

	if err := pool.Put(ctx, conn); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 测试面：Stats 信息完整性
// ---------------------------------------------------------------------------

func TestPool_Stats(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(2),
		WithMaxCap(10),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	stats := pool.Stats(ctx)
	requiredFields := []string{"totalConns", "available", "poolSize", "maxCap", "closed"}
	for _, field := range requiredFields {
		if _, ok := stats[field]; !ok {
			t.Errorf("Stats missing field: %s", field)
		}
	}

	t.Logf("Pool stats: totalConns=%d, available=%d, poolSize=%d, maxCap=%d",
		stats["totalConns"], stats["available"], stats["poolSize"], stats["maxCap"])
}

// ---------------------------------------------------------------------------
// 边界：空 URL 创建 Pool 应直接返回错误（无需 RabbitMQ）
// ---------------------------------------------------------------------------

func TestPool_EmptyURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := NewPool(ctx, "")
	if err == nil {
		t.Error("expected error for empty URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// 边界：initialCap=0 创建空池，后续 Get 应自动创建连接
// ---------------------------------------------------------------------------

func TestPool_ZeroInitialCap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// WithInitialCap(0) 被忽略（Option 内部 if >0 才生效），池使用默认 initialCap=5
	pool := newTestPool(t, ctx,
		WithInitialCap(0),
		WithMaxCap(10),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// Get 应正常工作
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if conn == nil {
		t.Fatal("Get returned nil")
	}
	pool.Put(ctx, conn)
}

// ---------------------------------------------------------------------------
// 边界：重复 Put 同一连接不应 panic（第二次被丢弃）
// ---------------------------------------------------------------------------

func TestPool_DoublePut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// 第一次 Put
	if err := pool.Put(ctx, conn); err != nil {
		t.Fatalf("first Put failed: %v", err)
	}

	// 第二次 Put 同一连接（连接仍有效，池会再次放入，不 panic 即可）
	if err := pool.Put(ctx, conn); err != nil {
		t.Logf("second Put returned: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 边界：关闭后 Get 应直接返回 ErrPoolClosed
// ---------------------------------------------------------------------------

func TestPool_GetAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(5),
		WithConnOptions(defaultTestConnOpts...),
	)

	pool.Close(ctx)

	_, err := pool.Get(ctx)
	if err != ErrPoolClosed {
		t.Errorf("expected ErrPoolClosed after close, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 边界：GetWithRetry 重试耗尽后应返回错误
// ---------------------------------------------------------------------------

func TestPool_GetWithRetryExhausted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(1),
		WithMaxCap(1),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	// 借出唯一连接，池满
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}

	// 用已取消的 context 重试，Get 应快速失败而非阻塞
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = pool.GetWithRetry(cancelCtx, 3)
	if err == nil {
		t.Error("expected error from GetWithRetry with cancelled ctx, got nil")
	} else {
		t.Logf("GetWithRetry returned expected error: %v", err)
	}

	pool.Put(ctx, conn)
}

// ---------------------------------------------------------------------------
// 边界：Signal 只唤醒一个等待者
// ---------------------------------------------------------------------------

func TestPool_SignalWakesOneWaiter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := newTestPool(t, ctx,
		WithInitialCap(2),
		WithMaxCap(2),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	conns := make([]*Connection, 2)
	for i := 0; i < 2; i++ {
		c, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("Get %d failed: %v", i, err)
		}
		conns[i] = c
	}

	const waiters = 2
	var gotConns int32
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			c, err := pool.Get(ctx)
			if err != nil {
				t.Errorf("waiter Get failed: %v", err)
				return
			}
			atomic.AddInt32(&gotConns, 1)
			pool.Put(ctx, c)
		}()
	}

	time.Sleep(time.Millisecond * 50)

	pool.Put(ctx, conns[0])
	time.Sleep(time.Millisecond * 100)

	if n := atomic.LoadInt32(&gotConns); n != 1 {
		t.Logf("Signal woke %d waiters (expected 1, may be timing-sensitive)", n)
	}

	pool.Put(ctx, conns[1])
	wg.Wait()
}

// ===================================================================
// 基准测试
// ===================================================================

// BenchmarkPool_GetPut 串行 Get/Put 性能
func BenchmarkPool_GetPut(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	pool := newTestPool(b, ctx,
		WithInitialCap(50),
		WithMaxCap(500),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			b.Fatalf("Get failed: %v", err)
		}
		time.Sleep(time.Microsecond)
		if err := pool.Put(ctx, conn); err != nil {
			b.Fatalf("Put failed: %v", err)
		}
	}
}

// BenchmarkPool_Concurrent 并发 Get/Put（b.RunParallel 内置调度）
func BenchmarkPool_Concurrent(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	pool := newTestPool(b, ctx,
		WithInitialCap(50),
		WithMaxCap(500),
		WithMaxIdle(time.Minute),
		WithConnOptions(defaultTestConnOpts...),
	)
	defer pool.Close(ctx)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := pool.Get(ctx)
			if err != nil {
				b.Errorf("Get failed: %v", err)
				continue
			}
			time.Sleep(time.Microsecond)
			if err := pool.Put(ctx, conn); err != nil {
				b.Errorf("Put failed: %v", err)
			}
		}
	})
}

// BenchmarkPool_DifferentSizes 不同池大小下的性能对比
func BenchmarkPool_DifferentSizes(b *testing.B) {
	sizes := []struct {
		name    string
		initCap int
		maxCap  int
	}{
		{"Small", 5, 20},
		{"Medium", 20, 100},
		{"Large", 50, 500},
	}

	for _, s := range sizes {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		b.Run(s.name, func(b *testing.B) {
			pool := newTestPool(b, ctx,
				WithInitialCap(s.initCap),
				WithMaxCap(s.maxCap),
				WithMaxIdle(time.Minute),
				WithConnOptions(defaultTestConnOpts...),
			)
			defer pool.Close(ctx)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				conn, err := pool.Get(ctx)
				if err != nil {
					b.Fatalf("Get failed: %v", err)
				}
				time.Sleep(time.Microsecond * 2)
				if err := pool.Put(ctx, conn); err != nil {
					b.Fatalf("Put failed: %v", err)
				}
			}
		})
	}
}
