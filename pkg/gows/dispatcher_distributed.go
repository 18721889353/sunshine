package gows

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

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

	backend    Backend              // 分布式后端（如 RabbitMQ Fanout，nil=单机模式）
	instanceID string               // 本实例唯一标识，用于跳过自发布消息回环
	done       chan struct{}
	wg         sync.WaitGroup
	started    int32                // 原子标记，确保 Start 只执行一次

	// 分布式客户端注册表：跨实例同步的在线 UID 集合
	// uid → count（同一 UID 多设备连接）
	remoteUIDs sync.Map
	remoteMu   sync.Mutex          // 保护 remoteUIDs 与本地状态的一致性

	// WorkerPool 配置
	workerNum int32                // worker 协程数（默认 4）
	workerCh  chan func()          // 投递任务通道
	workerWg  sync.WaitGroup       // 等待 worker 退出
	workerStarted int32            // 原子标记
}

// DispatcherOption 分发中心配置选项函数类型
type DispatcherOption func(*DistributedDispatcher)

// WithMaxConnections 设置最大连接数限制。
// 参数:
//   - n: 最大连接数。达到上限时 Register 返回 ErrMaxConnections。
//     0 表示不限制（默认）。
func WithMaxConnections(n int) DispatcherOption {
	return func(d *DistributedDispatcher) {
		if n > 0 {
			atomic.StoreInt32(&d.maxConns, int32(n))
		}
	}
}

// WithWorkerPool 设置后台 Worker 协程数，用于消息投递的背压保护。
// 参数:
//   - n: Worker 协程数（默认 4，建议 2~16）。Worker 池满时消息降级为串行处理。
//     0 或 1 表示关闭 WorkerPool，receiveLoop 串行投递。
func WithWorkerPool(n int) DispatcherOption {
	return func(d *DistributedDispatcher) {
		if n > 1 {
			atomic.StoreInt32(&d.workerNum, int32(n))
		}
	}
}

// ErrMaxConnections 达到最大连接数限制的错误
var ErrMaxConnections = fmt.Errorf("dispatcher: max connections reached")

// NewDispatcher 创建并初始化一个新的消息分发中心。
// 参数:
//   - backend: 分布式后端，传 nil 表示单机模式（仅供本地消息投递）
//   - opts: 可选配置（如 WithMaxConnections、WithWorkerPool）
//
// 示例（单机）:
//
//	d := gows.NewDispatcher(nil)
//	d.Register(client)
//
// 示例（分布式）:
//
//	backend := gows.NewRabbitMQBackend("amqp://...", "ws:messages")
//	d := gows.NewDispatcher(backend, gows.WithMaxConnections(10000))
//	d.Start(ctx)
func NewDispatcher(backend Backend, opts ...DispatcherOption) *DistributedDispatcher {
	d := &DistributedDispatcher{
		clients:    make(map[*Client]struct{}),
		backend:    backend,
		done:       make(chan struct{}),
		instanceID: fmt.Sprintf("%p", backend), // 默认以 backend 指针地址作为实例 ID
		workerNum:  4,                           // 默认 4 个 worker
	}
	for _, opt := range opts {
		opt(d)
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
			go dd.workerLoop(i)
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
func (dd *DistributedDispatcher) workerLoop(id int32) {
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
		_ = dd.backend.Close()
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

// Register 将客户端连接注册到 Dispatcher 全局列表。
// 如果达到最大连接数上限，返回 ErrMaxConnections。
// 分布式模式下自动向所有实例广播上线通知。
// 调用方应检查此错误并执行 client.Close() 释放连接资源。
//
// 注意: 上线通知在锁内发布，确保其他实例收到通知时本实例的 clients 已可见，
// 避免自发布通知被误认为是远端连接（修复 remoteUIDs 竞态）。
func (dd *DistributedDispatcher) Register(client *Client) error {
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
		dd.publishClientEvent(context.Background(), "client_online", client.uid)
		dd.mu.Unlock()
		return nil
	}

	dd.mu.Lock()
	dd.clients[client] = struct{}{}
	dd.publishClientEvent(context.Background(), "client_online", client.uid)
	dd.mu.Unlock()

	return nil
}

// Unregister 从 Dispatcher 全局列表中移除客户端连接。
// 分布式模式下自动向所有实例广播下线通知。
func (dd *DistributedDispatcher) Unregister(client *Client) {
	dd.mu.Lock()
	delete(dd.clients, client)
	dd.publishClientEvent(context.Background(), "client_offline", client.uid)
	dd.mu.Unlock()
}

// publishClientEvent 向 Backend 发布客户端上线/下线事件。
// 自动填充 InstanceID 用于自发布消息跳过。
func (dd *DistributedDispatcher) publishClientEvent(ctx context.Context, eventType, uid string) {
	if dd.backend == nil {
		return
	}
	data, _ := json.Marshal(map[string]string{"uid": uid})
	_ = dd.backend.Publish(ctx, &PubSubMessage{
		InstanceID: dd.instanceID,
		Type:       eventType,
		UIDs:       []string{uid},
		Payload:    data,
	})
}

// receiveLoop 后台协程：持续从后端接收跨实例消息并在本地投递。
// 使用 WorkerPool 实现背压保护：当 Worker 全忙时，新消息降级为串行处理。
func (dd *DistributedDispatcher) receiveLoop(ctx context.Context, msgCh <-chan *PubSubMessage) {
	defer dd.wg.Done()

	tracer := otel.Tracer("gows")
	for {
		select {
		case <-dd.done:
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			dd.dispatchMessage(ctx, tracer, msg)
		}
	}
}

// dispatchMessage 将消息分发到 WorkerPool 或串行处理。
// WorkerPool 可用时异步投递，否则串行降级（反压保护）。
func (dd *DistributedDispatcher) dispatchMessage(ctx context.Context, tracer trace.Tracer, msg *PubSubMessage) {
	if atomic.LoadInt32(&dd.workerStarted) == 1 {
		// WorkerPool 模式：尝试投递到 Worker 队列
		task := func() {
			dd.deliverMessage(ctx, tracer, msg)
		}
		select {
		case dd.workerCh <- task:
			return // 成功投递
		default:
			// Worker 队列满，降级为串行处理（反压保护）
			logger.WarnWithCtx(ctx, "ws dispatcher worker pool full, fallback to serial processing")
		}
	}
	// 串行处理（默认或降级）
	dd.deliverMessage(ctx, tracer, msg)
}

// deliverMessage 将一条跨实例消息投递到本地匹配的客户端。
func (dd *DistributedDispatcher) deliverMessage(ctx context.Context, tracer trace.Tracer, msg *PubSubMessage) {
	_, span := tracer.Start(ctx, "ws.distributed.deliver",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()
	span.SetAttributes(
		attribute.String("ws.msg_type", msg.Type),
		attribute.Int("ws.target_uids", len(msg.UIDs)),
		requestIDAttr(ctx),
	)

	switch msg.Type {
	case "broadcast":
		dd.deliverBroadcast(span, msg.Payload)

	case "send_to_uid":
		dd.deliverToUIDs(span, msg.UIDs, msg.Payload)

	case "client_online":
		dd.handleRemoteOnline(msg.UIDs)

	case "client_offline":
		dd.handleRemoteOffline(msg.UIDs)

	default:
		span.SetStatus(codes.Error, "unknown message type: "+msg.Type)
	}
}

// handleRemoteOnline 处理远端客户端上线通知。
// 如果 UID 已在本实例上连接，则不重复计入远端（避免自发布干扰）。
func (dd *DistributedDispatcher) handleRemoteOnline(uids []string) {
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		// 跳过本实例已有的 UID
		if dd.hasLocalUID(uid) {
			continue
		}
		// 递增计数
		for {
			val, _ := dd.remoteUIDs.LoadOrStore(uid, int32(0))
			count := val.(int32)
			if dd.remoteUIDs.CompareAndSwap(uid, count, count+1) {
				break
			}
		}
	}
}

// handleRemoteOffline 处理远端客户端下线通知。
func (dd *DistributedDispatcher) handleRemoteOffline(uids []string) {
	for _, uid := range uids {
		if uid == "" {
			continue
		}
		// 如果本实例上仍有此 UID，说明是本实例的上线通知被其他实例回传，忽略下线
		if dd.hasLocalUID(uid) {
			continue
		}
		val, ok := dd.remoteUIDs.Load(uid)
		if !ok {
			continue
		}
		count := val.(int32)
		if count <= 1 {
			dd.remoteUIDs.Delete(uid)
		} else {
			dd.remoteUIDs.CompareAndSwap(uid, count, count-1)
		}
	}
}

// hasLocalUID 检查指定 UID 是否在本实例上有在线连接。
func (dd *DistributedDispatcher) hasLocalUID(uid string) bool {
	dd.mu.RLock()
	defer dd.mu.RUnlock()
	for client := range dd.clients {
		if client.uid == uid {
			return true
		}
	}
	return false
}

// deliverBroadcast 向本地所有在线客户端广播消息。
func (dd *DistributedDispatcher) deliverBroadcast(span trace.Span, payload json.RawMessage) {
	dd.mu.RLock()
	clients := make([]*Client, 0, len(dd.clients))
	for client := range dd.clients {
		clients = append(clients, client)
	}
	dd.mu.RUnlock()

	var deadClients []*Client
	var sentCount int
	for _, client := range clients {
		if !client.IsAlive() {
			deadClients = append(deadClients, client)
			continue
		}
		if err := client.WriteRaw(payload); err != nil {
			logger.WarnWithCtx(client.ctx, "ws distributed broadcast write failed",
				logger.String("uid", client.uid),
				logger.Err(err),
			)
		}
		sentCount++
	}

	span.SetAttributes(
		attribute.Int("ws.local_sent", sentCount),
		attribute.Int("ws.local_clients", len(clients)),
	)

	if len(deadClients) > 0 {
		dd.mu.Lock()
		for _, client := range deadClients {
			delete(dd.clients, client)
		}
		dd.mu.Unlock()
	}

	span.SetStatus(codes.Ok, "broadcast delivered")
}

// deliverToUIDs 向本地指定 UID 的客户端投递消息。
func (dd *DistributedDispatcher) deliverToUIDs(span trace.Span, uids []string, payload json.RawMessage) {
	if len(uids) == 0 {
		span.SetStatus(codes.Ok, "no target uids")
		return
	}

	uidSet := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uidSet[uid] = struct{}{}
	}

	dd.mu.RLock()
	clients := make([]*Client, 0, len(dd.clients))
	for client := range dd.clients {
		clients = append(clients, client)
	}
	dd.mu.RUnlock()

	var deadClients []*Client
	var sentCount int
	for _, client := range clients {
		if !client.IsAlive() {
			deadClients = append(deadClients, client)
			continue
		}
		if _, ok := uidSet[client.uid]; ok {
			if err := client.WriteRaw(payload); err != nil {
				logger.WarnWithCtx(client.ctx, "ws distributed send_to_uid write failed",
					logger.String("uid", client.uid),
					logger.Err(err),
				)
			}
			sentCount++
		}
	}

	span.SetAttributes(
		attribute.Int("ws.local_sent", sentCount),
		attribute.Int("ws.local_clients", len(clients)),
	)

	if len(deadClients) > 0 {
		dd.mu.Lock()
		for _, client := range deadClients {
			delete(dd.clients, client)
		}
		dd.mu.Unlock()
	}

	span.SetStatus(codes.Ok, "send_to_uid delivered")
}

// ---------------------------------------------------------------------------
// 公开 API
// ---------------------------------------------------------------------------

// SendToUIDCtx 向指定 UID 的客户端发送消息（跨实例）。
// 所有实例通过 Backend 接收后在本地查找并投递。
// 自动清理已关闭的僵尸连接。
func (dd *DistributedDispatcher) SendToUIDCtx(ctx context.Context, uid string, v any) {
	dd.SendToMultiUIDCtx(ctx, []string{uid}, v)
}

// SendToMultiUIDCtx 向多个指定 UID 的客户端发送消息（跨实例）。
func (dd *DistributedDispatcher) SendToMultiUIDCtx(ctx context.Context, uids []string, v any) {
	if dd.backend == nil {
		// 单机模式：直接本地投递
		payload, _ := json.Marshal(v)
		tracer := otel.Tracer("gows")
		_, span := tracer.Start(ctx, "ws.local.send_to_uids",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		defer span.End()
		dd.deliverToUIDs(span, uids, payload)
		return
	}

	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.distributed.send_to_uids",
		trace.WithSpanKind(trace.SpanKindProducer),
	)
	defer span.End()
	span.SetAttributes(
		attribute.StringSlice("ws.target_uids", uids),
		requestIDAttr(ctx),
	)

	if len(uids) == 0 {
		span.SetStatus(codes.Ok, "no target uids")
		return
	}

	payload, err := json.Marshal(v)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws distributed send_to_uids marshal failed",
			logger.Err(err),
		)
		return
	}

	msg := &PubSubMessage{
		InstanceID: dd.instanceID,
		Type:       "send_to_uid",
		UIDs:       uids,
		Payload:    payload,
	}
	if err := dd.backend.Publish(ctx, msg); err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws distributed send_to_uids publish failed",
			logger.Err(err),
			logger.String("uids", fmt.Sprintf("%v", uids)),
		)
		return
	}

	span.SetStatus(codes.Ok, "published")
}

// BroadcastCtx 向所有在线客户端广播消息（跨实例）。
func (dd *DistributedDispatcher) BroadcastCtx(ctx context.Context, v any) {
	if dd.backend == nil {
		// 单机模式：直接本地广播
		payload, _ := json.Marshal(v)
		tracer := otel.Tracer("gows")
		_, span := tracer.Start(ctx, "ws.local.broadcast",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		defer span.End()
		dd.deliverBroadcast(span, payload)
		return
	}

	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.distributed.broadcast",
		trace.WithSpanKind(trace.SpanKindProducer),
	)
	defer span.End()
	span.SetAttributes(requestIDAttr(ctx))

	payload, err := json.Marshal(v)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws distributed broadcast marshal failed",
			logger.Err(err),
		)
		return
	}

	msg := &PubSubMessage{
		InstanceID: dd.instanceID,
		Type:       "broadcast",
		Payload:    payload,
	}
	if err := dd.backend.Publish(ctx, msg); err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.WarnWithCtx(ctx, "ws distributed broadcast publish failed",
			logger.Err(err),
		)
		return
	}

	span.SetStatus(codes.Ok, "published")
}

// BroadcastFilterCtx 向满足 filter 条件的本地客户端广播消息（仅本地）。
func (dd *DistributedDispatcher) BroadcastFilterCtx(ctx context.Context, v any, filter func(*Client) bool) {
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.broadcast_filter", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(requestIDAttr(ctx))

	dd.mu.RLock()
	clients := make([]*Client, 0, len(dd.clients))
	for client := range dd.clients {
		clients = append(clients, client)
	}
	dd.mu.RUnlock()

	span.SetAttributes(attribute.Int("ws.broadcast_targets", len(clients)))

	var deadClients []*Client
	payload, _ := json.Marshal(v)
	for _, client := range clients {
		if !client.IsAlive() {
			deadClients = append(deadClients, client)
			continue
		}
		if filter(client) {
			if err := client.WriteRaw(payload); err != nil {
				logger.WarnWithCtx(ctx, "ws broadcast_filter write failed",
					logger.String("uid", client.uid),
					logger.Err(err),
				)
			}
		}
	}

	if len(deadClients) > 0 {
		span.SetAttributes(attribute.Int("ws.dead_clients_cleaned", len(deadClients)))
		dd.mu.Lock()
		for _, client := range deadClients {
			delete(dd.clients, client)
		}
		dd.mu.Unlock()
	}

	span.SetStatus(codes.Ok, "broadcast_filter completed")
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
		uid := key.(string)
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
