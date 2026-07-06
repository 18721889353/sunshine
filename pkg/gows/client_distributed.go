package gows

import "context"

// ---------------------------------------------------------------------------
// Dispatcher 代理方法
// 这些方法将消息发送委托给关联的 DistributedDispatcher，
// 实现跨节点的消息路由，与 client_local.go 中的本地 I/O 操作职责分离。
// ---------------------------------------------------------------------------

// SendToUIDCtx 通过关联的 Dispatcher 向指定 UID 的用户发送消息。
// 发送者自身不会收到消息（自动过滤）。
// 仅在 Client 已注册到 Dispatcher 时有效（通过 Upgrade 传入 WithDispatcher）。
func (c *Client) SendToUIDCtx(ctx context.Context, uid string, v any) error {
	if c.dispatcher == nil {
		return ErrNoDispatcher
	}
	return c.dispatcher.SendToUIDCtx(ctx, uid, v)
}

// SendToMultiUIDCtx 通过关联的 Dispatcher 向多个 UID 的用户发送消息。
// 发送者自身不会收到消息（自动过滤）。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) SendToMultiUIDCtx(ctx context.Context, uids []string, v any) error {
	if c.dispatcher == nil {
		return ErrNoDispatcher
	}
	// 发送给自己由 dispatcher 的 InstanceID 机制自动过滤 MQ 回环，本地重复投递由 WriteRawToLocalUIDs 通过 uidSet 去重。
	return c.dispatcher.SendToMultiUIDCtx(ctx, uids, v)
}

// BroadcastCtx 通过关联的 Dispatcher 向所有在线客户端广播消息。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) BroadcastCtx(ctx context.Context, v any) error {
	if c.dispatcher == nil {
		return ErrNoDispatcher
	}
	return c.dispatcher.BroadcastCtx(ctx, v)
}

// BroadcastReliableCtx 通过关联的 Dispatcher 进行可靠广播（按 UID 下发）。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) BroadcastReliableCtx(ctx context.Context, v any) error {
	if c.dispatcher == nil {
		return ErrNoDispatcher
	}
	return c.dispatcher.BroadcastReliableCtx(ctx, v)
}

// DisconnectByUID 通过关联的 Dispatcher 断开指定 UID 的连接。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) DisconnectByUID(ctx context.Context, uid string) error {
	if c.dispatcher == nil {
		return ErrNoDispatcher
	}
	return c.dispatcher.DisconnectByUID(uid)
}

// DisconnectByUIDs 通过关联的 Dispatcher 断开多个 UID 的连接。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) DisconnectByUIDs(ctx context.Context, uids ...string) int {
	if c.dispatcher == nil {
		return 0
	}
	return c.dispatcher.DisconnectByUIDs(uids...)
}
