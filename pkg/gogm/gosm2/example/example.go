package main

import (
	"fmt"
	"github.com/18721889353/sunshine/pkg/gogm/gosm2"
	"log"
)

func main() {
	//// 创建SM2实例，去除PEM格式的头部和尾部
	sm2Instance := gosm2.NewSM2(
		gosm2.WithStripHeader(true),
		gosm2.WithSave("cert/private.key", "cert/public.key"),
	)
	keyPair, err := sm2Instance.GenerateKeyPair()
	if err != nil {
		log.Fatalf("Failed to generate key pair: %v", err)
	}
	fmt.Printf("Private Key (PEM):\n%s\n", keyPair.PrivateKeyPEM)
	fmt.Printf("Public Key (PEM):\n%s\n", keyPair.PublicKeyPEM)
	fmt.Printf("Private Key (hex):\n%s\n", keyPair.PrivateKeyHex)
	fmt.Printf("Public Key (hex):\n%s\n", keyPair.PublicKeyHex)

	// 创建SM2实例，自动生成并保存密钥对到文件
	//sm2Instance :=gogm.NewSM2(
	//	sm2.WithStripHeader(true),
	//	sm2.WithSave("private.key", "public.key"),
	//)

	//// 创建SM2实例，使用自定义随机数生成器
	//sm2Instance :=gogm.NewSM2(sm2.WithRand(nil)) // nil表示使用默认随机数生成器
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
	//sm2Instance := gosm2.NewSM2(gosm2.WithUnescapeHTML(false))

	//// 生成密钥对
	//keyPair, err := sm2Instance.GenerateKeyPair()
	//if err != nil {
	//	log.Fatalf("Failed to generate key pair: %v", err)
	//}
	//
	//// 从PEM格式解析私钥
	//privateKey, err := sm2Instance.ParsePrivateKeyFromPem(keyPair.PrivateKeyPEM)
	//if err != nil {
	//	log.Fatalf("Failed to parse private key from PEM: %v", err)
	//}
	//
	//// 从PEM格式解析公钥
	//publicKey, err := sm2Instance.ParsePublicKeyFromPem(keyPair.PublicKeyPEM)
	//if err != nil {
	//	log.Fatalf("Failed to parse public key from PEM: %v", err)
	//}
	//
	//// 将私钥转换为十六进制
	//privateKeyHex, err := sm2Instance.PrivateKeyToHex(privateKey)
	//if err != nil {
	//	log.Fatalf("Failed to convert private key to hex: %v", err)
	//}
	//
	//// 将公钥转换为十六进制
	//publicKeyHex, err := sm2Instance.PublicKeyToHex(publicKey)
	//if err != nil {
	//	log.Fatalf("Failed to convert public key to hex: %v", err)
	//}
	//fmt.Println("Private Key (hex):", privateKeyHex)
	//fmt.Println("Public Key (hex):", publicKeyHex)

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
	privateKeyHex := "6510d906cab1bdb8e0c950601e218e8f88733fe81113b807625707a3b3428b92"
	publicKeyHex := "2c59a63ef7ee015edfe9a2d411775fd204d5fe37b2c2617793d7b90d381ac9e81fa8348fc6dcf6cb5a9b08322fef3e585ddef297b00b067e96e8bda1473a1838"
	//从十六进制解析私钥
	privateKeyFromHex, err := sm2Instance.ParsePrivateKeyFromHex(privateKeyHex)
	if err != nil {
		log.Fatalf("Failed to parse private key from hex: %v", err)
	}

	// 从十六进制解析公钥
	publicKeyFromHex, err := sm2Instance.ParsePublicKeyFromHex(publicKeyHex)
	if err != nil {
		log.Fatalf("Failed to parse public key from hex: %v", err)
	}

	//// 加密数据并链式调用转换格式
	plaintext := []byte("hello")
	//// 直接获取十六进制字符串 - 使用压缩格式
	//hexResult, err := sm2Instance.Encrypt(publicKeyFromHex, plaintext, gogm.C1C3C2Compressed).ToHex()
	//if err != nil {
	//	log.Fatalf("Failed to encrypt data: %v", err)
	//}
	//fmt.Println("Encrypted data (hex):", hexResult)
	//
	//// 尝试使用不同的格式进行解密
	//// 首先尝试C1C3C2格式解密压缩数据
	//bytes, err := sm2Instance.DecryptFromHex(privateKeyFromHex, hexResult, gogm.C1C3C2Compressed).ToBytes()
	//if err != nil {
	//	fmt.Printf("Failed to decrypt with C1C3C2Compressed format: %v\n", err)
	//} else {
	//	fmt.Println("Decrypted data:", string(bytes))
	//}

	toBytes, err := sm2Instance.Sign(privateKeyFromHex, plaintext).ToHex()
	if err != nil {
		panic(err)
	}
	fmt.Println(toBytes)
	verify := sm2Instance.VerifyFromHex(publicKeyFromHex, plaintext, toBytes)
	fmt.Println(verify)
}
