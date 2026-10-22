package rsaes

import (
	"bytes"
	"crypto/rsa"
	"errors"
	"testing"
)

// FuzzEncryptDecryptRoundTrip 守护的是不变量而不是具体输出值：
// 任意明文经本包加密再解密，只有两种合法结局——原样还原，或因超过 k-11 上界返回
// rsa.ErrMessageTooLong。出现第三种结局（panic、解密失败、还原成别的内容）即为缺陷。
func FuzzEncryptDecryptRoundTrip(f *testing.F) {
	f.Add([]byte("hello"))
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add(bytes.Repeat([]byte{0x41}, 117)) // 1024 位密钥的明文上界 k-11
	f.Add(bytes.Repeat([]byte{0x42}, 118)) // 刚越界
	f.Add(bytes.Repeat([]byte{0x00}, 128)) // 全零且越界

	f.Fuzz(func(t *testing.T, msg []byte) {
		ciphertext, err := Encrypt(&testKey.PublicKey, msg)
		if err != nil {
			if !errors.Is(err, rsa.ErrMessageTooLong) {
				t.Fatalf("非预期的加密错误: %v", err)
			}
			if len(msg) <= testKey.PublicKey.Size()-emFixedOverhead {
				t.Fatalf("上界内的明文被拒绝, len=%d: %v", len(msg), err)
			}
			return
		}

		if len(msg) > testKey.PublicKey.Size()-emFixedOverhead {
			t.Fatalf("越界明文未被拒绝, len=%d", len(msg))
		}

		got, err := Decrypt(testKey, ciphertext)
		if err != nil {
			t.Fatalf("自产密文解密失败, len=%d: %v", len(msg), err)
		}
		if !bytes.Equal(got, msg) {
			t.Fatalf("往返不一致: got %x, want %x", got, msg)
		}
	})
}
