// demo/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/es"
	// 添加客户端方法的扩展
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
	config.Username = "elastic"                              // 用户名
	config.Password = "elastic"                              // 密码

	// 自定义连接池参数
	config.MaxIdleConns = 30                    // 最大空闲连接数
	config.MaxIdleConnsPerHost = 10             // 每个主机最大空闲连接数
	config.MaxConnsPerHost = 50                 // 每个主机最大连接数
	config.IdleConnTimeout = 120 * time.Second  // 空闲连接超时时间
	config.ConnectionTimeout = 10 * time.Second // 连接超时时间

	// 自定义重试参数
	config.MaxRetries = 5                        // 最大重试次数
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

	// 1.5 删除文档
	if err := client.Document().Delete(ctx, "users", "1"); err != nil {
		log.Fatal("Delete document failed:", err)
	}
	fmt.Println("文档删除成功")

	// 1. 单文档操作演示
	fmt.Println("\n=== 单文档操作演示 ===")

	// 1.1 索引单个文档
	user := User{
		Name:      "张三",
		Age:       25,
		Email:     "zhangsan@example.com",
		CreatedAt: time.Now(),
	}

	if err := client.Document().Index(ctx, "users", user, "1"); err != nil {
		log.Fatal("Index document failed:", err)
	}
	fmt.Println("单个文档索引完成")

	// 1.2 获取文档
	var retrievedUser User
	if err := client.Document().Get(ctx, "users", "1", &retrievedUser); err != nil {
		log.Fatal("Get document failed:", err)
	}
	fmt.Printf("获取到的文档: %+v\n", retrievedUser)

	// 1.3 更新文档
	updateData := map[string]interface{}{
		"age": 26,
	}
	if err := client.Document().Update(ctx, "users", "1", updateData); err != nil {
		log.Fatal("Update document failed:", err)
	}
	fmt.Println("文档更新完成")

	// 1.4 再次获取文档验证更新
	if err := client.Document().Get(ctx, "users", "1", &retrievedUser); err != nil {
		log.Fatal("Get document failed:", err)
	}
	fmt.Printf("更新后的文档: %+v\n", retrievedUser)

	// 2. 批量操作演示
	fmt.Println("\n=== 批量操作演示 ===")

	// 2.1 批量索引文档
	users := make([]map[string]interface{}, 0)
	for i := 0; i < 5; i++ {
		users = append(users, map[string]interface{}{
			"name":       fmt.Sprintf("用户%d", i),
			"age":        20 + i,
			"email":      fmt.Sprintf("user%d@example.com", i),
			"created_at": time.Now(),
		})
	}

	if err := client.Document().BulkIndex(ctx, "users", users); err != nil {
		log.Fatal("Bulk index failed:", err)
	}
	fmt.Println("批量索引完成")

	// 2.2 批量创建文档
	newUsers := make([]map[string]interface{}, 0)
	for i := 10; i < 15; i++ {
		newUsers = append(newUsers, map[string]interface{}{
			"name":       fmt.Sprintf("新用户%d", i),
			"age":        30 + i,
			"email":      fmt.Sprintf("newuser%d@example.com", i),
			"created_at": time.Now(),
		})
	}

	if err := client.Document().BulkCreate(ctx, "users", newUsers); err != nil {
		log.Fatal("Bulk create failed:", err)
	}
	fmt.Println("批量创建完成")

	// 2.4 批量删除文档
	idsToDelete := []string{"2", "3"}
	if err := client.Document().BulkDelete(ctx, "users", idsToDelete); err != nil {
		log.Fatal("Bulk delete failed:", err)
	}
	fmt.Println("批量删除完成")

	// 2.5 直接使用Bulk接口进行混合操作
	operations := []es.BulkOperation{
		{Index: "users", ID: "mixed_1", Action: "index", Payload: map[string]interface{}{"name": "混合操作用户1", "age": 40}},
		{Index: "users", ID: "mixed_2", Action: "create", Payload: map[string]interface{}{"name": "混合操作用户2", "age": 41}},
		{Index: "users", ID: "4", Action: "update", Payload: map[string]interface{}{"doc": map[string]interface{}{"age": 99}}},
		{Index: "users", ID: "10", Action: "delete"},
	}

	if err := client.Bulk().BulkExecute(ctx, operations); err != nil {
		log.Fatal("Mixed bulk execute failed:", err)
	}
	fmt.Println("混合批量操作完成")

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

	// 3.2 使用原始查询搜索
	rawQuery := []byte(`{
		"query": {
			"match": {
				"name": "用户"
			}
		},
		"size": 10
	}`)

	rawSearchResult, err := client.Search().SearchWithRawQuery(ctx, "users", rawQuery)
	if err != nil {
		log.Fatal("Raw query search failed:", err)
	}

	fmt.Printf("\n使用原始查询找到 %d 个文档\n", rawSearchResult.Hits.Total.Value)

	// 3.3 分页查询示例
	fmt.Println("\n=== 分页查询示例 ===")
	paginatedReq := es.PaginatedSearchRequest{
		Query: map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
		Pagination: es.Pagination{
			Page:     1,
			PageSize: 3,
		},
		Sort: map[string]interface{}{
			"age": map[string]interface{}{
				"order": "asc",
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

	// 3.4 Scroll API 示例
	fmt.Println("\n=== Scroll API 示例 ===")
	scrollReq := es.SearchRequest{
		Query: map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
		Size: 5,
		Sort: []map[string]interface{}{
			{"age": map[string]interface{}{"order": "asc"}},
			{"_id": map[string]interface{}{"order": "asc"}},
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

	// 3.5 Search After 示例
	fmt.Println("\n=== Search After 示例 ===")
	searchAfterReq := es.SearchRequestWithSearchAfter{
		SearchRequest: es.SearchRequest{
			Query: map[string]interface{}{
				"match_all": map[string]interface{}{},
			},
			Size: 3,
			Sort: []map[string]interface{}{
				{"age": map[string]interface{}{"order": "asc"}},
				{"_id": map[string]interface{}{"order": "asc"}},
			},
		},
	}

	searchAfterResult, err := client.Search().SearchWithSearchAfter(ctx, "users", searchAfterReq)
	if err != nil {
		log.Fatal("Search after failed:", err)
	}

	fmt.Printf("通过 Search After 获取到 %d 个文档\n", len(searchAfterResult.Hits.Hits))

	// 4. 集群信息演示
	fmt.Println("\n=== 集群信息演示 ===")

	// 获取集群健康状态
	health, err = client.HealthCheck(ctx)
	if err != nil {
		log.Fatal("Failed to check cluster health:", err)
	}
	fmt.Printf("集群健康状态: %s\n", health)

	// 获取集群信息
	info, err = client.Info()
	if err != nil {
		log.Fatal("Failed to get cluster info:", err)
	}
	fmt.Printf("集群名称: %s, 版本: %s\n",
		info["cluster_name"],
		info["version"].(map[string]interface{})["number"])

	fmt.Println("\n=== 所有功能演示完成 ===")
}
