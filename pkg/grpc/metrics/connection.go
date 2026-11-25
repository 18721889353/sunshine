package metrics

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/stats"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ConnectionOption set connection option
type ConnectionOption func(*connectionOptions)

type connectionOptions struct {
	connectionGauge prometheus.Gauge
}

func defaultConnectionOptions() *connectionOptions {
	return &connectionOptions{}
}

func (o *connectionOptions) apply(opts ...ConnectionOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithConnectionsGauge set prometheus gauge for connections
func WithConnectionsGauge() ConnectionOption {
	return func(o *connectionOptions) {
		o.connectionGauge = grpcConnectionGauge
	}
}

// ------------------------------------------------------------------------------------------

// CustomConn custom connections, intercept disconnected behavior
type CustomConn struct {
	net.Conn
	listener  *CustomListener
	closeOnce sync.Once
}

// CustomListener custom listener for counting connections
type CustomListener struct {
	net.Listener
	activeConnections int
	mu                sync.Mutex
	connectionGauge   prometheus.Gauge
}

// Accept waits for and returns the next connection to the listener.
func (l *CustomListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	var count int
	l.mu.Lock()
	l.activeConnections++
	count = l.activeConnections
	l.mu.Unlock()

	if l.connectionGauge != nil {
		l.connectionGauge.Set(float64(count))
	}

	return &CustomConn{
		Conn:     conn,
		listener: l,
	}, nil
}

// GetActiveConnections returns the number of active connections.
func (l *CustomListener) GetActiveConnections() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.activeConnections
}

// closes the connection and decrements the active connections count.
func (l *CustomListener) closeConnection(clientAddr string) {
	var count int
	l.mu.Lock()
	l.activeConnections--
	count = l.activeConnections
	l.mu.Unlock()

	logger.InfoWithCtx(context.Background(), "grpc client disconnected",
		logger.String("client", clientAddr),
		logger.Int("active_connections", count),
	)

	if l.connectionGauge != nil {
		l.connectionGauge.Set(float64(count))
	}
}

// Close closes the listener, any blocked except operations will be unblocked and return errors.
//
// gRPC 内部 transport 关闭和 Server 清理连接两条路径都会调用 Close，
// 这里通过 sync.Once 保证每个连接只关闭、计数一次，避免连接数变成负数。
func (c *CustomConn) Close() error {
	c.closeOnce.Do(func() {
		clientAddr := c.Conn.RemoteAddr().String()
		err := c.Conn.Close()
		if err != nil && !errors.Is(err, net.ErrClosed) {
			logger.WarnWithCtx(context.Background(), "failed to close connection",
				logger.String("client", clientAddr),
				logger.Err(err),
			)
		}
		c.listener.closeConnection(clientAddr)
	})
	return nil
}

// NewCustomListener creates a new custom listener.
func NewCustomListener(listener net.Listener, opts ...ConnectionOption) *CustomListener {
	o := defaultConnectionOptions()
	o.apply(opts...)

	return &CustomListener{
		Listener:        listener,
		connectionGauge: o.connectionGauge,
	}
}

// ------------------------------------------------------------------------------------------
// 客户端侧活跃连接数（与 server 侧 CustomListener 计数对称）。
//
// provider 不提供 client 连接 gauge，用 gRPC stats.Handler 的
// ConnBegin/ConnEnd 回调计数：ConnBegin = 底层 transport 建立、
// ConnEnd = transport 关闭，语义与 server 侧 Accept/Close 对称。
// gauge 为进程级单值（与 server 侧一致，不带 target label——
// 多 target 连接合并计数，先保证对称可观测，需要下钻再加 label）。
var grpcClientConnectionGauge = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "grpc_client_active_connections",
		Help: "Current number of active outbound gRPC connections to downstream services",
	},
)

// 活跃连接计数是【进程级】状态：每个 grpc.ClientConn 都会 new 一个 handler，
// 若计数放在 handler 实例里，多个 handler 各自从 0 算并 Set 同一个全局 gauge，
// 会互相覆盖。故 count 与 gauge 一样必须是包级单例。
var clientConnCount atomic.Int64

// clientConnCountingHandler 统计客户端到下游的活跃连接数。
// 与 otel 的 stats.Handler 并存（gRPC 支持注册多个 handler，互不覆盖）。
//
// 不直接用 gauge.Inc/Dec：gRPC 的 ConnEnd 由内部多条关闭路径触发
// （Close/goaway/read error），虽内部有 state 防重，但把正确性寄托在
// 第三方内部实现上很脆弱——多 Dec 会让 gauge 变负数（活跃连接数语义错误）。
// 用原子计数 + clamp(>=0)，再 Set 到 gauge：自己兜底，不依赖 gRPC 保证。
type clientConnCountingHandler struct{}

// NewClientConnCountingHandler 返回连接计数 stats.Handler（enableMetrics 时注册）。
func NewClientConnCountingHandler() stats.Handler {
	return &clientConnCountingHandler{}
}

// TagRPC 实现 stats.Handler（连接级计数不关心 RPC 级事件）。
func (h *clientConnCountingHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

// HandleRPC 实现 stats.Handler（连接级计数不关心 RPC 级事件）。
func (h *clientConnCountingHandler) HandleRPC(_ context.Context, _ stats.RPCStats) {}

// HandleConn 按 transport 生命周期增减计数（clamp >=0，防多 Dec 变负）。
func (h *clientConnCountingHandler) HandleConn(_ context.Context, cs stats.ConnStats) {
	var delta int64
	switch cs.(type) {
	case *stats.ConnBegin:
		delta = 1
	case *stats.ConnEnd:
		delta = -1
	default:
		return
	}
	for {
		cur := clientConnCount.Load()
		next := cur + delta
		if next < 0 {
			next = 0 // clamp：重复 ConnEnd 不应产生负数
		}
		if clientConnCount.CompareAndSwap(cur, next) {
			grpcClientConnectionGauge.Set(float64(next))
			return
		}
	}
}

// TagConn 实现 stats.Handler（连接级计数不关心 conn tag）。
func (h *clientConnCountingHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}
