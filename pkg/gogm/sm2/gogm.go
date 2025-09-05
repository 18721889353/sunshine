package sm2

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tjfoc/gmsm/sm2"
	"github.com/tjfoc/gmsm/x509"
)

// KeyPair 结构体包含PEM和HEX格式的密钥对
type KeyPair struct {
	PrivateKeyPEM string
	PublicKeyPEM  string
	PrivateKeyHex string
	PublicKeyHex  string
}

// SM2Option 是用于配置SM2的函数类型
type SM2Option func(*sm2Options)

// sm2Options 包含SM2的所有可配置选项
type sm2Options struct {
	rand        io.Reader
	stripHeader bool
	save        bool
	privFile    string
	pubFile     string
}

// defaultSM2Options 返回默认的SM2选项
func defaultSM2Options() *sm2Options {
	return &sm2Options{
		rand: rand.Reader,
	}
}

// apply 应用所有提供的选项
func (o *sm2Options) apply(opts ...SM2Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithRand 设置随机数生成器
func WithRand(reader io.Reader) SM2Option {
	return func(o *sm2Options) {
		o.rand = reader
	}
}

// WithStripHeader 设置是否去除公钥和私钥PEM格式的头部和尾部
func WithStripHeader(strip bool) SM2Option {
	return func(o *sm2Options) {
		o.stripHeader = strip
	}
}

// WithSave 设置是否自动保存密钥到文件
func WithSave(privFile, pubFile string) SM2Option {
	return func(o *sm2Options) {
		o.save = true
		o.privFile = privFile
		o.pubFile = pubFile
	}
}

// SM2 封装了SM2算法相关的操作
type SM2 struct {
	rand        io.Reader
	stripHeader bool
	save        bool
	privFile    string
	pubFile     string
}

// NewSM2 创建一个新的SM2实例
func NewSM2(opts ...SM2Option) *SM2 {
	o := defaultSM2Options()
	o.apply(opts...)
	return &SM2{
		rand:        o.rand,
		stripHeader: o.stripHeader,
		save:        o.save,
		privFile:    o.privFile,
		pubFile:     o.pubFile,
	}
}

// GenerateKeyPair 生成SM2密钥对
func (s *SM2) GenerateKeyPair() (*KeyPair, error) {
	privateKey, err := sm2.GenerateKey(s.rand)
	if err != nil {
		return nil, err
	}

	// 类型断言检查
	pubKey, ok := privateKey.Public().(*sm2.PublicKey)
	if !ok {
		return nil, fmt.Errorf("failed to get public key")
	}

	// 直接保存密钥的字节表示
	privBytes := privateKey.D.Bytes()
	pubBytes := append(pubKey.X.Bytes(), pubKey.Y.Bytes()...)

	privatePem, err := x509.WritePrivateKeyToPem(privateKey, nil) // 生成密钥文件
	if err != nil {
		return nil, err
	}
	pubKeyPem, err := x509.WritePublicKeyToPem(pubKey) // 生成公钥文件
	if err != nil {
		return nil, err
	}

	// 处理私钥PEM格式
	privateKeyStr := string(privatePem)
	// 处理公钥PEM格式
	publicKeyStr := string(pubKeyPem)

	// 用于返回的keyPair中的内容根据stripHeader选项决定是否去除header
	privateKeyDisplay := privateKeyStr
	publicKeyDisplay := publicKeyStr
	if s.stripHeader {
		// 去除 BEGIN 和 END 行，仅用于显示/返回
		privateKeyDisplay = stripPEMHeader(privateKeyStr)
		publicKeyDisplay = stripPEMHeader(publicKeyStr)
	}

	keyPair := &KeyPair{
		PrivateKeyPEM: privateKeyDisplay, // 根据选项决定是否去除header
		PublicKeyPEM:  publicKeyDisplay,  // 根据选项决定是否去除header
		PrivateKeyHex: hex.EncodeToString(privBytes),
		PublicKeyHex:  hex.EncodeToString(pubBytes),
	}

	// 如果设置了自动保存，则保存到文件（保存完整PEM格式）
	if s.save {
		// 保存时使用完整的PEM格式（包含header和footer）
		if err := s.SaveKeyPairRaw(privateKeyStr, publicKeyStr, s.privFile, s.pubFile); err != nil {
			return nil, fmt.Errorf("failed to save key pair to file: %v", err)
		}
	}

	return keyPair, nil
}

// SaveKeyPair 保存密钥对到文件（使用keyPair中的内容，可能已去除header）
func (s *SM2) SaveKeyPair(keyPair *KeyPair, privFile, pubFile string) error {
	if err := saveToFile(privFile, []byte(keyPair.PrivateKeyPEM), 0600); err != nil {
		return fmt.Errorf("failed to save private key: %v", err)
	}
	if err := saveToFile(pubFile, []byte(keyPair.PublicKeyPEM), 0644); err != nil {
		return fmt.Errorf("failed to save public key: %v", err)
	}
	return nil
}

// SaveKeyPairRaw 保存原始密钥对到文件（保留完整PEM格式）
func (s *SM2) SaveKeyPairRaw(privateKeyPem, publicKeyPem, privFile, pubFile string) error {
	if err := saveToFile(privFile, []byte(privateKeyPem), 0600); err != nil {
		return fmt.Errorf("failed to save private key: %v", err)
	}
	if err := saveToFile(pubFile, []byte(publicKeyPem), 0644); err != nil {
		return fmt.Errorf("failed to save public key: %v", err)
	}
	return nil
}

// saveToFile 安全地保存文件
func saveToFile(filename string, data []byte, perm os.FileMode) error {
	return os.WriteFile(filename, data, perm)
}

// ParsePrivateKeyFromHex 从十六进制字符串解析私钥
func (s *SM2) ParsePrivateKeyFromHex(hexKey string) (*sm2.PrivateKey, error) {
	privateKey, err := x509.ReadPrivateKeyFromHex(hexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex string: %v", err)
	}

	return privateKey, nil
}

// ParsePublicKeyFromHex 从十六进制字符串解析公钥
func (s *SM2) ParsePublicKeyFromHex(hexKey string) (*sm2.PublicKey, error) {
	publicKey, err := x509.ReadPublicKeyFromHex(hexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex string: %v", err)
	}
	return publicKey, nil
}

// PrivateKeyToHex 将私钥转换为十六进制字符串
func (s *SM2) PrivateKeyToHex(privateKey *sm2.PrivateKey) (string, error) {
	privBytes := privateKey.D.Bytes()
	return hex.EncodeToString(privBytes), nil
}

// PublicKeyToHex 将公钥转换为十六进制字符串
func (s *SM2) PublicKeyToHex(publicKey *sm2.PublicKey) (string, error) {
	pubBytes := append(publicKey.X.Bytes(), publicKey.Y.Bytes()...)
	return hex.EncodeToString(pubBytes), nil
}

// ParsePrivateKeyFromPem 从PEM格式字符串解析私钥
func (s *SM2) ParsePrivateKeyFromPem(pemKey string) (*sm2.PrivateKey, error) {
	// 如果PEM字符串不包含header和footer，添加它们
	if !strings.Contains(pemKey, "-----BEGIN") {
		pemKey = "-----BEGIN EC PRIVATE KEY-----\n" + pemKey + "\n-----END EC PRIVATE KEY-----"
	}

	privateKey, err := x509.ReadPrivateKeyFromPem([]byte(pemKey), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key from PEM: %v", err)
	}

	return privateKey, nil
}

// ParsePublicKeyFromPem 从PEM格式字符串解析公钥
func (s *SM2) ParsePublicKeyFromPem(pemKey string) (*sm2.PublicKey, error) {
	// 如果PEM字符串不包含header和footer，添加它们
	if !strings.Contains(pemKey, "-----BEGIN") {
		pemKey = "-----BEGIN PUBLIC KEY-----\n" + pemKey + "\n-----END PUBLIC KEY-----"
	}

	publicKey, err := x509.ReadPublicKeyFromPem([]byte(pemKey))
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key from PEM: %v", err)
	}

	return publicKey, nil
}

// PrivateKeyToPem 将私钥转换为PEM格式字符串
func (s *SM2) PrivateKeyToPem(privateKey *sm2.PrivateKey) (string, error) {
	pemBytes, err := x509.WritePrivateKeyToPem(privateKey, nil)
	if err != nil {
		return "", fmt.Errorf("failed to convert private key to PEM: %v", err)
	}
	return string(pemBytes), nil
}

// PublicKeyToPem 将公钥转换为PEM格式字符串
func (s *SM2) PublicKeyToPem(publicKey *sm2.PublicKey) (string, error) {
	pemBytes, err := x509.WritePublicKeyToPem(publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to convert public key to PEM: %v", err)
	}
	return string(pemBytes), nil
}

// stripPEMHeader 去除PEM格式的头部和尾部
func stripPEMHeader(pemStr string) string {
	lines := strings.Split(pemStr, "\n")
	var contentLines []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "-----BEGIN") && !strings.HasPrefix(line, "-----END") && line != "" {
			contentLines = append(contentLines, line)
		}
	}
	return strings.Join(contentLines, "\n")
}

// EncryptFormat 定义加密格式类型
type EncryptFormat int

const (
	// C1C3C2 格式
	C1C3C2 EncryptFormat = iota
	// C1C2C3 格式
	C1C2C3
)

// Encrypt 使用SM2公钥加密数据，支持指定格式
func (s *SM2) Encrypt(publicKey *sm2.PublicKey, data []byte, format EncryptFormat) *EncryptResult {
	var encrypted []byte
	var err error

	switch format {
	case C1C2C3:
		encrypted, err = sm2.Encrypt(publicKey, data, s.rand, sm2.C1C2C3)
	default:
		encrypted, err = sm2.Encrypt(publicKey, data, s.rand, sm2.C1C3C2)
	}

	return &EncryptResult{Result: &Result{data: encrypted, err: err}}
}

// EncryptResult 包装加密结果，支持链式调用转换格式
type EncryptResult struct {
	*Result
}

// DecryptFormat 定义解密格式类型
type DecryptFormat = EncryptFormat

// Decrypt 使用SM2私钥解密数据，支持指定格式
func (s *SM2) Decrypt(privateKey *sm2.PrivateKey, encryptedData []byte, format DecryptFormat) *DecryptResult {
	var decrypted []byte
	var err error

	switch format {
	case C1C2C3:
		decrypted, err = sm2.Decrypt(privateKey, encryptedData, sm2.C1C2C3)
	default:
		decrypted, err = sm2.Decrypt(privateKey, encryptedData, sm2.C1C3C2)
	}

	return &DecryptResult{Result: &Result{data: decrypted, err: err}}
}

// DecryptResult 包装解密结果，支持链式调用转换格式
type DecryptResult struct {
	*Result
}

// DecryptFromBytes 使用SM2私钥解密字节数据，支持指定格式
func (s *SM2) DecryptFromBytes(privateKey *sm2.PrivateKey, byteData []byte, format DecryptFormat) *DecryptResult {
	return s.Decrypt(privateKey, byteData, format)
}

// DecryptFromHex 使用SM2私钥解密十六进制字符串数据，支持指定格式
func (s *SM2) DecryptFromHex(privateKey *sm2.PrivateKey, hexData string, format DecryptFormat) *DecryptResult {
	encryptedData, err := hex.DecodeString(hexData)
	if err != nil {
		return &DecryptResult{Result: &Result{err: fmt.Errorf("failed to decode hex string: %v", err)}}
	}

	return s.Decrypt(privateKey, encryptedData, format)
}

// DecryptFromBase64 使用SM2私钥解密Base64编码字符串数据，支持指定格式
func (s *SM2) DecryptFromBase64(privateKey *sm2.PrivateKey, base64Data string, format DecryptFormat) *DecryptResult {
	encryptedData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return &DecryptResult{Result: &Result{err: fmt.Errorf("failed to decode base64 string: %v", err)}}
	}

	return s.Decrypt(privateKey, encryptedData, format)
}

// Result 包装通用结果数据
type Result struct {
	data []byte
	err  error
}

// ToHex 将结果转换为十六进制字符串
func (r *Result) ToHex() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return fmt.Sprintf("%x", r.data), nil
}

// ToBase64 将结果转换为Base64编码字符串
func (r *Result) ToBase64() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return base64.StdEncoding.EncodeToString(r.data), nil
}

// ToBytes 返回原始字节数据
func (r *Result) ToBytes() ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.data, nil
}

// Sign 使用SM2私钥对数据进行签名
func (s *SM2) Sign(privateKey *sm2.PrivateKey, data []byte) ([]byte, error) {
	return privateKey.Sign(s.rand, data, nil)
}

// Verify 使用SM2公钥验证签名
func (s *SM2) Verify(publicKey *sm2.PublicKey, data, signature []byte) bool {
	return publicKey.Verify(data, signature)
}
