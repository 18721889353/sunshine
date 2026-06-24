package gows

import (
	"time"

	"github.com/gorilla/websocket"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

const (
	// defaultHeartbeatInterval 默认心跳间隔 30 秒
	defaultHeartbeatInterval = 30 * time.Second
	// defaultPongTimeout 等待 Pong 响应超时时间。
	// 超过此时间未收到 Pong 则认为连接已断开。
	defaultPongTimeout = 10 * time.Second
	// defaultPingWriteWait Ping 控制帧写入超时
	defaultPingWriteWait = 5 * time.Second
)

// HeartbeatOption 心跳配置选项函数类型。
// 采用 Functional Options 模式，支持可扩展的配置传递。
type HeartbeatOption func(*heartbeatOptions)

// heartbeatOptions 心跳内部配置参数集合
type heartbeatOptions struct {
	interval      time.Duration // 心跳发送间隔
	pongTimeout   time.Duration // Pong 等待超时（默认 10s）
	pingWriteWait time.Duration // Ping 控制帧写入超时（默认 5s）
}

// WithHeartbeatInterval 设置心跳发送间隔。
// 参数:
//   - interval: 间隔时间，建议 10~60 秒。<=0 时使用默认值 30 秒。
func WithHeartbeatInterval(interval time.Duration) HeartbeatOption {
	return func(o *heartbeatOptions) {
		if interval > 0 {
			o.interval = interval
		}
	}
}

// WithPongTimeout 设置 Pong 超时时间。
// 参数:
//   - timeout: 超过此时间未收到 Pong 响应帧，触发连接关闭。
//     建议为心跳间隔的 1/3 ~ 1/2，默认 10 秒。
func WithPongTimeout(timeout time.Duration) HeartbeatOption {
	return func(o *heartbeatOptions) {
		if timeout > 0 {
			o.pongTimeout = timeout
		}
	}
}

// WithPingWriteWait 设置 Ping 控制帧的写入超时时间。
// 参数:
//   - timeout: 超过此时间 Ping 帧写入失败，认为连接异常。默认 5 秒。
func WithPingWriteWait(timeout time.Duration) HeartbeatOption {
	return func(o *heartbeatOptions) {
		if timeout > 0 {
			o.pingWriteWait = timeout
		}
	}
}

// SetupPongHandler 在底层 WebSocket 连接上设置 Pong 处理器和读取超时。
//
// 核心机制：
//   - 设置 SetPongHandler，每次收到 Pong 响应帧时刷新 ReadDeadline
//   - ReadDeadline = interval + 3 × pongTimeout（保证大于心跳间隔，避免竞态）
//   - 配合 StartHeartbeat 发送的 Ping 控制帧，构成协议级连接活性检测
//   - 当网络断开时，ReadMessage 会在 ReadDeadline 过后返回 timeout 错误
//
// 参数:
//   - conn: 底层 WebSocket 连接
//   - interval: 心跳间隔，用于计算 ReadDeadline 的下限
//   - pongTimeout: Pong 超时基准值
func SetupPongHandler(conn *websocket.Conn, interval, pongTimeout time.Duration) {
	// readDeadline 必须大于 heartbeatInterval，避免 ticker 与 deadline 同频竞态
	// 公式: interval + 2 × pongTimeout，默认值下 = 30 + 20 = 50s
	readDeadline := interval + 2*pongTimeout
	conn.SetReadDeadline(time.Now().Add(readDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(readDeadline))
		return nil
	})
}

// StartHeartbeat 启动 WebSocket 协议级 Ping/Pong 心跳保活循环。
//
// 设计说明：
//   - 通过 conn.WriteControl 发送 PingMessage 控制帧
//   - 对端 WebSocket 协议栈自动回复 Pong 响应帧
//   - PongHandler 每次收到 Pong 时刷新 ReadDeadline
//   - 网络断开时 Pong 超时 → ReadDeadline 过期 → ReadMessage 返回超时错误
//   - 心跳协程退出时主动关闭 TCP 连接，触发 read 方感知断开
//
// 参数:
//   - client: 需要保活的客户端连接
//   - opts: 可选心跳配置（间隔、Pong 超时等）
func StartHeartbeat(client *Client, opts ...HeartbeatOption) {
	o := heartbeatOptions{
		interval:      defaultHeartbeatInterval,
		pongTimeout:   defaultPongTimeout,
		pingWriteWait: defaultPingWriteWait,
	}
	for _, opt := range opts {
		opt(&o)
	}

	// 链路追踪：心跳协程生命周期
	tracer := otel.Tracer("gows")
	ctx, span := tracer.Start(client.ctx, "ws.heartbeat", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.uid", client.uid),
		attribute.String("ws.remote_addr", client.remoteAddr),
		attribute.String("ws.heartbeat_interval", o.interval.String()),
		requestIDAttr(client.ctx),
	)

	// 设置协议级 Pong 处理器 + ReadDeadline
	// ReadDeadline 必须大于 heartbeatInterval，避免 ticker 与 deadline 同频竞态
	SetupPongHandler(client.conn, o.interval, o.pongTimeout)

	// 立即发送一次 Ping，让 PongHandler 在首帧 tick 前刷新 ReadDeadline
	// 避免 ReadDeadline 在首帧 Ping 发送前过期导致误判
	if err := client.conn.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(o.pingWriteWait)); err != nil {
		span.SetAttributes(attribute.Bool("ws.initial_ping_failed", true))
		logger.WarnWithCtx(ctx, "ws initial ping failed, connection dead",
			logger.String("uid", client.uid),
			logger.String("remote_addr", client.remoteAddr),
			logger.Err(err),
		)
		client.forceCloseConn()
		return
	}

	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 发送 WebSocket 协议级 Ping 控制帧（连接活性探测）
			// 对端协议栈自动回复 Pong，PongHandler 刷新 ReadDeadline
			// 写入超时由 pingWriteWait 控制，防止 TCP 半连接导致卡死
			if err := client.conn.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(o.pingWriteWait)); err != nil {
				span.SetAttributes(attribute.Int("ws.ping_fail_count", 1))
				span.SetStatus(codes.Error, err.Error())
				logger.WarnWithCtx(ctx, "ws protocol ping failed, connection dead",
					logger.String("uid", client.uid),
					logger.String("remote_addr", client.remoteAddr),
					logger.Err(err),
				)
				client.forceCloseConn()
				return
			}

		case <-client.Done():
			span.SetStatus(codes.Ok, "heartbeat stopped")
			return
		}
	}
}
