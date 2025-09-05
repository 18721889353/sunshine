package main

import (
	"fmt"
	"github.com/18721889353/sunshine/pkg/gogm/sm2"
	"log"
)

func main() {
	//// 创建SM2实例，使用默认配置
	//sm2Instance := sm2.NewSM2()

	//// 创建SM2实例，去除PEM格式的头部和尾部
	//sm2Instance := sm2.NewSM2(sm2.WithStripHeader(true))

	// 创建SM2实例，自动生成并保存密钥对到文件
	//sm2Instance := sm2.NewSM2(
	//	sm2.WithStripHeader(true),
	//	sm2.WithSave("private.key", "public.key"),
	//)

	//// 创建SM2实例，使用自定义随机数生成器
	//sm2Instance := sm2.NewSM2(sm2.WithRand(nil)) // nil表示使用默认随机数生成器
	//
	//// 生成密钥对并自动保存到文件
	//keyPair, err := sm2Instance.GenerateKeyPair()
	//if err != nil {
	//	log.Fatalf("Failed to generate key pair: %v", err)
	//}
	//
	//fmt.Printf("Private Key (PEM):\n%s\n", keyPair.PrivateKeyPEM)
	//fmt.Printf("Public Key (PEM):\n%s\n", keyPair.PublicKeyPEM)
	//fmt.Printf("Private Key (hex):\n%s\n", keyPair.PrivateKeyHex)
	//fmt.Printf("Public Key (hex):\n%s\n", keyPair.PublicKeyHex)

	// 创建SM2实例
	sm2Instance := sm2.NewSM2()

	// 生成密钥对
	keyPair, err := sm2Instance.GenerateKeyPair()
	if err != nil {
		log.Fatalf("Failed to generate key pair: %v", err)
	}

	// 从PEM格式解析私钥
	privateKey, err := sm2Instance.ParsePrivateKeyFromPem(keyPair.PrivateKeyPEM)
	if err != nil {
		log.Fatalf("Failed to parse private key from PEM: %v", err)
	}

	// 从PEM格式解析公钥
	publicKey, err := sm2Instance.ParsePublicKeyFromPem(keyPair.PublicKeyPEM)
	if err != nil {
		log.Fatalf("Failed to parse public key from PEM: %v", err)
	}

	// 将私钥转换为十六进制
	privateKeyHex, err := sm2Instance.PrivateKeyToHex(privateKey)
	if err != nil {
		log.Fatalf("Failed to convert private key to hex: %v", err)
	}

	// 将公钥转换为十六进制
	publicKeyHex, err := sm2Instance.PublicKeyToHex(publicKey)
	if err != nil {
		log.Fatalf("Failed to convert public key to hex: %v", err)
	}
	fmt.Println("Private Key (hex):", privateKeyHex)
	fmt.Println("Public Key (hex):", publicKeyHex)

	////密钥转换为PEM格式
	//privateKeyPem, err := sm2Instance.PrivateKeyToPem(privateKey)
	//if err != nil {
	//	log.Fatalf("Failed to convert private key to PEM: %v", err)
	//}
	//
	//publicKeyPem, err := sm2Instance.PublicKeyToPem(publicKey)
	//if err != nil {
	//	log.Fatalf("Failed to convert public key to PEM: %v", err)
	//}
	//
	//fmt.Println("Private Key (PEM):", privateKeyPem)
	//fmt.Println("Public Key (PEM):", publicKeyPem)
	//
	////从十六进制解析私钥
	//privateKeyFromHex, err := sm2Instance.ParsePrivateKeyFromHex(privateKeyHex)
	//if err != nil {
	//	log.Fatalf("Failed to parse private key from hex: %v", err)
	//}
	//fmt.Println("Private Key (hex):", privateKeyFromHex)
	//
	//// 从十六进制解析公钥
	//publicKeyFromHex, err := sm2Instance.ParsePublicKeyFromHex(publicKeyHex)
	//if err != nil {
	//	log.Fatalf("Failed to parse public key from hex: %v", err)
	//}
	//fmt.Println("Public Key (hex):", publicKeyFromHex)

	// 加密数据并链式调用转换格式
	plaintext := []byte("需要加密的数据")
	// 直接获取十六进制字符串
	hexResult, err := sm2Instance.Encrypt(publicKey, plaintext, sm2.C1C3C2).ToBase64()
	if err != nil {
		log.Fatalf("Failed to encrypt data: %v", err)
	}
	fmt.Println("Encrypted data (hex):", hexResult)
	bytes, err := sm2Instance.DecryptFromBase64(privateKey, hexResult, sm2.C1C3C2).ToBytes()
	if err != nil {
		log.Fatalf("Failed to Decrypt data: %v", err)
	}
	fmt.Println("Decrypted data:", string(bytes))

}
