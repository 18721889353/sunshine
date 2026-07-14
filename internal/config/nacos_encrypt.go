package config

import (
	"fmt"

	"github.com/18721889353/sunshine/pkg/gogm/gosm4"
)

// nacosSM4Key SM4 密钥，用于加密/解密 Nacos 配置文件中的敏感凭据。
// 16 字节（128 位），硬编码在代码中，仅提供"静态存储加密"保护，
// 防止服务器上配置文件被直接读取泄露明文。
var nacosSM4Key = []byte("Nacos@SM4!Key#16")

// nacosSM4Iv SM4 初始化向量，配合密钥使用。
var nacosSM4Iv = []byte("Nacos@SM4!IV#16!")

// decryptNacosField 使用 SM4-CBC 解密 Nacos 凭据字段（Hex 编码输入）。
func decryptNacosField(encryptedHex string) (string, error) {
	if encryptedHex == "" {
		return "", nil
	}
	sm4 := gosm4.NewSM4(gosm4.WithUnescapeHTML(false))
	result := sm4.DecryptCBCFromHex(encryptedHex, nacosSM4Key, nacosSM4Iv)
	data, err := result.ToBytes()
	if err != nil {
		return "", fmt.Errorf("SM4 解密 Nacos 凭据失败: %w", err)
	}
	return string(data), nil
}

// DecryptNacosCredentials 解密 Center 结构体中的 Nacos 凭据。
// 字段值为 "ENC(hex)" 格式时提取 hex 密文并解密；
// 非此格式则保持原值不动（兼容已有明文配置）。
func DecryptNacosCredentials(center *Center) error {
	if center == nil {
		return nil
	}

	var err error
	center.Nacos.Username, err = tryDecryptField(center.Nacos.Username)
	if err != nil {
		return fmt.Errorf("解密 Nacos 用户名失败: %w", err)
	}

	center.Nacos.Password, err = tryDecryptField(center.Nacos.Password)
	if err != nil {
		return fmt.Errorf("解密 Nacos 密码失败: %w", err)
	}

	return nil
}

/*
tryDecryptField 尝试解密单个字段。

检测字段值是否以 "ENC(" 开头并以 ")" 结尾，若是则提取中间 Hex 密文并调用 decryptNacosField 解密；
否则将字段值视为明文直接返回，不执行任何解密操作。

参数:
  - value: 待处理的字段值，可能为 "ENC(hex)" 格式或普通明文

返回值:
  - string: 解密后的明文（若为 ENC 格式），或原值（若为非 ENC 格式）
  - error: 解密失败时返回错误，非 ENC 格式返回 nil
*/
func tryDecryptField(value string) (string, error) {
	const prefix = "ENC("
	const suffix = ")"

	if len(value) > len(prefix)+len(suffix) && value[:len(prefix)] == prefix && value[len(value)-len(suffix):] == suffix {
		hexStr := value[len(prefix) : len(value)-len(suffix)]
		return decryptNacosField(hexStr)
	}
	return value, nil
}

// EncryptNacosField 使用 SM4-CBC 加密 Nacos 凭据字段（返回 Hex 编码），
// 用于生成加密后的配置值（配合 ENC() 格式使用）。
func EncryptNacosField(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	sm4 := gosm4.NewSM4(gosm4.WithUnescapeHTML(false))
	result := sm4.EncryptCBC([]byte(plaintext), nacosSM4Key, nacosSM4Iv)
	hexStr, err := result.ToHex()
	if err != nil {
		return "", fmt.Errorf("SM4 加密 Nacos 凭据失败: %w", err)
	}
	return hexStr, nil
}
