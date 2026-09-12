# gogroutine - 生产级 goroutine 管理

基于 [ants](https://github.com/panjf2000/ants) 协程池的生产级 goroutine 管理包，提供安全、可控、可观测的异步任务执行能力。

## 核心特性

| 特性 | 说明 |
|------|------|
| **自动 panic 恢复** | 任务 panic 不会导致程序崩溃 |
| **并发控制** | 基于协程池，防止 goroutine 泄漏和资源耗尽 |
| **池满降级** | 池满时自动降级为原生 goroutine，保证任务不丢失 |
| **context 校验** | ctx 已取消的任务直接跳过 |
| **链路追踪** | 内置 OpenTelemetry Span，每个任务自动创建追踪链路 |
| **指标监控** | 内置 Metrics 接口，支持 Prometheus 等监控系统 |
| **优雅关闭** | 支持自动释放和手动控制 |

## 快速开始

```go
import "github.com/18721889353/sunshine/pkg/gogroutine"

// 基础用法（自动初始化）
gogroutine.Go(ctx, func() {
    // 异步任务
})

// 带名称（便于日志追踪和监控）
gogroutine.GoWithName(ctx, "sendMQ", func() {
    // 异步任务
})

// 带超时控制
gogroutine.GoWithTimeout(ctx, "download", 30*time.Second, func(ctx context.Context) {
    // ctx 会在 30 秒后自动取消
})

// 批量执行并等待全部完成
tasks := []func(){task1, task2, task3}
gogroutine.GoBatch(ctx, tasks)

// 泛型版本：批量执行并收集结果（返回所有错误）
results, err := gogroutine.GoBatchWithResult(ctx, "fetchUsers", tasks)
if err != nil {
    // err 包含所有失败任务的错误（errors.Join 聚合）
    // 可使用 errors.Is(err, targetErr) 检查特定错误
}
```

## 生产配置

```go
gogroutine.Init(
    gogroutine.WithPoolSize(500),        // 池大小（默认 1000）
    gogroutine.WithPreAlloc(true),        // 预分配内存（高并发场景）
    gogroutine.WithDisablePurge(true),    // 禁用自动清理（突发流量场景）
    gogroutine.WithGracefulShutdown(true),// 启用优雅关闭（推荐）
    gogroutine.WithGracefulShutdownTimeout(10*time.Second), // 关闭超时（默认 30s）
)
```

## 生产案例

### HTTP Handler 异步任务

```go
func RegisterHandler(c *gin.Context) {
    user, err := svc.CreateUser(c.Request.Context(), &req)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    // 异步操作：不阻塞响应
    ctx := c.Request.Context()
    gogroutine.GoWithName(ctx, "sendWelcomeEmail", func() {
        mail.SendWelcome(user.Email)
    })
    gogroutine.GoWithName(ctx, "initPoints", func() {
        points.Init(user.ID, 100)
    })

    c.JSON(200, gin.H{"user_id": user.ID})
}
```

### 批量并发查询

```go
func DashboardHandler(c *gin.Context) {
    tasks := []func() (any, error){
        func() (any, error) { return svc.GetUserProfile(ctx, userID) },
        func() (any, error) { return svc.GetUserOrders(ctx, userID) },
        func() (any, error) { return svc.GetUserPoints(ctx, userID) },
    }

    results, err := gogroutine.GoBatchWithResult(ctx, "dashboard", tasks)
    if err != nil {
        // err 包含所有失败任务的错误
        logger.WarnWithCtx(ctx, "dashboard batch failed", logger.Err(err))
    }

    c.JSON(200, gin.H{
        "profile": results[0],
        "orders":  results[1],
        "points":  results[2],
    })
}
```

### 消息队列消费者

```go
func ConsumeOrder(ctx context.Context, msg amqp.Delivery) {
    var order OrderMessage
    if err := json.Unmarshal(msg.Body, &order); err != nil {
        msg.Nack(false, false)
        return
    }

    // 异步处理（不阻塞消费循环）
    gogroutine.GoWithName(ctx, "processOrder", func() {
        svc.ProcessOrder(ctx, &order)
    })

    msg.Ack(false)
}
```

### 批量数据同步

```go
func SyncDataToDownstream(ctx context.Context) {
    items, _ := svc.GetAllItems(ctx)
    batchSize := 50

    for i := 0; i < len(items); i += batchSize {
        batch := items[i:min(i+batchSize, len(items))]
        tasks := make([]func(), len(batch))

        for j := range batch {
            item := batch[j]
            tasks[j] = func() {
                downstream.Push(ctx, item)
            }
        }

        gogroutine.GoBatchWithName(ctx, "sync", tasks)
    }
}
```

## 优雅关闭

### 方式一：自动释放（推荐）

```go
gogroutine.Init(
    gogroutine.WithGracefulShutdown(true),
    gogroutine.WithGracefulShutdownTimeout(10*time.Second),
)

// 注册退出回调（可选）
gogroutine.RegisterGracefulShutdownHook(func() {
    db.Close()
    cache.Flush()
})
```

### 方式二：手动控制

```go
// 程序退出前调用
gogroutine.ReleaseAndWaitWithTimeout(10 * time.Second)
```

## 监控集成

```go
// 获取池状态
s := gogroutine.PoolStats()
fmt.Printf("running: %d, waiting: %d, cap: %d\n", s.Running, s.Waiting, s.Cap)
fmt.Printf("success: %d, panic: %d, fallback: %d\n", s.Success, s.Panic, s.Fallback)
```

### 注入 Prometheus 监控

```go
type myMetrics struct {
    running  *prometheus.GaugeVec
    panic    *prometheus.CounterVec
    fallback *prometheus.CounterVec
}

func (m *myMetrics) IncRunning(name string)  { m.running.WithLabelValues(name).Inc() }
func (m *myMetrics) DecRunning(name string)  { m.running.WithLabelValues(name).Dec() }
func (m *myMetrics) IncPanic(name string)    { m.panic.WithLabelValues(name).Inc() }
func (m *myMetrics) IncFallback(name string) { m.fallback.WithLabelValues(name).Inc() }
func (m *myMetrics) ObserveTaskDuration(name string, d time.Duration) {}

gogroutine.SetMetrics(&myMetrics{})
```

## 生产注意事项

1. **Init 可选**：不调用 `Init()` 时，首次提交任务会自动使用默认配置
2. **panic 安全**：任务内的 panic 不会崩溃程序，自动恢复并记录日志
3. **池满不阻塞**：池满时自动降级为原生 goroutine（默认行为）
4. **资源释放**：推荐启用 `WithGracefulShutdown(true)` 自动释放
5. **ctx 传递**：使用 `c.Request.Context()` 传递 request_id，便于链路追踪
6. **任务命名**：使用 `GoWithName` 便于监控系统按任务名称聚合指标
7. **K8s 配置**：`terminationGracePeriodSeconds` 建议比 ShutdownTimeout 大 5 秒

## API 速查

| 函数 | 说明 |
|------|------|
| `Init(opts ...Option)` | 初始化全局协程池（可选） |
| `Go(ctx, task)` | 提交任务（fire-and-forget） |
| `GoWithName(ctx, name, task)` | 提交带名称的任务 |
| `GoWithTimeout(ctx, name, timeout, task)` | 提交带超时的任务 |
| `GoBatch(ctx, tasks)` | 批量执行并等待完成 |
| `GoBatchWithName(ctx, name, tasks)` | 批量执行带统一名称前缀 |
| `GoBatchWithResult(ctx, name, tasks)` | 泛型版本：批量执行并收集结果（返回所有错误） |
| `PoolStats()` | 获取池统计信息 |
| `Release()` | 释放池（非阻塞） |
| `ReleaseAndWait()` | 释放池并等待完成（默认 30s 超时） |
| `ReleaseAndWaitWithTimeout(timeout)` | 释放池并等待完成（自定义超时） |
| `SetMetrics(m)` | 注入自定义监控实现 |
| `RegisterGracefulShutdownHook(hook)` | 注册退出回调 |
| `IsGracefulShutdownEnabled()` | 检查是否启用优雅关闭 |