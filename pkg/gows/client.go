package gows

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// Message WebSocket 服务端通用消息结构。
// 所有服务端推送消息统一使用此结构序列化，
// 客户端通过 Type 字段区分消息类型进行分发处理。
type Message struct {
	Type string `json:"type"`           // 消息类型标识（如 "ping", "notify", "greeting"）
	Msg  string `json:"msg,omitempty"`  // 消息内容文本（可选）
	Data any    `json:"data,omitempty"` // 附加业务数据（可选，任意类型）
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

// ErrWriteQueueFull 写入队列已满，消息被丢弃的错误标识。
// 当客户端写入缓冲区满载时 WriteJSON 返回此错误，
// 发送方可根据此错误判断是否为短暂拥塞，决定是否降级处理。
var ErrWriteQueueFull = &writeQueueFullError{}

// writeQueueFullError 写入队列满载的错误类型
type writeQueueFullError struct{}

// Error 实现 error 接口，返回写入队列已满的错误描述信息。
// 当 WriteJSON 因写入通道满载而丢弃消息时，调用方可使用 errors.Is 判断
// 此错误类型，以决定是否进行降级处理或重试。
// 返回:
//   - string: 错误描述文本 "write queue is full, message dropped"
func (e *writeQueueFullError) Error() string {
	return "write queue is full, message dropped"
}

// ClientOption 客户端配置选项函数类型。
// 采用 Functional Options 模式，支持可扩展的配置传递，
// 新增配置项无需修改 NewClient 的函数签名。
type ClientOption func(*clientOptions)

// clientOptions 客户端内部配置参数集合
type clientOptions struct {
	writeQueueSize int  // 写入队列缓冲区容量（默认 64）
	readLimit      int64 // 单条消息读取大小限制（默认 0=不限制）
}

// WithWriteQueueSize 设置客户端写入队列缓冲区大小。
// 参数:
//   - size: 缓冲区容量（默认 64）。当并发写入量超过连接消费能力时，
//     增大此值可减少丢包，但会占用更多内存。
//     建议根据业务峰值 QPS 和消息大小估算，通常在 64~256 之间。
//
// 返回:
//   - ClientOption: 配置选项，用于 NewClient 的变参传入
func WithWriteQueueSize(size int) ClientOption {
	return func(o *clientOptions) {
		if size > 0 {
			o.writeQueueSize = size
		}
	}
}

// WithReadLimit 设置单条消息的最大读取字节数。
// 参数:
//   - limit: 最大字节数。超过此大小的消息将被拒绝，连接关闭。
//     默认 0 表示不限制（采用 gorilla/websocket 默认值 32768）。
//     生产环境建议根据业务消息大小设置，防止内存耗尽。
func WithReadLimit(limit int64) ClientOption {
	return func(o *clientOptions) {
		if limit > 0 {
			o.readLimit = limit
		}
	}
}

// Client 代表一个 WebSocket 客户端连接。
// 采用 Channel 驱动写入模型替代传统 Mutex 锁，核心设计原则:
//   - writeCh 缓冲通道解耦业务协程和网络写入协程，发送方永不阻塞
//   - writeLoop 后台 goroutine 串行化消费通道数据，规避锁竞争
//   - 队列满载时自动丢弃消息，防止慢客户端拖慢整体吞吐
//   - 原子 CAS 保证关闭幂等，关闭信号通知所有监听方优雅退出
//   - 网络波动保护：写入失败时指数退避重试，Ping/Pong 协议级保活
//   - 健康指标：记录读写时间与错误计数，支持 IsAlive 探测
type Client struct {
	conn        *websocket.Conn // 底层 WebSocket 连接，仅 writeLoop 协程直接写入
	uid         string          // 用户唯一标识（从 JWT 中提取）
	writeCh     chan []byte     // 写入队列通道，WriteJSON 向其非阻塞发送序列化数据
	closeCh     chan struct{}   // 关闭信号通道，close 时触发所有监听协程退出
	closed      int32           // 原子关闭标记，0=未关闭，1=已关闭
	wg          sync.WaitGroup  // 等待 writeLoop 协程退出，保障 Close 语义确定性
	onClose     func()          // 可选关闭回调钩子，Close 时在清理前调用
	ctx         context.Context // 连接上下文，Close 时自动取消，用于传递超时和链路追踪
	ctxCancel   context.CancelFunc // 上下文取消函数，Close 时调用
	remoteAddr  string          // 客户端远程地址（IP:Port），创建时从连接中提取
	numSent     int64           // 原子计数: 已成功发送消息数
	numReceived int64           // 原子计数: 已接收消息数

	// 网络波动容错与健康监控字段
	lastWriteTime   time.Time     // 最后一次成功写入时间
	lastReadTime    time.Time     // 最后一次成功读取时间
	writeErrCount   int64         // 原子计数: 写入失败累计次数
	lastWriteErr    atomic.Value  // 最近一次写入错误信息（string）
	healthMu        sync.RWMutex  // 保护 lastWriteTime / lastReadTime 并发读写
}

// NewClient 创建并初始化一个新的 WebSocket 客户端连接。
// 自动启动 writeLoop 后台 goroutine 负责串行化写入底层连接。
// 参数:
//   - conn: 已建立的 WebSocket 底层连接
//   - uid: 用户唯一标识（从 JWT 解析获取）
//   - opts: 可选配置参数（如 WithWriteQueueSize）
//
// 返回:
//   - *Client: 具备非阻塞写入能力和自动关闭清理的客户端实例
func NewClient(conn *websocket.Conn, uid string, opts ...ClientOption) *Client {
	o := clientOptions{writeQueueSize: 64}
	for _, opt := range opts {
		opt(&o)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		conn:       conn,
		uid:        uid,
		writeCh:    make(chan []byte, o.writeQueueSize),
		closeCh:    make(chan struct{}),
		ctx:        ctx,
		ctxCancel:  cancel,
		remoteAddr: conn.RemoteAddr().String(),
	}

	if o.readLimit > 0 {
		c.conn.SetReadLimit(o.readLimit)
	}

	c.wg.Add(1)
	go c.writeLoop()
	return c
}

// writeLoopRetries 写入失败最大重试次数
const (
	writeLoopRetries  = 3            // 写入失败最大重试次数
	writeDeadline     = 10 * time.Second // 单次写入超时时间
)

// writeLoop 内部写入循环 goroutine。
// 从 writeCh 中逐条消费数据并通过底层连接写入网络。
// 网络写入失败时进行最多 writeLoopRetries 次指数退避重试：
//   - 第 1 次重试等待 100ms
//   - 第 2 次重试等待 200ms
//   - 第 3 次重试等待 400ms
//
// 全部重试失败后调用 forceCloseConn 关闭底层 TCP 连接，
// 触发消息读取方 ReadMessage 返回错误，进而由调用方执行 Close 完整清理。
// 使用 forceCloseConn 而非直接调用 Close 避免 writeLoop 自锁。
func (c *Client) writeLoop() {
	defer c.wg.Done()
	for {
		select {
		case data, ok := <-c.writeCh:
			if !ok {
				return
			}
			if err := c.writeWithRetry(data); err != nil {
				logger.WarnWithCtx(c.ctx, "ws client write failed after retries, force closing",
					logger.String("uid", c.uid),
					logger.String("remote_addr", c.remoteAddr),
					logger.Int("retries", writeLoopRetries),
					logger.Err(err),
				)
				c.forceCloseConn()
				return
			}
			atomic.AddInt64(&c.numSent, 1)
			c.markLastWrite()
		case <-c.closeCh:
			return
		}
	}
}

// writeWithRetry 带指数退避重试的写入操作。
// 每次写入前设置 10 秒 WriteDeadline，防止网络卡死。
// 参数:
//   - data: 待写入的序列化字节数据
//
// 返回:
//   - error: 所有重试均失败时返回最后一次错误
func (c *Client) writeWithRetry(data []byte) error {
	var lastErr error
	for attempt := 0; attempt <= writeLoopRetries; attempt++ {
		if attempt > 0 {
			// 指数退避：100ms, 200ms, 400ms
			backoff := time.Duration(100*math.Pow(2, float64(attempt-1))) * time.Millisecond
			time.Sleep(backoff)
		}

		// 设置写入超时，防止 TCP 半连接导致永久阻塞
		// 若设置 deadline 失败，说明连接已不可用，直接进入重试
		if err := c.conn.SetWriteDeadline(time.Now().Add(writeDeadline)); err != nil {
			lastErr = err
			c.recordWriteErr(err)
			continue
		}

		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			lastErr = err
			c.recordWriteErr(err)
			continue
		}
		return nil
	}
	return fmt.Errorf("write failed after %d retries: %w", writeLoopRetries, lastErr)
}

// recordWriteErr 原子记录写入错误计数和最近一次错误信息
func (c *Client) recordWriteErr(err error) {
	atomic.AddInt64(&c.writeErrCount, 1)
	c.lastWriteErr.Store(err.Error())
}

// markLastWrite 记录最后一次成功写入时间
func (c *Client) markLastWrite() {
	c.healthMu.Lock()
	c.lastWriteTime = time.Now()
	c.healthMu.Unlock()
}

// markLastRead 记录最后一次成功读取时间
func (c *Client) markLastRead() {
	c.healthMu.Lock()
	c.lastReadTime = time.Now()
	c.healthMu.Unlock()
}

// UID 返回当前连接关联的用户标识。
// 返回:
//   - string: 用户唯一标识，只读字段 goroutine 安全
func (c *Client) UID() string {
	return c.uid
}

// WriteJSON 异步非阻塞地向客户端发送 JSON 消息。
// 处理流程: JSON 序列化 → 写入缓冲通道 writeCh → writeLoop 串行写入网络。
// 当写入队列满载时立即返回 ErrWriteQueueFull 并丢弃消息，避免阻塞业务协程。
// 参数:
//   - v: 待发送的数据，会被序列化为 JSON 格式
//
// 返回:
//   - error: 连接已关闭时返回 websocket.ErrCloseSent，
//     队列满载时返回 ErrWriteQueueFull，
//     JSON 序列化失败时返回 json.Marshal 原始错误，
//     成功返回 nil
func (c *Client) WriteJSON(v any) error {
	// 链路追踪
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(c.ctx, "ws.write", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.uid", c.uid),
		attribute.String("ws.remote_addr", c.remoteAddr),
		requestIDAttr(c.ctx),
	)

	if atomic.LoadInt32(&c.closed) == 1 {
		span.SetAttributes(attribute.Bool("ws.closed", true))
		span.SetStatus(codes.Error, "connection closed")
		return websocket.ErrCloseSent
	}
	data, err := json.Marshal(v)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	select {
	case c.writeCh <- data:
		span.SetAttributes(attribute.Int("ws.write_queue_len", len(c.writeCh)))
		span.SetStatus(codes.Ok, "message queued")
		return nil
	default:
		span.SetAttributes(attribute.Bool("ws.write_queue_full", true))
		span.SetStatus(codes.Error, "write queue full, message dropped")
		return ErrWriteQueueFull
	}
}

// ReadMessage 同步阻塞读取客户端发送的一条消息。
// 直接委托给底层 websocket.Conn.ReadMessage。
// 配合 SetupPongHandler 设置的 ReadDeadline，当网络断开时返回超时错误。
// 返回:
//   - int: 消息类型（websocket.TextMessage=1 / websocket.BinaryMessage=2）
//   - []byte: 消息原始字节内容
//   - error: 读取失败、连接关闭或读取超时时返回非 nil 错误
func (c *Client) ReadMessage() (int, []byte, error) {
	// 链路追踪
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(c.ctx, "ws.read", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.uid", c.uid),
		attribute.String("ws.remote_addr", c.remoteAddr),
		requestIDAttr(c.ctx),
	)

	msgType, data, err := c.conn.ReadMessage()
	if err == nil {
		atomic.AddInt64(&c.numReceived, 1)
		c.markLastRead()
		span.SetAttributes(attribute.Int("ws.msg_type", msgType), attribute.Int("ws.msg_size", len(data)))
		span.SetStatus(codes.Ok, "message received")
	} else {
		span.SetStatus(codes.Error, err.Error())
	}
	return msgType, data, err
}

// SetCloseHook 设置关闭回调钩子，在 Close 时自动调用。
// 可用于执行 Dispatcher 注销等清理操作。
// 参数:
//   - fn: 回调函数，在底层连接关闭前执行
func (c *Client) SetCloseHook(fn func()) {
	c.onClose = fn
}

// Close 优雅关闭 WebSocket 连接，触发关闭信号并释放所有资源。
// 关闭顺序: 执行关闭钩子 → 取消上下文 → 关闭关闭信号通道 → 关闭写入队列通道 → 等待 writeLoop 退出 → 关闭底层连接。
// 通过 atomic.CompareAndSwapInt32 保证幂等性，首次调用执行完整关闭流程，
// 后续调用直接返回 nil。
// 返回:
//   - error: 首次关闭底层连接失败时返回 error，重复关闭返回 nil
func (c *Client) Close() error {
	if atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		if c.onClose != nil {
			c.onClose()
		}
		c.ctxCancel()                // 取消连接上下文，通知依赖 ctx 的协程
		close(c.closeCh)             // 通知监听 Done() 的所有协程退出
		close(c.writeCh)             // 触发 writeLoop 的 ok=false 分支退出
		c.wg.Wait()                  // 等待 writeLoop 完全退出，确保写入完毕或终止
		return c.conn.Close()
	}
	return nil
}

// forceCloseConn 强制关闭底层 TCP 连接，不等待 writeLoop 退出。
// 由 writeLoop（写入重试全部失败后）或 StartHeartbeat（Ping 发送失败后）调用。
// 关闭 TCP 连接后，消息读取方的 ReadMessage 会立即返回"use of closed network connection"错误，
// 调用方感知错误后执行 Close() 完成完整清理流程（幂等安全）。
// 此方法与 Close 分离设计，避免 writeLoop 自锁（Close 中 wg.Wait 等待 writeLoop 退出）。
func (c *Client) forceCloseConn() {
	if err := c.conn.Close(); err != nil {
		logger.WarnWithCtx(c.ctx, "ws force close conn failed",
			logger.String("uid", c.uid),
			logger.String("remote_addr", c.remoteAddr),
			logger.Err(err),
		)
	}
}

// RemoteAddr 返回客户端的远程网络地址（IP:Port）。
func (c *Client) RemoteAddr() string {
	return c.remoteAddr
}

// Context 返回客户端的关联上下文。
// Close 后该上下文被取消，可通过 ctx.Done() 感知连接关闭事件。
func (c *Client) Context() context.Context {
	return c.ctx
}

// SetContext 设置客户端的关联上下文。
// 可用于传递链路追踪、超时控制等业务上下文信息。
func (c *Client) SetContext(ctx context.Context) {
	c.ctx = ctx
}

// IsAlive 判断客户端连接是否处于健康状态。
// 返回 false 的场景：
//   - Close() 已调用
//   - writeLoop 已因写入重试全部失败退出（forceCloseConn 已触发）
//   - 超过 3 个心跳周期无成功写入（疑似僵尸连接）
//
// 注意：
//   - 此方法返回 true 不代表底层网络一定可达，仅表示组件内部状态正常
//   - 精确的活性检测依赖 Ping/Pong 协议级心跳 + ReadDeadline 联动
func (c *Client) IsAlive() bool {
	if atomic.LoadInt32(&c.closed) == 1 {
		return false
	}

	c.healthMu.RLock()
	lastWrite := c.lastWriteTime
	c.healthMu.RUnlock()

	// 如果从未写入过，视为存活（刚建立的连接）
	if lastWrite.IsZero() {
		return true
	}

	// 超过 3 个心跳周期无成功写入，标记为异常
	if time.Since(lastWrite) > 3*defaultHeartbeatInterval {
		return false
	}

	return true
}

// ClientStats 客户端连接统计信息。
type ClientStats struct {
	UID            string // 用户标识
	RemoteAddr     string // 远程地址
	NumSent        int64  // 已发送消息数
	NumReceived    int64  // 已接收消息数
	WriteQueueSize int    // 写入队列容量
	WriteQueueLen  int    // 写入队列当前长度
	IsClosed       bool   // 是否已关闭
	IsAlive        bool   // 是否健康（基于 IsAlive() 判断）
	WriteErrCount  int64  // 写入失败累计次数
	LastWriteErr   string // 最近一次写入错误信息
	LastWriteTime  string // 最后一次成功写入时间（ISO8601）
	LastReadTime   string // 最后一次成功读取时间（ISO8601）
}

// Stats 返回客户端连接的实时统计信息。
// 各字段均为 goroutine 安全读取。
func (c *Client) Stats() ClientStats {
	c.healthMu.RLock()
	lastWriteStr := ""
	lastReadStr := ""
	if !c.lastWriteTime.IsZero() {
		lastWriteStr = c.lastWriteTime.Format(time.RFC3339Nano)
	}
	if !c.lastReadTime.IsZero() {
		lastReadStr = c.lastReadTime.Format(time.RFC3339Nano)
	}
	c.healthMu.RUnlock()

	lastWriteErrStr, _ := c.lastWriteErr.Load().(string)

	return ClientStats{
		UID:            c.uid,
		RemoteAddr:     c.remoteAddr,
		NumSent:        atomic.LoadInt64(&c.numSent),
		NumReceived:    atomic.LoadInt64(&c.numReceived),
		WriteQueueSize: cap(c.writeCh),
		WriteQueueLen:  len(c.writeCh),
		IsClosed:       atomic.LoadInt32(&c.closed) == 1,
		IsAlive:        c.IsAlive(),
		WriteErrCount:  atomic.LoadInt64(&c.writeErrCount),
		LastWriteErr:   lastWriteErrStr,
		LastWriteTime:  lastWriteStr,
		LastReadTime:   lastReadStr,
	}
}

// Done 返回一个只读 channel，当连接关闭时该 channel 被 close。
// 调用方可通过 select 或 <-client.Done() 感知连接断开事件，
// 常用于心跳协程和消息读取协程的退出通知。
// 返回:
//   - <-chan struct{}: 关闭信号接收通道，关闭时立即返回零值
func (c *Client) Done() <-chan struct{} {
	return c.closeCh
}
