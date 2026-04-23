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
	"os"
)

const (
	// 密钥长度
	KEYSIZE = 1024
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
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
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

// Sign 签名数据 - 使用SHA1和PKCS1v15填充，与Java的SHA1WithRSA签名算法兼容
func Sign(data []byte, privateKeyStr string) (string, error) {
	// 解码Base64私钥
	privateKeyPEM, err := base64.StdEncoding.DecodeString(privateKeyStr)
	if err != nil {
		return "", err
	}

	// 解码PEM格式私钥
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return "", errors.New("failed to decode PEM block containing private key")
	}

	// 解析私钥，支持PKCS#1和PKCS#8格式
	var privateKey *rsa.PrivateKey
	if block.Type == "PRIVATE KEY" {
		// PKCS#8格式私钥
		key, decryptErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if decryptErr != nil {
			return "", err
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return "", errors.New("not an RSA private key")
		}
	} else if block.Type == "RSA PRIVATE KEY" {
		// PKCS#1格式私钥
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", err
		}
	} else {
		return "", errors.New("unsupported private key format")
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
	// 解码Base64公钥
	publicKeyPEM, err := base64.StdEncoding.DecodeString(publicKeyStr)
	if err != nil {
		return false, err
	}

	// 解码PEM格式公钥
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return false, errors.New("failed to decode PEM block containing public key")
	}

	// 解析公钥
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return false, err
	}

	rsaPublicKey, ok := publicKey.(*rsa.PublicKey)
	if !ok {
		return false, errors.New("not an RSA public key")
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

	// 加密数据 - 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容
	encryptedData, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPublicKey, data)
	if err != nil {
		return nil, err
	}

	// 返回Base64编码的加密数据
	return []byte(base64.StdEncoding.EncodeToString(encryptedData)), nil
}

// DecryptByPrivateKey 使用私钥解密 - 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容
func DecryptByPrivateKey(data []byte, privateKeyStr string) ([]byte, error) {
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
	var privateKey *rsa.PrivateKey
	if block.Type == "PRIVATE KEY" {
		// PKCS#8格式私钥
		key, signErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if signErr != nil {
			return nil, err
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("not an RSA private key")
		}
	} else if block.Type == "RSA PRIVATE KEY" {
		// PKCS#1格式私钥
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("unsupported private key format")
	}

	// 解码Base64数据
	encryptedData, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, err
	}

	// 解密数据 - 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容
	decryptedData, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, encryptedData)
	if err != nil {
		return nil, err
	}

	return decryptedData, nil
}

// EncryptByPrivateKey 使用私钥加密（签名）- 使用PKCS1v15填充，与Java的RSA/ECB/PKCS1Padding转换模式兼容
func EncryptByPrivateKey(data []byte, privateKeyStr string) ([]byte, error) {
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
	var privateKey *rsa.PrivateKey
	if block.Type == "PRIVATE KEY" {
		// PKCS#8格式私钥
		key, verifyErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if verifyErr != nil {
			return nil, err
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("not an RSA private key")
		}
	} else if block.Type == "RSA PRIVATE KEY" {
		// PKCS#1格式私钥
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("unsupported private key format")
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
