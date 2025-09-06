package gosm4

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"io"
)

// 添加gmsm/sm4导入以支持SM4加密
import (
	"github.com/tjfoc/gmsm/sm4"
)

// SM4Option 是用于配置SM4的函数类型
type SM4Option func(*sm4Options)

// sm4Options 包含SM4的所有可配置选项
type sm4Options struct {
	rand     io.Reader
	save     bool
	saveFile string
}

// defaultSM4Options 返回默认的SM4选项
func defaultSM4Options() *sm4Options {
	return &sm4Options{
		rand: rand.Reader,
	}
}

// apply 应用所有提供的选项
func (o *sm4Options) apply(opts ...SM4Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithRand 设置随机数生成器
func WithRand(reader io.Reader) SM4Option {
	return func(o *sm4Options) {
		o.rand = reader
	}
}

// SM4 封装了SM4算法相关的操作
type SM4 struct {
	rand io.Reader
	save bool
}

// NewSM4 创建一个新的SM4实例
func NewSM4(opts ...SM4Option) *SM4 {
	o := defaultSM4Options()
	o.apply(opts...)
	return &SM4{
		rand: o.rand,
		save: o.save,
	}
}

// EncryptECB 使用SM4 ECB模式加密数据
func (s *SM4) EncryptECB(plaintext []byte, key []byte) *EncryptResult {
	plaintextByte := []byte(html.UnescapeString(string(plaintext)))
	ciphertext, err := sm4.Sm4Ecb(key, plaintextByte, true)
	if err != nil {
		return &EncryptResult{&Result{nil, fmt.Errorf("failed to encrypt with SM4 ECB: %w", err)}}
	}

	return &EncryptResult{&Result{ciphertext, nil}}
}

// DecryptECB 使用SM4 ECB模式解密数据
func (s *SM4) DecryptECB(ciphertext []byte, key []byte) *DecryptResult {
	// 使用gmsm库中的Sm4Ecb方法进行ECB解密
	plaintext, err := sm4.Sm4Ecb(key, ciphertext, false)
	if err != nil {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to decrypt with SM4 ECB: %w", err)}}
	}
	
	return &DecryptResult{&Result{plaintext, nil}}
}

// DecryptECBFromByte 使用SM4 ECB模式解密字节数据
func (s *SM4) DecryptECBFromByte(ciphertext []byte, key []byte) *DecryptResult {
	return s.DecryptECB(ciphertext, key)
}

// DecryptECBFromHex 使用SM4 ECB模式解密十六进制字符串
func (s *SM4) DecryptECBFromHex(ciphertextHex string, key []byte) *DecryptResult {
	// 将十六进制字符串解码为字节
	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to decode hex string: %w", err)}}
	}
	
	// 调用基础解密方法
	return s.DecryptECB(ciphertext, key)
}

// DecryptECBFromBase64 使用SM4 ECB模式解密Base64编码字符串
func (s *SM4) DecryptECBFromBase64(ciphertextBase64 string, key []byte) *DecryptResult {
	// 将Base64字符串解码为字节
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return &DecryptResult{&Result{nil, fmt.Errorf("failed to decode base64 string: %w", err)}}
	}
	
	// 调用基础解密方法
	return s.DecryptECB(ciphertext, key)
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

// pkcs7Padding 使用PKCS7填充数据
func pkcs7Padding(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := make([]byte, padding)
	for i := range padtext {
		padtext[i] = byte(padding)
	}
	return append(data, padtext...)
}

// pkcs7Unpadding 去除PKCS7填充
func pkcs7Unpadding(data []byte) ([]byte, error) {
	length := len(data)
	if length == 0 {
		return nil, fmt.Errorf("invalid padding size")
	}

	padding := int(data[length-1])
	if padding > length {
		return nil, fmt.Errorf("invalid padding size")
	}

	return data[:(length - padding)], nil
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
