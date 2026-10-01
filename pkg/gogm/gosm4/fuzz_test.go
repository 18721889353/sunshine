package gosm4

import (
	"bytes"
	"testing"
)

// FuzzCBCEncryptDecryptRoundTrip 守护不变量：任意字节明文经 CBC 加解密后必须原样还原。
// 种子语料随常规 go test 执行；挖掘需显式 go test -fuzz=FuzzCBC。
func FuzzCBCEncryptDecryptRoundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("a"))
	f.Add(bytes.Repeat([]byte("x"), 15))
	f.Add(bytes.Repeat([]byte("y"), 16))
	f.Add(bytes.Repeat([]byte("z"), 17))
	f.Add([]byte("&amp;含HTML实体的明文"))
	f.Fuzz(func(t *testing.T, plaintext []byte) {
		if len(plaintext) > 4096 {
			t.Skip("限定单次载荷不超过 4KiB，覆盖边界已由种子保证")
		}
		s := NewSM4(WithUnescapeHTML(false))
		ciphertext, err := s.EncryptCBC(plaintext, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("加密不应失败: %v", err)
		}
		got, err := s.DecryptCBC(ciphertext, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("解密自身密文不应失败: %v", err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("还原不一致: 得到 %d 字节, 期望 %d 字节", len(got), len(plaintext))
		}
	})
}

// FuzzCBCDecryptRobustness 守护不变量：任意字节密文解密不得 panic；
// 若解密成功（填充合法），用同一 key/iv 重新加密必须还原出原密文——
// 这是 CBC + PKCS7 的双射性质，可同时捕捉填充校验与分组链路的实现缺陷。
func FuzzCBCDecryptRobustness(f *testing.F) {
	// 合法路径种子：真实加密产出的密文，解密成功后重加密必须逐字节还原
	validCiphertext, err := NewSM4(WithUnescapeHTML(false)).
		EncryptCBC([]byte("valid ciphertext seed"), testKey16, testIV16).ToBytes()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(validCiphertext)
	// 非法/边界输入种子：解密要么报错要么满足重加密不变量，填充校验分支由运行时实际解密块决定
	f.Add([]byte{})
	f.Add(make([]byte, 16))
	f.Add(bytes.Repeat([]byte{0x10}, 16))
	f.Add(bytes.Repeat([]byte{0x01}, 16))
	f.Add(bytes.Repeat([]byte{0x7f}, 31)) // 31 字节，长度非 16 倍数
	f.Add([]byte("garbage-ciphertext!"))  // 19 字节，长度非法
	f.Fuzz(func(t *testing.T, ciphertext []byte) {
		if len(ciphertext) > 4096 {
			t.Skip("限定单次载荷不超过 4KiB")
		}
		s := NewSM4(WithUnescapeHTML(false))
		plaintext, err := s.DecryptCBC(ciphertext, testKey16, testIV16).ToBytes()
		if err != nil {
			return // 报错是合法结果（长度/填充校验失败），本用例只要求不 panic
		}
		reencrypted, err := s.EncryptCBC(plaintext, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("重加密失败: %v", err)
		}
		if !bytes.Equal(reencrypted, ciphertext) {
			t.Fatalf("解密成功但重加密不还原原密文: 得到 %d 字节, 期望 %d 字节",
				len(reencrypted), len(ciphertext))
		}
	})
}
