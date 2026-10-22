// Package rsaes 实现 RFC 8017 的 RSAES-PKCS1-v1_5 填充，也就是 Java 侧的 RSA/ECB/PKCS1Padding。
//
// 为什么要在仓内自己实现：Go 1.26 起 crypto/rsa 把 PKCS #1 v1.5 的加解密入口
// （EncryptPKCS1v15、DecryptPKCS1v15、DecryptPKCS1v15SessionKey、PKCS1v15DecryptOptions）
// 全部标记为 Deprecated，官方给出的唯一替代是 OAEP。但存量对接方——Java 的
// RSA/ECB/PKCS1Padding、各家云厂商开放接口——用的就是 PKCS #1 v1.5，直接切 OAEP
// 等于单方面断协议，不是可选项。于是把「继续提供 v1.5 兼容」这件事收敛到本包：
// 一份实现、一份说明、一组测试，gocrypto 与 gorsa 两个调用方共用，免得各抄一遍而漂移。
//
// 两侧的安全姿态（改这个文件前请先读这段）：
//   - Encrypt 只做公钥运算 c = m^e mod n。公钥和密文都是公开量，没有需要保密的时间侧信道，
//     因此不需要常量时间实现；唯一需要保密的是随机填充串 PS 本身，取自 crypto/rand。
//     但入参形状必须校严，因为 math/big 对非法密钥是静默改语义而不是报错（见 checkPublicKey）。
//   - Decrypt 不自己写模幂，而是走 crypto.Decrypter 接口方法 (*rsa.PrivateKey).Decrypt(rand, ct, nil)。
//     标准库这条路里，私钥模幂是常量时间的（crypto/internal/fips140/bigmod 的 4 bit 窗口幂，
//     查表用常量时间选择）、去填充也是常量时间扫描，且所有失败只返回同一个 ErrDecryption。
//     自己重写这些既容易出错，也必然弱于标准库。
//
// 与标准库的一处不对齐：标准库在 GODEBUG=fips140=only 下会拒绝产生也拒绝解出 v1.5 密文，
// 而 crypto/fips140 只导出 Enabled()（on 与 only 都为 true），无法区分二者——硬判会误伤 on 模式。
// 因此本包不在 Encrypt 里复刻该门控（Decrypt 经标准库路径仍会被拦住）；
// 需要 FIPS 合规的部署请整链路改用 OAEP，不要依赖本包。
//
// 新对接请优先使用 OAEP（crypto/rsa.EncryptOAEP / DecryptOAEP）；本包只作为协议兼容层保留。
package rsaes

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"math/big"
)

const (
	// minPSLen RFC 8017 7.2.1 规定随机填充串 PS 至少 8 个非零字节。
	minPSLen = 8
	// emFixedOverhead EM = 0x00 || 0x02 || PS || 0x00 || M 相对明文的固定开销，
	// 等于 2 个前导字节 + 1 个分隔字节 + minPSLen，即明文最长 k-11。
	emFixedOverhead = 2 + minPSLen + 1
	// emTypeTwoPadding 第二字节固定为 0x02，表示「公钥加密」产物；0x01 是私钥侧的签名产物。
	emTypeTwoPadding = 2
	// minModulusBits 模数比特数下限，与标准库 checkKeySize 同一档：1024 位以下已被判定为不安全，
	// 兼容层不放大弱密钥的可用面（GODEBUG=rsa1024min=0 是标准库内部开关，本包不复刻）。
	minModulusBits = 1024
	// minPublicExponent 公钥指数下限。标准库的判定是 e >= 2 且为奇数，等价于 e >= 3 且为奇数。
	minPublicExponent = 3
	// maxPublicExponent 公钥指数上限，与标准库 checkPublicKey 同档：
	// 限定在 31 位以内，使 32 位与 64 位平台的行为一致（int 宽度不影响可解的密钥范围）。
	maxPublicExponent = 1<<31 - 1
)

// Encrypt 用公钥把 msg 加密成一个 k 字节密文（k 为模数长度），填充方案为 RSAES-PKCS1-v1_5。
//
// 明文长度不得超过 k-emFixedOverhead，超出时返回标准库的 rsa.ErrMessageTooLong，
// 与直接调用 crypto/rsa 的旧代码保持一致。
func Encrypt(pub *rsa.PublicKey, msg []byte) ([]byte, error) {
	if err := checkPublicKey(pub); err != nil {
		return nil, err
	}

	k := pub.Size()
	if len(msg) > k-emFixedOverhead {
		return nil, rsa.ErrMessageTooLong
	}

	// EM = 0x00 || 0x02 || PS || 0x00 || M，末段留明文，分隔符之前的区间填 PS。
	em := make([]byte, k)
	em[1] = emTypeTwoPadding
	ps := em[2 : k-len(msg)-1]
	if err := fillNonZeroRandomBytes(ps, rand.Reader); err != nil {
		return nil, err
	}
	em[k-len(msg)-1] = 0
	copy(em[k-len(msg):], msg)

	// EM 首字节为 0x00，所以 EM < 256^(k-1)；而 k = ceil(BitLen(N)/8) 意味着
	// BitLen(N) > 8(k-1)，即 N >= 2^(8(k-1)) = 256^(k-1)。两边合起来就是 EM < N，
	// 模幂入参一定落在 [0, N) 内，结果也不超过 k 字节，FillBytes 不会 panic
	//（前提就是 checkPublicKey 已保证 N 为正数且长度合法）。
	c := new(big.Int).Exp(new(big.Int).SetBytes(em), big.NewInt(int64(pub.E)), pub.N)
	return c.FillBytes(make([]byte, k)), nil
}

// Decrypt 用私钥解密 RSAES-PKCS1-v1_5 密文，返回明文。
//
// 填充不合法、密文长度不对、密文不小于模数、用错私钥等情况一律返回标准库的
// rsa.ErrDecryption，不区分具体错在哪一步——外部可观测的失败原因必须只有一种，
// 否则就成了 Bleichenbacher 百万消息攻击的 oracle。
func Decrypt(priv *rsa.PrivateKey, ciphertext []byte) ([]byte, error) {
	if priv == nil {
		return nil, errors.New("rsaes: nil private key")
	}

	// opts 传 nil：按 crypto.Decrypter 的约定就是 PKCS #1 v1.5 解密，
	// 常量时间模幂与常量时间去填充校验都由标准库完成，并保留 1024 位模数下限检查。
	return priv.Decrypt(rand.Reader, ciphertext, nil)
}

// checkPublicKey 校验公钥形状，判定项与标准库 checkPublicKey 对齐。
// 模数符号与指数范围两项不能省，它们防的是 math/big 的静默改语义而不是报错：
//   - Exp 的文档语义是 z = x**y mod |m|，m 的符号被忽略：负模数会被当正模数继续用下去；
//     而 m == 0 时根本不取模，直接返回整个 x**y，随后 FillBytes 因结果超出 k 字节而 panic。
//   - 指数为负时 Exp 转去算模逆（x 与 m 不互质时还会直接返回 nil），
//     与加密原语该做的运算完全不同。
func checkPublicKey(pub *rsa.PublicKey) error {
	if pub == nil || pub.N == nil {
		return errors.New("rsaes: missing public modulus")
	}
	if pub.N.Sign() <= 0 {
		return errors.New("rsaes: public modulus must be positive")
	}
	if bits := pub.N.BitLen(); bits < minModulusBits {
		return fmt.Errorf("rsaes: %d-bit keys are insecure (minimum %d bits)", bits, minModulusBits)
	}
	if pub.N.Bit(0) == 0 {
		return errors.New("rsaes: public modulus is even")
	}
	if pub.E < minPublicExponent || pub.E%2 == 0 {
		return fmt.Errorf("rsaes: invalid public exponent %d (must be an odd number >= %d)",
			pub.E, minPublicExponent)
	}
	if pub.E > maxPublicExponent {
		return fmt.Errorf("rsaes: public exponent too large (maximum %d)", maxPublicExponent)
	}
	return nil
}

// fillNonZeroRandomBytes 用 r 填满 s，并保证其中不含 0x00。
// PKCS #1 v1.5 要求 PS 由非零字节构成：任何一个 0x00 都会提前形成分隔符，
// 使解密方按错误的位置切分明文。
func fillNonZeroRandomBytes(s []byte, r io.Reader) error {
	if _, err := io.ReadFull(r, s); err != nil {
		return err
	}
	for i := range s {
		for s[i] == 0 {
			if _, err := io.ReadFull(r, s[i:i+1]); err != nil {
				return err
			}
		}
	}
	return nil
}
