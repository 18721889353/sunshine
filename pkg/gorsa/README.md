# gorsa

RSA 非对称加解密与签名的业务封装，面向「和 Java 服务对接」这类存量协议场景：入参统一是
`base64(PEM)` 字符串，填充与签名方案与 Java 侧一一对应。

<br>

## 架构概览

```
pkg/gorsa/
├── gorsa.go                        # 密钥生成/读写、v1.5 加解密、SHA1WithRSA 签名验签
├── gorsa_test.go                   # 单元测试：往返、跨实现等价、弱密钥与畸形入参拒绝
└── example/
    ├── main.go                     # 可直接跑的调用样例
    └── mobile_public_key.pem       # 样例公钥
```

填充算法本身不在这里：PKCS#1 v1.5 的加解密实现收敛在
[`pkg/gocrypto/rsaes`](../gocrypto/rsaes/README.md)，本包只做「密钥编码编排 + 入参约定 + 错误传递」。

**核心设计原则**：

- **入参形态只有一种：`base64(PEM)`**。不是裸 PEM，也不是 `*rsa.PrivateKey`。
  对端（多为 Java/前端）传进来的就是这一串，本包不解一层就接不上。
- **密钥解析只有两处：`parsePrivateKey` / `parsePublicKey`**。签名、解密、私钥加密三个入口
  过去各抄了一遍解析逻辑，且都抄成了同一个静默失败（PKCS#8 解析出错时返回零值 + `nil` 错误）；
  现在收敛成一处，错误用 `%w` 包装透传。
- **私钥两种块类型都要吃得下**：`PRIVATE KEY`（PKCS#8）与 `RSA PRIVATE KEY`（PKCS#1）。
  对接方给哪一种不可控，测试对两种编码各断言一次。
- **签名与加密是两条路，不能混用**。`EncryptByPrivateKey` 名字叫「加密」，实际产出的是
  `rsa.SignPKCS1v15` 的签名，可用 `Verify` 验；它不产生可解密的密文。
- **不放大弱密钥的可用面**。模数短于 1024 位、指数非 `3..2^31-1` 内奇数的公钥一律拒绝，
  这条下限由 `rsaes` 保证，本包不绕过它。

<br>

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| `base64(PEM)` 入参的解析与编排 | 是 | 本包 |
| PKCS#1 v1.5 的填充与去填充 | 否 | `pkg/gocrypto/rsaes` |
| 私钥模幂的常量时间实现 | 否 | `crypto/rsa`（经 `rsaes.Decrypt`） |
| SHA1WithRSA 签名与验签 | 是 | 本包（直接调 `rsa.SignPKCS1v15` / `VerifyPKCS1v15`，标准库未弃用） |
| OAEP 加解密（新对接首选） | 否 | `crypto/rsa` 的 `EncryptOAEP` / `DecryptOAEP` |
| 长数据加密（信封方案） | 否 | `pkg/gocrypto`（RSA 加密 AES 会话密钥 + AES 加密正文） |

复核命令：`grep -rn "EncryptPKCS1v15\|DecryptPKCS1v15" pkg/gorsa/gorsa.go`（应无结果）

<br>

## 使用场景选择

### 场景一：Java 侧 `RSA/ECB/PKCS1Padding` 的字段加解密

**适用场景**：对端把填充方案写死为 PKCS#1 v1.5，只传输 `base64` 密文串。

```go
package main

import (
	"fmt"

	"github.com/18721889353/sunshine/pkg/gorsa"
)

func main() {
	// 生成密钥对：返回 [base64(PKIX 公钥), base64(PKCS#8 私钥)]
	keys, err := gorsa.GenerateKey()
	if err != nil {
		panic(err)
	}
	publicKey, privateKey := keys[0], keys[1]

	// 明文长度上限 = 模数字节数 - 11（KEYSIZE=1024 时为 117 字节）
	encrypted, err := gorsa.EncryptByPublicKey([]byte("敏感字段"), publicKey)
	if err != nil {
		panic(err)
	}
	fmt.Printf("密文（base64）：%s\n", encrypted)

	decrypted, err := gorsa.DecryptByPrivateKey(encrypted, privateKey)
	if err != nil {
		panic(err)
	}
	fmt.Printf("还原：%s\n", decrypted)
}
```

**内部行为**：

1. `base64` 解码入参 → `pem.Decode` → 按块类型选 PKCS#8 或 PKCS#1 解析私钥；公钥走 PKIX。
2. 加密委托 `rsaes.Encrypt`：校验公钥形状 → 校验明文 ≤ `k-11` → 组装
   `EM = 0x00 || 0x02 || PS || 0x00 || M` → `c = EM^e mod N` → 定长 `k` 字节。
3. 结果再套一层 `base64` 返回；解密侧先 `base64` 解码，再委托 `rsaes.Decrypt`。

**注意**：

- 密文每次都用新的随机填充，**同一明文两次密文不同**，不能把密文当等值字段比对或建索引。
- 需要加密超过 `k-11` 字节的数据时，`EncryptByPublicKey` 返回 `rsa.ErrMessageTooLong`；
  请改信封方案（随机 AES 密钥 + 本函数只加密该密钥），不要拆成多段 RSA。

---

### 场景二：SHA1WithRSA 签名与验签

**适用场景**：对接方要求 `Signature=SHA1WithRSA`（Java `Signature.getInstance("SHA1WithRSA")`）。

```go
signature, err := gorsa.Sign([]byte("payload"), privateKey)
if err != nil {
	panic(err)
}

ok, err := gorsa.Verify([]byte("payload"), publicKey, signature)
if err != nil || !ok {
	panic(fmt.Sprintf("验签失败: ok=%v err=%v", ok, err))
}
```

**内部行为**：先 `sha1.Sum(data)`，再 `rsa.SignPKCS1v15(..., crypto.SHA1, hashed)`，产物 `base64` 返回；
`Verify` 对称地做 `rsa.VerifyPKCS1v15`。

**注意**：`Verify` 的两个失败维度要分开看——签名不匹配时返回 `(false, err)`，入参非法时同样返回
`(false, err)`，不要只判 `ok` 而丢弃 `err`（排错全靠它）。

---

### 场景三：新对接、两端都可控 —— 不要用本包

v1.5 加密没有可证明安全性，且有 Bleichenbacher 类选择密文攻击面；SHA1 签名同理偏弱。
新接口请用 `rsa.EncryptOAEP(sha256.New(), ...)` 与 `rsa.SignPSS`。本包只为存量协议保留。

<br>

## API 速查

### GenerateKey — 生成密钥对

```go
func GenerateKey() ([]string, error)
```

- 返回 `[base64(PKIX 公钥), base64(PKCS#8 私钥)]`，长度由包级常量 `KEYSIZE`（当前 1024）决定。
- **注意**：见下方「已知偏离」——1024 位是 v1.5 兼容下限档，不是现代推荐强度。

### EncryptByPublicKey / DecryptByPrivateKey — v1.5 加解密

```go
func EncryptByPublicKey(data []byte, publicKeyStr string) ([]byte, error)
func DecryptByPrivateKey(data []byte, privateKeyStr string) ([]byte, error)
```

- 入参 `publicKeyStr` / `privateKeyStr` 均为 `base64(PEM)`；`DecryptByPrivateKey` 的 `data` 是加密返回值本身。
- 私钥 PEM 支持 PKCS#8 与 PKCS#1 两种块类型。
- **注意**：所有非法密文（长度不对 / 填充非法 / 密文 ≥ 模数 / 用错私钥）都塌缩成同一个
  `rsa.ErrDecryption`，这是刻意设计——可被外部观测的失败差异就是选择密文攻击的 oracle。

### EncryptByPrivateKey — 私钥加密（实为签名）

```go
func EncryptByPrivateKey(data []byte, privateKeyStr string) ([]byte, error)
```

- 产物等价于 `Sign` 的签名（`SHA1` + `rsa.SignPKCS1v15`），只能用 `Verify` 校验，**不能**被
  `DecryptByPrivateKey` 解密。名字是历史包袱，勿据此理解为加密。

### Sign / Verify — SHA1WithRSA 签名与验签

```go
func Sign(data []byte, privateKeyStr string) (string, error)
func Verify(data []byte, publicKeyStr string, sign string) (bool, error)
```

- `Sign` 返回 `base64` 签名字符串；`Verify` 的 `sign` 即该字符串。
- **注意**：哈希固定 SHA1，没有可选哈希的入口——需要 SHA256 签名请用 `pkg/gocrypto`。

### LoadPublicKeyFromFile / LoadPrivateKeyFromFile / SaveKeyToPemFile — 密钥读写

```go
func LoadPublicKeyFromFile(filename string) (string, error)
func LoadPrivateKeyFromFile(filename string) (string, error)
func SaveKeyToPemFile(publicKeyBase64, privateKeyBase64, publicKeyFile, privateKeyFile string) error
```

- `Load*` 读 PEM 文件并返回本包 API 需要的 `base64(PEM)` 形态（会先 `pem.Decode` 再规范化编码）。
- `SaveKeyToPemFile` 写入的是**裸 PEM**（先把入参 base64 解一层），公钥 `0644`、私钥 `0600`。
- **注意**：私钥落盘即敏感文件，不要写到仓库可见路径；Windows 的 `FileMode` 不反映真实 ACL，
  权限位只在 unix 语义下生效。

<br>

## 错误处理

| 场景 | 行为 |
|------|------|
| 密钥入参不是合法 base64 | 透传 `base64` 解码错误 |
| base64 解出来不是 PEM | `failed to decode PEM block containing public/private key` |
| 私钥块类型既非 PKCS#8 也非 PKCS#1 | `unsupported private key format` |
| PKCS#8 块内容畸形 | `failed to parse PKCS#8 private key: <原因>`（`errors.Is`/`%w` 可判） |
| PKCS#8 块装的不是 RSA 密钥 | `not an RSA private key` |
| 公钥块装的不是 RSA 公钥（如 EC） | `not an RSA public key` |
| 公钥形状非法（模数 < 1024 位 / 偶数，指数非法） | 由 `rsaes.checkPublicKey` 返回对应错误 |
| 明文超过 `k-11` | `rsa.ErrMessageTooLong` |
| 密文非法（各类） | 统一 `rsa.ErrDecryption` |
| 验签不通过 | `(false, err)`，`err` 来自 `rsa.VerifyPKCS1v15` |

<br>

## 测试交付物

`gorsa_test.go`（9 个顶层用例 + 子用例，语句覆盖率实测 **89.8%**，
`go test ./pkg/gorsa/ -count=1 -cover`，本机 Windows/amd64）：

| 用例 | 守护的是什么 |
|------|--------------|
| `TestEncryptDecryptRoundTrip` | 空/含零/中文/达上界 `k-11` 明文，在 PKCS#8 与 PKCS#1 两种私钥编码下都能还原 |
| `TestEncryptCiphertextIsFixedLengthAndRandomized` | 密文定长 `k`、同明文两次不同、密文整数 < 模数 |
| `TestEncryptRejectsOversizedPlaintext` | 超长仍返回 `rsa.ErrMessageTooLong`，错误值不被改写 |
| `TestInteropsWithStdlibReference` | 与标准库双向互操作：标准库能解本包密文，本包能解标准库密文 |
| `TestDecryptRejectsInvalidInput` | 各类非法输入（含「PKCS#8 解析失败必须给错误而非 nil」） |
| `TestEncryptByPublicKeyRejectsBadPublicKey` | 非 base64 / 非 PEM / 畸形 PKIX / EC 公钥 / 短模数全部拒绝 |
| `TestSignAndVerify` | 两种私钥编码签名可验、篡改与换钥必须失败、静默 nil 错误不复发 |
| `TestGenerateKeyProducesUsablePair` | 生成的密钥对本包 API 立即可用（兜住 `KEYSIZE` 与 `rsaes` 下限一致） |
| `TestSaveAndLoadKeyFiles` | 文件读写往返 + 私钥权限位断言（仅 unix 生效，Windows 明确跳过） |

> 本包**没有**独立的集成测试：它是纯本地计算，跨实现（openssl/Java）互操作的证据由
> `pkg/gocrypto/rsaes/integration_test.go` 提供（`-tags=integration`，实测 OpenSSL 1.1.1m 三组全 PASS）。
> 本包用标准库入口作参照实现 oracle，测的是「协议等价」而不是「自证往返清白」。

<br>

## 已知偏离与建议

- **`KEYSIZE = 1024` 低于现代推荐强度（2048 起）**。这是一个**协议可见**的参数：
  改它会同时改变密文字节数（128 → 256）与 base64 长度，存量对接方若有写死的字段宽度/列长度就会断。
  因此本次**不擅自改**，只在此登记。要改请连同对端一起评估，并同步本包的明文上界
  （`k-11`：1024 位是 117 字节，2048 位是 245 字节）。
- **签名哈希固定 SHA1**。同上，属对端约定，改它要双方同步升级验签实现。
- **`EncryptByPrivateKey` 的命名与实际语义不符**（它是签名）。保留是为了不打断存量调用；
  新代码请直接使用 `Sign`。
