// Package gows 提供 WebSocket 服务端封装，包含连接管理、消息读写、心跳保活和全局分发。
//
// 核心设计:
//   - Upgrade: 从 gin.Context 将 HTTP 请求升级为 WebSocket 长连接，返回封装后的 Client
//   - Client: 连接封装层，采用 Channel 驱动写入模型，非阻塞 WriteJSON，自动关闭清理
//   - Dispatcher: 全局连接注册中心，支持在线连接管理
//   - StartHeartbeat: 心跳保活循环，周期性发送 ping
//   - ParseToken: JWT token 解析，提取用户标识
//
// 使用示例:
//
//	client, err := ws.Upgrade(c,
//	    ws.WithCheckOrigin(func(r *http.Request) bool { return true }),
//	    ws.WithHeartbeat(),
//	    ws.WithDispatcher(ws.DefaultDispatcher),
//	)
//	if err != nil {
//	    logger.WarnWithCtx(c.Request.Context(), "upgrade failed", logger.Err(err))
//	    return
//	}
//	defer client.Close()
//
//	ctx := context.WithValue(c.Request.Context(), key, client)
//	for {
//	    _, message, err := client.ReadMessage()
//	    if err != nil {
//	        break
//	    }
//	    _ = client.WriteJSON(ws.Message{Type: "reply", Msg: string(message)})
//	}
package gows

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// 默认配置常量
const (
	defaultReadBufferSize  = 4096 // 默认读取缓冲区大小 (4KB)
	defaultWriteBufferSize = 4096 // 默认写入缓冲区大小 (4KB)
)

// defaultCheckOrigin 默认的跨域检查函数，拒绝所有来源。
// 生产环境必须使用 WithCheckOrigin 显式设置允许的来源域名。
var defaultCheckOrigin = func(_ *http.Request) bool { return false }

// defaultCheckOriginWarned 确保 CORS 安全警告只打印一次
var defaultCheckOriginWarned bool

// checkOriginWarnOnce 首次使用默认 CheckOrigin 时打印安全警告
func checkOriginWarnOnce() {
	if !defaultCheckOriginWarned {
		defaultCheckOriginWarned = true
		logger.WarnWithCtx(nil, "[安全] gows: 默认 CheckOrigin 拒绝所有来源，" +
			"请使用 gows.WithCheckOrigin(fn) 显式设置允许的来源域名，" +
			"否则所有 WebSocket 连接将被拒绝")
	}
}

// 全局限流状态
var (
	upgradeLimiter   *rate.Limiter // 全局升级速率限制器
	upgradeLimiterMu sync.Mutex    // 保护限流器初始化

	perIPConns   sync.Map // map[string]*ipConnCounter 单IP连接计数
)

// ipConnCounter 单 IP 连接计数器
type ipConnCounter struct {
	count int32
}

func (c *ipConnCounter) add(n int32) int32 {
	return atomic.AddInt32(&c.count, n)
}

func (c *ipConnCounter) load() int32 {
	return atomic.LoadInt32(&c.count)
}

// extractClientIP 从 gin.Context 中提取客户端真实 IP
func extractClientIP(c *gin.Context) string {
	// 优先取 X-Forwarded-For
	if fwd := c.GetHeader("X-Forwarded-For"); fwd != "" {
		if ip := strings.TrimSpace(strings.Split(fwd, ",")[0]); ip != "" {
			return ip
		}
	}
	// 其次 X-Real-IP
	if realIP := c.GetHeader("X-Real-IP"); realIP != "" {
		return strings.TrimSpace(realIP)
	}
	// 最后从 RemoteAddr 解析
	if host, _, err := net.SplitHostPort(c.Request.RemoteAddr); err == nil {
		return host
	}
	return c.Request.RemoteAddr
}

// ErrUpgradeRateLimited 全局升级速率达到上限的错误
type ErrUpgradeRateLimited struct {
	limit rate.Limit
}

func (e *ErrUpgradeRateLimited) Error() string {
	return fmt.Sprintf("ws upgrade rate limited: %.2f rps", e.limit)
}

// ErrUpgradeMaxConnPerIP 单 IP 连接数达到上限的错误
type ErrUpgradeMaxConnPerIP struct {
	ip  string
	max int32
}

func (e *ErrUpgradeMaxConnPerIP) Error() string {
	return fmt.Sprintf("ws upgrade max connections per IP reached: ip=%s max=%d", e.ip, e.max)
}

// upgradeOptions 内部升级配置参数集合
type upgradeOptions struct {
	checkOrigin       func(r *http.Request) bool  // 跨域检查函数
	checkOriginSet    bool                        // 用户是否显式设置了 CheckOrigin
	readBufSize       int                         // 读取缓冲区大小，0 表示使用默认值 4096
	writeBufSize      int                         // 写入缓冲区大小，0 表示使用默认值 4096
	subprotocols      []string                    // 子协议协商列表
	enableCompression bool                        // 是否启用压缩（默认 true）
	enableHeart       bool                        // 是否自动启动心跳保活
	heartbeatOpts     []HeartbeatOption           // 心跳高级配置（与 enableHeart 配合使用）
	dispatcher        *DistributedDispatcher      // 非 nil 时自动注册连接到此分发中心
	clientUID         string                      // 客户端用户标识（可选，提供给 NewClient）
	errorHandler      func(*gin.Context, error)   // 升级失败时的自定义错误处理
	beforeUpgrade     func(*gin.Context) error    // 升级前钩子，返回 error 则中止升级
	afterUpgrade      func(*gin.Context, *Client) // 升级后钩子，可用于链路追踪注入等

	// 限流配置（仅在 Upgrade 函数中读取，不存储在选项上）
	enableRateLimit  bool  // 是否启用全局速率限制
	maxConnPerIP     int32 // 单 IP 最大连接数（0=不限制）
	readTimeout      time.Duration // 单次 ReadMessage 超时（0=由心跳管理）
}

// UpgradeOption 升级配置选项函数类型。
// 采用 Functional Options 模式，支持可扩展的配置传递，
// 新增配置项无需修改 Upgrade 的函数签名。
type UpgradeOption func(*upgradeOptions)

// WithCheckOrigin 设置 WebSocket 跨域检查函数。
// 参数:
//   - fn: 接收 *http.Request，返回 true 表示允许该来源连接
//
// 默认拒绝所有来源。生产环境必须根据实际域名配置。
func WithCheckOrigin(fn func(r *http.Request) bool) UpgradeOption {
	return func(o *upgradeOptions) {
		o.checkOrigin = fn
		o.checkOriginSet = true
	}
}

// WithRateLimit 设置全局 WebSocket 升级速率限制。
// 参数:
//   - rps: 每秒最大升级次数（如 100 表示每秒最多处理 100 次握手）
//   - burst: 最大突发量（如 20 表示短时间内允许最多 20 个突发连接）
//
// 超出限制的 Upgrade 调用返回 ErrUpgradeRateLimited。
// 默认不限制。
func WithRateLimit(rps int, burst int) UpgradeOption {
	return func(o *upgradeOptions) {
		if rps > 0 && burst > 0 {
			o.enableRateLimit = true
			upgradeLimiterMu.Lock()
			upgradeLimiter = rate.NewLimiter(rate.Limit(rps), burst)
			upgradeLimiterMu.Unlock()
		}
	}
}

// WithMaxConnPerIP 设置单个 IP 的最大 WebSocket 连接数。
// 参数:
//   - n: 单 IP 最大连接数（如 10 表示每个 IP 最多建立 10 个连接）
//
// 超出限制的 Upgrade 调用返回 ErrUpgradeMaxConnPerIP。
// 默认不限制。连接关闭时自动从计数器中移除。
func WithMaxConnPerIP(n int) UpgradeOption {
	return func(o *upgradeOptions) {
		if n > 0 {
			o.maxConnPerIP = int32(n)
		}
	}
}

// WithReadTimeout 设置单次 ReadMessage 的超时时间。
// 参数:
//   - timeout: 读取超时时间（如 60*time.Second）。超过此时间未收到消息，
//     ReadMessage 返回超时错误，触发连接断开和重连。
//
// 此超时会在 Upgrade 内部自动传播到创建的 Client，无需额外传入 NewClient。
// 默认 0 表示不设置主动超时，由心跳机制间接管理 ReadDeadline。
func WithReadTimeout(timeout time.Duration) UpgradeOption {
	return func(o *upgradeOptions) {
		if timeout > 0 {
			o.readTimeout = timeout
		}
	}
}

// WithBufferSize 设置 WebSocket 读写缓冲区大小（字节）。
// 参数:
//   - read: 读取缓冲区大小，<=0 时使用默认值 4096
//   - write: 写入缓冲区大小，<=0 时使用默认值 4096
//
// 对于传输大消息的业务（如文件流、大 JSON），建议增大到 16384 以上。
func WithBufferSize(read, write int) UpgradeOption {
	return func(o *upgradeOptions) {
		if read > 0 {
			o.readBufSize = read
		}
		if write > 0 {
			o.writeBufSize = write
		}
	}
}

// WithSubprotocols 设置 WebSocket 子协议协商列表。
// 参数:
//   - protocols: 服务端支持的子协议列表，按优先级排列
//
// 客户端在 Sec-WebSocket-Protocol 头中声明支持的协议列表，
// 服务端从中选择一个匹配的返回，用于协议版本协商或多协议支持。
func WithSubprotocols(protocols ...string) UpgradeOption {
	return func(o *upgradeOptions) {
		o.subprotocols = protocols
	}
}

// WithHeartbeat 启用心跳保活机制，升级后自动在后台协程运行 StartHeartbeat。
// 心跳间隔固定为 30 秒，发送 {"type":"ping"} 消息。
func WithHeartbeat() UpgradeOption {
	return func(o *upgradeOptions) {
		o.enableHeart = true
	}
}

// WithHeartbeatOptions 启用心跳保活并传入高级配置参数。
// 参数:
//   - opts: 心跳配置选项，如 WithHeartbeatInterval、WithPingMessage 等
//
// 示例:
//
//	ws.Upgrade(c,
//	    ws.WithHeartbeatOptions(
//	        ws.WithHeartbeatInterval(15*time.Second),
//	        ws.WithPingMessage(func() any { return Message{Type: "ping", Data: time.Now().Unix()} }),
//	    ),
//	)
func WithHeartbeatOptions(opts ...HeartbeatOption) UpgradeOption {
	return func(o *upgradeOptions) {
		o.enableHeart = true
		o.heartbeatOpts = opts
	}
}

// WithDispatcher 设置升级后自动将客户端注册到指定 Dispatcher。
// 参数:
//   - d: 分发中心实例，通常传入 ws.DefaultDispatcher
//
// 注册后可通过 Dispatcher 全局管理在线连接（如广播消息）。
// 连接关闭时自动从 Dispatcher 注销。
func WithDispatcher(d *DistributedDispatcher) UpgradeOption {
	return func(o *upgradeOptions) {
		o.dispatcher = d
	}
}

// WithClientUID 设置客户端用户标识，传递给 Client 用于业务追踪。
// 参数:
//   - uid: 用户唯一标识（如从 JWT 解析的 sub）
func WithClientUID(uid string) UpgradeOption {
	return func(o *upgradeOptions) {
		o.clientUID = uid
	}
}

// WithErrorHandler 设置 WebSocket 升级失败时的错误处理回调。
// 参数:
//   - fn: 接收 gin.Context 和错误信息，可在此进行自定义响应（如返回 JSON 错误体）
//
// 默认行为: Upgrade 返回 error 给调用方自行处理。
// 设置后，在返回 error 的同时会额外调用此回调，方便统一错误处理。
func WithErrorHandler(fn func(*gin.Context, error)) UpgradeOption {
	return func(o *upgradeOptions) {
		o.errorHandler = fn
	}
}

// WithBeforeUpgrade 设置升级前钩子，在 HTTP 升级为 WebSocket 之前执行。
// 参数:
//   - fn: 钩子函数，接收 gin.Context。返回 error 则中止升级流程。
//
// 可用于自定义鉴权、限流检查等前置校验场景。
func WithBeforeUpgrade(fn func(*gin.Context) error) UpgradeOption {
	return func(o *upgradeOptions) {
		o.beforeUpgrade = fn
	}
}

// WithAfterUpgrade 设置升级后钩子，在 WebSocket 连接建立后执行。
// 参数:
//   - fn: 钩子函数，接收 gin.Context 和已创建的 Client
//
// 可用于注入链路追踪上下文、记录连接日志等场景。
func WithAfterUpgrade(fn func(*gin.Context, *Client)) UpgradeOption {
	return func(o *upgradeOptions) {
		o.afterUpgrade = fn
	}
}

// WithEnableCompression 设置是否启用 WebSocket 压缩。
// 参数:
//   - enable: true 启用（默认），false 禁用。
//
// 压缩可减少带宽消耗，但会增加 CPU 开销。
// 内网环境或传输已压缩数据（如视频流）时建议关闭。
func WithEnableCompression(enable bool) UpgradeOption {
	return func(o *upgradeOptions) {
		o.enableCompression = enable
	}
}

// Upgrade 将 HTTP 请求升级为 WebSocket 长连接，并返回封装后的 Client。
//
// 参数:
//   - c: Gin 上下文，提供 ResponseWriter 和 Request
//   - opts: 可选配置参数
//
// 返回:
//   - *Client: 封装后的 WebSocket 客户端，具备非阻塞写入能力
//   - error: 升级失败时返回错误
//
// 使用示例:
//
//	client, err := gows.Upgrade(c,
//	    gows.WithHeartbeat(),
//	    gows.WithDispatcher(gows.DefaultDispatcher),
//	)
//
// Upgrade 负责:
//  1. 执行升级前钩子（可选）
//  2. 创建 websocket.Upgrader 并应用配置
//  3. 执行 HTTP→WebSocket 升级
//  4. 执行升级后钩子（可选）
//  5. 用 *websocket.Conn 创建 *Client（含 writeLoop）
//  6. 可选启动心跳、注册 Dispatcher
//
// 安全保护:
//   - CORS: 默认拒绝所有来源，需显式调用 WithCheckOrigin
//   - 限流: WithRateLimit 设置全局升级速率
//   - 单IP限制: WithMaxConnPerIP 设置单IP最大连接数
//
// 调用方需负责 defer client.Close() 确保资源释放。
func Upgrade(c *gin.Context, opts ...UpgradeOption) (*Client, error) {
	tracer := otel.Tracer("gows")
	ctx, span := tracer.Start(c.Request.Context(), "ws.upgrade", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	requestID := requestIDAttr(c.Request.Context())

	o := upgradeOptions{
		checkOrigin:       defaultCheckOrigin,
		readBufSize:       defaultReadBufferSize,
		writeBufSize:      defaultWriteBufferSize,
		enableCompression: true,
	}
	for _, opt := range opts {
		opt(&o)
	}

	clientIP := extractClientIP(c)

	span.SetAttributes(
		attribute.String("ws.client_uid", o.clientUID),
		attribute.String("ws.remote_addr", c.Request.RemoteAddr),
		attribute.String("ws.client_ip", clientIP),
		requestID,
	)

	// CORS 安全检查：默认 CheckOrigin 为拒绝所有来源，需显式配置
	if !o.checkOriginSet {
		checkOriginWarnOnce()
		span.SetAttributes(attribute.Bool("ws.cors_rejected", true))
		span.SetStatus(codes.Error, "CORS check rejected")
		return nil, fmt.Errorf("ws upgrade: CORS check rejected, use gows.WithCheckOrigin() to allow origins")
	}
	if !o.checkOrigin(c.Request) {
		span.SetAttributes(attribute.Bool("ws.cors_rejected", true))
		span.SetStatus(codes.Error, "CORS check rejected")
		return nil, fmt.Errorf("ws upgrade: CORS check rejected by CheckOrigin function")
	}

	// 全局速率限制检查
	if o.enableRateLimit {
		upgradeLimiterMu.Lock()
		limiter := upgradeLimiter
		upgradeLimiterMu.Unlock()
		if limiter != nil && !limiter.Allow() {
			span.SetAttributes(attribute.Bool("ws.rate_limited", true))
			span.SetStatus(codes.Error, "rate limited")
			return nil, &ErrUpgradeRateLimited{limit: limiter.Limit()}
		}
	}

	// 单 IP 连接数检查
	if o.maxConnPerIP > 0 {
		actual, _ := perIPConns.LoadOrStore(clientIP, &ipConnCounter{})
		counter := actual.(*ipConnCounter)
		if counter.load() >= o.maxConnPerIP {
			span.SetAttributes(attribute.Bool("ws.per_ip_limit_reached", true))
			span.SetStatus(codes.Error, "per-IP max connections reached")
			return nil, &ErrUpgradeMaxConnPerIP{ip: clientIP, max: o.maxConnPerIP}
		}
	}

	// 升级前钩子：可用于鉴权等前置校验
	if o.beforeUpgrade != nil {
		if err := o.beforeUpgrade(c); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("ws before upgrade: %w", err)
		}
	}

	upgrader := websocket.Upgrader{
		CheckOrigin:       o.checkOrigin,
		ReadBufferSize:    o.readBufSize,
		WriteBufferSize:   o.writeBufSize,
		Subprotocols:      o.subprotocols,
		EnableCompression: o.enableCompression,
	}

	rawConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		if o.errorHandler != nil {
			o.errorHandler(c, err)
		}
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ws upgrade: %w", err)
	}

	// 升级成功后，递增单 IP 连接计数
	if o.maxConnPerIP > 0 {
		actual, _ := perIPConns.LoadOrStore(clientIP, &ipConnCounter{})
		counter := actual.(*ipConnCounter)
		counter.add(1)
	}

	client := NewClient(rawConn, o.clientUID)
	// 传播 readTimeout（UpgradeOption → Client）
	if o.readTimeout > 0 {
		client.readTimeout = o.readTimeout
	}
	// 使用带追踪的上下文替换 client 的默认上下文
	client.SetContext(ctx)

	// 注册 IP 连接清理钩子
	if o.maxConnPerIP > 0 {
		prevHook := client.onClose
		client.SetCloseHook(func() {
			if prevHook != nil {
				prevHook()
			}
			// 递减 IP 计数
			if actual, ok := perIPConns.Load(clientIP); ok {
				counter := actual.(*ipConnCounter)
				if counter.add(-1) <= 0 {
					perIPConns.Delete(clientIP)
				}
			}
		})
	}

	// 升级后钩子：可用于注入链路追踪、记录连接日志等
	if o.afterUpgrade != nil {
		o.afterUpgrade(c, client)
	}

	// 心跳保活
	if o.enableHeart {
		if len(o.heartbeatOpts) > 0 {
			go StartHeartbeat(client, o.heartbeatOpts...)
		} else {
			go StartHeartbeat(client)
		}
	}

	// 全局分发注册
	if o.dispatcher != nil {
		if err := o.dispatcher.Register(client); err != nil {
			// 注册失败（如达到连接上限），立即清理并返回错误
			if closeErr := client.Close(); closeErr != nil {
				logger.WarnWithCtx(c.Request.Context(), "ws client close after register failed",
					logger.Err(closeErr),
				)
			}
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("ws dispatcher register: %w", err)
		}
		client.SetCloseHook(func() {
			// 先执行之前的钩子（IP 清理等）
			if prevHook := client.onClose; prevHook != nil {
				prevHook()
			}
			o.dispatcher.Unregister(client)
		})
	}

	span.SetStatus(codes.Ok, "upgrade success")
	return client, nil
}
