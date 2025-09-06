package gorabbitmq

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// DefaultURL default rabbitmq url
const DefaultURL = "amqp://guest:guest@localhost:5672/"

var defaultLogger, _ = zap.NewProduction()

// ConnectionOption connection option.
type ConnectionOption func(*connectionOptions)

type connectionOptions struct {
	tlsConfig       *tls.Config   // tls config, if the url is amqps this field must be set
	reconnectTime   time.Duration // reconnect time interval, default is 3s
	dialTimeout     time.Duration // dial timeout for the connection, default is 5s
	heartbeat       time.Duration
	deadlineTimeout time.Duration
	zapLog          *zap.Logger
	maxRetries      int // 最大重连次数，0表示无限重试
}

func (o *connectionOptions) apply(opts ...ConnectionOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// default connection settings
func defaultConnectionOptions() *connectionOptions {
	return &connectionOptions{
		tlsConfig:       nil,
		reconnectTime:   time.Second * 3,
		dialTimeout:     time.Second * 5,
		heartbeat:       time.Second * 3,
		deadlineTimeout: time.Second * 30,
		zapLog:          defaultLogger,
		maxRetries:      0, // 默认无限重试
	}
}

// WithTLSConfig set tls config option.
func WithTLSConfig(tlsConfig *tls.Config) ConnectionOption {
	return func(o *connectionOptions) {
		if tlsConfig == nil {
			tlsConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		}
		o.tlsConfig = tlsConfig
	}
}

// WithReconnectTime set reconnect time interval option.
func WithReconnectTime(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 3
		}
		o.reconnectTime = d
	}
}

// WithDialTimeout set dial timeout option.
func WithDialTimeout(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 5
		}
		o.dialTimeout = d
	}
}
func WithHeartbeat(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 5
		}
		o.heartbeat = d
	}
}

func WithDeadlineTimeout(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 30
		}
		o.deadlineTimeout = d
	}
}

// WithLogger set logger option.
func WithLogger(zapLog *zap.Logger) ConnectionOption {
	return func(o *connectionOptions) {
		if zapLog == nil {
			return
		}
		o.zapLog = zapLog
	}
}

// WithMaxRetries 设置最大重连次数，-1表示无限重试，默认为-1
func WithMaxRetries(maxRetries int) ConnectionOption {
	return func(o *connectionOptions) {
		o.maxRetries = maxRetries
	}
}

// -------------------------------------------------------------------------------------------

// Connection rabbitmq connection
type Connection struct {
	mutex sync.Mutex

	url             string
	tlsConfig       *tls.Config
	reconnectTime   time.Duration
	dialTimeout     time.Duration
	heartbeat       time.Duration
	deadlineTimeout time.Duration
	maxRetries      int
	exit            chan struct{}
	zapLog          *zap.Logger

	conn        *amqp.Connection
	blockChan   chan amqp.Blocking
	closeChan   chan *amqp.Error
	isConnected bool

	// 连接状态统计
	reconnectCount int64
	lastError      error
	lastErrorTime  time.Time
}

// NewConnection rabbitmq connection
func NewConnection(url string, opts ...ConnectionOption) (*Connection, error) {
	if url == "" {
		return nil, errors.New("url is empty")
	}

	o := defaultConnectionOptions()
	o.apply(opts...)
	connection := &Connection{
		url:             url,
		reconnectTime:   o.reconnectTime,
		tlsConfig:       o.tlsConfig,
		dialTimeout:     o.dialTimeout,
		heartbeat:       o.heartbeat,
		deadlineTimeout: o.deadlineTimeout,
		maxRetries:      o.maxRetries,
		exit:            make(chan struct{}),
		zapLog:          o.zapLog,
	}

	conn, err := connect(connection)
	if err != nil {
		logger.Error("[rabbitmq connection] connection error", zap.String("url", url), zap.String("err", err.Error()))
		return nil, err
	}

	connection.conn = conn
	connection.blockChan = connection.conn.NotifyBlocked(make(chan amqp.Blocking, 1))
	connection.closeChan = connection.conn.NotifyClose(make(chan *amqp.Error, 1))
	connection.isConnected = true

	go connection.monitor()

	connection.zapLog.Info("[rabbitmq connection] connected successfully", zap.String("url", url))
	return connection, nil
}

func connect(c *Connection) (*amqp.Connection, error) {
	url := c.url
	tlsConfig := c.tlsConfig
	dialTimeout := c.dialTimeout
	heartbeat := c.heartbeat
	deadlineTimeout := c.deadlineTimeout
	var (
		conn *amqp.Connection
		err  error
	)
	if strings.HasPrefix(url, "amqps://") {
		if tlsConfig == nil {
			return nil, errors.New("tls not set, e.g. NewConnection(url, WithTLSConfig(tlsConfig))")
		}
		conn, err = amqp.DialTLS(url, tlsConfig)
		if err != nil {
			return nil, err
		}
	} else {
		conn, err = amqp.DialConfig(url, amqp.Config{
			Dial: func(network, addr string) (net.Conn, error) {
				//return net.DialTimeout(network, addr, dialTimeout) // 使用自定义的超时时间
				conn, err := net.DialTimeout(network, addr, dialTimeout)
				if err != nil {
					return nil, err
				}
				err = conn.SetDeadline(time.Now().Add(deadlineTimeout)) // 设置读写超时时间
				if err != nil {
					return nil, err
				}
				return conn, nil
			},
			Heartbeat: heartbeat, // 增加心跳间隔
		})
		if err != nil {
			return nil, err
		}
	}

	return conn, nil
}

// CheckConnected 检查连接是否正常
func (c *Connection) CheckConnected() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.isConnected && c.conn != nil && !c.conn.IsClosed()
}

func (c *Connection) monitor() {
	reconnectTip := fmt.Sprintf("[rabbitmq connection] lost connection, attempting reconnect in %s", c.reconnectTime)

	for {
		select {
		case <-c.exit:
			_ = c.closeConn()
			c.zapLog.Info("[rabbitmq connection] closed")
			return
		case b := <-c.blockChan:
			if b.Active {
				c.zapLog.Error("[rabbitmq connection] TCP blocked", zap.String("reason", b.Reason))
			} else {
				c.zapLog.Info("[rabbitmq connection] TCP unblocked")
			}
		case closeChanErr := <-c.closeChan:
			c.mutex.Lock()
			c.isConnected = false
			c.lastError = closeChanErr
			c.lastErrorTime = time.Now()
			c.mutex.Unlock()

			atomic.AddInt64(&c.reconnectCount, 1)
			retryCount := c.GetReconnectCount()

			// 检查是否超过最大重试次数
			if c.maxRetries > 0 && int(retryCount) > c.maxRetries {
				c.zapLog.Error("[rabbitmq connection] max retries exceeded, stopping reconnection attempts",
					zap.Int64("retryCount", retryCount),
					zap.Int("maxRetries", c.maxRetries),
					zap.String("url", c.url))
				return
			}

			if closeChanErr != nil {
				c.zapLog.Error("[rabbitmq connection] lost connection error",
					zap.String("err", closeChanErr.Error()),
					zap.Int64("retryCount", retryCount),
					zap.String("url", c.url))
			} else {
				c.zapLog.Error("[rabbitmq connection] lost connection error",
					zap.Int64("retryCount", retryCount),
					zap.String("url", c.url))
			}

			c.zapLog.Info(reconnectTip,
				zap.Int64("retryCount", retryCount),
				zap.String("url", c.url))
			time.Sleep(c.reconnectTime)

			amqpConn, amqpErr := connect(c)
			if amqpErr != nil {
				c.zapLog.Error("[rabbitmq connection] reconnect error",
					zap.String("err", amqpErr.Error()),
					zap.Int64("retryCount", retryCount),
					zap.String("url", c.url))
				continue
			}

			c.zapLog.Info("[rabbitmq connection] reconnected successfully",
				zap.Int64("retryCount", retryCount),
				zap.String("url", c.url))

			// 设置新连接
			c.mutex.Lock()
			c.isConnected = true
			c.conn = amqpConn
			c.blockChan = c.conn.NotifyBlocked(make(chan amqp.Blocking, 1))
			c.closeChan = c.conn.NotifyClose(make(chan *amqp.Error, 1))
			c.mutex.Unlock()
		}
	}
}

// Close rabbitmq connection
func (c *Connection) Close() {
	c.mutex.Lock()
	if c.isConnected {
		c.isConnected = false
		close(c.exit)
	}
	c.mutex.Unlock()
}

func (c *Connection) closeConn() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn.Close()
	}

	return nil
}

// GetReconnectCount 获取重连次数
func (c *Connection) GetReconnectCount() int64 {
	return atomic.LoadInt64(&c.reconnectCount)
}

// GetLastError 获取最后的错误信息
func (c *Connection) GetLastError() (error, time.Time) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.lastError, c.lastErrorTime
}

// GetConnectionStatus 获取连接状态信息
func (c *Connection) GetConnectionStatus() map[string]interface{} {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	status := map[string]interface{}{
		"connected":      c.isConnected && c.conn != nil && !c.conn.IsClosed(),
		"reconnectCount": atomic.LoadInt64(&c.reconnectCount),
		"url":            c.url,
		"maxRetries":     c.maxRetries,
	}

	if c.lastError != nil {
		status["lastError"] = c.lastError.Error()
		status["lastErrorTime"] = c.lastErrorTime
	}

	return status
}

// GetConn 获取AMQP连接
func (c *Connection) GetConn() *amqp.Connection {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn
	}
	return nil
}
