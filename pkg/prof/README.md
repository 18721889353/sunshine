## prof

封装官方 `net/http/pprof` 的 HTTP 路由注册和运行时 profiling 采样能力，提供大厂标准化的性能分析工具集。

<br>

### 功能特性

- **HTTP 实时分析**：将 pprof 路由注册到 `http.ServeMux`，支持鉴权中间件保护生产环境
- **信号触发采样**：通过系统信号动态开关采样，按需采集 profile 文件，不影响正常服务性能
- **IO 等待时间分析**：集成 fgprof，额外采集 IO 等待时间，适合 IO 密集型服务
- **丰富 profile 类型**：CPU、Memory、Goroutine、Block、Mutex、ThreadCreate、Trace
- **Options 模式配置**：采样时长、输出目录、trace 开关、错误处理均可定制

<br>

---

### 一、HTTP 方式实时分析

#### 1.1 基础用法

将 pprof 路由注册到标准 `http.ServeMux`，通过浏览器或 `go tool pprof` 实时查看。

```go
package main

import (
    "net/http"
    "time"

    "github.com/18721889353/sunshine/pkg/prof"
)

func main() {
    mux := http.NewServeMux()
    prof.Register(mux)

    // 访问 http://localhost:8080/debug/pprof/ 即可查看
    httpServer := &http.Server{
        Addr:    ":8080",
        Handler: mux,
    }

    if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        panic(err)
    }
}
```

<br>

#### 1.2 自定义路由前缀

```go
prof.Register(mux, prof.WithPrefix("/my-pprof"))

// 访问 http://localhost:8080/my-pprof/ 查看 pprof 首页
// 访问 http://localhost:8080/my-pprof/heap 查看堆内存
```

<br>

#### 1.3 启用 IO 等待时间分析

开启 fgprof 支持，采集标准 pprof 不包含的 IO 等待时间，适合分析 IO 密集型服务（如网络、磁盘操作频繁的应用）。

```go
prof.Register(mux,
    prof.WithPrefix("/debug/pprof"),
    prof.WithIOWaitTime(),
)

// 额外增加 /debug/pprof/profile-io 路由
// 使用: go tool pprof -http=:8081 http://localhost:8080/debug/pprof/profile-io
```

<br>

#### 1.4 生产环境鉴权保护

pprof 可能暴露敏感信息（源码路径、goroutine 堆栈、环境变量等），生产环境务必添加鉴权。

```go
package main

import (
    "net/http"

    "github.com/18721889353/sunshine/pkg/prof"
)

// simpleTokenAuth 简单的 Token 鉴权中间件
func simpleTokenAuth(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("X-Auth-Token") != "my-secret-token" {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
}

func main() {
    mux := http.NewServeMux()
    prof.Register(mux,
        prof.WithPrefix("/debug/pprof"),
        prof.WithIOWaitTime(),
        prof.WithAuth(simpleTokenAuth),
    )

    http.ListenAndServe(":8080", mux)
}

// 使用方式:
//   curl -H "X-Auth-Token: my-secret-token" http://localhost:8080/debug/pprof/heap
```

<br>

#### 1.5 结合 go tool pprof 分析

```bash
# 查看堆内存（交互式）
go tool pprof http://localhost:8080/debug/pprof/heap

# 查看 CPU 使用（采样 30 秒）
go tool pprof http://localhost:8080/debug/pprof/profile

# 查看协程堆栈
go tool pprof http://localhost:8080/debug/pprof/goroutine

# 查看互斥锁
go tool pprof http://localhost:8080/debug/pprof/mutex

# 查看阻塞
go tool pprof http://localhost:8080/debug/pprof/block

# 查看内存分配（含历史）
go tool pprof http://localhost:8080/debug/pprof/allocs

# Web 界面分析
go tool pprof -http=:8081 http://localhost:8080/debug/pprof/heap
```

<br>

---

### 二、信号触发离线采样

适用于生产环境：平时不采样，通过发送系统信号按需开启，采样完成后自动保存文件到磁盘。

#### 2.1 基础用法（默认配置）

```go
package main

import (
    "os"
    "os/signal"
    "syscall"

    "github.com/18721889353/sunshine/pkg/prof"
)

func main() {
    p := prof.NewProfile()

    signals := make(chan os.Signal, 1)
    signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGTRAP)

    for {
        v := <-signals
        switch v {
        case syscall.SIGTRAP:
            // 开关式：第一次开始采样，第二次停止采样
            p.StartOrStop()

        case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP:
            // 清理 profile 文件后退出
            p.Cleanup()
            os.Exit(0)
        }
    }
}
```

```bash
# 查看服务 PID
ps aux | grep 服务名称

# 发送 SIGTRAP 信号开启采样（默认采样 60 秒）
kill -TRAP <pid>

# 再次发送 SIGTRAP 信号可提前停止采样
kill -TRAP <pid>

# 采样文件保存到 /tmp/<服务名>_profile/ 目录
# ls /tmp/<服务名>_profile/
# 20260721T150405_12345_服务名_cpu.out
# 20260721T150405_12345_服务名_mem.out
# 20260721T150405_12345_服务名_goroutine.out
```

<br>

#### 2.2 自定义采样配置

通过 ProfileOption 定制采样时长、输出目录、trace 开关、错误处理等。

```go
package main

import (
    "log"
    "os"
    "os/signal"
    "path/filepath"
    "syscall"

    "github.com/18721889353/sunshine/pkg/prof"
)

func main() {
    // 自定义输出目录
    outputDir := filepath.Join("/data", "profiles")

    p := prof.NewProfile(
        prof.WithProfileDuration(30),            // 采样时长 30 秒（默认 60 秒）
        prof.WithProfileTrace(true),              // 启用 trace 采样（默认关闭）
        prof.WithProfileOutputDir(outputDir),     // 输出到 /data/profiles/
        prof.WithProfileErrorHandler(func(err error) {
            log.Printf("[profile] 采样错误: %v", err)
        }),
    )

    signals := make(chan os.Signal, 1)
    signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGTRAP)

    for {
        v := <-signals
        switch v {
        case syscall.SIGTRAP:
            p.StartOrStop()
            // 停止后可立即获取文件列表进行分析
            for _, f := range p.Files() {
                log.Printf("profile 文件: %s", f)
            }

        case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP:
            p.Cleanup() // 退出时清理 profile 文件
            os.Exit(0)
        }
    }
}
```

<br>

#### 2.3 采集文件管理与清理

`Files()` 返回只读文件列表，`Cleanup()` 清理磁盘文件。

```go
p := prof.NewProfile(prof.WithProfileDuration(10))

// 开始采样
p.StartOrStop()
time.Sleep(5 * time.Second)
// 手动停止
p.StartOrStop()

// 获取生成的文件列表（只读副本，不影响内部状态）
files := p.Files()
for _, f := range files {
    fmt.Printf("采样文件: %s\n", f)
}

// 分析完成后清理文件
p.Cleanup()
// 此时 Files() 返回空列表，磁盘文件已删除
```

<br>

#### 2.4 定时自动采样

结合 cron 定时任务，定期采集 profile 用于性能基线对比。

```go
package main

import (
    "log"
    "time"

    "github.com/18721889353/sunshine/pkg/prof"
)

func collectProfile() {
    p := prof.NewProfile(
        prof.WithProfileDuration(10),
        prof.WithProfileOutputDir("/tmp/periodic_profiles"),
    )

    p.StartOrStop()
    // 等待采样完成（或者手动停止）
    time.Sleep(12 * time.Second)

    log.Printf("采集完成，文件: %v", p.Files())
    // 可在此上传文件到对象存储等
    // p.Cleanup() // 根据需要决定是否清理
}

func main() {
    // 每小时的 05 分钟采集一次
    for {
        now := time.Now()
        next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 5, 0, 0, now.Location())
        if next.Before(now) {
            next = next.Add(time.Hour)
        }
        time.Sleep(time.Until(next))
        collectProfile()
    }
}
```

<br>

---

### 三、框架集成（Sunshine 微服务框架）

> prof 包已深度集成到 Sunshine 框架中，使用 `sunshine` CLI 工具创建的项目默认支持性能分析。
> HTTP 服务通过 `pkg/gin/prof` 注册 pprof 路由，gRPC 服务通过 `pkg/prof` 将 pprof 挂载到内置 HTTP mux。

#### 3.1 YAML 配置驱动

通过 `configs/serverNameExample.yml` 中的 `enableHTTPProfile` 控制是否启用 pprof 路由：

```yaml
app:
  enableHTTPProfile: true    # 是否开启性能分析, true:开启, false:关闭
  enableStat: false          # 是否开启资源统计（用于自适应采集）
```

> [!tip] `http.timeout` 若设置了非零值，需确保大于采样时长（如设为 `0` 或 `> 60`），
> 否则 Gin 超时中间件可能在采样完成前中断请求。

<br>

#### 3.2 HTTP 服务集成（Gin 引擎）

在 `internal/routers/routers.go` 的 `NewRouter()` 中，通过 `pkg/gin/prof` 包将 pprof 路由注册到 Gin 引擎：

```go
import "github.com/18721889353/sunshine/pkg/gin/prof"

func NewRouter() *gin.Engine {
    r := gin.New()
    // ... 中间件链 ...

    // profile performance analysis
    if cfg.App.EnableHTTPProfile {
        prof.Register(r, prof.WithIOWaitTime())
    }

    // ... 业务路由 ...
    return r
}
```

注册后的访问地址：
- **HTTP 服务**：`http://localhost:8001/debug/pprof/`（端口由 `http.port` 配置）
- **PB 路由模式**：`http://localhost:8001/debug/pprof/`（通过 `NewRouter_pbExample()`）

<br>

#### 3.3 gRPC 服务集成

gRPC 服务内置独立的 HTTP mux 用于暴露 pprof 和指标接口（端口由 `grpc.httpPort` 配置）：

```go
import "github.com/18721889353/sunshine/pkg/prof"

func (s *grpcServer) registerProfMux() {
    if s.mux == nil {
        s.mux = http.NewServeMux()
    }
    prof.Register(s.mux, prof.WithIOWaitTime())
}

func NewGRPCServer(addr string, opts ...GrpcOption) app.IServer {
    // ...
    s.addHTTPRouter()               // 注册错误码、配置等路由
    if config.Get().App.EnableHTTPProfile {
        s.registerProfMux()         // 注册 pprof 路由
    }
    // ...
}
```

访问地址：`http://localhost:6001/debug/pprof/`（端口由 `grpc.httpPort` 配置，默认 6001）

> [!note] gRPC 与 HTTP 混合服务架构中，pprof 统一由 gRPC 端在独立端口暴露，HTTP 端不重复注册。

<br>

#### 3.4 自适应采集集成

Sunshine 框架的 `internal/stat` 包会定期采集 CPU 和内存使用率，当满足告警条件时自动触发 prof 采样：

```yaml
app:
  enableStat: true           # 启用资源统计（自适应采集的前提）
  enableHTTPProfile: true    # 启用 pprof 路由
```

告警阈值（默认）：
- CPU 使用率连续 3 次（每分钟一次）平均超过 80%
- 物理内存使用率连续 3 次（每分钟一次）平均超过 80%
- 持续超过阈值后默认间隔 15 分钟再次告警

触发告警时，框架内部调用 `kill -TRAP <pid>` 采集 profile，文件保存到 `/tmp/<服务名>_profile/`。
即使半夜发生资源异常，第二天仍可通过分析 profile 文件定位根因。

> [!warning] 自适应采集在 Windows 环境不受支持。

<br>

#### 3.5 代码生成模板自动集成

使用 `sunshine` CLI 创建新服务时，prof 集成代码由模板自动生成：

```bash
# 创建 HTTP 服务（自动集成 pprof）
sunshine new http-serverNameExample

# 创建 gRPC + HTTP 混合服务（自动集成 pprof 到 gRPC 端）
sunshine new grpc-http-serverNameExample
```

生成的服务代码中：
- `internal/routers/routers.go` → 通过 `pkg/gin/prof` 注册 Gin 路由
- `internal/server/grpc.go` → 通过 `pkg/prof` 注册 mux 路由
- `configs/serverNameExample.yml` → 自动包含 `enableHTTPProfile: true`

开发者只需确保 YAML 中 `enableHTTPProfile: true`，无需手动编写任何 prof 相关代码。

---

### 四、离线文件分析

获得 profile 离线文件后，使用 pprof 工具分析：

```bash
# 交互式分析
go tool pprof /tmp/myapp_profile/20260721T150405_12345_myapp_cpu.out

# Web 界面分析
go tool pprof -http=:8081 /tmp/myapp_profile/20260721T150405_12345_myapp_mem.out

# 对比两次采样（如升级前后的内存变化）
go tool pprof -http=:8081 \
    --base /tmp/myapp_profile/20260721T150405_12345_myapp_mem_baseline.out \
    /tmp/myapp_profile/20260721T150405_12345_myapp_mem.out
```

<br>

---

### 五、API 参考

#### HTTP 注册

| 函数/选项 | 说明 |
|-----------|------|
| `Register(mux, opts...)` | 将 pprof 路由注册到 `http.ServeMux` |
| `WithPrefix(prefix)` | 自定义路由前缀，默认 `/debug/pprof` |
| `WithIOWaitTime()` | 启用 fgprof IO 等待时间分析 |
| `WithAuth(authFn)` | 添加鉴权中间件保护生产环境 |

#### 信号采样

| 函数/选项 | 说明 |
|-----------|------|
| `NewProfile(opts...)` | 创建 Profile 采样器 |
| `StartOrStop()` | 开关式启动/停止采样 |
| `Files()` | 获取本次采样生成的文件列表（只读） |
| `Cleanup()` | 删除本次采样产生的所有文件 |
| `WithProfileDuration(sec)` | 设置采样时长，默认 60 秒 |
| `WithProfileTrace(enabled)` | 启用/禁用 trace 采样 |
| `WithProfileOutputDir(dir)` | 设置输出目录 |
| `WithProfileErrorHandler(fn)` | 设置错误处理回调 |

<br>

### 六、路由一览

以默认前缀 `/debug/pprof` 为例：

| 路径 | 说明 | 采样时长 |
|------|------|----------|
| `/debug/pprof/` | pprof 首页 | - |
| `/debug/pprof/cmdline` | 命令行参数 | - |
| `/debug/pprof/profile` | CPU profile | 30 秒 |
| `/debug/pprof/profile-io` | IO 等待时间 | 需 `WithIOWaitTime` |
| `/debug/pprof/symbol` | 符号查询 | - |
| `/debug/pprof/trace` | 执行轨迹 | 1 秒 |
| `/debug/pprof/allocs` | 内存分配（历史） | 即时 |
| `/debug/pprof/heap` | 堆内存 | 即时 |
| `/debug/pprof/goroutine` | 协程堆栈 | 即时 |
| `/debug/pprof/threadcreate` | 线程创建 | 即时 |
| `/debug/pprof/block` | 阻塞分析 | 即时 |
| `/debug/pprof/mutex` | 互斥锁 | 即时 |
