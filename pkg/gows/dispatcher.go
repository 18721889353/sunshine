package gows

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/panjf2000/ants/v2"
)

// DispatcherOption / dispatcherOptions / WithMaxConnections / WithWorkerPool / ErrMaxConnections 定义在 dispatcher_options.go

// DistributedDispatcher 消息分发中心。
//
// 负责管理所有在线 WebSocket 客户端连接，支持跨实例消息转发。
// 所有公开方法均为 goroutine 安全，适用于高并发业务场景。
//
// # 分布式架构
//
//	实例A (alice)          Backend (RabbitMQ)         实例B (bob)
//	  │                                                     │
//	  ├─ SendToUIDCtx("bob")                                │
//	  │   └─ Publish ─────────────────────────────────────►├─ receiveLoop
//	  │                                                      └─ client.WriteJSON ✓
//
// # 工作原理
//   - Send 系列方法将消息序列化为 JSON → 发布到 Backend
//   - receiveLoop 协程接收所有实例的消息 → 通过 WorkerPool 并发投递到本地连接
//   - 发送方也通过 Backend 接收自己发布的消息（统一处理路径）
//   - 通过 InstanceID 跳过自发布消息，避免回环干扰
//
// # 背压保护
//   - receiveLoop 使用 WorkerPool 异步处理消息，避免单条慢消息阻塞后续投递
//   - WorkerPool 满时阻塞发送，天然反压到消息源
//
// # 网络波动保护
//   - Broadcast 系列方法自动清理已关闭的僵尸连接
//   - 连接管理使用 sync.Map，无锁化高并发安全
type DistributedDispatcher struct {
	clients        sync.Map       // *Client → struct{} 本地在线客户端集合
	maxClientNum   atomic.Int32   // 最大客户端数（0=不限制）
	backend        Backend        // 分布式后端（如 RabbitMQ Direct，nil=单机模式）
	instanceID     string         // 本实例唯一标识，用于跳过自发布消息回环
	receiveStopCh  chan struct{}  // 关闭时通知 receiveLoop 退出
	receiveWg      sync.WaitGroup // 等待 receiveLoop goroutine 退出
	receiveStarted atomic.Bool    // 是否已启动，确保 Start 幂等

	// uidCancelChs 按 UID 的订阅取消信号通道
	// uid → chan struct{}，用于通知 subscribeUID 启动的 goroutine 退出
	uidCancelChs sync.Map

	// 分布式客户端注册表：跨实例同步的在线 UID 集合
	// uid → count（同一 UID 多设备连接）
	remoteUIDCounts sync.Map
	// 远端 UID 计数：避免 Len()/Stats() O(n) 遍历 remoteUIDTotal
	remoteUIDTotal atomic.Int32

	localClientTotal atomic.Int32 // clients 中的客户端数量，优化 Len() 性能
	// 本地 UID 计数：uid → 连接数，避免 hasLocalUID O(n) 扫描
	localUIDCounts sync.Map // uid → *atomic.Int32 本地 UID 连接计数

	// 单点登录：同一 UID 仅保留最新连接（后登录踢前登录）
	enableSSO  atomic.Bool // 是否启用单点登录
	ssoClients sync.Map    // uid → *Client 本地 SSO 映射

	// workerPool 消息处理协程池（背压保护），用于 dispatchLoop 异步投递
	workerPool *ants.Pool

	// deliverPool 本地并发投递协程池，限制 deliverBroadcast / WriteRawToLocalUIDs 的 goroutine 数
	deliverPool *ants.Pool
}

// NewDispatcher 创建并初始化一个新的消息分发中心。
// 参数:
//   - backend: 分布式后端，传 nil 表示单机模式（仅供本地消息投递）
//   - opts: 可选配置（如 WithMaxConnections、WithWorkerPool）
//
// 示例（单机）:
//
//	d := gows.NewDispatcher(nil)
//	d.RegisterCtx(ctx, client)
//
// 示例（分布式）:
//
//	backend := gows.NewRabbitMQBackend("amqp://...", "ws:messages")
//	d := gows.NewDispatcher(backend, gows.WithMaxConnections(10000))
//	d.Start(ctx)
func NewDispatcher(backend Backend, opts ...DispatcherOption) *DistributedDispatcher {
	o := defaultDispatcherOptions()
	o.apply(opts...)

	d := &DistributedDispatcher{
		backend:       backend,
		receiveStopCh: make(chan struct{}),
	}
	// backend 为 nil（单机模式）时使用默认实例 ID
	if backend != nil {
		d.instanceID = fmt.Sprintf("%p", backend)
	} else {
		d.instanceID = "standalone"
	}
	d.maxClientNum.Store(o.maxClientNum)
	// workerNum > 1 时创建 ants 协程池作为 WorkerPool
	if o.workerNum > 1 {
		pool, err := ants.NewPool(int(o.workerNum))
		if err != nil {
			logger.WarnWithCtx(context.Background(), "create worker pool failed", logger.Err(err))
		} else {
			d.workerPool = pool
			logger.InfoWithCtx(context.Background(), "worker pool created",
				logger.Int32("workers", o.workerNum),
			)
		}
	}
	// 本地并发投递协程池，4096 个 worker，覆盖 1000 并发 WebSocket 写入
	pool, err := ants.NewPool(4096)
	if err != nil {
		logger.WarnWithCtx(context.Background(), "create deliver pool failed", logger.Err(err))
	}
	d.deliverPool = pool
	return d
}

// DefaultDispatcher 默认全局 Dispatcher 单例（单机模式）。
var DefaultDispatcher = NewDispatcher(nil)

// Start 启动后台接收协程和 WorkerPool，开始监听其他实例的消息。
// 仅分布式模式下调用（backend != nil），单机模式无需调用。
// ctx 用于链路追踪。
// Start 是幂等的，多次调用安全。
func (dd *DistributedDispatcher) Start(ctx context.Context) {
	if dd.backend == nil {
		return // 单机模式无需启动
	}
	if !dd.receiveStarted.CompareAndSwap(false, true) {
		return
	}

	// 隔离上游 ctx 的取消信号（如 Gin 请求结束），保留 tracing 等 value 数据
	lifecycleCtx := context.WithoutCancel(ctx)

	// 1. 先连接后端，获取广播消息通道
	msgCh, err := dd.backend.SubscribeBroadcast(lifecycleCtx)
	if err != nil {
		logger.WarnWithCtx(ctx, "dispatcher start receive failed", logger.Err(err))
		dd.receiveStarted.Store(false)
		return
	}

	// 2. 启动 dispatchLoop（WorkerPool 已在 NewDispatcher 中创建）
	dd.receiveWg.Add(1)
	go dd.dispatchLoop(lifecycleCtx, msgCh)
	logger.InfoWithCtx(ctx, "distributed dispatcher started")
}

// Stop 停止后台接收协程，释放后端资源。
// 关闭顺序: 关闭 UID 订阅 → 关闭 Backend → 释放 WorkerPool → 关闭 receiveLoop。
func (dd *DistributedDispatcher) Stop() {
	// 1. 关闭所有 UID 订阅
	dd.uidCancelChs.Range(func(key, value any) bool {
		if ch, ok := value.(chan struct{}); ok {
			close(ch)
		}
		dd.uidCancelChs.Delete(key)
		return true
	})

	// 2. 关闭后端，停止接收新消息
	if dd.backend != nil {
		if err := dd.backend.Close(); err != nil {
			logger.WarnWithCtx(context.Background(), "dispatcher backend close failed",
				logger.Err(err),
			)
		}
	}

	// 3. 释放 WorkerPool（unblock dispatchLoop 若阻塞在 Submit）
	if dd.workerPool != nil {
		dd.workerPool.Release()
	}

	// 4. 关闭 receiveLoop
	close(dd.receiveStopCh)
	dd.receiveWg.Wait()

	// 5. 释放本地投递协程池
	if dd.deliverPool != nil {
		dd.deliverPool.Release()
	}
}

// Len 返回全局在线连接数（本地 + 远端）。
func (dd *DistributedDispatcher) Len() int {
	return int(dd.localClientTotal.Load()) + int(dd.remoteUIDTotal.Load())
}

// MaxConnections 返回最大连接数限制（0=不限制）。
func (dd *DistributedDispatcher) MaxConnections() int {
	return int(dd.maxClientNum.Load())
}

// Clients 返回当前所有已注册客户端的快照切片。
func (dd *DistributedDispatcher) Clients() []*Client {
	var clients []*Client
	dd.clients.Range(func(key, _ any) bool {
		if c, ok := key.(*Client); ok {
			clients = append(clients, c)
		}
		return true
	})
	return clients
}

// Range 遍历所有已注册客户端。
// 如果 fn 返回 false 则停止遍历。
// 注意：fn 中调用 Delete 等修改操作是安全的，但可能导致遍历结果不一致。
// 如需一致性快照，使用 Clients() 替代。
func (dd *DistributedDispatcher) Range(fn func(*Client) bool) {
	dd.clients.Range(func(key, _ any) bool {
		c, ok := key.(*Client)
		if !ok {
			return true
		}
		return fn(c)
	})
}

// DispatcherStats 分发中心统计信息。
type DispatcherStats struct {
	TotalConnections int // 当前在线连接总数（本地 + 远端）
	LocalConnections int // 本地在线连接数
	RemoteUIDs       int // 远端实例上的唯一 UID 数
	MaxConnections   int // 最大连接数限制（0=不限制）
}

// Stats 返回分发中心的实时统计信息。
func (dd *DistributedDispatcher) Stats() DispatcherStats {
	local := dd.LenLocal()
	remote := int(dd.remoteUIDTotal.Load())

	return DispatcherStats{
		TotalConnections: local + remote,
		LocalConnections: local,
		RemoteUIDs:       remote,
		MaxConnections:   dd.MaxConnections(),
	}
}

// LenLocal 返回本地在线连接数。
func (dd *DistributedDispatcher) LenLocal() int {
	return int(dd.localClientTotal.Load())
}

// ConnectedUIDs 返回所有实例上的在线 UID 列表（去重）。
func (dd *DistributedDispatcher) ConnectedUIDs() []string {
	// 预分配 map 容量（本地连接数 + 远端 UID 数），减少 reallocation
	seen := make(map[string]struct{}, dd.LenLocal()+int(dd.remoteUIDTotal.Load()))

	// 1. 本地 UID
	dd.clients.Range(func(key, _ any) bool {
		client, ok := key.(*Client)
		if !ok {
			return true
		}
		if client.uid != "" {
			seen[client.uid] = struct{}{}
		}
		return true
	})

	// 2. 远端 UID
	dd.remoteUIDCounts.Range(func(key, _ any) bool {
		uid, ok := key.(string)
		if !ok {
			return true
		}
		if uid != "" {
			seen[uid] = struct{}{}
		}
		return true
	})

	uids := make([]string, 0, len(seen))
	for uid := range seen {
		uids = append(uids, uid)
	}
	return uids
}

// forEachAliveClient 遍历所有存活客户端并执行 fn，遍历结束后自动清理僵尸连接。
// 返回 (totalCount, deadCleaned) 即遍历总数和清理的僵尸数。
func (dd *DistributedDispatcher) forEachAliveClient(fn func(c *Client)) (totalCount int, deadCleaned int) {
	var deadClients []*Client
	dd.clients.Range(func(key, _ any) bool {
		c, ok := key.(*Client)
		if !ok {
			return true
		}
		totalCount++
		if !c.IsAlive() {
			deadClients = append(deadClients, c)
			return true
		}
		fn(c)
		return true
	})
	if len(deadClients) > 0 {
		dd.deleteDeadClients(deadClients)
	}
	return totalCount, len(deadClients)
}

// CleanupDeadConns 清理所有已关闭的僵尸连接，释放 Dispatcher 内存。
// 同时递减 localUIDCounts 引用计数，确保 hasLocalUID 查询不漂移。
// 返回清理的连接数。
func (dd *DistributedDispatcher) CleanupDeadConns() int {
	var cleaned int
	dd.clients.Range(func(key, _ any) bool {
		client, ok := key.(*Client)
		if !ok {
			return true
		}
		if !client.IsAlive() {
			if _, loaded := dd.clients.LoadAndDelete(key); loaded {
				cleaned++
				dd.localClientTotal.Add(-1)
				if client.uid != "" {
					dd.decrementLocalUIDCount(client.uid)
				}
			}
		}
		return true
	})
	return cleaned
}

// DisconnectByUID 断开指定 UID 的客户端连接。
// 如果该 UID 有多个连接（同账号多设备），全部断开。
// 返回:
//   - error: 未找到该 UID 时返回 ErrClientNotFound
func (dd *DistributedDispatcher) DisconnectByUID(ctx context.Context, uid string) error {
	var found bool
	dd.clients.Range(func(key, _ any) bool {
		c, ok := key.(*Client)
		if !ok || c.uid != uid {
			return true
		}
		if err := c.Close(); err != nil {
			logger.WarnWithCtx(ctx, "close client failed during DisconnectByUID",
				logger.Err(err),
				logger.String("uid", c.uid),
			)
		}
		found = true
		return true // 继续遍历，断开该 UID 所有连接
	})
	if !found {
		return ErrClientNotFound
	}
	return nil
}

// DisconnectByUIDs 断开多个指定 UID 的客户端连接。
// 参数:
//   - uids: 要断开的 UID 列表
//
// 返回:
//   - int: 实际断开的连接数
func (dd *DistributedDispatcher) DisconnectByUIDs(ctx context.Context, uids ...string) int {
	if len(uids) == 0 {
		return 0
	}
	uidSet := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uidSet[uid] = struct{}{}
	}
	var count int
	dd.clients.Range(func(key, _ any) bool {
		c, ok := key.(*Client)
		if !ok {
			return true
		}
		if _, hit := uidSet[c.uid]; hit {
			if err := c.Close(); err != nil {
				logger.WarnWithCtx(ctx, "close client failed during DisconnectByUIDs",
					logger.Err(err),
					logger.String("uid", c.uid),
				)
			}
			count++
		}
		return true
	})
	return count
}
