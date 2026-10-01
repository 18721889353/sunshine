package gosm2

import (
	"testing"

	"github.com/tjfoc/gmsm/sm2"
)

// newTestSM2 构造关闭 HTML 转义的 SM2 实例，保证明文字节与密文字节严格一一对应，
// 排除 unescapeHTML 对测试输入的干扰（HTML 行为由 TestEncryptSignHTMLUnescape 单独覆盖）。
func newTestSM2(tb testing.TB) *SM2 {
	tb.Helper()
	return NewSM2(WithUnescapeHTML(false))
}

// mustGenerateKeyPair 生成一对 SM2 密钥并从 Hex 解析回来，任一步失败即终止当前测试。
// 返回 (keyPair, privateKey, publicKey)，供加密/签名/格式用例共享。
func mustGenerateKeyPair(tb testing.TB) (*KeyPair, *sm2.PrivateKey, *sm2.PublicKey) {
	tb.Helper()
	s := newTestSM2(tb)
	keyPair, err := s.GenerateKeyPair()
	if err != nil {
		tb.Fatalf("生成 SM2 密钥对失败: %v", err)
	}
	privateKey, err := s.ParsePrivateKeyFromHex(keyPair.PrivateKeyHex)
	if err != nil {
		tb.Fatalf("解析私钥 Hex 失败: %v", err)
	}
	publicKey, err := s.ParsePublicKeyFromHex(keyPair.PublicKeyHex)
	if err != nil {
		tb.Fatalf("解析公钥 Hex 失败: %v", err)
	}
	return keyPair, privateKey, publicKey
}
