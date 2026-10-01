// Package gosm4 封装国密 SM4 对称加密算法，提供 ECB / CBC 两种模式的加解密能力。
//
// 核心功能：
//   - ECB 模式：EncryptECB / DecryptECB，另有 FromByte/FromHex/FromBase64 输入变体。
//   - CBC 模式：EncryptCBC / DecryptCBC，IV 由调用方每次传入，实现不读写任何全局状态，可并发调用。
//   - PKCS7 填充：包内自实现填充与校验，解密失败（乱码密文、长度非 16 倍数、空输入）如实返回错误，
//     不会静默返回空数据，也不会对合法空明文误报。
//   - 选项模式：WithUnescapeHTML 控制加密前是否做 HTML 反转义（默认开启，兼容历史密文生成路径）。
//
// 密文与 gmsm 上游 sm4.Sm4Ecb/Sm4Cbc 字节级兼容，internal/config 已有的 Nacos ENC() 密文无需迁移。
package gosm4

import (
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"

	"github.com/tjfoc/gmsm/sm4"
)

// SM4Option 是用于配置SM4的函数类型
type SM4Option func(*sm4Options)

// sm4Options 包含SM4的所有可配置选项
type sm4Options struct {
	unescapeHTML bool
}

// defaultSM4Options 返回默认的SM4选项
func defaultSM4Options() *sm4Options {
	return &sm4Options{
		unescapeHTML: true, // 默认进行HTML转义处理，保持向后兼容
	}
}

// apply 应用所有提供的选项
func (o *sm4Options) apply(opts ...SM4Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithUnescapeHTML 设置是否进行HTML转义处理。
// ⚠ 默认 true：EncryptECB/EncryptCBC 会先把明文 html.UnescapeString（&amp; → &），
// 解密入口不做对应处理，对含 HTML 实体的输入 Decrypt(Encrypt(x)) != x（往返不守恒）；
// 配置、密码等要求字节原样的场景必须显式 WithUnescapeHTML(false)。
func WithUnescapeHTML(unescape bool) SM4Option {
	return func(o *sm4Options) {
		o.unescapeHTML = unescape
	}
}

// SM4 封装了SM4算法相关的操作
type SM4 struct {
	unescapeHTML bool
}

// NewSM4 创建一个新的SM4实例。
// 不返回 error：当前 Option 均为纯值、无 IO；实例无全局状态，可并发使用。
func NewSM4(opts ...SM4Option) *SM4 {
	o := defaultSM4Options()
	o.apply(opts...)
	return &SM4{
		unescapeHTML: o.unescapeHTML,
	}
}

// EncryptECB 使用SM4 ECB模式加密数据。
// 注意：默认对明文做 HTML 反转义，见 WithUnescapeHTML 的数据改写警告。
func (s *SM4) EncryptECB(plaintextByte, keyByte []byte) *EncryptResult {
	if s.unescapeHTML {
		plaintextByte = []byte(html.UnescapeString(string(plaintextByte)))
	}
	ciphertext, err := sm4ECBEncrypt(keyByte, plaintextByte)
	if err != nil {
		return &EncryptResult{&Result{nil, fmt.Errorf("failed to encrypt with SM4 ECB: %w", err)}}
	}

	return &EncryptResult{&Result{ciphertext, nil}}
}

// DecryptECB 使用SM4 ECB模式解密数据
func (s *SM4) DecryptECB(ciphertextByte, keyByte []byte) *DecryptResult {
	// 自实现解密：gmsm 上游会丢弃 PKCS7 校验错误并对空输入越界 panic（见 sm4ECBDecrypt 注释）
	plaintext, err := sm4ECBDecrypt(keyByte, ciphertextByte)
	if err != nil {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to decrypt with SM4 ECB: %w", err)}}
	}

	return &DecryptResult{&Result{plaintext, nil}}
}

// DecryptECBFromByte 使用SM4 ECB模式解密字节数据
func (s *SM4) DecryptECBFromByte(ciphertextByte []byte, keyByte []byte) *DecryptResult {
	return s.DecryptECB(ciphertextByte, keyByte)
}

// DecryptECBFromHex 使用SM4 ECB模式解密十六进制字符串
func (s *SM4) DecryptECBFromHex(ciphertextHex string, keyByte []byte) *DecryptResult {
	// 将十六进制字符串解码为字节
	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to decode hex string: %w", err)}}
	}

	// 调用基础解密方法
	return s.DecryptECB(ciphertext, keyByte)
}

// DecryptECBFromBase64 使用SM4 ECB模式解密Base64编码字符串
func (s *SM4) DecryptECBFromBase64(ciphertextBase64 string, keyByte []byte) *DecryptResult {
	// 将Base64字符串解码为字节
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to decode base64 string: %w", err)}}
	}

	// 调用基础解密方法
	return s.DecryptECB(ciphertext, keyByte)
}

// EncryptCBC 使用SM4 CBC模式加密数据。
// 注意：默认对明文做 HTML 反转义，见 WithUnescapeHTML 的数据改写警告。
func (s *SM4) EncryptCBC(plaintextByte, keyByte, ivByte []byte) *EncryptResult {
	if s.unescapeHTML {
		plaintextByte = []byte(html.UnescapeString(string(plaintextByte)))
	}
	// IV 长度校验先于加密执行，错误文本与历史版本（gmsm sm4.SetIV）保持一致
	if len(ivByte) != sm4.BlockSize {
		return &EncryptResult{&Result{nil, fmt.Errorf("failed to set IV: %w", errIVSize)}}
	}
	ciphertextByte, err := sm4CBCEncrypt(keyByte, ivByte, plaintextByte)
	if err != nil {
		return &EncryptResult{&Result{nil, fmt.Errorf("failed to encrypt with SM4 CBC: %w", err)}}
	}
	return &EncryptResult{&Result{ciphertextByte, nil}}
}

// DecryptCBC 使用SM4 CBC模式解密数据
func (s *SM4) DecryptCBC(ciphertextByte, keyByte, ivByte []byte) *DecryptResult {
	if len(ivByte) != sm4.BlockSize {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to set IV: %w", errIVSize)}}
	}
	plaintextByte, err := sm4CBCDecrypt(keyByte, ivByte, ciphertextByte)
	if err != nil {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to decrypt with SM4 CBC: %w", err)}}
	}
	return &DecryptResult{&Result{plaintextByte, nil}}
}

// DecryptCBCFromByte 使用SM4 CBC模式解密字节数据
func (s *SM4) DecryptCBCFromByte(ciphertextByte, keyByte, ivByte []byte) *DecryptResult {
	return s.DecryptCBC(ciphertextByte, keyByte, ivByte)
}

// DecryptCBCFromHex 使用SM4 CBC模式解密十六进制字符串
func (s *SM4) DecryptCBCFromHex(ciphertextHex string, keyByte, ivByte []byte) *DecryptResult {
	// 将十六进制字符串解码为字节
	ciphertextByte, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to decode hex string: %w", err)}}
	}

	// 调用基础解密方法
	return s.DecryptCBC(ciphertextByte, keyByte, ivByte)
}

// DecryptCBCFromBase64 使用SM4 CBC模式解密Base64编码字符串
func (s *SM4) DecryptCBCFromBase64(ciphertextBase64 string, keyByte, ivByte []byte) *DecryptResult {
	// 将Base64字符串解码为字节
	ciphertextByte, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to decode base64 string: %w", err)}}
	}

	// 调用基础解密方法
	return s.DecryptCBC(ciphertextByte, keyByte, ivByte)
}

// EncryptResult 包装加密结果，支持链式调用转换格式
type EncryptResult struct {
	*Result
}

// DecryptResult 包装解密结果，支持链式调用转换格式
type DecryptResult struct {
	*Result
}

// Result 包装通用结果数据
type Result struct {
	data []byte
	err  error
}

// 错误哨兵：errIVSize 文本与 gmsm v1.4.1 sm4.SetIV 返回值保持一致，保证上层错误串不变。
var (
	// errIVSize IV 长度不等于 16 字节。
	errIVSize = errors.New("SM4: invalid iv size")
	// errCiphertextLen 密文长度不是 16 字节的整倍数，无法按块解密。
	errCiphertextLen = errors.New("SM4: 密文长度必须是 16 字节的整倍数")
	// errPadding PKCS7 填充校验失败（乱码密文 / 空输入）。gmsm 上游会丢弃该错误返回
	// (nil, nil) 甚至对空输入越界 panic，本包改为如实上抛，避免解密失败被误判为成功。
	errPadding = errors.New("PKCS7 填充校验失败")
)

// sm4ECBEncrypt 基于 sm4.NewCipher 自实现 SM4-ECB + PKCS7 加密，
// 密文与 gmsm sm4.Sm4Ecb(key, in, true) 字节级一致。
// 调用方保证 len(key) 由 NewCipher 校验（错误文本与上游一致：SM4: invalid key size N）。
func sm4ECBEncrypt(key, plaintext []byte) ([]byte, error) {
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plaintext)
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += sm4.BlockSize {
		block.Encrypt(out[i:i+sm4.BlockSize], padded[i:i+sm4.BlockSize])
	}
	return out, nil
}

// sm4ECBDecrypt 基于 sm4.NewCipher 自实现 SM4-ECB 解密 + PKCS7 校验。
// 不调用 gmsm sm4.Sm4Ecb 的原因：其解密路径 out, _ = pkcs7UnPadding(out) 丢弃校验错误，
// 乱码密文会静默返回 (nil, nil)；空输入还会在 pkcs7UnPadding 内 src[length-1] 越界 panic
// （gmsm v1.4.1 sm4.go L277-292 / L369 取证）。本实现对这两种情况均返回明确错误。
func sm4ECBDecrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%sm4.BlockSize != 0 {
		return nil, errCiphertextLen
	}
	out := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += sm4.BlockSize {
		block.Decrypt(out[i:i+sm4.BlockSize], ciphertext[i:i+sm4.BlockSize])
	}
	return pkcs7Unpad(out)
}

// sm4CBCEncrypt 基于 sm4.NewCipher + crypto/cipher 实现 SM4-CBC + PKCS7 加密。
// 不调用 gmsm sm4.Sm4Cbc/SetIV 的原因：上游把 IV 存在包级全局变量 sm4.IV 中
// （gmsm v1.4.1 sm4.go L29/L293/L311 取证），SetIV 与 Sm4Cbc 两步无任何同步，
// 并发使用不同 IV 会互相覆盖导致错误加解密；本实现 IV 只存在于本次调用栈内。
// 密文与 gmsm sm4.Sm4Cbc(key, in, true) 字节级一致（标准 CBC + PKCS7）。
// 调用方保证 len(key)、len(iv) 均为 16（方法入口已校验，NewCipher 另行兜底 key）。
func sm4CBCEncrypt(key, iv, plaintext []byte) ([]byte, error) {
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	ivCopy := make([]byte, sm4.BlockSize)
	copy(ivCopy, iv) // 显式拷贝，杜绝调用方复用底层数组时被中途修改
	padded := pkcs7Pad(plaintext)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, ivCopy).CryptBlocks(out, padded)
	return out, nil
}

// sm4CBCDecrypt 基于 sm4.NewCipher + crypto/cipher 实现 SM4-CBC 解密 + PKCS7 校验，
// 错误处理语义与 sm4ECBDecrypt 一致：长度非整倍数 / 填充非法 / 空输入均返回明确错误。
func sm4CBCDecrypt(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%sm4.BlockSize != 0 {
		return nil, errCiphertextLen
	}
	ivCopy := make([]byte, sm4.BlockSize)
	copy(ivCopy, iv)
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, ivCopy).CryptBlocks(out, ciphertext)
	return pkcs7Unpad(out)
}

// pkcs7Pad 按 PKCS7 填充到 16 字节边界。
// 总是分配新切片，不写入入参的底层数组（gmsm 上游 pkcs7Padding 用 append 会污染调用方数据）。
func pkcs7Pad(src []byte) []byte {
	padding := sm4.BlockSize - len(src)%sm4.BlockSize
	out := make([]byte, len(src)+padding)
	copy(out, src)
	for i := len(src); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}

// pkcs7Unpad 校验并去除 PKCS7 填充，失败返回 errPadding。
// 空入参直接报错，避免上游的越界 panic；合法空明文（整块填充）可正常还原为空切片。
func pkcs7Unpad(src []byte) ([]byte, error) {
	if len(src) == 0 || len(src)%sm4.BlockSize != 0 {
		return nil, errPadding
	}
	n := int(src[len(src)-1])
	if n == 0 || n > sm4.BlockSize || n > len(src) {
		return nil, errPadding
	}
	for i := len(src) - n; i < len(src); i++ {
		if src[i] != byte(n) {
			return nil, errPadding
		}
	}
	return src[:len(src)-n], nil
}

// ToBytes 返回原始字节数据
func (r *Result) ToBytes() ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.data, nil
}

// ToHex 返回十六进制字符串格式的数据
func (r *Result) ToHex() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return hex.EncodeToString(r.data), nil
}

// ToBase64 返回Base64编码的字符串格式数据
func (r *Result) ToBase64() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return base64.StdEncoding.EncodeToString(r.data), nil
}
