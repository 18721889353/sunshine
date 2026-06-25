package gows

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/18721889353/sunshine/pkg/logger"
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
//   - WorkerPool 满时降级为串行处理（反压），防止内存溢出
//
// # 网络波动保护
//   - Broadcast 系列方法自动清理已关闭的僵尸连接
//   - 连接管理使用 RWMutex，读多写少场景高性能
type DistributedDispatcher struct {
	mu            sync.RWMutex
	clients       map[*Client]struct{} // 本地在线客户端集合
	maxConns      int32                // 最大连接数（0=不限制）
	totalRejected int32                // 原子计数: 因达到上限被拒绝的连接数

	backend    Backend // 分布式后端（如 RabbitMQ Fanout，nil=单机模式）
	instanceID string  // 本实例唯一标识，用于跳过自发布消息回环
	done       chan struct{}
	wg         sync.WaitGroup
	started    int32 // 原子标记，确保 Start 只执行一次

	// 分布式客户端注册表：跨实例同步的在线 UID 集合
	// uid → count（同一 UID 多设备连接）
	remoteUIDs sync.Map

	// WorkerPool 配置
	workerNum     int32          // worker 协程数（默认 4）
	workerCh      chan func()    // 投递任务通道
	workerWg      sync.WaitGroup // 等待 worker 退出
	workerStarted int32          // 原子标记
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
		clients:    make(map[*Client]struct{}),
		backend:    backend,
		done:       make(chan struct{}),
		instanceID: fmt.Sprintf("%p", backend), // 默认以 backend 指针地址作为实例 ID
		maxConns:   o.maxConns,
		workerNum:  o.workerNum,
	}
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
	if !atomic.CompareAndSwapInt32(&dd.started, 0, 1) {
		return
	}

	// 启动 Worker Pool（背压保护）
	if n := atomic.LoadInt32(&dd.workerNum); n > 0 {
		dd.workerCh = make(chan func(), n*2) // 缓冲队列为 worker 数的 2 倍
		for i := int32(0); i < n; i++ {
			dd.workerWg.Add(1)
			go dd.workerLoop()
		}
		atomic.StoreInt32(&dd.workerStarted, 1)
		logger.InfoWithCtx(ctx, "dispatcher worker pool started", logger.Int32("workers", n))
	}

	msgCh, err := dd.backend.Receive(ctx)
	if err != nil {
		logger.WarnWithCtx(ctx, "dispatcher start receive failed",
			logger.Err(err),
		)
		atomic.StoreInt32(&dd.started, 0)
		return
	}

	dd.wg.Add(1)
	go dd.receiveLoop(ctx, msgCh)
	logger.InfoWithCtx(ctx, "distributed dispatcher started")
}

// workerLoop Worker 协程：从 workerCh 取任务执行。
func (dd *DistributedDispatcher) workerLoop() {
	defer dd.workerWg.Done()
	for task := range dd.workerCh {
		task()
	}
}

// Stop 停止后台接收协程，释放后端资源。
// 关闭顺序: 关闭 Backend（停止接收新消息）→ 关闭 WorkerPool → 通知 receiveLoop 退出 → 等待所有协程结束。
func (dd *DistributedDispatcher) Stop() {
	// 1. 先关闭后端，停止接收新消息
	if dd.backend != nil {
		if err := dd.backend.Close(); err != nil {
			logger.WarnWithCtx(context.Background(), "dispatcher backend close failed",
				logger.Err(err),
			)
		}
	}

	// 2. 关闭 receiveLoop
	close(dd.done)
	dd.wg.Wait()

	// 3. 关闭 WorkerPool
	if atomic.LoadInt32(&dd.workerStarted) == 1 {
		close(dd.workerCh)
		dd.workerWg.Wait()
	}
}

// Len 返回全局在线连接数（本地 + 远端）。
func (dd *DistributedDispatcher) Len() int {
	dd.mu.RLock()
	local := len(dd.clients)
	dd.mu.RUnlock()

	remote := 0
	dd.remoteUIDs.Range(func(_, _ any) bool {
		remote++
		return true
	})
	return local + remote
}

// MaxConnections 返回最大连接数限制（0=不限制）。
func (dd *DistributedDispatcher) MaxConnections() int {
	return int(atomic.LoadInt32(&dd.maxConns))
}

// TotalRejected 返回因达到连接上限被拒绝的累计连接数。
func (dd *DistributedDispatcher) TotalRejected() int {
	return int(atomic.LoadInt32(&dd.totalRejected))
}

// Clients 返回当前所有已注册客户端的快照切片。
func (dd *DistributedDispatcher) Clients() []*Client {
	dd.mu.RLock()
	defer dd.mu.RUnlock()
	clients := make([]*Client, 0, len(dd.clients))
	for client := range dd.clients {
		clients = append(clients, client)
	}
	return clients
}

// Range 遍历所有已注册客户端。
// 如果 fn 返回 false 则停止遍历。
func (dd *DistributedDispatcher) Range(fn func(*Client) bool) {
	dd.mu.RLock()
	clients := make([]*Client, 0, len(dd.clients))
	for client := range dd.clients {
		clients = append(clients, client)
	}
	dd.mu.RUnlock()

	for _, client := range clients {
		if !fn(client) {
			break
		}
	}
}

// DispatcherStats 分发中心统计信息。
type DispatcherStats struct {
	TotalConnections int // 当前在线连接总数（本地 + 远端）
	LocalConnections int // 本地在线连接数
	RemoteUIDs       int // 远端实例上的唯一 UID 数
	MaxConnections   int // 最大连接数限制（0=不限制）
	TotalRejected    int // 因达到上限被拒绝的累计连接数
}

// Stats 返回分发中心的实时统计信息。
func (dd *DistributedDispatcher) Stats() DispatcherStats {
	local := dd.LenLocal()

	remote := 0
	dd.remoteUIDs.Range(func(_, _ any) bool {
		remote++
		return true
	})

	return DispatcherStats{
		TotalConnections: local + remote,
		LocalConnections: local,
		RemoteUIDs:       remote,
		MaxConnections:   dd.MaxConnections(),
		TotalRejected:    dd.TotalRejected(),
	}
}

// LenLocal 返回本地在线连接数。
func (dd *DistributedDispatcher) LenLocal() int {
	dd.mu.RLock()
	defer dd.mu.RUnlock()
	return len(dd.clients)
}

// ConnectedUIDs 返回所有实例上的在线 UID 列表（去重）。
func (dd *DistributedDispatcher) ConnectedUIDs() []string {
	seen := make(map[string]struct{})

	// 1. 本地 UID
	dd.mu.RLock()
	for client := range dd.clients {
		if client.uid != "" {
			seen[client.uid] = struct{}{}
		}
	}
	dd.mu.RUnlock()

	// 2. 远端 UID
	dd.remoteUIDs.Range(func(key, _ any) bool {
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

// CleanupDeadConns 清理所有已关闭的僵尸连接，释放 Dispatcher 内存。
// 返回清理的连接数。
func (dd *DistributedDispatcher) CleanupDeadConns() int {
	dd.mu.Lock()
	defer dd.mu.Unlock()

	var cleaned int
	for client := range dd.clients {
		if !client.IsAlive() {
			delete(dd.clients, client)
			cleaned++
		}
	}
	return cleaned
}
