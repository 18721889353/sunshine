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

// HeartbeatOption / heartbeatOptions / WithHeartbeatInterval / WithPongTimeout / WithPingWriteWait 定义在 heartbeat_options.go

const (
	// defaultHeartbeatInterval 默认心跳间隔 30 秒
	defaultHeartbeatInterval = 30 * time.Second
	// defaultPongTimeout 等待 Pong 响应超时时间。
	// 超过此时间未收到 Pong 则认为连接已断开。
	defaultPongTimeout = 10 * time.Second
	// defaultPingWriteWait Ping 控制帧写入超时
	defaultPingWriteWait = 5 * time.Second
)

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
	if err := conn.SetReadDeadline(time.Now().Add(readDeadline)); err != nil {
		return // 连接可能已关闭，无需继续设置
	}
	conn.SetPongHandler(func(string) error {
		if err := conn.SetReadDeadline(time.Now().Add(readDeadline)); err != nil {
			return nil // 连接可能已关闭，忽略
		}
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
	o := defaultHeartbeatOptions()
	o.apply(opts...)

	// 链路追踪：心跳协程生命周期
	tracer := otel.Tracer("gows")
	ctx, span := tracer.Start(client.clientCtx, "ws.heartbeat", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.uid", client.uid),
		attribute.String("ws.remote_addr", client.remoteAddr),
		attribute.String("ws.heartbeat_interval", o.interval.String()),
		requestIDAttr(client.clientCtx),
	)

	// 设置协议级 Pong 处理器 + ReadDeadline
	// ReadDeadline 必须大于 heartbeatInterval，避免 ticker 与 deadline 同频竞态
	SetupPongHandler(client.wsConn, o.interval, o.pongTimeout)

	// 立即发送一次 Ping，让 PongHandler 在首帧 tick 前刷新 ReadDeadline
	// 避免 ReadDeadline 在首帧 Ping 发送前过期导致误判
	if err := client.wsConn.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(o.pingWriteWait)); err != nil {
		span.SetAttributes(attribute.Bool("ws.initial_ping_failed", true))
		logger.WarnWithCtx(ctx, "ws initial ping failed, connection dead",
			logger.String("uid", client.uid),
			logger.String("remote_addr", client.remoteAddr),
			logger.Err(err),
		)
		if closeErr := client.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "ws close failed after initial ping failure",
				logger.String("uid", client.uid),
				logger.String("remote_addr", client.remoteAddr),
				logger.Err(closeErr),
			)
		}
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
			if err := client.wsConn.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(o.pingWriteWait)); err != nil {
				span.SetAttributes(attribute.Int("ws.ping_fail_count", 1))
				span.SetStatus(codes.Error, err.Error())
				logger.WarnWithCtx(ctx, "ws protocol ping failed, connection dead",
					logger.String("uid", client.uid),
					logger.String("remote_addr", client.remoteAddr),
					logger.Err(err),
				)
				if closeErr := client.Close(); closeErr != nil {
					logger.WarnWithCtx(ctx, "ws close failed after ping failure",
						logger.String("uid", client.uid),
						logger.String("remote_addr", client.remoteAddr),
						logger.Err(closeErr),
					)
				}
				return
			}

		case <-client.Done():
			span.SetStatus(codes.Ok, "heartbeat stopped")
			return
		}
	}
}
