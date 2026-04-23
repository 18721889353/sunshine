// Package main 是国密 SM2/SM3/SM4 算法的示例程序。
// 该程序演示如何使用国密算法进行加密、解密、签名和验签操作。
package main

import (
	"crypto"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/tjfoc/gmsm/sm2"
	"github.com/tjfoc/gmsm/sm3"
	"github.com/tjfoc/gmsm/sm4"
	"github.com/tjfoc/gmsm/x509"
)

func main() {
	// 获取当前文件路径信息
	_, filename, line, ok := runtime.Caller(0)
	_ = line
	_ = ok
	currentDir := filepath.Dir(filename)

	fmt.Println("当前工作目录:", currentDir)

	// 构建证书文件的绝对路径
	zgyhPublicPem := filepath.Join(currentDir, "cert", "public.key")
	sm2PrivatePem := filepath.Join(currentDir, "cert", "private.key")

	fmt.Println("----------------------body---------------------------")
	json := `{"customerName":"test","mobile":"18721889351",` +
		`"createDate":"2021\/01\/15 15:18:55","identityNumber":"32038219961025702X",` +
		`"customerId":"276005749","identityType":"1","ibknum":"44433","gender":"2"}`
	base64json := base64.StdEncoding.EncodeToString([]byte(json))
	sec := "1234567890abcdef"
	ecbDec1, err := sm4.Sm4Ecb([]byte(sec), []byte(base64json), true) //sm4Ecb模式pksc7填充解密
	if err != nil {
		fmt.Printf("sm4 dec error:%s\n", err.Error())
		return
	}
	body := base64.StdEncoding.EncodeToString(ecbDec1)
	fmt.Println(body)
	fmt.Println("----------------------skey1---------------------------")
	publicKey1, err1 := ReadPublicPem(zgyhPublicPem)
	if err1 != nil {
		fmt.Println("ReadPublicPem Error: ", err1)
	}
	skey := Encrypt(sec, publicKey1)
	fmt.Println(skey)
	fmt.Println("----------------------hmaCipherText1---------------------------")
	aa := body + sec
	h := sm3.New()
	h.Write([]byte(aa))
	sum := h.Sum(nil)
	hmaCipherText := hex.EncodeToString(sum)
	fmt.Println(hmaCipherText)
	fmt.Println("--------------------------hmac1-----------------------")
	privateKey1, err1 := ReadPrivatePem(sm2PrivatePem, nil)
	if err1 != nil {
		fmt.Println("ReadPrivatePem Error: ", err1)
	}
	signByte, err := privateKey1.Sign(rand.Reader, sum, nil)
	if err1 != nil {
		fmt.Println("privateKey1.Sign Error: ", err)
	}
	hmac := hex.EncodeToString(signByte)
	fmt.Println(hmac)
	fmt.Println("-------------------------------------------------")
	//cipherTex := "{\"hmaCipherText\":\"2a77153e34953c6bd09527ae22bb7ce0de78e0930cb89dab109bb7a388b5b2c1\"," +
	//	"\"hmac\":\"3045022100b0256f7169ff892e513e9c884fbecf7e677c36c0027f3ad26a6943749e620c2702200f6a506b34c6f07b35dd99aa8bfcfc062706f53bff6992c3b4813a72a2d7c50a\"," +
	//	"\"skey\":\"BP41O41m+FOB3OENnE/Ztb3OXC7i6+I9QtLprobm4yUdj1d8K2wuT7Cl4xlRPmGqFl9gzNeXVe2J4OICpTIal9faWKRad4Ooa3EE6G1nlN2Vq0TBJiU4n545QMLD4JzQMembK/95qp/SizyT31UHWM54r5Ohq7loGEbQ15Y7lME5\"," +
	//	"\"body\":\"b25e0eedded3f43962f3aa9f7b9ee2a8cca70e6ed50c89f26be84104225a55c3668c2c74b11ba34aecd37779be3d0ee5c4c721cd3190d2a2154c1166896f10f1d274e2082e52784ea72a86" +
	//	    "20ca8b5d2cca5df803c4efdced90f2714e04fb416cddea0e56d7d4810abbd3bed90e767cad549785b8907163b7d88d800bd64f5b308ad2012fb0ed00c713529bca646227021a9cc7242a38" +
	//	    "39ad4a88b7fdabca56316bf0d4288de21834221d403d475b6910bd5b10fed224415d568ee2d1575e6d50561e32b8f33e03f16d2f7f79fd8ae08f52d549fcf7f17667d2f4b47611bd27b91e" +
	//	    "80676ec8325936ca3ea6197c12c2cd057ca2071247114f41b338007d7dec1910f8b2b31689e5df22b1afa70058ccaa\"}"
	//skey := "BP41O41m+FOB3OENnE/Ztb3OXC7i6+I9QtLprobm4yUdj1d8K2wuT7Cl4xlRPmGqFl9gzNeXVe2J4OICpTIal9faWKRad4Ooa3EE6G1nlN2Vq0TBJiU4n545QMLD4JzQMembK/95qp/SizyT31UHWM54r5Ohq7loGEbQ15Y7lME5"
	//hmaCipherText := "2a77153e34953c6bd09527ae22bb7ce0de78e0930cb89dab109bb7a388b5b2c1"
	//hmac := "3045022100b0256f7169ff892e513e9c884fbecf7e677c36c0027f3ad26a6943749e620c2702200f6a506b34c6f07b35dd99aa8bfcfc062706f53bff6992c3b4813a72a2d7c50a"
	//body = "b25e0eedded3f43962f3aa9f7b9ee2a8cca70e6ed50c89f26be84104225a55c3668c2c74b11ba34aecd37779be3d0ee5c4c721cd3190d2a2154c1166896f10f1d274e2082e52784ea72a8620ca8b5d2cca5df803c4efdced90f2" +
	//    "714e04fb416cddea0e56d7d4810abbd3bed90e767cad549785b8907163b7d88d800bd64f5b308ad2012fb0ed00c713529bca646227021a9cc7242a3839ad4a88b7fdabca56316bf0d4288de21834221d403d475b6910bd5b10fe" +
	//    "d224415d568ee2d1575e6d50561e32b8f33e03f16d2f7f79fd8ae08f52d549fcf7f17667d2f4b47611bd27b91e80676ec8325936ca3ea6197c12c2cd057ca2071247114f41b338007d7dec1910f8b2b31689e5df22b1afa70058" +
	//    "ccaa"
	//fmt.Println(hmaCipherText, hmac, skey, body)
	fmt.Println("-----------------SM2私钥解密skey获得SM4密钥----------------------------")
	privateKey, err := ReadPrivatePem(sm2PrivatePem, nil)
	if err != nil {
		fmt.Println("ReadPrivatePem Error: ", err)
	}
	str, err := base64.StdEncoding.DecodeString(skey)
	if err != nil {
		fmt.Println("base64.StdEncoding.DecodeString Error: ", err)
	}
	sm4Key, err := Decode(str, privateKey)
	if err != nil {
		fmt.Println("Decode Error: ", err)
		panic(err)
	}
	fmt.Println("SM4密钥:", sm4Key)
	fmt.Println("----------------sm2验签数据--------------------------")
	publicKey, err := ReadPublicPem(zgyhPublicPem)
	if err != nil {
		fmt.Println("ReadPublicPem Error: ", err)
	}
	hmaCipherTextBytes, err := hex.DecodeString(hmaCipherText)
	if err != nil {
		fmt.Println("hex.DecodeString Error: ", err)
	}

	sign, err := hex.DecodeString(hmac)
	if err != nil {
		fmt.Println("hex.DecodeString Error: ", err)
	}
	verify := publicKey.Verify(hmaCipherTextBytes, sign)
	fmt.Println(verify)
	fmt.Println("----------------SM3摘要校验-----------------------")
	h1 := sm3.New()
	h1.Write([]byte(body + sm4Key))
	sum1 := h1.Sum(nil)
	cc := hex.EncodeToString(sum1)
	fmt.Println(cc == hmaCipherText)
	fmt.Println("----------------sm4解密body数据-----------------------")
	data, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		fmt.Printf("hex.DecodeString error:%s\n", err.Error())
		return
	}
	ecbDec, err := sm4.Sm4Ecb([]byte(sm4Key), data, false) //sm4Ecb模式pksc7填充解密
	if err != nil {
		fmt.Printf("sm4 dec error:%s\n", err.Error())
		return
	}
	decodeString, _ := base64.StdEncoding.DecodeString(string(ecbDec))
	fmt.Printf("%s\n", decodeString)
	fmt.Println("------------------------------------------------------------------------------")
}

func Base64encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
func Base64Decode(data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(data)
}

// CreateSM2Key 随机生成 SM2 公私钥对
func CreateSM2Key() (privateKey *sm2.PrivateKey, publicKey *sm2.PublicKey, err error) {
	// 生成 SM2 秘钥对
	privateKey, err = sm2.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	// 进行 SM2 公钥断言
	publicKey = privateKey.Public().(*sm2.PublicKey)
	return privateKey, publicKey, nil
}

// CreatePrivatePem
/**
 *  @Description: 创建私钥文件
 *  @param privateKey 私钥
 *  @param pwd 私钥密码
 *  @param path 生成的私钥文件路径
 *  @return err
 */
func CreatePrivatePem(privateKey *sm2.PrivateKey, pwd []byte, path string) (err error) {
	// 将私钥反序列化并进行pem编码
	var privateKeyToPem []byte

	privateKeyToPem, err = x509.WritePrivateKeyToPem(privateKey, pwd)
	if err != nil {
		return err
	}
	// 将私钥写入磁盘
	if path == "" {
		// 获取当前文件路径信息
		_, filename, _, _ := runtime.Caller(0)
		currentDir := filepath.Dir(filename)
		path = filepath.Join(currentDir, "cert", "sm2Private.Pem")
	}
	// 获取文件中的路径
	paths, _ := filepath.Split(path)
	err = os.MkdirAll(paths, os.ModePerm)
	if err != nil {
		return err
	}
	var file *os.File
	file, err = os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = file.Write(privateKeyToPem)
	if err != nil {
		return err
	}
	return nil
}

// CreatePublicPem
/**
*  @Description: 创建公钥文件
*  @param publicKey 公钥
*  @param path 生成的公钥文件路径
*  @return err
 */
func CreatePublicPem(publicKey *sm2.PublicKey, path string) (err error) {
	// 将私钥反序列化并进行pem编码
	var publicKeyToPem []byte
	publicKeyToPem, err = x509.WritePublicKeyToPem(publicKey)
	if err != nil {
		return err
	}
	// 将公钥写入磁盘
	if path == "" {
		// 获取当前文件路径信息
		_, filename, _, _ := runtime.Caller(0)
		currentDir := filepath.Dir(filename)
		path = filepath.Join(currentDir, "cert", "sm2Public.Pem")
	}
	// 获取文件中的路径
	paths, _ := filepath.Split(path)
	err = os.MkdirAll(paths, os.ModePerm)
	if err != nil {
		return err
	}
	var file *os.File
	file, err = os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = file.Write(publicKeyToPem)
	if err != nil {
		return err
	}
	return nil
}

// ReadPrivatePem 读取私钥文件
func ReadPrivatePem(path string, pwd []byte) (privateKey *sm2.PrivateKey, err error) {
	// 如果路径是相对路径，则基于当前文件位置构建绝对路径
	if !filepath.IsAbs(path) {
		_, filename, _, _ := runtime.Caller(0)
		currentDir := filepath.Dir(filename)
		path = filepath.Join(currentDir, path)
	}

	// 打开文件读取私钥
	var file *os.File
	file, err = os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var fileInfo os.FileInfo
	fileInfo, err = file.Stat()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, fileInfo.Size())
	_, err = file.Read(buf)
	if err != nil {
		return nil, err
	}
	// 将 PEM 格式私钥文件进行反序列化
	privateKey, err = x509.ReadPrivateKeyFromPem(buf, pwd)
	if err != nil {
		return nil, err
	}
	return privateKey, nil
}

// ReadPublicPem 读取公钥文件
func ReadPublicPem(path string) (publicKey *sm2.PublicKey, err error) {
	// 如果路径是相对路径，则基于当前文件位置构建绝对路径
	if !filepath.IsAbs(path) {
		_, filename, _, _ := runtime.Caller(0)
		currentDir := filepath.Dir(filename)
		path = filepath.Join(currentDir, path)
	}

	// 打开文件读取公钥
	var file *os.File
	file, err = os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var fileInfo os.FileInfo
	fileInfo, err = file.Stat()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, fileInfo.Size())
	_, err = file.Read(buf)
	if err != nil {
		return nil, err
	}
	// 将 PEM 格式公钥文件进行反序列化
	publicKey, err = x509.ReadPublicKeyFromPem(buf)
	if err != nil {
		return nil, err
	}
	return publicKey, nil
}

// Encrypt
/**
*  @Description: SM2加密（公钥加密）
*  @param data 需要加密的数据
*  @param publicKey 公钥
*  @return cipherStr 加密后的字符串
 */
func Encrypt(data string, publicKey *sm2.PublicKey) (cipherStr string) {
	// 将字符串转为[]byte
	dataByte := []byte(data)
	// sm2加密
	//cipherTxt, err := publicKey.EncryptAsn1(dataByte, rand.Reader)
	cipherTxt, err := sm2.Encrypt(publicKey, dataByte, rand.Reader, sm2.C1C3C2)
	if err != nil {
		// 在 main 或 init 函数外不应使用 log.Fatal，返回错误
		panic(fmt.Sprintf("SM2 加密失败: %v", err))
	}
	// 转为16进制字符串输出
	//cipherStr = fmt.Sprintf("%x", cipherTxt)
	return base64.StdEncoding.EncodeToString(cipherTxt)
	// cipherStr = hex.EncodeToString(cipherTxt)
	// return
}

// Decode
/**
*  @Description: SM2解密（私钥解密）
*  @param cipherStr 加密后的字符串
*  @param privateKey 私钥
*  @return data 解密后的数据
*  @return err
 */
func Decode(cipherStr []byte, privateKey *sm2.PrivateKey) (data string, err error) {
	// sm2解密
	var dataByte []byte
	//dataByte, err = privateKey.DecryptAsn1(bytes)
	dataByte, err = sm2.Decrypt(privateKey, cipherStr, sm2.C1C3C2)

	if err != nil {
		return data, err
	}
	// byte数组直接转成string，优化内存
	str := (*string)(unsafe.Pointer(&dataByte))
	return *str, err
}

// Sign
/**
 *  @Description: 签名
 *  @param msg 需要签名的内容
 *  @param privateKey 私钥
 *  @param signer
 *  @return sign
 *  @return err
 */
func Sign(dataByte []byte, privateKey *sm2.PrivateKey, signer crypto.SignerOpts) (sign string, err error) {
	//dataByte := []byte(msg)
	var signByte []byte
	// sm2签名
	signByte, err = privateKey.Sign(rand.Reader, dataByte, signer)
	if err != nil {
		return "", err
	}
	// 转为16进制字符串输出
	sign = hex.EncodeToString(signByte)
	return sign, nil
}

// Verify
/**
*  @Description: 验签
*  @param msg 需要验签的内容
*  @param sign 验签
*  @param publicKey 公钥
*  @return verify
 */
func Verify(msgBytes []byte, sign string, publicKey *sm2.PublicKey) (verify bool) {
	// 16进制字符串转[]byte
	//msgBytes := []byte(msg)
	signBytes, _ := hex.DecodeString(sign)
	// sm2 验签
	verify = publicKey.Verify(msgBytes, signBytes)
	return verify
}
