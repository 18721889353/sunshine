// Package main 是国密 SM3 哈希算法的示例程序。
// 该程序演示如何使用 SM3 算法对数据进行哈希计算。
package main

import (
	"fmt"
	"log"

	"github.com/18721889353/sunshine/pkg/gogm/gosm3"
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
