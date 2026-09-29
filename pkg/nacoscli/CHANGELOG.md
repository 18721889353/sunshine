# 变更日志

本文件记录 `pkg/nacoscli` 的显著变更，遵循[语义化版本](https://semver.org/lang/zh-CN/)。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
变更类型分为 `新增`（Added）、`变更`（Changed）、`废弃`（Deprecated）、`移除`（Removed）、`修复`（Fixed）。

本包尚无发布版本，以下记录未发布期间的累计变更；打第一个 tag（如 `v1.0.0`）时，将下方 `## [未发布]` 拆为
`## [未发布]`（保留后续新改动）与 `## [v1.0.0] - YYYY-MM-DD`（归档本次全部变更）。

## [未发布]

### 移除

- `Logger` 接口、`WithLogger` 选项，以及包内 `defaultLogger` / `resolveLogger` / `argsToFields` 翻译层：
  库内日志统一改走全仓底座 `pkg/logger`（`logger.Debug/Info/Warn/ErrorWithCtx`，zap 实现、ctx 感知、自带 request_id 关联）。
  原因：日志抽象层的默认实现不进入项目日志管道，形成生产日志盲区；且 `WithLogger` 在全仓生产代码中零调用点（仅单测使用）。
  **破坏性变更**：调用 `nacoscli.WithLogger(...)` 的代码升级后编译失败，直接删除该选项即可。
- `RequestIDExtractor` 类型与 `WithRequestIDExtractor` 选项：request_id 改为直接读取全项目约定的
  `logger.ContextKeyRequestID`（与 gin/grpc middleware 写入的 key 一致）。
  **行为变更（修复性质）**：此前未注入 `WithRequestIDExtractor` 时 Span **不带** request_id；删除注入点后默认生效。
  **破坏性变更**：调用 `nacoscli.WithRequestIDExtractor(...)` 的代码升级后编译失败，直接删除该选项即可。
- 服务注册与发现的包装方法（`RegisterInstance` / `SelectInstances` / `Subscribe` 等）及本包封装类型 `*NamingClient`：
  与 `pkg/servicerd` 的 `Registry` / `Discovery` 职责重复，且生产调用点只用到「创建客户端」这一件事，
  双重埋点还会导致 `nacos.register` 等 Span 重复上报。
  **破坏性变更**：相关调用改由 `pkg/servicerd` 承担；客户端生命周期改由调用方持有 SDK 接口，用完调用 `CloseClient()`。

### 变更

- `WithScheme` 由「原样写入」改为**白名单 + 归一化**：仅接受 `http`/`https`/`grpc`，大小写与首尾空格不敏感
  （`" HTTPS "` 归一化为 `https`）；**空串与白名单外取值均被忽略，保留先前设置的值**。
  **行为变更**：此前 `WithScheme("h2")` 会把非法协议名写入配置，且由于 SDK 直接拼接请求地址
  （`getAddress: scheme://ip:port`），它不会在创建时报错，而是到运行期请求才失败、错误信息也不指向配置项本身；
  现在非法输入在选项阶段就被拦下。`WithScheme("")` 不再能用来清空协议（因为 `Params.Scheme` 为空是常态，
  写入空串会抹掉调用方显式设置的协议），确需回落默认请改用 `WithClientConfig`/`WithServerConfigs`。
- `Params.Format` 不支持时的错误消息由 `Format=%s` 改为 `Format=%q`。
  **对外可见变化**：错误文本由 `Format=xml` 变为 `Format="xml"`（多一对引号）；目的是把 `Format`（来自配置文件
  /配置中心，外部可控）中的换行与控制字符转义，避免一行日志被拆成多行（日志注入）。用 `strings.Contains`
  匹配该错误文本的代码需同步调整。
- `GetConfig` 与 `Client.GetConfig` 的**返回值改为匿名**（原 `(format string, content []byte, err error)`）：
  函数体内多处局部 `err` 与具名返回值同名（同一块内 `:=` 是复用而非遮蔽，语义正确），但容易让阅读者
  误判边界；取消具名返回值不影响调用侧（参数名仅存于 doc comment）。**非破坏性变更**。
- `requestIDAttr` 由返回 `*attribute.KeyValue` 改为返回 `attribute.KeyValue` 值（以 `attr.Key != ""` 判断命中）。
  实测收益：命中路径 69.6ns/64B/1alloc → **26.5ns/0B/0alloc**（四次重测读数稳定）；
  代价是未命中路径 4.6ns → 12.8ns（返回零值结构体多拷几个字），而生产上 request_id 几乎总是存在，
  命中才是代表场景。包私有函数，不影响对外 API。
- `listener.go` / `watch.go` / `nacoscli.go` 的日志调用点：由 `Warn(msg, key, value...)` 风格的
  key-value 裸参数改为 `logger.WarnWithCtx(ctx, msg, logger.Err(err))`，字段以强类型 `logger.Field` 上报，不再丢类型。
- `NewNamingClient` 定位为**纯工厂**（返回 SDK 的 `naming_client.INamingClient`），并在 doc comment 与 README
  中补写「适用边界」：返回 SDK 类型意味着与 `nacos-sdk-go` 的 API 稳定性耦合，仅在本仓 mono-repo 内成立，
  若作为独立模块对外发布需改回封装类型。
- `TestMain` 的集成测试总时长由硬编码 120 秒改为 `NACOS_TEST_TIMEOUT` 环境变量可配，非法值打印告警后回退默认值。

### 新增

- `naming.go` / `naming_test.go`：`NewNamingClient` 工厂函数，与 `NewConfigClient` 共用一套
  地址/命名空间/认证/超时参数构建，并显式校验地址与端口缺失。
- `logging.go`：包私有的日志钩子变量（`logDebug` / `logInfo` / `logWarn` / `logError`），仅用于单元测试替换以捕获日志，
  不对外暴露注入能力；文件顶部写明「替换期间禁止 `t.Parallel()`、禁止嵌套捕获、生产代码视为只读」的约束。
- `logging_test.go`：
  - `logCollector`——收集日志时内部用 `sync.Mutex` 保护写入与读取（`msgs()` 返回快照副本）。
    钩子的写入方可能是 Nacos SDK 的后台回调 goroutine（`listener.go` 的 `buildOnChange`），与被测 goroutine
    之间没有 happens-before 保证，无锁 map 在 `-race` 下会报 concurrent map writes。
  - `captureLogs` 的重复调用拦截（`atomic.Bool`）：同一测试内嵌套或二次捕获直接 `t.Fatal`。
  - `TestLogHooksReplaceAndRestore`：验证钩子默认指向全局 logger、可被替换捕获、测试结束后自动还原。
  - `TestLogCollectorConcurrentAdd`：两个 goroutine 并发写读，验证不丢消息。
- README 三个约定/决策专章：「为何不依赖倒置日志」（含可复核的事实依据与复核命令）、
  「Option 赋值语义（最后赋值胜出）」、「测试命名规范」（含与 `pkg/goredis` 的跨包差异说明）。
- `benchmark_test.go`：性能基线（`BenchmarkClientGetConfig` 128B/1KB/64KB、`BenchmarkClientGetConfigWithRequestID`、
  `BenchmarkValid`、`BenchmarkApplyOptions`（含 `nil` Option）、`BenchmarkBuildConfigs`、`BenchmarkRequestIDAttr`、
  `BenchmarkSafeAttrText`（合法 ASCII / 合法中文 / 含非法字节）。
  基准基于包内 `mockConfigClient`，**不含网络 RTT**，定位是「相对回归基线」而非生产延迟；读数与解读见 README
  「性能基线与模糊测试」。
- `fuzz_test.go`：模糊测试 6 个 target（`FuzzParamsValid` / `FuzzClientGetConfig` / `FuzzBuildOnChange` /
  `FuzzOptionZeroGuard` / `FuzzSafeAttrText` / `FuzzWithSchemeWhitelist`），守护不变量：不 panic、错误路径不返回半成品、
  回调恰好一次且参数原样透传、零值/负值防护对任意输入恒成立、**写入 Span 的文本必为合法 UTF-8 且合法输入不被改写**、
  **scheme 只能是未设置或白名单值且恒小写无空格**，以及**错误消息不含裸换行/回车**（防日志注入）。
  种子语料随常规 `go test` 执行，挖掘需显式 `go test -fuzz=...`。
  历轮挖掘未复现缺陷，因此它的价值是「防止后续回退」，与 `pkg/goredis` 由 Fuzz 挖出真实 bug 形成互补。
- `integration_test.go` 新增三个等待时长常量（`listenRegistrationWarmup=3s`、`watchRegistrationWarmup=2s`、
  `retryCompletionWait=5s`）替掉原先散落在 6 个用例里的 `time.Sleep(2/3/5 * time.Second)` 硬编码，
  并在注释里写清三件事：**为何只能固定预热**（SDK 未暴露「注册已生效」的可观测信号）、
  **取值依据**、**为何不会因而 flaky**（结果判定一律靠已有的「轮询 + deadline」循环，预热不足只会让回调晚到）。
  等待值本身**未作改变**；后续已在真实 Nacos 环境跑通集成测试，并补齐了实测读数（见下条）。
- **真实 Nacos 环境下的集成测试补全**（回答「远程配置被修改后本包能否监听变更」）：
  - `TestIntegration_WatchConfigExternalChange`：修改方走 **Nacos Open API**（`POST {ctx}/v1/cs/configs`，
    即控制台点「发布」、运维脚本、CI 使用的同一入口），监听方全程只读不改，因此它证明的是真实运维场景
    「**别人改了配置，本进程能感知**」，而不是「SDK 自己发布给自己监听」的自闭环。
    实测送达延迟（多轮）：首次 2.264~2.283s、后续 210~275ms、**空闲 15s 后 220/221ms**，回调总数恰为 3。
  - 新增 Open API 辅助：`nacosContextPath` / `nacosHTTPClient` / `httpBodySnippet` / `nacosAccessToken` /
    `publishConfigViaOpenAPI`（登录取 token → 发布，命名空间用 `tenant` 而非 `namespaceId`）/
    `awaitExternalChange`（**按内容匹配**等待，不用「收到任意一次回调」判定，避免注册前的变更被补推导致误判）。
  - 新增常量 `externalChangeDeadline=20s`（实测亚秒~2s 级，留数量级余量）与 `externalIdleWait=15s`
    （插入空闲窗口以覆盖「长连接空闲后失效」这一故障模式；15s 是经验值，不是服务端某个具体周期）。
  - `TestMain` 里的 `loadDotEnv` 自动加载包内 `.env`（只补缺、不覆盖已设置变量），集成测试不再依赖 IDE EnvFile 插件。
- **服务发现侧的定位与用例加固**（均属测试质量，未改生产行为）：
  - 定位到 SDK v2.3.5 的 `HealthyOnly` 语义陷阱：`naming_client.go` 的 `selectInstances` 过滤条件为
    `host.Healthy == param.HealthyOnly`，缺省（false）时**只返回不健康实例**，与 HTTP API 含义相反；
    集成测试的查询全部改为显式 `HealthyOnly: true`。已 Grep 确认生产侧
    `pkg/servicerd/registry/nacos/registry.go` 三处调用均显式传 `true`，**不存在线上缺陷**。
  - 新增 `pollHealthyInstances`（空列表时 SDK 返回 error `instance list is empty!` 而非空切片，因此错误也
    当作「尚未可见」继续重试）与 `listInstancesViaOpenAPI` / `countAllInstances`（绕过 SDK 本地缓存直接问服务端，
    用于区分「服务端没收到注册」与「收到了但没推给客户端」）；新增常量 `namingReadyWait=10s`。
  - `TestIntegration_NamingClientListInstances` 的断言由「≥ 3 个」降为「≥ 1 个」：实测连续注册 3 个临时实例后，
    SDK 侧与服务端 Open API 侧**都只看到 1 个**且不同时刻看到的那 1 个会变，属 Nacos 部署层面现象；
    「看到几个」降级为日志（同时打印 SDK 缓存数与服务端快照），不留一条永不满足的断言。
- README 新增「**职责边界**」章节：以表格明写配置读取/监听在本包，而**配置发布/删除/搜索
  （`PublishConfig`/`DeleteConfig`/`SearchConfig`）不在本包范围**（它们是写操作，涉及幂等、并发写冲突与权限，
  属业务/运维平台职责），并给出可复核命令；服务注册发现的归属仍为 `pkg/servicerd`。
- 竞态/基准/挖掘均使用原生 `go test` 命令（**不新增 Makefile target**，保持项目根构建入口不变）：
  `CGO_ENABLED=1 go test -race -count=1 -short ./pkg/nacoscli/`、`go test -run='^$' -bench=. -benchmem ./pkg/nacoscli/`、
  `go test -run='^$' -fuzz=<Target> -fuzztime=30s ./pkg/nacoscli/`。在此之前 `-race` 没有成文的执行方式，
  本包的并发测试（`TestLogCollectorConcurrentAdd`）实际只在非 race 模式下跑过；完整命令与口径见 README
  「性能基线与模糊测试」。

### 修复

- **`WatchConfig` 后台 goroutine 未捕获 panic**：`listener.safeCallHandler` 只对业务回调做了 recover，
  但 `NewListenClient` / `Start` / `Stop` 都会进入 Nacos SDK 内部，SDK 自行 panic 会**直接打死整个进程**。
  现在后台 goroutine 顶部补了一层 defer recover，记录 Error 日志后本轮监听终止，进程不受影响；
  `wg.Done()` 仍为 defer，因此 `stop()` 在 panic 路径也能正常返回（不泄漏、不阻塞）。
- **非法 UTF-8 的 `DataID`/`Group` 污染 Span 属性**：`Client.GetConfig` 将两个外部可控字段原样写入
  `nacos.data_id` / `nacos.group`，含非法 UTF-8 字节时 OTLP（protobuf string 字段要求合法 UTF-8）
  会**整批丢弃该 Span**，表现为「配置拉取链路凭空消失」而非报错，排查成本极高。
  新增包私有 `safeAttrText`：合法输入直接返回（`utf8.ValidString` 快路径，无拷贝），非法输入用
  `strings.ToValidUTF8` 替换。与 `pkg/goredis` 本轮修的 Redis key 截断属同一类缺陷（本包由代码审阅发现，
  goredis 由 Fuzz 发现）。
- `Client.GetConfig` 的 panic 转错误由 `fmt.Errorf("GetConfig panic: %v", r)` 改为：recover 值是 `error` 时
  用 `%w` 包装（**保留错误链**，调用方可 `errors.Is`/`As` 穿透 SDK 错误），非 error 时退回 `%v`（文本不变）。

### 兼容性说明

本包与 `pkg/goredis` 的日志方案完全对齐：**基础设施包不再各自定义 `Logger` 接口**，统一依赖 `pkg/logger`。
若将来需要替换底层日志实现，收口点是 `pkg/logger` 自身。跨包规范的统一边界与验收清单现统一维护在
skill [`package-quality-baseline`](../../.qoder/skills/package-quality-baseline/SKILL.md)
（交付物矩阵、测试命名、集成测试方法论、证据标准）与 `project-conventions`（代码写法与 README 规则），
本文不再引用尚未创建的根目录 `CONTRIBUTING.md`。
