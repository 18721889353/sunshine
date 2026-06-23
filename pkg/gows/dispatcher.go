package gows

import (
	"sync"
)

// Dispatcher 全局消息分发中心，负责管理所有在线 WebSocket 客户端连接。
// 所有公开方法均为 goroutine 安全，适用于高并发业务场景。
type Dispatcher struct {
	mu      sync.RWMutex
	clients map[*Client]struct{} // 全局在线客户端集合
}

// NewDispatcher 创建并初始化一个新的消息分发中心。
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		clients: make(map[*Client]struct{}),
	}
}

// DefaultDispatcher 默认全局 Dispatcher 单例。
var DefaultDispatcher = NewDispatcher()

// Register 将客户端连接注册到 Dispatcher 全局列表。
func (d *Dispatcher) Register(client *Client) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.clients[client] = struct{}{}
}

// Unregister 从 Dispatcher 全局列表中移除客户端连接。
func (d *Dispatcher) Unregister(client *Client) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.clients, client)
}

// Broadcast 向所有已注册客户端广播消息。
// 逐个发送，某个客户端写入失败不影响其他客户端。
func (d *Dispatcher) Broadcast(v any) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for client := range d.clients {
		_ = client.WriteJSON(v)
	}
}

// BroadcastFilter 向满足 filter 条件的客户端广播消息。
// filter 返回 true 表示该客户端需要接收消息。
func (d *Dispatcher) BroadcastFilter(v any, filter func(*Client) bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for client := range d.clients {
		if filter(client) {
			_ = client.WriteJSON(v)
		}
	}
}

// SendToUID 向指定 UID 的客户端发送消息。
// 如果该 UID 有多个连接，则全部发送。
func (d *Dispatcher) SendToUID(uid string, v any) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for client := range d.clients {
		if client.UID() == uid {
			_ = client.WriteJSON(v)
		}
	}
}

// Range 遍历所有已注册客户端。
// 如果 fn 返回 false 则停止遍历。
func (d *Dispatcher) Range(fn func(*Client) bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for client := range d.clients {
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
}

// Stats 返回分发中心的实时统计信息。
func (d *Dispatcher) Stats() DispatcherStats {
	return DispatcherStats{
		TotalConnections: d.Len(),
	}
}
