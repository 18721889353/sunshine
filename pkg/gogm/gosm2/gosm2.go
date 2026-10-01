// Package gosm2 封装国密 SM2 非对称加密算法，提供密钥生成、加解密、签名验签与多格式转换能力。
//
// 核心功能：
//   - 密钥管理：GenerateKeyPair 生成 PEM/Hex 双格式密钥对，支持去除 PEM 头（WithStripHeader）
//     与自动落盘（WithSave）。
//   - 格式转换：PEM/Hex 与 sm2.PrivateKey/sm2.PublicKey 互转；Hex 输出固定宽度（私钥 64 位、
//     公钥 128 位），前导零自动补位，保证本包生成的密钥一定能被本包重新解析。
//   - 加解密：Encrypt/Decrypt 支持 C1C3C2/C1C2C3 及各自压缩格式，结果经 EncryptResult/
//     DecryptResult 链式转换为 Hex/Base64/字节。
//   - 签名验签：Sign 产出 SignResult，配合 VerifyFromBytes/VerifyFromHex/VerifyFromBase64 验签。
//   - 选项模式：WithRand/WithStripHeader/WithSave/WithUnescapeHTML。
package gosm2

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
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
	rand         io.Reader
	stripHeader  bool
	save         bool
	privateFile  string
	pubFile      string
	unescapeHTML bool
}

// defaultSM2Options 返回默认的SM2选项
func defaultSM2Options() *sm2Options {
	return &sm2Options{
		rand:         rand.Reader,
		unescapeHTML: true, // 默认进行HTML转义处理，保持向后兼容
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

// WithStripHeader 设置是否去除公钥和私钥PEM格式的头部和尾部。
// 只影响返回 KeyPair 的显示内容；落盘不受影响：SaveKeyPair 对去头内容
// 会自动补全标准 PEM 头，WithSave 自动落盘的始终是完整 PEM。
func WithStripHeader(strip bool) SM2Option {
	return func(o *sm2Options) {
		o.stripHeader = strip
	}
}

// WithSave 设置是否自动保存密钥到文件
func WithSave(privateFile, pubFile string) SM2Option {
	return func(o *sm2Options) {
		o.save = true
		o.privateFile = privateFile
		o.pubFile = pubFile
	}
}

// WithUnescapeHTML 设置是否进行HTML转义处理
func WithUnescapeHTML(unescape bool) SM2Option {
	return func(o *sm2Options) {
		o.unescapeHTML = unescape
	}
}

// SM2 封装了SM2算法相关的操作
type SM2 struct {
	rand         io.Reader
	stripHeader  bool
	save         bool
	privateFile  string
	pubFile      string
	unescapeHTML bool
}

// NewSM2 创建一个新的SM2实例。
// 不返回 error：当前全部 Option 均为纯值、无 IO 与前置校验；
// 若未来引入需校验的选项（如文件路径、密钥源），将改为 (SM2, error) 双返回值。
func NewSM2(opts ...SM2Option) *SM2 {
	o := defaultSM2Options()
	o.apply(opts...)
	return &SM2{
		rand:         o.rand,
		stripHeader:  o.stripHeader,
		save:         o.save,
		privateFile:  o.privateFile,
		pubFile:      o.pubFile,
		unescapeHTML: o.unescapeHTML,
	}
}

// GenerateKeyPair 生成SM2密钥对。
// 注意：启用 WithSave 时落盘失败会直接返回错误且不返回密钥对（fail-fast），
// 已生成的密钥不会以任何形式交给调用方——调用方需修正落盘路径后重新生成。
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

	// 固定 32 字节宽度的字节表示：big.Int.Bytes() 会省略前导零，
	// 不补位时 X/Y 分量带前导零（各约 1/256 概率）会让公钥 Hex 短于 128 位，
	// x509.ReadPublicKeyFromHex 因长度不足拒绝解析（"publicKey is not uncompressed."）
	privBytes := paddedBytes(privateKey.D.Bytes())
	pubBytes := append(paddedBytes(pubKey.X.Bytes()), paddedBytes(pubKey.Y.Bytes())...)

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
		if err := s.SaveKeyPairRaw(privateKeyStr, publicKeyStr, s.privateFile, s.pubFile); err != nil {
			return nil, fmt.Errorf("failed to save key pair to file: %v", err)
		}
	}

	return keyPair, nil
}

// SaveKeyPair 保存密钥对到文件，落盘内容恒为标准 PEM：
// KeyPair 来自 WithStripHeader(true) 的去头内容时自动补全 BEGIN/END 头，
// 且块类型与 gmsm x509.WritePrivateKeyToPem/WritePublicKeyToPem 原生输出一致
// （私钥 PKCS8 "PRIVATE KEY"、公钥 "PUBLIC KEY"），保证与 WithSave 自动落盘
// 的产物同构，openssl 与本包均可直接解析。
func (s *SM2) SaveKeyPair(keyPair *KeyPair, privateFile, pubFile string) error {
	if err := saveToFile(privateFile, []byte(ensurePEMHeader(keyPair.PrivateKeyPEM, "PRIVATE KEY")), 0600); err != nil {
		return fmt.Errorf("failed to save private key: %v", err)
	}
	if err := saveToFile(pubFile, []byte(ensurePEMHeader(keyPair.PublicKeyPEM, "PUBLIC KEY")), 0644); err != nil {
		return fmt.Errorf("failed to save public key: %v", err)
	}
	return nil
}

// ensurePEMHeader 确保 PEM 内容含标准头尾：已含 BEGIN 时原样返回，
// 缺头时按 blockType 补齐（blockType 取 gmsm 原生块类型，见 SaveKeyPair 注释）。
func ensurePEMHeader(pemStr, blockType string) string {
	if strings.Contains(pemStr, "-----BEGIN") {
		return pemStr
	}
	return "-----BEGIN " + blockType + "-----\n" + pemStr + "\n-----END " + blockType + "-----"
}

// SaveKeyPairRaw 保存原始密钥对到文件（保留完整PEM格式）
func (s *SM2) SaveKeyPairRaw(privateKeyPem, publicKeyPem, privateFile, pubFile string) error {
	if err := saveToFile(privateFile, []byte(privateKeyPem), 0600); err != nil {
		return fmt.Errorf("failed to save private key: %v", err)
	}
	if err := saveToFile(pubFile, []byte(publicKeyPem), 0644); err != nil {
		return fmt.Errorf("failed to save public key: %v", err)
	}
	return nil
}

// saveToFile 安全地保存文件：先写同目录临时文件再 rename 覆盖，
// 任一步失败时旧文件保持完整——避免写入中断留下半截密钥
// （半截 PEM 会在下次解析时才报错，比落盘当场失败更难排查）。
func saveToFile(filename string, data []byte, perm os.FileMode) error {
	// 获取文件路径中的目录部分
	dir := filepath.Dir(filename)

	// 如果目录不存在，则创建目录（包括必要的父目录）
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}

	// 临时文件与目标同目录，保证 rename 同分区原子
	tmp := filename + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, filename); err != nil {
		// 兜底清理临时文件；清理成功（或文件本就不存在）时 rename 错误原样上抛，
		// 仅当清理也失败时附加提示——两个错误都丢弃会留下无主的 .tmp 文件且无人知晓
		if rmErr := os.Remove(tmp); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("%w (临时文件清理也失败: %v)", err, rmErr)
		}
		return err
	}
	return nil
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

// PrivateKeyToHex 将私钥转换为十六进制字符串（固定 64 位，前导零补位）
func (s *SM2) PrivateKeyToHex(privateKey *sm2.PrivateKey) (string, error) {
	privBytes := paddedBytes(privateKey.D.Bytes())
	return hex.EncodeToString(privBytes), nil
}

// PublicKeyToHex 将公钥转换为十六进制字符串（固定 128 位，X/Y 各补位到 32 字节）
func (s *SM2) PublicKeyToHex(publicKey *sm2.PublicKey) (string, error) {
	pubBytes := append(paddedBytes(publicKey.X.Bytes()), paddedBytes(publicKey.Y.Bytes())...)
	return hex.EncodeToString(pubBytes), nil
}

// paddedBytes 将 big.Int 的字节表示左侧补零到 32 字节（SM2 标量/坐标分量的固定宽度）。
// big.Int.Bytes() 会省略前导零，直接拼接会导致输出长度不定：公钥 Hex 短于 128 位时
// x509.ReadPublicKeyFromHex 会拒绝解析，生成的密钥自己解析不回去。
func paddedBytes(b []byte) []byte {
	const sm2ScalarLen = 32
	if len(b) >= sm2ScalarLen {
		return b
	}
	out := make([]byte, sm2ScalarLen)
	copy(out[sm2ScalarLen-len(b):], b)
	return out
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
	// C1C3C2Compressed 压缩格式
	C1C3C2Compressed
	// C1C2C3Compressed 压缩格式
	C1C2C3Compressed
)

// Encrypt 使用SM2公钥加密数据，支持指定格式。
//
// ⚠ 数据改写警告：默认（未显式 WithUnescapeHTML(false)）会先对明文做
// html.UnescapeString——"&amp;hello" 实际加密的是 "&hello"，而解密入口不做对应处理，
// 因此对含 HTML 实体的输入 Decrypt(Encrypt(x)) != x（往返不守恒）；
// 需要字节原样进出时必须显式 NewSM2(WithUnescapeHTML(false))。
//
// 空明文直接返回错误：上游 gmsm sm2.Encrypt 内 kdf(0) 恒返回 false（sm2.go
// kdf 尾部检查循环对 length=0 不进入，直接 return c, false），调用方 continue
// 永久重试（每轮 2 次曲线运算，goroutine 卡死）；本入口在进入上游前拦截，
// 将死循环降级为立即失败。
func (s *SM2) Encrypt(publicKey *sm2.PublicKey, plainTextByte []byte, format EncryptFormat) *EncryptResult {
	if len(plainTextByte) == 0 {
		return &EncryptResult{&Result{nil, fmt.Errorf("SM2: 明文不能为空（上游对空明文会无限重试）")}}
	}
	if s.unescapeHTML {
		plainTextByte = []byte(html.UnescapeString(string(plainTextByte)))
	}
	var encrypted []byte
	var err error
	switch format {
	case C1C2C3:
		encrypted, err = sm2.Encrypt(publicKey, plainTextByte, s.rand, sm2.C1C2C3)
	case C1C3C2Compressed:
		// 使用标准C1C3C2格式加密，然后手动处理压缩
		encrypted, err = sm2.Encrypt(publicKey, plainTextByte, s.rand, sm2.C1C3C2)
		if err == nil && len(encrypted) >= 65 && encrypted[0] == 0x04 {
			// 移除0x04前缀以实现压缩效果
			encrypted = encrypted[1:]
		}
	case C1C2C3Compressed:
		// 使用标准C1C2C3格式加密，然后手动处理压缩
		encrypted, err = sm2.Encrypt(publicKey, plainTextByte, s.rand, sm2.C1C2C3)
		if err == nil && len(encrypted) >= 65 && encrypted[0] == 0x04 {
			// 移除0x04前缀以实现压缩效果
			encrypted = encrypted[1:]
		}
	default:
		encrypted, err = sm2.Encrypt(publicKey, plainTextByte, s.rand, sm2.C1C3C2)
	}

	return &EncryptResult{Result: &Result{data: encrypted, err: err}}
}

// EncryptResult 包装加密结果，支持链式调用转换格式
type EncryptResult struct {
	*Result
}

// DecryptFormat 定义解密格式类型
type DecryptFormat = EncryptFormat

// Decrypt 使用SM2私钥解密数据，支持指定格式。
// 注意：压缩格式（C1C3C2Compressed/C1C2C3Compressed）密文必须是**不含 0x04 前缀**的
// 64 字节 C1——本方法无条件补前缀；已带前缀的数据需调用方先去前缀再传入。
// 不做"首字节是否 0x04"探测：合法压缩密文的 C1 首字节本就有 1/256 概率恰为 0x04，
// 探测必然误判（把合法密文当已带前缀而跳过补位，导致其永远解不开）。
func (s *SM2) Decrypt(privateKey *sm2.PrivateKey, ciphertextByte []byte, format DecryptFormat) *DecryptResult {
	var decrypted []byte
	var err error

	// 针对压缩格式，需要在解密前添加0x04前缀
	var dataToDecrypt []byte
	switch format {
	case C1C3C2Compressed, C1C2C3Compressed:
		// 为压缩格式数据添加0x04前缀以便正确解密
		dataToDecrypt = make([]byte, len(ciphertextByte)+1)
		dataToDecrypt[0] = 0x04
		copy(dataToDecrypt[1:], ciphertextByte)
	default:
		dataToDecrypt = ciphertextByte
	}

	switch format {
	case C1C2C3, C1C2C3Compressed:
		decrypted, err = sm2.Decrypt(privateKey, dataToDecrypt, sm2.C1C2C3)
	default:
		decrypted, err = sm2.Decrypt(privateKey, dataToDecrypt, sm2.C1C3C2)
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

// ToHex 将结果转换为十六进制字符串，不改写数据。
//
// 历史实现对“65 字节且首字节 0x04”（SM2 未压缩公钥裸字节）自动裁剪前缀，
// 但本方法已拆除该特例，反证如下：
//  1. Result.data 为私有字段，外部无法把公钥字节注入 Result；包内 Encrypt 结果
//     ≥ 113 字节、Sign（DER）≥ 70 字节，均不会命中 65 字节；
//  2. 唯一命中路径是 Decrypt 出 65 字节且首字节恰为 0x04 的明文（概率 1/256），
//     旧特例会静默丢弃明文首字节——这是数据损坏，不是兼容行为；
//  3. 公钥 Hex 场景已有专用 PublicKeyToHex（定宽 128 位），不依赖本方法。
//
// 故 ToHex 对任何数据恒为原样 hex.EncodeToString。
func (r *Result) ToHex() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return hex.EncodeToString(r.data), nil
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

// SignResult 包装签名结果，支持链式调用转换格式
type SignResult struct {
	*Result
}

// Sign 使用SM2私钥对数据进行签名。
//
// ⚠ 数据改写警告：与 Encrypt 相同，默认会先对数据做 html.UnescapeString——
// 签名者提供的原始字节可能不是实际签署的内容（"&amp;" 被签成 "&"），
// 需要签署原始字节时必须显式 NewSM2(WithUnescapeHTML(false))。
func (s *SM2) Sign(privateKey *sm2.PrivateKey, dataByte []byte) *SignResult {
	if s.unescapeHTML {
		dataByte = []byte(html.UnescapeString(string(dataByte)))
	}
	signature, err := privateKey.Sign(s.rand, dataByte, nil)
	return &SignResult{Result: &Result{data: signature, err: err}}
}

// VerifyFromBytes 使用SM2公钥验证签名。
// 返回 false 表示验签不通过；本方法不区分“签名字节结构非法”与“签名/数据不匹配”。
// 需要分级监控时应由调用方先自行解析签名字节，对结构错误与验签失败分别记账。
func (s *SM2) VerifyFromBytes(publicKey *sm2.PublicKey, dataByte, signatureByte []byte) bool {
	return publicKey.Verify(dataByte, signatureByte)
}

// VerifyFromHex 使用SM2公钥验证十六进制字符串签名。
// 返回 false 有三种可能：Hex 解码失败（调用方输入错误）、签名字节结构非法、
// 签名或数据不匹配（含篡改）——三者不作区分；需要把格式错误与验签失败分开
// 记账时，请先 hex.DecodeString 自行解码（捕获格式错误）再调 VerifyFromBytes。
func (s *SM2) VerifyFromHex(publicKey *sm2.PublicKey, dataByte []byte, hexSignature string) bool {
	signature, err := hex.DecodeString(hexSignature)
	if err != nil {
		return false
	}
	return s.VerifyFromBytes(publicKey, dataByte, signature)
}

// VerifyFromBase64 使用SM2公钥验证Base64编码字符串签名。
// 返回 false 的三种可能与 VerifyFromHex 相同（解码失败/结构非法/不匹配，不作区分）；
// 需要分级时请先 base64.StdEncoding.DecodeString 自行解码再调 VerifyFromBytes。
func (s *SM2) VerifyFromBase64(publicKey *sm2.PublicKey, dataByte []byte, base64Signature string) bool {
	signature, err := base64.StdEncoding.DecodeString(base64Signature)
	if err != nil {
		return false
	}
	return s.VerifyFromBytes(publicKey, dataByte, signature)
}
