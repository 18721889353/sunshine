// Package rabbitmq is a go wrapper for github.com/rabbitmq/amqp091-go
//
// producer and consumer using the five types direct, topic, fanout, headers, x-delayed-message.
// publisher and subscriber using the fanout message type.
package rabbitmq

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

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
		dialTimeout:     time.Second * 5, // 设置默认超时时间为5秒
		heartbeat:       time.Second * 3,
		deadlineTimeout: time.Second * 30,
		zapLog:          defaultLogger,
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
	exit            chan struct{}
	zapLog          *zap.Logger

	conn        *amqp.Connection
	blockChan   chan amqp.Blocking
	closeChan   chan *amqp.Error
	isConnected bool
}

// NewConnection rabbitmq connection
func NewConnection(url string, opts ...ConnectionOption) (*Connection, error) {
	if url == "" {
		return nil, errors.New("url is empty")
	}

	o := defaultConnectionOptions()
	o.apply(opts...)

	connection := &Connection{
		url:           url,
		reconnectTime: o.reconnectTime,
		tlsConfig:     o.tlsConfig,
		dialTimeout:   o.dialTimeout,
		heartbeat:     o.heartbeat,
		exit:          make(chan struct{}),
		zapLog:        o.zapLog,
	}

	conn, err := connect(connection)
	if err != nil {
		return nil, err
	}
	//connection.zapLog.Info("[rabbitmq connection] connected successfully.")

	connection.conn = conn
	connection.blockChan = connection.conn.NotifyBlocked(make(chan amqp.Blocking, 1))
	connection.closeChan = connection.conn.NotifyClose(make(chan *amqp.Error, 1))
	connection.isConnected = true

	go connection.monitor()

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

// CheckConnected rabbitmq connection
func (c *Connection) CheckConnected() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.isConnected
}

func (c *Connection) monitor() {
	retryCount := 0
	reconnectTip := fmt.Sprintf("[rabbitmq connection] lost connection, attempting reconnect in %s", c.reconnectTime)

	for {
		select {
		case <-c.exit:
			_ = c.closeConn()
			//c.zapLog.Info("[rabbitmq connection] closed")
			return
		case b := <-c.blockChan:
			if b.Active {
				c.zapLog.Warn("[rabbitmq connection] TCP blocked: " + b.Reason)
			} else {
				c.zapLog.Warn("[rabbitmq connection] TCP unblocked")
			}
		case closeChanErr := <-c.closeChan:
			c.mutex.Lock()
			c.isConnected = false
			c.mutex.Unlock()

			retryCount++
			c.zapLog.Warn("[rabbitmq connection] lost connection error", zap.String("err", closeChanErr.Error()), zap.Int("retryCount", retryCount))
			c.zapLog.Warn(reconnectTip)
			time.Sleep(c.reconnectTime) // wait for reconnect

			amqpConn, amqpErr := connect(c)
			if amqpErr != nil {
				c.zapLog.Warn("[rabbitmq connection] reconnect error", zap.String("err", amqpErr.Error()), zap.Int("retryCount", retryCount))
				continue
			}
			//c.zapLog.Info("[rabbitmq connection] reconnected successfully.")
			// set new connection
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
	c.isConnected = false
	c.mutex.Unlock()

	close(c.exit)
}

func (c *Connection) closeConn() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

func (c *Connection) GetConn() *amqp.Connection {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.conn != nil {
		return c.conn
	}
	return nil
}
