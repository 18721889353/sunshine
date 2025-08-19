package main

import (
	"context"
	"fmt"
	"github.com/18721889353/sunshine/pkg/es"
	"log"
)

type User struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email"`
}

func main() {
	// 初始化客户端
	config := es.Config{
		Addresses: []string{"http://localhost:9200"},
		Username:  "elastic",
		Password:  "changeme",
	}

	client, err := es.NewClient(config)
	if err != nil {
		log.Fatal(err)
	}

	// 检查连接
	if err := client.Ping(); err != nil {
		log.Fatal("ES connection failed:", err)
	}

	ctx := context.Background()

	// 索引文档
	user := User{
		Name:  "张三",
		Age:   25,
		Email: "zhangsan@example.com",
	}

	if err := client.Document().Index(ctx, "users", user, "1"); err != nil {
		log.Fatal("Index document failed:", err)
	}

	// 获取文档
	var result User
	if err := client.Document().Get(ctx, "users", "1", &result); err != nil {
		log.Fatal("Get document failed:", err)
	}

	fmt.Printf("Retrieved user: %+v\n", result)

	// 搜索文档
	searchReq := es.SearchRequest{
		Query: map[string]interface{}{
			"match": map[string]interface{}{
				"name": "张三",
			},
		},
		Size: 10,
	}

	searchResult, err := client.Search().Search(ctx, "users", searchReq)
	if err != nil {
		log.Fatal("Search failed:", err)
	}

	fmt.Printf("Found %d documents\n", searchResult.Hits.Total.Value)
}
