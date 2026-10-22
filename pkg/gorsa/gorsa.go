// Package gorsa 提供 RSA 非对称加密算法的封装。
// 该包支持密钥生成、加解密、签名和验签等功能。
package gorsa

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/18721889353/sunshine/pkg/gocrypto/rsaes"
)

const (
	// KEYSIZE 密钥长度
	KEYSIZE = 1024
)

// PEM 块类型：编码与解析两侧共用同一份定义，与 x509 Marshal*/Parse* 产出的块头一致。
const (
	// pemTypePublicKey PKIX 格式的公钥块
	pemTypePublicKey = "PUBLIC KEY"
	// pemTypePrivateKey PKCS#8 格式的私钥块
	pemTypePrivateKey = "PRIVATE KEY"
	// pemTypePKCS1PrivateKey PKCS#1 格式的 RSA 私钥块
	pemTypePKCS1PrivateKey = "RSA PRIVATE KEY"
)

// GenerateKey 生成RSA公私钥对
func GenerateKey() ([]string, error) {
	// 生成RSA密钥对
	privateKey, err := rsa.GenerateKey(rand.Reader, KEYSIZE)
	if err != nil {
		return nil, err
	}

	// 将公钥转换为PKIX格式
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, err
	}

	// 将私钥转换为PKCS#8格式
	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	// 使用PEM编码
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  pemTypePublicKey,
		Bytes: publicKeyBytes,
	})

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  pemTypePrivateKey,
		Bytes: privateKeyBytes,
	})

	// 使用Base64编码
	publicKeyBase64 := base64.StdEncoding.EncodeToString(publicKeyPEM)
	privateKeyBase64 := base64.StdEncoding.EncodeToString(privateKeyPEM)

	return []string{publicKeyBase64, privateKeyBase64}, nil
}

// LoadPublicKeyFromFile 从文件加载公钥
func LoadPublicKeyFromFile(filename string) (string, error) {
	// 读取文件内容
	publicKeyPEM, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}

	// PEM解码
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return "", errors.New("failed to decode PEM block containing public key")
	}

	// 重新编码为PEM格式并转换为Base64
	publicKeyPEM = pem.EncodeToMemory(block)
	publicKeyBase64 := base64.StdEncoding.EncodeToString(publicKeyPEM)

	return publicKeyBase64, nil
}

// LoadPrivateKeyFromFile 从文件加载私钥
func LoadPrivateKeyFromFile(filename string) (string, error) {
	// 读取文件内容
	privateKeyPEM, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}

	// PEM解码
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return "", errors.New("failed to decode PEM block containing private key")
	}

	// 重新编码为PEM格式并转换为Base64
	privateKeyPEM = pem.EncodeToMemory(block)
	privateKeyBase64 := base64.StdEncoding.EncodeToString(privateKeyPEM)

	return privateKeyBase64, nil
}

// parsePrivateKey 解析 base64(PEM) 编码的 RSA 私钥，同时支持 PKCS#1 与 PKCS#8 两种块类型。
// 签名、解密、私钥加密三个入口原本各抄了一遍这段逻辑，且都抄成了同一个错：PKCS#8 解析失败时
// `return xxx, err`，而那个 err 当时恒为 nil——解析失败却返回零值加 nil 错误，
// 调用方拿到空签名也查不出原因。收敛到一处，既修掉静默失败，也不给再抄一份的机会。
func parsePrivateKey(privateKeyStr string) (*rsa.PrivateKey, error) {
	// 解码Base64私钥
	privateKeyPEM, err := base64.StdEncoding.DecodeString(privateKeyStr)
	if err != nil {
		return nil, err
	}

	// 解码PEM格式私钥
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, errors.New("failed to decode PEM block containing private key")
	}

	// 解析私钥，支持PKCS#1和PKCS#8格式
	switch block.Type {
	case pemTypePrivateKey:
		// PKCS#8格式私钥
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse PKCS#8 private key: %w", err)
		}

		privateKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("not an RSA private key")
		}

		return privateKey, nil
	case pemTypePKCS1PrivateKey:
		// PKCS#1格式私钥
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, errors.New("unsupported private key format")
	}
}

// parsePublicKey 解析 base64(PEM) 编码的 PKIX RSA 公钥，加密与验签两个入口共用。
func parsePublicKey(publicKeyStr string) (*rsa.PublicKey, error) {
	// 解码Base64公钥
	publicKeyPEM, err := base64.StdEncoding.DecodeString(publicKeyStr)
	if err != nil {
		return nil, err
	}

	// 解码PEM格式公钥
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return nil, errors.New("failed to decode PEM block containing public key")
	}

	// 解析公钥
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPublicKey, ok := publicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}

	return rsaPublicKey, nil
}

// Sign 签名数据 - 使用SHA1和PKCS1v15填充，与Java的SHA1WithRSA签名算法兼容
func Sign(data []byte, privateKeyStr string) (string, error) {
	privateKey, err := parsePrivateKey(privateKeyStr)
	if err != nil {
		return "", err
	}

	// 计算数据的SHA1哈希值
	hashed := sha1.Sum(data)

	// 签名 - 使用PKCS1v15填充，与Java的SHA1WithRSA签名算法兼容
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA1, hashed[:])
	if err != nil {
		return "", err
	}

	// 返回Base64编码的签名
	return base64.StdEncoding.EncodeToString(signature), nil
}

// Verify 验证签名 - 使用SHA1和PKCS1v15填充，与Java的SHA1WithRSA签名算法兼容
func Verify(data []byte, publicKeyStr string, sign string) (bool, error) {
	rsaPublicKey, err := parsePublicKey(publicKeyStr)
	if err != nil {
		return false, err
	}

	// 解码Base64签名
	signature, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return false, err
	}

	// 计算数据的SHA1哈希值
	hashed := sha1.Sum(data)

	// 验证签名 - 使用PKCS1v15填充，与Java的SHA1WithRSA签名算法兼容
	err = rsa.VerifyPKCS1v15(rsaPublicKey, crypto.SHA1, hashed[:], signature)
	if err != nil {
		return false, err
	}

	return true, nil
}

// EncryptByPublicKey 使用公钥加密 - 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容
func EncryptByPublicKey(data []byte, publicKeyStr string) ([]byte, error) {
	rsaPublicKey, err := parsePublicKey(publicKeyStr)
	if err != nil {
		return nil, err
	}

	// 加密数据 - 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容（实现见 pkg/gocrypto/rsaes）
	encryptedData, err := rsaes.Encrypt(rsaPublicKey, data)
	if err != nil {
		return nil, err
	}

	// 返回Base64编码的加密数据
	return []byte(base64.StdEncoding.EncodeToString(encryptedData)), nil
}

// DecryptByPrivateKey 使用私钥解密 - 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容
func DecryptByPrivateKey(data []byte, privateKeyStr string) ([]byte, error) {
	privateKey, err := parsePrivateKey(privateKeyStr)
	if err != nil {
		return nil, err
	}

	// 解码Base64数据
	encryptedData, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, err
	}

	// 解密数据 - 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容（实现见 pkg/gocrypto/rsaes）
	decryptedData, err := rsaes.Decrypt(privateKey, encryptedData)
	if err != nil {
		return nil, err
	}

	return decryptedData, nil
}

// EncryptByPrivateKey 使用私钥加密（签名）- 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容
func EncryptByPrivateKey(data []byte, privateKeyStr string) ([]byte, error) {
	privateKey, err := parsePrivateKey(privateKeyStr)
	if err != nil {
		return nil, err
	}

	// 计算数据的SHA1哈希值
	hashed := sha1.Sum(data)

	// 签名数据（相当于私钥加密）- 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA1, hashed[:])
	if err != nil {
		return nil, err
	}

	// 返回Base64编码的签名
	return []byte(base64.StdEncoding.EncodeToString(signature)), nil
}

// SaveKeyToPemFile 将公钥和私钥保存到PEM文件
func SaveKeyToPemFile(publicKeyBase64, privateKeyBase64, publicKeyFile, privateKeyFile string) error {
	// 解码Base64公钥
	publicKeyPEM, err := base64.StdEncoding.DecodeString(publicKeyBase64)
	if err != nil {
		return err
	}

	// 解码Base64私钥
	privateKeyPEM, err := base64.StdEncoding.DecodeString(privateKeyBase64)
	if err != nil {
		return err
	}

	// 保存公钥到文件
	err = os.WriteFile(publicKeyFile, publicKeyPEM, 0644)
	if err != nil {
		return err
	}

	// 保存私钥到文件
	err = os.WriteFile(privateKeyFile, privateKeyPEM, 0600)
	if err != nil {
		return err
	}

	return nil
}
