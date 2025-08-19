// demo/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/es"
)

// User 用户结构体，用于映射Elasticsearch中的文档
type User struct {
	Name      string    `json:"name"`       // 用户名
	Age       int       `json:"age"`        // 年龄
	Email     string    `json:"email"`      // 邮箱
	CreatedAt time.Time `json:"created_at"` // 创建时间
}

func main() {
	// 初始化客户端，使用完全自定义的连接池配置
	config := es.GetDefaultConfig()
	config.Addresses = []string{"http://43.143.78.234:9200"} // Elasticsearch服务地址
	config.Username = "elastic"                               // 用户名
	config.Password = "elastic"                               // 密码

	// 自定义连接池参数
	config.MaxIdleConns = 30              // 最大空闲连接数
	config.MaxIdleConnsPerHost = 10       // 每个主机最大空闲连接数
	config.MaxConnsPerHost = 50           // 每个主机最大连接数
	config.IdleConnTimeout = 120 * time.Second // 空闲连接超时时间
	config.ConnectionTimeout = 10 * time.Second // 连接超时时间

	// 自定义重试参数
	config.MaxRetries = 5                    // 最大重试次数
	config.RetryBackoff = 200 * time.Millisecond // 重试间隔

	// 验证配置
	if err := config.Validate(); err != nil {
		log.Fatal("Invalid configuration:", err)
	}

	client, err := es.NewClient(config)
	if err != nil {
		log.Fatal("Failed to create client:", err)
	}

	// 连接检查使用较短的超时
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connectCancel()

	if err := client.Ping(); err != nil {
		log.Fatal("ES connection failed:", err)
	}

	// 检查集群健康状态
	health, err := client.HealthCheck(connectCtx)
	if err != nil {
		log.Fatal("Failed to check cluster health:", err)
	}
	fmt.Printf("Cluster health status: %s\n", health)

	info, err := client.Info()
	if err != nil {
		log.Fatal("Failed to get ES info:", err)
	}

	fmt.Printf("Connected to Elasticsearch cluster: %s (version: %s)\n",
		info["cluster_name"], info["version"].(map[string]interface{})["number"])

	// 其他操作使用配置的超时时间
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// 批量索引文档以测试连接池
	users := make([]map[string]interface{}, 0)
	for i := 0; i < 25; i++ {
		users = append(users, map[string]interface{}{
			"name":       fmt.Sprintf("用户%d", i),            // 用户名
			"age":        20 + i,                            // 年龄
			"email":      fmt.Sprintf("user%d@example.com", i), // 邮箱
			"created_at": time.Now(),                        // 创建时间
		})
	}

	// 使用批量操作索引用户数据
	if err := client.Document().BulkIndex(ctx, "users", users); err != nil {
		log.Fatal("Bulk index failed:", err)
	}
	fmt.Println("Bulk indexing completed")

	// 搜索文档
	searchReq := es.SearchRequest{
		Query: map[string]interface{}{
			"range": map[string]interface{}{
				"age": map[string]interface{}{
					"gte": 20, // 年龄大于等于20
					"lte": 45, // 年龄小于等于45
				},
			},
		},
		Size: 20, // 返回20条记录
	}

	searchResult, err := client.Search().Search(ctx, "users", searchReq)
	if err != nil {
		log.Fatal("Search failed:", err)
	}

	fmt.Printf("Found %d documents\n", searchResult.Hits.Total.Value)

	// 打印搜索结果
	for _, hit := range searchResult.Hits.Hits {
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("Failed to unmarshal document %s: %v", hit.ID, err)
			continue
		}
		fmt.Printf("Document ID: %s, Score: %.2f, User: %+v\n", hit.ID, hit.Score, user)
	}

	// 分页查询示例
	fmt.Println("\n=== 分页查询示例 ===")
	paginatedReq := es.PaginatedSearchRequest{
		Query: map[string]interface{}{
			"match_all": map[string]interface{}{}, // 匹配所有文档
		},
		Pagination: es.Pagination{
			Page:     2,  // 第2页
			PageSize: 5,  // 每页5条记录
		},
		Sort: map[string]interface{}{
			"age": map[string]interface{}{
				"order": "asc", // 按年龄升序排列
			},
		},
	}

	paginatedResult, err := client.Search().SearchWithPagination(ctx, "users", paginatedReq)
	if err != nil {
		log.Fatal("Paginated search failed:", err)
	}

	fmt.Printf("第%d页，共%d页，总共%d条记录\n", 
		paginatedResult.Pagination.Page,
		paginatedResult.Pagination.TotalPages,
		paginatedResult.Pagination.Total)

	for _, hit := range paginatedResult.Hits.Hits {
		var user User
		if err := json.Unmarshal(hit.Source, &user); err != nil {
			log.Printf("Failed to unmarshal document %s: %v", hit.ID, err)
			continue
		}
		fmt.Printf("Document ID: %s, Score: %.2f, User: %+v\n", hit.ID, hit.Score, user)
	}

	// Scroll API 示例
	fmt.Println("\n=== Scroll API 示例 ===")
	scrollReq := es.SearchRequest{
		Query: map[string]interface{}{
			"match_all": map[string]interface{}{}, // 匹配所有文档
		},
		Size: 10, // 每次滚动返回10条记录
		Sort: []map[string]interface{}{
			{"age": map[string]interface{}{"order": "asc"}},   // 按年龄升序
			{"_id": map[string]interface{}{"order": "asc"}},   // 按ID升序
		},
	}

	// 初始化 Scroll 搜索
	scrollResult, err := client.Search().ScrollSearch(ctx, "users", scrollReq, 1*time.Minute)
	if err != nil {
		log.Fatal("Scroll search failed:", err)
	}

	fmt.Printf("通过 Scroll 获取到 %d 个文档\n", len(scrollResult.Hits.Hits))

	// 继续 Scroll 搜索
	if scrollResult.ScrollID != "" {
		nextScrollResult, err := client.Search().ScrollContinue(ctx, scrollResult.ScrollID, 1*time.Minute)
		if err != nil {
			log.Fatal("Continue scroll search failed:", err)
		}
		
		fmt.Printf("通过继续 Scroll 获取到 %d 个文档\n", len(nextScrollResult.Hits.Hits))
		
		// 清除 Scroll 上下文
		err = client.Search().ScrollClear(ctx, []string{scrollResult.ScrollID})
		if err != nil {
			log.Printf("Warning: Failed to clear scroll context: %v", err)
		}
	}
}