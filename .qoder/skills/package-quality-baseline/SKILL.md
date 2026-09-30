---
name: package-quality-baseline
description: Defines the Sunshine repo's per-package quality baseline - deliverable file matrix (unit/integration/benchmark/fuzz tests, README, CHANGELOG), the single unified test naming scheme, integration test environment/skip/timeout conventions, the evidence standard for claims marked "实测", external-view verification methodology, and doc section templates. Use when creating any pkg/* package, adding or fixing tests, investigating flaky or integration test failures, writing package README/CHANGELOG, or reviewing test and documentation quality.
---

# Sunshine 包级质量基线

## 与 project-conventions 的分工（先读这条，避免两处漂移）

| 主题 | 权威位置 | 说明 |
|------|----------|------|
| Go 代码写法、lint 规则、中文注释/日志格式 | `project-conventions` | 描述性规则 |
| README **编写规则明细**（场景驱动、表格列、反模式） | `project-conventions` 第十九节 | 本 skill 只给**模板骨架**，不重抄规则 |
| **测试交付物、测试命名、集成测试方法论、证据标准、验收清单** | **本 skill** | 操作性基线；`project-conventions` 原第十六节的集成测试内容已迁移至此 |

冲突时以上表为准。改任何一条规则，必须同时检查另一处是否残留旧表述。

## 一、交付物文件矩阵

每个 `pkg/*` 包做改动时逐项核对：

| 交付物 | 要求 | 不合格判据 |
|--------|------|------------|
| `xxx_test.go` | 与每个源文件 `xxx.go` **一对一**映射 | 所有测试堆在一个文件里 |
| `integration_test.go` | 带 `//go:build integration`，只放依赖真实外部服务的用例 | 单测与集成测试混写 |
| `benchmark_test.go` | 基于包内 mock，**不含网络 RTT**，定位是回归基线 | 把基准写成集成测试（读数无意义） |
| `fuzz_test.go` | 守护**不变量**而非输出值；种子语料随常规 `go test` 跑 | 断言具体输出字符串 |
| `README.md` | 所有 `pkg/` 包必须有；文件数 ≥ 4 必须写架构概览 | 只列函数签名 |
| `CHANGELOG.md` | 有行为变更/缺陷修复的包必须有（现存 `goredis`/`logger`/`jwt`/`tracer`/`nacoscli` 五包已有） | 把变更写进 commit 而不落文档 |
| `.env` | 集成测试凭据；根 `.gitignore` 的 `*.env` 已覆盖 | 明文凭据入库 |
| `test_helpers_test.go` | 跨测试文件共享的 helper 集中放这里；**文件名必须带 `_test.go` 后缀**，否则 `unused` 会把仅测试使用的 helper 报 `is unused`（见第八节） | 同名 helper 在多个文件重复定义；或 helper 文件缺 `_test.go` 后缀 |

## 二、测试命名：全仓统一为方案 A

**唯一规范**（今后新包一律遵循，存量包见下方待收口清单）：

- 标识符：`Test<被测方法><场景>`，**全英文驼峰**，如 `TestWatchConfigCancelledCtx`、`TestBuildConfigsDefaultValues`
- doc 注释：首行 `// TestXxxYyy 验证……。`（中文描述意图）
- 子测试 `name` 字段：中文（如 `"恰好达上限"`），它只出现在 `go test -v`
- 集成测试统一前缀：`TestIntegration_<场景>`
- **包内禁止混用**两套风格

为什么统一用英文标识符：`-run` 过滤、IDE 符号索引、grep 定位、重构可跟踪性都依赖标识符；中文部分不参与编译，无法反映真实 API 名。

> **存量待收口**：`pkg/goredis` 采用方案 B（`TestXxx_中文描述`）。统一为方案 A 需要一次性重命名其测试函数，
> 属于独立的机械改动（不改断言逻辑），未与本轮文档工作合并执行。枚举待改点：
> `grep -rn "^func Test.*_" pkg/*/  --include=*_test.go | grep -v Integration`

## 三、测试分层与各自合格线

| 层 | 依赖 | 默认是否跑 | 合格线 |
|----|------|-----------|--------|
| 单元 | 包内 mock | 是 | 每个导出函数 ≥1 正向 + ≥1 错误/边界；goroutine 测试用 channel/WaitGroup 等待，禁止裸 `time.Sleep` 判定结果 |
| 集成 | 真实外部服务 | 否（需 `-tags=integration`） | 环境变量缺失即 Skip；不可达即 Skip；**断言不符必须 Fail** |
| 基准 | mock | 否（需 `-bench`） | 标注 `-benchtime`，并说明口径（是否含 RTT） |
| 模糊 | mock | 种子跑，挖掘需显式 `-fuzz` | 每条不变量能写成一句可判定的话 |

## 四、集成测试规范

### 4.1 环境变量与自动加载

- 命名前缀统一 `<SERVICE>_*`：`NACOS_IP_ADDR`、`NACOS_PORT`、`NACOS_NAMESPACE_ID`、`NACOS_GROUP`、
  `NACOS_DATA_ID`、`NACOS_USERNAME`、`NACOS_PASSWORD`、`NACOS_GRPC_PORT`、`NACOS_CONTEXT_PATH`、`NACOS_TEST_TIMEOUT`
- `TestMain` 先调 `loadDotEnv(".env")`：**只补缺、不覆盖已设置的变量**，使 `go test -tags=integration` 不依赖
  IDE EnvFile 插件，也不需手动 export；CI 用真实环境变量覆盖 `.env` 即可，无需 `.env` 文件
- 不引入第三方 dotenv 依赖，手写解析：跳过空行/`#`、按**第一个** `=` 分割（值可含 `=` 与括号）、去成对引号、不做变量展开

### 4.2 TestMain 超时保护（强制）

依赖外部服务的包**必须**实现 `TestMain` 限制总时长：SDK 后台 goroutine 泄漏会让 `go test` 永远挂起，
而挂起的测试拿不到任何读数。默认 120s，用 `<SERVICE>_TEST_TIMEOUT` 上调，无需改代码。

### 4.3 Skip 与 Fail 的边界（最容易写错的一条）

| 现象 | 处理 | 理由 |
|------|------|------|
| 必需环境变量未设置 | `t.Skip` | 环境选择 |
| 端口 TCP 不可达（探测返回 error） | `t.Skipf` 并打印具体 error | 区分「没映射」「IP 错」「防火墙丢包」 |
| 服务端拒绝匿名请求（401 / `User not found`） | `t.Skip`（用 `isAuthErr` 归类） | 部署选择，非代码缺陷 |
| SDK 返回 `false`（连接处于 `STARTING`） | `t.Skipf` | 基础设施未就绪 |
| 加密配置无密钥无法解密 | `t.Logf` 记录后通过 | 预期行为 |
| **断言值不符** | `t.Fatalf` / `assert` | 这才是被测代码的问题 |

> 反向陷阱：**不要**把基础设施故障归类成「预期失败」。曾出现过把 `config encrypted data key` 当加密特征，
> 而它实际是「本地缓存文件不存在」的错误文本片段，导致 gRPC 不可达被静默放过。
> 关键词判定表必须写清「为何某词不能加进来」。

### 4.4 等待时长：常量化 + 命名 + 处置顺序

- 所有预热/等待必须抽成有名字的常量（`listenRegistrationWarmup`、`watchRegistrationWarmup`、
  `retryCompletionWait`、`namingReadyWait`、`externalIdleWait`…），禁止散落的 `time.Sleep(3 * time.Second)`
- 每个常量注释写清三件事：**为何只能固定等待**（外部库未暴露「已生效」信号）、**取值依据**（实测读数或经验余量，二者要分清）、
  **为何不会因而 flaky**（结果判定靠「轮询 + deadline」，预热不足只会让回调晚到）
- 出现 flaky 时的处置顺序：**先调等待常量 → 再看轮询上界 → 最后才动断言**；反过来改断言会掩盖真实原因
- 固定等待不能省的前提下，**判定逻辑**一律用轮询 + deadline，不允许用固定 sleep 后的单次读数下结论

### 4.5 依赖外部 SDK 时，语义要读到源码

外部库的行为差异不许靠猜。本轮两个实例（详见 [evidence-examples.md](evidence-examples.md)）：

- SDK v2.3.5 `selectInstances` 的过滤条件是 `host.Healthy == param.HealthyOnly`，缺省 false 时**只返回不健康实例**，
  与 HTTP API 的 `healthyOnly=false`（返回全部）语义**相反**
- 实例列表为空时返回 **error** `instance list is empty!`，而不是空切片 → 轮询函数必须把 error 也当作「尚未可见」继续重试

定位到「文件名 + 函数名 + 判断条件」写进注释，才算证据。

## 五、证据标准（凡写「实测」都得给证据）

这一节是本 skill 相对 `project-conventions` 的最大增量。适用于**注释、README、CHANGELOG、PR 描述**全部场景。

1. **写「实测」必附读数**：数值 + 单位 + 环境（哪个服务/哪台机器）+ 轮数。
   例：`首次送达 2.264~2.283s（4 轮），后续 210~275ms`。
2. **未验证的必须显式声明未验证**：不许用「应该」「一般来说」。
   反例（本轮真实修正过）：注释里写「取值是经验余量而非实测，且本轮未在有真实 Nacos 的环境下跑过」——
   一旦后续跑过了，**这句话必须同步改掉**，否则文档变成假信息。
3. **假设被推翻 → 把误判过程写进注释**：写「曾判为 X，读数不变才暴露真因是 Y」，防止后人重蹈。
4. **未取证只写现象，不写原因**：看到「注册 3 个只可见 1 个」时，先给两份视图读数（客户端缓存数、服务端快照），
   再下结论「属部署层面」，不能直接写「集群同步延迟」。
5. **本机无法复现的限制要如实标注**：如 Windows/MinGW 下 `-race` 报 `0xc0000139`，
   则 README **不得声称跑过 `-race`**，只给命令形式并注明真实证据需在 Linux/CI 采集。
6. **单一事实单一来源**：同一读数/结论只在一处维护（源码注释或 README 之一），另一处用引用指向，
   避免改一处漏一处。

## 六、验证方法论：从外部视角证明能力

被问「X 能不能工作」时，弱证据会让结论失效。四条硬性要求：

1. **区分自环与外部视角**。验证「能否感知远程配置变更」时，修改方必须走**与运维相同的入口**
   （Nacos 是 Open API `POST {ctx}/v1/cs/configs`，即控制台点「发布」同一入口），
   监听方全程只读不改。若两侧都用同一 SDK 发布/订阅，只能证明「SDK 自己发给自己」的自闭环。
2. **按内容匹配等待**，不能「收到任意一次回调」就算通过——注册动作之前发生的变更可能被补推一次，会造成假阳性。
3. **活性验证要插空闲窗口**。连续两次秒级变更只能证明「不是一次性消耗」；
   必须 `time.Sleep(externalIdleWait)` 后再改一次，才覆盖「长连接空闲后失效」这一故障模式。
   空闲时长是经验值时要写明，不要暗示它是服务端某个周期。
4. **读数不全时补齐两份视图再降级断言**。若外部条件导致预期无法达到，
   把不可控的部分降级为日志（同时打印各层计数），**而不是留一条永远不可能满足的断言**。
   降级必须在注释里附实测证据 + 归属判断（环境层面 or 代码缺陷）。

## 七、README / CHANGELOG 模板

- README 章节顺序骨架、场景块模板、集成测试章节、实测结果汇总表 → [doc-templates.md](doc-templates.md)
- CHANGELOG 四小节结构与「未验证声明」写法 → 同上
- 编写**规则**（不是模板）明细见 `project-conventions` 第十九节

## 八、工具链陷阱（本仓 Windows + Git Bash 实测）

| 陷阱 | 现象 | 规避 |
|------|------|------|
| Git Bash 把 `-run "A\|B"` 里的 `\|` 当管道 | 日志出现 `TestIntegration_Xxx: command not found` | 用无竖线正则（`Config.*Change`、`TestIntegration_Naming`）或拆多次执行 |
| 编辑进行中就跑 `go test` | `could not import encoding/json / net/http` 等假错误 | 先 `gofmt -s -l . && go vet -tags=integration ./...` 再跑 |
| `GetProblems` 对 `//go:build integration` 文件 | 恒报 No errors（无 tag 时整文件被排除） | 不能作为验证手段，必须用 `go vet -tags=integration` |
| 共享 helper 文件不带 `_test.go` 后缀 | `golangci-lint` 的 `unused` 报 `const/func is unused`（本仓 `run.tests: false`，helper 的使用点全在被排除的 `_test.go` 中） | helper 落 `test_helpers_test.go`；实测案例见 `pkg/jwt` |
| IDE 终端强杀长命令 | 日志文件为空或 `EXIT=1` 无输出 | `go test -c -o x.test .` 编出二进制再跑；或分段跑；控制单命令 <20s |
| `$TEMP` 取值随 shell 变 | 同一命令写的日志读不到（可能是 `/tmp` → `D:\Git\tmp`） | 输出写绝对路径，或用包目录相对文件名 |
| `-race` 需 cgo | `exit status 0xc0000139`（本机 gcc 问题） | `CGO_ENABLED=1`；本机跑不通就在文档里声明未取证 |
| 测试命令入 Makefile | 污染根构建入口 | 一律走原生 `go test`，**不在项目根 Makefile 新增 target** |
| 排障脚本落进包目录 | `runlist.bat`、`list8.log` 被 git 状态带出 | 收尾必查 `git status --short <pkg>`，临时文件不进包目录 |

## 九、验收清单（改完一个包，逐项打勾）

```
- [ ] gofmt -s -l <pkg> 无输出
- [ ] go vet -tags=integration ./<pkg>/... == 0（带集成标签也须编译通过）
- [ ] go test ./<pkg>/ -count=1 全绿（不含集成）
- [ ] go test -tags=integration -count=1 全绿或有 Skip 且 Skip 原因已说明
- [ ] 每个新增/修改的源文件都有同名 _test.go；helper 放 test_helpers_test.go（必须带 _test.go 后缀）
- [ ] 新测试函数命名符合方案 A（全英文标识符 + 中文 doc）
- [ ] 集成测试：环境变量缺失→Skip；不可达→Skip 带 error；断言不符→Fail
- [ ] 等待时长全部是命名常量，注释含「为何固定等待 / 取值依据 / 为何不 flaky」
- [ ] 文档与注释里每句「实测」都有读数；未验证的显式声明未验证
- [ ] README 更新（场景、参数表、集成测试章节、实测结果汇总）
- [ ] CHANGELOG 登记（新增/变更/修复/兼容性说明），并删掉过期声明
- [ ] git status 干净：无 .log/.bat/二进制等排障残留；凭据未入库
- [ ] 未在根 Makefile 新增 target
```

## 十、下一阶段工作（团队级 TODO）

跨包共性事项**不挂在单个包的 README 里**（否则会永远挂着），统一在本清单推进：

- [ ] **CI `-race` 首跑取证**：`.github/workflows/race.yml` 已就位但从未首跑。推送触发一次后，
  把 jwt/tracer/logger 三包 README 中的「本机无法跑 `-race`」诚实声明替换为 CI 结论
  （自第 3 轮起悬挂，logger 第七轮评审 §四-3 升级至此）
- [ ] **`loadDotEnv` 抽 `internal/dotenv`**：nacoscli/goredis/tracer 三包 `TestMain` 各自维护同一份
  手写 dotenv 解析（语义一致：只补缺、不覆盖），抽取后改动只维护一处
- [ ] **`pkg/goredis` 测试命名收口**：方案 B → 方案 A（见第二节存量清单）
- [ ] **CHANGELOG 评审编号统一**：存量 `§四-1` 式引用改为 `R<轮次>-<原编号>`
  （logger 已收口，jwt 等包待对齐，见 doc-templates.md 第五节）

## 附加资源

- [doc-templates.md](doc-templates.md) — README / CHANGELOG 章节模板（可直接粘贴）
- [integration-skeleton.md](integration-skeleton.md) — `integration_test.go` 骨架（TestMain/loadDotEnv/require*/等待常量/外部探针）
- [evidence-examples.md](evidence-examples.md) — 实测证据的合格样例与反例（源自 nacoscli 真实排障记录）
