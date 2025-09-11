package main

import (
	"fmt"
	"github.com/18721889353/sunshine/pkg/gogm/gosm3"
	"log"
)

func main() {
	// 创建SM3实例
	sm3 := gosm3.NewSM3()
	data := "Hello, SM3!"
	// 对字符串进行哈希
	hashResult, err := sm3.HashString(data).ToHex()
	if err != nil {
		log.Fatalf("Failed to hash data: %v", err)
	}

	fmt.Printf("Data: %s\n", data)
	fmt.Printf("SM3 Hash: %s\n", hashResult)

	// 对字节数据进行哈希
	hashResult, err = sm3.Hash([]byte(data)).ToHex()
	if err != nil {
		log.Fatalf("Failed to hash byte data: %v", err)
	}

	fmt.Printf("Data: %s\n", data)
	fmt.Printf("SM3 Hash: %s\n", hashResult)

}
