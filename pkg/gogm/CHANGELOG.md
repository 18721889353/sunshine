# Changelog

本文件记录 `pkg/gogm` 的显著变更。

## Unreleased

### 新增

- **三子包测试体系从零补齐（22 个单元测试 + 3 个 fuzz 目标 + 9 个基准）**：此前 `gosm2/gosm3/gosm4`
  零测试，全部风险靠人工阅读兜底。本轮按仓库质量基线补齐：
  - `gosm4`：CBC/ECB 往返（6 档长度表）、与 gmsm 上游**字节级兼容 oracle**（测试内串行调用
    `sm4.Sm4Cbc/Sm4Ecb` 断言同密文，守护存量 Nacos `ENC()` 密文）、非法密文拒绝、并发不同 IV
    回归守护、HTML 转义选项行为、格式转换与解码错误 10 个测试 + 2 个 fuzz + 4 个基准。
  - `gosm2`：Hex 固定宽度、50 轮密钥往返、四种密文格式加解密、签名验签与篡改拒绝、PEM 解析、
    密钥落盘、HTML 转义、解析错误 10 个测试 + 1 个 fuzz + 3 个基准。
  - `gosm3`：GB/T 32905—2016 官方向量（`abc`、`abcd`×16）与摘要不变量 2 个测试 + 2 个基准。
- **README 与 CHANGELOG 建立**：`README.md` 含架构概览、职责边界、三类使用场景、全量 API 速查、
  错误处理表、实测基准读数与 fuzz 不变量清单；本文件登记全部显著变更。
- **R2 轮：测试体系扩到三包对称（23 单测 + 6 fuzz + 9 基准）**：针对质量评估报告
  P1-4/P1-5 收口——
  - `gosm2` 新增 `FuzzEncryptDecryptRoundTrip`（四种密文格式往返守恒 + 空明文快速拦截断言）
    与 `FuzzSignVerifyRoundTrip`（原数据验签通过、篡改一字节必拒、Hex 往返、非法字符不 panic），
    加上新增单测 `TestEncryptRejectsEmptyPlaintext`，共 11 单测 + 3 fuzz。
  - `gosm3` 新增 `test_helpers_test.go`（`mustHash`，与另两包测试体系对齐）与
    `FuzzHashInvariants`（32 字节/64 位 Hex 恒定、`Hash` ≡ `HashString`、确定性、不 panic）。
  - fuzz 实测（`-fuzztime=10s`）：`FuzzEncryptDecryptRoundTrip` 685 次、
    `FuzzSignVerifyRoundTrip` 388 次、`FuzzHashInvariants` 46468 次，全部 PASS；
    首跑曾暴露 fuzz 目标自身的负索引缺陷（负数 `tamper` 取模），已修复并保留
    `gosm2/testdata/fuzz/FuzzSignVerifyRoundTrip/` 1 条回归语料守护不回退（非库 bug）。
- **R3 轮：fuzz 双向不变量补齐（24 单测 + 6 fuzz + 9 基准）**（三、四轮评审 P1 收口）：
  - `FuzzSignVerifyRoundTrip` 增加“篡改签名必被拒”断言——原实现只验证“数据被篡改”
    被拒，未覆盖“签名被篡改”（伪造攻击的直接目标）；与数据篡改构成双向不变量，
    对照 `gosm4 FuzzCBCDecryptRobustness` 的双向守护思路。
  - 同一目标补齐 Hex 长度非法两面：**截断**（解码成功但 DER 结构不完整）与
    **奇数长度**（解码直接失败）均断言返回 false 不 panic，与既有的非法字符断言
    构成三类输入错误全覆盖。实测 10s PASS（652 次）。
  - 新增单测 `TestDecryptToHexKeeps65Byte04Plaintext`，守护下方 ToHex 特例拆除。

### 变更

- **移除 `sm/` 试验目录（安全清理）**：该目录是独立小工程（`main.go` + `.idea/`），不属于
  `pkg` 库代码；其中 `cert/private.key`、`cert/sm2Private.Pem` 等 **5 个密钥/证书文件已入 git 历史**。
  - 影响面：仓库内无任何代码引用该目录（`grep -rn "gogm/sm" --include=*.go` 无结果），删除不影响构建。
  - 安全提示：入库私钥应视为**已泄露**，若曾在任何环境使用，必须轮换后再删除历史。
- **移除三个 `example/` 演示目录**：`gosm2/gosm3/gosm4` 各自的 `example/example.go` 演示代码删除，
  可复制运行的完整示例改由 `README.md`「使用场景选择」承载，演示与文档不再双份维护。
- **R2 轮：安全语义与工程闭环文档收口**（对应质量评估报告 P1/P2 项，均不改变导出 API）：
  - `WithUnescapeHTML` 默认 `true` 的数据改写风险在 README/Option 表/`Encrypt`/`Sign` doc
    四处加醒目警告（明确 `Decrypt(Encrypt(x)) != x` 往返不守恒）；**默认值不改**——
    改 `false` 属破坏性行为变更，留待大版本统一处理。
  - `VerifyFrom*` 返回 `false` 的三义（格式错误/数据篡改/签名篡改）在 doc 与错误表中说明，
    并给出"自行解码后分级计数"的规避路径（报告 P1-3 方案 C）。
  - 压缩格式解密"无条件补 `0x04`、不做已带前缀探测"写入 doc——合法压缩密文首字节
    有 1/256 概率恰为 `0x04`，探测会误判导致永久解不开（报告 P2-3 改道）。
  - `NewSM2/NewSM3/NewSM4` 不返回 `error` 的理由（纯值构造、无 IO）补齐 doc；
    `Result.ToHex` 的 65 字节特例、`WithSave` 落盘失败即报错且不返回密钥对（fail-fast）
    同步写入文档（报告 P2-1/P2-4/P2-5）。
  - 基准复测命令改为 `-count=3` 三轮取中位（报告 P2-6）；`.github/workflows/race.yml`
    扩至 `./pkg/gogm/...`，Linux CI 侧补 `-race` detector 级证据。

### 修复

- **SM4-CBC 并发数据竞争导致密文错乱（数据损坏，R1-P0-1）**：根因是 gmsm v1.4.1 上游
  `sm4.go` 的包级全局 `var IV`——`SetIV` 写入与 `Sm4Cbc` 拷贝读取两步之间无任何同步，
  并发调用方各自 `SetIV` 后互相覆盖，产生"用 A 的 IV 却按 B 的 IV 加密"的错乱密文 →
  `gosm4` 改为本地实现（`sm4.NewCipher` + `crypto/cipher`），IV 每次调用显式传入、
  函数内防御性拷贝，**全包不再读写任何全局状态**。
  - 影响面：所有并发 CBC 调用方（多协程不同 IV 场景此前必然互相污染）；单线程调用的
    密文字节输出不变。
  - 守护：`TestCBCConcurrentDifferentIVs`（8 协程 ×100 轮不同 key/IV）+
    `TestCipherOutputCompatibleWithGmsm`（与上游字节级一致）。
- **SM4 解密静默返回 nil 与空输入 panic（阻断级，R1-P0-2）**：根因是上游
  `out, _ = pkcs7UnPadding(out)` 丢弃填充校验错误——乱码密文解密得到 `(nil, nil)` 被上层
  当作成功，错误串出现 `%!w(<nil>)` 畸形文本；且 `pkcs7UnPadding` 对空输入 `src[length-1]`
  越界 panic → `gosm4` 包内自实现 PKCS7 填充校验，错误如实上抛（`PKCS7 填充校验失败`、
  `SM4: 密文长度必须是 16 字节的整倍数`），空输入返回错误而非 panic，合法空明文可正常还原。
  - 影响面：非法/乱码/截断密文的解密路径（此前静默 nil + 畸形错误串，甚至进程崩溃）；
    合法密文的明文输出与历史一致。
  - 守护：`TestDecryptRejectsInvalidCiphertext`、`TestDecryptEmptyPlaintextSucceeds`、
    `FuzzCBCDecryptRobustness`（任意字节不 panic，解密成功则重加密必还原原密文）。
- **SM2 Hex 密钥前导零丢失导致自产密钥解析失败（解析失败，R1-P1-1）**：根因是
  `big.Int.Bytes()` 省略前导零——私钥 `D`、公钥 `X/Y` 分量带前导零（各约 1/256 概率）时
  Hex 输出短于 64/128 位，`x509.ReadPublicKeyFromHex` 严格要求 64 字节拒绝解析，
  "自己生成的密钥自己解析不回去" → 新增 `paddedBytes` 统一左侧补零到 32 字节。
  - 影响面：`GenerateKeyPair`、`PrivateKeyToHex`、`PublicKeyToHex` 三个出口的 Hex 宽度
    （此前不定长，现在恒为 64/128 位）；PEM 路径与密钥数学值不受影响。
  - 守护：`TestPaddedBytesFixedWidth`（直接测补位函数边界）+
    `TestGenerateKeyPairHexRoundTrip`（50 轮生成→解析→重导出逐字比对）。
- **SM2 空明文加密死循环导致 goroutine 卡死（DoS，R2-P0-2，本轮 fuzz 首跑实测发现）**：
  根因在上游 gmsm v1.4.1 `sm2.go`——`kdf(length)` 尾部检查循环 `for i:=0; i<length; i++`
  对 `length=0` 不进入，恒 `return c, false`（L605-610 取证）；`Encrypt` 内
  `ct, ok := kdf(...); if !ok { continue }`（L282-285 取证）对空明文永久重试
  （每轮 2 次曲线运算，实测 `FuzzEncryptDecryptRoundTrip` 首跑 `panic: test timed out after 2m0s`）
  → `gosm2.Encrypt` 入口增加空明文拦截，返回 `SM2: 明文不能为空（上游对空明文会无限重试）`，
  将上游死循环降级为立即失败。
  - 影响面：所有传空明文的加密调用方（此前进程挂起）；非空明文路径不受影响。
  - 守护：`TestEncryptRejectsEmptyPlaintext`（四格式循环断言快速报错）+
    `FuzzEncryptDecryptRoundTrip` 空明文分支（断言不挂起、必报错）。
  - 注：`Decrypt` 路径无此问题（上游解密对非法输入返回 error，不重试）。
- **SaveKeyPair 对去头 KeyPair 落盘产生非标准 PEM（外部工具解析失败，R2-P1-2 方案 A）**：
  原实现直接写 `keyPair.PrivateKeyPEM`，调用方启用 `WithStripHeader(true)` 后落盘内容
  无 `-----BEGIN/END-----` 头，openssl/gmsm x509 直读失败（本包 `ParsePrivateKeyFromPem`
  有自动补头所以能读回，属一厢情愿）→ 新增 `ensurePEMHeader`，`SaveKeyPair` **恒写标准 PEM**
  （私钥 `PRIVATE KEY`、公钥 `PUBLIC KEY`，块类型与 gmsm `x509/utils.go` 写出的一致），
  与 Option 无关；`WithStripHeader` 只影响返回值。
  - 守护：`TestSaveKeyPairFiles` 新增两个子测试——去头内容落盘仍含标准头且可回读、
    同路径重复落盘被原子覆盖。
- **密钥落盘非原子写，中断留下半截文件（R2-P2-2）**：`saveToFile` 原为单次
  `os.WriteFile`，写入中断会产生不完整 PEM，下次解析失败且旧文件已被截断 →
  改为写 `filename.tmp` 后 `os.Rename` 原子替换（同分区原子），失败时清理临时文件；
  错误值保持 os 原始错误不变。
- **Result.ToHex 对 65 字节 0x04 明文静默丢首字节（数据损坏，R3-P2）**：历史特例
  为“公钥裸 Hex 去 0x04 前缀”而设，但三轮评审的反证显示其在导出 API 上
  **唯一命中路径是解密出 65 字节且首字节恰为 0x04 的明文**（概率 1/256）
  ——`Result.data` 私有、外部无法注入公钥字节，包内 Encrypt 结果 ≥ 113 字节、
  Sign（DER）≥ 70 字节均不会命中 65 字节 → 拆除特例，`ToHex` 恒原样
  `hex.EncodeToString`；公钥 Hex 场景本就有专用 `PublicKeyToHex`。
  - 影响面：Decrypt 出的 65 字节 0x04 开头明文经 `ToHex` 转换（此前丢首字节）；
    加密/签名/哈希结果长度不可能命中，行为不变。
  - 守护：`TestDecryptToHexKeeps65Byte04Plaintext`（65 字节 0x04 明文往返转 Hex 断言原样）。

### 安全（待办）

- **git 历史已入库私钥尚未清理（R2-P0-1，首轮报告唯一 P0）**：`sm/cert/` 下 5 个密钥/证书
  文件随提交 `ee0504b1`、`b8f59bf3` 进入历史，仅删除当前目录不清除历史——任何 clone 过
  仓库的人都能用 `git log --all -- sm/cert/private.key` 找回。登记待办步骤：
  1. ~~**先确认**：这些私钥是否曾在任何生产环境使用过~~ **已完成（2026-10-01 确认）**：
     经仓库负责人确认，这 5 个密钥/证书**从未在任何生产或真实环境使用过**——
     无轮换必要，**不构成安全事件**；风险定性从“已泄露密钥”降级为
     “历史卫生债”（仅当仓库公开或外泄时才需重新评估）；
  2. 历史重写：`git filter-repo --path sm/ --invert-paths`，全团队 force push 并重新 clone
     （不可逆 + 需团队协调，已决策**暂不执行**，待协调后再授权）；
  3. CI 守护：历史清理后再加 `.github/workflows/secret-scan.yml`（gitleaks），防止复发
     （先清历史再扫描，否则存量告警会淹没新增告警）。

### 兼容性说明

- **密文字节级兼容 gmsm 上游**：`gosm4` 本地实现的输出与 `sm4.Sm4Ecb/Sm4Cbc` 逐字节一致
  （oracle 测试守护），`internal/config` 存量 Nacos `ENC()` 密文**零迁移**。
- **导出 API 不变**：三子包全部导出函数签名、成功路径输出、历史错误文本
  （`SM4: invalid iv size`、`failed to set IV`、`SM4: invalid key size N` 等）保持原样；
  本轮新增的错误仅覆盖此前"静默失败"的填充校验路径。
- **分包结构不变**：`gosm2/gosm3/gosm4` 一算法一包，沿用 Go 标准库 `crypto/<alg>` 与
  上游 gmsm `sm2/sm3/sm4` 的分包惯例；包间无共享状态，互不引入。
