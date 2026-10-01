package gosm2

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPaddedBytesFixedWidth 验证 Hex 补零辅助函数：不足 32 字节左侧补零到 32，
// 恰好或超过 32 字节原样返回。这是「本包生成的公钥一定能被 ParsePublicKeyFromHex 解析」
// 的单元基础——历史实现直接拼接 big.Int.Bytes()，X/Y 分量前导零被吞导致公钥 Hex
// 短于 128 位，上游以 "publicKey is not uncompressed." 拒收。
func TestPaddedBytesFixedWidth(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"空切片", []byte{}},
		{"1字节", []byte{0x01}},
		{"31字节", bytes.Repeat([]byte{0xab}, 31)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := paddedBytes(tc.in)
			if len(got) != 32 {
				t.Fatalf("补零后长度必须是 32, 实际 %d", len(got))
			}
			// 左侧补零、原字节右对齐
			if !bytes.Equal(got[32-len(tc.in):], tc.in) {
				t.Fatalf("原字节未右对齐保留: 得到 %x", got)
			}
			if !bytes.Equal(got[:32-len(tc.in)], make([]byte, 32-len(tc.in))) {
				t.Fatalf("左侧应补零: 得到 %x", got)
			}
		})
	}

	t.Run("32字节原样返回", func(t *testing.T) {
		in := bytes.Repeat([]byte{0xcd}, 32)
		if got := paddedBytes(in); !bytes.Equal(got, in) {
			t.Fatalf("恰好 32 字节应原样返回: 得到 %x", got)
		}
	})

	t.Run("超32字节原样返回", func(t *testing.T) {
		in := bytes.Repeat([]byte{0xcd}, 40)
		if got := paddedBytes(in); !bytes.Equal(got, in) {
			t.Fatalf("超长输入应原样返回（由上层保证不超长）: 得到 %x", got)
		}
	})
}

// TestGenerateKeyPairHexRoundTrip 验证生成的密钥对 Hex 固定宽度（私钥 64 位、公钥 128 位）
// 且能被本包 PEM/Hex 双路径重新解析。循环 50 轮覆盖随机密钥中 X/Y/D 分量前导零的边界
// （单分量前导零概率约 1/256，50 轮内出现与否均被固定宽度断言覆盖）。
func TestGenerateKeyPairHexRoundTrip(t *testing.T) {
	s := newTestSM2(t)
	for i := 0; i < 50; i++ {
		keyPair, err := s.GenerateKeyPair()
		if err != nil {
			t.Fatalf("第 %d 轮生成密钥对失败: %v", i, err)
		}
		if len(keyPair.PrivateKeyHex) != 64 {
			t.Fatalf("第 %d 轮私钥 Hex 应为 64 位, 实际 %d: %s", i, len(keyPair.PrivateKeyHex), keyPair.PrivateKeyHex)
		}
		if len(keyPair.PublicKeyHex) != 128 {
			t.Fatalf("第 %d 轮公钥 Hex 应为 128 位, 实际 %d: %s", i, len(keyPair.PublicKeyHex), keyPair.PublicKeyHex)
		}
		if _, err := hex.DecodeString(keyPair.PublicKeyHex); err != nil {
			t.Fatalf("第 %d 轮公钥 Hex 非法: %v", i, err)
		}

		privateKey, err := s.ParsePrivateKeyFromHex(keyPair.PrivateKeyHex)
		if err != nil {
			t.Fatalf("第 %d 轮解析私钥 Hex 失败: %v", i, err)
		}
		publicKey, err := s.ParsePublicKeyFromHex(keyPair.PublicKeyHex)
		if err != nil {
			t.Fatalf("第 %d 轮解析公钥 Hex 失败: %v", i, err)
		}
		// 还原后重新导出必须与原 Hex 逐字一致（D/X/Y 双向等价）
		privHex, err := s.PrivateKeyToHex(privateKey)
		if err != nil {
			t.Fatalf("第 %d 轮私钥导出 Hex 失败: %v", i, err)
		}
		if privHex != keyPair.PrivateKeyHex {
			t.Fatalf("第 %d 轮私钥 Hex 还原不一致: 得到 %s, 期望 %s", i, privHex, keyPair.PrivateKeyHex)
		}
		pubHex, err := s.PublicKeyToHex(publicKey)
		if err != nil {
			t.Fatalf("第 %d 轮公钥导出 Hex 失败: %v", i, err)
		}
		if pubHex != keyPair.PublicKeyHex {
			t.Fatalf("第 %d 轮公钥 Hex 还原不一致: 得到 %s, 期望 %s", i, pubHex, keyPair.PublicKeyHex)
		}
	}
}

// TestEncryptDecryptRoundTrip 验证四种密文格式的加解密都能原样还原。
func TestEncryptDecryptRoundTrip(t *testing.T) {
	s := newTestSM2(t)
	_, privateKey, publicKey := mustGenerateKeyPair(t)
	plaintext := []byte("hello 国密 SM2 roundtrip")

	formats := []struct {
		name   string
		format EncryptFormat
	}{
		{"C1C3C2", C1C3C2},
		{"C1C2C3", C1C2C3},
		{"C1C3C2Compressed", C1C3C2Compressed},
		{"C1C2C3Compressed", C1C2C3Compressed},
	}
	for _, tc := range formats {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := s.Encrypt(publicKey, plaintext, tc.format).ToBytes()
			if err != nil {
				t.Fatalf("加密失败: %v", err)
			}
			got, err := s.Decrypt(privateKey, ciphertext, tc.format).ToBytes()
			if err != nil {
				t.Fatalf("解密失败: %v", err)
			}
			if !bytes.Equal(got, plaintext) {
				t.Fatalf("还原不一致: 得到 %q, 期望 %q", got, plaintext)
			}
		})
	}
}

// TestSignAndVerify 验证签名/验签闭环：原数据通过、篡改数据拒绝、
// Hex/Base64 签名变体与字节变体等价。
func TestSignAndVerify(t *testing.T) {
	s := newTestSM2(t)
	_, privateKey, publicKey := mustGenerateKeyPair(t)
	data := []byte("sign me 国密")

	signature, err := s.Sign(privateKey, data).ToBytes()
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}

	t.Run("字节验签通过", func(t *testing.T) {
		if !s.VerifyFromBytes(publicKey, data, signature) {
			t.Fatalf("原数据验签应通过")
		}
	})

	t.Run("篡改数据拒绝", func(t *testing.T) {
		if s.VerifyFromBytes(publicKey, []byte("sign me 国密!"), signature) {
			t.Fatalf("篡改数据验签应拒绝")
		}
	})

	t.Run("篡改签名拒绝", func(t *testing.T) {
		corrupted := append([]byte(nil), signature...)
		corrupted[0] ^= 0xff
		if s.VerifyFromBytes(publicKey, data, corrupted) {
			t.Fatalf("篡改签名验签应拒绝")
		}
	})

	t.Run("Hex签名变体", func(t *testing.T) {
		sigHex, err := s.Sign(privateKey, data).ToHex()
		if err != nil {
			t.Fatalf("签名转 Hex 失败: %v", err)
		}
		if !s.VerifyFromHex(publicKey, data, sigHex) {
			t.Fatalf("Hex 签名验签应通过")
		}
	})

	t.Run("Base64签名变体", func(t *testing.T) {
		sigB64, err := s.Sign(privateKey, data).ToBase64()
		if err != nil {
			t.Fatalf("签名转 Base64 失败: %v", err)
		}
		if !s.VerifyFromBase64(publicKey, data, sigB64) {
			t.Fatalf("Base64 签名验签应通过")
		}
	})
}

// TestVerifyDecodeErrors 验证签名编码非法时返回 false 而非 panic。
func TestVerifyDecodeErrors(t *testing.T) {
	s := newTestSM2(t)
	_, _, publicKey := mustGenerateKeyPair(t)
	data := []byte("data")

	if s.VerifyFromHex(publicKey, data, "zz-not-hex") {
		t.Fatalf("非法 Hex 签名应验签失败")
	}
	if s.VerifyFromBase64(publicKey, data, "!!not-base64!!") {
		t.Fatalf("非法 Base64 签名应验签失败")
	}
}

// TestParsePEMWithoutHeader 验证剥离 PEM 头尾的裸内容也能被解析（自动补全分支）。
func TestParsePEMWithoutHeader(t *testing.T) {
	s := newTestSM2(t)
	keyPair, err := s.GenerateKeyPair()
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}

	strippedPriv := stripPEMHeader(keyPair.PrivateKeyPEM)
	strippedPub := stripPEMHeader(keyPair.PublicKeyPEM)
	if strings.Contains(strippedPriv, "-----BEGIN") || strings.Contains(strippedPub, "-----BEGIN") {
		t.Fatalf("剥离后不应包含 BEGIN 头")
	}

	if _, err := s.ParsePrivateKeyFromPem(strippedPriv); err != nil {
		t.Fatalf("裸私钥内容解析失败: %v", err)
	}
	if _, err := s.ParsePublicKeyFromPem(strippedPub); err != nil {
		t.Fatalf("裸公钥内容解析失败: %v", err)
	}
}

// TestStripPEMHeader 验证只去掉 BEGIN/END 行与空行，保留中间内容。
func TestStripPEMHeader(t *testing.T) {
	in := "-----BEGIN PUBLIC KEY-----\nAAAA\nBBBB\n-----END PUBLIC KEY-----"
	got := stripPEMHeader(in)
	if got != "AAAA\nBBBB" {
		t.Fatalf("得到 %q, 期望 %q", got, "AAAA\nBBBB")
	}
}

// TestSaveKeyPairFiles 验证 WithSave 自动落盘与 SaveKeyPair 手动落盘的文件
// 都是完整可解析的 PEM（MkdirAll 自动建目录）。
func TestSaveKeyPairFiles(t *testing.T) {
	t.Run("WithSave自动落盘", func(t *testing.T) {
		dir := t.TempDir()
		privFile := filepath.Join(dir, "nested", "private.pem")
		pubFile := filepath.Join(dir, "nested", "public.pem")
		s := NewSM2(WithSave(privFile, pubFile))
		if _, err := s.GenerateKeyPair(); err != nil {
			t.Fatalf("生成密钥对失败: %v", err)
		}
		privPEM, err := os.ReadFile(privFile)
		if err != nil {
			t.Fatalf("读取私钥文件失败: %v", err)
		}
		if _, err := s.ParsePrivateKeyFromPem(string(privPEM)); err != nil {
			t.Fatalf("落盘私钥解析失败: %v", err)
		}
		pubPEM, err := os.ReadFile(pubFile)
		if err != nil {
			t.Fatalf("读取公钥文件失败: %v", err)
		}
		if _, err := s.ParsePublicKeyFromPem(string(pubPEM)); err != nil {
			t.Fatalf("落盘公钥解析失败: %v", err)
		}
	})

	t.Run("SaveKeyPair手动落盘", func(t *testing.T) {
		dir := t.TempDir()
		s := newTestSM2(t)
		keyPair, err := s.GenerateKeyPair()
		if err != nil {
			t.Fatalf("生成密钥对失败: %v", err)
		}
		privFile := filepath.Join(dir, "manual.key")
		pubFile := filepath.Join(dir, "manual.pub")
		if err := s.SaveKeyPair(keyPair, privFile, pubFile); err != nil {
			t.Fatalf("SaveKeyPair 失败: %v", err)
		}
		privPEM, err := os.ReadFile(privFile)
		if err != nil {
			t.Fatalf("读取私钥文件失败: %v", err)
		}
		if _, err := s.ParsePrivateKeyFromPem(string(privPEM)); err != nil {
			t.Fatalf("落盘私钥解析失败: %v", err)
		}
	})

	t.Run("StripHeader去头内容落盘仍为标准PEM", func(t *testing.T) {
		dir := t.TempDir()
		s := NewSM2(WithStripHeader(true))
		keyPair, err := s.GenerateKeyPair()
		if err != nil {
			t.Fatalf("生成密钥对失败: %v", err)
		}
		if strings.Contains(keyPair.PrivateKeyPEM, "-----BEGIN") {
			t.Fatalf("StripHeader(true) 返回的私钥应已去头")
		}
		privFile := filepath.Join(dir, "stripped.key")
		pubFile := filepath.Join(dir, "stripped.pub")
		if err := s.SaveKeyPair(keyPair, privFile, pubFile); err != nil {
			t.Fatalf("SaveKeyPair 失败: %v", err)
		}
		// P1-2 守护：落盘必须是含标准头的完整 PEM（openssl 等通用工具可解析），
		// 而非 WithStripHeader 去头后的显示内容
		privPEM, err := os.ReadFile(privFile)
		if err != nil {
			t.Fatalf("读取私钥文件失败: %v", err)
		}
		if !strings.Contains(string(privPEM), "-----BEGIN PRIVATE KEY-----") {
			t.Fatalf("落盘私钥应自动补全 PRIVATE KEY 标准头，实际内容:\n%s", privPEM)
		}
		if _, err := s.ParsePrivateKeyFromPem(string(privPEM)); err != nil {
			t.Fatalf("落盘私钥回读失败: %v", err)
		}
		pubPEM, err := os.ReadFile(pubFile)
		if err != nil {
			t.Fatalf("读取公钥文件失败: %v", err)
		}
		if !strings.Contains(string(pubPEM), "-----BEGIN PUBLIC KEY-----") {
			t.Fatalf("落盘公钥应自动补全 PUBLIC KEY 标准头，实际内容:\n%s", pubPEM)
		}
		if _, err := s.ParsePublicKeyFromPem(string(pubPEM)); err != nil {
			t.Fatalf("落盘公钥回读失败: %v", err)
		}
	})

	t.Run("同路径重复落盘原子覆盖", func(t *testing.T) {
		dir := t.TempDir()
		s := newTestSM2(t)
		privFile := filepath.Join(dir, "repeat.key")
		pubFile := filepath.Join(dir, "repeat.pub")
		first, err := s.GenerateKeyPair()
		if err != nil {
			t.Fatalf("第一次生成失败: %v", err)
		}
		if err := s.SaveKeyPair(first, privFile, pubFile); err != nil {
			t.Fatalf("第一次落盘失败: %v", err)
		}
		second, err := s.GenerateKeyPair()
		if err != nil {
			t.Fatalf("第二次生成失败: %v", err)
		}
		// 原子写守护：rename 必须能覆盖已存在的旧文件（否则第二次落盘报错）
		if err := s.SaveKeyPair(second, privFile, pubFile); err != nil {
			t.Fatalf("重复落盘应原子覆盖旧文件，实际失败: %v", err)
		}
		privPEM, err := os.ReadFile(privFile)
		if err != nil {
			t.Fatalf("读取私钥文件失败: %v", err)
		}
		if _, err := s.ParsePrivateKeyFromPem(string(privPEM)); err != nil {
			t.Fatalf("覆盖后私钥应回读成功: %v", err)
		}
	})
}

// TestEncryptSignHTMLUnescape 验证 unescapeHTML 开关对 Encrypt 与 Sign 的影响：
// 默认开启时加密/签名前先做 HTML 反转义（&amp; → &），关闭时原样处理。
func TestEncryptSignHTMLUnescape(t *testing.T) {
	plaintext := []byte("&amp;hello")

	t.Run("默认开启加密前反转义", func(t *testing.T) {
		s := NewSM2()
		_, privateKey, publicKey := mustGenerateKeyPair(t)
		ciphertext, err := s.Encrypt(publicKey, plaintext, C1C3C2).ToBytes()
		if err != nil {
			t.Fatalf("加密失败: %v", err)
		}
		got, err := s.Decrypt(privateKey, ciphertext, C1C3C2).ToBytes()
		if err != nil {
			t.Fatalf("解密失败: %v", err)
		}
		if string(got) != "&hello" {
			t.Fatalf("默认应先反转义再加密: 得到 %q, 期望 %q", got, "&hello")
		}
	})

	t.Run("默认开启签名前反转义", func(t *testing.T) {
		s := NewSM2()
		_, privateKey, publicKey := mustGenerateKeyPair(t)
		signature, err := s.Sign(privateKey, plaintext).ToBytes()
		if err != nil {
			t.Fatalf("签名失败: %v", err)
		}
		// Sign 内部先反转义，实际签署的是 "&hello"
		if !s.VerifyFromBytes(publicKey, []byte("&hello"), signature) {
			t.Fatalf("对反转义后的数据验签应通过")
		}
		if s.VerifyFromBytes(publicKey, plaintext, signature) {
			t.Fatalf("对原始实体串验签应拒绝")
		}
	})

	t.Run("关闭后原样保留", func(t *testing.T) {
		s := newTestSM2(t)
		_, privateKey, publicKey := mustGenerateKeyPair(t)
		ciphertext, err := s.Encrypt(publicKey, plaintext, C1C3C2).ToBytes()
		if err != nil {
			t.Fatalf("加密失败: %v", err)
		}
		got, err := s.Decrypt(privateKey, ciphertext, C1C3C2).ToBytes()
		if err != nil {
			t.Fatalf("解密失败: %v", err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("关闭转义应原样还原: 得到 %q, 期望 %q", got, plaintext)
		}
	})
}

// TestParseHexErrors 验证非法 Hex 输入返回包装错误而非 panic 或空密钥。
func TestParseHexErrors(t *testing.T) {
	s := newTestSM2(t)

	t.Run("非法Hex字符", func(t *testing.T) {
		if _, err := s.ParsePrivateKeyFromHex("zz-not-hex"); err == nil {
			t.Fatalf("非法 Hex 私钥应返回错误")
		}
		if _, err := s.ParsePublicKeyFromHex("zz-not-hex"); err == nil {
			t.Fatalf("非法 Hex 公钥应返回错误")
		}
	})

	t.Run("公钥长度非法", func(t *testing.T) {
		if _, err := s.ParsePublicKeyFromHex("00ff"); err == nil {
			t.Fatalf("非 64/65 字节的公钥应返回错误")
		}
	})

	t.Run("私钥D溢出", func(t *testing.T) {
		overflow := strings.Repeat("ff", 33)
		if _, err := s.ParsePrivateKeyFromHex(overflow); err == nil {
			t.Fatalf("D 溢出曲线阶应返回错误")
		}
	})
}

// TestEncryptRejectsEmptyPlaintext 验证空明文在入口被拦截并立即返回错误：
// 上游 gmsm sm2.Encrypt 对空明文因 kdf(0) 恒返回 false 会无限 continue
// （每轮 2 次曲线运算，goroutine 永久卡死——首次跑 FuzzEncryptDecryptRoundTrip
// 时实测 2 分钟超时取证），本包必须将其降级为快速失败。
func TestEncryptRejectsEmptyPlaintext(t *testing.T) {
	s := newTestSM2(t)
	_, _, publicKey := mustGenerateKeyPair(t)
	for _, format := range []EncryptFormat{C1C3C2, C1C2C3, C1C3C2Compressed, C1C2C3Compressed} {
		if _, err := s.Encrypt(publicKey, []byte{}, format).ToBytes(); err == nil {
			t.Fatalf("format=%d 空明文应返回错误", format)
		}
	}
}

// TestDecryptToHexKeeps65Byte04Plaintext 守护 ToHex 不改写数据：解密出
// 65 字节且首字节恰为 0x04 的明文时，历史特例（为公钥裸 Hex 而设的 0x04 裁剪）
// 会静默丢弃首字节——概率 1/256 的数据损坏。Result.data 私有、外部无法注入
// 公钥字节，该特例在导出 API 上无任何合法用途，拆除后 ToHex 恒原样编码。
func TestDecryptToHexKeeps65Byte04Plaintext(t *testing.T) {
	s := newTestSM2(t)
	_, privateKey, publicKey := mustGenerateKeyPair(t)
	plaintext := make([]byte, 65)
	plaintext[0] = 0x04 // 命中旧特例的首字节条件
	for i := 1; i < len(plaintext); i++ {
		plaintext[i] = byte(i)
	}
	ciphertext, err := s.Encrypt(publicKey, plaintext, C1C3C2).ToBytes()
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	got, err := s.Decrypt(privateKey, ciphertext, C1C3C2).ToHex()
	if err != nil {
		t.Fatalf("解密转 Hex 失败: %v", err)
	}
	if want := hex.EncodeToString(plaintext); got != want {
		t.Fatalf("ToHex 不应裁剪首字节: 得到 %s..., 期望 %s", got[:8], want[:8])
	}
}
