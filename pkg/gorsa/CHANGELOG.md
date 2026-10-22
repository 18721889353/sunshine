# Changelog

本文件记录 `pkg/gorsa` 的显著变更。

## Unreleased

### 新增

- **包内第一份测试**：`gorsa_test.go`（9 个顶层用例 + 子用例，语句覆盖率实测 89.8%，
  `go test ./pkg/gorsa/ -count=1 -cover`，Windows/amd64）。此前本包零测试，而它承担的是
  与 Java 侧对接的加解密与签名，改动没有任何回归防线。测试用**标准库弃用入口作参照实现
  oracle**（双向：标准库能解本包密文、本包能解标准库密文），守的是「协议等价」而不是自证往返清白。
- **包文档**：`README.md` 登记入参约定（`base64(PEM)`）、两种私钥块类型、签名与加密两条路的区别、
  错误语义表，以及三条**已知偏离**（`KEYSIZE=1024`、签名哈希固定 SHA1、`EncryptByPrivateKey` 名不副实）。

### 变更

- **PKCS#1 v1.5 加解密下沉到 `pkg/gocrypto/rsaes`**：`EncryptByPublicKey` / `DecryptByPrivateKey`
  不再直接调用 `rsa.EncryptPKCS1v15` / `rsa.DecryptPKCS1v15`（Go 1.26 起 Deprecated，SA1019），
  改为委托仓内兼容层。之所以不按官方建议直接切 OAEP：本包承担的是存量协议对接
  （Java `RSA/ECB/PKCS1Padding`），换填充等于单方面断协议。
  对外签名、密文格式（定长 `k` 字节 v1.5 密文 + base64）、错误值全部未变；
  守护证据是上面的标准库双向参照测试 + `rsaes` 的 openssl 集成测试。
- **私钥/公钥解析收敛为 `parsePrivateKey` / `parsePublicKey`**：签名、解密、私钥加密三个入口
  原本各抄一遍同样的 if/else-if 链，公钥侧两处重复；改为 switch 单一入口，行为保持不变
  （错误文案沿用原英文措辞，`unsupported private key format` 等一字未动）。

### 修复

- **PKCS#8 私钥解析失败被静默吞掉（安全风险：签名/解密静默产出错误结果）**：
  三处解析逻辑都写成
  `key, signErr := x509.ParsePKCS8PrivateKey(...); if signErr != nil { return "", err }`，
  而那个 `err` 在这一刻**恒为 nil**（它是 base64 解码的 err，已经判过）。
  后果是：块类型为 `PRIVATE KEY` 但 DER 内容畸形时，`Sign` 返回**空签名 + nil 错误**、
  `DecryptByPrivateKey` / `EncryptByPrivateKey` 返回 `nil + nil 错误`，
  调用方无法从返回值察觉任何异常。
  - 影响面：仅「PEM 块头是 `PRIVATE KEY` 但 DER 内容解不开」这条路径。原本阴差阳错能报错的
    路径不变——`RSA PRIVATE KEY` 块（走 `ParsePKCS1PrivateKey`，err 用对了）、块类型不认识
    （`unsupported private key format`）、base64/PEM 本身解不开。公钥侧行为本来就正确。
  - 修复方式：`return nil, fmt.Errorf("failed to parse PKCS#8 private key: %w", err)`，
    错误带原因并可用 `errors.Is`/`errors.As` 判定。
  - 守护：`TestDecryptRejectsInvalidInput/PKCS#8_块解析失败必须给出错误而不是_nil` 与
    `TestSignAndVerify/PKCS#8_块解析失败必须给出错误而不是_nil`（后者还断言失败时不应有返回值）。

### 兼容性说明

- 对外 API 无签名变化、无删除；错误文案只有 PKCS#8 畸形私钥这一条从「nil 错误」变为具体错误，
  属于把静默失败改成可观测失败，不构成调用方需要迁移的破坏性变更。
- `KEYSIZE` 仍为 1024，**本次刻意未改**：它是协议可见参数（决定密文字节数与 base64 长度），
  调整需与对接方同步，理由与影响面登记在 README「已知偏离与建议」。
- 签名路径继续直接调用 `rsa.SignPKCS1v15` / `rsa.VerifyPKCS1v15`：Go 只弃用了 v1.5 的**加密**，
  这两个入口未被弃用。
