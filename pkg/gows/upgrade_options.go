package gows

import (
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// upgraderConfig
// 嵌入 upgradeOptions，作为 websocket.Upgrader 的配置载体。
// 按大厂标准将 Upgrade 相关字段独立为嵌入结构体，与 clientConfig 模式一致。
type upgraderConfig struct {
	checkOrigin       func(r *http.Request) bool // 跨域检查函数
	checkOriginSet    bool                       // 用户是否显式设置了 CheckOrigin
	readBufSize       int                        // 读取缓冲区大小，0 表示使用默认值 4096
	writeBufSize      int                        // 写入缓冲区大小，0 表示使用默认值 4096
	subprotocols      []string                   // 子协议协商列表
	enableCompression bool                       // 是否启用压缩（默认 true）
}

// upgradeOptions 内部升级配置参数集合
type upgradeOptions struct {
	// 单点登录配置（仅在 Upgrade 函数中读取，不存储在选项上）
	enableSSO bool // 是否启用单点登录（后登录踢前登录，同一 UID 仅保留一个连接）

	upgraderConfig // 嵌入升级器配置字段

	clientConfig // 嵌入通用客户端配置，由 Upgrade 直接传入 NewClient

	enableHeart   bool              // 是否自动启动心跳保活
	heartbeatOpts []HeartbeatOption // 心跳高级配置（与 enableHeart 配合使用）

	enableDistributed bool // 是否启用分布式分发注册

	// 限流配置（仅在 Upgrade 函数中读取，不存储在选项上）
	enableWsRateLimit bool // 是否启用全局 WebSocket 速率限制

	clientUID    string // 客户端用户标识（传递给 NewClient）
	maxConnPerIP int32  // 单 IP 最大连接数（0=不限制）

}

func defaultUpgradeOptions() *upgradeOptions {
	return &upgradeOptions{
		clientConfig: clientConfig{
			writeChSize: 1024,
			readChSize:  1024,
		},
		upgraderConfig: upgraderConfig{
			checkOrigin:       func(_ *http.Request) bool { return false },
			readBufSize:       4096,
			writeBufSize:      4096,
			enableCompression: true,
		},
	}
}

func (o *upgradeOptions) apply(opts ...UpgradeOption) {
	for _, opt := range opts {
		opt(o)
	}
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

// WithWsRateLimit 设置全局 WebSocket 升级速率限制。
// 参数:
//   - rps: 每秒最大升级次数（如 100 表示每秒最多处理 100 次握手）
//   - burst: 最大突发量（如 20 表示短时间内允许最多 20 个突发连接）
//
// 超出限制的 Upgrade 调用返回 rate limited 错误。
// 默认不限制。
func WithWsRateLimit(rps int, burst int) UpgradeOption {
	return func(o *upgradeOptions) {
		if rps > 0 && burst > 0 {
			o.enableWsRateLimit = true
			wsLimiter.Store(rate.NewLimiter(rate.Limit(rps), burst))
		}
	}
}

// WithMaxConnPerIP 设置单个 IP 的最大 WebSocket 连接数。
// 参数:
//   - n: 单 IP 最大连接数（如 10 表示每个 IP 最多建立 10 个连接）
//
// 超出限制的 Upgrade 调用返回 max connections per IP 错误。
// 默认不限制。连接关闭时自动从计数器中移除。
func WithMaxConnPerIP(n int) UpgradeOption {
	return func(o *upgradeOptions) {
		if n > 0 {
			o.maxConnPerIP = int32(n)
		}
	}
}

// WithReadLimit 设置单条消息的最大读取字节数（Upgrade 入口）。
// 超过此大小的消息将被拒绝，连接关闭。
// 默认 0 表示不限制（采用 gorilla/websocket 默认值 32768）。
// 此配置会在 Upgrade 内部自动传播到创建的 Client。
func WithReadLimit(limit int64) UpgradeOption {
	return func(o *upgradeOptions) {
		if limit > 0 {
			o.readLimit = limit
		}
	}
}

// WithWriteLimit 设置单条消息的最大写入字节数（Upgrade 入口）。
// 超过此大小的消息将被 WriteJSON/WriteRaw 拒绝，返回 ErrWriteLimitExceeded。
// 默认 0 表示不限制。
// 此配置会在 Upgrade 内部自动传播到创建的 Client。
func WithWriteLimit(limit int64) UpgradeOption {
	return func(o *upgradeOptions) {
		if limit > 0 {
			o.writeLimit = limit
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

// WithWriteTimeout 设置单次写入的超时时间。
// 参数:
//   - timeout: 写入超时时间（如 30*time.Second）。超过此时间写入操作未完成，
//     将触发重试机制。
//
// 此超时会在 Upgrade 内部自动传播到创建的 Client 的 writeWithRetry。
// 默认 0 表示使用默认值 10s。
func WithWriteTimeout(timeout time.Duration) UpgradeOption {
	return func(o *upgradeOptions) {
		if timeout > 0 {
			o.writeTimeout = timeout
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

// WithEnableDistributed 设置是否启用分布式分发注册。
// 参数:
//   - enabled: true 启用分布式模式，连接自动注册到 Dispatcher
//
// 需同时调用 WithDispatcher 设置目标 Dispatcher 实例。
func WithEnableDistributed(enabled bool) UpgradeOption {
	return func(o *upgradeOptions) {
		o.enableDistributed = enabled
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

// WithSSO 启用单点登录模式，同一 UID 仅保留一个有效连接。
// 当已在线用户再次建立连接时，旧连接会被自动关闭（后登录踢前登录）。
func WithSSO() UpgradeOption {
	return func(o *upgradeOptions) {
		o.enableSSO = true
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

// WithQueueSize 设置客户端读写队列缓冲区容量。
// 参数:
//   - writeSize: 写入队列容量，<=0 时使用默认值 1024。增大可减少高并发时的丢包，但占用更多内存。
//   - readSize:  读取队列容量，<=0 时使用默认值 1024。增大可应对消费速度跟不上生产速度的场景。
//
// 队列满载时新消息会被丢弃（写入）或丢失（读取），这是背压保护机制。
// 建议根据业务峰值 QPS 和消息大小估算，通常在 1024~4096 之间。
func WithQueueSize(writeSize, readSize int) UpgradeOption {
	return func(o *upgradeOptions) {
		if writeSize > 0 {
			o.writeChSize = writeSize
		}
		if readSize > 0 {
			o.readChSize = readSize
		}
	}
}
