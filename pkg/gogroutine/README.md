# gogroutine - 生产级 goroutine 管理

基于 [ants](https://github.com/panjf2000/ants) 协程池，参考字节跳动 gopool 设计，支持多实例池管理。

## 核心特性

| 特性 | 说明 |
|------|------|
| **自动 panic 恢复** | 任务 panic 不会导致程序崩溃 |
| **并发控制** | 基于协程池，防止 goroutine 泄漏 |
| **池满降级** | 池满时自动降级为原生 goroutine |
| **链路追踪** | 内置 OpenTelemetry Span |
| **优雅关闭** | 支持自动释放和手动控制 |
| **多实例池** | 支持创建独立命名的池实例 |

## 生产案例

### 案例一：HTTP Handler 异步任务

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

### 案例二：批量并发查询

```go
func DashboardHandler(c *gin.Context) {
    tasks := []func() (any, error){
        func() (any, error) { return svc.GetUserProfile(ctx, userID) },
        func() (any, error) { return svc.GetUserOrders(ctx, userID) },
        func() (any, error) { return svc.GetUserPoints(ctx, userID) },
    }

    results, err := gogroutine.GoBatchWithResult(ctx, "dashboard", tasks)
    if err != nil {
        logger.WarnWithCtx(ctx, "dashboard batch failed", logger.Err(err))
    }

    c.JSON(200, gin.H{
        "profile": results[0],
        "orders":  results[1],
        "points":  results[2],
    })
}
```

### 案例三：消息队列消费者

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

### 案例四：批量数据同步

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

### 案例五：独立服务池（订单处理）

```go
// 启动时初始化独立池
var orderPool = gogroutine.New("order-processor", 100)

// 订单处理
func HandleOrder(ctx context.Context, order *Order) {
    orderPool.CtxGo(ctx, func() {
        svc.ProcessOrder(ctx, order)
    })
}

// 关闭时释放
func Shutdown() {
    orderPool.Release()
}
```

### 案例六：多服务池隔离

```go
// 不同服务使用独立池，互不影响
var (
    orderPool    = gogroutine.New("orders", 50)
    paymentPool  = gogroutine.New("payments", 30)
    notifyPool   = gogroutine.New("notifications", 20)
)

func HandlePayment(ctx context.Context, payment *Payment) {
    paymentPool.CtxGo(ctx, func() {
        paymentSvc.Process(ctx, payment)
    })
    // 通知异步
    notifyPool.Go(func() {
        notifySvc.Send(ctx, payment.UserID)
    })
}
```

### 案例七：Kafka 消费者并发处理

```go
func KafkaConsumer(ctx context.Context, msg *kafka.Message) {
    var event Event
    if err := json.Unmarshal(msg.Value, &event); err != nil {
        logger.ErrorWithCtx(ctx, "unmarshal failed", logger.Err(err))
        return
    }

    gogroutine.GoWithName(ctx, "kafka-"+event.Type, func() {
        switch event.Type {
        case "order.created":
            orderSvc.HandleCreated(ctx, &event)
        case "order.paid":
            orderSvc.HandlePaid(ctx, &event)
        }
    })
}
```

### 案例八：定时任务批量执行

```go
func DailySync(ctx context.Context) {
    tasks := []func(){
        func() { syncUsers(ctx) },
        func() { syncOrders(ctx) },
        func() { syncProducts(ctx) },
        func() { cleanExpiredCache(ctx) },
    }
    gogroutine.GoBatch(ctx, tasks)
}
```

## 生产配置

```go
gogroutine.Init(
    gogroutine.WithPoolSize(500),                      // 池大小（默认 1000）
    gogroutine.WithPreAlloc(true),                      // 预分配内存
    gogroutine.WithGracefulShutdown(true),              // 启用优雅关闭
    gogroutine.WithGracefulShutdownTimeout(10*time.Second), // 关闭超时
)
```

## 优雅关闭

```go
// 方式一：自动释放（推荐）
gogroutine.Init(gogroutine.WithGracefulShutdown(true))

// 方式二：手动控制
gogroutine.ReleaseAndWaitWithTimeout(10 * time.Second)
```

## 监控

```go
s := gogroutine.PoolStats()
fmt.Printf("running: %d, waiting: %d, cap: %d\n", s.Running, s.Waiting, s.Cap)
fmt.Printf("success: %d, panic: %d, fallback: %d\n", s.Success, s.Panic, s.Fallback)
```

## 注意事项

1. **panic 安全**：任务内 panic 不会崩溃程序
2. **池满不阻塞**：自动降级为原生 goroutine
3. **ctx 传递**：使用 `c.Request.Context()` 传递 request_id
4. **任务命名**：使用 `GoWithName` 便于监控聚合
5. **K8s 配置**：`terminationGracePeriodSeconds` 建议比 ShutdownTimeout 大 5 秒

## API 速查

| 函数 | 说明 |
|------|------|
| `Init(opts ...Option)` | 初始化全局协程池 |
| `Go(ctx, task)` | 提交任务 |
| `GoWithName(ctx, name, task)` | 提交带名称的任务 |
| `GoWithTimeout(ctx, name, timeout, task)` | 提交带超时的任务 |
| `GoBatch(ctx, tasks)` | 批量执行并等待完成 |
| `GoBatchWithResult(ctx, name, tasks)` | 批量执行并收集结果 |
| `New(name, capacity, opts...)` | 创建独立池实例 |
| `Get(name)` | 获取池实例 |
| `ReleasePool(name)` | 释放指定池实例 |
| `ReleaseAllPools()` | 释放所有池实例 |
| `PoolStats()` | 获取全局池统计 |
| `Release()` | 释放全局池 |
| `ReleaseAndWait()` | 释放并等待完成 |
| `SetMetrics(m)` | 注入自定义监控 |
