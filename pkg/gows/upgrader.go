// Package ws 提供 WebSocket 服务端封装，包含连接管理、消息读写、心跳保活和全局分发。
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
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// 默认配置常量
const (
	defaultReadBufferSize  = 4096 // 默认读取缓冲区大小 (4KB)
	defaultWriteBufferSize = 4096 // 默认写入缓冲区大小 (4KB)
)

// defaultCheckOrigin 默认的跨域检查函数，允许所有来源。
// 生产环境中建议显式配置 WithCheckOrigin 限制可信域名。
var defaultCheckOrigin = func(r *http.Request) bool { return true }

// upgradeOptions 内部升级配置参数集合
type upgradeOptions struct {
	checkOrigin       func(r *http.Request) bool // 跨域检查函数
	readBufSize       int                        // 读取缓冲区大小，0 表示使用默认值 4096
	writeBufSize      int                        // 写入缓冲区大小，0 表示使用默认值 4096
	subprotocols      []string                   // 子协议协商列表
	enableCompression bool                       // 是否启用压缩（默认 true）
	enableHeart       bool                       // 是否自动启动心跳保活
	heartbeatOpts     []HeartbeatOption          // 心跳高级配置（与 enableHeart 配合使用）
	dispatcher        *Dispatcher                // 非 nil 时自动注册连接到此分发中心
	clientUid         string                     // 客户端用户标识（可选，提供给 NewClient）
	errorHandler      func(*gin.Context, error)  // 升级失败时的自定义错误处理
	beforeUpgrade     func(*gin.Context) error   // 升级前钩子，返回 error 则中止升级
	afterUpgrade      func(*gin.Context, *Client) // 升级后钩子，可用于链路追踪注入等
}

// UpgradeOption 升级配置选项函数类型。
// 采用 Functional Options 模式，支持可扩展的配置传递，
// 新增配置项无需修改 Upgrade 的函数签名。
type UpgradeOption func(*upgradeOptions)

// WithCheckOrigin 设置 WebSocket 跨域检查函数。
// 参数:
//   - fn: 接收 *http.Request，返回 true 表示允许该来源连接
//
// 默认允许所有来源。生产环境必须根据实际域名配置。
func WithCheckOrigin(fn func(r *http.Request) bool) UpgradeOption {
	return func(o *upgradeOptions) {
		o.checkOrigin = fn
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
func WithDispatcher(d *Dispatcher) UpgradeOption {
	return func(o *upgradeOptions) {
		o.dispatcher = d
	}
}

// WithClientUID 设置客户端用户标识，传递给 Client 用于业务追踪。
// 参数:
//   - uid: 用户唯一标识（如从 JWT 解析的 sub）
func WithClientUID(uid string) UpgradeOption {
	return func(o *upgradeOptions) {
		o.clientUid = uid
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
// 调用方需负责 defer client.Close() 确保资源释放。
func Upgrade(c *gin.Context, opts ...UpgradeOption) (*Client, error) {
	o := upgradeOptions{
		checkOrigin:       defaultCheckOrigin,
		readBufSize:       defaultReadBufferSize,
		writeBufSize:      defaultWriteBufferSize,
		enableCompression: true,
	}
	for _, opt := range opts {
		opt(&o)
	}

	// 升级前钩子：可用于鉴权、限流等前置校验
	if o.beforeUpgrade != nil {
		if err := o.beforeUpgrade(c); err != nil {
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
		return nil, fmt.Errorf("ws upgrade: %w", err)
	}

	client := NewClient(rawConn, o.clientUid)

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
		o.dispatcher.Register(client)
		client.SetCloseHook(func() {
			o.dispatcher.Unregister(client)
		})
	}

	return client, nil
}
