package gorabbitmq

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// connError 连接错误快照，用于原子读写
type connError struct {
	err  error
	time time.Time
}

// Connection RabbitMQ 连接结构体
type Connection struct {
	// 连接配置（嵌入 connectionOptions，消除字段重复）
	*connectionOptions

	url         string        // 连接 URL
	connCloseCh chan struct{} // 连接关闭信号通道

	mqConn      atomic.Pointer[amqp.Connection] // AMQP 连接对象（原子指针，无锁读）
	mqBlockChan chan amqp.Blocking              // 阻塞通知通道
	mqCloseChan chan *amqp.Error                // 关闭通知通道
	isConnected atomic.Bool                     // 是否已连接（原子布尔，无锁读）

	// 连接状态统计
	reconnectCount atomic.Int64              // 重连次数
	lastConnErr    atomic.Pointer[connError] // 最近一次连接错误（含时间戳），nil 表示无错误
}

// NewConnection 创建新的 RabbitMQ 连接
func NewConnection(ctx context.Context, url string, opts ...ConnectionOption) (*Connection, error) {
	if url == "" {
		return nil, errors.New("url is empty")
	}

	o := defaultConnectionOptions()
	o.apply(opts...)
	c := &Connection{
		url:               url,
		connectionOptions: o,
		connCloseCh:       make(chan struct{}),
	}

	mqConn, err := getMqConnect(ctx, c)
	if err != nil {
		return nil, err
	}

	c.mqConn.Store(mqConn)
	c.mqBlockChan = c.mqConn.Load().NotifyBlocked(make(chan amqp.Blocking, 1))
	c.mqCloseChan = c.mqConn.Load().NotifyClose(make(chan *amqp.Error, 1))
	c.isConnected.Store(true)

	go c.monitor(context.WithoutCancel(ctx))

	return c, nil
}

// getMqConnect 建立 AMQP 连接
func getMqConnect(ctx context.Context, c *Connection) (*amqp.Connection, error) {
	url := c.url
	tlsConfig := c.tlsConfig
	dialTimeout := c.dialTimeout
	heartbeat := c.heartbeat
	deadlineTimeout := c.deadlineTimeout

	// amqps 必须配置 TLS
	if strings.HasPrefix(url, "amqps://") && tlsConfig == nil {
		return nil, errors.New("tls not set, e.g. NewConnection(url, WithTLSConfig(tlsConfig))")
	}

	amqpCfg := amqp.Config{
		Dial: func(network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: 30 * time.Second,
			}
			tcpConn, dialErr := dialer.DialContext(ctx, network, addr)
			if dialErr != nil {
				return nil, dialErr
			}
			// 设置建联阶段的绝对截止时间（保护后续 TLS/AMQP 握手）
			// 注意：amqp091-go 内部会在握手完成后，基于 Heartbeat 重新设置读写截止时间，
			// 因此此截止时间仅影响握手阶段，不会影响正常业务收发。
			if err := tcpConn.SetDeadline(time.Now().Add(deadlineTimeout)); err != nil {
				return nil, err
			}
			return tcpConn, nil
		},
		Heartbeat: heartbeat,
	}
	if tlsConfig != nil {
		amqpCfg.TLSClientConfig = tlsConfig
	}
	mqConn, err := amqp.DialConfig(url, amqpCfg)
	if err != nil {
		return nil, err
	}
	return mqConn, nil
}

// CheckConnected 检查连接是否可用。
// 通过原子操作读取连接状态、AMQP 连接对象是否存在且未关闭，无需加锁。
//
// 参数:
//   - _: 上下文（保留参数，用于接口一致性，当前未使用）
//
// 返回值:
//   - bool: true 表示连接可用，false 表示已断开或未就绪
func (c *Connection) CheckConnected(_ context.Context) bool {
	return c.isConnected.Load() && c.mqConn.Load() != nil && !c.mqConn.Load().IsClosed()
}

// handleExitSignal 处理退出信号
func (c *Connection) handleExitSignal(ctx context.Context) {
	if err := c.closeConn(); err != nil {
		logger.WarnWithCtx(ctx, "[rabbitmq connection] 关闭连接失败", logger.Err(err))
	}
	logger.WarnWithCtx(ctx, "[rabbitmq connection] closed")
}

// handleBlockNotification 处理阻塞通知
func (c *Connection) handleBlockNotification(ctx context.Context, b amqp.Blocking) {
	if b.Active {
		logger.WarnWithCtx(ctx, "[rabbitmq connection] TCP blocked",
			logger.String("reason", b.Reason),
			logger.String("url", maskURL(c.url)))
	}
}

// handleCloseAndReconnect 处理连接关闭错误并执行重连
// 返回值：true 表示继续监控（重连成功或需要继续重试），false 表示终止监控（超过最大重试或服务已退出）
func (c *Connection) handleCloseAndReconnect(ctx context.Context, mqCloseChanErr *amqp.Error) bool {
	// 1. 标记断开，记录错误
	c.isConnected.Store(false)
	c.lastConnErr.Store(&connError{err: mqCloseChanErr, time: time.Now()})

	retryCount := c.reconnectCount.Add(1)

	// 2. 检查是否超过最大重试次数
	if c.maxRetries > 0 && int(retryCount) > c.maxRetries {
		logger.WarnWithCtx(ctx, "[rabbitmq] max retries exceeded, stop reconnecting",
			logger.Int64("retryCount", retryCount),
			logger.Int("maxRetries", c.maxRetries),
			logger.String("url", maskURL(c.url)))
		return false
	}

	// 3. 计算具备“指数退避 + 随机抖动”的等待时间
	backoffFactor := float64(int(retryCount-1)/5 + 1) // 每失败5次，系数+1
	if backoffFactor > 6 {
		backoffFactor = 6 // 最大退避到 6 倍的基准时间（如 5s -> 30s）
	}
	baseWait := float64(c.reconnectTime) * backoffFactor
	jitterRange := baseWait * 0.15 // ±15% 随机抖动
	actualWaitDuration := time.Duration(baseWait + (rand.Float64()*2-1)*jitterRange)

	// 4. 节流日志：每 10 次重试输出一次
	if retryCount%10 == 1 {
		fields := []logger.Field{
			logger.Int64("retryCount", retryCount),
			logger.Duration("reconnectIn", actualWaitDuration),
			logger.String("url", maskURL(c.url)),
		}
		if mqCloseChanErr != nil {
			fields = append(fields, logger.String("err", mqCloseChanErr.Error()))
		}
		logger.WarnWithCtx(ctx, "[rabbitmq] connection lost, reconnecting...", fields...)
	}

	// 5. 可中断的等待：在等待期间如果外部调用了 Close()，立刻退出
	select {
	case <-c.connCloseCh:
		logger.InfoWithCtx(ctx, "[rabbitmq] connection closing detected during retry wait, abort reconnect")
		return false
	case <-time.After(actualWaitDuration):
	}

	// 6. 带超时上下文的重连（支持被 Close 中断）
	reconnectCtx, reconnectCancel := context.WithCancel(context.Background())
	defer reconnectCancel()

	// 监听关闭信号，一旦触发则取消重连
	go func() {
		select {
		case <-c.connCloseCh:
			reconnectCancel()
		case <-reconnectCtx.Done():
		}
	}()

	reconnectStart := time.Now()
	amqpConn, err := getMqConnect(reconnectCtx, c)
	if err != nil {
		// 如果是因为外部 Close 导致的 context canceled，直接终止
		if errors.Is(err, context.Canceled) {
			return false
		}
		// 其他错误：记录日志，返回 true 让 monitor 继续下一次重试
		if retryCount%10 == 1 {
			logger.WarnWithCtx(ctx, "[rabbitmq] reconnect failed",
				logger.Err(err),
				logger.Int64("retryCount", retryCount),
				logger.String("url", maskURL(c.url)))
		}
		return true
	}

	// 7. 重连成功：替换连接，清空错误快照
	logger.InfoWithCtx(ctx, "[rabbitmq] reconnected",
		logger.Int64("retryCount", retryCount),
		logger.String("url", maskURL(c.url)),
		logger.Duration("cost", time.Since(reconnectStart)))

	c.isConnected.Store(true)
	c.mqConn.Store(amqpConn)
	c.mqBlockChan = c.mqConn.Load().NotifyBlocked(make(chan amqp.Blocking, 1))
	c.mqCloseChan = c.mqConn.Load().NotifyClose(make(chan *amqp.Error, 1))

	// 【改进】重连成功后清除错误快照，避免 GetLastError() 返回过期错误
	c.lastConnErr.Store(nil)

	return true
}

// monitor 监控连接状态，在后台 goroutine 中运行
func (c *Connection) monitor(ctx context.Context) {
	for {
		// 1. 每次循环开始，先进行一次非阻塞的退出检查（确保能最快速度退出）
		select {
		case <-c.connCloseCh:
			c.handleExitSignal(ctx)
			return
		default:
		}

		// 2. 核心事件监听与 Panic 保护
		shouldExit := func() bool {
			defer func() {
				if r := recover(); r != nil {
					logger.WarnWithCtx(ctx, "[rabbitmq] monitor recovered from panic",
						logger.Any("panic", r),
						logger.String("url", maskURL(c.url)))
				}
			}()

			select {
			case <-c.connCloseCh:
				return true // ① 外部主动关闭
			case b := <-c.mqBlockChan:
				c.handleBlockNotification(ctx, b)
				return false // ② 阻塞通知，仅日志，继续监控
			case mqCloseChanErr := <-c.mqCloseChan:
				if !c.handleCloseAndReconnect(ctx, mqCloseChanErr) {
					return true // ③ 重连失败/终止，退出
				}
				return false // ④ 重连成功/继续重试，继续监控
			}
		}()

		// 3. 检查内部匿名函数的退出指示
		if shouldExit {
			c.handleExitSignal(ctx)
			return
		}

		// 4. 安全防抖：既能防止过快循环，又能【瞬间响应】退出信号！
		select {
		case <-c.connCloseCh:
			c.handleExitSignal(ctx)
			return
		case <-time.After(time.Millisecond * 100):
			// 正常无事发生，等待 100ms 后进入下一次循环
		}
	}
}

// Close 关闭 RabbitMQ 连接
func (c *Connection) Close() {
	// 原子 CAS 确保只关闭一次，无需外部锁
	if c.isConnected.CompareAndSwap(true, false) {
		close(c.connCloseCh)
	}
}

// closeConn 关闭 AMQP 连接
func (c *Connection) closeConn() error {
	if conn := c.mqConn.Load(); conn != nil && !conn.IsClosed() {
		return conn.Close()
	}
	return nil
}

// GetReconnectCount 获取重连次数
func (c *Connection) GetReconnectCount(_ context.Context) int64 {
	return c.reconnectCount.Load()
}

// maskURL 脱敏 URL，隐藏用户名密码
func maskURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	prefix := ""
	rest := rawURL
	if idx := strings.Index(rawURL, "://"); idx != -1 {
		prefix = rawURL[:idx+3]
		rest = rawURL[idx+3:]
	}
	if idx := strings.Index(rest, "@"); idx != -1 {
		return prefix + "***:***@" + rest[idx+1:]
	}
	return rawURL
}

// GetLastError 获取最后的错误信息
func (c *Connection) GetLastError(_ context.Context) (time.Time, error) {
	if v := c.lastConnErr.Load(); v != nil {
		return v.time, v.err
	}
	return time.Time{}, nil
}

// GetConnectionStatus 获取连接状态信息
func (c *Connection) GetConnectionStatus(_ context.Context) map[string]interface{} {
	status := map[string]interface{}{
		"connected":      c.isConnected.Load() && c.mqConn.Load() != nil && !c.mqConn.Load().IsClosed(),
		"reconnectCount": c.reconnectCount.Load(),
		"url":            maskURL(c.url),
		"maxRetries":     c.maxRetries,
		"reconnectTime":  c.reconnectTime.String(),
	}

	if v := c.lastConnErr.Load(); v != nil {
		status["lastError"] = v.err.Error()
		status["lastErrorTime"] = v.time
	}

	return status
}

// Done 返回一个只读 channel，当连接被关闭时会收到信号
// 用于外部监听连接关闭事件，替代直接访问未导出字段 connCloseCh
func (c *Connection) Done() <-chan struct{} {
	return c.connCloseCh
}

// GetConn 获取 AMQP 连接
func (c *Connection) GetConn(_ context.Context) *amqp.Connection {
	if conn := c.mqConn.Load(); conn != nil && !conn.IsClosed() {
		return conn
	}
	return nil
}
