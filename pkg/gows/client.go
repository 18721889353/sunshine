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

// Message 定义在 message.go
// ErrWriteQueueFull / ErrWriteLimitExceeded 定义在 readwrite.go
// ClientOption / clientOptions / With* / withClient* 定义在 client_options.go
// healthState / IsAlive / ClientStats / Stats 定义在 health.go
// readLoop / writeLoop / writeWithRetry / WriteJSON* / WriteRaw* / ReadMessage* 定义在 readwrite.go

// Client 代表一个 WebSocket 客户端连接。
// 采用 Channel 驱动读写模型替代传统 Mutex 锁，核心设计原则:
//   - writeCh 缓冲通道解耦业务协程和网络写入协程，发送方永不阻塞
//   - writeLoop 后台 goroutine 串行化消费通道数据，规避锁竞争
//   - readCh 缓冲通道解耦底层连接和业务读取协程
//   - readLoop 后台 goroutine 持续从底层连接读取消息并推入 readCh
//   - 队列满载时自动丢弃消息，防止慢客户端拖慢整体吞吐
//   - 原子 CAS 保证关闭幂等，关闭信号通知所有监听协程优雅退出
//   - 网络波动保护：写入失败时指数退避重试（含 jitter），Ping/Pong 协议级保活
//   - 健康指标：记录读写时间与错误计数，支持 IsAlive 探测（定义在 health.go）
//   - 读写超时：readLoop 和 writeWithRetry 均支持独立超时，不依赖心跳机制
//   - dispatcher 字段用于代理 SendToUIDCtx / BroadcastCtx 等分发方法
type Client struct {
	wsConn         *websocket.Conn        // 底层 WebSocket 连接
	uid            string                 // 用户唯一标识（从 JWT 中提取）
	dispatcher     *DistributedDispatcher // 关联的分发中心，nil=未注册
	writeCh        chan []byte            // 写入队列通道，WriteJSON 向其非阻塞发送序列化数据
	readCh         chan []byte            // 读取队列通道，readLoop 向其推送接收到的消息数据
	closeSignalCh  chan struct{}          // 关闭信号通道，close 时触发所有监听协程退出
	closed         atomic.Bool            // 原子关闭标记，true=已关闭
	loopWg         sync.WaitGroup         // 等待 writeLoop 和 readLoop 协程退出
	onClose        func()                 // 可选关闭回调钩子，Close 时在清理前调用
	clientCtx      context.Context        // 连接上下文，Close 时自动取消，用于传递超时和链路追踪
	ctxCancel      context.CancelFunc     // 上下文取消函数，Close 时调用
	remoteAddr     string                 // 客户端远程地址（IP:Port），创建时从连接中提取
	numSent        atomic.Int64           // 原子计数: 已成功发送消息数
	numReceived    atomic.Int64           // 原子计数: 已接收消息数
	health         healthState            // 健康监控（读写时间、错误计数）
	readTimeout    time.Duration          // readLoop 读取超时时间（0=不限制）
	writeTimeout   time.Duration          // writeWithRetry 写入超时时间（0=默认 10s）
	readLimit      int64                  // 单条消息读取大小限制（0=不限制）
	writeLimit     int64                  // 单条消息写入大小限制（0=不限制）
	writeQueueSize int                    // 写入队列缓冲区容量（默认 64）
	readQueueSize  int                    // 读取队列缓冲区容量（默认 64）
}

// NewClient 创建并初始化一个新的 WebSocket 客户端连接。
// 自动启动 writeLoop 和 readLoop 后台 goroutine:
//   - writeLoop 负责串行化写入底层连接
//   - readLoop 负责持续从底层连接读取消息并推入 readCh
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

	// 从传入 ctx 派生可取消子 context，Close 时取消此子 context 不影响调用方
	clientCtx, clientCancel := context.WithCancel(ctx)
	c := &Client{
		wsConn:         conn,
		uid:            uid,
		dispatcher:     o.dispatcher,
		writeCh:        make(chan []byte, o.writeQueueSize),
		readCh:         make(chan []byte, o.readQueueSize),
		closeSignalCh:  make(chan struct{}),
		clientCtx:      clientCtx,
		ctxCancel:      clientCancel,
		remoteAddr:     conn.RemoteAddr().String(),
		readTimeout:    o.readTimeout,
		writeTimeout:   o.writeTimeout,
		readLimit:      o.readLimit,
		writeLimit:     o.writeLimit,
		writeQueueSize: o.writeQueueSize,
		readQueueSize:  o.readQueueSize,
	}

	if o.readLimit > 0 {
		c.wsConn.SetReadLimit(o.readLimit)
	}

	c.loopWg.Add(2)
	go c.writeLoop()
	go c.readLoop()
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
// 关闭顺序: 执行关闭钩子 → 取消上下文 → 关闭关闭信号通道 → 关闭写入队列通道
// → 关闭底层连接（触发 readLoop 退出）→ 等待 writeLoop 和 readLoop 完全退出。
// 通过 atomic.CompareAndSwap 保证幂等性，首次调用执行完整关闭流程，
// 后续调用直接返回 nil。
// 返回:
//   - error: 首次关闭底层连接失败时返回 error，重复关闭返回 nil
func (c *Client) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		if c.onClose != nil {
			c.onClose()
		}
		c.ctxCancel()              // 取消连接上下文，通知依赖 ctx 的协程
		close(c.closeSignalCh)           // 通知监听 closeSignalCh 的所有协程退出
		close(c.writeCh)                 // 触发 writeLoop 的 ok=false 分支退出
		closeErr := c.wsConn.Close()     // 关闭底层连接，触发 readLoop 的 ReadMessage 返回错误
		c.loopWg.Wait()                  // 等待 writeLoop 和 readLoop 完全退出（readLoop 的 defer 会关闭 readCh）
		return closeErr
	}
	return nil
}

// forceCloseConn 强制关闭底层 TCP 连接，不等待 writeLoop 退出。
// 由 writeLoop（写入重试全部失败后）或 StartHeartbeat（Ping 发送失败后）调用。
// 关闭 TCP 连接后，消息读取方的 ReadMessage 会立即返回"use of closed network connection"错误，
// 调用方感知错误后执行 Close() 完成完整清理流程（幂等安全）。
// 此方法与 Close 分离设计，避免 writeLoop 自锁（Close 中 wg.Wait 等待 writeLoop 退出）。
func (c *Client) forceCloseConn() {
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
	return c.closeSignalCh
}

// ---------------------------------------------------------------------------
// Dispatcher 代理方法
// ---------------------------------------------------------------------------

// SendToUIDCtx 通过关联的 Dispatcher 向指定 UID 的用户发送消息。
// 发送者自身不会收到消息（自动过滤）。
// 仅在 Client 已注册到 Dispatcher 时有效（通过 Upgrade 传入 WithDispatcher）。
func (c *Client) SendToUIDCtx(ctx context.Context, uid string, v any) {
	if c.dispatcher == nil {
		return
	}
	if uid == c.uid {
		return // 不发送给自己
	}
	c.dispatcher.SendToUIDCtx(ctx, uid, v)
}

// SendToMultiUIDCtx 通过关联的 Dispatcher 向多个 UID 的用户发送消息。
// 发送者自身不会收到消息（自动过滤）。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) SendToMultiUIDCtx(ctx context.Context, uids []string, v any) {
	if c.dispatcher == nil {
		return
	}
	// 过滤掉发送者自己
	filtered := make([]string, 0, len(uids))
	for _, uid := range uids {
		if uid != c.uid {
			filtered = append(filtered, uid)
		}
	}
	if len(filtered) == 0 {
		return
	}
	c.dispatcher.SendToMultiUIDCtx(ctx, filtered, v)
}

// BroadcastCtx 通过关联的 Dispatcher 向所有在线客户端广播消息。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) BroadcastCtx(ctx context.Context, v any) {
	if c.dispatcher == nil {
		return
	}
	c.dispatcher.BroadcastCtx(ctx, v)
}

// BroadcastReliableCtx 通过关联的 Dispatcher 进行可靠广播（按 UID 下发）。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) BroadcastReliableCtx(ctx context.Context, v any) {
	if c.dispatcher == nil {
		return
	}
	c.dispatcher.BroadcastReliableCtx(ctx, v)
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
