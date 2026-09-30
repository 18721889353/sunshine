# jwt

基于 HMAC 签名的 JWT token 签发、解析与刷新工具包，为 gin/gRPC 鉴权中间件提供登录态校验能力。

## 架构概览

```
pkg/jwt/
├── jwt.go               # 主文件：包注释、Claims 定义、签发/解析/刷新、全局配置原子读写
├── option.go            # 选项模式：Option 类型、默认值、With* 系列设置函数
├── jwt_test.go          # 对应 jwt.go 的单元测试（含并发安全用例）
├── option_test.go       # 对应 option.go 的单元测试
├── benchmark_test.go    # 性能基线（纯 CPU 计算，不含网络 RTT）
├── fuzz_test.go         # 模糊测试（不变量守护）
└── test_helpers_test.go # 跨测试文件共享的测试工具
```

**核心设计原则**：
- **全局单例配置 + 原子读写**：`Init` 与所有签发/解析函数共享一份配置，配置存放在 `atomic.Pointer[options]` 中，
  配置热更新（`internal/config` 的 `reloadJwtConfig`）与请求线程并发解析之间无数据竞争。
- **一套配置、两套 Claims**：`Claims`（uid + name + 自定义字段）面向常规登录态；`CustomClaims`（纯 KV）面向自由字段场景。
- **选项模式**：`defaultOptions` + `apply`，未设置的选项保留默认值，同名选项最后赋值胜出。
- **仅支持 HMAC 系列签名**（HS256 / HS384 / HS512），不支持 RS/ES 非对称算法。
- **注册声明集中构造**：四个签发入口统一走 `buildRegisteredClaims`，避免字段更新遗漏；`nbf` 零值不写入 token。

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| token 签发 / 解析 / 刷新 | 是 | 本包 |
| HTTP 鉴权中间件、忽略路径列表 | 否 | `pkg/gin/middleware`、`pkg/grpc/interceptor` |
| WebSocket 鉴权 | 否 | `pkg/gows` |
| token 黑名单 / 主动吊销 | 否 | 本包不提供，需调用方基于 `pkg/cache` 等自行实现 |
| 密钥轮换（kid 多密钥共存） | 否 | 本包单一密钥配置，不支持 kid；轮换需调用方按部署策略灰度切换（先加新密钥再换签发） |
| 日志与链路追踪 | 否 | `pkg/logger`、`pkg/tracer`（本包纯计算，不打日志、不建 Span） |
| 非对称签名（RS* / ES*） | 否 | 暂不支持，`WithSigningMethod` 仅接受 HMAC 算法 |

复核命令：`grep -rn "logger\." pkg/jwt/*.go`（应无结果，只查 Go 源码，避免命中本文档自身）

## 使用场景选择

> ⚠️ **安全警告**：本包**不提供默认签名密钥**。`Init` 未传 `WithSigningKey`（或只传了空串）时，
> 签发/解析会返回 `ErrSigningKeyNotConfigured`，不会静默降级。生产环境请务必显式传入
> `WithSigningKey`（建议 ≥ 32 字节随机串），并推荐追加 `jwt.RequireSigningKey()`——
> 密钥缺失时 `Init` 直接 panic，让「配置漏读」在启动阶段就暴露，而不是带病上线。

### 场景一：常规登录态 token（GenerateToken / ParseToken）

**适用场景**：登录后为用户签发带 uid、name 的 token，后续请求在鉴权中间件中解析校验。

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/jwt"
)

func main() {
	// 初始化全局配置，进程内调用一次即可
	jwt.Init(
		jwt.WithSigningKey("your-secret-key"),
		jwt.WithExpire(time.Hour),
		jwt.WithIssuer("sunshine"),
	)

	// 签发 token
	token, err := jwt.GenerateToken("10001", "sunshine", map[string]any{"role": "admin"})
	if err != nil {
		log.Fatalf("签发 token 失败: %v", err)
	}

	// 解析 token
	claims, err := jwt.ParseToken(token)
	if err != nil {
		log.Fatalf("解析 token 失败: %v", err)
	}

	// 校验
	if claims.UID != "10001" || claims.Name != "sunshine" {
		log.Fatal("token 校验失败")
	}
	role, ok := claims.Fields["role"]
	if !ok {
		log.Fatal("token 中缺少 role 字段")
	}
	fmt.Printf("uid=%s name=%s role=%v\n", claims.UID, claims.Name, role)
}
```

**内部行为**：

1. `GenerateToken` 读取全局配置：未初始化返回 `ErrNotInitialized`，
   已初始化但未配置密钥返回 `ErrSigningKeyNotConfigured`（fail loud，绝不静默降级）。
2. 组装 `Claims`：uid、name、自定义字段（`kvs` 只取第一个 map，内部浅拷贝后使用，
   不传则 Fields 为空 map）；
   注册声明（exp/iat/iss/sub/aud/jti/nbf）由 `buildRegisteredClaims` 按当前配置统一构造。
3. 用配置的 HMAC 算法签名，返回三段式 token 字符串。
4. `ParseToken` 校验签名与 exp，且只信任当前配置的算法（`WithValidMethods`），
   成功返回 `*Claims`；任何失败路径 claims 均为 nil。

**注意**：`Init` 之后全局生效，多处 `Init` 以最后一次整体生效（配置热更新场景）。

---

### 场景二：自定义字段 token（GenerateCustomToken / ParseCustomToken）

**适用场景**：token 主体是自由 KV 字段（如第三方对接、临时凭证），不需要 uid/name 结构。

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/jwt"
)

func main() {
	jwt.Init(
		jwt.WithSigningKey("your-secret-key"),
		jwt.WithExpire(time.Hour),
	)

	// 签发自定义字段 token
	token, err := jwt.GenerateCustomToken(jwt.KV{"id": 123, "foo": "bar"})
	if err != nil {
		log.Fatalf("签发 token 失败: %v", err)
	}

	// 解析自定义字段 token
	claims, err := jwt.ParseCustomToken(token)
	if err != nil {
		log.Fatalf("解析 token 失败: %v", err)
	}

	// 类型化取值：字段不存在或类型不匹配返回 false
	id, ok := claims.GetInt("id")
	if !ok {
		log.Fatal("token 中缺少 id 字段")
	}
	foo, ok := claims.GetString("foo")
	if !ok {
		log.Fatal("token 中缺少 foo 字段")
	}
	fmt.Printf("id=%d foo=%s\n", id, foo)
}
```

**内部行为**：

1. `GenerateCustomToken` 将 `KV` 浅拷贝后写入 claims 的 `data` 字段，注册声明同场景一。
2. `ParseCustomToken` 校验签名与 exp，成功返回 `*CustomClaims`。
3. 取值走 `Get` / `GetString` / `GetInt` / `GetUint64`，统一返回 `(值, 是否存在且类型匹配)`。

**不适用**：需要固定 uid/name 结构的登录态，请用场景一。

**注意**：数字字段经 token JSON 往返后类型为 `float64`，`GetInt` / `GetUint64` 已做归一化；
内存对象中的 `int` 只被 `GetInt` 识别，`GetUint64` 只识别 `float64` / `uint64`
（有意不对称：避免负 `int` 被无符号读取静默损坏，见 API 速查）。

---

### 场景三：续期刷新（RefreshToken / RefreshCustomToken）

**适用场景**：用户活跃期间对未过期 token 提前续期，避免频繁重新登录。

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/jwt"
)

func main() {
	jwt.Init(
		jwt.WithSigningKey("your-secret-key"),
		jwt.WithExpire(time.Hour),
	)

	token, err := jwt.GenerateToken("10001", "sunshine")
	if err != nil {
		log.Fatalf("签发 token 失败: %v", err)
	}

	// 未过期时提前续期
	newToken, err := jwt.RefreshToken(token)
	if err != nil {
		log.Fatalf("刷新 token 失败: %v", err)
	}

	claims, err := jwt.ParseToken(newToken)
	if err != nil {
		log.Fatalf("解析刷新后的 token 失败: %v", err)
	}
	fmt.Printf("刷新成功 uid=%s 过期时间=%s\n", claims.UID, claims.ExpiresAt.Format(time.RFC3339))
}
```

**内部行为**：

1. 只 Load 一次配置快照，先用它校验原 token（含过期校验）。
2. 校验通过后保留业务字段（uid/name/Fields），用**同一份配置快照**重新构造注册声明并签名，
   有效期从刷新时刻重新计算——校验与签发冻结在同一配置上，
   避免两次 Load 之间配置热更新造成的中间态。
3. 返回新 token 字符串，原 token 不会被作废（无黑名单机制，见职责边界）。

**注意**：已过期的 token 无法刷新（步骤 1 会返回 `ErrTokenExpired`），只支持「未过期时提前续期」。

---

## 核心结构体/类型说明

### Claims — 标准 token 的声明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `UID` | `string` | 否 | 用户 id，来自 `GenerateToken` 第一参数 |
| `Name` | `string` | 否 | 用户名，来自 `GenerateToken` 第二参数 |
| `Fields` | `map[string]any` | 否 | 自定义字段，json 标签 `data`，不传时为空 map |
| `CustomRegisteredClaims` | 嵌入 | — | 注册声明（exp/iat/iss/sub/aud/jti/nbf） |

### CustomRegisteredClaims — 自定义注册声明

| 字段 | 类型 | 说明 |
|------|------|------|
| `RegisteredClaims` | `jwt.RegisteredClaims` | 标准注册声明，由 `buildRegisteredClaims` 统一构造 |
| `Subject` | `any` | `sub` 字段，遮蔽内层同名字段（见下方注意） |

> **注意（易混淆，读 `sub` 必看）**：`CustomRegisteredClaims.Subject`（`any`）与内嵌
> `jwt.RegisteredClaims.Subject`（`string`）的 json 标签同为 `sub`，encoding/json 按「浅层字段优先」
> 只序列化外层的 `any` 字段。这是有意保留的兼容设计：历史 token 的 `sub` 可能是数字等非字符串类型，
> 只有 `any` 能保证旧 token 仍可解析。读取 `sub` 请用 `claims.Subject`；
> `claims.RegisteredClaims.Subject` 始终为空串，不要使用。

### CustomClaims — 自定义字段 token 的声明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `Fields` | `KV` | 否 | 业务字段，json 标签 `data`；`GenerateCustomToken(nil)` 签发的 token 解析后为 nil |
| `CustomRegisteredClaims` | 嵌入 | — | 同上 |

### KV — 自定义字段 map 类型

```go
type KV = map[string]any
```

`map[string]any` 的类型别名，与 `GenerateCustomToken` 参数、`Claims.Fields` 通用。

## Option 列表

所有 Option 通过 `Init` 传入，对全部 API 全局生效（签发决定写入内容，解析决定签名密钥）。

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithSigningKey(key)` | 签名密钥，空串视为未设置 | **无（必须显式设置）** | 全部 |
| `WithSigningMethod(sm)` | 签名算法 `HS256`/`HS384`/`HS512`，传 `nil` 忽略 | `HS256` | 全部 |
| `WithExpire(d)` | 有效期，从签发时刻起算；0/负值有意接受（构造过期 token 的测试手段） | `24h` | 全部 |
| `WithIssuer(s)` | `iss` 字段（仅写入，不校验；校验用 `WithExpectedIssuer`） | 空（不写入） | 全部 |
| `WithSubject(s)` | `sub` 字段 | 空（不写入） | 全部 |
| `WithAudience(list)` | `aud` 字段（仅写入，不校验；校验用 `WithExpectedAudience`），入参切片内部拷贝 | 空（不写入） | 全部 |
| `WithID(id)` | `jti` 字段 | 空（不写入） | 全部 |
| `WithNotBefore(t)` | `nbf` 字段 | 零值（不写入） | 全部 |
| `WithExpectedIssuer(s)` | 解析侧校验 token 的 `iss` 必须等于 s，空串跳过校验 | 空（不校验） | `ParseToken`、`ParseCustomToken`、`RefreshToken`、`RefreshCustomToken` |
| `WithExpectedAudience(list)` | 解析侧校验 token 的 `aud` 声明与期望列表**至少有一个交集**时通过（由 golang-jwt/v5 的 `WithAudience` 定义），空列表跳过校验 | 空（不校验） | `ParseToken`、`ParseCustomToken`、`RefreshToken`、`RefreshCustomToken` |
| `WithLeeway(d)` | 过期/生效时间的时钟偏差容忍（分布式节点对时误差） | `0` | `ParseToken`、`ParseCustomToken`、`RefreshToken`、`RefreshCustomToken` |
| `RequireSigningKey()` | `Init` 时强校验密钥非空，缺失则 panic（fail fast） | 关闭 | `Init` |

> **安全模型**：本包刻意不提供默认密钥——未配置密钥时签发/解析返回 `ErrSigningKeyNotConfigured`，
> 避免「配置漏读静默用公开密钥签发」的漏洞路径。密钥强度建议 ≥ 32 字节随机串。

**Option 赋值语义**：同名选项多次设置时最后赋值胜出；`WithSigningMethod(nil)` 与
`WithSigningKey("")`（空串）是仅有的两个被忽略的取值（保留当前值）。

**签发侧 vs 解析侧**：`WithExpectedIssuer` / `WithExpectedAudience` / `WithLeeway` 只影响解析与刷新的
校验行为，不改变签发内容；其余选项（密钥、算法、注册声明字段）对签发与解析同时生效。

## API 速查

### Init — 初始化全局配置

```go
func Init(opts ...Option)
```

- 进程内调用一次即可；不传任何选项等价于恢复默认配置（含清空密钥）
- 可重复调用（配置热更新），以最后一次调用**整体生效**，调用之间应由调用方保证串行
- **安全校验**：未配置密钥时签发/解析返回 `ErrSigningKeyNotConfigured`；
  传入 `RequireSigningKey()` 而密钥缺失或为空时 `Init` 直接 panic（fail fast）
- **注意**：`Init` 与签发/解析并发执行是安全的（原子读写），但解析中的请求可能读到新配置，
  属预期行为

### GenerateToken — 签发标准 token

```go
func GenerateToken(uid string, name string, kvs ...map[string]any) (string, error)
```

- `kvs` 只取第一个 map 作为附加字段，多余的忽略；不传或传空时 `Fields` 为空 map；
  传入的 map 会先浅拷贝，签发后调用方继续修改原 map 不影响已签发 token
- **并发约定**：内部会遍历传入的 map（Go map 非并发安全），调用方需保证该 map
  在函数返回前不被并发读写；浅拷贝不递归，嵌套引用类型 value 由调用方保证
- 未调用 `Init` 时返回 `ErrNotInitialized`；未配置签名密钥时返回 `ErrSigningKeyNotConfigured`

### ParseToken — 解析并校验标准 token

```go
func ParseToken(tokenString string) (*Claims, error)
```

- 校验签名与 `exp`，且只信任当前配置的签名算法（`WithValidMethods`）；
  token 过期返回 `ErrTokenExpired`
- 配置了 `WithExpectedIssuer` / `WithExpectedAudience` 时校验 `iss` / `aud`
  （不匹配返回 `jwt.ErrTokenInvalidIssuer` / `jwt.ErrTokenInvalidAudience`，
  多租户同密钥场景下的隔离手段）：`iss` 需完全相等，`aud` 为
  **token 的 `aud` 声明与期望列表至少有一个交集时通过**（golang-jwt/v5 `WithAudience` 语义）；
  配置 `WithLeeway` 时按偏差容忍放宽时间校验
- **注意**：任何错误路径返回的 claims 都是 nil，调用方无需二次判空

### RefreshToken — 刷新标准 token

```go
func RefreshToken(tokenString string) (string, error)
```

- 保留 uid/name/Fields，按当前配置重新签发，有效期从刷新时刻重算
- **配置一致性**：校验与重新签发只 Load 一次配置，冻结在同一份快照上，
  无「旧配置校验、新配置签发」的中间态
- **注意**：原 token 必须未过期（内部先解析校验）；原 token 不会被作废

### GenerateCustomToken — 签发自定义字段 token

```go
func GenerateCustomToken(kv KV) (string, error)
```

- 业务数据全部放在 `kv` 中；`kv` 为 nil 时解析后 `Fields` 为 nil
- **并发约定**：同 `GenerateToken`——调用方需保证 `kv` 在函数返回前不被并发读写

### ParseCustomToken — 解析并校验自定义字段 token

```go
func ParseCustomToken(tokenString string) (*CustomClaims, error)
```

- 校验语义与 `ParseToken` 一致，返回 `*CustomClaims`

### RefreshCustomToken — 刷新自定义字段 token

```go
func RefreshCustomToken(tokenString string) (string, error)
```

- 保留 `Fields`，其余语义与 `RefreshToken` 一致

### CustomClaims.Get / GetString / GetInt / GetUint64 — 类型化取值

```go
func (c *CustomClaims) Get(key string) (val interface{}, isExist bool)
func (c *CustomClaims) GetString(key string) (string, bool)
func (c *CustomClaims) GetInt(key string) (int, bool)
func (c *CustomClaims) GetUint64(key string) (uint64, bool)
```

- 字段不存在或类型不匹配时返回零值 + `false`
- **注意**：JSON 数字统一为 `float64`，`GetInt`/`GetUint64` 已归一化；`GetUint64` 的类型集合是
  `float64`/`uint64`，**有意**不识别内存对象中的 `int`——负 `int` 若被 `uint64(v)` 静默转换
  会得到巨大的无符号值（数据损坏而非读取成功），需要读 `int` 请用 `GetInt`

### 导出错误 — errors.Is 可判定

| 错误 | 含义 |
|------|------|
| `ErrTokenExpired` | token 已过期，等价于 `jwt.ErrTokenExpired` |
| `ErrNotInitialized` | 未调用 `Init` |
| `ErrSigningKeyNotConfigured` | 签名密钥未配置（`Init` 未传 `WithSigningKey` 或只传了空串） |
| `ErrSignatureInvalid` | 兜底校验失败（claims 类型断言失败或校验位为假） |

### 签名算法常量

`HS256`（默认）、`HS384`、`HS512`，均为 `*jwt.SigningMethodHMAC`，配合 `WithSigningMethod` 使用。

## 错误处理

| 场景 | 行为 |
|------|------|
| 未调用 `Init` 就签发/解析/刷新 | 返回 `ErrNotInitialized`，`errors.Is` 可判断 |
| 已 `Init` 但未配置签名密钥 | 返回 `ErrSigningKeyNotConfigured`，`errors.Is` 可判断（fail loud，不静默降级） |
| `RequireSigningKey()` 下密钥缺失 | `Init` 直接 panic（fail fast，启动阶段暴露） |
| token 格式非法（空串、段数不对） | 返回底层库错误（`errors.Is(err, jwt.ErrTokenMalformed)` 可判断），claims 为 nil |
| 签名不匹配（密钥不同、被篡改） | 返回底层库错误（`errors.Is(err, jwt.ErrTokenSignatureInvalid)` 可判断），claims 为 nil |
| token 已过期 | 返回 `ErrTokenExpired`，`errors.Is` 可判断 |
| `iss` 与 `WithExpectedIssuer` 不匹配 | 返回 `jwt.ErrTokenInvalidIssuer`（`errors.Is` 可判断），claims 为 nil |
| `aud` 与 `WithExpectedAudience` 不匹配 | 返回 `jwt.ErrTokenInvalidAudience`（`errors.Is` 可判断），claims 为 nil |
| 刷新已过期 token | 返回 `ErrTokenExpired`（刷新前先解析校验） |
| claims 校验位为假（兜底路径） | 返回 `ErrSignatureInvalid` |

## 测试与验证

**测试文件矩阵**（与源文件一对一映射）：

| 源文件 | 测试文件 |
|--------|----------|
| `jwt.go` | `jwt_test.go` |
| `option.go` | `option_test.go` |
| —（性能基线） | `benchmark_test.go` |
| —（不变量守护） | `fuzz_test.go` |
| —（共享测试工具） | `test_helpers_test.go` |

**测试命名**：标识符 `Test<被测方法><场景>`（全英文驼峰），doc 注释中文描述意图，子测试名为中文。

**运行命令**：

```bash
# 单元测试 + fuzz 种子语料（无需任何环境变量，日常使用）
go test ./pkg/jwt/ -count=1

# 仅单元测试（-v 查看子测试）
go test ./pkg/jwt/ -run Test -v -count=1

# 性能基线
go test ./pkg/jwt/ -bench=. -benchtime=1s -run=^$

# 模糊挖掘（种子已随常规 go test 执行，挖掘需显式指定）
go test ./pkg/jwt/ -fuzz=FuzzParseTokenNeverPanics -fuzztime=30s
```

**集成测试**：本包不依赖任何外部服务（纯 CPU 计算），按包级质量基线不设 `integration_test.go` / `.env`，
`go test -tags=integration` 与普通 `go test` 等价。

## 性能基线与模糊测试

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 -short ./pkg/jwt/
```

> **诚实声明**：本机（Windows + MinGW）执行 `-race` 因 cgo 运行库问题报 `exit status 0xc0000139`，
> **本 README 不声称已经跑过 `-race`**。
> 已提供 Linux CI 取证入口 [`.github/workflows/race.yml`](../../.github/workflows/race.yml)
> （pkg/jwt、pkg/tracer、pkg/logger 三包共用，`go test -race -short`），**待首次推送后取证**；
> 首跑通过后本声明将替换为 CI 结论。`TestInitConcurrentWithParse`
> （8 对 goroutine × 200 次 `Init`/签发/解析并发）即为该 workflow 复核的并发用例，
> 当前仅常规 `go test` 通过。

### 基准测试

读数环境：Windows 10、Intel i7-10750H @ 2.60GHz、Go 1.25.0，`-benchtime=1s`，三轮读数：

| 基准 | 场景 | ns/op（三轮） | B/op | allocs/op（三轮恒定） |
|------|------|---------------|------|-----------|
| `BenchmarkGenerateToken` | 签发标准 token（1 个自定义字段） | 5342 ~ 5875 | 3147 | 39 |
| `BenchmarkParseToken` | 解析标准 token（验签 + 反序列化） | 8145 ~ 9010 | 3520 | 61 |
| `BenchmarkGenerateCustomToken` | 签发自定义字段 token（5 字段） | 6327 ~ 8297 | 3468 | 46 |
| `BenchmarkParseCustomToken` | 解析自定义字段 token（5 字段） | 9181 ~ 10751 | 3640 | 73 |
| `BenchmarkRefreshToken` | 刷新（解析 + 重新签发） | 13057 ~ 13333 | 5518 | 87 |

**口径与解读（很重要，否则数字会被误读）**：
- 全部基准是**纯 CPU 开销**（HMAC 签名 + JSON 序列化），**不含网络 RTT**，也未测中间件/存储层，
  定位是「相对回归基线」而非生产延迟。
- `allocs/op` 与 `B/op` 三轮完全稳定，是回归判定的首选指标；`ns/op` 本轮三轮间波动大于上轮
  （`BenchmarkGenerateCustomToken` 第二轮 8297 为离群值，第一/三轮为 6361/6327，疑似系统干扰），
  单轮读数波动更大时应看分配指标下结论。
- **安全加固的分配代价（预期变化）**：`GenerateToken`/`GenerateCustomToken` 对入参 map 浅拷贝（+2 allocs）、
  两处 parse 增加 `WithValidMethods`（+1 allocs），与上一轮基线（37/60/44/72/86）相比各 +1~2，
  属安全默认值改造的已知开销，非回归缺陷。
- 解析比签发慢（验签 + 反序列化 vs 序列化 + 签名），刷新约等于两者之和，符合结构预期。
- 每条读数对应本轮真实执行的三轮 `go test -bench=. -benchtime=1s`，非估算值。

### 模糊测试

| Target | 不变量 |
|--------|--------|
| `FuzzParseTokenNeverPanics` | 对任意字符串不 panic；失败 ⇒ claims 必为 nil；成功 ⇒ claims 非空且同一 token 可重复解析成功 |
| `FuzzParseCustomTokenNeverPanics` | 同上（`CustomClaims` 版本） |
| `FuzzTokenRoundTripFields` | uid/name 为合法 UTF-8 时经签发 + 解析原样保留；字符串字段原样保留；数字字段 JSON 往返后为 `float64` 且无损还原为原整数值 |

> 不变量限定「合法 UTF-8」的原因：claims 走 `encoding/json` 序列化，非法 UTF-8 字节会被强制替换为
> U+FFFD（替换字符），往返后不再逐字节相等。这是 JSON 层的既定行为（fuzz 实测发现），
> 不是本包缺陷，因此该类输入在 target 内显式 Skip 并注明原因。
