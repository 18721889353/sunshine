package rsaes

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"math/big"
	"math/bits"
	"strings"
	"testing"
)

// testKeyBits 单元测试的密钥长度。取标准库允许的下限 1024 位：跑得快，
// 且 k=128 让「明文上界 k-11」这类边界好写好算。
const testKeyBits = 1024

// testKey 是包级共享的一次性密钥，避免每个用例重复生成。
var testKey = mustGenerateKey(testKeyBits)

// mustGenerateKey 生成测试密钥，失败直接 panic——它只在包初始化时调用，
// 此时还没有 *testing.T 可用，而密钥生成失败意味着整个测试文件没有运行意义。
func mustGenerateKey(bits int) *rsa.PrivateKey {
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		panic("rsaes: 生成测试密钥失败: " + err.Error())
	}
	return priv
}

// 本文件刻意调用 crypto/rsa 的弃用入口作为「参照实现 oracle」：
// 测试文件不参与 lint（.golangci.yml 的 run.tests: false 与 exclusions.paths 已排除 *_test.go），
// 而标准库正是 RFC 8017 的权威实现，用它双向校验本包产物，比自证清白强得多。
// 生产代码里一处都不要出现这些调用。

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// 1024 位密钥的 k=128，明文上界 k-11=117，这里把下界、上界、上界+1 都覆盖到。
	cases := []struct {
		name string
		msg  []byte
	}{
		{"空明文", []byte{}},
		{"单字节明文", []byte{0x00}},
		{"普通明文", []byte("rsaes_abcdefghijklmnopqrstuvwxyz0123456789")},
		{"含零字节的明文", []byte("a\x00b\x00c")},
		{"达到上界的明文", bytes.Repeat([]byte{'x'}, 117)},
		{"中文明文", []byte("阳光框架")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := Encrypt(&testKey.PublicKey, tc.msg)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			got, err := Decrypt(testKey, ciphertext)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if !bytes.Equal(got, tc.msg) {
				t.Fatalf("往返不一致: got %q, want %q", got, tc.msg)
			}
		})
	}
}

func TestEncryptCiphertextReadableByStdlib(t *testing.T) {
	msg := []byte("rsaes 产物必须能被标准库 RSAES-PKCS1-v1_5 解密路径还原")

	ciphertext, err := Encrypt(&testKey.PublicKey, msg)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	got, err := rsa.DecryptPKCS1v15(rand.Reader, testKey, ciphertext)
	if err != nil {
		t.Fatalf("标准库解密本包密文失败，说明填充编码不符合 RFC 8017: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %q, want %q", got, msg)
	}
}

func TestDecryptAcceptsStdlibCiphertext(t *testing.T) {
	msg := []byte("既有密文必须能被本包解开")

	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, &testKey.PublicKey, msg)
	if err != nil {
		t.Fatalf("标准库加密失败: %v", err)
	}

	got, err := Decrypt(testKey, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %q, want %q", got, msg)
	}
}

func TestEncryptOutputIsUniformLengthAndRandomized(t *testing.T) {
	msg := []byte("同一明文两次加密")
	k := testKey.PublicKey.Size()

	first, err := Encrypt(&testKey.PublicKey, msg)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := Encrypt(&testKey.PublicKey, msg)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if len(first) != k || len(second) != k {
		t.Fatalf("密文长度应为模数长度 %d，实际 %d / %d", k, len(first), len(second))
	}
	// 密文作为整数必须落在 [0, N) 内，否则接收方按 k 字节切分后无法解密。
	if new(big.Int).SetBytes(first).Cmp(testKey.N) >= 0 {
		t.Fatal("密文不小于模数，接收方将无法解密")
	}
	if bytes.Equal(first, second) {
		t.Fatal("同一明文两次密文相同，说明随机填充未生效")
	}
}

func TestEncryptRejectsMessageTooLong(t *testing.T) {
	cases := []struct {
		name   string
		msgLen int
	}{
		{"刚好上界 k-11", 117},
		{"超上界一个字节", 118},
		{"远超上界", 512},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Encrypt(&testKey.PublicKey, bytes.Repeat([]byte{'y'}, tc.msgLen))
			if tc.msgLen <= 117 {
				if err != nil {
					t.Fatalf("上界内不应报错: %v", err)
				}
				return
			}
			if !errors.Is(err, rsa.ErrMessageTooLong) {
				t.Fatalf("应返回 rsa.ErrMessageTooLong，实际 %v", err)
			}
		})
	}
}

func TestEncryptRejectsWeakPublicKey(t *testing.T) {
	t.Run("模数不足 1024 位", func(t *testing.T) {
		// 直接构造 512 位模数：标准库 GenerateKey 已拒绝生成弱密钥，
		// 但调用方可能从历史配置里读到，兼容层不能放行。
		weakN := new(big.Int).Rsh(testKey.N, 512)
		if weakN.BitLen() >= minModulusBits {
			t.Fatalf("测试数据未构造出弱模数，BitLen=%d", weakN.BitLen())
		}
		_, err := Encrypt(&rsa.PublicKey{N: weakN, E: testKey.E}, []byte("x"))
		if err == nil || !strings.Contains(err.Error(), "insecure") {
			t.Fatalf("应拒绝小模数，实际 %v", err)
		}
	})

	// big.Int.Exp 在指数为负时会转去算模逆，给出一个「看起来合法」的密文，
	// 因此非法指数必须在入口就挡掉，不能依赖后续步骤暴露问题。
	t.Run("偶数模数", func(t *testing.T) {
		// 把长度拉够但左移一位变成偶数：排除「被模数长度检查顺带拦下」的可能。
		evenN := new(big.Int).Lsh(testKey.N, 1)
		_, err := Encrypt(&rsa.PublicKey{N: evenN, E: testKey.E}, []byte("x"))
		if err == nil || !strings.Contains(err.Error(), "even") {
			t.Fatalf("应拒绝偶数模数，实际 %v", err)
		}
	})

	t.Run("公钥指数非法", func(t *testing.T) {
		for _, e := range []int{0, 1, 2, -65537} {
			_, err := Encrypt(&rsa.PublicKey{N: testKey.N, E: e}, []byte("x"))
			if err == nil || !strings.Contains(err.Error(), "exponent") {
				t.Fatalf("指数 %d 应被拒绝，实际 %v", e, err)
			}
		}
	})

	// 上限判定保证 32 位与 64 位平台行为一致，取值与标准库 checkPublicKey 同档。
	t.Run("公钥指数超出 31 位上限", func(t *testing.T) {
		if bits.UintSize != 64 {
			t.Skip("32 位平台的 int 根本放不下大于 2^31-1 的指数，这一分支由类型系统保证")
		}
		if _, err := Encrypt(&rsa.PublicKey{N: testKey.N, E: maxPublicExponent}, []byte("x")); err != nil {
			t.Fatalf("上限内的奇数指数不应被拒绝: %v", err)
		}
		// 用变量而不是常量表达式越界：写成 maxPublicExponent+2 会在 32 位平台编译期就溢出报错。
		e := maxPublicExponent
		e += 2
		_, err := Encrypt(&rsa.PublicKey{N: testKey.N, E: e}, []byte("x"))
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("应拒绝超过 31 位上限的指数，实际 %v", err)
		}
	})

	t.Run("公钥为 nil、缺模数或模数非正", func(t *testing.T) {
		if _, err := Encrypt(nil, []byte("x")); err == nil {
			t.Fatal("nil 公钥应报错")
		}
		if _, err := Encrypt(&rsa.PublicKey{E: 65537}, []byte("x")); err == nil {
			t.Fatal("缺模数的公钥应报错")
		}
		// 负模数与零模数都是 big.Int.Exp 的 panic 入口，必须在形状校验阶段挡掉。
		negN := new(big.Int).Neg(testKey.N)
		if _, err := Encrypt(&rsa.PublicKey{N: negN, E: testKey.E}, []byte("x")); err == nil ||
			!strings.Contains(err.Error(), "positive") {
			t.Fatalf("负模数应报错，实际 %v", err)
		}
		if _, err := Encrypt(&rsa.PublicKey{N: big.NewInt(0), E: testKey.E}, []byte("x")); err == nil {
			t.Fatal("零模数应报错")
		}
	})
}

func TestDecryptReturnsSingleErrorValue(t *testing.T) {
	valid, err := Encrypt(&testKey.PublicKey, []byte("uniform error"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	cases := []struct {
		name       string
		ciphertext []byte
	}{
		{"密文被截断", valid[:len(valid)-1]},
		{"密文多出字节", append(append([]byte{}, valid...), 0x01)},
		{"全零密文", make([]byte, len(valid))},
		{"随机密文", randomBytes(t, len(valid))},
		{"明文直接当密文", []byte("这不是密文，长度也不对")},
		{"空密文", []byte{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decrypt(testKey, tc.ciphertext)
			// 所有非法输入必须塌缩成同一个错误值：错误原因可被外部观测，
			// 就等价于给 Bleichenbacher 百万消息攻击提供了 oracle。
			if !errors.Is(err, rsa.ErrDecryption) {
				t.Fatalf("应统一返回 rsa.ErrDecryption，实际 %v", err)
			}
		})
	}
}

func TestDecryptRejectsMismatchedKey(t *testing.T) {
	other := mustGenerateKey(testKeyBits)

	ciphertext, err := Encrypt(&testKey.PublicKey, []byte("wrong key"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := Decrypt(other, ciphertext); !errors.Is(err, rsa.ErrDecryption) {
		t.Fatalf("用错误私钥解密应返回 rsa.ErrDecryption，实际 %v", err)
	}
}

func TestDecryptNilPrivateKey(t *testing.T) {
	// nil 私钥不能 panic，本包对外承诺只返回 error。
	_, err := Decrypt(nil, []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "private key") {
		t.Fatalf("nil 私钥应报错，实际 %v", err)
	}
}

func TestFillNonZeroRandomBytes(t *testing.T) {
	t.Run("产物不含零字节", func(t *testing.T) {
		// 多轮采样：零字节出现概率约 1/256，单轮通过不代表逻辑正确，
		// 这里跑 200 轮，把「重试分支」真实压出来。
		for round := 0; round < 200; round++ {
			buf := make([]byte, 64)
			if err := fillNonZeroRandomBytes(buf, rand.Reader); err != nil {
				t.Fatalf("第 %d 轮: %v", round, err)
			}
			if bytes.ContainsRune(buf, 0) {
				t.Fatalf("第 %d 轮产物含零字节: %x", round, buf)
			}
		}
	})

	t.Run("读到的零字节被逐个补齐", func(t *testing.T) {
		// 前 3 字节固定为 0，后续全 1：验证 0 会被重新读取替换掉。
		reader := io.MultiReader(strings.NewReader("\x00\x00\x00"), bytes.NewReader(bytes.Repeat([]byte{0x01}, 64)))
		buf := make([]byte, 5)
		if err := fillNonZeroRandomBytes(buf, reader); err != nil {
			t.Fatalf("fillNonZeroRandomBytes: %v", err)
		}
		if bytes.ContainsRune(buf, 0) {
			t.Fatalf("产物仍含零字节: %x", buf)
		}
	})

	t.Run("随机源出错被透传", func(t *testing.T) {
		wantErr := errors.New("随机源故障")
		err := fillNonZeroRandomBytes(make([]byte, 8), errReader{err: wantErr})
		if !errors.Is(err, wantErr) {
			t.Fatalf("应透传随机源错误，实际 %v", err)
		}
	})
}

// errReader 总是一次性返回错误的 io.Reader，用于注入随机源故障。
type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

// randomBytes 生成 n 字节随机数据，失败直接 Fatal。
func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		t.Fatalf("生成随机密文失败: %v", err)
	}
	return buf
}
