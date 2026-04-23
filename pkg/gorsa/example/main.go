// Package main 是 RSA 加密解密和签名验证的示例程序。
// 该程序演示如何生成密钥对、保存密钥到文件、加载密钥以及进行签名验证操作。
package main

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"

	"github.com/18721889353/sunshine/pkg/gorsa"
)

func main() {
	// 获取当前文件路径信息
	_, filename, line, ok := runtime.Caller(0)
	_ = line
	_ = ok
	currentDir := filepath.Dir(filename)

	fmt.Println("当前工作目录:", currentDir)
	keys, err := gorsa.GenerateKey()
	if err != nil {
		// 处理错误：生成密钥失败时抛出异常
		panic(err)
	}
	// 保存密钥到PEM文件
	err = gorsa.SaveKeyToPemFile(keys[0], keys[1], filepath.Join(currentDir, "public_key.pem"), filepath.Join(currentDir, "private_key.pem"))
	if err != nil {
		// 处理错误
		panic(err)
	}

	// 构建证书文件的绝对路径
	publicPem := filepath.Join(currentDir, "mobile_public_key.pem")

	strJSON := `{"busiInfo":{"coupons":"2330847183529721951","duration":"30",` +
		`"memberReceiveTime":"20250928171225","msisdn":"17768670711",` +
		`"orderEffectiveTime":"20250928171225","orderId":"229610171414886776811",` +
		`"skuId":"RX001","spId":"RX001","spServiceId":"SELF0001"},` +
		`"public":{"messageType":"1000","system":"0000",` +
		`"transactionId":"2296088786297306683","transactionTime":"20250928171225"}}`

	// 尝试从文件加载密钥，如果失败则生成新的密钥对
	publicKey, err := gorsa.LoadPublicKeyFromFile(publicPem)
	if err != nil {
		panic(err)
	}
	fmt.Println(publicKey, "99999999999999")
	//privateKey, err := rsa.LoadPrivateKeyFromFile("private_key.pem")
	//if err != nil {
	//	panic(err)
	//}
	//
	//fmt.Println("Loaded keys from files")
	// 签名
	//signature, err := rsa.Sign([]byte(strJSON), privateKey)
	//if err != nil {
	//	fmt.Printf("Error signing data: %v\n", err)
	//	return
	//}
	//fmt.Println("Signature:", signature)
	signature := "M0U3kqBKr7Iaz9IaIbhqbrCvWCsB9Bd19yrNGp%2FLrNqcJpfC0gBg%2BrAxYL%2Ba6CcIrvg61d%2FeiAjevTrrRbUktzrKSdM46lU%2FH7TeF9RcwUyRDzafoXWJGL8e6dgjFjI4Zfx%2B" +
		"d4Ahb6dyGnac54b8FNKRY2DB5Sf1hVeXO8tVnHM%3D"
	decodedSignature, err := url.QueryUnescape(signature)
	if err != nil {
		fmt.Printf("Error decoding signature: %v\n", err)
		return
	}
	fmt.Println(decodedSignature)
	fmt.Println("111111111", publicKey, "11111111111")
	// 验证签名
	verified, err := gorsa.Verify([]byte(strJSON), publicKey, decodedSignature)
	if err != nil {
		fmt.Printf("Error verifying signature: %v\n", err)
		return
	}
	fmt.Println("Signature verified:", verified)
}
