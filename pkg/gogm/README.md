# gogm

国密算法（SM2/SM3/SM4）封装集：提供密钥管理、加解密、签名验签与摘要计算的统一 Go API，
基于 `github.com/tjfoc/gmsm` 实现，并对上游的并发与错误处理缺陷做本地加固。

## 架构概览

```
pkg/gogm/
├── README.md              # 本文件：三子包总览、场景、API 速查、实测基线
├── CHANGELOG.md           # 显著变更记录
├── gosm2/                 # SM2 非对称：密钥管理、加解密、签名验签
│   ├── gosm2.go           # 全部实现（Option / 密钥 / 格式转换 / 加解密 / 签名）
│   ├── gosm2_test.go      # 12 个单元测试
│   ├── test_helpers_test.go
│   ├── fuzz_test.go       # 3 个 fuzz 目标
│   └── benchmark_test.go  # 3 个基准
├── gosm3/                 # SM3 摘要
│   ├── gosm3.go           # Hash / HashString 封装
│   ├── gosm3_test.go      # GB/T 32905—2016 标准向量 + 不变量
│   ├── test_helpers_test.go
│   ├── fuzz_test.go       # FuzzHashInvariants
│   └── benchmark_test.go  # 2 个基准
└── gosm4/                 # SM4 对称：ECB / CBC + PKCS7 填充
    ├── gosm4.go           # 本地 CBC/ECB 实现（无全局状态）
    ├── gosm4_test.go      # 10 个单元测试（含上游字节级兼容 oracle）
    ├── test_helpers_test.go
    ├── fuzz_test.go       # 2 个 fuzz 目标
    └── benchmark_test.go  # 4 个基准
```

合计：24 个单元测试 + 6 个 fuzz 目标 + 9 个基准（三包测试体系对称）。

**核心设计原则**：

1. **一算法一包**：沿用 Go 标准库 `crypto/<alg>` 与上游 gmsm `sm2/sm3/sm4` 的分包惯例，
   调用方按算法取用，三包之间零依赖、零共享状态。
2. **统一构造风格**：三包均为 `NewSMx(opts ...SMxOption)` + 独立 Option 函数，用法一致、可迁移。
3. **链式结果延迟报错**：加解密/哈希返回 `*XxxResult`，错误统一在 `ToBytes/ToHex/ToBase64`
   取出，调用点单处分叉，不在构造路径 panic。
4. **无全局状态**：`gosm4` 的 IV 与密钥只存在于调用栈（显式传入 + 防御性拷贝），
   规避 gmsm v1.4.1 上游包级全局 `var IV` 的并发竞态（详见 CHANGELOG R1-P0-1）。
5. **字节级兼容上游**：SM4 密文与 `sm4.Sm4Ecb/Sm4Cbc` 逐字节一致（oracle 测试守护），
   `internal/config` 存量 Nacos `ENC()` 密文零迁移。

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| SM4 ECB/CBC 加解密、PKCS7 填充校验 | 是 | 本包 `gosm4` |
| SM2 密钥生成 / 格式转换 / 加解密 / 签名验签 | 是 | 本包 `gosm2` |
| SM3 摘要计算 | 是 | 本包 `gosm3` |
| SM2/SM3/SM4 算法本体（曲线运算、压缩函数、S 盒） | 否 | `github.com/tjfoc/gmsm` |
| Nacos 配置里 `ENC(hex)` 前缀的识别与提取 | 否 | `internal/config`（本包只提供 Hex 密文解密入口） |
| 密钥托管 / 轮换 / KMS | 否 | 不在本仓，需外部 KMS |
| 国密证书链与 X.509 验证 | 否 | `gmsm/x509`（本包仅借其做密钥编解码） |
| 密码存储哈希（bcrypt/argon2 类） | 否 | 非 SM3 职责，见下方场景三说明 |

复核命令：`grep -rniE "kms|vault|keyring" pkg/gogm/`（应无结果）。

## 使用场景选择

### 场景一：SM4-CBC 加解密 Nacos `ENC()` 配置密文（`EncryptCBC` / `DecryptCBCFromHex`）

**适用场景**：配置中心等场景下，用 16 字节密钥 + 16 字节 IV 对字符串做对称加解密。

```go
package main

import (
	"fmt"
	"log"

	"github.com/18721889353/sunshine/pkg/gogm/gosm4"
)

func main() {
	key := []byte("nacosSm4Key16byt") // 16 字节
	iv := []byte("nacosSm4Iv16byt")   // 16 字节，每次调用显式传入

	// 配置解析场景必须关闭 HTML 转义，保证 & < > 等字符原样进出
	enc := gosm4.NewSM4(gosm4.WithUnescapeHTML(false))
	hexCipher, err := enc.EncryptCBC([]byte("db.password=P@ss&word"), key, iv).ToHex()
	if err != nil {
		log.Fatalf("加密失败: %v", err)
	}
	fmt.Println("ENC(" + hexCipher + ")")

	// 解密 ENC(hex) 提取出的密文
	dec := gosm4.NewSM4(gosm4.WithUnescapeHTML(false))
	plaintext, err := dec.DecryptCBCFromHex(hexCipher, key, iv).ToBytes()
	if err != nil {
		log.Fatalf("解密失败: %v", err)
	}
	fmt.Println(string(plaintext)) // db.password=P@ss&word
}
```

**内部行为**：

1. 加密：先校验 IV 长度（≠16 字节即报 `failed to set IV: SM4: invalid iv size`），
   再由 `sm4.NewCipher` 校验密钥长度，随后明文做 PKCS7 填充到 16 字节整倍数并 CBC 加密。
2. 解密：同样先校验 IV、再校验密钥，密文长度必须是 16 字节整倍数，解密后逐字节校验 PKCS7
   填充，任一步失败即返回错误——**不会静默返回空数据，也不会对空输入 panic**。
3. 全程不读写任何全局状态：IV 与密钥只存在于本次调用栈内（内部防御性拷贝）。

**注意**：`WithUnescapeHTML` 默认 `true`（历史兼容）——加密/签名入口会先把输入中的
`&amp;` 等 HTML 实体反转义再处理。这会**破坏往返守恒**：`Decrypt(Encrypt(x)) != x`
（加密侧反转义、解密侧不反转义），且签名场景下"签 A 数据"实际签的是"B 数据"。
配置、密码类字段务必显式 `WithUnescapeHTML(false)`，保证 `& < > "` 等字符原样进出。
解密入口不受该选项影响。

---

### 场景二：SM2 密钥对生成、签名验签与加解密（`GenerateKeyPair` / `Sign` / `Encrypt`）

**适用场景**：非对称签名（令牌、报文防篡改）与公钥加密（密钥交换、小报文加密）。

```go
package main

import (
	"fmt"
	"log"

	"github.com/18721889353/sunshine/pkg/gogm/gosm2"
)

func main() {
	s := gosm2.NewSM2()

	// 1. 生成密钥对：PEM/Hex 双格式，Hex 定宽（私钥 64 位、公钥 128 位）
	keyPair, err := s.GenerateKeyPair()
	if err != nil {
		log.Fatalf("生成密钥对失败: %v", err)
	}

	privateKey, err := s.ParsePrivateKeyFromHex(keyPair.PrivateKeyHex)
	if err != nil {
		log.Fatalf("解析私钥失败: %v", err)
	}
	publicKey, err := s.ParsePublicKeyFromHex(keyPair.PublicKeyHex)
	if err != nil {
		log.Fatalf("解析公钥失败: %v", err)
	}

	// 2. 签名与验签
	data := []byte("order id = 10086")
	signature, err := s.Sign(privateKey, data).ToBytes()
	if err != nil {
		log.Fatalf("签名失败: %v", err)
	}
	if !s.VerifyFromBytes(publicKey, data, signature) {
		log.Fatal("验签失败: 数据被篡改")
	}
	fmt.Println("验签通过")

	// 3. 公钥加密 / 私钥解密（默认 C1C3C2 格式）
	ciphertext, err := s.Encrypt(publicKey, data, gosm2.C1C3C2).ToBytes()
	if err != nil {
		log.Fatalf("加密失败: %v", err)
	}
	plaintext, err := s.Decrypt(privateKey, ciphertext, gosm2.C1C3C2).ToBytes()
	if err != nil {
		log.Fatalf("解密失败: %v", err)
	}
	fmt.Println(string(plaintext)) // order id = 10086
}
```

**内部行为**：

1. `GenerateKeyPair` 生成密钥后，将 `D/X/Y` 分量**左侧补零到 32 字节**再转 Hex，
   保证输出恒为 64/128 位、本包生成的密钥一定能被本包重新解析（前导零不丢失）。
2. PEM 输出可选去头（`WithStripHeader` 只影响返回的显示串）；`SaveKeyPair` 与
   `WithSave` 落盘的**恒为完整标准 PEM**（缺头时自动补齐），与 Option 无关，
   私钥文件权限 `0600`、公钥 `0644`，写入采用临时文件 + rename 原子替换。
3. `Sign` 默认对输入做 HTML 反转义后再签名（与 `Encrypt` 同一开关），同样破坏
   往返守恒——需要原样签名字面串时显式 `WithUnescapeHTML(false)`。
4. **空明文拒绝**：`Encrypt` 对空明文立即返回错误（上游 gmsm `kdf(0)` 恒 false
   会无限重试导致卡死，详见 CHANGELOG R2-P0-2），不会挂起。

**注意**：四种密文格式（`C1C3C2`/`C1C2C3` 及各自 `Compressed`）加密与解密必须成对使用；
压缩格式的密文**必须不含 `0x04` 前缀**，解密入口无条件补前缀（不做"已带前缀"探测——
合法压缩密文首字节有 1/256 概率恰为 `0x04`，探测会误判导致永久解不开）。
从其他库导出的密文若自带 `0x04` 前缀，请改用非压缩格式入口。

---

### 场景三：SM3 摘要计算（`Hash` / `HashString`）

**适用场景**：数据完整性校验、指纹比对、唯一性索引。

```go
package main

import (
	"fmt"
	"log"

	"github.com/18721889353/sunshine/pkg/gogm/gosm3"
)

func main() {
	s := gosm3.NewSM3()

	digest, err := s.HashString("abc").ToHex()
	if err != nil {
		log.Fatalf("计算摘要失败: %v", err)
	}
	fmt.Println(digest)
	// 66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0（GB/T 32905—2016）
}
```

**内部行为**：

1. 输入经 `gmsm sm3.Sm3Sum` 计算，输出恒为 32 字节；`ToHex` 输出恒为 64 位小写 Hex。
2. `HashString(s)` 与 `Hash([]byte(s))` 完全等价。

**注意**：SM3 是**无盐、无密钥**的通用摘要，只适合完整性校验；用户密码存储请用
bcrypt/argon2 等专用算法，不要用 SM3 直接哈希后入库。

## 参数/结构体说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `KeyPair.PrivateKeyPEM` | string | — | 私钥 PEM；`WithStripHeader(true)` 时为去头内容（仅显示用） |
| `KeyPair.PublicKeyPEM`  | string | — | 公钥 PEM，同上 |
| `KeyPair.PrivateKeyHex` | string | — | 私钥 Hex，**恒为 64 位**（前导零补位） |
| `KeyPair.PublicKeyHex`  | string | — | 公钥 Hex，**恒为 128 位**（X/Y 各 32 字节补位） |

`SM2`/`SM3`/`SM4` 是操作句柄，无导出字段，配置一律通过 Option 传入（见下表）。

## Option 列表

| Option | 说明 | 默认值 | 适用 API |
|--------|------|--------|----------|
| `WithRand(reader io.Reader)` | 自定义随机数源 | `crypto/rand.Reader` | `gosm2` 全部 |
| `WithStripHeader(strip bool)` | 返回的 KeyPair 去除 PEM BEGIN/END 头尾（仅影响显示串；`SaveKeyPair` 落盘仍恒为标准 PEM） | `false` | `GenerateKeyPair` |
| `WithSave(privFile, pubFile string)` | 生成成功后自动落盘完整 PEM（私钥 0600 / 公钥 0644，自动建父目录） | 不落盘 | `GenerateKeyPair` |
| `WithUnescapeHTML(unescape bool)` | 加密/签名入口先对输入做 HTML 反转义（**⚠ 默认 true 会改写输入，破坏往返守恒**） | `true` | `gosm2` `Encrypt`/`Sign`、`gosm4` `EncryptECB`/`EncryptCBC` |
| `SM3Option` | 预留（当前无配置项，`NewSM3()` 直接可用） | — | `gosm3` `NewSM3` |

## API 速查

### gosm2（`import "github.com/18721889353/sunshine/pkg/gogm/gosm2"`）

#### NewSM2 — 创建 SM2 操作句柄

```go
func NewSM2(opts ...SM2Option) *SM2
```

- 应用 Option 后返回句柄；无 Option 时即为默认配置。
- 不返回 `error`：当前全部 Option 均为纯值（无 IO、无外部校验），构造不可能失败；
  若未来引入需要校验的选项，将改为双返回值（与 `gocron.New` 同约定）。

#### GenerateKeyPair — 生成 PEM/Hex 双格式密钥对

```go
func (s *SM2) GenerateKeyPair() (*KeyPair, error)
```

- `D/X/Y` 分量补零到 32 字节再转 Hex，输出定宽 64/128 位。
- **注意**：启用 `WithSave` 时落盘失败会返回错误（密钥对不再返回）。

#### SaveKeyPair / SaveKeyPairRaw — 密钥落盘

```go
func (s *SM2) SaveKeyPair(keyPair *KeyPair, privateFile, pubFile string) error
func (s *SM2) SaveKeyPairRaw(privateKeyPem, publicKeyPem, privateFile, pubFile string) error
```

- `SaveKeyPair` **恒写入标准 PEM**（缺头时按 `PRIVATE KEY`/`PUBLIC KEY` 自动补头，
  与 `WithStripHeader` 无关），通用工具（openssl、gmsm x509）可直接解析；
  `SaveKeyPairRaw` 写入传入的原始内容（调用方自行保证完整 PEM）。
- 自动创建父目录；私钥 `0600`、公钥 `0644`；临时文件 + rename 原子写入，
  中断不会留下半截密钥文件。

#### ParsePrivateKeyFromHex / ParsePublicKeyFromHex — Hex 解析密钥

```go
func (s *SM2) ParsePrivateKeyFromHex(hexKey string) (*sm2.PrivateKey, error)
func (s *SM2) ParsePublicKeyFromHex(hexKey string) (*sm2.PublicKey, error)
```

- 公钥要求解码后恰为 64 字节（128 位 Hex），否则上游报 `publicKey is not uncompressed.`。
- **注意**：解析失败错误包装为 `failed to decode hex string: ...`。

#### PrivateKeyToHex / PublicKeyToHex — 密钥转定宽 Hex

```go
func (s *SM2) PrivateKeyToHex(privateKey *sm2.PrivateKey) (string, error)
func (s *SM2) PublicKeyToHex(publicKey *sm2.PublicKey) (string, error)
```

- 前导零自动补位：私钥恒 64 位、公钥恒 128 位。

#### ParsePrivateKeyFromPem / ParsePublicKeyFromPem — PEM 解析密钥

```go
func (s *SM2) ParsePrivateKeyFromPem(pemKey string) (*sm2.PrivateKey, error)
func (s *SM2) ParsePublicKeyFromPem(pemKey string) (*sm2.PublicKey, error)
```

- 输入缺 `-----BEGIN` 头时自动补齐默认头（私钥 `EC PRIVATE KEY`、公钥 `PUBLIC KEY`）。

#### PrivateKeyToPem / PublicKeyToPem — 密钥转 PEM

```go
func (s *SM2) PrivateKeyToPem(privateKey *sm2.PrivateKey) (string, error)
func (s *SM2) PublicKeyToPem(publicKey *sm2.PublicKey) (string, error)
```

- 输出带完整 BEGIN/END 头的标准 PEM。

#### Encrypt — 公钥加密

```go
func (s *SM2) Encrypt(publicKey *sm2.PublicKey, plainTextByte []byte, format EncryptFormat) *EncryptResult
```

- `format` 取 `C1C3C2`（默认）/ `C1C2C3` / `C1C3C2Compressed` / `C1C2C3Compressed`。
- **⚠ 数据改写警告**：默认会先对明文做 `html.UnescapeString` 再加密（`WithUnescapeHTML`），
  `&amp;` 等实体被反转义——`Decrypt(Encrypt(x)) != x`，需原样加密请显式传 `false`。
- **空明文立即报错**：返回 `SM2: 明文不能为空（上游对空明文会无限重试）`，
  不会挂起（上游 `kdf(0)` 恒 false 死循环已拦截）。
- 解密时同格式成对使用。

#### Decrypt 及其输入变体 — 私钥解密

```go
func (s *SM2) Decrypt(privateKey *sm2.PrivateKey, ciphertextByte []byte, format DecryptFormat) *DecryptResult
func (s *SM2) DecryptFromBytes(privateKey *sm2.PrivateKey, byteData []byte, format DecryptFormat) *DecryptResult
func (s *SM2) DecryptFromHex(privateKey *sm2.PrivateKey, hexData string, format DecryptFormat) *DecryptResult
func (s *SM2) DecryptFromBase64(privateKey *sm2.PrivateKey, base64Data string, format DecryptFormat) *DecryptResult
```

- `From*` 变体先解码输入再走 `Decrypt`；解码失败不 panic，错误进入 Result 链。
- **注意**：压缩格式密文解密时自动补 `0x04` 前缀，且**无条件**补——密文本身不得
  已带 `0x04` 前缀（不做过探测，原因见上方场景二注意事项）。

#### Sign — 私钥签名

```go
func (s *SM2) Sign(privateKey *sm2.PrivateKey, dataByte []byte) *SignResult
```

- 默认对输入做 HTML 反转义后签名（与 `Encrypt` 同一开关）。
- **⚠ 数据改写警告**：需要对字面串签名时显式 `WithUnescapeHTML(false)`，
  否则实际签名内容与输入不一致。

#### VerifyFromBytes / VerifyFromHex / VerifyFromBase64 — 公钥验签

```go
func (s *SM2) VerifyFromBytes(publicKey *sm2.PublicKey, dataByte, signatureByte []byte) bool
func (s *SM2) VerifyFromHex(publicKey *sm2.PublicKey, dataByte []byte, hexSignature string) bool
func (s *SM2) VerifyFromBase64(publicKey *sm2.PublicKey, dataByte []byte, base64Signature string) bool
```

- 返回 `bool`（非 Result 链）：签名非法/解码失败一律 `false`，不返回错误。
- **false 含三义，监控需自行分级**：① 输入格式错误（调用方 bug）；② 签名字节被篡改；
  ③ 数据被篡改。若需区分，先自行解码签名再调 `VerifyFromBytes`，对解码错误单独计数。

#### Result.ToHex / ToBase64 / ToBytes — 结果取值

```go
func (r *Result) ToHex() (string, error)
func (r *Result) ToBase64() (string, error)
func (r *Result) ToBytes() ([]byte, error)
```

- 前置步骤的任何错误在此统一返回；成功时 `ToHex` 对数据**原样** hex 编码，
  不做任何字节改写——历史实现对「65 字节且首字节 0x04」裁剪前缀的特例已拆除：
  `Result.data` 私有、外部无法注入公钥字节，该特例在导出 API 上唯一命中路径是
  “解密出 65 字节且首字节 0x04 的明文”（1/256），会静默丢首字节（数据损坏）；
  公钥 Hex 请用 `PublicKeyToHex`（定宽 128 位）。守护：`TestDecryptToHexKeeps65Byte04Plaintext`。

### gosm3（`import "github.com/18721889353/sunshine/pkg/gogm/gosm3"`）

#### NewSM3 — 创建 SM3 操作句柄

```go
func NewSM3(opts ...SM3Option) *SM3
```

- `SM3Option` 为预留（当前无配置项）；不返回 `error`（纯值构造，不可能失败）。

#### Hash / HashString — 摘要计算

```go
func (s *SM3) Hash(data []byte) *HashResult
func (s *SM3) HashString(data string) *HashResult
```

- 输出恒为 32 字节；两入口完全等价。
- **注意**：无盐无密钥，勿用于密码存储。

#### HashResult.ToBytes / ToHex — 结果取值

```go
func (r *Result) ToBytes() ([]byte, error)
func (r *Result) ToHex() (string, error)
```

- `ToHex` 输出恒为 64 位小写 Hex。

### gosm4（`import "github.com/18721889353/sunshine/pkg/gogm/gosm4"`）

#### NewSM4 — 创建 SM4 操作句柄

```go
func NewSM4(opts ...SM4Option) *SM4
```

- 无全局状态：不同句柄、不同 goroutine 可并发使用，互不影响。
- 不返回 `error`：Option 均为纯值，构造不可能失败（与 `NewSM2`/`NewSM3` 同约定）。

#### WithUnescapeHTML — 加密入口 HTML 反转义开关

```go
func WithUnescapeHTML(unescape bool) SM4Option
```

- 默认 `true`（历史兼容）；仅作用于 `EncryptECB`/`EncryptCBC` 的输入预处理，解密不受影响。

#### EncryptECB / DecryptECB 及其输入变体

```go
func (s *SM4) EncryptECB(plaintextByte, keyByte []byte) *EncryptResult
func (s *SM4) DecryptECB(ciphertextByte, keyByte []byte) *DecryptResult
func (s *SM4) DecryptECBFromByte(ciphertextByte []byte, keyByte []byte) *DecryptResult
func (s *SM4) DecryptECBFromHex(ciphertextHex string, keyByte []byte) *DecryptResult
func (s *SM4) DecryptECBFromBase64(ciphertextBase64 string, keyByte []byte) *DecryptResult
```

- 密钥必须 16 字节；`From*` 变体先解码输入，解码失败错误进入 Result 链。
- 密文与 `sm4.Sm4Ecb` 字节级一致。

#### EncryptCBC / DecryptCBC 及其输入变体

```go
func (s *SM4) EncryptCBC(plaintextByte, keyByte, ivByte []byte) *EncryptResult
func (s *SM4) DecryptCBC(ciphertextByte, keyByte, ivByte []byte) *DecryptResult
func (s *SM4) DecryptCBCFromByte(ciphertextByte []byte, keyByte, ivByte []byte) *DecryptResult
func (s *SM4) DecryptCBCFromHex(ciphertextHex string, keyByte, ivByte []byte) *DecryptResult
func (s *SM4) DecryptCBCFromBase64(ciphertextBase64 string, keyByte, ivByte []byte) *DecryptResult
```

- IV 必须 16 字节，**每次调用显式传入**，不读写全局状态，可并发。
- 密文与 `sm4.Sm4Cbc` 字节级一致（标准 CBC + PKCS7）。

#### EncryptResult / DecryptResult / Result — 链式结果

```go
func (r *Result) ToBytes() ([]byte, error)
func (r *Result) ToHex() (string, error)
func (r *Result) ToBase64() (string, error)
```

- 任何前置错误（IV/密钥/长度/填充/解码）在此统一返回。

## 错误处理

| 场景 | 行为 |
|------|------|
| SM4 密钥长度 ≠ 16 字节 | `failed to encrypt/decrypt with SM4 ...: SM4: invalid key size N`（文本来自 gmsm `NewCipher`，原样保留） |
| SM4-CBC IV 长度 ≠ 16 字节 | `failed to set IV: SM4: invalid iv size`（加密与解密入口均**先于**算法执行校验，文本与历史 `sm4.SetIV` 一致） |
| SM4 密文长度非 16 字节整倍数 | `failed to decrypt with SM4 ...: SM4: 密文长度必须是 16 字节的整倍数` |
| SM4 PKCS7 填充校验失败（乱码/截断密文） | `failed to decrypt with SM4 ...: PKCS7 填充校验失败`；空密文同此路径——**返回错误，不 panic、不静默返回 nil** |
| SM2 Hex/Base64 输入解码失败 | `failed to decode hex/base64 string: ...` |
| SM2 空明文加密 | `SM2: 明文不能为空（上游对空明文会无限重试）`（入口拦截，不挂起） |
| SM2 公钥 Hex 非法/长度不足 | `failed to decode hex string: ...`（附 gmsm 上游原因，如 `publicKey is not uncompressed.`） |
| 错误出现时机 | 链式 Result 不在调用时 panic，全部延迟到 `ToBytes/ToHex/ToBase64` 统一返回 |
| 验签失败 | `VerifyFrom*` 返回 `false`（不走 Result 链，无错误串；含格式错误/数据篡改/签名篡改三义） |

## 集成测试

**不适用**：本包是纯算法封装，无数据库、网络、文件系统等外部依赖，全部行为由 24 个单元测试
覆盖（`pkg/gogm/*/*_test.go` 中无 `//go:build integration` 文件，无环境变量要求）；
其中 `TestSaveKeyPairFiles` 涉及临时目录落盘，由 `t.TempDir()` 自动清理。

```bash
# 运行三子包全部单元测试（含 fuzz 种子语料，无需任何环境变量）
go test ./pkg/gogm/... -count=1
```

## 性能基线与模糊测试

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 ./pkg/gogm/...
```

> **诚实声明**：本 README 不声称已经跑过 `-race`。本轮在本机（Windows / MinGW）实测该命令
> 直接失败：`exit status 0xc0000139`（race runtime 因缺 CGO 工具链无法启动），
> **未取得任何 `-race` 通过读数**；真实竞态证据由 CI 补采——`.github/workflows/race.yml`
> 已将 `./pkg/gogm/...` 纳入 Linux 侧 `-race` 扫描范围（与 jwt/tracer/logger 同一 workflow），
> 其中 gosm4 的并发 IV 修复（R1-P0-1）由 `TestCBCConcurrentDifferentIVs`
> （8 goroutine × 100 轮、各自 key/IV）做逻辑级守护，CI `-race` 提供 detector 级证据。

### 基准测试

环境：`windows/amd64`，`11th Gen Intel Core i5-1135G7 @ 2.40GHz`，口径固定 `-benchtime=200000x`。

| 包 | 基准 | 载荷 | ns/op | 吞吐 | B/op | allocs/op |
|----|------|------|-------|------|------|-----------|
| gosm4 | BenchmarkSM4CBCEncrypt | 1KiB | 11194 | 91.48 MB/s | 2728 | 12 |
| gosm4 | BenchmarkSM4CBCDecrypt | 1KiB | 10565 | 96.93 MB/s | 1576 | 11 |
| gosm4 | BenchmarkSM4ECBEncrypt | 1KiB | 11293 | 90.67 MB/s | 2600 | 8 |
| gosm4 | BenchmarkSM4ECBDecrypt | 1KiB | 10952 | 93.50 MB/s | 1448 | 7 |
| gosm2 | BenchmarkGenerateKeyPair | — | 298049 | — | 11830 | 165 |
| gosm2 | BenchmarkEncryptC1C3C2 | 136B | 1553089 | 0.09 MB/s | 76952 | 1558 |
| gosm2 | BenchmarkSign | 136B | 279256 | 0.49 MB/s | 5140 | 100 |
| gosm3 | BenchmarkHash | 1KiB | 9831 | 104.16 MB/s | 1176 | 6 |
| gosm3 | BenchmarkHashString | 1KiB | 9745 | 105.08 MB/s | 1304 | 8 |

**口径与解读（很重要，否则数字会被误读）：**

- 全部读数只含**算法本身 + 结果编组**，不含网络/磁盘 RTT；复测对比机器或版本时必须固定
  `-benchtime=200000x`，否则迭代次数不同不可比。
- **稳定指标看 allocs/op**：SM4/SM3 的 allocs/op 波动很小，是本包开销的真实刻度；
  `BenchmarkEncryptC1C3C2` 的 ns/op 方差大（每次加密含随机 k + 椭圆曲线标量乘），
  其 1558 allocs/op 是 SM2 曲线运算本身的分配，不是编组开销。
- 每条读数来自单轮实测（`-count=1`），未取中位数，±10% 以内的差异不应解读为回归。
  **复测建议三轮取中位**：`go test -bench=. -benchtime=200000x -count=3 ./pkg/gogm/...`
  （与同仓 jwt/gocron 的采样口径一致）。

### 模糊测试

```bash
go test ./pkg/gogm/gosm4/ -fuzz=FuzzCBCEncryptDecryptRoundTrip -fuzztime=10s
go test ./pkg/gogm/gosm4/ -fuzz=FuzzCBCDecryptRobustness -fuzztime=10s
go test ./pkg/gogm/gosm2/ -fuzz=FuzzParseHexNeverPanics -fuzztime=10s
go test ./pkg/gogm/gosm2/ -fuzz=FuzzEncryptDecryptRoundTrip -fuzztime=10s
go test ./pkg/gogm/gosm2/ -fuzz=FuzzSignVerifyRoundTrip -fuzztime=10s
go test ./pkg/gogm/gosm3/ -fuzz=FuzzHashInvariants -fuzztime=10s
```

| Target | 不变量 | 本轮实测（`-fuzztime=10s`） |
|--------|--------|------------------------------|
| `FuzzCBCEncryptDecryptRoundTrip` | 任意输入 `decrypt(encrypt(x)) == x`，且全程不 panic | PASS，702904 次执行 |
| `FuzzCBCDecryptRobustness` | 任意字节不 panic；解密成功则以同 key/IV 重加密必逐字节还原原密文 | PASS，118350 次执行 |
| `FuzzParseHexNeverPanics` | 任意 Hex/公钥组合不 panic，仅以 error 返回 | PASS，48731 次执行 |
| `FuzzEncryptDecryptRoundTrip` | 四种密文格式任意明文往返守恒；空明文必须被入口快速拦截（不挂起） | PASS，685 次执行 |
| `FuzzSignVerifyRoundTrip` | 原数据验签通过、篡改数据必拒、**篡改签名必拒**（双向不变量）；Hex 往返一致；非法字符/截断/奇数长度三类非法 Hex 均返回 false 不 panic | PASS，652 次执行 |
| `FuzzHashInvariants` | 摘要恒 32 字节/64 位 Hex；`Hash(x) == HashString(string(x))`；确定性；不 panic | PASS，46468 次执行 |

- 六个目标的种子语料随普通 `go test` 一并执行（日常 CI 即覆盖合法/非法边界样本）；
  `-fuzz` 长跑发现的新增语料写入 Go 构建缓存，无库缺陷 crash 落盘。
  `gosm2/testdata/fuzz/FuzzSignVerifyRoundTrip/` 仅存 1 条回归语料—— fuzz 变异出负数
  `tamper` 触发 fuzz 目标自身的负索引缺陷（已修复，语料保留守护修复不回退），非库 bug。
- SM2 两类目标每轮含椭圆曲线运算（签名/验签各一次标量乘），执行频率天然低于 SM3/SM4，
  读数偏低属预期而非覆盖不足。
