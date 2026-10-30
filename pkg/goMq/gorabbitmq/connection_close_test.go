package gorabbitmq

import (
	"context"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// 本文件覆盖 Close 的关闭信号语义：守卫必须是 closeOnce，不能依赖 isConnected。
// 用例均不建立真实 AMQP 连接，因此不依赖 broker、不会被跳过。

// newFakeDisconnectedConn 构造一条"曾经过、现已断线"的连接：
// isConnected 为 false（等价于 handleCloseAndReconnect 置 false 后正在退避重连的状态），
// 但 monitor 协程仍在运行，唯一的退出入口是 connCloseCh。
func newFakeDisconnectedConn() *Connection {
	return &Connection{connCloseCh: make(chan struct{})}
}

// TestCloseSendsSignalWhenAlreadyDisconnected 复现原缺陷：
// 断线状态下调用 Close，若用 CAS(isConnected) 做守卫则整个 Close 是空操作，
// connCloseCh 永不关闭，monitor 与所有 Done() 消费方都将永久滞留。
func TestCloseSendsSignalWhenAlreadyDisconnected(t *testing.T) {
	c := newFakeDisconnectedConn()
	c.Close()

	select {
	case <-c.Done():
		// 收到关闭信号，符合预期
	default:
		t.Fatal("Close() on a disconnected connection did not signal connCloseCh, monitor would leak")
	}

	// 关闭信号必须是"已关闭"而非"有个值可读"：Done() 关闭后任意数量消费者都能立刻返回
	select {
	case _, ok := <-c.Done():
		if ok {
			t.Error("connCloseCh should be closed, not receive a value")
		}
	default:
		t.Fatal("closed channel should be ready for receive")
	}
}

// TestCloseIsIdempotent 验证重复调用不会 panic（close of closed channel），
// 且断开状态不会被并发/串行调用打乱。
func TestCloseIsIdempotent(t *testing.T) {
	c := newFakeDisconnectedConn()
	c.isConnected.Store(true)

	// 先正常关闭一次，再从"已断开"状态重复关闭：多次 Close 都必须安全
	c.Close()
	c.Close()

	// 并发调用同样不能 panic，并且关闭后 CheckConnected 必须为 false
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 32; i++ {
			c.Close()
		}
	}()
	other := make(chan struct{})
	go func() {
		defer close(other)
		for i := 0; i < 32; i++ {
			c.Close()
		}
	}()
	<-done
	<-other

	if c.CheckConnected(context.Background()) {
		t.Error("CheckConnected should be false after Close")
	}
}

// TestCloseAfterReconnectFailureStillSignals 覆盖更贴近故障现场的一串状态迁移：
// 建连成功(true) → 断线(false) → 重连失败仍为 false → Close 必须发出信号。
func TestCloseAfterReconnectFailureStillSignals(t *testing.T) {
	c := newFakeDisconnectedConn()
	c.isConnected.Store(true)
	c.mqConn.Store(&amqp.Connection{})

	// 断线：handleCloseAndReconnect 的第一步
	c.isConnected.Store(false)

	c.Close()

	select {
	case <-c.Done():
	default:
		t.Fatal("Close() must signal after a failed reconnect cycle")
	}
}

// TestMonitorExitsOnCloseWhenDisconnected 端到端验证修复的实际收益：
// monitor 协程能在断线状态下被 Close 唤醒并退出，而不是无限拨号。
func TestMonitorExitsOnCloseWhenDisconnected(t *testing.T) {
	c := newFakeDisconnectedConn()
	c.connectionOptions = defaultConnectionOptions()
	c.mqBlockChan = make(chan amqp.Blocking, 1)
	c.mqCloseChan = make(chan *amqp.Error, 1)

	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		c.monitor(context.Background())
	}()

	// 不断开、不触发任何 AMQP 事件，直接 Close：修复前 monitor 不会退出
	c.Close()

	select {
	case <-monitorDone:
	case <-time.After(3 * time.Second):
		t.Fatal("monitor goroutine did not exit after Close while disconnected")
	}
}
