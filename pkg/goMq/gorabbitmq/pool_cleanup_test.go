package gorabbitmq

import (
	"context"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// 本文件覆盖 cleanup 拆分后的锁内摘除逻辑（reapPoolConns）与锁外探测（probePoolConns）。
// 这些用例刻意不建立真实 AMQP 连接：伪造的连接只驱动本地原子状态判断，
// 因此不依赖 broker、不会跳过，也不会在持锁路径上产生网络交互。

// newFakeAliveConn 构造本地状态显示"可用"的连接：
// isConnected 为 true，且 mqConn 持有零值 amqp.Connection（其 closed 原子布尔为 false，IsClosed 返回 false）。
// connCloseCh 必须非空，否则 Close 会 panic。
func newFakeAliveConn() *Connection {
	c := &Connection{connCloseCh: make(chan struct{})}
	c.isConnected.Store(true)
	c.mqConn.Store(&amqp.Connection{})

	return c
}

// newFakeDeadConn 构造本地状态显示"已断开"的连接
func newFakeDeadConn() *Connection {
	return &Connection{connCloseCh: make(chan struct{})}
}

// newOfflinePool 构造一个不发起任何网络连接、仅用于测试池内摘除逻辑的连接池
func newOfflinePool(initialCap int, maxIdle time.Duration, conns ...*poolConn) *Pool {
	p := &Pool{
		poolOpts: &poolOptions{
			initialCap: initialCap,
			maxCap:     len(conns) + 1,
			maxIdle:    maxIdle,
		},
		poolConns: conns,
	}
	p.totalConns.Store(int64(len(conns)))

	return p
}

// countClosed 统计已关闭（isConnected 被 CAS 置否）的连接数
func countClosed(conns []*poolConn) int {
	closed := 0
	for _, pc := range conns {
		if !pc.connInfo.isConnected.Load() {
			closed++
		}
	}

	return closed
}

func TestReapPoolConnsRemovesLocallyDeadDespiteMinKeep(t *testing.T) {
	ctx := context.Background()
	// initialCap 大于连接数：minKeep 本应保护所有连接，但失效连接不受保护
	conns := []*poolConn{
		{connInfo: newFakeDeadConn(), lastUsed: time.Now()},
		{connInfo: newFakeDeadConn(), lastUsed: time.Now()},
		{connInfo: newFakeDeadConn(), lastUsed: time.Now()},
	}
	p := newOfflinePool(5, time.Hour, conns...)

	removed, remaining := p.reapPoolConns(ctx, nil)

	if len(removed) != 3 {
		t.Fatalf("removed = %d, want 3 (dead connections ignore minKeep)", len(removed))
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}
	if got := p.totalConns.Load(); got != 0 {
		t.Errorf("totalConns = %d, want 0 after removing 3 connections", got)
	}
	if len(p.poolConns) != 0 {
		t.Errorf("poolConns len = %d, want 0", len(p.poolConns))
	}
}

func TestReapPoolConnsKeepsMinKeepIdleConnections(t *testing.T) {
	ctx := context.Background()
	stale := time.Now().Add(-10 * time.Minute)
	conns := make([]*poolConn, 0, 5)
	for range 5 {
		conns = append(conns, &poolConn{connInfo: newFakeAliveConn(), lastUsed: stale})
	}
	// minKeep=2：5 条全部超时空闲，只应摘掉 3 条
	p := newOfflinePool(2, time.Minute, conns...)

	// 原地过滤复用底层数组，必须在调用前记录期望的留存顺序
	wantKept := []*poolConn{conns[3], conns[4]}

	removed, remaining := p.reapPoolConns(ctx, nil)

	if len(removed) != 3 {
		t.Fatalf("removed = %d, want 3", len(removed))
	}
	if remaining != 2 {
		t.Fatalf("remaining = %d, want 2", remaining)
	}
	if len(p.poolConns) != 2 {
		t.Fatalf("poolConns len = %d, want 2", len(p.poolConns))
	}
	// 池从尾部借出连接(getConnFromPool 取最后一个)，留存顺序错乱会让空位统计与借出次序不符
	if p.poolConns[0] != wantKept[0] || p.poolConns[1] != wantKept[1] {
		t.Errorf("kept order = [%p %p], want [%p %p]",
			p.poolConns[0], p.poolConns[1], wantKept[0], wantKept[1])
	}
	if got := p.totalConns.Load(); got != 2 {
		t.Errorf("totalConns = %d, want 2", got)
	}
	// 存活的连接不应被关闭
	if closed := countClosed(removed); closed != 0 {
		t.Errorf("removed contains %d closed connections, want all still open", closed)
	}
}

func TestReapPoolConnsRemovesProbedDeadEvenWhenFresh(t *testing.T) {
	ctx := context.Background()
	deadConn := newFakeAliveConn()
	liveConn := newFakeAliveConn()
	conns := []*poolConn{
		{connInfo: deadConn, lastUsed: time.Now()},
		{connInfo: liveConn, lastUsed: time.Now()},
	}
	// minKeep=5 且 maxIdle=1h：两条都不该被空闲规则摘掉，只锁外探测判死的那条被摘
	p := newOfflinePool(5, time.Hour, conns...)

	dead := map[*Connection]struct{}{deadConn: {}}

	removed, remaining := p.reapPoolConns(ctx, dead)

	if len(removed) != 1 || removed[0].connInfo != deadConn {
		t.Fatalf("removed = %v, want exactly the probed-dead connection", removed)
	}
	if remaining != 1 {
		t.Errorf("remaining = %d, want 1", remaining)
	}
	if p.poolConns[0].connInfo != liveConn {
		t.Errorf("survivor is not the live connection")
	}
	if got := p.totalConns.Load(); got != 1 {
		t.Errorf("totalConns = %d, want 1", got)
	}
}

// TestReapPoolConnsMatchesProbedConnAcrossNewWrapper 覆盖 Put 重新包装的场景：
// 探测期间连接可能已被取出又归还，此时池里是新的 poolConn 包装但底层 *Connection 同一条，
// dead 集合按 *Connection 匹配，因此仍能识别。
func TestReapPoolConnsMatchesProbedConnAcrossNewWrapper(t *testing.T) {
	ctx := context.Background()
	probed := newFakeAliveConn()
	// 模拟 Get 之后 Put 归还：新建包装对象，lastUsed 为当前时间
	p := newOfflinePool(0, time.Hour, &poolConn{connInfo: probed, lastUsed: time.Now()})

	dead := map[*Connection]struct{}{probed: {}}

	removed, remaining := p.reapPoolConns(ctx, dead)

	if len(removed) != 1 {
		t.Fatalf("removed = %d, want 1: probe verdict must match by *Connection, not by wrapper", len(removed))
	}
	if remaining != 0 || len(p.poolConns) != 0 {
		t.Errorf("remaining = %d, poolConns = %d, want both 0", remaining, len(p.poolConns))
	}
}

func TestReapPoolConnsNoOpAfterPoolClosed(t *testing.T) {
	ctx := context.Background()
	conns := []*poolConn{
		{connInfo: newFakeDeadConn(), lastUsed: time.Now()},
		{connInfo: newFakeDeadConn(), lastUsed: time.Now()},
	}
	p := newOfflinePool(0, time.Minute, conns...)
	p.isClosed.Store(true)

	removed, remaining := p.reapPoolConns(ctx, nil)

	if removed != nil {
		t.Errorf("removed = %v, want nil when pool is closed", removed)
	}
	if remaining != 2 {
		t.Errorf("remaining = %d, want 2 (untouched)", remaining)
	}
	if got := p.totalConns.Load(); got != 2 {
		t.Errorf("totalConns = %d, want 2 (must not be decremented after Close)", got)
	}
}

func TestProbePoolConnsDetectsLocallyDead(t *testing.T) {
	ctx := context.Background()
	alive := newFakeAliveConn()
	conns := []*poolConn{
		{connInfo: newFakeDeadConn(), lastUsed: time.Now()},
		{connInfo: alive, lastUsed: time.Now()},
	}
	p := newOfflinePool(0, time.Hour, conns...)

	dead := p.probePoolConns(ctx)

	if len(dead) != 1 {
		t.Fatalf("dead = %d entries, want 1", len(dead))
	}
	if _, ok := dead[conns[0].connInfo]; !ok {
		t.Errorf("dead set should contain the locally-dead connection")
	}
	if _, ok := dead[alive]; ok {
		t.Errorf("dead set should not contain the live connection")
	}
}

func TestProbePoolConnsReturnsEmptyAfterPoolClosed(t *testing.T) {
	ctx := context.Background()
	p := newOfflinePool(0, time.Hour,
		&poolConn{connInfo: newFakeDeadConn(), lastUsed: time.Now()},
		&poolConn{connInfo: newFakeDeadConn(), lastUsed: time.Now()},
	)
	p.isClosed.Store(true)

	if dead := p.probePoolConns(ctx); len(dead) != 0 {
		t.Errorf("dead = %d entries, want 0 when pool is closed", len(dead))
	}
}

// TestCleanupClosesRemovedConnections 验证 cleanup 端到端把失效连接摘除并关闭
func TestCleanupClosesRemovedConnections(t *testing.T) {
	ctx := context.Background()
	conns := []*poolConn{
		{connInfo: newFakeDeadConn(), lastUsed: time.Now()},
		{connInfo: newFakeAliveConn(), lastUsed: time.Now()},
	}
	p := newOfflinePool(0, time.Hour, conns...)

	// minKeep=0：dead 分支摘除第 1 条；第 2 条存活且 lastUsed 新鲜，两条规则都不命中
	p.cleanup(ctx)

	if len(p.poolConns) != 1 || p.poolConns[0].connInfo != conns[1].connInfo {
		t.Fatalf("poolConns = %v, want only the live connection", p.poolConns)
	}
	if !conns[0].connInfo.isConnected.Load() {
		t.Errorf("removed dead connection should stay in removed set without being re-opened")
	}
	if got := p.totalConns.Load(); got != 1 {
		t.Errorf("totalConns = %d, want 1", got)
	}
}
