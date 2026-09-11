# gogroutine - 生产级 goroutine 管理工具

## 简介

`gogroutine` 是 Sunshine 微服务框架的 goroutine 管理包，提供安全、可控、可观测的异步任务执行能力。

基于 [ants](https://github.com/panjf2000/ants) 协程池，经过生产验证。

## 核心特性

| 特性 | 说明 |
|------|------|
| **自动 panic 恢复** | 任务 panic 不会导致程序崩溃，自动记录日志和链路 |
| **并发控制** | 基于 ants 协程池，防止 goroutine 泄漏和资源耗尽 |
| **池满降级** | 协程池满时自动降级为原生 goroutine，保证任务不丢失 |
| **context 校验** | ctx 已取消的任务直接跳过，避免无效入队 |
| **任务命名** | 带名称的任务便于日志追踪和监控聚合 |
| **链路追踪** | 内置 OpenTelemetry Span，每个任务自动创建追踪链路 |
| **指标监控** | 内置 Metrics 接口，可注入 Prometheus 等监控实现 |
| **优雅关闭** | `ReleaseAndWait` 带超时控制，避免无限阻塞 |

## 快速开始

### 基础用法

```go
import "github.com/18721889353/sunshine/pkg/gogroutine"

// 自动初始化（首次调用时使用默认配置）
gogroutine.Go(ctx, func() {
    // 异步任务
})

// 带名称（便于日志追踪和监控）
gogroutine.GoWithName(ctx, "sendMQ", func() {
    // 异步任务
})
```

### 带超时控制

```go
// 超时自动取消
gogroutine.GoWithTimeout(ctx, "download", 30*time.Second, func(ctx context.Context) {
    // ctx 会在 30 秒后自动取消
    select {
    case <-ctx.Done():
        return // 超时退出
    case result := <-doWork():
        // 处理结果
    }
})

// 截止时间自动取消
deadline := time.Now().Add(1 * time.Hour)
gogroutine.GoWithDeadline(ctx, "report", deadline, func(ctx context.Context) {
    // ctx 会在 deadline 到达后取消
})

// 手动取消
cancel := gogroutine.GoWithCancel(ctx, "longTask", func(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
            // 执行任务
        }
    }
})
// 需要时调用 cancel() 提前终止
```

### 批量任务

```go
// 批量执行并等待全部完成
tasks := []func(){
    func() { /* 任务1 */ },
    func() { /* 任务2 */ },
    func() { /* 任务3 */ },
}
gogroutine.GoBatch(ctx, tasks)

// 带统一名称前缀（便于监控聚合）
gogroutine.GoBatchWithName(ctx, "fetchData", tasks)

// 泛型版本：批量执行并收集结果
tasks := []func() (string, error){
    func() (string, error) { return fetchUser(1) },
    func() (string, error) { return fetchUser(2) },
}
results, err := gogroutine.GoBatchWithResult(ctx, "fetchUsers", tasks)
// results 按索引对应，err 包含第一个失败的错误
```

### 初始化配置

```go
// 可选：显式初始化（不调用则使用默认配置）
gogroutine.Init(
    gogroutine.WithPoolSize(500),        // 池大小（默认 1000，范围 [10, 10000]）
    gogroutine.WithNonBlocking(true),     // 非阻塞模式（池满时立即返回错误）
    gogroutine.WithPreAlloc(true),        // 预分配内存（适合高并发）
    gogroutine.WithDisablePurge(true),    // 禁用自动清理（适合突发流量）
)
```

### 监控指标

```go
// 查看池状态
s := gogroutine.PoolStats()
fmt.Printf("running: %d, waiting: %d, cap: %d\n", s.Running, s.Waiting, s.Cap)
fmt.Printf("success: %d, panic: %d, fallback: %d\n", s.Success, s.Panic, s.Fallback)

// 快捷方式
fmt.Println("running:", gogroutine.Running())
fmt.Println("waiting:", gogroutine.Waiting())
fmt.Println("capacity:", gogroutine.Cap())
fmt.Println("is full:", gogroutine.IsFull())

// 格式化输出（调试/日志用途）
fmt.Println(gogroutine.FormatStats(s))
// 输出: pool[running=5 waiting=2 cap=1000] counters[success=100 panic=0 fallback=0]
```

### 注入自定义监控

```go
// 实现 Metrics 接口，注入 Prometheus 等监控系统
type myMetrics struct{}

func (m *myMetrics) IncRunning(name string)                    { /* ... */ }
func (m *myMetrics) DecRunning(name string)                    { /* ... */ }
func (m *myMetrics) ObserveTaskDuration(name string, d time.Duration) { /* ... */ }
func (m *myMetrics) IncPanic(name string)                      { /* ... */ }
func (m *myMetrics) IncFallback(name string)                   { /* ... */ }

gogroutine.SetMetrics(&myMetrics{})
```

### 优雅关闭

```go
// 等待所有任务完成（默认 30 秒超时）
gogroutine.ReleaseAndWait()

// 自定义超时
gogroutine.ReleaseAndWaitWithTimeout(10 * time.Second)

// 立即释放（非阻塞，不等待）
gogroutine.Release()
```

---

## 生产级使用案例

以下案例来自真实微服务场景，覆盖 HTTP 处理、消息队列、批量处理、优雅关闭、监控集成等核心场景。

---

### 案例一：HTTP Handler 异步任务

> 场景：用户注册后异步发送欢迎邮件和初始化积分，不阻塞 HTTP 响应。

```go
func RegisterHandler(c *gin.Context) {
    var req RegisterRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // 同步操作：创建用户
    user, err := svc.CreateUser(c.Request.Context(), &req)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    // 异步操作：不阻塞响应
    ctx := c.Request.Context() // 携带 request_id

    // 异步发邮件
    gogroutine.GoWithName(ctx, "sendWelcomeEmail", func() {
        if err := mail.SendWelcome(user.Email); err != nil {
            logger.WarnWithCtx(ctx, "welcome email failed",
                logger.String("email", user.Email),
                logger.Err(err),
            )
        }
    })

    // 异步初始化积分
    gogroutine.GoWithName(ctx, "initPoints", func() {
        if err := points.Init(user.ID, 100); err != nil {
            logger.WarnWithCtx(ctx, "init points failed",
                logger.Int64("user_id", user.ID),
                logger.Err(err),
            )
        }
    })

    c.JSON(200, gin.H{"user_id": user.ID})
}
```

**要点**：
- `ctx` 携带 request_id，异步任务自动继承链路追踪
- 每个任务命名便于监控聚合
- 邮件和积分失败只记日志，不阻塞主流程

---

### 案例二：HTTP Handler 批量并发查询

> 场景：聚合接口需要并发调用多个下游服务，汇总结果返回。

```go
func DashboardHandler(c *gin.Context) {
    userID := c.Param("user_id")
    ctx := c.Request.Context()

    // 定义 3 个并发查询任务
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

**要点**：
- `GoBatchWithResult` 泛型版本自动收集结果
- 任意一个下游失败不会中断其他查询
- 任务名称自动聚合为 `dashboard_0`、`dashboard_1`、`dashboard_2`

---

### 案例三：消息队列消费者

> 场景：RabbitMQ 消费者收到消息后异步处理，避免阻塞消费循环。

```go
func ConsumeOrder消息(ctx context.Context, msg amqp.Delivery) {
    var order OrderMessage
    if err := json.Unmarshal(msg.Body, &order); err != nil {
        logger.ErrorWithCtx(ctx, "unmarshal order failed", logger.Err(err))
        msg.Nack(false, false) // 无法解析的消息直接丢弃
        return
    }

    // 异步处理订单（不阻塞消费循环）
    gogroutine.GoWithName(ctx, "processOrder", func() {
        if err := svc.ProcessOrder(ctx, &order); err != nil {
            logger.ErrorWithCtx(ctx, "process order failed",
                logger.String("order_id", order.ID),
                logger.Err(err),
            )
            // 重试逻辑由 mq 层处理
        }
    })

    msg.Ack(false) // 立即确认，异步处理
}
```

**要点**：
- 立即 Ack 避免消费阻塞
- 异步任务 panic 不会影响消费循环
- 链路追踪通过 ctx 自动串联

---

### 案例四：批量数据同步（Fan-Out 模式）

> 场景：定时任务需要同步 1000 条数据到下游，并发执行 + 进度追踪。

```go
func SyncDataToDownstream(ctx context.Context) {
    items, err := svc.GetAllItems(ctx)
    if err != nil {
        logger.ErrorWithCtx(ctx, "get items failed", logger.Err(err))
        return
    }

    // 按批次分组，每批 50 条并发同步
    batchSize := 50
    for i := 0; i < len(items); i += batchSize {
        end := i + batchSize
        if end > len(items) {
            end = len(items)
        }
        batch := items[i:end]

        tasks := make([]func(), len(batch))
        for j := range batch {
            item := batch[j] // 显式捕获循环变量
            tasks[j] = func() {
                if err := downstream.Push(ctx, item); err != nil {
                    logger.WarnWithCtx(ctx, "sync item failed",
                        logger.String("item_id", item.ID),
                        logger.Err(err),
                    )
                }
            }
        }

        // GoBatch 等待当前批次完成后继续下一批
        gogroutine.GoBatchWithName(ctx, fmt.Sprintf("sync_batch_%d", i/batchSize), tasks)
    }

    logger.InfoWithCtx(ctx, "sync completed",
        logger.Int("total", len(items)),
    )
}
```

**要点**：
- 分批控制并发度，避免瞬时压力过大
- `GoBatch` 保证每批完成后才进入下一批
- 单条失败不影响同批次其他数据

---

### 案例五：带超时的外部 API 调用

> 场景：异步调用第三方 API，超时自动放弃。

```go
func SyncUserAvatar(ctx context.Context, user *User) {
    gogroutine.GoWithTimeout(ctx, "syncAvatar", 10*time.Second, func(tctx context.Context) {
        resp, err := http.Get(tctx, user.AvatarURL)
        if err != nil {
            // 超时或网络错误，记录日志即可
            logger.WarnWithCtx(tctx, "sync avatar failed",
                logger.Int64("user_id", user.ID),
                logger.Err(err),
            )
            return
        }
        defer resp.Body.Close()

        if err := storage.Upload(tctx, fmt.Sprintf("avatars/%d.jpg", user.ID), resp.Body); err != nil {
            logger.WarnWithCtx(tctx, "upload avatar failed",
                logger.Int64("user_id", user.ID),
                logger.Err(err),
            )
        }
    })
}
```

**要点**：
- 10 秒超时自动取消，避免 goroutine 泄漏
- 任务内始终检查 `tctx.Done()`
- 失败只记日志，不影响主业务

---

### 案例六：手动取消长时间任务

> 场景：后台任务需要支持管理员手动中止。

```go
type TaskManager struct {
    cancels sync.Map // task_id -> cancel func
}

func (tm *TaskManager) StartReport(ctx context.Context, reportID string) {
    cancel := gogroutine.GoWithCancel(ctx, "generateReport", func(tctx context.Context) {
        for page := 1; ; page++ {
            select {
            case <-tctx.Done():
                logger.InfoWithCtx(tctx, "report generation cancelled",
                    logger.String("report_id", reportID),
                )
                return
            default:
            }

            if err := generatePage(tctx, reportID, page); err != nil {
                logger.ErrorWithCtx(tctx, "generate page failed",
                    logger.Int("page", page),
                    logger.Err(err),
                )
                return
            }
        }
    })
    tm.cancels.Store(reportID, cancel)
}

func (tm *TaskManager) StopReport(reportID string) {
    if v, ok := tm.cancels.LoadAndDelete(reportID); ok {
        if cancel, ok := v.(context.CancelFunc); ok {
            cancel()
        }
    }
}
```

**要点**：
- `GoWithCancel` 返回 cancel 函数，通过 sync.Map 存储
- 任务内周期性检查 `tctx.Done()`
- 支持管理员通过 API 中止任务

---

### 案例七：Prometheus 指标集成

> 场景：将协程池指标暴露给 Prometheus 监控系统。

```go
import "github.com/prometheus/client_golang/prometheus"

// gogroutineMetrics 实现 gogroutine.Metrics 接口
type gogroutineMetrics struct {
    running    *prometheus.GaugeVec
    panic      *prometheus.CounterVec
    fallback   *prometheus.CounterVec
    taskDuration *prometheus.HistogramVec
}

func NewGogroutineMetrics() *gogroutineMetrics {
    m := &gogroutineMetrics{
        running: prometheus.NewGaugeVec(
            prometheus.GaugeOpts{Name: "gogroutine_running"},
            []string{"task_name"},
        ),
        panic: prometheus.NewCounterVec(
            prometheus.CounterOpts{Name: "gogroutine_panic_total"},
            []string{"task_name"},
        ),
        fallback: prometheus.NewCounterVec(
            prometheus.CounterOpts{Name: "gogroutine_fallback_total"},
            []string{"task_name"},
        ),
        taskDuration: prometheus.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "gogroutine_task_duration_seconds",
                Buckets: []float64{0.001, 0.01, 0.1, 0.5, 1, 5},
            },
            []string{"task_name"},
        ),
    }
    prometheus.MustRegister(m.running, m.panic, m.fallback, m.taskDuration)
    return m
}

func (m *gogroutineMetrics) IncRunning(name string)  { m.running.WithLabelValues(name).Inc() }
func (m *gogroutineMetrics) DecRunning(name string)  { m.running.WithLabelValues(name).Dec() }
func (m *gogroutineMetrics) IncPanic(name string)    { m.panic.WithLabelValues(name).Inc() }
func (m *gogroutineMetrics) IncFallback(name string) { m.fallback.WithLabelValues(name).Inc() }
func (m *gogroutineMetrics) ObserveTaskDuration(name string, d time.Duration) {
    m.taskDuration.WithLabelValues(name).Observe(d.Seconds())
}

// 在 main.go 中初始化
gogroutine.SetMetrics(NewGogroutineMetrics())
```

**PromQL 示例**：
```promql
# 降级率（5分钟窗口）
rate(gogroutine_fallback_total[5m])

# panic 率
rate(gogroutine_panic_total[5m])

# 平均任务耗时
histogram_quantile(0.95, rate(gogroutine_task_duration_seconds_bucket[5m]))
```

---

### 案例八：优雅关闭（main 函数）

> 场景：服务启动时初始化协程池，退出时等待所有任务完成。

```go
func main() {
    // 1. 初始化协程池（高并发场景推荐配置）
    gogroutine.Init(
        gogroutine.WithPoolSize(2000),
        gogroutine.WithPreAlloc(true),        // 预分配，减少 GC
        gogroutine.WithDisablePurge(true),    // 保留空闲 worker，应对突发流量
    )

    // 2. 注入 Prometheus 监控
    gogroutine.SetMetrics(NewGogroutineMetrics())

    // 3. 启动 HTTP 服务
    srv := &http.Server{Addr: ":8080", Handler: router}
    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("listen: %v", err)
        }
    }()

    // 4. 等待中断信号
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("shutting down...")

    // 5. 先关闭 HTTP 服务（拒绝新请求）
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        log.Printf("http shutdown: %v", err)
    }

    // 6. 再等待协程池任务完成（10 秒超时）
    gogroutine.ReleaseAndWaitWithTimeout(10 * time.Second)
    log.Println("server exited")
}
```

**要点**：
- 关闭顺序：HTTP → 协程池，确保新请求不会继续提交任务
- `ReleaseAndWaitWithTimeout` 防止无限阻塞
- `WithDisablePurge(true)` 保留 worker 减少启动延迟

---

### 案例九：健康检查暴露协程池状态

> 场景：Kubernetes 就绪探针需要检查协程池健康状态。

```go
func HealthCheckHandler(c *gin.Context) {
    s := gogroutine.PoolStats()

    // 协程池满时标记为不健康
    if gogroutine.IsFull() {
        c.JSON(503, gin.H{
            "status":  "degraded",
            "running":  s.Running,
            "waiting":  s.Waiting,
            "cap":      s.Cap,
            "panic":    s.Panic,
            "fallback": s.Fallback,
        })
        return
    }

    c.JSON(200, gin.H{
        "status":  "ok",
        "running":  s.Running,
        "waiting":  s.Waiting,
        "cap":      s.Cap,
        "success":  s.Success,
        "panic":    s.Panic,
        "fallback": s.Fallback,
    })
}
```

---

### 案例十：定时任务批量通知

> 场景：每天批量发送通知，带 ctx 控制 + panic 隔离。

```go
func DailyNotification(ctx context.Context) {
    users, err := svc.GetActiveUsers(ctx)
    if err != nil {
        logger.ErrorWithCtx(ctx, "get users failed", logger.Err(err))
        return
    }

    // 为每个用户创建独立任务
    for _, user := range users {
        u := user // 显式捕获循环变量
        gogroutine.GoWithName(ctx, "dailyNotify", func() {
            // panic 隔离：单个用户的通知失败不影响其他人
            if err := sendNotification(ctx, u); err != nil {
                logger.WarnWithCtx(ctx, "notification failed",
                    logger.Int64("user_id", u.ID),
                    logger.Err(err),
                )
            }
        })
    }

    logger.InfoWithCtx(ctx, "daily notification submitted",
        logger.Int("count", len(users)),
    )
}
```

**要点**：
- 单个任务 panic 不会影响其他用户的任务
- 默认池大小 1000 足以应对大多数通知场景
- 命名 `dailyNotify` 方便在监控中聚合

---

## 文件结构

```
gogroutine/
├── groutine.go       # 核心 API：Go/GoWithName/GoBatch + 生命周期管理
├── pool.go           # Pool 接口 + ants 实现 + 全局池管理
├── options.go        # 配置项：poolConfig + Option 函数
├── executor.go       # 任务执行器：panic 恢复 + 指标 + 链路追踪
├── metrics.go        # Metrics 接口 + 原子计数器
└── README.md         # 本文档
```

### 职责划分

| 文件 | 职责 | 对外导出 |
|------|------|----------|
| `groutine.go` | 任务提交 API、批量辅助、生命周期管理 | `Go`, `GoWithName`, `GoWithTimeout`, `GoWithDeadline`, `GoWithCancel`, `GoBatch`, `GoBatchWithName`, `GoBatchWithResult`, `Init`, `Release`, `ReleaseAndWait`, `PoolStats` |
| `pool.go` | 协程池抽象和实现 | `Pool` 接口 |
| `options.go` | 配置选项 | `Option`, `WithPoolSize`, `WithNonBlocking`, `WithPreAlloc`, `WithDisablePurge` |
| `executor.go` | 任务执行统一入口 | 无（内部使用） |
| `metrics.go` | 指标监控接口 | `Metrics` 接口, `SetMetrics` |

## 架构设计

### 任务执行流程

```
Go/GoWithName(ctx, name, task)
  │
  ├─ 1. ctx 已取消？ → 跳过（不入队）
  │
  ├─ 2. 初始化全局池（懒加载，sync.Once 保证并发安全）
  │
  ├─ 3. 提交到协程池
  │     └─ 成功 → 由 executeTask 执行
  │     └─ 失败（池满）→ 降级为原生 goroutine
  │
  └─ 4. executeTask 执行流程：
        ├─ 创建 OpenTelemetry Span
        ├─ IncRunning 指标
        ├─ 执行 task()
        ├─ DecRunning + ObserveTaskDuration
        ├─ 记录 Span 耗时
        └─ panic 恢复 → 记录日志 + 链路错误 + panicCount++
```

### 关键设计决策

| 设计 | 理由 |
|------|------|
| `Pool` 接口抽象 | 支持 mock 测试、多实例管理 |
| `sync.RWMutex` 保护池引用 | Release 后置 nil 防 use-after-close |
| context 前置校验 | ctx 已取消的任务不入队，节省资源 |
| 池满自动降级 | 保证任务不丢失，降级日志可监控 |
| `atomic.Int64` 计数器 | 高并发无锁计数（项目公约十二） |
| Option 模式配置 | 灵活组合，遵循项目公约十五 |
| 任务显式捕获循环变量 | 兼容 Go <1.22（项目公约十一） |

## 常量

| 常量 | 值 | 说明 |
|------|----|------|
| `DefaultPoolSize` | 1000 | 默认池大小 |
| `MinPoolSize` | 10 | 最小池大小（normalize 下限） |
| `MaxPoolSize` | 10000 | 最大池大小（normalize 上限） |

## 注意事项

1. **Init 可选**：不调用 `Init()` 时，首次提交任务会自动使用默认配置创建池
2. **Init 幂等**：多次调用 `Init()` 只有首次生效（`sync.Once`）
3. **panic 安全**：任务内的 panic 不会崩溃程序，自动恢复并记录日志
4. **池满不阻塞**：默认阻塞模式下池满会自动降级为原生 goroutine；非阻塞模式下返回错误
5. **资源释放**：程序退出前应调用 `ReleaseAndWait()` 等待任务完成
6. **链路追踪**：每个任务自动创建 OpenTelemetry Span，SpanKind 为 Internal
