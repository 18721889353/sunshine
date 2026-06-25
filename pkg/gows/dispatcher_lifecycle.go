package gows

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/18721889353/sunshine/pkg/logger"
)

// RegisterCtx 将客户端连接注册到 Dispatcher 全局列表（带自定义上下文）。
// 如果达到最大连接数上限，返回 ErrMaxConnections。
// 分布式模式下自动向所有实例广播上线通知，ctx 中的 tracing 信息会随消息传播。
// 调用方应检查此错误并执行 client.Close() 释放连接资源。
//
// 注意:
//   - 上线通知在锁内发布，确保其他实例收到通知时本实例的 clients 已可见
//   - 若 ctx 已过期，返回 ctx.Err() 并回滚 map 修改
func (dd *DistributedDispatcher) RegisterCtx(ctx context.Context, client *Client) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 尝试获取锁前先检查 ctx 是否已取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxConns := atomic.LoadInt32(&dd.maxConns)
	if maxConns > 0 {
		dd.mu.Lock()
		if int32(len(dd.clients)) >= maxConns {
			atomic.AddInt32(&dd.totalRejected, 1)
			dd.mu.Unlock()
			logger.WarnWithCtx(client.ctx, "ws dispatcher max connections reached, rejecting",
				logger.String("uid", client.uid),
				logger.String("remote_addr", client.remoteAddr),
				logger.Int32("max", maxConns),
			)
			return ErrMaxConnections
		}
		dd.clients[client] = struct{}{}
		// 更新 map 后再次检查 ctx，过期则回滚
		select {
		case <-ctx.Done():
			delete(dd.clients, client)
			dd.mu.Unlock()
			return ctx.Err()
		default:
		}
		dd.publishClientEvent(ctx, "client_online", client.uid)
		dd.mu.Unlock()
		return nil
	}

	dd.mu.Lock()
	dd.clients[client] = struct{}{}
	// 更新 map 后检查 ctx，过期则回滚
	select {
	case <-ctx.Done():
		delete(dd.clients, client)
		dd.mu.Unlock()
		return ctx.Err()
	default:
	}
	dd.publishClientEvent(ctx, "client_online", client.uid)
	dd.mu.Unlock()

	return nil
}

// Register 将客户端连接注册到 Dispatcher 全局列表，使用 context.Background()。
// 如需自定义 tracing 上下文，请使用 RegisterCtx。
func (dd *DistributedDispatcher) Register(client *Client) error {
	return dd.RegisterCtx(context.Background(), client)
}

// UnregisterCtx 从 Dispatcher 全局列表中移除客户端连接（带自定义上下文）。
// 分布式模式下自动向所有实例广播下线通知，ctx 中的 tracing 信息会随消息传播。
// 若 ctx 已过期，回滚 map 删除并返回 ctx.Err()。
func (dd *DistributedDispatcher) UnregisterCtx(ctx context.Context, client *Client) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 尝试获取锁前先检查 ctx 是否已取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	dd.mu.Lock()
	delete(dd.clients, client)
	// 删除后再次检查 ctx，过期则回滚
	select {
	case <-ctx.Done():
		dd.clients[client] = struct{}{}
		dd.mu.Unlock()
		return ctx.Err()
	default:
	}
	dd.publishClientEvent(ctx, "client_offline", client.uid)
	dd.mu.Unlock()
	return nil
}

// Unregister 从 Dispatcher 全局列表中移除客户端连接，使用 context.Background()。
// 如需自定义 tracing 上下文，请使用 UnregisterCtx。
func (dd *DistributedDispatcher) Unregister(client *Client) {
	if err := dd.UnregisterCtx(context.Background(), client); err != nil {
		logger.WarnWithCtx(context.Background(), "dispatcher unregister failed",
			logger.String("uid", client.uid),
			logger.Err(err),
		)
	}
}

// publishClientEvent 向 Backend 发布客户端上线/下线事件。
// 自动填充 InstanceID 用于自发布消息跳过。
func (dd *DistributedDispatcher) publishClientEvent(ctx context.Context, eventType, uid string) {
	if dd.backend == nil {
		return
	}
	data, err := json.Marshal(map[string]string{"uid": uid})
	if err != nil {
		logger.WarnWithCtx(ctx, "dispatcher publish event marshal failed",
			logger.String("event", eventType),
			logger.String("uid", uid),
			logger.Err(err),
		)
		return
	}
	if err := dd.backend.Publish(ctx, &PubSubMessage{
		InstanceID: dd.instanceID,
		Type:       eventType,
		UIDs:       []string{uid},
		Payload:    data,
	}); err != nil {
		logger.WarnWithCtx(ctx, "dispatcher publish event failed",
			logger.String("event", eventType),
			logger.Err(err),
		)
	}
}
