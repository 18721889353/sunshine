package metrics

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

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
