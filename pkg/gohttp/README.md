# gohttp

基于 `go-resty/resty` 的 HTTP 客户端封装：连接池调优、指数退避重试、熔断器、SSRF 双层防护、请求体上限、TLS/mTLS、OpenTelemetry 追踪与 W3C 上下文传播，构造期快速失败。

> **破坏性变更提示**：`New` 签名已由 `New(opts ...Option) *Client` 改为 `New(opts ...Option) (*Client, error)`，
> 配置非法（负超时、证书缺失、nil transport 等）在构造期返回错误。升级时所有调用点必须改为双返回值。
> 变更详情与迁移方式见 [CHANGELOG.md](CHANGELOG.md)。

## 架构概览

```
pkg/gohttp/
├── gohttp.go        # Client 构造（New → apply → validate）、请求执行 do 流程、重试条件、动态配置、连接池统计
├── option.go        # Option 类型与 13 个 With* 选项；options 暂存结构（选项只写暂存，New 统一校验后应用）
├── tracing.go       # OpenTelemetry Span 生命周期、W3C traceparent 注入、http.url 脱敏、request_id 属性
├── security.go      # SSRF 双层防护（拨号层直拨已校验 IP）、熔断器三态状态机、URL 脱敏 redactURL
└── *_test.go        # 与上述源文件一对一的测试 + benchmark/fuzz/integration/example/test_helpers
```

**核心设计原则**：

- **构造期快速失败**：选项全部写入 `options` 暂存结构，`New` 内 `validate` 统一校验后再装配 resty
  （对齐 `pkg/goredis` Init+validate、`pkg/nacoscli` NewConfigClient 的模式），非法配置不进运行期。
- **拦截点上 `req.RawRequest` 恒为 nil**：resty 的用户中间件（udBeforeRequest）先于内置
  `parseRequestURL/createHTTPRequest` 执行，因此体积上限读 `req.Body` 类型估算、URL 属性用
  `resolveRequestURL` 拼 BaseURL（证据见 `checkSizeLimit` 与 `resolveRequestURL` 注释）。
- **安全防护放在拨号层**：SSRF 校验完成后直拨已校验的 IP，不留「解析→拨号」之间的 DNS 重绑定窗口。
- **敏感参数不进日志与 APM**：transport 错误消息与 `http.url` Span 属性统一过 `redactURL`。
- **熔断与重试分工**：重试消化瞬时抖动（网络故障 + 502/503/504/429），熔断阻止持续故障继续打下游。

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| HTTP 客户端构造 / 请求执行 / 重试 / 熔断 | 是 | 本包 |
| URL 字面量校验（协议白名单 + 内网 IP 拦截） | 是 | 本包 `ValidateURL` |
| 域名指向内网的攻击拦截（DNS 解析后校验、拨号层直拨） | 是 | 本包 `WithSSRFProtection` |
| 服务端 HTTP 路由 / 中间件 | 否 | `pkg/gin`、`pkg/grpc` |
| 分布式链路的 Span 采样与导出（TracerProvider 配置） | 否 | 应用启动层，经 `WithTracerProvider` 注入 |
| 日志管道 | 否 | `pkg/logger`（本包只产出字段） |

复核命令：`grep -rn "http.Handle\|gin.Engine" pkg/gohttp/`（应无结果）。

## 使用场景选择

### 场景一：基础请求（`New` + `Request`）

**适用场景**：调用固定 base URL 的内部服务，绝大多数业务代码从这里开始。

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/18721889353/sunshine/pkg/gohttp"
)

func main() {
    client, err := gohttp.New(
        gohttp.WithBaseURL("https://api.example.com"),
        gohttp.WithTimeout(10*time.Second),
        gohttp.WithRetry(3, 1*time.Second, 5*time.Second),
    )
    if err != nil {
        log.Fatalf("构造失败（配置错误应在此暴露）: %v", err)
    }
    defer client.Close()

    ctx := context.Background()
    resp, err := client.Request(ctx).Get("/users/123")
    if err != nil {
        log.Fatalf("请求失败: %v", err)
    }
    fmt.Printf("status=%d time=%v body=%s\n", resp.StatusCode(), resp.ResponseTime(), resp.String())
}
```

**内部行为**：

1. `New` 按「暂存选项 → validate → 装配 resty」顺序执行，任一校验失败返回 `(*Client, nil 以外的错误)`。
2. `Request(ctx)` 返回构建器；`ctx` 传 `nil` 时自动回落 `context.Background()`。
3. `do` 流程：SSRF 拦截 → 请求体上限 → 熔断器放行判定 → resty 执行（重试由 resty 条件触发）→ 结果包装。

**注意**：客户端持有连接池，应作为全局单例复用并在进程退出时 `Close()`；不要每请求新建。

---

### 场景二：调用外部不可信地址（`WithSSRFProtection` + `WithRequestSizeLimit`）

**适用场景**：URL 来自用户输入或第三方回调（webhook、回调地址、富文本里的图片地址）。

```go
client, err := gohttp.New(
    gohttp.WithBaseURL("https://api.example.com"),
    gohttp.WithSSRFProtection(),          // 拨号层拦截内网地址
    gohttp.WithRequestSizeLimit(1<<20),   // 请求体超过 1MiB 拒绝发送
)
if err != nil {
    log.Fatalf("构造失败: %v", err)
}
defer client.Close()

// 字面量校验做第一道闸（协议白名单 + IP 字面量内网拦截）
if err := gohttp.ValidateURL(userInputURL); err != nil {
    log.Printf("URL 被拒绝: %v", err)
    return
}
```

**内部行为**：

1. `ValidateURL` 只做**语法与字面量**校验：非 http/https 协议拒绝、内网 IP 字面量拒绝；不发起 DNS。
2. `WithSSRFProtection` 是拨号层防护：IP 字面量直接拦截；域名先解析、逐个校验解析结果，全部公网才直拨已校验 IP。
3. 两者互补不能互相替代——域名指向内网的攻击只有拨号层能拦，字面量校验则在建连前就快速拒绝。

**注意**：确实要访问内网微服务时**不要**启用 `WithSSRFProtection`（会被自己拦住）；改为不启用并对
内网调用走独立客户端。

---

### 场景三：高可用第三方调用（`WithCircuitBreaker` + `WithRetry`）

**适用场景**：依赖不稳定的第三方 API，既要消化瞬时抖动，也要在持续故障时快速失败。

```go
client, err := gohttp.New(
    gohttp.WithBaseURL("https://third-party-api.com"),
    gohttp.WithTimeout(5*time.Second),
    gohttp.WithRetry(3, 1*time.Second, 5*time.Second), // 先重试消化抖动
    gohttp.WithCircuitBreaker(5),                      // 连续 5 次失败后熔断
)
if err != nil {
    log.Fatalf("构造失败: %v", err)
}
defer client.Close()

resp, err := client.Request(ctx).Post("/orders", payload)
if err != nil {
    if errors.Is(err, gohttp.ErrCircuitBreakerOpen) {
        log.Println("熔断器已打开，请求被本地拒绝（未发出）")
        return
    }
    log.Fatalf("请求失败: %v", err)
}
```

**内部行为**：

1. **重试条件**：仅物理网络故障与 `502/503/504/429` 触发指数退避（`gohttp.go` 的 `AddRetryCondition`）；4xx 不重试。
2. **熔断口径**：一次 `do` 计一次；本地拒绝（熔断打开 / 体积超限 / ctx 取消）不计入；4xx 记成功（下游可达，故障在调用方）；transport 错误与 5xx 记失败。
3. **三态状态机**：连续 `threshold` 次失败 → `open`（拒绝一切请求）→ 冷却 10s → `halfOpen`（仅放行 1 个探测）→ 探测成功回 `closed`、失败重回 `open`。

**注意**：重试与熔断叠加时，计数以最终结果为准（重试耗尽仍失败才记一次失败）。

---

### 场景四：TLS/mTLS 与自签证书

**适用场景**：访问内部服务需要双向认证，或开发环境用自签证书。

```go
// 生产：自签 CA + 客户端证书（mTLS）
client, err := gohttp.New(
    gohttp.WithBaseURL("https://secure-api.internal"),
    gohttp.WithRootCA("/path/ca.crt"),               // 与系统根证书池合并，不覆盖系统信任链
    gohttp.WithClientCert("/path/client.crt", "/path/client.key"),
)

// 开发环境：跳过证书验证（仅限开发/测试）
devClient, err := gohttp.New(
    gohttp.WithBaseURL("https://self-signed.local"),
    gohttp.WithInsecureSkipVerify(),
)
```

**内部行为**：证书文件缺失、证书与私钥不匹配、CA 无有效 PEM 时，`New` 直接返回错误（不再打 stderr 后静默继续）。

**注意**：`client.UpdateInsecureSkipVerify(bool)` 可运行时切换，但直接修改 TLS 配置结构，
**应在无在途请求时调用**（启动阶段或排空后），与在途请求并发存在数据竞争。

---

### 场景五：OpenTelemetry 追踪与 request_id 传播

**适用场景**：微服务链路中需要自动创建 HTTP Span、注入 W3C `traceparent`、并把 request_id 带进 Span 属性。

```go
// 通常在应用启动时初始化 TracerProvider 后注入
client, err := gohttp.New(
    gohttp.WithBaseURL("https://api.example.com"),
    gohttp.WithTracerProvider(tp), // 不注入时默认用全局 otel.Tracer("gohttp")
)

// ctx 里已有父 Span 时自动成为其子 Span，并自动注入 traceparent 请求头
ctx, span := tracer.Start(context.Background(), "order.Create")
defer span.End()
resp, err := client.Request(ctx).Post("/orders", body)
```

**内部行为**：

1. `WithTracerProvider(nil)` 或 typed nil 回退全局 provider；未注入时使用全局 `otel.Tracer("gohttp")`。
2. 全局默认是 noop provider——产无效 SpanContext 时 W3C propagator **不会**注入 `traceparent`（非缺陷，见 `tracing_test.go` 的注释）。
3. request_id 直接读全仓约定的 `logger.ContextKeyRequestID`，作为 Span 属性上报。
4. `http.url` 属性经 `redactURL` 脱敏，敏感 query 参数不进入 APM。

**注意**：`gohttp` 全局默认 provider 产的 Span 不会导出；要拿到真实 Span 必须注入已配置的 provider（如 `WithTracerProvider(tp)`）。

## 参数/结构体说明

### ErrorResponse（非 2xx 的强类型包装）

| 字段 | 类型 | 说明 |
|------|------|------|
| `StatusCode` | `int` | HTTP 状态码 |
| `Message` | `string` | 服务端返回的错误信息 |
| `Body` | `[]byte` | 原始响应体 |

### 默认值常量

| 常量 | 值 | 说明 |
|------|----|------|
| `DefaultTimeout` | `10s` | 未设置 `WithTimeout` 时的请求超时 |
| `DefaultMaxRetries` | `3` | 未设置 `WithRetry` 时的重试次数 |
| `DefaultMinRetryWait` / `DefaultMaxRetryWait` | `1s` / `5s` | 退避等待区间 |
| `DefaultMaxConnsPerHost` | `100` | 单主机最大连接数（标准库默认仅 2） |
| `DefaultMaxIdleConns` | `500` | 全局最大空闲连接 |
| `DefaultDNSCacheTTL` | `5m` | DNS 缓存时间 |

## Option 列表

| Option | 说明 | 默认值 | 非法输入的构造期行为 |
|--------|------|--------|----------------------|
| `WithBaseURL(string)` | 基础 URL | 空 | 空串 → `ErrEmptyBaseURL`（仅在需要它的场景下报错） |
| `WithTimeout(time.Duration)` | 请求超时 | `10s` | 非正数 → 返回错误 |
| `WithRetry(count, minWait, maxWait)` | 退避重试 | `3, 1s, 5s` | `count<0` 或 `minWait>maxWait` → 返回错误 |
| `WithCircuitBreaker(threshold)` | 熔断阈值 | 关闭 | 非正数 → 返回错误 |
| `WithRequestSizeLimit(int64)` | 请求体上限（字节） | 不限制 | 非正数 → 返回错误 |
| `WithSSRFProtection()` | 拨号层 SSRF 双层防护 | 关闭 | — |
| `WithRootCA(path)` | 注入自签/私有 CA（与系统池合并） | 无 | 文件缺失/无有效 PEM → 返回错误 |
| `WithClientCert(cert, key)` | 客户端证书（mTLS） | 无 | 文件缺失/不匹配 → 返回错误 |
| `WithInsecureSkipVerify()` | 跳过证书验证（仅开发/测试） | 关闭 | — |
| `WithTransport(*http.Transport)` | 自定义传输层 | 内置调优值 | `nil` → 返回错误；实例会被就地配置，勿跨 Client 共享 |
| `WithProxy(url)` | 代理（空串=不使用） | 无 | 非 `http/https/socks5/socks5h` 协议 → 返回错误 |
| `WithDebug(bool)` | resty 调试输出（打 stdout） | 关闭 | 生产禁用 |
| `WithTracerProvider(oteltrace.TracerProvider)` | 注入自定义 TracerProvider | 全局 provider | nil / typed nil → 回退全局，不报错 |

**Option 赋值语义**：同一 Option 传多次时**最后赋值胜出**（与 `pkg/nacoscli` 的约定一致）。

## API 速查

### New — 构造期快速失败

```go
func New(opts ...Option) (*Client, error)
```

- 顺序：选项写入暂存结构 → `validate` 校验 → 装配 resty（连接池/TLS/代理/重试条件/SSRF 包装）。
- **校验失败时返回 `nil` 客户端与错误**，此时不要使用第一个返回值。
- 证书 IO、代理解析都在此阶段完成，运行期不再有配置类 IO。

### Client.Request — 请求构建器入口

```go
func (c *Client) Request(ctx context.Context) *Request
```

- `ctx` 为 `nil` 时回落 `context.Background()`。
- 返回的 `*Request` 是一次性构建器，链式设置后调用 `Get/Post/...` 发起请求。

### Client.NewRequestWithValidation — 带 URL 校验的入口

```go
func (c *Client) NewRequestWithValidation(ctx context.Context, _, requestURL string) (*Request, error)
```

- 发起前执行 `ValidateURL(requestURL)`，被拦截时返回 `ErrSSRFBlocked` 或协议错误。
- **注意**：它只做字面量校验；域名指向内网仍需 `WithSSRFProtection`。

### Client.UpdateTimeout / Client.UpdateInsecureSkipVerify — 动态配置

```go
func (c *Client) UpdateTimeout(timeout time.Duration)
func (c *Client) UpdateInsecureSkipVerify(insecure bool)
```

- 运行时调整，无需重建客户端；内部有读写锁保护配置快照。
- **注意**：二者都会触碰 resty 底层结构，**应在无在途请求时调用**（启动阶段或排空后），与在途请求并发存在数据竞争。

### Client.GetConnectionPoolStats — 连接池配置快照

```go
func (c *Client) GetConnectionPoolStats() map[string]int
```

- 返回 `max_idle_conns`、`max_conns_per_host` 等**配置值快照**（并发安全，持读锁），不是实时连接数。

### Client.Close — 优雅关闭

```go
func (c *Client) Close()
```

- 释放全部空闲连接；幂等，可重复调用，适合 `defer`。

### ValidateURL — URL 字面量校验

```go
func ValidateURL(rawURL string) error
```

- 协议白名单（仅 `http/https`）+ 主机名校验 + 内网 IP 字面量拦截；不发起 DNS。
- 被拦截返回 `ErrSSRFBlocked`（或协议类错误），错误文本不含裸换行（防日志注入，由 Fuzz 守护）。

### Request 链式方法

| 方法 | 说明 |
|------|------|
| `SetHeader(k, v)` / `SetHeaders(map)` | 单个 / 批量设置请求头 |
| `SetQueryParam(k, v)` / `SetQueryParams(map)` | 单个 / 批量设置查询参数 |
| `SetBody(interface{})` | 请求体（`[]byte`/`string`/`bytes.Buffer`/struct 均可） |
| `SetResult(interface{})` | 响应自动解析目标 |
| `Get/Post/Put/Delete/Patch(url)` | 发起对应 HTTP 方法请求，返回 `(*Response, error)` |

### Response 响应对象

| 方法 | 说明 |
|------|------|
| `StatusCode() int` | 状态码 |
| `Body() []byte` / `String() string` | 原始响应体 / 字符串 |
| `IsSuccess() bool` | 是否 2xx |
| `JSON(v) error` | 解析 JSON 到结构体 |
| `Header() http.Header` | 响应头 |
| `ResponseTime() time.Duration` | 耗时 |

## 错误处理

| 场景 | 行为 |
|------|------|
| 构造配置非法 | `New` 返回 `(nil, error)`，不产生可用客户端 |
| 非 2xx 响应 | 返回 `*ErrorResponse`（可 `errors.As` 取 `StatusCode/Message/Body`） |
| 熔断器打开 / 半开有探测在途 | 返回包装 `ErrCircuitBreakerOpen` 的错误（可用 `errors.Is`），请求未发出 |
| 请求体超限 | 返回包装 `ErrRequestTooLarge` 的错误，请求未发出 |
| SSRF 拦截 | 返回包装 `ErrSSRFBlocked` 的错误 |
| transport 层失败 | 返回 `*url.Error` 类错误，消息经 `redactURL` 脱敏 |

```go
resp, err := client.Request(ctx).Post("/orders", payload)
if err != nil {
    var httpErr *gohttp.ErrorResponse
    switch {
    case errors.As(err, &httpErr):           // 业务错误：下游可达，拿到状态码
        log.Printf("HTTP %d: %s", httpErr.StatusCode, httpErr.Message)
    case errors.Is(err, gohttp.ErrCircuitBreakerOpen): // 熔断：本地拒绝
        log.Println("依赖服务熔断中")
    case errors.Is(err, gohttp.ErrRequestTooLarge):
        log.Println("请求体超限")
    default:                                  // 网络/配置错误
        log.Printf("transport error: %v", err)
    }
}
```

## 集成测试

集成测试带 `//go:build integration` 构建标签，依赖真实 HTTP 服务，通过环境变量配置。

**环境变量一览**（与 `integration_test.go` 实际读取的变量一一对应）：

| 变量 | 必填 | 用途 | 默认值 |
|------|------|------|--------|
| `GOHTTP_TEST_BASE_URL` | 是 | 被测服务 base URL，未设置时全部集成测试自动 Skip | 无 |
| `GOHTTP_TEST_PATH` | 否 | 拼在 base URL 后的请求路径 | `/` |
| `GOHTTP_TEST_EXPECT_STATUS` | 否 | 期望响应状态码 | `200` |
| `GOHTTP_TEST_TLS_URL` | 否 | 设置后追加真实 HTTPS TLS 握手用例 | 无（该用例 Skip） |
| `GOHTTP_TEST_TIMEOUT` | 否 | `TestMain` 总时长上限（Go duration） | `120s` |

**运行方式**：

```bash
cd pkg/gohttp

# 方式一：显式传环境变量（CI 用法，优先级高于 .env）
GOHTTP_TEST_BASE_URL=https://httpbin.org go test -tags=integration -v -count=1

# 方式二：包内 .env（推荐本地，无需任何插件）
# TestMain 里的 loadDotEnv 自动读取包内 .env 并补齐环境变量，
# 语义是「只补缺、不覆盖已设置的变量」；根 .gitignore 的 *.env 已覆盖该文件，不会入库。
go test -tags=integration -v -count=1

# 只跑某一组（注意：多个用例名之间不要直接用 |，Git Bash 会把 | 当管道）
go test -tags=integration -count=1 -v -run TestIntegration

# 仅单元测试（无需任何环境变量，日常使用）
go test ./pkg/gohttp/ -v
```

**行为说明**：

- 未设置 `GOHTTP_TEST_BASE_URL` 时集成用例自动 Skip，单元测试不受影响
- TCP 探测不可达时 `t.Skipf` 并打印具体 error（区分「没映射」「IP 写错」「防火墙丢包」）
- 状态码与期望不符时 `t.Fatalf`（断言不符是被测代码的问题，不是环境问题）
- `TestMain` 限制总时长（`GOHTTP_TEST_TIMEOUT` 可调），防止网络 goroutine 泄漏导致 `go test` 挂死

**实测结果汇总**（Windows 10 本机 Git Bash，`go test -tags=integration -count=1`，总耗时 1.322s）：

| 分组 | 结果 | 关键读数 |
|------|------|----------|
| `TestIntegration_GetEndpoint` | PASS | `GET https://httpbin.org/get → 200`，耗时 1.0347902s（上一轮读数 1.1954465s，公网 RTT 波动属正常） |
| `TestIntegration_TLSRequest` | SKIP | `GOHTTP_TEST_TLS_URL 未设置`（按设计跳过） |
| `.env` 自动加载 | 生效 | `已从 .env 加载 3 个环境变量（不覆盖已设置的变量）` |

**环境限制记录**：

1. **本机 `-race` 无法取证**：Windows/MinGW 下 `go test -race` 报 `0xc0000139`（本机 gcc 动态库问题）。
   本 README 不声称已跑过 `-race`；真实证据需在 Linux CI 采集（命令见下方「性能基线与模糊测试」）。

## 性能基线与模糊测试

### 测试结构（一对一映射）

| 源文件 | 测试文件 | 覆盖重点 |
|--------|----------|----------|
| `gohttp.go` | `gohttp_test.go` | 请求成功/重试 502/ErrorResponse/Close 幂等/动态配置/panic 恢复 |
| `option.go` | `option_test.go` | 每个 With* 的合法值与非法值快速失败、证书错误 |
| `tracing.go` | `tracing_test.go` | Span 生命周期、scheme 属性、traceparent 注入、request_id |
| `security.go` | `security_test.go` | SSRF 双层拦截、熔断状态机、体积上限回归、脱敏 |

共享 helper 集中在 `test_helpers_test.go`（带 `_test.go` 后缀，避免 `unused` 误报）。
测试命名统一方案 A：`Test<被测方法><场景>`（全英文驼峰标识符 + 中文 doc 注释）。

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 ./pkg/gohttp/
```

> 诚实声明：本 README 不声称已经跑过 `-race`——本机（Windows/MinGW）执行报 `0xc0000139`。
> 真实证据需在 Linux/CI 采集后再回来更新本节。

### 基准测试

```bash
# 全部基准（-run='^$' 跳过常规单测）
go test -run='^$' -bench=. -benchmem ./pkg/gohttp/
```

实测读数（Windows 10，i7-10750H @2.60GHz，12 逻辑核，默认 benchtime 1s，总耗时 16.431s）：

| 基准 | 场景 | ns/op | B/op | allocs/op |
|------|------|------:|-----:|----------:|
| `BenchmarkValidateURL` | 公网域名通过 | 358.4 | 192 | 2 |
| | 内网字面量拦截 | 481.0 | 216 | 5 |
| | 非法协议拒绝 | 429.9 | 240 | 4 |
| `BenchmarkRedactURL` | 无敏感信息 | 2872 | 353 | 6 |
| | 敏感参数命中 | 3335 | 612 | 12 |
| `BenchmarkBodySize` | `[]byte` 已知长度 | 2.325 | 0 | 0 |
| | 未知类型不预知 | 2.495 | 0 | 0 |
| `BenchmarkCircuitBreakerAllow` | 放行判定 | 13.37 | 0 | 0 |
| `BenchmarkRequestIDAttr` | 命中 | 22.37 | 0 | 0 |
| | 未命中 | 10.62 | 0 | 0 |
| `BenchmarkOptionsApplyAndValidate` | 选项应用+校验 | 291.5 | 368 | 2 |
| `BenchmarkNewClient` | 构造（不含证书 IO） | 1872 | 3440 | 35 |

**口径与解读（很重要，否则数字会被误读）：**

- 全部用例是**纯本包函数**（校验/脱敏/估算/熔断状态机/构造），不发起任何网络请求、不含回环 RTT；
  测得的是「代码改动是否让热路径变慢」的回归基线，**不是生产请求延迟**。
- `RedactURL` 的 ~2.9~3.3µs 是正则扫描的代价，只发生在**出错与打 Span** 两条路径上，不在成功请求热路径。
- `NewClient` 的 35 allocs 是构造期一次性成本（进程内通常只发生一次）。
- 稳定指标看 `allocs/op`（0 alloc 的 `BodySize`/`CircuitBreakerAllow`/`RequestIDAttr` 不受频率影响）；
  ns/op 在共享机器上会有波动，对比回归建议固定迭代次数重测。

### 模糊测试

```bash
# 种子语料随常规 go test 一起执行（无需额外命令，耗时可忽略）
go test ./pkg/gohttp/ -run Fuzz

# 真正的挖掘：不进默认流程，按需本地或定时任务跑
go test -run='^$' -fuzz=FuzzRedactURL -fuzztime=30s ./pkg/gohttp/
```

| Target | 守护的不变量 |
|--------|--------------|
| `FuzzRedactURL` | 脱敏后构造的敏感参数对 `name=secret` 必然消失（断言参数对而非孤立值，避免 fuzz 输入自带同名子串时假阳性） |
| `FuzzValidateURL` | 任意输入不 panic；校验通过必然满足「http/https + 非空主机名」；错误文本不含裸换行/回车（防日志注入） |
| `FuzzBodySize` | 任意输入不 panic；判定「已知」时长度非负，且 `[]byte`/`string` 给出精确长度（`checkSizeLimit` 依赖此性质） |

## 相关文档

- [CHANGELOG.md](CHANGELOG.md) — 变更、修复与兼容性说明（含 `New` 签名迁移方式）
- 质量基线规范：`.qoder/skills/package-quality-baseline/SKILL.md`（交付物矩阵、测试命名、证据标准）
