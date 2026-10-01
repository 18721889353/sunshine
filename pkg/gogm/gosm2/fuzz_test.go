package gosm2

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// FuzzParseHexNeverPanics 守护不变量：任意 Hex 字符串解析 SM2 公私钥不得 panic；
// 解析成功时必须返回非空密钥。种子语料随常规 go test 执行，
// 挖掘需显式 go test -fuzz=FuzzParseHex。
func FuzzParseHexNeverPanics(f *testing.F) {
	// 与 example.go 相同的演示密钥对（固定种子，保证合法路径每次执行）
	f.Add(
		"6510d906cab1bdb8e0c950601e218e8f88733fe81113b807625707a3b3428b92",
		"2c59a63ef7ee015edfe9a2d411775fd204d5fe37b2c2617793d7b90d381ac9e81fa8348fc6dcf6cb5a9b08322fef3e585ddef297b00b067e96e8bda1473a1838",
	)
	f.Add("", "")
	f.Add("zz", "zz")
	f.Add("00", "04")               // 长度边界：解码后不足 64 字节
	f.Add("ffffffffffffffff", "04") // 公钥仅 04 前缀，无坐标体
	f.Add("0000000000000000000000000000000000000000000000000000000000000001",
		"04"+"ffffffffffffffffffffffffffffffff"+"ffffffffffffffffffffffffffffffff") // D=1 合法、公钥未验证点
	f.Fuzz(func(t *testing.T, privHex, pubHex string) {
		if len(privHex) > 1024 || len(pubHex) > 1024 {
			t.Skip("限定单字段不超过 1KiB")
		}
		s := NewSM2()
		if privateKey, err := s.ParsePrivateKeyFromHex(privHex); err == nil && privateKey == nil {
			t.Fatalf("私钥解析成功但返回空: %q", privHex)
		}
		if publicKey, err := s.ParsePublicKeyFromHex(pubHex); err == nil && publicKey == nil {
			t.Fatalf("公钥解析成功但返回空: %q", pubHex)
		}
	})
}

// sm2FuzzFormats 四种密文格式全量覆盖（加密与解密必须同格式成对使用）。
var sm2FuzzFormats = [...]EncryptFormat{C1C3C2, C1C2C3, C1C3C2Compressed, C1C2C3Compressed}

// FuzzEncryptDecryptRoundTrip 守护不变量：固定合法密钥对下，任意明文 × 任意格式
// 满足 decrypt(encrypt(x)) == x 且全程不 panic。密钥对固定生成一次，
// 只变异明文与格式参数，保证每轮都走合法加密路径。
func FuzzEncryptDecryptRoundTrip(f *testing.F) {
	f.Add([]byte(""), uint8(0))                        // 空明文：必须被入口拦截（上游死循环路径）
	f.Add([]byte("a"), uint8(1))                       // 1 字节
	f.Add([]byte("123456789012345"), uint8(2))         // 15 字节（差 1 字节满块）
	f.Add([]byte("1234567890123456"), uint8(3))        // 16 字节（恰满块，填充整块）
	f.Add([]byte("&amp;hello <b>world</b>"), uint8(0)) // 含 HTML 实体（关转义下须原样往返）
	f.Add([]byte{0x00, 0xff, 0x7f, 0x80}, uint8(1))    // 非文本二进制
	s := newTestSM2(f)
	_, privateKey, publicKey := mustGenerateKeyPair(f)
	f.Fuzz(func(t *testing.T, data []byte, formatIdx uint8) {
		if len(data) > 4096 {
			t.Skip("限定明文不超过 4KiB")
		}
		format := sm2FuzzFormats[int(formatIdx)%len(sm2FuzzFormats)]
		if len(data) == 0 {
			// 空明文：上游 kdf(0) 恒 false 导致 Encrypt 无限 continue（实测卡死），
			// 本包入口必须拦截——此处断言“快速返回错误”，守住 DoS 防线
			if _, err := s.Encrypt(publicKey, data, format).ToBytes(); err == nil {
				t.Fatalf("空明文应返回错误而非成功 (format=%d)", formatIdx)
			}
			return
		}
		ciphertext, err := s.Encrypt(publicKey, data, format).ToBytes()
		if err != nil {
			t.Fatalf("合法密钥加密不应失败: %v", err)
		}
		plaintext, err := s.Decrypt(privateKey, ciphertext, format).ToBytes()
		if err != nil {
			t.Fatalf("解密自产密文不应失败: %v", err)
		}
		if !bytes.Equal(plaintext, data) {
			t.Fatalf("往返不守恒: 得到 %x, 期望 %x (format=%d)", plaintext, data, formatIdx)
		}
	})
}

// FuzzSignVerifyRoundTrip 守护不变量：任意数据签名后——原数据验签必通过、
// 篡改一个字节必被拒；签名 Hex 编码往返一致，注入非法字符必返回 false（不 panic）。
func FuzzSignVerifyRoundTrip(f *testing.F) {
	f.Add([]byte(""), 0)                       // 空数据（无处篡改，跳过篡改断言）
	f.Add([]byte("a"), 1)                      // 单字节
	f.Add([]byte("order id = 10086"), 2)       // 典型报文
	f.Add([]byte("&amp;entity"), 3)            // 含 HTML 实体
	f.Add(bytes.Repeat([]byte{0x5a}, 1024), 4) // 1KiB 重复字节
	s := newTestSM2(f)
	_, privateKey, publicKey := mustGenerateKeyPair(f)
	f.Fuzz(func(t *testing.T, data []byte, tamper int) {
		if len(data) > 4096 {
			t.Skip("限定数据不超过 4KiB")
		}
		signature, err := s.Sign(privateKey, data).ToBytes()
		if err != nil {
			t.Fatalf("合法私钥签名不应失败: %v", err)
		}
		if !s.VerifyFromBytes(publicKey, data, signature) {
			t.Fatalf("原数据验签应通过: data=%x", data)
		}
		// 篡改一个字节必被拒（空数据无字节可翻转，跳过）
		if len(data) > 0 {
			tampered := append([]byte(nil), data...)
			// tamper 由 fuzz 变异可为负数，负数取模仍为负，需归一到 [0, len) 再下标
			idx := tamper % len(tampered)
			if idx < 0 {
				idx += len(tampered)
			}
			tampered[idx] ^= 0xFF
			if s.VerifyFromBytes(publicKey, tampered, signature) {
				t.Fatalf("篡改数据验签应拒绝: data=%x", tampered)
			}
		}
		// 篡改签名必被拒——签名是伪造攻击的直接目标，与数据篡改构成双向不变量
		// （对照 gosm4 FuzzCBCDecryptRobustness 的双向守护思路）
		tamperedSig := append([]byte(nil), signature...)
		sigIdx := tamper % len(tamperedSig)
		if sigIdx < 0 {
			sigIdx += len(tamperedSig)
		}
		tamperedSig[sigIdx] ^= 0xFF
		if s.VerifyFromBytes(publicKey, data, tamperedSig) {
			t.Fatalf("篡改签名验签应拒绝: sig=%x", tamperedSig)
		}
		// Hex 编码往返：原签名 Hex 验签通过；注入非法字符返回 false 而非 panic
		sigHex := hex.EncodeToString(signature)
		if !s.VerifyFromHex(publicKey, data, sigHex) {
			t.Fatalf("签名 Hex 往返验签应通过")
		}
		if s.VerifyFromHex(publicKey, data, sigHex+"zz") {
			t.Fatalf("含非法字符的签名 Hex 应返回 false")
		}
		// 长度非法两面：截断（解码成功但 DER 结构不完整）与奇数长度（解码直接失败）
		// 都必须返回 false 而非 panic——与上面的非法字符构成三类输入错误全覆盖
		if s.VerifyFromHex(publicKey, data, sigHex[:len(sigHex)/2]) {
			t.Fatalf("截断的签名 Hex 应返回 false")
		}
		if s.VerifyFromHex(publicKey, data, sigHex[:len(sigHex)-1]) {
			t.Fatalf("奇数长度的签名 Hex 应返回 false")
		}
	})
}
