package gosm4

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/tjfoc/gmsm/sm4"
)

// TestEncryptDecryptCBCRoundTrip 验证 SM4-CBC 加解密在各长度明文上都能原样还原。
func TestEncryptDecryptCBCRoundTrip(t *testing.T) {
	s := newTestSM4(t)
	cases := []struct {
		name string
		data []byte
	}{
		{"空明文", nil},
		{"1字节", []byte("a")},
		{"15字节不满一块", []byte("123456789012345")},
		{"16字节整一块", []byte("1234567890123456")},
		{"17字节跨块", []byte("12345678901234567")},
		{"1024字节多块", bytes.Repeat([]byte("1234567890123456"), 64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := s.EncryptCBC(tc.data, testKey16, testIV16).ToBytes()
			if err != nil {
				t.Fatalf("CBC 加密失败: %v", err)
			}
			if len(ciphertext)%sm4.BlockSize != 0 {
				t.Fatalf("密文长度必须是 16 的倍数, 实际 %d", len(ciphertext))
			}
			plaintext, err := s.DecryptCBC(ciphertext, testKey16, testIV16).ToBytes()
			if err != nil {
				t.Fatalf("CBC 解密失败: %v", err)
			}
			if !bytes.Equal(plaintext, tc.data) {
				t.Fatalf("还原明文不一致: 得到 %q, 期望 %q", plaintext, tc.data)
			}
		})
	}
}

// TestEncryptDecryptECBRoundTrip 验证 SM4-ECB 加解密在各长度明文上都能原样还原。
func TestEncryptDecryptECBRoundTrip(t *testing.T) {
	s := newTestSM4(t)
	cases := []struct {
		name string
		data []byte
	}{
		{"空明文", nil},
		{"1字节", []byte("a")},
		{"15字节不满一块", []byte("123456789012345")},
		{"16字节整一块", []byte("1234567890123456")},
		{"17字节跨块", []byte("12345678901234567")},
		{"1024字节多块", bytes.Repeat([]byte("1234567890123456"), 64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := s.EncryptECB(tc.data, testKey16).ToBytes()
			if err != nil {
				t.Fatalf("ECB 加密失败: %v", err)
			}
			plaintext, err := s.DecryptECB(ciphertext, testKey16).ToBytes()
			if err != nil {
				t.Fatalf("ECB 解密失败: %v", err)
			}
			if !bytes.Equal(plaintext, tc.data) {
				t.Fatalf("还原明文不一致: 得到 %q, 期望 %q", plaintext, tc.data)
			}
		})
	}
}

// TestCipherOutputCompatibleWithGmsm 验证本包 ECB/CBC 密文与 gmsm v1.4.1 上游
// sm4.Sm4Ecb/Sm4Cbc 字节级一致，且上游密文可被本包解密。
// 这是 internal/config 已有 Nacos ENC() 密文无需迁移的守护测试：
// 参照实现只在测试内串行调用（SetIV 写上游全局 IV，测试串行执行无并发窗口）。
func TestCipherOutputCompatibleWithGmsm(t *testing.T) {
	s := newTestSM4(t)
	plaintext := []byte("nacos credential compatibility check 中国")

	// 参照实现：gmsm CBC（依赖其包级全局 IV）
	if err := sm4.SetIV(testIV16); err != nil {
		t.Fatalf("设置参照 IV 失败: %v", err)
	}
	wantCBC, err := sm4.Sm4Cbc(testKey16, plaintext, true)
	if err != nil {
		t.Fatalf("参照 CBC 加密失败: %v", err)
	}
	gotCBC, err := s.EncryptCBC(plaintext, testKey16, testIV16).ToBytes()
	if err != nil {
		t.Fatalf("本包 CBC 加密失败: %v", err)
	}
	if !bytes.Equal(gotCBC, wantCBC) {
		t.Fatalf("CBC 密文与 gmsm 不一致: 得到 %x, 期望 %x", gotCBC, wantCBC)
	}

	// 反向：参照密文必须能被本包正确解密（覆盖存量 ENC() 数据）
	restored, err := s.DecryptCBC(wantCBC, testKey16, testIV16).ToBytes()
	if err != nil {
		t.Fatalf("本包解密参照密文失败: %v", err)
	}
	if !bytes.Equal(restored, plaintext) {
		t.Fatalf("解密参照密文不一致: 得到 %q, 期望 %q", restored, plaintext)
	}

	// 参照实现：gmsm ECB
	wantECB, err := sm4.Sm4Ecb(testKey16, plaintext, true)
	if err != nil {
		t.Fatalf("参照 ECB 加密失败: %v", err)
	}
	gotECB, err := s.EncryptECB(plaintext, testKey16).ToBytes()
	if err != nil {
		t.Fatalf("本包 ECB 加密失败: %v", err)
	}
	if !bytes.Equal(gotECB, wantECB) {
		t.Fatalf("ECB 密文与 gmsm 不一致: 得到 %x, 期望 %x", gotECB, wantECB)
	}
}

// TestDecryptRejectsInvalidCiphertext 验证解密失败必须返回明确错误，不得静默返回空数据，
// 也不得 panic（历史实现对空密文会在 gmsm pkcs7UnPadding 内越界 panic，对乱码密文返回 (nil, nil)）。
func TestDecryptRejectsInvalidCiphertext(t *testing.T) {
	s := newTestSM4(t)
	cases := []struct {
		name     string
		data     []byte
		wantPart string
	}{
		{"空密文", []byte{}, "PKCS7 填充校验失败"},
		{"全零密文", make([]byte, 16), "PKCS7 填充校验失败"},
		{"长度非16倍数", []byte{1, 2, 3}, "16 字节的整倍数"},
	}
	for _, tc := range cases {
		t.Run("CBC/"+tc.name, func(t *testing.T) {
			_, err := s.DecryptCBC(tc.data, testKey16, testIV16).ToBytes()
			if err == nil {
				t.Fatalf("非法密文应返回错误, 实际成功")
			}
			if !strings.Contains(err.Error(), tc.wantPart) {
				t.Fatalf("错误信息应包含 %q, 实际 %q", tc.wantPart, err.Error())
			}
		})
		t.Run("ECB/"+tc.name, func(t *testing.T) {
			_, err := s.DecryptECB(tc.data, testKey16).ToBytes()
			if err == nil {
				t.Fatalf("非法密文应返回错误, 实际成功")
			}
			if !strings.Contains(err.Error(), tc.wantPart) {
				t.Fatalf("错误信息应包含 %q, 实际 %q", tc.wantPart, err.Error())
			}
		})
	}
}

// TestDecryptEmptyPlaintextSucceeds 验证「加密空明文→解密」应还原为空切片：
// 空明文经 PKCS7 后是整块填充，解密结果合法为空，不能被误判为解密失败
// （历史实现用 len==0 判断错误，把这种合法情况与乱码密文一起误报）。
func TestDecryptEmptyPlaintextSucceeds(t *testing.T) {
	s := newTestSM4(t)
	ciphertext, err := s.EncryptCBC(nil, testKey16, testIV16).ToBytes()
	if err != nil {
		t.Fatalf("加密空明文失败: %v", err)
	}
	if len(ciphertext) != sm4.BlockSize {
		t.Fatalf("空明文密文应为一个填充块 16 字节, 实际 %d", len(ciphertext))
	}
	plaintext, err := s.DecryptCBC(ciphertext, testKey16, testIV16).ToBytes()
	if err != nil {
		t.Fatalf("解密空明文密文不应报错: %v", err)
	}
	if len(plaintext) != 0 {
		t.Fatalf("应还原为空明文, 实际 %q", plaintext)
	}
}

// TestCBCRejectsInvalidKeyAndIV 验证密钥/IV 长度不合法时返回错误且错误文本与历史版本一致。
func TestCBCRejectsInvalidKeyAndIV(t *testing.T) {
	s := newTestSM4(t)

	t.Run("密钥长度非法", func(t *testing.T) {
		_, err := s.EncryptCBC([]byte("data"), []byte("short-key"), testIV16).ToBytes()
		if err == nil {
			t.Fatalf("密钥长度非法应返回错误")
		}
		if !strings.Contains(err.Error(), "invalid key size") {
			t.Fatalf("错误信息应包含 %q, 实际 %q", "invalid key size", err.Error())
		}
	})

	t.Run("IV长度非法", func(t *testing.T) {
		shortIV := []byte("short-iv")
		if _, err := s.EncryptCBC([]byte("data"), testKey16, shortIV).ToBytes(); err == nil ||
			!strings.Contains(err.Error(), "failed to set IV") {
			t.Fatalf("加密侧 IV 非法应返回含 failed to set IV 的错误, 实际 %v", err)
		}
		if _, err := s.DecryptCBC([]byte{1}, testKey16, shortIV).ToBytes(); err == nil ||
			!strings.Contains(err.Error(), "failed to set IV") {
			t.Fatalf("解密侧 IV 非法应返回含 failed to set IV 的错误, 实际 %v", err)
		}
	})
}

// TestUnescapeHTMLOption 验证 WithUnescapeHTML 开关控制加密前是否做 HTML 反转义：
// 默认开启（&amp; → & 后再加密），关闭时明文字节原样保留。
func TestUnescapeHTMLOption(t *testing.T) {
	plaintext := []byte("&amp;hello")

	t.Run("默认开启反转义", func(t *testing.T) {
		s := NewSM4()
		ciphertext, err := s.EncryptCBC(plaintext, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("加密失败: %v", err)
		}
		got, err := s.DecryptCBC(ciphertext, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("解密失败: %v", err)
		}
		if string(got) != "&hello" {
			t.Fatalf("默认应先反转义再加密: 得到 %q, 期望 %q", got, "&hello")
		}
	})

	t.Run("显式关闭保持原样", func(t *testing.T) {
		s := NewSM4(WithUnescapeHTML(false))
		ciphertext, err := s.EncryptCBC(plaintext, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("加密失败: %v", err)
		}
		got, err := s.DecryptCBC(ciphertext, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("解密失败: %v", err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("关闭转义应原样还原: 得到 %q, 期望 %q", got, plaintext)
		}
	})
}

// TestCBCConcurrentDifferentIVs 验证并发使用不同 IV/密钥的 CBC 加解密互不干扰。
// 守护背景：历史实现经 gmsm sm4.SetIV 写包级全局 IV，与 Sm4Cbc 读取两步无同步，
// 并发时会互相覆盖导致错 IV 加解密；本实现 IV 只在本次调用栈内，任何交错都必须正确。
func TestCBCConcurrentDifferentIVs(t *testing.T) {
	const goroutines = 8
	const iterations = 100

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			s := NewSM4(WithUnescapeHTML(false))
			key := bytes.Repeat([]byte{byte(g + 1)}, sm4.BlockSize)
			iv := bytes.Repeat([]byte{byte(g + 17)}, sm4.BlockSize)
			plain := []byte(fmt.Sprintf("goroutine-%d 的载荷", g))
			for i := 0; i < iterations; i++ {
				ciphertext, err := s.EncryptCBC(plain, key, iv).ToBytes()
				if err != nil {
					errCh <- fmt.Errorf("goroutine %d 第 %d 次加密失败: %w", g, i, err)
					return
				}
				got, err := s.DecryptCBC(ciphertext, key, iv).ToBytes()
				if err != nil {
					errCh <- fmt.Errorf("goroutine %d 第 %d 次解密失败: %w", g, i, err)
					return
				}
				if !bytes.Equal(got, plain) {
					errCh <- fmt.Errorf("goroutine %d 第 %d 次还原不一致: 得到 %q, 期望 %q", g, i, got, plain)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

// TestFormatConversionRoundTrip 验证 Hex/Base64 编码链路：加密→编码→解码→解密可还原，
// 且 DecryptCBCFromByte/DecryptECBFromByte 与基础方法等价。
func TestFormatConversionRoundTrip(t *testing.T) {
	s := newTestSM4(t)
	plain := []byte("format conversion roundtrip")

	t.Run("CBC-Hex", func(t *testing.T) {
		hexStr, err := s.EncryptCBC(plain, testKey16, testIV16).ToHex()
		if err != nil {
			t.Fatalf("转 Hex 失败: %v", err)
		}
		got, err := s.DecryptCBCFromHex(hexStr, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("Hex 解密失败: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("还原不一致: 得到 %q", got)
		}
	})

	t.Run("CBC-Base64", func(t *testing.T) {
		b64, err := s.EncryptCBC(plain, testKey16, testIV16).ToBase64()
		if err != nil {
			t.Fatalf("转 Base64 失败: %v", err)
		}
		got, err := s.DecryptCBCFromBase64(b64, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("Base64 解密失败: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("还原不一致: 得到 %q", got)
		}
	})

	t.Run("ECB-Hex", func(t *testing.T) {
		hexStr, err := s.EncryptECB(plain, testKey16).ToHex()
		if err != nil {
			t.Fatalf("转 Hex 失败: %v", err)
		}
		got, err := s.DecryptECBFromHex(hexStr, testKey16).ToBytes()
		if err != nil {
			t.Fatalf("Hex 解密失败: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("还原不一致: 得到 %q", got)
		}
	})

	t.Run("ECB-Base64", func(t *testing.T) {
		b64, err := s.EncryptECB(plain, testKey16).ToBase64()
		if err != nil {
			t.Fatalf("转 Base64 失败: %v", err)
		}
		got, err := s.DecryptECBFromBase64(b64, testKey16).ToBytes()
		if err != nil {
			t.Fatalf("Base64 解密失败: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("还原不一致: 得到 %q", got)
		}
	})

	t.Run("CBC-FromByte与基础方法等价", func(t *testing.T) {
		ciphertext, err := s.EncryptCBC(plain, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("加密失败: %v", err)
		}
		got, err := s.DecryptCBCFromByte(ciphertext, testKey16, testIV16).ToBytes()
		if err != nil {
			t.Fatalf("FromByte 解密失败: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("还原不一致: 得到 %q", got)
		}
	})
}

// TestDecodeInputErrors 验证非法 Hex/Base64 输入返回包装后的错误而非 panic 或空成功。
func TestDecodeInputErrors(t *testing.T) {
	s := newTestSM4(t)

	t.Run("非法Hex", func(t *testing.T) {
		if _, err := s.DecryptCBCFromHex("zz-not-hex", testKey16, testIV16).ToBytes(); err == nil {
			t.Fatalf("非法 Hex 应返回错误")
		}
		if _, err := s.DecryptECBFromHex("zz-not-hex", testKey16).ToBytes(); err == nil {
			t.Fatalf("非法 Hex 应返回错误")
		}
	})

	t.Run("非法Base64", func(t *testing.T) {
		if _, err := s.DecryptCBCFromBase64("!!not-base64!!", testKey16, testIV16).ToBytes(); err == nil {
			t.Fatalf("非法 Base64 应返回错误")
		}
		if _, err := s.DecryptECBFromBase64("!!not-base64!!", testKey16).ToBytes(); err == nil {
			t.Fatalf("非法 Base64 应返回错误")
		}
	})
}
