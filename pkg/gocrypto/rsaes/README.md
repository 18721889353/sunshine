# rsaes

`RSAES-PKCS1-v1_5`（RFC 8017，Java 侧叫 `RSA/ECB/PKCS1Padding`）填充的加解密兼容层。

<br>

## 为什么存在这个包

Go 1.26 起，`crypto/rsa` 把 PKCS #1 v1.5 的加解密入口**全部**标记为 `Deprecated`：

| 被弃用的入口 | 官方给的替代 |
|--------------|--------------|
| `rsa.EncryptPKCS1v15` | `rsa.EncryptOAEP` |
| `rsa.DecryptPKCS1v15` | `rsa.DecryptOAEP` |
| `rsa.DecryptPKCS1v15SessionKey` | 无（建议改协议） |
| `rsa.PKCS1v15DecryptOptions` | `rsa.OAEPOptions` |

对能被自己控制两端的新接口来说，照官方建议切 OAEP 就是正确答案。但 `gocrypto` 与 `gorsa`
承担的是**存量协议对接**：Java 服务写死 `RSA/ECB/PKCS1Padding`、部分云厂商开放接口只收
v1.5 密文。换填充等于单方面断协议，不是可选项。

于是把这件事收敛成一个包：一份实现、一份说明、一组跨实现测试，两个调用方共用。
调用点不再出现弃用 API，静态检查（`staticcheck` SA1019）归零，兼容能力也没有丢。

<br>

## 架构概览

```
pkg/gocrypto/rsaes/
├── rsaes.go                  # Encrypt（仓内实现公钥运算）+ Decrypt（复用 crypto.Decrypter）
├── rsaes_test.go             # 单元测试：往返、边界、弱密钥拒绝、错误值统一性
├── fuzz_test.go              # 不变量模糊测试：往返一致 或 ErrMessageTooLong，不允许第三种结局
├── benchmark_test.go         # 基准：本包 Encrypt/Decrypt + 标准库弃用入口作对照
└── integration_test.go       # `-tags=integration`：请 openssl 当外部对端，双向互操作
```

**核心设计原则**：

- **加密侧自己写，解密侧借用标准库**。加密只做公钥运算 `c = m^e mod n`：公钥和密文都是公开量，
  没有需要保密的时间侧信道，实现只有 20 行；解密要碰私钥，必须用标准库的
  **常量时间模幂 + 常量时间去填充校验**，自己重写既容易出错也必然更弱。
- **解密走 `crypto.Decrypter` 接口方法** `(*rsa.PrivateKey).Decrypt(rand, ct, nil)`。
  按标准库文档，`opts` 为 nil 就是 PKCS #1 v1.5 解密；这条路不是「绕开弃用标记」，
  而是 HSM/KMS 密钥本来就用的入口，安全实现仍由 Go 官方维护。
  读过的实现细节：私钥模幂是 `crypto/internal/fips140/bigmod` 的 4 bit 窗口幂，
  查表靠 `assign(ctEq(...))` 做常量时间选择；去填充靠 `subtle` 扫描分隔符并校验 PS ≥ 8 字节。
  注意 Go 1.27 的这条路**已没有旧的盲化步骤**（改成靠常量时间窗口幂 + CRT 防护），
  所以不要把「本包继承盲化」当成已知前提写进注释。
- **失败原因只有一种**。非法密文一律塌缩到 `rsa.ErrDecryption`，不区分「长度不对」「填充错」
  「密文过大」——能被外部观测的失败差异就是 Bleichenbacher 百万消息攻击的 oracle。
  测试里用 6 类畸形输入断言同一错误值，防的就是日后有人「为了好排错」把错误拆细。
- **不放大弱密钥的可用面，也不给 panic 留入口**。模数必须为正奇数且 ≥ 1024 位，
  指数必须是 ≥ 3 的奇数且不超过 2^31-1，五项判定与标准库 `checkPublicKey` 对齐。
  指数与模数符号两项不能省：`big.Int.Exp` 的文档语义是 `z = x**y mod |m|`，对非法入参是**静默改语义**
  而不是报错——负指数会转去算模逆（给出一个「看起来正常」的结果），负模数被当正模数用下去，
  而 `m == 0` 时根本不取模、直接返回整个 `x**y`，到 `FillBytes` 才因结果超出 `k` 字节而 panic。
- **只做填充，不碰密钥编码与业务**。PEM/PKIX/PKCS#1/PKCS#8 的解析仍由调用方负责。

<br>

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| PKCS #1 v1.5 的填充与去填充 | 是 | 本包 |
| 私钥模幂与去填充的常量时间实现 | 否（复用标准库） | `crypto/rsa` 及其内部 `fips140/bigmod` |
| PEM / PKIX / PKCS#1 / PKCS#8 密钥解析 | 否 | `pkg/gocrypto`、`pkg/gorsa` |
| OAEP 加解密（新对接首选） | 否 | `crypto/rsa` 的 `EncryptOAEP` / `DecryptOAEP` |
| PKCS#1 v1.5 签名与验签 | 否（标准库**未**弃用） | `rsa.SignPKCS1v15` / `VerifyPKCS1v15` |

复核命令：`grep -rn "EncryptPKCS1v15\|DecryptPKCS1v15" pkg/gocrypto/rsa.go pkg/gorsa/gorsa.go`（应无结果）

> 签名类 API 不在本包范围：Go 只弃用了 v1.5 的**加密**，`SignPKCS1v15` / `VerifyPKCS1v15` 依然推荐
> （v1.5 签名的问题是可证明安全性，靠加盐的 PSS 解决，但没有被弃用）。`gorsa` 的 SHA1WithRSA
> 签名路径因此保持直接调用标准库。

<br>

## 使用场景选择

### 场景一：对端协议写死了 PKCS#1 v1.5（`rsaes.Encrypt` / `rsaes.Decrypt`）

**适用场景**：Java 侧 `RSA/ECB/PKCS1Padding`、只接受 v1.5 密文的云厂商开放接口。

```go
package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/18721889353/sunshine/pkg/gocrypto/rsaes"
)

func main() {
	pubBlock, _ := pem.Decode(pubPEM)
	if pubBlock == nil {
		panic("公钥不是 PEM 格式")
	}
	pubAny, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if err != nil {
		panic(err)
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		panic("不是 RSA 公钥")
	}

	ciphertext, err := rsaes.Encrypt(pub, []byte("敏感字段"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("密文长度 %d 字节\n", len(ciphertext))
}
```

（完整可运行示例见 `pkg/gocrypto/rsa.go` 的 `RsaEncrypt` / `RsaDecrypt`，本包通常不直接调用。）

**内部行为**：

1. 校验公钥形状：模数存在且为正、位长 ≥ 1024、为奇数，指数为 ≥ 3 且 ≤ 2^31-1 的奇数，任一项不满足直接返回错误。
2. 校验明文长度 ≤ `k-11`，超出返回 `rsa.ErrMessageTooLong`（与直接调用标准库时同一个值）。
3. 组装 `EM = 0x00 || 0x02 || PS || 0x00 || M`，`PS` 为 `k-len(M)-3` 个**非零**随机字节。
4. `c = EM^e mod N`，用 `FillBytes` 定长输出 `k` 字节（**含前导零字节**，长度不固定就不是合法密文）。

**注意**：`Encrypt` 每次都用新的随机填充，同一明文两次密文不同，**不能**把密文当等值字段做比对或建索引。

### 场景二：新对接、两端都可控 —— 不要用本包

直接用 `crypto/rsa` 的 OAEP：

```go
ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, plaintext, label)
```

v1.5 加密没有可证明安全性，且存在 Bleichenbacher 类选择密文攻击面；OAEP 才是当前推荐做法。
本包只为存量协议保留，不要在新接口里扩散。

<br>

## API 速查

### Encrypt — 公钥加密（RSAES-PKCS1-v1_5）

```go
func Encrypt(pub *rsa.PublicKey, msg []byte) ([]byte, error)
```

- 返回定长 `k` 字节密文，`k = pub.Size()`。
- 明文上界 `k-11`：1024 位密钥为 117 字节，2048 位为 245 字节。需要加密更长的数据请改用
  信封方案（随机 AES 密钥 + 本函数只加密该密钥）。
- 只做公钥运算，不接触秘密，因此不需要盲化；随机量只有填充串 `PS`。

### Decrypt — 私钥解密（RSAES-PKCS1-v1_5）

```go
func Decrypt(priv *rsa.PrivateKey, ciphertext []byte) ([]byte, error)
```

- 内部调用 `priv.Decrypt(rand.Reader, ciphertext, nil)`，即 `crypto.Decrypter` 接口，
  `opts` 为 nil 按标准库约定就是 PKCS #1 v1.5。
- **所有**非法密文返回同一个 `rsa.ErrDecryption`。
- 私钥为 nil 时返回错误而不是 panic。

<br>

## 错误处理

| 场景 | 行为 |
|------|------|
| 公钥为 nil / 缺模数 | `rsaes: missing public modulus` |
| 模数 ≤ 0（负值会被 `big.Int.Exp` 静默当正值用，零值最终在 `FillBytes` panic） | `rsaes: public modulus must be positive` |
| 模数 < 1024 位 | `rsaes: %d-bit keys are insecure (minimum 1024 bits)` |
| 模数为偶数（畸形密钥） | `rsaes: public modulus is even` |
| 指数 < 3 或为偶数 | `rsaes: invalid public exponent %d (must be an odd number >= 3)` |
| 指数 > 2^31-1 | `rsaes: public exponent too large (maximum 2147483647)` |
| 明文超过 `k-11` | `rsa.ErrMessageTooLong`（标准库同一错误值，`errors.Is` 可判） |
| 随机源读取失败 | 透传 `io.ReadFull` 的错误 |
| 私钥为 nil | `rsaes: nil private key` |
| 密文长度不对 / 填充非法 / 密文 ≥ 模数 / 用错私钥 | 统一 `rsa.ErrDecryption` |

**FIPS 140-only 模式说明**：标准库在 `GODEBUG=fips140=only` 下会**拒绝产生也拒绝解出** v1.5 密文
（`EncryptPKCS1v15` 与 `decryptPKCS1v15` 入口都有 `fips140only.Enforced()` 判定）。
本包的 `Decrypt` 经标准库路径，因此**仍会被拦住**；`Encrypt` 则**不复刻**该门控，
因为 `crypto/fips140` 只导出 `Enabled()`（`on` 与 `only` 都为 true），无法区分二者，
硬判会误伤 `on` 模式。需要在 FIPS 环境合规的部署，请整链路改用 OAEP，不要依赖本包。

<br>

## 集成测试

`integration_test.go` 带 `//go:build integration`，依赖本机 `openssl` 可执行文件，
用**外部实现**当对端——只用 Go 自己往返证明不了 Java/OpenSSL 能解开。

| 变量 | 必填 | 用途 | 默认值 |
|------|------|------|--------|
| `OPENSSL_BIN` | 否 | 指定 openssl 可执行文件；找不到 openssl 时整套自动 Skip | `openssl`（走 PATH） |

```bash
cd pkg/gocrypto/rsaes

# 跨实现互操作（双向）
go test -tags=integration -count=1 -v

# 仅单元测试（无需 openssl，日常使用）
go test ./ -count=1 -v
```

三个用例：

1. `TestIntegration_OpenSSLDecryptsOurCiphertext`：本包加密 → `openssl pkeyutl -decrypt` 还原，
   覆盖 ASCII / 中文 / 含零字节 / 空明文 / 达到上界 `k-11` 五类明文。
2. `TestIntegration_OpenSSLEncryptsForOurDecrypt`：`openssl pkeyutl -encrypt -pkeyopt rsa_padding_mode:pkcs1`
   加密 → 本包解密还原。
3. `TestIntegration_OpenSSLChecksOurKeyPEM`：Go 生成的 PKCS#1 私钥 PEM 与 PKIX 公钥 PEM
   能被 `openssl rsa -check` / `-pubin` 直接读入（对端读不了就等于接不上现成运维流程）。

**实测结果汇总**（Windows 10 + Git Bash，OpenSSL 1.1.1m 2021-12-14，
`go test -tags=integration -count=1`，总耗时 0.652s）：

| 分组 | 结果 | 关键读数 |
|------|------|----------|
| openssl 解本包密文 | PASS（5 个子用例） | 单个 0.02s，含空明文与 245 字节上界明文 |
| 本包解 openssl 密文 | PASS（3 个子用例） | 单个 0.02s |
| Go 密钥 PEM 被 openssl 读入 | PASS | `openssl rsa -check -noout` 退出码 0 |

<br>

## 性能基线与模糊测试

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 ./pkg/gocrypto/rsaes/
```

> 诚实声明：本机（Windows/MinGW）`-race` 依赖 cgo 且历史上报 `0xc0000139`，
> **本轮未采集 `-race` 读数**，真实证据需在 Linux/CI 上采集。
> 本包无共享可变状态（`Encrypt`/`Decrypt` 每次只用局部变量与只读入参），竞态面限于调用方自己传入的切片。

### 基准测试

2048 位密钥、32 字节明文（`-benchtime=100000x`，`count=3`，Intel i7-10750H @ 2.60GHz，Windows/amd64）：

| 基准 | 场景 | ns/op | B/op | allocs/op |
|------|------|-------|------|-----------|
| `BenchmarkEncrypt` | 本包公钥加密 | 40442 ~ 40633 | 5188 | 10 |
| `BenchmarkEncryptStdlibBaseline` | 标准库弃用入口同样工作量 | 32033 ~ 32294 | 1376 | 9 |
| `BenchmarkDecrypt` | 本包私钥解密（走标准库） | 1163722 ~ 1180728 | 256 | 1 |

> B/op 与 allocs/op 是另一次 `-benchtime=10000x -benchmem -count=2` 采集的（ns/op 为 43162~43277 /
> 33035~33512 / 1149211~1201767），与上表不同轮，跨轮只比对 allocs 口径——本仓惯例。

**口径与解读**：

- 不含任何网络 IO，量的是纯密码学开销；`Decrypt` 比 `Encrypt` 慢 ~29 倍是 RSA 本身的性质
  （私钥指数 `d` ≈ 模数长度 vs 公钥指数 `e=65537`），不是本包实现的问题——
  而且解密完全走标准库，读数与直接调用 `DecryptPKCS1v15` 等价。
- **本包 `Encrypt` 比标准库慢约 25%（40.5µs vs 32.1µs）、多分配约 3.8KB（5188B vs 1376B）**，
  allocs 只差 1 个（10 vs 9），说明差距在单次分配的体积而不是次数。
  根因：标准库的裸模幂在 `crypto/internal/fips140/bigmod` 里——定长 `Nat` + Montgomery 乘，
  模数上下文预先算好，中间量基本不碰堆（加密路径用 `ExpShortVarTime`，指数是公开的 65537）；
  仓内只能走 `math/big.Int.Exp`，它的中间 `nat` 会按位长增长而重新分配。
  取舍：8µs / 3.8KB 是单次 RSA-2048 公钥加密的量级，而 v1.5 在本框架里只用来加密小字段
  （口令、AES 会话密钥），一次请求的网络 RTT 就是它的十倍以上；若日后要压这 8µs，
  正确做法是推动 Go 导出裸模幂（或整体切 OAEP），而不是在本包里手写 Montgomery 约减。
- `ns/op` 受机器状态影响会波动，跨轮对比以 `allocs/op` 为准（本仓惯例）。

### 模糊测试

| Target | 不变量 |
|--------|--------|
| `FuzzEncryptDecryptRoundTrip` | 任意明文经本包加密再解密，只有两种合法结局：原样还原，或因超过 `k-11` 返回 `rsa.ErrMessageTooLong`；出现第三种结局（panic / 解密失败 / 还原成别的内容）即为缺陷 |

实测：`go test -run '^$' -fuzz FuzzEncryptDecryptRoundTrip -fuzztime=10s` → PASS，
10s 内 4060 execs、新增 interesting 输入 10 个（判定标准是 PASS 本身，execs 跨轮波动不作对比依据）。
