package gows

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"go.opentelemetry.io/otel/attribute"

	"github.com/18721889353/sunshine/pkg/logger"
)

// msgFromWsToCh / msgFromChToWs / writeWithRetry / checkWriteLimit / ErrWriteQueueFull / ErrWriteLimitExceeded 定义在 client_readwrite.go
// WriteJSONCtx / WriteRawCtx / ReadMessageCtx 定义在 client_local.go
// ClientOption / clientOptions / With* / withClient* 定义在 client_options.go
// healthState / IsAlive / ClientStats / Stats 定义在 health.go

// Client 代表一个 WebSocket 客户端连接。
// 采用 Channel 驱动读写模型替代传统 Mutex 锁，核心设计原则:
//   - writeCh 缓冲通道解耦业务协程和网络写入协程，发送方永不阻塞
//   - msgFromChToWs 后台 goroutine 串行化消费通道数据，规避锁竞争
//   - readCh 缓冲通道解耦底层连接和业务读取协程
//   - msgFromWsToCh 后台 goroutine 持续从底层连接读取消息并推入 readCh
//   - 队列满载时自动丢弃消息，防止慢客户端拖慢整体吞吐
//   - 原子 CAS 保证关闭幂等，关闭信号通知所有监听协程优雅退出
//   - 网络波动保护：写入失败时指数退避重试（含 jitter），Ping/Pong 协议级保活
//   - 健康指标：记录读写时间与错误计数，支持 IsAlive 探测（定义在 health.go）
//   - 读写超时：msgFromWsToCh 和 msgFromChToWs 均支持独立超时，不依赖心跳机制
//   - dispatcher 字段用于代理 SendToUIDCtx / BroadcastCtx 等分发方法
type Client struct {
	wsConn      *websocket.Conn        // 底层 WebSocket 连接
	uid         string                 // 用户唯一标识（从 JWT 中提取）
	dispatcher  *DistributedDispatcher // 关联的分发中心，nil=未注册
	writeCh     chan []byte            // 写入队列通道，WriteJSON 向其非阻塞发送序列化数据
	readCh      chan []byte            // 读取队列通道，msgFromWsToCh 向其推送接收到的消息数据
	writeChSize int                    // 写入通道缓冲区容量（默认 1024）
	readChSize  int                    // 读取通道缓冲区容量（默认 1024）

	closed             atomic.Bool        // 原子关闭标记，true=已关闭
	readWg             sync.WaitGroup     // 等待 msgFromWsToCh 协程退出
	writeWg            sync.WaitGroup     // 等待 msgFromChToWs 协程退出
	onClose            func()             // 可选关闭回调钩子，Close 时在清理前调用
	clientCtx          context.Context    // 连接上下文，Close 时自动取消，用于传递超时和链路追踪
	clientCtxCancel    context.CancelFunc // 取消 clientCtx，Close 时调用
	remoteAddr         string             // 客户端远程地址（IP:Port），创建时从连接中提取
	numSent            atomic.Int64       // 原子计数: 已成功发送消息数
	numReceived        atomic.Int64       // 原子计数: 已接收消息数
	health             healthState        // 健康监控（读写时间、错误计数）
	wsConnReadTimeout  time.Duration      // msgFromWsToCh 读取超时时间（0=不限制）
	wsConnWriteTimeout time.Duration      // msgFromChToWs 写入超时时间（0=默认 10s）
	readLimit          int64              // 单条消息读取大小限制（0=不限制）
	writeLimit         int64              // 单条消息写入大小限制（0=不限制）

}

// NewClient 创建并初始化一个新的 WebSocket 客户端连接。
// 自动启动 msgFromChToWs 和 msgFromWsToCh 后台 goroutine:
//   - msgFromChToWs 负责从 writeCh 获取消息并写入底层连接
//   - msgFromWsToCh 负责从底层连接读取消息并推入 readCh
//
// 参数:
//   - ctx:   上下文，用于链路追踪和生命周期管理。Close 时会取消其派生 context。
//   - conn:  已建立的 WebSocket 底层连接
//   - uid:   用户唯一标识（从 JWT 解析获取）
//   - opts:  可选配置参数（内部选项，通过 Upgrade 传播）
//
// 返回:
//   - *Client: 具备非阻塞读写能力和自动关闭清理的客户端实例
func NewClient(ctx context.Context, conn *websocket.Conn, uid string, opts ...ClientOption) *Client {
	o := defaultClientOptions()
	o.apply(opts...)

	// 写入超时默认值
	if o.writeTimeout <= 0 {
		o.writeTimeout = writeDeadline
	}

	// 使用 WithoutCancel 阻断上游 Gin 超时传递，Close 时手动取消
	clientCtx, clientCancel := context.WithCancel(context.WithoutCancel(ctx))
	c := &Client{
		wsConn:     conn,
		uid:        uid,
		dispatcher: o.dispatcher,
		writeCh:    make(chan []byte, o.writeChSize),
		readCh:     make(chan []byte, o.readChSize),

		clientCtx:          clientCtx,
		clientCtxCancel:    clientCancel,
		remoteAddr:         conn.RemoteAddr().String(),
		wsConnReadTimeout:  o.readTimeout,
		wsConnWriteTimeout: o.writeTimeout,
		readLimit:          o.readLimit,
		writeLimit:         o.writeLimit,
		writeChSize:        o.writeChSize,
		readChSize:         o.readChSize,
	}

	// 设置读取大小限制（0=不限制，覆盖 gorilla/websocket 默认 4096 字节）
	c.wsConn.SetReadLimit(o.readLimit)

	c.readWg.Add(1)
	go c.msgFromWsToCh()
	c.writeWg.Add(1)
	go c.msgFromChToWs()
	return c
}

// SetCloseHook 设置关闭回调钩子，在 Close 时自动调用。
// 可用于执行 Dispatcher 注销等清理操作。
// 参数:
//   - fn: 回调函数，在底层连接关闭前执行
func (c *Client) SetCloseHook(fn func()) {
	c.onClose = fn
}

// Close 优雅关闭 WebSocket 连接，触发关闭信号并释放所有资源。
// 关闭顺序: 执行关闭钩子 → 取消上下文 → 等待 msgFromChToWs 完全退出
// (drain writeCh 剩余消息后退出，确保所有排队消息已写入)
// → 关闭底层连接(触发 msgFromWsToCh 的 ReadMessage 返回错误)
// → 等待 msgFromWsToCh 完全退出。
// 通过 atomic.CompareAndSwap 保证幂等性，首次调用执行完整关闭流程，
// 后续调用直接返回 nil。
// 返回:
//   - error: 首次关闭底层连接失败时返回 error，重复关闭返回 nil
func (c *Client) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		if c.onClose != nil {
			c.onClose()
		}
		c.clientCtxCancel()          // 取消 clientCtx，通知所有协程退出（msgFromChToWs drain 后退出）
		c.writeWg.Wait()             // 等待所有排队消息写入完毕
		closeErr := c.wsConn.Close() // 关闭底层连接，触发 msgFromWsToCh 的 ReadMessage 返回错误
		c.readWg.Wait()              // 等待 msgFromWsToCh 完全退出（defer 会关闭 readCh）
		return closeErr
	}
	return nil
}

// closeWsConn 直接关闭底层 TCP 连接，不等待 msgFromChToWs 退出。
// 由 msgFromChToWs（写入重试全部失败后）或 StartHeartbeat（Ping 发送失败后）调用。
// 关闭 TCP 连接后，消息读取方的 ReadMessage 会立即返回"use of closed network connection"错误，
// 调用方感知错误后执行 Close() 完成完整清理流程（幂等安全）。
// 此方法与 Close 分离设计，避免 msgFromChToWs 自锁（Close 中 wg.Wait 等待 msgFromChToWs 退出）。
func (c *Client) closeWsConn() {
	if err := c.wsConn.Close(); err != nil {
		logger.WarnWithCtx(c.clientCtx, "ws force close conn failed",
			logger.String("uid", c.uid),
			logger.String("remote_addr", c.remoteAddr),
			logger.Err(err),
		)
	}
}

// UID 返回当前连接关联的用户标识。
func (c *Client) UID() string {
	return c.uid
}

// RemoteAddr 返回客户端的远程网络地址（IP:Port）。
func (c *Client) RemoteAddr() string {
	return c.remoteAddr
}

// Context 返回客户端的关联上下文。
// Close 后该上下文被取消，可通过 ctx.Done() 感知连接关闭事件。
func (c *Client) Context() context.Context {
	return c.clientCtx
}

// Done 返回一个只读 channel，当连接关闭时该 channel 被 close。
// 调用方可通过 select 或 <-client.Done() 感知连接断开事件，
// 常用于心跳协程和消息读取协程的退出通知。
// 返回:
//   - <-chan struct{}: 关闭信号接收通道，关闭时立即返回零值
func (c *Client) Done() <-chan struct{} {
	return c.clientCtx.Done()
}

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
// 若上下文中不存在 request_id，返回空字符串值。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("ws.request_id", reqID)
		}
	}
	return attribute.String("ws.request_id", "")
}
