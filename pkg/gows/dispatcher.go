package gows

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// Dispatcher 全局消息分发中心，负责管理所有在线 WebSocket 客户端连接。
// 所有公开方法均为 goroutine 安全，适用于高并发业务场景。
// 网络波动保护：Broadcast 系列方法自动清理已关闭的僵尸连接。
type Dispatcher struct {
	mu            sync.RWMutex
	clients       map[*Client]struct{} // 全局在线客户端集合
	maxConns      int32                // 最大连接数（0=不限制）
	totalRejected int32                // 原子计数: 因达到上限被拒绝的连接数
}

// DispatcherOption 分发中心配置选项函数类型
type DispatcherOption func(*Dispatcher)

// WithMaxConnections 设置 Dispatcher 最大连接数限制。
// 参数:
//   - max: 最大连接数。达到上限时 Register 返回 ErrMaxConnections。
//     0 表示不限制（默认）。
//
// 大厂标准：生产环境必须设置合理上限防止内存泄漏。
func WithMaxConnections(n int) DispatcherOption {
	return func(d *Dispatcher) {
		if n > 0 {
			atomic.StoreInt32(&d.maxConns, int32(n))
		}
	}
}

// ErrMaxConnections 达到最大连接数限制的错误
var ErrMaxConnections = fmt.Errorf("dispatcher: max connections reached")

// NewDispatcher 创建并初始化一个新的消息分发中心。
func NewDispatcher(opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{
		clients: make(map[*Client]struct{}),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// DefaultDispatcher 默认全局 Dispatcher 单例。
var DefaultDispatcher = NewDispatcher()

// Register 将客户端连接注册到 Dispatcher 全局列表。
// 如果达到最大连接数上限，返回 ErrMaxConnections。
// 调用方应检查此错误并执行 client.Close() 释放连接资源。
func (d *Dispatcher) Register(client *Client) error {
	maxConns := atomic.LoadInt32(&d.maxConns)
	if maxConns > 0 {
		d.mu.Lock()
		if int32(len(d.clients)) >= maxConns {
			atomic.AddInt32(&d.totalRejected, 1)
			d.mu.Unlock()
			logger.WarnWithCtx(client.ctx, "ws dispatcher max connections reached, rejecting",
				logger.String("uid", client.uid),
				logger.String("remote_addr", client.remoteAddr),
				logger.Int32("max", maxConns),
			)
			return ErrMaxConnections
		}
		d.clients[client] = struct{}{}
		d.mu.Unlock()
		return nil
	}

	d.mu.Lock()
	d.clients[client] = struct{}{}
	d.mu.Unlock()
	return nil
}

// Unregister 从 Dispatcher 全局列表中移除客户端连接。
func (d *Dispatcher) Unregister(client *Client) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.clients, client)
}

// BroadcastCtx 向所有已注册客户端广播消息（带 context 的版本）。
// ctx 用于链路追踪传播，其中应包含 request_id。
// 逐个发送，某个客户端写入失败不影响其他客户端。
// 自动清理已关闭的僵尸连接，防止内存泄漏。
func (d *Dispatcher) BroadcastCtx(ctx context.Context, v any) {
	// 链路追踪
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.broadcast", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(requestIDAttr(ctx))

	d.mu.RLock()
	clients := make([]*Client, 0, len(d.clients))
	for client := range d.clients {
		clients = append(clients, client)
	}
	d.mu.RUnlock()

	span.SetAttributes(attribute.Int("ws.broadcast_targets", len(clients)))

	var deadClients []*Client
	for _, client := range clients {
		if !client.IsAlive() {
			deadClients = append(deadClients, client)
			continue
		}
		if err := client.WriteJSON(v); err != nil {
			logger.WarnWithCtx(ctx, "ws broadcast write failed",
				logger.String("uid", client.uid),
				logger.Err(err),
			)
		}
	}

	// 批量清理已关闭的僵尸连接
	if len(deadClients) > 0 {
		span.SetAttributes(attribute.Int("ws.dead_clients_cleaned", len(deadClients)))
		d.mu.Lock()
		for _, client := range deadClients {
			delete(d.clients, client)
		}
		d.mu.Unlock()
	}

	span.SetStatus(codes.Ok, "broadcast completed")
}

// BroadcastFilterCtx 向满足 filter 条件的客户端广播消息（带 context 的版本）。
// ctx 用于链路追踪传播，其中应包含 request_id。
// filter 返回 true 表示该客户端需要接收消息。
// 自动清理已关闭的僵尸连接。
func (d *Dispatcher) BroadcastFilterCtx(ctx context.Context, v any, filter func(*Client) bool) {
	// 链路追踪
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.broadcast_filter", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(requestIDAttr(ctx))

	d.mu.RLock()
	clients := make([]*Client, 0, len(d.clients))
	for client := range d.clients {
		clients = append(clients, client)
	}
	d.mu.RUnlock()

	span.SetAttributes(attribute.Int("ws.broadcast_targets", len(clients)))

	var deadClients []*Client
	for _, client := range clients {
		if !client.IsAlive() {
			deadClients = append(deadClients, client)
			continue
		}
		if filter(client) {
			if err := client.WriteJSON(v); err != nil {
				logger.WarnWithCtx(ctx, "ws broadcast_filter write failed",
					logger.String("uid", client.uid),
					logger.Err(err),
				)
			}
		}
	}

	if len(deadClients) > 0 {
		span.SetAttributes(attribute.Int("ws.dead_clients_cleaned", len(deadClients)))
		d.mu.Lock()
		for _, client := range deadClients {
			delete(d.clients, client)
		}
		d.mu.Unlock()
	}

	span.SetStatus(codes.Ok, "broadcast_filter completed")
}

// SendToUIDCtx 向指定 UID 的客户端发送消息（带 context 的版本）。
// ctx 用于链路追踪传播，其中应包含 request_id。
// 如果该 UID 有多个连接，则全部发送。
// 自动清理已关闭的僵尸连接。
func (d *Dispatcher) SendToUIDCtx(ctx context.Context, uid string, v any) {
	// 链路追踪
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.send_to_uid", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.target_uid", uid),
		requestIDAttr(ctx),
	)

	d.mu.RLock()
	clients := make([]*Client, 0, len(d.clients))
	for client := range d.clients {
		clients = append(clients, client)
	}
	d.mu.RUnlock()

	var sentCount int
	var deadClients []*Client
	for _, client := range clients {
		if !client.IsAlive() {
			deadClients = append(deadClients, client)
			continue
		}
		if client.UID() == uid {
			if err := client.WriteJSON(v); err != nil {
				logger.WarnWithCtx(ctx, "ws send_to_uid write failed",
					logger.String("uid", client.uid),
					logger.Err(err),
				)
			}
			sentCount++
		}
	}

	span.SetAttributes(attribute.Int("ws.sent_count", sentCount))

	if len(deadClients) > 0 {
		span.SetAttributes(attribute.Int("ws.dead_clients_cleaned", len(deadClients)))
		d.mu.Lock()
		for _, client := range deadClients {
			delete(d.clients, client)
		}
		d.mu.Unlock()
	}

	if sentCount > 0 {
		span.SetStatus(codes.Ok, "message sent")
	} else {
		span.SetStatus(codes.Ok, "no matching client")
	}
}

// Range 遍历所有已注册客户端。
// 如果 fn 返回 false 则停止遍历。
func (d *Dispatcher) Range(fn func(*Client) bool) {
	d.mu.RLock()
	clients := make([]*Client, 0, len(d.clients))
	for client := range d.clients {
		clients = append(clients, client)
	}
	d.mu.RUnlock()

	for _, client := range clients {
		if !fn(client) {
			break
		}
	}
}

// Len 返回当前在线连接数。
func (d *Dispatcher) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.clients)
}

// MaxConnections 返回最大连接数限制（0=不限制）。
func (d *Dispatcher) MaxConnections() int {
	return int(atomic.LoadInt32(&d.maxConns))
}

// TotalRejected 返回因达到连接上限被拒绝的累计连接数。
func (d *Dispatcher) TotalRejected() int {
	return int(atomic.LoadInt32(&d.totalRejected))
}

// Clients 返回当前所有已注册客户端的快照切片。
// 调用方可遍历此切片进行批量操作，无需持有锁。
func (d *Dispatcher) Clients() []*Client {
	d.mu.RLock()
	defer d.mu.RUnlock()
	clients := make([]*Client, 0, len(d.clients))
	for client := range d.clients {
		clients = append(clients, client)
	}
	return clients
}

// DispatcherStats 分发中心统计信息。
type DispatcherStats struct {
	TotalConnections int // 当前在线连接总数
	MaxConnections   int // 最大连接数限制（0=不限制）
	TotalRejected    int // 因达到上限被拒绝的累计连接数
}

// Stats 返回分发中心的实时统计信息。
func (d *Dispatcher) Stats() DispatcherStats {
	return DispatcherStats{
		TotalConnections: d.Len(),
		MaxConnections:   d.MaxConnections(),
		TotalRejected:    d.TotalRejected(),
	}
}

// CleanupDeadConns 清理所有已关闭的僵尸连接，释放 Dispatcher 内存。
// 在网络波动后批量清理死连接非常有用。
// 返回清理的连接数。
func (d *Dispatcher) CleanupDeadConns() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	var cleaned int
	for client := range d.clients {
		if !client.IsAlive() {
			delete(d.clients, client)
			cleaned++
		}
	}
	return cleaned
}
