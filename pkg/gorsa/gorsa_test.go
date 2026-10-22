// 本文件守护 pkg/gorsa 的对外契约：加解密底层已切到 pkg/gocrypto/rsaes，
// 但密文格式（定长 PKCS#1 v1.5）、base64 封装、以及 Java 侧 RSA/ECB/PKCS1Padding 能读通
// 这三件事不能有丝毫漂移。签名路径（rsa.SignPKCS1v15）本次未改动实现，一并纳入回归，
// 防的是日后有人「顺手」把签名也一起改了。
//
// 文件里刻意调用 crypto/rsa 的弃用入口 EncryptPKCS1v15 / DecryptPKCS1v15 作为参照实现 oracle：
// 测试文件不参与 lint（.golangci.yml 的 run.tests: false 与 exclusions.paths 已排除 *_test.go），
// 而标准库正是 RFC 8017 的权威实现，用它双向校验本包产物，比只在本包内部往返自证有力得多。
package gorsa

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	// testKeyBits 与包内 KEYSIZE 保持同档，保证明文上界算式一致。
	testKeyBits = 1024
	// ciphertextLen 是 1024 位密钥的定长密文字节数，即模数长度 k。
	ciphertextLen = testKeyBits / 8
	// maxPlaintextLen 是 PKCS#1 v1.5 在 1024 位密钥下的明文上界，即 k-11。
	maxPlaintextLen = ciphertextLen - 11
)

// keyPair 是本包 API 直接可消费的密钥集合：API 入参是 base64(PEM)，不是裸的 *rsa.PrivateKey。
type keyPair struct {
	public       string // PKIX 公钥，加密/验签用
	privatePKCS8 string // PKCS#8 私钥，GenerateKey 的产出格式
	privatePKCS1 string // PKCS#1 私钥，Java 侧常见的 RSA PRIVATE KEY 块
	key          *rsa.PrivateKey
}

// mustKeyPair 现场生成一对测试密钥。用生成而非固定 PEM，
// 是为了让失败只可能指向逻辑本身，而不是指向一份过期的 fixture。
func mustKeyPair(t *testing.T) keyPair {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, testKeyBits)
	if err != nil {
		t.Fatalf("生成测试密钥失败: %v", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("公钥转 PKIX 失败: %v", err)
	}
	pkcs8DER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("私钥转 PKCS#8 失败: %v", err)
	}

	return keyPair{
		public:       encodeBase64(pemEncode(pemTypePublicKey, pubDER)),
		privatePKCS8: encodeBase64(pemEncode(pemTypePrivateKey, pkcs8DER)),
		privatePKCS1: encodeBase64(pemEncode(pemTypePKCS1PrivateKey, x509.MarshalPKCS1PrivateKey(priv))),
		key:          priv,
	}
}

// pemEncode 按块类型打包 DER，块头与本包解析侧共用同一份常量。
func pemEncode(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}

// encodeBase64 按本包约定在 PEM 之外再套一层 base64。
func encodeBase64(pemBytes []byte) string {
	return base64.StdEncoding.EncodeToString(pemBytes)
}

// rawCiphertext 从本包加密返回值（base64 文本）取回裸密文。
func rawCiphertext(t *testing.T, encrypted []byte) []byte {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString(string(encrypted))
	if err != nil {
		t.Fatalf("加密返回值不是合法 base64: %v", err)
	}

	return raw
}

// mustEncrypt 加密失败直接 Fatal，让各测试正文只关心断言。
func mustEncrypt(t *testing.T, plaintext []byte, publicKey string) []byte {
	t.Helper()

	encrypted, err := EncryptByPublicKey(plaintext, publicKey)
	if err != nil {
		t.Fatalf("EncryptByPublicKey: %v", err)
	}

	return encrypted
}

// privateKeyFormat 命名一种私钥 PEM 块编码。
type privateKeyFormat struct {
	name   string
	keyPem string
}

// privateKeyFormats 返回两种私钥编码，用于「同一份密文在两种块类型下都要能解」的断言。
func privateKeyFormats(kp keyPair) []privateKeyFormat {
	return []privateKeyFormat{
		{"PKCS#8 私钥", kp.privatePKCS8},
		{"PKCS#1 私钥", kp.privatePKCS1},
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	kp := mustKeyPair(t)

	plaintexts := [][]byte{
		{},
		{0x00, 0x01, 0x00},
		[]byte("password=123456"),
		[]byte("中文与多字节明文，长度也不小"),
		make([]byte, maxPlaintextLen), // 恰好达到上界
	}

	for _, plaintext := range plaintexts {
		encrypted := mustEncrypt(t, plaintext, kp.public)

		// 对接方给的 PEM 块类型不可控，两种格式都要走得通。
		for _, format := range privateKeyFormats(kp) {
			decrypted, err := DecryptByPrivateKey(encrypted, format.keyPem)
			if err != nil {
				t.Fatalf("%s 解密 %d 字节明文失败: %v", format.name, len(plaintext), err)
			}
			if string(decrypted) != string(plaintext) {
				t.Fatalf("%s 往返后明文不一致：want %x，got %x", format.name, plaintext, decrypted)
			}
		}
	}
}

func TestEncryptCiphertextIsFixedLengthAndRandomized(t *testing.T) {
	kp := mustKeyPair(t)
	plaintext := []byte("same plaintext twice")

	first := rawCiphertext(t, mustEncrypt(t, plaintext, kp.public))
	second := rawCiphertext(t, mustEncrypt(t, plaintext, kp.public))

	// 定长是 v1.5 的硬性质：长度不等于模数长度就不是合法密文，前导零字节也必须保留。
	if len(first) != ciphertextLen || len(second) != ciphertextLen {
		t.Fatalf("密文应为 %d 字节，实际 %d / %d", ciphertextLen, len(first), len(second))
	}
	// 每次都要有新的随机填充，否则密文会被调用方当成可等值比对的字段。
	if string(first) == string(second) {
		t.Fatal("同一明文两次密文相同，随机填充失效")
	}
	// 密文作为整数必须小于模数，否则解密侧一律判失败。
	if new(big.Int).SetBytes(first).Cmp(kp.key.N) >= 0 {
		t.Fatal("密文不小于模数，不是合法 RSA 密文")
	}
}

func TestEncryptRejectsOversizedPlaintext(t *testing.T) {
	kp := mustKeyPair(t)

	// 超过 k-11 必须报 ErrMessageTooLong：这个错误值直接透出给调用方，不能被改写成正则式或措辞。
	if _, err := EncryptByPublicKey(make([]byte, maxPlaintextLen+1), kp.public); !errors.Is(err, rsa.ErrMessageTooLong) {
		t.Fatalf("超长明文应返回 rsa.ErrMessageTooLong，实际 %v", err)
	}
}

func TestInteropsWithStdlibReference(t *testing.T) {
	kp := mustKeyPair(t)
	plaintext := []byte("interop probe")

	// 方向一：本包加密的密文，标准库必须能解出——等价于 Java/openssl 能解开。
	ours := rawCiphertext(t, mustEncrypt(t, plaintext, kp.public))
	got, err := rsa.DecryptPKCS1v15(rand.Reader, kp.key, ours)
	if err != nil {
		t.Fatalf("标准库解不开本包密文: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("标准库还原不一致: want %q, got %q", plaintext, got)
	}

	// 方向二：标准库加密的密文，本包必须能解出——等价于本包接得上对端。
	theirs, err := rsa.EncryptPKCS1v15(rand.Reader, &kp.key.PublicKey, plaintext)
	if err != nil {
		t.Fatalf("标准库加密失败: %v", err)
	}

	decrypted, err := DecryptByPrivateKey([]byte(encodeBase64(theirs)), kp.privatePKCS8)
	if err != nil {
		t.Fatalf("本包解不开标准库密文: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("本包还原不一致: want %q, got %q", plaintext, decrypted)
	}
}

func TestDecryptRejectsInvalidInput(t *testing.T) {
	kp := mustKeyPair(t)
	other := mustKeyPair(t)
	valid := mustEncrypt(t, []byte("valid"), kp.public)

	t.Run("用错私钥只报 ErrDecryption", func(t *testing.T) {
		// 失败原因必须塌缩成同一个错误值：能被外部观测的失败差异就是选择密文攻击的 oracle。
		if _, err := DecryptByPrivateKey(valid, other.privatePKCS8); !errors.Is(err, rsa.ErrDecryption) {
			t.Fatalf("应返回 rsa.ErrDecryption，实际 %v", err)
		}
	})

	t.Run("密文被截断只报 ErrDecryption", func(t *testing.T) {
		truncated := rawCiphertext(t, valid)[:ciphertextLen-1]
		if _, err := DecryptByPrivateKey([]byte(encodeBase64(truncated)), kp.privatePKCS8); !errors.Is(err, rsa.ErrDecryption) {
			t.Fatalf("截断密文应返回 rsa.ErrDecryption，实际 %v", err)
		}
	})

	t.Run("密文不是 base64", func(t *testing.T) {
		if _, err := DecryptByPrivateKey([]byte("!!! 非 base64 !!!"), kp.privatePKCS8); err == nil {
			t.Fatal("非 base64 密文应报错")
		}
	})

	t.Run("私钥参数不是 base64", func(t *testing.T) {
		if _, err := DecryptByPrivateKey(valid, "!!! 非 base64 !!!"); err == nil {
			t.Fatal("非 base64 私钥应报错")
		}
	})

	t.Run("私钥不是 PEM", func(t *testing.T) {
		if _, err := DecryptByPrivateKey(valid, encodeBase64([]byte("这不是 PEM"))); err == nil {
			t.Fatal("非 PEM 私钥应报错")
		}
	})

	t.Run("私钥块类型不支持", func(t *testing.T) {
		notAKey := encodeBase64(pemEncode("CERTIFICATE", []byte("hello")))
		if _, err := DecryptByPrivateKey(valid, notAKey); err == nil {
			t.Fatal("CERTIFICATE 块当私钥用应报错")
		}
	})

	t.Run("PKCS#8 块解析失败必须给出错误而不是 nil", func(t *testing.T) {
		// 这条守护本次修掉的静默失败：块类型是 PRIVATE KEY 但 DER 内容是垃圾时，
		// 旧实现走 `return nil, err`，而那个 err 恒为 nil，调用方拿到空结果还以为成功了。
		broken := encodeBase64(pemEncode(pemTypePrivateKey, []byte("garbage der")))
		if _, err := DecryptByPrivateKey(valid, broken); err == nil {
			t.Fatal("畸形 PKCS#8 私钥必须返回错误")
		}
	})
}

func TestEncryptByPublicKeyRejectsBadPublicKey(t *testing.T) {
	kp := mustKeyPair(t)
	plaintext := []byte("x")

	if _, err := EncryptByPublicKey(plaintext, "!!! 非 base64 !!!"); err == nil {
		t.Fatal("非 base64 公钥应报错")
	}
	if _, err := EncryptByPublicKey(plaintext, encodeBase64([]byte("非 PEM"))); err == nil {
		t.Fatal("非 PEM 公钥应报错")
	}
	// 块类型对但内容不是合法 DER 编码的公钥：应停在 x509.ParsePKIXPublicKey。
	if _, err := EncryptByPublicKey(plaintext, encodeBase64(pemEncode(pemTypePublicKey, []byte("garbage")))); err == nil {
		t.Fatal("畸形 PKIX 公钥应报错")
	}

	// 合法的 EC 公钥必须被拒：本包只承诺 RSA，误用要早失败、要说清。
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成 EC 测试密钥失败: %v", err)
	}

	ecDER, err := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
	if err != nil {
		t.Fatalf("EC 公钥转 PKIX 失败: %v", err)
	}

	if _, err := EncryptByPublicKey(plaintext, encodeBase64(pemEncode(pemTypePublicKey, ecDER))); err == nil {
		t.Fatal("EC 公钥应被本包拒绝")
	}

	// 模数短于 1024 位的公钥不得被放过：兼容层不放大弱密钥的可用面。
	// 不能用 rsa.GenerateKey(rand, 512) 造样本——Go 自己已经拒绝生成这种密钥；
	// 拿现成 1024 位模数右移一位，就得到一个 1023 位且仍为奇数的模数。
	weakN := new(big.Int).Rsh(kp.key.N, 1)
	if weakN.Bit(0) == 0 {
		weakN.Or(weakN, big.NewInt(1))
	}

	weakDER, err := x509.MarshalPKIXPublicKey(&rsa.PublicKey{N: weakN, E: kp.key.E})
	if err != nil {
		t.Fatalf("弱公钥转 PKIX 失败: %v", err)
	}

	if _, err := EncryptByPublicKey(plaintext, encodeBase64(pemEncode(pemTypePublicKey, weakDER))); err == nil {
		t.Fatal("模数短于 1024 位的公钥应被拒绝")
	}
}

func TestSignAndVerify(t *testing.T) {
	kp := mustKeyPair(t)
	data := []byte("payload to sign")

	t.Run("PKCS#8 私钥签名可验", func(t *testing.T) {
		signature, err := Sign(data, kp.privatePKCS8)
		if err != nil {
			t.Fatalf("签名失败: %v", err)
		}
		if ok, err := Verify(data, kp.public, signature); err != nil || !ok {
			t.Fatalf("验签应通过，ok=%v err=%v", ok, err)
		}
	})

	t.Run("PKCS#1 私钥签名可验", func(t *testing.T) {
		signature, err := Sign(data, kp.privatePKCS1)
		if err != nil {
			t.Fatalf("签名失败: %v", err)
		}
		if ok, err := Verify(data, kp.public, signature); err != nil || !ok {
			t.Fatalf("验签应通过，ok=%v err=%v", ok, err)
		}
	})

	t.Run("私钥加密产物即签名可验", func(t *testing.T) {
		signature, err := EncryptByPrivateKey(data, kp.privatePKCS8)
		if err != nil {
			t.Fatalf("EncryptByPrivateKey 失败: %v", err)
		}
		if ok, err := Verify(data, kp.public, string(signature)); err != nil || !ok {
			t.Fatalf("验签应通过，ok=%v err=%v", ok, err)
		}
	})

	t.Run("明文被改动则验签失败", func(t *testing.T) {
		signature, err := Sign(data, kp.privatePKCS8)
		if err != nil {
			t.Fatalf("签名失败: %v", err)
		}

		tampered := append(append([]byte{}, data...), '!')
		if ok, err := Verify(tampered, kp.public, signature); ok || err == nil {
			t.Fatal("篡改明文后验签必须失败并给出错误")
		}
	})

	t.Run("换公钥则验签失败", func(t *testing.T) {
		signature, err := Sign(data, kp.privatePKCS8)
		if err != nil {
			t.Fatalf("签名失败: %v", err)
		}
		if ok, _ := Verify(data, mustKeyPair(t).public, signature); ok {
			t.Fatal("用他人公钥验签不应通过")
		}
	})

	t.Run("签名串不是 base64", func(t *testing.T) {
		if ok, err := Verify(data, kp.public, "!!! 非 base64 !!!"); ok || err == nil {
			t.Fatal("非 base64 签名串应报错")
		}
	})

	t.Run("PKCS#8 块解析失败必须给出错误而不是 nil", func(t *testing.T) {
		broken := encodeBase64(pemEncode(pemTypePrivateKey, []byte("garbage der")))
		signature, err := Sign(data, broken)
		if err == nil {
			t.Fatalf("畸形 PKCS#8 私钥必须返回错误，实际返回签名 %q", signature)
		}
		if signature != "" {
			t.Fatalf("签名失败时不应有返回值，实际 %q", signature)
		}
	})
}

func TestGenerateKeyProducesUsablePair(t *testing.T) {
	keys, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("应返回 [公钥, 私钥] 两项，实际 %d 项", len(keys))
	}

	// 生成的密钥对本包 API 立即可用，这一条兜住了 KEYSIZE 与 rsaes 模数下限的一致性。
	plaintext := []byte("fresh key pair")
	encrypted := mustEncrypt(t, plaintext, keys[0])

	decrypted, err := DecryptByPrivateKey(encrypted, keys[1])
	if err != nil {
		t.Fatalf("用生成的私钥解密失败: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("往返不一致: want %q, got %q", plaintext, decrypted)
	}

	// 生成的密钥同样要能签名，且能被标准库验通——否则 Java 侧验签一定接不上。
	signature, err := Sign(plaintext, keys[1])
	if err != nil {
		t.Fatalf("用生成的私钥签名失败: %v", err)
	}
	if ok, err := Verify(plaintext, keys[0], signature); err != nil || !ok {
		t.Fatalf("用生成的公钥验签失败，ok=%v err=%v", ok, err)
	}
}

func TestSaveAndLoadKeyFiles(t *testing.T) {
	kp := mustKeyPair(t)
	dir := t.TempDir()
	pubFile := filepath.Join(dir, "pub.pem")
	privFile := filepath.Join(dir, "priv.pem")

	if err := SaveKeyToPemFile(kp.public, kp.privatePKCS8, pubFile, privFile); err != nil {
		t.Fatalf("SaveKeyToPemFile: %v", err)
	}

	// 私钥落盘不应带 group/other 权限位：PEM 私钥被同机其他用户读到就等于密钥已泄。
	// Windows 上 FileMode 不反映真实 ACL（0600 写入必然读出 0666），
	// 只在真正实施 unix 权限的平台上断言，避免在本机留一个永远红的测试。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(privFile)
		if err != nil {
			t.Fatalf("Stat 私钥文件: %v", err)
		}

		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Fatalf("私钥文件不应有 group/other 权限位，实际 %o", perm)
		}
	}

	loadedPub, err := LoadPublicKeyFromFile(pubFile)
	if err != nil {
		t.Fatalf("LoadPublicKeyFromFile: %v", err)
	}
	loadedPriv, err := LoadPrivateKeyFromFile(privFile)
	if err != nil {
		t.Fatalf("LoadPrivateKeyFromFile: %v", err)
	}

	// 文件往返后仍能用，且加载结果就是本 API 期望的 base64(PEM) 形态。
	plaintext := []byte("via file")
	encrypted := mustEncrypt(t, plaintext, loadedPub)

	decrypted, err := DecryptByPrivateKey(encrypted, loadedPriv)
	if err != nil {
		t.Fatalf("用文件载入私钥解密失败: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("往返不一致: want %q, got %q", plaintext, decrypted)
	}

	t.Run("文件不存在", func(t *testing.T) {
		missing := filepath.Join(dir, "nope.pem")
		if _, err := LoadPublicKeyFromFile(missing); err == nil {
			t.Fatal("读取不存在的公钥文件应报错")
		}
		if _, err := LoadPrivateKeyFromFile(missing); err == nil {
			t.Fatal("读取不存在的私钥文件应报错")
		}
	})

	t.Run("内容不是 PEM", func(t *testing.T) {
		notPem := filepath.Join(dir, "not-pem.pem")
		if err := os.WriteFile(notPem, []byte("plain text"), 0o600); err != nil {
			t.Fatalf("写测试文件: %v", err)
		}
		if _, err := LoadPublicKeyFromFile(notPem); err == nil {
			t.Fatal("非 PEM 公钥文件应报错")
		}
		if _, err := LoadPrivateKeyFromFile(notPem); err == nil {
			t.Fatal("非 PEM 私钥文件应报错")
		}
	})
}
