// Package main 演示 jsonutil 包的使用示例。
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/utils/jsonutil"
)

// 定义一个订单结构体用于演示
type Order struct {
	ID        int64                  `json:"id"`
	UserID    int64                  `json:"user_id"`
	Items     []OrderItem            `json:"items"`
	Total     float64                `json:"total"`
	CreatedAt time.Time              `json:"created_at"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type OrderItem struct {
	ProductID int64   `json:"product_id"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Quantity  int     `json:"quantity"`
}

func main() {
	fmt.Println("=== jsonutil 包使用示例 ===")

	// 1. 基本序列化和反序列化
	fmt.Println("\n1. 基本序列化和反序列化:")
	order := Order{
		ID:     1001,
		UserID: 12345,
		Items: []OrderItem{
			{ProductID: 1, Name: "笔记本电脑", Price: 5999.99, Quantity: 1},
			{ProductID: 2, Name: "无线鼠标", Price: 199.99, Quantity: 2},
		},
		Total:     6399.97,
		CreatedAt: time.Now(),
		Metadata: map[string]interface{}{
			"source": "web",
			"promo":  "WELCOME10",
		},
	}

	// 序列化订单
	jsonData, err := jsonutil.Marshal(order)
	if err != nil {
		log.Fatalf("序列化失败: %v", err)
	}
	fmt.Printf("序列化结果:\n%s\n", string(jsonData))

	// 反序列化订单
	var newOrder Order
	err = jsonutil.Unmarshal(jsonData, &newOrder)
	if err != nil {
		log.Fatalf("反序列化失败: %v", err)
	}
	fmt.Printf("反序列化成功，订单ID: %d\n", newOrder.ID)

	// 2. 使用带缩进的格式化输出
	fmt.Println("\n2. 格式化JSON输出:")
	prettyJSON, err := jsonutil.MarshalIndent(order, "", "  ")
	if err != nil {
		log.Fatalf("格式化序列化失败: %v", err)
	}
	fmt.Printf("格式化JSON:\n%s\n", string(prettyJSON))

	// 3. 验证JSON有效性
	fmt.Println("\n3. JSON有效性验证:")
	testJSON := `{"valid": "json", "data": 123}`
	invalidJSON := `{"invalid": json, "missing": quote}`

	fmt.Printf("有效JSON验证: %t\n", jsonutil.IsValid([]byte(testJSON)))
	fmt.Printf("无效JSON验证: %t\n", jsonutil.IsValid([]byte(invalidJSON)))

	// 4. 使用自定义配置
	fmt.Println("\n4. 自定义配置示例:")
	type SafeData struct {
		Script string `json:"script"`
	}

	safeData := SafeData{Script: "<script>alert('xss')</script>"}

	// 启用HTML转义的配置
	config := jsonutil.Config{
		EscapeHTML: true,
	}

	escapedData, err := jsonutil.MarshalWithConfig(safeData, config)
	if err != nil {
		log.Fatalf("带配置序列化失败: %v", err)
	}
	fmt.Printf("启用HTML转义: %s\n", string(escapedData))

	// 5. 严格验证反序列化
	fmt.Println("\n5. 严格验证反序列化:")
	strictJSON := `{"id": 1002, "user_id": 12345, "total": 100.0}`
	invalidStrictJSON := `{"id": 1002, "user_id": 12345, "total": 100.0, "unknown_field": "should_fail"}`

	var order2 Order
	err = jsonutil.UnmarshalWithValidation([]byte(strictJSON), &order2)
	if err != nil {
		log.Printf("严格验证反序列化失败: %v", err)
	} else {
		fmt.Printf("严格验证成功，订单ID: %d\n", order2.ID)
	}

	var order3 Order
	err = jsonutil.UnmarshalWithValidation([]byte(invalidStrictJSON), &order3)
	if err != nil {
		fmt.Printf("正确拒绝了包含未知字段的JSON: %v\n", err)
	} else {
		fmt.Printf("错误：应该拒绝包含未知字段的JSON\n")
	}

	// 6. 性能对比示例（简单演示）
	fmt.Println("\n6. 性能对比示例:")
	// 这里只是简单演示，实际性能测试需要运行基准测试
	start := time.Now()
	for i := 0; i < 1000; i++ {
		_, _ = jsonutil.Marshal(order)
	}
	jsonutilTime := time.Since(start)

	fmt.Printf("jsonutil 1000次序列化耗时: %v\n", jsonutilTime)

	fmt.Println("\n=== 示例执行完成 ===")
}
