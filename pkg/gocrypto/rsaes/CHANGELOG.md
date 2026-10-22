# Changelog

本文件记录 `pkg/gocrypto/rsaes` 的显著变更。

## Unreleased

### 新增

- **RSAES-PKCS1-v1_5 兼容层**：把 Go 1.26 起被弃用的 `crypto/rsa` PKCS #1 v1.5 加解密能力
  收敛成一份仓内实现，供 `gocrypto` 与 `gorsa` 共用。之所以不能按官方建议直接切 OAEP：
  这两个包承担的是存量协议对接（Java `RSA/ECB/PKCS1Padding`、只接受 v1.5 密文的云厂商开放接口），
  换填充等于单方面断协议。
  - **Encrypt 自实现**：`EM = 0x00 || 0x02 || PS || 0x00 || M` + `c = m^e mod n`。
    公钥运算不接触秘密，因此无需常量时间；五项公钥形状判定（模数存在、模数为正、≥ 1024 位、
    为奇数，指数为 3 ≤ e ≤ 2^31-1 的奇数）与标准库 `checkPublicKey` 对齐。
  - **Decrypt 复用标准库**：走 `crypto.Decrypter` 接口方法 `(*rsa.PrivateKey).Decrypt(rand, ct, nil)`，
    按标准库文档 `opts == nil` 即 PKCS #1 v1.5。这样常量时间模幂（`fips140/bigmod` 的 4 bit 窗口幂）、
    去填充的常量时间扫描、统一的 `rsa.ErrDecryption` 全部由 Go 官方维护，比自己重写强。
    查证过程中修正了一个想当然的前提：**Go 1.27 的这条路径已无「盲化」步骤**，
    防护手段是常量时间窗口幂 + CRT，注释与文档都按实测源码写法。
- **测试交付物**：`rsaes_test.go`（往返/边界/弱密钥拒绝/错误值统一性，并用标准库弃用入口作参照实现
  双向校验）、`fuzz_test.go`（不变量：还原一致或 `ErrMessageTooLong`，不允许第三种结局）、
  `benchmark_test.go`（含标准库对照基准）、`integration_test.go`（`-tags=integration`，
  请 openssl 当外部对端做双向互操作 + 校验 Go 生成的密钥 PEM 可被 openssl 读入）。
  实测：Windows 10 + Git Bash、OpenSSL 1.1.1m，3 个集成用例全 PASS（单向 0.02s/子用例），
  覆盖 ASCII / 中文 / 含零字节 / 空明文 / 达到上界 `k-11=245` 五类明文。

### 兼容性说明

- **对外行为不变**：`pkg/gocrypto.RsaEncrypt` / `RsaDecrypt`、`pkg/gorsa.EncryptByPublicKey` /
  `DecryptByPrivateKey` 的签名、密文格式与错误值均未变动（密文仍为定长 `k` 字节 v1.5 密文，
  超长仍返回 `rsa.ErrMessageTooLong`，解密失败仍返回 `rsa.ErrDecryption`）。
  守护证据：上面的 openssl 双向互操作 + 标准库参照实现两个方向的测试，以及 `pkg/gorsa/gorsa_test.go`
  里的标准库双向参照用例。
  备注：本次切换只动填充实现的来源；`gorsa` 侧另有一处**与填充无关**的 PKCS#8 私钥解析
  静默失败（解析出错却返回 nil 错误）被一并修掉，详情登记在 `pkg/gorsa/CHANGELOG.md`。
- **FIPS 140-only**：`Decrypt` 沿用标准库的 `fips140only.Enforced()` 拦截；
  `Encrypt` 未复刻该门控（`crypto/fips140` 只导出 `Enabled()`，区分不了 `on` 与 `only`，
  硬判会误伤 `on` 模式）。FIPS 合规部署需整链路改用 OAEP。
- 本包只处理填充：密钥的 PEM/DER 解析、业务编码仍归 `gocrypto` / `gorsa`。
  新接口不要引入本包，直接用 `rsa.EncryptOAEP` / `rsa.DecryptOAEP`。
