package rsaes

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

// benchKeyBits 基准用密钥长度。2048 位是当前生产默认档位，
// 读数才对外有参考价值；1024 位只用于功能用例（见 rsaes_test.go 的 testKeyBits）。
const benchKeyBits = 2048

var (
	benchKey        = mustGenerateKey(benchKeyBits)
	benchPlaintext  = bytes.Repeat([]byte{'s'}, 32)
	benchCiphertext = func() []byte {
		ct, err := Encrypt(&benchKey.PublicKey, benchPlaintext)
		if err != nil {
			panic("rsaes: 生成基准密文失败: " + err.Error())
		}
		return ct
	}()
)

// BenchmarkEncrypt 度量公钥加密单次开销，不含任何 IO。
// 口径备注：本包的 Encrypt 是仓内实现（big.Int.Exp + 随机填充），
// 与基准对照的意义在于回归——填充随机数或模幂路径变慢会在这里暴露。
func BenchmarkEncrypt(b *testing.B) {
	b.SetBytes(int64(len(benchPlaintext)))
	for i := 0; i < b.N; i++ {
		if _, err := Encrypt(&benchKey.PublicKey, benchPlaintext); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecrypt 度量私钥解密单次开销（完全走标准库的常量时间模幂路径）。
func BenchmarkDecrypt(b *testing.B) {
	b.SetBytes(int64(len(benchPlaintext)))
	for i := 0; i < b.N; i++ {
		if _, err := Decrypt(benchKey, benchCiphertext); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncryptStdlibBaseline 是同一工作量下标准库弃用入口的读数，
// 用来判断本包实现相对标准库有没有引入额外开销。测试文件不参与 lint，
// 这里的弃用调用只作基准对照，不构成生产依赖。
func BenchmarkEncryptStdlibBaseline(b *testing.B) {
	b.SetBytes(int64(len(benchPlaintext)))
	for i := 0; i < b.N; i++ {
		if _, err := rsa.EncryptPKCS1v15(rand.Reader, &benchKey.PublicKey, benchPlaintext); err != nil {
			b.Fatal(err)
		}
	}
}
