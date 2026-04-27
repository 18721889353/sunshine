package gorabbitmq

import (
	"context"
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
)

// DefaultURL 默认的 RabbitMQ 连接 URL
const DefaultURL = "amqp://guest:guest@localhost:5672/"

// ConnectionOption 连接配置选项函数类型
type ConnectionOption func(*connectionOptions)

// connectionOptions 连接配置选项
type connectionOptions struct {
	tlsConfig       *tls.Config   // TLS 配置，如果使用 amqps 协议则必须设置
	reconnectTime   time.Duration // 重连时间间隔，默认为 3 秒
	dialTimeout     time.Duration // 连接超时时间，默认为 5 秒
	heartbeat       time.Duration // 心跳间隔
	deadlineTimeout time.Duration // 截止时间超时
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

	conn        *amqp.Connection   // AMQP 连接对象
	blockChan   chan amqp.Blocking // 阻塞通知通道
	closeChan   chan *amqp.Error   // 关闭通知通道
	isConnected bool               // 是否已连接

	// 连接状态统计
	reconnectCount int64     // 重连次数
	lastError      error     // 最后一次错误
	lastErrorTime  time.Time // 最后一次错误时间
}

// NewConnection 创建新的 RabbitMQ 连接
func NewConnection(ctx context.Context, url string, opts ...ConnectionOption) (*Connection, error) {
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
	}

	conn, err := connect(ctx, connection)
	if err != nil {
		return nil, err
	}

	connection.conn = conn
	connection.blockChan = connection.conn.NotifyBlocked(make(chan amqp.Blocking, 1))
	connection.closeChan = connection.conn.NotifyClose(make(chan *amqp.Error, 1))
	connection.isConnected = true

	go connection.monitor(ctx)

	return connection, nil
}

// connect 建立 AMQP 连接
func connect(ctx context.Context, c *Connection) (*amqp.Connection, error) {
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
		dialer := &net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}

		conn, err = amqp.DialConfig(url, amqp.Config{
			Dial: func(network, addr string) (net.Conn, error) {
				c, dialErr := dialer.DialContext(ctx, network, addr)
				if dialErr != nil {
					return nil, dialErr
				}
				dialErr = c.SetDeadline(time.Now().Add(deadlineTimeout)) // 设置读写超时时间
				if dialErr != nil {
					return nil, dialErr
				}
				return c, nil
			},
			Heartbeat: heartbeat, // 增加心跳间隔
		})
		if err != nil {
			return nil, err
		}
	}

	return conn, nil
}

// maskURL 脱敏 URL，移除用户名和密码(暂未使用,保留供将来扩展)
// func maskURL(url string) string {
// 	if url == "" {
// 		return url
// 	}
// 	prefix := ""
// 	rest := url
// 	if idx := strings.Index(url, "://"); idx != -1 {
// 		prefix = url[:idx+3]
// 		rest = url[idx+3:]
// 	}
// 	if idx := strings.Index(rest, "@"); idx != -1 {
// 		return prefix + "***:***@" + rest[idx+1:]
// 	}
// 	return url
// }

// CheckConnected 检查连接是否正常
func (c *Connection) CheckConnected(_ context.Context) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.isConnected && c.conn != nil && !c.conn.IsClosed()
}

// monitor 监控连接状态
func (c *Connection) monitor(_ context.Context) {
	reconnectTip := fmt.Sprintf("[rabbitmq connection] lost connection, attempting reconnect in %s", c.reconnectTime)

	for {
		// 使用 defer/recover 防止 monitor goroutine 因异常而退出
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WarnWithCtx(context.Background(), "[rabbitmq connection] monitor recovered from panic",
						logger.Any("panic", r),
						logger.String("url", c.url))
				}
			}()

			select {
			case <-c.exit:
				if err := c.closeConn(); err != nil {
					logger.WarnWithCtx(context.Background(), "[rabbitmq connection] 关闭连接失败", logger.Err(err))
				}
				logger.WarnWithCtx(context.Background(), "[rabbitmq connection] closed")
				return
			case b := <-c.blockChan:
				if b.Active {
					logger.WarnWithCtx(context.Background(), "[rabbitmq connection] TCP blocked", logger.String("reason", b.Reason))
				}
			case closeChanErr := <-c.closeChan:
				c.mutex.Lock()
				c.isConnected = false
				c.lastError = closeChanErr
				c.lastErrorTime = time.Now()
				c.mutex.Unlock()

				atomic.AddInt64(&c.reconnectCount, 1)
				retryCount := c.GetReconnectCount(context.Background())

				// 检查是否超过最大重试次数
				if c.maxRetries > 0 && int(retryCount) > c.maxRetries {
					logger.WarnWithCtx(context.Background(), "[rabbitmq connection] max retries exceeded, stopping reconnection attempts",
						logger.Int64("retryCount", retryCount),
						logger.Int("maxRetries", c.maxRetries),
						logger.String("url", c.url))
					return
				}

				if closeChanErr != nil {
					if retryCount%10 == 1 {
						logger.WarnWithCtx(context.Background(), "[rabbitmq connection] lost connection error",
							logger.String("err", closeChanErr.Error()),
							logger.Int64("retryCount", retryCount),
							logger.String("url", c.url))
					}
				} else {
					if retryCount%10 == 1 {
						logger.WarnWithCtx(context.Background(), "[rabbitmq connection] lost connection error",
							logger.Int64("retryCount", retryCount),
							logger.String("url", c.url))
					}
				}

				if retryCount%10 == 1 {
					logger.InfoWithCtx(context.Background(), reconnectTip,
						logger.Int64("retryCount", retryCount),
						logger.String("url", c.url))
				}
				time.Sleep(c.reconnectTime)

				// 重连
				reconnectStart := time.Now()
				amqpConn, amqpErr := connect(context.Background(), c)
				reconnectDuration := time.Since(reconnectStart)

				if amqpErr != nil {
					if retryCount%10 == 1 {
						logger.WarnWithCtx(context.Background(), "[rabbitmq connection] reconnect error",
							logger.Err(amqpErr),
							logger.Int64("retryCount", retryCount),
							logger.String("url", c.url))
					}
					// 继续下一次循环尝试重连
					return
				}

				logger.InfoWithCtx(context.Background(), "[rabbitmq connection] reconnected successfully",
					logger.Int64("retryCount", retryCount),
					logger.String("url", c.url),
					logger.Duration("duration", reconnectDuration))

				// 设置新连接
				c.mutex.Lock()
				c.isConnected = true
				c.conn = amqpConn
				c.blockChan = c.conn.NotifyBlocked(make(chan amqp.Blocking, 1))
				c.closeChan = c.conn.NotifyClose(make(chan *amqp.Error, 1))
				c.mutex.Unlock()
			}
		}()

		// 防止过快重试
		select {
		case <-c.exit:
			if err := c.closeConn(); err != nil {
				logger.WarnWithCtx(context.Background(), "[rabbitmq connection] 关闭连接失败", logger.Err(err))
			}
			logger.WarnWithCtx(context.Background(), "[rabbitmq connection] closed")
			return
		case <-time.After(time.Millisecond * 100):
			// 继续下一次循环
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
func (c *Connection) GetReconnectCount(_ context.Context) int64 {
	return atomic.LoadInt64(&c.reconnectCount)
}

// GetLastError 获取最后的错误信息
func (c *Connection) GetLastError(_ context.Context) (time.Time, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.lastErrorTime, c.lastError
}

// GetConnectionStatus 获取连接状态信息
func (c *Connection) GetConnectionStatus(_ context.Context) map[string]interface{} {
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
func (c *Connection) GetConn(_ context.Context) *amqp.Connection {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn
	}
	return nil
}
