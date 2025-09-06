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

// DefaultURL 默认的 RabbitMQ 连接 URL
const DefaultURL = "amqp://guest:guest@localhost:5672/"

var defaultLogger, _ = zap.NewProduction()

// ConnectionOption 连接配置选项函数类型
type ConnectionOption func(*connectionOptions)

// connectionOptions 连接配置选项
type connectionOptions struct {
	tlsConfig       *tls.Config   // TLS 配置，如果使用 amqps 协议则必须设置
	reconnectTime   time.Duration // 重连时间间隔，默认为 3 秒
	dialTimeout     time.Duration // 连接超时时间，默认为 5 秒
	heartbeat       time.Duration // 心跳间隔
	deadlineTimeout time.Duration // 截止时间超时
	zapLog          *zap.Logger   // 日志记录器
	maxRetries      int           // 最大重连次数，0 表示无限重试
}

// apply 应用连接配置选项
func (o *connectionOptions) apply(opts ...ConnectionOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultConnectionOptions 默认连接配置选项
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

// WithTLSConfig 设置 TLS 配置选项
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

// WithReconnectTime 设置重连时间间隔选项
func WithReconnectTime(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 3
		}
		o.reconnectTime = d
	}
}

// WithDialTimeout 设置连接超时时间选项
func WithDialTimeout(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 5
		}
		o.dialTimeout = d
	}
}

// WithHeartbeat 设置心跳间隔选项
func WithHeartbeat(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 5
		}
		o.heartbeat = d
	}
}

// WithDeadlineTimeout 设置截止时间超时选项
func WithDeadlineTimeout(d time.Duration) ConnectionOption {
	return func(o *connectionOptions) {
		if d == 0 {
			d = time.Second * 30
		}
		o.deadlineTimeout = d
	}
}

// WithLogger 设置日志记录器选项
func WithLogger(zapLog *zap.Logger) ConnectionOption {
	return func(o *connectionOptions) {
		if zapLog == nil {
			return
		}
		o.zapLog = zapLog
	}
}

// WithMaxRetries 设置最大重连次数，0表示无限重试，默认为0
func WithMaxRetries(maxRetries int) ConnectionOption {
	return func(o *connectionOptions) {
		o.maxRetries = maxRetries
	}
}

// -------------------------------------------------------------------------------------------

// Connection RabbitMQ 连接结构体
type Connection struct {
	mutex sync.Mutex

	url             string        // 连接 URL
	tlsConfig       *tls.Config   // TLS 配置
	reconnectTime   time.Duration // 重连时间间隔
	dialTimeout     time.Duration // 连接超时时间
	heartbeat       time.Duration // 心跳间隔
	deadlineTimeout time.Duration // 截止时间超时
	maxRetries      int           // 最大重连次数
	exit            chan struct{} // 退出信号通道
	zapLog          *zap.Logger   // 日志记录器

	conn        *amqp.Connection // AMQP 连接对象
	blockChan   chan amqp.Blocking // 阻塞通知通道
	closeChan   chan *amqp.Error   // 关闭通知通道
	isConnected bool             // 是否已连接

	// 连接状态统计
	reconnectCount int64     // 重连次数
	lastError      error     // 最后一次错误
	lastErrorTime  time.Time // 最后一次错误时间
}

// NewConnection 创建新的 RabbitMQ 连接
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

// connect 建立 AMQP 连接
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

// monitor 监控连接状态
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
			fmt.Println(c.maxRetries, int(retryCount))
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

// Close 关闭 RabbitMQ 连接
func (c *Connection) Close() {
	c.mutex.Lock()
	if c.isConnected {
		c.isConnected = false
		close(c.exit)
	}
	c.mutex.Unlock()
}

// closeConn 关闭 AMQP 连接
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

// GetConn 获取 AMQP 连接
func (c *Connection) GetConn() *amqp.Connection {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn
	}
	return nil
}