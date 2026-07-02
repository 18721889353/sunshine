package gows

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// 全局限流状态
var (
	wsLimiter atomic.Pointer[rate.Limiter] // 全局 WebSocket 升级速率限制器
	ipConnCounts   sync.Map                     // map[string]*atomic.Int32 单IP连接计数
)

// upgradeOptions / UpgradeOption / With* 定义在 upgrade_options.go

// upgradePerIPCheck 执行单 IP 连接数检查（只读，不创建计数器）。
func upgradePerIPCheck(clientIP string, maxConnPerIP int32, span trace.Span) error {
	actual, ok := ipConnCounts.Load(clientIP)
	if !ok {
		return nil // 首次连接，无现有计数
	}
	counter, ok := actual.(*atomic.Int32)
	if !ok {
		return fmt.Errorf("ws upgrade: per-IP counter type assertion failed")
	}
	if counter.Load() >= maxConnPerIP {
		span.SetAttributes(attribute.Bool("ws.per_ip_limit_reached", true))
		span.SetStatus(codes.Error, "per-IP max connections reached")
		return fmt.Errorf("ws upgrade max connections per IP reached: ip=%s max=%d", clientIP, maxConnPerIP)
	}
	return nil
}

// ipConnCountDecrement 客户端关闭时递减单 IP 连接计数，计数归零时删除记录。
func ipConnCountDecrement(clientIP string) {
	if actual, ok := ipConnCounts.Load(clientIP); ok {
		counter, ok := actual.(*atomic.Int32)
		if !ok {
			return
		}
		if counter.Add(-1) <= 0 {
			ipConnCounts.Delete(clientIP)
		}
	}
}

// upgradeRegisterDispatcher 注册客户端到 Dispatcher 并设置关闭清理钩子。
// 调用方需确保 o.enableDistributed && o.dispatcher != nil。
func upgradeRegisterDispatcher(ctx context.Context, client *Client, o *upgradeOptions) error {
	// 传播单点登录配置到 Dispatcher
	if o.enableSSO {
		o.dispatcher.enableSSO = true
	}
	if err := o.dispatcher.RegisterCtx(ctx, client); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "ws client close after register failed",
				logger.Err(closeErr),
			)
		}
		return err
	}
	// 先保存之前的钩子再设置新钩子，避免闭包中引用 client.closeHook 自身导致递归
	prevCloseHook := client.closeHook
	client.SetCloseHook(func() {
		if prevCloseHook != nil {
			prevCloseHook()
		}
		if err := o.dispatcher.UnregisterCtx(client.clientCtx, client); err != nil {
			logger.WarnWithCtx(client.clientCtx, "ws dispatcher unregister failed",
				logger.String("uid", client.uid),
				logger.Err(err),
			)
		}
	})
	return nil
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
//  5. 用 *websocket.Conn 创建 *Client（含 msgFromChToWs）
//  6. 可选启动心跳、注册 Dispatcher
//
// 安全保护:
//   - CORS: 默认拒绝所有来源，需显式调用 WithCheckOrigin
//   - 限流: WithWsRateLimit 设置全局升级速率
//   - 单IP限制: WithMaxConnPerIP 设置单IP最大连接数
//
// 调用方需负责 defer client.Close() 确保资源释放。
func Upgrade(c *gin.Context, opts ...UpgradeOption) (*Client, error) {
	tracer := otel.Tracer("gows")
	ctx, span := tracer.Start(c.Request.Context(), "ws.upgrade", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	requestID := requestIDAttr(c.Request.Context())

	o := defaultUpgradeOptions()
	o.apply(opts...)

	clientIP := c.ClientIP()

	span.SetAttributes(
		attribute.String("ws.client_uid", o.clientUID),
		attribute.String("ws.remote_addr", c.Request.RemoteAddr),
		attribute.String("ws.client_ip", clientIP),
		requestID,
	)

	// CORS 安全检查：默认拒绝所有来源，需显式调用 WithCheckOrigin
	if !o.checkOriginSet {
		span.SetAttributes(attribute.Bool("ws.cors_rejected", true))
		span.SetStatus(codes.Error, "CORS check rejected")
		return nil, fmt.Errorf("ws upgrade: CORS check rejected, use gows.WithCheckOrigin() to allow origins")
	}
	if !o.checkOrigin(c.Request) {
		span.SetAttributes(attribute.Bool("ws.cors_rejected", true))
		span.SetStatus(codes.Error, "CORS check rejected")
		return nil, fmt.Errorf("ws upgrade: CORS check rejected by CheckOrigin function")
	}
	if o.enableWsRateLimit {
		// 全局速率限制检查
		if limiter := wsLimiter.Load(); limiter != nil {
			if !limiter.Allow() {
				span.SetAttributes(attribute.Bool("ws.rate_limited", true))
				span.SetStatus(codes.Error, "rate limited")
				return nil, fmt.Errorf("ws upgrade rate limited: %.2f rps", limiter.Limit())
			}
		}
	}

	// 单 IP 连接数检查
	if o.maxConnPerIP > 0 {
		if err := upgradePerIPCheck(clientIP, o.maxConnPerIP, span); err != nil {
			return nil, err
		}
	}

	upgrader := websocket.Upgrader{
		CheckOrigin:       o.checkOrigin,
		ReadBufferSize:    o.readBufSize,
		WriteBufferSize:   o.writeBufSize,
		Subprotocols:      o.subprotocols,
		EnableCompression: o.enableCompression,
	}

	wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		if o.errorHandler != nil {
			o.errorHandler(c, err)
		}
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ws upgrade: %w", err)
	}

	// 升级成功后，创建/确保计数器存在并递增
	if o.maxConnPerIP > 0 {
		actual, _ := ipConnCounts.LoadOrStore(clientIP, &atomic.Int32{})
		if counter, ok := actual.(*atomic.Int32); ok {
			counter.Add(1)
		}
	}

	// 将 UpgradeOption 中的 Client 配置通过 clientConfig 直接传入 NewClient
	// 使用 WithoutCancel 阻断上游 Gin 超时传递，Close 时手动取消
	client := newClientWithConfig(context.WithoutCancel(ctx), wsConn, o.clientUID, &o.clientConfig)

	// 注册 IP 连接清理钩子
	if o.maxConnPerIP > 0 {
		prevHook := client.closeHook
		client.SetCloseHook(func() {
			if prevHook != nil {
				prevHook()
			}
			ipConnCountDecrement(clientIP)
		})
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
	if o.enableDistributed && o.dispatcher != nil {
		if err := upgradeRegisterDispatcher(ctx, client, o); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("ws dispatcher register: %w", err)
		}
	}

	span.SetStatus(codes.Ok, "upgrade success")
	return client, nil
}
