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
func (c *Client) SendToUIDCtx(ctx context.Context, uid string, v any) {
	if c.dispatcher == nil {
		return
	}
	if uid == c.uid {
		return // 不发送给自己
	}
	c.dispatcher.SendToUIDCtx(ctx, uid, v)
}

// SendToMultiUIDCtx 通过关联的 Dispatcher 向多个 UID 的用户发送消息。
// 发送者自身不会收到消息（自动过滤）。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) SendToMultiUIDCtx(ctx context.Context, uids []string, v any) {
	if c.dispatcher == nil {
		return
	}
	// 过滤掉发送者自己
	filtered := make([]string, 0, len(uids))
	for _, uid := range uids {
		if uid != c.uid {
			filtered = append(filtered, uid)
		}
	}
	if len(filtered) == 0 {
		return
	}
	c.dispatcher.SendToMultiUIDCtx(ctx, filtered, v)
}

// BroadcastCtx 通过关联的 Dispatcher 向所有在线客户端广播消息。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) BroadcastCtx(ctx context.Context, v any) {
	if c.dispatcher == nil {
		return
	}
	c.dispatcher.BroadcastCtx(ctx, v)
}

// BroadcastReliableCtx 通过关联的 Dispatcher 进行可靠广播（按 UID 下发）。
// 仅在 Client 已注册到 Dispatcher 时有效。
func (c *Client) BroadcastReliableCtx(ctx context.Context, v any) {
	if c.dispatcher == nil {
		return
	}
	c.dispatcher.BroadcastReliableCtx(ctx, v)
}
