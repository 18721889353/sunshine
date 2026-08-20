package metrics

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// ConnectionOption set connection option
type ConnectionOption func(*connectionOptions)

type connectionOptions struct {
	zapLogger       *zap.Logger
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

// WithConnectionsLogger set logger for connection
func WithConnectionsLogger(l *zap.Logger) ConnectionOption {
	return func(o *connectionOptions) {
		if l != nil {
			o.zapLogger = l
		}
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
	zapLogger         *zap.Logger
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

	if l.zapLogger != nil {
		l.zapLogger.Info("new grpc client connected", zap.String("client", conn.RemoteAddr().String()), zap.Int("active connections", count))
	}

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

	if l.zapLogger != nil {
		l.zapLogger.Info("grpc client disconnected", zap.String("client", clientAddr), zap.Int("active connections", count))
	}

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
			if c.listener.zapLogger != nil {
				c.listener.zapLogger.Warn("failed to close connection",
					zap.String("client", clientAddr),
					zap.Error(err),
				)
			} else {
				fmt.Printf("close connection error (client %s): %v\n", clientAddr, err)
			}
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
		zapLogger:       o.zapLogger,
		connectionGauge: o.connectionGauge,
	}
}
